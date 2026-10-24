package brick

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
)

// A component's sibling Card.css ships like an inline <brick:style>: global
// by default, first in the one content-hashed sheet (so an inline rule wins
// a tie), and recorded in Result.Files so live reload sees an edit.
func TestSiblingCSSGlobal(t *testing.T) {
	t.Parallel()
	e := newEngine(t, Options{FS: theme(
		"components/Card.html", "<brick:style>\n.t { color: blue }\n</brick:style>\n<div><p class=\"t\">x</p></div>",
		"components/Card.css", ".t { color: red }\n",
		"content/a.html", `{{ assets }}<Card />`,
	)})
	res, err := e.RenderChunks(context.Background(), "content/a.html", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := cssOf(t, e, "Card"); got != ".t { color: red }\n.t { color: blue }\n" {
		t.Fatalf("css = %q", got)
	}
	if !slices.Contains(res.Files, "components/Card.css") {
		t.Fatalf("Files = %v, want components/Card.css", res.Files)
	}
	page, err := res.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(page), scopeAttr) {
		t.Fatalf("a global sheet marked the root: %s", page)
	}
}

// brick: {css: scoped|inclusive} wraps the file like <brick:style scoped>,
// and the root carries the marker even with no inline block.
func TestSiblingCSSScoped(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ mode, limit string }{
		{cssScoped, `to ([data-brick-s])`},
		{cssInclusive, `to ([data-brick-s] > *)`},
	} {
		e := newEngine(t, Options{FS: theme(
			"components/Card.html", "---\nbrick:\n  css: "+c.mode+"\n---\n<div><p class=\"t\">x</p></div>",
			"components/Card.css", ".t { color: red }",
			"content/a.html", `{{ assets }}<Card />`,
		)})
		page := render(t, e, "content/a.html", nil)
		_, rest, ok := strings.Cut(page, scopeAttr+`="`)
		if !ok {
			t.Fatalf("%s: root not marked: %s", c.mode, page)
		}
		hash, _, _ := strings.Cut(rest, `"`)
		want := "@scope ([data-brick-s=\"" + hash + "\"]) " + c.limit + " {\n.t { color: red }\n}\n"
		if got := cssOf(t, e, "Card"); got != want {
			t.Fatalf("%s:\n got %q\nwant %q", c.mode, got, want)
		}
	}
}

// Language variants share the sibling sheet; a page never picks one up
// (a static style.css beside a page is the site's, not a component's).
func TestSiblingCSSVariantsAndPages(t *testing.T) {
	t.Parallel()
	e := newEngine(t, Options{FS: theme(
		"components/Card.html", "<div>en</div>",
		"components/Card.fr.html", "<div>fr</div>",
		"components/Card.css", ".c{}",
		"content/a.html", `{{ assets }}<Card />`,
		"content/a.css", ".page{}",
	)})
	page := render(t, e, "content/a.html", nil, WithLocale("fr"))
	if !strings.Contains(page, "fr") || cssOf(t, e, "Card") != ".c{}\n" {
		t.Fatalf("fr variant: %s / %q", page, cssOf(t, e, "Card"))
	}
	if _, err := e.RenderChunks(context.Background(), "content/a.html", nil); err != nil {
		t.Fatal(err)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	for name := range e.assetFiles {
		if strings.HasPrefix(name, assetDir+"/page.") {
			t.Fatalf("a page picked up a sibling stylesheet: %s", name)
		}
	}
}

// A bad css: value fails the file; css: on a page is an error; css:
// set with no file is a warning, not an error (nothing required).
func TestSiblingCSSErrors(t *testing.T) {
	t.Parallel()
	_, err := newEngineErr(Options{FS: theme("components/Card.html", "---\nbrick:\n  css: scope\n---\n<div></div>", "content/a.html", `<Card />`)}, "content/a.html")
	var be *Error
	if !errors.As(err, &be) || !strings.Contains(err.Error(), "brick: css: must be one of global, scoped, inclusive") {
		t.Fatalf("bad value: %v", err)
	}
	_, err = newEngineErr(Options{FS: theme("content/a.html", "---\nbrick:\n  css: scoped\n---\n<p>x</p>")}, "content/a.html")
	if err == nil || !strings.Contains(err.Error(), "only a component has a sibling stylesheet") {
		t.Fatalf("page: %v", err)
	}
	e := newEngine(t, Options{FS: theme("components/Card.html", "---\nbrick:\n  css: scoped\n---\n<div></div>", "content/a.html", `<Card />`)})
	render(t, e, "content/a.html", nil)
	if !slices.ContainsFunc(e.Warnings(), func(d Diagnostic) bool { return strings.Contains(d.Message, "there is no ~/components/Card.css") }) {
		t.Fatalf("warnings = %v", e.Warnings())
	}
	// A scoped sheet needs one root, like a scoped block, and says so.
	_, err = newEngineErr(Options{FS: theme("components/Card.html", "---\nbrick:\n  css: scoped\n---\n<p>a</p><p>b</p>", "components/Card.css", "p{}", "content/a.html", `<Card />`)}, "content/a.html")
	if err == nil || !strings.Contains(err.Error(), "brick: {css: scoped}") {
		t.Fatalf("multi-root: %v", err)
	}
}
