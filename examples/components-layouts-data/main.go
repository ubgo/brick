// Command components-layouts-data renders pages that get their values
// from outside the component: a layout chain, .Ctx through provide: and
// inject:, site config and data files (.Site, LoadSite), a data file
// imported with use: (.Data), and translations with a language variant
// (t, LoadMessages, WithLocale, about.fr.html).
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

// render is one render this program does: a page, in a language, with
// optional extra render options.
type render struct {
	name   string // the heading printed above the output
	path   string
	locale string
	opts   []brick.RenderOption
}

// renders are the renders this program prints, in order.
var renders = []render{
	{name: "content/index.html", path: "content/index.html", locale: "en"},
	{name: "content/index.html as a fragment", path: "content/index.html", locale: "en", opts: []brick.RenderOption{brick.WithLayout("none")}},
	{name: "content/blog/hello.html", path: "content/blog/hello.html", locale: "en"},
	{name: "content/plans.html", path: "content/plans.html", locale: "en"},
	{name: "content/cart.html (en)", path: "content/cart.html", locale: "en"},
	{name: "content/cart.html (fr)", path: "content/cart.html", locale: "fr"},
	{name: "content/about.html (en)", path: "content/about.html", locale: "en"},
	{name: "content/about.html (fr)", path: "content/about.html", locale: "fr"},
}

// siteEnv is the only environment site.yaml may read: ${BASE_URL} is
// filled from it, and nothing else in the process environment reaches a
// template. A real host passes os.LookupEnv.
var siteEnv = map[string]string{"BASE_URL": "https://example.com"}

// ds:def id=lookup-ycm8wjqj
func lookup(name string) (string, bool) {
	v, ok := siteEnv[name]
	return v, ok
}

// defaultLocale is the language a key falls back to when the requested
// one has no message for it.
const defaultLocale = "en"

// newEngine loads the site config, data files and messages explicitly,
// then builds the engine over the theme folder: nothing is discovered.
// ds:def id=newengine-7h38t45k
func newEngine(dir string) (*brick.Engine, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	fsys := root.FS()
	site, err := brick.LoadSite(fsys, "site.yaml", "data/", brick.WithEnv(lookup))
	if err != nil {
		return nil, err
	}
	msgs, err := brick.LoadMessages(fsys, "i18n/")
	if err != nil {
		return nil, err
	}
	return brick.New(brick.Options{FS: fsys, Site: site, Messages: msgs, DefaultLocale: defaultLocale})
}

// renderAll runs every render and returns its HTML, keyed by name.
func renderAll(ctx context.Context, eng *brick.Engine) (map[string]string, error) {
	out := make(map[string]string, len(renders))
	for _, r := range renders {
		opts := append([]brick.RenderOption{brick.WithLocale(r.locale)}, r.opts...)
		html, err := eng.Render(ctx, r.path, nil, opts...)
		if err != nil {
			return nil, err
		}
		out[r.name] = string(html)
	}
	return out, nil
}

// run does every render into w, each under a "== name" heading: what
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
	for _, r := range renders {
		if _, err := fmt.Fprintf(w, "== %s\n%s\n", r.name, out[r.name]); err != nil {
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
