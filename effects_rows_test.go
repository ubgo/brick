package brick

import (
	"context"
	"strings"
	"testing"
)

// Tests for row streams and the submit effect. BV
// in a comment marks a break-verified test.

// rowsScripting is the chat component's script: actions that record rows
// and submit effects, each reading what to record from rowsCase.
func rowsScripting(spec *RowsSpec) ctxScripting {
	state := func(a []any) map[string]any { st, _ := a[0].(map[string]any); return st }
	return ctxScripting{
		"chat": {
			"post": func(ctx context.Context, a ...any) (any, error) {
				return state(a), EffectsFrom(ctx).Rows(ctx, *spec)
			},
			"login": func(ctx context.Context, a ...any) (any, error) {
				return state(a), EffectsFrom(ctx).Submit("login")
			},
		},
	}
}

const chatFile = "---\nstate: {}\n---\n<brick:script>chat</brick:script>\n<div><ul brick:ref=\"log\" brick:ignore></ul></div>"

// rowsAction renders the chat page and runs action, returning its effects.
func rowsAction(t *testing.T, spec RowsSpec, action string) ([]Effect, error) {
	t.Helper()
	e := effectsEngine(t, Options{Scripting: rowsScripting(&spec)},
		"components/Chat.html", chatFile,
		"components/Msg.html", "<li class=\"msg\">{{ .Props.text }}</li>",
		"content/c.html", `<Chat />`)
	page, err := fillFile(t, e, "content/c.html")
	if err != nil {
		t.Fatal(err)
	}
	h, tok := handleOf(t, page)
	res, err := e.RunAction(context.Background(), ActionRequest{Handle: h, Token: tok, Action: action})
	if err != nil {
		return nil, err
	}
	return res.Effects, nil
}

// BV: rendering a row with text/template instead of RenderComponent (or
// letting RowsSpec carry raw HTML) puts the <script> into the page.
func TestRowsRenderEscaped(t *testing.T) {
	effs, err := rowsAction(t, RowsSpec{Ref: "log", Component: "Msg", At: RowsTop, Limit: 50, Items: []map[string]any{
		{"id": 7, "text": `<script>alert(1)</script>`},
		{"id": "b", "text": "hi"},
	}}, "post")
	if err != nil {
		t.Fatal(err)
	}
	if len(effs) != 1 {
		t.Fatalf("effects = %+v", effs)
	}
	ef := effs[0]
	if ef.Kind != EffectRows || ef.Op != RowsInsert || ef.At != RowsTop || ef.Limit != 50 || ef.Ref != "log" || len(ef.Rows) != 2 {
		t.Fatalf("rows effect = %+v", ef)
	}
	if ef.Rows[0].Key != "7" || string(ef.Rows[0].HTML) != `<li class="msg">&lt;script&gt;alert(1)&lt;/script&gt;</li>` {
		t.Fatalf("row 0 = %+v", ef.Rows[0])
	}
	if ef.Rows[1].Key != "b" || string(ef.Rows[1].HTML) != `<li class="msg">hi</li>` {
		t.Fatalf("row 1 = %+v", ef.Rows[1])
	}
}

func TestRowsDeleteAndReset(t *testing.T) {
	effs, err := rowsAction(t, RowsSpec{Ref: "log", Op: RowsDelete, Keys: []string{"7", "8"}}, "post")
	if err != nil || len(effs) != 1 || effs[0].Op != RowsDelete || len(effs[0].Keys) != 2 || effs[0].At != "" {
		t.Fatalf("delete = %+v, %v", effs, err)
	}
	// A reset with no items empties the list; the default end is bottom.
	effs, err = rowsAction(t, RowsSpec{Ref: "log", Op: RowsReset}, "post")
	if err != nil || len(effs) != 1 || effs[0].Op != RowsReset || effs[0].At != RowsBottom || len(effs[0].Rows) != 0 {
		t.Fatalf("reset = %+v, %v", effs, err)
	}
	// key names the field rows are keyed by.
	effs, err = rowsAction(t, RowsSpec{Ref: "log", Component: "Msg", Key: "slug", Items: []map[string]any{{"slug": "x", "text": "a"}}}, "post")
	if err != nil || effs[0].Rows[0].Key != "x" {
		t.Fatalf("key field = %+v, %v", effs, err)
	}
}

