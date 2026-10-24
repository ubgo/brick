// Command interactivity-events-modifiers serves one stateful Inbox whose
// elements carry every kind of event attribute: input with .debounce, blur,
// keydown and keyup with keys and combos, click.outside, change on a
// select, click.details, a confirm.prompt, click.once, intersect.once and
// a poll. Each action appends its name to a log the page shows, so a test
// (or a person clicking) can see exactly which call ran with which value.
//
//	cd examples/interactivity-events-modifiers && go run .      then open http://localhost:8092/
package main

import (
	"flag"
	"log"
	"log/slog"
	"net/http"
	"os"

	"github.com/ubgo/brick"
	"github.com/ubgo/brick/actionmw"
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
// in-memory state store, the two things actions need. Actions are logged
// to slog's default logger.
func newApp(dir string) (*app, error) {
	return newAppLogged(dir, slog.Default())
}

// newAppLogged is newApp with the action log going to logger. The
// actionmw.Slog middleware writes one line per action, and a component's
// brick: {log:} setting decides whether its successful actions appear and
// at which level.
func newAppLogged(dir string, logger *slog.Logger) (*app, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	sc, err := scriptstarlark.New(scriptstarlark.Options{})
	if err != nil {
		return nil, err
	}
	eng, err := brick.New(brick.Options{
		FS:               root.FS(),
		Scripting:        sc,
		States:           brick.NewMemoryStates(0, 0),
		ActionMiddleware: []brick.ActionMiddleware{actionmw.Slog(logger)},
	})
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
const defaultAddr = ":8092"

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
