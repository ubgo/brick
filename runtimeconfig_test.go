package brick

import (
	"strings"
	"testing"
	"time"
)

// The zero RuntimeConfig writes no meta, so every existing page keeps its
// bytes; a set field writes the meta just before the runtime script, with
// only the fields the host set, in milliseconds.
func TestRuntimeConfigMeta(t *testing.T) {
	t.Parallel()
	page := theme("content/a.html", "<p>a</p>{{ assets }}")
	plain := render(t, newEngine(t, Options{Navigation: true, FS: page}), "content/a.html", nil)
	if strings.Contains(plain, "brick-config") {
		t.Fatalf("zero config wrote a meta: %s", plain)
	}
	e := newEngine(t, Options{Navigation: true, FS: page, Runtime: RuntimeConfig{
		Debounce: 400 * time.Millisecond, PollMin: 500 * time.Millisecond, ActionURL: "https://api.example.com/_brick/action", Retries: 3, RetryMaxWait: 10 * time.Second,
	}})
	out := render(t, e, "content/a.html", nil)
	want := `<brick-assets><meta name="brick-config" content="{&#34;debounce&#34;:400,&#34;pollMin&#34;:500,&#34;actionURL&#34;:&#34;https://api.example.com/_brick/action&#34;,&#34;retries&#34;:3,&#34;retryMaxWait&#34;:10000}"><script type="module" src="/_brick/c/runtime.`
	if !strings.Contains(out, want) {
		t.Fatalf("meta missing or not before the runtime:\n%s", out)
	}
	if strings.Count(out, "brick-config") != 1 {
		t.Fatalf("meta written more than once: %s", out)
	}
}

// NoRetry is written as retries 0; Retries 0 is the default and writes
// nothing.
func TestRuntimeConfigNoRetry(t *testing.T) {
	t.Parallel()
	if got := (RuntimeConfig{Retries: NoRetry}).meta(); !strings.Contains(got, "{&#34;retries&#34;:0}") {
		t.Fatalf("NoRetry meta = %s", got)
	}
	if got := (RuntimeConfig{}).meta(); got != "" {
		t.Fatalf("zero meta = %q", got)
	}
}

// New refuses a config the runtime would silently ignore or misuse.
func TestRuntimeConfigRefused(t *testing.T) {
	t.Parallel()
	for name, c := range map[string]RuntimeConfig{
		"negative debounce":      {Debounce: -time.Millisecond},
		"poll below floor":       {PollMin: 100 * time.Millisecond},
		"retries below -1":       {Retries: -2},
		"negative max wait":      {RetryMaxWait: -time.Second},
		"url not ending /action": {ActionURL: "https://api.example.com/act"},
		"url with query":         {ActionURL: "https://api.example.com/_brick/action?x=1"},
		"url with space":         {ActionURL: "https://api.example.com/a b/action"},
	} {
		if _, err := New(Options{Runtime: c}); err == nil {
			t.Errorf("%s: New accepted %+v", name, c)
		}
	}
	if _, err := New(Options{Runtime: RuntimeConfig{PollMin: MinPollMin, ActionURL: "/api/action", Retries: NoRetry}}); err != nil {
		t.Fatalf("valid config refused: %v", err)
	}
}
