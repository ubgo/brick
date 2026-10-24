package brick

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/ubgo/brick/internal/transpile"
)

// Lazy islands. An island marked brick:lazy is not rendered
// with its page: the page carries a placeholder holding a ticket, and the
// runtime redeems the ticket at GET {AssetBase}island once the island is
// due (on load, when visible, when idle). The browser redeems something the
// server issued; it never names a component or its props, so it cannot ask
// for a render the page did not contain.
const (
	// lazyAttr is the call-site attribute; a bare brick:lazy means LazyLoad.
	lazyAttr = "brick:lazy"
	// LazyLoad, LazyVisible and LazyIdle are when the runtime fetches a
	// lazy island: right after the page loads, when it scrolls into view,
	// or when the browser is idle.
	LazyLoad    = "load"
	LazyVisible = "visible"
	LazyIdle    = "idle"
	// LazyStream renders the island in the same response, out of order
	//: the page goes out with the loading block and the
	// island follows when it is ready (Engine.FillStream). Where the
	// response cannot stream, it renders in place.
	LazyStream = "stream"
	// lazyTag is the engine-owned placeholder element. The runtime styles
	// it display: contents, so it never changes the layout around it.
	lazyTag = "brick-lazy"
	// IslandEndpoint is the lazy island route relative to the asset base,
	// beside ActionEndpoint; runtime.js derives the same URL from its own
	// script URL.
	IslandEndpoint = "island"
	// IslandContentType is the endpoint's JSON answer {v, html, assets}.
	IslandContentType = "application/vnd.brick.island+json"
	// IslandErrorHeader marks an answer whose island failed: its html is
	// the boundary's fallback (Prod) or the error block (Dev), sent with
	// status 200 so the runtime shows it like any answer.
	IslandErrorHeader = "Brick-Island-Error"
	// islandTicketParam is the query parameter holding the ticket.
	islandTicketParam = "t"
	// islandQueryHeader carries the page's own query string (without "?"),
	// so a lazy island with brick: {query:} reads the URL the visitor sees,
	// not the endpoint's.
	islandQueryHeader = "Brick-Query"
	// islandEnvelopeVersion is the answer's v.
	islandEnvelopeVersion = 1
	// islandTicketStore is the store name sealed into a ticket's additional
	// data. It holds a colon, which validStoreName refuses, so no state
	// store can ever share it: a ticket never opens as an action handle,
	// and a handle never redeems as a ticket.
	islandTicketStore = "brick:island"
	// DefaultIslandTicketTTL bounds how long a page's tickets stay
	// redeemable; an older one answers 410 and the runtime reloads once.
	DefaultIslandTicketTTL = time.Hour
	// DefaultIslandTicketMax bounds the in-memory ticket store.
	DefaultIslandTicketMax = 10000
	// lazyTicketWarnBytes is the ticket size above which Dev warns: sealed
	// props travel in the page.
	lazyTicketWarnBytes = 4 << 10
)

// lazyModes are the values brick:lazy and brick: {lazy:} accept.
var lazyModes = []string{LazyLoad, LazyVisible, LazyIdle, LazyStream}

// Keys of a ticket's State map: the island's pieces that Instance has no
// field for. ticketMark is always true, so a state handle that some shared
// store returns is never taken for a ticket.
const (
	ticketMark        = "brick:ticket"
	ticketKey         = "key"
	ticketFallback    = "fallback"
	ticketHasFallback = "hasFallback"
	ticketLazy        = "lazy"
)

// IslandTickets turns a deferred island into an opaque string the page
// carries, and back. Issue runs once per response that shows
// the island; Redeem once per fetch. A ticket must not let anyone choose
// what renders: the default seals it (or keeps it server-side), so a
// changed byte never redeems.
//
// The engine sets StateScopeFrom(ctx).Session to the hashed session on
// both calls, and compares Island.Session itself after Redeem, so an
// implementation that forgets session binding still never renders another
// session's island. Redeem answers ErrGone for a ticket it does not know.
type IslandTickets interface {
	Issue(ctx context.Context, isl *Island) (string, error)
	Redeem(ctx context.Context, ticket string) (*Island, error)
}

// stateTickets keeps tickets in a StateStore of their own, encoded as an
// Instance: SealedStates seals them into the ticket, MemoryStates keeps
// them in this process. The store's TTL makes tickets expire.
type stateTickets struct {
	store StateStore
	clock Clock
	rand  func() (string, error)
}

