package bricktest_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/ubgo/brick"
	"github.com/ubgo/brick/bricktest"
)

const windowFocusComp = `---
state:
  log: []
---
<brick:script>ev</brick:script>
<div brick:focus.window='note("back")' brick:blur.window='note("away")'>
  <input id="name" brick:focus='note("focus")' brick:focus.window='note("window back")' brick:blur.window='note("away 2")'>
</div>`

func windowFocusEngine(t *testing.T) *brick.Engine {
	t.Helper()
	sc := scripting{"ev": {
		"note": func(_ context.Context, a ...any) (any, error) {
			s, _ := a[0].(map[string]any)
			l, _ := s["log"].([]any)
			s["log"] = append(l, strings.TrimSpace(fmt.Sprintln(a[2:]...)))
			return s, nil
		},
	}}
	return bricktest.Engine(t, brick.Options{Scripting: sc, States: brick.NewMemoryStates(0, 0)}, bricktest.Theme(
		"components/W.html", windowFocusComp,
		"content/a.html", `<main><W /></main>`,
	))
}

// LivePage.WindowBlur and WindowFocus run every .window attribute in
// document order, as the runtime's window listener does; an element's own
// Focus runs only its brick:focus, never a .window one.
func TestLiveWindowFocus(t *testing.T) {
	live := bricktest.Live(t, windowFocusEngine(t), "content/a.html")
	if got := live.WindowBlur(); len(got) != 2 {
		t.Fatalf("WindowBlur ran %d actions, want 2", len(got))
	}
	if got := live.WindowFocus(); len(got) != 2 {
		t.Fatalf("WindowFocus ran %d actions, want 2", len(got))
	}
	e := live.Component("W")
	e.Focus("#name")
	e.AssertSet(t, "log", []any{"away", "away 2", "back", "window back", "focus"})

	if !fails(t, func(ft testing.TB) {
		// #name has only a .window blur: an element blur finds none.
		bricktest.Live(ft, windowFocusEngine(t), "content/a.html").Component("W").Blur("#name", "x")
	}) {
		t.Error("Blur ran a brick:blur.window")
	}
}