func TestRowsRefused(t *testing.T) {
	msg := func(items ...map[string]any) RowsSpec { return RowsSpec{Ref: "log", Component: "Msg", Items: items} }
	for name, tc := range map[string]struct {
		spec RowsSpec
		want string
	}{
		"bad op":          {RowsSpec{Ref: "log", Op: "upsert", Component: "Msg", Items: []map[string]any{{"id": 1}}}, `op "upsert"`},
		"bad end":         {RowsSpec{Ref: "log", Component: "Msg", At: "middle", Items: []map[string]any{{"id": 1}}}, `at "middle"`},
		"negative limit":  {RowsSpec{Ref: "log", Component: "Msg", Limit: -1, Items: []map[string]any{{"id": 1}}}, "negative"},
		"bad ref":         {RowsSpec{Ref: "a b", Component: "Msg", Items: []map[string]any{{"id": 1}}}, "brick:ref name"},
		"empty insert":    {RowsSpec{Ref: "log"}, "an insert has no rows"},
		"no component":    {RowsSpec{Ref: "log", Items: []map[string]any{{"id": 1}}}, "needs the component"},
		"no key field":    {msg(map[string]any{"text": "a"}), `no "id" field`},
		"duplicate key":   {msg(map[string]any{"id": 1}, map[string]any{"id": "1"}), `"1" appears twice`},
		"delete no keys":  {RowsSpec{Ref: "log", Op: RowsDelete}, "names the keys"},
		"delete items":    {RowsSpec{Ref: "log", Op: RowsDelete, Component: "Msg", Items: []map[string]any{{"id": 1}}}, "takes keys, not items"},
		"keys on insert":  {RowsSpec{Ref: "log", Component: "Msg", Keys: []string{"1"}, Items: []map[string]any{{"id": 1}}}, "keys are for a delete"},
		"unknown":         {RowsSpec{Ref: "log", Component: "Nope", Items: []map[string]any{{"id": 1}}}, "item 0"},
		"control in key":  {RowsSpec{Ref: "log", Op: RowsDelete, Keys: []string{"a\nb"}}, "control characters"},
		"overlong key":    {RowsSpec{Ref: "log", Op: RowsDelete, Keys: []string{strings.Repeat("k", maxRowKey+1)}}, "1 to 128"},
		"empty key value": {RowsSpec{Ref: "log", Op: RowsDelete, Keys: []string{""}}, "1 to 128"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := rowsAction(t, tc.spec, "post")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
}

// A hand-built rows effect goes through the same checks, and a row that
// rendered nothing is refused.
func TestRowsAddChecked(t *testing.T) {
	eff := &Effects{}
	if err := eff.Add(Effect{Kind: EffectRows, Ref: "log", Op: RowsInsert, Rows: []Row{{Key: "1", HTML: " "}}}); err == nil || !strings.Contains(err.Error(), "rendered nothing") {
		t.Fatalf("blank row: %v", err)
	}
	if err := eff.Rows(context.Background(), RowsSpec{Ref: "log", Component: "Msg", Items: []map[string]any{{"id": 1}}}); err == nil || !strings.Contains(err.Error(), "no engine") {
		t.Fatalf("engineless collector: %v", err)
	}
	var none *Effects
	if err := none.Rows(context.Background(), RowsSpec{Ref: "log", Op: RowsReset}); err == nil {
		t.Fatal("a nil collector recorded rows")
	}
}

func TestSubmitEffect(t *testing.T) {
	effs, err := rowsAction(t, RowsSpec{}, "login")
	if err != nil || len(effs) != 1 || effs[0].Kind != EffectSubmit || effs[0].Ref != "login" {
		t.Fatalf("submit = %+v, %v", effs, err)
	}
	eff := &Effects{}
	if err := eff.Submit(""); err == nil {
		t.Fatal("an empty ref was accepted")
	}
	// Submit leaves the page, so it is terminal: the later exit wins.
	if err := eff.Submit("login"); err != nil {
		t.Fatal(err)
	}
	if err := eff.Redirect("/home"); err != nil {
		t.Fatal(err)
	}
	if l := eff.List(); len(l) != 1 || l[0].Kind != EffectRedirect {
		t.Fatalf("terminal effects = %+v", l)
	}
	if err := eff.Submit("login"); err != nil {
		t.Fatal(err)
	}
	if l := eff.List(); len(l) != 1 || l[0].Kind != EffectSubmit {
		t.Fatalf("terminal effects = %+v", l)
	}
}
