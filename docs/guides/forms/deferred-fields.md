# Deferred fields, lists and nested paths

## What it is and when you want it

`brick:model` binds an input to a field of a stateful component's state. Plain `brick:model` sends a `$set` after each pause in typing, so a five-field form costs five requests before anyone clicks *Save*. Mark the fields `.defer` and they send nothing on their own: their values ride along with the component's next action, as `sets`, in the same request. Use it for any form that is filled in and then submitted; keep live `brick:model` for a search box or a filter that should respond while the user types.

## Before you start

- A stateful component (`state:` in its frontmatter) with an action in `<brick:script>`, as in [Actions](../../guide.md#actions--a-button-that-changes-server-state).
- An engine with scripting and a state store. The example mounts the engine on a plain `net/http` mux; the site layer (`brick serve`) mounts the same endpoints for you.
- The example: [examples/forms-signup](../../../examples/forms-signup/).

## Step 1 — Write the form with deferred fields

Each field holds its value in the browser until an action runs, so filling the form costs no requests and the server sees every value at once.

`theme/components/Signup.html`

<!-- snippet: examples/forms-signup/theme/components/Signup.html -->
```html
---
state:
  name: ""
  email: ""
  tags: []
  address: {city: ""}
  saves: 0
---
<brick:script>
def save(state, props):
    return dict(state, saves = state["saves"] + 1)

def clear(state, props):
    return dict(state, name = "", email = "", tags = [], address = {"city": ""}, saves = 0)
</brick:script>
<form brick:submit="save">
  <input brick:model.defer="name" value="{{ .State.name }}">
  <input brick:model.defer="email" value="{{ .State.email }}">
  <input brick:model.defer="address.city" value="{{ .State.address.city }}">
  <label><input type="checkbox" brick:model.defer="tags" value="go"> Go</label>
  <label><input type="checkbox" brick:model.defer="tags" value="js"> JS</label>
  <p>Hello, <b brick:text="name">{{ .State.name }}</b></p>
  <p id="saved">Saves: {{ .State.saves }}, topics: {{ .State.tags }}, city: {{ .State.address.city }}</p>
  <p brick:dirty hidden>Unsaved changes.</p>
  <span brick:loading.delay="save" hidden>Still saving…</span>
  <button type="button" brick:click="clear" brick:confirm="Clear the form and everything saved?">Clear</button>
  <button brick:disable-with="Saving…">Save</button>
</form>
```

What each attribute does:

- `brick:model.defer="name"` keeps the value in the browser until the component's next request of any kind (a click, a submit, a key, a poll). That request carries every held value as `sets`, and the server applies them **before** the action, through the same checks as `$set`, all or nothing.
- `brick:model.defer="tags"` on two checkboxes binds as a **list**, because `tags` is a list in `state:`: the value is the checked boxes' `value`s in page order, even with one box. brick knows which fields are lists from the state and writes `data-brick-arrays="tags"` on the root, so nothing guesses from the markup. `<select multiple>` is always a list.
- `brick:model.defer="address.city"` is a dotted path. The first part must be a declared state field and every later part must already exist (`items.0.qty` works the same way for a list index). The value is converted to the type of what is there now.
- `brick:text="name"` follows the field in the browser with no request; so do `brick:show` and `brick:dirty`.
- `<p brick:dirty hidden>` shows while any field differs from what the server last rendered: the honest way to say "nothing has been sent yet".
- `brick:loading.delay="save"` shows only while `save` runs and only after 200 ms, so a fast save never flashes it.
- `brick:confirm` asks before `clear` runs; on Cancel nothing is sent.
- `brick:disable-with="Saving…"` disables the button and swaps its label while its form's submit is in flight.

## Step 2 — Put the component on a page

The page is plain HTML; `{{ assets }}` is where the runtime's script tags go.

`theme/content/index.html`

<!-- snippet: examples/forms-signup/theme/content/index.html -->
```html
<!doctype html>
<html><body>
<h1>Join the newsletter</h1>
<Signup />
<Cart />
{{ assets }}
</body></html>
```

## Step 3 — Mount the engine

A stateful component needs a state store and the action endpoint; the asset handler serves the runtime the page links.

`main.go`

<!-- ds:block id=page-8we867s4 title="examples/forms-signup/main.go" -->
**examples/forms-signup/main.go** · [`examples/forms-signup/main.go:23`](../../../examples/forms-signup/main.go#L23-L23)

```go
"content/index.html"
```
<!-- /ds:block hash=dbf526 -->

<!-- ds:block id=newapp-afwdpfg2 title="examples/forms-signup/main.go" -->
**examples/forms-signup/main.go** · [`examples/forms-signup/main.go:29-39`](../../../examples/forms-signup/main.go#L29-L39)

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
	return brick.New(brick.Options{FS: root.FS(), Scripting: sc, States: brick.NewMemoryStates(0, 0)})
}
```
<!-- /ds:block hash=7515c7 -->

<!-- ds:block id=handler-htt4fe93 title="examples/forms-signup/main.go" -->
**examples/forms-signup/main.go** · [`examples/forms-signup/main.go:45-59`](../../../examples/forms-signup/main.go#L45-L59)

```go
func handler(eng *brick.Engine) http.Handler {
	mux := http.NewServeMux()
	mux.Handle(brick.DefaultAssetBase+brick.ActionEndpoint, eng.ActionHandler())
	mux.Handle(brick.DefaultAssetBase, assetshttp.Handler(brick.DefaultAssetBase, func() assetshttp.Source { return eng }))
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		html, err := eng.Render(r.Context(), page, nil)
		if err != nil {
			http.Error(w, "render failed", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(html)
	})
	return mux
}
```
<!-- /ds:block hash=ab37be -->

## Step 4 — Send the deferred values with the action

This is the request the runtime sends when the user types *Ada*, *ada@x.io* and *Oslo*, ticks *Go* and clicks *Save*: one action, every held value in `sets`. The test sends it the same way and checks the answer.

`main_test.go`

<!-- snippet: examples/forms-signup/main_test.go lines=27-78 -->
```go
// Deferred fields send nothing on their own: one Save carries every held
// value as sets, the server applies them before the action, and a refused
// set fails the whole request without touching state.
func TestSignupDeferredSets(t *testing.T) {
	eng, err := newApp("theme")
	if err != nil {
		t.Fatal(err)
	}
	h := handler(eng)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	page := rec.Body.String()
	for _, want := range []string{
		`data-brick-name="Signup" data-brick-arrays="tags" brick:submit="save" method="post">`,
		`<input brick:model.defer="address.city" value="" name="brick:model:address.city">`,
		`<p id="saved">Saves: 0, topics: [], city: </p>`,
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("page lacks %s:\n%s", want, page)
		}
	}
	m := instanceRE.FindStringSubmatch(page)
	if m == nil {
		t.Fatal("no stateful root")
	}
	inst := `{"handle":"` + m[1] + `","token":"` + m[2] + `",`

	code, body := post(t, h, inst+`"action":"save","sets":[["name","Ada"],["email","ada@x.io"],["tags",["go"]],["address.city","Oslo"]]}`)
	if code != http.StatusOK || !strings.Contains(body, `<p>Hello, <b brick:text="name">Ada</b></p>`) || !strings.Contains(body, `<p id="saved">Saves: 1, topics: [go], city: Oslo</p>`) {
		t.Fatalf("save with sets: %d\n%s", code, body)
	}

	for _, c := range []struct{ sets, want string }{
		{`[["name","Bo"],["address.zip","0150"]]`, `brick: field value refused: address.zip: no key "zip" (a client cannot add one)`},
		{`[["nmae","Bo"]]`, `brick: field value refused: "nmae" is not a field of this component's state`},
	} {
		code, body = post(t, h, inst+`"action":"save","sets":`+c.sets+`}`)
		if code != http.StatusUnprocessableEntity || strings.TrimSpace(body) != c.want {
			t.Fatalf("refused set %s: %d %q", c.sets, code, body)
		}
	}

	// The refused request left state alone: name is still Ada, not Bo.
	code, body = post(t, h, inst+`"action":"save"}`)
	if code != http.StatusOK || !strings.Contains(body, `<b brick:text="name">Ada</b>`) || !strings.Contains(body, `Saves: 2, topics: [go]`) {
		t.Fatalf("save after refusal: %d\n%s", code, body)
	}
	code, body = post(t, h, inst+`"action":"clear"}`)
	if code != http.StatusOK || !strings.Contains(body, `<p id="saved">Saves: 0, topics: [], city: </p>`) {
		t.Fatalf("clear: %d\n%s", code, body)
	}
}
```

## Step 5 — Bind list rows and choose how to ask before you act

A list row is a path like any other, so each cart line gets its own deferred field, and `brick:confirm` can ask with your own dialogs instead of the browser's. The `Cart` component below shows both; the page from Step 2 places it after `<Signup />` (`<Cart />`).

`theme/components/Cart.html`

<!-- snippet: examples/forms-signup/theme/components/Cart.html -->
```html
---
state:
  label: Cart
  items:
    - {name: Tea, qty: 1}
    - {name: Cake, qty: 2}
  total: 0
---
<brick:script>
def checkout(state, props):
    total = 0
    for item in state["items"]:
        total += item["qty"]
    return dict(state, total = total)

def rename(state, props, answer):
    return dict(state, label = answer)

def empty_cart(state, props):
    return dict(state, items = [], total = 0)
</brick:script>
<brick:client>
// Ask with the app's own dialogs instead of window.confirm and
// window.prompt. Each call returns a function that puts the previous
// asker back; this page never needs it, so it is kept only to show it.
const restoreConfirm = brick.confirmWith((message, el) =>
  Promise.resolve(window.confirm("[cart] " + message)));
const restorePrompt = brick.promptWith((message, el) =>
  Promise.resolve(window.prompt(message, el.dataset.default || "")));
window.addEventListener("pagehide", () => { restoreConfirm(); restorePrompt(); });
</brick:client>
<form brick:submit="checkout">
  <h2 id="label">{{ .State.label }}</h2>
  <input brick:model.defer="items.0.qty" value="{{ (index .State.items 0).qty }}">
  <input brick:model.defer="items.1.qty" value="{{ (index .State.items 1).qty }}">
  <p id="total">Total: {{ .State.total }}</p>
  <button type="button" brick:click="rename" brick:confirm.prompt="Name this cart?" data-default="Cart">Rename</button>
  <button type="button" brick:click="empty_cart" brick:confirm="Empty the cart?">Empty</button>
  <button>Check out</button>
</form>
```

- `brick:model.defer="items.0.qty"` names the first row's `qty`. The first part must be a declared state field and every later part must already exist, so the client changes a value, never the shape of the list. A held value arrives as the string the input holds and is converted to the type of what is there now (an int here).
- `brick.confirmWith(fn)` replaces what `brick:confirm` asks with. `fn(message, el)` returns a boolean or a promise of one, and the call returns a function that puts the previous asker back. `brick.promptWith(fn)` does the same for `brick:confirm.prompt`: return the text, `null` for Cancel, or a promise of either.
- `brick:confirm.prompt="Name this cart?"` asks for text first and passes the answer as the action's last argument, so `rename(state, props, answer)` receives it.

Only a browser draws a dialog and waits for the answer. The tests check the parts that are Go: the held values land through `sets`, a row past the end of the list is refused, and the page links the module that holds `brick.confirmWith` and `brick.promptWith`.

`main_test.go`

<!-- snippet: examples/forms-signup/main_test.go lines=80-110 -->
```go
// A list index is a path like any other: items.0.qty names the first
// row's qty, the held value arrives as the string an input holds, and the
// server converts it to the type there now (an int). A path past the end
// of the list is refused like any key the client cannot add.
func TestCartNestedPaths(t *testing.T) {
	eng, err := newApp("theme")
	if err != nil {
		t.Fatal(err)
	}
	h := handler(eng)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	page := rec.Body.String()
	ms := instanceRE.FindAllStringSubmatch(page, -1)
	if len(ms) != 2 {
		t.Fatalf("want Signup and Cart roots, got %d:\n%s", len(ms), page)
	}
	inst := `{"handle":"` + ms[1][1] + `","token":"` + ms[1][2] + `",`
	if !strings.Contains(page, `<input brick:model.defer="items.1.qty" value="2" name="brick:model:items.1.qty">`) {
		t.Fatalf("the list rows are not bound by index:\n%s", page)
	}

	code, body := post(t, h, inst+`"action":"checkout","sets":[["items.0.qty","3"],["items.1.qty","4"]]}`)
	if code != http.StatusOK || !strings.Contains(body, `<p id="total">Total: 7</p>`) {
		t.Fatalf("checkout with sets: %d\n%s", code, body)
	}
	code, body = post(t, h, inst+`"action":"checkout","sets":[["items.2.qty","1"]]}`)
	if code != http.StatusUnprocessableEntity || !strings.HasPrefix(strings.TrimSpace(body), "brick: field value refused: items.2.qty") {
		t.Fatalf("a row past the end: %d %q", code, body)
	}
}
```

<!-- snippet: examples/forms-signup/main_test.go lines=112-151 -->
```go
// The Cart's confirmer and prompter are browser code: Go can only check
// that the module reaches the page and holds the calls. What the dialogs
// look like, and that a click waits for the answer, only a browser shows.
func TestCartAsksServed(t *testing.T) {
	eng, err := newApp("theme")
	if err != nil {
		t.Fatal(err)
	}
	h := handler(eng)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	page := rec.Body.String()
	for _, want := range []string{
		`brick:click="rename" brick:confirm.prompt="Name this cart?"`,
		`brick:click="empty_cart" brick:confirm="Empty the cart?"`,
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("page lacks %s:\n%s", want, page)
		}
	}
	m := regexp.MustCompile(`src="(/_brick/c/Cart\.[0-9a-f]+\.js)"`).FindStringSubmatch(page)
	if m == nil {
		t.Fatalf("no Cart module on the page:\n%s", page)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, m[1], nil))
	for _, want := range []string{`brick.confirmWith(`, `brick.promptWith(`} {
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), want) {
			t.Fatalf("module lacks %s (%d):\n%s", want, rec.Code, rec.Body)
		}
	}

	// The prompt's answer is the action's last argument.
	m2 := instanceRE.FindAllStringSubmatch(page, -1)
	inst := `{"handle":"` + m2[1][1] + `","token":"` + m2[1][2] + `",`
	code, body := post(t, h, inst+`"action":"rename","args":["Gifts"]}`)
	if code != http.StatusOK || !strings.Contains(body, `<h2 id="label">Gifts</h2>`) {
		t.Fatalf("rename: %d\n%s", code, body)
	}
}
```

## Try it

```sh
cd examples
go test ./forms-signup/ -v
```

```text
=== RUN   TestSignupDeferredSets
--- PASS: TestSignupDeferredSets (0.00s)
=== RUN   TestCartNestedPaths
--- PASS: TestCartNestedPaths (0.00s)
=== RUN   TestCartAsksServed
--- PASS: TestCartAsksServed (0.00s)
=== RUN   TestSignupLinksRuntime
--- PASS: TestSignupLinksRuntime (0.00s)
=== RUN   TestFormTyposFailAtCompile
--- PASS: TestFormTyposFailAtCompile (0.00s)
=== RUN   TestMisspeltActionFailsAtRunTime
--- PASS: TestMisspeltActionFailsAtRunTime (0.00s)
PASS
ok  	github.com/ubgo/brick/examples/forms-signup	0.021s
```

Or run the server and send the action by hand (`go run ./forms-signup -addr 127.0.0.1:18084`, then read `data-brick-state` and `data-brick-token` from the page). A real run printed:

```text
POST /_brick/action  {"action":"save","sets":[["name","Ada"],["email","ada@x.io"],["tags",["go"]],["address.city","Oslo"]],…}
  <p>Hello, <b brick:text="name">Ada</b></p>
  <p id="saved">Saves: 1, topics: [go], city: Oslo</p>

