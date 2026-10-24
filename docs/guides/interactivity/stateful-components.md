# Stateful components and actions

## What it is and when you want it

A stateful component keeps its own data on the server and changes it in response to clicks, typing and other events, without a page reload and without an API or client code of your own. You declare the data under `state:`, write the changes as Starlark functions, and point `brick:click` (or another event attribute) at them. brick saves the new state, re-renders only that component, and the browser patches it in place. You want it for a like button, a counter, a cart line, a filter panel: anything where the page changes something on the server and shows the result.

## Before you start

- You know how to write a component and pass props: see the guide's [components](../../guide.md#components) and [props](../../guide.md#passing-values--props) sections.
- Actions are Starlark functions, so the engine needs a `Scripting` backend ([`contrib/script-starlark`](../../../contrib/script-starlark/); the `brick` command has it built in) and a state store (`Options.States`).
- The example is [`examples/interactivity-counter`](../../../examples/interactivity-counter/).

## Step 1 — Give the engine scripting and a state store

An action needs two things the engine does not have by default: something to run Starlark, and somewhere to keep each visitor's state. `brick.NewMemoryStates(0, 0)` keeps state in this process; [state that survives a restart](../../guide.md#state-that-survives-a-restart--brick_secret) covers sealed and shared stores. From `examples/interactivity-counter/main.go`:

