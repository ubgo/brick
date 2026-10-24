# Validation: rules declared once

## What it is and when you want it

Declare each field's checks once under `brick: {rules:}` and the engine computes `.Errors` at every render, but only for the fields the user has touched, so a fresh form shows no errors and a field shows its error the moment the user leaves it. The submit action calls `brick.validate()` to check everything at once. Rules reach into lists with `*`, `.Form` gives each field one value carrying its path, id, value and error, and `Options.Validator` lets the host add its own rules or replace the engine.

## Before you start

- A stateful component with `brick:model` fields ([Deferred fields](deferred-fields.md)).
- The example: [examples/forms-validation-lists](../../../examples/forms-validation-lists/), two forms on one page.

## Step 1 — Declare the rules of a signup form

The rules sit next to the state they check, and `messages:` replaces one rule's message by `path.rule`.

`theme/components/Signup.html`

<!-- snippet: examples/forms-validation-lists/theme/components/Signup.html -->
```html
---
state:
  name: ""
  email: ""
  password: ""
  confirm: ""
brick:
  rules:
    name: [required, {min: 2}]
    email: [required, email, {max: 120}]
    password: [required, {min: 12}]
    confirm: [required, {same: password}]
  messages:
    confirm.same: "The passwords do not match."
---
<brick:script>
def signup(state, props):
    errors = brick.validate()
    if errors:
        return [state, errors]
    if state["email"].endswith("@taken.example"):
        return [state, {"email": "That e-mail already has an account."}]
    brick.untouch()
    brick.flash("Welcome!", "success")
    return dict(state, name = "", email = "", password = "", confirm = "")
</brick:script>
<form brick:submit="signup" novalidate>
  <input brick:model.lazy="name" value="{{ .State.name }}">
  {{ with .Errors.name }}<p class="error">name: {{ . }}</p>{{ end }}
  <input type="email" brick:model.lazy="email" value="{{ .State.email }}">
  {{ with .Errors.email }}<p class="error">email: {{ . }}</p>{{ end }}
  <input type="password" brick:model.lazy="password" value="{{ .State.password }}">
  {{ with .Errors.password }}<p class="error">password: {{ . }}</p>{{ end }}
  <input type="password" brick:model.lazy="confirm" value="{{ .State.confirm }}">
  {{ with .Errors.confirm }}<p class="error">confirm: {{ . }}</p>{{ end }}
  <button>Create account</button>
</form>
```

The default rules:

| Rule | Argument | Fails when |
|---|---|---|
| `required` | none | the value is missing, blank, or an empty list (`false` and `0` are values) |
| `min`, `max` | a number | a string has fewer/more characters, a list fewer/more items, a number is smaller/larger |
| `email` | none | the value is not a plain address (`ada@example.com`, no display name) |
| `url` | none | the value is not an `http`/`https` URL with a host |
| `pattern` | a Go regexp | the whole value does not match |
| `in` | a list | the value (or any element of a list) is not one of them |
| `same` | another path | the value differs from that field's |

Every rule but `required` and `same` passes an empty value, so an optional field says so by not being `required`.

When errors show:

- A field becomes **touched** when the browser first sends it: a `$set` from `brick:model` (with `.lazy`, when the user leaves the field) or a deferred value in `sets`.
- `brick.validate()` checks the named fields (every field with rules when called with none), touches them, and returns path → message for those that fail. It sees the state with the request's deferred values applied.
- An action's own errors (`[state, errors]`) are laid over the rule errors for that render and win for their fields; that is how *That e-mail already has an account.* replaces a passing `email` rule.
- `brick.untouch()` forgets touched fields, so the emptied form after a successful signup shows no errors.

## Step 2 — Check every row of a list with `*` and render it with `.Form`

A list's length changes while the user edits it, so the rule names the pattern (`todos.*.title`) and errors come back keyed by the concrete path (`todos.2.title`). `.Form` gives each field `.Name` (the path `brick:model` takes), `.ID`, `.Value`, `.Error`, `.Touched` and `.Invalid`; `.Items` walks a list and `.Fields` a dict.

`theme/components/Todos.html`

