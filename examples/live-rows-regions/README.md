# live-rows-regions

A chat log fed row by row (`brick.rows`: insert at the top with a limit, delete by key, reset), streamed text (`brick.stream`), a "load more" feed that keeps its earlier pages (`brick.region`), and a sign-in form handed to the browser's own submit (`brick.submit`). None of the lists live in a component's state.

From the `examples` folder:

- `go run ./live-rows-regions -demo` prints each action, its streamed chunks and its effects (the same text as `testdata/transcript.txt`).
- `go run ./live-rows-regions` serves it on http://localhost:8085/.
- `go test ./live-rows-regions/` checks the transcript, that row text is escaped, and the error messages the guide quotes.

Guide: [Rows, regions, streams and native submit](../../docs/guides/live/rows-and-regions.md).

## Test output

```text
--- PASS: TestTranscript (0.01s)
--- PASS: TestRowTextIsEscaped (0.00s)
--- PASS: TestPitfalls (0.00s)
ok  	github.com/ubgo/brick/examples/live-rows-regions	0.029s
```

(`go test -v ./live-rows-regions/` from the `examples` folder.)