package brick

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"
)

// Tests for renderless actions and replies
// init/updated/$reset, URL-synced state and remembered state. BV in a comment marks a break-verified test.

// v8bCalls records what the hooks saw, for assertions.
type v8bCalls struct {
	mu      sync.Mutex
	updated [][]any
	inits   int
}

func (c *v8bCalls) addUpdated(p []any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.updated = append(c.updated, p)
}

// v8bScripting is the list component's script.
func v8bScripting(calls *v8bCalls) ctxScripting {
	state := func(a []any) map[string]any { st, _ := a[0].(map[string]any); return st }
	return ctxScripting{
		"list": {
			"setup": func(ctx context.Context, a ...any) (any, error) {
				calls.mu.Lock()
				calls.inits++
				calls.mu.Unlock()
				st := state(a)
				props, _ := a[1].(map[string]any)
				st["title"] = fmt.Sprintf("post-%v", props["id"])
				st["sort"] = "from-init"
				return st, nil
			},
			"noisy": func(ctx context.Context, a ...any) (any, error) {
				return nil, EffectsFrom(ctx).Dispatch("seeded", nil, "")
			},
			"broken": func(ctx context.Context, a ...any) (any, error) {
				return nil, errors.New("record not found")
			},
			"tidy": func(ctx context.Context, a ...any) (any, error) {
				paths, _ := a[2].([]any)
				calls.addUpdated(paths)
				st := state(a)
				q, _ := st["q"].(string)
				st["q"] = strings.ToLower(strings.TrimSpace(q))
				if st["q"] == "bad" {
					return []any{st, map[string]any{"q": "no bad words"}}, nil
				}
				return st, nil
			},
			"bump": func(ctx context.Context, a ...any) (any, error) {
				st := state(a)
				n, _ := st["count"].(int64)
				st["count"] = n + 1
				return st, nil
			},
			"suggest": func(ctx context.Context, a ...any) (any, error) {
				eff := EffectsFrom(ctx)
				if err := eff.SkipRender(); err != nil {
					return nil, err
				}
				if err := eff.Reply([]any{"ada", "bob"}); err != nil {
					return nil, err
				}
				st := state(a)
				n, _ := st["count"].(int64)
				st["count"] = n + 1
				return st, nil
			},
			"replyFail": func(ctx context.Context, a ...any) (any, error) {
				eff := EffectsFrom(ctx)
				_ = eff.SkipRender()
				_ = eff.Reply("x")
				return nil, errors.New("db down")
			},
			"badReply": func(ctx context.Context, a ...any) (any, error) {
				return state(a), EffectsFrom(ctx).Reply(map[string]any{"f": func() {}})
			},
			"setPage": func(ctx context.Context, a ...any) (any, error) {
				st := state(a)
				st["page"] = a[2]
				return st, nil
			},
			"setView": func(ctx context.Context, a ...any) (any, error) {
				st := state(a)
				st["view"] = a[2]
				return st, nil
			},
		},
	}
}

// v8bList is the list component with the given brick: settings.
func v8bList(brick string) string {
	return "---\nprops:\n  id: {type: int, default: 7}\nstate:\n  q: \"\"\n  page: 1\n  status: []\n  title: \"\"\n  sort: \"name\"\n  view: \"grid\"\n  count: 0\nbrick: " + brick + "\n---\n<brick:script>list</brick:script>\n<div class=\"list\">q={{ .State.q }}|page={{ .State.page }}|status={{ .State.status }}|title={{ .State.title }}|sort={{ .State.sort }}|view={{ .State.view }}|count={{ .State.count }}|err={{ .Errors.q }}</div>"
}

func v8bEngine(t *testing.T, opts Options, calls *v8bCalls, brick string, files ...string) *Engine {
	t.Helper()
	if calls == nil {
		calls = &v8bCalls{}
	}
	opts.FS = theme(append([]string{"components/List.html", v8bList(brick), "content/a.html", `<List />`}, files...)...)
	opts.Scripting = v8bScripting(calls)
	if opts.States == nil {
		opts.States = NewMemoryStates(0, 0)
	}
	return newEngine(t, opts)
}

