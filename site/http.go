package site

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ubgo/brick"
)

// Dev-server paths under the asset base.
const (
	liveEndpoint = "_brick/live"
	playPrefix   = "_brick/play/"
	// heartbeat keeps idle event streams open through proxies.
	heartbeat = 25 * time.Second
)

// Server timeouts for ListenAndServe and ListenAndServeHandler.
const (
	// readHeaderTimeout bounds how long a client may take to send its
	// request headers, so a slow or idle connection cannot hold a server
	// goroutine forever (slowloris).
	readHeaderTimeout = 10 * time.Second
	// shutdownTimeout is how long in-flight requests get to finish once the
	// context ends; after it the server closes what is left.
	shutdownTimeout = 5 * time.Second
)

// bodyClose is where the live-reload script goes, matched
// case-insensitively against a lowercased copy of the page. (The <head>
// start tag is found by headOpenRE in base.go, since it may carry
// attributes.)
const (
	bodyClose = "</body>"
	// htmlContentType is the Content-Type of every page the site serves;
	// htmlMediaType is its media type alone, what a response is matched on.
	htmlContentType = "text/html; charset=utf-8"
	htmlMediaType   = "text/html"
	// secFetchMode and secFetchNavigate are the request header a browser
	// sends when it loads a document; only such a request gets a streamed
	// page (streams).
	secFetchMode     = "Sec-Fetch-Mode"
	secFetchNavigate = "navigate"
	// accelBuffering is nginx's per-response switch for proxy buffering,
	// which would hold a streamed page's islands back until the end.
	accelBuffering = "X-Accel-Buffering"
)

// liveScript refreshes the page when the server says its URL (or "*") was
// invalidated. Injected before </body> in Dev mode only.
//
// With navigation on, the refresh is brick.refresh (nav.js): the page is
// fetched and morphed in, so scroll position and typed input survive a
// template edit; nav.js itself falls back to a full load when the page's
// stylesheets or scripts changed. Without nav.js it is location.reload.
//
// The stream is closed on pagehide and reopened when a page returns from
// the back/forward cache: a page kept in that cache would otherwise hold its
// connection open, and a handful of them use up the browser's six
// connections per host, stalling every later navigation to the dev server.
//
// A site under a base path listens at <base>/_brick/live, and compares the
// URLs the server names (which have no base) with the path after the base.
func liveScript(base string) string {
	return `<script>(()=>{let es;const b=` + strconv.Quote(base) + `,open=()=>{es=new EventSource(b+"/` + liveEndpoint + `");es.onmessage=e=>{const u=JSON.parse(e.data);if(u.includes("*")||u.includes(location.pathname.slice(b.length)))window.brick&&brick.refresh?brick.refresh():location.reload()}};open();addEventListener("pagehide",()=>es.close());addEventListener("pageshow",e=>{if(e.persisted)open()})})()</script>`
}

