// Command forms-method-csrf serves two posts with brick:method="delete"
// links, for an app that guards its write routes with a CSRF token. The
// token reaches the page in a meta tag; the component's client code adds
// it to every method link's hidden form with brick.methodFormWith; the
// DELETE route refuses a request without it.
//
//	go run ./forms-method-csrf -addr :8088      then open /
package main

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/ubgo/brick"
	assetshttp "github.com/ubgo/brick/contrib/assets-http"
)

const (
	// page is the one page this app serves.
	page = "content/index.html"
	// csrfField is the form field the client hook adds; it must match the
	// name in PostRow.html.
	csrfField = "_csrf"
	// tokenBytes is the size of the random token: 128 bits, so it cannot be
	// guessed.
	tokenBytes = 16
)

// app is the engine and the one token this demo process hands out. A real
// app ties the token to the visitor's session; one value keeps the example
// to the part that is about brick.
type app struct {
	eng   *brick.Engine
	token string
}

// newApp builds the engine over dir and draws a token.
func newApp(dir string) (*app, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	eng, err := brick.New(brick.Options{FS: root.FS()})
	if err != nil {
		return nil, err
	}
	b := make([]byte, tokenBytes)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	return &app{eng: eng, token: hex.EncodeToString(b)}, nil
}

// handler is the host's router. MethodOverride turns the hidden form's
// POST back into DELETE and leaves its fields in r.PostForm, which is
// where the route reads the token the browser hook added.
func (a *app) handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle(brick.DefaultAssetBase, assetshttp.Handler(brick.DefaultAssetBase, func() assetshttp.Source { return a.eng }))
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		html, err := a.eng.Render(r.Context(), page, map[string]any{"csrf": a.token})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(html)
	})
	mux.HandleFunc("DELETE /posts/{id}", func(w http.ResponseWriter, r *http.Request) {
		if subtle.ConstantTimeCompare([]byte(r.PostFormValue(csrfField)), []byte(a.token)) != 1 {
			http.Error(w, "csrf token missing or wrong", http.StatusForbidden)
			return
		}
		fmt.Fprintf(w, "deleted post %s", r.PathValue("id"))
	})
	return brick.MethodOverride(mux)
}

// defaultAddr is this example's own port, so the examples of one guide can run
// side by side. Override it with -addr when the port is taken.
const defaultAddr = ":8088"

// main serves the same handler the tests drive, so the behaviour pinned in
// main_test.go is what a browser gets here. Flags only choose where, never what.
func main() {
	addr := flag.String("addr", defaultAddr, "listen address")
	dir := flag.String("theme", "forms-method-csrf/theme", "the project directory")
	flag.Parse()
	a, err := newApp(*dir)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("serving on %s", *addr)
	log.Fatal(http.ListenAndServe(*addr, a.handler()))
}
