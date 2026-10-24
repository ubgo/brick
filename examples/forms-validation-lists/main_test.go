package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/ubgo/brick"
	scriptstarlark "github.com/ubgo/brick/contrib/script-starlark"
	"github.com/ubgo/brick/validate"
)

// instanceRE reads each stateful root's handle and token, in page order
// (Signup first, Todos second).
var instanceRE = regexp.MustCompile(`data-brick-state="([^"]+)" data-brick-token="([0-9a-f]+)"`)

// post sends one JSON action the way runtime.js does.
func post(t *testing.T, h http.Handler, body string) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/_brick/action", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Brick-Action", "1")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("%s: %d %s", body, rec.Code, rec.Body)
	}
	return rec.Body.String()
}

// mustHave fails unless every want is in got.
func mustHave(t *testing.T, step, got string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(got, w) {
			t.Fatalf("%s: missing %s in\n%s", step, w, got)
		}
	}
}

// start renders the page and returns the handler and each root's
// handle-and-token prefix for an action body.
func start(t *testing.T) (http.Handler, string, string, string) {
	t.Helper()
	eng, err := newApp("theme")
	if err != nil {
		t.Fatal(err)
	}
	h := handler(eng)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	page := rec.Body.String()
	ms := instanceRE.FindAllStringSubmatch(page, -1)
	if len(ms) != 2 {
		t.Fatalf("want two stateful roots:\n%s", page)
	}
	inst := func(m []string) string { return `{"handle":"` + m[1] + `","token":"` + m[2] + `",` }
	return h, page, inst(ms[0]), inst(ms[1])
}

// Errors show only for touched fields; brick.validate touches every field
// with rules; an action's own error wins for its field; brick.untouch
// leaves the emptied form quiet.
// ds:def id=testsignuprules-uwzra6ps
func TestSignupRules(t *testing.T) {
	h, page, signup, _ := start(t)
	if strings.Contains(page, `class="error"`) {
		t.Fatalf("errors before anything is touched:\n%s", page)
	}
	out := post(t, h, signup+`"action":"$set","args":["email","nope"]}`)
	mustHave(t, "$set email", out, `<p class="error">email: Enter a valid email address.</p>`)
	if strings.Contains(out, "name: ") {
		t.Fatalf("an untouched field shows an error:\n%s", out)
	}
	out = post(t, h, signup+`"action":"signup"}`)
	mustHave(t, "submit", out,
		`<p class="error">name: This field is required.</p>`,
		`<p class="error">email: Enter a valid email address.</p>`,
		`<p class="error">password: This field is required.</p>`,
		`<p class="error">confirm: This field is required.</p>`)
	out = post(t, h, signup+`"action":"signup","sets":[["name","Ada"],["email","ada@taken.example"],["password","correct horse battery"],["confirm","correct horse batter"]]}`)
	mustHave(t, "mismatch", out, `<p class="error">confirm: The passwords do not match.</p>`)
	out = post(t, h, signup+`"action":"signup","sets":[["confirm","correct horse battery"]]}`)
	mustHave(t, "taken", out, `<p class="error">email: That e-mail already has an account.</p>`)
	out = post(t, h, signup+`"action":"signup","sets":[["email","ada@example.com"]]}`)
	if strings.Contains(out, `class="error"`) || !strings.Contains(out, `brick:model.lazy="name" value=""`) {
		t.Fatalf("after a good signup:\n%s", out)
	}
}

