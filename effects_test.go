package brick

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/ubgo/brick/runtime"
)

// Tests for the effects channel and events
// between components. BV in a comment marks a break-verified test.

// ctxScripting is a Scripting whose functions see the call's context, as a
// real backend's capabilities do (brick.EffectsFrom(threadCtx)).
type ctxScripting map[string]ctxModule

type ctxModule map[string]func(ctx context.Context, args ...any) (any, error)

func (m ctxModule) Functions() []string { return sortedKeys(m) }
func (m ctxModule) Call(ctx context.Context, fn string, args ...any) (any, error) {
	return m[fn](ctx, args...)
}

func (s ctxScripting) Compile(_ context.Context, src ScriptSource) (ScriptModule, error) {
	m, ok := s[strings.TrimSpace(string(src.Src))]
	if !ok {
		return nil, fmt.Errorf("no fake module %q", src.Src)
	}
	return m, nil
}

// effectsScripting is the contacts component's script: actions that record
// effects, and a template function that tries to.
func effectsScripting() ctxScripting {
	state := func(a []any) map[string]any { st, _ := a[0].(map[string]any); return st }
	return ctxScripting{
		"contacts": {
			"remove": func(ctx context.Context, a ...any) (any, error) {
				eff := EffectsFrom(ctx)
				if err := eff.Dispatch("contact-deleted", map[string]any{"id": 42}, ""); err != nil {
					return nil, err
				}
				if err := eff.Flash("Deleted Ada.", FlashSuccess); err != nil {
					return nil, err
				}
				return state(a), nil
			},
			"fail": func(ctx context.Context, a ...any) (any, error) {
				_ = EffectsFrom(ctx).Dispatch("contact-deleted", nil, "")
				return nil, errors.New("database is down")
			},
			"big": func(ctx context.Context, a ...any) (any, error) {
				return state(a), EffectsFrom(ctx).Dispatch("contact-deleted", map[string]any{"rows": strings.Repeat("x", 40<<10)}, "")
			},
			"arm": func(ctx context.Context, a ...any) (any, error) {
				st := state(a)
				st["try"] = true
				return st, nil
			},
			// view runs at render: the collector is hidden there.
			"view": func(ctx context.Context, a ...any) (any, error) {
				if err := EffectsFrom(ctx).Dispatch("contact-deleted", nil, ""); err != nil {
					return nil, err
				}
				return "drew", nil
			},
			"reload": func(ctx context.Context, a ...any) (any, error) { return state(a), nil },
		},
	}
}

const contactsFile = "---\nstate:\n  try: false\n---\n<brick:script>contacts</brick:script>\n<div class=\"contacts\">{{ if .State.try }}{{ view }}{{ end }}</div>"

// effectsEngine builds an engine over the contacts component plus extra
// files.
func effectsEngine(t *testing.T, opts Options, files ...string) *Engine {
	t.Helper()
	opts.FS = theme(append([]string{"components/Contacts.html", contactsFile, "content/a.html", `<Contacts />`}, files...)...)
	if opts.Scripting == nil {
		opts.Scripting = effectsScripting()
	}
	if opts.States == nil {
		opts.States = NewMemoryStates(0, 0)
	}
	return newEngine(t, opts)
}

// fillFile renders and fills one page.
func fillFile(t *testing.T, e *Engine, page string) (string, error) {
	t.Helper()
	ctx := context.Background()
	r, err := e.RenderChunks(ctx, page, nil)
	if err != nil {
		return "", err
	}
	b, err := e.Fill(ctx, r)
	return string(b), err
}

// handleOf returns the first stateful root's handle and token.
func handleOf(t *testing.T, page string) (string, string) {
	t.Helper()
	m := stateRE.FindStringSubmatch(page)
	if m == nil {
		t.Fatalf("no handle in %s", page)
	}
	return m[1], m[2]
}

