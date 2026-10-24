package site

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/ubgo/brick"
)

// No-JavaScript forms, through the real site handler.

// formComponent is a sign-up form: name is a named field the action reads,
// agree and plan are brick:model controls (sets), role is locked.
const formComponent = "---\nstate:\n  name: \"\"\n  agree: false\n  plan: free\n  tags: []\n  role: user\n  saved: false\nbrick: {locked: [role]}\n---\n" +
	"<brick:script>save</brick:script>\n" +
	`<form brick:submit="save" class="f"><input name="name" value="{{ .State.name }}"><input type="checkbox" brick:model="agree"{{ if .State.agree }} checked{{ end }}><select brick:model.lazy="plan"><option>free</option><option>pro</option></select><input type="checkbox" brick:model="tags" value="go"><input type="checkbox" brick:model="tags" value="js"><i>{{ with .Errors.name }}E:{{ . }}{{ end }}</i><b>{{ .State.saved }}|{{ .State.name }}|{{ .State.agree }}|{{ .State.plan }}|{{ .State.tags }}|{{ .State.role }}</b></form>`

func formSite(extra ...string) fstest.MapFS {
	fsys := blog()
	fsys["components/Signup.html"] = &fstest.MapFile{Data: []byte(formComponent)}
	fsys["content/signup.html"] = &fstest.MapFile{Data: []byte("<Signup />")}
	for i := 0; i+1 < len(extra); i += 2 {
		fsys[extra[i]] = &fstest.MapFile{Data: []byte(extra[i+1])}
	}
	return fsys
}

// saveScripting's save is the action: a blank name is an error returned as
// [state, errors]; a name "go" redirects to /about/.
type saveScripting struct{}

func (saveScripting) Compile(context.Context, brick.ScriptSource) (brick.ScriptModule, error) {
	return saveModule{}, nil
}

type saveModule struct{}

func (saveModule) Functions() []string { return []string{"save"} }
func (saveModule) Call(ctx context.Context, _ string, args ...any) (any, error) {
	st := map[string]any{}
	for k, v := range args[0].(map[string]any) {
		st[k] = v
	}
	var fields map[string]any
	if len(args) > 2 {
		fields, _ = args[2].(map[string]any)
	}
	name, _ := fields["name"].(string)
	st["name"] = name
	if strings.TrimSpace(name) == "" {
		return []any{st, map[string]any{"name": "Enter a name."}}, nil
	}
	if name == "go" {
		if err := brick.EffectsFrom(ctx).Redirect("/about/"); err != nil {
			return nil, err
		}
	}
	st["saved"] = true
	return st, nil
}

var hiddenRE = regexp.MustCompile(`<input type="hidden" name="(brick:[a-z]+)" value="([^"]*)">`)

// formFields reads the page's form: its hidden fields, unescaped.
func formFields(t *testing.T, body string) url.Values {
	t.Helper()
	v := url.Values{}
	for _, m := range hiddenRE.FindAllStringSubmatch(body, -1) {
		v.Set(m[1], html.UnescapeString(m[2]))
	}
	if v.Get("brick:state") == "" {
		t.Fatalf("no hidden fields in the page:\n%s", body)
	}
	return v
}

