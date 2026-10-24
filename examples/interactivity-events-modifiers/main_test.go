package main

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/ubgo/brick"
	"github.com/ubgo/brick/bricktest"
	scriptstarlark "github.com/ubgo/brick/contrib/script-starlark"
)

// Every event attribute, driven the way the browser runtime fires it.
// bricktest reads the same modifier table as the runtime: a .debounce or
// .throttle lets each call through (timing is the browser's), .once runs
// once per element, and keys and combos must match exactly.
func TestEveryEvent(t *testing.T) {
	a, err := newApp("theme")
	if err != nil {
		t.Fatal(err)
	}
	live := bricktest.Live(t, a.eng, page)
	in := live.Component("Inbox")

	in.Input("#q", "ada")            // brick:input.debounce.300ms → search("ada")
	in.Blur("#q", "ada")             // brick:blur → validate("ada")
	in.Keydown("#msg", "Enter")      // brick:keydown.enter → send
	in.Keydown("#msg", "ctrl+Enter") // brick:keydown.ctrl.enter → send_all, not send
	in.Keyup("#msg", "Escape")       // brick:keyup.escape → clear
	in.ClickOutside("#menu")         // brick:click.outside → close
	in.Change("#plan", "pro")        // brick:change → choose("pro")
	in.ClickWith("#chart", bricktest.ClickDetails{Shift: true, X: 10, Count: 1})
	in.ClickPrompt("#rename", "Ada") // brick:confirm.prompt → rename(3, "Ada")
	in.Click("#hello")               // brick:click.once → welcome
	if res := in.Click("#hello"); res != nil {
		t.Fatalf(".once ran twice: %v", res)
	}
	in.Intersect("#sentinel") // brick:intersect.once → more
	in.Call("tick")           // what brick:poll.10s sends every ten seconds
	if _, err := in.TryCall("tick_tock"); !errors.Is(err, brick.ErrUnknownAction) {
		t.Fatalf("a misspelled action: %v", err) // answered 400 over HTTP
	}

	want := []string{"search(ada)", "validate(ada)", "send", "send_all", "clear", "close", "choose(pro)", "at(x=10 shift=True count=1)", "rename(3, Ada)", "welcome", "more", "tick"}
	in.AssertSet(t, "log", toAny(want))
	if !strings.Contains(in.HTML(), "<li>at(x=10 shift=True count=1)</li>") {
		t.Fatalf("log not rendered:\n%s", in.HTML())
	}
}

// toAny is want as the []any a state list decodes to.
func toAny(s []string) []any {
	out := make([]any, len(s))
	for i, v := range s {
		out[i] = v
	}
	return out
}

// Typos fail when the file is read, with the file, the line and the
// allowed list, never as a click that silently does nothing.
func TestTyposFailAtCompile(t *testing.T) {
	sc, err := scriptstarlark.New(scriptstarlark.Options{})
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		`<button brick:click.onec="go">x</button>`: `brick:click.onec: unknown modifier "onec" (brick:click accepts .once, .debounce.<ms>, .throttle.<ms>, .optimistic, .outside, .details)`,
		`<input brick:keydown.cmd.k="go">`:         `brick:keydown.cmd.k: unknown modifier "cmd"`,
		`<input brick:model.defer.lazy="q">`:       `.defer`,
		`<div brick:poll="soon"></div>`:            `brick:poll`,
	}
	for markup, want := range cases {
		comp := "---\nstate: {q: \"\"}\n---\n<brick:script>\ndef go(state, props):\n    return state\n</brick:script>\n<div>" + markup + "</div>"
		eng, err := brick.New(brick.Options{Scripting: sc, States: brick.NewMemoryStates(0, 0), FS: fstest.MapFS{
			"components/Box.html": {Data: []byte(comp)},
			"content/index.html":  {Data: []byte(`<Box />`)},
		}})
		if err != nil {
			t.Fatal(err)
		}
		_, err = eng.RenderChunks(context.Background(), "content/index.html", nil)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s:\n  err  = %v\n  want %q", markup, err, want)
		}
	}
}

// logLines runs fn with an app whose action log goes to a buffer and
// returns what was logged.
func logLines(t *testing.T, fn func(live *bricktest.LivePage)) string {
	t.Helper()
	var buf strings.Builder
	h := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	a, err := newAppLogged("theme", slog.New(h))
	if err != nil {
		t.Fatal(err)
	}
	fn(bricktest.Live(t, a.eng, page))
	return buf.String()
}

// The window-level attributes on Panel: a held key, the tab losing and
// regaining focus, the dwell on the sponsored block. bricktest runs the
// action each one names; the holding, the focus change and the two
// seconds are the browser's.
func TestPanelWindowDwellOffline(t *testing.T) {
	a, err := newApp("theme")
	if err != nil {
		t.Fatal(err)
	}
	live := bricktest.Live(t, a.eng, page)
	p := live.Component("Panel")

	live.Keydown("n") // brick:keydown.norepeat.window.n → advance
	p.AssertSet(t, "page", 2)

	live.WindowBlur() // brick:blur.window → went_away
	p.AssertSet(t, "away", true)
	live.WindowFocus() // brick:focus.window → came_back
	p.AssertSet(t, "away", false)

	p.IntersectFor("#ad", 2*time.Second) // brick:intersect.dwell.2000ms → viewed
	p.AssertSet(t, "views", 1)
	if res := p.IntersectFor("#ad", 500*time.Millisecond); res != nil {
		t.Fatalf("a stay shorter than the dwell still ran %v", res)
	}

	// The offline markup is served as written; showing it is the browser's.
	html := p.HTML()
	for _, want := range []string{`brick:offline>`, `brick:offline.remove>`} {
		if !strings.Contains(html, want) {
			t.Errorf("no %s in:\n%s", want, html)
		}
	}
}

// brick: {log:} tunes the success line: Panel logs at debug, Ticker is
// silent, and Inbox, which says nothing, logs at the default Info.
func TestActionLogSettings(t *testing.T) {
	out := logLines(t, func(live *bricktest.LivePage) {
		live.Component("Panel").Call("advance")
		live.Component("Ticker").Call("tick")
		live.Component("Inbox").Call("tick")
	})
	if !strings.Contains(out, "level=DEBUG msg=\"brick action\" component=Panel action=advance") {
		t.Errorf("Panel's debug line missing:\n%s", out)
	}
	if strings.Contains(out, "component=Ticker") {
		t.Errorf("Ticker logged despite log: false:\n%s", out)
	}
	if !strings.Contains(out, "level=INFO msg=\"brick action\" component=Inbox action=tick") {
		t.Errorf("Inbox's default line missing:\n%s", out)
	}
}

// A failed action is logged whatever the component says: a refusal is a
// security signal that one file must not be able to hide.
func TestActionLogOffStillLogsFailures(t *testing.T) {
	out := logLines(t, func(live *bricktest.LivePage) {
		if _, err := live.Component("Ticker").TryCall("explode"); err == nil {
			t.Fatal("explode succeeded")
		}
	})
	if !strings.Contains(out, "level=WARN msg=\"brick action failed\" component=Ticker action=explode") {
		t.Fatalf("the failure was not logged:\n%s", out)
	}
}
