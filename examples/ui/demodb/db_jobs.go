package demodb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ubgo/brick"
)

// The background job demo: an
// action records a row and starts demo.report; the job works for a while,
// writes its result into the row and publishes demo:jobs, so the listening
// component refreshes and reads the row again.
const (
	// JobReport is the demo's one job name.
	JobReport = "demo.report"
	// JobsTopic is what the demo job publishes when it ends.
	JobsTopic = "demo:jobs"
	// jobWork is how long the demo job "works": long enough to see the
	// running state, short enough for the browser check.
	jobWork = 1500 * time.Millisecond
	// Job row statuses, as the component shows them.
	jobRunning = "running"
	jobDone    = "done"
	jobFailed  = "failed"
)

// jobsSchema is the demo's job table. Created here, not in seed(), so the
// whole demo lives in this file; "if not exists" because Jobs runs once per
// database.
const jobsSchema = `
create table if not exists demo_jobs (
  id text primary key,
  status text not null,
  result text not null default ''
);
`

// errDemoFail is the failure the demo's "fail" button asks for.
var errDemoFail = errors.New("the report source refused the request (the demo's failure path)")

// Jobs creates the job table and returns the demo's job registry and the
// OnError hook that records a failure where the component reads it.
func Jobs(ctx context.Context, db *sql.DB) (map[string]brick.JobFunc, func(context.Context, brick.Job, error), error) {
	if _, err := db.ExecContext(ctx, jobsSchema); err != nil {
		return nil, nil, fmt.Errorf("jobs schema: %w", err)
	}
	report := func(ctx context.Context, job brick.Job) error {
		select {
		case <-time.After(jobWork):
		case <-ctx.Done():
			return ctx.Err()
		}
		if fail, _ := job.Args["fail"].(bool); fail {
			return errDemoFail
		}
		var n int
		if err := db.QueryRowContext(ctx, "select count(*) from push_notes").Scan(&n); err != nil {
			return err
		}
		_, err := db.ExecContext(ctx, "update demo_jobs set status = ?, result = ? where id = ?", jobDone, fmt.Sprintf("%d notes on the live board", n), job.ID)
		return err
	}
	onError := func(ctx context.Context, job brick.Job, err error) {
		// Recorded before brick publishes the topic, so the refresh shows it.
		if _, dbErr := db.ExecContext(ctx, "update demo_jobs set status = ?, result = ? where id = ?", jobFailed, err.Error(), job.ID); dbErr != nil {
			fmt.Printf("demo job %s: %v (and recording it: %v)\n", job.ID, err, dbErr)
		}
	}
	return map[string]brick.JobFunc{JobReport: report}, onError, nil
}
