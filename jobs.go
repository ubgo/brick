package brick

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"runtime/debug"
	"slices"
	"sync"
	"sync/atomic"
	"time"
)

// Background jobs. An action runs
// inside its POST under the script limits, so a slow piece of work (a
// report, an export, a third-party call) either blocks the button or is
// killed. Instead the action starts a job and answers at once; the job runs
// after the response, writes its result through the site's own data layer,
// and publishes a topic when it ends, so every component listening to that
// topic (v17 push) refreshes and reads the result through its own render.
//
// Who may cancel a job: any server code by id
// (Jobs.Cancel, Engine.CancelJob), and an action only a job an action of
// the same component started under the same session (Effects.CancelJob,
// brick.cancel_job). A job id is unguessable, but a script may still show
// it on the page or take it as an argument, so the id alone is never the
// permission: the job's recorded origin is.
//
// Who may start a job: only server code. An action records one with
// Effects.StartJob (brick.start_job in Starlark), Go code calls
// Engine.StartJob or Jobs.Start. The browser names an action, never a job,
// and a job's name must be one of JobsOptions.Funcs, a closed set of Go
// functions the host registered: a script never chooses the code that runs.
//
// What a job may touch: its Job (name, args, topic, origin) and the
// context Jobs gives it. Never the request's state handle, instance,
// session or *http.Request: the request is over when the job runs, and a
// job that saved into a component's state would race the user's next
// action. Its result reaches the page only through the data layer plus the
// topic, so push stays a refresh signal and every read stays a checked
// render.
const (
	// MaxJobArgsBytes caps a job's encoded args: a job takes ids, not
	// records, and a real queue carries the Job as a message.
	MaxJobArgsBytes = 16 << 10
	// DefaultJobWorkers is the in-process pool's concurrency when
	// JobsOptions.Workers is zero.
	DefaultJobWorkers = 4
	// DefaultJobQueueSize is how many started jobs the in-process pool
	// holds waiting for a worker when JobsOptions.QueueSize is zero; one
	// more is refused (ErrJobQueueFull) rather than block the action.
	DefaultJobQueueSize = 256
	// DefaultJobTimeout bounds one job when JobsOptions.Timeout is zero, so
	// a stuck job frees its worker and is reported.
	DefaultJobTimeout = 10 * time.Minute
	// jobIDBytes is a job id's random length (128 bits): unguessable, so
	// an id kept in state or a queue message names one job and no other.
	jobIDBytes = 16
)

// Job errors.
var (
	// ErrJobsOff is returned when a job is started without Options.Jobs.
	ErrJobsOff = errors.New("brick: background jobs are off (Options.Jobs is nil)")
	// ErrJobQueueFull is a job the in-process pool had no room for.
	ErrJobQueueFull = errors.New("brick: the job queue is full")
	// ErrJobsClosed is a job started, or still waiting, after Jobs.Close.
	ErrJobsClosed = errors.New("brick: jobs are closed")
	// ErrUnknownJob is a job name that is not in JobsOptions.Funcs.
	ErrUnknownJob = errors.New("brick: unknown job")
	// ErrJobCancelled is how a cancelled job ends: OnError is told and the
	// topic is published, like any job that did not succeed, and Run
	// returns it so a retrying queue can tell it from a failure.
	ErrJobCancelled = errors.New("brick: the job was cancelled")
	// ErrJobNotFound is a cancel for a job that is not waiting or running
	// (it already ended, or never existed).
	ErrJobNotFound = errors.New("brick: no such job is waiting or running")
	// ErrJobCancelUnsupported is a cancel for a job this process is not
	// running when JobsOptions.Queue does not implement JobCanceller: the
	// queue cannot cancel, and brick says so rather than pretend.
	ErrJobCancelUnsupported = errors.New("brick: the job queue cannot cancel jobs (it does not implement brick.JobCanceller)")
	// ErrJobNotYours is an action cancelling a job it may not: one Go code
	// started, or one another component or session started.
	ErrJobNotYours = errors.New("brick: this action may not cancel that job")
)

