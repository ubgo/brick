# Scripts and loaders

## What it is and when you want it

A `<RecentPosts>` widget sits in the sidebar of forty pages. Without help, every page must fetch the posts and pass them in, and a page that forgets shows an empty box. A **loader** fixes that: `load:` names a script function, and whatever it returns becomes the component's props, so `<RecentPosts />` works anywhere with nothing passed in. Scripts are Starlark (a small, sealed, Python-like language) through `contrib/script-starlark`; they also give templates functions that a theme can add without rebuilding Go. A page loader can also **answer** instead of returning data: redirect, refuse with a status, or set a response header.

| You need | Use |
|---|---|
| A function used in one file | an inline `<brick:script>` |
| A function used in many files | a `.star` module imported with `use:` |
| Data a component fetches for itself | `load:` |
| The same, called once for every copy on the page | `brick: {load_many:}` |
| A capability from your Go program | `scriptstarlark.Options.Globals` (or `DB`, `HTTPAllow`) |

## Before you start

- Read [Components and props](props.md).
- Scripting is a seam: the root module names no script language, and your Go program passes one in as `Options.Scripting`. The examples use `github.com/ubgo/brick/contrib/script-starlark`.
- Scripts are sealed. A script can never open a file while it runs; `load()` imports are fixed paths brick resolves before anything runs. It has no network, clock or randomness unless your program hands one in, and every call has a step limit (1,000,000 steps) and a timeout (2 seconds), less when the request's deadline is sooner.

## Step 1 — Give the engine a script language

The host decides what scripts may reach. Here they get nothing but one string global, which is how any Go value reaches a script.

