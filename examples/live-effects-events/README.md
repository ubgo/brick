# live-effects-events

An inbox whose actions ask the browser to do things besides re-rendering: a flash, a new page title, a navigation, a download, an address-bar change, a redirect, a custom `x-confetti` effect added by Go middleware after the action, and a second custom `x-audit` effect that other middleware records before the action with `brick.EffectsFrom(ctx).Add`. Archiving a message dispatches `message-archived`, which the separate Badge component hears through `brick: {on:}`. The Inbox's `<brick:client>` block also shows a request hook, runtime event listeners and `brick.reloadComponent`, and `main.go` sets `Options.Runtime` (retries), `Options.RedirectHosts` (one payment host) and `Options.MaxEffectBytes` (4 KiB). A `Thread` page shows a child telling its parent with `brick:on:`, `$dispatch` and `$parent`, and the `Badge` has a client module using `brick.mount`, `brick.watch`, `brick.find` and `brick.all`.

From the `examples` folder:

- `go run ./live-effects-events -demo` prints each action and the effects its answer carried (the same text as `testdata/transcript.txt`).
- `go run ./live-effects-events` serves it on http://localhost:8084/.
- `go test ./live-effects-events/` checks the transcript, the listener delivery with `bricktest.Live`, the client module, the `brick-config` meta and the error messages the guides quote, the audit effect, the allowed and the refused redirect host, the over-limit error, the thread's child events, and that the Badge's client module is served. Running that module (the watcher firing after a morph) takes a browser. A passing run prints `ok  	github.com/ubgo/brick/examples/live-effects-events`.

Guides: [Effects](../../docs/guides/live/effects.md), [Events](../../docs/guides/live/events.md), [The browser runtime](../../docs/guides/live/browser-runtime.md).
