// Command site-protected shows pages that depend on who is asking: an
// island whose loader sends a signed-out visitor to /login/ and refuses a
// non-admin with 403 (rendered by content/error.html), and a whole page
// marked brick: {per_request: true}. The visitor comes from a "user"
// cookie, standing in for your own session. It is the example behind
// docs/guides/site/protected-pages.md and not-found-and-error-pages.md.
//
//	go run .                    serve on :8092
//	curl -i localhost:8092/admin/
//	curl -i --cookie user=admin localhost:8092/admin/
package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"

	"github.com/ubgo/brick"
	scriptstarlark "github.com/ubgo/brick/contrib/script-starlark"
	"github.com/ubgo/brick/site"
)

// themeDir is the project folder, relative to this example's directory,
// which is where `go run .` and `go test .` both run.
const themeDir = "theme"

// userCookie names the cookie this demo trusts as the signed-in user. A
// real program reads its own session here instead.
const userCookie = "user"

// visitor gives every island and per-request page the request's user,
// as .Ctx.user. It runs for each response; the cached page never sees it.
// ds:def id=visitor-xbkx7jr2
func visitor(r *http.Request) []brick.RenderOption {
	if c, err := r.Cookie(userCookie); err == nil {
		return []brick.RenderOption{brick.Provide("user", c.Value)}
	}
	return nil
}

// newSite builds the site; the guard and the per-request page read the
// visitor that Request provides.
// ds:def id=newsite-c9ajaymd
func newSite(ctx context.Context, dir string) (*site.Site, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	sc, err := scriptstarlark.New(scriptstarlark.Options{})
	if err != nil {
		return nil, err
	}
	return site.New(ctx, site.Options{
		FS:      root.FS(),
		Engine:  brick.Options{Scripting: sc},
		Request: visitor,
	})
}

// defaultAddr is this example's own port, so the examples of one guide can run
// side by side. Override it with -addr when the port is taken.
const defaultAddr = ":8092"

// main serves the same handler the tests drive, so the behaviour pinned in
// main_test.go is what a browser gets here. Flags only choose where, never what.
func main() {
	addr := flag.String("addr", defaultAddr, "listen address")
	flag.Parse()
	ctx := context.Background()
	s, err := newSite(ctx, themeDir)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("serving on %s", *addr)
	log.Fatal(s.ListenAndServe(ctx, *addr))
}
