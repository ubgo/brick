package brick

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
)

// formTheme is a component with every shape a form binds: a string, a
// number, a list of scalars, a dict and a list of dicts.
const formComponent = "---\nstate:\n  name: \"\"\n  qty: 1\n  tags: []\n  address: {city: \"\", zip: \"\"}\n  items: [{name: Pen, qty: 1}, {name: Ink, qty: 2}]\n---\n<brick:script>f</brick:script>\n<form brick:submit=\"save\"><i>{{ .State.name }}|{{ .State.qty }}|{{ .State.address.city }}|{{ range .State.items }}{{ .qty }},{{ end }}|{{ range .State.tags }}{{ . }};{{ end }}</i></form>"

// formEngine renders the form component and returns its handle and token,
// and a pointer to the state the last save saw.
func formEngine(t *testing.T, mw ...ActionMiddleware) (*Engine, ActionRequest, *map[string]any, *int) {
	t.Helper()
	var mu sync.Mutex
	var seen map[string]any
	calls := 0
	sc := fakeScripting{modules: map[string]map[string]func(args ...any) (any, error){
		"f": {"save": func(a ...any) (any, error) {
			mu.Lock()
			defer mu.Unlock()
			calls++
			seen, _ = deepCopy(a[0]).(map[string]any)
			return a[0], nil
		}},
	}}
	e := newEngine(t, Options{States: NewMemoryStates(0, 0), Scripting: sc, ActionMiddleware: mw, FS: theme(
		"components/Form.html", formComponent,
		"content/a.html", `<Form />`,
	)})
	page, err := e.Render(context.Background(), "content/a.html", nil)
	if err != nil {
		t.Fatal(err)
	}
	m := stateRE.FindStringSubmatch(string(page))
	if m == nil {
		t.Fatalf("no state attrs: %s", page)
	}
	return e, ActionRequest{Handle: m[1], Token: m[2]}, &seen, &calls
}

func body(t *testing.T, html []byte) string {
	t.Helper()
	s := string(html)
	i, j := strings.Index(s, "<i>"), strings.Index(s, "</i>")
	if i < 0 || j < 0 {
		t.Fatalf("no <i> in %s", s)
	}
	return s[i+3 : j]
}

