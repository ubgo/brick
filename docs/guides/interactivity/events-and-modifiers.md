# Events and modifiers

## What it is and when you want it

`brick:click` is one of eleven event attributes. Each one runs an action of the component around it when a browser event happens: typing (`brick:input`), leaving a field (`brick:blur`), a key (`brick:keydown`, `brick:keyup`), a select changing (`brick:change`), the element scrolling into view (`brick:intersect`), a timer (`brick:poll`), and more. Modifiers after a dot say when and how: `.debounce.300ms`, `.once`, `.enter`, `.ctrl.enter`, `.outside`, `.window`, `.details`. The compiler checks every attribute and modifier against one table that the browser runtime also reads, so a typo is a compile error with a file and line, never a click that silently does nothing. You want this page when `brick:click` alone is not the event you need.

## Before you start

- Read [stateful components and actions](stateful-components.md): event attributes are accepted only inside a component that declares `state:`.
- Forms (`brick:model`, `brick:submit`, deferred fields, `brick:loading`, `brick:dirty`, `brick:confirm`) are covered by the forms guides; this page keeps to events.
- The example is [`examples/interactivity-events-modifiers`](../../../examples/interactivity-events-modifiers/).

## Step 1 — Write the actions

Each action appends what it was called with to a log, so you can see which call ran with which value. An attribute that passes a value (`brick:input`, `brick:blur`, `brick:change`) adds it as the action's last argument. `brick:click.details` adds a dict of the click's keys, button, count and position. `brick:confirm.prompt` adds the visitor's answer after the call's own arguments.

## Step 2 — Put the attributes on the elements

`components/Inbox.html`:

<!-- snippet: examples/interactivity-events-modifiers/theme/components/Inbox.html -->
```html
---
state:
  q: ""
  log: []
  seen: 0
---
<brick:script>
def _note(state, text):
    return dict(state, log = state["log"] + [text])

def search(state, props, q):
    return _note(dict(state, q = q), "search(%s)" % q)

def validate(state, props, q):
    return _note(state, "validate(%s)" % q)

def send(state, props):
    return _note(state, "send")

def send_all(state, props):
    return _note(state, "send_all")

def clear(state, props):
    return _note(state, "clear")

def close(state, props):
    return _note(state, "close")

def choose(state, props, plan):
    return _note(state, "choose(%s)" % plan)

def at(state, props, d):
    return _note(state, "at(x=%d shift=%s count=%d)" % (d["x"], d["shift"], d["count"]))

def rename(state, props, id, name):
    return _note(state, "rename(%d, %s)" % (id, name))

def welcome(state, props):
    return _note(state, "welcome")

def more(state, props):
    return _note(dict(state, seen = state["seen"] + 1), "more")

def tick(state, props):
    return _note(state, "tick")
</brick:script>
<section class="inbox" brick:poll.10s="tick">
  <div id="menu" brick:click.outside="close">
    <input id="q" brick:input.debounce.300ms="search" brick:blur="validate" value="{{ .State.q }}">
    <textarea id="msg" brick:keydown.enter="send" brick:keydown.ctrl.enter="send_all" brick:keyup.escape="clear"></textarea>
  </div>
  <select id="plan" brick:change="choose">
    <option value="free">Free</option>
    <option value="pro">Pro</option>
  </select>
  <div id="chart" brick:click.details="at">chart</div>
  <button id="rename" brick:click="rename(3)" brick:confirm.prompt="New name?">Rename</button>
  <button id="hello" brick:click.once="welcome">Say hello once</button>
  <div id="sentinel" brick:intersect.once="more">Loading more…</div>
  <ol id="log">{{ range .State.log }}<li>{{ . }}</li>{{ end }}</ol>
</section>
```

What each attribute does in the browser:

| On | Event | Calls |
|---|---|---|
| `#q` | typing `ada` quickly | `search("ada")` once, 300 ms after the last key |
| `#q` | leaving the field | `validate("ada")` |
| `#msg` | Enter / ctrl+Enter / shift+Enter | `send()` / `send_all()` / nothing (a newline is typed) |
| `#msg` | releasing Escape | `clear()` |
| `#menu` | a click anywhere outside it, while it is shown | `close()` |
| `#plan` | choosing Pro | `choose("pro")` |
| `#chart` | shift+click 10 px from its left edge | `at({"x": 10, "shift": true, "count": 1, …})` |
| `#rename` | a click, then typing `Ada` in the prompt | `rename(3, "Ada")`; Cancel sends nothing |
| `#hello` | the first click / any later click | `welcome()` / nothing |
| `#sentinel` | scrolling into view | `more()`, once |
| the root | every ten seconds while the tab is visible | `tick()` |

