package brick

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// Tests for declared rules, the touched set and
// brick.validate. BV in a comment marks a break-verified test.

const signupComponent = `---
state:
  name: ""
  email: ""
  password: ""
  confirm: ""
brick:
  rules:
    name: [required, {min: 2}]
    email: [required, email]
    password: [required, {min: 12}]
    confirm: [{same: password}]
  messages:
    confirm.same: "The passwords do not match."
---
<brick:script>signup</brick:script>
<div><i>{{ range $k, $v := .Errors }}{{ $k }}={{ $v }};{{ end }}|{{ range $k, $v := .Touched }}{{ $k }};{{ end }}</i></div>`

// signupScripting is the signup component's script: submit validates
// everything, save resets and untouches, taken returns its own error.
func signupScripting() ctxScripting {
	state := func(a []any) map[string]any { st, _ := a[0].(map[string]any); return st }
	asAny := func(m map[string]string) map[string]any {
		out := map[string]any{}
		for k, v := range m {
			out[k] = v
		}
		return out
	}
	return ctxScripting{"signup": {
		"submit": func(ctx context.Context, a ...any) (any, error) {
			errs, err := ValidationFrom(ctx).Validate(ctx)
			if err != nil {
				return nil, err
			}
			if len(errs) > 0 {
				return []any{state(a), asAny(errs)}, nil
			}
			return state(a), nil
		},
		"only": func(ctx context.Context, a ...any) (any, error) {
			_, err := ValidationFrom(ctx).Validate(ctx, "emial")
			return state(a), err
		},
		"save": func(ctx context.Context, a ...any) (any, error) {
			ValidationFrom(ctx).Untouch()
			st := state(a)
			for _, k := range []string{"name", "email", "password", "confirm"} {
				st[k] = ""
			}
			return st, nil
		},
		"taken": func(ctx context.Context, a ...any) (any, error) {
			return []any{state(a), map[string]any{"email": "That email is taken."}}, nil
		},
	}}
}

func signupEngine(t *testing.T, opts Options) (*Engine, ActionRequest) {
	t.Helper()
	opts.FS = theme("components/Signup.html", signupComponent, "content/a.html", `<Signup />`)
	opts.Scripting = signupScripting()
	opts.States = NewMemoryStates(0, 0)
	e := newEngine(t, opts)
	page, err := e.Render(context.Background(), "content/a.html", nil)
	if err != nil {
		t.Fatal(err)
	}
	h, tok := handleOf(t, string(page))
	if got := body(t, page); got != "|" {
		t.Fatalf("first render shows errors or touched fields: %q", got)
	}
	return e, ActionRequest{Handle: h, Token: tok}
}

func run(t *testing.T, e *Engine, base ActionRequest, action string, args []any, sets ...FieldSet) string {
	t.Helper()
	res, err := e.RunAction(context.Background(), ActionRequest{Handle: base.Handle, Token: base.Token, Action: action, Args: args, Sets: sets})
	if err != nil {
		t.Fatal(err)
	}
	return body(t, res.HTML)
}

// $set touches the field it sends, and only touched fields show errors.
// BV: rendering every rule path (not only touched ones) fails the first
// assertion.
func TestSetTouchesOneField(t *testing.T) {
	t.Parallel()
	e, base := signupEngine(t, Options{})
	got := run(t, e, base, "$set", []any{"name", "A"})
	if got != "name=Must be at least 2 characters.;|name;" {
		t.Fatalf("after $set name: %q", got)
	}
	got = run(t, e, base, "$set", []any{"name", "Ada"})
	if got != "|name;" {
		t.Fatalf("a passing touched field keeps no error: %q", got)
	}
	// A deferred set touches too.
	got = run(t, e, base, "$refresh", nil, FieldSet{Path: "email", Value: "nope"})
	if got != "email=Enter a valid email address.;|email;name;" {
		t.Fatalf("after a deferred email: %q", got)
	}
}