// Job is one started job. It is JSON so a real queue can carry it as a
// message and a worker in another process can run it with Jobs.Run.
type Job struct {
	// ID is made by brick (128 random bits, hex) when the job starts; an
	// action keeps it in its state to tell its job's record apart.
	ID string `json:"id"`
	// Name is a key of JobsOptions.Funcs.
	Name string `json:"name"`
	// Args are JSON values only, at most MaxJobArgsBytes encoded.
	Args map[string]any `json:"args,omitempty"`
	// Topic is published when the job ends, success or failure; "" for
	// none.
	Topic string `json:"topic,omitempty"`
	// Component and Action name the action that started the job, for logs;
	// both are "" for a job Go code started.
	Component string `json:"component,omitempty"`
	Action    string `json:"action,omitempty"`
	// Session is the hashed session id of the instance whose action
	// started the job ("" for Go code or no session): with Component it is
	// what an action's cancel is checked against, so a queue that
	// implements JobCanceller must carry it.
	Session string `json:"session,omitempty"`
	// Created is when the job started.
	Created time.Time `json:"created"`
}

// JobFunc does one job's work. ctx is not the request's: it ends at
// JobsOptions.Timeout or when Jobs.Close gives up waiting, and a JobFunc
// must return when it ends. It writes through the site's own data layer
// (closed over by the host) and returns an error to fail the job.
type JobFunc func(ctx context.Context, job Job) error

// JobQueue is where a started job goes: the seam a site plugs a real queue
// into (Redis, Hatchet, SQS). Enqueue hands the job over and returns; the
// queue's worker, in this process or another, runs it with Jobs.Run, which
// looks the name up again, reports a failure and publishes the topic. Nil
// in JobsOptions means the in-process pool.
type JobQueue interface {
	Enqueue(ctx context.Context, job Job) error
}

// JobCanceller is the optional upgrade a JobQueue implements to cancel a
// job it holds, detected by type assertion, so a queue that
// cannot cancel is never forced to fake it. CancelJob finds the job by id
// and, before cancelling, calls may with it when may is not nil: a non-nil
// error from may is returned unchanged and nothing is cancelled. This is
// how an action's ownership check runs on the record the queue holds, in
// one call. A job the queue no longer has returns ErrJobNotFound. A job
// already running in a worker should end with ErrJobCancelled (the queue's
// worker passes it to Run's context, or Jobs.Cancel in that process).
type JobCanceller interface {
	CancelJob(ctx context.Context, id string, may func(Job) error) error
}

// JobsOptions configures background jobs. Only Funcs is required.
type JobsOptions struct {
	// Funcs are the only jobs that can run, by name. A name is 1 to
	// MaxTopicLen characters from [A-Za-z0-9_.:/@-], like a topic, so it is
	// safe in a queue name and a log line.
	Funcs map[string]JobFunc
	// Push publishes a job's Topic when it ends. Nil means a job with a
	// topic is refused when it starts.
	Push *Push
	// Queue replaces the in-process pool; Workers and QueueSize are then
	// unused.
	Queue JobQueue
	// Workers is the in-process pool's concurrency. Zero means
	// DefaultJobWorkers.
	Workers int
	// QueueSize bounds the jobs waiting for a worker. Zero means
	// DefaultJobQueueSize.
	QueueSize int
	// Timeout bounds one job. Zero means DefaultJobTimeout.
	Timeout time.Duration
	// OnError is told of every job that failed: its JobFunc returned an
	// error, panicked or timed out, it could not be queued, it was still
	// waiting at Close, or its topic failed to publish. It runs before the
	// topic is published, so a host can record the failure where the
	// listening components will read it. Nil means slog.Error.
	OnError func(ctx context.Context, job Job, err error)
	// Clock replaces time.Now, for tests.
	Clock Clock
}

// Jobs runs background jobs. Like Push it is shared by every Engine an
// Options value makes, so a site's reload keeps the one running pool.
type Jobs struct {
	opts JobsOptions
	// base is the context every job runs under; Close cancels it.
	base    context.Context
	cancel  context.CancelFunc
	closed  atomic.Bool
	running atomic.Int64
	// pool is the in-process queue, nil when JobsOptions.Queue is set.
	pool *jobPool
	// mu guards live: the jobs waiting in the pool or running in this
	// process, by id, so one can be cancelled. An entry leaves when its job
	// ends, so live holds at most QueueSize waiting plus the running ones.
	mu   sync.Mutex
	live map[string]*jobEntry
}

// jobEntry is one cancellable job. cancel is set while it runs; cancelled
// is set by Cancel, so a waiting job is skipped when a worker takes it.
type jobEntry struct {
	job       Job
	cancel    context.CancelCauseFunc
	cancelled bool
}

