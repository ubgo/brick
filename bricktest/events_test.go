package bricktest_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ubgo/brick"
	"github.com/ubgo/brick/bricktest"
	"github.com/ubgo/brick/runtime"
)

const eventsComp = `---
state:
  log: []
  n: 0
---
<brick:script>ev</brick:script>
<div>
  <select id="plan" brick:change="choose"><option value="a">A</option><option value="b">B</option></select>
  <label><input id="agree" type="checkbox" brick:change='note("agree")'></label>
  <div id="group" brick:change="grp"><input id="inner" value="x"></div>
  <input id="q" brick:keydown.enter='note("enter")' brick:keydown.escape.once='note("esc")'>
  <ul brick:keydown='note("any")'><li><a id="row">row</a></li></ul>
  <span brick:keydown.escape.window='note("winesc")' brick:keydown.window='note("win")'></span>
  <input id="optin" brick:change.optimistic='$set("n", 1)'>
  <p id="more" brick:intersect='note("more")'></p>
  <p id="seen" brick:intersect.once='note("once")' brick:intersect.leave='note("left")'></p>
  <button id="go" brick:click.once.debounce.300ms='note("go")'>go</button>
  <p id="ad" brick:intersect.dwell='note("imp")' brick:intersect.leave='note("adleft")'></p>
  <p id="hero" brick:intersect.dwell.2000ms.once.half='note("hero")'></p>
</div>`

func eventsEngine(t *testing.T) *brick.Engine {
	t.Helper()
	logged := func(a []any, entry any) (any, error) {
		s, _ := a[0].(map[string]any)
		l, _ := s["log"].([]any)
		s["log"] = append(l, entry)
		return s, nil
	}
	sc := scripting{"ev": {
		"choose": func(_ context.Context, a ...any) (any, error) { return logged(a, fmt.Sprint("pick:", a[len(a)-1])) },
		"grp":    func(_ context.Context, a ...any) (any, error) { return logged(a, fmt.Sprint("grp:", len(a))) },
		"note": func(_ context.Context, a ...any) (any, error) {
			return logged(a, strings.TrimSpace(fmt.Sprintln(a[2:]...)))
		},
	}}
	return bricktest.Engine(t, brick.Options{Scripting: sc, States: brick.NewMemoryStates(0, 0)}, bricktest.Theme(
		"components/Ev.html", eventsComp,
		"content/a.html", `<main><Ev /></main>`,
	))
}

func TestLiveChange(t *testing.T) {
	e := bricktest.Live(t, eventsEngine(t), "content/a.html").Component("Ev")
	// A select sends its value as a string; a checkbox its checked bool.
	e.Change("#plan", "b")
	e.Change("#agree", true)
	// A brick:change on a non-control runs with no extra argument.
	e.Change("#inner", nil)
	// .optimistic $set does not take the event's value.
	e.Change("#optin", "ignored")
	e.AssertSet(t, "log", []any{"pick:b", "agree true", "grp:2"})
	e.AssertSet(t, "n", 1)

	for name, fn := range map[string]func(ft testing.TB){
		"checkbox wants a bool": func(ft testing.TB) {
			bricktest.Live(ft, eventsEngine(t), "content/a.html").Component("Ev").Change("#agree", "on")
		},
		"value on a non-control": func(ft testing.TB) {
			bricktest.Live(ft, eventsEngine(t), "content/a.html").Component("Ev").Change("#inner", "x")
		},
		"no brick:change": func(ft testing.TB) {
			bricktest.Live(ft, eventsEngine(t), "content/a.html").Component("Ev").Change("#q", "x")
		},
	} {
		if !fails(t, fn) {
			t.Errorf("%s: Change did not fail", name)
		}
	}
}

func TestLiveKeydown(t *testing.T) {
	live := bricktest.Live(t, eventsEngine(t), "content/a.html")
	e := live.Component("Ev")
	e.Keydown("#q", "Enter")
	if res := e.Keydown("#q", "Escape"); res == nil {
		t.Fatal("the first Escape did not run")
	}
	// .once: the second press does nothing.
	if res := e.Keydown("#q", "Escape"); res != nil {
		t.Fatal("brick:keydown.escape.once ran twice")
	}
	// A bare brick:keydown above the focused element takes any key.
	e.Keydown("#row", "x")
	// Window listeners hear a key pressed anywhere; the key filter holds.
	if got := live.Keydown("Escape"); len(got) != 2 {
		t.Fatalf("window Escape ran %d actions, want 2 (escape and bare)", len(got))
	}
	if got := live.Keydown("z"); len(got) != 1 {
		t.Fatalf("window z ran %d actions, want 1", len(got))
	}
	e.AssertSet(t, "log", []any{"enter", "esc", "any", "winesc", "win", "win"})

	for name, fn := range map[string]func(ft testing.TB){
		"no key match": func(ft testing.TB) {
			bricktest.Live(ft, eventsEngine(t), "content/a.html").Component("Ev").Keydown("#q", "Tab")
		},
		"window only": func(ft testing.TB) {
			bricktest.Live(ft, eventsEngine(t), "content/a.html").Component("Ev").Keydown("#more", "Escape")
		},
	} {
		if !fails(t, fn) {
			t.Errorf("%s: Keydown did not fail", name)
		}
	}
}

