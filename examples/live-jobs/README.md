# live-jobs

A background job: a button starts a slow report (`brick.start_job`), the action answers at once, and the component refreshes when the job publishes its topic. `OnError` records failures and cancels, and a second button cancels a running job (`brick.cancel_job`). `queue.go` is the smallest custom `JobQueue`.

From the `examples` folder:

- `go run ./live-jobs -demo` prints a job that finishes and one that is cancelled (the same text as `testdata/transcript.txt`). Add `-queue` to run the jobs through `queue.go`.
- `go run ./live-jobs` serves it on http://localhost:8087/reports/ (the served job takes 3 seconds).
- `go test ./live-jobs/` checks the transcript through both the built-in pool and the custom queue, and the error messages the guide quotes.

Guide: [Background jobs](../../docs/guides/live/background-jobs.md).

## Test output

```text
--- PASS: TestTranscript (0.11s)
--- PASS: TestPitfalls (0.00s)
ok  	github.com/ubgo/brick/examples/live-jobs	0.131s
```

(`go test -v ./live-jobs/` from the `examples` folder.)