<!-- snippet: examples/forms-validation-lists/theme/components/Todos.html -->
```html
---
state:
  slug: ""
  todos: [{title: "", tags: []}, {title: "x", tags: [ab, good]}, {title: "Fine", tags: []}]
brick:
  rules:
    slug: [required, slug]
    todos: [{min: 1}]
    todos.*.title: [required, {min: 2}]
    todos.*.tags.*: [{min: 3}]
  messages:
    todos.*.title.required: "Each todo needs a title."
---
<brick:script>
def add_todo(state, props):
    return dict(state, todos = state["todos"] + [{"title": "", "tags": []}])

def save(state, props):
    errors = brick.validate()
    if errors:
        return [state, errors]
    brick.untouch()
    return state
</brick:script>
<brick:client>
// Read the errors the last render left on the component, with no request.
// has() asks if there are any, first() is the message of the first path in
// path order, all() is the whole path-to-message map.
document.addEventListener("click", (event) => {
  const button = event.target.closest("[data-first-error]");
  if (!button) return;
  const errors = brick.errors(button);
  button.textContent = errors.has() ? "First: " + errors.first() : "No errors";
});
</brick:client>
<form brick:submit="save" novalidate>
  {{ with .Form.slug }}<label for="{{ .ID }}">List address</label>
  <input id="{{ .ID }}" brick:model.lazy="{{ .Name }}" value="{{ .Value }}"{{ if .Invalid }} aria-invalid="true"{{ end }}>
  {{ with .Error }}<p class="error">{{ . }}</p>{{ end }}{{ end }}
  <ol>
  {{ range .Form.todos.Items }}{{ with .Fields.title }}
    <li><input id="{{ .ID }}" brick:model.lazy="{{ .Name }}" value="{{ .Value }}">{{ with .Error }} <span class="error">{{ . }}</span>{{ end }}</li>
  {{ end }}{{ end }}
  </ol>
  {{ range $k, $v := .Errors }}<p class="all">{{ $k }}={{ $v }}</p>
  {{ end }}
  <button type="button" brick:click="add_todo">Add todo</button>
  <button type="button" data-first-error>Show first error</button>
  <button>Save</button>
</form>
```

- The `<brick:client>` block and the *Show first error* button belong to Step 5.
- `todos: [{min: 1}]` is a rule on the list itself: `brick.validate()` checks only items that exist, so an empty list would otherwise pass.
- `todos.*.tags.*` checks every element of every item's `tags` list.
- `slug` is not a default rule; Step 3 adds it.

## Step 3 — Add your own rule with `Options.Validator`

The engine calls `Check` once per declared rule when it compiles a file, and `Validate` with one rule at a time at render, so a wrapper can handle its own names and pass everything else to `validate.Default()`.

`main.go`

