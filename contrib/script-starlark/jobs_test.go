package scriptstarlark

import (
	"context"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/ubgo/brick"
)

// brick.start_job returns the job's id, which the action can keep in its
// state; the job runs with the args the script gave once the action
// succeeded, and an unknown name fails the action.
func TestBrickStartJob(t *testing.T) {
	t.Parallel()
	sc, err := New(Options{})
	if err != nil {
		t.Fatal(err)
	}
	script := "<brick:script>\n" +
		"def go(state, props):\n" +
		"    return dict(state, job = brick.start_job(\"report\", {\"n\": 2}, topic = \"reports\"))\n\n" +
		"def unknown(state, props):\n" +
		"    brick.start_job(\"rm_rf\")\n" +
		"    return state\n" +
		"</brick:script>\n"
	fsys := files("components/C.html", "---\nstate: {job: \"\"}\n---\n"+script+"<div id=\"j\">{{ .State.job }}</div>", "content/a.html", `<C />`)
	push, err := brick.NewPush(brick.PushOptions{})
	if err != nil {
		t.Fatal(err)
	}
	ran := make(chan brick.Job, 1)
	jobs, err := brick.NewJobs(brick.JobsOptions{Push: push, Funcs: map[string]brick.JobFunc{"report": func(_ context.Context, job brick.Job) error {
		ran <- job
		return nil
	}}})
	if err != nil {
		t.Fatal(err)
	}
	defer jobs.Close(context.Background())
	e, err := brick.New(brick.Options{Scripting: sc, States: brick.NewMemoryStates(0, 0), Push: push, Jobs: jobs, FS: fsys})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	page, err := e.Render(ctx, "content/a.html", nil)
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`data-brick-state="([^"]+)" data-brick-token="([0-9a-f]+)"`).FindStringSubmatch(string(page))
	if _, err := e.RunAction(ctx, brick.ActionRequest{Handle: m[1], Token: m[2], Action: "unknown"}); err == nil || !strings.Contains(err.Error(), "unknown job") {
		t.Fatalf("unknown job: %v", err)
	}
	res, err := e.RunAction(ctx, brick.ActionRequest{Handle: m[1], Token: m[2], Action: "go"})
	if err != nil {
		t.Fatal(err)
	}
	var job brick.Job
	select {
	case job = <-ran:
	case <-time.After(5 * time.Second):
		t.Fatal("the job never ran")
	}
	if !strings.Contains(string(res.HTML), `id="j">`+job.ID+`</div>`) {
		t.Fatalf("the state does not hold the job id %s: %s", job.ID, res.HTML)
	}
	if job.Args["n"] != int64(2) && job.Args["n"] != 2 {
		t.Fatalf("args = %#v", job.Args)
	}
}