// post sends a form post the way a browser with scripts off does.
func post(h http.Handler, target string, form url.Values, cookies []*http.Cookie, hdr map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	for k, v := range hdr {
		if v == "" {
			req.Header.Del(k)
		} else {
			req.Header.Set(k, v)
		}
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func getWith(h http.Handler, target string, cookies []*http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, target, nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func newFormSite(t *testing.T, opts Options) http.Handler {
	t.Helper()
	opts.Engine.Scripting = saveScripting{}
	return newSite(t, formSite(), opts).Handler()
}

// The rendered form posts to its own page and carries what the runtime
// sends: the handle, the token and the call, and a name for each
// brick:model control, listed with its kind.
func TestFormRewritten(t *testing.T) {
	t.Parallel()
	h := newFormSite(t, Options{})
	rec, handle, token := get(t, h, "/signup/")
	body := rec.Body.String()
	for _, want := range []string{
		`data-brick-arrays="tags" brick:submit="save" class="f" method="post">`,
		`<input type="checkbox" brick:model="agree" name="brick:model:agree">`,
		`<select brick:model.lazy="plan" name="brick:model:plan">`,
		`name="brick:model:tags"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, ` action=`) {
		t.Errorf("the form must post to its own page, not name an action URL")
	}
	f := formFields(t, body)
	if f.Get("brick:state") != handle || f.Get("brick:token") != token || f.Get("brick:action") != "save" {
		t.Errorf("hidden fields %v, root %s %s", f, handle, token)
	}
	if got := f.Get("brick:models"); got != `[["c","agree",""],["v","plan",""],["a","tags",""]]` {
		t.Errorf("brick:models = %s", got)
	}
}

// Valid post: 303 back to the page, which then shows the new state; the
// cookie is consumed by that page. Model fields land as sets: checked box
// true, select value, checkbox group as a list.
func TestFormPostRedirectsAndResumes(t *testing.T) {
	t.Parallel()
	h := newFormSite(t, Options{})
	rec, _, _ := get(t, h, "/signup/?x=1")
	f := formFields(t, rec.Body.String())
	f.Set("name", "Ada")
	f.Set("brick:model:agree", "on")
	f.Set("brick:model:plan", "pro")
	f["brick:model:tags"] = []string{"go", "js"}
	res := post(h, "/signup/?x=1", f, nil, nil)
	if res.Code != http.StatusSeeOther || res.Header().Get("Location") != "/signup/?x=1" {
		t.Fatalf("post: %d → %q\n%s", res.Code, res.Header().Get("Location"), res.Body)
	}
	cookies := res.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != "brick-form" || !cookies[0].HttpOnly || cookies[0].MaxAge != 60 || cookies[0].SameSite != http.SameSiteLaxMode {
		t.Fatalf("resume cookie: %+v", cookies)
	}
	page := getWith(h, "/signup/?x=1", cookies)
	if want := "<b>true|Ada|true|pro|[go js]|user</b>"; !strings.Contains(page.Body.String(), want) {
		t.Errorf("redirected page lacks %q:\n%s", want, page.Body)
	}
	if c := page.Result().Cookies(); len(c) != 1 || c[0].MaxAge >= 0 {
		t.Errorf("the page must clear the cookie: %+v", c)
	}
	// Unchecked boxes post nothing: a bool goes false, a list empty; an
	// absent value field is left alone.
	f2 := formFields(t, page.Body.String())
	f2.Set("name", "Ada")
	res = post(h, "/signup/?x=1", f2, nil, nil)
	page = getWith(h, "/signup/?x=1", res.Result().Cookies())
	if want := "<b>true|Ada|false|pro|[]|user</b>"; !strings.Contains(page.Body.String(), want) {
		t.Errorf("after unchecking, page lacks %q:\n%s", want, page.Body)
	}
	// Without the cookie the page renders fresh.
	if fresh := getWith(h, "/signup/", nil); !strings.Contains(fresh.Body.String(), "<b>false||false|free|[]|user</b>") {
		t.Errorf("fresh page:\n%s", fresh.Body)
	}
}

// An action that returns errors answers 422 with the page itself: the
// error and the typed value show, and nothing is redirected.
func TestFormPostErrorsRenderThePage(t *testing.T) {
	t.Parallel()
	h := newFormSite(t, Options{})
	rec, _, _ := get(t, h, "/signup/")
	f := formFields(t, rec.Body.String())
	f.Set("name", "  ")
	f.Set("brick:model:plan", "pro")
	res := post(h, "/signup/", f, nil, nil)
	body := res.Body.String()
	if res.Code != http.StatusUnprocessableEntity || res.Header().Get("Location") != "" {
		t.Fatalf("errors: %d %q", res.Code, res.Header().Get("Location"))
	}
	for _, want := range []string{"<i>E:Enter a name.</i>", "<b>false|  |false|pro|[]|user</b>", `<html>`} {
		if !strings.Contains(body, want) {
			t.Errorf("422 page lacks %q:\n%s", want, body)
		}
	}
	if got := res.Header().Get("Cache-Control"); !strings.HasPrefix(got, "private") {
		t.Errorf("Cache-Control = %q", got)
	}
	// The 422 page's own form posts on from where it is.
	f = formFields(t, body)
	f.Set("name", "Ada")
	if res := post(h, "/signup/", f, nil, nil); res.Code != http.StatusSeeOther {
		t.Errorf("second post: %d %s", res.Code, res.Body)
	}
}

// An action's redirect effect is the post's target.
func TestFormPostFollowsRedirectEffect(t *testing.T) {
	t.Parallel()
	h := newFormSite(t, Options{})
	rec, _, _ := get(t, h, "/signup/")
	f := formFields(t, rec.Body.String())
	f.Set("name", "go")
	res := post(h, "/signup/", f, nil, nil)
	if res.Code != http.StatusSeeOther || res.Header().Get("Location") != "/about/" {
		t.Errorf("redirect effect: %d → %q", res.Code, res.Header().Get("Location"))
	}
}

// Every refusal: cross-site posts (Sec-Fetch-Site, Origin, neither), a
// wrong token, a locked field, a client-only call, an unknown action, a
// missing field, and a planted cookie with a foreign token.
func TestFormPostRefusals(t *testing.T) {
	t.Parallel()
	h := newFormSite(t, Options{})
	rec, _, _ := get(t, h, "/signup/")
	good := formFields(t, rec.Body.String())
	good.Set("name", "Ada")
	with := func(k, v string) url.Values {
		f := url.Values{}
		for kk, vv := range good {
			f[kk] = vv
		}
		f.Set(k, v)
		return f
	}
	cases := []struct {
		name   string
		form   url.Values
		hdr    map[string]string
		status int
	}{
		{"cross-site", good, map[string]string{"Sec-Fetch-Site": "cross-site"}, http.StatusForbidden},
		{"same-site", good, map[string]string{"Sec-Fetch-Site": "same-site"}, http.StatusForbidden},
		{"foreign origin", good, map[string]string{"Sec-Fetch-Site": "", "Origin": "https://evil.example"}, http.StatusForbidden},
		{"null origin", good, map[string]string{"Sec-Fetch-Site": "", "Origin": "null"}, http.StatusForbidden},
		{"no origin", good, map[string]string{"Sec-Fetch-Site": ""}, http.StatusForbidden},
		{"wrong token", with("brick:token", strings.Repeat("0", 32)), nil, http.StatusForbidden},
		{"locked field", with("brick:models", `[["v","role",""]]`), nil, http.StatusUnprocessableEntity},
		{"client-only call", with("brick:action", `$dispatch("x")`), nil, http.StatusBadRequest},
		{"unknown action", with("brick:action", "drop"), nil, http.StatusBadRequest},
		{"no token", with("brick:token", ""), nil, http.StatusBadRequest},
	}
	for _, c := range cases {
		form := c.form
		if c.name == "locked field" {
			form.Set("brick:model:role", "admin")
		}
		res := post(h, "/signup/", form, nil, c.hdr)
		if res.Code != c.status {
			t.Errorf("%s: %d, want %d: %s", c.name, res.Code, c.status, res.Body)
		}
		if c.name == "client-only call" && !strings.Contains(res.Body.String(), "runs in the browser only") {
			t.Errorf("client-only call refused for the wrong reason: %s", res.Body)
		}
		if c.name == "locked field" && !strings.Contains(res.Body.String(), "state.role is locked") {
			t.Errorf("locked field refused for the wrong reason: %s", res.Body)
		}
	}
	// A same-origin Origin without Sec-Fetch-Site (an older browser) passes.
	if res := post(h, "/signup/", good, nil, map[string]string{"Sec-Fetch-Site": "", "Origin": "http://example.com"}); res.Code != http.StatusSeeOther {
		t.Errorf("same-origin Origin: %d %s", res.Code, res.Body)
	}
	// A cookie naming a real handle with the wrong token renders fresh.
	res := post(h, "/signup/", good, nil, nil)
	planted := res.Result().Cookies()
	if len(planted) != 1 {
		t.Fatalf("no cookie: %+v", planted)
	}
	forged := *planted[0]
	forged.Value = forgeToken(t, forged.Value)
	if page := getWith(h, "/signup/", []*http.Cookie{&forged}); !strings.Contains(page.Body.String(), "<b>false||false|free|[]|user</b>") {
		t.Errorf("a forged token must render fresh:\n%s", page.Body)
	}
	// A POST to a page without the form fields is still refused.
	req := httptest.NewRequest(http.MethodPost, "/signup/", strings.NewReader("a=1"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("plain POST: %d", w.Code)
	}
}

// Another session's instance is never resumed: the session binding holds
// for the cookie as for an action.
func TestFormResumeNeedsTheSession(t *testing.T) {
	t.Parallel()
	h := newFormSite(t, Options{Engine: brick.Options{Sessions: brick.SessionFunc(func(r *http.Request) (string, error) { return r.Header.Get("X-User"), nil })}})
	req := httptest.NewRequest(http.MethodGet, "/signup/", nil)
	req.Header.Set("X-User", "ada")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	f := formFields(t, rec.Body.String())
	f.Set("name", "Ada")
	res := post(h, "/signup/", f, nil, map[string]string{"X-User": "ada"})
	if res.Code != http.StatusSeeOther {
		t.Fatalf("post: %d %s", res.Code, res.Body)
	}
	cookies := res.Result().Cookies()
	for user, want := range map[string]string{"ada": "<b>true|Ada|", "eve": "<b>false||"} {
		req := httptest.NewRequest(http.MethodGet, "/signup/", nil)
		req.Header.Set("X-User", user)
		for _, c := range cookies {
			req.AddCookie(c)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("%s sees:\n%s", user, rec.Body)
		}
	}
}

// Options.NoFormPosts leaves forms alone and refuses posts.
func TestNoFormPosts(t *testing.T) {
	t.Parallel()
	h := newFormSite(t, Options{Engine: brick.Options{NoFormPosts: true}})
	rec, _, _ := get(t, h, "/signup/")
	if strings.Contains(rec.Body.String(), `method="post"`) || strings.Contains(rec.Body.String(), "brick:token") {
		t.Errorf("form rewritten with NoFormPosts:\n%s", rec.Body)
	}
	if res := post(h, "/signup/", url.Values{"brick:action": {"save"}}, nil, nil); res.Code != http.StatusMethodNotAllowed {
		t.Errorf("post with NoFormPosts: %d", res.Code)
	}
}

var base64RawURL = base64.RawURLEncoding

// forgeToken rewrites the token inside a resume cookie's value.
func forgeToken(t *testing.T, v string) string {
	t.Helper()
	var fr brick.FormResume
	b, err := base64RawURL.DecodeString(v)
	if err != nil || json.Unmarshal(b, &fr) != nil {
		t.Fatalf("cookie value %q", v)
	}
	fr.Token = strings.Repeat("0", 32)
	b, _ = json.Marshal(fr)
	return base64RawURL.EncodeToString(b)
}
