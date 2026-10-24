package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/ubgo/brick"
	"github.com/ubgo/brick/bricktest"
	scriptstarlark "github.com/ubgo/brick/contrib/script-starlark"
)

// The whole flow over real HTTP; testdata/transcript.txt is the output
// docs/guides/live/effects.md and events.md show under "Try it": every effect kind the guide documents,
// the custom one the middleware adds, and the listener the dispatch wakes.
func TestTranscript(t *testing.T) {
	a, err := newApp("theme")
	if err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	if err := demo(&out, a.handler()); err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile("testdata/transcript.txt")
	if err != nil {
		t.Fatal(err)
	}
	if out.String() != string(want) {
		t.Fatalf("transcript:\n%s\nwant:\n%s", out.String(), want)
	}
}

// The page links the runtime, and the Inbox's client code (the x-confetti
// handler) ships as one of its assets.
func TestPageLinksRuntime(t *testing.T) {
	a, err := newApp("theme")
	if err != nil {
		t.Fatal(err)
	}
	page, err := a.eng.Render(context.Background(), inboxPage, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(page), "/runtime.") {
		t.Fatalf("no runtime script on the page:\n%s", page)
	}
	// Options.Runtime reaches the browser as the brick-config meta.
	if want := `<meta name="brick-config" content="{&#34;retries&#34;:2,&#34;retryMaxWait&#34;:3000}">`; !strings.Contains(string(page), want) {
		t.Fatalf("no %s on the page:\n%s", want, page)
	}
}

// A runtime setting the runtime would ignore is refused by brick.New.
func TestRuntimeConfigRefused(t *testing.T) {
	_, err := brick.New(brick.Options{FS: bricktest.Theme("content/index.html", "<p>x</p>"), Runtime: brick.RuntimeConfig{PollMin: 100 * time.Millisecond}})
	if err == nil || !strings.Contains(err.Error(), "is below the 250ms floor") {
		t.Fatalf("err = %v", err)
	}
}

// The pitfalls the guides list, each with the error a user sees.
func TestPitfalls(t *testing.T) {
	sc, err := scriptstarlark.New(scriptstarlark.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, body, want string
	}{
		{"uppercase event", `brick.dispatch("Message-Archived")`, `event names are lowercase`},
		{"reserved event", `brick.dispatch("navigate")`, `is reserved for the runtime`},
		{"foreign redirect", `brick.redirect("https://elsewhere.example/")`, `which is neither this site nor in Options.RedirectHosts`},
		{"bad flash level", `brick.flash("hi", "error")`, `is not one of neutral, info, success, warning, danger`},
		{"too many effects", `brick.dispatch("big", {"blob": "x" * 40000})`, `effects over 32768 bytes (Options.MaxEffectBytes)`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			eng := bricktest.Engine(t, brick.Options{Scripting: sc, States: brick.NewMemoryStates(0, 0)}, bricktest.Theme(
				"components/Go.html", "---\nstate: {}\n---\n<brick:script>\ndef go(state, props):\n    "+tc.body+"\n    return state\n</brick:script>\n<button brick:click=\"go\">Go</button>",
				"content/index.html", `<Go />`,
			))
			live := bricktest.Live(t, eng, "content/index.html")
			_, err := live.Component("Go").TryCall("go")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want it to mention %q", err, tc.want)
			}
			t.Logf("%s: %v", tc.name, err)
		})
	}
}

// Without HTTP: bricktest.Live holds the page's components, Call runs an
// action, and Deliver runs the listeners its dispatch wakes, the way the
// browser would.
// ds:def id=testbadgehearsarchive-y3vufghu
func TestBadgeHearsArchive(t *testing.T) {
	a, err := newApp("theme")
	if err != nil {
		t.Fatal(err)
	}
	live := bricktest.Live(t, a.eng, inboxPage)
	res := live.Component("Inbox").Call("archive", 2)
	bricktest.AssertDispatched(t, res, eventArchived, map[string]any{"id": 2})
	bricktest.AssertFlash(t, res, brick.FlashSuccess, "Archived message 2.")
	live.Deliver(res)
	badge := live.Component("Badge")
	badge.AssertSet(t, "archived", 1)
	badge.AssertSet(t, "last", 2)
}

// A listener must name an action the component has: a typo fails when
// the file is read, not when the event arrives.
func TestListenerTypoFailsEarly(t *testing.T) {
	sc, err := scriptstarlark.New(scriptstarlark.Options{})
	if err != nil {
		t.Fatal(err)
	}
	eng := bricktest.Engine(t, brick.Options{Scripting: sc, States: brick.NewMemoryStates(0, 0)}, bricktest.Theme(
		"components/Badge.html", "---\nstate: {n: 0}\nbrick: {on: {message-archived: cuont}}\n---\n<brick:script>\ndef count(state, props, event):\n    return state\n</brick:script>\n<p>{{ .State.n }}</p>",
		"content/index.html", `<Badge />`,
	))
	_, err = eng.Render(context.Background(), "content/index.html", nil)
	if err == nil || !strings.Contains(err.Error(), `on: message-archived: "cuont" is not a function of this component (have: count, $refresh)`) {
		t.Fatalf("err = %v", err)
	}
}

// inboxScriptRE finds the Inbox's client module on the page.
var inboxScriptRE = regexp.MustCompile(`src="(/_brick/c/Inbox\.[0-9a-f]+\.js)"`)