// v8bFill renders and fills a.html with opts.
func v8bFill(t *testing.T, e *Engine, opts ...RenderOption) (page, handle, token string) {
	t.Helper()
	return fillPage(t, e, opts...)
}

// v8bPost posts one action with args, sets and the envelope Accept.
func v8bPost(e *Engine, handle, token, action string, args []any, sets [][2]any, session string) *httptest.ResponseRecorder {
	body := map[string]any{"handle": handle, "token": token, "action": action, "args": args}
	if args == nil {
		body["args"] = []any{}
	}
	if len(sets) > 0 {
		body["sets"] = sets
	}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/_brick/action", strings.NewReader(string(b)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Brick-Action", "1")
	req.Header.Set("Accept", EffectsContentType)
	if session != "" {
		req.Header.Set("X-Session", session)
	}
	rec := httptest.NewRecorder()
	e.ActionHandler().ServeHTTP(rec, req)
	return rec
}

// envelopeOf decodes an action answer's envelope.
func envelopeOf(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var env map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("envelope: %v: %s", err, rec.Body)
	}
	return env
}

// ---- 8 · renderless actions and replies ------------------------------------

// BV: a renderless action stores its state and answers with the new handle
// instead of HTML; with a sealed store the handle changes, and the next
// action must run on it (breaking the envelope's handle leaves count at 1).
func TestRenderlessAction(t *testing.T) {
	t.Parallel()
	for name, store := range map[string]StateStore{"memory": NewMemoryStates(0, 0), "sealed": sealedStore(t, sealedOpts{})} {
		t.Run(name, func(t *testing.T) {
			e := v8bEngine(t, Options{States: store}, nil, "{}")
			_, h, tok := v8bFill(t, e)
			env := envelopeOf(t, v8bPost(e, h, tok, "suggest", nil, nil, ""))
			if _, has := env["html"]; has {
				t.Fatalf("a renderless answer carries no html: %v", env)
			}
			if !reflect.DeepEqual(env["reply"], []any{"ada", "bob"}) {
				t.Fatalf("reply %v", env["reply"])
			}
			next, _ := env["handle"].(string)
			if next == "" || env["token"] != tok || env["version"] != float64(1) {
				t.Fatalf("handle/token/version: %v", env)
			}
			if name == "sealed" && next == h {
				t.Fatal("a sealed store's renderless answer must carry the new blob")
			}
			// The next action runs on the stored state: count was bumped.
			rec := v8bPost(e, next, tok, "bump", nil, nil, "")
			if html, _ := envelopeOf(t, rec)["html"].(string); !strings.Contains(html, "count=2") {
				t.Fatalf("state after a renderless action was not stored: %s", html)
			}
		})
	}
}

// A failed action drops its reply and skip; a reply that is not JSON fails
// the action; a middleware can clear the reply.
func TestReplyRules(t *testing.T) {
	t.Parallel()
	e := v8bEngine(t, Options{}, nil, "{}")
	_, h, tok := v8bFill(t, e)
	rec := v8bPost(e, h, tok, "replyFail", nil, nil, "")
	if rec.Code != http.StatusUnprocessableEntity || strings.Contains(rec.Body.String(), "reply") {
		t.Fatalf("failed action: %d %s", rec.Code, rec.Body)
	}
	if rec := v8bPost(e, h, tok, "badReply", nil, nil, ""); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("a non-JSON reply must fail: %d %s", rec.Code, rec.Body)
	}
	var seen any
	mw := func(next ActionFunc) ActionFunc {
		return func(ctx context.Context, a *ActionCall) (*ActionResult, error) {
			res, err := next(ctx, a)
			if err == nil {
				seen, res.Reply = res.Reply, nil
				if !res.SkipRender {
					t.Error("middleware should see SkipRender")
				}
			}
			return res, err
		}
	}
	e2 := v8bEngine(t, Options{ActionMiddleware: []ActionMiddleware{mw}}, nil, "{}")
	_, h, tok = v8bFill(t, e2)
	env := envelopeOf(t, v8bPost(e2, h, tok, "suggest", nil, nil, ""))
	if _, has := env["reply"]; has || seen == nil {
		t.Fatalf("middleware clears the reply: env %v, seen %v", env, seen)
	}
	if err := (&Effects{}).Reply(func() {}); err == nil {
		t.Fatal("Reply(func) must fail")
	}
}

