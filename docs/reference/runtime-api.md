# Browser runtime API (`window.brick`)

Every function the browser runtime puts on `window.brick`, every option the engine passes it, and every event it emits. The runtime is `runtime/runtime.js`, linked by `{{ assets }}` on any page that needs it; `runtime/nav.js` adds `brick.navigate` and `brick.refresh` when `Options.Navigation` is on. This page lists exactly what those two files export; names starting with `_` are internal test seams and not part of the API.

Client code runs in a `<brick:client>` block of a component (shipped once per component type) or in your own script loaded after the runtime. Where a function takes `el`, it is any element inside a stateful component; the runtime walks up to that component's root.

## Contents

- [Functions](#functions): [components and actions](#components-and-actions), [events and effects](#events-and-effects), [hooks](#hooks), [forms, errors and uploads](#forms-errors-and-uploads), [navigation](#navigation), [debugging](#debugging)
- [Runtime events](#runtime-events)
- [Options from the server (`Options.Runtime`)](#options-from-the-server-optionsruntime)

## Functions

### Components and actions

| Function | Returns | What it does | Guide |
|---|---|---|---|
| `brick.mount(type, fn)` | — | Runs `fn(el, props)` once for every element of a mounted component type, those on the page and any inserted later; `fn` may return a cleanup, run when the element is removed. `props` are the props marked `client: true`. | [Styles and browser JavaScript](../guide.md#styles-and-browser-javascript) |
| `brick.action(el, name, args = [])` | Promise | Calls server action `name` of the stateful component around `el`, as a `brick:click` would: for drag and drop, a keyboard shortcut, a command palette. `"$parent.save"` targets the stateful component around that one. Resolves when the answer is applied; rejects outside a stateful component. | [Server state](../guide.md#server-state--replies-first-values-the-url-and-memory) |
| `brick.reloadComponent(el)` | Promise | Renders `el`'s component fresh from the server after any action already queued on it, replaying nothing: the way back from an action that failed with a 500. Never fires `action-error` itself, so a listener that calls it cannot loop. | [The browser runtime](../guides/live/browser-runtime.md#step-3--recover-a-component-that-failed) |
| `brick.find(type)` | Element or `null` | The first stateful component root of that type (its file stem, `data-brick-name`), or the first of any type when `type` is omitted. | [Find components and follow their signals](../guides/live/browser-runtime.md#step-7--find-components-and-follow-their-signals) |
| `brick.all(type)` | Element[] | Every stateful root of that type in document order, or every stateful root when `type` is omitted. | [Find components and follow their signals](../guides/live/browser-runtime.md#step-7--find-components-and-follow-their-signals) |
| `brick.ref(el, name)` | Element or `null` | The element with `brick:ref="name"` in `el`'s stateful root (or on the page outside every root). | [Rows, regions, streams](../guides/live/rows-and-regions.md) |
| `brick.watch(el, path, fn)` | unwatch function | Calls `fn(value, old)` whenever the signal `path` of `el`'s stateful root changes: from a bound input, an optimistic write or a morph. Throws outside a stateful component. | [Find components and follow their signals](../guides/live/browser-runtime.md#step-7--find-components-and-follow-their-signals) |
| `brick.js(el, commands)` | — | Runs a `brick:js` command list from code (`brick.js(el, el.getAttribute("data-on-close"))`), with `el` standing for `this`. A bad list throws. | [Open a menu with client commands](../guides/interactivity/morph-transitions-client-commands.md#step-3--open-a-menu-with-client-commands) |
| `brick.morph(parent, next)` | — | Patches `parent`'s children into `next`'s with the runtime's own morph (keys, `brick:ignore`, kept children and focus respected). | — |

### Events and effects

| Function | Returns | What it does | Guide |
|---|---|---|---|
| `brick.on(name, fn)` | off function | Calls `fn(detail)` for every event `name`: a [runtime event](#runtime-events) or an app event an action dispatched. App events carry `detail.$source`, the sender's `data-brick` id. | [Events](../guides/live/events.md) |
| `brick.emit(name, detail)` | — | Emits an event on the page's bus, for client code only; it wakes no server listener. | [Events](../guides/live/events.md) |
| `brick.dispatch(el, name, data = {}, {to = ""} = {})` | — | Sends an app event from `el`'s component, as an action's `brick.dispatch` would: `brick.on` hears it and each listening component runs its action. Sends no request of its own. A bad or reserved name throws. | [Events](../guides/live/events.md#more-ways-to-send-and-hear-events) |
| `brick.effect(kind, fn)` | unregister function | Registers the handler of an effect kind: `fn(effect, root, ctx)`. A site's own kinds start with `x-`; an engine kind (`flash`, `title`, …) may be replaced, and the unregister function puts the previous handler back. Any other name throws `brick.effect: custom kinds start with "x-"`. | [Effects](../guides/live/effects.md#step-3--add-your-own-effect-kind-from-go) |

### Hooks

| Function | Returns | What it does | Guide |
|---|---|---|---|
| `brick.hook(name, fn)` | unregister function | Registers a hook. An unknown name throws, listing the five. A `ready` hook registered after boot runs at once. | [The browser runtime](../guides/live/browser-runtime.md#step-4--add-a-header-to-every-action) |

| Hook | `fn` receives | Notes |
|---|---|---|
| `request` | `{root, action, args, trigger, retry, headers, cancel()}` | Before each action, upload (`action: "$upload"`) and lazy island request (`root: null`, `island`). Change `headers` in place to add your own; changing `Content-Type`, `Accept` or any `Brick-*` header throws and fails the action. `cancel()` drops the request (an action then emits `action-cancelled`). May be async. |
| `response` | `{root, action, status, envelope}` | After each action answer, before it is applied. |
| `morph` | `{from, to}` | During a morph, per element; return `false` to leave `from` as it is. Runs synchronously; a hook that throws counts as allowing. |
| `morphed` | `{root}` | After a component's answer was morphed in. |
| `ready` | `{}` | Once, after the runtime booted. |

### Forms, errors and uploads

| Function | Returns | What it does | Guide |
|---|---|---|---|
| `brick.errors(el)` | object | The validation errors of `el`'s stateful root as its last render left them, with no request: `has(path)`, `get(path)`, `first(path)`, `all()`, and `clear(path)` (no path: all), which drops them from this view and emits `errors-cleared`. | [Validation, Step 5](../guides/forms/validation.md#step-5--read-the-errors-from-browser-code-with-brickerrors) |
| `brick.upload(el, field, files)` | Promise | Sends files to an upload field of `el`'s component, as a `brick:upload` input does: for a drop zone, a paste, a camera capture. Resolves once every accepted file is done. | [Uploads, Step 7](../guides/forms/uploads.md#step-7--send-files-from-code-with-brickupload-and-hear-the-events) |
| `brick.confirmWith(fn)` | restore function | Replaces the confirmer `brick:confirm` asks: `fn(message, el)` returns a boolean or a promise of one. | [Deferred fields, Step 5](../guides/forms/deferred-fields.md#step-5--bind-list-rows-and-choose-how-to-ask-before-you-act) |
| `brick.promptWith(fn)` | restore function | Replaces the prompter `brick:confirm.prompt` asks: `fn(message, el)` returns the answer, `null` for a cancel, or a promise of either. | [Deferred fields, Step 5](../guides/forms/deferred-fields.md#step-5--bind-list-rows-and-choose-how-to-ask-before-you-act) |
| `brick.methodFormWith(fn)` | restore function | Replaces the hook a `brick:method` link's hidden form passes through before it is submitted: `fn(form, link)` may append fields, such as your CSRF token. | [Method links, Step 4](../guides/forms/method-links.md#step-4--add-your-csrf-token-with-brickmethodformwith) |

### Navigation

Present only when `runtime/nav.js` is loaded (`Options.Navigation`).

| Function | Returns | What it does | Guide |
|---|---|---|---|
| `brick.navigate(url, {replace = false} = {})` | — | Shows `url` in place, keeping `brick:persist` elements; `replace` swaps the history entry instead of adding one. The `navigate` effect uses it when it exists. | [Navigation without reloads](../guide.md#navigation-without-reloads) |
| `brick.refresh()` | — | Re-renders the current page in place (the dev server's live reload calls it), falling back to a full reload when assets changed. Emits `refreshed`. | [A whole site](../guide.md#a-whole-site--urls-serving-building) |

### Debugging

| Function | Returns | What it does | Guide |
|---|---|---|---|
| `brick.debug(on = true)` | — | Logs every action request and every answer the runtime lands to the console. | [The browser runtime](../guides/live/browser-runtime.md#step-6--debug-from-the-console) |
| `brick.latency(ms = 0)` | — | Delays each request by `ms`, to watch loading states and races on a fast local server. `0` turns it off. | [The browser runtime](../guides/live/browser-runtime.md#step-6--debug-from-the-console) |

Both start off and are not in the `brick-config` meta: they are for your own browser, never a page's behaviour.

## Runtime events

Events the runtime emits on `brick.on`. Their names are reserved: an action that dispatches one of the reserved names fails, and so does `brick.dispatch` in the browser.

| Event | `detail` | When |
|---|---|---|
| `action-error` | `{action, message, root, id, status}` | An action failed (`status` is the HTTP status, 0 for a network error). For a malformed call in the page only `{action, message}`. |
| `action-retry` | `{id, action, status, retry, wait}` | An action answered 503 or 429 and is sent again after `wait` ms (`retry` counts from 1). |
| `action-cancelled` | `{id, action}` | A `request` hook cancelled an action. |
| `state-gone` | `{id, action, preventDefault()}` | The server no longer has a component's state (410). The runtime rebuilds it from a fresh render and replays the action, unless a listener calls `preventDefault()`. |
| `recovered` | `{id}` | That rebuild succeeded and the action ran. |
| `state-lost` | `{id}` | Recovery failed and the page has already reloaded the maximum number of times in a minute; the page stays as it is. |
| `flash` | `{message, level}` | A flash effect. With a listener (a kit `<Toaster>`), the runtime does not show its own message. |
| `flash-clear` | `{level}` | A clear-flash effect; `level` is `""` for all. |
| `effect` | the effect object | An effect kind no handler is registered for (also logged with `console.warn`). |
| `event-loop` | `{name, chain}` | An app event was not delivered: it would have repeated a name in its chain or gone past 4 hops. |
| `push-error` | `{failures}` | The page's push stream was still refused after 8 retries; the runtime stops retrying. |
| `upload-progress` | `{field, name, size, loaded, total}` | An upload sent more bytes. |
| `upload-done` | `{field, name, size}` | An upload finished. |
| `upload-error` | `{message, field, name, size, …}` | An upload failed or was refused. |
| `island-loaded` | `{failed}` | A lazy island arrived; `failed` is true when the server rendered its error fallback. |
| `island-error` | `{message}` | A lazy island could not be fetched. |
| `errors-cleared` | `{root, id, path}` | `brick.errors(el).clear(path)` ran; `path` is `""` for all. |
| `navigate` | `{url, preventDefault()}` | nav.js is about to navigate in place; `preventDefault()` stops it. |
| `navigated` | `{url}` or `{url, patched: true}` | An in-place navigation finished; `patched` when only URL parameters a component owns changed. |
| `refreshed` | `{url}` | `brick.refresh()` re-rendered the page. |

The reserved names are `action-error`, `action-cancelled`, `action-retry`, `recovered`, `state-gone`, `state-lost`, `navigate`, `navigated`, `flash`, `flash-clear`, `effect`, `toast`, `event-loop`, `island-loaded`, `island-error`, `errors-cleared`, `upload-progress`, `upload-done`, `upload-error`, `push-error`, `refreshed`, `dirty`, `clean`, and anything starting with `brick:`.

## Options from the server (`Options.Runtime`)

`brick.Options.Runtime` (`brick.RuntimeConfig`) is written into the page as `<meta name="brick-config" content="…">` just before the runtime script and read once when the runtime starts. The zero value writes no meta. A missing, wrong-typed or out-of-range value keeps the default, and `brick.New` refuses a setting the runtime would ignore.

| Go field | Meta key | Default | Meaning |
|---|---|---|---|
| `Debounce` | `debounce` (ms) | 250 ms (`DefaultDebounce`) | How long `brick:model` waits after the last keystroke when the attribute names no `.debounce`. |
| `PollMin` | `pollMin` (ms) | 1 s (`DefaultPollMin`) | The floor of every `brick:poll` interval; never below 250 ms (`MinPollMin`). |
| `ActionURL` | `actionURL` | beside the runtime script | Where actions are posted, for a static export whose actions run on another server (which must allow CORS). Must end in `/action`; uploads and islands go beside it. |
| `Retries` | `retries` | 1 (`DefaultRetries`) | How many times an action refused with 503 or 429 is sent again before `action-error`; `brick.NoRetry` turns retrying off. Uploads are never re-sent. |
| `RetryMaxWait` | `retryMaxWait` (ms) | 5 s (`DefaultRetryMaxWait`) | The longest `Retry-After` the runtime waits for; an answer asking for longer is reported as an error at once. |

Push stream retries are fixed: 1 s doubling to 30 s, 8 attempts, then `push-error`.

## Related

- Guides: [Effects](../guides/live/effects.md), [Events](../guides/live/events.md), [Rows, regions, streams and native submit](../guides/live/rows-and-regions.md), [Push and presence](../guides/live/push-and-presence.md), [Background jobs](../guides/live/background-jobs.md), [The browser runtime](../guides/live/browser-runtime.md).
- Feature index: [Browser runtime API](features.md#browser-runtime-api).
- Source: [runtime/runtime.js](../../runtime/runtime.js) and [runtime/nav.js](../../runtime/nav.js).