// The Inbox's <brick:client> block is served as one hashed module, and it
// holds the runtime code the guides show: the custom effect handler, the
// request hook and the runtime event listeners.
func TestClientModuleServed(t *testing.T) {
	a, err := newApp("theme")
	if err != nil {
		t.Fatal(err)
	}
	h := a.handler()
	get := func(path string) string {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s: %d", path, rec.Code)
		}
		return rec.Body.String()
	}
	m := inboxScriptRE.FindStringSubmatch(get("/"))
	if m == nil {
		t.Fatal("no Inbox module on the page")
	}
	js := get(m[1])
	for _, want := range []string{`brick.effect("x-confetti"`, `brick.effect("x-audit"`, `brick.hook("request"`, `brick.on("action-error"`, `brick.on("action-retry"`} {
		if !strings.Contains(js, want) {
			t.Errorf("module lacks %s:\n%s", want, js)
		}
	}
}

// The audit middleware records its custom effect through
// brick.EffectsFrom before the script runs, so it comes first.
// ds:def id=testauditeffect-rgpqq5vp
func TestAuditEffect(t *testing.T) {
	a, err := newApp("theme")
	if err != nil {
		t.Fatal(err)
	}
	live := bricktest.Live(t, a.eng, inboxPage)
	res := live.Component("Inbox").Call("export")
	bricktest.AssertEffects(t, res, auditKind, brick.EffectDownload)
	bricktest.AssertEffect(t, res, brick.Effect{Kind: auditKind, Data: map[string]any{"action": "export", "component": "Inbox"}})
	bricktest.AssertNoEffect(t, live.Component("Inbox").Call("dismiss"), auditKind)
}

// RedirectHosts lets one outside host through and refuses every other,
// with the error text the guide quotes.
// ds:def id=testredirecthosts-bgf65v6x
func TestRedirectHosts(t *testing.T) {
	a, err := newApp("theme")
	if err != nil {
		t.Fatal(err)
	}
	live := bricktest.Live(t, a.eng, inboxPage)
	bricktest.AssertRedirect(t, live.Component("Inbox").Call("pay"), "https://pay.example/checkout")
	_, err = live.Component("Inbox").TryCall("leave")
	if err == nil || !strings.Contains(err.Error(), `names host "elsewhere.example", which is neither this site nor in Options.RedirectHosts`) {
		t.Fatalf("err = %v", err)
	}
}

// MaxEffectBytes is the app's own 4 KiB, not the 32 KiB default.
// ds:def id=testmaxeffectbytes-t8k2m4qz
func TestMaxEffectBytes(t *testing.T) {
	a, err := newApp("theme")
	if err != nil {
		t.Fatal(err)
	}
	live := bricktest.Live(t, a.eng, inboxPage)
	_, err = live.Component("Inbox").TryCall("bulk")
	if err == nil || !strings.Contains(err.Error(), "effects over 4096 bytes (Options.MaxEffectBytes)") {
		t.Fatalf("err = %v", err)
	}
	t.Log(err)
}

// threadPage holds a Thread that places a Reply child.
const threadPage = "content/thread.html"

// A child's $dispatch reaches only the parent that wrote the tag with
// brick:on:liked, and $parent.pin runs an action of the parent.
// ds:def id=testthreadchildevents-m3v9c8xd
func TestThreadChildEvents(t *testing.T) {
	a, err := newApp("theme")
	if err != nil {
		t.Fatal(err)
	}
	live := bricktest.Live(t, a.eng, threadPage)
	thread := live.Component("Thread")
	thread.AssertSet(t, "liked", 0)

	// $dispatch("liked", {"id": 7}) runs no request; Deliver does what the
	// runtime does with the event.
	live.Deliver(live.Component("Reply").Click("#like"))
	thread.AssertSet(t, "liked", 1)

	live.Component("Reply").Click("#pin") // $parent.pin
	thread.AssertSet(t, "pinned", true)
	if !strings.Contains(thread.HTML(), `1 liked, pinned`) {
		t.Fatalf("thread:\n%s", thread.HTML())
	}
}

// badgeScriptRE finds the Badge's client module on the page.
var badgeScriptRE = regexp.MustCompile(`src="(/_brick/c/Badge\.[0-9a-f]+\.js)"`)

// The Badge's client module is served with the runtime calls the guide
// shows. Running them (the watcher firing after a morph, find/all
// answering from the live DOM) takes a browser; this checks the code
// reaches the page.
// ds:def id=testbadgeclientserved-z7c5n2wk
func TestBadgeClientServed(t *testing.T) {
	a, err := newApp("theme")
	if err != nil {
		t.Fatal(err)
	}
	h := a.handler()
	get := func(path string) string {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s: %d", path, rec.Code)
		}
		return rec.Body.String()
	}
	m := badgeScriptRE.FindStringSubmatch(get("/"))
	if m == nil {
		t.Fatal("no Badge module on the page")
	}
	js := get(m[1])
	for _, want := range []string{`brick.mount("Badge"`, `brick.watch(badge, "archived"`, `brick.find("Inbox")`, `brick.all().length`} {
		if !strings.Contains(js, want) {
			t.Errorf("module lacks %s:\n%s", want, js)
		}
	}
}
