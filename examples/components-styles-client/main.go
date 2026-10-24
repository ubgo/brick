// Command components-styles-client renders a page whose components carry
// their own CSS and browser JavaScript: scoped and inclusive <brick:style>
// blocks, a sibling stylesheet (Badge.css), a host StyleTransform, and
// <brick:client> modules mounted with brick.mount, one importing another
// through "brick:Modal". {{ assets }} links each file once, with SRI and
// the request's nonce.
//
//	go run .          prints the page, then every asset file it links
//	go test .         checks the exact output
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path"
	"strings"

	"github.com/ubgo/brick"
)

// page is the one page this program renders.
const page = "content/index.html"

// nonce is the CSP nonce this example renders with. A real host makes a
// fresh random one per response and sends it in its
// Content-Security-Policy header; a fixed one keeps the output stable.
const nonce = "r4nd0m"

// minify is the host's StyleTransform: it collapses each block's
// whitespace. It runs when a file compiles and its output is hashed into
// the asset URL, so it must be deterministic; returning b.CSS unchanged is
// the identity transform.
// ds:def id=minify-kfznkq8x
func minify(b brick.StyleBlock) ([]byte, error) {
	return []byte(strings.Join(strings.Fields(string(b.CSS)), " ")), nil
}

// newEngine builds the engine over the theme folder.
// ds:def id=newengine-8kneqnh6
func newEngine(dir string) (*brick.Engine, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	return brick.New(brick.Options{FS: root.FS(), StyleTransform: minify})
}

// renderPage renders the page with the nonce.
func renderPage(ctx context.Context, eng *brick.Engine) (string, error) {
	html, err := eng.Render(ctx, page, nil, brick.WithNonce(nonce))
	return string(html), err
}

// assetFiles returns every file the engine serves under its asset base
// (component CSS and JS, and the runtime), path → content. A host serves
// eng.Assets() with contrib/assets-http; this program only lists it.
func assetFiles(eng *brick.Engine) (map[string]string, []string, error) {
	files := map[string]string{}
	var names []string
	err := fs.WalkDir(eng.Assets(), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := fs.ReadFile(eng.Assets(), p)
		if err != nil {
			return err
		}
		files[p] = string(b)
		names = append(names, p)
		return nil
	})
	return files, names, err
}

// run prints the page and then every asset file it links into w: what
// `go run .` prints and testdata/output.txt holds. The runtime is named
// but not printed; it is the same file for every page.
func run(ctx context.Context, w io.Writer, dir string) error {
	eng, err := newEngine(dir)
	if err != nil {
		return err
	}
	html, err := renderPage(ctx, eng)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "== %s\n%s\n", page, html); err != nil {
		return err
	}
	files, names, err := assetFiles(eng)
	if err != nil {
		return err
	}
	for _, n := range names {
		if strings.HasPrefix(path.Base(n), "runtime.") {
			_, err = fmt.Fprintf(w, "== %s (the runtime, not shown)\n\n", n)
		} else {
			_, err = fmt.Fprintf(w, "== %s\n%s\n\n", n, files[n])
		}
		if err != nil {
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
