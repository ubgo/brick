<!-- Generated from the design site by `task docs:public`; edit the design site source cookbook.md, not this file. -->

# Cookbook

Real tasks, start to finish. Each recipe shows every file you write, in full, and exactly what comes out. This is where you see the features working together.

> [!NOTE]
> **How to read these**
>
> Every recipe is run by a test (`examples/cookbook_test.go`) that builds these exact files and compares brick's real output with the output shown here, ignoring whitespace between tags. Whitespace is tidied for reading.

## The smallest possible site

**Goal:** render one page from your Go app, with nothing else set up.

`files`

```text
content/index.html
main.go
```

`content/index.html`

```html
<h1>Hello</h1>
```

`main.go`

```go
package main

import (
    "context"
    "os"

    "github.com/ubgo/brick"
)

func main() {
    root, err := os.OpenRoot(".") // the project folder; brick can't read outside it
    if err != nil {
        panic(err)
    }
    eng, err := brick.New(brick.Options{FS: root.FS()})
    if err != nil {
        panic(err)
    }
    html, err := eng.Render(context.Background(), "content/index.html", nil)
    if err != nil {
        panic(err)
    }
    os.Stdout.Write(html)
}
```

**What you get:**

`output`

```html
<h1>Hello</h1>
```

No frontmatter, no layout (none exists), no components. Every one of those is optional.

> [!NOTE]
> **Why the page is a path**
>
> `Render` used to take the page as bytes, so brick had no idea where the page lived to resolve `use: ~/components/Card.html` — it didn't know where `~/` pointed. Now the project is `Options.FS` and a page is a path inside it (rule A1). Bytes from a database still work: `eng.Render(ctx, "content/promo.html", nil, brick.WithSource(src))`.

## A blog home page

**Goal:** a home page listing posts as cards, inside the site's layout.

`files`

```text
site.yaml
layouts/default.html
components/PostList.html
components/Card.html
content/index.html
```

`site.yaml`

```yaml
title: My Blog
```

`layouts/default.html`

```html
<!doctype html>
<html>
  <head><title>{{ .Title }} · {{ .Site.title }}</title></head>
  <body>
    <header><a href="/">{{ .Site.title }}</a></header>
    <main>{{ children }}</main>
  </body>
</html>
```

`components/PostList.html`

```html
<ul class="posts">
  {{ range .Props.posts }}
    <li>{{ node "item" . }}</li>
  {{ end }}
</ul>
```

`components/Card.html`

```html
<article class="card">
  <h3>{{ .Props.title }}</h3>
  {{ children }}
</article>
```

`content/index.html`

```html
---
title: Home
posts:
  - {Title: Hello world, URL: /hello, Summary: The first post.}
  - {Title: Second post, URL: /second, Summary: More words.}
---
<h1>Latest posts</h1>
<PostList :posts=".Params.posts">
  <brick:prop name="item">
    <Card :title=".Slot.Title">{{ .Slot.Summary }} <a href="{{ .Slot.URL }}">Read</a></Card>
  </brick:prop>
</PostList>
```

**What you get:**

`output`

```html
<!doctype html>
<html>
  <head><title>Home · My Blog</title></head>
  <body>
    <header><a href="/">My Blog</a></header>
    <main>
      <h1>Latest posts</h1>
      <ul class="posts">
        <li><article class="card"><h3>Hello world</h3>The first post. <a href="/hello">Read</a></article></li>
        <li><article class="card"><h3>Second post</h3>More words. <a href="/second">Read</a></article></li>
      </ul>
    </main>
  </body>
</html>
```

