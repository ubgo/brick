# Method links: Delete, Publish and Log out without a form

## What it is and when you want it

A *Delete* or *Log out* link in a list row has no form around it, and a link can only send GET. `brick:method="delete"` on an `<a href>` makes a click submit a hidden form to the href: `post` posts as is, and `put`, `patch` and `delete` post with a `_method` field that `brick.MethodOverride` turns back into the verb, so your router routes `DELETE /posts/{id}` as usual. It works in any file (no `state:` needed). The runnable program is [examples/methodlinks](../../../examples/methodlinks/); this page walks through it.

## Before you start

- Your own router for the write routes, answering with a redirect (POST-redirect-GET).
- A page that links the runtime with `{{ assets }}`; a page with a method link links it on its own.

## Step 1 — Put the verb on the link

`brick:confirm` asks first, and `brick:disable-with` marks the link busy while the request runs. A component call works too, when the component places `{{ attrs }}` on its `<a>`.

`theme/content/index.html`

<!-- snippet: examples/methodlinks/theme/content/index.html -->
```html
<!doctype html>
<html><body>
<h1>Posts</h1>
<ul>
{{ range .Props.posts }}
  <li id="post-{{ .id }}">{{ .title }}
    <a id="edit-{{ .id }}" href="/posts/{{ .id }}" brick:method="patch">Publish</a>
    <a id="delete-{{ .id }}" href="/posts/{{ .id }}" brick:method="delete" brick:confirm="Delete {{ .title }}?" brick:disable-with="Deleting…">Delete</a>
  </li>
{{ end }}
</ul>
<p><LinkButton id="create" href="/posts" brick:method="post">New post</LinkButton></p>
<p><a id="plain" href="/posts/1">A plain link (GET)</a></p>
<p id="log">{{ .Props.log }}</p>
{{ assets }}
</body></html>
```

`theme/components/LinkButton.html`

<!-- snippet: examples/methodlinks/theme/components/LinkButton.html -->
```html
<a class="button" {{ attrs }}>{{ children }}</a>
```

## Step 2 — Wrap the router once

`MethodOverride` sits in front of the mux, so the `DELETE` and `PATCH` routes see the verb the link asked for.

`main.go`

<!-- snippet: examples/methodlinks/main.go lines=44-73 -->
```go
// handler is the host's router; MethodOverride sits in front of it, so
// the DELETE and PATCH routes see the verb a method link asked for.
func (a *app) handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle(brick.DefaultAssetBase, assetshttp.Handler(brick.DefaultAssetBase, func() assetshttp.Source { return a.eng }))
	mux.HandleFunc("GET /{$}", a.index)
	mux.HandleFunc("GET /posts/{id}", func(w http.ResponseWriter, r *http.Request) {
		a.record("GET " + r.URL.Path)
		fmt.Fprintf(w, "post %s", r.PathValue("id"))
	})
	mux.HandleFunc("POST /posts", func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		a.posts[a.next] = fmt.Sprintf("Post %d", a.next)
		a.next++
		a.mu.Unlock()
		a.done(w, r, "POST "+r.URL.Path)
	})
	mux.HandleFunc("PATCH /posts/{id}", func(w http.ResponseWriter, r *http.Request) {
		a.done(w, r, "PATCH "+r.URL.Path)
	})
	mux.HandleFunc("DELETE /posts/{id}", func(w http.ResponseWriter, r *http.Request) {
		var id int
		_, _ = fmt.Sscan(r.PathValue("id"), &id)
		a.mu.Lock()
		delete(a.posts, id)
		a.mu.Unlock()
		a.done(w, r, "DELETE "+r.URL.Path)
	})
	return brick.MethodOverride(mux)
}
```

## Step 3 — Post the hidden form the way the runtime does

`main_test.go`