<!-- ds:block id=ruleslug-g8ueakes title="examples/forms-validation-lists/main.go" -->
**examples/forms-validation-lists/main.go** · [`examples/forms-validation-lists/main.go:31`](../../../examples/forms-validation-lists/main.go#L31-L31)

```go
"slug"
```
<!-- /ds:block hash=535879 -->

<!-- ds:block id=slugre-h5d9nfzc title="examples/forms-validation-lists/main.go" -->
**examples/forms-validation-lists/main.go** · [`examples/forms-validation-lists/main.go:36`](../../../examples/forms-validation-lists/main.go#L36-L36)

```go
var slugRE = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
```
<!-- /ds:block hash=a14efd -->

<!-- ds:block id=slugvalidator-vb8rkb35 title="examples/forms-validation-lists/main.go" -->
**examples/forms-validation-lists/main.go** · [`examples/forms-validation-lists/main.go:44`](../../../examples/forms-validation-lists/main.go#L44-L44)

```go
type slugValidator struct{ base validate.Validator }
```
<!-- /ds:block hash=99846b -->

<!-- ds:block id=slugvalidator-check-tqesyum2 title="examples/forms-validation-lists/main.go" -->
**examples/forms-validation-lists/main.go** · [`examples/forms-validation-lists/main.go:49-57`](../../../examples/forms-validation-lists/main.go#L49-L57)

```go
func (v slugValidator) Check(path string, r validate.Rule) error {
	if r.Name != ruleSlug {
		return v.base.Check(path, r)
	}
	if r.Arg != nil {
		return fmt.Errorf("%s takes no argument; write it as a bare name", ruleSlug)
	}
	return nil
}
```
<!-- /ds:block hash=6c6513 -->

<!-- ds:block id=slugvalidator-validate-z8agdfan title="examples/forms-validation-lists/main.go" -->
**examples/forms-validation-lists/main.go** · [`examples/forms-validation-lists/main.go:62-76`](../../../examples/forms-validation-lists/main.go#L62-L76)

```go
func (v slugValidator) Validate(ctx context.Context, path string, value any, rules []validate.Rule, state map[string]any, locale string) string {
	for _, r := range rules {
		if r.Name != ruleSlug {
			if msg := v.base.Validate(ctx, path, value, []validate.Rule{r}, state, locale); msg != "" {
				return msg
			}
			continue
		}
		// Like every default rule but required, an empty value passes.
		if s, _ := value.(string); s != "" && !slugRE.MatchString(s) {
			return "Use lowercase letters, digits and single dashes."
		}
	}
	return ""
}
```
<!-- /ds:block hash=892210 -->

<!-- ds:block id=newapp-zjqcqrqh title="examples/forms-validation-lists/main.go" -->
**examples/forms-validation-lists/main.go** · [`examples/forms-validation-lists/main.go:81-98`](../../../examples/forms-validation-lists/main.go#L81-L98)

```go
func newApp(dir string) (*brick.Engine, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	sc, err := scriptstarlark.New(scriptstarlark.Options{})
	if err != nil {
		return nil, err
	}
	return brick.New(brick.Options{
		FS:        root.FS(),
		Scripting: sc,
		States:    brick.NewMemoryStates(0, 0),
		// Options.Validator is the one place the rules' meaning lives; the
		// wrapper keeps the default's rules and adds slug.
		Validator: slugValidator{base: validate.Default()},
	})
}
```
<!-- /ds:block hash=8275f1 -->

The same seam takes go-playground/validator, ozzo or CUE behind a `brick.Validator`; `validate.Match`, `validate.Expand` and `validate.Exists` help a custom one handle `*` patterns (`Check` gets the declared pattern, `Validate` the concrete path).

## Step 4 — Drive both forms

`main_test.go`

<!-- ds:block id=testsignuprules-uwzra6ps title="examples/forms-validation-lists/main_test.go" -->
**examples/forms-validation-lists/main_test.go** · [`examples/forms-validation-lists/main_test.go:69-93`](../../../examples/forms-validation-lists/main_test.go#L69-L93)

```go
func TestSignupRules(t *testing.T) {
	h, page, signup, _ := start(t)
	if strings.Contains(page, `class="error"`) {
		t.Fatalf("errors before anything is touched:\n%s", page)
	}
	out := post(t, h, signup+`"action":"$set","args":["email","nope"]}`)
	mustHave(t, "$set email", out, `<p class="error">email: Enter a valid email address.</p>`)
	if strings.Contains(out, "name: ") {
		t.Fatalf("an untouched field shows an error:\n%s", out)
	}
	out = post(t, h, signup+`"action":"signup"}`)
	mustHave(t, "submit", out,
		`<p class="error">name: This field is required.</p>`,
		`<p class="error">email: Enter a valid email address.</p>`,
		`<p class="error">password: This field is required.</p>`,
		`<p class="error">confirm: This field is required.</p>`)
	out = post(t, h, signup+`"action":"signup","sets":[["name","Ada"],["email","ada@taken.example"],["password","correct horse battery"],["confirm","correct horse batter"]]}`)
	mustHave(t, "mismatch", out, `<p class="error">confirm: The passwords do not match.</p>`)
	out = post(t, h, signup+`"action":"signup","sets":[["confirm","correct horse battery"]]}`)
	mustHave(t, "taken", out, `<p class="error">email: That e-mail already has an account.</p>`)
	out = post(t, h, signup+`"action":"signup","sets":[["email","ada@example.com"]]}`)
	if strings.Contains(out, `class="error"`) || !strings.Contains(out, `brick:model.lazy="name" value=""`) {
		t.Fatalf("after a good signup:\n%s", out)
	}
}
```
<!-- /ds:block hash=ead3de -->

<!-- ds:block id=testtodolistrules-b3czke5h title="examples/forms-validation-lists/main_test.go" -->
**examples/forms-validation-lists/main_test.go** · [`examples/forms-validation-lists/main_test.go:99-124`](../../../examples/forms-validation-lists/main_test.go#L99-L124)

```go
func TestTodoListRules(t *testing.T) {
	h, page, _, todos := start(t)
	mustHave(t, "page", page,
		`<label for="b537d9f9b-slug">List address</label>`,
		`<input id="b537d9f9b-todos-1-title" brick:model.lazy="todos.1.title" value="x" name="brick:model:todos.1.title">`)
	out := post(t, h, todos+`"action":"$set","args":["slug","My List"]}`)
	mustHave(t, "$set slug", out, `value="My List" aria-invalid="true"`, `<p class="error">Use lowercase letters, digits and single dashes.</p>`)
	out = post(t, h, todos+`"action":"save"}`)
	mustHave(t, "save", out,
		`<p class="all">todos.0.title=Each todo needs a title.</p>`,
		`<p class="all">todos.1.tags.0=Must be at least 3 characters.</p>`,
		`<p class="all">todos.1.title=Must be at least 2 characters.</p>`,
		`value="x" name="brick:model:todos.1.title"> <span class="error">Must be at least 2 characters.</span>`)
	out = post(t, h, todos+`"action":"save","sets":[["slug","my-list"],["todos.0.title","Buy milk"],["todos.1.title","Call"],["todos.1.tags.0","home"]]}`)
	if strings.Contains(out, `class="error"`) || strings.Contains(out, `class="all"`) {
		t.Fatalf("after fixing every row:\n%s", out)
	}
	// A row added later is checked by the next save, not before.
	out = post(t, h, todos+`"action":"add_todo"}`)
	mustHave(t, "add_todo", out, `id="b537d9f9b-todos-3-title"`)
	if strings.Contains(out, `class="error"`) {
		t.Fatalf("a new row shows an error before save:\n%s", out)
	}
	out = post(t, h, todos+`"action":"save"}`)
	mustHave(t, "save new row", out, `<p class="all">todos.3.title=Each todo needs a title.</p>`)
}
```
<!-- /ds:block hash=2cfe62 -->