func TestEffectsEnvelope(t *testing.T) {
	e := effectsEngine(t, Options{Mode: Dev})
	page, err := fillFile(t, e, "content/a.html")
	if err != nil {
		t.Fatal(err)
	}
	h, tok := handleOf(t, page)
	rec := postAction(e, h, tok, "remove", map[string]string{"Accept": EffectsContentType + ", text/html;q=0.5"})
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != EffectsContentType {
		t.Fatalf("status %d type %q: %s", rec.Code, rec.Header().Get("Content-Type"), rec.Body)
	}
	var env struct {
		V       int      `json:"v"`
		Version int64    `json:"version"`
		HTML    string   `json:"html"`
		Effects []Effect `json:"effects"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.V != 1 || env.Version != 1 || !strings.Contains(env.HTML, `class="contacts"`) {
		t.Fatalf("envelope = %+v", env)
	}
	if len(env.Effects) != 2 || env.Effects[0].Kind != EffectDispatch || env.Effects[0].Name != "contact-deleted" || env.Effects[1].Kind != EffectFlash || env.Effects[1].Level != FlashSuccess {
		t.Fatalf("effects out of order or missing: %+v", env.Effects)
	}

	// Without the Accept: plain HTML, and Dev says what was dropped.
	h, tok = handleOf(t, env.HTML)
	rec = postAction(e, h, tok, "remove", nil)
	if !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") || !strings.Contains(rec.Body.String(), `class="contacts"`) {
		t.Fatalf("plain response = %q %s", rec.Header().Get("Content-Type"), rec.Body)
	}
	found := false
	for _, w := range e.Warnings() {
		found = found || strings.Contains(w.Message, "<Contacts>.remove recorded 2 effects the client did not accept")
	}
	if !found {
		t.Fatalf("no Dev line about dropped effects: %v", e.Warnings())
	}
}

func TestRunActionReturnsEffects(t *testing.T) {
	e := effectsEngine(t, Options{})
	page, err := fillFile(t, e, "content/a.html")
	if err != nil {
		t.Fatal(err)
	}
	h, tok := handleOf(t, page)
	res, err := e.RunAction(context.Background(), ActionRequest{Handle: h, Token: tok, Action: "remove"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Effects) != 2 || res.Effects[0].Data["id"] != 42 {
		t.Fatalf("effects = %+v", res.Effects)
	}
}

// BV: removing eff.reset() in coreAction's error path makes the middleware
// see the half-done dispatch.
func TestEffectsDiscardedOnError(t *testing.T) {
	var after []Effect
	seen := func(next ActionFunc) ActionFunc {
		return func(ctx context.Context, a *ActionCall) (*ActionResult, error) {
			res, err := next(ctx, a)
			after = EffectsFrom(ctx).List()
			return res, err
		}
	}
	e := effectsEngine(t, Options{ActionMiddleware: []ActionMiddleware{seen}})
	page, err := fillFile(t, e, "content/a.html")
	if err != nil {
		t.Fatal(err)
	}
	h, tok := handleOf(t, page)
	rec := postAction(e, h, tok, "fail", map[string]string{"Accept": EffectsContentType})
	if rec.Code != http.StatusUnprocessableEntity || strings.Contains(rec.Body.String(), "contact-deleted") {
		t.Fatalf("status %d body %s", rec.Code, rec.Body)
	}
	if len(after) != 0 {
		t.Fatalf("a failed action kept effects: %+v", after)
	}
}

// BV: rendering with the action's ctx (no withEffects(ctx, nil)) lets the
// re-render's template function record an event.
func TestEffectsOnlyInsideAction(t *testing.T) {
	// At a first render there is no action at all.
	e := effectsEngine(t, Options{}, "components/Eager.html", "---\nstate:\n  try: true\n---\n<brick:script>contacts</brick:script>\n<div>{{ view }}</div>", "content/b.html", `<Eager />`)
	if _, err := fillFile(t, e, "content/b.html"); err == nil || !strings.Contains(err.Error(), "only be recorded inside an action") {
		t.Fatalf("render-time dispatch: %v", err)
	}
	// The re-render after an action is drawing too.
	page, err := fillFile(t, e, "content/a.html")
	if err != nil {
		t.Fatal(err)
	}
	h, tok := handleOf(t, page)
	_, err = e.RunAction(context.Background(), ActionRequest{Handle: h, Token: tok, Action: "arm"})
	if err == nil || !strings.Contains(err.Error(), "only be recorded inside an action") {
		t.Fatalf("re-render dispatch: %v", err)
	}
}

// BV: dropping checkURL's host comparison lets https://evil.example through.
func TestEffectRedirectRefusesOtherOrigin(t *testing.T) {
	e := newEngine(t, Options{FS: theme(), RedirectHosts: []string{"accounts.example.com"}})
	eff := e.newEffects("app.example.com")
	for _, bad := range []string{"javascript:alert(1)", "data:text/html,x", "//evil.example/x", "https://evil.example/", "/\\evil.example", "ftp://app.example.com/", "https://user@app.example.com/", ""} {
		if err := eff.Redirect(bad); err == nil {
			t.Errorf("redirect %q was allowed", bad)
		}
	}
	for _, ok := range []string{"/contacts/42", "https://app.example.com/x", "https://accounts.example.com/login"} {
		if err := eff.Redirect(ok); err != nil {
			t.Errorf("redirect %q: %v", ok, err)
		}
	}
	if err := eff.URL("https://accounts.example.com/x", false); err == nil {
		t.Error("history url to another host was allowed")
	}
	if got := eff.List(); len(got) != 1 || got[0].URL != "https://accounts.example.com/login" {
		t.Fatalf("terminal effects not collapsed to the last: %+v", got)
	}
}

// BV: removing the reserved-name check lets "recovered" through.
func TestEffectReservedNames(t *testing.T) {
	e := newEngine(t, Options{FS: theme()})
	eff := e.newEffects("")
	for _, bad := range []string{"recovered", "brick:x", "navigate", "flash", "Contact-Deleted", "", "has space"} {
		if err := eff.Dispatch(bad, nil, ""); err == nil {
			t.Errorf("dispatch %q was allowed", bad)
		}
	}
	if err := eff.Dispatch("Contact-Deleted", nil, ""); err == nil || !strings.Contains(err.Error(), `"contact-deleted"`) {
		t.Errorf("uppercase name gives no lowercase hint: %v", err)
	}
	for _, ok := range []string{"contact-deleted", "contact-updated.42", "cart:changed"} {
		if err := eff.Dispatch(ok, nil, "self"); err != nil {
			t.Errorf("dispatch %q: %v", ok, err)
		}
	}
	if err := eff.Dispatch("x", nil, "<script>"); err == nil {
		t.Error("to accepted a non-name")
	}
	if err := eff.Add(Effect{Kind: "confetti"}); err == nil {
		t.Error("a custom kind without x- was allowed")
	}
	if err := eff.Add(Effect{Kind: "x-confetti", Data: map[string]any{"n": 3}}); err != nil {
		t.Error(err)
	}
	if err := eff.Flash("hi", "loud"); err == nil {
		t.Error("an unknown flash level was allowed")
	}
}

func TestEffectDataJSONOnly(t *testing.T) {
	e := newEngine(t, Options{FS: theme()})
	eff := e.newEffects("")
	if err := eff.Dispatch("x", map[string]any{"when": struct{}{}}, ""); err == nil {
		t.Fatal("a Go struct in data was allowed")
	}
	data := map[string]any{"ids": []any{1, 2}}
	if err := eff.Dispatch("x", data, ""); err != nil {
		t.Fatal(err)
	}
	data["ids"] = nil // the recorded copy does not change
	if got := eff.List()[0].Data["ids"]; got == nil {
		t.Fatal("effects share the caller's map")
	}
}

// BV: removing the cap check in Effects.Add (and in action) lets 40 KiB
// through.
func TestEffectSizeCap(t *testing.T) {
	e := effectsEngine(t, Options{})
	page, err := fillFile(t, e, "content/a.html")
	if err != nil {
		t.Fatal(err)
	}
	h, tok := handleOf(t, page)
	rec := postAction(e, h, tok, "big", map[string]string{"Accept": EffectsContentType})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	// Middleware additions count too.
	bloat := func(next ActionFunc) ActionFunc {
		return func(ctx context.Context, a *ActionCall) (*ActionResult, error) {
			res, err := next(ctx, a)
			if err == nil {
				res.Effects = append(res.Effects, Effect{Kind: "x-bloat", Data: map[string]any{"s": strings.Repeat("y", 40<<10)}})
			}
			return res, err
		}
	}
	e = effectsEngine(t, Options{ActionMiddleware: []ActionMiddleware{bloat}})
	page, _ = fillFile(t, e, "content/a.html")
	h, tok = handleOf(t, page)
	if _, err := e.RunAction(context.Background(), ActionRequest{Handle: h, Token: tok, Action: "remove"}); err == nil || !strings.Contains(err.Error(), "MaxEffectBytes") {
		t.Fatalf("middleware bloat: %v", err)
	}
}

func TestMiddlewareSeesAndAddsEffects(t *testing.T) {
	var audited []string
	mw := func(next ActionFunc) ActionFunc {
		return func(ctx context.Context, a *ActionCall) (*ActionResult, error) {
			if err := EffectsFrom(ctx).Title("Working"); err != nil { // before next
				return nil, err
			}
			res, err := next(ctx, a)
			if err == nil {
				for _, ef := range res.Effects {
					audited = append(audited, ef.Kind)
				}
				res.Effects = append(res.Effects, Effect{Kind: "x-confetti"})
			}
			return res, err
		}
	}
	e := effectsEngine(t, Options{ActionMiddleware: []ActionMiddleware{mw}})
	page, err := fillFile(t, e, "content/a.html")
	if err != nil {
		t.Fatal(err)
	}
	h, tok := handleOf(t, page)
	res, err := e.RunAction(context.Background(), ActionRequest{Handle: h, Token: tok, Action: "remove"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(audited, ",") != "title,dispatch,flash" || len(res.Effects) != 4 || res.Effects[3].Kind != "x-confetti" {
		t.Fatalf("audited %v, effects %+v", audited, res.Effects)
	}
}

// countFile listens to two events, one with a dynamic name.
const countFile = "---\nstate: {}\nprops:\n  id: string\nbrick:\n  on:\n    contact-deleted: $refresh\n    \"contact-updated.{{ .Props.id }}\": contacts.reload\nuse:\n  contacts: ~/lib/contacts.star\n---\n<span class=\"count\">n</span>"

func TestOnListeners(t *testing.T) {
	e := effectsEngine(t, Options{Mode: Dev}, "lib/contacts.star", "contacts", "components/Count.html", countFile, "content/c.html", `<Count id="42" />`)
	page, err := fillFile(t, e, "content/c.html")
	if err != nil {
		t.Fatal(err)
	}
	// state: {} is stateful (the page carries a handle), the YAML key
	// "on" stayed a string (not YAML 1.1's true), and the dynamic name
	// rendered with the props.
	handleOf(t, page)
	want := `data-brick-name="Count" data-brick-on="{&#34;contact-deleted&#34;:&#34;$refresh&#34;,&#34;contact-updated.42&#34;:&#34;contacts.reload&#34;}"`
	if !strings.Contains(page, want) {
		t.Fatalf("listener attributes missing:\n%s", page)
	}
	// A listener re-renders like any action: the attributes survive it.
	h, tok := handleOf(t, page)
	res, err := e.RunAction(context.Background(), ActionRequest{Handle: h, Token: tok, Action: "contacts.reload", Args: []any{map[string]any{"id": 42}}})
	if err != nil || !strings.Contains(string(res.HTML), want) {
		t.Fatalf("after a listener action: %v\n%s", err, res.HTML)
	}

	// An invalid rendered name fails in Dev.
	e = effectsEngine(t, Options{Mode: Dev}, "lib/contacts.star", "contacts", "components/Count.html", countFile, "content/c.html", `<Count id="Ada Lovelace" />`)
	if _, err := fillFile(t, e, "content/c.html"); err == nil || !strings.Contains(err.Error(), "contact-updated.Ada Lovelace") {
		t.Fatalf("invalid dynamic name: %v", err)
	}
}

func TestOnListenerCompileChecks(t *testing.T) {
	for name, tc := range map[string]struct{ file, want string }{
		"unknown function": {"---\nstate: {}\nbrick:\n  on:\n    contact-deleted: relaod\n---\n<brick:script>contacts</brick:script>\n<i></i>", `"relaod" is not a function`},
		"$set listens":     {"---\nstate: {}\nbrick:\n  on:\n    contact-deleted: $set(\"a\", 1)\n---\n<i></i>", "$set cannot be a listener"},
		"no state":         {"---\nbrick:\n  on:\n    contact-deleted: $refresh\n---\n<i></i>", "needs state:"},
		"reserved name":    {"---\nstate: {}\nbrick:\n  on:\n    recovered: $refresh\n---\n<i></i>", "reserved"},
		"bad call":         {"---\nstate: {}\nbrick:\n  on:\n    x: \"f('a')\"\n---\n<i></i>", "JSON literals"},
	} {
		e := effectsEngine(t, Options{}, "components/L.html", tc.file, "content/l.html", `<L />`)
		if _, err := fillFile(t, e, "content/l.html"); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v, want %q", name, err, tc.want)
		}
	}
}

func TestBrickOnCallSite(t *testing.T) {
	row := "<li class=\"row\">{{ .Props.label }}</li>"
	owner := "---\nstate: {}\n---\n<brick:script>contacts</brick:script>\n<ul>{{ range (list \"a\" \"b\") }}<Row :label=\".\" brick:on:deleted=\"reload\" brick:on:starred=\"$refresh\" />{{ end }}</ul>"
	e := effectsEngine(t, Options{}, "components/Row.html", row, "components/Owner.html", owner, "content/o.html", `<Owner />`)
	page, err := fillFile(t, e, "content/o.html")
	if err != nil {
		t.Fatal(err)
	}
	want := `<li data-brick-up="{&#34;deleted&#34;:&#34;reload&#34;,&#34;starred&#34;:&#34;$refresh&#34;}" class="row">a</li>`
	if !strings.Contains(page, want) || strings.Contains(page, ` brick:on:`) {
		t.Fatalf("data-brick-up missing or brick:on leaked:\n%s", page)
	}
	// A component no caller listens to is unchanged.
	if out, err := e.RenderComponent(context.Background(), "Row", map[string]any{"label": "x"}, ""); err != nil || string(out) != `<li class="row">x</li>` {
		t.Fatalf("plain row = %q, %v", out, err)
	}

	for name, tc := range map[string]struct{ files []string }{
		"stateless writer": {[]string{"components/Owner.html", "<ul><Row brick:on:deleted=\"reload\" /></ul>"}},
		"unknown action":   {[]string{"components/Owner.html", "---\nstate: {}\n---\n<brick:script>contacts</brick:script>\n<ul><Row brick:on:deleted=\"relaod\" /></ul>"}},
		"uppercase event":  {[]string{"components/Owner.html", "---\nstate: {}\n---\n<brick:script>contacts</brick:script>\n<ul><Row brick:on:Deleted=\"reload\" /></ul>"}},
		"multi-root child": {[]string{"components/Row.html", "<li>a</li><li>b</li>", "components/Owner.html", "---\nstate: {}\n---\n<brick:script>contacts</brick:script>\n<ul><Row brick:on:deleted=\"reload\" /></ul>"}},
	} {
		files := append([]string{"components/Row.html", row, "content/o.html", `<Owner />`}, tc.files...)
		e := effectsEngine(t, Options{}, files...)
		if _, err := fillFile(t, e, "content/o.html"); err == nil {
			t.Errorf("%s: no error", name)
		} else {
			t.Logf("%s: %v", name, err)
		}
	}
}

func TestDispatchCallCheck(t *testing.T) {
	for _, ok := range []string{`$dispatch("contact-selected", {"id": 42})`, `$dispatch("x")`, `$dispatch("x", null, "self")`, `$parent.save`, `$parent.save(1, "x")`} {
		if err := checkActionCall(ok); err != nil {
			t.Errorf("%s: %v", ok, err)
		}
	}
	for _, bad := range []string{`$dispatch()`, `$dispatch(42)`, `$dispatch("Recovered")`, `$dispatch("x", [1])`, `$dispatch("x", {}, 3)`} {
		if err := checkActionCall(bad); err == nil {
			t.Errorf("%s: accepted", bad)
		}
	}
}

// A stateful child is an island; its call-site listeners travel with its
// props and stay on its root after its own actions.
func TestBrickOnStatefulChild(t *testing.T) {
	srow := "---\nstate: {}\n---\n<brick:script>contacts</brick:script>\n<li class=\"srow\">x</li>"
	owner := "---\nstate: {}\n---\n<brick:script>contacts</brick:script>\n<ul><SRow brick:on:deleted=\"reload\" /></ul>"
	e := effectsEngine(t, Options{}, "components/SRow.html", srow, "components/Owner.html", owner, "content/o.html", `<Owner />`)
	page, err := fillFile(t, e, "content/o.html")
	if err != nil {
		t.Fatal(err)
	}
	up := `data-brick-up="{&#34;deleted&#34;:&#34;reload&#34;}"`
	i := strings.Index(page, `class="srow"`)
	if i < 0 || !strings.Contains(page[:i], up) {
		t.Fatalf("stateful child lacks data-brick-up:\n%s", page)
	}
	ms := stateRE.FindAllStringSubmatch(page, -1)
	if len(ms) != 2 {
		t.Fatalf("want two stateful roots:\n%s", page)
	}
	res, err := e.RunAction(context.Background(), ActionRequest{Handle: ms[1][1], Token: ms[1][2], Action: "reload"})
	if err != nil || !strings.Contains(string(res.HTML), up) {
		t.Fatalf("after the child's action: %v\n%s", err, res.HTML)
	}
}

// The Go list and the runtime's RESERVED set must name the same events: a
// name only the runtime reserves would pass Dispatch here and fail in the
// browser (errors-cleared, v20, was the case that found this).
// Every name the runtime emits itself (brick.emit with a literal name, in
// runtime.js or nav.js) is reserved, so an action can never fake one and a
// name the runtime starts emitting cannot be forgotten in the list.
// BV: dropping "upload-done" from reservedEvents fails this test.
func TestRuntimeEmittedEventsReserved(t *testing.T) {
	t.Parallel()
	call := regexp.MustCompile(`brick\.emit\(([^,)]*)[,)]`)
	lit := regexp.MustCompile(`"([^"]+)"`)
	seen := 0
	for _, src := range [][]byte{runtime.JS, runtime.Nav} {
		for _, m := range call.FindAllSubmatch(src, -1) {
			for _, q := range lit.FindAllSubmatch(m[1], -1) {
				seen++
				name := string(q[1])
				if err := checkEventName(name); err == nil {
					t.Errorf("the runtime emits %q but an action may dispatch it", name)
				}
			}
		}
	}
	if seen < 20 {
		t.Fatalf("found only %d brick.emit names; the scan no longer matches the runtime", seen)
	}
}

func TestReservedEventsMatchRuntime(t *testing.T) {
	m := regexp.MustCompile(`const RESERVED = new Set\(\[([^\]]*)\]\)`).FindSubmatch(runtime.JS)
	if m == nil {
		t.Fatal("runtime.js has no RESERVED set")
	}
	var js []string
	for _, q := range regexp.MustCompile(`"([^"]+)"`).FindAllSubmatch(m[1], -1) {
		js = append(js, string(q[1]))
	}
	goList := slices.Clone(reservedEvents)
	slices.Sort(js)
	slices.Sort(goList)
	if !slices.Equal(js, goList) {
		t.Errorf("reserved events differ:\n runtime.js %v\n effects.go %v", js, goList)
	}
}
