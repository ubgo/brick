package scriptstarlark

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/ubgo/brick"
)

// brick.transition records the action's own transition effect (v22): a
// type, "none", or no argument for an untyped one; a bad type fails the
// action.
func TestBrickTransition(t *testing.T) {
	t.Parallel()
	sc, err := New(Options{})
	if err != nil {
		t.Fatal(err)
	}
	script := "<brick:script>\n" +
		"def slide(state, props):\n" +
		"    brick.transition(\"slide back\")\n" +
		"    return state\n\n" +
		"def still(state, props):\n" +
		"    brick.transition(\"none\")\n" +
		"    return state\n\n" +
		"def plain(state, props):\n" +
		"    brick.transition()\n" +
		"    return state\n\n" +
		"def bad(state, props):\n" +
		"    brick.transition(\"no!\")\n" +
		"    return state\n" +
		"</brick:script>\n"
	fsys := files("components/C.html", "---\nstate: {n: 0}\n---\n"+script+"<div>{{ .State.n }}</div>", "content/a.html", `<C />`)
	e, err := brick.New(brick.Options{Scripting: sc, States: brick.NewMemoryStates(0, 0), FS: fsys})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	page, err := e.Render(ctx, "content/a.html", nil)
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`data-brick-state="([^"]+)" data-brick-token="([0-9a-f]+)"`)
	m := re.FindStringSubmatch(string(page))
	for action, want := range map[string]string{"slide": "slide back", "still": brick.TransitionNone, "plain": ""} {
		res, err := e.RunAction(ctx, brick.ActionRequest{Handle: m[1], Token: m[2], Action: action})
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Effects) != 1 || res.Effects[0].Kind != brick.EffectTransition || res.Effects[0].Name != want {
			t.Fatalf("%s: effects = %+v", action, res.Effects)
		}
		m = re.FindStringSubmatch(string(res.HTML))
	}
	if _, err := e.RunAction(ctx, brick.ActionRequest{Handle: m[1], Token: m[2], Action: "bad"}); err == nil || !strings.Contains(err.Error(), "CSS identifier") {
		t.Fatalf("a bad type: got %v", err)
	}
}
