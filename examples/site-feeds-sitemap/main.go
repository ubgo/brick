// Command site-feeds-sitemap shows output pages and the built-in sitemap:
// content/feed.xml.html is served at /feed.xml as application/xml,
// content/robots.txt.html at /robots.txt, content/tags/[tag]/feed.xml.html
// writes one feed per tag, and site.Options.Sitemap adds /sitemap.xml. It
// is the example behind docs/guides/site/feeds-and-sitemap.md.
//
//	go run .                    serve on :8093
//	go run . -export public     write the pages, feeds and sitemap.xml
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

// newSite builds the site with the built-in sitemap on. An empty
// Sitemap.BaseURL takes site.yaml's baseURL; the sitemap needs an absolute
// base because search engines want absolute locations.
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
		Sitemap: &site.Sitemap{},
	})
}

// defaultAddr is this example's own port, so the examples of one guide can run
// side by side. Override it with -addr when the port is taken.
const defaultAddr = ":8093"

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
		fmt.Printf("wrote %d pages to %s (sitemap: %v)\n", rep.Pages, *export, rep.Sitemap)
		return
	}
	log.Printf("serving on %s", *addr)
	log.Fatal(s.ListenAndServe(ctx, *addr))
}