// ---- 9 · init, updated, $reset --------------------------------------------

// init seeds the state from props (with defaults) before the first Put,
// and runs once per instance, never on an action's re-render.
func TestInitSeedsState(t *testing.T) {
	t.Parallel()
	calls := &v8bCalls{}
	e := v8bEngine(t, Options{}, calls, "{init: setup}")
	page, h, tok := v8bFill(t, e)
	if !strings.Contains(page, "title=post-7") || !strings.Contains(page, "sort=from-init") {
		t.Fatalf("init did not seed: %s", page)
	}
	env := envelopeOf(t, v8bPost(e, h, tok, "bump", nil, nil, ""))
	if html, _ := env["html"].(string); !strings.Contains(html, "title=post-7") || calls.inits != 1 {
		t.Fatalf("init ran %d times; html %s", calls.inits, html)
	}
}

func TestInitFailures(t *testing.T) {
	t.Parallel()
	// Drawing is not acting: brick.dispatch from init fails the render.
	e := v8bEngine(t, Options{}, nil, "{init: noisy}")
	if _, err := fillFile(t, e, "content/a.html"); err == nil || !strings.Contains(err.Error(), "inside an action") {
		t.Fatalf("dispatch from init: %v", err)
	}
	// A failing init inside a boundary shows the fallback.
	e = v8bEngine(t, Options{Mode: Prod}, nil, "{init: broken}", "content/b.html", `<brick:boundary><List /><brick:prop name="fallback">FALLBACK</brick:prop></brick:boundary>`)
	out, err := fillFile(t, e, "content/b.html")
	if err != nil || !strings.Contains(out, "FALLBACK") {
		t.Fatalf("boundary around a failing init: %v %s", err, out)
	}
}

func TestStateHookCompileErrors(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"{init: nope}":                     `brick: init: "nope" is not a function`,
		"{updated: missing}":               `brick: updated: "missing" is not a function`,
		"{init: 3}":                        "must name one of the component's script functions",
		"{on: {x: \"$reset\"}}":            "$reset cannot be a listener",
		"{query: {sort: {history: back}}}": "history: must be replace or push",
		"{query: [nope]}":                  "nope is not a top-level field",
		"{remember: [view]}":               "no Rememberer configured",
	}
	for brick, want := range cases {
		e := v8bEngine(t, Options{}, nil, brick)
		_, err := fillFile(t, e, "content/a.html")
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: got %v, want %q", brick, err, want)
		}
	}
	// The error points at the key's frontmatter line.
	e := v8bEngine(t, Options{}, nil, "{init: nope}")
	_, err := fillFile(t, e, "content/a.html")
	var be *Error
	if !errors.As(err, &be) || be.Line != 12 {
		t.Fatalf("line: %v", err)
	}
	// A dict field cannot be queried.
	e = newEngine(t, Options{States: NewMemoryStates(0, 0), Scripting: v8bScripting(&v8bCalls{}), FS: theme(
		"components/D.html", "---\nstate:\n  f: {a: 1}\nbrick: {query: [f]}\n---\n<div>x</div>", "content/a.html", `<D />`)})
	if _, err := fillFile(t, e, "content/a.html"); err == nil || !strings.Contains(err.Error(), "never a dict") {
		t.Fatalf("dict query field: %v", err)
	}
}

