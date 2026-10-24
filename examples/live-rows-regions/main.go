// Command live-rows-regions shows three ways an action changes part of a
// page without the component's state holding all of it: a chat log fed
// row by row (brick.rows), a "load more" list that keeps its earlier pages
// (brick.region), streamed text (brick.stream), and a sign-in form handed
// to the browser's own submit (brick.submit).
//
//	go run ./live-rows-regions             serve on :8085, then open /
//	go run ./live-rows-regions -demo       print what each action sends
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/ubgo/brick"
	assetshttp "github.com/ubgo/brick/contrib/assets-http"
	scriptstarlark "github.com/ubgo/brick/contrib/script-starlark"
)

// page is the one page: Chat, Feed and Login side by side.
const page = "content/index.html"

// app is the engine; every list lives in the browser or in your own data,
// never in a component's state.
type app struct {
	eng *brick.Engine
}

// newApp builds the engine over the project in dir, with Starlark for the
// actions and an in-memory store for the components' small state.
func newApp(dir string) (*app, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	sc, err := scriptstarlark.New(scriptstarlark.Options{})
	if err != nil {
		return nil, err
	}
	eng, err := brick.New(brick.Options{FS: root.FS(), Scripting: sc, States: brick.NewMemoryStates(0, 0)})
	if err != nil {
		return nil, err
	}
	return &app{eng: eng}, nil
}

// handler serves the page, the engine's routes, and POST /session: the
// classic form post brick.submit hands the sign-in form to.
// ds:def id=app-handler-t9c9w5t8
func (a *app) handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle(brick.DefaultAssetBase, assetshttp.Handler(brick.DefaultAssetBase, func() assetshttp.Source { return a.eng }))
	mux.Handle("POST "+brick.DefaultAssetBase+brick.ActionEndpoint, a.eng.ActionHandler())
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		html, err := a.eng.Render(r.Context(), page, nil)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(html)
	})
	mux.HandleFunc("POST /session", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "signed in as %s", r.FormValue("email"))
	})
	return mux
}

// defaultAddr is this example's own port, so the examples of one guide can run
// side by side. Override it with -addr when the port is taken.
const defaultAddr = ":8085"

// main serves the same handler the tests drive, so the behaviour pinned in
// main_test.go is what a browser gets here. Flags only choose where, never what.
func main() {
	addr := flag.String("addr", defaultAddr, "listen address")
	dir := flag.String("theme", "live-rows-regions/theme", "the project directory")
	demoRun := flag.Bool("demo", false, "print what each action sends, then exit")
	flag.Parse()
	a, err := newApp(*dir)
	if err != nil {
		log.Fatal(err)
	}
	if *demoRun {
		if err := demo(os.Stdout, a.handler()); err != nil {
			log.Fatal(err)
		}
		return
	}
	log.Printf("serving on %s", *addr)
	log.Fatal(http.ListenAndServe(*addr, a.handler()))
}
