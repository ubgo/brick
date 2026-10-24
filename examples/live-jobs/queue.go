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
