package brick

import (
	"context"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"
)

// paramsCalls records what the brick: {params:} hook received.
type paramsCalls struct {
	mu   sync.Mutex
	seen []map[string]any
}

func (c *paramsCalls) all() []map[string]any {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]map[string]any(nil), c.seen...)
}

// paramsMaxPage is the catalog's last page: the hook clamps to it.
const paramsMaxPage = 5

// paramsEngine has one catalog component whose tab and page are URL
// parameters and whose params hook clamps the page and records each call.
func paramsEngine(t *testing.T, calls *paramsCalls) *Engine {
	t.Helper()
	comp := "---\nstate:\n  tab: all\n  page: 1\n  note: \"\"\nbrick: {query: [tab, page], params: on_params}\n---\n<brick:script>cat</brick:script>\n<div>tab={{ .State.tab }}|page={{ .State.page }}|note={{ .State.note }}</div>"
	sc := ctxScripting{"cat": {
		"on_params": func(ctx context.Context, a ...any) (any, error) {
			st, _ := a[0].(map[string]any)
			p, _ := a[2].(map[string]any)
			calls.mu.Lock()
			calls.seen = append(calls.seen, p)
			calls.mu.Unlock()
			if n, _ := st["page"].(int64); n > paramsMaxPage {
				st["page"], st["note"] = int64(paramsMaxPage), "clamped"
			}
			return st, nil
		},
		"bump": func(ctx context.Context, a ...any) (any, error) {
			st, _ := a[0].(map[string]any)
			n, _ := st["page"].(int64)
			st["page"] = n + 1
			return st, nil
		},
	}}
	return newEngine(t, Options{FS: theme("components/Cat.html", comp, "content/a.html", `<Cat />`), Scripting: sc, States: NewMemoryStates(0, 0)})
}

// queryEffectOf is the query effect of an action answer, or nil.
func queryEffectOf(env map[string]any) map[string]any {
	effs, _ := env["effects"].([]any)
	for _, ef := range effs {
		if m, _ := ef.(map[string]any); m["kind"] == EffectQuery {
			return m
		}
	}
	return nil
}

// The hook runs once when the instance is created, after the URL seeded
// it, and sees the component's own parameters as the address bar shows
// them; a parameter at its declared value is absent.
func TestParamsHookOnCreate(t *testing.T) {
	t.Parallel()
	calls := &paramsCalls{}
	e := paramsEngine(t, calls)
	page, _, _ := fillPage(t, e, WithQuery(url.Values{"page": {"9"}, "other": {"x"}}))
	if !strings.Contains(page, "tab=all|page=5|note=clamped") {
		t.Fatalf("params on create: %s", page)
	}
	if got := calls.all(); len(got) != 1 || !reflect.DeepEqual(got[0], map[string]any{"page": "9"}) {
		t.Fatalf("hook calls: %v (want one call with only its own, non-default parameter)", got)
	}
}

// $params applies the URL's values, runs the hook, and writes the URL back
// only for the hook's own correction, replacing the entry. $refresh and
// named actions never run the hook.
func TestParamsBuiltin(t *testing.T) {
	t.Parallel()
	calls := &paramsCalls{}
	e := paramsEngine(t, calls)
	_, h, tok := fillPage(t, e)
	env := envelopeOf(t, v8bPost(e, h, tok, actionParams, nil, [][2]any{{"tab", "books"}, {"page", "3"}}, ""))
	html, _ := env["html"].(string)
	if !strings.Contains(html, "tab=books|page=3|") {
		t.Fatalf("$params: %s", html)
	}
	if got := calls.all(); len(got) != 2 || !reflect.DeepEqual(got[1], map[string]any{"tab": "books", "page": "3"}) {
		t.Fatalf("hook calls: %v", got)
	}
	if ef := queryEffectOf(env); ef != nil {
		t.Fatalf("the URL already says this; no query effect: %v", ef)
	}
	h = stateRE.FindStringSubmatch(html)[1]
	env = envelopeOf(t, v8bPost(e, h, tok, actionParams, nil, [][2]any{{"page", "9"}}, ""))
	ef := queryEffectOf(env)
	if ef == nil || ef["replace"] != true || !reflect.DeepEqual(ef["data"], map[string]any{"set": map[string]any{"page": "5"}}) {
		t.Fatalf("a correction replaces the entry: %v", ef)
	}
	h = stateRE.FindStringSubmatch(env["html"].(string))[1]
	n := len(calls.all())
	env = envelopeOf(t, v8bPost(e, h, tok, actionRefresh, nil, [][2]any{{"page", "2"}}, ""))
	h = stateRE.FindStringSubmatch(env["html"].(string))[1]
	envelopeOf(t, v8bPost(e, h, tok, "bump", nil, nil, ""))
	if len(calls.all()) != n {
		t.Fatalf("$refresh or a named action ran the params hook: %v", calls.all()[n:])
	}
}

// The compile check: params: needs query: (it would never run) and must
// name one of the file's functions.
func TestParamsCompileCheck(t *testing.T) {
	t.Parallel()
	sc := ctxScripting{"cat": {"on_params": func(ctx context.Context, a ...any) (any, error) { return a[0], nil }}}
	cases := map[string]string{
		"brick: {params: on_params}":               "params: needs query:",
		"brick: {query: [page], params: nope}":     "params:",
		"brick: {query: [page], params: \"$set\"}": "must name one of the component's script functions",
	}
	for b, want := range cases {
		comp := "---\nstate:\n  page: 1\n" + b + "\n---\n<brick:script>cat</brick:script>\n<div>{{ .State.page }}</div>"
		e := newEngine(t, Options{FS: theme("components/Cat.html", comp, "content/a.html", `<Cat />`), Scripting: sc, States: NewMemoryStates(0, 0)})
		_, err := e.RenderChunks(context.Background(), "content/a.html", nil)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err = %v, want %q", b, err, want)
		}
	}
}
