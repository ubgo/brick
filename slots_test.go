package brick

import (
	"context"
	"strings"
	"testing"
)

// slotTheme is a stateful Modal wrapping the caller's children and a
// named block: the everyday case.
func slotTheme(extra ...string) []string {
	return append([]string{
		"components/Modal.html", "---\nstate:\n  open: false\n---\n<dialog><b>{{ .State.open }}</b>{{ node \"title\" }}{{ children }}</dialog>",
		"content/a.html", "---\nuser: Ada\n---\n<Modal><brick:prop name=\"title\"><h2>Hi {{ .Params.user }}</h2></brick:prop><form>{{ .Params.user }}</form></Modal>",
	}, extra...)
}

// Children and blocks survive an action: the re-render runs from no caller
// scope, so it must emit the HTML stored when the page rendered.
func TestSlotsSurviveAction(t *testing.T) {
	t.Parallel()
	e := newEngine(t, Options{States: NewMemoryStates(0, 0), FS: theme(slotTheme()...)})
	ctx := context.Background()
	out, err := e.Render(ctx, "content/a.html", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"<form>Ada</form>", "<h2>Hi Ada</h2>"} {
		if !strings.Contains(string(out), want) {
			t.Fatalf("first render lacks %s: %s", want, out)
		}
	}
	h := stateRE.FindStringSubmatch(string(out))
	out, err = e.Action(ctx, ActionRequest{Handle: h[1], Token: h[2], Action: "$set", Args: []any{"open", true}})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"<b>true</b>", "<form>Ada</form>", "<h2>Hi Ada</h2>"} {
		if !strings.Contains(string(out), want) {
			t.Fatalf("re-render lacks %s: %s", want, out)
		}
	}
}

// actOnce renders a page, runs one $set on its first stateful root and
// returns both outputs.
func actOnce(t *testing.T, e *Engine, page, key string, v any) (first, after string) {
	t.Helper()
	ctx := context.Background()
	out, err := e.Render(ctx, page, nil)
	if err != nil {
		t.Fatal(err)
	}
	h := stateRE.FindStringSubmatch(string(out))
	if h == nil {
		t.Fatalf("no stateful root: %s", out)
	}
	re, err := e.Action(ctx, ActionRequest{Handle: h[1], Token: h[2], Action: "$set", Args: []any{key, v}})
	if err != nil {
		t.Fatal(err)
	}
	return string(out), string(re)
}

// The slots ride inside a sealed handle: the action opens the blob and
// emits them, with no server-side memory of the page.
func TestSlotsSealed(t *testing.T) {
	t.Parallel()
	e := newEngine(t, Options{States: sealedStore(t, sealedOpts{}), FS: theme(slotTheme()...)})
	_, after := actOnce(t, e, "content/a.html", "open", true)
	for _, want := range []string{"<b>true</b>", "<form>Ada</form>", "<h2>Hi Ada</h2>"} {
		if !strings.Contains(after, want) {
			t.Fatalf("sealed re-render lacks %s: %s", want, after)
		}
	}
}

// The codec carries Slots and omits the field when there are none, so
// instances stored before slots existed encode the same.
func TestSlotsCodecRoundTrip(t *testing.T) {
	t.Parallel()
	inst := &Instance{Token: "t", Path: "components/Modal.html", Name: "Modal", State: map[string]any{}, Slots: map[string]string{"children": `<form a="1">x</form>`, "title": "", "z": "<i>é</i>"}}
	b, err := EncodeInstance(inst)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeInstance(b)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Slots) != 3 || got.Slots["children"] != inst.Slots["children"] || got.Slots["z"] != inst.Slots["z"] {
		t.Fatalf("slots: %#v", got.Slots)
	}
	if _, ok := got.Slots["title"]; !ok {
		t.Fatal("an empty block must survive: hasNode reads its presence")
	}
	inst.Slots = nil
	b, err = EncodeInstance(inst)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "slots") {
		t.Fatalf("no slots must encode no field: %s", b)
	}
	cp := copyInstance(&Instance{Slots: map[string]string{"children": "a"}})
	orig := &Instance{Slots: map[string]string{"children": "a"}}
	cp2 := copyInstance(orig)
	cp2.Slots["children"] = "b"
	if orig.Slots["children"] != "a" || cp.Slots["children"] != "a" {
		t.Fatal("copyInstance must not share the slots map")
	}
}

