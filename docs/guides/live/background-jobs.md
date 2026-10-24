# Background jobs — slow work that reports back

## What it is and when you want it

An action runs inside its request, under the script's step and time limits. A monthly report or a slow third-party call either keeps the button spinning for a minute or is stopped halfway. A background job moves that work out of the request: the action starts the job and answers at once, the job runs afterwards and writes its result through your own data layer, and when it ends it publishes a topic, so every component [listening to that topic](push-and-presence.md) refreshes and shows the result. You want a job for anything slower than a click should wait for: exports, reports, imports, calls to slow services.

## Before you start

- Jobs report back through server push, so set up [push](push-and-presence.md) first: the component listens to the job's topic, and `JobsOptions.Push` publishes it.
- The engine has `Options.Jobs` (`brick.NewJobs`).
- The example is [examples/live-jobs](../../../examples/live-jobs/). Run it from the `examples` folder.

## Step 1 — Start the job from an action

`brick.start_job(name, args, topic)` returns the job's id at once. The action keeps the id in its state; the template asks your data layer (`report`, a Go function) what became of it. `brick.cancel_job(id)` in another action stops it.

`examples/live-jobs/theme/components/Report.html`

<!-- snippet: examples/live-jobs/theme/components/Report.html -->
```html
---
state: {job: ""}
props: {month: {type: string}}
brick: {listen: reports}
---
<brick:script>
def start(state, props):
    id = brick.start_job("report", {"month": props["month"]}, topic = "reports")
    return dict(state, job = id)

def cancel(state, props):
    brick.cancel_job(state["job"])
    return state
</brick:script>
{{ $r := report .State.job }}
<div class="report">
  {{ if eq $r.Status "running" }}
    <p>Building the {{ .Props.month }} report…</p>
    <button brick:click="cancel">Cancel</button>
  {{ else }}
    {{ with $r.Status }}<p>{{ . }}: {{ $r.Detail }}</p>{{ end }}
    <button brick:click="start">Build report</button>
  {{ end }}
</div>
```

## Step 2 — Register the job and record its outcome

The job's name must be a key of `JobsOptions.Funcs`, a closed set of Go functions, so a script never chooses the code that runs. `OnError` hears every failure, including a cancel, before the topic is published, so the outcome is written by the time the page re-renders.

`examples/live-jobs/main.go`

<!-- snippet: examples/live-jobs/main.go lines=89-140 -->
```go
// newApp wires push and jobs into the engine. Jobs publish their topic
// through Push when they end, so the two are built in that order. queue
// is nil for the built-in pool, or a JobQueue such as chanQueue.
func newApp(dir string, queue brick.JobQueue) (*app, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	a := &app{reports: &reports{byID: map[string]report{}}, work: sleepWork}
	push, err := brick.NewPush(brick.PushOptions{})
	if err != nil {
		return nil, err
	}
	jobs, err := brick.NewJobs(brick.JobsOptions{
		Push:  push,
		Funcs: map[string]brick.JobFunc{jobReport: a.runReport},
		// Queue nil means the in-process pool (4 workers, 256 waiting).
		Queue: queue,
		// OnError runs before the topic is published, so the failure is
		// recorded by the time the listening component re-renders.
		OnError: func(ctx context.Context, job brick.Job, err error) {
			if errors.Is(err, brick.ErrJobCancelled) {
				a.reports.set(job.ID, report{Status: statusCancelled, Detail: "stopped before it finished"})
				return
			}
			a.reports.set(job.ID, report{Status: statusFailed, Detail: err.Error()})
		},
	})
	if err != nil {
		return nil, err
	}
	if q, ok := queue.(*chanQueue); ok {
		go q.work(jobs.Run)
	}
	sc, err := scriptstarlark.New(scriptstarlark.Options{})
	if err != nil {
		return nil, err
	}
	eng, err := brick.New(brick.Options{
		FS:        root.FS(),
		Scripting: sc,
		States:    brick.NewMemoryStates(0, 0),
		Push:      push,
		Jobs:      jobs,
		Funcs:     map[string]any{"report": a.reports.get},
	})
	if err != nil {
		return nil, err
	}
	a.eng, a.jobs = eng, jobs
	return a, nil
}
```

The job itself gets its args and its own context, never the request's, the session or the component's state. It must return when the context ends: that is how a cancel and the timeout stop it.

