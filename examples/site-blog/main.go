// Command site-blog is a small blog built with the site package: pages at
// URLs from their file paths, a blog index from .Site.Pages, a markdown
// post with a table of contents, a collection schema, a draft, a 404 page,
// and a static export. It is the example behind docs/guides/site/.
//
//	go run .                        serve on :8090
//	go run . -dev -drafts           with live reload, the toolbar and drafts
//	go run . -watch                 your own http.Server plus site.Watch, no Dev
//	go run . -export public         write the static site to ./public
package main

import (
	"context"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"

	"github.com/ubgo/brick"
	markdowngoldmark "github.com/ubgo/brick/contrib/markdown-goldmark"
	"github.com/ubgo/brick/site"
)

// themeDir is the project folder, relative to this example's directory,
// which is where `go run .` and `go test .` both run.
const themeDir = "theme"

// newSite builds the site from dir. Markdown needs a backend; goldmark
// with heading ids lets the doc layout link its table of contents.
// ds:def id=newsite-8vbpnjfz
func newSite(ctx context.Context, dir string, dev, drafts bool) (*site.Site, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	return newSiteFS(ctx, root.FS(), dev, drafts)
}

// newSiteFS is newSite over any file system, so a test can add a file.
// ds:def id=newsitefs-5nq2ppfu
func newSiteFS(ctx context.Context, fsys fs.FS, dev, drafts bool) (*site.Site, error) {
	return site.New(ctx, site.Options{
		FS:     fsys,
		Engine: brick.Options{Markdown: markdowngoldmark.New(markdowngoldmark.WithHeadingIDs())},
		Dev:    dev,
		Drafts: drafts,
	})
}

// defaultAddr is this example's own port, so the examples of one guide can run
// side by side. Override it with -addr when the port is taken.
const defaultAddr = ":8090"

// main serves the same handler the tests drive, so the behaviour pinned in
// main_test.go is what a browser gets here. Flags only choose where, never what.
func main() {
	addr := flag.String("addr", defaultAddr, "listen address")
	dev := flag.Bool("dev", false, "live reload, playground and the dev toolbar")
	drafts := flag.Bool("drafts", false, "include pages with draft: true")
	poll := flag.Bool("watch", false, "serve s.Handler() on its own http.Server and poll the files, without Dev")
	export := flag.String("export", "", "write the static site to this directory and exit")
	flag.Parse()
	ctx := context.Background()
	s, err := newSite(ctx, themeDir, *dev, *drafts)
	if err != nil {
		log.Fatal(err)
	}
	for _, d := range s.Diagnostics() {
		log.Printf("%s: %s", d.File, d.Message)
	}
	if *export != "" {
		rep, err := s.Export(ctx, *export)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("wrote %d pages, %d static files to %s\n", rep.Pages, rep.Static, *export)
		return
	}
	if *poll {
		go func() { log.Printf("watch: %v", watchSite(ctx, s, pollEvery)) }()
		log.Printf("serving on %s, watching files", *addr)
		log.Fatal(http.ListenAndServe(*addr, s.Handler()))
	}
	log.Printf("serving on %s", *addr)
	log.Fatal(s.ListenAndServe(ctx, *addr))
}
