package scriptstarlark

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/ubgo/brick"
)

// brick.cancel_job cancels a job this component started once the action
// succeeded, and brick.clear_flash records the flash-clear effect
// .
func TestBrickCancelJobAndClearFlash(t *testing.T) {
	t.Parallel()
	sc, err := New(Options{})
	if err != nil {
		t.Fatal(err)
	}
	script := "<brick:script>\n" +
		"def go(state, props):\n" +
		"    return dict(state, job = brick.start_job(\"slow\"))\n\n" +
		"def stop(state, props):\n" +
		"    brick.cancel_job(state[\"job\"])\n" +
		"    return state\n\n" +
		"def dismiss(state, props):\n" +
		"    brick.clear_flash()\n" +
		"    brick.clear_flash(level = \"danger\")\n" +
		"    return state\n" +
		"</brick:script>\n"
	fsys := files("components/C.html", "---\nstate: {job: \"\"}\n---\n"+script+"<div>{{ .State.job }}</div>", "content/a.html", `<C />`)
	ended := make(chan error, 1)
	jobs, err := brick.NewJobs(brick.JobsOptions{
		Funcs:   map[string]brick.JobFunc{"slow": func(ctx context.Context, _ brick.Job) error { <-ctx.Done(); return ctx.Err() }},
		OnError: func(_ context.Context, _ brick.Job, err error) { ended <- err },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer jobs.Close(context.Background())
	e, err := brick.New(brick.Options{Scripting: sc, States: brick.NewMemoryStates(0, 0), Jobs: jobs, FS: fsys})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	page, err := e.Render(ctx, "content/a.html", nil)
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`data-brick-state="([^"]+)" data-brick-token="([0-9a-f]+)"`)
	m := re.FindStringSubmatch(string(page))
	res, err := e.RunAction(ctx, brick.ActionRequest{Handle: m[1], Token: m[2], Action: "go"})
	if err != nil {
		t.Fatal(err)
	}
	m = re.FindStringSubmatch(string(res.HTML))
	if res, err = e.RunAction(ctx, brick.ActionRequest{Handle: m[1], Token: m[2], Action: "stop"}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-ended:
		if !errors.Is(err, brick.ErrJobCancelled) {
			t.Fatalf("the job ended with %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the job was not cancelled")
	}
	m = re.FindStringSubmatch(string(res.HTML))
	res, err = e.RunAction(ctx, brick.ActionRequest{Handle: m[1], Token: m[2], Action: "dismiss"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Effects) != 2 || res.Effects[0].Kind != brick.EffectFlashClear || res.Effects[0].Level != "" || res.Effects[1].Level != brick.FlashDanger {
		t.Fatalf("effects = %+v", res.Effects)
	}
}
