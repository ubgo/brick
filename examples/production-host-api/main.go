// Command production-host-api uses the brick engine from Go with no site
// layer: Render for a whole page, RenderComponent for one component,
// RenderChunks and Fill for a page cached once with a per-request island,
// ActionHandler for the browser runtime's actions, and RunAction and
// Instance for code that calls actions without HTTP.
//
//	go run ./production-host-api                  serve on :8086
//	go run ./production-host-api -render static   print one page and exit
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/ubgo/brick"
	assetshttp "github.com/ubgo/brick/contrib/assets-http"
	scriptstarlark "github.com/ubgo/brick/contrib/script-starlark"
)

// home is the page with an island, cached as a Result and filled per
// request.
const home = "content/index.html"

// teamPage holds an island that renders components of its own, so the
// counts Fill adds are visible.
const teamPage = "content/team.html"

// limits are the render bounds this host sets. brick's defaults are far
// larger; a host that renders data it does not control lowers them so one
// bad record cannot make a page of a million components.
// ds:def id=limits-ud5w8x9n
type limits struct {
	MaxComponents  int
	MaxOutputBytes int
	// MaxActionBytes caps an action request's JSON body; zero keeps the
	// default (brick.DefaultMaxActionBytes).
	MaxActionBytes int
	// ScriptSteps and ScriptTimeout bound one script call (a loader or an
	// action function); zero keeps the Starlark runtime's own defaults
	// (scriptstarlark.DefaultSteps, DefaultTimeout).
	ScriptSteps   uint64
	ScriptTimeout time.Duration
}

// newEngine builds the engine. Every Options field is optional; this host
// sets the project root, a script runtime, a state store, its limits and
// its own action error envelope.
// ds:def id=newengine-z7ddf77v
func newEngine(dir string, l limits) (*brick.Engine, error) {
	return buildEngine(dir, l, sources{})
}

// sources are the two inputs a render reads that a test wants to fix: the
// clock and the random source. The zero value keeps brick's own (the real
// clock, crypto/rand).
type sources struct {
	Clock brick.Clock
	Rand  io.Reader
}

// newPinnedEngine is newEngine with the clock and the random source fixed,
// the way a golden test needs them: stateful components carry random
// action tokens, and a pinned source makes two renders carry the same one.
// ds:def id=newpinnedengine-krcch6ug
func newPinnedEngine(dir string, src sources) (*brick.Engine, error) {
	return buildEngine(dir, limits{}, src)
}

// buildEngine is the one place the host's Options are assembled.
// ds:def id=buildengine-dmfsuna9
func buildEngine(dir string, l limits, src sources) (*brick.Engine, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	sc, err := scriptstarlark.New(scriptstarlark.Options{Steps: l.ScriptSteps, Timeout: l.ScriptTimeout})
	if err != nil {
		return nil, err
	}
	return brick.New(brick.Options{
		FS:             root.FS(),
		Scripting:      sc,
		States:         brick.NewMemoryStates(0, 0),
		MaxComponents:  l.MaxComponents,
		MaxOutputBytes: l.MaxOutputBytes,
		MaxActionBytes: l.MaxActionBytes,
		ActionErrors:   actionError,
		Clock:          src.Clock,
		Rand:           src.Rand,
	})
}