// NewJobs checks the registry, fills the defaults and starts the
// in-process pool unless a Queue is given.
func NewJobs(opts JobsOptions) (*Jobs, error) {
	if len(opts.Funcs) == 0 {
		return nil, errors.New("brick: NewJobs: JobsOptions.Funcs names no job")
	}
	for name, fn := range opts.Funcs {
		if !ValidTopic(name) {
			return nil, fmt.Errorf("brick: NewJobs: %q is not a job name (1-%d characters from A-Z a-z 0-9 _ . : / @ -)", name, MaxTopicLen)
		}
		if fn == nil {
			return nil, fmt.Errorf("brick: NewJobs: job %q has no function", name)
		}
	}
	opts.Funcs = maps.Clone(opts.Funcs)
	if opts.Workers <= 0 {
		opts.Workers = DefaultJobWorkers
	}
	if opts.QueueSize <= 0 {
		opts.QueueSize = DefaultJobQueueSize
	}
	if opts.Timeout <= 0 {
		opts.Timeout = DefaultJobTimeout
	}
	if opts.Clock == nil {
		opts.Clock = time.Now
	}
	if opts.OnError == nil {
		opts.OnError = func(ctx context.Context, job Job, err error) {
			slog.ErrorContext(ctx, "brick: background job failed", "job", job.Name, "id", job.ID, "component", job.Component, "action", job.Action, "err", err)
		}
	}
	j := &Jobs{opts: opts, live: map[string]*jobEntry{}}
	j.base, j.cancel = context.WithCancel(context.Background())
	if opts.Queue == nil {
		j.pool = newJobPool(j, opts.Workers, opts.QueueSize)
	}
	return j, nil
}

// newJob validates a job and gives it an id. It is the one gate every
// start goes through: an action's record and Go's Start alike.
func (j *Jobs) newJob(name string, args map[string]any, topic string) (Job, error) {
	if _, ok := j.opts.Funcs[name]; !ok {
		return Job{}, fmt.Errorf("%w %q (registered in JobsOptions.Funcs: %s)", ErrUnknownJob, name, j.names())
	}
	if topic != "" {
		if !ValidTopic(topic) {
			return Job{}, fmt.Errorf("brick: job %s: %q is not a topic (1-%d characters from A-Z a-z 0-9 _ . : / @ -)", name, topic, MaxTopicLen)
		}
		if j.opts.Push == nil {
			return Job{}, fmt.Errorf("brick: job %s: a topic needs JobsOptions.Push", name)
		}
	}
	if args != nil {
		args, _ = deepCopy(args).(map[string]any)
		if err := checkJSONValue("args", args); err != nil {
			return Job{}, fmt.Errorf("brick: job %s: args must be JSON values: %w", name, err)
		}
		b, err := json.Marshal(args)
		if err != nil {
			return Job{}, fmt.Errorf("brick: job %s: args: %w", name, err)
		}
		if len(b) > MaxJobArgsBytes {
			return Job{}, fmt.Errorf("brick: job %s: args are %d bytes, over %d: a job takes ids, not records", name, len(b), MaxJobArgsBytes)
		}
	}
	id := make([]byte, jobIDBytes)
	if _, err := rand.Read(id); err != nil {
		return Job{}, fmt.Errorf("brick: job id: %w", err)
	}
	return Job{ID: hex.EncodeToString(id), Name: name, Args: args, Topic: topic, Created: j.opts.Clock()}, nil
}

// names lists the registered jobs for an error message.
func (j *Jobs) names() string {
	var s string
	for _, n := range sortedKeys(j.opts.Funcs) {
		if s != "" {
			s += ", "
		}
		s += n
	}
	return s
}

// Start validates and queues a job from Go code (a webhook, another job).
// A refused queue is returned, not reported: the caller is still there to
// handle it.
func (j *Jobs) Start(ctx context.Context, name string, args map[string]any, topic string) (Job, error) {
	job, err := j.newJob(name, args, topic)
	if err != nil {
		return Job{}, err
	}
	if err := j.enqueue(ctx, job); err != nil {
		return Job{}, err
	}
	return job, nil
}

// enqueue hands a validated job to the queue.
func (j *Jobs) enqueue(ctx context.Context, job Job) error {
	if j.closed.Load() {
		return ErrJobsClosed
	}
	if j.pool != nil {
		return j.pool.enqueue(job)
	}
	return j.opts.Queue.Enqueue(ctx, job)
}