**What this used:** [layouts](guide.md#layouts) (the page names none, so `default.html` wraps it), [props](guide.md#passing-values--props), [scoped slots](guide.md#scoped-slots--let-the-page-draw-each-row) (`PostList` owns the loop, the page draws each row), and [`.Site`](guide.md#where-values-come-from).

## A sidebar widget that fetches its own data

**Goal:** a "recent posts" box that works on any page with nothing passed in, and that doesn't break the page when the database is down.

`components/RecentPosts.html`

```html
---
use:
  posts: ~/components/posts.star
load: posts.recent
props:
  limit: {type: int, default: 5}
---
<ul class="recent">
  {{ range .Props.items }}
    <li><a href="{{ .url }}">{{ .title }}</a></li>
  {{ end }}
</ul>
```

`components/posts.star`

```python
def recent(input):
    rows = db.query(
        "select title, url from posts order by date desc limit ?",
        input["props"]["limit"])
    return {"items": rows}
```

`any page`

```html
<aside>
  <h2>Recent</h2>
  <brick:boundary>
    <RecentPosts :limit="2" />
    <brick:prop name="fallback"><p>Recent posts are unavailable.</p></brick:prop>
  </brick:boundary>
</aside>
```

`main.go (the one line that allows it)`

```go
sc, err := scriptstarlark.New(scriptstarlark.Options{DB: readOnlyDB}) // without DB, scripts cannot reach a database
eng, err := brick.New(brick.Options{FS: root.FS(), Scripting: sc})
```

**What you get** when the query works, and when it fails:

`output: database up`

```html
<aside>
  <h2>Recent</h2>
  <ul class="recent">
    <li><a href="/newest">Newest post</a></li>
    <li><a href="/older">Older post</a></li>
  </ul>
</aside>
```

`output: database down`

```html
<aside>
  <h2>Recent</h2>
  <p>Recent posts are unavailable.</p>
</aside>
```

The failure is still logged and sent to your app's error hooks; it just doesn't take the page down.

**What this used:** [loaders](guide.md#components-that-fetch-their-own-data), [scripts and capabilities](guide.md#functions-and-scripts), [boundaries](guide.md#when-things-go-wrong).

## A page built from CMS blocks

**Goal:** a CMS sends a list of blocks; render each with the right component, without an `if` chain.

`components/Hero.html`

```html
<section class="hero"><h1>{{ .Props.title }}</h1></section>
```

`components/Quote.html`

```html
<blockquote>{{ .Props.text }}</blockquote>
```

`content/about.html`

```html
---
title: About
layout: none
blocks:
  - {type: Hero, title: Welcome}
  - {type: Quote, text: "Less, but better."}
  - {type: Hero, title: Our team}
---
<brick:boundary>
  {{ range .Params.blocks }}
    <brick:render :name=".type" :props="." />
  {{ end }}
  <brick:prop name="fallback"><p>This page could not be shown.</p></brick:prop>
</brick:boundary>
```

**What you get:**

`output`

```html
<section class="hero"><h1>Welcome</h1></section>
<blockquote>Less, but better.</blockquote>
<section class="hero"><h1>Our team</h1></section>
```

A block with `type: Gallery` and no `Gallery.html` would fail at render time and show the fallback. A block with `type: ../secret` is refused outright: `<brick:render>` takes names, never paths.

**What this used:** [choosing a component from data](guide.md#choosing-a-component-from-data), [boundaries](guide.md#when-things-go-wrong), and `layout: none` from [layouts](guide.md#layouts).

## A section with its own colour

**Goal:** everything under the docs section is green and everything under the blog is blue, without passing a colour through every component.

`layouts/docs.html`

```html
---
provide:
  accent: green
---
<html><body class="docs">{{ children }}</body></html>
```

`components/Callout.html`

```html
---
inject:
  accent: {default: gray}
---
<div class="callout callout--{{ .Ctx.accent }}">{{ children }}</div>
```

`content/docs/install.html`

```html
---
title: Install
layout: docs
---
<Callout>Run the installer first.</Callout>
```

`content/contact.html`

```html
---
title: Contact
layout: none
---
<Callout>We reply within a day.</Callout>
```

**What you get:**

`output: docs/install`

```html
<html><body class="docs">
  <div class="callout callout--green">Run the installer first.</div>
</body></html>
```

`output: contact`

```html
<div class="callout callout--gray">We reply within a day.</div>
```

The contact page has no provider above it, so `inject:` supplies the default. Without the `inject:` block it would render `callout--` and nobody would notice.

**What this used:** [shared values for a section](guide.md#shared-values-for-a-section--ctx), [layouts](guide.md#layouts).

## A button in two languages

**Goal:** the same component says "Add to cart" in English and "Ajouter au panier" in French, with correct plurals.

`i18n/en.yaml`

```yaml
cart:
  add: Add to cart
  items:
    one: "{count} item"
    other: "{count} items"
```

`i18n/fr.yaml`

```yaml
cart:
  add: Ajouter au panier
  items:
    one: "{count} article"
    other: "{count} articles"
```

`layouts/default.html`

```html
{{ provide "locale" .Page.Params.lang }}
<html lang="{{ .Page.Params.lang }}"><body>{{ children }}</body></html>
```

`components/CartButton.html`

```html
<button>{{ t "cart.add" }}</button>
<p>{{ t "cart.items" (dict "count" .Props.n) }}</p>
```

`content/fr/panier.html`

```html
---
lang: fr
---
<CartButton :n="3" />
```

`main.go`

```go
msgs, err := brick.LoadMessages(root.FS(), "i18n/")
eng, err := brick.New(brick.Options{FS: root.FS(), Messages: msgs, DefaultLocale: "en"})
```

**What you get:**

`output`

```html
<html lang="fr"><body>
  <button>Ajouter au panier</button>
  <p>3 articles</p>
</body></html>
```

With `lang: en` the same page says "Add to cart" and "3 items". With `lang: pt-BR` and no Portuguese file, it falls back to English.

**What this used:** [translations](guide.md#translations), [shared values](guide.md#shared-values-for-a-section--ctx) (the locale is provided by the layout).

## One hand-written page for one language

**Goal:** every page is shared and translated with `t`, except the French "about" page, which a French writer wrote by hand. Arabic also needs a right-to-left layout.

`files`

```text
layouts/default.html
layouts/default.ar.html
content/about.html
content/about.fr.html
```

`layouts/default.html`

```html
<html lang="{{ .Page.Locale }}"><body>{{ children }}</body></html>
```

`layouts/default.ar.html`

```html
<html lang="ar" dir="rtl"><body class="rtl">{{ children }}</body></html>
```

`content/about.html`

```html
<h1>{{ t "about.title" }}</h1>
<p>{{ t "about.body" }}</p>
```

`content/about.fr.html`

```html
<h1>À propos</h1>
<p>Écrit à la main par notre équipe à Lyon.</p>
```

`main.go`

```go
eng.Render(ctx, "content/about.html", nil, brick.WithLocale("fr"))
eng.Render(ctx, "content/about.html", nil, brick.WithLocale("ar"))
eng.Render(ctx, "content/about.html", nil, brick.WithLocale("de"))
```

**What you get:**

`output`

```html
<!-- fr: the hand-written page, the shared layout -->
<html lang="fr"><body><h1>À propos</h1><p>Écrit à la main par notre équipe à Lyon.</p></body></html>

<!-- ar: the shared page, the Arabic layout -->
<html lang="ar" dir="rtl"><body class="rtl"><h1>من نحن</h1><p>…</p></body></html>

<!-- de: nothing German exists, so the shared page and layout -->
<html lang="de"><body><h1>Über uns</h1><p>…</p></body></html>
```

You always ask for `content/about.html`; brick picks the variant. No file is copied, and a language with no variant just uses the shared file. (The `…` stands for whatever the `ar` and `de` message files contain.)

**What this used:** [translations](guide.md#translations) and language variants.

## A page that shows code

**Goal:** show Go source on a page without anything inside it being treated as a template or a tag.

`components/CodeBlock.html`

```html
---
raw: true
props:
  lang: {type: string, default: text}
---
<pre><code class="language-{{ .Props.lang }}">{{ .Raw }}</code></pre>
```

`examples/hello.go`

```go
package main

import "fmt"

func main() {
    if n := 3; n < 10 {
        fmt.Println("{{ not a template }}")
    }
}
```

`content/tutorial.html`

```html
<h1>Your first program</h1>
<CodeBlock lang="go" src="~/examples/hello.go" />
```

**What you get:**

`output`

```html
<h1>Your first program</h1>
<pre><code class="language-go">package main

import &#34;fmt&#34;

func main() {
    if n := 3; n &lt; 10 {
        fmt.Println(&#34;{{ not a template }}&#34;)
    }
}</code></pre>
```

The `<` and quotes are escaped by Go on the way out, and `{{ not a template }}` is plain text. Because the code lives in a real `.go` file, it can be compiled and tested.

**What this used:** [code and other raw text](guide.md#code-and-other-raw-text), `src=`.

## A wrapper that passes its content through

**Goal:** a `<Panel>` that wraps a `<Card>`, hands its own content to the card, and lets the page set the panel's `id`.

`components/Panel.html`

```html
---
props:
  title: string
---
<section class="panel" {{ attrs }}>
  <Card :title=".Props.title">{{ children }}</Card>
</section>
```

`components/Card.html`

```html
<article class="card">
  <h3>{{ .Props.title }}</h3>
  {{ children }}
</article>
```

`content/pricing.html`

```html
---
layout: none
---
<Panel title="Plans" id="pricing"><p>Pick one.</p></Panel>
```

**What you get:**

`output`

```html
<section class="panel" id="pricing">
  <article class="card"><h3>Plans</h3><p>Pick one.</p></article>
</section>
```

The `{{ children }}` inside Panel's `<Card>` means **Panel's** children, because it was written in `Panel.html`. Under the old design it would have meant Card's own children and called itself forever. `id="pricing"` reached the section through `{{ attrs }}`.

> [!NOTE]
> **Settled while building (2026-09-27)**
>
> To pass the extra attributes on to an *inner component*, write `<Card brick:spread=".Attrs">`: every entry becomes a prop, and an attribute written on the tag wins. (`<Card {{ attrs }}>` stays an error, because it is an action between attributes, S2.)

**What this used:** [children](guide.md#children), [attributes on a component](guide.md#attributes-on-a-component), [layouts](guide.md#layouts) (`layout: none`).

## A list split into pages

**Goal:** a blog list with two posts per page at `/blog/page/1/`, `/blog/page/2/` and so on, written by `brick build` as one file per page, with no pagination feature in brick.

One dynamic page does it. Its loader slices the list for the page number in the URL, and `brick: {paths:}` names every page number, so `brick build` knows which pages to write and `brick serve` answers a number past the end with your 404 page without running anything.

`content/blog/page/[n].html`

```html
---
title: Blog
use:
  blog: ~/scripts/blog.star
load: blog.page
brick:
  paths: blog.pages
---
<ul>
  {{ range .Params.posts }}
    <li><a href="{{ .url }}">{{ .title }}</a></li>
  {{ end }}
</ul>
<nav>
  {{ if .Params.newer }}<a href="/blog/page/{{ .Params.newer }}/">Newer</a>{{ end }}
  <span>Page {{ .Params.n }} of {{ .Params.total }}</span>
  {{ if .Params.older }}<a href="/blog/page/{{ .Params.older }}/">Older</a>{{ end }}
</nav>
```

`scripts/blog.star`

```python
load("~/data/posts.yaml", posts = "data")

PER_PAGE = 2

def _total():
    return (len(posts) + PER_PAGE - 1) // PER_PAGE

def pages():
    # brick: {paths:}: one entry per page; route values are text.
    return [{"n": str(i)} for i in range(1, _total() + 1)]

def page(input):
    n = int(input["page"]["route"]["n"])
    start = (n - 1) * PER_PAGE
    return {
        "posts": posts[start:start + PER_PAGE],
        "n": n,
        "total": _total(),
        "newer": n - 1 if n > 1 else 0,
        "older": n + 1 if n < _total() else 0,
    }
```

`data/posts.yaml`

```yaml
- {title: Fifth post, url: /blog/fifth/}
- {title: Fourth post, url: /blog/fourth/}
- {title: Third post, url: /blog/third/}
- {title: Second post, url: /blog/second/}
- {title: First post, url: /blog/first/}
```

`layouts/default.html`

```html
<main>{{ children }}</main>
```

**What you get** from `brick build`: three files, `blog/page/1/index.html` to `blog/page/3/index.html`. The first and the last:

`output: /blog/page/1/`

```html
<main>
  <ul>
    <li><a href="/blog/fifth/">Fifth post</a></li>
    <li><a href="/blog/fourth/">Fourth post</a></li>
  </ul>
  <nav>
    <span>Page 1 of 3</span>
    <a href="/blog/page/2/">Older</a>
  </nav>
</main>
```

`output: /blog/page/3/`

```html
<main>
  <ul>
    <li><a href="/blog/first/">First post</a></li>
  </ul>
  <nav>
    <a href="/blog/page/2/">Newer</a>
    <span>Page 3 of 3</span>
  </nav>
</main>
```

In `brick serve`, `/blog/page/4/` is your 404 page, because `pages()` does not name it. With posts in a database instead of a data file, `pages()` counts rows and `page()` queries with `limit ? offset ?`; the page file does not change. To make `/blog/` show the first page, link to `/blog/page/1/` or give `content/blog/index.html` the same loader with a fixed page number.

**What this used:** [dynamic routes](guide.md#server-state--replies-first-values-the-url-and-memory), [loaders](guide.md#components-that-fetch-their-own-data), [script imports](guide.md#functions-and-scripts).

## Protecting a page

**Goal:** `/account/` shows a signed-in member their account; a signed-out visitor is sent to `/login/`. The page is still cached, and one member's page is never served to anyone else.

The guard cannot go in the page's own loader: the site caches a page's render and shares it between visitors, so that loader runs once, for nobody. It goes in an island's loader instead. An island is filled on every response with your request values, so its loader sees who is asking, and its answer is sent with `Cache-Control: private, no-store`.

`components/Account.html`

```html
---
brick: {island: true}
inject: {user: {default: ""}}
use: {auth: ~/scripts/auth.star}
load: auth.member
---
<p>Signed in as {{ .Props.name }}.</p>
```

`scripts/auth.star`

```python
def member(input):
    user = input["ctx"].get("user")
    if not user:
        brick.redirect("/login/")
    return {"name": user}
```

`content/account.html`

```html
<h1>Your account</h1>
<Account />
```

`content/login.html`

```html
<h1>Sign in</h1>
```

`layouts/default.html`

```html
<main>{{ children }}</main>
```

Your Go program says who is asking, the way it already knows from its session. Here it is a `user` cookie:

`main.go`

```go
s, err := site.New(ctx, site.Options{
    FS:     os.DirFS("site"),
    Engine: brick.Options{Scripting: sc},
    Request: func(r *http.Request) []brick.RenderOption {
        if c, err := r.Cookie("user"); err == nil {
            return []brick.RenderOption{brick.Provide("user", c.Value)}
        }
        return nil
    },
})
```

**What you get** from `brick serve`. Signed in as `ada`:

`output: /account/ as ada`

```html
<main>
  <h1>Your account</h1>
  <p>Signed in as ada.</p>
</main>
```

Signed out, before and after `ada`'s visit:

`responses: /account/ signed out`

```text
HTTP/1.1 303 See Other
Cache-Control: private, no-store
Location: /login/
```

The second signed-out request still gets the redirect, not `ada`'s page: the page shell is cached, the island runs for each visitor, and an answer is never cached. To refuse a signed-in visitor instead, say a non-admin, call `brick.error(403, "Admins only.")` in the loader and add a `content/error.html` that shows `{{ .Ctx.error.status }}` and `{{ .Ctx.error.message }}`.

`brick build` has no visitor, so it writes `account/index.html` as a redirect page, never the account itself:

`output: brick build account/index.html`

```html
<!doctype html><html><head><meta charset="utf-8"><title>Redirecting…</title><meta name="robots" content="noindex"><link rel="canonical" href="/login/"><meta http-equiv="refresh" content="0; url=/login/"></head><body><a href="/login/">/login/</a></body></html>
```

**What this used:** [islands](guide.md#islands--per-request-parts-of-a-cached-page), [loaders](guide.md#components-that-fetch-their-own-data), [loader answers](guide.md#protect-a-page-redirect-and-answer-with-a-status).

## A form that works without JavaScript

**Goal:** a sign-up form that saves with one request when scripts run, and still works when they do not: scripts switched off, a script blocked, or a slow first paint before the runtime loads. Nothing extra is written for the second case.

A stateful component's `<form brick:submit="save">` is rendered with `method="post"` and the hidden fields the runtime would send, so without the runtime the browser posts it to its own page. The site runs `save` exactly as an action, then sends the visitor back to the page with `303`, or shows the page again with `422` and the errors when `save` returned some.

`components/Signup.html`

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
</form>
```

`content/signup.html`

```html
<Signup />
```

`layouts/default.html`

```html
<main>{{ children }}</main>
```

**What you get.** The form as sent to the browser: `method="post"`, a `name` on the `brick:model` box, and four hidden fields before `</form>` (the handle and token are shortened here):

`output: the form's new parts`

```html
method="post">
<label><input type="checkbox" brick:model="agree" name="brick:model:agree"> I agree</label>
<input type="hidden" name="brick:action" value="save">
```

With scripts off, *Sign up* with an empty name answers the page itself, with the error under the field:

`response: an empty name`

```text
HTTP/1.1 422 Unprocessable Entity
```

`output: the error in the 422 page`

```html
<input name="name" value=""><p class="error">Enter a name.</p>
```

Typing `Ada`, ticking the box and submitting again saves, and sends the visitor back to the page, which shows the new state with the box still ticked:

`response: Ada`

```text
HTTP/1.1 303 See Other
Location: /signup/
```

`output: /signup/ after the redirect`

```html
<p>Welcome, Ada.</p><input name="name" value="Ada">
<label><input type="checkbox" brick:model="agree" checked name="brick:model:agree"> I agree</label>
```

The new state travels across the redirect in a 60-second `HttpOnly` cookie that the page reads once and clears; the URL never carries a handle. The post must come from the site itself (`Sec-Fetch-Site: same-origin`, or else an `Origin` naming the host), so another site cannot submit the form for a visitor.

With scripts on, nothing changes: the runtime catches the submit and sends one JSON action whose fields dict is `{"name": "Ada"}`, leaving out every `brick:` field. A form that writes its own `action` or `method`, and a form with `brick:confirm`, are left alone; `Options.NoFormPosts` turns the fallback off.

**What this used:** [actions](guide.md#actions--a-button-that-changes-server-state), [forms without JavaScript](guide.md#forms-without-javascript).

## A list whose page and filter live in the URL

**Goal:** a post list with a tag filter and *Newer* / *Older* links, where the tag and the page number are in the address bar (`/blog/?tag=go&page=2`), so a link can be shared, Back and Forward step through what the visitor clicked, and a click re-renders only the list, with no page fetch.

The list is a stateful component whose `tag` and `page` fields are named in `query:`. The URL seeds them, and every link that changes only those two parameters is a **patch**: the runtime sends them to the component, which re-renders in place. `params:` names the one function that runs whenever the URL hands the component new values, so the slicing and the "page 9 of 3" check live in one place.

`components/PostList.html`

```html
---
state:
  tag: all
  page: 1
  last: 1
  posts: []
brick: {query: [tag, page], params: on_params}
---
<brick:script>
load("~/data/posts.yaml", all_posts = "data")

PER_PAGE = 2

def on_params(state, props, params):
    tag = state["tag"]
    found = [p for p in all_posts if tag == "all" or tag in p["tags"]]
    last = max(1, (len(found) + PER_PAGE - 1) // PER_PAGE)
    page = min(max(state["page"], 1), last)
    start = (page - 1) * PER_PAGE
    return dict(state, page = page, last = last, posts = found[start:start + PER_PAGE])
</brick:script>
<section>
  <nav><a href="?tag=all">All</a> <a href="?tag=go">Go</a> <a href="?tag=css">CSS</a></nav>
  <ul>
    {{ range .State.posts }}<li><a href="{{ .url }}">{{ .title }}</a></li>{{ else }}<li>No posts.</li>{{ end }}
  </ul>
  <p>
    {{ if gt .State.page 1 }}<a href="?tag={{ .State.tag }}&page={{ sub .State.page 1 }}">Newer</a>{{ end }}
    <span>Page {{ .State.page }} of {{ .State.last }}</span>
    {{ if lt .State.page .State.last }}<a href="?tag={{ .State.tag }}&page={{ add .State.page 1 }}">Older</a>{{ end }}
  </p>
</section>
```

`data/posts.yaml`

```yaml
- {title: Fifth post, url: /blog/fifth/, tags: [go]}
- {title: Fourth post, url: /blog/fourth/, tags: [css]}
- {title: Third post, url: /blog/third/, tags: [go]}
- {title: Second post, url: /blog/second/, tags: [go, css]}
- {title: First post, url: /blog/first/, tags: [go]}
```

`content/blog.html`

```html
<h1>Blog</h1>
<PostList />
```

`layouts/default.html`

```html
<main>{{ children }}</main>
```

**What you get** from `brick serve`. The `<section>` also carries the engine's `data-brick-*` attributes (the state handle, its token and the query map), left out here because the handle changes on every render.

`output: /blog/`

```html
<main>
  <h1>Blog</h1>
  <section>
    <nav><a href="?tag=all">All</a> <a href="?tag=go">Go</a> <a href="?tag=css">CSS</a></nav>
    <ul>
      <li><a href="/blog/fifth/">Fifth post</a></li>
      <li><a href="/blog/fourth/">Fourth post</a></li>
    </ul>
    <p>
      <span>Page 1 of 3</span>
      <a href="?tag=all&page=2">Older</a>
    </p>
  </section>
</main>
```

`output: /blog/?tag=go&page=2`

```html
<main>
  <h1>Blog</h1>
  <section>
    <nav><a href="?tag=all">All</a> <a href="?tag=go">Go</a> <a href="?tag=css">CSS</a></nav>
    <ul>
      <li><a href="/blog/second/">Second post</a></li>
      <li><a href="/blog/first/">First post</a></li>
    </ul>
    <p>
      <a href="?tag=go&page=1">Newer</a>
      <span>Page 2 of 2</span>
    </p>
  </section>
</main>
```

A shared link that asks for a page past the end is corrected by `on_params` before anything is drawn, so `/blog/?tag=go&page=9` renders exactly the same as `/blog/?tag=go&page=2` above. A tag nothing carries is an empty list on page 1, not an error:

`output: /blog/?tag=rust`

```html
<main>
  <h1>Blog</h1>
  <section>
    <nav><a href="?tag=all">All</a> <a href="?tag=go">Go</a> <a href="?tag=css">CSS</a></nav>
    <ul>
      <li>No posts.</li>
    </ul>
    <p>
      <span>Page 1 of 1</span>
    </p>
  </section>
</main>
```

In the browser, clicking *Go* or *Older* is a patch: no page fetch, a new history entry, and only `<PostList>` re-renders, through the `$params` built-in, which runs `on_params` again. Back and Forward do the same. When `on_params` corrects a value the URL brought on a patch (an edited `?page=9`), the address bar is rewritten with `replaceState`, so Back never lands on the wrong page; on the first load the correction shows in the list but not in the address bar. Clicking *All* drops both parameters from the URL, because each is back at its declared value. A link that changes the path, or a parameter no component on the page owns, is still a normal navigation.

**What this used:** [state in the URL and `params:`](guide.md#server-state--replies-first-values-the-url-and-memory), [script imports](guide.md#functions-and-scripts).

## Who is here right now

**Goal:** a lobby page that lists everyone who has it open, by name, and updates for every viewer when someone arrives or leaves, with nothing written in the page to track connections.

The `Room` component declares a presence topic under `listen:`. Each open page joins that topic through its push stream, and leaves when the tab closes. A join or a leave refreshes every viewer's `Room`, which reads the list with `{{ .Presence }}`.

`components/Room.html`

```html
---
state: {n: 0}
props: {id: {type: string}}
brick: {listen: {presence: "room:{{ .Props.id }}"}}
---
{{ $here := .Presence (printf "room:%s" .Props.id) }}
<aside>
  <h2>{{ $here.Total }} here</h2>
  <ul>
    {{ range $here.Entries }}<li>{{ index .Data "name" }}{{ if gt .Conns 1 }} ({{ .Conns }} tabs){{ end }}</li>{{ end }}
  </ul>
</aside>
```

`content/lobby.html`

```html
<h1>Lobby</h1>
<Room id="lobby" />
```

`layouts/default.html`

```html
<main>{{ children }}</main>
```

Your Go program says who each viewer is. Here it is a `user` cookie; a visitor without one still gets live updates but is not listed:

`main.go`

```go
push, err := brick.NewPush(brick.PushOptions{Presence: brick.PresenceOptions{
    Identify: func(r *http.Request, topic string) (brick.PresenceEntry, bool) {
        c, err := r.Cookie("user")
        if err != nil {
            return brick.PresenceEntry{}, false
        }
        return brick.PresenceEntry{ID: c.Value, Data: map[string]string{"name": c.Value}}, true
    },
}})
if err != nil {
    return err
}
s, err := site.New(ctx, site.Options{
    FS:     os.DirFS("site"),
    Engine: brick.Options{Push: push},
})
```

**What you get** from that program. As in the last recipe, the `<aside>` also carries `data-brick-*` attributes (here including `data-brick-listen="brick:presence:room:lobby"` and the stream token), left out below. `Ada` opens `/lobby/` first; her page is drawn before its stream connects, so she is not on it yet:

`output: /lobby/ as Ada, first`

```html
<main>
  <h1>Lobby</h1>
  <aside>
    <h2>0 here</h2>
    <ul></ul>
  </aside>
</main>
```

`Bob` opens it while Ada's tab is open:

`output: /lobby/ as Bob`

```html
<main>
  <h1>Lobby</h1>
  <aside>
    <h2>1 here</h2>
    <ul>
      <li>Ada</li>
    </ul>
  </aside>
</main>
```

When Bob's stream connects, every open `Room` refreshes. Ada then opens a second tab, which makes her one entry with two connections:

`output: /lobby/ with Bob connected and Ada in two tabs`

```html
<main>
  <h1>Lobby</h1>
  <aside>
    <h2>2 here</h2>
    <ul>
      <li>Ada (2 tabs)</li>
      <li>Bob</li>
    </ul>
  </aside>
</main>
```

When Bob closes his tab, his entry leaves and the remaining viewers refresh again:

`output: /lobby/ after Bob leaves`

```html
<main>
  <h1>Lobby</h1>
  <aside>
    <h2>1 here</h2>
    <ul>
      <li>Ada (2 tabs)</li>
    </ul>
  </aside>
</main>
```

If a server dies without closing its streams, its entries lapse after `PresenceOptions.TTL` (90 seconds). This list lives in the one process; with several servers, set `PresenceOptions.Store` to `pushredis.NewPresenceStore(rdb, pushredis.Options{Prefix: "app1:"})` from `contrib/push-redis`, or `Room` fails to render with an error that names the missing store.

**What this used:** [server push, listening and presence](guide.md#server-push--live-updates-from-elsewhere).

## Testing a component on its own

**Goal:** pin what `<Card>` renders with a golden file, with no page, no disk and no server.

`card_test.go`

```go
func TestCard(t *testing.T) {
    theme := fstest.MapFS{
        "components/Card.html": {Data: []byte(
            `<article class="card"><h3>{{ .Props.title }}</h3>{{ children }}</article>`)},
    }
    eng, err := brick.New(brick.Options{FS: theme})
    if err != nil {
        t.Fatal(err)
    }
    got, err := eng.RenderComponent(context.Background(), "Card",
        map[string]any{"title": "Hi"}, `<p>body</p>`)
    if err != nil {
        t.Fatal(err)
    }
    bricktest.Golden(t, "card", got) // testdata/card.golden; go test -update rewrites it
}
```

**What you get** in `testdata/card.golden`:

`testdata/card.golden`

```html
<article class="card"><h3>Hi</h3><p>body</p></article>
```

It passes on every machine, every run: output is byte-identical for the same input.

> [!NOTE]
> **Settled while building (2026-09-27)**
>
> `children` is a string of brick markup, compiled like a component body, and props arrive with their Go types. `bricktest` provides `Theme`, `Engine` and `Golden` (Implementation status).

**What this used:** [for your Go app](guide.md#for-your-go-app).
