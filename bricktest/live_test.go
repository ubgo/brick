package bricktest_test

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ubgo/brick"
	"github.com/ubgo/brick/bricktest"
)

// scripting is a Go-written Scripting whose functions see the call ctx.
type scripting map[string]module

type module map[string]func(ctx context.Context, args ...any) (any, error)

func (m module) Functions() []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

func (m module) Call(ctx context.Context, fn string, args ...any) (any, error) {
	return m[fn](ctx, args...)
}

func (s scripting) Compile(_ context.Context, src brick.ScriptSource) (brick.ScriptModule, error) {
	m, ok := s[strings.TrimSpace(string(src.Src))]
	if !ok {
		return nil, fmt.Errorf("no module %q", src.Src)
	}
	return m, nil
}

func TestLiveDeliver(t *testing.T) {
	var total atomic.Int64
	total.Store(128)
	var pings atomic.Int64
	st := func(a []any) map[string]any { s, _ := a[0].(map[string]any); return s }
	sc := scripting{"app": {
		"remove": func(ctx context.Context, a ...any) (any, error) {
			total.Add(-1)
			eff := brick.EffectsFrom(ctx)
			if err := eff.Dispatch("contact-deleted", map[string]any{"id": a[2]}, ""); err != nil {
				return nil, err
			}
			return st(a), eff.Flash("Deleted Ada Lovelace.", brick.FlashSuccess)
		},
		"leave": func(ctx context.Context, a ...any) (any, error) {
			eff := brick.EffectsFrom(ctx)
			_ = eff.Dispatch("contact-deleted", nil, "")
			return st(a), eff.Navigate("/contacts")
		},
		"total": func(context.Context, ...any) (any, error) { return total.Load(), nil },
		// ping answers an event by dispatching it again: the cycle guard
		// stops the second round.
		"ping": func(ctx context.Context, a ...any) (any, error) {
			pings.Add(1)
			return st(a), brick.EffectsFrom(ctx).Dispatch("contact-deleted", nil, "")
		},
	}}
	eng := bricktest.Engine(t, brick.Options{Scripting: sc, States: brick.NewMemoryStates(0, 0)}, bricktest.Theme(
		"components/ContactsApp.html", "---\nstate: {}\n---\n<brick:script>app</brick:script>\n<div class=\"app\">rows</div>",
		"components/ContactCount.html", "---\nstate: {}\nbrick:\n  on:\n    contact-deleted: $refresh\n---\n<brick:script>app</brick:script>\n<span>{{ total }} contacts</span>",
		"components/Pinger.html", "---\nstate: {}\nbrick:\n  on:\n    contact-deleted: ping\n---\n<brick:script>app</brick:script>\n<b>p</b>",
		"content/a.html", `<main><ContactCount /><ContactsApp /><Pinger /></main>`,
	))
	live := bricktest.Live(t, eng, "content/a.html")
	res := live.Component("ContactsApp").Call("remove", 42)
	bricktest.AssertDispatched(t, res, "contact-deleted", map[string]any{"id": 42})
	bricktest.AssertFlash(t, res, brick.FlashSuccess, "Deleted Ada Lovelace.")
	bricktest.AssertNoRedirect(t, res)
	if got := live.Component("ContactCount").Text(); got != "128 contacts" {
		t.Fatalf("before delivery: %q", got)
	}
	got := live.Deliver(res)
	if c := live.Component("ContactCount").Text(); c != "127 contacts" {
		t.Fatalf("after delivery: %q", c)
	}
	// The count and the pinger ran once each; the pinger's own dispatch is
	// not delivered, because contact-deleted is already in its chain.
	if len(got) != 2 || pings.Load() != 1 {
		t.Fatalf("listener runs = %d, pings = %d", len(got), pings.Load())
	}

	// A terminal effect wakes no listener.
	res = live.Component("ContactsApp").Call("leave")
	bricktest.AssertNavigate(t, res, "/contacts")
	if got := live.Deliver(res); len(got) != 0 {
		t.Fatalf("listeners ran despite navigate: %d", len(got))
	}
}

// A parent's Call sends the children the page holds and splices each
// kept one back, as the runtime does.
func TestLiveKeepsNestedChildren(t *testing.T) {
	eng := bricktest.Engine(t, brick.Options{States: brick.NewMemoryStates(0, 0)}, bricktest.Theme(
		"components/Row.html", "---\nstate:\n  open: false\n---\n<div class=\"row\">{{ if .State.open }}open{{ else }}shut{{ end }}</div>",
		"components/Panel.html", "---\nstate:\n  n: 0\n---\n<section><i>{{ .State.n }}</i> <Row /></section>",
		"content/a.html", `<Panel />`,
	))
	live := bricktest.Live(t, eng, "content/a.html")
	live.Component("Row").Set("open", true)
	live.Component("Panel").Set("n", 1)
	if got := live.Component("Panel").Text(); got != "1 open" {
		t.Fatalf("child state lost across the parent's action: %q (%s)", got, live.HTML())
	}
	// The kept child still acts with its own handle.
	live.Component("Row").Set("open", false)
	if got := live.Component("Panel").Text(); got != "1 shut" {
		t.Fatalf("kept child no longer acts: %q", got)
	}
}

// Stream collects what an action streamed, in order.
func TestLiveStream(t *testing.T) {
	sc := scripting{"chat": {
		"ask": func(ctx context.Context, a ...any) (any, error) {
			s := brick.StreamFrom(ctx)
			_ = s.Write("answer", "Hel", false)
			_ = s.Write("answer", "Hello", true)
			st, _ := a[0].(map[string]any)
			st["answer"] = "Hello"
			return st, nil
		},
	}}
	eng := bricktest.Engine(t, brick.Options{Scripting: sc, States: brick.NewMemoryStates(0, 0)}, bricktest.Theme(
		"components/Chat.html", "---\nstate:\n  answer: \"\"\n---\n<brick:script>chat</brick:script>\n<p brick:ref=\"answer\">{{ .State.answer }}</p>",
		"content/a.html", `<Chat />`,
	))
	live := bricktest.Live(t, eng, "content/a.html")
	_, chunks := live.Component("Chat").Stream("ask")
	want := []bricktest.StreamChunk{{Ref: "answer", Text: "Hel"}, {Ref: "answer", Text: "Hello", Replace: true}}
	if fmt.Sprint(chunks) != fmt.Sprint(want) || live.Component("Chat").Text() != "Hello" {
		t.Fatalf("chunks %v text %q", chunks, live.Component("Chat").Text())
	}
}
