package brick

import (
	"context"
	"regexp"
	"strings"
	"testing"
)

// nestedTheme is a stateful Panel holding a stateful Counter: the shape of
// nested stateful components (an accordion inside a list, a form inside a
// dialog).
func nestedTheme(counter string) []string {
	return []string{
		"components/Counter.html", counter,
		"components/Panel.html", "---\nstate:\n  n: 0\n---\n<section><i>{{ .State.n }}</i><Counter /></section>",
		"content/a.html", `<Panel />`,
	}
}

// dataBrickOf returns the data-brick id of the root carrying handle.
func dataBrickOf(t *testing.T, page, handle string) string {
	t.Helper()
	m := regexp.MustCompile(`data-brick="([^"]+)" data-brick-state="` + regexp.QuoteMeta(handle) + `"`).FindStringSubmatch(page)
	if m == nil {
		t.Fatalf("no data-brick id for %s: %s", handle, page)
	}
	return m[1]
}

// renderNested renders the page and returns the parent's and the child's
// handle and token, in document order.
func renderNested(t *testing.T, e *Engine) (parent, child []string, page string) {
	t.Helper()
	out, err := e.Render(context.Background(), "content/a.html", nil)
	if err != nil {
		t.Fatal(err)
	}
	roots := stateRE.FindAllStringSubmatch(string(out), -1)
	if len(roots) != 2 {
		t.Fatalf("want two stateful roots: %s", out)
	}
	return roots[0][1:], roots[1][1:], string(out)
}

