# Morph controls, transitions and client commands

## What it is and when you want it

After an action the runtime does not replace a component: it **morphs** the new HTML into the old, so focus, typed text and scroll stay where they were. Four attributes tune that morph: `brick:key` keys a row among its siblings, `brick:ignore` keeps an element as it is (a third-party map), `brick:replace` swaps an element for a fresh copy, and `brick:keep="open aria-expanded"` names attributes the browser owns, which the morph leaves as they are (a `<details>` the user opened). Three more animate it: `brick:transition` runs a stateful root's morphs in a view transition, `brick.transition(...)` lets one action choose, and `brick:enter` / `brick:leave` play classes when an element is shown, hidden or removed. **Client commands**, `brick:js.<event>`, change the page in the browser with no request at all: a menu that opens and closes needs no server. You want this page when a re-render moves the wrong row, wipes a widget, snaps where it should slide, or costs a request for something purely visual.

## Before you start

- Read [stateful components and actions](stateful-components.md). Morph controls and client commands work in any file (a navigation morphs the whole page); `brick:transition` needs a stateful root.
- What the browser does with these attributes is runtime behaviour: the example's test checks everything the server does (the rendered keys, the passed-through attributes, each action's transition choice, the compile checks) and this page describes the rest.
- The example is [`examples/interactivity-client-commands`](../../../examples/interactivity-client-commands/).

## Step 1 — Key the rows and protect the widgets

Rows in a `{{ range .State.… }}` need an `id` or a `brick:key`, or inserting a row at the top re-renders every row after it (a focused input moves to the wrong row); Dev warns about a stateful range of plain rows with neither. `brick:ignore` keeps `#map` exactly as a map library left it; `brick:replace` swaps `#chart` for a fresh copy, which remounts its client code. `components/Todos.html`:

<!-- snippet: examples/interactivity-client-commands/theme/components/Todos.html -->
```html
---
state:
  rows: []
  next: 1
  open: false
---
<brick:script>
def add_row(state, props, title):
    row = {"id": state["next"], "title": title}
    return dict(state, rows = [row] + state["rows"], next = state["next"] + 1)

def shuffle(state, props):
    brick.transition("shuffle")
    return dict(state, rows = list(reversed(state["rows"])))

def quiet(state, props):
    brick.transition("none")
    return dict(state, next = state["next"] + 1)
</brick:script>
<section class="todos" brick:transition="slide">
  <button id="add" brick:click='add_row("Write docs")'>Add</button>
  <button id="shuffle" brick:click="shuffle">Reverse</button>
  <button id="quiet" brick:click="quiet">No animation</button>
  <button id="toggle" brick:click='$toggle("open")'>Notes</button>
  <ul>{{ range .State.rows }}<li brick:key="{{ .id }}">{{ .title }} #{{ .id }}</li>{{ end }}</ul>
  <div id="map" brick:ignore></div>
  <div id="chart" brick:replace>{{ len .State.rows }} rows</div>
  <p id="notes" brick:show="open" class="fade" brick:enter="faded" brick:leave="faded" {{ if not .State.open }}hidden{{ end }}>Notes</p>
</section>
```

`brick:key` is scoped to the siblings, so it need not be unique in the document; `id` wins when both are present. `brick:ignore.self` keeps only the element's attributes and still patches its children. `brick:keep="open"` on a `<details>` keeps the user's open or closed choice while the morph patches everything else, including its other attributes.

## Step 2 — Animate the morph

`brick:transition="slide"` on the root wraps each action's morph in `document.startViewTransition` with the `slide` type, for `:active-view-transition-type(slide)` in your CSS. An action can choose for itself: `shuffle` calls `brick.transition("shuffle")` (another type), and `quiet` calls `brick.transition("none")` (no animation this time); the last call in an action wins, and the choice also overrides the rule that polls never animate. In Go middleware the same is `brick.EffectsFrom(ctx).Transition(types)`.

The notes paragraph fades: `brick:enter="faded"` is the look it starts from when `brick:show` reveals it (the class comes off on the next frame), and `brick:leave="faded"` the look it goes to before it is hidden or removed. The runtime waits for the transition the classes start, at most 2 s, marks the element `inert` and `data-brick-leaving` meanwhile, and skips all of it under `prefers-reduced-motion`. Naming elements (`view-transition-name`) is your CSS.

## Step 3 — Open a menu with client commands

A menu that opens and closes needs no state and no request. `brick:js.<event>` runs a fixed list of DOM commands in the browser. `components/Menu.html` (a plain component, no `state:`):

<!-- snippet: examples/interactivity-client-commands/theme/components/Menu.html -->
```html
<div class="menu" data-on-close='hide("#menu-list")' brick:js.click.outside='hide("#menu-list")' brick:js.keydown.escape.window='hide("#menu-list")'>
  <button type="button" aria-expanded="false" brick:js.click='toggle("#menu-list"); toggleAttr("this", "aria-expanded", "true", "false")'>Menu</button>
  <ul id="menu-list" hidden brick:leave="fade-out">
    <li><a href="/settings">Settings</a></li>
    <li><button type="button" brick:js.click='addClass("closest .menu", "dimmed"); focus("#search")'>Dim</button></li>
  </ul>
</div>
<brick:client>
// brick.js runs the same command list a brick:js attribute holds, from
// code, with the element as "this". The list stays in the markup
// (data-on-close), so the script never builds a command from data. The
// "navigated" event fires after a client navigation (Options.Navigation),
// and brick.on returns the function that stops listening, which a mount
// callback may return as its cleanup.
brick.mount("Menu", (menu) =>
  brick.on("navigated", () => brick.js(menu, menu.getAttribute("data-on-close"))),
);
</brick:client>
```

