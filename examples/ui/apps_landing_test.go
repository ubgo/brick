package main

import (
	"encoding/json"
	"html"
	"net/http"
	"regexp"
	"strings"
	"testing"
)

var (
	landStateRe = regexp.MustCompile(`data-brick-state="([^"]+)"`)
	landTokenRe = regexp.MustCompile(`data-brick-token="([^"]+)"`)
)

// landSession drives the one stateful component on a page the way the
// runtime does: act posts an action and keeps the handle and token the
// patch carries, so calls chain like clicks.
type landSession struct {
	t             *testing.T
	h             http.Handler
	handle, token string
}

// landOpen reads the state handle and token from a rendered page.
func landOpen(t *testing.T, h http.Handler, url string) *landSession {
	t.Helper()
	_, page := get(t, h, url)
	hm, tm := landStateRe.FindStringSubmatch(page), landTokenRe.FindStringSubmatch(page)
	if hm == nil || tm == nil {
		t.Fatalf("%s: no state handle or token", url)
	}
	return &landSession{t: t, h: h, handle: hm[1], token: tm[1]}
}

// act posts one action with args and returns the patched HTML.
func (s *landSession) act(action string, args ...any) string {
	s.t.Helper()
	code, out := postAction(s.t, s.h, s.handle, s.token, action, args...)
	if code != http.StatusOK {
		s.t.Fatalf("%s: status %d\n%s", action, code, out)
	}
	if m := landStateRe.FindStringSubmatch(out); m != nil {
		s.handle = m[1]
	}
	if m := landTokenRe.FindStringSubmatch(out); m != nil {
		s.token = m[1]
	}
	return out
}

// The landing page renders every section, and its pricing toggle is two
// native radios with both prices in the HTML (CSS picks one, no JS).
func TestLandingPage(t *testing.T) {
	_, h := showcase(t)
	code, body := get(t, h, "/apps/landing/")
	if code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	mustContain(t, "landing", body,
		`class="bk-navbar bk-navbar--sticky"`, `Ship server‑rendered apps, not build pipelines`, `src="/demo/dashboard.svg"`,
		`Trusted by engineering teams at`, `id="features"`, `Works without JavaScript`, `4,200+`,
		`id="pricing"`, `type="radio" name="billing" value="Monthly" checked`, `value="Yearly"`,
		`<span class="land-price--monthly">$24</span><span class="land-price--yearly">$19</span>`, `land-plan land-plan--popular`,
		`id="customers"`, `CTO, Northwind`, `name="faq"`, `Is there a self-hosted option?`,
		`Ready to ship your next app?`, `<footer class="bk-footer bk-footer--muted"`, `href="/apps/auth/"`, `href="/apps/auth/?mode=register"`)
	if n := strings.Count(body, `land-plan--popular`); n != 1 {
		t.Errorf("%d popular plans, want 1", n)
	}
}

// landCallRe matches the runtime's grammar for brick:click: a name, then
// optionally JSON arguments in parentheses (runtime.js parseCall).
var landCallRe = regexp.MustCompile(`^[\w.]+(\((.*)\))?$`)

// landCallsParse fails for any brick:click the browser runtime would
// reject: its arguments are JSON, so 'single quotes' throw at click time
// even though a direct POST of the same action works.
func landCallsParse(t *testing.T, page string) {
	t.Helper()
	for _, m := range regexp.MustCompile(`brick:click="([^"]*)"`).FindAllStringSubmatch(page, -1) {
		call := html.UnescapeString(m[1])
		c := landCallRe.FindStringSubmatch(call)
		if c == nil {
			t.Errorf("brick:click=%q is not name(args)", call)
			continue
		}
		if c[2] != "" {
			var args []any
			if err := json.Unmarshal([]byte("["+c[2]+"]"), &args); err != nil {
				t.Errorf("brick:click=%q: arguments are not JSON: %v", call, err)
			}
		}
	}
}