// brick.validate touches every path with rules; untouch clears them.
func TestValidateAllAndUntouch(t *testing.T) {
	t.Parallel()
	e, base := signupEngine(t, Options{})
	got := run(t, e, base, "submit", nil, FieldSet{Path: "name", Value: "Ada"}, FieldSet{Path: "confirm", Value: "x"})
	want := "confirm=The passwords do not match.;email=This field is required.;password=This field is required.;|confirm;email;name;password;"
	if got != want {
		t.Fatalf("submit:\n got %q\nwant %q", got, want)
	}
	if got := run(t, e, base, "save", nil); got != "|" {
		t.Fatalf("after save and untouch: %q", got)
	}
	if _, err := e.RunAction(context.Background(), ActionRequest{Handle: base.Handle, Token: base.Token, Action: "only"}); err == nil || !strings.Contains(err.Error(), `"emial" has no rules`) {
		t.Fatalf("a typo in validate(fields) must fail the action: %v", err)
	}
}

// same re-checks confirm when password changes, since every touched path
// is validated at every render.
func TestSameReevaluates(t *testing.T) {
	t.Parallel()
	e, base := signupEngine(t, Options{})
	run(t, e, base, "$set", []any{"password", "correct horse battery"})
	got := run(t, e, base, "$set", []any{"confirm", "correct horse battery"})
	if got != "|confirm;password;" {
		t.Fatalf("matching: %q", got)
	}
	got = run(t, e, base, "$set", []any{"password", "correct horse battery staple"})
	if got != "confirm=The passwords do not match.;|confirm;password;" {
		t.Fatalf("password changed, confirm must fail: %q", got)
	}
}

// An action's own errors win over rule errors for the same path.
// BV: overlaying rule errors after the action's fails this test.
func TestActionErrorsOverrideRules(t *testing.T) {
	t.Parallel()
	e, base := signupEngine(t, Options{})
	got := run(t, e, base, "taken", nil, FieldSet{Path: "email", Value: "bad"})
	if got != "email=That email is taken.;|email;" {
		t.Fatalf("action error must win: %q", got)
	}
	// One render only: the next one is rules again.
	if got := run(t, e, base, "$refresh", nil); got != "email=Enter a valid email address.;|email;" {
		t.Fatalf("next render: %q", got)
	}
}

// Touched is part of the instance encoding, so a sealed or shared store
// keeps it across requests.
func TestTouchedEncoded(t *testing.T) {
	t.Parallel()
	in := &Instance{Token: "t", State: map[string]any{}, Touched: []string{"a", "b.c"}}
	b, err := EncodeInstance(in)
	if err != nil {
		t.Fatal(err)
	}
	out, err := DecodeInstance(b)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(out.Touched, ",") != "a,b.c" {
		t.Fatalf("touched lost: %v", out.Touched)
	}
	b2, _ := EncodeInstance(&Instance{Token: "t", State: map[string]any{}})
	if strings.Contains(string(b2), "touched") {
		t.Fatalf("an untouched instance must not grow: %s", b2)
	}
}

// A component without rules records nothing and has no Validation.
func TestNoRulesCostsNothing(t *testing.T) {
	t.Parallel()
	e, base, _, _ := formEngine(t)
	if _, err := e.RunAction(context.Background(), ActionRequest{Handle: base.Handle, Token: base.Token, Action: "$set", Args: []any{"name", "Ada"}}); err != nil {
		t.Fatal(err)
	}
	name, handle, _ := strings.Cut(base.Handle, ".")
	inst, ok, err := e.stores[name].Get(context.Background(), handle)
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	if inst.Touched != nil {
		t.Fatalf("a component without rules recorded touched paths: %v", inst.Touched)
	}
}

// upperValidator is a host Validator: one rule, "upper".
type upperValidator struct{}

func (upperValidator) Check(_ string, r Rule) error {
	if r.Name != "upper" {
		return errors.New("only upper")
	}
	return nil
}

func (upperValidator) Validate(_ context.Context, _ string, v any, _ []Rule, _ map[string]any, locale string) string {
	if s, _ := v.(string); s != strings.ToUpper(s) {
		return "SHOUT (" + locale + ")"
	}
	return ""
}