// launch queues a job an action recorded. The action has already answered
// success, so a refusal cannot fail it: the job ends through fail, which
// reports it and publishes the topic, like any other failed job; the error
// is returned for the engine's own OnError hooks.
func (j *Jobs) launch(ctx context.Context, job Job) error {
	if err := j.enqueue(ctx, job); err != nil {
		err = fmt.Errorf("brick: job %s could not start: %w", job.Name, err)
		j.fail(context.WithoutCancel(ctx), job, err)
		return err
	}
	return nil
}

// Run runs one job to its end: the in-process pool calls it, and so does a
// real queue's worker (JobsOptions.Queue). The name is looked up again,
// because a message from a queue is data. A panic, an error and the
// timeout all end in OnError; the topic is published either way, so
// listeners re-read whatever the job (or OnError) recorded. The returned
// error is the job's, for a queue that retries.
func (j *Jobs) Run(ctx context.Context, job Job) error {
	fn, ok := j.opts.Funcs[job.Name]
	if !ok {
		err := fmt.Errorf("%w %q", ErrUnknownJob, job.Name)
		j.fail(ctx, job, err)
		return err
	}
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	if !j.track(job, cancel) {
		// Cancelled between the queue and the start: never run.
		j.fail(context.WithoutCancel(ctx), job, fmt.Errorf("brick: job %s: %w", job.Name, ErrJobCancelled))
		return ErrJobCancelled
	}
	defer j.untrack(job.ID)
	j.running.Add(1)
	defer j.running.Add(-1)
	// Close cancels every job through base, whatever context the queue's
	// worker passed in.
	ctx, stop := context.WithTimeout(ctx, j.opts.Timeout)
	defer stop()
	unhook := context.AfterFunc(j.base, stop)
	defer unhook()
	err := callJob(ctx, fn, job)
	if err == nil && ctx.Err() != nil {
		// A JobFunc that ignores ctx and returns nil after the deadline
		// still failed: its work may be half done.
		err = ctx.Err()
	}
	if err != nil && errors.Is(context.Cause(ctx), ErrJobCancelled) && !errors.Is(err, ErrJobCancelled) {
		// The JobFunc saw its context end; the reason is the cancel.
		if errors.Is(err, context.Canceled) {
			err = ErrJobCancelled
		} else {
			err = errors.Join(ErrJobCancelled, err)
		}
	}
	if err != nil {
		j.fail(context.WithoutCancel(ctx), job, fmt.Errorf("brick: job %s: %w", job.Name, err))
		return err
	}
	j.publish(context.WithoutCancel(ctx), job)
	return nil
}

// track records job as running with its cancel, and reports false when it
// was cancelled while it waited.
func (j *Jobs) track(job Job, cancel context.CancelCauseFunc) bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	if en, ok := j.live[job.ID]; ok {
		if en.cancelled {
			delete(j.live, job.ID)
			return false
		}
		en.cancel = cancel
		return true
	}
	j.live[job.ID] = &jobEntry{job: job, cancel: cancel}
	return true
}

// untrack forgets a job that ended.
func (j *Jobs) untrack(id string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	delete(j.live, id)
}

// Cancel cancels one job by id, from server code: no
// ownership check, because server code may cancel any job. A job waiting
// in the in-process pool never runs; a running one's context ends with
// ErrJobCancelled as its cause, and the JobFunc must return. Either way it
// ends through OnError with ErrJobCancelled and its topic is published.
// Cancelling a job already cancelled is nil. A job neither waiting nor
// running here is asked of JobsOptions.Queue when it implements
// JobCanceller; ErrJobCancelUnsupported when it does not, ErrJobNotFound
// with the in-process pool.
func (j *Jobs) Cancel(ctx context.Context, id string) error {
	return j.cancelJob(ctx, id, nil)
}

// cancelJob is Cancel with an optional ownership check, run on the job's
// record before anything is cancelled.
func (j *Jobs) cancelJob(ctx context.Context, id string, may func(Job) error) error {
	if !validJobID(id) {
		return fmt.Errorf("%w: %q is not a job id", ErrJobNotFound, id)
	}
	j.mu.Lock()
	en, ok := j.live[id]
	if ok {
		if may != nil {
			if err := may(en.job); err != nil {
				j.mu.Unlock()
				return err
			}
		}
		stop := en.cancel
		en.cancelled = true
		j.mu.Unlock()
		if stop != nil {
			stop(ErrJobCancelled)
		}
		return nil
	}
	j.mu.Unlock()
	if j.pool != nil {
		return fmt.Errorf("%w: %s", ErrJobNotFound, id)
	}
	qc, ok := j.opts.Queue.(JobCanceller)
	if !ok {
		return ErrJobCancelUnsupported
	}
	return qc.CancelJob(ctx, id, may)
}

