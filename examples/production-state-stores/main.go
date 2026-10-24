// Command production-state-stores keeps two kinds of state in two stores:
// a search box's short-lived state in this process's memory (the default
// store) and a cart in a named store, "cart", backed by a BytesStore the
// host writes itself. The BytesStore here is a map so the example runs
// anywhere; a real one is a table or a Redis (contrib/states-sql,
// contrib/states-redis) and has exactly these three methods plus the
// optional revocation upgrade.
//
//	go run ./production-state-stores      then open /?user=ada
package main

import (
	"context"
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

// Store sizes and lifetimes. The search box is cheap to lose, so it gets
// a small in-memory store; a cart must outlive a tab, so it lives a day.
const (
	maxSearches = 1_000
	searchTTL   = 30 * time.Minute
	cartTTL     = 24 * time.Hour
)

// page is the one page this app serves.
const page = "content/index.html"

// mapStore is a BytesStore: bytes under a key, with a TTL and the hashed
// session each value belongs to. brick.Bytes adds the handles, the codec
// and the checks; the store only keeps bytes.
type mapStore struct {
	mu   sync.Mutex
	rows map[string]row
	now  func() time.Time
}

type row struct {
	data    []byte
	expires time.Time
	session string
}

// newMapStore returns an empty store that reads expiry from time.Now, kept in a
// field so a test can swap the clock instead of sleeping.
func newMapStore() *mapStore {
	return &mapStore{rows: map[string]row{}, now: time.Now}
}

// Load returns a live value; an expired one is missing, never an error.
func (m *mapStore) Load(_ context.Context, key string) ([]byte, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.rows[key]
	if !ok || !m.now().Before(r.expires) {
		return nil, false, nil
	}
	return r.data, true, nil
}

// Save stores a copy: the caller may reuse its slice.
func (m *mapStore) Save(_ context.Context, key string, data []byte, ttl time.Duration, session string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rows[key] = row{data: append([]byte(nil), data...), expires: m.now().Add(ttl), session: session}
	return nil
}

// Remove deletes key; removing a missing key is not an error, so a retried
// cleanup stays idempotent.
func (m *mapStore) Remove(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.rows, key)
	return nil
}

// RevokeSession is the optional Revoker upgrade: Engine.RevokeSession
// (sign-out, "log out other devices") ends every value of one session.
// This is why Save is given the hashed session id.
func (m *mapStore) RevokeSession(_ context.Context, session string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for k, r := range m.rows {
		if r.session == session {
			delete(m.rows, k)
		}
	}
	return nil
}

// Len is how many values the store holds, for the test.
func (m *mapStore) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.rows)
}

// newApp builds the engine with both stores and returns the cart's
// backing store so the host (and the test) can look inside it.
func newApp(dir string) (*brick.Engine, *mapStore, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, nil, err
	}
	sc, err := scriptstarlark.New(scriptstarlark.Options{})
	if err != nil {
		return nil, nil, err
	}
	carts := newMapStore()
	eng, err := brick.New(brick.Options{
		FS:        root.FS(),
		Scripting: sc,
		States:    brick.NewMemoryStates(maxSearches, searchTTL),
		StateStores: map[string]brick.StateStore{
			"cart": brick.Bytes(carts, brick.BytesOptions{TTL: cartTTL}),
		},
		Sessions: brick.SessionFunc(userOf),
	})
	return eng, carts, err
}

// userCookie carries the stand-in session: the page sets it from ?user=,
// and the runtime's action requests send it back. A real app reads its
// own session cookie.
const userCookie = "user"

// userOf reads the signed-in user from the cookie, falling back to ?user= so a
// first page load without a cookie still works. It never errors: anonymous is a
// valid answer here.
func userOf(r *http.Request) (string, error) {
	if c, err := r.Cookie(userCookie); err == nil {
		return c.Value, nil
	}
	return r.URL.Query().Get(userCookie), nil
}

// handler serves the pages, the action endpoint and the runtime assets from one
// mux, the minimum a host needs for stateful components.
func handler(eng *brick.Engine) http.Handler {
	mux := http.NewServeMux()
	mux.Handle(brick.DefaultAssetBase+brick.ActionEndpoint, eng.ActionHandler())
	mux.Handle(brick.DefaultAssetBase, assetshttp.Handler(brick.DefaultAssetBase, func() assetshttp.Source { return eng }))
	mux.HandleFunc("POST /signout", func(w http.ResponseWriter, r *http.Request) {
		u, _ := userOf(r)
		if err := eng.RevokeSession(r.Context(), u); err != nil {
			log.Printf("sign-out: %v", err)
		}
		http.Redirect(w, r, "/", http.StatusSeeOther)
	})
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		u, _ := userOf(r)
		if u != "" {
			http.SetCookie(w, &http.Cookie{Name: userCookie, Value: u, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode})
		}
		html, err := eng.Render(r.Context(), page, nil, brick.WithSession(u))
		if err != nil {
			log.Printf("render %s: %v", page, err)
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
const defaultAddr = ":8088"

// main serves the same handler the tests drive, so the behaviour pinned in
// main_test.go is what a browser gets here. Flags only choose where, never what.
func main() {
	addr := flag.String("addr", defaultAddr, "listen address")
	dir := flag.String("theme", "production-state-stores/theme", "the project directory")
	flag.Parse()
	eng, _, err := newApp(*dir)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("serving on %s", *addr)
	log.Fatal(http.ListenAndServe(*addr, handler(eng)))
}