// NewIslandTickets is the default IslandTickets over a store used for
// nothing else: NewSealedStates (a ticket that survives restarts and
// servers) or NewMemoryStates (this process only). Giving it the store that
// holds component state would let a ticket's handle reach the action
// endpoint, so the engine never does.
func NewIslandTickets(store StateStore) IslandTickets {
	return &stateTickets{store: store, clock: time.Now, rand: func() (string, error) { return randomHex(nil) }}
}

func (t *stateTickets) Issue(ctx context.Context, isl *Island) (string, error) {
	// A random token the client never sees: a ticket stored where an
	// action could look it up can never pass the token check.
	token, err := t.rand()
	if err != nil {
		return "", err
	}
	var fallback bytes.Buffer
	for _, c := range isl.Fallback {
		if c.Kind == ChunkBytes {
			fallback.Write(c.Bytes)
		}
	}
	inst := &Instance{
		Token: token, Store: islandTicketStore, Path: isl.Path, Name: isl.Name, ID: isl.ID,
		Props: isl.Props, Ctx: isl.Ctx, Locale: isl.Locale, Session: isl.Session, PagePath: isl.PagePath,
		Route: isl.Route, RouteParts: isl.RouteParts, Slots: isl.Slots, Issued: t.clock().UTC(),
		State: map[string]any{ticketMark: true, ticketKey: isl.Key, ticketFallback: fallback.String(), ticketHasFallback: isl.HasFallback, ticketLazy: isl.Lazy},
	}
	return t.store.Put(WithStateScope(ctx, StateScope{Store: islandTicketStore, Session: isl.Session}), inst)
}

func (t *stateTickets) Redeem(ctx context.Context, ticket string) (*Island, error) {
	inst, ok, err := t.store.Get(WithStateScope(ctx, StateScope{Store: islandTicketStore, Session: StateScopeFrom(ctx).Session}), ticket)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	if !ok || inst.State[ticketMark] != true {
		return nil, ErrGone
	}
	isl := &Island{
		Name: inst.Name, Path: inst.Path, Props: inst.Props, Key: inst.State[ticketKey], ID: inst.ID, Ctx: inst.Ctx,
		Slots: inst.Slots, Session: inst.Session, PagePath: inst.PagePath, Locale: inst.Locale, Route: inst.Route, RouteParts: inst.RouteParts,
	}
	isl.Lazy, _ = inst.State[ticketLazy].(string)
	isl.HasFallback, _ = inst.State[ticketHasFallback].(bool)
	if fb, _ := inst.State[ticketFallback].(string); fb != "" {
		isl.Fallback = []Chunk{{Kind: ChunkBytes, Bytes: []byte(fb)}}
	}
	return isl, nil
}

// defaultTickets picks the engine's ticket store when Options.IslandTickets
// is nil: sealed with the state store's Sealer when the engine seals state
// (a ticket then survives restarts and servers, like a handle), otherwise
// bounded memory, which Dev warns about on first use.
func defaultTickets(opts *Options) (IslandTickets, bool) {
	if ss, ok := opts.States.(*SealedStates); ok {
		st, err := NewSealedStates(SealedOptions{Sealer: ss.opts.Sealer, Codec: cmpCodec(ss.opts.Codec, opts.Codec), TTL: DefaultIslandTicketTTL, Clock: ss.opts.Clock})
		if err == nil {
			return NewIslandTickets(st), false
		}
	}
	return NewIslandTickets(NewMemoryStates(DefaultIslandTicketMax, DefaultIslandTicketTTL)), true
}

// cmpCodec is the first non-nil codec.
func cmpCodec(a, b Codec) Codec {
	if a != nil {
		return a
	}
	return b
}

// lazyOf is a call's lazy mode: the call site's brick:lazy wins over the
// component's brick: {lazy:}; "" is not lazy. A bare or empty brick:lazy
// is LazyLoad.
func lazyOf(call *transpile.Call, m *meta) string {
	for _, p := range call.Props {
		if p.Name != lazyAttr {
			continue
		}
		if p.Kind == transpile.Bare || p.Text == "" {
			return LazyLoad
		}
		return p.Text
	}
	if m != nil {
		return m.lazy
	}
	return ""
}

// checkLazyValue is the compile check of a written lazy mode.
func checkLazyValue(v string) error {
	if !slices.Contains(lazyModes, v) {
		return fmt.Errorf("%s takes %s, written literally", lazyAttr, strings.Join(lazyModes, ", "))
	}
	return nil
}

