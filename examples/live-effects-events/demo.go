package main

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"

	liveclient "github.com/ubgo/brick/examples/live-client"
)

// badgeRE finds the Badge's markup on a page or in an answer.
var badgeRE = regexp.MustCompile(`(?s)<p[^>]*class="badge"[^>]*>.*?</p>`)

// demo runs the app the way a browser would, over real HTTP, and writes
// each request with the effects its answer carried. The test compares the
// whole transcript, so the guide's output is the program's output.
func demo(w io.Writer, h http.Handler) error {
	srv := httptest.NewServer(h)
	defer srv.Close()
	c := liveclient.New(srv.URL)
	page, err := c.Get("/")
	if err != nil {
		return err
	}
	inbox, err := liveclient.Find(page, "Inbox")
	if err != nil {
		return err
	}
	badge, err := liveclient.Find(page, "Badge")
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "GET /\n  %s, %d messages\n", liveclient.Tidy(badgeRE.FindString(page)), strings.Count(page, "<li>"))

	tr := liveclient.Transcript{W: w, C: c}
	act := tr.Act

	ans, err := act(inbox, "archive", 2)
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "  html: %d messages\n", strings.Count(ans.HTML, "<li>"))
	// The runtime delivers the dispatch: Badge lists message-archived
	// under brick: {on:}, so it runs Badge's count action with the event's
	// data as the last argument.
	fmt.Fprintf(w, "the runtime delivers %s to Badge:\n", eventArchived)
	ans, err = act(badge, "count", map[string]any{"id": 2})
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "  html: %s\n", liveclient.Tidy(ans.HTML))
	steps := []struct {
		action string
		args   []any
	}{
		{"archive", []any{2}},
		{"read", []any{3}},
		{"unread_only", nil},
		{"export", nil},
		{"dismiss", nil},
		{"sign_out", nil},
	}
	for _, s := range steps {
		if _, err := act(inbox, s.action, s.args...); err != nil {
			return err
		}
	}
	return nil
}
