package brick

import (
	"bytes"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"regexp"
	"strings"
)

// ChunkKind says what a chunk holds.
type ChunkKind int

// Chunk kinds. Markers live outside the byte stream (T9), so no data can
// forge one: a marker is a separate chunk, never a sentinel string.
const (
	// ChunkBytes is final HTML.
	ChunkBytes ChunkKind = iota
	// ChunkAssets is where `{{ assets }}` was called.
	ChunkAssets
	// ChunkNonce is where `{{ nonce }}` was called.
	ChunkNonce
	// ChunkDeferred stays reserved. A streamed island is a
	// ChunkIsland whose Island.Lazy is LazyStream, so every host that
	// walks chunks for islands already sees it.
	ChunkDeferred
	// ChunkIsland is an island's place, filled per request by Engine.Fill.
	ChunkIsland
)

// Chunk is one piece of rendered output.
type Chunk struct {
	Kind  ChunkKind
	Bytes []byte
	// Island describes a ChunkIsland; nil for every other kind.
	Island *Island
}

// Result is a render before per-request values are filled in (A7). Only a
// Result is cacheable: it holds nonce and asset markers, which Bytes fills
// fresh each time. Fields may be added, never removed.
type Result struct {
	Chunks []Chunk
	// Types are the component names rendered, in first-render order (E6).
	Types []string
	// Tags are the cache tags recorded, deduplicated and sorted (E6).
	Tags []string
	// Errors are the failures a boundary contained (they did not fail the
	// render).
	Errors []error
	// Warnings are the render's non-fatal findings.
	Warnings []Diagnostic
	// Files are every file the render read, sorted: the page, layouts,
	// components, data, scripts and their loads, src= files. A change to any
	// other file cannot change this page; a cache invalidates by it.
	Files []string
	// Stats counts what the render did (components, islands, loader calls),
	// for the Dev toolbar. It never changes the page's bytes.
	Stats RenderStats
	// Headers are the response headers the page's and layouts' loaders set
	// with brick.header (LoaderHeaders only), nil when none. They describe
	// the page, not a visitor: the site layer caches them with the Result
	// and sends them on every response. An island's headers are per
	// response and go through WithResponseHeaders on Engine.Fill instead.
	Headers http.Header

	assets []asset
	// page and locale are what Engine.Fill renders islands with.
	page   *Page
	locale string
}

// hasIslands reports whether Fill is needed.
func (r *Result) hasIslands() bool {
	for _, c := range r.Chunks {
		if c.Kind == ChunkIsland {
			return true
		}
	}
	return false
}

// asset is one component module or stylesheet the page needs.
type asset struct {
	URL       string
	Integrity string
	CSS       bool
	// Config is HTML written just before this asset's tag: the runtime's
	// brick-config meta, "" for every other asset.
	Config string
}

// Bytes fills the markers and returns the page. Pass WithNonce for a
// per-request nonce; without one, no nonce attribute is written.
func (r *Result) Bytes(opts ...RenderOption) ([]byte, error) {
	var ro renderOptions
	for _, o := range opts {
		o(&ro)
	}
	if ro.err != nil {
		return nil, ro.err
	}
	if r.hasIslands() {
		return nil, ErrIslands
	}
	var b bytes.Buffer
	for _, c := range r.Chunks {
		switch c.Kind {
		case ChunkBytes:
			b.Write(c.Bytes)
		case ChunkNonce:
			b.WriteString(ro.nonce)
		case ChunkAssets:
			b.WriteString(assetsHTML(r.assets, ro.nonce))
		}
	}
	return b.Bytes(), nil
}

// assetsHTML emits the page's asset tags inside a stable wrapper element, so
// a later navigation runtime can find and diff it.
func assetsHTML(list []asset, nonce string) string {
	var b strings.Builder
	b.WriteString("<brick-assets>")
	nonceAttr := ""
	if nonce != "" {
		nonceAttr = ` nonce="` + nonce + `"`
	}
	for _, a := range list {
		b.WriteString(a.Config)
		url := template.HTMLEscapeString(a.URL)
		if a.CSS {
			fmt.Fprintf(&b, `<link rel="stylesheet" href="%s" integrity="%s"%s>`, url, a.Integrity, nonceAttr)
		} else {
			fmt.Fprintf(&b, `<script type="module" src="%s" integrity="%s"%s></script>`, url, a.Integrity, nonceAttr)
		}
	}
	b.WriteString("</brick-assets>")
	return b.String()
}

// nonceRE is the allowed nonce alphabet: base64 and base64url (X5). A nonce is
// substituted after escaping, so an unchecked one would be raw attribute
// injection.
var nonceRE = regexp.MustCompile(`^[A-Za-z0-9+/_=-]+$`)