// updated sees only the changed paths of a built-in, its errors join
// .Errors, and it never runs for a named action.
func TestUpdatedHook(t *testing.T) {
	t.Parallel()
	calls := &v8bCalls{}
	e := v8bEngine(t, Options{}, calls, "{updated: tidy}")
	_, h, tok := v8bFill(t, e)
	env := envelopeOf(t, v8bPost(e, h, tok, "$set", []any{"q", "  ADA "}, [][2]any{{"page", 1}}, ""))
	html, _ := env["html"].(string)
	if !strings.Contains(html, "q=ada|") {
		t.Fatalf("updated did not normalise: %s", html)
	}
	if len(calls.updated) != 1 || !reflect.DeepEqual(calls.updated[0], []any{"q"}) {
		t.Fatalf("updated paths: %v (page was sent unchanged and must not be listed)", calls.updated)
	}
	h = stateRE.FindStringSubmatch(html)[1]
	env = envelopeOf(t, v8bPost(e, h, tok, "$set", []any{"q", "BAD"}, nil, ""))
	if html, _ = env["html"].(string); !strings.Contains(html, "err=no bad words") {
		t.Fatalf("updated errors in .Errors: %s", html)
	}
	h = stateRE.FindStringSubmatch(html)[1]
	envelopeOf(t, v8bPost(e, h, tok, "bump", nil, [][2]any{{"q", "zed"}}, ""))
	if len(calls.updated) != 2 {
		t.Fatalf("updated ran for a named action: %v", calls.updated)
	}
}

// $reset restores the declared value (not init's) and refuses an
// undeclared path.
func TestResetBuiltin(t *testing.T) {
	t.Parallel()
	e := v8bEngine(t, Options{}, nil, "{init: setup}")
	_, h, tok := v8bFill(t, e)
	env := envelopeOf(t, v8bPost(e, h, tok, "$set", []any{"q", "x"}, nil, ""))
	h = stateRE.FindStringSubmatch(env["html"].(string))[1]
	env = envelopeOf(t, v8bPost(e, h, tok, "$reset", []any{"q", "sort"}, nil, ""))
	html := env["html"].(string)
	if !strings.Contains(html, "q=|") || !strings.Contains(html, "sort=name") {
		t.Fatalf("$reset: %s", html)
	}
	h = stateRE.FindStringSubmatch(html)[1]
	if rec := v8bPost(e, h, tok, "$reset", []any{"ghost"}, nil, ""); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("$reset of an undeclared path: %d %s", rec.Code, rec.Body)
	}
	if rec := v8bPost(e, h, tok, "$reset", nil, nil, ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("$reset with no path: %d", rec.Code)
	}
}

// ---- 10 · URL-synced state --------------------------------------------------

const v8bQuery = "{query: {q: {as: search}, page: {}, status: {history: push}, sort: {keep: true}}}"

// BV: URL values go through the $set gate: a value of the wrong type is
// ignored (never failing the page), lists read repeated parameters.
func TestQuerySeedsState(t *testing.T) {
	t.Parallel()
	e := v8bEngine(t, Options{Mode: Dev}, nil, v8bQuery)
	q := url.Values{"search": {"acme"}, "page": {"3"}, "status": {"open", "late"}}
	page, _, _ := v8bFill(t, e, WithQuery(q))
	if !strings.Contains(page, "q=acme|page=3|status=[open late]") {
		t.Fatalf("query seed: %s", page)
	}
	if !strings.Contains(page, `data-brick-query="`) {
		t.Fatal("the root carries data-brick-query")
	}
	page, _, _ = v8bFill(t, e, WithQuery(url.Values{"page": {"abc"}, "q": {"not-mine"}}))
	if !strings.Contains(page, "q=|page=1|") {
		t.Fatalf("a bad or foreign parameter must be ignored: %s", page)
	}
	found := false
	for _, w := range e.Warnings() {
		found = found || strings.Contains(w.Message, "ignored the URL parameter page")
	}
	if !found {
		t.Fatal("Dev warns about an ignored parameter")
	}
}