## Step 3 — Drive it from a test

`bricktest.Live` fills the page and holds its components; each driver acts the way the runtime does and reads the same modifier table. Timing modifiers (`.debounce`, `.throttle`, `.dwell`) let each call through, because timing is the browser's job; `.once` runs once per element, and keys and combos must match exactly. From `examples/interactivity-events-modifiers/main_test.go`:

<!-- snippet: examples/interactivity-events-modifiers/main_test.go lines=21-53 -->
```go
func TestEveryEvent(t *testing.T) {
	a, err := newApp("theme")
	if err != nil {
		t.Fatal(err)
	}
	live := bricktest.Live(t, a.eng, page)
	in := live.Component("Inbox")

	in.Input("#q", "ada")            // brick:input.debounce.300ms → search("ada")
	in.Blur("#q", "ada")             // brick:blur → validate("ada")
	in.Keydown("#msg", "Enter")      // brick:keydown.enter → send
	in.Keydown("#msg", "ctrl+Enter") // brick:keydown.ctrl.enter → send_all, not send
	in.Keyup("#msg", "Escape")       // brick:keyup.escape → clear
	in.ClickOutside("#menu")         // brick:click.outside → close
	in.Change("#plan", "pro")        // brick:change → choose("pro")
	in.ClickWith("#chart", bricktest.ClickDetails{Shift: true, X: 10, Count: 1})
	in.ClickPrompt("#rename", "Ada") // brick:confirm.prompt → rename(3, "Ada")
	in.Click("#hello")               // brick:click.once → welcome
	if res := in.Click("#hello"); res != nil {
		t.Fatalf(".once ran twice: %v", res)
	}
	in.Intersect("#sentinel") // brick:intersect.once → more
	in.Call("tick")           // what brick:poll.10s sends every ten seconds
	if _, err := in.TryCall("tick_tock"); !errors.Is(err, brick.ErrUnknownAction) {
		t.Fatalf("a misspelled action: %v", err) // answered 400 over HTTP
	}

	want := []string{"search(ada)", "validate(ada)", "send", "send_all", "clear", "close", "choose(pro)", "at(x=10 shift=True count=1)", "rename(3, Ada)", "welcome", "more", "tick"}
	in.AssertSet(t, "log", toAny(want))
	if !strings.Contains(in.HTML(), "<li>at(x=10 shift=True count=1)</li>") {
		t.Fatalf("log not rendered:\n%s", in.HTML())
	}
}
```

## Step 4 — Respond to the window, a held key, a long look and the connection

Some events belong to the page, not to an element. `components/Panel.html` collects four of them, plus the `log:` setting that Step 5 explains:

<!-- snippet: examples/interactivity-events-modifiers/theme/components/Panel.html -->
```html
---
state:
  page: 1
  away: false
  views: 0
brick:
  log: debug
---
<brick:script>
def advance(state, props):
    return dict(state, page = state["page"] + 1)

def went_away(state, props):
    return dict(state, away = True)

def came_back(state, props):
    return dict(state, away = False)

def viewed(state, props):
    return dict(state, views = state["views"] + 1)
</brick:script>
<aside class="panel" brick:focus.window="came_back" brick:blur.window="went_away">
  <p id="offline-banner" brick:offline>You are offline. Changes wait until you are back.</p>
  <p id="online-note" brick:offline.remove>Connected</p>
  <p id="page" brick:keydown.norepeat.window.n="advance">Page {{ .State.page }}</p>
  <p id="presence">{{ if .State.away }}Away{{ else }}Here{{ end }}</p>
  <div id="ad" brick:intersect.dwell.2000ms="viewed">Sponsored</div>
  <p id="views">Views: {{ .State.views }}</p>
</aside>
```

What each one does in the browser:

