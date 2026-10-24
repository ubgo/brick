// Command interactivity-lazy-stream shows the parts of a page that render
// later than the page: an island (Account, filled per request from a
// provided user), a lazy island (SalesChart, fetched by the browser when
// it scrolls into view), a streamed island (Revenue, sent later in the
// same response) and a streamed action reply (Ask, whose text grows as
// brick.stream sends it). The page itself is rendered once and cached.
//
//	cd examples/interactivity-lazy-stream && go run .      then open http://localhost:8093/?user=Sara
package main

import (
	"bytes"
	"flag"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

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

// ticketTTL is how long a lazy island's ticket redeems: a visitor who
// leaves the tab open for the afternoon still gets the chart, a ticket
// found in an old log is dead by the next day.
const ticketTTL = 4 * time.Hour

// demoKeyByte fills the demo's 32-byte ticket key. It is public in this
// source, so it protects nothing: a real deployment makes a key with
// `brick keygen` and reads it from the environment (brick.KeysFromEnv).
const demoKeyByte = 0x2a

// demoKeys is the key ring newApp seals tickets under.
func demoKeys() (brick.KeyRing, error) {
	return brick.StaticKeys(bytes.Repeat([]byte{demoKeyByte}, brick.StateKeySize))
}

// newApp builds the engine with tickets sealed under the demo key ring.
func newApp(dir string) (*app, error) {
	keys, err := demoKeys()
	if err != nil {
		return nil, err
	}
	return newAppWithKeys(dir, keys)
}

// newAppWithKeys builds the engine with Options.IslandTickets set: a
// lazy island's ticket is the island itself, sealed (encrypted and
// authenticated) under keys, so it redeems on any server holding the same
// keys, survives a restart, and a changed byte is refused. Without this
// option tickets live in process memory and a restart or a second server
// answers 410 (the engine warns about it in Dev). The ticket store must
// be its own: never the store that holds component state.
func newAppWithKeys(dir string, keys brick.KeyRing) (*app, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	sc, err := scriptstarlark.New(scriptstarlark.Options{})
	if err != nil {
		return nil, err
	}
	sealed, err := brick.NewSealedStates(brick.SealedOptions{Sealer: brick.NewAESGCMSealer(keys, nil), TTL: ticketTTL})
	if err != nil {
		return nil, err
	}
	eng, err := brick.New(brick.Options{
		FS:            root.FS(),
		Scripting:     sc,
		States:        brick.NewMemoryStates(0, 0),
		IslandTickets: brick.NewIslandTickets(sealed),
	})
	if err != nil {
		return nil, err
	}
	return &app{eng: eng}, nil
}

// handler mounts the three engine endpoints a page with islands needs:
// actions, lazy islands, and the asset files.
// ds:def id=app-handler-peugtyhy
func (a *app) handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle(brick.DefaultAssetBase+brick.ActionEndpoint, a.eng.ActionHandler())
	mux.Handle(brick.DefaultAssetBase+brick.IslandEndpoint, a.eng.IslandHandler())
	mux.Handle(brick.DefaultAssetBase, assetshttp.Handler(brick.DefaultAssetBase, func() assetshttp.Source { return a.eng }))
	mux.HandleFunc("GET /{$}", a.index)
	return mux
}

// userOf is this demo's stand-in for a login: ?user=Sara.
func userOf(r *http.Request) []brick.RenderOption {
	if u := r.URL.Query().Get("user"); u != "" {
		return []brick.RenderOption{brick.Provide("user", u)}
	}
	return nil
}

// index fills the cached page. A browser loading the page gets the
// stream islands later in the same response; anything else (curl, a
// crawler, a test) gets them rendered in place, with the same bytes as a
// plain island.
// ds:def id=app-index-r7v5qe8m
func (a *app) index(w http.ResponseWriter, r *http.Request) {
	a.once.Do(func() { a.page, a.err = a.eng.RenderChunks(r.Context(), page, nil) })
	if a.err != nil {
		http.Error(w, a.err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	flusher, canFlush := w.(http.Flusher)
	if canFlush && a.page.HasStream() && r.Header.Get("Sec-Fetch-Mode") == "navigate" {
		w.Header().Set("X-Accel-Buffering", "no") // tell nginx not to buffer
		if err := a.eng.Stream(r.Context(), w, flusher.Flush, a.page, userOf(r)...); err != nil {
			log.Printf("stream: %v", err)
		}
		return
	}
	html, err := a.eng.Fill(r.Context(), a.page, userOf(r)...)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_, _ = w.Write(html)
}

// defaultAddr is this example's own port, so the examples of one guide can run
// side by side. Override it with -addr when the port is taken.
const defaultAddr = ":8093"

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
