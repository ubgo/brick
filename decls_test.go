package brick

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// Tests for frontmatter declarations: declared slots, prop and slot docs
// with ComponentDocs, and temporary state.
// BV in a comment marks a break-verified test.

// slotCard declares a required title block, an optional footer and a
// required body.
const slotCard = `---
props:
  tone:
    type: string
    default: plain
    oneof: [plain, loud]
    doc: How loud the card is.
    examples: [plain, loud]
brick:
  slots:
    title: {required: true, doc: The heading., examples: ["Welcome"]}
    footer:
    children: {required: true}
---
<article class="{{ .Props.tone }}">{{ node "title" }}{{ children }}{{ node "footer" }}</article>`

// TestSlotDeclaredRender: a call that gives every required slot renders as
// before; declaring slots changes nothing about rendering.
func TestSlotDeclaredRender(t *testing.T) {
	t.Parallel()
	e := newEngine(t, Options{FS: theme(
		"components/Card.html", slotCard,
		"content/a.html", `<Card><brick:prop name="title"><h2>T</h2></brick:prop>body<brick:prop name="footer"><i>f</i></brick:prop></Card>`,
	)})
	out := render(t, e, "content/a.html", nil)
	if want := `<article class="plain"><h2>T</h2>body<i>f</i></article>`; !strings.Contains(out, want) {
		t.Fatalf("got %s, want %s", out, want)
	}
}

// TestSlotRequiredMissing: a required block left out fails at compile with
// the caller's file and line. BV: dropping the Required check in
// checkDeclaredSlots makes this test fail.
func TestSlotRequiredMissing(t *testing.T) {
	t.Parallel()
	e := newEngine(t, Options{FS: theme(
		"components/Card.html", slotCard,
		"content/a.html", "<p>x</p>\n<Card>body</Card>",
	)})
	err := renderErr(t, e, "content/a.html")
	if err.File != "content/a.html" || err.Line != 2 || !strings.Contains(err.Error(), `<Card> requires a <brick:prop name="title"> block`) {
		t.Fatalf("got %v (file %s line %d)", err, err.File, err.Line)
	}
}

// TestSlotRequiredChildren: `children` names the call's body.
func TestSlotRequiredChildren(t *testing.T) {
	t.Parallel()
	e := newEngine(t, Options{FS: theme(
		"components/Card.html", slotCard,
		"content/a.html", `<Card><brick:prop name="title">T</brick:prop></Card>`,
	)})
	err := renderErr(t, e, "content/a.html")
	if !strings.Contains(err.Error(), "<Card> requires children") {
		t.Fatalf("got %v", err)
	}
}

// TestSlotUndeclared: a block the component does not declare fails at
// compile, pointing at the block. BV: removing the undeclared-block loop
// in checkDeclaredSlots makes this test fail.
func TestSlotUndeclared(t *testing.T) {
	t.Parallel()
	e := newEngine(t, Options{FS: theme(
		"components/Card.html", slotCard,
		"content/a.html", "<Card><brick:prop name=\"title\">T</brick:prop>body\n  <brick:prop name=\"aside\">a</brick:prop></Card>",
	)})
	err := renderErr(t, e, "content/a.html")
	if err.Line != 2 || !strings.Contains(err.Error(), `<brick:prop name="aside">: <Card> declares no such slot (declared blocks: footer, title)`) {
		t.Fatalf("got %v (line %d)", err, err.Line)
	}
}

// TestSlotUndeclaredComponentUnchecked: without brick: {slots:} any block
// is accepted, as before slots could be declared.
func TestSlotUndeclaredComponentUnchecked(t *testing.T) {
	t.Parallel()
	e := newEngine(t, Options{FS: theme(
		"components/Box.html", `<div>{{ node "anything" }}</div>`,
		"content/a.html", `<Box><brick:prop name="anything">ok</brick:prop><brick:prop name="other">x</brick:prop></Box>`,
	)})
	if out := render(t, e, "content/a.html", nil); !strings.Contains(out, "<div>ok</div>") {
		t.Fatalf("got %s", out)
	}
}

// TestDeclBadShapes: malformed slot, doc and temporary declarations fail
// loudly instead of being ignored.
func TestDeclBadShapes(t *testing.T) {
	t.Parallel()
	cases := map[string]struct{ head, want string }{
		"slot setting":    {"brick:\n  slots:\n    title: {requird: true}", `brick: slots: title: unknown setting "requird"`},
		"slot shape":      {"brick:\n  slots: [title]", "brick: slots: must map block names"},
		"slot required":   {"brick:\n  slots:\n    title: {required: yes please}", "brick: slots: title: required must be true or false"},
		"prop doc":        {"props:\n  n: {type: int, doc: [x]}", "props: n: doc must be text"},
		"prop examples":   {"props:\n  n: {type: int, examples: 3}", "props: n: examples must be a list"},
		"temp no state":   {"brick:\n  temporary: [rows]", "brick: temporary: needs state:"},
		"temp undeclared": {"state:\n  rows: []\nbrick:\n  temporary: [rowz]", "brick: temporary: rowz is not a top-level field of state:"},
		"temp shape":      {"state:\n  rows: []\nbrick:\n  temporary: rows", "brick: temporary: must list state fields"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			e := newEngine(t, Options{States: NewMemoryStates(0, 0), FS: theme(
				"components/Card.html", "---\n"+tc.head+"\n---\n<div>x</div>",
				"content/a.html", `<Card />`,
			)})
			err := renderErr(t, e, "content/a.html")
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want %q", err, tc.want)
			}
		})
	}
}