| Attribute | Runs when |
|---|---|
| `brick:keydown.norepeat.window.n="advance"` | the visitor presses `n` anywhere on the page, once per press: a held key sends one call, where plain `brick:keydown` repeats with the keyboard's auto-repeat |
| `brick:blur.window="went_away"` and `brick:focus.window="came_back"` | the tab or window loses or regains focus, not an input inside it; `.window` on `brick:focus` and `brick:blur` is the only way to hear that |
| `brick:intersect.dwell.2000ms="viewed"` | the block has stayed in view for two seconds, so a row scrolled past is not counted; `.dwell` alone waits one second (`brick.DwellDefaultMS`), and it cannot be combined with `.leave` |
| `brick:offline` and `brick:offline.remove` | the browser goes offline: the first element is shown while offline, the second is removed; the runtime also puts `data-offline` on `<html>` |

`brick:offline` is not an event that calls an action, so it needs no function. The test drives the other three the way the runtime does, with the page-level drivers `live.Keydown`, `live.WindowBlur` and `live.WindowFocus`, and `IntersectFor` for a stay of a given length:

<!-- snippet: examples/interactivity-events-modifiers/main_test.go lines=111-140 -->
```go
func TestPanelWindowDwellOffline(t *testing.T) {
	a, err := newApp("theme")
	if err != nil {
		t.Fatal(err)
	}
	live := bricktest.Live(t, a.eng, page)
	p := live.Component("Panel")

	live.Keydown("n") // brick:keydown.norepeat.window.n → advance
	p.AssertSet(t, "page", 2)

	live.WindowBlur() // brick:blur.window → went_away
	p.AssertSet(t, "away", true)
	live.WindowFocus() // brick:focus.window → came_back
	p.AssertSet(t, "away", false)

	p.IntersectFor("#ad", 2*time.Second) // brick:intersect.dwell.2000ms → viewed
	p.AssertSet(t, "views", 1)
	if res := p.IntersectFor("#ad", 500*time.Millisecond); res != nil {
		t.Fatalf("a stay shorter than the dwell still ran %v", res)
	}

	// The offline markup is served as written; showing it is the browser's.
	html := p.HTML()
	for _, want := range []string{`brick:offline>`, `brick:offline.remove>`} {
		if !strings.Contains(html, want) {
			t.Errorf("no %s in:\n%s", want, html)
		}
	}
}
```

What only a browser does is hold the key down, move the window's focus, wait the two seconds and lose the network. The test checks what the server can: that each attribute compiles, that the action it names runs and changes the state, that a stay shorter than the dwell calls nothing, and that the offline markup is served as written.

## Step 5 — Tune what the action log says

`actionmw.Slog` writes one line per action. A component can say how loud its successful actions are with `brick: {log:}` in its frontmatter: `log: false` keeps a chatty component out of the log, and `log: debug` (or `info`, `warn`, `error`) moves it to that level. `Panel` above sets `log: debug`; `components/Ticker.html` polls every five seconds and sets `log: false`:

<!-- snippet: examples/interactivity-events-modifiers/theme/components/Ticker.html -->
```html
---
state:
  ticks: 0
brick:
  log: false
---
<brick:script>
def tick(state, props):
    return dict(state, ticks = state["ticks"] + 1)

def explode(state, props):
    fail("the clock broke")
</brick:script>
<p id="ticks" brick:poll.5s="tick">Ticks: {{ .State.ticks }}</p>
```

The middleware is added to the engine in Go, and the logger is a parameter so a test can read what was written:

<!-- snippet: examples/interactivity-events-modifiers/main.go lines=44-63 -->
```go
func newAppLogged(dir string, logger *slog.Logger) (*app, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	sc, err := scriptstarlark.New(scriptstarlark.Options{})
	if err != nil {
		return nil, err
	}
	eng, err := brick.New(brick.Options{
		FS:               root.FS(),
		Scripting:        sc,
		States:           brick.NewMemoryStates(0, 0),
		ActionMiddleware: []brick.ActionMiddleware{actionmw.Slog(logger)},
	})
	if err != nil {
		return nil, err
	}
	return &app{eng: eng}, nil
}
```

The test runs one action of each component and reads the text the handler wrote:

