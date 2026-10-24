package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/ubgo/brick"
	"github.com/ubgo/brick/bricktest"
)

// A row keeps its open state when the parent acts; a reactive prop still
// follows the parent; a removed row takes its own state with it and the
// next row keeps its own, because rows are keyed.
func TestRowsKeepStateAndFollowReactiveProps(t *testing.T) {
	a, err := newApp("theme")
	if err != nil {
		t.Fatal(err)
	}
	live := bricktest.Live(t, a.eng, page)
	app := live.Component("ContactsApp")
	rows := app.Children()
	if len(rows) != 3 {
		t.Fatalf("want 3 rows, got %d", len(rows))
	}
	rows[1].Click(".toggle") // Grace's notes open
	rows[1].AssertSet(t, "open", true)

	app.Call("rename", 2, "Grace H.") // the parent acts; Grace's row is kept
	html := app.HTML()
	if !strings.Contains(html, "Notes for Grace H.") {
		t.Fatalf("the open row lost its state or its reactive prop:\n%s", html)
	}
	if strings.Contains(html, "Notes for Ada") || strings.Contains(html, "Notes for Linus") {
		t.Fatalf("a closed row opened:\n%s", html)
	}

	app.Call("remove", 1) // Ada leaves; Grace must not inherit Ada's slot
	html = app.HTML()
	if strings.Contains(html, "Ada") || !strings.Contains(html, "Notes for Grace H.") {
		t.Fatalf("state followed the position, not the contact:\n%s", html)
	}
}

// Without :key a repeated stateful child is identified by position, and
// Dev says "give it key=". The example's own theme, which keys its rows,
// produces no such warning.
func TestMissingKeyWarns(t *testing.T) {
	warns := func(call string) bool {
		eng := bricktest.Engine(t, brick.Options{Mode: brick.Dev, States: brick.NewMemoryStates(0, 0)}, bricktest.Theme(
			"components/Row.html", "---\nstate: {open: false}\nprops:\n  label: string\n---\n<li>{{ .Props.label }}</li>",
			"components/List.html", "---\nstate: {}\n---\n<ul>{{ range (list \"a\" \"b\") }}"+call+"{{ end }}</ul>",
			"content/a.html", "<List />",
		))
		if _, err := eng.Render(context.Background(), "content/a.html", nil); err != nil {
			t.Fatal(err)
		}
		for _, d := range eng.Warnings() {
			if strings.Contains(d.Message, "give it key=") {
				return true
			}
		}
		return false
	}
	if !warns(`<Row :label="." />`) {
		t.Error("an unkeyed repeated stateful child did not warn")
	}
	if warns(`<Row :label="." :key="." />`) {
		t.Error("a keyed one warned")
	}
}

var handleRE = regexp.MustCompile(`data-brick-name="ContactRow"|data-brick-state="([^"]+)" data-brick-token="[0-9a-f]+" data-brick-name="ContactRow"`)

// Engine.Instance reads an instance the way an action does and the route
// shows it; an unknown handle is a 404.
func TestInspectRoute(t *testing.T) {
	a, err := newApp("theme")
	if err != nil {
		t.Fatal(err)
	}
	h := a.handler()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	m := handleRE.FindStringSubmatch(rec.Body.String())
	if m == nil || m[1] == "" {
		t.Fatalf("no ContactRow handle on the page:\n%s", rec.Body)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/inspect?handle="+m[1], nil))
	var got struct {
		Component string
		State     map[string]any
		Props     map[string]any
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || got.Component != "ContactRow" || got.State["open"] != false {
		t.Fatalf("inspect = %s (%v)", rec.Body, err)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/inspect?handle=default.nope", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown handle: %d", rec.Code)
	}
}

// Signing a session out ends its instances, and the hook hears each one
// with the reason and the component's name.
func TestEndHookHearsRevoke(t *testing.T) {
	a, err := newApp("theme")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Demo-Session", "sess-1")
	a.handler().ServeHTTP(httptest.NewRecorder(), req)

	if err := a.eng.RevokeSession(context.Background(), "sess-1"); err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range a.ends.got() {
		if e.Reason != brick.EndRevoked || e.Store != "default" {
			t.Errorf("end = %+v", e)
		}
		names = append(names, e.Instance.Name)
	}
	if len(names) != 4 { // the ContactsApp and its three rows
		t.Fatalf("heard %v, want ContactsApp and three ContactRow", names)
	}
}
