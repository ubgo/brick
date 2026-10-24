package brick

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// jobRecorder collects what OnError was told.
type jobRecorder struct {
	mu   sync.Mutex
	errs map[string]error
}

func (r *jobRecorder) onError(_ context.Context, job Job, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.errs == nil {
		r.errs = map[string]error{}
	}
	r.errs[job.ID] = err
}

func (r *jobRecorder) get(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.errs[id]
}

func (r *jobRecorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.errs)
}

func newJobs(t *testing.T, o JobsOptions) *Jobs {
	t.Helper()
	j, err := NewJobs(o)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = j.Close(context.Background()) })
	return j
}

// recv waits for one topic on ch.
func recv(t *testing.T, ch <-chan string) string {
	t.Helper()
	select {
	case got := <-ch:
		return got
	case <-time.After(5 * time.Second):
		t.Fatal("no topic published")
		return ""
	}
}

// An action's job starts only after the action succeeded, runs after the
// answer with a context the request does not own, and publishes its topic
// when it ends; a failed action starts nothing.
func TestActionStartsJobAfterSuccess(t *testing.T) {
	t.Parallel()
	b := NewMemoryBroadcaster()
	p := newPush(t, PushOptions{Broadcaster: b})
	release := make(chan struct{})
	var ran atomic.Int64
	var got atomic.Value
	jobs := newJobs(t, JobsOptions{Push: p, Funcs: map[string]JobFunc{"report": func(ctx context.Context, job Job) error {
		<-release
		if ctx.Err() != nil {
			return ctx.Err()
		}
		ran.Add(1)
		got.Store(job)
		return nil
	}}})
	var fail atomic.Bool
	var id atomic.Value
	mw := func(next ActionFunc) ActionFunc {
		return func(ctx context.Context, call *ActionCall) (*ActionResult, error) {
			jid, err := EffectsFrom(ctx).StartJob("report", map[string]any{"n": 3}, "reports")
			if err != nil {
				return nil, err
			}
			id.Store(jid)
			if fail.Load() {
				return nil, errors.New("boom")
			}
			return next(ctx, call)
		}
	}
	e := pushEngine(t, Options{Push: p, Jobs: jobs, ActionMiddleware: []ActionMiddleware{mw}})
	page, err := e.Render(context.Background(), "content/a.html", nil)
	if err != nil {
		t.Fatal(err)
	}
	m := stateRE.FindStringSubmatch(string(page))
	ch, cancel, err := b.Subscribe(context.Background(), []string{"reports"})
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	fail.Store(true)
	if _, err := e.RunAction(context.Background(), ActionRequest{Handle: m[1], Token: m[2], Action: "$refresh"}); err == nil {
		t.Fatal("the failing action succeeded")
	}
	fail.Store(false)
	// The request's context ends with the answer; the job must outlive it.
	reqCtx, endRequest := context.WithCancel(context.Background())
	if _, err := e.RunAction(reqCtx, ActionRequest{Handle: m[1], Token: m[2], Action: "$refresh"}); err != nil {
		t.Fatal(err)
	}
	endRequest()
	close(release)
	if topic := recv(t, ch); topic != "reports" {
		t.Fatalf("published %q", topic)
	}
	if n := ran.Load(); n != 1 {
		t.Fatalf("the job ran %d times; the failed action must start none", n)
	}
	job := got.Load().(Job)
	if job.ID != id.Load().(string) || job.Component != "Orders" || job.Action != "$refresh" || job.Args["n"] != 3 {
		t.Fatalf("job = %+v", job)
	}
}

// Only a registered job can start, with JSON args under the cap and a valid
// topic that has a Push; each refusal fails the action that asked.
func TestStartJobRefusals(t *testing.T) {
	t.Parallel()
	p := newPush(t, PushOptions{})
	noop := func(context.Context, Job) error { return nil }
	jobs := newJobs(t, JobsOptions{Push: p, Funcs: map[string]JobFunc{"ok": noop}})
	e := newEngine(t, Options{Push: p, Jobs: jobs})
	eff := e.newEffects("")
	cases := []struct {
		name, topic string
		args        map[string]any
		want        string
	}{
		{"nope", "", nil, "unknown job"},
		{"ok", "bad topic", nil, "not a topic"},
		{"ok", "", map[string]any{"f": func() {}}, "JSON values"},
		{"ok", "", map[string]any{"big": strings.Repeat("x", MaxJobArgsBytes)}, "ids, not records"},
	}
	for _, c := range cases {
		if _, err := eff.StartJob(c.name, c.args, c.topic); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("StartJob(%q, %q): %v, want %q", c.name, c.topic, err, c.want)
		}
	}
	if len(eff.startedJobs()) != 0 {
		t.Fatal("a refused job was recorded")
	}
	// A topic needs a Push to publish it.
	noPush := newJobs(t, JobsOptions{Funcs: map[string]JobFunc{"ok": noop}})
	if _, err := noPush.Start(context.Background(), "ok", nil, "t"); err == nil || !strings.Contains(err.Error(), "needs JobsOptions.Push") {
		t.Errorf("topic without push: %v", err)
	}
	// Without Options.Jobs, starting is an error, never silent.
	off := newEngine(t, Options{})
	if _, err := off.newEffects("").StartJob("ok", nil, ""); !errors.Is(err, ErrJobsOff) {
		t.Errorf("no jobs: %v", err)
	}
	if _, err := off.StartJob(context.Background(), "ok", nil, ""); !errors.Is(err, ErrJobsOff) {
		t.Errorf("engine no jobs: %v", err)
	}
	var nilEff *Effects
	if _, err := nilEff.StartJob("ok", nil, ""); err == nil {
		t.Error("a job started outside an action")
	}
	// The registry is checked when it is made.
	if _, err := NewJobs(JobsOptions{}); err == nil {
		t.Error("an empty registry was accepted")
	}
	if _, err := NewJobs(JobsOptions{Funcs: map[string]JobFunc{"a b": noop}}); err == nil {
		t.Error("a bad job name was accepted")
	}
	if _, err := NewJobs(JobsOptions{Funcs: map[string]JobFunc{"a": nil}}); err == nil {
		t.Error("a nil job was accepted")
	}
}