The commands are `show`, `hide`, `toggle` (the `hidden` attribute), `addClass`, `removeClass`, `toggleClass`, `setAttr`, `removeAttr`, `toggleAttr(target, name[, on[, off]])`, `focus` and `dispatch(target, name[, detail])`, separated by `;`. Each takes its target first: a CSS selector (every match), `"@name"` (a `brick:ref` in the element's own component), `"this"` or `"closest .card"`. Every argument is a JSON literal, so the attribute takes single quotes. The events are `click dblclick input change submit focus blur keydown keyup mouseenter mouseleave`, with `.once`, `.outside` (click only), and `.window` and key combos (keydown and keyup only).

What a command changed survives the next morph, so a menu it opened stays open when an action re-renders the component. From client code, `brick.js(el, commands)` runs the same list with `el` as `this`. The Menu above carries its close commands in `data-on-close` and a `<brick:client>` block closes it whenever the page navigates (the `navigated` event fires after a client navigation, see [navigation](navigation.md)); `brick.on` returns the function that stops listening, which a `brick.mount` callback may return as its cleanup. Keeping the list in the markup means the script never builds a command from data. Both halves are already in the file shown above; the client block is its last lines.

Two more calls help when client code needs to look around: `brick.find(type)` and `brick.all()` return stateful component roots, and `brick.watch(el, path, fn)` follows one signal of a component. They are shown with a runnable example in [the browser runtime](../live/browser-runtime.md#step-7--find-components-and-follow-their-signals).

## Try it

```sh
cd examples/interactivity-client-commands && go run .
```

Open http://localhost:8095/: Menu opens and closes with no request (watch the network tab), Escape or a click outside closes it, and the Todos buttons morph the list. The test checks the server's side:

```text
click Add, click Add   → <ul><li brick:key="2">Write docs #2</li><li brick:key="1">Write docs #1</li></ul>
                         effects: none (the root's brick:transition="slide" animates it)
click Reverse          → effects: [{"kind":"transition","name":"shuffle"}]
click No animation     → effects: [{"kind":"transition","name":"none"}]
every render           → <div id="map" brick:ignore></div> and <div id="chart" brick:replace>2 rows</div> pass through
```

`TestMenuClientServed` fetches the page and the Menu's client module and checks that the `data-on-close` list and the `brick.js` call are both served; running them (the menu closing after a navigation) is the browser's part.

`cd examples && go test ./interactivity-client-commands/` prints `ok  github.com/ubgo/brick/examples/interactivity-client-commands`.

## Pitfalls

The example's `TestRefused` pins each of these messages:

- **`brick:ignore` on a stateful root is refused**: `brick:ignore on a stateful component's root would keep the old root after every action, so the component could never show its own result; put it on an element inside, or use brick:ignore.self`.
- **`brick:transition` needs `state:`**: on a plain file it fails with `brick:transition is reserved (on plain elements brick accepts nav, prefetch, persist, keep, key, ignore, replace, ref, offline, current, leave, enter, scroll, method, js); action events and brick:transition need a component that declares state:`.
- **Client commands cannot run code.** `setAttr` and `toggleAttr` refuse event handlers, `srcdoc`, engine attributes and script URLs: `setAttr: "onclick" is an event handler, which would run code the page did not ship`. `dispatch` refuses brick's own event names. The runtime repeats these checks, because `{{ }}` can fill a value.
- **The command list is closed**: `unknown command "open" (commands: addClass, dispatch, focus, hide, removeAttr, removeClass, setAttr, show, toggle, toggleAttr, toggleClass)`. There are no variables or conditions; state still changes only in actions.
- **Arguments are JSON**: `toggle(#m)` fails with `toggle: arguments must be JSON literals (strings in double quotes, so the attribute takes single quotes)`.
- **`brick:key` needs a value** (`brick:key needs a value`), and two siblings with one key warn once in the browser console.
- **Put `.outside` on a wrapper that holds both the button and the menu**, or the button's own click counts as outside and closes what it just opened. Commands never prevent the default, so use `type="button"` inside a form.
- **What does not animate:** polls, `$set` and a recovery never animate unless an action calls `brick.transition`; a morph runs plainly when the browser lacks the API, the user prefers reduced motion, or another transition is running. `brick:leave` does not cover `brick:replace` or navigation swaps.

## Related

- [Events and modifiers](events-and-modifiers.md): the server-side events; `brick:js` has its own event list.
- [Stateful components and actions](stateful-components.md), [navigation](navigation.md) (`brick:persist` across pages).
- The guide's [browser runtime section](../../guide.md#the-browser-runtime--keys-transitions-viewport-actions-hooks), and the runtime API reference for `brick.hook("morph")`, `brick.watch`, `brick.find` and `brick.all`.
- [Attribute reference](../../reference/attributes.md) and [feature index](../../reference/features.md#bindings-and-events).
- Example: [`examples/interactivity-client-commands`](../../../examples/interactivity-client-commands/).