// Handler serves the site: pages by URL, component assets, static files,
// and the 404 page. In Dev mode it adds live reload and the playground.
//
// A site with a BasePath answers only under it: / and /docs are redirected
// to /docs/, and the base is stripped before anything else looks at the
// path. (Under Mount, / goes to the root site, if there is one.)
func (s *Site) Handler() http.Handler {
	assetBase := s.current().eng.AssetBase()
	base := s.opts.BasePath
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if base != "" {
			switch {
			case p == "/" || p == base:
				http.Redirect(w, r, base+"/", http.StatusMovedPermanently)
				return
			case !strings.HasPrefix(p, base+"/"):
				http.NotFound(w, r)
				return
			}
			p = strings.TrimPrefix(p, base)
		}
		// Actions and uploads are the only routes that take a body
		//, and a page takes a brick:submit
		// form posted without JavaScript.
		formPost := false
		if r.Method != http.MethodGet && r.Method != http.MethodHead && p != assetBase+brick.ActionEndpoint && p != assetBase+brick.UploadEndpoint {
			if formPost = s.current().eng.IsFormPost(r); !formPost || strings.HasPrefix(p, assetBase) || (s.opts.Dev && (p == "/"+liveEndpoint || p == "/"+instancesEndpoint || strings.HasPrefix(p, "/"+playPrefix))) {
				w.Header().Set("Allow", "GET, HEAD")
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
		}
		switch {
		case s.opts.Dev && p == "/"+liveEndpoint:
			s.live.serve(w, r)
			return
		case s.opts.Dev && p == "/"+instancesEndpoint:
			s.inspect(w, r)
			return
		case s.opts.Dev && strings.HasPrefix(p, "/"+playPrefix):
			s.play(w, r, strings.TrimPrefix(p, "/"+playPrefix))
			return
		case p == assetBase+brick.EventsEndpoint:
			// Server push: a stream of topic names, with no URLs
			// to rebase under a base path.
			s.current().eng.EventsHandler().ServeHTTP(w, r)
			return
		case p == assetBase+brick.ActionEndpoint, p == assetBase+brick.UploadEndpoint, p == assetBase+brick.IslandEndpoint:
			eng := s.current().eng
			h := eng.ActionHandler()
			switch p {
			case assetBase + brick.UploadEndpoint:
				h = eng.UploadHandler()
			case assetBase + brick.IslandEndpoint:
				h = eng.IslandHandler()
			}
			if base == "" {
				h.ServeHTTP(w, r)
				return
			}
			rw := &rebaseWriter{ResponseWriter: w, site: s}
			h.ServeHTTP(rw, r)
			rw.flush(s)
			return
		case strings.HasPrefix(p, assetBase):
			s.serveFile(w, r, s.current().eng.Assets(), strings.TrimPrefix(p, assetBase), true)
			return
		}
		st := s.current()
		if p == "/"+sitemapFile && s.opts.Sitemap != nil && s.ownSitemap(st) == "" {
			// A project's own sitemap file wins; build logged it.
			s.serveSitemap(w, r, st)
			return
		}
		// A slash encoded into one segment never reaches a dynamic page
		//: a route value is one segment of text.
		dynamic := !hasEncodedSeparator(r.URL.EscapedPath())
		isPage := func(u string) bool {
			if _, ok := st.routes[u]; ok {
				return true
			}
			return dynamic && st.has(u)
		}
		if formPost {
			if !isPage(p) {
				w.Header().Set("Allow", "GET, HEAD")
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			s.formPost(w, r, st, p)
			return
		}
		if !isPage(p) {
			if path.Ext(p) == "" && isPage(p+"/") {
				http.Redirect(w, r, base+p+"/", http.StatusMovedPermanently)
				return
			}
			if static, err := fs.Sub(s.opts.FS, s.opts.StaticDir); err == nil && s.serveFile(w, r, static, strings.TrimPrefix(p, "/"), false) {
				return
			}
			s.notFound(w, r)
			return
		}
		start := time.Now()
		res, hit, err := s.renderRequest(r, s.current(), p)
		if isNotFound(err) {
			// A value off the paths: list, or a loader's brick.not_found().
			visitorNoStore(w, err)
			s.notFound(w, r)
			return
		}
		if s.answer(w, r, err, false) {
			// A page or layout loader's redirect or error status: the
			// same for every visitor, and never cached (a failed render).
			return
		}
		s.writePage(w, r, res, err, http.StatusOK, pageTiming{start: start, hit: hit, output: st.routeAt(p).Output, prolog: st.routeAt(p).prolog})
	})
}