func TestLiveIntersect(t *testing.T) {
	e := bricktest.Live(t, eventsEngine(t), "content/a.html").Component("Ev")
	// .leave fires only after the element was seen.
	if res := e.Leave("#seen"); res != nil {
		t.Fatal("left before it was seen")
	}
	e.Intersect("#more")
	e.Intersect("#more")
	e.Intersect("#seen")
	if res := e.Intersect("#seen"); res != nil {
		t.Fatal("brick:intersect.once ran twice")
	}
	e.Leave("#seen")
	e.AssertSet(t, "log", []any{"more", "more", "once", "left"})

	// .dwell (default brick.DwellDefaultMS) runs only for a stay that long,
	// once per stay; a shorter glimpse leaves without it.
	if res := e.IntersectFor("#ad", 999*time.Millisecond); res == nil {
		t.Fatal("the glimpse ran nothing: its .leave should still run")
	}
	e.IntersectFor("#ad", time.Duration(brick.DwellDefaultMS)*time.Millisecond)
	e.Intersect("#ad")
	if res := e.Intersect("#ad"); res != nil {
		t.Fatal(".dwell ran twice in one stay")
	}
	e.Leave("#ad")
	e.Intersect("#ad") // a new stay counts again
	// .dwell.2000ms.once: too short, then long enough, then never again.
	if res := e.IntersectFor("#hero", 1500*time.Millisecond); res != nil {
		t.Fatal(".dwell.2000ms ran after 1.5s")
	}
	e.IntersectFor("#hero", 2*time.Second)
	e.IntersectFor("#hero", time.Minute)
	e.AssertSet(t, "log", []any{"more", "more", "once", "left", "adleft", "imp", "adleft", "imp", "adleft", "imp", "hero"})

	// The observer watches the element itself, never an ancestor.
	if !fails(t, func(ft testing.TB) {
		bricktest.Live(ft, eventsEngine(t), "content/a.html").Component("Ev").Intersect("#go")
	}) {
		t.Error("Intersect on an element without brick:intersect did not fail")
	}
}

func TestLiveClickModifiers(t *testing.T) {
	e := bricktest.Live(t, eventsEngine(t), "content/a.html").Component("Ev")
	// .debounce lets one click through; .once stops the second.
	if e.Click("#go") == nil || e.Click("#go") != nil {
		t.Fatal("brick:click.once.debounce.300ms did not run exactly once")
	}
	e.AssertSet(t, "log", []any{"go"})
}

// TestKeyEventMatchesRuntime pins the driver's key table to the runtime's
// KEY_EVENT, so a key added to one cannot silently differ in the other.
func TestKeyEventMatchesRuntime(t *testing.T) {
	want := `const KEY_EVENT = { enter: "Enter", escape: "Escape", tab: "Tab", space: " ", up: "ArrowUp", down: "ArrowDown", left: "ArrowLeft", right: "ArrowRight", backspace: "Backspace", delete: "Delete" };`
	if !strings.Contains(string(runtime.JS), want) {
		t.Fatalf("runtime.js KEY_EVENT changed; update keyEvent in events.go and this line")
	}
	if got := bricktest.KeyEventForTest(); got != want {
		t.Fatalf("keyEvent renders as\n%s\nwant\n%s", got, want)
	}
}

const events14Comp = `---
state:
  log: []
---
<brick:script>ev</brick:script>
<div>
  <input id="q" brick:input.debounce.300ms="search">
  <input id="cb" type="checkbox" brick:input='note("cb")'>
  <input id="name" brick:focus='note("focus")' brick:blur="check" brick:blur.once='note("blur once")'>
  <form brick:blur='note("never")'><input id="inner"></form>
  <textarea id="t" brick:keydown.enter='note("send")' brick:keydown.ctrl.enter='note("all")' brick:keydown.meta.k='note("palette")' brick:keyup.escape='note("up esc")'></textarea>
  <span brick:keyup.ctrl.s.window='note("save")'></span>
  <div id="menu" brick:click.outside='note("close")' brick:click='note("inside")'><button id="item">x</button></div>
  <div hidden><p id="pop" brick:click.outside='note("never")'></p></div>
</div>`