<!-- snippet: examples/interactivity-events-modifiers/main_test.go lines=144-172 -->
```go
func TestActionLogSettings(t *testing.T) {
	out := logLines(t, func(live *bricktest.LivePage) {
		live.Component("Panel").Call("advance")
		live.Component("Ticker").Call("tick")
		live.Component("Inbox").Call("tick")
	})
	if !strings.Contains(out, "level=DEBUG msg=\"brick action\" component=Panel action=advance") {
		t.Errorf("Panel's debug line missing:\n%s", out)
	}
	if strings.Contains(out, "component=Ticker") {
		t.Errorf("Ticker logged despite log: false:\n%s", out)
	}
	if !strings.Contains(out, "level=INFO msg=\"brick action\" component=Inbox action=tick") {
		t.Errorf("Inbox's default line missing:\n%s", out)
	}
}

// A failed action is logged whatever the component says: a refusal is a
// security signal that one file must not be able to hide.
func TestActionLogOffStillLogsFailures(t *testing.T) {
	out := logLines(t, func(live *bricktest.LivePage) {
		if _, err := live.Component("Ticker").TryCall("explode"); err == nil {
			t.Fatal("explode succeeded")
		}
	})
	if !strings.Contains(out, "level=WARN msg=\"brick action failed\" component=Ticker action=explode") {
		t.Fatalf("the failure was not logged:\n%s", out)
	}
}
```

A failed or refused action is logged at Warn whatever the component says (`brick action failed`), because a refusal is a security signal and one file must not be able to hide it. Any other value after `log:` is a compile error that lists the accepted words. The setting reaches every action middleware as `ActionCall.Log` (`*brick.ActionLog`, nil when unset), so your own logger or tracer can honour it too.

## Every event attribute

Accepted only inside a component with `state:` (on a component call too, which the callee places with `{{ attrs }}`).

| Attribute | Runs when | Passes | Modifiers |
|---|---|---|---|
| `brick:click` | the element is clicked | nothing | `.once`, `.debounce.<ms>`, `.throttle.<ms>`, `.optimistic`, `.outside`, `.details` |
| `brick:submit` | the form is submitted | its fields | `.once`, `.debounce.<ms>`, `.throttle.<ms>`, `.lock` |
| `brick:change` | the control's value is committed | the value | `.once`, `.debounce.<ms>`, `.throttle.<ms>`, `.optimistic` |
| `brick:input` | every edit | the value | same as `brick:change` |
| `brick:focus` | the element gains focus | nothing | `.once`, `.debounce.<ms>`, `.throttle.<ms>`, `.window` |
| `brick:blur` | the element loses focus | the value | `.once`, `.debounce.<ms>`, `.throttle.<ms>`, `.optimistic`, `.window` |
| `brick:keydown` | a key goes down | nothing | as `brick:keyup`, plus `.norepeat` |
| `brick:keyup` | a key is released | nothing | `.once`, `.debounce.<ms>`, `.throttle.<ms>`, `.optimistic`, `.window`, `.ctrl`, `.meta`, `.shift`, `.alt`, one key |
| `brick:intersect` | the element enters the viewport | nothing | `.leave`, `.once`, `.half`, `.full`, `.margin.<px>`, `.dwell[.<ms>]` |
| `brick:poll` | on a timer | nothing | the interval (`.10s`, `.500ms`, `.1m`), `.visible`, `.keep-alive` |
| `brick:model` | typing (a binding, not an event) | the value as `$set` | `.lazy`, `.defer`, `.debounce.<ms>`, `.throttle.<ms>` |

Keys are `enter escape tab space up down left right backspace delete`, a letter `a`–`z` or a digit `0`–`9`. Modifier keys go before the key and match exactly, so `.enter` never fires for ctrl+Enter and `.ctrl.enter` never for ctrl+shift+Enter. A bare `brick:keydown` fires for every key. `brick:keydown.enter` cancels the key's default, so it never also submits the form around it. `.window` listens on the whole window (a page-level Escape; for focus and blur, the tab gaining or losing focus). `.once` is per rendered element. The full list, with value types, is in the [attribute reference](../../reference/attributes.md).

## Try it

```sh
cd examples/interactivity-events-modifiers && go run .
```

Open http://localhost:8092/, type in the box, press keys, click outside it: each call appears in the numbered log. The test runs every row of the table above and the compile checks:

```text
$ cd examples && go test -v ./interactivity-events-modifiers/
=== RUN   TestEveryEvent
--- PASS: TestEveryEvent (0.01s)
=== RUN   TestTyposFailAtCompile
--- PASS: TestTyposFailAtCompile (0.00s)
=== RUN   TestPanelWindowDwellOffline
--- PASS: TestPanelWindowDwellOffline (0.00s)
=== RUN   TestActionLogSettings
--- PASS: TestActionLogSettings (0.00s)
=== RUN   TestActionLogOffStillLogsFailures
--- PASS: TestActionLogOffStillLogsFailures (0.00s)
PASS
ok  	github.com/ubgo/brick/examples/interactivity-events-modifiers	0.034s
```

`TestEveryEvent` also prints the engine's own action log (one `INFO brick action component=Inbox action=search …` line per call, and a `WARN brick action failed` line for the misspelled action); the lines are trimmed above.

## Pitfalls

- **Typos fail when the file is read, with the allowed list.** These are the messages the example's test pins:

  ```text
  components/Box.html:8:20: brick:click.onec: unknown modifier "onec" (brick:click accepts .once, .debounce.<ms>, .throttle.<ms>, .optimistic, .outside, .details)
  components/Box.html:8:19: brick:keydown.cmd.k: unknown modifier "cmd" (brick:keydown accepts .once, .debounce.<ms>, .throttle.<ms>, .optimistic, .window, .ctrl, .meta, .shift, .alt, .<key>: enter escape tab space up down left right backspace delete a-z 0-9, .norepeat)
  components/Box.html:8:19: brick:model.defer.lazy: .defer cannot be combined with .lazy (brick:model accepts .lazy, .defer, .debounce.<ms>, .throttle.<ms>)
  components/Box.html:8:17: brick:poll="soon": the value is an interval such as 5s, or write brick:poll.5s="action" to run an action
  ```

  The Mac key is `.meta`, not `.cmd`.
- **An unknown action name is not one of them.** `brick:click="tick_tock"` compiles, and the click answers 400 `brick: unknown action`; the test checks `TryCall` returns `brick.ErrUnknownAction`.
- **Event attributes need `state:`.** On a file without it the compile says `action events and brick:transition need a component that declares state:`; for a click that only changes the page (open a menu), use [client commands](morph-transitions-client-commands.md#step-3--open-a-menu-with-client-commands) instead.
- **`.optimistic` takes only a literal `$set(path, value)` or `$toggle(path)`.** Counting (`likes + 1`) would need code in the browser, so anything else with `.optimistic` is a compile error; render the count on the server.
- **`.prevent`, `.stop` and generic events are left out on purpose.** Clicks and submits already prevent the default; client code calling `brick.action(el, name, args)` covers the rest.
- **Polls are quiet and pause.** A poll sets no `aria-busy` or `data-loading`, skips its ticks while the tab is hidden (unless `.keep-alive`) and stops when its element is removed. `brick:poll="5s"` with no action refreshes the component; `brick:poll.10s="tick"` runs an action.
- **In a test, a driver with no matching attribute fails the test.** `Keydown("#msg", "shift+Enter")` stops with `has no brick:keydown for "shift+Enter" on it or above it` rather than returning nil, so assert what does fire.
- **`.dwell` cannot be combined with `.leave`**, and a modifier written twice is an error, like every other pair in the table above: `.dwell` times a stay in view, and leaving ends the stay rather than completing it.
- **`.window` on a key means anywhere on the page, including inside a text box.** A page-level `brick:keydown.norepeat.window.n` also fires while the visitor types an `n` into a field; put the key behind a modifier (`.ctrl`, `.meta`) when the page has inputs.
- **`log: false` hides success only.** A failed action still logs `brick action failed`, so a typo in an action name shows up even in a silent component.
- **Choose action names that are not template functions.** `def pick(...)` fails with `<brick:script> function "pick" shadows a built-in or an import (C12)`; this example uses `choose`.

## Related

- [Stateful components and actions](stateful-components.md), [state in the URL and the server](url-and-server-state.md).
- [Morph controls, transitions and client commands](morph-transitions-client-commands.md): `brick:js.<event>` has its own event list.
- The guide's [forms section](../../guide.md#forms--deferred-fields-arrays-modifiers-loading-and-confirm) for `brick:model`, `brick:loading`, `brick:dirty` and `brick:confirm`.
- [Attribute reference](../../reference/attributes.md) and [feature index](../../reference/features.md#bindings-and-events).
- Example: [`examples/interactivity-events-modifiers`](../../../examples/interactivity-events-modifiers/).