// A parent's action re-renders the parent, not its stateful children: a
// child the runtime says it already has (children) is written as a
// brick-keep placeholder, and its state survives.
func TestNestedStatefulChildKeepsState(t *testing.T) {
	t.Parallel()
	e := newEngine(t, Options{States: NewMemoryStates(0, 0), Scripting: counterScripting(), FS: theme(nestedTheme(counterFile)...)})
	ctx := context.Background()
	parent, child, page := renderNested(t, e)
	childID := dataBrickOf(t, page, child[0])

	// Act on the child: its count becomes 7.
	out, err := e.Action(ctx, ActionRequest{Handle: child[0], Token: child[1], Action: "$set", Args: []any{"count", 7}})
	if err != nil {
		t.Fatal(err)
	}
	child = stateRE.FindStringSubmatch(string(out))[1:]

	// Act on the parent, telling it which child the page holds.
	out, err = e.Action(ctx, ActionRequest{Handle: parent[0], Token: parent[1], Action: "$set", Args: []any{"n", 1},
		Children: map[string]ChildRef{childID: {Handle: child[0], Token: child[1]}}})
	if err != nil {
		t.Fatal(err)
	}
	if want := `<brick-keep data-brick="` + childID + `"></brick-keep>`; !strings.Contains(string(out), want) {
		t.Fatalf("parent re-render must keep the child (%s): %s", want, out)
	}
	if strings.Count(string(out), "data-brick-state=") != 1 {
		t.Fatalf("a kept child is not rendered: %s", out)
	}
	if !strings.Contains(string(out), "<i>1</i>") {
		t.Fatalf("parent did not re-render: %s", out)
	}
	// The child's instance is untouched: its next action starts from 7.
	out, err = e.Action(ctx, ActionRequest{Handle: child[0], Token: child[1], Action: "increment"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "<span>8</span>") {
		t.Fatalf("child state lost: %s", out)
	}

	// An older client that sends no children gets today's behaviour: the
	// child renders fresh.
	if out := mustAction(t, e, parent, "$set", "n", 2); !strings.Contains(string(out), "<span>0</span>") || strings.Contains(string(out), "brick-keep") {
		t.Fatalf("no children: the child renders fresh: %s", out)
	}
}

func mustAction(t *testing.T, e *Engine, root []string, action string, args ...any) []byte {
	t.Helper()
	out, err := e.Action(context.Background(), ActionRequest{Handle: root[0], Token: root[1], Action: action, Args: args})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// rowTheme is a stateful Panel passing a reactive label to a stateful Row
// that has its own open/closed state.
func rowTheme(page string) []string {
	return []string{
		"components/Row.html", "---\nstate:\n  open: false\nprops:\n  label: {type: string, reactive: true}\n---\n<div class=\"row\"><b>{{ .Props.label }}</b>{{ if .State.open }}<em>open</em>{{ end }}</div>",
		"components/Panel.html", "---\nstate:\n  label: a\n  n: 0\n---\n<section><i>{{ .State.n }}</i><Row :label=\".State.label\" /></section>",
		"content/a.html", page,
	}
}

// keepAction acts on the parent, sending child as its only kept child.
func keepAction(t *testing.T, e *Engine, parent, child []string, childID, action string, args []any, opts ...RenderOption) []byte {
	t.Helper()
	out, err := e.Action(context.Background(), ActionRequest{Handle: parent[0], Token: parent[1], Action: action, Args: args,
		Children: map[string]ChildRef{childID: {Handle: child[0], Token: child[1]}}}, opts...)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// A changed reactive prop re-renders the kept child with its state and the
// new value; an unchanged one keeps it. Both store
// kinds: the sealed store's handle changes on every save.
func TestReactivePropRerendersWithState(t *testing.T) {
	t.Parallel()
	stores := map[string]func(t *testing.T) StateStore{
		"memory": func(*testing.T) StateStore { return NewMemoryStates(0, 0) },
		"sealed": func(t *testing.T) StateStore { return sealedStore(t, sealedOpts{}) },
	}
	for name, mk := range stores {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			e := newEngine(t, Options{States: mk(t), FS: theme(rowTheme(`<Panel />`)...)})
			parent, child, page := renderNested(t, e)
			childID := dataBrickOf(t, page, child[0])
			child = stateRE.FindStringSubmatch(string(mustAction(t, e, child, "$set", "open", true)))[1:]

			out := keepAction(t, e, parent, child, childID, "$set", []any{"label", "b"})
			if strings.Contains(string(out), "brick-keep") || !strings.Contains(string(out), "<b>b</b><em>open</em>") {
				t.Fatalf("changed reactive prop: want the child re-rendered, state kept: %s", out)
			}
			m := stateRE.FindAllStringSubmatch(string(out), -1)
			if len(m) != 2 || m[1][2] != child[1] {
				t.Fatalf("the child keeps its instance (same token): %s", out)
			}
			parent, child = m[0][1:], m[1][1:]
			// The child's own next action renders with the new label.
			if out := mustAction(t, e, child, "$refresh"); !strings.Contains(string(out), "<b>b</b><em>open</em>") {
				t.Fatalf("stored props not updated: %s", out)
			}

			out = keepAction(t, e, parent, child, childID, "$set", []any{"n", 1})
			if !strings.Contains(string(out), `<brick-keep data-brick="`+childID+`">`) {
				t.Fatalf("unchanged reactive prop: want a keep: %s", out)
			}
		})
	}
}

// ★ children is untrusted: an id the re-render does
// not produce changes nothing, and a reactive child whose handle does not
// verify — a wrong token, another component's handle, another session's
// instance — renders fresh and is never adopted.
func TestChildrenUntrusted(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newEngine(t, Options{States: NewMemoryStates(0, 0), FS: theme(rowTheme(`<Panel />`)...)})
	alice := WithSession("alice")
	render := func(opts ...RenderOption) (parent, child []string, id string) {
		out, err := e.Render(ctx, "content/a.html", nil, opts...)
		if err != nil {
			t.Fatal(err)
		}
		roots := stateRE.FindAllStringSubmatch(string(out), -1)
		return roots[0][1:], roots[1][1:], dataBrickOf(t, string(out), roots[1][1])
	}
	parent, child, childID := render(alice)
	child = stateRE.FindStringSubmatch(string(mustActionAs(t, e, child, alice, "$set", "open", true)))[1:]

	// An id this render does not produce: ignored, the child renders as
	// without children.
	out, err := e.Action(ctx, ActionRequest{Handle: parent[0], Token: parent[1], Action: "$refresh",
		Children: map[string]ChildRef{"b0000000": {Handle: child[0], Token: child[1]}}}, alice)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "brick-keep") || strings.Contains(string(out), "<em>open</em>") {
		t.Fatalf("unknown child id must be ignored: %s", out)
	}

	_, bobChild, _ := render(WithSession("bob"))
	forged := map[string][]string{
		"wrong token":       {child[0], strings.Repeat("0", 32)},
		"another component": parent,
		"another session":   bobChild,
	}
	for name, ref := range forged {
		out := keepAction(t, e, parent, ref, childID, "$set", []any{"label", name}, alice)
		m := stateRE.FindAllStringSubmatch(string(out), -1)
		if strings.Contains(string(out), "brick-keep") || strings.Contains(string(out), "<em>open</em>") || len(m) != 2 || m[1][1] == ref[0] || m[1][2] == ref[1] {
			t.Fatalf("%s: a handle that does not verify must render fresh, never be adopted: %s", name, out)
		}
	}
	// The real child is untouched by all of it.
	if out := mustActionAs(t, e, child, alice, "$refresh"); !strings.Contains(string(out), "<b>a</b><em>open</em>") {
		t.Fatalf("real child changed: %s", out)
	}
}

