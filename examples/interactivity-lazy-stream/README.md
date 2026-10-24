# interactivity-lazy-stream

A cached page with an island filled per request (`Account`), a lazy island fetched by the browser when it scrolls into view (`SalesChart`, `brick:lazy="visible"`), a streamed island sent later in the same response (`Revenue`, `brick:lazy="stream"`) and an action that streams its reply into a `brick:ref` with `brick.stream` (`Ask`). The engine is built with `Options.IslandTickets` over a sealed store, so a ticket on a visitor's screen still redeems after a restart or on a second server that holds the same keys; the demo key in `main.go` is public and only for the example.

The guide that walks through it: [Islands, lazy islands and streaming](../../docs/guides/interactivity/islands-lazy-and-streaming.md).

## Run it

```sh
cd examples/interactivity-lazy-stream
go run .
```

Then open http://localhost:8093/?user=Sara. `-addr` changes the listen address and `-theme` the project directory. `curl -s 'localhost:8093/?user=Sara'` shows the filled island, the lazy placeholder with its sealed ticket (`data-brick-lazy="s1.…"`) and the stream island rendered in place.

## Test it

```sh
cd examples
go test -v ./interactivity-lazy-stream/
```

```text
--- PASS: TestIslandsAndLazy (0.01s)
--- PASS: TestStreamIsland (0.01s)
--- PASS: TestStreamedReply (0.01s)
--- PASS: TestTicketsSurviveARestart (0.01s)
--- PASS: TestLazyInlineInTests (0.03s)
ok  	github.com/ubgo/brick/examples/interactivity-lazy-stream	0.131s
```

`TestTicketsSurviveARestart` redeems a ticket on a new engine with the same keys (200) and with other keys (410); `TestLazyInlineInTests` reads a lazy island with `brick.WithLazyInline()`.