// Every way a job can fail reaches OnError, before its topic is published.
func TestJobFailuresReported(t *testing.T) {
	t.Parallel()
	b := NewMemoryBroadcaster()
	p := newPush(t, PushOptions{Broadcaster: b})
	rec := &jobRecorder{}
	var order []string
	var mu sync.Mutex
	jobs := newJobs(t, JobsOptions{Push: p, Timeout: 50 * time.Millisecond, OnError: func(ctx context.Context, job Job, err error) {
		mu.Lock()
		order = append(order, "onerror:"+job.Name)
		mu.Unlock()
		rec.onError(ctx, job, err)
	}, Funcs: map[string]JobFunc{
		"err":    func(context.Context, Job) error { return errors.New("source down") },
		"panic":  func(context.Context, Job) error { panic("kaboom") },
		"slow":   func(ctx context.Context, _ Job) error { <-ctx.Done(); return ctx.Err() },
		"deaf":   func(context.Context, Job) error { time.Sleep(100 * time.Millisecond); return nil },
		"thanks": func(context.Context, Job) error { return nil },
	}})
	ch, cancel, err := b.Subscribe(context.Background(), []string{"t"})
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	want := map[string]string{"err": "source down", "panic": "kaboom", "slow": "deadline exceeded", "deaf": "deadline exceeded"}
	for name, msg := range want {
		job, err := jobs.Start(context.Background(), name, nil, "t")
		if err != nil {
			t.Fatal(err)
		}
		if got := recv(t, ch); got != "t" {
			t.Fatalf("%s published %q", name, got)
		}
		if err := rec.get(job.ID); err == nil || !strings.Contains(err.Error(), msg) {
			t.Errorf("%s: OnError got %v, want %q", name, err, msg)
		}
	}
	mu.Lock()
	n := len(order)
	mu.Unlock()
	if n != len(want) {
		t.Fatalf("OnError ran %d times, want %d", n, len(want))
	}
	// A job that succeeds reports nothing.
	ok, err := jobs.Start(context.Background(), "thanks", nil, "t")
	if err != nil {
		t.Fatal(err)
	}
	recv(t, ch)
	if rec.get(ok.ID) != nil {
		t.Error("a successful job was reported")
	}
	// A message from a queue naming an unknown job is reported, not run.
	if err := jobs.Run(context.Background(), Job{ID: "x", Name: "nope"}); !errors.Is(err, ErrUnknownJob) || rec.get("x") == nil {
		t.Errorf("unknown job from a queue: %v", err)
	}
}

