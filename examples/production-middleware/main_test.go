package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

// stateAttrs finds the handle and token the page carries for Likes.
var stateAttrs = regexp.MustCompile(`data-brick-state="([^"]+)" data-brick-token="([0-9a-f]+)"`)

// headers are the stand-in auth headers of one request.
type headers map[string]string

type client struct {
	t             *testing.T
	h             http.Handler
	handle, token string
}

// open renders / as a user and keeps the component's handle.
func open(t *testing.T, h http.Handler, hd headers) *client {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	for k, v := range hd {
		r.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	m := stateAttrs.FindStringSubmatch(rec.Body.String())
	if rec.Code != http.StatusOK || m == nil {
		t.Fatalf("GET /: %d\n%s", rec.Code, rec.Body)
	}
	return &client{t: t, h: h, handle: m[1], token: m[2]}
}

// call posts one action the way the browser runtime does.
func (c *client) call(action string, hd headers) *httptest.ResponseRecorder {
	body := `{"handle":"` + c.handle + `","token":"` + c.token + `","action":"` + action + `"}`
	r := httptest.NewRequest(http.MethodPost, "/_brick/action", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Brick-Action", "1")
	for k, v := range hd {
		r.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	c.h.ServeHTTP(rec, r)
	return rec
}

func start(t *testing.T) (http.Handler, *bytes.Buffer) {
	t.Helper()
	var logs bytes.Buffer
	eng, err := newApp("theme", &logs)
	if err != nil {
		t.Fatal(err)
	}
	return handler(eng), &logs
}

func TestChain(t *testing.T) {
	h, logs := start(t)
	sara := headers{userHeader: "sara"}
	c := open(t, h, sara)

	// A signed-in like runs, and the re-render sees the user (ActionRequest).
	rec := c.call("like", sara)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "1 likes, seen by sara") {
		t.Fatalf("like: %d %s", rec.Code, rec.Body)
	}

	// Not an admin: the app's middleware refuses wipe with its own message.
	rec = c.call("wipe", sara)
	if rec.Code != http.StatusForbidden || strings.TrimSpace(rec.Body.String()) != `{"error":{"status":403,"message":"only admins may wipe"}}` {
		t.Fatalf("wipe as user: %d %s", rec.Code, rec.Body)
	}

	// Same session, now an admin: the role is read on every request.
	rec = c.call("wipe", headers{userHeader: "sara", roleHeader: adminRole})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "0 likes") {
		t.Fatalf("wipe as admin: %d %s", rec.Code, rec.Body)
	}

	// Three actions spent the budget; the fourth is a 429 with Retry-After.
	rec = c.call("like", sara)
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("rate limit: %d %v %s", rec.Code, rec.Header(), rec.Body)
	}
	t.Logf("429 Retry-After=%s %s", rec.Header().Get("Retry-After"), strings.TrimSpace(rec.Body.String()))

	for _, want := range []string{
		`level=INFO msg="brick action" component=Likes action=like store=default`,
		`level=WARN msg="brick action failed" component=Likes action=wipe store=default`,
		`err="only admins may wipe"`,
	} {
		if !strings.Contains(logs.String(), want) {
			t.Errorf("log lacks %q:\n%s", want, logs)
		}
	}
	t.Logf("log:\n%s", logs)
}

// No session: RequireSession refuses before the limiter spends a token.
// A guest page still renders, with the inject: default.
func TestGuest(t *testing.T) {
	h, _ := start(t)
	c := open(t, h, nil)
	rec := c.call("like", nil)
	if rec.Code != http.StatusForbidden || strings.TrimSpace(rec.Body.String()) != `{"error":{"status":403,"message":"Forbidden"}}` {
		t.Fatalf("guest like: %d %s", rec.Code, rec.Body)
	}
	t.Logf("guest: %d %s", rec.Code, strings.TrimSpace(rec.Body.String()))
}