## Step 5 — Read the errors from browser code with `brick.errors`

`.Errors` is for templates. Client code that must respond to the same errors (focus the first bad field, count them, show a summary elsewhere) reads them with `brick.errors(el)`, which takes any element inside the stateful component and sends no request. The runtime reads them from the `data-brick-errors` attribute the last render left on the root, so they are never stale after an action.

`theme/components/Todos.html` (the `<brick:client>` block)

<!-- snippet: examples/forms-validation-lists/theme/components/Todos.html lines=25-35 -->
```html
<brick:client>
// Read the errors the last render left on the component, with no request.
// has() asks if there are any, first() is the message of the first path in
// path order, all() is the whole path-to-message map.
document.addEventListener("click", (event) => {
  const button = event.target.closest("[data-first-error]");
  if (!button) return;
  const errors = brick.errors(button);
  button.textContent = errors.has() ? "First: " + errors.first() : "No errors";
});
</brick:client>
```

The object has `has(path)` (no path: any error at all), `get(path)`, `first(path)` (no path: the first by path order), `all()` (a copy of the path-to-message map) and `clear(path)` (no path: all), which drops them from this view and emits `errors-cleared`; the messages the server rendered change with the next render. Called outside a stateful component it throws `brick.errors: not inside a stateful component`.

Go can check that the root carries the errors after a failed save and not after a clean one, and that the module holding the call is served. What the button then shows is the browser's part.

`main_test.go`