// Wildcard rules check every list item that exists when the action runs,
// keyed by the concrete path; .Form gives each row its path, id and error;
// the host's slug rule runs beside the default ones.
// ds:def id=testtodolistrules-b3czke5h
func TestTodoListRules(t *testing.T) {
	h, page, _, todos := start(t)
	mustHave(t, "page", page,
		`<label for="b537d9f9b-slug">List address</label>`,
		`<input id="b537d9f9b-todos-1-title" brick:model.lazy="todos.1.title" value="x" name="brick:model:todos.1.title">`)
	out := post(t, h, todos+`"action":"$set","args":["slug","My List"]}`)
	mustHave(t, "$set slug", out, `value="My List" aria-invalid="true"`, `<p class="error">Use lowercase letters, digits and single dashes.</p>`)
	out = post(t, h, todos+`"action":"save"}`)
	mustHave(t, "save", out,
		`<p class="all">todos.0.title=Each todo needs a title.</p>`,
		`<p class="all">todos.1.tags.0=Must be at least 3 characters.</p>`,
		`<p class="all">todos.1.title=Must be at least 2 characters.</p>`,
		`value="x" name="brick:model:todos.1.title"> <span class="error">Must be at least 2 characters.</span>`)
	out = post(t, h, todos+`"action":"save","sets":[["slug","my-list"],["todos.0.title","Buy milk"],["todos.1.title","Call"],["todos.1.tags.0","home"]]}`)
	if strings.Contains(out, `class="error"`) || strings.Contains(out, `class="all"`) {
		t.Fatalf("after fixing every row:\n%s", out)
	}
	// A row added later is checked by the next save, not before.
	out = post(t, h, todos+`"action":"add_todo"}`)
	mustHave(t, "add_todo", out, `id="b537d9f9b-todos-3-title"`)
	if strings.Contains(out, `class="error"`) {
		t.Fatalf("a new row shows an error before save:\n%s", out)
	}
	out = post(t, h, todos+`"action":"save"}`)
	mustHave(t, "save new row", out, `<p class="all">todos.3.title=Each todo needs a title.</p>`)
}

// A rule the Validator does not know, or a bad argument to the host's
// rule, fails when the file is compiled, with the file and the line.
func TestRuleTyposFailAtCompile(t *testing.T) {
	sc, err := scriptstarlark.New(scriptstarlark.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ rules, want string }{
		{`[requird]`, `unknown rule "requird" (known: required, min, max, email, url, pattern, in, same)`},
		{`[{slug: yes}]`, `slug takes no argument; write it as a bare name`},
	} {
		fsys := fstest.MapFS{
			"content/index.html": {Data: []byte(`<F />`)},
			"components/F.html":  {Data: []byte("---\nstate:\n  slug: \"\"\nbrick:\n  rules:\n    slug: " + c.rules + "\n---\n<p>{{ .State.slug }}</p>\n")},
		}
		eng, err := brick.New(brick.Options{FS: fsys, Scripting: sc, States: brick.NewMemoryStates(0, 0), Validator: slugValidator{base: validate.Default()}})
		if err != nil {
			t.Fatal(err)
		}
		_, err = eng.Render(context.Background(), "content/index.html", nil)
		if err == nil || !strings.Contains(err.Error(), c.want) || !strings.Contains(err.Error(), "components/F.html") {
			t.Fatalf("%s: %v", c.rules, err)
		}
		t.Log(err)
	}
}

// brick.errors(el) reads the errors the last render left on the root, so
// the root must carry them: a failed save writes data-brick-errors, and a
// clean one removes it. The client module that reads them is served with
// the page; that the button then shows the message is the browser's part.
func TestErrorsReadableByTheBrowser(t *testing.T) {
	h, page, _, todos := start(t)
	if strings.Contains(page, "data-brick-errors") {
		t.Fatalf("errors before anything is touched:\n%s", page)
	}
	out := post(t, h, todos+`"action":"save"}`)
	mustHave(t, "failed save", out, `data-brick-errors="`, `todos.0.title`, `data-first-error`)
	out = post(t, h, todos+`"action":"save","sets":[["slug","my-list"],["todos.0.title","Buy milk"],["todos.1.title","Call"],["todos.1.tags.0","home"]]}`)
	if strings.Contains(out, "data-brick-errors") {
		t.Fatalf("a clean save still carries errors:\n%s", out)
	}
	m := regexp.MustCompile(`src="(/_brick/c/Todos\.[0-9a-f]+\.js)"`).FindStringSubmatch(page)
	if m == nil {
		t.Fatalf("no Todos module on the page:\n%s", page)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, m[1], nil))
	if !strings.Contains(rec.Body.String(), "brick.errors(button)") {
		t.Fatalf("module lacks brick.errors:\n%s", rec.Body)
	}
}
