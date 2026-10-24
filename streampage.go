package brick

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"maps"
	"net/http"
	"slices"
	"strconv"
	"sync"
)

// Streamed page render. A brick:lazy="stream" island is not
// fetched later like the other lazy modes: it is rendered while the page
// is already on its way, and written into the same response when it is
// ready. The page goes out first with a placeholder holding the boundary's
// loading block; each island follows, in completion order, as a hidden
// <brick-fill> that a tiny inline script moves into its placeholder.
//
// Why a <brick-fill hidden> and not the spec's <template>: a <template>
// shows nothing without JavaScript, and a JS-off browser still makes a
// navigation request, so it would sit on loading blocks forever. Each fill
// carries a <noscript><style> that, only when scripting is off, hides its
// placeholder and shows the fill (author display: contents beats the
// UA's [hidden] rule), so a no-JS reader sees the content, at the end of
// the body. With JavaScript on, the <noscript> is inert text and hidden
// keeps the fill invisible until it is moved, so nothing flashes.
const (
	// DefaultStreamWorkers is Options.StreamWorkers' default.
	DefaultStreamWorkers = 4
	// streamTag is the fill element; the placeholder is lazyTag.
	streamTag = "brick-fill"
	// streamIDPrefix starts every placeholder id, so ids are engine-made
	// ASCII ([a-z0-9]) and never need escaping in a selector.
	streamIDPrefix = "s"
	// streamApplier is the inline script that moves a fill into its
	// placeholder as soon as it is parsed. The runtime is a module script,
	// which runs only after the whole response, so without this the page
	// would stream bytes and show them only at the end. It skips a fill
	// that carries assets: the runtime links those first, then moves it.
	// It is defined once, by the first fill's script.
	streamApplier = `window.__brickFill=window.__brickFill||function(i){var d=document,f=d.querySelector('brick-fill[data-brick-fill="'+i+'"]'),p=d.querySelector('brick-lazy[data-brick-stream="'+i+'"]');if(!f||!p||f.hasAttribute("data-brick-assets"))return;var r=d.createDocumentFragment();while(f.firstChild)r.appendChild(f.firstChild);p.replaceWith(r);f.remove()};`
)

// HasStream reports whether the result holds a brick:lazy="stream"
// island: a host that can flush its response renders it with
// Engine.FillStream or Engine.Stream instead of Fill.
func (r *Result) HasStream() bool {
	for _, c := range r.Chunks {
		if c.Kind == ChunkIsland && c.Island != nil && c.Island.Lazy == LazyStream {
			return true
		}
	}
	return false
}

// Fills are a streamed page's islands still to render (Engine.FillStream).
type Fills struct {
	e        *Engine
	r        *Result
	ro       renderOptions
	owners   map[string]string
	islands  []*Island
	ids      []string
	linked   map[string]bool
	workers  int
	consumed bool
	// failed are the islands' errors that Each wrote a fallback for; read
	// only by Failed, after Each returns.
	mu     sync.Mutex
	failed []error
}

// Failed are the errors of the islands that Each wrote a fallback or error
// block for, in completion order, for the host's log: the response itself
// stays a success. Call it after Each returns. A loader's answer
// (redirect, status) is not among them, as it is not a fault.
func (f *Fills) Failed() []error {
	if f == nil {
		return nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.failed)
}

// Len is how many fills Each will write.
func (f *Fills) Len() int {
	if f == nil {
		return 0
	}
	return len(f.islands)
}

// FillStream is Fill for a response that can be flushed: it renders every
// island except the brick:lazy="stream" ones, writes each of those as a
// placeholder showing its boundary's loading block, and returns the page
// with the Fills still to render. fills is nil when the page has no stream
// island, and page is then exactly Fill's bytes. A Dev-mode island
// failure is returned with the page, as Fill returns it.
func (e *Engine) FillStream(ctx context.Context, r *Result, opts ...RenderOption) (page []byte, fills *Fills, err error) {
	var ro renderOptions
	for _, o := range opts {
		o(&ro)
	}
	if ro.err != nil {
		return nil, nil, ro.err
	}
	if !r.HasStream() {
		page, err = e.Fill(ctx, r, opts...)
		return page, nil, err
	}
	var ids []string
	placeholder := func(isl *Island) []Chunk {
		id := streamIDPrefix + strconv.Itoa(len(ids)+1)
		ids = append(ids, id)
		out := []Chunk{{Kind: ChunkBytes, Bytes: []byte(`<` + lazyTag + ` data-brick-stream="` + id + `">`)}}
		out = append(out, isl.Loading...)
		return append(out, Chunk{Kind: ChunkBytes, Bytes: []byte(`</` + lazyTag + `>`)})
	}
	out, deferred, firstErr, err := e.fill(ctx, r, &ro, placeholder)
	if err != nil {
		return nil, nil, err
	}
	page, err = out.Bytes(opts...)
	if err != nil {
		return nil, nil, err
	}
	linked := make(map[string]bool, len(out.assets))
	for _, a := range out.assets {
		linked[a.URL] = true
	}
	return page, &Fills{e: e, r: r, ro: ro, owners: ro.queryOwners, islands: deferred, ids: ids, linked: linked, workers: e.opts.StreamWorkers}, firstErr
}

// streamDone is one rendered fill.
type streamDone struct {
	i     int
	attrs string
	html  []byte
	err   error
}