// TestComponentDocs: every registered component, sorted, with its props
// and slots as declared, docs included.
func TestComponentDocs(t *testing.T) {
	t.Parallel()
	e := newEngine(t, Options{States: NewMemoryStates(0, 0), FS: theme(
		"components/Card.html", slotCard,
		"components/Box.html", `<div></div>`,
		"components/Counter.html", "---\nprops:\n  step: {type: int, required: true}\nstate:\n  n: 0\n---\n<b>{{ .State.n }}</b>",
	)})
	docs, err := e.ComponentDocs()
	if err != nil {
		t.Fatal(err)
	}
	got, _ := json.Marshal(docs)
	want := `[{"name":"Box","path":"components/Box.html"},` +
		`{"name":"Card","path":"components/Card.html","props":[{"name":"tone","type":"string","default":"plain","hasDefault":true,"oneof":["plain","loud"],"doc":"How loud the card is.","examples":["plain","loud"]}],` +
		`"slots":[{"name":"children","required":true},{"name":"footer"},{"name":"title","required":true,"doc":"The heading.","examples":["Welcome"]}]},` +
		`{"name":"Counter","path":"components/Counter.html","stateful":true,"island":true,"props":[{"name":"step","type":"int","required":true}]}]`
	if string(got) != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
}

// TestComponentDocsBadFile: a component whose frontmatter is wrong fails
// ComponentDocs with its file, rather than vanishing from the docs.
func TestComponentDocsBadFile(t *testing.T) {
	t.Parallel()
	e := newEngine(t, Options{FS: theme(
		"components/Card.html", "---\nprops:\n  n: {doc: 3}\n---\n<div></div>",
	)})
	_, err := e.ComponentDocs()
	if err == nil || !strings.Contains(err.Error(), "components/Card.html") || !strings.Contains(err.Error(), "doc must be text") {
		t.Fatalf("got %v", err)
	}
}

// temporaryTheme is a stateful list whose rows are temporary.
func temporaryTheme(temp string) []string {
	return []string{
		"components/Feed.html", "---\nstate:\n  rows: []\n  page: 1\nbrick:\n" + temp + "\n---\n<ul data-page=\"{{ .State.page }}\">{{ range .State.rows }}<li>{{ . }}</li>{{ end }}</ul>",
		"content/a.html", `<Feed />`,
	}
}

// TestTemporaryState: the action's value renders, the stored instance holds
// the declared value, and the next action starts from it. BV: returning
// inst unchanged from withoutTemporary makes this test fail.
func TestTemporaryState(t *testing.T) {
	t.Parallel()
	states := NewMemoryStates(0, 0)
	e := newEngine(t, Options{States: states, FS: theme(temporaryTheme("  temporary: [rows]")...)})
	ctx := context.Background()
	out, err := e.Render(ctx, "content/a.html", nil)
	if err != nil {
		t.Fatal(err)
	}
	h := stateRE.FindStringSubmatch(string(out))
	re, err := e.Action(ctx, ActionRequest{Handle: h[1], Token: h[2], Action: "$set", Args: []any{"rows", []any{"a", "b"}}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(re), "<li>a</li><li>b</li>") {
		t.Fatalf("the action's rows do not render: %s", re)
	}
	stored := map[string]any{}
	_ = states.Each(ctx, func(inst *Instance) bool { stored = inst.State; return true })
	if rows, _ := stored["rows"].([]any); len(rows) != 0 {
		t.Fatalf("stored rows = %v, want the declared empty list", stored["rows"])
	}
	h = stateRE.FindStringSubmatch(string(re))
	re, err = e.Action(ctx, ActionRequest{Handle: h[1], Token: h[2], Action: "$set", Args: []any{"page", 2}})
	if err != nil {
		t.Fatal(err)
	}
	if s := string(re); strings.Contains(s, "<li>") || !strings.Contains(s, `data-page="2"`) {
		t.Fatalf("next action should start from the declared rows and keep page: %s", s)
	}
}

// TestTemporaryStateOff: without temporary: the rows are kept, so the
// reset above is the declaration's doing.
func TestTemporaryStateOff(t *testing.T) {
	t.Parallel()
	e := newEngine(t, Options{States: NewMemoryStates(0, 0), FS: theme(temporaryTheme("  store: default")...)})
	ctx := context.Background()
	out, err := e.Render(ctx, "content/a.html", nil)
	if err != nil {
		t.Fatal(err)
	}
	h := stateRE.FindStringSubmatch(string(out))
	re, err := e.Action(ctx, ActionRequest{Handle: h[1], Token: h[2], Action: "$set", Args: []any{"rows", []any{"a"}}})
	if err != nil {
		t.Fatal(err)
	}
	h = stateRE.FindStringSubmatch(string(re))
	re, err = e.Action(ctx, ActionRequest{Handle: h[1], Token: h[2], Action: "$set", Args: []any{"page", 2}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(re), "<li>a</li>") {
		t.Fatalf("rows should be kept without temporary: %s", re)
	}
}
