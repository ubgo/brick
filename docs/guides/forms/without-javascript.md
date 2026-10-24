# Forms without JavaScript, in your own router

## What it is and when you want it

Without the runtime, `<form brick:submit="save">` would be a plain form with no `method`: scripts off, a script blocked, or a slow first paint, and the browser submits it as a GET to the same page, so `save` never runs. brick rewrites every rendered `brick:submit` form to `method="post"` with the instance fields the runtime would send, and `Engine.FormPost` runs the action from that post through the same pipeline as a JSON action. The site layer (`brick serve`) answers these posts for you; this page is for a host with its own router.

## Before you start

- A stateful component with a `brick:submit` form ([Deferred fields](deferred-fields.md) shows one).
- Your own `net/http` routes for the page: a GET that renders it and a POST to the same URL.
- The example: [examples/forms-nojs](../../../examples/forms-nojs/).

## Step 1 — Write the form as usual

Nothing in the component is specific to the no-JavaScript path: named fields become the action's last argument (one dict), and a `brick:model` control becomes a set applied before the action.

`theme/components/Signup.html`

<!-- snippet: examples/forms-nojs/theme/components/Signup.html -->
```html
---
state:
  name: ""
  agree: false
  saved: false
---
<brick:script>
def save(state, props, fields):
    name = fields.get("name", "").strip()
    if not name:
        return [dict(state, name = name), {"name": "Enter a name."}]
    return dict(state, name = name, saved = True)
</brick:script>
<form brick:submit="save">
  {{ if .State.saved }}<p>Welcome, {{ .State.name }}.</p>{{ end }}
  <input name="name" value="{{ .State.name }}">
  {{ with .Errors.name }}<p class="error">{{ . }}</p>{{ end }}
  <label><input type="checkbox" brick:model="agree"{{ if .State.agree }} checked{{ end }}> I agree</label>
  <button>Sign up</button>
  <button type="button" brick:click='$dispatch("signup-cancelled")'>Cancel</button>
</form>
```

`theme/content/signup.html`

<!-- snippet: examples/forms-nojs/theme/content/signup.html -->
```html
<!doctype html>
<html><body>
<h1>Sign up</h1>
<Signup />
{{ assets }}
</body></html>
```

