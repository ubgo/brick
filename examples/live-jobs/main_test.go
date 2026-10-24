package main

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/ubgo/brick"
	"github.com/ubgo/brick/bricktest"
	scriptstarlark "github.com/ubgo/brick/contrib/script-starlark"
)

// The flow over a real server and event stream, through the built-in pool
// and through the example's own JobQueue: both print
// testdata/transcript.txt, the output docs/guides/live/background-jobs.md
// shows under "Try it".
func TestTranscript(t *testing.T) {
	want, err := os.ReadFile("testdata/transcript.txt")
	if err != nil {
		t.Fatal(err)
	}
	for name, queue := range map[string]func() brick.JobQueue{
		"built-in pool": func() brick.JobQueue { return nil },
		"own queue":     func() brick.JobQueue { return newChanQueue(chanQueueSize) },
	} {
		t.Run(name, func(t *testing.T) {
			a, err := newApp("theme", queue())
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = a.jobs.Close(context.Background()) })
			var out strings.Builder
			if err := demo(&out, a); err != nil {
				t.Fatalf("%v\nso far:\n%s", err, out.String())
			}
			if out.String() != string(want) {
				t.Fatalf("transcript:\n%s\nwant:\n%s", out.String(), want)
			}
		})
	}
}

// The pitfalls the guide lists, each with the error a user sees.
func TestPitfalls(t *testing.T) {
	sc, err := scriptstarlark.New(scriptstarlark.Options{})
	if err != nil {
		t.Fatal(err)
	}
	push, err := brick.NewPush(brick.PushOptions{})
	if err != nil {
		t.Fatal(err)
	}
	jobs, err := brick.NewJobs(brick.JobsOptions{Push: push, Funcs: map[string]brick.JobFunc{
		"report": func(ctx context.Context, job brick.Job) error { return nil },
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = jobs.Close(context.Background()) })
	for _, tc := range []struct {
		name string
		jobs *brick.Jobs
		body string
		want string
	}{
		{"jobs off", nil, `brick.start_job("report")`, "background jobs are off (Options.Jobs is nil)"},
		{"unknown job", jobs, `brick.start_job("export")`, `unknown job "export" (registered in JobsOptions.Funcs: report)`},
		{"records in args", jobs, `brick.start_job("report", {"rows": ["x" * 100] * 200})`, "a job takes ids, not records"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			eng := bricktest.Engine(t, brick.Options{Scripting: sc, States: brick.NewMemoryStates(0, 0), Push: push, Jobs: tc.jobs}, bricktest.Theme(
				"components/Go.html", "---\nstate: {}\n---\n<brick:script>\ndef go(state, props):\n    "+tc.body+"\n    return state\n</brick:script>\n<button brick:click=\"go\">Go</button>",
				"content/index.html", `<Go />`,
			))
			live := bricktest.Live(t, eng, "content/index.html")
			_, err := live.Component("Go").TryCall("go")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}
