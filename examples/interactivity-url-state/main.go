// Command interactivity-url-state shows the state keys that tie a
// component to the URL, the session and the server: query: (fields in the
// address bar), params: (a hook when the URL changes them), remember:
// (fields kept per session), temporary: (fields never saved), locked:
// (fields only the server writes) and writable: (the only fields the
// client may write). The engine is used alone, so the session binding and
// the per-request query are visible in the handler.
//
//	cd examples/interactivity-url-state && go run .      then open http://localhost:8091/?search=Acme&page=3
package main

import (
	"crypto/rand"
	"flag"
	"log"
	"net/http"
	"os"
	"sync"

	"github.com/ubgo/brick"
	assetshttp "github.com/ubgo/brick/contrib/assets-http"
	scriptstarlark "github.com/ubgo/brick/contrib/script-starlark"
)

// page is the one page this app serves.
const page = "content/index.html"

// sessionCookie names the cookie that carries this demo's session id;
// remember: keys its values by that id, and an action must come from the
// session that rendered the component.
const sessionCookie = "sid"

// app holds the engine and the page's cached Result: the page itself is
// rendered once, and each request only fills its stateful islands.
type app struct {
	eng  *brick.Engine
	once sync.Once
	page *brick.Result
	err  error
}

// sessionOf is the app's own idea of who is asking. A real app reads its
// login session here; brick only keeps a hash of it.
func sessionOf(r *http.Request) (string, error) {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return "", nil // no session: remember: loads and saves nothing
	}
	return c.Value, nil
}

// newApp builds an engine with a state store, a Rememberer for remember:
// fields and the session binding both renders and actions use.
// ds:def id=newapp-cfau8wfz
func newApp(dir string) (*app, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	sc, err := scriptstarlark.New(scriptstarlark.Options{})
	if err != nil {
		return nil, err
	}
	eng, err := brick.New(brick.Options{
		FS:        root.FS(),
		Scripting: sc,
		States:    brick.NewMemoryStates(0, 0),
		Remember:  brick.NewMemoryRemember(),
		Sessions:  brick.SessionFunc(sessionOf),
	})
	if err != nil {
		return nil, err
	}
	return &app{eng: eng}, nil
}

// handler serves the page, the assets and the action endpoint.
func (a *app) handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle(brick.DefaultAssetBase+brick.ActionEndpoint, a.eng.ActionHandler())
	mux.Handle(brick.DefaultAssetBase, assetshttp.Handler(brick.DefaultAssetBase, func() assetshttp.Source { return a.eng }))
	mux.HandleFunc("GET /{$}", a.index)
	return mux
}

// index fills the cached page for this visitor: their session, and the
// URL's query, which seeds every query: field of a new instance.
// ds:def id=app-index-7uzt29qc
func (a *app) index(w http.ResponseWriter, r *http.Request) {
	a.once.Do(func() { a.page, a.err = a.eng.RenderChunks(r.Context(), page, nil) })
	if a.err != nil {
		http.Error(w, a.err.Error(), http.StatusInternalServerError)
		return
	}
	sid, _ := sessionOf(r)
	if sid == "" {
		sid = rand.Text()
		http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: sid, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode})
	}
	html, err := a.eng.Fill(r.Context(), a.page, brick.WithSession(sid), brick.WithQuery(r.URL.Query()))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(html)
}

// defaultAddr is this example's own port, so the examples of one guide can run
// side by side. Override it with -addr when the port is taken.
const defaultAddr = ":8091"

// main serves the same handler the tests drive, so the behaviour pinned in
// main_test.go is what a browser gets here. Flags only choose where, never what.
func main() {
	addr := flag.String("addr", defaultAddr, "listen address")
	dir := flag.String("theme", "theme", "the project directory")
	flag.Parse()
	a, err := newApp(*dir)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("serving on %s", *addr)
	log.Fatal(http.ListenAndServe(*addr, a.handler()))
}
