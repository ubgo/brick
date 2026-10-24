package brick

import (
	"context"
	"strings"
	"testing"
)

// modelTheme is a stateful Cart binding its qty to a modelable Stepper's
// value (v22, modelable.go).
func modelTheme(stepper string) []string {
	return []string{
		"components/Stepper.html", stepper,
		"components/Cart.html", "---\nstate:\n  qty: 3\n  n: 0\n---\n<section><i>{{ .State.n }}</i><Stepper brick:model=\"qty\" /></section>",
		"content/a.html", `<Cart />`,
	}
}

const stepperFile = "---\nstate:\n  value: 1\n  open: false\nbrick:\n  model: value\n---\n<div class=\"stepper\"><b>{{ .State.value }}</b>{{ if .State.open }}<em>open</em>{{ end }}</div>"

// Down: a fresh modelable child starts from the parent's field, carries
// the field it is bound to and its model value on its root, and the
// binding attribute never reaches its markup.
func TestModelableStartsFromParent(t *testing.T) {
	t.Parallel()
	e := newEngine(t, Options{States: NewMemoryStates(0, 0), FS: theme(modelTheme(stepperFile)...)})
	_, _, page := renderNested(t, e)
	for _, want := range []string{`<b>3</b>`, `data-brick-model="qty"`, `data-brick-model-value="3"`} {
		if !strings.Contains(page, want) {
			t.Fatalf("want %s: %s", want, page)
		}
	}
	if strings.Contains(page, `brick:model=`) {
		t.Fatalf("the binding must not be placed on the child's markup: %s", page)
	}
}

// ★ The keep decision (keep.go): a parent render whose field equals the
// child's model keeps the child; one whose field differs re-renders the
// kept child adopted — same instance, other state kept, the parent's
// value written in. BV: making differs always false keeps the stale child.
func TestModelableParentOwnsOnRender(t *testing.T) {
	t.Parallel()
	e := newEngine(t, Options{States: NewMemoryStates(0, 0), FS: theme(modelTheme(stepperFile)...)})
	parent, child, page := renderNested(t, e)
	childID := dataBrickOf(t, page, child[0])

	// The child's own answer: value 5. Its root still carries its model
	// value; the bound field comes from the call site only, which the
	// runtime carries across (data-brick-up's rule).
	out := mustAction(t, e, child, "$set", "value", 5)
	if !strings.Contains(string(out), `data-brick-model-value="5"`) || strings.Contains(string(out), `data-brick-model="`) {
		t.Fatalf("child's own render: want its model value and no bound field: %s", out)
	}
	child = stateRE.FindStringSubmatch(string(out))[1:]
	child = stateRE.FindStringSubmatch(string(mustAction(t, e, child, "$set", "open", true)))[1:]

	// The runtime's up-trip: the parent $sets qty to 5. The values agree,
	// so the child is kept.
	out = keepAction(t, e, parent, child, childID, "$set", []any{"qty", 5})
	if !strings.Contains(string(out), `<brick-keep data-brick="`+childID+`">`) {
		t.Fatalf("equal values: want the child kept: %s", out)
	}
	parent = stateRE.FindStringSubmatch(string(out))[1:]

	// The parent writes its own field: the kept child re-renders with it,
	// keeping its instance and its other state.
	out = keepAction(t, e, parent, child, childID, "$set", []any{"qty", 9})
	if strings.Contains(string(out), "brick-keep") || !strings.Contains(string(out), "<b>9</b><em>open</em>") {
		t.Fatalf("parent changed the field: want the child adopted with 9: %s", out)
	}
	m := stateRE.FindAllStringSubmatch(string(out), -1)
	if len(m) != 2 || m[1][2] != child[1] {
		t.Fatalf("the child keeps its instance (same token): %s", out)
	}
	child = m[1][1:]
	// The child's instance now holds 9 for its own next action.
	if out := mustAction(t, e, child, "$refresh"); !strings.Contains(string(out), "<b>9</b>") {
		t.Fatalf("the adopted value was not saved: %s", out)
	}
}

// brick: {model:} is checked at compile, and a binding takes no modifiers.
func TestModelableChecks(t *testing.T) {
	t.Parallel()
	cases := []struct{ stepper, call, err string }{
		{"---\nbrick:\n  model: value\n---\n<div>x</div>", `<Stepper brick:model="qty" />`, "model: needs state:"},
		{"---\nstate:\n  value: 1\nbrick:\n  model: nosuch\n---\n<div>x</div>", `<Stepper brick:model="qty" />`, `"nosuch" is not a field of state:`},
		{"---\nstate:\n  value: 1\nbrick:\n  model: a.b\n---\n<div>x</div>", `<Stepper brick:model="qty" />`, "one top-level state field"},
		{stepperFile, `<Stepper brick:model.lazy="qty" />`, "takes no modifiers"},
	}
	for _, tc := range cases {
		e := newEngine(t, Options{States: NewMemoryStates(0, 0), FS: theme(
			"components/Stepper.html", tc.stepper,
			"components/Cart.html", "---\nstate:\n  qty: 3\n---\n<section>"+tc.call+"</section>",
			"content/a.html", `<Cart />`,
		)})
		_, err := e.Render(context.Background(), "content/a.html", nil)
		if err == nil || !strings.Contains(err.Error(), tc.err) {
			t.Errorf("%s / %s: got %v, want %q", tc.stepper, tc.call, err, tc.err)
		}
	}
}