// errBadNonce is returned for a nonce outside the base64 alphabet.
var errBadNonce = errors.New("brick: WithNonce: a nonce must be base64 or base64url (X5)")

// builder collects output as chunks. text/template writes to it in order
// with no buffering (T9), so a marker splits the stream exactly where the
// marker function was called.
type builder struct {
	chunks []Chunk
	cur    bytes.Buffer
	size   int
	limit  int
}

// errOutputLimit is returned once output passes MaxOutputBytes (E4).
var errOutputLimit = errors.New("output passed MaxOutputBytes")

func (b *builder) Write(p []byte) (int, error) {
	b.size += len(p)
	if b.limit > 0 && b.size > b.limit {
		return 0, errOutputLimit
	}
	return b.cur.Write(p)
}

// mark appends a marker chunk.
func (b *builder) mark(k ChunkKind) {
	b.flush()
	b.chunks = append(b.chunks, Chunk{Kind: k})
}

func (b *builder) flush() {
	if b.cur.Len() > 0 {
		b.chunks = append(b.chunks, Chunk{Kind: ChunkBytes, Bytes: bytes.Clone(b.cur.Bytes())})
		b.cur.Reset()
	}
}

// island appends an island chunk.
func (b *builder) island(isl *Island) {
	b.flush()
	b.chunks = append(b.chunks, Chunk{Kind: ChunkIsland, Island: isl})
}

// checkpoint is a position a boundary can roll back to.
type checkpoint struct {
	chunks int
	cur    int
	size   int
}

func (b *builder) checkpoint() checkpoint {
	return checkpoint{chunks: len(b.chunks), cur: b.cur.Len(), size: b.size}
}

// rollback discards everything written after cp. A checkpoint is taken
// before the chunk it sits in is flushed, so the current buffer is restored
// from the chunk that was cut from it.
func (b *builder) rollback(cp checkpoint) {
	if len(b.chunks) > cp.chunks {
		// The buffer at cp was flushed into chunk cp.chunks (if it held bytes).
		var head []byte
		if first := b.chunks[cp.chunks]; first.Kind == ChunkBytes && cp.cur > 0 {
			head = bytes.Clone(first.Bytes[:cp.cur])
		}
		b.chunks = b.chunks[:cp.chunks]
		b.cur.Reset()
		b.cur.Write(head)
	} else {
		b.cur.Truncate(cp.cur)
	}
	b.size = cp.size
}

// isSpace is the whitespace trimSince removes.
func isSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' }

// trimSince removes the whitespace at both edges of what was written after
// cp — a component's output (T11). The newline after a component's
// frontmatter or style block, and the one ending its file, are layout of
// the source, not content: kept, they put stray spaces around an inline
// component such as <Kbd> or <Badge> in a sentence. Only bytes written
// after cp are touched, and a marker chunk ends the trim.
func (b *builder) trimSince(cp checkpoint) {
	// Trailing: the open buffer, never before the checkpoint's offset when
	// nothing was flushed since.
	low := 0
	if len(b.chunks) == cp.chunks {
		low = cp.cur
	}
	buf := b.cur.Bytes()
	end := len(buf)
	for end > low && isSpace(buf[end-1]) {
		end--
	}
	b.size -= len(buf) - end
	b.cur.Truncate(end)
	// Leading: the first bytes written after the checkpoint, in the open
	// buffer or in the chunk it was flushed into.
	if len(b.chunks) == cp.chunks {
		buf = b.cur.Bytes()
		i := cp.cur
		for i < len(buf) && isSpace(buf[i]) {
			i++
		}
		if i > cp.cur {
			rest := bytes.Clone(buf[i:])
			b.cur.Truncate(cp.cur)
			b.cur.Write(rest)
			b.size -= i - cp.cur
		}
		return
	}
	first := &b.chunks[cp.chunks]
	if first.Kind != ChunkBytes {
		return
	}
	i := cp.cur
	for i < len(first.Bytes) && isSpace(first.Bytes[i]) {
		i++
	}
	if i > cp.cur {
		first.Bytes = append(first.Bytes[:cp.cur:cp.cur], first.Bytes[i:]...)
		b.size -= i - cp.cur
	}
}

// result finishes the builder.
func (b *builder) result() []Chunk {
	b.flush()
	return b.chunks
}

// withoutIslands drops island chunks, for showing a failed Dev render.
func (r *Result) withoutIslands() *Result {
	out := *r
	out.Chunks = nil
	for _, c := range r.Chunks {
		if c.Kind != ChunkIsland {
			out.Chunks = append(out.Chunks, c)
		}
	}
	return &out
}
