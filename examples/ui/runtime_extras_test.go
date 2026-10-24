package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/ubgo/brick"
)

// The Rows, hand-offs and declarations page (content/runtime-extras.html)
// shows the v9 features in the showcase; the browser suite drives them
// (runtime/test/e2e_ui_chrome.mjs, the "v9:" checks). These tests pin what
// the server alone decides.

// The page renders every demo: the seeded feed rows inside the brick:ignore
// list, the five cards answered by one load_many call (four distinct
// inputs), the panel's declared blocks and the heading written by tag.
func TestRuntimeExtrasPage(t *testing.T) {
	_, h := showcase(t)
	code, out := get(t, h, "/runtime-extras/")
	if code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	mustContain(t, "runtime extras", out,
		`brick:ref="feed" brick:ignore`, `brick:key="3"`, "Seeded message 1",
		`action="/demo/sign-in"`, `brick:ref="classic"`,
		`id="rx-crash"`,
		"Ada Lovelace", "Margaret Hamilton",
		`<div id="rp-title" style="font-weight: 600">Quarterly report</div>`,
		`<h2 id="rh-heading" class="rh-heading">A heading at level 2</h2>`,
	)
	if n := strings.Count(out, `class="ru-card" data-batch="4"`); n != 5 {
		t.Fatalf("%d cards report one call with 4 inputs, want 5", n)
	}
}

// The classic POST the brick.submit demo hands off to sets its cookie and
// redirects back with the name, and refuses anything but a named POST.
func TestDemoSignIn(t *testing.T) {
	h := withDemoSignIn(http.NotFoundHandler(), "/ui")
	post := func(body string) *http.Response {
		req, err := http.NewRequest(http.MethodPost, "/ui"+signInPath, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		return serve(h, req)
	}
	res := post("name=+Ada+")
	if res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != "/ui"+signInBack+"?signed=Ada" {
		t.Fatalf("sign-in = %d %q", res.StatusCode, res.Header.Get("Location"))
	}
	if c := res.Cookies(); len(c) != 1 || c[0].Name != signInCookie || c[0].Value != "Ada" || !c[0].HttpOnly || c[0].Path != "/ui/" {
		t.Fatalf("cookie = %+v", c)
	}
	if res := post("name=+"); res.StatusCode != http.StatusBadRequest {
		t.Fatalf("an empty name answered %d", res.StatusCode)
	}
	get, err := http.NewRequest(http.MethodGet, "/ui"+signInPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res := serve(h, get); res.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("a GET answered %d", res.StatusCode)
	}
}

// A failed action of the reload demo is answered 500, the status
// brick.reloadComponent recovers from; other components keep the engine's
// 422 for a script failure.
func TestServerFaults(t *testing.T) {
	mw := serverFaults(serverFaultDemo)
	failing := func(context.Context, *brick.ActionCall) (*brick.ActionResult, error) {
		return nil, os.ErrInvalid
	}
	_, err := mw(failing)(context.Background(), &brick.ActionCall{Component: serverFaultDemo, Action: "crash"})
	if got := brick.ActionStatus(err); got != http.StatusInternalServerError {
		t.Fatalf("the demo's failure answered %d", got)
	}
	_, err = mw(failing)(context.Background(), &brick.ActionCall{Component: "ContactsApp", Action: "save"})
	if got := brick.ActionStatus(err); got != http.StatusUnprocessableEntity {
		t.Fatalf("another component's failure answered %d", got)
	}
}

// RuntimePanel's declared slots are enforced at the caller's line, and its
// docs reach Engine.ComponentDocs (what `brick docs` prints).
func TestRuntimePanelDeclarations(t *testing.T) {
	src, err := os.ReadFile("demos/RuntimePanel.html")
	if err != nil {
		t.Fatal(err)
	}
	e, err := brick.New(brick.Options{Roots: []string{"demos"}, FS: fstest.MapFS{
		"demos/RuntimePanel.html": {Data: src},
		"content/ok.html":         {Data: []byte(`<RuntimePanel><brick:prop name="title">T</brick:prop>body</RuntimePanel>`)},
		"content/bad.html":        {Data: []byte("<p>x</p>\n<RuntimePanel>body</RuntimePanel>")},
		"content/extra.html":      {Data: []byte(`<RuntimePanel><brick:prop name="title">T</brick:prop><brick:prop name="footer">f</brick:prop>body</RuntimePanel>`)},
	}})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := e.Render(ctx, "content/ok.html", nil); err != nil {
		t.Fatalf("a call with every required block: %v", err)
	}
	if _, err := e.Render(ctx, "content/bad.html", nil); err == nil || !strings.Contains(err.Error(), "content/bad.html:2") || !strings.Contains(err.Error(), "title") {
		t.Fatalf("a missing title block: %v", err)
	}
	if _, err := e.Render(ctx, "content/extra.html", nil); err == nil || !strings.Contains(err.Error(), "footer") {
		t.Fatalf("an undeclared block: %v", err)
	}
	docs, err := e.ComponentDocs()
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range docs {
		if d.Name != "RuntimePanel" {
			continue
		}
		if len(d.Props) != 1 || d.Props[0].Doc == "" || len(d.Slots) != 3 {
			t.Fatalf("docs = %+v", d)
		}
		want := map[string]bool{"actions": false, "children": true, "title": true} // name → required
		for _, s := range d.Slots {
			if req, ok := want[s.Name]; !ok || s.Required != req || s.Doc == "" {
				t.Fatalf("slot %+v", s)
			}
		}
		return
	}
	t.Fatal("RuntimePanel is not in ComponentDocs")
}

// serve runs one request through h and returns the response.
func serve(h http.Handler, req *http.Request) *http.Response {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Result()
}