// Options.Validator replaces the default: its rules compile, the
// default's do not, and it gets the render locale.
func TestCustomValidator(t *testing.T) {
	t.Parallel()
	comp := "---\nstate:\n  code: \"\"\nbrick:\n  rules:\n    code: [upper]\n---\n<brick:script>signup</brick:script>\n<div><i>{{ .Errors.code }}</i></div>"
	e := newEngine(t, Options{Validator: upperValidator{}, Scripting: signupScripting(), States: NewMemoryStates(0, 0), FS: theme("components/Code.html", comp, "content/a.html", `<Code />`)})
	page, err := e.Render(context.Background(), "content/a.html", nil, WithLocale("de"))
	if err != nil {
		t.Fatal(err)
	}
	h, tok := handleOf(t, string(page))
	if got := run(t, e, ActionRequest{Handle: h, Token: tok}, "$set", []any{"code", "abc"}); got != "SHOUT (de)" {
		t.Fatalf("custom validator: %q", got)
	}
	_, err = newEngineErr(Options{Validator: upperValidator{}, FS: theme("components/X.html", "---\nstate:\n  a: \"\"\nbrick:\n  rules:\n    a: [required]\n---\n<i></i>", "content/a.html", `<X />`)}, "content/a.html")
	if err == nil || !strings.Contains(err.Error(), "only upper") {
		t.Fatalf("the default's rules must not compile under a custom Validator: %v", err)
	}
}

// Every bad declaration is a compile error at the key's line.
func TestRuleCompileErrors(t *testing.T) {
	t.Parallel()
	head := "---\nstate:\n  name: \"\"\n  addr: {city: \"\"}\n  items: []\nbrick:\n"
	cases := map[string]struct {
		fm   string
		want string
		line int
	}{
		"unknown":       {"  rules:\n    name: [requird]\n", `unknown rule "requird"`, 8},
		"bad arg":       {"  rules:\n    name: [{min: two}]\n", "min takes a number", 8},
		"undeclared":    {"  rules:\n    nmae: [required]\n", `"nmae" is not declared`, 8},
		"nested":        {"  rules:\n    addr.zip: [required]\n", `addr has no key "zip"`, 8},
		"bad pattern":   {"  rules:\n    name: [{pattern: \"[\"}]\n", "pattern:", 8},
		"message":       {"  rules:\n    name: [required]\n  messages:\n    name.min: x\n", `"name.min" names no declared rule`, 10},
		"two keys":      {"  rules:\n    name: [{min: 1, max: 2}]\n", "one-key map", 0},
		"arg on bare":   {"  rules:\n    name: [{email: true}]\n", "email takes no argument", 8},
		"same itself":   {"  rules:\n    name: [{same: name}]\n", "same names the field itself", 8},
		"list index ok": {"  rules:\n    items.0.qty: [required]\n", "", 0},
	}
	for name, tc := range cases {
		src := head + tc.fm + "---\n<i></i>"
		_, err := newEngineErr(Options{States: NewMemoryStates(0, 0), FS: theme("components/X.html", src, "content/a.html", `<X />`)}, "content/a.html")
		if tc.want == "" {
			if err != nil {
				t.Errorf("%s: %v", name, err)
			}
			continue
		}
		var be *Error
		if !errors.As(err, &be) || !strings.Contains(err.Error(), tc.want) || (tc.line > 0 && be.Line != tc.line) {
			t.Errorf("%s: want %q at line %d, got %v", name, tc.want, tc.line, err)
		}
	}
	_, err := newEngineErr(Options{FS: theme("components/X.html", "---\nbrick:\n  rules:\n    a: [required]\n---\n<i></i>", "content/a.html", `<X />`)}, "content/a.html")
	if err == nil || !strings.Contains(err.Error(), "rules: needs state:") {
		t.Fatalf("rules without state: %v", err)
	}
}

// newEngineErr builds an engine and renders page, returning the first
// error (New or Render).
func newEngineErr(opts Options, page string) ([]byte, error) {
	e, err := New(opts)
	if err != nil {
		return nil, err
	}
	return e.Render(context.Background(), page, nil)
}
