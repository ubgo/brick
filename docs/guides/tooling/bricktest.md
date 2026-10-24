# Testing a theme with bricktest

## What it is and when you want it

`bricktest` is a Go test helper that drives a brick page the way the browser runtime does, with no browser: it renders and fills the page, then clicks, submits forms, uploads files and delivers effects against the real engine (middleware, tokens, state store and all). Use it to prove that a component's actions, rules and loaders do what the guide says, in milliseconds, inside `go test`. Browser-only behaviour (focus, layout, real timers) is not covered; see the pitfalls.

## Before you start

- A theme with at least one stateful component ([Stateful components](../interactivity/stateful-components.md)); the engine needs `Options.States`.
- `import "github.com/ubgo/brick/bricktest"`. It is part of the root module, so there is nothing to install.
- The example this page builds is [examples/tooling-bricktest](../../../examples/tooling-bricktest/): a validated sign-up form, an avatar upload and a price whose loader adds a value the template never prints.

## Step 1 — Build the engine the same way the program does

A test that builds a different engine from the one you serve proves nothing about it, so the program and the tests share one constructor. It takes the upload sink as a parameter: a disk sink in `main`, `bricktest.MemoryUploads` in a test, so a test never writes to disk.

<!-- snippet: examples/tooling-bricktest/main.go lines=34-64 -->
```go
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
		Uploads:   uploads,
	})
}

// defaultAddr is this example's own port, so the examples of one guide can run
// side by side. Override it with -addr when the port is taken.
const defaultAddr = ":8087"

// main serves the same handler the tests drive, so the behaviour pinned in
// main_test.go is what a browser gets here. Flags only choose where, never what.
func main() {
	addr := flag.String("addr", defaultAddr, "listen address")
	dir := flag.String("theme", "tooling-bricktest/theme", "the project directory")
	files := flag.String("uploads", os.TempDir(), "where uploaded files are written")
	flag.Parse()
	sink, err := disk.New(disk.Options{Dir: *files, URLPrefix: uploadsURL})
	if err != nil {
		log.Fatal(err)
	}
	eng, err := newEngine(*dir, sink)
```

For a throwaway test with no theme folder, `bricktest.Theme("path", "content", ...)` builds an in-memory project from path and content pairs, and `bricktest.Engine(t, opts, fsys)` returns an engine over it (failing the test when `brick.New` fails).

## Step 2 — Render the page once with `Live`

`bricktest.Live(t, eng, page)` renders and fills the page and returns a `*LivePage`. `page.Component("Signup")` returns the first stateful root of that component (its file stem); the test fails if there is none. Every action you run next replaces the component's held HTML with the action's own, as the browser's morph would.

<!-- snippet: examples/tooling-bricktest/main_test.go lines=1-23 -->
```go
package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ubgo/brick"
	"github.com/ubgo/brick/bricktest"
)

// live renders the page with an in-memory upload sink and returns the
// page the tests drive.
func live(t *testing.T) (*bricktest.LivePage, *bricktest.MemoryUploads) {
	t.Helper()
	sink := bricktest.NewMemoryUploads()
	eng, err := newEngine("theme", sink)
	if err != nil {
		t.Fatal(err)
	}
	return bricktest.Live(t, eng, page), sink
}
```

## Step 3 — Submit a form and assert

`Submit(formSelector, values)` fills the form's fields from the values (plus whatever the HTML already holds), sends the deferred fields along as the runtime does, and runs the form's `brick:submit` action. The assertions read the component's real state and the effects the action returned.

<!-- snippet: examples/tooling-bricktest/main_test.go lines=25-48 -->
```go
// Submitting the form runs the action the way the runtime does: the
// deferred fields ride along, the rules run, and the flash is an effect.
func TestSignup(t *testing.T) {
	page, _ := live(t)
	form := page.Component("Signup")

	// Bad input: the action returns the rule errors and the page shows them.
	form.Submit("#signup", map[string]any{"name": "A", "email": "nope"})
	form.AssertErrors(t, "name", "email")
	if form.View().Errors["email"] == "" {
		t.Fatalf("no email error: %+v", form.View().Errors)
	}

	// Good input: errors clear, the count goes up, a flash is recorded.
	res := form.Submit("#signup", map[string]any{"name": "Ada", "email": "ada@example.com"})
	form.AssertNoErrors(t)
	form.AssertSet(t, "accounts", 1)
	form.AssertSet(t, "name", "")
	bricktest.AssertFlash(t, res, "success", "Welcome, Ada!")
	bricktest.AssertRendered(t, res)
	if got := form.Text(); !strings.Contains(got, "1 accounts") {
		t.Fatalf("text: %q", got)
	}
}
```