<!-- snippet: examples/forms-validation-lists/main_test.go lines=153-177 -->
```go
// brick.errors(el) reads the errors the last render left on the root, so
// the root must carry them: a failed save writes data-brick-errors, and a
// clean one removes it. The client module that reads them is served with
// the page; that the button then shows the message is the browser's part.
func TestErrorsReadableByTheBrowser(t *testing.T) {
	h, page, _, todos := start(t)
	if strings.Contains(page, "data-brick-errors") {
		t.Fatalf("errors before anything is touched:\n%s", page)
	}
	out := post(t, h, todos+`"action":"save"}`)
	mustHave(t, "failed save", out, `data-brick-errors="`, `todos.0.title`, `data-first-error`)
	out = post(t, h, todos+`"action":"save","sets":[["slug","my-list"],["todos.0.title","Buy milk"],["todos.1.title","Call"],["todos.1.tags.0","home"]]}`)
	if strings.Contains(out, "data-brick-errors") {
		t.Fatalf("a clean save still carries errors:\n%s", out)
	}
	m := regexp.MustCompile(`src="(/_brick/c/Todos\.[0-9a-f]+\.js)"`).FindStringSubmatch(page)
	if m == nil {
		t.Fatalf("no Todos module on the page:\n%s", page)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, m[1], nil))
	if !strings.Contains(rec.Body.String(), "brick.errors(button)") {
		t.Fatalf("module lacks brick.errors:\n%s", rec.Body)
	}
}
```

## Try it

```sh
cd examples
go test ./forms-validation-lists/ -v
```

```text
=== RUN   TestSignupRules
--- PASS: TestSignupRules (0.00s)
=== RUN   TestTodoListRules
--- PASS: TestTodoListRules (0.00s)
=== RUN   TestRuleTyposFailAtCompile
    main_test.go:149: components/F.html:6: brick: rules: slug: unknown rule "requird" (known: required, min, max, email, url, pattern, in, same)
    main_test.go:149: components/F.html:6: brick: rules: slug: slug takes no argument; write it as a bare name
--- PASS: TestRuleTyposFailAtCompile (0.00s)
=== RUN   TestErrorsReadableByTheBrowser
--- PASS: TestErrorsReadableByTheBrowser (0.00s)
PASS
ok  	github.com/ubgo/brick/examples/forms-validation-lists	0.024s
```

What the todo list rendered after the first *Save* in a real run (from the `{{ range $k, $v := .Errors }}` lines):

```html
  <p class="all">slug=Use lowercase letters, digits and single dashes.</p>
  <p class="all">todos.0.title=Each todo needs a title.</p>
  <p class="all">todos.1.tags.0=Must be at least 3 characters.</p>
  <p class="all">todos.1.title=Must be at least 2 characters.</p>
```

## Pitfalls

- **Nothing validates automatically before an action.** A *Cancel* or *Add todo* button must not be blocked by a half-filled form, so the submit action calls `brick.validate()` itself and returns `[state, errors]`.
- **A typo in a rule fails when the file is read**: `brick: rules: slug: unknown rule "requird" (known: required, min, max, email, url, pattern, in, same)`. A wrapper that delegates unknown names to the default inherits that message, so the list names only the default rules; write your own `Check` message if you want yours listed.
- **An action may not reuse a built-in's name.** This example's add-row action was first called `add`, and the page failed with `components/Todos.html:14: <brick:script> function "add" shadows a built-in or an import (C12)`.
- **`*` matches one list index, never a dict key**, and cannot be the first segment; where `*` stands the declared value must be a list (or `null`). A dict has named fields, so name them.
- **A row added later shows no error until something touches it.** `TestTodoListRules` adds a row, sees no error, and only the next `brick.validate()` reports `todos.3.title`.
- **The browser runs no rule engine.** Add native `required`, `minlength` or `type="email"` yourself for instant hints; brick never injects attributes.

## Related

- [Deferred fields](deferred-fields.md), [Forms without JavaScript](without-javascript.md) (rule errors come back on the 422 page), [Uploads](uploads.md) (`avatar: [required]` works on an upload field).
- `brick.errors(el)` reads a component's errors in client code: [Validation — rules declared once](../../guide.md#validation--rules-declared-once).
- Feature index rows: [Forms, validation and uploads](../../reference/features.md#forms-validation-and-uploads).
- Example: [examples/forms-validation-lists](../../../examples/forms-validation-lists/).
