package brick

import (
	"context"
	"strings"
	"testing"
)

// brick:scroll="<name>" (G20) is checked at compile time in any file: the
// name is required and is a name, it takes no modifiers, a name used twice
// in one file is an error (the runtime keeps the first only), and the name
// space is its own, so a brick:ref of the same name does not collide.
func TestScrollAttrGrammar(t *testing.T) {
	t.Parallel()
	cases := []struct {
		markup string
		err    string // "" = compiles
	}{
		{`<nav brick:scroll="docs-nav">x</nav>`, ""},
		{`<nav brick:scroll="{{ .Params.side }}">x</nav>`, ""},
		{`<nav brick:scroll>x</nav>`, "a scroll is a name"},
		{`<nav brick:scroll="a b">x</nav>`, "a scroll is a name"},
		{`<nav brick:scroll.x="side">x</nav>`, `unknown modifier "x"`},
		{`<nav brick:scroll="side">x</nav><div brick:scroll="side"></div>`, "used twice"},
		{`<nav brick:scroll="side" brick:ref="side">x</nav>`, ""},
	}
	for _, tc := range cases {
		e := newEngine(t, Options{FS: theme("content/a.html", tc.markup)})
		res, err := e.Render(context.Background(), "content/a.html", nil)
		switch {
		case tc.err == "" && err != nil:
			t.Errorf("%s: %v", tc.markup, err)
		case tc.err != "" && (err == nil || !strings.Contains(err.Error(), tc.err)):
			t.Errorf("%s: got %v, want an error containing %q", tc.markup, err, tc.err)
		case tc.err == "" && !strings.Contains(string(res), "brick:scroll="):
			t.Errorf("%s: brick:scroll did not reach the page: %s", tc.markup, res)
		}
	}
}