// writePage sends a rendered page. In Dev mode a failed render still sends
// the page with its inline error block, with status 500, and every page
// gets the Dev toolbar, measured from t.start to the filled body.
func (s *Site) writePage(w http.ResponseWriter, r *http.Request, res *brick.Result, err error, status int, t pageTiming) {
	if err != nil {
		status = statusOf(err)
		if status != http.StatusNotFound {
			s.renderFailed(r, err)
		}
		if res == nil {
			http.Error(w, http.StatusText(status), status)
			return
		}
	}
	var opts []brick.RenderOption
	if s.opts.Request != nil {
		opts = s.opts.Request(r)
	}
	if sb := s.opts.Engine.Sessions; sb != nil {
		// Bind new instances to the visitor's session, so an action from
		// another session (a sign-out rotated it) is refused.
		// A binder that cannot answer fails the response, as it fails an
		// action: rendering unbound would hand out state any session opens.
		id, err := sb.Session(r)
		if err != nil {
			s.opts.Logger.Error("site: session", "err", err)
			http.Error(w, http.StatusText(http.StatusServiceUnavailable), http.StatusServiceUnavailable)
			return
		}
		opts = append(opts, brick.WithSession(id))
	}
	// A page a form post redirected to shows the instance the post left
	//; a 422 answer passes it directly instead.
	if t.form != nil {
		opts = append(opts, t.form)
	} else if !t.errorPage {
		opts = append(opts, s.current().eng.FormResumeOption(w, r))
	}
	// Query-synced state reads the request's query. Such a
	// component is an island, filled per request, so the cached page never
	// carries one visitor's query to another; Export passes none.
	opts = append(opts, brick.WithQuery(r.URL.Query()))
	// The toolbar adds what Fill renders to a copy of the page's counts.
	var stats brick.RenderStats
	if s.opts.Dev && res != nil {
		stats = res.Stats
		opts = append(opts, brick.WithStats(&stats))
	}
	// An island's loader headers are this response's alone.
	islandHeaders := http.Header{}
	eng := s.current().eng
	var body []byte
	var fills *brick.Fills
	var ferr error
	fl, canFlush := w.(http.Flusher)
	if err == nil && t.output == "" && !t.errorPage && canFlush && res.HasStream() && streams(r) {
		body, fills, ferr = eng.FillStream(brick.WithResponseHeaders(r.Context(), islandHeaders), res, opts...)
	} else {
		body, ferr = eng.Fill(brick.WithResponseHeaders(r.Context(), islandHeaders), res, opts...)
	}
	if brick.IsAnswer(ferr) {
		// An island's loader turned this visitor away: per request, so
		// sent with no-store. An error page whose own island answers gets
		// a plain response instead of rendering itself again.
		if t.errorPage {
			w.Header().Set("Cache-Control", noStore)
			http.Error(w, http.StatusText(answerStatus(ferr)), answerStatus(ferr))
			return
		}
		s.answer(w, r, ferr, true)
		return
	}
	if ferr != nil {
		// A failing island: Dev shows its error block in the page, Prod
		// sends a bare 500.
		status = http.StatusInternalServerError
		s.renderFailed(r, ferr)
		if body == nil {
			http.Error(w, http.StatusText(status), status)
			return
		}
	}
	setHeaders(w, res.Headers)
	setHeaders(w, islandHeaders)
	if t.output != "" {
		// An output page (feed.xml, robots.txt) is sent as rendered: the
		// toolbar, live reload, base rewrite and speculation rules are HTML.
		s.stateHeaders(w, res)
		w.Header().Set("Content-Type", outputContentType(Route{Output: t.output}))
		w.WriteHeader(status)
		_, _ = w.Write(withProlog(t.prolog, body))
		return
	}
	// Fragment navigation (v22): a page that loads nav.js keeps its marks
	// and names its layout; when the navigation asking for it shows that
	// same layout, only the page is sent. Every other page loses the marks.
	if start, end, ok := pageRegion(body); ok && status == http.StatusOK && !t.errorPage && bytes.Contains(body, []byte(eng.AssetBase()+navScriptMark)) {
		fp := layoutPrint(body, start, end)
		w.Header().Add("Vary", headerNav)
		w.Header().Set(headerLayout, fp)
		if fills.Len() == 0 && r.Header.Get(headerNav) == fp {
			s.stateHeaders(w, res)
			w.Header().Set(headerFragment, "1")
			w.Header().Set("Content-Type", htmlContentType)
			w.WriteHeader(status)
			_, _ = w.Write(rebase(fragmentDoc(body, start, end), s.opts.BasePath, s.opts.KeepPaths))
			return
		}
		body = injectLayoutMeta(body, fp)
	} else {
		body = stripMarks(body)
	}
	if s.opts.Dev {
		// Added to this response's bytes only: the cached Result and an
		// exported file never see a clock reading.
		body = injectBeforeBody(body, toolbarHTML(stats, t.hit, time.Since(t.start)))
	}
	body = s.speculate(s.finishHTML(body))
	s.stateHeaders(w, res)
	w.Header().Set("Content-Type", htmlContentType)
	if fills.Len() > 0 {
		s.streamPage(w, fl, r, status, body, fills)
		return
	}
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// streams reports whether a request gets a streamed page:
// only a browser loading a document, which runs the inline script that
// moves each island into place. curl, crawlers and nav.js (which fetches
// with mode cors and swaps the whole page in) get the page rendered in
// place: complete without JavaScript, the same bytes as a plain island.
func streams(r *http.Request) bool {
	return r.Header.Get(secFetchMode) == secFetchNavigate
}

// streamPage sends a page whose stream islands are still rendering: the
// page up to its last </body>, a flush, each island as it is ready
// (flushed, URLs rewritten like the page's), then the rest. The status is
// sent with the first flush, so a late island that fails shows its
// fallback and never turns the page into an error.
func (s *Site) streamPage(w http.ResponseWriter, fl http.Flusher, r *http.Request, status int, body []byte, fills *brick.Fills) {
	head, tail := body, []byte(nil)
	if i := bytes.LastIndex(bytes.ToLower(body), []byte(bodyClose)); i >= 0 {
		head, tail = body[:i], body[i:]
	}
	w.Header().Set(accelBuffering, "no")
	w.WriteHeader(status)
	_, _ = w.Write(head)
	fl.Flush()
	err := fills.Each(r.Context(), func(b []byte) error {
		if _, err := w.Write(rebase(b, s.opts.BasePath, s.opts.KeepPaths)); err != nil {
			return err
		}
		fl.Flush()
		return nil
	})
	if err != nil && r.Context().Err() == nil {
		s.renderFailed(r, err)
	}
	// An island that failed shows its fallback, so the response is a
	// success; the operator still needs the file:line.
	for _, ferr := range fills.Failed() {
		s.renderFailed(r, ferr)
	}
	_, _ = w.Write(tail)
}

// renderFailed logs a page or island that failed to render. Prod sends
// the visitor a bare 500 that names nothing, so without this line the
// failing file:line exists nowhere an operator can read it; Dev shows the
// same error inline as well.
func (s *Site) renderFailed(r *http.Request, err error) {
	s.opts.Logger.Error("site: render failed", "url", r.URL.Path, "err", err)
}

// formPost answers a brick:submit form posted without JavaScript to the
// page at p: the action runs through Engine.FormPost, then the
// visitor is sent back to the page (303, the new state in a short-lived
// cookie), or, when the action returned errors or the state does not fit
// in a cookie, gets the page itself rendered with that state — 422 for
// errors, so the errors and typed values show.
func (s *Site) formPost(w http.ResponseWriter, r *http.Request, st *state, p string) {
	start := time.Now()
	w.Header().Set("Cache-Control", noStore)
	out, err := st.eng.FormPost(r)
	if err != nil {
		st.eng.FormError(w, r, err)
		return
	}
	loc := out.Location
	if out.Effect {
		loc = s.basePrefixed(loc)
	}
	if !out.Failed && (out.Resume.Handle == "" || st.eng.SetFormResume(w, r, out.Resume)) {
		http.Redirect(w, r, loc, http.StatusSeeOther)
		return
	}
	status := http.StatusOK
	if out.Failed {
		status = http.StatusUnprocessableEntity
	}
	if out.Effect {
		// An action that redirects elsewhere is followed even with errors.
		http.Redirect(w, r, loc, http.StatusSeeOther)
		return
	}
	res, hit, rerr := s.renderRequest(r, st, p)
	if isNotFound(rerr) {
		visitorNoStore(w, rerr)
		s.notFound(w, r)
		return
	}
	if s.answer(w, r, rerr, false) {
		return
	}
	s.writePage(w, r, res, rerr, status, pageTiming{start: start, hit: hit, output: st.routeAt(p).Output, prolog: st.routeAt(p).prolog, form: brick.WithFormResume(out.Resume)})
}

// injectLive puts the live-reload script before </body>, or at the end.
func injectLive(b []byte, base string) []byte {
	return injectBeforeBody(b, liveScript(base))
}

// notFound renders content/404.html with status 404, or a plain 404.
func (s *Site) notFound(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	res, ok, err := s.RenderNotFound(r.Context())
	if !ok {
		http.NotFound(w, r)
		return
	}
	s.writePage(w, r, res, err, http.StatusNotFound, pageTiming{start: start, errorPage: true})
}

// serveFile serves name from fsys; it reports false when there is no such
// file. Hashed component assets are cached forever.
func (s *Site) serveFile(w http.ResponseWriter, r *http.Request, fsys fs.FS, name string, immutable bool) bool {
	if name == "" || !fs.ValidPath(name) {
		if immutable {
			http.NotFound(w, r)
		}
		return false
	}
	f, err := fsys.Open(name)
	if err != nil {
		if immutable {
			http.NotFound(w, r)
		}
		return false
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.IsDir() {
		if immutable {
			http.NotFound(w, r)
		}
		return false
	}
	if ct := mime.TypeByExtension(path.Ext(name)); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	if immutable {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	}
	if rs, ok := f.(io.ReadSeeker); ok {
		http.ServeContent(w, r, path.Base(name), st.ModTime(), rs)
		return true
	}
	b, err := io.ReadAll(f)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return true
	}
	http.ServeContent(w, r, path.Base(name), st.ModTime(), bytes.NewReader(b))
	return true
}

// play renders one component: /_brick/play/Card?title=Hi. Query values are
// text props, as if written as attributes.
func (s *Site) play(w http.ResponseWriter, r *http.Request, name string) {
	props := map[string]any{}
	for k, vs := range r.URL.Query() {
		if len(vs) > 0 && k != "children" {
			props[k] = vs[len(vs)-1]
		}
	}
	out, err := s.current().eng.RenderComponent(r.Context(), name, props, r.URL.Query().Get("children"), brick.WithLocale(r.URL.Query().Get("lang")))
	status := http.StatusOK
	if err != nil {
		status = http.StatusInternalServerError
		s.renderFailed(r, err)
		if out == nil {
			http.Error(w, err.Error(), status)
			return
		}
	}
	page := fmt.Sprintf("<!doctype html><html><head><meta charset=\"utf-8\"><title>%s · playground</title></head><body>%s</body></html>", name, out)
	w.Header().Set("Content-Type", htmlContentType)
	w.WriteHeader(status)
	_, _ = w.Write(s.finishHTML([]byte(page)))
}

// hub fans invalidation events out to live-reload clients.
type hub struct {
	mu      sync.Mutex
	clients map[chan []string]struct{}
}

func newHub() *hub { return &hub{clients: map[chan []string]struct{}{}} }

// publish tells every client which URLs changed; a slow client misses the
// event rather than blocking the rebuild.
func (h *hub) publish(urls []string) {
	if len(urls) == 0 {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clients {
		select {
		case c <- urls:
		default:
		}
	}
}

// publishError reloads every page so the Dev error block shows.
func (h *hub) publishError(error) { h.publish([]string{"*"}) }

// serve is the server-sent events stream.
func (h *hub) serve(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	c := make(chan []string, 4)
	h.mu.Lock()
	h.clients[c] = struct{}{}
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		delete(h.clients, c)
		h.mu.Unlock()
	}()
	fmt.Fprint(w, ": connected\n\n")
	fl.Flush()
	t := time.NewTicker(heartbeat)
	defer t.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-t.C:
			fmt.Fprint(w, ": ping\n\n")
			fl.Flush()
		case urls := <-c:
			b, _ := json.Marshal(urls)
			fmt.Fprintf(w, "data: %s\n\n", b)
			fl.Flush()
		}
	}
}

// ListenAndServe serves the site on addr and, in Dev mode, watches the
// project for changes, until ctx ends. It is ListenAndServeHandler for this
// one site.
func (s *Site) ListenAndServe(ctx context.Context, addr string) error {
	return ListenAndServeHandler(ctx, addr, s.Handler(), s)
}
