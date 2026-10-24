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

// lockedComponent is a list row editor: editing_id is the row the server
// chose to edit, draft its loaded copy, open a server-owned flag. A client
// that could $set editing_id would then save another user's row.
const lockedComponent = "---\nstate:\n  editing_id: 0\n  draft: {title: \"\"}\n  open: false\n  name: \"\"\nbrick: {locked: [editing_id, draft, open]}\n---\n<brick:script>f</brick:script>\n<div><i>{{ .State.editing_id }}|{{ .State.draft.title }}|{{ .State.open }}|{{ .State.name }}</i></div>"

// lockedEngine renders the locked component and returns the request base
// and how many times save ran.
func lockedEngine(t *testing.T) (*Engine, ActionRequest, *int) {
	t.Helper()
	var mu sync.Mutex
	saves := 0
	sc := fakeScripting{modules: map[string]map[string]func(args ...any) (any, error){
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
	e := newEngine(t, Options{States: NewMemoryStates(0, 0), Scripting: sc, FS: theme(
		"components/Row.html", lockedComponent,
		"content/a.html", `<Row />`,
	)})
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

// Every way a client writes state is refused on a locked field, nested
// paths included, through the real HTTP endpoint: a 422 naming the field,
// the action not run, the state untouched. SECURITY: a client must never
// choose the row an action trusts.
func TestLockedRefusesClientWrites(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, body, field string
	}{
		{"$set", `"action":"$set","args":["editing_id",7]`, "editing_id"},
		{"$set nested", `"action":"$set","args":["draft.title","x"]`, "draft"},
		{"$toggle", `"action":"$toggle","args":["open"]`, "open"},
		{"$reset", `"action":"$reset","args":["name","editing_id"]`, "editing_id"},
		{"deferred set", `"action":"save","args":[],"sets":[["name","Ada"],["editing_id",7]]`, "editing_id"},
		{"deferred nested set", `"action":"save","args":[],"sets":[["draft.title","x"]]`, "draft"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			e, base, saves := lockedEngine(t)
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
			if w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), "state."+c.field+" is locked") {
				t.Fatalf("%d %s", w.Code, w.Body)
			}
			if *saves != 0 {
				t.Fatal("the action ran after a refused write")
			}
			res, err := e.RunAction(ctx, ActionRequest{Handle: base.Handle, Token: base.Token, Action: "$refresh"})
			if err != nil {
				t.Fatal(err)
			}
			if got := body(t, res.HTML); got != "7|Row 7|true|" {
				t.Fatalf("state changed by a refused write: %q", got)
			}
		})
	}
}

// The lock binds the client only: actions write a locked field, unlocked
// fields stay client-settable, and the error is ErrLockedField, which is an
// ErrBadValue.
func TestLockedServerWritesAndUnlockedFields(t *testing.T) {
	t.Parallel()
	e, base, _ := lockedEngine(t)
	ctx := context.Background()
	req := func(action string, args ...any) (*ActionResult, error) {
		return e.RunAction(ctx, ActionRequest{Handle: base.Handle, Token: base.Token, Action: action, Args: args})
	}
	if res, err := req("edit", int64(9)); err != nil || body(t, res.HTML) != "9|Row 7|true|" {
		t.Fatalf("server write: %v", err)
	}
	if res, err := req("$set", "name", "Ada"); err != nil || body(t, res.HTML) != "9|Row 7|true|Ada" {
		t.Fatalf("unlocked $set: %v", err)
	}
	_, err := req("$set", "editing_id", int64(1))
	if !errors.Is(err, ErrLockedField) || !errors.Is(err, ErrBadValue) {
		t.Fatalf("err = %v", err)
	}
}

// Without brick: {locked:} nothing changes: the same $set succeeds.
func TestLockedOptIn(t *testing.T) {
	t.Parallel()
	src := strings.Replace(lockedComponent, "brick: {locked: [editing_id, draft, open]}\n", "", 1)
	e := newEngine(t, Options{States: NewMemoryStates(0, 0), Scripting: fakeScripting{modules: map[string]map[string]func(args ...any) (any, error){"f": {}}}, FS: theme("components/Row.html", src, "content/a.html", `<Row />`)})
	page, err := e.Render(context.Background(), "content/a.html", nil)
	if err != nil {
		t.Fatal(err)
	}
	m := stateRE.FindStringSubmatch(string(page))
	res, err := e.RunAction(context.Background(), ActionRequest{Handle: m[1], Token: m[2], Action: "$set", Args: []any{"editing_id", int64(7)}})
	if err != nil || body(t, res.HTML) != "7||false|" {
		t.Fatalf("unlocked: %v", err)
	}
}

// brick check (compile) refuses a binding or literal call that would be
// refused at run time, and a locked: that names no field or contradicts
// query:. A filled {{ }} value is left to the runtime gate.
func TestLockedCompileChecks(t *testing.T) {
	t.Parallel()
	comp := func(fm, html string) string {
		return "---\nstate:\n  editing_id: 0\n  draft: {title: \"\"}\n  q: \"\"\nbrick:\n" + fm + "---\n<div>" + html + "</div>"
	}
	locked := "  locked: [editing_id, draft]\n"
	cases := []struct{ name, src, want string }{
		{"model", comp(locked, `<input brick:model="editing_id">`), `state.editing_id is locked`},
		{"model nested", comp(locked, `<input brick:model.lazy="draft.title">`), `state.draft is locked`},
		{"literal $set", comp(locked, `<button brick:click='$set("editing_id", 3)'>x</button>`), `$set from the client cannot write it`},
		{"literal $reset", comp(locked, `<button brick:click='$reset("q", "draft.title")'>x</button>`), `state.draft is locked`},
		{"literal $toggle", comp(locked, `<button brick:change='$toggle("editing_id")'>x</button>`), `state.editing_id is locked`},
		{"undeclared", comp("  locked: [missing]\n", ""), "missing is not a top-level field"},
		{"not a list", comp("  locked: editing_id\n", ""), "must list state fields"},
		{"queried", comp("  locked: [q]\n  query: [q]\n", ""), "q is a query field"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			_, err := newEngineErr(Options{FS: theme("components/U.html", c.src, "content/a.html", `<U />`), States: NewMemoryStates(0, 0)}, "content/a.html")
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want %q", err, c.want)
			}
		})
	}
	ok := comp(locked, `<input brick:model="q"><button brick:click='$set("q", "x")'>x</button><button brick:click='$set("{{ .State.q }}", 1)'>y</button>`)
	if _, err := newEngineErr(Options{FS: theme("components/U.html", ok, "content/a.html", `<U />`), States: NewMemoryStates(0, 0)}, "content/a.html"); err != nil {
		t.Fatalf("unlocked bindings refused: %v", err)
	}
}

// A locked upload field contradicts uploads: (the client writes it by
// uploading), so the file fails.
func TestLockedUploadField(t *testing.T) {
	t.Parallel()
	src := "---\nstate:\n  avatar: null\nbrick:\n  uploads:\n    avatar: {}\n  locked: [avatar]\n---\n<div></div>"
	_, err := newEngineErr(Options{FS: theme("components/U.html", src, "content/a.html", `<U />`), States: NewMemoryStates(0, 0), Uploads: newMemSink()}, "content/a.html")
	if err == nil || !strings.Contains(err.Error(), "avatar is an upload field") {
		t.Fatalf("err = %v", err)
	}
}
