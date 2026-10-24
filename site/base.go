package site

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/ubgo/brick"
)

// A site mounted under a base path (Options.BasePath) is written as if it
// were at the root: its pages, links and asset URLs stay "/blog/",
// "/_brick/c/…". The site layer adds the base on the way out, rewriting
// every root-absolute URL in the HTML it serves or exports, and strips it on
// the way in. So a theme needs no edits to be hosted under /docs/ or next to
// other sites on one server, and a site renders the same bytes at the root
// as it always did — the base changes only the served copy.

// urlAttrRE finds an attribute holding one URL, with its quoted value. Only
// root-absolute values (starting with /) are candidates; the value group
// is checked for // (protocol-relative) by rebaseURL.
var urlAttrRE = regexp.MustCompile(`(\s(?:href|src|action|formaction|poster)\s*=\s*)(?:"(/[^"]*)"|'(/[^']*)')`)

// headOpenRE finds the <head> start tag, with or without attributes
// (<head lang="en">) and in any case, so the base meta lands inside it.
var headOpenRE = regexp.MustCompile(`(?i)<head(?:\s[^>]*)?>`)

// srcsetRE finds a srcset attribute, whose value lists URLs and widths.
var srcsetRE = regexp.MustCompile(`(\ssrcset\s*=\s*)(?:"([^"]*)"|'([^']*)')`)

// normalizeBase checks and cleans Options.BasePath: "" or "/" is the root;
// anything else is an absolute path with no trailing slash, e.g. "/docs".
func normalizeBase(b string) (string, error) {
	if b == "" || b == "/" {
		return "", nil
	}
	if !strings.HasPrefix(b, "/") || strings.HasPrefix(b, "//") {
		return "", fmt.Errorf("site: BasePath %q must start with a single /", b)
	}
	clean := path.Clean(b)
	if clean != strings.TrimSuffix(b, "/") || strings.ContainsAny(clean, "?#\"'<> ") {
		return "", fmt.Errorf("site: BasePath %q must be a plain path such as /docs", b)
	}
	return clean, nil
}

// rebaseURL puts base in front of one root-absolute URL, unless it is
// protocol-relative (//host/…) or listed in keep (Options.KeepPaths).
func rebaseURL(u, base string, keep []string) string {
	if strings.HasPrefix(u, "//") || !strings.HasPrefix(u, "/") {
		return u
	}
	for _, k := range keep {
		k = strings.TrimSuffix(k, "/")
		if u == k || strings.HasPrefix(u, k+"/") {
			return u
		}
	}
	return base + u
}

// rebase rewrites the root-absolute URLs in an HTML document for a site
// mounted at base: href, src, action, formaction, poster and every URL of a
// srcset. Text, inline scripts and styles are not touched — values there
// are not attribute syntax, and html/template escapes quotes in text, so a
// code sample that shows href="/x" is never rewritten.
func rebase(b []byte, base string, keep []string) []byte {
	if base == "" {
		return b
	}
	b = urlAttrRE.ReplaceAllFunc(b, func(m []byte) []byte {
		sub := urlAttrRE.FindSubmatch(m)
		q, v := byte('"'), sub[2]
		if sub[3] != nil {
			q, v = '\'', sub[3]
		}
		out := append([]byte(nil), sub[1]...)
		out = append(out, q)
		out = append(out, rebaseURL(string(v), base, keep)...)
		return append(out, q)
	})
	return srcsetRE.ReplaceAllFunc(b, func(m []byte) []byte {
		sub := srcsetRE.FindSubmatch(m)
		q, v := byte('"'), sub[2]
		if sub[3] != nil {
			q, v = '\'', sub[3]
		}
		parts := strings.Split(string(v), ",")
		for i, p := range parts {
			f := strings.Fields(p)
			if len(f) > 0 {
				f[0] = rebaseURL(f[0], base, keep)
				parts[i] = " " + strings.Join(f, " ")
			}
		}
		out := append([]byte(nil), sub[1]...)
		out = append(out, q)
		out = append(out, strings.TrimPrefix(strings.Join(parts, ","), " ")...)
		return append(out, q)
	})
}

