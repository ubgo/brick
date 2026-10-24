// Command interactivity-navigation serves a three-page site with
// navigation: true, through the site layer: links fetch the next page and
// patch only what changed, brick:current marks the active link,
// brick:prefetch loads a page before the click, brick:scroll and
// brick:persist keep a sidebar's scroll and an audio player across pages,
// and brick:nav="off" leaves a download as a full load. Because every page
// shares one layout, the server answers a navigation with only the page
// (fragment navigation); the test shows both answers.
//
//	cd examples/interactivity-navigation && go run .      then open http://localhost:8094/
package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"

	"github.com/ubgo/brick"
	"github.com/ubgo/brick/site"
)

// configFile is the project file that turns navigation on. The brick CLI
// reads it itself; a Go host loads and applies it, or sets
// brick.Options.Navigation directly.
const configFile = "brick.yaml"

// newSite builds the site from dir with the engine settings brick.yaml
// asks for.
// ds:def id=newsite-2kuuspzw
func newSite(ctx context.Context, dir string) (*site.Site, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	cfg, err := brick.LoadConfig(root.FS(), configFile)
	if err != nil {
		return nil, err
	}
	var eng brick.Options
	if err := cfg.Apply(&eng); err != nil {
		return nil, err
	}
	return site.New(ctx, site.Options{FS: root.FS(), Engine: eng})
}

// defaultAddr is this example's own port, so the examples of one guide can run
// side by side. Override it with -addr when the port is taken.
const defaultAddr = ":8094"

// main serves the same handler the tests drive, so the behaviour pinned in
// main_test.go is what a browser gets here. Flags only choose where, never what.
func main() {
	addr := flag.String("addr", defaultAddr, "listen address")
	dir := flag.String("theme", "theme", "the project directory")
	flag.Parse()
	s, err := newSite(context.Background(), *dir)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("serving on %s", *addr)
	log.Fatal(http.ListenAndServe(*addr, s.Handler()))
}
