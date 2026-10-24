package brick

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// formOriginOK is the CSRF gate of a no-JS form post: Sec-Fetch-Site must
// be same-origin when present, otherwise Origin must be this host, and a
// post with neither is refused. BV: making formOriginOK return true fails
// every refusal row.
func TestFormOriginOK(t *testing.T) {
	t.Parallel()
	type tc struct {
		name, fetchSite, origin, host string
		want                          bool
	}
	for _, c := range []tc{
		{"same-origin fetch", "same-origin", "", "a.test", true},
		{"same-origin wins over a foreign Origin", "same-origin", "https://evil.test", "a.test", true},
		{"same-site sibling subdomain", "same-site", "https://b.a.test", "a.test", false},
		{"cross-site", "cross-site", "https://a.test", "a.test", false},
		{"typed URL (none)", "none", "", "a.test", false},
		{"Origin of this host", "", "https://a.test", "a.test", true},
		{"Origin of this host over http", "", "http://a.test", "a.test", true},
		{"Origin host case differs", "", "https://A.TEST", "a.test", true},
		{"Origin with this host and port", "", "https://a.test:8443", "a.test:8443", true},
		{"Origin port differs", "", "https://a.test:8443", "a.test", false},
		{"foreign Origin", "", "https://evil.test", "a.test", false},
		{"null Origin (sandboxed frame, file:)", "", "null", "a.test", false},
		{"no Origin and no Sec-Fetch-Site", "", "", "a.test", false},
		{"non-web Origin", "", "javascript:a.test", "a.test", false},
		{"Origin without a scheme", "", "a.test", "a.test", false},
	} {
		r := httptest.NewRequest(http.MethodPost, "/", nil)
		r.Host = c.host
		if c.fetchSite != "" {
			r.Header.Set("Sec-Fetch-Site", c.fetchSite)
		}
		if c.origin != "" {
			r.Header.Set("Origin", c.origin)
		}
		if got := formOriginOK(r); got != c.want {
			t.Errorf("%s: formOriginOK = %v, want %v", c.name, got, c.want)
		}
	}
}

// An ActionError keeps its status only inside 400-599; the edges on both
// sides fall back to 403, the same answer as a non-error status.
func TestActionStatusErrorRangeEdges(t *testing.T) {
	t.Parallel()
	for status, want := range map[int]int{
		399: http.StatusForbidden, 400: 400, 599: 599, 600: http.StatusForbidden, 0: http.StatusForbidden,
	} {
		if got := ActionStatus(&ActionError{Status: status}); got != want {
			t.Errorf("ActionStatus(ActionError{Status: %d}) = %d, want %d", status, got, want)
		}
	}
	// The cause stays reachable through the status mapping.
	cause := errors.New("cause")
	if !errors.Is(&ActionError{Status: 409, Err: cause}, cause) {
		t.Error("ActionError does not unwrap to its cause")
	}
}