// validJobID reports an id brick could have made: jobIDBytes of hex.
func validJobID(id string) bool {
	if len(id) != 2*jobIDBytes {
		return false
	}
	_, err := hex.DecodeString(id)
	return err == nil
}

// actionMayCancel is the ownership check for a cancel an action recorded:
// only a job an action of the same component started, under the same
// session. A job Go code started (no component) is never an action's to
// cancel.
func actionMayCancel(component, session string) func(Job) error {
	return func(job Job) error {
		if job.Component == "" {
			return fmt.Errorf("%w: job %s was started by Go code", ErrJobNotYours, job.ID)
		}
		if job.Component != component || job.Session != session {
			return fmt.Errorf("%w: job %s was started by another component or session", ErrJobNotYours, job.ID)
		}
		return nil
	}
}

// callJob runs fn, turning a panic into an error with its stack: a job's
// panic must not take the process down, and must not vanish.
func callJob(ctx context.Context, fn JobFunc, job Job) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v\n%s", r, debug.Stack())
		}
	}()
	return fn(ctx, job)
}

// fail reports a job that did not succeed, then publishes its topic.
func (j *Jobs) fail(ctx context.Context, job Job, err error) {
	func() {
		defer func() { _ = recover() }() // a reporter never takes a worker down
		j.opts.OnError(ctx, job, err)
	}()
	j.publish(ctx, job)
}

// publish sends a job's topic; a failure goes to OnError (never twice).
func (j *Jobs) publish(ctx context.Context, job Job) {
	if job.Topic == "" || j.opts.Push == nil {
		return
	}
	if err := j.opts.Push.Publish(ctx, job.Topic); err != nil {
		func() {
			defer func() { _ = recover() }()
			j.opts.OnError(ctx, job, fmt.Errorf("brick: job %s: publish %s: %w", job.Name, job.Topic, err))
		}()
	}
}

// Running is the number of jobs running now in this process.
func (j *Jobs) Running() int { return int(j.running.Load()) }

// Close stops accepting jobs and cancels the context of every running one,
// then waits for the in-process workers until ctx ends. Jobs still waiting
// for a worker end through OnError with ErrJobsClosed, never silently. A
// real queue (JobsOptions.Queue) is the host's to drain; Close still
// cancels the jobs Run is running here. Safe to call more than once.
func (j *Jobs) Close(ctx context.Context) error {
	if j.closed.Swap(true) {
		return nil
	}
	j.cancel()
	if j.pool == nil {
		return nil
	}
	return j.pool.close(ctx)
}

// jobPool is the default JobQueue: a bounded channel and a fixed number of
// workers, so a burst of actions cannot start unbounded goroutines.
type jobPool struct {
	jobs *Jobs
	ch   chan Job
	// mu guards closing ch against a concurrent enqueue.
	mu     sync.RWMutex
	closed bool
	wg     sync.WaitGroup
}

func newJobPool(j *Jobs, workers, size int) *jobPool {
	p := &jobPool{jobs: j, ch: make(chan Job, size)}
	p.wg.Add(workers)
	for range workers {
		go p.work()
	}
	return p
}

func (p *jobPool) work() {
	defer p.wg.Done()
	for job := range p.ch {
		if p.jobs.base.Err() != nil {
			// Closed while waiting: reported, not run.
			p.jobs.untrack(job.ID)
			p.jobs.fail(context.Background(), job, fmt.Errorf("brick: job %s: %w", job.Name, ErrJobsClosed))
			continue
		}
		_ = p.jobs.Run(p.jobs.base, job) // Run reported any failure
	}
}

// enqueue never blocks: a full pool refuses, so an action never stalls
// behind slow jobs.
func (p *jobPool) enqueue(job Job) error {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.closed {
		return ErrJobsClosed
	}
	// Recorded before it is sent, so a cancel right after Start finds it.
	p.jobs.mu.Lock()
	p.jobs.live[job.ID] = &jobEntry{job: job}
	p.jobs.mu.Unlock()
	select {
	case p.ch <- job:
		return nil
	default:
		p.jobs.untrack(job.ID)
		return ErrJobQueueFull
	}
}

