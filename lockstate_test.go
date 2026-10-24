package brick

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// closedRow is lockedComponent without locked:, plus a queried field q.
// Each test adds its own brick: settings.
func closedRow(brickKeys string) string {
	return "---\nstate:\n  editing_id: 0\n  draft: {title: \"\"}\n  open: false\n  name: \"\"\n  q: \"\"\nbrick:\n  query: [q]\n" + brickKeys + "---\n<brick:script>f</brick:script>\n<div><i>{{ .State.editing_id }}|{{ .State.draft.title }}|{{ .State.open }}|{{ .State.name }}|{{ .State.q }}</i></div>"
}

// closedEngine renders closedRow(keys) under opts and returns the request
// base and how many times save ran.
func closedEngine(t *testing.T, keys string, opts Options) (*Engine, ActionRequest, *int) {
	t.Helper()
	var mu sync.Mutex
	saves := 0
	opts.States = NewMemoryStates(0, 0)
	opts.Scripting = fakeScripting{modules: map[string]map[string]func(args ...any) (any, error){
		"f": {
			"edit": func(a ...any) (any, error) {
				st, _ := deepCopy(a[0]).(map[string]any)
				st["editing_id"] = a[2]
				st["draft"] = map[string]any{"title": "Row 7"}
				st["open"] = true
				return st, nil
			},
			"save": func(a ...any) (any, error) {
				mu.Lock()
				defer mu.Unlock()
				saves++
				return a[0], nil
			},
		},
	}}
	opts.FS = theme("components/Row.html", closedRow(keys), "content/a.html", `<Row />`)
	e := newEngine(t, opts)
	page, err := e.Render(context.Background(), "content/a.html", nil)
	if err != nil {
		t.Fatal(err)
	}
	m := stateRE.FindStringSubmatch(string(page))
	if m == nil {
		t.Fatalf("no state attrs: %s", page)
	}
	return e, ActionRequest{Handle: m[1], Token: m[2]}, &saves
}

// closedSetups are the three ways a component ends up closed: the engine
// switch with a writable: list, writable: alone, and lock_state: true.
// name is writable in the first two only.
var closedSetups = []struct {
	name, keys   string
	opts         Options
	nameWritable bool
}{
	{"Options.LockState", "  writable: [name]\n", Options{LockState: true}, true},
	{"writable alone", "  writable: [name]\n", Options{}, true},
	{"lock_state true", "  lock_state: true\n", Options{}, false},
}

// SECURITY: in a closed component every client write to a field it does
// not list is refused through the real HTTP endpoint: a 422 naming the
// field, the action not run, the state untouched.
func TestLockStateRefusesClientWrites(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, body, field string }{
		{"$set", `"action":"$set","args":["editing_id",7]`, "editing_id"},
		{"$set nested", `"action":"$set","args":["draft.title","x"]`, "draft"},
		{"$toggle", `"action":"$toggle","args":["open"]`, "open"},
		{"$reset", `"action":"$reset","args":["editing_id"]`, "editing_id"},
		{"deferred set", `"action":"save","args":[],"sets":[["editing_id",7]]`, "editing_id"},
		{"deferred nested set", `"action":"save","args":[],"sets":[["draft.title","x"]]`, "draft"},
	}
	for _, s := range closedSetups {
		for _, c := range cases {
			t.Run(s.name+"/"+c.name, func(t *testing.T) {
				t.Parallel()
				e, base, saves := closedEngine(t, s.keys, s.opts)
				ctx := context.Background()
				if _, err := e.RunAction(ctx, ActionRequest{Handle: base.Handle, Token: base.Token, Action: "edit", Args: []any{int64(7)}}); err != nil {
					t.Fatal(err)
				}
				b := `{"handle":"` + base.Handle + `","token":"` + base.Token + `",` + c.body + `}`
				r := httptest.NewRequest(http.MethodPost, "/_brick/action", strings.NewReader(b))
				r.Header.Set("Content-Type", "application/json")
				r.Header.Set(actionHeader, "1")
				w := httptest.NewRecorder()
				e.ActionHandler().ServeHTTP(w, r)
				if w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), "state."+c.field+" is closed to the client") {
					t.Fatalf("%d %s", w.Code, w.Body)
				}
				if *saves != 0 {
					t.Fatal("the action ran after a refused write")
				}
				res, err := e.RunAction(ctx, ActionRequest{Handle: base.Handle, Token: base.Token, Action: "$refresh"})
				if err != nil {
					t.Fatal(err)
				}
				if got := body(t, res.HTML); got != "7|Row 7|true||" {
					t.Fatalf("state changed by a refused write: %q", got)
				}
			})
		}
	}
}

