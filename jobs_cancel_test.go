package brick

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// waitErr waits for OnError to be told about id.
func waitErr(t *testing.T, rec *jobRecorder, id string) error {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if err := rec.get(id); err != nil {
			return err
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("job %s never ended through OnError", id)
	return nil
}

// A waiting job that is cancelled never runs; a running one sees its
// context end with ErrJobCancelled as the cause. Both end through OnError
// with ErrJobCancelled; a job that ended, and a malformed id, are
// ErrJobNotFound.
func TestJobsCancel(t *testing.T) {
	t.Parallel()
	rec := &jobRecorder{}
	started := make(chan string, 4)
	var causes sync.Map
	var ranB atomic.Bool
	j := newJobs(t, JobsOptions{Workers: 1, OnError: rec.onError, Funcs: map[string]JobFunc{
		"block": func(ctx context.Context, job Job) error {
			started <- job.ID
			<-ctx.Done()
			causes.Store(job.ID, context.Cause(ctx))
			return ctx.Err()
		},
		"b": func(context.Context, Job) error { ranB.Store(true); return nil },
	}})
	ctx := context.Background()
	a, err := j.Start(ctx, "block", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	<-started
	b, err := j.Start(ctx, "b", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := j.Cancel(ctx, b.ID); err != nil {
		t.Fatalf("cancel waiting: %v", err)
	}
	if err := j.Cancel(ctx, b.ID); err != nil {
		t.Fatalf("a second cancel of a cancelled job: %v", err)
	}
	if err := j.Cancel(ctx, a.ID); err != nil {
		t.Fatalf("cancel running: %v", err)
	}
	if err := waitErr(t, rec, a.ID); !errors.Is(err, ErrJobCancelled) {
		t.Fatalf("running job ended with %v", err)
	}
	if err := waitErr(t, rec, b.ID); !errors.Is(err, ErrJobCancelled) {
		t.Fatalf("waiting job ended with %v", err)
	}
	if c, _ := causes.Load(a.ID); !errors.Is(c.(error), ErrJobCancelled) {
		t.Fatalf("the running job's cause = %v", c)
	}
	if ranB.Load() {
		t.Fatal("a cancelled waiting job ran")
	}
	if err := j.Cancel(ctx, a.ID); !errors.Is(err, ErrJobNotFound) {
		t.Fatalf("cancel an ended job: %v", err)
	}
	if err := j.Cancel(ctx, "nope"); !errors.Is(err, ErrJobNotFound) {
		t.Fatalf("cancel a malformed id: %v", err)
	}
}

// plainQueue holds jobs and cannot cancel.
type plainQueue struct{}

func (plainQueue) Enqueue(context.Context, Job) error { return nil }

// cancelQueue holds jobs and cancels them, running may first.
type cancelQueue struct {
	mu   sync.Mutex
	jobs map[string]Job
	gone []string
}

func (q *cancelQueue) Enqueue(_ context.Context, job Job) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.jobs[job.ID] = job
	return nil
}

func (q *cancelQueue) CancelJob(_ context.Context, id string, may func(Job) error) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	job, ok := q.jobs[id]
	if !ok {
		return ErrJobNotFound
	}
	if may != nil {
		if err := may(job); err != nil {
			return err
		}
	}
	delete(q.jobs, id)
	q.gone = append(q.gone, id)
	return nil
}

// Across the JobQueue seam: a queue without JobCanceller says it cannot
// cancel rather than pretend; one with it is asked, with the ownership
// check, and a job Run is running in this process is cancelled here
// whatever the queue can do.
func TestJobsCancelQueueSeam(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	funcs := map[string]JobFunc{"x": func(ctx context.Context, _ Job) error { <-ctx.Done(); return ctx.Err() }}
	plain := newJobs(t, JobsOptions{Queue: plainQueue{}, Funcs: funcs, OnError: (&jobRecorder{}).onError})
	job, err := plain.Start(ctx, "x", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := plain.Cancel(ctx, job.ID); !errors.Is(err, ErrJobCancelUnsupported) {
		t.Fatalf("a queue without JobCanceller: %v", err)
	}
	// Run in this process: cancellable here even though the queue cannot.
	done := make(chan error, 1)
	go func() { done <- plain.Run(ctx, job) }()
	deadline := time.Now().Add(5 * time.Second)
	for plain.Running() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if err := plain.Cancel(ctx, job.ID); err != nil {
		t.Fatalf("cancel a job running here: %v", err)
	}
	if err := <-done; !errors.Is(err, ErrJobCancelled) {
		t.Fatalf("Run returned %v; a retrying queue needs ErrJobCancelled", err)
	}

	q := &cancelQueue{jobs: map[string]Job{}}
	cj := newJobs(t, JobsOptions{Queue: q, Funcs: funcs})
	job, err = cj.Start(ctx, "x", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	// An action's check runs on the queue's record: a Go-started job is
	// not an action's to cancel.
	if err := cj.cancelJob(ctx, job.ID, actionMayCancel("Orders", "")); !errors.Is(err, ErrJobNotYours) {
		t.Fatalf("action cancel of a Go job through the queue: %v", err)
	}
	if err := cj.Cancel(ctx, job.ID); err != nil {
		t.Fatal(err)
	}
	if len(q.gone) != 1 || q.gone[0] != job.ID {
		t.Fatalf("the queue cancelled %v", q.gone)
	}
}

// cancelTheme is two components that share the cancel middleware.
func cancelTheme() Options {
	comp := "---\nstate: {n: 0}\n---\n<div>{{ .State.n }}</div>"
	return Options{States: NewMemoryStates(0, 0), FS: theme("components/Orders.html", comp, "components/Other.html", comp,
		"content/a.html", `<Orders />`, "content/b.html", `<Other />`)}
}

// An action may cancel only a job an action of the same component started
// under the same session; Go-started jobs, other components' and other
// sessions' are refused and reported, and the job keeps running. A failed
// action cancels nothing. Server code cancels any of them.
func TestActionCancelJobGuard(t *testing.T) {
	t.Parallel()
	rec := &jobRecorder{}
	started := make(chan string, 8)
	jobs := newJobs(t, JobsOptions{Workers: 8, OnError: rec.onError, Funcs: map[string]JobFunc{"block": func(ctx context.Context, job Job) error {
		started <- job.ID
		<-ctx.Done()
		return ctx.Err()
	}}})
	// The middleware starts a job for "$refresh" with no arg, and cancels
	// the id in Args[0] otherwise; "fail" makes the action fail after.
	var fail atomic.Bool
	var startedID atomic.Value
	mw := func(next ActionFunc) ActionFunc {
		return func(ctx context.Context, call *ActionCall) (*ActionResult, error) {
			eff := EffectsFrom(ctx)
			if len(call.Args) == 0 {
				id, err := eff.StartJob("block", nil, "")
				if err != nil {
					return nil, err
				}
				startedID.Store(id)
			} else if err := eff.CancelJob(call.Args[0].(string)); err != nil {
				return nil, err
			}
			if fail.Load() {
				return nil, errors.New("boom")
			}
			call.Args = nil
			return next(ctx, call)
		}
	}
	var reported []string
	var rmu sync.Mutex
	o := cancelTheme()
	o.Jobs = jobs
	o.ActionMiddleware = []ActionMiddleware{mw}
	o.Hooks = []any{onErrorFunc(func(_ context.Context, err error) {
		rmu.Lock()
		defer rmu.Unlock()
		reported = append(reported, err.Error())
	})}
	e := newEngine(t, o)
	ctx := context.Background()
	render := func(path, session string) ActionRequest {
		page, err := e.Render(ctx, path, nil, WithSession(session))
		if err != nil {
			t.Fatal(err)
		}
		h, tok := handleOf(t, string(page))
		return ActionRequest{Handle: h, Token: tok, Action: "$refresh"}
	}
	run := func(req ActionRequest, session string, args ...any) {
		t.Helper()
		req.Args = args
		_, err := e.RunAction(ctx, req, WithSession(session))
		if err != nil && !fail.Load() {
			t.Fatal(err)
		}
	}
	mine := render("content/a.html", "alice")
	run(mine, "alice")
	id := startedID.Load().(string)
	<-started

	other := render("content/b.html", "alice")
	run(other, "alice", id) // another component
	bob := render("content/a.html", "bob")
	run(bob, "bob", id) // same component, another session
	goJob, err := e.StartJob(ctx, "block", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	<-started
	run(render("content/a.html", "alice"), "alice", goJob.ID) // Go-started
	fail.Store(true)
	run(render("content/a.html", "alice"), "alice", id) // a failed action
	fail.Store(false)
	time.Sleep(50 * time.Millisecond)
	if rec.get(id) != nil || rec.get(goJob.ID) != nil {
		t.Fatalf("a refused cancel stopped a job: %v / %v", rec.get(id), rec.get(goJob.ID))
	}
	rmu.Lock()
	got := strings.Join(reported, "\n")
	rmu.Unlock()
	if n := strings.Count(got, ErrJobNotYours.Error()); n != 3 {
		t.Fatalf("%d refusals reported, want 3:\n%s", n, got)
	}
	if !strings.Contains(got, "started by Go code") {
		t.Fatalf("the Go-started refusal is not named:\n%s", got)
	}

	run(render("content/a.html", "alice"), "alice", id) // the owner
	if err := waitErr(t, rec, id); !errors.Is(err, ErrJobCancelled) {
		t.Fatalf("owner cancel: %v", err)
	}
	if err := e.CancelJob(ctx, goJob.ID); err != nil {
		t.Fatalf("server code cancel: %v", err)
	}
	if err := waitErr(t, rec, goJob.ID); !errors.Is(err, ErrJobCancelled) {
		t.Fatalf("Go cancel: %v", err)
	}
}

// onErrorFunc adapts a function to the OnError hook.
type onErrorFunc func(context.Context, error)

func (f onErrorFunc) OnError(ctx context.Context, err error) { f(ctx, err) }

// The id is checked for shape when the action records the cancel, and a
// cancel needs Options.Jobs.
func TestEffectsCancelJobChecks(t *testing.T) {
	t.Parallel()
	var nilEff *Effects
	if err := nilEff.CancelJob("x"); err == nil {
		t.Fatal("cancel outside an action succeeded")
	}
	e := newEngine(t, cancelTheme())
	if err := e.newEffects("").CancelJob(strings.Repeat("a", 32)); !errors.Is(err, ErrJobsOff) {
		t.Fatalf("no jobs: %v", err)
	}
	if err := e.CancelJob(context.Background(), strings.Repeat("a", 32)); !errors.Is(err, ErrJobsOff) {
		t.Fatalf("engine cancel with no jobs: %v", err)
	}
	o := cancelTheme()
	o.Jobs = newJobs(t, JobsOptions{Funcs: map[string]JobFunc{"x": func(context.Context, Job) error { return nil }}})
	e = newEngine(t, o)
	if err := e.newEffects("").CancelJob("../etc"); err == nil || !strings.Contains(err.Error(), "not a job id") {
		t.Fatalf("malformed id: %v", err)
	}
}
