// Command components-slots renders pages that pass markup into
// components: {{ children }}, named blocks (<brick:prop>, node, hasNode),
// a scoped slot (.Slot), declared slots, child tags read as data
// (.Children, child) and raw bodies (raw: true, src=).
//
//	go run .          prints every page
//	go test .         checks the exact output
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/ubgo/brick"
)

// pages are the files this program renders, in the order it prints them.
var pages = []string{"content/index.html", "content/tabs.html", "content/code.html"}

// newEngine builds an engine over the theme folder. os.OpenRoot confines
// every ~/ path, src= included, to that folder.
func newEngine(dir string) (*brick.Engine, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	return brick.New(brick.Options{FS: root.FS()})
}

// renderAll renders every page and returns its HTML, keyed by path.
func renderAll(ctx context.Context, eng *brick.Engine) (map[string]string, error) {
	out := make(map[string]string, len(pages))
	for _, p := range pages {
		html, err := eng.Render(ctx, p, nil)
		if err != nil {
			return nil, err
		}
		out[p] = string(html)
	}
	return out, nil
}

// run renders every page into w, each under a "== path" heading: what
// `go run .` prints and testdata/output.txt holds.
func run(ctx context.Context, w io.Writer, dir string) error {
	eng, err := newEngine(dir)
	if err != nil {
		return err
	}
	out, err := renderAll(ctx, eng)
	if err != nil {
		return err
	}
	for _, p := range pages {
		if _, err := fmt.Fprintf(w, "== %s\n%s\n", p, out[p]); err != nil {
			return err
		}
	}
	return nil
}

// main prints what run writes. The tests call run with a buffer instead of
// stdout, so the output they pin is this program's real output.
func main() {
	dir := flag.String("theme", "theme", "the project directory")
	flag.Parse()
	if err := run(context.Background(), os.Stdout, *dir); err != nil {
		log.Fatal(err)
	}
}