// A full pool refuses at once, and an action's job it refused ends through
// OnError and the engine's own error hooks, never silently.
func TestJobQueueFull(t *testing.T) {
	t.Parallel()
	block := make(chan struct{})
	rec := &jobRecorder{}
	jobs := newJobs(t, JobsOptions{Workers: 1, QueueSize: 1, OnError: rec.onError, Funcs: map[string]JobFunc{
		"wait": func(context.Context, Job) error { <-block; return nil },
	}})
	defer close(block)
	if _, err := jobs.Start(context.Background(), "wait", nil, ""); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the worker to take the first job", func() bool { return jobs.Running() == 1 })
	if _, err := jobs.Start(context.Background(), "wait", nil, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := jobs.Start(context.Background(), "wait", nil, ""); !errors.Is(err, ErrJobQueueFull) {
		t.Fatalf("over the queue: %v", err)
	}
	// From an action, the refusal cannot fail the answered action: it is
	// reported instead.
	var hooked atomic.Value
	mw := func(next ActionFunc) ActionFunc {
		return func(ctx context.Context, call *ActionCall) (*ActionResult, error) {
			if _, err := EffectsFrom(ctx).StartJob("wait", nil, ""); err != nil {
				return nil, err
			}
			return next(ctx, call)
		}
	}
	e := pushEngine(t, Options{Push: newPush(t, PushOptions{}), Jobs: jobs, ActionMiddleware: []ActionMiddleware{mw}, Hooks: []any{errorHook(func(err error) { hooked.Store(err) })}})
	page, err := e.Render(context.Background(), "content/a.html", nil)
	if err != nil {
		t.Fatal(err)
	}
	m := stateRE.FindStringSubmatch(string(page))
	if _, err := e.RunAction(context.Background(), ActionRequest{Handle: m[1], Token: m[2], Action: "$refresh"}); err != nil {
		t.Fatal(err)
	}
	if rec.count() != 1 {
		t.Fatalf("OnError saw %d refused jobs, want 1", rec.count())
	}
	if err, _ := hooked.Load().(error); err == nil || !errors.Is(err, ErrJobQueueFull) {
		t.Fatalf("engine hook: %v", err)
	}
}

// errorHook adapts a func to ErrorHook.
type errorHook func(error)

func (h errorHook) OnError(_ context.Context, err error) { h(err) }

// Close cancels running jobs, reports the ones still waiting, refuses new
// ones, and leaves no worker behind.
func TestJobsClose(t *testing.T) {
	before := runtime.NumGoroutine()
	rec := &jobRecorder{}
	started := make(chan struct{})
	jobs, err := NewJobs(JobsOptions{Workers: 1, OnError: rec.onError, Funcs: map[string]JobFunc{
		"long": func(ctx context.Context, _ Job) error {
			close(started)
			<-ctx.Done()
			return ctx.Err()
		},
		"later": func(context.Context, Job) error { return nil },
	}})
	if err != nil {
		t.Fatal(err)
	}
	running, err := jobs.Start(context.Background(), "long", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	<-started
	waiting, err := jobs.Start(context.Background(), "later", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := jobs.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if err := rec.get(running.ID); !errors.Is(err, context.Canceled) {
		t.Errorf("running job: %v", err)
	}
	if err := rec.get(waiting.ID); !errors.Is(err, ErrJobsClosed) {
		t.Errorf("waiting job: %v", err)
	}
	if _, err := jobs.Start(context.Background(), "later", nil, ""); !errors.Is(err, ErrJobsClosed) {
		t.Errorf("after close: %v", err)
	}
	if err := jobs.Close(ctx); err != nil {
		t.Errorf("second close: %v", err)
	}
	waitFor(t, "workers to stop", func() bool { return runtime.NumGoroutine() <= before+1 })
}

// fakeQueue is a real queue's shape: it carries the job as data and a
// worker runs it later with Jobs.Run.
type fakeQueue struct {
	mu   sync.Mutex
	jobs []Job
}

func (q *fakeQueue) Enqueue(_ context.Context, job Job) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.jobs = append(q.jobs, job)
	return nil
}

// A site's own queue receives the job and its worker runs it with Run,
// which publishes the topic like the pool does.
func TestCustomJobQueue(t *testing.T) {
	t.Parallel()
	b := NewMemoryBroadcaster()
	p := newPush(t, PushOptions{Broadcaster: b})
	q := &fakeQueue{}
	var args atomic.Value
	jobs := newJobs(t, JobsOptions{Push: p, Queue: q, Funcs: map[string]JobFunc{"r": func(_ context.Context, job Job) error {
		args.Store(job.Args)
		return nil
	}}})
	job, err := jobs.Start(context.Background(), "r", map[string]any{"id": "42"}, "t")
	if err != nil {
		t.Fatal(err)
	}
	if len(q.jobs) != 1 || q.jobs[0].ID != job.ID {
		t.Fatalf("queue got %+v", q.jobs)
	}
	ch, cancel, err := b.Subscribe(context.Background(), []string{"t"})
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	if err := jobs.Run(context.Background(), q.jobs[0]); err != nil {
		t.Fatal(err)
	}
	if recv(t, ch) != "t" || args.Load().(map[string]any)["id"] != "42" {
		t.Fatal("the worker's run did not publish or lost the args")
	}
}

// Close cancels a job a real queue's worker runs here, whatever context
// that worker passed to Run.
func TestJobsCloseCancelsQueueRun(t *testing.T) {
	t.Parallel()
	started := make(chan struct{})
	jobs, err := NewJobs(JobsOptions{Queue: &fakeQueue{}, OnError: func(context.Context, Job, error) {}, Funcs: map[string]JobFunc{"long": func(ctx context.Context, _ Job) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}}})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- jobs.Run(context.Background(), Job{ID: "q", Name: "long"}) }()
	<-started
	if err := jobs.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("run ended with %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not cancel the worker's job")
	}
}
