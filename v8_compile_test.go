package brick

import (
	"context"
	"strings"
	"testing"
)

// The v8 batch A attributes are checked at compile time: new
// grammar rows and modifiers, the morph controls, refs, transitions, and
// .optimistic only on a literal $set/$toggle (D8).
func TestV8AttrGrammar(t *testing.T) {
	t.Parallel()
	cases := []struct {
		markup string
		err    string // "" = compiles
	}{
		{`<div brick:intersect="save"></div>`, ""},
		{`<div brick:intersect.leave.once.half.margin.200px="save"></div>`, ""},
		{`<div brick:intersect.full="save"></div>`, ""},
		{`<div brick:intersect.half.full="save"></div>`, "cannot be combined"},
		{`<div brick:intersect.margin="save"></div>`, "needs a length"},
		{`<div brick:intersect.margin.200="save"></div>`, "needs a length"},
		{`<div brick:intersect.margin.200ms="save"></div>`, "needs a length"},
		{`<div brick:intersect.soon="save"></div>`, `unknown modifier "soon"`},
		{`<div brick:intersect="save('x')"></div>`, "JSON literals"},
		{`<button brick:click.optimistic='$toggle("liked")'>x</button>`, ""},
		{`<button brick:click.optimistic='$set("name", "x")'>x</button>`, ""},
		{`<input brick:change.optimistic='$set("liked", true)'>`, ""},
		{`<input brick:keydown.enter.optimistic='$toggle("liked")'>`, ""},
		{`<button brick:click.optimistic="save">x</button>`, ".optimistic needs a literal $set"},
		{`<button brick:click.optimistic="$refresh">x</button>`, ".optimistic needs a literal $set"},
		{`<button brick:click.optimistic='$toggle("ghost")'>x</button>`, ""}, // the path is the server's to refuse
		{`<form brick:submit.optimistic="save"></form>`, `unknown modifier "optimistic"`},
		{`<form brick:submit.lock="save"></form>`, ""},
		{`<button brick:click.lock="save">x</button>`, `unknown modifier "lock"`},
		{`<li brick:key="a">x</li>`, ""},
		{`<li brick:key="{{ .State.name }}">x</li>`, ""},
		{`<li brick:key>x</li>`, "needs a value"},
		{`<p brick:ignore>x</p>`, ""},
		{`<p brick:ignore.self>x</p>`, ""},
		{`<p brick:ignore.deep>x</p>`, `unknown modifier "deep"`},
		{`<p brick:ignore="yes">x</p>`, "takes no value"},
		{`<p brick:replace>x</p>`, ""},
		{`<p brick:replace.self>x</p>`, `unknown modifier "self"`},
		{`<p brick:ref="out">x</p>`, ""},
		{`<p brick:ref="out">x</p><b brick:ref="out"></b>`, "used twice"},
		{`<p brick:ref="a b">x</p>`, "a ref is a name"},
		{`<p brick:offline.remove hidden>x</p>`, ""},
		{`<a href="/" brick:current.exact>x</a>`, ""},
		{`<a href="/" brick:current.prefix>x</a>`, `unknown modifier "prefix"`},
		{`<section brick:transition="slide">x</section>`, ""},
		{`<details brick:keep="open">x</details>`, ""},
	}
	sc := fakeScripting{modules: map[string]map[string]func(args ...any) (any, error){
		"f": {"save": func(a ...any) (any, error) { return a[0], nil }},
	}}
	for _, tc := range cases {
		e := newEngine(t, Options{States: NewMemoryStates(0, 0), Scripting: sc, FS: theme(
			"components/F.html", "---\nstate:\n  name: \"\"\n  liked: false\n---\n<brick:script>f</brick:script>\n<div>"+tc.markup+"</div>",
			"content/a.html", `<F />`,
		)})
		_, err := e.Render(context.Background(), "content/a.html", nil)
		switch {
		case tc.err == "" && err != nil:
			t.Errorf("%s: %v", tc.markup, err)
		case tc.err != "" && (err == nil || !strings.Contains(err.Error(), tc.err)):
			t.Errorf("%s: got %v, want an error containing %q", tc.markup, err, tc.err)
		}
	}
}