// What a closed component still lets through: its writable: fields, its
// query: fields (the URL writes them by declaration), and every server
// write. A writable field off the list is refused with ErrLockedField.
func TestLockStateAllows(t *testing.T) {
	t.Parallel()
	for _, s := range closedSetups {
		t.Run(s.name, func(t *testing.T) {
			t.Parallel()
			e, base, _ := closedEngine(t, s.keys, s.opts)
			ctx := context.Background()
			req := func(action string, args ...any) (*ActionResult, error) {
				return e.RunAction(ctx, ActionRequest{Handle: base.Handle, Token: base.Token, Action: action, Args: args})
			}
			if res, err := req("edit", int64(9)); err != nil || body(t, res.HTML) != "9|Row 7|true||" {
				t.Fatalf("server write: %v", err)
			}
			if res, err := req("$set", "q", "x"); err != nil || body(t, res.HTML) != "9|Row 7|true||x" {
				t.Fatalf("query field: %v", err)
			}
			_, err := req("$set", "name", "Ada")
			if s.nameWritable && err != nil {
				t.Fatalf("writable field refused: %v", err)
			}
			if !s.nameWritable && (!errors.Is(err, ErrLockedField) || !errors.Is(err, ErrBadValue)) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

// lock_state: false keeps one component open under a closed engine, and
// without either switch nothing changes.
func TestLockStateOpen(t *testing.T) {
	t.Parallel()
	for _, s := range []struct {
		name, keys string
		opts       Options
	}{
		{"lock_state false", "  lock_state: false\n", Options{LockState: true}},
		{"default off", "", Options{}},
	} {
		e, base, _ := closedEngine(t, s.keys, s.opts)
		res, err := e.RunAction(context.Background(), ActionRequest{Handle: base.Handle, Token: base.Token, Action: "$set", Args: []any{"editing_id", int64(7)}})
		if err != nil || body(t, res.HTML) != "7||false||" {
			t.Fatalf("%s: %v", s.name, err)
		}
	}
}

// An upload field is client-written by declaration, so a closed component
// never refuses it; the gate is checked on the resolved meta.
func TestLockStateUploadField(t *testing.T) {
	t.Parallel()
	m := &meta{state: map[string]any{"avatar": nil, "id": 0}, uploads: map[string]uploadSpec{"avatar": {}}, closed: true}
	if err := refuseLocked(m, "avatar"); err != nil {
		t.Fatalf("upload field refused: %v", err)
	}
	if err := refuseLocked(m, "id"); !errors.Is(err, ErrLockedField) {
		t.Fatalf("err = %v", err)
	}
}

// brick check (compile) refuses a literal binding to a field the client
// may not write, and every contradictory or malformed setting.
func TestLockStateCompileChecks(t *testing.T) {
	t.Parallel()
	comp := func(fm, html string) string {
		return "---\nstate:\n  editing_id: 0\n  draft: {title: \"\"}\n  q: \"\"\nbrick:\n" + fm + "---\n<div>" + html + "</div>"
	}
	w := "  writable: [q]\n"
	cases := []struct {
		name, src, want string
		opts            Options
	}{
		{"model", comp(w, `<input brick:model="editing_id">`), `state.editing_id is closed to the client`, Options{}},
		{"model nested", comp(w, `<input brick:model.lazy="draft.title">`), `state.draft is closed to the client`, Options{}},
		{"literal $set", comp(w, `<button brick:click='$set("editing_id", 3)'>x</button>`), `$set from the client cannot write it`, Options{}},
		{"engine default", "---\nstate:\n  q: \"\"\n---\n<div><input brick:model=\"q\"></div>", `list it in brick: {writable:}`, Options{LockState: true}},
		{"lock_state true", comp("  lock_state: true\n", `<input brick:model="q">`), `state.q is closed`, Options{}},
		{"undeclared", comp("  writable: [missing]\n", ""), "writable: missing is not a top-level field", Options{}},
		{"not a list", comp("  writable: q\n", ""), "writable: must list state fields", Options{}},
		{"also locked", comp("  writable: [q]\n  locked: [q]\n", ""), "q is also in locked:", Options{}},
		{"false with writable", comp("  lock_state: false\n  writable: [q]\n", ""), "writable: means nothing with lock_state: false", Options{}},
		{"lock_state not bool", comp("  lock_state: yes please\n", ""), "lock_state: must be true or false", Options{}},
		{"no state", "---\nbrick:\n  writable: [q]\n---\n<div></div>", "writable: needs state:", Options{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			c.opts.FS = theme("components/U.html", c.src, "content/a.html", `<U />`)
			c.opts.States = NewMemoryStates(0, 0)
			_, err := newEngineErr(c.opts, "content/a.html")
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want %q", err, c.want)
			}
		})
	}
	ok := comp(w, `<input brick:model="q"><button brick:click='$set("q", "x")'>x</button><button brick:click='$set("{{ .State.q }}", 1)'>y</button>`)
	if _, err := newEngineErr(Options{LockState: true, FS: theme("components/U.html", ok, "content/a.html", `<U />`), States: NewMemoryStates(0, 0)}, "content/a.html"); err != nil {
		t.Fatalf("writable bindings refused: %v", err)
	}
	// A stateless file is untouched by the engine switch.
	if _, err := newEngineErr(Options{LockState: true, FS: theme("components/U.html", "<div></div>", "content/a.html", `<U />`)}, "content/a.html"); err != nil {
		t.Fatalf("stateless component: %v", err)
	}
}

// brick.yaml lockState reaches Options.LockState.
func TestConfigLockState(t *testing.T) {
	t.Parallel()
	on := true
	var o Options
	if err := (Config{LockState: &on}).Apply(&o); err != nil || !o.LockState {
		t.Fatalf("LockState = %v, err %v", o.LockState, err)
	}
}
