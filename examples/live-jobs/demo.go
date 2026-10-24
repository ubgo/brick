package main

import (
	"context"
	"fmt"
	"io"
	"net/http/httptest"
	"regexp"
	"time"

	liveclient "github.com/ubgo/brick/examples/live-client"
)

// eventWait bounds every wait for a pushed event.
const eventWait = 5 * time.Second

// reportRE finds the Report's markup in a page or an answer.
var reportRE = regexp.MustCompile(`(?s)<div[^>]*class="report".*?</div>`)

// demo starts one job that finishes and one that is cancelled, over a
// real server and a real event stream. The work waits until the demo
// releases it, so the transcript never depends on timing.
func demo(w io.Writer, a *app) error {
	release := make(chan struct{})
	a.work = func(ctx context.Context, month string) error {
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return context.Cause(ctx)
		}
	}
	srv := httptest.NewServer(a.handler())
	defer srv.Close()
	c := liveclient.New(srv.URL)
	tr := liveclient.Transcript{W: w, C: c}
	page, err := c.Get("/reports/")
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "GET /reports/\n  %s\n", liveclient.Tidy(reportRE.FindString(page)))
	root, err := liveclient.Find(page, "Report")
	if err != nil {
		return err
	}
	stream, err := c.Listen(page)
	if err != nil {
		return err
	}
	defer stream.Close()

	show := func(ans *liveclient.Answer) {
		fmt.Fprintf(w, "  %s\n", liveclient.Tidy(reportRE.FindString(ans.HTML)))
	}
	// refresh waits for the job's topic and re-renders, as the runtime does.
	refresh := func() error {
		topic, err := stream.Next(eventWait)
		if err != nil {
			return err
		}
		fmt.Fprintf(w, "← event: refresh  data: %s\n", topic)
		ans, err := tr.Act(root, "$refresh")
		if err != nil {
			return err
		}
		show(ans)
		return nil
	}

	ans, err := tr.Act(root, "start")
	if err != nil {
		return err
	}
	show(ans)
	fmt.Fprintln(w, "the job's work finishes")
	release <- struct{}{}
	if err := refresh(); err != nil {
		return err
	}

	if ans, err = tr.Act(root, "start"); err != nil {
		return err
	}
	show(ans)
	if ans, err = tr.Act(root, "cancel"); err != nil {
		return err
	}
	show(ans)
	return refresh()
}
