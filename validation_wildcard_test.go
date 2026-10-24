package brick

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// Tests for wildcard rules (v19 G3): a * segment in a rules: path covers
// every list element, and errors are keyed by the concrete path. BV in a
// comment marks a break-verified test.

const todosComponent = `---
state:
  todos: [{title: "", tags: []}]
brick:
  rules:
    todos.*.title: [required, {min: 2}]
    todos.*.tags.*: [{min: 3}]
  messages:
    todos.*.title.required: "Each todo needs a title."
---
<brick:script>todos</brick:script>
<div><i>{{ range $k, $v := .Errors }}{{ $k }}={{ $v }};{{ end }}|{{ range $k, $v := .Touched }}{{ $k }};{{ end }}</i></div>`

// todosScripting: load replaces the list with its argument, submit
// validates every declaration, pattern and one validate one field each,
// forget untouches a wildcard.
func todosScripting() ctxScripting {
	state := func(a []any) map[string]any { st, _ := a[0].(map[string]any); return st }
	validated := func(fields ...string) func(ctx context.Context, a ...any) (any, error) {
		return func(ctx context.Context, a ...any) (any, error) {
			errs, err := ValidationFrom(ctx).Validate(ctx, fields...)
			if err != nil {
				return nil, err
			}
			out := map[string]any{}
			for k, v := range errs {
				out[k] = v
			}
			return []any{state(a), out}, nil
		}
	}
	return ctxScripting{"todos": {
		"load": func(_ context.Context, a ...any) (any, error) {
			st := state(a)
			st["todos"] = a[2]
			return st, nil
		},
		"submit":  validated(),
		"pattern": validated("todos.*.title"),
		"one":     validated("todos.1.title"),
		"typo":    validated("todos.*.titel"),
		"forget": func(ctx context.Context, a ...any) (any, error) {
			ValidationFrom(ctx).Untouch("todos.*.title")
			return state(a), nil
		},
	}}
}

func todosEngine(t *testing.T) (*Engine, ActionRequest) {
	t.Helper()
	e := newEngine(t, Options{
		FS:        theme("components/Todos.html", todosComponent, "content/a.html", `<Todos />`),
		Scripting: todosScripting(),
		States:    NewMemoryStates(0, 0),
	})
	page, err := e.Render(context.Background(), "content/a.html", nil)
	if err != nil {
		t.Fatal(err)
	}
	h, tok := handleOf(t, string(page))
	return e, ActionRequest{Handle: h, Token: tok}
}

// mixedTodos is a list with a failing title and tag, a short title, an
// item that is not a dict, and an item whose tags is not a list.
func mixedTodos() []any {
	return []any{
		map[string]any{"title": "", "tags": []any{"ab", "good"}},
		map[string]any{"title": "x"},
		"not a dict",
		map[string]any{"title": "Fine", "tags": "not a list"},
	}
}

// An empty list has no items, so validating it reports and touches nothing.
func TestWildcardEmptyList(t *testing.T) {
	t.Parallel()
	e, base := todosEngine(t)
	run(t, e, base, "load", []any{[]any{}})
	if got := run(t, e, base, "submit", nil); got != "|" {
		t.Fatalf("an empty list must expand to nothing: %q", got)
	}
}

// Nested and mixed lists: every element is checked under its concrete
// path, the wildcard message override applies, an element that is not a
// dict has no title (required fails), and a non-list under * expands to
// nothing.
func TestWildcardNestedAndMixed(t *testing.T) {
	t.Parallel()
	e, base := todosEngine(t)
	run(t, e, base, "load", []any{mixedTodos()})
	got := run(t, e, base, "submit", nil)
	want := "todos.0.tags.0=Must be at least 3 characters.;todos.0.title=Each todo needs a title.;todos.1.title=Must be at least 2 characters.;todos.2.title=Each todo needs a title.;" +
		"|todos.0.tags.0;todos.0.tags.1;todos.0.title;todos.1.title;todos.2.title;todos.3.title;"
	if got != want {
		t.Fatalf("submit:\n got %q\nwant %q", got, want)
	}
}

// validate() with a pattern expands only it; a concrete path a wildcard
// covers is accepted; a pattern matching no declaration fails the action;
// untouch with a pattern forgets every path it covers.
func TestWildcardValidateFields(t *testing.T) {
	t.Parallel()
	e, base := todosEngine(t)
	run(t, e, base, "load", []any{mixedTodos()})
	if got := run(t, e, base, "one", nil); got != "todos.1.title=Must be at least 2 characters.;|todos.1.title;" {
		t.Fatalf("one concrete path: %q", got)
	}
	got := run(t, e, base, "pattern", nil)
	want := "todos.0.title=Each todo needs a title.;todos.1.title=Must be at least 2 characters.;todos.2.title=Each todo needs a title.;|todos.0.title;todos.1.title;todos.2.title;todos.3.title;"
	if got != want {
		t.Fatalf("pattern:\n got %q\nwant %q", got, want)
	}
	if got := run(t, e, base, "forget", nil); got != "|" {
		t.Fatalf("untouch todos.*.title: %q", got)
	}
	if _, err := e.RunAction(context.Background(), ActionRequest{Handle: base.Handle, Token: base.Token, Action: "typo"}); err == nil || !strings.Contains(err.Error(), `"todos.*.titel" has no rules`) {
		t.Fatalf("a pattern with no declaration must fail the action: %v", err)
	}
}

