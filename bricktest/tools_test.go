package bricktest_test

import (
	"context"
	"strings"
	"testing"

	"github.com/ubgo/brick"
	"github.com/ubgo/brick/bricktest"
)

//, T2 and T3: the pressed submit button, the data a render
// saw, and lazy islands rendered in the page.

const draftComp = `---
state:
  intent: ""
  title: ""
---
<brick:script>draft</brick:script>
<div>
<form id="f" brick:submit="save">
  <input name="title" value="{{ .State.title }}">
  <button type="submit" name="intent" value="publish" id="pub">Publish</button>
  <button name="intent" value="delete" id="del">Delete</button>
  <button id="plain">Save</button>
  <input type="submit" name="intent" value="draft" id="inp">
  <button type="button" id="cancel">Cancel</button>
  <button name="intent" value="x" id="off" disabled>Off</button>
  <input type="image" name="pos" id="img" src="/i.png">
</form>
<button name="intent" value="out" id="outside">Outside</button>
<span class="got">{{ .State.intent }}</span>
</div>`

func draftEngine(t *testing.T) *brick.Engine {
	t.Helper()
	sc := scripting{"draft": {
		// save stores the posted fields: the form map is the last argument.
		"save": func(_ context.Context, a ...any) (any, error) {
			s, _ := a[0].(map[string]any)
			f, _ := a[len(a)-1].(map[string]any)
			s["intent"], s["title"] = f["intent"], f["title"]
			if f["intent"] == nil {
				s["intent"] = "none"
			}
			return s, nil
		},
	}}
	return bricktest.Engine(t, brick.Options{Scripting: sc, States: brick.NewMemoryStates(0, 0)}, bricktest.Theme(
		"components/Draft.html", draftComp,
		"content/a.html", `<main><Draft /></main>`,
	))
}

// failMsg runs fn with a fake T and returns the message it failed with,
// or "" when it passed.
func failMsg(t *testing.T, fn func(testing.TB)) string {
	t.Helper()
	ft := &fakeT{}
	func() {
		defer func() {
			if r := recover(); r != nil && r != ft {
				panic(r)
			}
		}()
		fn(ft)
	}()
	return ft.failed
}

func TestSubmitWith(t *testing.T) {
	eng := draftEngine(t)
	for _, c := range []struct{ button, want string }{
		{"#pub", "publish"}, {"#del", "delete"}, {"#inp", "draft"}, {"#plain", "none"},
	} {
		live := bricktest.Live(t, eng, "content/a.html")
		d := live.Component("Draft")
		d.SubmitWith("#f", c.button, map[string]any{"title": "Hi"})
		d.AssertSet(t, "intent", c.want)
		d.AssertSet(t, "title", "Hi")
	}
	// Submit sends no button, as a press of Enter in a form whose first
	// submit button has no name would.
	live := bricktest.Live(t, eng, "content/a.html")
	live.Component("Draft").Submit("#f", nil)
	live.Component("Draft").AssertSet(t, "intent", "none")
}

func TestSubmitWithRefuses(t *testing.T) {
	eng := draftEngine(t)
	for _, c := range []struct{ button, why string }{
		{"#cancel", "not a submit button"},
		{"#off", "disabled"},
		{"#img", "image button"},
		{"#outside", "not inside the form"},
		{"#nope", ""},
	} {
		msg := failMsg(t, func(ft testing.TB) {
			bricktest.Live(ft, eng, "content/a.html").Component("Draft").SubmitWith("#f", c.button, nil)
		})
		if msg == "" || !strings.Contains(msg, c.why) {
			t.Errorf("SubmitWith(%s): %q, want a failure saying %q", c.button, msg, c.why)
		}
	}
}

