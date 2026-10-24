package main

import (
	"net/http"
	"os"
	"strings"
	"testing"
)

// docpExcerpts are lines the "Building an app" page quotes from the apps'
// sources. Each must still appear, verbatim, in its file, so the page
// cannot drift from the code it teaches.
var docpExcerpts = map[string][]string{
	"apps/contacts/ContactsApp.html": {
		`{{ $v := contacts.view .State }}`,
		`<SearchInput brick:model.debounce.300ms="q" value="{{ .State.q }}" placeholder="Search name, e-mail, company…" shortcut="/" size="sm" class="ct-search" aria-label="Search contacts" />`,
		`<Field label="Name" :error=".Errors.name" required><Input name="name" value="{{ .State.form.name }}" placeholder="Ada Lovelace" autocomplete="off" /></Field>`,
		`:open=".State.form.open" closeAction="contacts.close_form" :dismissible="false">`,
		`<Pagination :page="$v.page" :total="$v.total" :perPage="$v.per" href="#page-%d" action="contacts.goto" size="sm" label="Contacts pages" />`,
	},
	"apps/contacts/contacts.star": {
		"def goto(state, props, page):\n    return dict(state, page = int(page), pageq = state[\"q\"].strip())",
		"    db.exec(X_DELETE, id)\n    brick.dispatch(\"contact-deleted\", {\"ids\": [id]})\n    brick.flash(\"Deleted \" + found[0][\"name\"] + \".\", \"success\")",
		"def _rows(key, args):\n    if key == \"name_asc\":\n        return db.query(Q_NAME_ASC, *args)",
		"        return [dict(state, form = f), errors]",
	},
	"main.go": {
		`Globals: starlark.StringDict{"today": today, "sanitize_html": sanitizeHTML(policy), "pause": pause},`,
		`Request: requestValues, BasePath: base, KeepPaths: keep, StateKeys: keys})`,
		`keys, err := demosite.StateKeys(os.LookupEnv, dev, log.Printf)`,
		`return []brick.RenderOption{brick.Provide(queryKey, q)}`,
	},
	"apps/contacts/ContactCount.html": {"brick:\n  on:\n    contact-added: $refresh\n    contact-deleted: $refresh"},
	"demodb/db.go":                    {`var seedEpoch = time.Date(2026, time.January, 5, 0, 0, 0, 0, time.UTC)`},
	"apps/auth/AuthSignInForm.html":   {"inject:\n  query: {default: {}}", "{{- if and (not .State.touched) (eq (print (index .Ctx.query `mode`)) `register`) }}{{ $mode = `register` }}{{ end -}}"},
	"apps_landing_test.go":            {"code, out := postAction(s.t, s.h, s.handle, s.token, action, args...)"},
	"main_test.go":                    {`body, err := json.Marshal(map[string]any{"handle": handle, "token": token, "action": action, "args": args})`, `req := httptest.NewRequest(http.MethodPost, actionPath, bytes.NewReader(body))`, `req.Header.Set("Brick-Action", "1")`},
}

// Both Getting started pages render with their key content, and every
// excerpt still matches its source file.
func TestDocpPages(t *testing.T) {
	_, h := showcase(t)
	pages := map[string][]string{
		"/building-apps/": {
			"<h1>Building an app</h1>", "1 · The page: layout, AppShell, AppNav", "4 · SQL is literal", "13 · Determinism",
			"contacts.close_form", "brick:model.debounce.300ms", "/_brick/action", "seedEpoch", `href="/patterns/"`,
			"&lt;ContactsApp /&gt;", // the page file shown whole through src=
		},
		"/patterns/": {
			"<h1>Patterns &amp; gotchas</h1>", "Mounts must be idempotent", "Dialog open state belongs to the server",
			"Give every flash toast a new id", `brick:click=&#39;cms.filter_status(&#34;draft&#34;)&#39;`, "unknown conversion %0",
			"[class*=&#34;bk-&#34;][hidden]", "Conditional children and .Children", "Per-request values live in islands",
			"Quote YAML flow values", "Listen on the form, not on document",
		},
	}
	for url, wants := range pages {
		code, body := get(t, h, url)
		if code != http.StatusOK {
			t.Errorf("%s: status %d", url, code)
			continue
		}
		mustContain(t, url, body, wants...)
	}
	for file, lines := range docpExcerpts {
		b, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, l := range lines {
			if !strings.Contains(string(b), l) {
				t.Errorf("%s no longer contains the excerpt %q: update content/building-apps.html", file, l)
			}
		}
	}
}
