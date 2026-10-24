// Command methodlinks serves a list of posts whose Publish, Delete and
// New links are brick:method links (v23): a click sends PATCH, DELETE or
// POST to a plain net/http router, through brick.MethodOverride. The page
// shows the last request the router saw, so a headless Chrome check
// (scripts/v23_chrome.sh) can read what arrived.
//
//	go run ./methodlinks -addr :8083      then open /
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"sort"
	"sync"

	"github.com/ubgo/brick"
	assetshttp "github.com/ubgo/brick/contrib/assets-http"
)

// app is the engine and an in-memory list of posts.
type app struct {
	eng   *brick.Engine
	mu    sync.Mutex
	posts map[int]string
	next  int
	log   string
}

func newApp(dir string, nav bool) (*app, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	eng, err := brick.New(brick.Options{FS: root.FS(), Navigation: nav})
	if err != nil {
		return nil, err
	}
	return &app{eng: eng, posts: map[int]string{1: "First post", 2: "Second post"}, next: 3}, nil
}

// handler is the host's router; MethodOverride sits in front of it, so
// the DELETE and PATCH routes see the verb a method link asked for.
func (a *app) handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle(brick.DefaultAssetBase, assetshttp.Handler(brick.DefaultAssetBase, func() assetshttp.Source { return a.eng }))
	mux.HandleFunc("GET /{$}", a.index)
	mux.HandleFunc("GET /posts/{id}", func(w http.ResponseWriter, r *http.Request) {
		a.record("GET " + r.URL.Path)
		fmt.Fprintf(w, "post %s", r.PathValue("id"))
	})
	mux.HandleFunc("POST /posts", func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		a.posts[a.next] = fmt.Sprintf("Post %d", a.next)
		a.next++
		a.mu.Unlock()
		a.done(w, r, "POST "+r.URL.Path)
	})
	mux.HandleFunc("PATCH /posts/{id}", func(w http.ResponseWriter, r *http.Request) {
		a.done(w, r, "PATCH "+r.URL.Path)
	})
	mux.HandleFunc("DELETE /posts/{id}", func(w http.ResponseWriter, r *http.Request) {
		var id int
		_, _ = fmt.Sscan(r.PathValue("id"), &id)
		a.mu.Lock()
		delete(a.posts, id)
		a.mu.Unlock()
		a.done(w, r, "DELETE "+r.URL.Path)
	})
	return brick.MethodOverride(mux)
}

func (a *app) record(s string) {
	a.mu.Lock()
	a.log = s
	a.mu.Unlock()
}

// done records the request and redirects back to the list
// (POST-redirect-GET), as a host's write route does.
func (a *app) done(w http.ResponseWriter, r *http.Request, s string) {
	a.record(s)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (a *app) index(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	ids := make([]int, 0, len(a.posts))
	for id := range a.posts {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	posts := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		posts = append(posts, map[string]any{"id": id, "title": a.posts[id]})
	}
	props := map[string]any{"posts": posts, "log": a.log}
	a.mu.Unlock()
	html, err := a.eng.Render(r.Context(), "content/index.html", props)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(html)
}

func main() {
	addr := flag.String("addr", ":8083", "listen address")
	dir := flag.String("theme", "methodlinks/theme", "the project directory")
	nav := flag.Bool("nav", false, "turn on in-place navigation (nav.js)")
	flag.Parse()
	a, err := newApp(*dir, *nav)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("serving on %s", *addr)
	log.Fatal(http.ListenAndServe(*addr, a.handler()))
}