// baseMeta tells the browser runtimes the base: navigation keeps in-place
// navigation inside it, so a link to a sibling site is a normal page load.
// base is written unescaped: normalizeBase guarantees it holds no quote or
// angle bracket, so it cannot leave the attribute (or, in liveScript, the
// script).
func baseMeta(base string) []byte {
	return []byte(`<meta name="brick-base" content="` + base + `">`)
}

// injectBase writes baseMeta into <head>, or at the start when the page
// has none.
func injectBase(b []byte, base string) []byte {
	if base == "" {
		return b
	}
	loc := headOpenRE.FindIndex(b)
	if loc == nil {
		return append(baseMeta(base), b...)
	}
	i := loc[1]
	out := append([]byte(nil), b[:i]...)
	out = append(out, baseMeta(base)...)
	return append(out, b[i:]...)
}

// finishHTML is the last step of every HTML response: the base meta, the
// live-reload script in Dev, then the URL rewrite.
func (s *Site) finishHTML(b []byte) []byte {
	b = injectBase(b, s.opts.BasePath)
	if s.opts.Dev {
		b = injectLive(b, s.opts.BasePath)
	}
	return rebase(b, s.opts.BasePath, s.opts.KeepPaths)
}

// rebaseWriter buffers an HTML response (an action's re-rendered
// component) so its URLs can be rewritten before it is sent. A streamed
// action answer (brick.StreamContentType) is passed on line by line
// instead, so chunks still arrive as they are written; only its last
// line, the envelope with the re-rendered HTML, is rewritten.
type rebaseWriter struct {
	http.ResponseWriter
	buf    bytes.Buffer
	status int
	// site is set for a streaming answer, which rebases as it goes.
	site      *Site
	streaming bool
}

func (w *rebaseWriter) WriteHeader(code int) {
	w.status = code
	if strings.HasPrefix(w.Header().Get("Content-Type"), brick.StreamContentType) {
		w.streaming = true
		w.ResponseWriter.WriteHeader(code)
	}
}

func (w *rebaseWriter) Write(p []byte) (int, error) {
	if !w.streaming {
		return w.buf.Write(p)
	}
	w.buf.Write(p)
	for {
		line, rest, ok := bytes.Cut(w.buf.Bytes(), []byte("\n"))
		if !ok {
			break
		}
		out := line
		// Chunk lines hold text only; the envelope line holds HTML.
		if !bytes.HasPrefix(line, []byte(`{"stream":`)) {
			out = rebaseEnvelope(line, w.site.opts.BasePath, w.site.opts.KeepPaths)
		}
		if _, err := w.ResponseWriter.Write(append(bytes.Clone(out), '\n')); err != nil {
			return 0, err
		}
		next := bytes.Clone(rest)
		w.buf.Reset()
		w.buf.Write(next)
	}
	return len(p), nil
}

// Flush passes a streaming answer's lines on as they are written.
func (w *rebaseWriter) Flush() {
	if w.streaming {
		_ = http.NewResponseController(w.ResponseWriter).Flush()
	}
}

// flush sends the buffered response, rewritten when it is HTML.
func (w *rebaseWriter) flush(s *Site) {
	if w.streaming {
		return
	}
	body := w.buf.Bytes()
	switch ct := w.Header().Get("Content-Type"); {
	case strings.HasPrefix(ct, htmlMediaType):
		body = rebase(body, s.opts.BasePath, s.opts.KeepPaths)
		w.Header().Del("Content-Length")
	case strings.HasPrefix(ct, brick.EffectsContentType):
		// The action envelope carries the re-rendered HTML as a string;
		// it is rebased like a page, or a link in an action's answer
		// would leave the mount.
		body = rebaseEnvelope(body, s.opts.BasePath, s.opts.KeepPaths)
		w.Header().Del("Content-Length")
	case strings.HasPrefix(ct, brick.IslandContentType):
		// A lazy island's HTML and its asset URLs leave the mount the
		// same way an action's answer would.
		body = rebaseIsland(body, s.opts.BasePath, s.opts.KeepPaths)
		w.Header().Del("Content-Length")
	}
	if w.status == 0 {
		w.status = http.StatusOK
	}
	w.ResponseWriter.WriteHeader(w.status)
	_, _ = w.ResponseWriter.Write(body)
}