// The query effect lists only changed fields, drops a value back at its
// default unless keep:, and pushes when a push field changed.
func TestQueryEffect(t *testing.T) {
	t.Parallel()
	e := v8bEngine(t, Options{}, nil, v8bQuery)
	_, h, tok := v8bFill(t, e, WithQuery(url.Values{"search": {"acme"}}))
	effectOf := func(env map[string]any) map[string]any {
		for _, ef := range env["effects"].([]any) {
			if m := ef.(map[string]any); m["kind"] == EffectQuery {
				return m
			}
		}
		return nil
	}
	env := envelopeOf(t, v8bPost(e, h, tok, "$set", []any{"page", "2"}, [][2]any{{"q", ""}}, ""))
	ef := effectOf(env)
	want := map[string]any{"set": map[string]any{"page": "2"}, "del": []any{"search"}}
	if ef == nil || !reflect.DeepEqual(ef["data"], want) || ef["replace"] != true {
		t.Fatalf("query effect: %v", ef)
	}
	h = stateRE.FindStringSubmatch(env["html"].(string))[1]
	env = envelopeOf(t, v8bPost(e, h, tok, "$set", []any{"status", []any{"open"}}, [][2]any{{"sort", "name"}}, ""))
	ef = effectOf(env)
	if ef == nil || !reflect.DeepEqual(ef["data"], map[string]any{"set": map[string]any{"status": []any{"open"}}}) || ef["replace"] == true {
		t.Fatalf("push effect: %v", ef)
	}
	h = stateRE.FindStringSubmatch(env["html"].(string))[1]
	env = envelopeOf(t, v8bPost(e, h, tok, "$set", []any{"sort", "date"}, nil, ""))
	h = stateRE.FindStringSubmatch(env["html"].(string))[1]
	env = envelopeOf(t, v8bPost(e, h, tok, "$set", []any{"sort", "name"}, nil, ""))
	if ef = effectOf(env); ef == nil || !reflect.DeepEqual(ef["data"], map[string]any{"set": map[string]any{"sort": "name"}}) {
		t.Fatalf("keep: true keeps the default in the URL: %v", ef)
	}
	h = stateRE.FindStringSubmatch(env["html"].(string))[1]
	if env = envelopeOf(t, v8bPost(e, h, tok, "bump", nil, nil, "")); effectOf(env) != nil {
		t.Fatal("no queried field changed: no effect")
	}
}