func mustActionAs(t *testing.T, e *Engine, root []string, opt RenderOption, action string, args ...any) []byte {
	t.Helper()
	out, err := e.Action(context.Background(), ActionRequest{Handle: root[0], Token: root[1], Action: action, Args: args}, opt)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// Only the root's direct children are kept: a grandchild id in children
// is ignored, because its parent renders fresh and speaks for it.
func TestKeepOnlyDirectChildren(t *testing.T) {
	t.Parallel()
	e := newEngine(t, Options{States: NewMemoryStates(0, 0), Scripting: counterScripting(), FS: theme(
		"components/Counter.html", counterFile,
		"components/Mid.html", "---\nstate:\n  m: 0\n---\n<article><Counter /></article>",
		"components/Panel.html", "---\nstate:\n  n: 0\n---\n<section><Mid /></section>",
		"content/a.html", `<Panel />`,
	)})
	out, err := e.Render(context.Background(), "content/a.html", nil)
	if err != nil {
		t.Fatal(err)
	}
	roots := stateRE.FindAllStringSubmatch(string(out), -1)
	if len(roots) != 3 {
		t.Fatalf("want three roots: %s", out)
	}
	grand := roots[2][1:]
	res := keepAction(t, e, roots[0][1:], grand, dataBrickOf(t, string(out), grand[0]), "$refresh", nil)
	if strings.Contains(string(res), "brick-keep") || strings.Count(string(res), "data-brick-state=") != 3 {
		t.Fatalf("a grandchild is not the root's to keep: %s", res)
	}
}

// reactive: needs state:, and must be a boolean.
func TestReactivePropDeclaration(t *testing.T) {
	t.Parallel()
	for name, file := range map[string]string{
		"stateless": "---\nprops:\n  x: {type: string, reactive: true}\n---\n<p></p>",
		"not bool":  "---\nstate: {}\nprops:\n  x: {type: string, reactive: yes please}\n---\n<p></p>",
	} {
		e := newEngine(t, Options{States: NewMemoryStates(0, 0), FS: theme("components/X.html", file, "content/a.html", `<X x="1" />`)})
		if _, err := e.Render(context.Background(), "content/a.html", nil); err == nil || !strings.Contains(err.Error(), "reactive") {
			t.Fatalf("%s: want a reactive: error, got %v", name, err)
		}
	}
}

// A stateful child repeated under a stateful parent without key= gets a
// Dev warning: its id is its position.
func TestRangeStatefulChildNeedsKey(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		call string
		warn bool
	}{{`<Row :label="." />`, true}, {`<Row :label="." :key="." />`, false}} {
		e := newEngine(t, Options{Mode: Dev, States: NewMemoryStates(0, 0), FS: theme(
			"components/Row.html", "---\nstate:\n  open: false\nprops:\n  label: string\n---\n<li>{{ .Props.label }}</li>",
			"components/List.html", "---\nstate: {}\n---\n<ul>{{ range (list \"a\" \"b\") }}"+tc.call+"{{ end }}</ul>",
			"content/a.html", `<List />`,
		)})
		if _, err := e.Render(context.Background(), "content/a.html", nil); err != nil {
			t.Fatal(err)
		}
		got := false
		for _, d := range e.Warnings() {
			got = got || strings.Contains(d.Message, "give it key=")
		}
		if got != tc.warn {
			t.Fatalf("%s: warning %v, want %v: %v", tc.call, got, tc.warn, e.Warnings())
		}
	}
}
