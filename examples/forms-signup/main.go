// Command forms-signup serves one signup form whose fields are
// brick:model.defer: typing sends nothing, and Save sends one action that
// carries every held value as sets (a list for the topic checkboxes, a
// dotted path for the city). The engine is mounted on a plain net/http
// mux, with the action endpoint and the runtime assets beside the page.
//
//	go run ./forms-signup -addr :8084      then open /
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
// ds:def id=page-8we867s4
const page = "content/index.html"

// newApp opens the theme directory and builds an engine whose stateful
// components keep their state in this process (NewMemoryStates with the
// default size and lifetime), which is all a demo needs.
// ds:def id=newapp-afwdpfg2
func newApp(dir string) (*brick.Engine, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	sc, err := scriptstarlark.New(scriptstarlark.Options{})
	if err != nil {
		return nil, err
	}
	return brick.New(brick.Options{FS: root.FS(), Scripting: sc, States: brick.NewMemoryStates(0, 0)})
}

// handler mounts the action endpoint, the runtime assets and the page.
// The action route must be registered before the asset prefix, which
// would otherwise claim every /_brick/ path.
// ds:def id=handler-htt4fe93
func handler(eng *brick.Engine) http.Handler {
	mux := http.NewServeMux()
	mux.Handle(brick.DefaultAssetBase+brick.ActionEndpoint, eng.ActionHandler())
	mux.Handle(brick.DefaultAssetBase, assetshttp.Handler(brick.DefaultAssetBase, func() assetshttp.Source { return eng }))
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		html, err := eng.Render(r.Context(), page, nil)
		if err != nil {
			http.Error(w, "render failed", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(html)
	})
	return mux
}

// defaultAddr is this example's own port, so the examples of one guide can run
// side by side. Override it with -addr when the port is taken.
const defaultAddr = ":8084"

// main serves the same handler the tests drive, so the behaviour pinned in
// main_test.go is what a browser gets here. Flags only choose where, never what.
func main() {
	addr := flag.String("addr", defaultAddr, "listen address")
	dir := flag.String("theme", "forms-signup/theme", "the project directory")
	flag.Parse()
	eng, err := newApp(*dir)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("serving on %s", *addr)
	log.Fatal(http.ListenAndServe(*addr, handler(eng)))
}
