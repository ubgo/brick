// Command components-props renders three pages that pass values to
// components: typed props with defaults and oneof, an object prop with a
// declared shape, the :post shorthand, undeclared attributes placed with
// {{ attrs }}, brick:spread, and a host's own template function and prop
// type (Options.Funcs, Options.PropTypes).
//
//	go run .          prints every page
//	go test .         checks the exact output
package main

import (
	"context"
	"flag"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/mail"
	"os"
	"strings"

	"github.com/ubgo/brick"
)

// pages are the files this program renders, in the order it prints them.
var pages = []string{"content/index.html", "content/attrs.html", "content/contact.html"}

// centsPerDollar converts the int prices the theme passes into dollars.
const centsPerDollar int64 = 100

// money is a host template function: a theme calls {{ money .Props.price }}
// without any script, and the same function serves every render, so it
// must be pure and safe to call at once. It takes int64 because a prop
// declared `int` arrives as int64; a func(int) fails at render with
// "wrong type for value; expected int; got int64".
// ds:def id=money-k25fr6em
func money(cents int64) string {
	return fmt.Sprintf("$%d.%02d", cents/centsPerDollar, cents%centsPerDollar)
}

// email is a host prop type: `to: email` in a props: block runs it on the
// call-site value, so a bad address is a prop error naming the value and a
// good one arrives lowercased.
// ds:def id=email-kataxkeu
func email(v any) (any, error) {
	s, _ := v.(string) // a non-string becomes "", which ParseAddress rejects below
	if _, err := mail.ParseAddress(s); err != nil {
		return nil, fmt.Errorf("%q is not an email address", s)
	}
	return strings.ToLower(s), nil
}

// newEngine builds an engine over the theme folder. os.OpenRoot confines
// every ~/ path to that folder.
// ds:def id=newengine-cmy9re2s
func newEngine(dir string) (*brick.Engine, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	return brick.New(brick.Options{
		FS:        root.FS(),
		Funcs:     template.FuncMap{"money": money},
		PropTypes: map[string]brick.PropType{"email": email},
	})
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
