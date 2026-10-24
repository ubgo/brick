package brick

import (
	"context"
	"strings"
	"testing"
)

// Tests for form objects: .Form joins .State, .Errors and
// .Touched per field, including list fields under wildcard rules. BV in a
// comment marks a break-verified test.

const formObjComponent = `---
state:
  name: ""
  address: {city: ""}
  todos: [{title: ""}]
  tags: [a, b]
brick:
  rules:
    name: [required]
    todos.*.title: [required]
---
<brick:script>todos</brick:script>
<div><i>{{ with .Form.name }}{{ .Name }}|{{ .ID }}|{{ .Value }}|{{ .Error }}|{{ .Touched }}|{{ .Invalid }}{{ end }};` +
	`{{ with .Form.address.Fields.city }}{{ .Name }}|{{ .ID }}{{ end }};` +
	`{{ range .Form.todos.Items }}{{ with .Fields.title }}{{ .Name }}={{ .Value }}:{{ .Error }}:{{ .Touched }}{{ end }},{{ end }};` +
	`{{ range .Form.tags.Items }}{{ .Name }}={{ .Value }},{{ end }};` +
	`{{ len .Form.name.Items }}{{ len .Form.name.Fields }}</i></div>`

func formObjEngine(t *testing.T) (*Engine, ActionRequest) {
	t.Helper()
	e := newEngine(t, Options{
		FS:        theme("components/Todos.html", formObjComponent, "content/a.html", `<Todos />`),
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

// Before anything is touched a field has its value and no error; after a
// submit every rule path is touched and each field carries its own error
// under its concrete path. A scalar has no items or fields.
// BV: reading the error by the declared pattern instead of the concrete
// path (src.errors[path] → "todos.*.title") empties the todos errors.
func TestFormFields(t *testing.T) {
	t.Parallel()
	e, base := formObjEngine(t)
	got := run(t, e, base, "load", []any{[]any{map[string]any{"title": "ok"}, map[string]any{"title": ""}}})
	idPrefix := strings.SplitN(got, "|", 3)[1]
	if !strings.HasSuffix(idPrefix, "-name") || idPrefix == "-name" {
		t.Fatalf("the id must join the instance id and the path: %q", got)
	}
	inst := strings.TrimSuffix(idPrefix, "-name")
	want := "name|" + inst + "-name|||false|false;address.city|" + inst + "-address-city;" +
		"todos.0.title=ok::false,todos.1.title=::false,;tags.0=a,tags.1=b,;00"
	if got != want {
		t.Fatalf("before submit:\n got %q\nwant %q", got, want)
	}
	got = run(t, e, base, "submit", nil)
	want = "name|" + inst + "-name||This field is required.|true|true;address.city|" + inst + "-address-city;" +
		"todos.0.title=ok::true,todos.1.title=:This field is required.:true,;tags.0=a,tags.1=b,;00"
	if got != want {
		t.Fatalf("after submit:\n got %q\nwant %q", got, want)
	}
}

// .Form agrees with .Errors: an error the action returned wins over a rule
// error, and a non-string error value is shown as text.
func TestFormActionErrorWins(t *testing.T) {
	t.Parallel()
	src := "---\nstate:\n  email: \"\"\n  n: 0\nbrick:\n  rules:\n    email: [required]\n---\n<brick:script>todos</brick:script>\n" +
		`<div><i>{{ .Form.email.Error }}|{{ .Form.n.Error }}|{{ index .Errors "email" }}</i></div>`
	sc := ctxScripting{"todos": {"save": func(ctx context.Context, a ...any) (any, error) {
		if _, err := ValidationFrom(ctx).Validate(ctx); err != nil {
			return nil, err
		}
		return []any{a[0], map[string]any{"email": "Taken.", "n": 3}}, nil
	}}}
	e := newEngine(t, Options{FS: theme("components/Todos.html", src, "content/a.html", `<Todos />`), Scripting: sc, States: NewMemoryStates(0, 0)})
	page, err := e.Render(context.Background(), "content/a.html", nil)
	if err != nil {
		t.Fatal(err)
	}
	h, tok := handleOf(t, string(page))
	if got := run(t, e, ActionRequest{Handle: h, Token: tok}, "save", nil); got != "Taken.|3|Taken." {
		t.Fatalf("the action's errors must win and match .Errors: %q", got)
	}
}

// A key the state does not declare behaves as it does for .State: under
// strictParams it fails the render, so a typo never renders an empty
// input. Outside a stateful component .Form is empty.
func TestFormUnknownField(t *testing.T) {
	t.Parallel()
	src := "---\nstate:\n  email: \"\"\n---\n<div>{{ .Form.emial.Value }}</div>"
	_, err := newEngineErr(Options{FS: theme("components/F.html", src, "content/a.html", `<F />`), States: NewMemoryStates(0, 0), StrictParams: true}, "content/a.html")
	if err == nil || !strings.Contains(err.Error(), "emial") {
		t.Fatalf("an undeclared field must fail the render: %v", err)
	}
	e := newEngine(t, Options{FS: theme("content/a.html", `<p>{{ len .Form }}</p>`)})
	out, err := e.Render(context.Background(), "content/a.html", nil)
	if err != nil || !strings.Contains(string(out), "<p>0</p>") {
		t.Fatalf("a page has an empty form: %s %v", out, err)
	}
}

// A field's ID is safe in a CSS selector and a for=: dots become dashes.
func TestFormFieldIDNoDots(t *testing.T) {
	t.Parallel()
	s := &Scope{ID: "b3", State: map[string]any{"todos": []any{map[string]any{"title": "x"}}}}
	f := s.Form()["todos"].Items()[0].Fields()["title"]
	if f.ID != "b3-todos-0-title" || f.Name != "todos.0.title" {
		t.Fatalf("id %q name %q", f.ID, f.Name)
	}
}