<!-- ds:block id=app-runreport-kunttkhe title="examples/live-jobs/main.go" -->
**examples/live-jobs/main.go** · [`examples/live-jobs/main.go:145-152`](../../../examples/live-jobs/main.go#L145-L152)

```go
func (a *app) runReport(ctx context.Context, job brick.Job) error {
	month, _ := job.Args["month"].(string)
	if err := a.work(ctx, month); err != nil {
		return err
	}
	a.reports.set(job.ID, report{Status: statusDone, Detail: "the " + month + " report has 3 rows"})
	return nil
}
```
<!-- /ds:block hash=f56e58 -->

The template function the component reads:

<!-- ds:block id=reports-get-69zu49hj title="examples/live-jobs/main.go" -->
**examples/live-jobs/main.go** · [`examples/live-jobs/main.go:59-69`](../../../examples/live-jobs/main.go#L59-L69)

```go
func (rs *reports) get(id string) report {
	if id == "" {
		return report{}
	}
	rs.mu.Lock()
	defer rs.mu.Unlock()
	if r, ok := rs.byID[id]; ok {
		return r
	}
	return report{Status: statusRunning}
}
```
<!-- /ds:block hash=694350 -->

## Step 3 — Close jobs on shutdown

`Jobs.Close(ctx)` cancels running jobs and reports the ones still waiting through `OnError`, so no job vanishes without a record.

<!-- ds:block id=main-8pguu8zt title="examples/live-jobs/main.go" -->
**examples/live-jobs/main.go** · [`examples/live-jobs/main.go:184-216`](../../../examples/live-jobs/main.go#L184-L216)

```go
func main() {
	addr := flag.String("addr", ":8087", "listen address")
	dir := flag.String("theme", "live-jobs/theme", "the project directory")
	demoRun := flag.Bool("demo", false, "print the job flow from Go, then exit")
	ownQueue := flag.Bool("queue", false, "run jobs through the example's own JobQueue instead of the built-in pool")
	flag.Parse()
	var queue brick.JobQueue
	if *ownQueue {
		queue = newChanQueue(chanQueueSize)
	}
	a, err := newApp(*dir, queue)
	if err != nil {
		log.Fatal(err)
	}
	// Close cancels running jobs and reports waiting ones through OnError.
	defer func() {
		if err := a.jobs.Close(context.Background()); err != nil {
			log.Print(err)
		}
	}()
	if *demoRun {
		if err := demo(os.Stdout, a); err != nil {
			log.Fatal(err)
		}
		return
	}
	log.Printf("serving on %s", *addr)
	// Not log.Fatal: returning lets the deferred Close record waiting jobs.
	srv := &http.Server{Addr: *addr, Handler: a.handler()}
	if err := srv.ListenAndServe(); err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
}
```
<!-- /ds:block hash=694d08 -->

## Step 4 (optional) — Use your own queue

By default jobs run in an in-process pool: 4 at once (`Workers`), 256 waiting (`QueueSize`); a full pool refuses with `ErrJobQueueFull` rather than stall the action. A real queue (Redis, SQS, Hatchet) plugs in through `JobsOptions.Queue`. It has one method, `Enqueue(ctx, job)`; its worker calls `jobs.Run(ctx, job)`, which looks the name up again, reports a failure to `OnError` and publishes the topic. This is the smallest one:

`examples/live-jobs/queue.go`

<!-- snippet: examples/live-jobs/queue.go -->
```go
package main

import (
	"context"

	"github.com/ubgo/brick"
)

// chanQueueSize is how many jobs the example queue holds before Enqueue
// refuses; a real queue (Redis, SQS, Hatchet) is bounded by its server.
const chanQueueSize = 16

// chanQueue is the smallest JobQueue: a channel and one worker. It shows
// the contract a real queue meets. Enqueue hands the job over and returns
// at once; the worker, here a goroutine and in production another
// process, runs it with Jobs.Run, which looks the name up in
// JobsOptions.Funcs again, reports a failure to OnError and publishes the
// topic. It does not implement brick.JobCanceller, so only a job already
// running can be cancelled (Jobs tracks those itself); cancelling one
// still waiting here answers brick.ErrJobCancelUnsupported.
type chanQueue struct {
	jobs chan brick.Job
}

// newChanQueue returns a queue that holds at most size waiting jobs; beyond
// that Enqueue refuses instead of blocking the action that started the job.
func newChanQueue(size int) *chanQueue {
	return &chanQueue{jobs: make(chan brick.Job, size)}
}

// Enqueue refuses rather than blocks when the queue is full: the action
// that started the job has already answered, and the refusal reaches
// OnError like any other failure.
func (q *chanQueue) Enqueue(ctx context.Context, job brick.Job) error {
	select {
	case q.jobs <- job:
		return nil
	default:
		return brick.ErrJobQueueFull
	}
}

// work runs every job it receives with run (Jobs.Run). Each job gets a
// fresh context: Jobs.Run adds the job timeout and the cancel itself.
func (q *chanQueue) work(run func(ctx context.Context, job brick.Job) error) {
	for job := range q.jobs {
		_ = run(context.Background(), job) // failures already went to OnError
	}
}
```

`go run ./live-jobs -queue` serves the app through it, and the test runs the same transcript through both the pool and this queue. To cancel a job that is still waiting in your queue, implement the optional `brick.JobCanceller` as well:

```go
type JobCanceller interface {
	CancelJob(ctx context.Context, id string, may func(Job) error) error
}
```

`CancelJob` finds the job by id and, before cancelling, calls `may` with it when `may` is not nil; an error from `may` is returned unchanged and nothing is cancelled (that is how an action's ownership check runs on the queue's own record). A job the queue no longer has returns `brick.ErrJobNotFound`. The `Job` you store must keep its `Session` and `Component` fields, because the ownership check reads them.

## Try it

From the `examples` folder:

```sh
go run ./live-jobs -demo
```

The demo starts one job that finishes and one that is cancelled, over a real event stream. The job's work waits until the demo releases it, so the output never depends on timing:

<!-- snippet: examples/live-jobs/testdata/transcript.txt -->
```text
GET /reports/
  <div class="report"><button brick:click="start">Build report</button></div>
POST Report.start()
  <div class="report"><p>Building the 2026-09 report…</p><button brick:click="cancel">Cancel</button></div>
the job's work finishes
← event: refresh  data: reports
POST Report.$refresh()
  <div class="report"><p>done: the 2026-09 report has 3 rows</p><button brick:click="start">Build report</button></div>
POST Report.start()
  <div class="report"><p>Building the 2026-09 report…</p><button brick:click="cancel">Cancel</button></div>
POST Report.cancel()
  <div class="report"><p>Building the 2026-09 report…</p><button brick:click="cancel">Cancel</button></div>
← event: refresh  data: reports
POST Report.$refresh()
  <div class="report"><p>cancelled: stopped before it finished</p><button brick:click="start">Build report</button></div>
```

The `start` answer shows "Building…" at once; the result arrives only through the `reports` topic. After `cancel` the answer still shows "Building…", because a cancel applies once the action succeeded; the job then ends through `OnError` with `brick.ErrJobCancelled`, its topic is published, and the refresh shows the recorded outcome. `go test ./live-jobs/` checks this transcript. Run `go run ./live-jobs` and open http://localhost:8087/reports/ to click it; the served job takes 3 seconds.

## Pitfalls

- **Jobs must be on.** `brick.start_job` without `Options.Jobs` fails with `brick: background jobs are off (Options.Jobs is nil)`.
- **Only registered names run.** `brick.start_job("export")` fails with `unknown job "export" (registered in JobsOptions.Funcs: report)`.
- **Args are ids, not records.** Args are JSON values up to 16 KiB (`brick.MaxJobArgsBytes`); past it the action fails with `args are 20610 bytes, over 16384: a job takes ids, not records`.
- **A job starts only if the action succeeds.** It starts after the action's state is saved; a failed action starts nothing. A refused queue does not fail the action, which has already answered: it reaches `OnError` (and the engine's own error hooks).
- **A job with a topic needs push.** `JobsOptions.Push` nil means a job with a topic is refused when it starts.
- **An action cancels only its own jobs.** `brick.cancel_job` works on a job an action of the same component started in the same session; anything else is refused with `brick.ErrJobNotYours`, reported to the error hooks, and keeps running. A job that already ended is ignored. Go code (`Engine.CancelJob`, `Jobs.Cancel`) may cancel any job.
- **Without `JobCanceller`, waiting jobs cannot be cancelled.** A custom queue that does not implement it answers a cancel of a job it still holds with `brick.ErrJobCancelUnsupported`; a job already running in this process is cancelled by `Jobs` itself.
- **Every job has a time limit.** Its context ends after `JobsOptions.Timeout` (10 minutes by default) or when `Jobs.Close` cancels it; the timeout is reported to `OnError` like any failure.

## Related

- [Push and presence](push-and-presence.md), how the job's topic reaches the page.
- [Effects](effects.md): `start_job` and `cancel_job` are recorded like effects but never sent to the browser.
- Feature index: [Push, presence and jobs](../../reference/features.md#push-presence-and-jobs).
- The one-page guide: [Background jobs](../../guide.md#background-jobs--slow-work-that-reports-back).
- Example: [examples/live-jobs](../../../examples/live-jobs/).