// close ends the channel, so workers drain what is left (each reported as
// closed) and stop, and waits for them until ctx ends.
func (p *jobPool) close(ctx context.Context) error {
	p.mu.Lock()
	p.closed = true
	close(p.ch)
	p.mu.Unlock()
	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("brick: jobs close: %w (%d still running)", ctx.Err(), p.jobs.Running())
	}
}

// StartJob starts a background job from Go code: the same
// closed set and checks as an action's brick.start_job, but it is queued
// now. It needs Options.Jobs.
func (e *Engine) StartJob(ctx context.Context, name string, args map[string]any, topic string) (Job, error) {
	if e.opts.Jobs == nil {
		return Job{}, ErrJobsOff
	}
	return e.opts.Jobs.Start(ctx, name, args, topic)
}

// CancelJob cancels one background job from Go code; see
// Jobs.Cancel. It needs Options.Jobs.
func (e *Engine) CancelJob(ctx context.Context, id string) error {
	if e.opts.Jobs == nil {
		return ErrJobsOff
	}
	return e.opts.Jobs.Cancel(ctx, id)
}

// CancelJob records a cancel of the job id, applied once the action has
// succeeded and its state is saved, like StartJob: a failed
// action cancels nothing. Only a job an action of the same component
// started under the same session may be cancelled; any other is refused
// when the cancel is applied, and reported, because the action has
// already answered. A job that already ended is not an error: a cancel
// racing a job's end is normal. The id is checked for shape now.
func (e *Effects) CancelJob(id string) error {
	if e == nil {
		return errors.New("brick: a job can only be cancelled inside an action")
	}
	if e.eng == nil || e.eng.opts.Jobs == nil {
		return ErrJobsOff
	}
	if !validJobID(id) {
		return fmt.Errorf("brick: cancel_job: %q is not a job id (brick.start_job returns one)", id)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if !slices.Contains(e.cancels, id) {
		e.cancels = append(e.cancels, id)
	}
	return nil
}

// cancelledJobs returns the recorded cancels.
func (e *Effects) cancelledJobs() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return slices.Clone(e.cancels)
}

// applyJobs runs an action's recorded cancels, then starts its recorded
// jobs (so "cancel the old export, start a new one" works in one action).
// It runs only after the action succeeded and its state is saved; the
// answer is decided, so a refusal is reported, never a failed action.
func (e *Engine) applyJobs(ctx context.Context, eff *Effects, inst *Instance, action string) {
	jobs := e.opts.Jobs
	if jobs == nil {
		return
	}
	for _, id := range eff.cancelledJobs() {
		err := jobs.cancelJob(ctx, id, actionMayCancel(inst.Name, inst.Session))
		if err != nil && !errors.Is(err, ErrJobNotFound) {
			e.reportError(ctx, fmt.Errorf("brick: <%s>.%s: cancel job %s: %w", inst.Name, action, id, err))
		}
	}
	for _, job := range eff.startedJobs() {
		job.Component, job.Action, job.Session = inst.Name, action, inst.Session
		if err := jobs.launch(ctx, job); err != nil {
			e.reportError(ctx, fmt.Errorf("brick: <%s>.%s: %w", inst.Name, action, err))
		}
	}
}

// StartJob records a background job to start once the action has succeeded
// and its state is saved, exactly like Publish: a failed
// action starts nothing, and a job never runs against state the action did
// not keep. It returns the job's id at once, so the action can keep it in
// its state. The name must be in JobsOptions.Funcs; args are JSON values;
// topic, when set, is published when the job ends.
func (e *Effects) StartJob(name string, args map[string]any, topic string) (string, error) {
	if e == nil {
		return "", errors.New("brick: a job can only be started inside an action")
	}
	if e.eng == nil || e.eng.opts.Jobs == nil {
		return "", ErrJobsOff
	}
	job, err := e.eng.opts.Jobs.newJob(name, args, topic)
	if err != nil {
		return "", err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.jobs = append(e.jobs, job)
	return job.ID, nil
}

// startedJobs returns the recorded jobs.
func (e *Effects) startedJobs() []Job {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]Job(nil), e.jobs...)
}
