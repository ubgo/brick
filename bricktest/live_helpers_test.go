package bricktest_test

import (
	"context"
	"strings"
	"testing"

	"github.com/ubgo/brick"
	"github.com/ubgo/brick/bricktest"
)

// helperEngine is a page with a stateful Board holding two stateful Rows,
// one of which holds a stateful Badge, plus a script whose actions skip
// the render and queue client effects.
func helperEngine(t *testing.T) *brick.Engine {
	t.Helper()
	st := func(a []any) map[string]any { s, _ := a[0].(map[string]any); return s }
	sc := scripting{"board": {
		// ping changes nothing on screen: it records a custom effect and a
		// dispatch and skips the re-render.
		"ping": func(ctx context.Context, a ...any) (any, error) {
			eff := brick.EffectsFrom(ctx)
			if err := eff.Add(brick.Effect{Kind: "x-confetti", Data: map[string]any{"count": 3}}); err != nil {
				return nil, err
			}
			if err := eff.Dispatch("pinged", nil, ""); err != nil {
				return nil, err
			}
			return st(a), eff.SkipRender()
		},
		"bump": func(_ context.Context, a ...any) (any, error) {
			s := st(a)
			n, _ := s["n"].(int64)
			s["n"] = n + 1
			return s, nil
		},
	}}
	return bricktest.Engine(t, brick.Options{Scripting: sc, States: brick.NewMemoryStates(0, 0)}, bricktest.Theme(
		"components/Badge.html", "---\nstate:\n  on: false\n---\n<em>badge</em>",
		"components/Row.html", "---\nprops:\n  label: string\nstate:\n  open: false\n---\n<div class=\"row\">{{ .Props.label }} {{ if .Props.label | eq \"b\" }}<Badge />{{ end }}</div>",
		"components/Board.html", "---\nstate:\n  n: 0\n---\n<brick:script>board</brick:script>\n<section><i>{{ .State.n }}</i> <Row label=\"a\" /> <Row label=\"b\" /></section>",
		"content/a.html", `<main><Board /></main>`,
	))
}

// Children lists the direct stateful children only; Child finds one at
// any depth.
func TestLiveChildren(t *testing.T) {
	live := bricktest.Live(t, helperEngine(t), "content/a.html")
	board := live.Component("Board")
	kids := board.Children()
	if len(kids) != 2 {
		t.Fatalf("Board has %d direct children, want 2 (the Badge is a grandchild): %s", len(kids), live.HTML())
	}
	for i, want := range []string{"a", "b"} {
		if kids[i].Name() != "Row" || kids[i].Text() != want && !strings.HasPrefix(kids[i].Text(), want+" ") {
			t.Fatalf("child %d is %s %q, want Row %q", i, kids[i].Name(), kids[i].Text(), want)
		}
	}
	if kids[0].ID() == kids[1].ID() {
		t.Fatalf("two Rows share the id %s", kids[0].ID())
	}
	if n := len(kids[0].Children()); n != 0 {
		t.Fatalf("Row a has %d children, want 0", n)
	}
	badge := board.Child("Badge")
	if badge.Name() != "Badge" || badge.Text() != "badge" {
		t.Fatalf("Child(Badge) = %s %q", badge.Name(), badge.Text())
	}
	// The handle acts like any other root.
	badge.Set("on", true)
	badge.AssertSet(t, "on", true)
	if !fails(t, func(ft testing.TB) {
		bricktest.Live(ft, helperEngine(t), "content/a.html").Component("Board").Child("Nope")
	}) {
		t.Fatal("Child of a missing component did not fail the test")
	}
}

// A renderless action keeps the component's HTML in the LivePage: before
// the fix the held HTML was replaced by the empty answer.
func TestLiveRenderSkipped(t *testing.T) {
	live := bricktest.Live(t, helperEngine(t), "content/a.html")
	board := live.Component("Board")
	before := board.HTML()
	res := board.Call("ping")
	bricktest.AssertRenderSkipped(t, res)
	if got := board.HTML(); got != before {
		t.Fatalf("a renderless action changed the held HTML:\nbefore %s\nafter  %s", before, got)
	}
	if !fails(t, func(ft testing.TB) { bricktest.AssertRendered(ft, res) }) {
		t.Fatal("AssertRendered passed for a skipped render")
	}
	bumped := board.Call("bump")
	bricktest.AssertRendered(t, bumped)
	if !fails(t, func(ft testing.TB) { bricktest.AssertRenderSkipped(ft, bumped) }) {
		t.Fatal("AssertRenderSkipped passed for a rendered action")
	}
	if got := board.Text(); !strings.HasPrefix(got, "1 ") {
		t.Fatalf("bump did not re-render: %q", got)
	}
}

// AssertEffect, AssertNoEffect and AssertEffects check what an action
// queued for the client, and each fails when it should.
func TestLiveEffectAssertions(t *testing.T) {
	live := bricktest.Live(t, helperEngine(t), "content/a.html")
	res := live.Component("Board").Call("ping")
	bricktest.AssertEffect(t, res, brick.Effect{Kind: "x-confetti", Data: map[string]any{"count": 3}})
	bricktest.AssertEffect(t, res, brick.Effect{Kind: "x-confetti"})
	bricktest.AssertEffect(t, res, brick.Effect{Kind: brick.EffectDispatch, Name: "pinged"})
	bricktest.AssertEffects(t, res, "x-confetti", brick.EffectDispatch)
	bricktest.AssertNoEffect(t, res, brick.EffectRedirect)

	cases := map[string]func(ft testing.TB){
		"wrong data": func(ft testing.TB) {
			bricktest.AssertEffect(ft, res, brick.Effect{Kind: "x-confetti", Data: map[string]any{"count": 4}})
		},
		"wrong name": func(ft testing.TB) {
			bricktest.AssertEffect(ft, res, brick.Effect{Kind: brick.EffectDispatch, Name: "other"})
		},
		"absent kind":    func(ft testing.TB) { bricktest.AssertEffect(ft, res, brick.Effect{Kind: brick.EffectFlash}) },
		"no kind":        func(ft testing.TB) { bricktest.AssertEffect(ft, res, brick.Effect{Name: "pinged"}) },
		"present kind":   func(ft testing.TB) { bricktest.AssertNoEffect(ft, res, "x-confetti") },
		"wrong order":    func(ft testing.TB) { bricktest.AssertEffects(ft, res, brick.EffectDispatch, "x-confetti") },
		"missing one":    func(ft testing.TB) { bricktest.AssertEffects(ft, res, "x-confetti") },
		"expect nothing": func(ft testing.TB) { bricktest.AssertEffects(ft, res) },
	}
	for name, fn := range cases {
		if !fails(t, fn) {
			t.Errorf("%s: the assertion passed", name)
		}
	}
	// An action with no effects satisfies AssertEffects with no kinds.
	bricktest.AssertEffects(t, live.Component("Board").Call("bump"))
}