<!-- snippet: examples/methodlinks/main_test.go lines=10-43 -->
```go
// The router sees the verb a method link's form asks for: a same-origin
// POST with _method=delete removes the post, and the page links the
// runtime that makes the click send that form.
func TestMethodLinksApp(t *testing.T) {
	a, err := newApp("theme", false)
	if err != nil {
		t.Fatal(err)
	}
	h := a.handler()
	get := func() string {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET /: %d %s", rec.Code, rec.Body)
		}
		return rec.Body.String()
	}
	page := get()
	if !strings.Contains(page, `brick:method="delete"`) || !strings.Contains(page, "/runtime.") {
		t.Fatalf("page:\n%s", page)
	}
	r := httptest.NewRequest(http.MethodPost, "/posts/2", strings.NewReader("_method=delete"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body)
	}
	page = get()
	if strings.Contains(page, `id="post-2"`) || !strings.Contains(page, "DELETE /posts/2") {
		t.Fatalf("after delete:\n%s", page)
	}
}
```

## Step 4 — Add your CSRF token with `brick.methodFormWith`

brick has no CSRF token for plain forms, so a method link's hidden form carries none. If your app guards its write routes with one, `brick.methodFormWith(fn)` replaces the hook the form passes through before it is submitted: `fn(form, link)` may append fields. It returns a function that puts the previous hook back. The second example, [examples/forms-method-csrf](../../../examples/forms-method-csrf/), keeps the token in a `<meta>` tag and adds it from a component's client code.

`theme/components/PostRow.html`

<!-- snippet: examples/forms-method-csrf/theme/components/PostRow.html -->
```html
---
props:
  id: string
  title: string
---
<brick:client>
// The app's CSRF token rides in a <meta> tag. A method link submits a
// hidden form, so this hook appends the token to it as a field the DELETE
// route can check. fn(form, link) runs once per click, before the submit.
const restore = brick.methodFormWith((form, link) => {
  const input = document.createElement("input");
  input.type = "hidden";
  input.name = "_csrf";
  input.value = document.querySelector('meta[name="csrf-token"]').content;
  form.append(input);
});
// restore() puts the previous hook back; a page that never swaps hooks
// still releases it when it goes away.
window.addEventListener("pagehide", restore);
</brick:client>
<li id="post-{{ .Props.id }}">{{ .Props.title }}
  <a href="/posts/{{ .Props.id }}" brick:method="delete" brick:confirm="Delete {{ .Props.title }}?">Delete</a></li>
```

The page prints the token into the `<meta>` tag and places the rows:

`theme/content/index.html`

<!-- snippet: examples/forms-method-csrf/theme/content/index.html -->
```html
<!doctype html>
<html>
<head><meta name="csrf-token" content="{{ .Props.csrf }}"></head>
<body>
<h1>Posts</h1>
<ul>
<PostRow id="1" title="First post" />
<PostRow id="2" title="Second post" />
</ul>
{{ assets }}
</body></html>
```

The DELETE route reads the field from `r.PostForm`, where `MethodOverride` leaves it, and refuses a request without the right value.

`main.go`

<!-- snippet: examples/forms-method-csrf/main.go lines=60-83 -->
```go
// handler is the host's router. MethodOverride turns the hidden form's
// POST back into DELETE and leaves its fields in r.PostForm, which is
// where the route reads the token the browser hook added.
func (a *app) handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle(brick.DefaultAssetBase, assetshttp.Handler(brick.DefaultAssetBase, func() assetshttp.Source { return a.eng }))
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		html, err := a.eng.Render(r.Context(), page, map[string]any{"csrf": a.token})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(html)
	})
	mux.HandleFunc("DELETE /posts/{id}", func(w http.ResponseWriter, r *http.Request) {
		if subtle.ConstantTimeCompare([]byte(r.PostFormValue(csrfField)), []byte(a.token)) != 1 {
			http.Error(w, "csrf token missing or wrong", http.StatusForbidden)
			return
		}
		fmt.Fprintf(w, "deleted post %s", r.PathValue("id"))
	})
	return brick.MethodOverride(mux)
}
```

Clicking the link, and the hook running before the submit, only a browser does. The test checks what Go owns: the page carries the token and the method link, the `PostRow` module holding `brick.methodFormWith(` is served, and the route answers `403` without the token or with a wrong one and `200` with the right one.

`main_test.go`

