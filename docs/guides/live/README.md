# Live updates and effects

Guides for everything that happens after an action runs: what the browser does with the answer, how other components and other people hear about it, and how slow work reports back. Each guide is built around a runnable example in `examples/live-*`, and every code block and output on these pages is copied from that example, whose test checks it.

| Guide | You want to… | Example |
|---|---|---|
| [Effects](effects.md) | show a flash, change the title, go to another page, download a file, change the address bar, or send your own `x-` effect | [live-effects-events](../../../examples/live-effects-events/) |
| [Events](events.md) | have one component tell the others that something changed (`brick.dispatch`, `brick: {on:}`) | [live-effects-events](../../../examples/live-effects-events/) |
| [Rows, regions, streams and native submit](rows-and-regions.md) | feed a list row by row, keep "load more" pages, stream text while an action runs, or hand a form to a classic POST | [live-rows-regions](../../../examples/live-rows-regions/) |
| [Push and presence](push-and-presence.md) | refresh every open page when data changes, from an action or from Go, and show who is on a page | [live-push-presence](../../../examples/live-push-presence/) |
| [Background jobs](background-jobs.md) | run slow work after the response and refresh the page when it ends; cancel it; use your own queue | [live-jobs](../../../examples/live-jobs/) |
| [The browser runtime](browser-runtime.md) | respond to runtime events, recover a failed component, add request headers, tune retries | [live-effects-events](../../../examples/live-effects-events/) |

Reference: [Browser runtime API (`window.brick`)](../../reference/runtime-api.md) lists every function, hook, runtime event and `brick-config` setting.

Run any example from the `examples` folder: `go run ./live-effects-events -demo` prints the requests and answers the guide shows, `go run ./live-effects-events` serves it for a browser, and `go test ./live-effects-events/` checks the output.

The [examples/live-client](../../../examples/live-client/) package is the small Go client the demos use to post actions and open push streams the way the browser runtime does; it is a teaching aid, not a second runtime.