// Two components claiming one parameter are warned about in Dev.
func TestQueryParamClaimedTwice(t *testing.T) {
	t.Parallel()
	e := v8bEngine(t, Options{Mode: Dev}, nil, "{query: [page]}",
		"components/Other.html", "---\nstate:\n  page: 1\nbrick: {query: [page]}\n---\n<div>{{ .State.page }}</div>",
		"content/two.html", `<List /><Other />`)
	r, err := e.RenderChunks(context.Background(), "content/two.html", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Fill(context.Background(), r, WithQuery(url.Values{"page": {"2"}})); err != nil {
		t.Fatal(err)
	}
	for _, w := range e.Warnings() {
		if strings.Contains(w.Message, `"page" is claimed by both`) {
			return
		}
	}
	t.Fatalf("no claimed-twice warning: %v", e.Warnings())
}

// ---- 11 · remembered state ----------------------------------------------------

type failingRemember struct{ *MemoryRemember }

func (failingRemember) Save(context.Context, RememberKey, map[string]any) error {
	return errors.New("redis down")
}

type errorSink struct {
	mu   sync.Mutex
	errs []error
}

func (s *errorSink) OnError(_ context.Context, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.errs = append(s.errs, err)
}

func v8bSessions() SessionBinder {
	return SessionFunc(func(r *http.Request) (string, error) { return r.Header.Get("X-Session"), nil })
}

// BV: remembered values are scoped to the session: another session never
// loads them. First values come in D14's order: query beats remembered
// beats init.
func TestRememberScopeAndOrder(t *testing.T) {
	t.Parallel()
	rem := NewMemoryRemember()
	e := v8bEngine(t, Options{Remember: rem, Sessions: v8bSessions()}, nil, "{init: setup, remember: [view, sort], query: [sort]}")
	_, h, tok := v8bFill(t, e, WithSession("ada"))
	envelopeOf(t, v8bPost(e, h, tok, "setView", []any{"list"}, nil, "ada"))
	page, h, tok := v8bFill(t, e, WithSession("ada"))
	if !strings.Contains(page, "view=list") || !strings.Contains(page, "sort=from-init") {
		t.Fatalf("ada's remembered view: %s", page)
	}
	// Remembered beats init: save sort, then render again.
	envelopeOf(t, v8bPost(e, h, tok, "$set", []any{"sort", "date"}, nil, "ada"))
	if page, _, _ = v8bFill(t, e, WithSession("ada")); !strings.Contains(page, "sort=date") {
		t.Fatalf("remembered beats init: %s", page)
	}
	// The URL beats memory.
	if page, _, _ = v8bFill(t, e, WithSession("ada"), WithQuery(url.Values{"sort": {"size"}})); !strings.Contains(page, "sort=size") {
		t.Fatalf("query beats remembered: %s", page)
	}
	// Another session sees the declared and init values only.
	if page, _, _ = v8bFill(t, e, WithSession("bob")); !strings.Contains(page, "view=grid") || !strings.Contains(page, "sort=from-init") {
		t.Fatalf("bob must not see ada's remembered values: %s", page)
	}
	// No session: nothing loaded.
	if page, _, _ = v8bFill(t, e); !strings.Contains(page, "view=grid") {
		t.Fatalf("no session loads nothing: %s", page)
	}
}

// Save only on change; a Save error is reported and the action succeeds; a
// stored value of the wrong type falls back to the declared one.
func TestRememberSave(t *testing.T) {
	t.Parallel()
	sink := &errorSink{}
	e := v8bEngine(t, Options{Remember: failingRemember{NewMemoryRemember()}, Sessions: v8bSessions(), Hooks: []any{sink}}, nil, "{remember: [view]}")
	_, h, tok := v8bFill(t, e, WithSession("ada"))
	envelopeOf(t, v8bPost(e, h, tok, "bump", nil, nil, "ada"))
	if len(sink.errs) != 0 {
		t.Fatalf("nothing remembered changed, nothing saved: %v", sink.errs)
	}
	envelopeOf(t, v8bPost(e, h, tok, "setView", []any{"list"}, nil, "ada"))
	if len(sink.errs) != 1 || !strings.Contains(sink.errs[0].Error(), "redis down") {
		t.Fatalf("save error reported: %v", sink.errs)
	}

	rem := NewMemoryRemember()
	e = v8bEngine(t, Options{Remember: rem, Sessions: v8bSessions()}, nil, "{remember: [count, view]}")
	key := RememberKey{Session: hashSession("ada"), Path: "components/List.html", PagePath: "content/a.html"}
	if err := rem.Save(context.Background(), key, map[string]any{"count": "many", "view": "list"}); err != nil {
		t.Fatal(err)
	}
	page, _, _ := v8bFill(t, e, WithSession("ada"))
	if !strings.Contains(page, "count=0") || !strings.Contains(page, "view=list") {
		t.Fatalf("a mistyped remembered value falls back: %s", page)
	}
}

// A stateful component on a dynamic route keeps .Page.Route after an
// action: the instance carries the route values, sealed with the rest
// .
func TestRouteSurvivesAction(t *testing.T) {
	t.Parallel()
	for name, store := range map[string]StateStore{"memory": NewMemoryStates(0, 0), "sealed": sealedStore(t, sealedOpts{})} {
		t.Run(name, func(t *testing.T) {
			e := newEngine(t, Options{States: store, Scripting: v8bScripting(&v8bCalls{}), FS: theme(
				"components/R.html", "---\nstate:\n  count: 0\n---\n<brick:script>list</brick:script>\n<div>slug={{ .Page.Route.slug }}|part={{ index .Page.RouteParts 0 }}|count={{ .State.count }}</div>",
				"content/a.html", `<R />`)})
			ctx := context.Background()
			route := WithRoute(map[string]string{"slug": "hello"}, []string{"hello"})
			r, err := e.RenderChunks(ctx, "content/a.html", nil, route)
			if err != nil {
				t.Fatal(err)
			}
			page, err := e.Fill(ctx, r, route)
			if err != nil {
				t.Fatal(err)
			}
			h, tok := handleOf(t, string(page))
			html, _ := envelopeOf(t, v8bPost(e, h, tok, "bump", nil, nil, ""))["html"].(string)
			if !strings.Contains(html, "slug=hello|part=hello|count=1") {
				t.Fatalf("route lost after an action: %s", html)
			}
		})
	}
}
