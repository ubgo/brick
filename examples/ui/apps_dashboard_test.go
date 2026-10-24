package main

import (
	"net/http"
	"regexp"
	"strings"
	"testing"
)

// dashURL is the Dashboard app.
const dashURL = "/apps/dashboard/"

var dashRootRe = regexp.MustCompile(`data-brick-state="([^"]+)" data-brick-token="([^"]+)"`)

// dashSession is one visitor's DashBoard instance: its handle and token,
// refreshed from every response.
type dashSession struct {
	t             *testing.T
	h             http.Handler
	handle, token string
}

func dashOpen(t *testing.T, h http.Handler, url string) (*dashSession, string) {
	t.Helper()
	code, page := get(t, h, url)
	if code != http.StatusOK {
		t.Fatalf("%s: status %d", url, code)
	}
	m := dashRootRe.FindStringSubmatch(page)
	if m == nil {
		t.Fatalf("%s: no stateful component", url)
	}
	return &dashSession{t: t, h: h, handle: m[1], token: m[2]}, page
}

// act posts one action to /_brick/action, as the runtime does, and returns
// the re-rendered component.
func (s *dashSession) act(action string, args ...any) string {
	s.t.Helper()
	code, out := postAction(s.t, s.h, s.handle, s.token, action, args...)
	if code != http.StatusOK {
		s.t.Fatalf("%s: status %d\n%s", action, code, out)
	}
	if m := dashRootRe.FindStringSubmatch(out); m != nil {
		s.handle, s.token = m[1], m[2]
	}
	return out
}

// The Dashboard renders every panel from the seeded orders: the default
// 30-day range ends on the newest order (Oct 1) and compares with the 30
// days before it.
func TestDashboardPage(t *testing.T) {
	_, h := showcase(t)
	_, page := dashOpen(t, h, dashURL)
	mustContain(t, dashURL, page,
		// Shell: the shared app nav with this app active, the top bar.
		`href="/apps/dashboard/" aria-current="page"`, `type="search" class="bk-input__control" name="q"`, `3 unread notifications`, `class="bk-dropdown`,
		// KPIs with deltas against the previous 30 days (html/template writes
		// a + as &#43;).
		`$1,637`, `&#43;9.1%`, `$90.94`, `20.0%`, `&#43;0.0 pts`,
		// The range control: native radios, 30 days checked.
		`name="range" value="30d" checked`,
		// Charts: the revenue line, orders by status, top products.
		`aria-label="Paid revenue per day, 7-day average"`, `<span class="bk-donut-chart__value">30</span>`,
		`title="Desk lamp&#10;Revenue: $632"`,
		// The orders table: newest first, status badges, pages as #orders-page-N.
		`#1120`, `Leather wallet`, `>Refunded<`, `href="#orders-page-2"`, `<b>1</b>–<b>8</b>`, `of <b>30</b>`,
		// Activity from the same orders.
		`Ethan Silva was refunded for Leather wallet`,
	)
}

// The date range and the orders page are actions: each re-queries and
// re-renders the board; a new range goes back to page 1.
func TestDashboardActions(t *testing.T) {
	_, h := showcase(t)
	s, _ := dashOpen(t, h, dashURL)
	out := s.act("page", 3)
	mustContain(t, "page 3", out, `<b>17</b>–<b>24</b>`, `aria-current="page">3</a>`, `name="range" value="30d" checked`)
	out = s.act("setRange", "7d")
	mustContain(t, "7 days", out, `name="range" value="7d" checked`, `$350`, `$116.67`, `28.6%`, `<b>1</b>–<b>7</b>`, `of <b>7</b>`)
	if strings.Contains(out, `href="#orders-page-2"`) {
		t.Error("7 days fits one page, and the page reset to 1")
	}
	out = s.act("setRange", "90d")
	mustContain(t, "90 days", out, `name="range" value="90d" checked`, `of <b>90</b>`, `aria-current="page">1</a>`)
	out = s.act("setRange", "1y")
	mustContain(t, "unknown range", out, `name="range" value="90d" checked`)
}
