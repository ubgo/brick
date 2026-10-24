package brick

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

// cssOf returns the stylesheet the engine registered for a component stem.
func cssOf(t *testing.T, e *Engine, stem string) string {
	t.Helper()
	e.mu.Lock()
	defer e.mu.Unlock()
	for name, f := range e.assetFiles {
		if strings.HasPrefix(name, assetDir+"/"+stem+".") && strings.HasSuffix(name, extCSS) {
			return string(f.data)
		}
	}
	t.Fatalf("no stylesheet for %s", stem)
	return ""
}

// scopedEngine renders one Card whose style block opens with open.
// The second result is the root's data-brick-s value.
func scopedEngine(t *testing.T, open string, opts Options) (*Engine, string) {
	t.Helper()
	opts.FS = theme(
		"components/Card.html", open+"\n.t { color: red }\n</brick:style>\n<div><p class=\"t\">x</p></div>",
		"content/a.html", `{{ assets }}<Card />`,
	)
	e := newEngine(t, opts)
	page, err := e.Render(context.Background(), "content/a.html", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, rest, _ := strings.Cut(string(page), scopeAttr+`="`)
	hash, _, _ := strings.Cut(rest, `"`)
	return e, hash
}

// The bound is pinned byte for byte: bare scoped keeps the v1 output, so
// no existing asset hash moves, and inclusive stops one element lower.
func TestScopedBounds(t *testing.T) {
	t.Parallel()
	cases := []struct{ open, limit string }{
		{`<brick:style scoped>`, `to ([data-brick-s])`},
		{`<brick:style scoped="exclusive">`, `to ([data-brick-s])`},
		{`<brick:style scoped="inclusive">`, `to ([data-brick-s] > *)`},
	}
	for _, c := range cases {
		e, hash := scopedEngine(t, c.open, Options{})
		if hash == "" {
			t.Fatalf("%s: root not marked", c.open)
		}
		got := cssOf(t, e, "Card")
		want := "@scope ([data-brick-s=\"" + hash + "\"]) " + c.limit + " {\n.t { color: red }\n}\n"
		if got != want {
			t.Errorf("%s:\n got %q\nwant %q", c.open, got, want)
		}
	}
	if e, _ := scopedEngine(t, `<brick:style>`, Options{}); cssOf(t, e, "Card") != ".t { color: red }\n" {
		t.Errorf("global block: %q", cssOf(t, e, "Card"))
	}
}

// A misspelled bound fails the file at the block's line instead of
// silently shipping exclusive rules.
func TestScopedBadBound(t *testing.T) {
	t.Parallel()
	src := "---\nprops:\n  a: string\n---\n<brick:style scoped=\"inclusiv\">\n.t{}\n</brick:style>\n<div></div>"
	_, err := newEngineErr(Options{FS: theme("components/Card.html", src, "content/a.html", `<Card />`)}, "content/a.html")
	var be *Error
	if !errors.As(err, &be) || be.Line != 5 || !strings.Contains(err.Error(), `scoped is bare, "exclusive" or "inclusive"`) {
		t.Fatalf("err = %v", err)
	}
}

// StyleTransform sees each block with what the engine would ship and
// replaces it; an error fails the compile naming the file.
func TestStyleTransform(t *testing.T) {
	t.Parallel()
	var seen []StyleBlock
	e, _ := scopedEngine(t, `<brick:style scoped="inclusive">`, Options{StyleTransform: func(b StyleBlock) ([]byte, error) {
		seen = append(seen, b)
		return append([]byte("/* t */\n"), b.CSS...), nil
	}})
	if len(seen) != 1 {
		t.Fatalf("calls = %d", len(seen))
	}
	b := seen[0]
	if b.File != "components/Card.html" || !b.Scoped || b.Bound != ScopeInclusive || !bytes.Contains(b.Source, []byte(".t { color: red }")) || !bytes.HasPrefix(b.CSS, []byte("@scope (")) || b.Hash == "" {
		t.Fatalf("block = %+v", b)
	}
	if got := cssOf(t, e, "Card"); !strings.HasPrefix(got, "/* t */\n@scope (") {
		t.Fatalf("css = %q", got)
	}

	src := "<brick:style>\n.t{}\n</brick:style>\n<div></div>"
	_, err := newEngineErr(Options{FS: theme("components/Card.html", src, "content/a.html", `<Card />`), StyleTransform: func(b StyleBlock) ([]byte, error) {
		if b.Scoped || b.Bound != "" {
			t.Errorf("global block reported scoped: %+v", b)
		}
		return nil, errors.New("boom")
	}}, "content/a.html")
	if err == nil || !strings.Contains(err.Error(), "Card.html") || !strings.Contains(err.Error(), "Options.StyleTransform: boom") {
		t.Fatalf("err = %v", err)
	}
}