// lazyChunks issues an island's ticket for this response and returns its
// placeholder: <brick-lazy> holding the enclosing boundary's loading block.
// The ticket is bound to the request's session, which is why it is issued
// per response and never cached with the page.
func (e *Engine) lazyChunks(ctx context.Context, page *Page, locale string, isl *Island, session string) ([]Chunk, error) {
	t := *isl
	// Request-scoped values never travel in a ticket: the redeeming request
	// provides them again, as an action's does.
	t.Ctx = map[string]any{}
	for k, v := range isl.Ctx {
		if !isl.reqKeys[k] {
			t.Ctx[k] = v
		}
	}
	t.reqKeys = nil
	t.Session, t.Locale = hashSession(session), locale
	if page != nil {
		t.PagePath, t.Route, t.RouteParts = page.Path, maps.Clone(page.Route), slices.Clone(page.RouteParts)
	}
	if e.ticketsMemory && e.opts.Mode == Dev {
		e.warn(Diagnostic{File: isl.Path, Code: CodeRuntime, Message: "lazy islands keep their tickets in this process: a restart or a second server answers 410 and the page reloads once; seal state (Options.States = brick.NewSealedStates, or site StateKeys) to seal tickets too"})
	}
	ticket, err := e.tickets.Issue(ctx, &t)
	if err != nil {
		return nil, err
	}
	if len(ticket) > lazyTicketWarnBytes && e.opts.Mode == Dev {
		e.warn(Diagnostic{File: isl.Path, Code: CodeRuntime, Message: fmt.Sprintf("<%s brick:lazy> carries a %d KiB ticket in the page: pass ids as props and load the rows in the island", isl.Name, len(ticket)>>10)})
	}
	open := fmt.Sprintf(`<%s data-brick-lazy="%s" data-when="%s">`, lazyTag, template.HTMLEscapeString(ticket), template.HTMLEscapeString(isl.Lazy))
	out := []Chunk{{Kind: ChunkBytes, Bytes: []byte(open)}}
	out = append(out, isl.Loading...)
	return append(out, Chunk{Kind: ChunkBytes, Bytes: []byte("</" + lazyTag + ">")}), nil
}

// WithLazyInline makes Fill and FillStream render every brick:lazy island
// in place, whatever its mode, the way a static export does but with its
// state stored as usual, so its actions still run. It is
// for tests (bricktest.Live), which then see an island's content without
// redeeming its ticket in a second request; a page served with it loses
// what lazy loading is for.
func WithLazyInline() RenderOption {
	return func(o *renderOptions) { o.lazyInline = true }
}

// ticketed reports whether the island is fetched later with a ticket: every
// lazy mode except LazyStream, which arrives in the page's own response.
func (isl *Island) ticketed() bool { return isl.Lazy != "" && isl.Lazy != LazyStream }

// HasLazy reports whether the result holds a lazy island. Its filled
// response carries a ticket bound to the visitor's session, so, like a
// stateful page, it must not be shared by a cache; a static export renders
// it in place and needs no keys for it.
func (r *Result) HasLazy() bool {
	for _, c := range r.Chunks {
		if c.Kind == ChunkIsland && c.Island != nil && c.Island.ticketed() {
			return true
		}
	}
	return false
}

// islandAnswer is the endpoint's JSON envelope.
type islandAnswer struct {
	V      int           `json:"v"`
	HTML   string        `json:"html"`
	Assets []islandAsset `json:"assets"`
}

// islandAsset is one stylesheet or module the island's components need.
// The runtime appends the ones the page does not link yet.
type islandAsset struct {
	URL       string `json:"url"`
	Integrity string `json:"integrity"`
	CSS       bool   `json:"css,omitempty"`
}

// IslandHandler serves lazy islands: GET {AssetBase}island?t=TICKET (the
// site layer mounts it beside the action endpoint; a Go host mounts or
// wraps it). It authorises exactly like an action handle: the ticket must
// open (sealed, unexpired), belong to the request's session
// (Options.Sessions), and name a component that still exists; anything
// else is 410, and the runtime reloads the page once. The request's render
// options come from Options.ActionRequest (the signed-in user), as Fill's
// come from the site's Request.
func (e *Engine) IslandHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Cache-Control", "private, no-store")
		start := time.Now()
		// Dev only: the island's counts ride back for the toolbar (inspect.go).
		var dev *islandDev
		if e.opts.Mode == Dev {
			dev = &islandDev{}
		}
		ans, failed, err := e.redeemIsland(r, dev)
		if err != nil {
			switch {
			case errors.Is(err, context.Canceled):
			case errors.Is(err, ErrStoreUnavailable):
				e.reportError(r.Context(), err)
				w.Header().Set("Retry-After", "1")
				http.Error(w, ErrStoreUnavailable.Error(), http.StatusServiceUnavailable)
			case errors.Is(err, ErrGone):
				http.Error(w, ErrGone.Error(), http.StatusGone)
			default:
				e.reportError(r.Context(), err)
				http.Error(w, "the island failed (error "+errorID(err)+")", http.StatusInternalServerError)
			}
			return
		}
		b, err := json.Marshal(ans)
		if err != nil {
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		if failed {
			w.Header().Set(IslandErrorHeader, "1")
		}
		if dev != nil {
			w.Header().Set(ServerTimingHeader, dev.timing(time.Since(start)))
		}
		w.Header().Set("Content-Type", IslandContentType)
		_, _ = w.Write(b)
	})
}

