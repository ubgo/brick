package brick

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
)

// TestGrammarMatchesRuntime keeps the browser runtime's modifier table in
// step with the one the compiler checks: runtime.js must
// carry GrammarJS() verbatim, so a modifier added on one side only fails
// here instead of being accepted by the compiler and ignored by the page.
func TestGrammarMatchesRuntime(t *testing.T) {
	src, err := os.ReadFile("runtime/runtime.js")
	if err != nil {
		t.Fatal(err)
	}
	want := GrammarJS()
	if !strings.Contains(string(src), want) {
		t.Fatalf("runtime/runtime.js does not carry the grammar table; paste this line into it:\n%s", want)
	}
}

// TestDwellMatchesRuntime keeps the runtime's DWELL_MS, the wait of a
// bare brick:intersect.dwell, equal to DwellDefaultMS, which bricktest
// and the docs read.
func TestDwellMatchesRuntime(t *testing.T) {
	src, err := os.ReadFile("runtime/runtime.js")
	if err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("const DWELL_MS = %d;", DwellDefaultMS)
	if !strings.Contains(string(src), want) {
		t.Fatalf("runtime/runtime.js does not carry %q", want)
	}
}

// Every brick: action attribute is checked against the one grammar table
// at compile time (D8): an unknown or conflicting modifier, an undeclared
// path or an unknown loading target is an error with the file, never an
// attribute the page silently ignores.
func TestBrickAttrGrammar(t *testing.T) {
	t.Parallel()
	cases := []struct {
		markup string
		err    string // "" = compiles
	}{
		{`<button brick:click="save">x</button>`, ""},
		{`<button brick:click.once.debounce.300ms="save">x</button>`, ""},
		{`<button brick:click.onec="save">x</button>`, `unknown modifier "onec"`},
		{`<button brick:click.window="save">x</button>`, `unknown modifier "window"`},
		{`<button brick:click.once.once="save">x</button>`, "given twice"},
		{`<button brick:click.debounce.300ms.throttle.100ms="save">x</button>`, "cannot be combined"},
		{`<input brick:keydown.enter="save">`, ""},
		{`<input brick:keydown.escape.window="save">`, ""},
		{`<input brick:keydown.enter.escape="save">`, "one key at most"},
		{`<input brick:keydown.f5="save">`, `unknown modifier "f5"`},
		{`<input brick:keydown.ctrl.enter="save">`, ""},
		{`<input brick:keydown.meta.k.window="save">`, ""},
		{`<input brick:keydown.ctrl.shift.alt.meta.z="save">`, ""},
		{`<input brick:keydown.7="save">`, ""},
		{`<input brick:keydown.ctrl="save">`, ".ctrl needs a key"},
		{`<input brick:keydown.shift.window="save">`, ".shift needs a key"},
		{`<input brick:keydown.ctrl.ctrl.k="save">`, "given twice"},
		{`<input brick:keydown.cmd.k="save">`, `unknown modifier "cmd"`},
		{`<input brick:keydown.ctrl.f5="save">`, `unknown modifier "f5"`},
		{`<input brick:keydown.ctrl.k.j="save">`, "one key at most"},
		{`<input brick:keyup.escape="save">`, ""},
		{`<input brick:keyup.ctrl.s.window.once="save">`, ""},
		{`<input brick:keyup.alt="save">`, ".alt needs a key"},
		{`<input brick:keyup.esc="save">`, `unknown modifier "esc"`},
		{`<input brick:input.debounce.300ms="save">`, ""},
		{`<input brick:input.window="save">`, `unknown modifier "window"`},
		{`<input brick:focus="save" brick:blur.once="save">`, ""},
		{`<input brick:focus.optimistic='$set("n", 1)'>`, `unknown modifier "optimistic"`},
		{`<input brick:blur.enter="save">`, `unknown modifier "enter"`},
		{`<input brick:blur="nosuch(">`, "not an action call"},
		{`<div brick:click.outside="save">x</div>`, ""},
		{`<div brick:click.outside.once="save">x</div>`, ""},
		{`<div brick:change.outside="save">x</div>`, `unknown modifier "outside"`},
		{`<div brick:click.away="save">x</div>`, `unknown modifier "away"`},
		{`<input brick:model="name">`, ""},
		{`<input brick:model.defer="address.city">`, ""},
		{`<input brick:model.throttle.200ms="name">`, ""},
		{`<input brick:model.lazy="name">`, ""},
		{`<input brick:model.debounce.300ms="name">`, ""},
		{`<input brick:model.debounce="name">`, "needs a duration"},
		{`<input brick:model.debounce.300="name">`, "needs a duration"},
		{`<input brick:model.defer.lazy="name">`, "cannot be combined"},
		{`<input brick:model.defer.debounce.300ms="name">`, "cannot be combined"},
		{`<input brick:model.blur="name">`, `unknown modifier "blur"`},
		{`<input brick:model="ghost">`, `"ghost" is not a field`},
		{`<input brick:model="">`, "not a state path"},
		{`<input brick:model="items.{{ 1 }}.qty">`, ""},
		{`<p brick:text="address.city">x</p>`, ""},
		{`<p brick:show="!name">x</p>`, ""},
		{`<p brick:show="!ghost">x</p>`, `"ghost" is not a field`},
		{`<p brick:text.lazy="name">x</p>`, `unknown modifier "lazy"`},
		{`<span brick:loading hidden>x</span>`, ""},
		{`<span brick:loading.delay="save $set" hidden>x</span>`, ""},
		{`<span brick:loading.delay.400ms.remove="save">x</span>`, ""},
		{`<span brick:loading="nosuch">x</span>`, "nosuch"},
		{`<span brick:loading="$dispatch">x</span>`, "not an action name"},
		{`<span brick:loading.class="save">x</span>`, `unknown modifier "class"`},
		// v21: call targets, a field's $set, .except.
		{`<span brick:loading="save(5)" hidden>x</span>`, ""},
		{`<span brick:loading='save(5, "a b") $set("name") $set("address.city")' hidden>x</span>`, ""},
		{`<span brick:loading.except="save" hidden>x</span>`, ""},
		{`<span brick:loading.except.remove="save(1)">x</span>`, ""},
		{`<span brick:loading.except hidden>x</span>`, "needs the actions it does not show for"},
		{`<span brick:loading="nosuch(5)">x</span>`, "nosuch"},
		{`<span brick:loading='$set("nope")'>x</span>`, `"nope" is not a field`},
		{`<span brick:loading="$set(5)">x</span>`, "targets a field by name"},
		{`<span brick:loading="save(5">x</span>`, "unbalanced"},
		{`<span brick:loading="save(x)">x</span>`, "arguments are JSON literals"},
		{`<span brick:loading="save({{ 5 }})" hidden>x</span>`, ""},
		// v21: .window on focus and blur; valueless disable-with.
		{`<div brick:focus.window="save" brick:blur.window.once="save">x</div>`, ""},
		{`<input brick:blur.window.optimistic='$set("name", "")'>`, ""},
		{`<button brick:click="save" brick:disable-with>x</button>`, ""},
		{`<p brick:dirty hidden>x</p>`, ""},
		{`<p brick:dirty.remove="name address.city">x</p>`, ""},
		{`<p brick:dirty="ghost">x</p>`, `"ghost" is not a field`},
		{`<div brick:poll="5s">x</div>`, ""},
		{`<div brick:poll="soon">x</div>`, "is an interval"},
		{`<div brick:poll.5s="save">x</div>`, ""},
		{`<div brick:poll.10s.visible='save("x")'>x</div>`, ""},
		{`<div brick:poll.5s>x</div>`, "needs an action call"},
		{`<div brick:poll.5s="save('x')">x</div>`, "JSON literals"},
		{`<div brick:poll.5s.every="save">x</div>`, `unknown modifier "every"`},
		{`<div brick:poll.5s.keep-alive="save">x</div>`, ""},
		{`<div brick:poll.keep-alive.visible="5s">x</div>`, ""},
		{`<div brick:poll.5s.keep-alive.keep-alive="save">x</div>`, "given twice"},
		{`<div brick:intersect.keep-alive="save">x</div>`, `unknown modifier "keep-alive"`},
		{`<button brick:click.keep-alive="save">x</button>`, `unknown modifier "keep-alive"`},
		{`<div brick:intersect.dwell="seen">x</div>`, ""},
		{`<div brick:intersect.dwell.2000ms.once.half="seen">x</div>`, ""},
		{`<div brick:intersect.once.full.margin.100px.dwell.500ms="seen">x</div>`, ""},
		{`<div brick:intersect.dwell.2s="seen">x</div>`, `unknown modifier "2s"`},
		{`<div brick:intersect.dwell.ms="seen">x</div>`, `unknown modifier "ms"`},
		{`<div brick:intersect.dwell.leave="seen">x</div>`, ".dwell cannot be combined with .leave"},
		{`<div brick:intersect.dwell.dwell.300ms="seen">x</div>`, "given twice"},
		{`<div brick:poll.5s.dwell="save">x</div>`, `unknown modifier "dwell"`},
		{`<button brick:click.dwell.300ms="save">x</button>`, `unknown modifier "dwell"`},
		{`<button brick:click="save" brick:confirm="Delete?" brick:disable-with="Saving…">x</button>`, ""},
		{`<button brick:click="save" brick:confirm.prompt="New name?">x</button>`, ""},
		{`<button brick:click="save" brick:confirm.prompt.prompt="New name?">x</button>`, "given twice"},
		{`<button brick:click="save" brick:confirm.ask="New name?">x</button>`, `unknown modifier "ask"`},
		{`<Btn brick:click.once="save" />`, ""},
		{`<Btn brick:click.onec="save" />`, `unknown modifier "onec"`},
		{`<Btn brick:loading="nosuch" />`, "nosuch"},
	}
	sc := fakeScripting{modules: map[string]map[string]func(args ...any) (any, error){
		"f": {"save": func(a ...any) (any, error) { return a[0], nil }},
	}}
	for _, tc := range cases {
		e := newEngine(t, Options{States: NewMemoryStates(0, 0), Scripting: sc, FS: theme(
			"components/F.html", "---\nstate:\n  name: \"\"\n  address: {city: \"\"}\n  items: []\n---\n<brick:script>f</brick:script>\n<div>"+tc.markup+"</div>",
			"components/Btn.html", "<button {{ attrs }}>b</button>",
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

// Action attributes stay reserved outside a stateful component.
func TestBrickAttrNeedsState(t *testing.T) {
	t.Parallel()
	e := newEngine(t, Options{FS: theme("content/a.html", `<input brick:keydown.enter="save">`)})
	if _, err := e.Render(context.Background(), "content/a.html", nil); err == nil || !strings.Contains(err.Error(), "declares state") {
		t.Fatalf("%v", err)
	}
}

// An unknown brick:loading target is reported at its attribute's line and
// column, like brick:model and brick:click, not at the file's first line.
func TestLoadingUnknownTargetPosition(t *testing.T) {
	t.Parallel()
	sc := fakeScripting{modules: map[string]map[string]func(args ...any) (any, error){
		"f": {"save": func(a ...any) (any, error) { return a[0], nil }},
	}}
	e := newEngine(t, Options{States: NewMemoryStates(0, 0), Scripting: sc, FS: theme(
		"components/F.html", "---\nstate:\n  name: \"\"\n---\n<brick:script>f</brick:script>\n<div>\n  <span brick:loading=\"sve\" hidden>x</span>\n</div>",
		"content/a.html", `<F />`,
	)})
	err := renderErr(t, e, "content/a.html")
	if err.File != "components/F.html" || err.Line != 7 || err.Col != 15 || !strings.Contains(err.Error(), "sve") {
		t.Fatalf("got %s:%d:%d %v, want components/F.html:7:15 naming sve", err.File, err.Line, err.Col, err)
	}
}