// Each renders the fills concurrently, at most Options.StreamWorkers at a
// time, and calls fn with each one's markup in completion order, from the
// calling goroutine, so fn may write and flush a ResponseWriter. An island
// that fails never fails the response, whose status is already sent: it
// writes its boundary's fallback (Prod) or the error block and fallback
// (Dev), and the error goes to the error hooks. Each returns the context's
// error when the client goes away, or fn's first error; either stops the
// renders still running. It may be called once.
func (f *Fills) Each(ctx context.Context, fn func(fill []byte) error) error {
	if f == nil || len(f.islands) == 0 {
		return nil
	}
	if f.consumed {
		return errors.New("brick: Fills.Each called twice")
	}
	f.consumed = true
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan streamDone, len(f.islands))
	sem := make(chan struct{}, max(f.workers, 1))
	go func() {
		for i := range f.islands {
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			go func() {
				defer func() { <-sem }()
				attrs, html, err := f.render(ctx, i)
				done <- streamDone{i: i, attrs: attrs, html: html, err: err}
			}()
		}
	}()
	for n := 0; n < len(f.islands); n++ {
		var d streamDone
		select {
		case d = <-done:
		case <-ctx.Done():
			return ctx.Err()
		}
		if d.err != nil {
			return d.err
		}
		if err := fn(f.markup(d.i, d.attrs, d.html, n == 0)); err != nil {
			return err
		}
	}
	return nil
}

// render renders one stream island with the request's options. Values
// that are not safe to share across concurrent renders are per island: a
// copy of the query owners (a parameter claimed by a page island and a
// stream island is still caught; two stream islands claiming one is not),
// no stats (the toolbar went out with the page) and its own header sink
// (the headers went out with the page too, so a loader's brick.header is
// dropped, with a Dev warning). attrs are extra attributes for the fill
// element.
func (f *Fills) render(ctx context.Context, i int) (attrs string, html []byte, err error) {
	isl := f.islands[i]
	ro := f.ro
	ro.stats = nil
	ro.queryOwners = maps.Clone(f.owners)
	headers := http.Header{}
	chunks, used, _, err := f.e.renderIsland(WithResponseHeaders(ctx, headers), f.r, isl, ro)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return "", nil, err
		}
		if !IsAnswer(err) {
			// A loader's answer arrives after the page was sent, so it
			// cannot become the response: the island shows its fallback,
			// and an answer is never reported as a fault (as a lazy
			// island's, redeemIsland).
			f.e.reportError(ctx, err)
			f.mu.Lock()
			f.failed = append(f.failed, err)
			f.mu.Unlock()
		}
		chunks, used = nil, nil
		if f.e.opts.Mode == Dev {
			chunks = append(chunks, Chunk{Kind: ChunkBytes, Bytes: []byte(errorBlock(nil, err))})
		}
		chunks = append(chunks, isl.Fallback...)
	}
	if len(headers) > 0 && f.e.opts.Mode == Dev {
		f.e.warn(Diagnostic{File: isl.Path, Code: CodeRuntime, Message: fmt.Sprintf("<%s brick:lazy=%q> set a response header after the page was sent; it was dropped (set it in the page's loader)", isl.Name, LazyStream)})
	}
	var bopts []RenderOption
	if f.ro.nonce != "" {
		bopts = append(bopts, WithNonce(f.ro.nonce))
	}
	body, err := (&Result{Chunks: chunks}).Bytes(bopts...)
	if err != nil {
		return "", nil, err
	}
	var assets []islandAsset
	for _, a := range f.e.mergeAssets(nil, used) {
		if !f.linked[a.URL] {
			assets = append(assets, islandAsset{URL: a.URL, Integrity: a.Integrity, CSS: a.CSS})
		}
	}
	if len(assets) == 0 {
		return "", body, nil
	}
	// Components chosen from data (brick:render) that the page could not
	// link: the runtime links them before it shows the fill.
	b, err := json.Marshal(assets)
	if err != nil {
		return "", nil, err
	}
	return ` data-brick-assets="` + template.HTMLEscapeString(string(b)) + `"`, body, nil
}

// markup wraps a rendered island as its fill.
func (f *Fills) markup(i int, attrs string, html []byte, first bool) []byte {
	id := f.ids[i]
	nonce := ""
	if f.ro.nonce != "" {
		nonce = ` nonce="` + f.ro.nonce + `"`
	}
	var b []byte
	b = append(b, `<noscript><style`+nonce+`>`+lazyTag+`[data-brick-stream="`+id+`"]{display:none}`+streamTag+`[data-brick-fill="`+id+`"]{display:contents}</style></noscript>`...)
	b = append(b, `<`+streamTag+` data-brick-fill="`+id+`" hidden`+attrs+`>`...)
	b = append(b, html...)
	b = append(b, `</`+streamTag+`><script`+nonce+`>`...)
	if first {
		b = append(b, streamApplier...)
	}
	b = append(b, `__brickFill("`+id+`")</script>`...)
	return b
}

// Stream writes a page with streamed islands: the page with
// each stream island's placeholder, a flush, then each island as it is
// ready, flushing after each. flush may be nil (an io.Writer that does not
// buffer). It is the plain transport; the site layer uses FillStream
// directly so it can finish the page's HTML before it is written.
func (e *Engine) Stream(ctx context.Context, w io.Writer, flush func(), r *Result, opts ...RenderOption) error {
	page, fills, err := e.FillStream(ctx, r, opts...)
	if page == nil {
		return err
	}
	if _, werr := w.Write(page); werr != nil {
		return werr
	}
	if flush != nil {
		flush()
	}
	serr := fills.Each(ctx, func(b []byte) error {
		if _, err := w.Write(b); err != nil {
			return err
		}
		if flush != nil {
			flush()
		}
		return nil
	})
	return errors.Join(err, serr)
}
