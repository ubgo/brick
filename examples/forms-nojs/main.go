// Command forms-nojs serves a signup form that works with JavaScript
// switched off, from the host's own router rather than the site layer:
// the GET route renders the page (resuming the instance a form post left
// in its cookie), and the POST route hands a brick:submit post to
// Engine.FormPost and answers 303, or 422 with the errors on the page.
//
//	go run ./forms-nojs -addr :8085      then open /signup with scripts off
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

// page is the file the /signup route renders.
// ds:def id=page-cspbmra2
const page = "content/signup.html"

// newApp builds an engine with in-process state; form posts are on by
// default (Options.NoFormPosts turns them off).
// ds:def id=newapp-br885xh3
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

// handler is the host's router: the JSON action endpoint for visitors
// with scripts on, the assets, and one page that also answers its own
// form posts.
// ds:def id=handler-p42hdhjm
func handler(eng *brick.Engine) http.Handler {
	mux := http.NewServeMux()
	mux.Handle(brick.DefaultAssetBase+brick.ActionEndpoint, eng.ActionHandler())
	mux.Handle(brick.DefaultAssetBase, assetshttp.Handler(brick.DefaultAssetBase, func() assetshttp.Source { return eng }))
	mux.HandleFunc("GET /signup", func(w http.ResponseWriter, r *http.Request) {
		// FormResumeOption reads and clears the cookie a successful post
		// left, so the page shows the saved instance once.
		write(w, eng, r, http.StatusOK, eng.FormResumeOption(w, r))
	})
	mux.HandleFunc("POST /signup", func(w http.ResponseWriter, r *http.Request) {
		if !eng.IsFormPost(r) {
			http.Error(w, "not a form post", http.StatusBadRequest)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		// Engine.FormPost runs the action the hidden fields name, as the
		// JSON endpoint would, and tells us where to send the visitor.
		out, err := eng.FormPost(r)
		if err != nil {
			// A cross-site post, a bad token, an unknown action: the
			// action endpoint's status and plain message.
			eng.FormError(w, r, err)
			return
		}
		if !out.Failed && (out.Resume.Handle == "" || eng.SetFormResume(w, r, out.Resume)) {
			// Success (or the instance was gone): POST-redirect-GET.
			http.Redirect(w, r, out.Location, http.StatusSeeOther)
			return
		}
		status := http.StatusOK // the state was too big for a cookie
		if out.Failed {
			status = http.StatusUnprocessableEntity
		}
		write(w, eng, r, status, brick.WithFormResume(out.Resume))
	})
	return mux
}

// write renders the page with opts and answers with status.
// ds:def id=write-p34ekjrs
func write(w http.ResponseWriter, eng *brick.Engine, r *http.Request, status int, opts ...brick.RenderOption) {
	html, err := eng.Render(r.Context(), page, nil, opts...)
	if err != nil {
		http.Error(w, "render failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(html)
}

// defaultAddr is this example's own port, so the examples of one guide can run
// side by side. Override it with -addr when the port is taken.
const defaultAddr = ":8085"

// main serves the same handler the tests drive, so the behaviour pinned in
// main_test.go is what a browser gets here. Flags only choose where, never what.
func main() {
	addr := flag.String("addr", defaultAddr, "listen address")
	dir := flag.String("theme", "forms-nojs/theme", "the project directory")
	flag.Parse()
	eng, err := newApp(*dir)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("serving on %s", *addr)
	log.Fatal(http.ListenAndServe(*addr, handler(eng)))
}
