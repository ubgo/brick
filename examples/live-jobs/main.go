// Command live-jobs shows a background job: a button starts a slow
// report, the action answers at once, and the page refreshes by itself
// when the job ends, because the job publishes a topic the component
// listens to. A second action cancels a running job.
//
//	go run ./live-jobs             serve on :8087, then open /reports/
//	go run ./live-jobs -demo       print the flow from Go
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/ubgo/brick"
	assetshttp "github.com/ubgo/brick/contrib/assets-http"
	scriptstarlark "github.com/ubgo/brick/contrib/script-starlark"
)

const (
	// jobReport is the one job scripts may start: brick.start_job names a
	// key of JobsOptions.Funcs, so a script never chooses the code that
	// runs.
	jobReport = "report"
	// The statuses the Report template shows. A job id the store has no
	// record of yet is running: the record is written when the job ends.
	statusRunning   = "running"
	statusDone      = "done"
	statusFailed    = "failed"
	statusCancelled = "cancelled"
	// slowWork stands in for a monthly report's query when the app is
	// served; the demo and the test replace it with a gate they open.
	slowWork = 3 * time.Second
)

// report is one job's outcome as the template reads it.
type report struct {
	Status string
	Detail string
}

// reports is the app's own data layer for job outcomes. A job never sees
// the component's state; it writes here, and the component reads here on
// its next render.
type reports struct {
	mu   sync.Mutex
	byID map[string]report
}

// get is the template function `report`: "" for no job yet, running for a
// job that has not ended, else what the job recorded.
// ds:def id=reports-get-69zu49hj
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

// set records the latest report for a job id, replacing the running placeholder
// that get hands out until the job finishes.
func (rs *reports) set(id string, r report) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	rs.byID[id] = r
}

// app is the engine, its jobs, and the data both of them read.
type app struct {
	eng     *brick.Engine
	jobs    *brick.Jobs
	reports *reports
	// work is the slow part of a report. It must return when ctx ends:
	// that is how a cancel or the job timeout stops it.
	work func(ctx context.Context, month string) error
}

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

// runReport is the job. It gets the job's args (JSON values: ids, not
// records) and its own context, never the request's.
// ds:def id=app-runreport-kunttkhe
func (a *app) runReport(ctx context.Context, job brick.Job) error {
	month, _ := job.Args["month"].(string)
	if err := a.work(ctx, month); err != nil {
		return err
	}
	a.reports.set(job.ID, report{Status: statusDone, Detail: "the " + month + " report has 3 rows"})
	return nil
}

// sleepWork waits slowWork, or less when ctx ends first.
func sleepWork(ctx context.Context, month string) error {
	select {
	case <-time.After(slowWork):
		return nil
	case <-ctx.Done():
		return context.Cause(ctx)
	}
}

// handler routes the page and the engine's action, asset and events
// routes.
func (a *app) handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle(brick.DefaultAssetBase, assetshttp.Handler(brick.DefaultAssetBase, func() assetshttp.Source { return a.eng }))
	mux.Handle("POST "+brick.DefaultAssetBase+brick.ActionEndpoint, a.eng.ActionHandler())
	mux.Handle("GET "+brick.DefaultAssetBase+brick.EventsEndpoint, a.eng.EventsHandler())
	mux.HandleFunc("GET /reports/{$}", func(w http.ResponseWriter, r *http.Request) {
		html, err := a.eng.Render(r.Context(), "content/reports/index.html", nil)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(html)
	})
	return mux
}

// ds:def id=main-8pguu8zt
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