// actionError is the host's own envelope for a failed action: the status
// brick would have chosen, with a JSON body a gateway can parse instead of
// the default plain text.
// ds:def id=actionerror-cy5tkee6
func actionError(w http.ResponseWriter, _ *http.Request, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(brick.ActionStatus(err))
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

// inspect reads what the store holds for one stateful component, the same
// view the dev server lists at /_brick/instances. Engine.Instance changes
// nothing, so a host's admin page or test can call it freely.
// ds:def id=inspect-756aqtra
func inspect(ctx context.Context, eng *brick.Engine, handle string) (component string, version int64, err error) {
	view, err := eng.Instance(ctx, handle)
	if err != nil {
		return "", 0, err
	}
	return view.Component, view.Version, nil
}

// server is the host's HTTP side: the engine and the home page's Result,
// rendered once.
type server struct {
	eng  *brick.Engine
	once sync.Once
	page *brick.Result
	err  error
}

// handler mounts the engine's own handlers beside the host's routes.
// ds:def id=server-handler-f4ujmxxj
func (s *server) handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle(brick.DefaultAssetBase+brick.ActionEndpoint, s.eng.ActionHandler())
	mux.Handle(brick.DefaultAssetBase, assetshttp.Handler(brick.DefaultAssetBase, func() assetshttp.Source { return s.eng }))
	mux.HandleFunc("GET /{$}", s.home)
	mux.HandleFunc("GET /preview/{name}", s.preview)
	mux.HandleFunc("GET /stats", s.stats)
	return mux
}

// home fills the cached page's island for this request: the shared parts
// are rendered once, only <Whoami> runs per response.
// ds:def id=server-home-z8tngqqw
func (s *server) home(w http.ResponseWriter, r *http.Request) {
	s.once.Do(func() { s.page, s.err = s.eng.RenderChunks(context.Background(), home, nil) })
	if s.err != nil {
		log.Printf("render %s: %v", home, s.err)
		http.Error(w, "render failed", http.StatusInternalServerError)
		return
	}
	var opts []brick.RenderOption
	if u := r.URL.Query().Get("user"); u != "" {
		opts = append(opts, brick.Provide("user", u))
	}
	html, err := s.eng.Fill(r.Context(), s.page, opts...)
	if err != nil {
		log.Printf("fill %s: %v", home, err)
		http.Error(w, "render failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(html)
}

// pageStats reports what one request rendered: the page's own counts plus
// what Fill added for its islands. Result.Stats is shared by every request
// that uses the cached Result, so the host copies it before Fill adds to it.
// ds:def id=pagestats-cxvaastn
func pageStats(ctx context.Context, eng *brick.Engine, page *brick.Result) (brick.RenderStats, error) {
	stats := page.Stats
	if _, err := eng.Fill(ctx, page, brick.WithStats(&stats)); err != nil {
		return brick.RenderStats{}, err
	}
	return stats, nil
}

// stats answers /stats with the counts as JSON, the numbers the dev
// toolbar shows.
func (s *server) stats(w http.ResponseWriter, r *http.Request) {
	page, err := s.eng.RenderChunks(r.Context(), teamPage, nil)
	if err != nil {
		http.Error(w, "render failed", http.StatusInternalServerError)
		return
	}
	st, err := pageStats(r.Context(), s.eng, page)
	if err != nil {
		http.Error(w, "render failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(st)
}

// preview renders one component alone with its props from the query
// string, the way a component gallery or a test does.
// ds:def id=server-preview-dczhmp94
func (s *server) preview(w http.ResponseWriter, r *http.Request) {
	props := map[string]any{}
	for k := range r.URL.Query() {
		props[k] = r.URL.Query().Get(k)
	}
	html, err := s.eng.RenderComponent(r.Context(), r.PathValue("name"), props, "")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(html)
}

// defaultAddr is this example's own port, so the examples of one guide can run
// side by side. Override it with -addr when the port is taken.
const defaultAddr = ":8086"

// main serves the same handler the tests drive, so the behaviour pinned in
// main_test.go is what a browser gets here. Flags only choose where, never what.
func main() {
	addr := flag.String("addr", defaultAddr, "listen address")
	dir := flag.String("theme", "production-host-api/theme", "the project directory")
	page := flag.String("render", "", "print content/<name>.html and exit")
	flag.Parse()
	eng, err := newEngine(*dir, limits{})
	if err != nil {
		log.Fatal(err)
	}
	if *page != "" {
		html, err := eng.Render(context.Background(), "content/"+*page+".html", nil)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Print(string(html))
		return
	}
	log.Printf("serving on %s", *addr)
	log.Fatal(http.ListenAndServe(*addr, (&server{eng: eng}).handler()))
}
