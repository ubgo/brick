// Command site-dynamic-routes shows pages whose file names hold [param]
// segments: content/posts/[slug].html answers /posts/hello/ for every slug
// its brick: {paths:} script lists, a loader fills the page from the slug,
// a static page beats the pattern, and content/docs/[...path].html takes
// any number of segments. It is the example behind
// docs/guides/site/dynamic-routes.md.
//
//	go run .                    serve on :8091
//	go run . -export public     write every listed URL to ./public
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/ubgo/brick"
	scriptstarlark "github.com/ubgo/brick/contrib/script-starlark"
	"github.com/ubgo/brick/site"
)

// themeDir is the project folder, relative to this example's directory,
// which is where `go run .` and `go test .` both run.
const themeDir = "theme"

// newSite builds the site. The paths: list and the loader are Starlark,
// so the engine needs the Starlark backend.
// ds:def id=newsite-t8jsqrzn
func newSite(ctx context.Context, dir string) (*site.Site, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	sc, err := scriptstarlark.New(scriptstarlark.Options{})
	if err != nil {
		return nil, err
	}
	return site.New(ctx, site.Options{FS: root.FS(), Engine: brick.Options{Scripting: sc}})
}

// defaultAddr is this example's own port, so the examples of one guide can run
// side by side. Override it with -addr when the port is taken.
const defaultAddr = ":8091"

// main serves the same handler the tests drive, so the behaviour pinned in
// main_test.go is what a browser gets here. Flags only choose where, never what.
func main() {
	addr := flag.String("addr", defaultAddr, "listen address")
	export := flag.String("export", "", "write the static site to this directory and exit")
	flag.Parse()
	ctx := context.Background()
	s, err := newSite(ctx, themeDir)
	if err != nil {
		log.Fatal(err)
	}
	if *export != "" {
		rep, err := s.Export(ctx, *export)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("wrote %d pages to %s\n", rep.Pages, *export)
		return
	}
	log.Printf("serving on %s", *addr)
	log.Fatal(s.ListenAndServe(ctx, *addr))
}
