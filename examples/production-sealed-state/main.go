// Command production-sealed-state serves a counter whose state is sealed
// into the page with the keys in BRICK_SECRET: the server keeps nothing,
// so the count survives a restart, a deploy or a second server. Each
// visitor gets a session cookie the state is bound to, and /signout
// refuses every blob that session was issued before.
//
//	BRICK_SECRET=$(brick keygen) go run ./production-sealed-state      then open /
package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
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

// secretEnv is the variable the key ring is read from: comma-separated
// base64 keys, newest first (the format brick keygen prints and the brick
// CLI reads), so the same value works for this program and for brick serve.
const secretEnv = "BRICK_SECRET"

// sessionCookie names the cookie that carries the visitor's session id.
// The id is the app's own; brick only ever keeps its SHA-256.
const sessionCookie = "sid"

// page is the one page this app serves.
const page = "content/index.html"

// app is the engine plus the sign-out list the sealed store consults.
type app struct {
	eng     *brick.Engine
	mu      sync.Mutex
	revoked map[string]time.Time // hashed session id → when it signed out
}

// newApp builds the engine with sealed state. lookup is where the keys
// come from (os.LookupEnv in main, a map in tests), so the program never
// reads the environment behind its caller's back.
func newApp(dir string, lookup func(string) (string, bool)) (*app, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	keys, err := brick.KeysFromEnv(lookup, secretEnv)
	if err != nil {
		return nil, err
	}
	a := &app{revoked: map[string]time.Time{}}
	states, err := brick.NewSealedStates(brick.SealedOptions{
		Sealer:  brick.NewAESGCMSealer(keys, nil),
		TTL:     time.Hour,
		Revoked: a.isRevoked,
	})
	if err != nil {
		return nil, err
	}
	sc, err := scriptstarlark.New(scriptstarlark.Options{})
	if err != nil {
		return nil, err
	}
	a.eng, err = brick.New(brick.Options{
		FS:        root.FS(),
		Scripting: sc,
		States:    states,
		Sessions:  brick.SessionFunc(sessionOf),
	})
	if err != nil {
		return nil, err
	}
	return a, nil
}

// sessionOf answers "who is this request?" from the cookie; no cookie is
// no session, which binds nothing.
func sessionOf(r *http.Request) (string, error) {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return "", nil
	}
	return c.Value, nil
}

// hashSession is how brick names a session to a store and to
// SealedOptions.Revoked: the hex SHA-256 of the app's id.
func hashSession(id string) string {
	sum := sha256.Sum256([]byte(id))
	return hex.EncodeToString(sum[:])
}

// isRevoked is SealedOptions.Revoked: a blob issued before its session
// signed out no longer opens. A sealed store keeps nothing on the server,
// so this list is the only way to end a session's state early.
func (a *app) isRevoked(_ context.Context, session string, issued time.Time) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	at, ok := a.revoked[session]
	return ok && !issued.After(at)
}

// handler serves the page, the actions and sign-out.
func (a *app) handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle(brick.DefaultAssetBase+brick.ActionEndpoint, a.eng.ActionHandler())
	mux.Handle(brick.DefaultAssetBase, assetshttp.Handler(brick.DefaultAssetBase, func() assetshttp.Source { return a.eng }))
	mux.HandleFunc("POST /signout", func(w http.ResponseWriter, r *http.Request) {
		if id, _ := sessionOf(r); id != "" {
			a.mu.Lock()
			a.revoked[hashSession(id)] = time.Now()
			a.mu.Unlock()
		}
		http.SetCookie(w, &http.Cookie{Name: sessionCookie, MaxAge: -1, Path: "/"})
		http.Redirect(w, r, "/", http.StatusSeeOther)
	})
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		id, _ := sessionOf(r)
		if id == "" {
			id = rand.Text()
			http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: id, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode})
		}
		// An engine-only host names the render's session itself; the
		// action handler asks Options.Sessions on every action.
		html, err := a.eng.Render(r.Context(), page, nil, brick.WithSession(id))
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
const defaultAddr = ":8084"

// main serves the same handler the tests drive, so the behaviour pinned in
// main_test.go is what a browser gets here. Flags only choose where, never what.
func main() {
	addr := flag.String("addr", defaultAddr, "listen address")
	dir := flag.String("theme", "production-sealed-state/theme", "the project directory")
	flag.Parse()
	a, err := newApp(*dir, os.LookupEnv)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("serving on %s", *addr)
	log.Fatal(http.ListenAndServe(*addr, a.handler()))
}