// hasNode still sees the caller's children and blocks after an action, and
// a block the caller did not write stays absent.
func TestSlotsHasNodeAfterAction(t *testing.T) {
	t.Parallel()
	e := newEngine(t, Options{States: NewMemoryStates(0, 0), FS: theme(
		"components/Modal.html", "---\nstate:\n  open: false\n---\n<dialog>{{ .State.open }}|{{ hasNode \"children\" }}|{{ hasNode \"title\" }}|{{ hasNode \"footer\" }}</dialog>",
		"content/a.html", `<Modal><brick:prop name="title">T</brick:prop>body</Modal>`,
	)})
	first, after := actOnce(t, e, "content/a.html", "open", true)
	if !strings.Contains(first, "false|true|true|false") || !strings.Contains(after, "true|true|true|false") {
		t.Fatalf("hasNode:\n%s\n%s", first, after)
	}
}

// A stateful component inside another one, given children by its parent's
// template: its own action re-renders it without the parent, so it must
// keep the children the parent rendered.
func TestSlotsNestedStatefulChild(t *testing.T) {
	t.Parallel()
	e := newEngine(t, Options{States: NewMemoryStates(0, 0), FS: theme(
		"components/Modal.html", "---\nstate:\n  open: false\n---\n<dialog><b>{{ .State.open }}</b>{{ children }}</dialog>",
		"components/Page.html", "---\nstate:\n  who: Ada\n---\n<main><Modal><p>{{ .State.who }}</p></Modal></main>",
		"content/a.html", `<Page />`,
	)})
	ctx := context.Background()
	out, err := e.Render(ctx, "content/a.html", nil)
	if err != nil {
		t.Fatal(err)
	}
	roots := stateRE.FindAllStringSubmatch(string(out), -1)
	if len(roots) != 2 || !strings.Contains(string(out), "<p>Ada</p>") {
		t.Fatalf("first render: %s", out)
	}
	child := roots[1]
	re, err := e.Action(ctx, ActionRequest{Handle: child[1], Token: child[2], Action: "$set", Args: []any{"open", true}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(re), "<b>true</b>") || !strings.Contains(string(re), "<p>Ada</p>") {
		t.Fatalf("child's own action lost the parent's children: %s", re)
	}
	// The parent's action re-renders the child from its live template,
	// so a change in what the parent writes reaches the child.
	parent := roots[0]
	re, err = e.Action(ctx, ActionRequest{Handle: parent[1], Token: parent[2], Action: "$set", Args: []any{"who", "Bo"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(re), "<p>Bo</p>") {
		t.Fatalf("parent's re-render must refresh the child's slot: %s", re)
	}
	child = stateRE.FindAllStringSubmatch(string(re), -1)[1]
	re, err = e.Action(ctx, ActionRequest{Handle: child[1], Token: child[2], Action: "$set", Args: []any{"open", false}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(re), "<p>Bo</p>") {
		t.Fatalf("child's instance must store the refreshed slot: %s", re)
	}
}

// A plain island (no state) takes children too: they are rendered with the
// page and shown in every request's fill, around per-request content.
func TestSlotsIslandFillsPerRequest(t *testing.T) {
	t.Parallel()
	e := newEngine(t, Options{FS: theme(
		"components/Hello.html", "---\nbrick:\n  island: true\n---\n<span>{{ with .Ctx.user }}{{ . }}{{ else }}guest{{ end }}:{{ children }}:{{ node \"tail\" }}</span>",
		"content/a.html", "---\nnote: kept\n---\n<Hello><em>{{ .Params.note }}</em><brick:prop name=\"tail\">T</brick:prop></Hello>",
	)})
	ctx := context.Background()
	r, err := e.RenderChunks(ctx, "content/a.html", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		opts []RenderOption
		want string
	}{
		{[]RenderOption{Provide("user", "Sara")}, "<span>Sara:<em>kept</em>:T</span>"},
		{nil, "<span>guest:<em>kept</em>:T</span>"},
	} {
		out, err := e.Fill(ctx, r, tc.opts...)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(out), tc.want) {
			t.Fatalf("fill: want %s in %s", tc.want, out)
		}
	}
}

// <brick:render> chooses a component from data, so the compile checks
// cannot see it: its children reach a stateful component (they were
// silently dropped before), and a stateful component it puts inside a
// slot is refused at render.
func TestSlotsDynamicRender(t *testing.T) {
	t.Parallel()
	e := newEngine(t, Options{States: NewMemoryStates(0, 0), FS: theme(
		"components/Modal.html", "---\nstate:\n  open: false\n---\n<dialog>{{ children }}</dialog>",
		"content/a.html", "<brick:render :name=\"`Modal`\"><form>x</form></brick:render>",
		"content/b.html", "<Modal><brick:render :name=\"`Modal`\" /></Modal>",
	)})
	_, after := actOnce(t, e, "content/a.html", "open", true)
	if !strings.Contains(after, "<form>x</form>") {
		t.Fatalf("dynamic call lost its children: %s", after)
	}
	if err := renderErr(t, e, "content/b.html"); err.Line != 1 || !strings.Contains(err.Error(), "inside the children of another one") {
		t.Fatalf("dynamic stateful inside a slot: %v", err)
	}
}

// Each slot compile rule fails with the file and line.
func TestSlotsCompileErrors(t *testing.T) {
	t.Parallel()
	head := "---\nstate:\n  open: false\n---\n"
	for _, tc := range []struct {
		name, file, src, want string
		line                  int
	}{
		{"valued node", "components/M.html", head + "<p>\n{{ node \"row\" .State }}</p>", "{{ node }} with a value", 6},
		{"valued node, trimmed", "components/M.html", head + "<div>{{- node \"row\" 1 -}}</div>", "{{ node }} with a value", 5},
		{".Children", "components/M.html", head + "<ul>\n{{ range .Children }}{{ child . }}{{ end }}</ul>", ".Children and {{ child }}", 6},
		{"stateful in slot", "content/p.html", "<M>\n  <p><M /></p>\n</M>", "inside the children of another one", 2},
		{"stateful never rendered", "content/p.html", "<M>{{ if false }}\n<M />{{ end }}</M>", "inside the children of another one", 2},
		{"stateful in a block", "content/p.html", "<M><brick:prop name=\"x\">\n<M /></brick:prop></M>", "inside the children of another one", 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			files := []string{"components/M.html", head + "<div>{{ children }}</div>", "content/p.html", "<M />"}
			for i := 0; i < len(files); i += 2 {
				if files[i] == tc.file {
					files[i+1] = tc.src
				}
			}
			e := newEngine(t, Options{States: NewMemoryStates(0, 0), FS: theme(files...)})
			err := renderErr(t, e, "content/p.html")
			if err.File != tc.file || err.Line != tc.line || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %s:%d %v", err.File, err.Line, err)
			}
		})
	}
	// What stays allowed: a plain node, hasNode, a valued node inside a
	// local component, and a non-stateful component in the slot.
	e := newEngine(t, Options{States: NewMemoryStates(0, 0), FS: theme(
		"components/M.html", head+"<brick:component name=\"Row\">{{ node \"r\" 1 }}</brick:component><div>{{ if hasNode \"t\" }}{{ node \"t\" }}{{ end }}{{ children }}</div>",
		"components/Tag.html", "<i>{{ children }}</i>",
		"content/p.html", "<M><brick:prop name=\"t\">T</brick:prop><Tag>x</Tag></M>",
	)})
	if out := render(t, e, "content/p.html", nil); !strings.Contains(out, "T<i>x</i>") {
		t.Fatalf("allowed forms: %s", out)
	}
}

// Stored slot HTML counts toward the state size: the Dev warning sees it,
// and a sealed store's MaxBytes refuses it at render.
func TestSlotsCountTowardStateSize(t *testing.T) {
	t.Parallel()
	big := strings.Repeat("<p>row</p>", (devStateWarnBytes/10)+100)
	files := theme(
		"components/Modal.html", "---\nstate:\n  open: false\n---\n<dialog>{{ children }}</dialog>",
		"content/a.html", "<Modal>"+big+"</Modal>",
	)
	dev := newEngine(t, Options{Mode: Dev, States: NewMemoryStates(0, 0), FS: files})
	render(t, dev, "content/a.html", nil)
	if w := dev.Warnings(); len(w) != 1 || !strings.Contains(w[0].Message, "KiB of state") {
		t.Fatalf("size warning: %v", w)
	}
	sealed := newEngine(t, Options{States: sealedStore(t, sealedOpts{SealedOptions: SealedOptions{MaxBytes: 4 << 10}}), FS: files})
	if _, err := sealed.Render(context.Background(), "content/a.html", nil); err == nil || !strings.Contains(err.Error(), "MaxBytes") {
		t.Fatalf("sealed cap: %v", err)
	}
}
