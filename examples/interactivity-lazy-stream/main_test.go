package main

import (
	"bytes"
	"github.com/ubgo/brick"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/ubgo/brick/bricktest"
)

// ticketRE reads a lazy island's ticket from its placeholder.
var ticketRE = regexp.MustCompile(`<brick-lazy data-brick-lazy="([0-9A-Za-z._-]+)" data-when="visible">`)

// get serves one request and fails on anything but 200.
func get(t *testing.T, h http.Handler, req *http.Request) string {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("%s: %d %s", req.URL, rec.Code, rec.Body)
	}
	return rec.Body.String()
}

// The island is filled per request; the lazy island is a placeholder with
// a ticket the browser redeems at /_brick/island; a plain request gets the
// stream island in place.
func TestIslandsAndLazy(t *testing.T) {
	a, err := newApp("theme")
	if err != nil {
		t.Fatal(err)
	}
	h := a.handler()
	sara := get(t, h, httptest.NewRequest(http.MethodGet, "/?user=Sara", nil))
	guest := get(t, h, httptest.NewRequest(http.MethodGet, "/", nil))
	if !strings.Contains(sara, `<a id="account" href="/account">Hi, Sara</a>`) || !strings.Contains(guest, `<a id="account" href="/login">Sign in</a>`) {
		t.Fatalf("island per request:\n%s\n%s", sara, guest)
	}
	if !strings.Contains(sara, `<p id="revenue">Revenue: $1,200</p>`) {
		t.Fatalf("a request that cannot stream renders the stream island in place:\n%s", sara)
	}
	m := ticketRE.FindStringSubmatch(sara)
	if m == nil || !strings.Contains(sara, `<div class="skeleton">Loading sales…</div></brick-lazy>`) {
		t.Fatalf("lazy placeholder:\n%s", sara)
	}
	island := get(t, h, httptest.NewRequest(http.MethodGet, "/_brick/island?t="+m[1], nil))
	if strings.TrimSpace(island) != `{"v":1,"html":"\u003cfigure id=\"sales\"\u003eSales: 42 orders\u003c/figure\u003e","assets":[]}` {
		t.Fatalf("island answer: %s", island)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/_brick/island?t=forged", nil))
	if rec.Code != http.StatusGone {
		t.Fatalf("a forged ticket: %d (want 410, the runtime then reloads the page once)", rec.Code)
	}
}

// A browser navigation streams: the placeholder first, then the island
// later in the same response.
func TestStreamIsland(t *testing.T) {
	a, err := newApp("theme")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Sec-Fetch-Mode", "navigate")
	page := get(t, a.handler(), req)
	hole := strings.Index(page, `<brick-lazy data-brick-stream="s1"><div class="skeleton">Loading revenue…</div></brick-lazy>`)
	fill := strings.Index(page, `<brick-fill data-brick-fill="s1" hidden><p id="revenue">Revenue: $1,200</p></brick-fill>`)
	if hole < 0 || fill < hole {
		t.Fatalf("stream order:\n%s", page)
	}
}

// brick.stream sends text into the brick:ref while the action runs; the
// action's own render lands after it.
func TestStreamedReply(t *testing.T) {
	a, err := newApp("theme")
	if err != nil {
		t.Fatal(err)
	}
	ask := bricktest.Live(t, a.eng, page).Component("Ask")
	_, chunks := ask.Stream("ask")
	var got []string
	for _, c := range chunks {
		got = append(got, c.Ref+":"+c.Text)
	}
	if strings.Join(got, "|") != "answer:Hel|answer:lo, |answer:Ada!" {
		t.Fatalf("chunks = %v", got)
	}
	ask.AssertSet(t, "answer", "Hello, Ada!")
}

// A ticket sealed under the app's keys redeems on a brand-new engine that
// holds the same keys (a restart, a second server) and is refused by one
// holding other keys. The in-memory default would answer 410 to both.
func TestTicketsSurviveARestart(t *testing.T) {
	first, err := newApp("theme")
	if err != nil {
		t.Fatal(err)
	}
	m := ticketRE.FindStringSubmatch(get(t, first.handler(), httptest.NewRequest(http.MethodGet, "/?user=Sara", nil)))
	if m == nil {
		t.Fatal("no ticket on the page")
	}
	redeem := func(a *app) int {
		rec := httptest.NewRecorder()
		a.handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/_brick/island?t="+m[1], nil))
		return rec.Code
	}

	restarted, err := newApp("theme")
	if err != nil {
		t.Fatal(err)
	}
	if code := redeem(restarted); code != http.StatusOK {
		t.Fatalf("same keys, new engine: %d, want 200", code)
	}

	other, err := brick.StaticKeys(bytes.Repeat([]byte{0x17}, brick.StateKeySize))
	if err != nil {
		t.Fatal(err)
	}
	stranger, err := newAppWithKeys("theme", other)
	if err != nil {
		t.Fatal(err)
	}
	if code := redeem(stranger); code != http.StatusGone {
		t.Fatalf("other keys: %d, want 410", code)
	}
}

// bricktest renders lazy islands in the page when asked, so a test can read
// their content and call their actions without redeeming a ticket.
func TestLazyInlineInTests(t *testing.T) {
	a, err := newApp("theme")
	if err != nil {
		t.Fatal(err)
	}
	live := bricktest.Live(t, a.eng, page, brick.WithLazyInline())
	if !strings.Contains(live.HTML(), `<figure id="sales">Sales: 42 orders</figure>`) || strings.Contains(live.HTML(), "<brick-lazy") {
		t.Fatalf("lazy island not rendered in place:\n%s", live.HTML())
	}
}
