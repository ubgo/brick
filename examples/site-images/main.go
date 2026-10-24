// Command site-images shows the image built-in: {{ image }} makes resized
// copies of a file in static/, serves them under /_brick/img/ with a
// content hash in the name, and gives the <img> tag its URL, srcset, width
// and height. It is the example behind docs/guides/site/images.md.
//
//	go run .                    serve on :8096
//	go run . -export public     write the page and every resized copy
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/ubgo/brick"
	imagewebp "github.com/ubgo/brick/contrib/image-webp"
	"github.com/ubgo/brick/site"
)

// themeDir is the project folder, relative to this example's directory,
// which is where `go run .` and `go test .` both run.
const themeDir = "theme"

// newSite builds the site. JPEG and PNG encoders are built in; WebP is
// registered from contrib/image-webp, as any other format would be.
// ds:def id=newsite-xkpagvwq
func newSite(ctx context.Context, dir string) (*site.Site, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	return site.New(ctx, site.Options{
		FS:     root.FS(),
		Engine: brick.Options{ImageEncoders: []brick.ImageEncoder{imagewebp.New()}},
	})
}

// defaultAddr is this example's own port, so the examples of one guide can run
// side by side. Override it with -addr when the port is taken.
const defaultAddr = ":8096"

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
		fmt.Printf("wrote %d pages, %d static files, %d assets to %s\n", rep.Pages, rep.Static, rep.Assets, *export)
		return
	}
	log.Printf("serving on %s", *addr)
	log.Fatal(s.ListenAndServe(ctx, *addr))
}
