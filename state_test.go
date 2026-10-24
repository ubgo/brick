package brick

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"
)

// counterScripting is a Scripting whose inline module has the counter's
// actions, written in Go.
func counterScripting() fakeScripting {
	return fakeScripting{modules: map[string]map[string]func(args ...any) (any, error){
		"counter": {
			"increment": func(a ...any) (any, error) {
				st, _ := a[0].(map[string]any)
				props, _ := a[1].(map[string]any)
				n, _ := toInt(st["count"])
				step, _ := toInt(props["step"])
				return map[string]any{"count": n + step}, nil
			},
			"bump": func(a ...any) (any, error) {
				st, _ := a[0].(map[string]any)
				n, _ := toInt(st["count"])
				m, _ := toInt(a[2])
				return map[string]any{"count": n + m}, nil
			},
			"broken":   func(...any) (any, error) { return "not a dict", nil },
			"_private": func(...any) (any, error) { return map[string]any{}, nil },
		},
	}}
}

const counterFile = "---\nstate:\n  count: 0\nprops:\n  step: {type: int, default: 1}\n---\n<brick:script>counter</brick:script>\n<div class=\"counter\"><span>{{ .State.count }}</span><button brick:click=\"increment\">+{{ .Props.step }}</button></div>"

var stateRE = regexp.MustCompile(`data-brick-state="([^"]+)" data-brick-token="([0-9a-f]+)"`)

