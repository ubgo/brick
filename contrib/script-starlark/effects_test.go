package scriptstarlark

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"go.starlark.net/starlark"

	"github.com/ubgo/brick"
)

// The brick module records effects inside an action and is a script error
// anywhere else.
func TestBrickModuleEffects(t *testing.T) {
	t.Parallel()
	sc, err := New(Options{})
	if err != nil {
		t.Fatal(err)
	}
	script := "<brick:script>\n" +
		"def remove(state, props, id):\n" +
		"    brick.dispatch(\"contact-deleted\", {\"id\": id})\n" +
		"    brick.dispatch(\"mine\", to = \"self\")\n" +
		"    brick.flash(\"Deleted.\", \"success\")\n" +
		"    brick.title(\"(3) Inbox\")\n" +
		"    brick.url(\"/c?page=2\", replace = True)\n" +
		"    brick.download(\"/files/a.csv\", \"a.csv\")\n" +
		"    brick.navigate(\"/contacts\")\n" +
		"    return state\n\n" +
		"def evil(state, props):\n" +
		"    brick.redirect(\"https://evil.example/\")\n" +
		"    return state\n\n" +
		"def drawn():\n" +
		"    brick.dispatch(\"x\")\n" +
		"    return \"never\"\n" +
		"</brick:script>\n"
	e, err := brick.New(brick.Options{Scripting: sc, States: brick.NewMemoryStates(0, 0), FS: files(
		"components/C.html", "---\nstate:\n  n: 0\n---\n"+script+"<div>{{ .State.n }}</div>",
		"components/D.html", "---\nstate:\n  n: 0\n---\n"+script+"<div>{{ drawn }}</div>",
		"content/a.html", `<C />`,
		"content/b.html", `<D />`,
	)})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	page, err := e.Render(ctx, "content/a.html", nil)
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`data-brick-state="([^"]+)" data-brick-token="([0-9a-f]+)"`).FindStringSubmatch(string(page))
	res, err := e.RunAction(ctx, brick.ActionRequest{Handle: m[1], Token: m[2], Action: "remove", Args: []any{int64(42)}})
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for _, ef := range res.Effects {
		kinds = append(kinds, ef.Kind)
	}
	if strings.Join(kinds, ",") != "dispatch,dispatch,flash,title,url,download,navigate" || res.Effects[0].Data["id"] != int64(42) || res.Effects[1].To != "self" || res.Effects[2].Level != "success" || !res.Effects[4].Replace {
		t.Fatalf("effects = %+v", res.Effects)
	}
	if _, err := e.RunAction(ctx, brick.ActionRequest{Handle: m[1], Token: m[2], Action: "evil"}); err == nil || !strings.Contains(err.Error(), "RedirectHosts") {
		t.Fatalf("open redirect: %v", err)
	}
	// BV: with EffectsFrom not hidden at render, drawn() records instead.
	if _, err := e.Render(ctx, "content/b.html", nil); err == nil || !strings.Contains(err.Error(), "brick.dispatch can only be called inside an action") {
		t.Fatalf("render-time brick.dispatch: %v", err)
	}
}

// A host global named brick would be shadowed by the effects module.
func TestBrickGlobalClash(t *testing.T) {
	t.Parallel()
	if _, err := New(Options{Globals: starlark.StringDict{"brick": starlark.None}}); err == nil {
		t.Fatal("Globals.brick accepted")
	}
}

