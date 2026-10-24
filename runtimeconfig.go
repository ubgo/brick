package brick

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/url"
	"strings"
	"time"
)

// RuntimeConfig tunes the browser runtime from Go, so a host changes the page-wide debounce, the poll
// floor, where actions are posted and how refused actions are retried
// without editing runtime.js. The engine writes the fields that are set as
// <meta name="brick-config" content='{…}'> just before the runtime script;
// the zero value writes no meta, so a page is byte-for-byte what it was
// before this option existed. Every zero field means its default.
type RuntimeConfig struct {
	// Debounce is how long brick:model waits after the last keystroke when
	// the attribute names no .debounce (DefaultDebounce), for apps whose
	// inputs search a slow API.
	Debounce time.Duration
	// PollMin floors every brick:poll interval (DefaultPollMin). It may be
	// lowered for a dashboard, but never below MinPollMin.
	PollMin time.Duration
	// ActionURL is where actions are posted, for a static export on a CDN
	// whose actions run on another server (which must then allow CORS for
	// the page's origin, the Brick-* request headers and credentials). Its
	// path must end in "/action": uploads and lazy islands are posted
	// beside it (".../upload", ".../island"). Empty means beside the
	// runtime script, i.e. on the server that served the page's assets.
	ActionURL string
	// Retries is how many times the runtime sends an action again after a
	// 503 or 429, waiting for the answer's Retry-After each time, before
	// it reports "action-error" (DefaultRetries). NoRetry turns retrying
	// off. Uploads' bodies are never sent again.
	Retries int
	// RetryMaxWait is the longest Retry-After the runtime waits for
	// (DefaultRetryMaxWait); an answer asking for longer is reported as an
	// error at once instead of freezing the component.
	RetryMaxWait time.Duration
}

const (
	// DefaultDebounce batches a typed word into one request while still
	// feeling live.
	DefaultDebounce = 250 * time.Millisecond
	// DefaultPollMin stops a typo like brick:poll.10ms from hammering the
	// server.
	DefaultPollMin = time.Second
	// MinPollMin is the safety floor no configuration removes: four
	// requests a second per polling element is already a lot of load.
	MinPollMin = 250 * time.Millisecond
	// DefaultRetries follows gotcha 14: one retry rides out a store or
	// limiter blip, and more would turn an outage into a request storm.
	DefaultRetries = 1
	// NoRetry is the RuntimeConfig.Retries value that turns retrying off
	// (zero already means DefaultRetries).
	NoRetry = -1
	// DefaultRetryMaxWait is as long as a user reasonably watches a
	// loading indicator before an error is more useful.
	DefaultRetryMaxWait = 5 * time.Second
)

// runtimeConfigJSON is the meta's content: only the fields the host set,
// times in whole milliseconds.
type runtimeConfigJSON struct {
	Debounce     int64  `json:"debounce,omitempty"`
	PollMin      int64  `json:"pollMin,omitempty"`
	ActionURL    string `json:"actionURL,omitempty"`
	Retries      *int   `json:"retries,omitempty"`
	RetryMaxWait int64  `json:"retryMaxWait,omitempty"`
}

// check reports the first field New must refuse: a configuration the
// runtime would silently ignore is a mistake the host should hear about.
func (c RuntimeConfig) check() error {
	switch {
	case c.Debounce < 0:
		return fmt.Errorf("brick: Options.Runtime.Debounce %s is negative", c.Debounce)
	case c.PollMin < 0 || (c.PollMin > 0 && c.PollMin < MinPollMin):
		return fmt.Errorf("brick: Options.Runtime.PollMin %s is below the %s floor", c.PollMin, MinPollMin)
	case c.Retries < NoRetry:
		return fmt.Errorf("brick: Options.Runtime.Retries %d: use a count, or NoRetry to turn retrying off", c.Retries)
	case c.RetryMaxWait < 0:
		return fmt.Errorf("brick: Options.Runtime.RetryMaxWait %s is negative", c.RetryMaxWait)
	}
	if c.ActionURL != "" {
		u, err := url.Parse(c.ActionURL)
		if err != nil || strings.ContainsAny(c.ActionURL, " \t\r\n") {
			return fmt.Errorf("brick: Options.Runtime.ActionURL %q is not a URL", c.ActionURL)
		}
		if !strings.HasSuffix(u.Path, "/action") || u.RawQuery != "" || u.Fragment != "" {
			return fmt.Errorf("brick: Options.Runtime.ActionURL %q: the path must end in /action (uploads and islands are posted beside it) with no query", c.ActionURL)
		}
	}
	return nil
}

// meta is the <meta name="brick-config"> tag, or "" when nothing is set.
func (c RuntimeConfig) meta() string {
	j := runtimeConfigJSON{Debounce: c.Debounce.Milliseconds(), PollMin: c.PollMin.Milliseconds(), ActionURL: c.ActionURL, RetryMaxWait: c.RetryMaxWait.Milliseconds()}
	if c.Retries != 0 {
		n := max(c.Retries, 0)
		j.Retries = &n
	}
	if j == (runtimeConfigJSON{}) {
		return ""
	}
	// A struct of ints and a string always marshals.
	b, _ := json.Marshal(j)
	return `<meta name="brick-config" content="` + template.HTMLEscapeString(string(b)) + `">`
}