func TestActions(t *testing.T) {
	t.Parallel()
	store := NewMemoryStates(0, 0)
	e := newEngine(t, Options{States: store, Scripting: counterScripting(), FS: theme(
		"components/Counter.html", counterFile,
		"content/a.html", `<Counter step="2" />`,
	)})
	ctx := context.Background()
	r, err := e.RenderChunks(ctx, "content/a.html", nil)
	if err != nil {
		t.Fatal(err)
	}
	page1, err := e.Fill(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	page2, _ := e.Fill(ctx, r)
	m1, m2 := stateRE.FindStringSubmatch(string(page1)), stateRE.FindStringSubmatch(string(page2))
	if m1 == nil || m2 == nil || m1[1] == m2[1] || store.Len() != 2 {
		t.Fatalf("each page view gets its own state: %s | %s (%d)", page1, page2, store.Len())
	}
	if !strings.Contains(string(page1), `<span>0</span><button brick:click="increment">+2</button>`) {
		t.Fatalf("first render: %s", page1)
	}
	h := e.ActionHandler()
	post := func(body string, headers bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/_brick/action", bytes.NewBufferString(body))
		if headers {
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Brick-Action", "1")
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	call := func(action string, args ...any) *httptest.ResponseRecorder {
		a := "[]"
		if len(args) > 0 {
			a = fmt.Sprintf("[%v]", args[0])
		}
		return post(fmt.Sprintf(`{"handle":%q,"token":%q,"action":%q,"args":%s}`, m1[1], m1[2], action, a), true)
	}
	if rec := call("increment"); rec.Code != 200 || !strings.Contains(rec.Body.String(), "<span>2</span>") || !strings.Contains(rec.Body.String(), m1[1]) {
		t.Fatalf("increment: %d %s", rec.Code, rec.Body)
	}
	if rec := call("bump", 10); rec.Code != 200 || !strings.Contains(rec.Body.String(), "<span>12</span>") {
		t.Fatalf("bump: %d %s", rec.Code, rec.Body)
	}
	// The other page view's state is untouched.
	other := post(fmt.Sprintf(`{"handle":%q,"token":%q,"action":"increment","args":[]}`, m2[1], m2[2]), true)
	if !strings.Contains(other.Body.String(), "<span>2</span>") {
		t.Fatalf("second view: %s", other.Body)
	}
	for name, tc := range map[string]struct {
		rec  *httptest.ResponseRecorder
		code int
	}{
		"wrong token": {post(fmt.Sprintf(`{"handle":%q,"token":"00","action":"increment"}`, m1[1]), true), 403},
		"unknown":     {call("nope"), 400},
		"private":     {call("_private"), 400},
		"bad result":  {call("broken"), 422},
		"gone":        {post(`{"handle":"ff","token":"00","action":"increment"}`, true), 410},
		"no header":   {post(`{}`, false), 400},
		"bad json":    {post(`{`, true), 400},
	} {
		if tc.rec.Code != tc.code {
			t.Errorf("%s: %d %s", name, tc.rec.Code, tc.rec.Body)
		}
	}
	get := httptest.NewRecorder()
	h.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/_brick/action", nil))
	if get.Code != 405 {
		t.Fatalf("GET: %d", get.Code)
	}
}

func TestStatefulNeedsStoreAndSlotRules(t *testing.T) {
	t.Parallel()
	e := newEngine(t, Options{Scripting: counterScripting(), FS: theme(
		"components/Counter.html", counterFile,
		"content/a.html", `<Counter />`,
		"content/kids.html", `<Counter><Counter /></Counter>`,
		"content/plain.html", `<button brick:click="x">+</button>`,
	)})
	if err := renderErr(t, e, "content/a.html"); !strings.Contains(err.Error(), "Options.States") {
		t.Fatalf("no store: %v", err)
	}
	if err := renderErr(t, e, "content/kids.html"); !strings.Contains(err.Error(), "inside the children of another one") {
		t.Fatalf("children: %v", err)
	}
	if err := renderErr(t, e, "content/plain.html"); !strings.Contains(err.Error(), "declares state:") {
		t.Fatalf("event outside a stateful component: %v", err)
	}
}

func TestMemoryStates(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m := NewMemoryStates(2, time.Minute)
	now := time.Unix(0, 0)
	m.now = func() time.Time { return now }
	for _, h := range []string{"a", "b"} {
		_, _ = m.Put(ctx, &Instance{Handle: h, State: map[string]any{"n": 1}})
	}
	if _, ok, _ := m.Get(ctx, "a"); !ok { // a is now most recent
		t.Fatal("a missing")
	}
	_, _ = m.Put(ctx, &Instance{Handle: "c"})
	if _, ok, _ := m.Get(ctx, "b"); ok {
		t.Fatal("least recently used b was not evicted")
	}
	got, _, _ := m.Get(ctx, "a")
	got.State["n"] = 99
	again, _, _ := m.Get(ctx, "a")
	if again.State["n"] != 1 {
		t.Fatal("Get returned shared state")
	}
	now = now.Add(2 * time.Minute)
	if _, ok, _ := m.Get(ctx, "a"); ok {
		t.Fatal("expired instance returned")
	}
	_ = m.Delete(ctx, "c")
	if m.Len() != 0 {
		t.Fatalf("len %d", m.Len())
	}
}

func TestBindingsServerSide(t *testing.T) {
	t.Parallel()
	sc := fakeScripting{modules: map[string]map[string]func(args ...any) (any, error){
		"form": {"save": func(a ...any) (any, error) {
			st, _ := a[0].(map[string]any)
			if n, _ := st["name"].(string); len(n) < 2 {
				return []any{st, map[string]any{"name": "Too short"}}, nil
			}
			return st, nil
		}},
	}}
	e := newEngine(t, Options{States: NewMemoryStates(0, 0), Scripting: sc, FS: theme(
		"components/Form.html", "---\nstate:\n  name: \"\"\n  qty: 1\n  agree: false\n---\n<brick:script>form</brick:script>\n<form brick:submit=\"save\"><input brick:model=\"name\" value=\"{{ .State.name }}\"><i>{{ .State.qty }}|{{ .State.agree }}</i>{{ with .Errors.name }}<p class=\"error\">{{ . }}</p>{{ end }}<span brick:loading hidden>…</span></form>",
		"content/a.html", `<Form />`,
	)})
	ctx := context.Background()
	page, err := e.Render(ctx, "content/a.html", nil)
	if err != nil {
		t.Fatal(err)
	}
	m := stateRE.FindStringSubmatch(string(page))
	act := func(action string, args ...any) (string, error) {
		out, err := e.Action(ctx, ActionRequest{Handle: m[1], Token: m[2], Action: action, Args: args})
		return string(out), err
	}
	if out, err := act("$set", "qty", "7"); err != nil || !strings.Contains(out, "<i>7|false</i>") {
		t.Fatalf("$set coerces to the initial type: %s %v", out, err)
	}
	if out, err := act("$set", "agree", true); err != nil || !strings.Contains(out, "<i>7|true</i>") {
		t.Fatalf("$set bool: %s %v", out, err)
	}
	// A malformed call is an unknown action (400); a refused field or
	// value is ErrBadValue (422).
	for _, bad := range [][]any{{"admin", true}, {"qty", "many"}} {
		if _, err := act("$set", bad...); !errors.Is(err, ErrBadValue) {
			t.Errorf("$set %v: %v", bad, err)
		}
	}
	if _, err := act("$set", "qty"); !errors.Is(err, ErrUnknownAction) {
		t.Errorf("$set qty: %v", err)
	}
	act("$set", "name", "A")
	out, err := act("save")
	if err != nil || !strings.Contains(out, `<p class="error">Too short</p>`) || !strings.Contains(out, `value="A"`) {
		t.Fatalf("validation errors: %s %v", out, err)
	}
	if out, _ := act("$refresh"); strings.Contains(out, "Too short") {
		t.Fatalf("errors last one render: %s", out)
	}
	act("$set", "name", "Alia")
	if out, _ := act("save"); strings.Contains(out, "error") {
		t.Fatalf("valid save: %s", out)
	}
}

// A re-render after an action runs the loader again: the instance keeps
// only the call site's props, never the loader's last result.
func TestRefreshRerunsLoader(t *testing.T) {
	t.Parallel()
	n := 0
	sc := fakeScripting{modules: map[string]map[string]func(args ...any) (any, error){
		"clock": {"tick": func(...any) (any, error) { n++; return map[string]any{"time": fmt.Sprint(n)}, nil }},
	}}
	e := newEngine(t, Options{States: NewMemoryStates(0, 0), Scripting: sc, FS: theme(
		"components/Clock.html", "---\nload: tick\nstate:\n  x: 0\n---\n<brick:script>clock</brick:script>\n<div><time>{{ .Props.time }}</time></div>",
		"content/a.html", `<Clock />`,
	)})
	ctx := context.Background()
	page, err := e.Render(ctx, "content/a.html", nil)
	if err != nil || !strings.Contains(string(page), "<time>1</time>") {
		t.Fatalf("first: %s %v", page, err)
	}
	m := stateRE.FindStringSubmatch(string(page))
	for want := 2; want <= 3; want++ {
		out, err := e.Action(ctx, ActionRequest{Handle: m[1], Token: m[2], Action: "$refresh"})
		if err != nil || !strings.Contains(string(out), fmt.Sprintf("<time>%d</time>", want)) {
			t.Fatalf("refresh %d: %s %v", want, out, err)
		}
	}
}