POST /_brick/action  {"action":"save","sets":[["address.zip","0150"]],…}
422 brick: field value refused: address.zip: no key "zip" (a client cannot add one)
```

## Pitfalls

- **A deferred field without a following action is never sent.** That is what the mode means. If the form has no submit or click, the values stay in the browser; show `brick:dirty` so the user can tell.
- **The client changes values, never shape.** `sets` and `$set` cannot add a key, grow a list or replace a whole dict. The test pins the answer: `422 brick: field value refused: address.zip: no key "zip" (a client cannot add one)`. Add rows in an action, then bind their parts by path.
- **One bad set refuses the whole request.** A refused set answers 422 and leaves state untouched (the test saves again afterwards and the name is still *Ada*, not the *Bo* sent beside the bad set). The runtime keeps the held values and sends them with the next request.
- **Typos fail when the file is read**, with the file, line and column: `brick:model="nmae": "nmae" is not a field of this component's state (declared: name, tags)`, `brick:model.defer.lazy: .defer cannot be combined with .lazy (brick:model accepts .lazy, .defer, .debounce.<ms>, .throttle.<ms>)`, `brick:click.onec: unknown modifier "onec" (…)` and `brick:loading: "sve" is not a function of this component (have: save, $refresh)`. Each is pinned by `TestFormTyposFailAtCompile` in `typos_test.go`.
- **A misspelt action name is not caught at compile time.** The compiler checks an action call's syntax (a name, JSON arguments), not that the function exists, so `brick:submit="sve"` compiles and the click answers `400 brick: unknown action "sve" on <F>` (pinned by `TestMisspeltActionFailsAtRunTime`).
- **An action may not reuse a built-in's name.** `def add(state, props)` fails with `function "add" shadows a built-in or an import (C12)`; name it `add_todo`.
- **A live `brick:model` on the same field drops the held value**, because the live one is newer. Do not mix `.defer` and live bindings on one field.

## Related

- [Forms without JavaScript](without-javascript.md): the same form when the runtime is not there.
- [Validation](validation.md): rules declared once, errors for touched fields.
- Runtime functions used here: `brick.confirmWith` and `brick.promptWith` in the [runtime API reference](../../reference/runtime-api.md).
- [Uploads](uploads.md) and [Method links](method-links.md).
- The full modifier table: [Forms — deferred fields, arrays, modifiers, loading and confirm](../../guide.md#forms--deferred-fields-arrays-modifiers-loading-and-confirm).
- Feature index rows: [Forms, validation and uploads](../../reference/features.md#forms-validation-and-uploads).
- Example: [examples/forms-signup](../../../examples/forms-signup/).