// The morph controls, refs and navigation attributes work in any file; a
// transition and the action rows need a stateful component.
func TestV8PlainAttrsAnywhere(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ markup, err string }{
		{`<ul><li brick:key="a">x</li></ul><div brick:ignore></div><p brick:offline hidden></p><a href="/" brick:current>h</a><b brick:ref="x"></b>`, ""},
		{`<section brick:transition>x</section>`, "need a component that declares state"},
		{`<div brick:intersect="save"></div>`, "need a component that declares state"},
		{`<p brick:ignore.deep>x</p>`, `unknown modifier "deep"`},
	} {
		e := newEngine(t, Options{FS: theme("content/a.html", tc.markup)})
		_, err := e.Render(context.Background(), "content/a.html", nil)
		switch {
		case tc.err == "" && err != nil:
			t.Errorf("%s: %v", tc.markup, err)
		case tc.err != "" && (err == nil || !strings.Contains(err.Error(), tc.err)):
			t.Errorf("%s: got %v, want an error containing %q", tc.markup, err, tc.err)
		}
	}
}

// brick:ignore on a stateful root would keep the old root after every
// action, so it is a compile error; .self and an inner element are fine.
func TestIgnoreOnStatefulRoot(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		body string
		err  bool
	}{
		{`<div brick:ignore>x</div>`, true},
		{"\n<div class=\"a\" brick:ignore>x</div>", true},
		{`<div brick:ignore.self>x</div>`, false},
		{`<div><p brick:ignore>x</p></div>`, false},
	} {
		e := newEngine(t, Options{States: NewMemoryStates(0, 0), FS: theme(
			"components/F.html", "---\nstate:\n  n: 0\n---\n"+tc.body,
			"content/a.html", `<F />`,
		)})
		_, err := e.Render(context.Background(), "content/a.html", nil)
		if got := err != nil && strings.Contains(err.Error(), "brick:ignore on a stateful component's root"); got != tc.err {
			t.Errorf("%s: error %v, want %v", tc.body, err, tc.err)
		}
	}
}

// Rows of a range over a state list in a stateful file with no id and no
// brick:key get one Dev warning; a keyed row, a component
// call, a range over a literal list or a stateless file does not.
func TestUnkeyedRangeWarning(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		row   string
		state bool
		warn  bool
	}{
		{`<li>{{ . }}</li>`, true, true},
		{"\n  <li class=\"r\">{{ . }}</li>\n", true, true},
		{`<li brick:key="{{ . }}">{{ . }}</li>`, true, false},
		{`<li id="row-{{ . }}">{{ . }}</li>`, true, false},
		{`<Row :label="." :key="." />`, true, false},
		{`{{ . }}`, true, false},
		{`<li>{{ . }}</li>`, false, false},
	} {
		head := "---\nstate:\n  rows: [a, b]\n---\n"
		if !tc.state {
			head = "---\nprops:\n  x: {type: string, default: \"\"}\n---\n{{ $_ := .Props.x }}"
		}
		e := newEngine(t, Options{Mode: Dev, States: NewMemoryStates(0, 0), FS: theme(
			"components/Row.html", "---\nprops:\n  label: string\n---\n<li>{{ .Props.label }}</li>",
			"components/List.html", head+"<ul>{{ range .State.rows }}"+tc.row+"{{ end }}{{ range $i, $r := .State.rows }}"+tc.row+"{{ end }}{{ range (list 1 2) }}<li>static</li>{{ end }}</ul>",
			"content/a.html", `<List />`,
		)})
		if _, err := e.Render(context.Background(), "content/a.html", nil); err != nil {
			t.Fatalf("%s: %v", tc.row, err)
		}
		got := false
		for _, d := range e.Warnings() {
			got = got || d.Code == CodeUnkeyed
		}
		if got != tc.warn {
			t.Errorf("%s (state %v): warning %v, want %v: %v", tc.row, tc.state, got, tc.warn, e.Warnings())
		}
	}
}
