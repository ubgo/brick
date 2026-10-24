// Command interactivity-nested serves a ContactsApp that holds one
// stateful ContactRow per contact. It shows the three things that make
// nested stateful components work: a row keeps its own open/closed state
// when the parent acts, a prop marked reactive: true follows the parent
// anyway, and each row is keyed (:key) so state follows the contact and
// not its position. It also shows how a host looks at live instances
// (Engine.Instance) and hears when one ends (an InstanceEndHook).
//
//	cd examples/interactivity-nested && go run .      then open http://localhost:8096/
package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"os"
	"slices"
	"sync"

	"github.com/ubgo/brick"
	assetshttp "github.com/ubgo/brick/contrib/assets-http"
	scriptstarlark "github.com/ubgo/brick/contrib/script-starlark"
)

// page is the one page this app serves.
const page = "content/index.html"

// handleParam is the query parameter /inspect reads a page's
// data-brick-state value from.
const handleParam = "handle"

// ends keeps what an InstanceEndHook was told. A real host would delete
// the files an ended instance's Pending uploads name, or count sessions;
// the example only remembers handle and reason so a page can show them.
type ends struct {
	mu   sync.Mutex
	list []brick.InstanceEnd
}

// OnInstanceEnd implements brick.InstanceEndHook. It describes an
// instance that no longer exists, so it has no request; it runs on the
// goroutine that noticed the end (a sweep, an eviction, a revoke), which
// is why it only appends under a lock and returns.
func (e *ends) OnInstanceEnd(_ context.Context, end brick.InstanceEnd) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.list = append(e.list, end)
}

// got returns a copy of the ends heard so far.
func (e *ends) got() []brick.InstanceEnd {
	e.mu.Lock()
	defer e.mu.Unlock()
	return slices.Clone(e.list)
}

// app holds the engine and the end log.
type app struct {
	eng  *brick.Engine
	ends *ends
}

// newApp builds the engine with the end hook registered in Options.Hooks:
// the hook fires for the stores that can see an instance end (the memory
// store here), and the engine logs a warning for one that cannot.
func newApp(dir string) (*app, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	sc, err := scriptstarlark.New(scriptstarlark.Options{})
	if err != nil {
		return nil, err
	}
	log := &ends{}
	eng, err := brick.New(brick.Options{
		FS:        root.FS(),
		Scripting: sc,
		States:    brick.NewMemoryStates(0, 0),
		Hooks:     []any{log},
	})
	if err != nil {
		return nil, err
	}
	return &app{eng: eng, ends: log}, nil
}

// inspect answers /inspect?handle=default.xxxx with the instance's state
// as JSON, the way the dev toolbar lists them at /_brick/instances. Engine.Instance is a
// Go API: it is never reachable over HTTP by itself, and a sealed handle
// opens only under the session it was rendered for (pass
// brick.WithSession). Mount a route like this only behind your own auth.
func (a *app) inspect(w http.ResponseWriter, r *http.Request) {
	view, err := a.eng.Instance(r.Context(), r.URL.Query().Get(handleParam))
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"component": view.Component, "version": view.Version, "state": view.State, "props": view.Props}) // headers are sent; a failed write has no one left to tell
}

// handler serves the page, the runtime and component assets, the action
// endpoint and the inspect route.
func (a *app) handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle(brick.DefaultAssetBase+brick.ActionEndpoint, a.eng.ActionHandler())
	mux.Handle(brick.DefaultAssetBase, assetshttp.Handler(brick.DefaultAssetBase, func() assetshttp.Source { return a.eng }))
	mux.HandleFunc("GET /inspect", a.inspect)
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		res, err := a.eng.RenderChunks(r.Context(), page, nil)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		html, err := a.eng.Fill(r.Context(), res, brick.WithSession(r.Header.Get("X-Demo-Session")))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(html)
	})
	return mux
}

// defaultAddr is this example's own port, so the examples of one guide can run
// side by side. Override it with -addr when the port is taken.
const defaultAddr = ":8096"

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
