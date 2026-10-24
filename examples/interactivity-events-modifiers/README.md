# interactivity-events-modifiers

One stateful `Inbox` whose elements carry every kind of event attribute: `brick:input.debounce`, `brick:blur`, `brick:keydown` and `brick:keyup` with keys and combos, `brick:click.outside`, `brick:change`, `brick:click.details`, `brick:confirm.prompt`, `brick:click.once`, `brick:intersect.once` and `brick:poll`. Each action logs what it was called with. A `Panel` adds the page-level events: `brick:keydown.norepeat.window.n`, `brick:focus.window` and `brick:blur.window`, `brick:intersect.dwell.2000ms`, and `brick:offline` with `.remove`. The engine logs actions through `actionmw.Slog`, and `Panel` and a polling `Ticker` set `brick: {log: debug}` and `brick: {log: false}`. A test pins the compile errors for modifier typos.

The guide that walks through it: [Events and modifiers](../../docs/guides/interactivity/events-and-modifiers.md).

## Run it

```sh
cd examples/interactivity-events-modifiers
go run .
```

Then open http://localhost:8092. `-addr` changes the listen address and `-theme` the project directory. Press `n`, switch tabs, go offline in the browser's network panel and watch the terminal: the `Ticker` poll never appears in the action log.

## Test it

```sh
cd examples
go test -v ./interactivity-events-modifiers/
```

```text
--- PASS: TestEveryEvent (0.01s)
--- PASS: TestTyposFailAtCompile (0.00s)
--- PASS: TestPanelWindowDwellOffline (0.00s)
--- PASS: TestActionLogSettings (0.00s)
--- PASS: TestActionLogOffStillLogsFailures (0.00s)
ok  	github.com/ubgo/brick/examples/interactivity-events-modifiers	0.034s
```
