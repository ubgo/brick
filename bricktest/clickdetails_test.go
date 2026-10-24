package bricktest_test

import (
	"context"
	"testing"

	"github.com/ubgo/brick"
	"github.com/ubgo/brick/bricktest"
)

// brick:click.details gets the click as its last argument (v22): Click
// sends a plain left click, ClickWith the one described, and an attribute
// without .details gets nothing extra.
func TestLiveClickDetails(t *testing.T) {
	sc := scripting{"cd": {
		"where": func(_ context.Context, a ...any) (any, error) {
			s, _ := a[0].(map[string]any)
			s["last"] = a[len(a)-1]
			s["argc"] = len(a)
			return s, nil
		},
	}}
	eng := bricktest.Engine(t, brick.Options{Scripting: sc, States: brick.NewMemoryStates(0, 0)}, bricktest.Theme(
		"components/Pad.html", "---\nstate:\n  last: null\n  argc: 0\n---\n<brick:script>cd</brick:script>\n<div><p id=\"pad\" brick:click.details=\"where\">pad</p><p id=\"plain\" brick:click=\"where\">plain</p></div>",
		"content/a.html", `<main><Pad /></main>`,
	))
	p := bricktest.Live(t, eng, "content/a.html").Component("Pad")
	p.Click("#pad")
	p.AssertSet(t, "last", map[string]any{"alt": false, "ctrl": false, "meta": false, "shift": false, "button": 0, "count": 1, "x": 0, "y": 0, "clientX": 0, "clientY": 0})
	p.ClickWith("#pad", bricktest.ClickDetails{Shift: true, Count: 2, X: 10, Y: 20, ClientX: 110, ClientY: 220})
	p.AssertSet(t, "last", map[string]any{"alt": false, "ctrl": false, "meta": false, "shift": true, "button": 0, "count": 2, "x": 10, "y": 20, "clientX": 110, "clientY": 220})
	p.Click("#plain")
	p.AssertSet(t, "argc", 2)
}