func TestAssertViewHas(t *testing.T) {
	sc := scripting{
		"lib": {
			// card is the Card component's loader: it gets the call site's
			// props and returns more.
			"card": func(_ context.Context, a ...any) (any, error) {
				return map[string]any{"price": 42, "currency": "EUR"}, nil
			},
		},
		"tally": {
			"bump": func(_ context.Context, a ...any) (any, error) {
				s, _ := a[0].(map[string]any)
				s["n"] = toInt(s["n"]) + 1
				return s, nil
			},
		},
	}
	eng := bricktest.Engine(t, brick.Options{Scripting: sc, States: brick.NewMemoryStates(0, 0)}, bricktest.Theme(
		"s/lib.star", "lib",
		// The template never prints currency: only the view log shows
		// that the loader's value reached it.
		"components/Card.html", "---\nuse: {lib: ~/s/lib.star}\nload: lib.card\nprops:\n  title: string\n  sale: bool\n---\n<b class=\"{{ if .Props.sale }}sale{{ end }}\">{{ .Props.title }} {{ .Props.price }}</b>",
		"components/Tally.html", "---\nstate:\n  n: 0\n---\n<brick:script>tally</brick:script>\n<p>{{ .State.n }} <Card title=\"in tally\" /></p>",
		"content/a.html", `<main><Card title="Mug" sale /><Tally /></main>`,
	))
	live := bricktest.Live(t, eng, "content/a.html")
	// The latest Card render is the one inside the Tally island.
	live.AssertViewHas(t, "Card", "title", "in tally")
	first := live.Views("Card")[0]
	if first.Props["title"] != "Mug" || first.Props["sale"] != true || first.Loaded["currency"] != "EUR" {
		t.Fatalf("first Card view = %+v", first)
	}
	live.AssertViewHas(t, "Card", "currency", "EUR")
	live.AssertViewHas(t, "Card", "price", 42)

	n := len(live.Views("Card"))
	live.Component("Tally").Call("bump")
	if got := len(live.Views("Card")); got != n+1 {
		t.Fatalf("the action's re-render was not logged: %d views, want %d", got, n+1)
	}
	live.AssertViewHas(t, "Card", "title", "in tally")

	for _, c := range []struct {
		comp, key string
		want      any
		why       string
	}{
		{"Card", "title", "Mug", `saw title = "in tally"`},
		{"Card", "missing", 1, `rendered without "missing"`},
		{"Nope", "x", 1, "did not render"},
	} {
		msg := failMsg(t, func(ft testing.TB) { live.AssertViewHas(ft, c.comp, c.key, c.want) })
		if msg == "" || !strings.Contains(msg, c.why) {
			t.Errorf("AssertViewHas(%s, %s): %q, want a failure saying %q", c.comp, c.key, msg, c.why)
		}
	}
}

func TestLiveLazyInline(t *testing.T) {
	sc := scripting{"tally": {
		"bump": func(_ context.Context, a ...any) (any, error) {
			s, _ := a[0].(map[string]any)
			s["n"] = toInt(s["n"]) + 1
			return s, nil
		},
	}}
	eng := bricktest.Engine(t, brick.Options{Scripting: sc, States: brick.NewMemoryStates(0, 0)}, bricktest.Theme(
		"components/Chart.html", "---\nbrick: {lazy: visible}\n---\n<figure>chart <Inner brick:lazy /></figure>",
		"components/Inner.html", "<em>inner</em>",
		"components/Counter.html", "---\nstate:\n  n: 0\n---\n<brick:script>tally</brick:script>\n<p class=\"n\">{{ .State.n }}</p>",
		"content/a.html", `<main><Chart /><Counter brick:lazy="idle" /><Counter brick:lazy="stream" /></main>`,
	))
	// Without the option the page holds placeholders the runtime redeems.
	plain := bricktest.Live(t, eng, "content/a.html")
	if !strings.Contains(plain.HTML(), "data-brick-lazy=") || strings.Contains(plain.HTML(), "chart") {
		t.Fatalf("lazy islands rendered without WithLazyInline: %s", plain.HTML())
	}
	live := bricktest.Live(t, eng, "content/a.html", brick.WithLazyInline())
	h := live.HTML()
	if strings.Contains(h, "data-brick-lazy=") || !strings.Contains(h, "<figure>chart <em>inner</em></figure>") {
		t.Fatalf("WithLazyInline left a placeholder or lost content: %s", h)
	}
	// A lazy stateful island is a live component: its actions run.
	cs := live.Components("Counter")
	if len(cs) != 2 {
		t.Fatalf("counters = %d: %s", len(cs), h)
	}
	cs[0].Call("bump")
	cs[0].AssertSet(t, "n", 1)
	if got := cs[0].Text(); got != "1" {
		t.Fatalf("counter after bump = %q", got)
	}
}
