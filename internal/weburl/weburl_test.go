package weburl

import (
	"net/url"
	"testing"
)

// TestIs is the shape test behind redirects, the form CSRF check, the
// sitemap base URL and the URL validation rule: http or https with a host,
// nothing else.
func TestIs(t *testing.T) {
	t.Parallel()
	for raw, want := range map[string]bool{
		"http://a.test": true, "https://a.test/x?y": true, "https://a.test:8443": true,
		"HTTPS://a.test":         true, // url.Parse lowers the scheme
		"//a.test":               false,
		"/path":                  false,
		"javascript:alert(1)":    false,
		"data:text/html,x":       false,
		"mailto:a@b.test":        false,
		"ftp://a.test":           false,
		"https://":               false,
		"https:///path-only":     false,
		"http:a.test":            false,
		"https://user@a.test/":   true, // the shape is fine; callers refuse userinfo themselves
		"wss://a.test/live":      false,
		"https://a.test/%zz-bad": false, // does not parse: never treated as a web URL
	} {
		u, err := url.Parse(raw)
		got := err == nil && Is(u)
		if got != want {
			t.Errorf("Is(%q) = %v, want %v", raw, got, want)
		}
	}
}
