package brick

import (
	"context"
	"strings"
	"testing"
)

// brick:js and brick:leave are plain attributes (any file, no state), and
// the compiler checks brick:js's event, modifiers and every literal
// command, so a typo is an error with a file instead of a button that
// silently does nothing.
func TestJSCommandsGrammar(t *testing.T) {
	t.Parallel()
	cases := []struct {
		markup string
		err    string // "" = compiles
	}{
		{`<button brick:js.click='toggle("#menu")'>x</button>`, ""},
		{`<button brick:js.click='toggle("#menu"); toggleAttr("this", "aria-expanded", "true", "false")'>x</button>`, ""},
		{`<button brick:js.click='addClass("@panel", "open shadow"); removeClass("closest .card", "dim"); toggleClass("this", "on")'>x</button>`, ""},
		{`<button brick:js.click='setAttr("#a", "title", "hi; there"); removeAttr("#a", "title"); focus("#q"); dispatch("this", "menu:open", {"id": 1})'>x</button>`, ""},
		{`<button brick:js.dblclick.once='show("#x")'>x</button>`, ""},
		{`<div brick:js.click.outside='hide("#menu")'>x</div>`, ""},
		{`<div brick:js.keydown.escape.window='hide("#menu")'>x</div>`, ""},
		{`<input brick:js.keyup.ctrl.k='focus("#q")'>`, ""},
		{`<div brick:js.mouseenter='show("#tip")' brick:js.mouseleave='hide("#tip")'>x</div>`, ""},
		{`<button brick:js.click='toggle("{{ print "#x" }}")'>x</button>`, ""},
		{`<button brick:js='toggle("#menu")'>x</button>`, "name the event"},
		{`<button brick:js.click.dblclick='toggle("#menu")'>x</button>`, "one event per attribute"},
		{`<button brick:js.tap='toggle("#menu")'>x</button>`, `unknown modifier "tap"`},
		{`<button brick:js.change.outside='hide("#m")'>x</button>`, ".outside goes with .click only"},
		{`<button brick:js.click.window='hide("#m")'>x</button>`, ".window goes with .keydown, .keyup, .focus and .blur only"},
		{`<button brick:js.click.enter='hide("#m")'>x</button>`, "keys go with .keydown and .keyup only"},
		{`<button brick:js.keydown.ctrl='hide("#m")'>x</button>`, ".ctrl needs a key"},
		{`<button brick:js.click=''>x</button>`, "needs commands"},
		{`<button brick:js.click='explode("#m")'>x</button>`, `unknown command "explode"`},
		{`<button brick:js.click='toggle'>x</button>`, "is not a command"},
		{`<button brick:js.click='toggle()'>x</button>`, "toggle takes 1 arguments"},
		{`<button brick:js.click='addClass("#m")'>x</button>`, "addClass takes 2 arguments"},
		{`<button brick:js.click='toggle(#m)'>x</button>`, "JSON literals"},
		{`<button brick:js.click='toggle("")'>x</button>`, "the target"},
		{`<button brick:js.click='toggle("@bad name")'>x</button>`, "a ref is @"},
		{`<button brick:js.click='toggle("closest ")'>x</button>`, "closest needs a selector"},
		{`<button brick:js.click='addClass("#m", "")'>x</button>`, "the classes"},
		{`<button brick:js.click='setAttr("#m", "onclick", "alert(1)")'>x</button>`, "event handler"},
		{`<button brick:js.click='setAttr("#m", "OnLoad", "x")'>x</button>`, "event handler"},
		{`<button brick:js.click='setAttr("#m", "srcdoc", "x")'>x</button>`, "would load a document"},
		{`<button brick:js.click='setAttr("#m", "brick:click", "x")'>x</button>`, "belongs to the engine"},
		{`<button brick:js.click='removeAttr("#m", "data-brick-state")'>x</button>`, "belongs to the engine"},
		{`<button brick:js.click='setAttr("#m", "href", " JavaScript:alert(1)")'>x</button>`, "at a script"},
		{`<button brick:js.click='setAttr("#m", "title", {"a": 1})'>x</button>`, "string, number or boolean"},
		{`<button brick:js.click='dispatch("this", "click")'>x</button>`, "re-trigger"},
		{`<li brick:leave="transition-opacity duration-300 opacity-0">x</li>`, ""},
		{`<li brick:leave>x</li>`, "needs a value"},
		{`<li brick:leave.slow="fade">x</li>`, `unknown modifier "slow"`},
		{`<li brick:enter="opacity-0" brick:leave="opacity-0">x</li>`, ""},
		{`<li brick:enter>x</li>`, "needs a value"},
		{`<div brick:js.focus.window='show("#m")' brick:js.blur.window='hide("#m")'>x</div>`, ""},
		{`<div brick:js.focus.window.enter='show("#m")'>x</div>`, "keys go with .keydown and .keyup only"},
	}
	for _, tc := range cases {
		e := newEngine(t, Options{FS: theme("content/a.html", "<div>"+tc.markup+"</div>")})
		_, err := e.Render(context.Background(), "content/a.html", map[string]any{"id": "#x"})
		switch {
		case tc.err == "" && err != nil:
			t.Errorf("%s: %v", tc.markup, err)
		case tc.err != "" && (err == nil || !strings.Contains(err.Error(), tc.err)):
			t.Errorf("%s: got %v, want an error containing %q", tc.markup, err, tc.err)
		}
	}
}

// splitJSCommands splits on semicolons outside JSON strings only.
func TestSplitJSCommands(t *testing.T) {
	t.Parallel()
	got := splitJSCommands(`setAttr("#a", "title", "a; \"b;\"");  ; hide("#b")`)
	if len(got) != 2 || !strings.HasPrefix(got[0], `setAttr("#a", "title", "a; \"b;\"")`) || strings.TrimSpace(got[1]) != `hide("#b")` {
		t.Fatalf("%q", got)
	}
}
