// Command engineonly embeds the brick engine in an ordinary net/http app,
// without the site layer: the page is rendered once and cached as a
// Result, its island is filled per request, component assets are served by
// contrib/assets-http, and actions by Engine.ActionHandler.
//
//	go run ./engineonly -addr :8081      then open /?user=Sara
package main

import (
	"context"
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

// app holds the engine and the page's cached Result.
type app struct {
	eng  *brick.Engine
	once sync.Once
	page *brick.Result
	err  error
}

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

// handler serves the page, the assets and the actions.
func (a *app) handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle(brick.DefaultAssetBase+brick.ActionEndpoint, a.eng.ActionHandler())
	mux.Handle(brick.DefaultAssetBase, assetshttp.Handler(brick.DefaultAssetBase, func() assetshttp.Source { return a.eng }))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Render the page once; every response only fills its island.
		a.once.Do(func() { a.page, a.err = a.eng.RenderChunks(context.Background(), page, nil) })
		if a.err != nil {
			http.Error(w, a.err.Error(), http.StatusInternalServerError)
			return
		}
		// Provide the user only when there is one: a provided value, even
		// empty, counts, and would hide the component's inject: default.
		var opts []brick.RenderOption
		if u := r.URL.Query().Get("user"); u != "" {
			opts = append(opts, brick.Provide("user", u))
		}
		html, err := a.eng.Fill(r.Context(), a.page, opts...)
		if err != nil {
			http.Error(w, "render failed", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(html)
	})
	return mux
}

func main() {
	addr := flag.String("addr", ":8081", "listen address")
	dir := flag.String("theme", "engineonly/theme", "the project directory")
	flag.Parse()
	a, err := newApp(*dir)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("serving on %s", *addr)
	log.Fatal(http.ListenAndServe(*addr, a.handler()))
}