// redeemIsland opens the request's ticket and renders the island. failed
// reports an island that failed and answers its fallback or error block.
// dev, non-nil in Dev mode, collects the island's counts.
func (e *Engine) redeemIsland(r *http.Request, dev *islandDev) (*islandAnswer, bool, error) {
	ctx := r.Context()
	ticket := r.URL.Query().Get(islandTicketParam)
	if ticket == "" {
		return nil, false, ErrGone
	}
	var ro renderOptions
	if e.opts.ActionRequest != nil {
		for _, o := range e.opts.ActionRequest(r) {
			o(&ro)
		}
	}
	if ro.err != nil {
		return nil, false, ro.err
	}
	if e.opts.Sessions != nil {
		s, err := e.opts.Sessions.Session(r)
		if err != nil {
			return nil, false, fmt.Errorf("%w: session: %v", ErrStoreUnavailable, err)
		}
		ro.session = s
	}
	if q, err := url.ParseQuery(r.Header.Get(islandQueryHeader)); err == nil {
		ro.query = q
	}
	hash := hashSession(ro.session)
	isl, err := e.tickets.Redeem(WithStateScope(ctx, StateScope{Store: islandTicketStore, Session: hash}), ticket)
	if err != nil {
		return nil, false, err
	}
	// Checked here, not only in the store, so a host's own IslandTickets
	// that forgets session binding still never renders another session's
	// island.
	if isl == nil || subtle.ConstantTimeCompare([]byte(isl.Session), []byte(hash)) != 1 {
		return nil, false, ErrGone
	}
	callee, err := e.load(ctx, isl.Path, kindComponent, isl.Locale)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			// The deploy removed or renamed the component.
			return nil, false, ErrGone
		}
		return nil, false, err
	}
	isl.Stateful = callee.meta.state != nil
	ro.queryOwners = map[string]string{}
	res := &Result{page: &Page{Path: isl.PagePath, Params: map[string]any{}, Route: isl.Route, RouteParts: isl.RouteParts}, locale: isl.Locale}
	if dev != nil {
		dev.name = isl.Name
		ro.stats = &dev.stats
	}
	chunks, used, _, err := e.renderIslandWith(ctx, res, isl, ro, nil, nil, nil)
	failed := false
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, false, err
		}
		if !IsAnswer(err) {
			// A loader's answer arrives after the page was sent, so it
			// cannot become the response: the island shows its fallback,
			// and an answer is never reported as a fault.
			e.reportError(ctx, err)
		}
		failed, used = true, nil
		chunks = nil
		if e.opts.Mode == Dev {
			chunks = append(chunks, Chunk{Kind: ChunkBytes, Bytes: []byte(errorBlock(nil, err))})
		}
		chunks = append(chunks, isl.Fallback...)
	}
	html, err := (&Result{Chunks: chunks}).Bytes()
	if err != nil {
		return nil, false, err
	}
	ans := &islandAnswer{V: islandEnvelopeVersion, HTML: string(html), Assets: []islandAsset{}}
	for _, a := range e.mergeAssets(nil, used) {
		// The runtime is already on the page, so its config is not sent.
		ans.Assets = append(ans.Assets, islandAsset{URL: a.URL, Integrity: a.Integrity, CSS: a.CSS})
	}
	return ans, failed, nil
}

// nestedLazy writes the placeholder of a lazy component met inside a
// filled island (a lazy answer may hold further lazy islands): its ticket
// is issued now, for the request being served.
func (st *renderState) nestedLazy(file *compiled, call *transpile.Call, callee *compiled, caller bodyScope, given []propValue, key any) error {
	isl, err := st.makeIsland(file, call, callee, caller, given, key)
	if err != nil {
		return err
	}
	if isl.Lazy == LazyStream {
		// An island being filled is one answer already: it cannot stream
		// part of itself, so a stream island inside it loads as soon as
		// the answer lands, which is the nearest thing.
		isl.Lazy = LazyLoad
	}
	chunks, err := st.e.lazyChunks(st.ctx, st.page, st.locale, isl, st.session)
	if err != nil {
		return st.fail(asError(err, CodeRuntime, file.path))
	}
	st.out.flush()
	st.out.chunks = append(st.out.chunks, chunks...)
	return nil
}