The component under test is the sign-up form; the action validates, untouches the fields it is about to empty and flashes:

<!-- snippet: examples/tooling-bricktest/theme/components/Signup.html -->
```html
---
state:
  name: ""
  email: ""
  accounts: 0
brick:
  rules:
    name: [required, {min: 2}]
    email: [required, email]
---
<brick:script>
def signup(state, props):
    errors = brick.validate()
    if errors:
        return [state, errors]
    # The fields are about to be emptied: untouch them, or the next render
    # reports "required" on a form the user has not typed into yet.
    brick.untouch()
    brick.flash("Welcome, %s!" % state["name"], "success")
    return dict(state, name = "", email = "", accounts = state["accounts"] + 1)
</brick:script>
<form id="signup" brick:submit="signup">
  <input brick:model.defer="name" value="{{ .State.name }}">
  {{ with .Errors.name }}<p class="error">{{ . }}</p>{{ end }}
  <input brick:model.defer="email" value="{{ .State.email }}">
  {{ with .Errors.email }}<p class="error">{{ . }}</p>{{ end }}
  <button type="submit">Create account</button>
  <p class="count">{{ .State.accounts }} accounts</p>
</form>
```

What each helper checks:

| Helper | Fails unless |
|---|---|
| `AssertErrors(t, "name", "email")` | every path has a rule error now (no paths: at least one error exists) |
| `AssertNoErrors(t)` | no touched field has a rule error |
| `AssertSet(t, "accounts", 1)` | the state at that path equals the value on its JSON form, so `1`, `int64(1)` and `float64(1)` all match |
| `bricktest.AssertFlash(t, res, "success", "Welcome, Ada!")` | the result carries that flash effect |
| `bricktest.AssertRendered(t, res)` | the action re-rendered the component (`AssertRenderSkipped` is the opposite) |

The same family covers the other effects an action can return: `AssertDispatched`, `AssertNotDispatched`, `AssertRedirect`, `AssertNavigate`, `AssertDownload`, `AssertNoRedirect`, `AssertTitle`, `AssertEffect`, `AssertNoEffect` and `AssertEffects`. When an action returns effects that target other components, `page.Deliver(res)` runs them the way the runtime would, so you can then read the other component's `Text()`.

## Step 4 — Click, type and press keys

`Click(selector)` finds the first element under the component matching a small CSS subset (`tag`, `#id`, `.class`, `[attr]`, `[attr=value]`, descendants separated by spaces) and runs the nearest `brick:click` the way the runtime would, including a `brick:confirm` (`ClickPrompt(selector, answer)` answers a prompt). The same shape covers `Change`, `Input`, `Blur`, `Focus`, `Keydown`, `Keyup`, `ClickOutside`, `ClickWith` (modifier keys and button), `Intersect` and `Leave`, and `page.Keydown`, `page.WindowFocus` and `page.WindowBlur` for window-level handlers. Modifiers resolve as in the runtime: a second click on a `.once` element returns `nil`. `Call(action, args...)` skips the element and runs an action by name, and `Stream` does the same while collecting the text the action streamed.

## Step 5 — Upload files into memory

`Upload(field, name, type, data)` sends a file through the engine's whole upload pipeline (limits, type sniffing, the sink) as `brick:upload` does, into `bricktest.MemoryUploads`. `TryUpload` returns the error instead of failing the test, so a refusal can be asserted. The sink keeps what arrived: `File("u1")` returns the bytes and ref, `Len()` counts stored files, and `Aborted()` and `Removed()` show what was cleaned up.

<!-- snippet: examples/tooling-bricktest/main_test.go lines=50-68 -->
```go
// Upload sends a file through the whole pipeline into MemoryUploads;
// TryUpload returns a refusal instead of failing the test.
func TestAvatarUpload(t *testing.T) {
	page, sink := live(t)
	avatar := page.Component("Avatar")
	png := []byte("\x89PNG\r\n\x1a\n0000")
	avatar.Upload("avatar", "me.png", "image/png", png)
	if html := avatar.HTML(); !strings.Contains(html, `alt="me.png"`) {
		t.Fatalf("html: %s", html)
	}
	if b, ref, ok := sink.File("u1"); !ok || string(b) != string(png) || ref.Type != "image/png" {
		t.Fatalf("stored: %q %+v %v", b, ref, ok)
	}
	_, err := avatar.TryUpload("avatar", "notes.txt", "text/plain", []byte("hello"))
	if !errors.Is(err, brick.ErrUploadType) || sink.Len() != 1 {
		t.Fatalf("text file: %v, files %d", err, sink.Len())
	}
	t.Logf("refused: %v", err)
}
```

The component declares what it accepts, so the text file is refused by the same check a browser upload meets:

