package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/ubgo/brick"
	"github.com/ubgo/brick/bricktest"
)

// session is the test visitor's session id.
const session = "visitor-1"

// rootRE finds one stateful root's handle and token by component name.
func rootRE(name string) *regexp.Regexp {
	return regexp.MustCompile(`data-brick-state="([^"]+)" data-brick-token="([0-9a-f]+)" data-brick-name="` + name + `"`)
}

// client posts actions the way the runtime does, with the visitor's
// session cookie, and keeps each component's latest handle.
type client struct {
	t *testing.T
	h http.Handler
}

// get fetches a page as the visitor, failing on anything but 200.
func (c client) get(target string) string {
	c.t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: session})
	rec := httptest.NewRecorder()
	c.h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		c.t.Fatalf("GET %s: %d %s", target, rec.Code, rec.Body)
	}
	return rec.Body.String()
}

// post sends one action with optional deferred sets and returns the
// status and the decoded envelope (html, effects, …).
func (c client) post(html, name, action string, args []any, sets [][2]any) (int, map[string]any) {
	c.t.Helper()
	m := rootRE(name).FindStringSubmatch(html)
	if m == nil {
		c.t.Fatalf("no %s root in:\n%s", name, html)
	}
	if args == nil {
		args = []any{}
	}
	body, _ := json.Marshal(map[string]any{"handle": m[1], "token": m[2], "action": action, "args": args, "sets": sets})
	req := httptest.NewRequest(http.MethodPost, "/_brick/action", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Brick-Action", "1")
	req.Header.Set("Accept", brick.EffectsContentType)
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: session})
	rec := httptest.NewRecorder()
	c.h.ServeHTTP(rec, req)
	env := map[string]any{}
	_ = json.Unmarshal(rec.Body.Bytes(), &env)
	if env["html"] == nil {
		env["error"] = rec.Body.String()
	}
	return rec.Code, env
}

// queryEffect is the URL write an answer asks the runtime to make.
func queryEffect(env map[string]any) map[string]any {
	effs, _ := env["effects"].([]any)
	for _, e := range effs {
		if m, _ := e.(map[string]any); m["kind"] == brick.EffectQuery {
			return m
		}
	}
	return nil
}

// query: seeds the fields from the URL, params: corrects them, an action
// writes changed fields back, and $params is how a patch link lands.
func TestURLState(t *testing.T) {
	a, err := newApp("theme")
	if err != nil {
		t.Fatal(err)
	}
	c := client{t, a.handler()}
	page := c.get("/?search=acme&page=9")
	if !strings.Contains(page, `<p id="where">q=acme tab=all page=5 columns=title,price rows=0</p>`) {
		t.Fatalf("seeded from the URL, page clamped by params::\n%s", page)
	}

	code, env := c.post(page, "Catalog", "$set", []any{"page", 2}, nil)
	ef := queryEffect(env)
	if code != http.StatusOK || ef == nil || ef["replace"] == true {
		t.Fatalf("Next: %d %v (want a push of page=2)", code, env)
	}
	if got, _ := json.Marshal(ef["data"]); string(got) != `{"set":{"page":"2"}}` {
		t.Fatalf("query effect data = %s", got)
	}

	// A click on <a href="?tab=books">: the runtime sends $params with the
	// URL's values as sets, and only this component re-renders.
	code, env = c.post(env["html"].(string), "Catalog", "$params", nil, [][2]any{{"tab", "books"}, {"page", "2"}, {"q", "acme"}})
	if html, _ := env["html"].(string); code != http.StatusOK || !strings.Contains(html, "q=acme tab=books page=2") {
		t.Fatalf("$params: %v", env)
	}
}

// remember: survives a new instance in the same session; temporary: is
// shown once and never saved.
func TestRememberAndTemporary(t *testing.T) {
	a, err := newApp("theme")
	if err != nil {
		t.Fatal(err)
	}
	live := bricktest.Live(t, a.eng, page, brick.WithSession(session))
	cat := live.Component("Catalog")
	cat.Click("#load")
	if !strings.Contains(cat.HTML(), "rows=3") {
		t.Fatalf("the render after load shows the rows:\n%s", cat.HTML())
	}
	cat.AssertSet(t, "rows", []any{}) // reset before the save
	cat.Click("#wide")

	again := bricktest.Live(t, a.eng, page, brick.WithSession(session)).Component("Catalog")
	again.AssertSet(t, "columns", []any{"title", "price", "stock"})
	other := bricktest.Live(t, a.eng, page, brick.WithSession("visitor-2")).Component("Catalog")
	other.AssertSet(t, "columns", []any{"title", "price"})
}

// locked: and writable: refuse a client write with 422 and leave the
// state alone; the server's own action still writes the field.
func TestLockedAndWritable(t *testing.T) {
	a, err := newApp("theme")
	if err != nil {
		t.Fatal(err)
	}
	c := client{t, a.handler()}
	page := c.get("/")

	code, env := c.post(page, "RowEditor", "$set", []any{"editing_id", 9}, nil)
	if code != http.StatusUnprocessableEntity || !strings.Contains(env["error"].(string), "state.editing_id is locked") {
		t.Fatalf("$set of a locked field: %d %v", code, env)
	}
	code, env = c.post(page, "RowEditor", "open", []any{7}, [][2]any{{"note", "hi"}})
	if code != http.StatusOK || !strings.Contains(env["html"].(string), "editing=7 note=hi") {
		t.Fatalf("open(7) with a deferred note: %d %v", code, env)
	}

	code, env = c.post(page, "Profile", "$set", []any{"role", "admin"}, nil)
	if code != http.StatusUnprocessableEntity || !strings.Contains(env["error"].(string), "state.role is closed to the client") {
		t.Fatalf("$set of a field not in writable:: %d %v", code, env)
	}
	code, env = c.post(page, "Profile", "$set", []any{"title", "Dr"}, nil)
	if code != http.StatusOK || !strings.Contains(env["html"].(string), "title=Dr role=viewer") {
		t.Fatalf("$set of a writable field: %d %v", code, env)
	}
}