<!-- snippet: examples/forms-method-csrf/main_test.go lines=30-69 -->
```go
// The route refuses a method link's form until the hook has added the
// token; the page carries the token and the module that adds it. That the
// hook runs on a click, and the form is then submitted, only a browser
// shows: the test posts the form the hook would build.
func TestMethodFormCarriesCSRFToken(t *testing.T) {
	a, err := newApp("theme")
	if err != nil {
		t.Fatal(err)
	}
	h := a.handler()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	page := rec.Body.String()
	if want := `<meta name="csrf-token" content="` + a.token + `">`; !strings.Contains(page, want) {
		t.Fatalf("page lacks %s:\n%s", want, page)
	}
	if !strings.Contains(page, `<a href="/posts/2" brick:method="delete" brick:confirm="Delete Second post?">Delete</a>`) {
		t.Fatalf("method link missing:\n%s", page)
	}
	m := moduleRE.FindStringSubmatch(page)
	if m == nil {
		t.Fatalf("no PostRow module on the page:\n%s", page)
	}
	js := httptest.NewRecorder()
	h.ServeHTTP(js, httptest.NewRequest(http.MethodGet, m[1], nil))
	if !strings.Contains(js.Body.String(), "brick.methodFormWith(") {
		t.Fatalf("module lacks brick.methodFormWith:\n%s", js.Body)
	}

	if rec := deleteForm(h, nil); rec.Code != http.StatusForbidden {
		t.Fatalf("without the token: %d %s", rec.Code, rec.Body)
	}
	if rec := deleteForm(h, url.Values{csrfField: {"wrong"}}); rec.Code != http.StatusForbidden {
		t.Fatalf("with a wrong token: %d %s", rec.Code, rec.Body)
	}
	rec = deleteForm(h, url.Values{csrfField: {a.token}})
	if rec.Code != http.StatusOK || rec.Body.String() != "deleted post 2" {
		t.Fatalf("with the token: %d %s", rec.Code, rec.Body)
	}
}
```

## Try it

```sh
cd examples
go test ./methodlinks/ -v
```

```text
=== RUN   TestMethodLinksApp
--- PASS: TestMethodLinksApp (0.00s)
PASS
ok  	github.com/ubgo/brick/examples/methodlinks	0.017s
```

```sh
go test ./forms-method-csrf/ -v
```

```text
=== RUN   TestMethodFormCarriesCSRFToken
--- PASS: TestMethodFormCarriesCSRFToken (0.00s)
PASS
ok  	github.com/ubgo/brick/examples/forms-method-csrf	0.029s
```

`go run ./methodlinks -addr :8083` serves the list; the page's last paragraph shows the request the router saw (`DELETE /posts/2` after a Delete).

## Pitfalls

- **Without JavaScript, and on a ctrl- or cmd-click, the link is an ordinary GET to its href**. That GET must never change anything; the example's `GET /posts/{id}` only shows the post.
- **Only a same-origin, form-encoded POST is rewritten** (`methodlink.go`, `overrideVerb`). A cross-site form posting `_method=delete` stays a POST, so it never reaches your DELETE route; a multipart or JSON body is never parsed.
- **brick has no CSRF token for plain forms.** If your app has one, add it in the browser with `brick.methodFormWith((form, link) => { /* append a hidden input */ })` (Step 4). Without the hook the form is posted without your token, and a route that checks one answers `403`.
- **The compiler refuses a method link anywhere but an `<a href>`**: `brick:method goes on an <a href> link, not <button>: a form already has method=, and a button can sit in a form with formmethod=`, `brick:method needs an href on its link`, and `brick:method="get": the verb is one of post, put, patch, delete (an ordinary link already sends GET)`.
- **`brick:confirm.prompt` is refused on a method link**: `brick:confirm.prompt: on a method link brick:confirm takes no modifier (.prompt's answer has no call to go to)`. Use a plain `brick:confirm`.

## Related

- [Forms without JavaScript](without-javascript.md) (the same origin check, `formOriginOK`), [Deferred fields](deferred-fields.md) (`brick:confirm`, `brick:disable-with`).
- Reference: *Method links (v23)* in [Forms — deferred fields, arrays, modifiers, loading and confirm](../../guide.md#forms--deferred-fields-arrays-modifiers-loading-and-confirm).
- Feature index row: Method links (`brick:method`, `brick.MethodOverride`) in [the feature index](../../reference/features.md).
- Example: [examples/methodlinks](../../../examples/methodlinks/) and [examples/forms-method-csrf](../../../examples/forms-method-csrf/).