// On a presentational component brick:model keeps its meaning: it is
// placed with {{ attrs }} on the input the component renders.
func TestModelOnPresentationalComponentIsPlaced(t *testing.T) {
	t.Parallel()
	e := newEngine(t, Options{States: NewMemoryStates(0, 0), FS: theme(
		"components/Field.html", `<input {{ attrs }}>`,
		"components/Form.html", "---\nstate:\n  name: \"\"\n---\n<form><Field brick:model=\"name\" /></form>",
		"content/a.html", `<Form />`,
	)})
	out, err := e.Render(context.Background(), "content/a.html", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `<input brick:model="name"`) || strings.Contains(string(out), "data-brick-model") {
		t.Fatalf("want the attribute placed on the input: %s", out)
	}
}

// ★ The model value is data in an attribute: escaped, never markup.
func TestModelValueEscaped(t *testing.T) {
	t.Parallel()
	stepper := "---\nstate:\n  value: \"\"\nbrick:\n  model: value\n---\n<div>x</div>"
	e := newEngine(t, Options{States: NewMemoryStates(0, 0), FS: theme(
		"components/Stepper.html", stepper,
		"components/Cart.html", "---\nstate:\n  qty: '\"><script>alert(1)</script>'\n---\n<section><Stepper brick:model=\"qty\" /></section>",
		"content/a.html", `<Cart />`,
	)})
	out, err := e.Render(context.Background(), "content/a.html", nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "<script>alert") {
		t.Fatalf("the model value broke out of its attribute: %s", out)
	}
	if !strings.Contains(string(out), `data-brick-model-value="&#34;\&#34;\u003e\u003cscript`) {
		t.Fatalf("want the JSON value escaped: %s", out)
	}
}

// ★ A transition effect: types, untyped or none; anything else fails the
// action that recorded it, and the last one recorded wins.
func TestEffectTransition(t *testing.T) {
	t.Parallel()
	e := newEngine(t, Options{FS: theme()})
	for _, ok := range []string{"slide", "slide back", "", TransitionNone, "a_b-c"} {
		if err := e.newEffects("").Transition(ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	for _, bad := range []string{"none slide", "bad!type", "1st", "a b c d e f g h i", `x"y`} {
		if err := e.newEffects("").Transition(bad); err == nil {
			t.Errorf("%q: want an error", bad)
		}
	}
	eff := e.newEffects("")
	_ = eff.Flash("hi", "")
	_ = eff.Transition("slide")
	_ = eff.Transition(TransitionNone)
	list := eff.List()
	if len(list) != 2 || list[1].Kind != EffectTransition || list[1].Name != TransitionNone {
		t.Fatalf("want one transition, the last: %+v", list)
	}
	eff.Filter(func(Effect) bool { return true })
	before := eff.size
	_ = eff.Transition("slide")
	if eff.size != before+len(`{"kind":"transition","name":"slide"}`)-len(`{"kind":"transition","name":"none"}`) {
		t.Fatalf("a replaced transition must give back its bytes: %d → %d", before, eff.size)
	}
}

// .norepeat belongs to keydown; .details to click.
func TestV22Modifiers(t *testing.T) {
	t.Parallel()
	cases := []struct{ markup, err string }{
		{`<input brick:keydown.norepeat.enter="save">`, ""},
		{`<input brick:keydown.norepeat.ctrl.k.window="save">`, ""},
		{`<input brick:keyup.norepeat.enter="save">`, `unknown modifier "norepeat"`},
		{`<div brick:click.details="save">x</div>`, ""},
		{`<div brick:click.outside.details="save">x</div>`, ""},
		{`<input brick:change.details="save">`, `unknown modifier "details"`},
	}
	sc := fakeScripting{modules: map[string]map[string]func(args ...any) (any, error){
		"f": {"save": func(a ...any) (any, error) { return a[0], nil }},
	}}
	for _, tc := range cases {
		e := newEngine(t, Options{States: NewMemoryStates(0, 0), Scripting: sc, FS: theme(
			"components/F.html", "---\nstate:\n  name: \"\"\n---\n<brick:script>f</brick:script>\n<div>"+tc.markup+"</div>",
			"content/a.html", `<F />`,
		)})
		_, err := e.Render(context.Background(), "content/a.html", nil)
		switch {
		case tc.err == "" && err != nil:
			t.Errorf("%s: %v", tc.markup, err)
		case tc.err != "" && (err == nil || !strings.Contains(err.Error(), tc.err)):
			t.Errorf("%s: got %v, want an error containing %q", tc.markup, err, tc.err)
		}
	}
}
