package brick

import (
	"context"
	"strings"
	"testing"
)

// Tests for the region effect. BV in a comment marks a
// break-verified test.

// Region records {kind: region, ref, op}, replace by default.
func TestRegionRecords(t *testing.T) {
	t.Parallel()
	eff := (&Engine{}).newEffects("")
	for _, op := range []string{"", RegionAppend, RegionPrepend, RegionReplace} {
		if err := eff.Region("feed", op); err != nil {
			t.Fatalf("op %q: %v", op, err)
		}
	}
	got := eff.list
	if len(got) != 4 || got[0].Kind != EffectRegion || got[0].Ref != "feed" || got[0].Op != RegionReplace || got[1].Op != RegionAppend || got[2].Op != RegionPrepend {
		t.Fatalf("recorded %+v", got)
	}
}

// A bad ref, an unknown op, rows fields through Add, and a region after
// skip_render all fail the action.
// BV: dropping the regionOps check lets "upsert" through.
func TestRegionRefuses(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		do   func(*Effects) error
		want string
	}{
		{"empty ref", func(e *Effects) error { return e.Region("", "") }, "must be a brick:ref name"},
		{"spaced ref", func(e *Effects) error { return e.Region("a b", "") }, "must be a brick:ref name"},
		{"unknown op", func(e *Effects) error { return e.Region("feed", "upsert") }, `op "upsert" is not one of replace, append, prepend`},
		{"rows fields", func(e *Effects) error {
			return e.Add(Effect{Kind: EffectRegion, Ref: "feed", Limit: 5})
		}, "belong to brick.rows"},
		{"after skip_render", func(e *Effects) error {
			if err := e.SkipRender(); err != nil {
				return err
			}
			return e.Region("feed", RegionAppend)
		}, "skipped its render"},
		// The order does not matter: skip_render after a region is refused
		// too, and Add is held to the same rule as Region.
		{"skip_render after region", func(e *Effects) error {
			if err := e.Region("feed", ""); err != nil {
				return err
			}
			return e.SkipRender()
		}, "a region is a piece of that render"},
		{"Add region after skip_render", func(e *Effects) error {
			if err := e.SkipRender(); err != nil {
				return err
			}
			return e.Add(Effect{Kind: EffectRegion, Ref: "feed"})
		}, "skipped its render"},
	}
	for _, c := range cases {
		eff := (&Engine{}).newEffects("")
		if err := c.do(eff); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: got %v, want %q", c.name, err, c.want)
		}
	}
	var none *Effects
	if err := none.Region("feed", ""); err == nil {
		t.Error("a region outside an action must fail")
	}
}

// An action's region effect reaches the result beside the render it
// takes its content from.
func TestRegionThroughAction(t *testing.T) {
	t.Parallel()
	src := "---\nstate:\n  page: 1\nbrick:\n  temporary: [page]\n---\n<brick:script>feed</brick:script>\n" +
		`<div><ul brick:ref="feed" brick:ignore><li>page {{ .State.page }}</li></ul></div>`
	sc := ctxScripting{"feed": {"more": func(ctx context.Context, a ...any) (any, error) {
		if err := EffectsFrom(ctx).Region("feed", RegionAppend); err != nil {
			return nil, err
		}
		st, _ := a[0].(map[string]any)
		st["page"] = 2
		return st, nil
	}}}
	e := newEngine(t, Options{FS: theme("components/Feed.html", src, "content/a.html", `<Feed />`), Scripting: sc, States: NewMemoryStates(0, 0)})
	page, err := e.Render(context.Background(), "content/a.html", nil)
	if err != nil {
		t.Fatal(err)
	}
	h, tok := handleOf(t, string(page))
	res, err := e.RunAction(context.Background(), ActionRequest{Handle: h, Token: tok, Action: "more"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Effects) != 1 || res.Effects[0].Kind != EffectRegion || res.Effects[0].Op != RegionAppend {
		t.Fatalf("effects %+v", res.Effects)
	}
	if !strings.Contains(string(res.HTML), `brick:ref="feed"`) || !strings.Contains(string(res.HTML), "<li>page 2</li>") {
		t.Fatalf("the render must hold the region's fresh content: %s", res.HTML)
	}
}
