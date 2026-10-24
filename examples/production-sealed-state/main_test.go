package main

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/ubgo/brick"
)

// stateAttrs finds the handle and token the page carries for the counter.
var stateAttrs = regexp.MustCompile(`data-brick-state="([^"]+)" data-brick-token="([0-9a-f]+)"`)

// newKey is one fresh base64 key, the format brick keygen prints. Tests
// make their own so no key is ever written into the repository.
func newKey(t *testing.T) string {
	t.Helper()
	k, err := brick.NewStateKey()
	if err != nil {
		t.Fatal(err)
	}
	return brick.EncodeStateKey(k)
}

// env is a lookup over one BRICK_SECRET value.
func env(secret string) func(string) (string, bool) {
	return func(name string) (string, bool) {
		if name == secretEnv {
			return secret, true
		}
		return "", false
	}
}

func start(t *testing.T, secret string) http.Handler {
	t.Helper()
	a, err := newApp("theme", env(secret))
	if err != nil {
		t.Fatal(err)
	}
	return a.handler()
}

// openPage renders / under a session and returns the counter's handle and token.
func openPage(t *testing.T, h http.Handler, sid string) (handle, token string) {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(&http.Cookie{Name: sessionCookie, Value: sid})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	m := stateAttrs.FindStringSubmatch(rec.Body.String())
	if rec.Code != http.StatusOK || m == nil {
		t.Fatalf("GET /: %d\n%s", rec.Code, rec.Body)
	}
	return m[1], m[2]
}

// click posts the counter's bump action the way the browser runtime does.
func click(h http.Handler, sid, handle, token string) *httptest.ResponseRecorder {
	body := `{"handle":"` + handle + `","token":"` + token + `","action":"bump"}`
	r := httptest.NewRequest(http.MethodPost, "/_brick/action", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Brick-Action", "1")
	if sid != "" {
		r.AddCookie(&http.Cookie{Name: sessionCookie, Value: sid})
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

// The state is in the page, so a new process with the same key opens it.
func TestSurvivesRestart(t *testing.T) {
	secret := newKey(t)
	handle, token := openPage(t, start(t, secret), "alice")
	if !strings.HasPrefix(handle, "default.s1.") {
		t.Fatalf("handle is not a sealed blob: %s", handle)
	}
	rec := click(start(t, secret), "alice", handle, token)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Clicked 1 times") {
		t.Fatalf("after restart: %d %s", rec.Code, rec.Body)
	}
}

// Rotation: a ring with the new key first still opens blobs sealed by the
// old one; once the old key is removed they are gone (410), and the
// runtime recovers by reloading the component.
func TestKeyRotation(t *testing.T) {
	oldKey, newKeyB := newKey(t), newKey(t)
	handle, token := openPage(t, start(t, oldKey), "alice")
	if rec := click(start(t, newKeyB+","+oldKey), "alice", handle, token); rec.Code != http.StatusOK {
		t.Fatalf("new,old ring: %d %s", rec.Code, rec.Body)
	}
	rec := click(start(t, newKeyB), "alice", handle, token)
	if rec.Code != http.StatusGone {
		t.Fatalf("old key removed: want 410, got %d %s", rec.Code, rec.Body)
	}
	t.Logf("old key removed: %d %s", rec.Code, strings.TrimSpace(rec.Body.String()))
}

// The blob is bound to the session that rendered it: another visitor
// replaying it gets 410, the same answer as a lost blob.
func TestBoundToSession(t *testing.T) {
	h := start(t, newKey(t))
	handle, token := openPage(t, h, "alice")
	rec := click(h, "mallory", handle, token)
	if rec.Code != http.StatusGone {
		t.Fatalf("other session: want 410, got %d %s", rec.Code, rec.Body)
	}
}

// Sign-out refuses every blob the session was issued before.
func TestSignOut(t *testing.T) {
	h := start(t, newKey(t))
	handle, token := openPage(t, h, "alice")
	r := httptest.NewRequest(http.MethodPost, "/signout", nil)
	r.AddCookie(&http.Cookie{Name: sessionCookie, Value: "alice"})
	h.ServeHTTP(httptest.NewRecorder(), r)
	rec := click(h, "alice", handle, token)
	if rec.Code != http.StatusGone {
		t.Fatalf("after sign-out: want 410, got %d %s", rec.Code, rec.Body)
	}
}

// A missing or malformed BRICK_SECRET fails at start, never at the first
// click.
func TestBadSecret(t *testing.T) {
	for _, secret := range []string{"", "not-base64!"} {
		if _, err := newApp("theme", env(secret)); err == nil {
			t.Errorf("BRICK_SECRET=%q: no error", secret)
		} else {
			t.Logf("BRICK_SECRET=%q: %v", secret, err)
		}
	}
	unset := func(string) (string, bool) { return "", false }
	if _, err := newApp("theme", unset); err == nil {
		t.Error("BRICK_SECRET unset: no error")
	} else {
		t.Logf("BRICK_SECRET unset: %v", err)
	}
}
