# The browser runtime — events, hooks, recovery and retries

## What it is and when you want it

The browser runtime (`runtime/runtime.js`, linked by `{{ assets }}`) sends actions, morphs answers in and runs effects. Most pages never talk to it directly. You reach for its JavaScript API (`window.brick`) when your own client code must respond to what it does: show a toast when an action fails, start a component over after a server error, add a header to every request, handle your own effect kind, or tune how it retries. This page walks through those cases; the [runtime API reference](../../reference/runtime-api.md) lists every function and event.

## Before you start

- Client code lives in a `<brick:client>` block of a component. It ships once per component type as a hashed module, however many times the component appears. A page or layout cannot hold one: it fails with `<brick:client> and <brick:style scoped> belong in a component, not a page`.
- The example is [examples/live-effects-events](../../../examples/live-effects-events/): its `Inbox.html` holds all the client code on this page.

## Step 1 — Handle your own effect kind

An action (or Go middleware) can send an effect kind of its own, such as `x-confetti`; `brick.effect` registers what the browser does with it. See [Effects, Step 3](effects.md#step-3--add-your-own-effect-kind-from-go) for the server side.

## Step 2 — Respond to runtime events

`brick.on(name, fn)` hears the runtime's own events as well as your app's. `action-error` carries the component's root and the HTTP status, `action-retry` says an action is being sent again. The Inbox's whole client block (it also handles the `x-audit` kind from [Effects, Step 4](effects.md#step-4--record-an-effect-from-go-with-effectsfrom)):

`examples/live-effects-events/theme/components/Inbox.html` (the `<brick:client>` block)

<!-- snippet: examples/live-effects-events/theme/components/Inbox.html lines=52-78 -->
```js
// The browser side of the host's own effect kind: the server sends data,
// never code, and this handler decides what x-confetti looks like.
brick.effect("x-confetti", (effect) => {
  document.documentElement.dataset.confetti = String(effect.data.pieces);
});

// The second custom kind, recorded from Go by the audit middleware.
brick.effect("x-audit", (effect) => {
  document.documentElement.dataset.audited = effect.data.action;
});

// Tell the host which build of the page sent each action. A hook may add
// headers; changing Content-Type, Accept or a Brick-* header throws.
brick.hook("request", (r) => {
  r.headers["X-Page-Build"] = "inbox-1";
});

// A 422 is a script failure the component already shows; a 500 or more
// comes from the host, so start that component over from a fresh render.
brick.on("action-error", (d) => {
  if (d.root && d.status >= 500) brick.reloadComponent(d.root);
});

// The runtime retried an action after a 503 or 429.
brick.on("action-retry", (d) => {
  console.info("retrying", d.action, "in", d.wait, "ms");
});
```

Every event and its `detail` is in the [runtime events table](../../reference/runtime-api.md#runtime-events).

## Step 3 — Recover a component that failed

`brick.reloadComponent(el)` (in the `action-error` listener above) fetches the page again and morphs that one component in fresh, replaying nothing. It waits for actions already queued on the component, resolves when the fresh render is in, and rejects without firing `action-error` when the page cannot be fetched, so the listener cannot loop. Your listener chooses which statuses to recover from: a failed script action answers 422, so a 500 comes from your own middleware or the server around it.

A lost state (410, the store forgot the instance or the key was retired) is recovered by the runtime itself: it emits `state-gone`, rebuilds the component from a fresh render, replays the action and emits `recovered`. Call `detail.preventDefault()` in a `state-gone` listener to handle it yourself.

## Step 4 — Add a header to every action

The `request` hook (above) runs before every action, upload and lazy island request with `{root, action, args, trigger, retry, headers, cancel()}`. Add headers to `headers`; call `cancel()` to drop the request, which then emits `action-cancelled`. The other hooks are `response` (`{root, action, status, envelope}`), `morph` (`{from, to}`, return `false` to leave an element alone), `morphed` (`{root}`) and `ready`. Each `brick.hook` call returns a function that unregisters it.

## Step 5 — Tune retries and other runtime settings from Go

A 503 (the state store or a limiter is down) or a 429 (a rate limit) means the action did not run, so the runtime sends it again, waiting for the answer's `Retry-After`. `Options.Runtime` sets how often and how long; the engine writes it into the page as a `brick-config` meta.

<!-- ds:block id=runtimeconfig-axe34jn4 title="examples/live-effects-events/main.go" -->
**examples/live-effects-events/main.go** · [`examples/live-effects-events/main.go:51`](../../../examples/live-effects-events/main.go#L51-L51)

```go
var runtimeConfig = brick.RuntimeConfig{Retries: 2, RetryMaxWait: retryMaxWait}
```
<!-- /ds:block hash=a68d12 -->

<!-- ds:block id=retrymaxwait-wcjb4a6k title="examples/live-effects-events/main.go" -->
**examples/live-effects-events/main.go** · [`examples/live-effects-events/main.go:56`](../../../examples/live-effects-events/main.go#L56-L56)

```go
const retryMaxWait = 3 * time.Second
```
<!-- /ds:block hash=b2cdfd -->

Your server asks for a retry by refusing with a status and a wait, from an `ActionMiddleware`: `return nil, &brick.ActionError{Status: http.StatusServiceUnavailable, RetryAfter: 2 * time.Second}`. The other settings, `Debounce`, `PollMin` and `ActionURL`, are in the [reference](../../reference/runtime-api.md#options-from-the-server-optionsruntime).

## Step 6 — Debug from the console

In the browser console on any brick page:

```js
brick.debug(true)   // log every action request and every answer
brick.latency(800)  // delay each request by 800 ms, to watch loading states
brick.latency(0)    // back to normal
```

Both start off and are not in `brick-config`: they are for your own browser, never the page's behaviour.

## Step 7 — Find components and follow their signals

Client code often needs another component, or needs to know when one value of its own component changes. `brick.find(type)` returns the first stateful component root of that file stem (or `null`), `brick.all()` returns every stateful root on the page in document order, and `brick.watch(el, path, fn)` calls `fn(value, old)` whenever the signal `path` of `el`'s root changes, whether a click, an optimistic write or a morph changed it. It returns a function that stops watching, so returning it from a `brick.mount` callback stops the watcher when the component leaves the page.

`examples/live-effects-events/theme/components/Badge.html`

<!-- snippet: examples/live-effects-events/theme/components/Badge.html -->
```html
---
state: {archived: 0, last: 0}
brick:
  on:
    message-archived: count
---
<brick:script>
def count(state, props, event):
    return dict(state, archived = state["archived"] + 1, last = event["id"])
</brick:script>
<brick:client>
// Run once for every Badge on the page, now or inserted later. brick.watch
// calls the function whenever the "archived" signal of this root changes,
// whether a click, an optimistic write or a morph changed it, and returns
// a function that stops watching; returning it as the cleanup runs it
// when the Badge leaves the page.
brick.mount("Badge", (badge) =>
  brick.watch(badge, "archived", (count, was) => {
    badge.dataset.delta = String(count - was);
  }),
);

// brick.find(type) is the first stateful root of that type (or null) and
// brick.all() every stateful root on the page, in document order.
brick.on("message-archived", () => {
  const inbox = brick.find("Inbox");
  if (inbox) inbox.dataset.touched = "yes";
  document.documentElement.dataset.statefulRoots = String(brick.all().length);
});
</brick:client>
<p class="badge">{{ .State.archived }} archived{{ if .State.last }}, last #{{ .State.last }}{{ end }}</p>
```

The Go test fetches the Badge's served module and checks that these calls are in it. What only a browser does is run them: the watcher firing after a morph and `brick.find` answering from the live page. To see it, run the example, archive a message, and read `document.querySelector("[data-delta]").dataset.delta` in the console (it is `1`); `brick.all()` there lists the Badge and the Inbox.

<!-- ds:block id=testbadgeclientserved-z7c5n2wk title="examples/live-effects-events/main_test.go" -->
**examples/live-effects-events/main_test.go** · [`examples/live-effects-events/main_test.go:246-270`](../../../examples/live-effects-events/main_test.go#L246-L270)

```go
func TestBadgeClientServed(t *testing.T) {
	a, err := newApp("theme")
	if err != nil {
		t.Fatal(err)
	}
	h := a.handler()
	get := func(path string) string {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s: %d", path, rec.Code)
		}
		return rec.Body.String()
	}
	m := badgeScriptRE.FindStringSubmatch(get("/"))
	if m == nil {
		t.Fatal("no Badge module on the page")
	}
	js := get(m[1])
	for _, want := range []string{`brick.mount("Badge"`, `brick.watch(badge, "archived"`, `brick.find("Inbox")`, `brick.all().length`} {
		if !strings.Contains(js, want) {
			t.Errorf("module lacks %s:\n%s", want, js)
		}
	}
}
```
<!-- /ds:block hash=11666a -->

## Try it

From the `examples` folder, serve the example and look at the meta the engine wrote:

```sh
go run ./live-effects-events -addr 127.0.0.1:18084 &
curl -s http://localhost:18084/ | grep -o '<meta name="brick-config"[^>]*>'
```

```text
<meta name="brick-config" content="{&#34;retries&#34;:2,&#34;retryMaxWait&#34;:3000}">
```

The attribute is HTML-escaped JSON, `{"retries":2,"retryMaxWait":3000}`. `go test ./live-effects-events/` checks this meta (`TestPageLinksRuntime`), checks that the Inbox's client module is served and holds the hook and listeners (`TestClientModuleServed`), that the Badge's holds `brick.watch`, `brick.find` and `brick.all` (`TestBadgeClientServed`), and checks that `brick.New` refuses a bad setting (`TestRuntimeConfigRefused`). Open http://localhost:18084/ and run `brick.debug(true)` in the console, then click Archive, to see the requests the runtime sends.

## Pitfalls

- **`brick.New` refuses settings the runtime would ignore.** `RuntimeConfig{PollMin: 100 * time.Millisecond}` fails with `brick: Options.Runtime.PollMin 100ms is below the 250ms floor`; negative durations and a `Retries` below `brick.NoRetry` fail the same way.
- **Retries have a ceiling.** An answer whose `Retry-After` is longer than `RetryMaxWait` (5 s by default) is not retried: the runtime reports `action-error` at once, because a page frozen that long is worse than an error. Uploads are never re-sent, and a streamed answer that has started is never retried.
- **Protected headers.** A `request` hook that changes `Content-Type`, `Accept` or any `Brick-*` header throws `brick: a request hook may not change the … header`, and the action fails. These headers are the CSRF floor and the protocol.
- **Reserved event names.** `brick.dispatch(el, "flash")` throws: the runtime's own event names are reserved so an action can never fake one.
- **Unknown hook names throw.** `brick.hook("before", fn)` throws `brick.hook: unknown hook "before" (request, response, morph, morphed, ready)`.
- **No handler, no failure.** An effect kind nobody registered is logged with `console.warn` and emitted as `effect`; the rest of the answer still applies.

## Related

- [Runtime API reference](../../reference/runtime-api.md): every function, hook, event and `brick-config` key.
- [Effects](effects.md) and [Events](events.md).
- Feature index: [Browser runtime API](../../reference/features.md#browser-runtime-api).
- The one-page guide: [The browser runtime](../../guide.md#the-browser-runtime--keys-transitions-viewport-actions-hooks).
- Example: [examples/live-effects-events](../../../examples/live-effects-events/).