The rendered form gains `method="post"` (no `action`, so it posts to the page's own URL), a `name` on each `brick:model` control, and four hidden fields before `</form>`. A real render:

```html
<form data-brick="b587d71b9" data-brick-state="default.<handle>" data-brick-token="<token>" data-brick-name="Signup" brick:submit="save" method="post">
  
  <input name="name" value="">
  
  <label><input type="checkbox" brick:model="agree" name="brick:model:agree"> I agree</label>
  <button>Sign up</button>
  <button type="button" brick:click='$dispatch("signup-cancelled")'>Cancel</button>
<input type="hidden" name="brick:state" value="default.ba1106f3989e832f9c1fe725e689c9dd"><input type="hidden" name="brick:token" value="cc87dacd3c57625967cafdec1baca8cd"><input type="hidden" name="brick:action" value="save"><input type="hidden" name="brick:models" value="[[&#34;c&#34;,&#34;agree&#34;,&#34;&#34;]]"></form>
```

The handle and token are new on every render; the `data-brick` id is stable.

The *Cancel* button is the one piece that needs a browser: `$dispatch("signup-cancelled")` tells other components on the page something happened, which takes a script. Without one the button does nothing and is left in the markup exactly as written (`TestDispatchStaysInert` pins it), and it adds no field to the post. `$parent.fn` and `$cancelUpload` behave the same way.

## Step 2 — Answer the post with `Engine.FormPost`

`IsFormPost` recognises the post (it reads the body, so call it before anything else does), `FormPost` runs the action, and the result says where to send the visitor and whether the action failed.

`main.go`

<!-- ds:block id=page-cspbmra2 title="examples/forms-nojs/main.go" -->
**examples/forms-nojs/main.go** · [`examples/forms-nojs/main.go:23`](../../../examples/forms-nojs/main.go#L23-L23)

```go
"content/signup.html"
```
<!-- /ds:block hash=3cb480 -->

<!-- ds:block id=newapp-br885xh3 title="examples/forms-nojs/main.go" -->
**examples/forms-nojs/main.go** · [`examples/forms-nojs/main.go:28-38`](../../../examples/forms-nojs/main.go#L28-L38)

```go
func newApp(dir string) (*brick.Engine, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	sc, err := scriptstarlark.New(scriptstarlark.Options{})
	if err != nil {
		return nil, err
	}
	return brick.New(brick.Options{FS: root.FS(), Scripting: sc, States: brick.NewMemoryStates(0, 0)})
}
```
<!-- /ds:block hash=7515c7 -->

<!-- ds:block id=handler-p42hdhjm title="examples/forms-nojs/main.go" -->
**examples/forms-nojs/main.go** · [`examples/forms-nojs/main.go:44-80`](../../../examples/forms-nojs/main.go#L44-L80)

```go
func handler(eng *brick.Engine) http.Handler {
	mux := http.NewServeMux()
	mux.Handle(brick.DefaultAssetBase+brick.ActionEndpoint, eng.ActionHandler())
	mux.Handle(brick.DefaultAssetBase, assetshttp.Handler(brick.DefaultAssetBase, func() assetshttp.Source { return eng }))
	mux.HandleFunc("GET /signup", func(w http.ResponseWriter, r *http.Request) {
		// FormResumeOption reads and clears the cookie a successful post
		// left, so the page shows the saved instance once.
		write(w, eng, r, http.StatusOK, eng.FormResumeOption(w, r))
	})
	mux.HandleFunc("POST /signup", func(w http.ResponseWriter, r *http.Request) {
		if !eng.IsFormPost(r) {
			http.Error(w, "not a form post", http.StatusBadRequest)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		// Engine.FormPost runs the action the hidden fields name, as the
		// JSON endpoint would, and tells us where to send the visitor.
		out, err := eng.FormPost(r)
		if err != nil {
			// A cross-site post, a bad token, an unknown action: the
			// action endpoint's status and plain message.
			eng.FormError(w, r, err)
			return
		}
		if !out.Failed && (out.Resume.Handle == "" || eng.SetFormResume(w, r, out.Resume)) {
			// Success (or the instance was gone): POST-redirect-GET.
			http.Redirect(w, r, out.Location, http.StatusSeeOther)
			return
		}
		status := http.StatusOK // the state was too big for a cookie
		if out.Failed {
			status = http.StatusUnprocessableEntity
		}
		write(w, eng, r, status, brick.WithFormResume(out.Resume))
	})
	return mux
}
```
<!-- /ds:block hash=0b99c3 -->

<!-- ds:block id=write-p34ekjrs title="examples/forms-nojs/main.go" -->
**examples/forms-nojs/main.go** · [`examples/forms-nojs/main.go:84-93`](../../../examples/forms-nojs/main.go#L84-L93)

```go
func write(w http.ResponseWriter, eng *brick.Engine, r *http.Request, status int, opts ...brick.RenderOption) {
	html, err := eng.Render(r.Context(), page, nil, opts...)
	if err != nil {
		http.Error(w, "render failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(html)
}
```
<!-- /ds:block hash=ed6d48 -->

The four outcomes the POST route handles:

| `FormPost` returned | The route answers |
|---|---|
| an error (cross-site post, bad token, unknown action) | `eng.FormError`: the action endpoint's status and plain message, or `Options.ActionErrors` |
| `out.Failed` (the action returned `[state, errors]`) | `422` with the page rendered `WithFormResume(out.Resume)`, so the errors and typed values show |
| success, and `SetFormResume` fit the state in its cookie | `303` to `out.Location`; the next GET's `FormResumeOption` shows that instance once |
| success, but the state was too big for a cookie | `200` with the page rendered `WithFormResume(out.Resume)` |

An empty `out.Resume.Handle` means the instance was gone (expired, restarted): redirect, and the page renders fresh. When `out.Effect` is set, `out.Location` is an action's `brick.redirect` or `brick.navigate` target, already checked by the open-redirect lock; a site under a base path prefixes it.

## Step 3 — Post the form the way a browser without scripts does

The test reads the hidden fields from the page and posts them back with `Sec-Fetch-Site`, as a browser does.

`main_test.go`

<!-- snippet: examples/forms-nojs/main_test.go lines=16-78 -->
```go
// A brick:submit form posted with scripts off: an empty name answers 422
// with the error on the page, a cross-site post is refused before the
// action, and a good one answers 303 with the new state in a one-minute
// cookie that the next GET consumes.
func TestSignupWithoutJavaScript(t *testing.T) {
	eng, err := newApp("theme")
	if err != nil {
		t.Fatal(err)
	}
	h := handler(eng)
	get := func(cookies []*http.Cookie) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, "/signup", nil)
		for _, c := range cookies {
			r.AddCookie(c)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		return rec
	}
	page := get(nil).Body.String()
	if !strings.Contains(page, `brick:submit="save" method="post">`) || !strings.Contains(page, `<input type="checkbox" brick:model="agree" name="brick:model:agree"> I agree</label>`) {
		t.Fatalf("form not rewritten:\n%s", page)
	}
	m := instanceRE.FindStringSubmatch(page)
	if m == nil {
		t.Fatalf("no instance fields:\n%s", page)
	}
	post := func(name, agree, site string) *httptest.ResponseRecorder {
		v := url.Values{"name": {name}, "brick:state": {m[1]}, "brick:token": {m[2]}, "brick:action": {"save"}, "brick:models": {`[["c","agree",""]]`}}
		if agree != "" {
			v.Set("brick:model:agree", agree)
		}
		r := httptest.NewRequest(http.MethodPost, "/signup", strings.NewReader(v.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("Sec-Fetch-Site", site)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		return rec
	}

	rec := post("", "", "same-origin")
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), `<p class="error">Enter a name.</p>`) {
		t.Fatalf("empty name: %d\n%s", rec.Code, rec.Body)
	}
	rec = post("Ada", "on", "cross-site")
	if rec.Code != http.StatusForbidden || strings.TrimSpace(rec.Body.String()) != "brick: action refused" {
		t.Fatalf("cross-site: %d %q", rec.Code, rec.Body)
	}

	rec = post("Ada", "on", "same-origin")
	cookie := rec.Header().Get("Set-Cookie")
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/signup" || !strings.HasPrefix(cookie, "brick-form=") || !strings.HasSuffix(cookie, "; Path=/; Max-Age=60; HttpOnly; SameSite=Lax") {
		t.Fatalf("good post: %d %q %q", rec.Code, rec.Header().Get("Location"), cookie)
	}
	after := get(rec.Result().Cookies())
	body := after.Body.String()
	if !strings.Contains(body, "<p>Welcome, Ada.</p>") || !strings.Contains(body, `brick:model="agree" checked name="brick:model:agree"`) || after.Header().Get("Set-Cookie") != "brick-form=; Path=/; Max-Age=0; HttpOnly; SameSite=Lax" {
		t.Fatalf("resume: %q\n%s", after.Header().Get("Set-Cookie"), body)
	}
	if strings.Contains(get(nil).Body.String(), "Welcome") {
		t.Fatal("a page without the cookie must render fresh")
	}
}
```

## Step 4 — Leave browser-only calls inert

A button that only makes sense with a script (a `$dispatch`, here) is not rewritten into a post. The test fetches the page and checks the button is untouched and became no hidden field.

`main_test.go`

<!-- snippet: examples/forms-nojs/main_test.go lines=80-97 -->
```go
// A call only the browser can make stays inert without scripts: the Cancel
// button's $dispatch is left in the markup as written, is not turned into
// a post, and is not among the fields the form posts back.
func TestDispatchStaysInert(t *testing.T) {
	eng, err := newApp("theme")
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	handler(eng).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/signup", nil))
	page := rec.Body.String()
	if want := `<button type="button" brick:click='$dispatch("signup-cancelled")'>Cancel</button>`; !strings.Contains(page, want) {
		t.Fatalf("Cancel button missing or rewritten, want %s:\n%s", want, page)
	}
	if strings.Contains(page, `name="brick:action" value="signup-cancelled"`) {
		t.Fatalf("a $dispatch became a form field:\n%s", page)
	}
}
```

## Try it

```sh
cd examples
go test ./forms-nojs/ -v
```

```text
=== RUN   TestSignupWithoutJavaScript
--- PASS: TestSignupWithoutJavaScript (0.00s)
=== RUN   TestDispatchStaysInert
--- PASS: TestDispatchStaysInert (0.00s)
PASS
ok  	github.com/ubgo/brick/examples/forms-nojs	0.020s
```

By hand, with `go run ./forms-nojs -addr 127.0.0.1:18085` and `curl` (cookie value shortened):

```text
POST /signup  name=&brick:state=…&brick:token=…&brick:action=save
422

POST /signup  name=Ada&brick:model:agree=on&brick:state=…&brick:token=…&brick:action=save&brick:models=[["c","agree",""]]
HTTP/1.1 303 See Other
Location: /signup
Set-Cookie: brick-form=…; Path=/; Max-Age=60; HttpOnly; SameSite=Lax

GET /signup  (with the cookie)
  <p>Welcome, Ada.</p>
  <label><input type="checkbox" brick:model="agree" checked name="brick:model:agree"> I agree</label>
```

## Pitfalls

- **A cross-site post is refused before the action.** A form post can come from another site (unlike the JSON action with its custom header), so `FormPost` refuses it unless `Sec-Fetch-Site` is `same-origin` or, when a browser sends no such header, `Origin` names the request's host. `same-site` is refused too. The test pins the answer: `403 brick: action refused`.
- **Some forms are left alone on purpose** (`nojs.go`): a form that writes its own `action` or `method`, a form with `brick:confirm` or `brick:confirm.prompt` (nobody can be asked, and a destructive action must not run unasked), and a call only the browser can make (`$dispatch`, `$parent.fn`, `$cancelUpload`). Those stay inert without scripts.
- **Call `IsFormPost` before anything reads `r.Body`.** It parses the body once (a urlencoded body whole, at most `Options.MaxActionBytes`; a multipart body up to its first upload part), and `FormPost` reads what it parsed.
- **Effects other than redirect and navigate are dropped** with scripts off: flash, title, dispatch, rows, download and URL history have nobody to run them.
- **`brick: {temporary:}` fields show their declared value after the redirect**, since the resumed instance is what was saved.
- **The resume cookie lasts 60 seconds and is read once.** `FormResumeOption` clears it (`Set-Cookie: brick-form=; Path=/; Max-Age=0`, pinned in the test), so a reload renders fresh. On HTTPS it is named `__Host-brick-form`.
- **`Options.NoFormPosts`** turns off both the rewrite and the endpoint; `FormPost` then answers `403 brick: form posts are off (Options.NoFormPosts)`.

## Related

- [Deferred fields](deferred-fields.md), [Validation](validation.md), [Uploads](uploads.md) (files in a form without JavaScript), [Method links](method-links.md).
- Reference: [Forms without JavaScript](../../guide.md#forms-without-javascript) in the one-page guide.
- Feature index rows: [Forms, validation and uploads](../../reference/features.md#forms-validation-and-uploads).
- Example: [examples/forms-nojs](../../../examples/forms-nojs/).
