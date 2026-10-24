// Command site-subpath serves two sites on one server, each under its own
// path: the docs site at /docs/ and the shop at /shop/. Both are written
// as if they lived at the root; the site layer adds each BasePath to every
// root-absolute URL it serves or exports, and KeepPaths leaves a link to
// the other site alone. It is the example behind
// docs/guides/site/subpath-and-mount.md.
//
//	go run .                          both sites on :8095
//	go run . -export public           the docs site, built for /docs/
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/ubgo/brick/site"
)

// The two projects (relative to this example's directory) and the paths
// they are hosted under. Each base also names the other site's prefix in
// KeepPaths, because a link to a sibling site must not get this site's base.
const (
	docsDir  = "docs"
	shopDir  = "shop"
	docsBase = "/docs"
	shopBase = "/shop"
)

// newSite opens one project under base; keep lists sibling sites' URL
// prefixes that the base rewrite leaves alone.
// ds:def id=newsite-g7xs2dc3
func newSite(ctx context.Context, dir, base string, keep ...string) (*site.Site, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	return site.New(ctx, site.Options{FS: root.FS(), BasePath: base, KeepPaths: keep})
}

// sites builds both sites, each keeping the other's prefix.
// ds:def id=sites-89veq6pm
func sites(ctx context.Context) (docs, shop *site.Site, err error) {
	if docs, err = newSite(ctx, docsDir, docsBase, shopBase+"/"); err != nil {
		return nil, nil, err
	}
	if shop, err = newSite(ctx, shopDir, shopBase, docsBase+"/"); err != nil {
		return nil, nil, err
	}
	return docs, shop, nil
}

// handler mounts both sites on one http.Handler, for a host's own server.
// ds:def id=handler-zfwgxx9a
func handler(ctx context.Context) (http.Handler, error) {
	docs, shop, err := sites(ctx)
	if err != nil {
		return nil, err
	}
	return site.Mount(docs, shop)
}

// defaultAddr is this example's own port, so the examples of one guide can run
// side by side. Override it with -addr when the port is taken.
const defaultAddr = ":8095"

// main serves the same handler the tests drive, so the behaviour pinned in
// main_test.go is what a browser gets here. Flags only choose where, never what.
func main() {
	addr := flag.String("addr", defaultAddr, "listen address")
	export := flag.String("export", "", "write the docs site, built for /docs/, to this directory and exit")
	flag.Parse()
	ctx := context.Background()
	docs, shop, err := sites(ctx)
	if err != nil {
		log.Fatal(err)
	}
	if *export != "" {
		rep, err := docs.Export(ctx, *export)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("wrote %d pages, %d static files to %s\n", rep.Pages, rep.Static, *export)
		return
	}
	log.Printf("serving /docs/ and /shop/ on %s", *addr)
	log.Fatal(site.ListenAndServeAll(ctx, *addr, docs, shop))
}