// $set of one item's field touches that concrete path, and its error is
// keyed by it.
func TestWildcardSetTouchesConcretePath(t *testing.T) {
	t.Parallel()
	e, base := todosEngine(t)
	run(t, e, base, "load", []any{[]any{map[string]any{"title": "ok"}, map[string]any{"title": "ok"}}})
	if got := run(t, e, base, "$set", []any{"todos.1.title", "x"}); got != "todos.1.title=Must be at least 2 characters.;|todos.1.title;" {
		t.Fatalf("$set todos.1.title: %q", got)
	}
}

// A touched item that was then removed has no error: it names nothing.
// BV: dropping the validate.Exists check in ruleErrors reports
// "todos.1.title=Each todo needs a title." here.
func TestWildcardRemovedItemHasNoError(t *testing.T) {
	t.Parallel()
	e, base := todosEngine(t)
	run(t, e, base, "load", []any{[]any{map[string]any{"title": "ok"}, map[string]any{"title": "ok"}}})
	run(t, e, base, "$set", []any{"todos.1.title", "x"})
	if got := run(t, e, base, "load", []any{[]any{map[string]any{"title": "ok"}}}); got != "|todos.1.title;" {
		t.Fatalf("a removed item must not show an error: %q", got)
	}
}

// An exact declaration and a wildcard both apply to the path they share,
// the exact one first.
func TestWildcardAndExactBothApply(t *testing.T) {
	t.Parallel()
	src := "---\nstate:\n  todos: [{title: \"\"}]\nbrick:\n  rules:\n    todos.*.title: [{min: 2}]\n    todos.0.title: [{max: 3}]\n---\n<brick:script>todos</brick:script>\n" +
		`<div><i>{{ range $k, $v := .Errors }}{{ $k }}={{ $v }};{{ end }}|{{ range $k, $v := .Touched }}{{ $k }};{{ end }}</i></div>`
	e := newEngine(t, Options{FS: theme("components/Todos.html", src, "content/a.html", `<Todos />`), Scripting: todosScripting(), States: NewMemoryStates(0, 0)})
	page, err := e.Render(context.Background(), "content/a.html", nil)
	if err != nil {
		t.Fatal(err)
	}
	h, tok := handleOf(t, string(page))
	base := ActionRequest{Handle: h, Token: tok}
	if got := run(t, e, base, "$set", []any{"todos.0.title", "long"}); got != "todos.0.title=Must be at most 3 characters.;|todos.0.title;" {
		t.Fatalf("exact rule: %q", got)
	}
	if got := run(t, e, base, "$set", []any{"todos.0.title", "x"}); got != "todos.0.title=Must be at least 2 characters.;|todos.0.title;" {
		t.Fatalf("wildcard rule: %q", got)
	}
}

// A * must stand where the declared value is a list (or nil): a dict, a
// scalar or the top level is a compile error, and the path past * is
// checked against the declared first element.
// BV: removing the dict case in checkStatePath lets "dict" compile.
func TestWildcardCompileErrors(t *testing.T) {
	t.Parallel()
	head := "---\nstate:\n  todos: [{title: \"\"}]\n  empty: []\n  later: null\n  addr: {city: \"\"}\n  name: \"\"\nbrick:\n"
	cases := map[string]struct {
		fm   string
		want string
	}{
		"dict":        {"  rules:\n    addr.*: [required]\n", "addr is a dict; * matches list elements only"},
		"scalar":      {"  rules:\n    name.*: [required]\n", "name is a string, not a list"},
		"top":         {"  rules:\n    \"*\": [required]\n", "cannot start with *"},
		"past *":      {"  rules:\n    todos.*.titel: [required]\n", `todos.* has no key "titel"`},
		"message":     {"  rules:\n    todos.*.title: [required]\n  messages:\n    todos.*.title.min: x\n", `"todos.*.title.min" names no declared rule`},
		"ok":          {"  rules:\n    todos.*.title: [required]\n  messages:\n    todos.*.title.required: x\n", ""},
		"ok empty":    {"  rules:\n    empty.*.anything: [required]\n", ""},
		"ok nil":      {"  rules:\n    later.*: [required]\n", ""},
		"ok nested *": {"  rules:\n    empty.*.tags.*: [{min: 1}]\n", ""},
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
		if !errors.As(err, &be) || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: want %q, got %v", name, tc.want, err)
		}
	}
}
