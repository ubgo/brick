// Command site-i18n serves one site in three languages: the first locale
// has no URL prefix (/about/), the others do (/fr/about/), a file such as
// about.fr.html replaces the shared page in its language, messages come
// from i18n/<locale>.yaml through t, and every page links its other
// languages through .Page.Translations. It is the example behind
// docs/guides/site/languages.md.
//
//	go run .                    serve on :8094
package main

import (
	"context"
	"flag"
	"log"
	"os"

	"github.com/ubgo/brick/site"
)

// themeDir is the project folder, relative to this example's directory,
// which is where `go run .` and `go test .` both run.
const themeDir = "theme"

// locales are the languages with their own URLs, default first. The
// default language is served without a prefix; the order is also the order
// of .Page.Translations and of the sitemap's hreflang links.
// ds:def id=locales-cwhnbuv4
var locales = []string{"en", "fr", "de"}

// newSite builds the site with every locale and the sitemap, whose
// hreflang alternates are the point of listing translations.
// ds:def id=newsite-w885xjxc
func newSite(ctx context.Context, dir string) (*site.Site, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	return site.New(ctx, site.Options{FS: root.FS(), Locales: locales, Sitemap: &site.Sitemap{}})
}

// defaultAddr is this example's own port, so the examples of one guide can run
// side by side. Override it with -addr when the port is taken.
const defaultAddr = ":8094"

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
