// Command interactivity-client-commands serves a page with two parts the
// browser runtime drives: a Menu that opens and closes with brick:js
// client commands (no server, no state), and a stateful Todos list that
// uses the morph controls (brick:key, brick:ignore, brick:replace), a
// view transition on its root (brick:transition), an action that picks
// its own transition (brick.transition) and enter and leave classes on a
// brick:show panel. Everything the server does is tested; what the
// browser does with the attributes is described in the guide.
//
//	cd examples/interactivity-client-commands && go run .      then open http://localhost:8095/
package main

import (
	"flag"
	"log"
	"net/http"
	"os"

	"github.com/ubgo/brick"
	assetshttp "github.com/ubgo/brick/contrib/assets-http"
	scriptstarlark "github.com/ubgo/brick/contrib/script-starlark"
)

// page is the one page this app serves.
const page = "content/index.html"

// app holds the engine; the page is rendered per request because a
// stateful component is an island and every visitor gets their own.
type app struct {
	eng *brick.Engine
}

// newApp builds an engine over dir with Starlark scripting and an
// in-memory state store, the two things actions need.
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

// handler serves the page, the runtime and component assets, and the
// action endpoint the runtime posts clicks to.
func (a *app) handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle(brick.DefaultAssetBase+brick.ActionEndpoint, a.eng.ActionHandler())
	mux.Handle(brick.DefaultAssetBase, assetshttp.Handler(brick.DefaultAssetBase, func() assetshttp.Source { return a.eng }))
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		res, err := a.eng.RenderChunks(r.Context(), page, nil)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		// Fill renders the islands (here, the stateful Counter) and the
		// asset block for this request.
		html, err := a.eng.Fill(r.Context(), res)
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
const defaultAddr = ":8095"

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