// Sets land before the action, in one request and one version bump
// .
func TestSetsAppliedBeforeAction(t *testing.T) {
	t.Parallel()
	e, base, seen, _ := formEngine(t)
	ctx := context.Background()
	r0, err := e.RunAction(ctx, ActionRequest{Handle: base.Handle, Token: base.Token, Action: "$refresh"})
	if err != nil {
		t.Fatal(err)
	}
	req := ActionRequest{Handle: base.Handle, Token: base.Token, Action: "save", Sets: []FieldSet{
		{Path: "name", Value: "Ada"}, {Path: "qty", Value: "3"}, {Path: "address.city", Value: "Paris"},
		{Path: "items.1.qty", Value: "5"}, {Path: "tags", Value: []any{"go", "js"}},
	}}
	res, err := e.RunAction(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if got := body(t, res.HTML); got != "Ada|3|Paris|1,5,|go;js;" {
		t.Fatalf("render: %q", got)
	}
	if (*seen)["name"] != "Ada" || (*seen)["qty"] != int64(3) {
		t.Fatalf("the action did not see the sets: %v", *seen)
	}
	if res.Version != r0.Version+1 {
		t.Fatalf("version %d after %d: sets and the action are one bump", res.Version, r0.Version)
	}
}

// One refused set refuses the whole request: the action does not run and
// no earlier set in the list sticks (D5).
func TestSetsRefusedAllOrNothing(t *testing.T) {
	t.Parallel()
	e, base, _, calls := formEngine(t)
	ctx := context.Background()
	bad := map[string]FieldSet{
		"undeclared":        {Path: "role", Value: "admin"},
		"wrong type":        {Path: "qty", Value: "many"},
		"missing key":       {Path: "address.country", Value: "FR"},
		"index past end":    {Path: "items.2.qty", Value: 1},
		"negative index":    {Path: "items.-1.qty", Value: 1},
		"padded index":      {Path: "items.01.qty", Value: 1},
		"new key on row":    {Path: "items.0.price", Value: 1},
		"dict replaced":     {Path: "address", Value: map[string]any{"city": "x", "admin": true}},
		"rows replaced":     {Path: "items", Value: []any{map[string]any{"qty": 9}}},
		"row replaced":      {Path: "items.0", Value: map[string]any{"qty": 9}},
		"list not a list":   {Path: "tags", Value: "go"},
		"list of dicts":     {Path: "tags", Value: []any{map[string]any{"x": 1}}},
		"through a scalar":  {Path: "name.x", Value: 1},
		"empty segment":     {Path: "address..city", Value: "x"},
		"nested wrong type": {Path: "items.0.qty", Value: "lots"},
	}
	for name, set := range bad {
		req := ActionRequest{Handle: base.Handle, Token: base.Token, Action: "save", Sets: []FieldSet{{Path: "name", Value: "Ada"}, set}}
		if _, err := e.RunAction(ctx, req); !errors.Is(err, ErrBadValue) {
			t.Errorf("%s: %v, want ErrBadValue", name, err)
		}
	}
	if *calls != 0 {
		t.Fatalf("the action ran %d times behind a refused set", *calls)
	}
	res, err := e.RunAction(ctx, ActionRequest{Handle: base.Handle, Token: base.Token, Action: "$refresh"})
	if err != nil {
		t.Fatal(err)
	}
	if got := body(t, res.HTML); got != "|1||1,2,|" {
		t.Fatalf("a refused request changed state: %q", got)
	}
}

// $set takes nested paths and lists through the same gate.
func TestSetNestedAndLists(t *testing.T) {
	t.Parallel()
	e, base, _, _ := formEngine(t)
	ctx := context.Background()
	set := func(args ...any) (string, error) {
		res, err := e.RunAction(ctx, ActionRequest{Handle: base.Handle, Token: base.Token, Action: "$set", Args: args})
		if err != nil {
			return "", err
		}
		return body(t, res.HTML), nil
	}
	if got, err := set("items.0.qty", 7.0); err != nil || got != "|1||7,2,|" {
		t.Fatalf("nested list index: %q %v", got, err)
	}
	if got, err := set("tags", []any{"go", int64(1), true}); err != nil || got != "|1||7,2,|go;1;true;" {
		t.Fatalf("list coerced element-wise to strings: %q %v", got, err)
	}
	if got, err := set("tags", []any{}); err != nil || got != "|1||7,2,|" {
		t.Fatalf("empty list: %q %v", got, err)
	}
	if _, err := set("address.admin", true); !errors.Is(err, ErrBadValue) {
		t.Fatalf("a client added a key: %v", err)
	}
	if _, err := set("qty"); !errors.Is(err, ErrUnknownAction) {
		t.Fatalf("malformed $set: %v", err)
	}
}

// A middleware sees the sets and may drop one; the core applies what it
// leaves.
func TestSetsVisibleToMiddleware(t *testing.T) {
	t.Parallel()
	var got []FieldSet
	policy := func(next ActionFunc) ActionFunc {
		return func(ctx context.Context, a *ActionCall) (*ActionResult, error) {
			got = slices.Clone(a.Sets)
			a.Sets = slices.DeleteFunc(a.Sets, func(s FieldSet) bool { return s.Path == "qty" })
			return next(ctx, a)
		}
	}
	e, base, _, _ := formEngine(t, policy)
	res, err := e.RunAction(context.Background(), ActionRequest{Handle: base.Handle, Token: base.Token, Action: "save", Sets: []FieldSet{{Path: "name", Value: "Ada"}, {Path: "qty", Value: 9}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[1].Path != "qty" {
		t.Fatalf("middleware saw %v", got)
	}
	if b := body(t, res.HTML); b != "Ada|1||1,2,|" {
		t.Fatalf("the dropped set was applied: %q", b)
	}
}

// Over HTTP, sets arrive as [[path, value], …] and a refused one is 422.
func TestSetsOverHTTP(t *testing.T) {
	t.Parallel()
	e, base, _, _ := formEngine(t)
	h := e.ActionHandler()
	post := func(sets string) *httptest.ResponseRecorder {
		b := `{"handle":"` + base.Handle + `","token":"` + base.Token + `","action":"save","args":[],"sets":` + sets + `}`
		r := httptest.NewRequest(http.MethodPost, "/_brick/action", strings.NewReader(b))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set(actionHeader, "1")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if w := post(`[["items.0.qty", "4"], ["address.city", "Oslo"]]`); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "|Oslo|4,2,|") {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if w := post(`[["items.5.qty", 1]]`); w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("out of range: %d %s", w.Code, w.Body)
	}
	if w := post(`[["name"]]`); w.Code != http.StatusBadRequest {
		t.Fatalf("malformed set: %d %s", w.Code, w.Body)
	}
}

func TestFieldSetJSON(t *testing.T) {
	t.Parallel()
	var sets []FieldSet
	if err := json.Unmarshal([]byte(`[["a.b", 3], ["c", [1.5, "x"]]]`), &sets); err != nil {
		t.Fatal(err)
	}
	if sets[0].Path != "a.b" || sets[0].Value != int64(3) || sets[1].Value.([]any)[0] != 1.5 {
		t.Fatalf("%#v", sets)
	}
	b, _ := json.Marshal(sets)
	if string(b) != `[["a.b",3],["c",[1.5,"x"]]]` {
		t.Fatalf("%s", b)
	}
	for _, bad := range []string{`[{"path":"a"}]`, `[[1, 2]]`, `[["a"]]`, `[["a", 1, 2]]`} {
		if err := json.Unmarshal([]byte(bad), &sets); err == nil {
			t.Errorf("%s decoded", bad)
		}
	}
}

// Every stateful root names its list-valued paths (D3), nested dicts
// included, so a one-option checkbox group still binds as a list.
func TestArrayPathsOnRoot(t *testing.T) {
	t.Parallel()
	e := newEngine(t, Options{States: NewMemoryStates(0, 0), FS: theme(
		"components/F.html", "---\nstate:\n  tags: []\n  name: \"\"\n  filters: {status: [open], q: \"\", deep: {ids: [1]}}\n---\n<div>x</div>",
		"components/G.html", "---\nstate:\n  n: 1\n---\n<div>x</div>",
		"content/a.html", `<F /><G />`,
	)})
	page, err := e.Render(context.Background(), "content/a.html", nil)
	if err != nil {
		t.Fatal(err)
	}
	s := string(page)
	if !strings.Contains(s, `data-brick-arrays="filters.deep.ids filters.status tags"`) {
		t.Fatalf("arrays: %s", s)
	}
	if strings.Count(s, "data-brick-arrays") != 1 {
		t.Fatalf("a root with no lists carries the attribute: %s", s)
	}
}
