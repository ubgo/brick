// Command site-tailwind builds its stylesheet with the standalone Tailwind
// CLI before serving, and again whenever a watched file changes, through
// site.Options.OnChange. tailwind.Build runs the binary only when the set
// of classes in the project (or the input CSS) changed, so a text edit
// reloads instantly. It is the example behind docs/guides/site/tailwind.md.
//
//	go run .                    build static/app.css, then serve on :8097 with live reload
//	go run . -tailwind ./tw     use a binary that is not on PATH
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"strings"

	"github.com/ubgo/brick/site"
	"github.com/ubgo/brick/tailwind"
	"github.com/ubgo/brick/watch"
)

// themeDir is the project folder, relative to this example's directory,
// which is where `go run .` and `go test .` both run.
const themeDir = "theme"

// css says where the stylesheet comes from and goes, as brick.yaml does
// for the brick command; Binary empty means tailwindcss on PATH.
// ds:def id=css-du27ku5d
func css(dir, binary string) tailwind.Options {
	return tailwind.Options{Dir: dir, Input: "styles/app.css", Output: "static/app.css", Binary: binary}
}

// newSite builds the site in Dev mode with a rebuild hook: a change to
// markup or to the input CSS may change the class set, so it calls Build,
// which decides from the stamp whether the binary has to run.
// ds:def id=newsite-t9dxbc8c
func newSite(ctx context.Context, dir string, tw tailwind.Options) (*site.Site, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	return site.New(ctx, site.Options{
		FS:  root.FS(),
		Dev: true,
		OnChange: func(ctx context.Context, changes []watch.Change) error {
			for _, c := range changes {
				if strings.HasSuffix(c.Path, ".html") || strings.HasSuffix(c.Path, ".md") || c.Path == tw.Input {
					_, err := tailwind.Build(ctx, tw)
					return err
				}
			}
			return nil
		},
	})
}

// defaultAddr is this example's own port, so the examples of one guide can run
// side by side. Override it with -addr when the port is taken.
const defaultAddr = ":8097"

// main serves the same handler the tests drive, so the behaviour pinned in
// main_test.go is what a browser gets here. Flags only choose where, never what.
func main() {
	addr := flag.String("addr", defaultAddr, "listen address")
	binary := flag.String("tailwind", "", "the tailwindcss binary (default: tailwindcss on PATH)")
	flag.Parse()
	ctx := context.Background()
	tw := css(themeDir, *binary)
	// Without the binary this stops with tailwind.ErrNoBinary, which says
	// where to get it.
	ran, err := tailwind.Build(ctx, tw)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("tailwind: rebuilt=%v %s", ran, tw.Output)
	s, err := newSite(ctx, themeDir, tw)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("serving on %s", *addr)
	log.Fatal(s.ListenAndServe(ctx, *addr))
}
