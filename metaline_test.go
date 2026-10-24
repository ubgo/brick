package brick

import "testing"

// A brick: key's parse error points at that key's frontmatter line, not
// line 1, so `brick check` lands on the mistake.
func TestBrickKeyErrorLine(t *testing.T) {
	t.Parallel()
	e := newEngine(t, Options{FS: theme(
		"content/a.html", "---\ntitle: T\nbrick:\n  temporary: [flash]\n---\n<div></div>",
	)})
	err := renderErr(t, e, "content/a.html")
	if err.File != "content/a.html" || err.Line != 4 || err.Code != CodeFrontmatter {
		t.Fatalf("got %s:%d (%s): %v", err.File, err.Line, err.Code, err)
	}
}

// The same holds for a component reached through a page: its brick: key
// error names the component file and the key's line, whichever key it is
// (the callee's frontmatter is read by calleeMeta, a separate path from
// compile, which once dropped the line to 0).
func TestCalleeBrickKeyErrorLine(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, fm string }{
		{"css", "brick:\n  css: sideways\n"},
		{"temporary", "brick:\n  temporary: [flash]\n"},
		{"model", "state: {n: 0}\nbrick:\n  model: nope\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			src := "---\ntitle: T\n" + tc.fm + "---\n<div></div>"
			e := newEngine(t, Options{FS: theme(
				"content/a.html", "<Card />",
				"components/Card.html", src,
			)})
			err := renderErr(t, e, "content/a.html")
			want := brickLine([]byte(src), tc.name)
			if err.File != "components/Card.html" || err.Line != want || want <= 1 || err.Code != CodeFrontmatter {
				t.Fatalf("got %s:%d (%s), want components/Card.html:%d: %v", err.File, err.Line, err.Code, want, err)
			}
		})
	}
}