func events14Engine(t *testing.T) *brick.Engine {
	t.Helper()
	logged := func(a []any, entry any) (any, error) {
		s, _ := a[0].(map[string]any)
		l, _ := s["log"].([]any)
		s["log"] = append(l, entry)
		return s, nil
	}
	sc := scripting{"ev": {
		"search": func(_ context.Context, a ...any) (any, error) { return logged(a, fmt.Sprint("search:", a[len(a)-1])) },
		"check":  func(_ context.Context, a ...any) (any, error) { return logged(a, fmt.Sprint("check:", a[len(a)-1])) },
		"note": func(_ context.Context, a ...any) (any, error) {
			return logged(a, strings.TrimSpace(fmt.Sprintln(a[2:]...)))
		},
	}}
	return bricktest.Engine(t, brick.Options{Scripting: sc, States: brick.NewMemoryStates(0, 0)}, bricktest.Theme(
		"components/Ev.html", events14Comp,
		"content/a.html", `<main><Ev /></main>`,
	))
}

// The v14 drivers follow the runtime: input carries the value like
// change, focus and blur use only the element's own attributes, key
// combos match exactly their modifiers, keyup has the keydown rules, and
// .outside runs only for a click outside a displayed element.
func TestLiveV14Events(t *testing.T) {
	live := bricktest.Live(t, events14Engine(t), "content/a.html")
	e := live.Component("Ev")
	e.Input("#q", "ad")
	e.Input("#cb", true)
	e.Focus("#name")
	e.Blur("#name", "Ada")
	if e.Blur("#name", "Ada") == nil {
		t.Fatal("the second blur ran nothing; brick:blur (without .once) should still run")
	}
	e.AssertSet(t, "log", []any{"search:ad", "cb true", "focus", "check:Ada", "blur once Ada", "check:Ada"})

	e = bricktest.Live(t, events14Engine(t), "content/a.html").Component("Ev")
	e.Keydown("#t", "Enter")
	e.Keydown("#t", "ctrl+Enter")
	e.Keydown("#t", "meta+K") // shift is not held: "K" is a letter in either case
	e.Keyup("#t", "Escape")
	e.AssertSet(t, "log", []any{"send", "all", "palette", "up esc"})

	live = bricktest.Live(t, events14Engine(t), "content/a.html")
	e = live.Component("Ev")
	if got := live.Keyup("s"); len(got) != 0 {
		t.Fatalf("plain s ran %d window keyup actions, want 0", len(got))
	}
	if got := live.Keyup("ctrl+s"); len(got) != 1 {
		t.Fatalf("ctrl+s ran %d window keyup actions, want 1", len(got))
	}
	if got := live.Keydown("ctrl+s"); len(got) != 0 {
		t.Fatalf("a keydown ran %d keyup.window actions", len(got))
	}
	e.Click("#item")
	e.ClickOutside("#menu")
	if e.ClickOutside("#pop") != nil {
		t.Fatal("a hidden element's .outside ran")
	}
	e.AssertSet(t, "log", []any{"save", "inside", "close"})

	for name, fn := range map[string]func(ft testing.TB){
		"exact modifiers": func(ft testing.TB) {
			bricktest.Live(ft, events14Engine(t), "content/a.html").Component("Ev").Keydown("#t", "ctrl+shift+Enter")
		},
		"enter with shift": func(ft testing.TB) {
			bricktest.Live(ft, events14Engine(t), "content/a.html").Component("Ev").Keydown("#t", "shift+Enter")
		},
		"unknown modifier": func(ft testing.TB) {
			bricktest.Live(ft, events14Engine(t), "content/a.html").Component("Ev").Keydown("#t", "cmd+k")
		},
		"keyup is not keydown": func(ft testing.TB) {
			bricktest.Live(ft, events14Engine(t), "content/a.html").Component("Ev").Keydown("#t", "Escape")
		},
		"blur does not bubble": func(ft testing.TB) {
			bricktest.Live(ft, events14Engine(t), "content/a.html").Component("Ev").Blur("#inner", "x")
		},
		"no brick:focus": func(ft testing.TB) {
			bricktest.Live(ft, events14Engine(t), "content/a.html").Component("Ev").Focus("#q")
		},
		"no .outside": func(ft testing.TB) {
			bricktest.Live(ft, events14Engine(t), "content/a.html").Component("Ev").ClickOutside("#item")
		},
		"click inside is not outside": func(ft testing.TB) {
			// #pop has only an .outside attribute: a click on it runs nothing.
			bricktest.Live(ft, events14Engine(t), "content/a.html").Component("Ev").Click("#pop")
		},
	} {
		if !fails(t, fn) {
			t.Errorf("%s: the driver did not fail", name)
		}
	}
}