// brick.validate returns the failing fields and touches them; untouch
// clears them; a component without rules has neither.
func TestBrickModuleValidate(t *testing.T) {
	t.Parallel()
	sc, err := New(Options{})
	if err != nil {
		t.Fatal(err)
	}
	script := "<brick:script>\n" +
		"def signup(state, props):\n" +
		"    errors = brick.validate()\n" +
		"    if errors:\n" +
		"        return [state, errors]\n" +
		"    brick.untouch()\n" +
		"    return dict(state, email = \"\")\n\n" +
		"def one(state, props):\n" +
		"    return [state, brick.validate(\"email\")]\n" +
		"</brick:script>\n"
	e, err := brick.New(brick.Options{Scripting: sc, States: brick.NewMemoryStates(0, 0), FS: files(
		"components/S.html", "---\nstate:\n  name: \"\"\n  email: \"\"\nbrick:\n  rules:\n    name: [required]\n    email: [required, email]\n---\n"+script+"<div>{{ range $k, $v := .Errors }}{{ $k }}={{ $v }};{{ end }}|{{ range $k, $v := .Touched }}{{ $k }};{{ end }}</div>",
		"components/N.html", "---\nstate:\n  n: 0\n---\n"+script+"<div></div>",
		"content/a.html", `<S /><N />`,
	)})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	page, err := e.Render(ctx, "content/a.html", nil)
	if err != nil {
		t.Fatal(err)
	}
	all := regexp.MustCompile(`data-brick-state="([^"]+)" data-brick-token="([0-9a-f]+)"`).FindAllStringSubmatch(string(page), -1)
	s, n := all[0], all[1]
	act := func(m []string, action string, sets ...brick.FieldSet) (string, error) {
		res, err := e.RunAction(ctx, brick.ActionRequest{Handle: m[1], Token: m[2], Action: action, Sets: sets})
		if err != nil {
			return "", err
		}
		h := string(res.HTML)
		return h[strings.Index(h, ">")+1 : strings.LastIndex(h, "<")], nil
	}
	got, err := act(s, "signup", brick.FieldSet{Path: "email", Value: "x"})
	if err != nil || got != "email=Enter a valid email address.;name=This field is required.;|email;name;" {
		t.Fatalf("validate all: %q %v", got, err)
	}
	got, err = act(s, "signup", brick.FieldSet{Path: "email", Value: "ada@example.com"}, brick.FieldSet{Path: "name", Value: "Ada"})
	if err != nil || got != "|" {
		t.Fatalf("after a valid submit and untouch: %q %v", got, err)
	}
	got, err = act(s, "one")
	if err != nil || got != "email=This field is required.;|email;" {
		t.Fatalf("validate one: %q %v", got, err)
	}
	if _, err := act(n, "signup"); err == nil || !strings.Contains(err.Error(), "brick.validate can only be called inside an action of a component with brick: {rules:}") {
		t.Fatalf("no rules: %v", err)
	}
}

// brick.reply and brick.skip_render reach the action result;
// init runs in Starlark with props and its defaults.
func TestBrickReplyAndInit(t *testing.T) {
	t.Parallel()
	sc, err := New(Options{})
	if err != nil {
		t.Fatal(err)
	}
	script := "<brick:script>\n" +
		"def setup(state, props):\n" +
		"    return dict(state, title = \"post-\" + str(props[\"id\"]))\n\n" +
		"def suggest(state, props, q):\n" +
		"    brick.skip_render()\n" +
		"    brick.reply([n for n in [\"ada\", \"bob\", \"al\"] if n.startswith(q)])\n" +
		"    return dict(state, n = state[\"n\"] + 1)\n" +
		"</brick:script>\n"
	e, err := brick.New(brick.Options{Scripting: sc, States: brick.NewMemoryStates(0, 0), FS: files(
		"components/C.html", "---\nprops:\n  id: {type: int, default: 7}\nstate:\n  n: 0\n  title: \"\"\nbrick: {init: setup}\n---\n"+script+"<div>{{ .State.title }}</div>",
		"content/a.html", `<C />`,
	)})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	page, err := e.Render(ctx, "content/a.html", nil)
	if err != nil || !strings.Contains(string(page), "post-7") {
		t.Fatalf("init: %v %s", err, page)
	}
	m := regexp.MustCompile(`data-brick-state="([^"]+)" data-brick-token="([0-9a-f]+)"`).FindStringSubmatch(string(page))
	res, err := e.RunAction(ctx, brick.ActionRequest{Handle: m[1], Token: m[2], Action: "suggest", Args: []any{"a"}})
	if err != nil {
		t.Fatal(err)
	}
	if !res.SkipRender || len(res.HTML) != 0 || res.Handle == "" || res.Version != 1 {
		t.Fatalf("renderless result: %+v", res)
	}
	if got, ok := res.Reply.([]any); !ok || len(got) != 2 || got[0] != "ada" || got[1] != "al" {
		t.Fatalf("reply = %#v", res.Reply)
	}
}