// Mount serves several sites from one handler, each under its own
// Options.BasePath (at most one at the root). A request goes to the site
// with the longest matching base; one outside every base is a 404, or the
// root site's. Each site keeps its own directory, engine, cache and live
// reload; only the URL space is shared.
func Mount(sites ...*Site) (http.Handler, error) {
	if len(sites) == 0 {
		return nil, errors.New("site: Mount needs at least one site")
	}
	type mounted struct {
		base string
		h    http.Handler
	}
	var all []mounted
	seen := map[string]bool{}
	for _, s := range sites {
		b := s.opts.BasePath
		if seen[b] {
			return nil, fmt.Errorf("site: two sites are mounted at %q", cmp.Or(b, "/"))
		}
		seen[b] = true
		all = append(all, mounted{b, s.Handler()})
	}
	// Longest base first, so /docs/api wins over /docs.
	slices.SortFunc(all, func(a, b mounted) int { return len(b.base) - len(a.base) })
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		for _, m := range all {
			if m.base == "" || p == m.base || strings.HasPrefix(p, m.base+"/") {
				m.h.ServeHTTP(w, r)
				return
			}
		}
		http.NotFound(w, r)
	}), nil
}

// ListenAndServeAll serves several sites on one address (Mount) and, for
// each site in Dev mode, watches its project for changes, until ctx ends.
func ListenAndServeAll(ctx context.Context, addr string, sites ...*Site) error {
	h, err := Mount(sites...)
	if err != nil {
		return err
	}
	return ListenAndServeHandler(ctx, addr, h, sites...)
}

// ListenAndServeHandler serves h on addr and watches each Dev site in
// sites, until ctx ends: for a host that wraps Mount in its own handler (an
// index page at /, a health check, auth).
func ListenAndServeHandler(ctx context.Context, addr string, h http.Handler, sites ...*Site) error {
	srv := &http.Server{Addr: addr, Handler: h, ReadHeaderTimeout: readHeaderTimeout}
	errc := make(chan error, len(sites)+1)
	go func() { errc <- srv.ListenAndServe() }()
	for _, s := range sites {
		go s.pruneLoop(ctx)
		if s.opts.Dev {
			go func() {
				if err := s.Watch(ctx, watchDefaults()); err != nil && !errors.Is(err, context.Canceled) {
					errc <- err
				}
			}()
		}
	}
	select {
	case <-ctx.Done():
		// ctx is already done, so shutdown gets its own deadline; it keeps
		// ctx's values (WithoutCancel), not its cancellation.
		shut, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
		defer cancel()
		return srv.Shutdown(shut)
	case err := <-errc:
		return err
	}
}

// envelopeHTMLKey is the action envelope's HTML field (brick's
// actionEnvelope).
const envelopeHTMLKey = "html"

// rebaseEnvelope rebases the HTML inside an action envelope, keeping every
// other field as it was sent. A body that is not a JSON object with a
// string html is returned unchanged.
func rebaseEnvelope(b []byte, base string, keep []string) []byte {
	var env map[string]json.RawMessage
	if json.Unmarshal(b, &env) != nil {
		return b
	}
	var html string
	if raw, ok := env[envelopeHTMLKey]; !ok || json.Unmarshal(raw, &html) != nil {
		return b
	}
	raw, err := json.Marshal(string(rebase([]byte(html), base, keep)))
	if err != nil {
		return b
	}
	env[envelopeHTMLKey] = raw
	out, err := json.Marshal(env)
	if err != nil {
		return b
	}
	return out
}

// rebaseIsland rewrites a lazy island answer (brick.IslandContentType):
// the HTML like a page, and each asset URL, so the runtime appends files
// that exist under the mount.
func rebaseIsland(b []byte, base string, keep []string) []byte {
	var ans struct {
		V      int    `json:"v"`
		HTML   string `json:"html"`
		Assets []struct {
			URL       string `json:"url"`
			Integrity string `json:"integrity"`
			CSS       bool   `json:"css,omitempty"`
		} `json:"assets"`
	}
	if json.Unmarshal(b, &ans) != nil {
		return b
	}
	ans.HTML = string(rebase([]byte(ans.HTML), base, keep))
	for i := range ans.Assets {
		ans.Assets[i].URL = rebaseURL(ans.Assets[i].URL, base, keep)
	}
	out, err := json.Marshal(ans)
	if err != nil {
		return b
	}
	return out
}
