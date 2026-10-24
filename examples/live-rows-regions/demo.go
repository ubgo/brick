package main

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"

	liveclient "github.com/ubgo/brick/examples/live-client"
)

// feedRE finds the feed list in a render, the region brick.region applies.
var feedRE = regexp.MustCompile(`(?s)<ul brick:ref="feed".*?</ul>`)

// demo drives the page over real HTTP and prints each action with what
// its answer carried; the test compares the whole transcript.
func demo(w io.Writer, h http.Handler) error {
	srv := httptest.NewServer(h)
	defer srv.Close()
	c := liveclient.New(srv.URL)
	html, err := c.Get("/")
	if err != nil {
		return err
	}
	roots := map[string]*liveclient.Root{}
	for _, name := range []string{"Chat", "Feed", "Login"} {
		if roots[name], err = liveclient.Find(html, name); err != nil {
			return err
		}
	}
	fmt.Fprintf(w, "GET /\n  feed: %s\n", liveclient.Tidy(feedRE.FindString(html)))
	tr := liveclient.Transcript{W: w, C: c}
	chat := roots["Chat"]
	for _, text := range []string{"Hello", "How are you?", "Still there?", "Bye"} {
		if _, err := tr.Act(chat, "send", text); err != nil {
			return err
		}
	}
	if _, err := tr.Act(chat, "remove", 2); err != nil {
		return err
	}
	if _, err := tr.Act(chat, "clear"); err != nil {
		return err
	}
	ans, err := tr.ActStream(chat, "think")
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "  html: %s\n", liveclient.Tidy(regexp.MustCompile(`<p brick:ref="answer">.*?</p>`).FindString(ans.HTML)))
	if ans, err = tr.Act(roots["Feed"], "more"); err != nil {
		return err
	}
	fmt.Fprintf(w, "  html: %s\n", liveclient.Tidy(feedRE.FindString(ans.HTML)))
	_, err = tr.Act(roots["Login"], "sign_in")
	return err
}