// streamSink collects brick.stream's chunks.
type streamSink []string

func (s *streamSink) Write(ref, text string, replace bool) error {
	*s = append(*s, fmt.Sprintf("%s:%s:%v", ref, text, replace))
	return nil
}

// brick.stream sends text to the action's stream, and is a
// script error outside an action.
func TestBrickStream(t *testing.T) {
	t.Parallel()
	sc, err := New(Options{})
	if err != nil {
		t.Fatal(err)
	}
	script := "<brick:script>\n" +
		"def ask(state, props):\n" +
		"    for part in [\"Hel\", \"lo\"]:\n" +
		"        brick.stream(\"answer\", part)\n" +
		"    brick.stream(\"answer\", \"Hello!\", replace = True)\n" +
		"    return dict(state, answer = \"Hello!\")\n" +
		"</brick:script>\n"
	e, err := brick.New(brick.Options{Scripting: sc, States: brick.NewMemoryStates(0, 0), FS: files(
		"components/C.html", "---\nstate:\n  answer: \"\"\n---\n"+script+"<p brick:ref=\"answer\">{{ .State.answer }}</p>",
		"content/a.html", `<C />`,
	)})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	page, err := e.Render(ctx, "content/a.html", nil)
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`data-brick-state="([^"]+)" data-brick-token="([0-9a-f]+)"`).FindStringSubmatch(string(page))
	var sink streamSink
	out, err := e.Action(brick.WithActionStream(ctx, &sink), brick.ActionRequest{Handle: m[1], Token: m[2], Action: "ask"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(sink, "|") != "answer:Hel:false|answer:lo:false|answer:Hello!:true" || !strings.Contains(string(out), "Hello!") {
		t.Fatalf("chunks %v html %s", sink, out)
	}
}

// brick.rows renders each item through a component and records a rows
// effect; brick.submit records a submit. There is no
// html argument, so a script cannot put a raw string into the page.
func TestBrickRowsAndSubmit(t *testing.T) {
	t.Parallel()
	sc, err := New(Options{})
	if err != nil {
		t.Fatal(err)
	}
	script := "<brick:script>\n" +
		"def post(state, props, text):\n" +
		"    brick.rows(\"log\", component = \"Msg\", items = [{\"id\": 1, \"text\": text}], at = \"top\", limit = 20)\n" +
		"    brick.rows(\"log\", op = \"delete\", keys = [7, \"x\"])\n" +
		"    return state\n\n" +
		"def raw(state, props):\n" +
		"    brick.rows(\"log\", html = \"<b>x</b>\")\n" +
		"    return state\n\n" +
		"def login(state, props):\n" +
		"    brick.submit(\"login\")\n" +
		"    return state\n" +
		"</brick:script>\n"
	e, err := brick.New(brick.Options{Scripting: sc, States: brick.NewMemoryStates(0, 0), FS: files(
		"components/C.html", "---\nstate: {}\n---\n"+script+"<div><ul brick:ref=\"log\" brick:ignore></ul></div>",
		"components/Msg.html", "<li>{{ .Props.text }}</li>",
		"content/a.html", `<C />`,
	)})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	page, err := e.Render(ctx, "content/a.html", nil)
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`data-brick-state="([^"]+)" data-brick-token="([0-9a-f]+)"`).FindStringSubmatch(string(page))
	res, err := e.RunAction(ctx, brick.ActionRequest{Handle: m[1], Token: m[2], Action: "post", Args: []any{"<i>hi</i>"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Effects) != 2 {
		t.Fatalf("effects = %+v", res.Effects)
	}
	ins, del := res.Effects[0], res.Effects[1]
	if ins.Kind != brick.EffectRows || ins.Op != brick.RowsInsert || ins.At != brick.RowsTop || ins.Limit != 20 || len(ins.Rows) != 1 || ins.Rows[0].Key != "1" || string(ins.Rows[0].HTML) != "<li>&lt;i&gt;hi&lt;/i&gt;</li>" {
		t.Fatalf("insert = %+v", ins)
	}
	if del.Op != brick.RowsDelete || strings.Join(del.Keys, ",") != "7,x" {
		t.Fatalf("delete = %+v", del)
	}
	if _, err := e.RunAction(ctx, brick.ActionRequest{Handle: m[1], Token: m[2], Action: "raw"}); err == nil || !strings.Contains(err.Error(), "unexpected keyword argument \"html\"") {
		t.Fatalf("html argument: %v", err)
	}
	res, err = e.RunAction(ctx, brick.ActionRequest{Handle: m[1], Token: m[2], Action: "login"})
	if err != nil || len(res.Effects) != 1 || res.Effects[0].Kind != brick.EffectSubmit || res.Effects[0].Ref != "login" {
		t.Fatalf("submit = %+v, %v", res, err)
	}
}

// brick.publish publishes once the action succeeded, never from
// an action that failed, and is an error with push off or a bad topic.
func TestBrickPublish(t *testing.T) {
	t.Parallel()
	sc, err := New(Options{})
	if err != nil {
		t.Fatal(err)
	}
	script := "<brick:script>\n" +
		"def ping(state, props):\n" +
		"    brick.publish(\"orders\")\n" +
		"    return state\n\n" +
		"def boom(state, props):\n" +
		"    brick.publish(\"orders\")\n" +
		"    fail(\"no\")\n\n" +
		"def bad(state, props):\n" +
		"    brick.publish(\"not a topic\")\n" +
		"    return state\n" +
		"</brick:script>\n"
	fsys := files("components/C.html", "---\nstate: {}\n---\n"+script+"<div>c</div>", "content/a.html", `<C />`)
	b := brick.NewMemoryBroadcaster()
	push, err := brick.NewPush(brick.PushOptions{Broadcaster: b})
	if err != nil {
		t.Fatal(err)
	}
	e, err := brick.New(brick.Options{Scripting: sc, States: brick.NewMemoryStates(0, 0), Push: push, FS: fsys})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	ch, cancel, err := b.Subscribe(ctx, []string{"orders"})
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	page, err := e.Render(ctx, "content/a.html", nil)
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`data-brick-state="([^"]+)" data-brick-token="([0-9a-f]+)"`).FindStringSubmatch(string(page))
	if _, err := e.RunAction(ctx, brick.ActionRequest{Handle: m[1], Token: m[2], Action: "boom"}); err == nil {
		t.Fatal("boom succeeded")
	}
	if _, err := e.RunAction(ctx, brick.ActionRequest{Handle: m[1], Token: m[2], Action: "bad"}); err == nil || !strings.Contains(err.Error(), "not a topic") {
		t.Fatalf("bad topic: %v", err)
	}
	select {
	case got := <-ch:
		t.Fatalf("a failed action published %q", got)
	default:
	}
	if _, err := e.RunAction(ctx, brick.ActionRequest{Handle: m[1], Token: m[2], Action: "ping"}); err != nil {
		t.Fatal(err)
	}
	if got := <-ch; got != "orders" {
		t.Fatalf("published %q", got)
	}
	// Without Options.Push the call is an error naming why.
	off, err := brick.New(brick.Options{Scripting: sc, States: brick.NewMemoryStates(0, 0), FS: fsys})
	if err != nil {
		t.Fatal(err)
	}
	page, err = off.Render(ctx, "content/a.html", nil)
	if err != nil {
		t.Fatal(err)
	}
	m = regexp.MustCompile(`data-brick-state="([^"]+)" data-brick-token="([0-9a-f]+)"`).FindStringSubmatch(string(page))
	if _, err := off.RunAction(ctx, brick.ActionRequest{Handle: m[1], Token: m[2], Action: "ping"}); err == nil || !strings.Contains(err.Error(), "server push is off") {
		t.Fatalf("push off: %v", err)
	}
}