<!-- ds:block id=newengine-ua88b6ec title="examples/components-loaders/main.go" -->
**examples/components-loaders/main.go** · [`examples/components-loaders/main.go:33-45`](../../../examples/components-loaders/main.go#L33-L45)

```go
func newEngine(dir string) (*brick.Engine, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	sc, err := scriptstarlark.New(scriptstarlark.Options{
		Globals: starlark.StringDict{"greeting": starlark.String("Written by the team")},
	})
	if err != nil {
		return nil, err
	}
	return brick.New(brick.Options{FS: root.FS(), Scripting: sc})
}
```
<!-- /ds:block hash=82742a -->

## Step 2 — Write a module that imports a module and a data file

`load()` takes a fixed `~/` path. A `.star` file binds the names you list; a data file binds one name, `data`, which `posts = "data"` renames.

<!-- snippet: examples/components-loaders/theme/lib/text.star -->
```python
def shout(s):
    return s.upper() + "!"
```

<!-- snippet: examples/components-loaders/theme/lib/posts.star -->
```python
load("~/lib/text.star", "shout")
load("~/data/posts.yaml", posts = "data")

MOVED = {"old-hello": "/posts/hello"}
REMOVED = ["gone"]

def recent(input):
    return {"items": posts[:input["props"]["limit"]]}

def one(input):
    slug = input["page"]["route"]["slug"]
    if slug in MOVED:
        brick.redirect(MOVED[slug], status = 308)
    if slug in REMOVED:
        brick.error(410, "This post was removed.")
    for p in posts:
        if p["slug"] == slug:
            brick.header("Cache-Control", "public, max-age=300")
            return {"title": shout(p["title"]), "by": greeting}
    brick.not_found()
```

<!-- snippet: examples/components-loaders/theme/data/posts.yaml -->
```yaml
- {slug: hello, title: Hello world, url: /posts/hello}
- {slug: again, title: Writing again, url: /posts/again}
- {slug: third, title: A third post, url: /posts/third}
```

## Step 3 — Let a component load its own data

`load: posts.recent` calls `recent` with `{props, page, ctx}`; `props` already has the declared defaults filled in. The dict it returns becomes props.

<!-- snippet: examples/components-loaders/theme/components/RecentPosts.html -->
```html
---
use:
  posts: ~/lib/posts.star
load: posts.recent
props:
  limit: {type: int, default: 2}
---
<ul>{{ range .Props.items }}<li><a href="{{ .url }}">{{ .title }}</a></li>{{ end }}</ul>
```

The page uses it three ways, plus an inline script function:

<!-- snippet: examples/components-loaders/theme/content/index.html -->
```html
---
title: Recent posts
pinned:
  - {title: Pinned, url: /pinned}
---
<brick:script>
def price(n):
    return "$%d" % n
</brick:script>
<h1>{{ .Title }}</h1>
<RecentPosts />
<RecentPosts :limit="1" />
<RecentPosts :items=".Params.pinned" />
<p>Pro costs {{ price 12 }}.</p>
```

What you write at the call site wins over what the loader returns, which wins over the default: `:limit="1"` changes the loader's input, and `:items=".Params.pinned"` replaces its answer, which is handy for previews and tests.

## Step 4 — Batch a loader used in a loop

A loader runs once per distinct input, so a component inside a `{{ range }}` calls its data source once per row. `brick: {load_many:}` names a batch loader instead: brick holds the calls until the page has run, calls it once with a list of every distinct input, and expects a list of dicts back, one per input, in the same order.

<!-- snippet: examples/components-loaders/theme/lib/users.star -->
```python
USERS = {"1": "Ada", "2": "Grace", "3": "Linus"}

def cards(inputs):
    return [{"name": USERS[i["props"]["id"]]} for i in inputs]
```

<!-- snippet: examples/components-loaders/theme/components/UserCard.html -->
```html
---
use:
  users: ~/lib/users.star
brick:
  load_many: users.cards
props:
  id: {type: string, required: true}
---
<li>{{ .Props.name }}</li>
```

<!-- snippet: examples/components-loaders/theme/content/team.html -->
```html
---
ids: ["2", "1", "2", "3"]
---
<ul>{{ range .Params.ids }}<UserCard id="{{ . }}" />{{ end }}</ul>
```

Four cards, one call to `cards` with the three distinct ids. `Result.Stats` counts it: `BatchCalls` is 1 and `BatchItems` is 3.

## Step 5 — Answer with a redirect, a status or a header

A page loader reads its route (`input["page"]["route"]["slug"]`, which your router passes with `brick.WithRoute`) and may call `brick.redirect(url, status=303)`, `brick.error(status, message)`, `brick.not_found()` or `brick.header(name, value)` (see `one` in step 2). The page:

<!-- snippet: examples/components-loaders/theme/content/post.html -->
```html
---
use:
  posts: ~/lib/posts.star
  helpers: ~/lib/text.star
load: posts.one
---
<h1>{{ .Params.title }}</h1>
<p>{{ .Params.by }} · {{ helpers.shout "read on" }}</p>
```

From Go, the answers are typed errors from `Render` and `RenderChunks`: `*brick.Redirect`, `*brick.HTTPError` and `brick.ErrNotFound`. Headers land in `Result.Headers`. A host with its own router turns them into the response:

<!-- ds:block id=serve-d7nbxtw8 title="examples/components-loaders/main.go" -->
**examples/components-loaders/main.go** · [`examples/components-loaders/main.go:69-105`](../../../examples/components-loaders/main.go#L69-L105)

```go
func serve(eng *brick.Engine, w http.ResponseWriter, r *http.Request, page string, opts ...brick.RenderOption) {
	res, err := eng.RenderChunks(r.Context(), page, nil, opts...)
	var rd *brick.Redirect
	var he *brick.HTTPError
	switch {
	case errors.As(err, &rd):
		loc, lerr := eng.RedirectLocation(r, rd)
		if lerr != nil {
			log.Printf("refused redirect: %v", lerr)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, loc, rd.Status)
		return
	case errors.As(err, &he):
		http.Error(w, he.Message, he.Status)
		return
	case errors.Is(err, brick.ErrNotFound):
		http.NotFound(w, r)
		return
	case err != nil:
		log.Printf("render %s: %v", page, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	html, err := res.Bytes()
	if err != nil {
		log.Printf("render %s: %v", page, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	for k, v := range res.Headers {
		w.Header()[k] = v
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(html)
}
```
<!-- /ds:block hash=b57d9a -->

`Engine.RedirectLocation` applies the open-redirect lock: a path always works, an absolute URL only on the request's own host or one listed in `Options.RedirectHosts`. The site layer (`brick serve`) does all of this for you, including an `error.html` page.

## Step 6 — Limit what a script may spend

Scripts are trusted code but their data may not be, and a loop that never ends would hold a request forever. The Starlark engine stops every call at a step limit and a wall-clock limit: `Options.Steps` (default `DefaultSteps`, one million) and `Options.Timeout` (default `DefaultTimeout`, two seconds), per call, and the request's own context deadline wins when it is sooner. The example lowers the step limit to 10,000:

<!-- snippet: examples/components-options/theme/lib/spin.star -->
```python
def spin():
    n = 0
    for i in range(100000000):
        n += i
    return n
```

<!-- snippet: examples/components-options/theme/content/spin.html -->
```html
---
use:
  s: ~/lib/spin.star
---
{{ s.spin }}
```

The engine is built with `scriptstarlark.Options{Steps: scriptSteps}` (see `newEngine` in [Components and props](props.md#step-5--find-components-in-more-than-one-folder)), and the page fails instead of hanging (lines 27–30 of the program's output):

<!-- snippet: examples/components-options/testdata/output.txt lines=27-30 -->
```text
== content/spin.html
error: content/spin.html: s.spin: Traceback (most recent call last):
  lib/spin.star:3:5: in spin
Error: Starlark computation cancelled: too many steps
```

## Try it

```sh
cd examples/components-loaders
go run . / /team /posts/hello /posts/old-hello /posts/gone /posts/nope
go test .
```

The program sends each path through its handler in process and prints the status, the `Location` and `Cache-Control` headers, and the body. `go run . -addr :8090` serves the same handler for a browser.

<!-- snippet: examples/components-loaders/testdata/output.txt -->
```text
GET / -> 200 OK

<h1>Recent posts</h1>
<ul><li><a href="/posts/hello">Hello world</a></li><li><a href="/posts/again">Writing again</a></li></ul>
<ul><li><a href="/posts/hello">Hello world</a></li></ul>
<ul><li><a href="/pinned">Pinned</a></li></ul>
<p>Pro costs $12.</p>

GET /team -> 200 OK
<ul><li>Grace</li><li>Ada</li><li>Grace</li><li>Linus</li></ul>

GET /posts/hello -> 200 OK
Cache-Control: public, max-age=300
<h1>HELLO WORLD!</h1>
<p>Written by the team · READ ON!</p>

GET /posts/old-hello -> 308 Permanent Redirect
Location: /posts/hello
<a href="/posts/hello">Permanent Redirect</a>.

GET /posts/gone -> 410 Gone
This post was removed.

GET /posts/nope -> 404 Not Found
404 page not found

```

## Pitfalls

- **A data file binds the name `data`.** `load("~/data/posts.yaml", "posts")` fails with `load: name posts not found in module ~/data/posts.yaml`. Write `load("~/data/posts.yaml", "data")` or rename it, `load("~/data/posts.yaml", posts = "data")`.
- **A loader must return a dict.** Returning a list fails with `load: bad returned []interface {}; a loader must return a dict`. A batch loader must return a list of exactly one dict per input.
- **`load:` must name something that exists.** `load: nope` in a file with no such function fails at compile with `load: nope: this file has no <brick:script> defining it`.
- **An inline function cannot reuse a built-in's name.** `def default(x)` fails with `<brick:script> function "default" shadows a built-in or an import (C12)`, so a theme can never redefine `default` by accident. The same holds for a name the host registered in `Options.Funcs`.
- **Starlark's `%` has no width or precision flags.** `"%02d" % n` fails when the script runs with `unknown conversion %0`. Pad by hand, or format in the template with `numberFormat`.
- **A frontmatter key with a loader key's name is warned.** A page or layout whose frontmatter sets `title` while its `load:` function also returns `title` keeps the frontmatter value and reports `frontmatter "title" shadows the key of the same name from this file's load: function; the frontmatter value is used`.
- **The call site wins, but the loader still runs.** `<RecentPosts :items=".Params.pinned" />` shows the pinned list, and the loader is still called (the example's home page makes three loader calls for three copies). Pass the data down from a parent when the call itself is the cost.
- **A loader answer is never contained.** A `<brick:boundary>` does not turn a redirect or an error status into its fallback, because that would send the page around it with status 200.
- **Answers that depend on the visitor belong in an island's loader.** A cached page's loader runs once, for nobody, so a sign-in check there would be served to everyone. See [Protect a page](../../guide.md#protect-a-page-redirect-and-answer-with-a-status).
- **SQL must be a literal.** With `scriptstarlark.Options{DB: db}`, `db.query("select … where slug = ?", slug)` compiles; SQL built by adding strings does not. `db` is read-only; writes need `DBExec`.
- **A limit failure is a render error, not a hang.** A loop past the step limit fails with `Starlark computation cancelled: too many steps`; one past the time limit with `over the time limit (2s per call)` (the duration is your `Timeout`). The step failure names the script file and line; either can be contained by a `<brick:boundary>`.
- **Limits are per call.** A page that calls a loader a thousand times has a thousand budgets; bound that with `load_many` (step 4) and the render limits in [When things go wrong](errors.md).

## Related

- [Components and props](props.md): `Options.Funcs` and `Options.PropTypes`, the way to share a Go function without a script.
- [When things go wrong](errors.md): a boundary around a loader that fails.
- [Frontmatter reference](../../reference/frontmatter.md): `use`, `load` and `brick.load_many`.
- [Feature index](../../reference/features.md): the "Scripts and loaders" rows.
- The single-page read: [Functions and scripts](../../guide.md#functions-and-scripts), [Components that fetch their own data](../../guide.md#components-that-fetch-their-own-data), [Protect a page, redirect, and answer with a status](../../guide.md#protect-a-page-redirect-and-answer-with-a-status).
- Example: [examples/components-loaders](../../../examples/components-loaders/).
- Example: [examples/components-options](../../../examples/components-options/) for the settings added in the later steps.