<!-- ds:block id=newapp-t4rzxrdx title="examples/interactivity-counter/main.go" -->
**examples/interactivity-counter/main.go** · [`examples/interactivity-counter/main.go:34-48`](../../../examples/interactivity-counter/main.go#L34-L48)

```go
func newApp(dir string) (*app, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	sc, err := scriptstarlark.New(scriptstarlark.Options{})
	if err != nil {
		return nil, err
	}
	eng, err := brick.New(brick.Options{FS: root.FS(), Scripting: sc, States: brick.NewMemoryStates(0, 0)})
	if err != nil {
		return nil, err
	}
	return &app{eng: eng}, nil
}
```
<!-- /ds:block hash=78e996 -->

## Step 2 — Mount the action endpoint

The browser runtime posts every action to `/_brick/action`. The site layer and `brick serve` mount it for you; with the engine alone, mount `eng.ActionHandler()` there, and the asset handler that serves the runtime. A stateful component is an island (every visitor gets their own), so the page is rendered with `RenderChunks` and filled per request with `Fill`:

<!-- ds:block id=app-handler-axu3tu64 title="examples/interactivity-counter/main.go" -->
**examples/interactivity-counter/main.go** · [`examples/interactivity-counter/main.go:53-74`](../../../examples/interactivity-counter/main.go#L53-L74)

```go
func (a *app) handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle(brick.DefaultAssetBase+brick.ActionEndpoint, a.eng.ActionHandler())
	mux.Handle(brick.DefaultAssetBase, assetshttp.Handler(brick.DefaultAssetBase, func() assetshttp.Source { return a.eng }))
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		res, err := a.eng.RenderChunks(r.Context(), page, nil)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		// Fill renders the islands (here, the stateful Counter) and the
		// asset block for this request.
		html, err := a.eng.Fill(r.Context(), res)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(html)
	})
	return mux
}
```
<!-- /ds:block hash=3e49ab -->

## Step 3 — Declare state and write the actions

`state:` lists the fields and their first values. Each Starlark function takes the current state and the props, plus any arguments the call passes, and returns the new state. `brick:click="bump(10)"` calls `bump(state, props, 10)`; arguments are JSON values, so a string needs double quotes and the attribute then takes single quotes. Four built-ins need no function at all: `$set(path, value)`, `$toggle(path)`, `$reset(path…)` and `$refresh`.

Two hook words run your functions at fixed moments: `init:` once when an instance is created (here, to start the count from the `start` prop), and `updated:` after `$set`, `$toggle`, `$reset` or a `brick:model` field changes a value (here, to trim the label). `brick.skip_render()` plus `brick.reply(value)` make a renderless action that answers JSON instead of HTML. `components/Counter.html`:

<!-- snippet: examples/interactivity-counter/theme/components/Counter.html -->
```html
---
props:
  start: {type: int, default: 0}
state:
  count: 0
  open: false
  label: ""
brick:
  init: setup
  updated: tidy
---
<brick:script>
def setup(state, props):
    return dict(state, count = props["start"])

def bump(state, props, n):
    return dict(state, count = state["count"] + n)

def tidy(state, props, paths):
    return dict(state, label = state["label"].strip())

def peek(state, props):
    brick.skip_render()
    brick.reply({"count": state["count"], "double": state["count"] * 2})
    return state
</brick:script>
<div class="counter">
  <output id="count">{{ .State.count }}</output>
  <button id="plus" brick:click="bump(1)">+1</button>
  <button id="plus10" brick:click="bump(10)">+10</button>
  <button id="reset" brick:click='$reset("count")'>Reset</button>
  <button id="details" brick:click='$toggle("open")'>Details</button>
  <input id="label" brick:model="label" value="{{ .State.label }}">
  <p id="panel" brick:show="open" {{ if not .State.open }}hidden{{ end }}>Label: {{ .State.label }}</p>
</div>
```

`brick:model="label"` sends a `$set` of `label` after each pause in typing, and `brick:show="open"` shows or hides the panel in the browser at once, with no request. Put the page around it, `content/index.html`:

<!-- snippet: examples/interactivity-counter/theme/content/index.html -->
```html
<!doctype html>
<html><body>
<h1>Counter</h1>
<Counter start="5" />
{{ assets }}
</body></html>
```

## Step 4 — Call a renderless action from code

`peek` keeps the state, skips the render and attaches a reply. From client code (a `<brick:client>` block or a `brick.mount` hook), `brick.action(el, name, args)` calls the action of the component around `el` and resolves with the reply:

```js
const { count, double } = await brick.action(document.querySelector("#count"), "peek", []);
```

A `brick:click` ignores the reply. The answer carries the component's new handle instead of HTML, because a sealed store's handle is the state itself. In Go, `brick.EffectsFrom(ctx).Reply(v)` and `.SkipRender()` do the same from middleware.

## Step 5 — Nest a component that binds to its parent (Modelable)

A stepper or a picker owns its own buttons, but the parent needs its value. In the child, `brick: {model: value}` names the one field a parent may bind; in the parent, `<Stepper brick:model="qty" />` binds its own `qty`. `components/Stepper.html`:

<!-- snippet: examples/interactivity-counter/theme/components/Stepper.html -->
```html
---
state:
  value: 1
brick:
  model: value
---
<brick:script>
def dec(state, props):
    return dict(state, value = max(0, state["value"] - 1))

def inc(state, props):
    return dict(state, value = state["value"] + 1)
</brick:script>
<span class="stepper">
  <button id="dec" brick:click="dec">−</button>
  <b id="value">{{ .State.value }}</b>
  <button id="inc" brick:click="inc">+</button>
</span>
```

`components/Cart.html`:

<!-- snippet: examples/interactivity-counter/theme/components/Cart.html -->
```html
---
state:
  qty: 2
---
<div class="cart">
  <Stepper brick:model="qty" />
  <button id="one" brick:click='$set("qty", 1)'>Reset to 1</button>
  <span id="total">Total: ${{ mul .State.qty 12 }}</span>
</div>
```

The parent's value is written into the child whenever the parent renders it, so the stepper starts at 2, not at its own 1. When one of the stepper's actions changes `value`, the browser runtime sends the parent a `$set` of `qty`, so the total follows. Each direction has one owner per round trip, so a change stops after one trip each way.

A stateful child inside a stateful parent keeps its own state when the parent acts: the runtime tells the server which children the page already has, and the server answers `<brick-keep>` in their place. A prop the kept child must follow opts in with `reactive: true` in its `props:`; in a `{{ range }}`, give each stateful child `key=` so its state follows the row, not the position. See the guide's [actions section](../../guide.md#actions--a-button-that-changes-server-state).

## Step 6 — Keep nested rows and let them follow the parent

A parent that lists stateful rows has two questions to answer: does a row keep what the visitor did to it (an open panel, half-typed text) when the parent acts, and does it still show the parent's new data? A row keeps its state by default; a prop follows the parent only when it opts in with `reactive: true`. `components/ContactsApp.html` is the parent, and its `{{ range }}` keys each row with `:key=".id"`:

<!-- snippet: examples/interactivity-nested/theme/components/ContactsApp.html -->
```html
---
state:
  contacts:
    - {id: 1, name: Ada}
    - {id: 2, name: Grace}
    - {id: 3, name: Linus}
---
<brick:script>
def rename(state, props, id, name):
    return dict(state, contacts = [dict(c, name = name) if c["id"] == id else c for c in state["contacts"]])

def remove(state, props, id):
    return dict(state, contacts = [c for c in state["contacts"] if c["id"] != id])
</brick:script>
<section class="contacts">
  <ul>{{ range .State.contacts }}
    <li><ContactRow :contact="." :key=".id" /></li>{{ end }}
  </ul>
</section>
```

`components/ContactRow.html` declares its own `open` state and one reactive prop:

<!-- snippet: examples/interactivity-nested/theme/components/ContactRow.html -->
```html
---
state:
  open: false
props:
  contact: {type: map, reactive: true}
---
<div class="row">
  <button class="toggle" brick:click='$toggle("open")'>{{ .Props.contact.name }}</button>
  {{ if .State.open }}<em class="notes">Notes for {{ .Props.contact.name }}</em>{{ end }}
</div>
```

The test opens Grace's row, renames her through the parent, then removes Ada. Grace's row stays open each time, shows the new name (the reactive prop), and does not slide into Ada's place:

<!-- snippet: examples/interactivity-nested/main_test.go lines=19-47 -->
```go
func TestRowsKeepStateAndFollowReactiveProps(t *testing.T) {
	a, err := newApp("theme")
	if err != nil {
		t.Fatal(err)
	}
	live := bricktest.Live(t, a.eng, page)
	app := live.Component("ContactsApp")
	rows := app.Children()
	if len(rows) != 3 {
		t.Fatalf("want 3 rows, got %d", len(rows))
	}
	rows[1].Click(".toggle") // Grace's notes open
	rows[1].AssertSet(t, "open", true)

	app.Call("rename", 2, "Grace H.") // the parent acts; Grace's row is kept
	html := app.HTML()
	if !strings.Contains(html, "Notes for Grace H.") {
		t.Fatalf("the open row lost its state or its reactive prop:\n%s", html)
	}
	if strings.Contains(html, "Notes for Ada") || strings.Contains(html, "Notes for Linus") {
		t.Fatalf("a closed row opened:\n%s", html)
	}

	app.Call("remove", 1) // Ada leaves; Grace must not inherit Ada's slot
	html = app.HTML()
	if strings.Contains(html, "Ada") || !strings.Contains(html, "Notes for Grace H.") {
		t.Fatalf("state followed the position, not the contact:\n%s", html)
	}
}
```

A prop without `reactive: true` keeps the value of the child's first render, which is cheaper: a plain keep reads nothing from the store. `reactive: true` needs a `state:` on the child and a boolean value, or the file fails to compile with an error that names `reactive`. Without `:key`, a repeated stateful child is identified by its position; Dev warns `give it key=`, and the example's `TestMissingKeyWarns` pins the warning for an unkeyed call and its absence for a keyed one.

## Step 7 — Look at a live instance and hear when one ends

Two things a host sometimes needs about instances are in Go, never in the page. `Engine.Instance(ctx, handle)` returns a read-only view of the instance behind a page's `data-brick-state` value: its component, version, props and state. It writes nothing and needs no token, because it is for code that already holds the engine; a sealed handle opens only under the session it was rendered for (pass `brick.WithSession`). The dev toolbar lists instances at `/_brick/instances` the same way. A route of your own, which must sit behind your own authentication:

<!-- snippet: examples/interactivity-nested/main.go lines=90-103 -->
```go
// inspect answers /inspect?handle=default.xxxx with the instance's state
// as JSON, the way the dev toolbar lists them at /_brick/instances. Engine.Instance is a
// Go API: it is never reachable over HTTP by itself, and a sealed handle
// opens only under the session it was rendered for (pass
// brick.WithSession). Mount a route like this only behind your own auth.
func (a *app) inspect(w http.ResponseWriter, r *http.Request) {
	view, err := a.eng.Instance(r.Context(), r.URL.Query().Get(handleParam))
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"component": view.Component, "version": view.Version, "state": view.State, "props": view.Props}) // headers are sent; a failed write has no one left to tell
}
```

An `InstanceEndHook` hears when an instance ends: `expired` (its time to live passed), `evicted` (the memory store's size limit pushed it out) or `revoked` (`Engine.RevokeSession` ended it with its session). It describes an instance that no longer exists, so it carries no request; use it to delete the files its pending uploads name or to count sessions. Register it in `Options.Hooks`:

<!-- snippet: examples/interactivity-nested/main.go lines=42-50 -->
```go
// OnInstanceEnd implements brick.InstanceEndHook. It describes an
// instance that no longer exists, so it has no request; it runs on the
// goroutine that noticed the end (a sweep, an eviction, a revoke), which
// is why it only appends under a lock and returns.
func (e *ends) OnInstanceEnd(_ context.Context, end brick.InstanceEnd) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.list = append(e.list, end)
}
```

<!-- snippet: examples/interactivity-nested/main.go lines=65-88 -->
```go
// newApp builds the engine with the end hook registered in Options.Hooks:
// the hook fires for the stores that can see an instance end (the memory
// store here), and the engine logs a warning for one that cannot.
func newApp(dir string) (*app, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	sc, err := scriptstarlark.New(scriptstarlark.Options{})
	if err != nil {
		return nil, err
	}
	log := &ends{}
	eng, err := brick.New(brick.Options{
		FS:        root.FS(),
		Scripting: sc,
		States:    brick.NewMemoryStates(0, 0),
		Hooks:     []any{log},
	})
	if err != nil {
		return nil, err
	}
	return &app{eng: eng, ends: log}, nil
}
```

The test renders the page under a session, signs the session out and checks that the hook heard all four instances (the app and its three rows) with the reason `revoked`:

<!-- snippet: examples/interactivity-nested/main_test.go lines=112-134 -->
```go
func TestEndHookHearsRevoke(t *testing.T) {
	a, err := newApp("theme")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Demo-Session", "sess-1")
	a.handler().ServeHTTP(httptest.NewRecorder(), req)

	if err := a.eng.RevokeSession(context.Background(), "sess-1"); err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range a.ends.got() {
		if e.Reason != brick.EndRevoked || e.Store != "default" {
			t.Errorf("end = %+v", e)
		}
		names = append(names, e.Instance.Name)
	}
	if len(names) != 4 { // the ContactsApp and its three rows
		t.Fatalf("heard %v, want ContactsApp and three ContactRow", names)
	}
}
```

## Try it

```sh
cd examples/interactivity-counter && go run .
```

Open http://localhost:8090/. The page the server sends (`curl -s localhost:8090/`), with the handle and token that change on every request:

```html
<h1>Counter</h1>
<div data-brick="bfe9dc222" data-brick-state="default.<handle>" data-brick-token="<token>" data-brick-name="Counter" class="counter">
  <output id="count">5</output>
  <button id="plus" brick:click="bump(1)">+1</button>
  ...
```

A click on "+10" is one POST, which the example's test sends exactly as the runtime does:

```text
POST /_brick/action   Content-Type: application/json   Brick-Action: 1
{"handle":"default.3ab3…","token":"6ba4…","action":"bump","args":[10]}
← <div data-brick="bfe9dc222" … class="counter">
    <output id="count">15</output>
```

The test drives every step of this page with `bricktest.Live`: `cd examples && go test ./interactivity-counter/` prints `ok  github.com/ubgo/brick/examples/interactivity-counter`.

## Pitfalls

- **`$reset` goes back to `state:`, not to `init:`'s output.** The counter starts at 5 because `init:` set it from the prop, but `$reset("count")` makes it 0. The test pins this (`c.AssertSet(t, "count", 0)` after Reset). Write an action if "reset" should mean "back to where it started".
- **Some function names are taken.** A `<brick:script>` function named like a template function fails the compile: `<brick:script> function "add" shadows a built-in or an import (C12)` (also `pick`, `join` and every other template function). `load` is a Starlark keyword, so `def load(...)` fails with `not an identifier`. Name actions after what they do: `add_row`, `choose`, `fill`.
- **A misspelled action name is not a compile error.** `brick:click="bmup"` compiles; the click answers 400 (`brick: unknown action`). Modifier typos and bad JSON arguments are compile errors; see [events and modifiers](events-and-modifiers.md). `bricktest`'s `TryCall` returns `brick.ErrUnknownAction` for it.
- **`updated:` never runs after a named action.** It runs after `$set`, deferred sets, `$toggle` and `$reset` only; a named action owns its state. Its own changes do not trigger it again.
- **`{{ assets }}` is where the runtime is linked.** A page with a stateful component gets `<script type="module" src="/_brick/c/runtime.….js">` in its asset block with the default options; a stateful component always gets it, even one chosen by `<brick:render>` at fill time; leave out `{{ assets }}` and the buttons render but nothing listens to them. The example's `TestCounterLinksRuntime` pins it.
- **A stateful component is always per request.** It renders as an island, so the page around it can be cached but the component cannot. Render the page once with `RenderChunks` and `Fill` it per request (as the other examples do with `sync.Once`), or let the site layer do both.
- **An end hook fires only for a store that can see instances end.** A sealed store keeps state in the handle itself and never sees an end; the engine logs `brick: state store reports no instance ends; OnInstanceEnd never fires for it`. Redis deletes an expired key by itself and cannot report expiry, so it reports the other reasons only (the engine logs which it never reports). The hook runs on the goroutine that noticed the end, so keep it short.
- **`Engine.Instance` answers `ErrGone` for a handle it cannot open**: an unknown store name, a key that has expired, or a sealed handle asked under another session. The example's `/inspect` route turns that into a 404.
- **Give every repeated stateful child a key.** `:key=".id"` makes a row's state follow the contact; without it the state follows the position, so after a delete each later row would show its neighbour's state. A prop the row must follow opts in with `reactive: true`.

## Related

- [State in the URL, the session and the server](url-and-server-state.md): `query:`, `params:`, `remember:`, `locked:`, `writable:`, `temporary:`.
- [Events and modifiers](events-and-modifiers.md): every event attribute beyond `brick:click`.
- [Morph controls, transitions and client commands](morph-transitions-client-commands.md).
- [Attribute reference](../../reference/attributes.md) and [feature index](../../reference/features.md#stateful-components-and-actions).
- Examples: [`examples/interactivity-counter`](../../../examples/interactivity-counter/) and, for Step 6 and Step 7, [`examples/interactivity-nested`](../../../examples/interactivity-nested/).