<!-- snippet: examples/tooling-bricktest/theme/components/Avatar.html -->
```html
---
state:
  avatar: null
brick:
  uploads:
    avatar: {accept: [image/png], max_size: 1024}
---
<div class="avatar">{{ with .State.avatar }}<img src="{{ .url }}" alt="{{ .name }}">{{ else }}No picture yet{{ end }}</div>
```

## Step 6 — Read the view log

A template shows only what it prints. `page.Views("Price")` returns the data each render of a component saw, oldest first, including values a loader added that no template prints; `AssertViewHas(t, "Price", "currency", "EUR")` checks the latest render's props.

<!-- snippet: examples/tooling-bricktest/main_test.go lines=70-79 -->
```go
// The view log shows what a template saw, including a loader's values it
// never prints.
func TestPriceLoader(t *testing.T) {
	page, _ := live(t)
	page.AssertViewHas(t, "Price", "sku", "mug")
	page.AssertViewHas(t, "Price", "currency", "EUR")
	if v := page.Views("Price"); len(v) != 1 || v[0].Loaded["amount"] != "12.00" {
		t.Fatalf("views: %+v", v)
	}
}
```

<!-- snippet: examples/tooling-bricktest/theme/components/Price.html -->
```html
---
use:
  shop: ~/lib/shop.star
load: shop.price
props:
  sku: string
---
<span class="price">{{ .Props.amount }}</span>
```

## Step 7 — Pin a component's HTML with a golden file

`bricktest.Golden(t, "price", out)` compares output with `testdata/price.golden`. `go test -update` rewrites it. A missing golden file fails with a hint, never passes.

<!-- snippet: examples/tooling-bricktest/main_test.go lines=81-93 -->
```go
// One component alone, compared with a golden file (go test -update
// rewrites it).
func TestPriceGolden(t *testing.T) {
	eng, err := newEngine("theme", bricktest.NewMemoryUploads())
	if err != nil {
		t.Fatal(err)
	}
	out, err := eng.RenderComponent(context.Background(), "Price", map[string]any{"sku": "mug"}, "")
	if err != nil {
		t.Fatal(err)
	}
	bricktest.Golden(t, "price", out)
}
```

## Try it

From `examples/`:

```console
$ go test -v ./tooling-bricktest/
=== RUN   TestSignup
--- PASS: TestSignup (0.00s)
=== RUN   TestAvatarUpload
    main_test.go:67: refused: brick: this type of file is not accepted: avatar takes image/png, not text/plain
--- PASS: TestAvatarUpload (0.00s)
=== RUN   TestPriceLoader
--- PASS: TestPriceLoader (0.00s)
=== RUN   TestPriceGolden
--- PASS: TestPriceGolden (0.00s)
PASS
ok  	github.com/ubgo/brick/examples/tooling-bricktest	0.025s
```

`go run ./tooling-bricktest` serves the same page on http://localhost:8087/.

## Pitfalls

- **Golden files hide mistakes if you accept them blindly.** `go test -update` rewrites the file with whatever the component rendered today. Read the diff as a behaviour change before you commit it. A missing file fails with `run with -update to create it`.
- **The rule errors on a fresh form can surprise you.** Errors come from `rules:` over touched fields, and an action that empties the fields must call `brick.untouch()` or the next render reports `required` on a form nobody has typed into. `AssertErrors` reads the stored errors; errors an action returned are shown by its own render and never stored, so assert those on `HTML()`.
- **Asking for a component that is not stateful fails.** `Component("Price")` gives `bricktest: no stateful <Price> on the page`, because only components with `state:` have a handle to act on. Use `Views` for the others.
- **A selector that matches nothing fails the test.** `bricktest: nothing in <id> matches "#nope"`. The selector language is small on purpose (no `>`, no pseudo-classes); a bad one reports `bricktest: selector ...`.
- **`Live` needs a state store.** Without `Options.States` the page fills but no action can run. Build the engine like the example does.
- **This is not a browser.** Real timers, layout, focus and JavaScript are not run; `brick:poll`, `brick:intersect` and the like are delivered when you call them. Browser-only behaviour needs the end-to-end checks of the [site tooling](../../reference/cli.md).
- **When a failing page needs eyes, dump it.** `page.Dump()` writes the current page to a temporary file, logs the path and returns it.

## Related

- [statetest: conformance for a custom store](statetest.md) · [Formatting](fmt.md) · [Editors](editors.md)
- [Stateful components](../interactivity/stateful-components.md) · [Forms](../forms/README.md) · [Uploads](../forms/uploads.md)
- [Feature index](../../reference/features.md)
- Example: [examples/tooling-bricktest](../../../examples/tooling-bricktest/)
