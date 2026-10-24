# Attribute reference

Every `brick:` attribute, its modifiers and its value, as the compiler checks it. The source of truth is four tables in the engine: the action grammar in [`grammar.go`](../../grammar.go) (`grammar`, which the browser runtime reads as the same table through `GrammarJS`), the non-action attributes in [`plainattrs.go`](../../plainattrs.go) (`plainBrickAttrs`, `plainRules`), method links in [`methodlink.go`](../../methodlink.go) and client commands in [`jscommands.go`](../../jscommands.go); the call-site attributes are checked in `checkCall` in [`compile.go`](../../compile.go). A typo in any of them is a compile error with the file, the line and the allowed list. `brick:` is engine-owned: `hx-*`, `x-*` and `data-*` attributes pass through untouched.

Tags (`<brick:prop>`, `<brick:boundary>`, `<brick:script>`, `<brick:client>`, `<brick:style>`, `<brick:render>`) and frontmatter keys are not attributes; see the [frontmatter reference](frontmatter.md) and the [guide](../guide.md).

## Action attributes

Accepted only inside a component that declares `state:`, on plain elements and on component calls (the callee places them with `{{ attrs }}`). A value `{{ }}` builds is checked when it is set instead of when the file is read.

| Attribute | Value | Modifiers | Guide |
|---|---|---|---|
| `brick:click` | an action call: `name`, `name(json args)`, `$set(path, value)`, `$toggle(path)`, `$reset(path…)`, `$refresh`, `$dispatch(name, data)`, `$parent.name` | `.once`, `.debounce.<ms>`, `.throttle.<ms>`, `.optimistic`, `.outside`, `.details` | [events](../guides/interactivity/events-and-modifiers.md) |
| `brick:submit` | an action call; the form's fields are passed | `.once`, `.debounce.<ms>`, `.throttle.<ms>`, `.lock` | forms guides |
| `brick:change` | an action call; the control's value is the last argument | `.once`, `.debounce.<ms>`, `.throttle.<ms>`, `.optimistic` | [events](../guides/interactivity/events-and-modifiers.md) |
| `brick:input` | an action call; the value is the last argument | `.once`, `.debounce.<ms>`, `.throttle.<ms>`, `.optimistic` | [events](../guides/interactivity/events-and-modifiers.md) |
| `brick:focus` | an action call; no value | `.once`, `.debounce.<ms>`, `.throttle.<ms>`, `.window` | [events](../guides/interactivity/events-and-modifiers.md#step-4--respond-to-the-window-a-held-key-a-long-look-and-the-connection) |
| `brick:blur` | an action call; the value is the last argument (none with `.window`) | `.once`, `.debounce.<ms>`, `.throttle.<ms>`, `.optimistic`, `.window` | [events](../guides/interactivity/events-and-modifiers.md#step-4--respond-to-the-window-a-held-key-a-long-look-and-the-connection) |
| `brick:keydown` | an action call | `.once`, `.debounce.<ms>`, `.throttle.<ms>`, `.optimistic`, `.window`, `.ctrl`, `.meta`, `.shift`, `.alt`, one key, `.norepeat` | [events](../guides/interactivity/events-and-modifiers.md#step-4--respond-to-the-window-a-held-key-a-long-look-and-the-connection) |
| `brick:keyup` | an action call | `.once`, `.debounce.<ms>`, `.throttle.<ms>`, `.optimistic`, `.window`, `.ctrl`, `.meta`, `.shift`, `.alt`, one key | [events](../guides/interactivity/events-and-modifiers.md) |
| `brick:intersect` | an action call | `.leave`, `.once`, `.half`, `.full`, `.margin.<px>`, `.dwell[.<ms>]` (1000 ms without a duration) | [events](../guides/interactivity/events-and-modifiers.md#step-4--respond-to-the-window-a-held-key-a-long-look-and-the-connection) |
| `brick:poll` | an interval (`5s`) to refresh, or an action call when the interval is a modifier (`brick:poll.10s="tick"`) | the interval (`.<n>ms`, `.<n>s`, `.<n>m`), `.visible`, `.keep-alive` | [events](../guides/interactivity/events-and-modifiers.md) |
| `brick:model` | one state path (`name`, `address.city`, `items.0.qty`); on a stateful component call, a field of the caller bound to the callee's `brick: {model:}` | `.lazy`, `.defer`, `.debounce.<ms>`, `.throttle.<ms>` (none on a component call) | [stateful components](../guides/interactivity/stateful-components.md#step-5--nest-a-component-that-binds-to-its-parent-modelable) |
| `brick:text` | one state path; the element's text follows the field in the browser | none | forms guides |
| `brick:show` | one state path, optionally negated with `!`; toggles `hidden` in the browser | none | [stateful components](../guides/interactivity/stateful-components.md) |
| `brick:loading` | space-separated action names (or a call such as `remove(3)`, or `$set("field")`), or empty for any action | `.delay[.<ms>]` (200 ms without a duration), `.remove`, `.except` | forms guides |
| `brick:dirty` | space-separated state paths, or empty for any field | `.remove` | forms guides |
| `brick:disable-with` | the busy label, or empty to keep the label | none | forms guides |
| `brick:confirm` | the question, plain text | `.prompt` (ask for text; the answer is the call's last argument) | [events](../guides/interactivity/events-and-modifiers.md) |
| `brick:upload` | one field declared in `brick: {uploads:}`, on `<input type=file>` | none | forms guides |
| `brick:upload-progress` | one upload field, on a `<progress>` | none | forms guides |

Keys for `brick:keydown` and `brick:keyup`: `enter escape tab space up down left right backspace delete`, a letter `a`–`z` or a digit `0`–`9`. A combo matches only when exactly the named modifier keys are held; a modifier key alone, or two keys, is an error. With no key the attribute fires for every key.

Modifiers that cannot be combined: `.defer` with `.lazy`, `.debounce` or `.throttle`; `.debounce` with `.throttle`; `.half` with `.full`; `.dwell` with `.leave`. A modifier given twice is an error. `.optimistic` takes only a literal `$set(path, value)` or `$toggle(path)`. Built-in names `brick:loading` accepts besides the component's functions: `$set $refresh $toggle $reset $upload $uploaded $removeUpload`.

## Attributes allowed in any file

These are not actions, so they work in any page, layout or component (a navigation morphs the whole page). They pass through to the HTML for the runtime.

| Attribute | Value | Modifiers | What it does | Guide |
|---|---|---|---|---|
| `brick:nav` | `off` or `replace` | none | `off`: a full page load for this link, form or anything inside the element; `replace`: replace the history entry | [navigation](../guides/interactivity/navigation.md) |
| `brick:prefetch` | empty, or `visible` | none | load the page on hover or focus (`visible`: when it scrolls into view); adds speculation rules | [navigation](../guides/interactivity/navigation.md) |
| `brick:persist` | none (the element needs an `id`) | none | keep the element across navigations, even when the new page moves it | [navigation](../guides/interactivity/navigation.md) |
| `brick:current` | none | `.exact` | mark the link to the page being shown with `aria-current="page"` and `data-current` | [navigation](../guides/interactivity/navigation.md) |
| `brick:scroll` | a name (letters, digits, `-`, `_`), once per file | none | keep this scroll container's position across navigations | [navigation](../guides/interactivity/navigation.md) |
| `brick:key` | required text | none | key an element among its siblings for the morph; `id` wins | [morph controls](../guides/interactivity/morph-transitions-client-commands.md) |
| `brick:ignore` | none | `.self` | keep the element as it is across morphs (`.self`: only its attributes); refused on a stateful root | [morph controls](../guides/interactivity/morph-transitions-client-commands.md) |
| `brick:replace` | none | none | swap the element for a fresh copy instead of patching it (remounts its client code) | [morph controls](../guides/interactivity/morph-transitions-client-commands.md) |
| `brick:keep` | space-separated attribute names | none | attributes the browser owns (`open`, `aria-expanded`); the morph leaves them alone | [morph controls](../guides/interactivity/morph-transitions-client-commands.md) |
| `brick:ref` | a name, once per file | none | name an element for `brick.ref`, `brick.stream`, `brick.rows`, `brick.region` and `"@name"` command targets | [streaming](../guides/interactivity/islands-lazy-and-streaming.md#step-4--stream-text-from-an-action) |
| `brick:offline` | none | `.remove` | show the element while the browser is offline (`.remove`: hide it) | [events, Step 4](../guides/interactivity/events-and-modifiers.md#step-4--respond-to-the-window-a-held-key-a-long-look-and-the-connection) |
| `brick:enter` | required classes | none | the look an element starts from when `brick:show` or a `show` command reveals it | [morph controls](../guides/interactivity/morph-transitions-client-commands.md#step-2--animate-the-morph) |
| `brick:leave` | required classes | none | the look an element goes to before it is hidden or removed | [morph controls](../guides/interactivity/morph-transitions-client-commands.md#step-2--animate-the-morph) |
| `brick:method` | `post`, `put`, `patch` or `delete`, on an `<a href>` | none | a click submits a hidden form with that verb; see `brick.MethodOverride` | forms guides |
| `brick:js.<event>` | client commands | see below | run DOM commands in the browser with no request | [client commands](../guides/interactivity/morph-transitions-client-commands.md#step-3--open-a-menu-with-client-commands) |

Only a stateful file may carry `brick:transition` (optional transition types, such as `slide`), because only a stateful root's action morph reads it: it runs that root's morphs in a view transition. On a method link (`brick:method`) in a file without `state:`, `brick:confirm` and `brick:disable-with` are allowed without modifiers.

## Client commands — `brick:js.<event>`

| Part | Allowed |
|---|---|
| Events (exactly one) | `click dblclick input change submit focus blur keydown keyup mouseenter mouseleave` |
| Modifiers | `.once`; `.outside` (click only); `.window` (keydown, keyup, focus, blur); `.ctrl`, `.meta`, `.shift`, `.alt` and one key (keydown and keyup only) |
| Commands (arguments, target included) | `show(t)`, `hide(t)`, `toggle(t)`, `focus(t)`, `addClass(t, "a b")`, `removeClass(t, "a b")`, `toggleClass(t, "a b")`, `setAttr(t, name, value)`, `removeAttr(t, name)`, `toggleAttr(t, name[, on[, off]])`, `dispatch(t, name[, detail])` |
| Targets | a CSS selector (every match), `"@name"` (a `brick:ref` in the element's component), `"this"`, `"closest <selector>"` |

Commands are separated by `;` and every argument is a JSON literal. `setAttr` and `toggleAttr` refuse `on*` handlers, `srcdoc`, engine attributes and script URLs in `href`, `src`, `action`, `formaction`, `xlink:href` and `poster`; `dispatch` refuses brick's own event names.

## Attributes on a component call

| Attribute | Value | What it does | Guide |
|---|---|---|---|
| `brick:lazy` | empty (`load`), `visible`, `idle` or `stream`, written literally | render the component after the page: fetched by the browser, or streamed later in the same response | [lazy islands](../guides/interactivity/islands-lazy-and-streaming.md) |
| `brick:mount` | `visible` or `idle`, written literally | delay the component's `brick.mount` hook until it scrolls into view or the browser is idle | guide |
| `brick:on:<event>` | an action call of the calling (stateful) component | run it when that one child dispatches `<event>`; the callee needs a single root | [events, Step 3](../guides/live/events.md#step-3--let-a-child-tell-its-parent-from-an-attribute) |
| `brick:spread` | a map (`.Attrs`) | pass every entry as a prop; an attribute written on the tag wins | guide |
| `brick:model` | a field of the calling stateful component | bind it to the callee's `brick: {model:}` field (Modelable) | [stateful components](../guides/interactivity/stateful-components.md#step-5--nest-a-component-that-binds-to-its-parent-modelable) |
| any attribute above | as above | placed by the callee with `{{ attrs }}`; action attributes only inside a stateful file | — |

Any other `brick:` name on a component call fails: `brick:<name> is reserved on a component call (brick accepts nav, prefetch, persist, keep, key, ignore, replace, ref, offline, current, leave, enter, scroll, method, js, and action events inside a stateful component)`.
