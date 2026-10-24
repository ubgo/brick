// Command components-loaders is a small net/http host whose components
// fetch their own data: load: and load_many: loaders in Starlark, a .star
// module that load()s another module and a data file, an inline
// <brick:script> function, a host global (scriptstarlark.Options.Globals),
// and a page loader that answers with a redirect, an error status or a
// header, which the host turns into the HTTP response.
//
//	go run . -addr :8090                 serve, then open /, /team, /posts/hello
//	go run . / /posts/old-hello          print what the handler answers for each path
//	go test .                            check the exact responses
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"

	"github.com/ubgo/brick"
	scriptstarlark "github.com/ubgo/brick/contrib/script-starlark"
	"go.starlark.net/starlark"
)

// newEngine builds the engine with Starlark scripting. Scripts get no
// database and no network here; the one capability the host adds is the
// greeting global, which is how a Go program hands scripts anything.
// ds:def id=newengine-ua88b6ec
func newEngine(dir string) (*brick.Engine, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	sc, err := scriptstarlark.New(scriptstarlark.Options{
		Globals: starlark.StringDict{"greeting": starlark.String("Written by the team")},
	})
	if err != nil {
		return nil, err
	}
	return brick.New(brick.Options{FS: root.FS(), Scripting: sc})
}

// handler routes three pages. The post page gets its slug through
// WithRoute, which its loader reads as input["page"]["route"]["slug"].
func handler(eng *brick.Engine) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		serve(eng, w, r, "content/index.html")
	})
	mux.HandleFunc("GET /team", func(w http.ResponseWriter, r *http.Request) {
		serve(eng, w, r, "content/team.html")
	})
	mux.HandleFunc("GET /posts/{slug}", func(w http.ResponseWriter, r *http.Request) {
		serve(eng, w, r, "content/post.html", brick.WithRoute(map[string]string{"slug": r.PathValue("slug")}, nil))
	})
	return mux
}

// serve renders a page and turns a loader's answer into the response: a
// *brick.Redirect becomes a Location (checked by RedirectLocation, so loader
// data can never make an open redirect), a *brick.HTTPError its status and
// message, ErrNotFound a 404, and the loader's brick.header values arrive
// in Result.Headers. Any other error is a fault, logged and answered 500.
// ds:def id=serve-d7nbxtw8
func serve(eng *brick.Engine, w http.ResponseWriter, r *http.Request, page string, opts ...brick.RenderOption) {
	res, err := eng.RenderChunks(r.Context(), page, nil, opts...)
	var rd *brick.Redirect
	var he *brick.HTTPError
	switch {
	case errors.As(err, &rd):
		loc, lerr := eng.RedirectLocation(r, rd)
		if lerr != nil {
			log.Printf("refused redirect: %v", lerr)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, loc, rd.Status)
		return
	case errors.As(err, &he):
		http.Error(w, he.Message, he.Status)
		return
	case errors.Is(err, brick.ErrNotFound):
		http.NotFound(w, r)
		return
	case err != nil:
		log.Printf("render %s: %v", page, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	html, err := res.Bytes()
	if err != nil {
		log.Printf("render %s: %v", page, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	for k, v := range res.Headers {
		w.Header()[k] = v
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(html)
}

// shownHeaders are the response headers printed for each path: the ones
// a loader's answer sets. Others (Content-Type, Content-Length) are noise
// for this example.
var shownHeaders = []string{"Location", "Cache-Control"}

// fetch runs one GET through the handler in-process and describes the
// response: status line, the shown headers, then the body.
func fetch(h http.Handler, path string) string {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	var b strings.Builder
	fmt.Fprintf(&b, "GET %s -> %d %s\n", path, rec.Code, http.StatusText(rec.Code))
	for _, k := range shownHeaders {
		if v := rec.Header().Get(k); v != "" {
			fmt.Fprintf(&b, "%s: %s\n", k, v)
		}
	}
	b.WriteString(strings.TrimRight(rec.Body.String(), "\n"))
	return b.String()
}

// main prints each requested path's response, or serves the same handler over
// HTTP when -addr is given. The tests call run and handler directly, so the
// output they pin is this program's real output.
func main() {
	addr := flag.String("addr", "", "serve on this address instead of printing responses")
	dir := flag.String("theme", "theme", "the project directory")
	flag.Parse()
	eng, err := newEngine(*dir)
	if err != nil {
		log.Fatal(err)
	}
	if *addr != "" {
		log.Printf("serving on %s", *addr)
		log.Fatal(http.ListenAndServe(*addr, handler(eng)))
	}
	paths := flag.Args()
	if len(paths) == 0 {
		paths = []string{"/"}
	}
	if err := run(os.Stdout, eng, paths); err != nil {
		log.Fatal(err)
	}
}

// run prints the handler's answer for each path into w: what
// `go run . <paths>` prints and testdata/output.txt holds.
func run(w io.Writer, eng *brick.Engine, paths []string) error {
	h := handler(eng)
	for _, p := range paths {
		if _, err := fmt.Fprintf(w, "%s\n\n", fetch(h, p)); err != nil {
			return err
		}
	}
	return nil
}
