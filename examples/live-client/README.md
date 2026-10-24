# live-client

`package liveclient`: the part of the browser runtime the `live-*` examples need to show their wire traffic from Go. It reads stateful components off a page, posts actions the way `runtime.js` does (a JSON POST with the `Brick-Action` header, asking for the effects envelope or the NDJSON stream), opens a page's push stream, and prints a transcript. It is a teaching aid, not a runtime: it does not morph, queue, retry or run effects.

`go test ./live-client/` checks how it reads roots and tidies markup. It is used by [live-effects-events](../live-effects-events/), [live-rows-regions](../live-rows-regions/), [live-push-presence](../live-push-presence/) and [live-jobs](../live-jobs/); the guides are under [docs/guides/live](../../docs/guides/live/README.md).

## Test output

```sh
cd examples
go test -v ./live-client/
```

```text
--- PASS: TestRootsAndFind (0.00s)
--- PASS: TestTidy (0.00s)
ok  	github.com/ubgo/brick/examples/live-client	0.017s
```