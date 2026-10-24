package brick

import (
	"errors"
	"fmt"
	"html/template"
	"net/mail"
	"strings"
	"testing"
)

// moneyFuncs is a host function map as a Go program would register it.
var moneyFuncs = template.FuncMap{
	"money": func(cents int) string { return fmt.Sprintf("$%d.%02d", cents/100, cents%100) },
}

// TestHostFuncAvailable pins Options.Funcs: a Go host's function is callable
// from every file, a page and a component alike.
func TestHostFuncAvailable(t *testing.T) {
	t.Parallel()
	e := newEngine(t, Options{Funcs: moneyFuncs, FS: theme(
		"content/a.html", `<p>{{ money 1999 }}</p><Price :cents="250" />`,
		"components/Price.html", `<b>{{ money .Props.cents }}</b>`,
	)})
	if got := render(t, e, "content/a.html", nil); got != "<p>$19.99</p><b>$2.50</b>" {
		t.Fatalf("got %q", got)
	}
}

// TestHostFuncMissingNamesIt pins portability: a theme calling a function
// its host did not register fails to compile, and the error names it.
func TestHostFuncMissingNamesIt(t *testing.T) {
	t.Parallel()
	e := newEngine(t, Options{FS: theme("content/a.html", `<p>{{ money 1999 }}</p>`)})
	be := renderErr(t, e, "content/a.html")
	if !strings.Contains(be.Error(), "money") {
		t.Fatalf("error does not name the function: %v", be)
	}
}

// TestHostFuncCannotShadowBuiltin pins C12 for host functions: Go builtins,
// engine pure built-ins, lexically scoped built-ins and `__` names are all
// refused by New.
func TestHostFuncCannotShadowBuiltin(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"len", "upper", "sanitize", "children", "attrs", "__render"} {
		fm := template.FuncMap{name: func() string { return "x" }}
		_, err := New(Options{Funcs: fm})
		if err == nil || !strings.Contains(err.Error(), name) {
			t.Errorf("%s: want an error naming it, got %v", name, err)
		}
	}
}

// TestHostFuncBadValue pins that a value html/template would panic on is an
// error from New, not a panic at the first compile.
func TestHostFuncBadValue(t *testing.T) {
	t.Parallel()
	for name, v := range map[string]any{
		"notFunc":   "x",
		"threeOuts": func() (int, int, error) { return 0, 0, nil },
		"noOuts":    func() {},
		"bad-name":  func() string { return "" },
	} {
		if _, err := New(Options{Funcs: template.FuncMap{name: v}}); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

// emailType is a host prop type: it accepts a parseable address and returns
// it lower-cased.
func emailType(v any) (any, error) {
	s, ok := v.(string)
	if !ok {
		return nil, fmt.Errorf("%v is not an email address", v)
	}
	if _, err := mail.ParseAddress(s); err != nil {
		return nil, fmt.Errorf("%q is not an email address", s)
	}
	return strings.ToLower(s), nil
}

var hostTypes = map[string]PropType{"email": emailType}

// TestPropTypeCustom pins Options.PropTypes: a declared custom type converts
// a valid value and refuses an invalid one, naming the prop.
func TestPropTypeCustom(t *testing.T) {
	t.Parallel()
	fs := theme(
		"components/Contact.html", "---\nprops:\n  to: email\n---\n<a>{{ .Props.to }}</a>",
		"content/ok.html", `<Contact to="Ann@Example.com" />`,
		"content/bad.html", `<Contact to="not an address" />`,
	)
	e := newEngine(t, Options{FS: fs, PropTypes: hostTypes})
	if got := render(t, e, "content/ok.html", nil); got != "<a>ann@example.com</a>" {
		t.Fatalf("got %q", got)
	}
	be := renderErr(t, e, "content/bad.html")
	if be.Code != CodeProp || !strings.Contains(be.Error(), "not an email address") {
		t.Fatalf("got %v", be)
	}
}

// TestPropTypeCustomDefault pins that a default goes through the custom
// check at compile, so a bad default fails before any render uses it.
func TestPropTypeCustomDefault(t *testing.T) {
	t.Parallel()
	fs := theme(
		"components/Contact.html", "---\nprops:\n  to: {type: email, default: nobody}\n---\n<a>{{ .Props.to }}</a>",
		"content/a.html", `<Contact />`,
	)
	e := newEngine(t, Options{FS: fs, PropTypes: hostTypes})
	be := renderErr(t, e, "content/a.html")
	if !strings.Contains(be.Error(), "default") {
		t.Fatalf("got %v", be)
	}
}

// TestPropTypeUnknownNamesIt pins portability: a theme naming a type its
// host did not register fails to compile with an error naming the type.
func TestPropTypeUnknownNamesIt(t *testing.T) {
	t.Parallel()
	fs := theme(
		"components/Contact.html", "---\nprops:\n  to: email\n---\n<a>{{ .Props.to }}</a>",
		"content/a.html", `<Contact to="a@b.c" />`,
	)
	e := newEngine(t, Options{FS: fs})
	be := renderErr(t, e, "content/a.html")
	if !strings.Contains(be.Error(), `"email"`) || !strings.Contains(be.Error(), "Options.PropTypes") {
		t.Fatalf("got %v", be)
	}
}

// TestPropTypeCannotRedefineBuiltin pins that `int` means the same thing
// under every host.
func TestPropTypeCannotRedefineBuiltin(t *testing.T) {
	t.Parallel()
	_, err := New(Options{PropTypes: map[string]PropType{"int": emailType}})
	if err == nil || !strings.Contains(err.Error(), `"int"`) {
		t.Fatalf("got %v", err)
	}
	if _, err := New(Options{PropTypes: map[string]PropType{"email": nil}}); err == nil {
		t.Fatal("a nil check function must be refused")
	}
}

// TestPropTypeCheckErrorIsWrapped pins that the host's error survives as a
// cause, so a host can match its own sentinel errors.
func TestPropTypeCheckErrorIsWrapped(t *testing.T) {
	t.Parallel()
	errNope := errors.New("nope")
	d := &propDecl{Name: "x", Type: "t", custom: func(any) (any, error) { return nil, errNope }}
	if _, err := checkProp(d, "v"); !errors.Is(err, errNope) {
		t.Fatalf("got %v", err)
	}
}

// TestInlineScriptCannotShadowHostFunc pins C12 for host functions from the
// other side: an inline <brick:script> function named like a host function
// is a compile error, never a silent per-file shadow.
func TestInlineScriptCannotShadowHostFunc(t *testing.T) {
	t.Parallel()
	sc := fakeScripting{modules: map[string]map[string]func(args ...any) (any, error){
		"inline": {"money": func(a ...any) (any, error) { return "shadowed", nil }},
	}}
	e := newEngine(t, Options{Scripting: sc, Funcs: moneyFuncs, FS: theme(
		"content/a.html", "<brick:script>inline</brick:script>{{ money 5 }}",
	)})
	be := renderErr(t, e, "content/a.html")
	if be.Code != CodeReserved || !strings.Contains(be.Error(), `"money"`) {
		t.Fatalf("got %v", be)
	}
}
