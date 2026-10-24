<!-- Generated from the design site by `task docs:public`; edit the design site source guide.md, not this file. -->

# The guide

Every brick feature, taught with a small example. Each section is the same shape: **the problem**, **the fix**, then a few things worth knowing. Read top to bottom the first time; later, jump from the list on the left.

> [!NOTE]
> **Built — 2026-09-27**
>
> Everything here is implemented. Rule ids such as point at the exact rule, explained in plain words on the Rules page.

## Pages

**The problem:** you want a page with a title and some data, without any setup.

**The fix:** a page is an `.html` file. The optional part between `---` lines is data about the page (YAML); the rest is a normal Go template.

`content/about.html`

```html
---
title: About
price: 4200
---
<h1>{{ .Title }}</h1>
<p>{{ .Params.price }}</p>
```

`you get`

```html
<h1>About</h1>
<p>4200</p>
```

- **`.Params.<key>` always works**, with the key exactly as you typed it. A few common keys also have short names: `.Title`, `.Date`, `.Layout`, `.Description`, `.Draft`.
- **Nothing is required.** `<h1>Hello</h1>` with no `---` block is a complete page.
- **A missing key renders empty**, like Go. To catch typos instead, add `strictParams: true` to the page; then `.Params.titel` is an error naming the key. Read a genuinely optional key with `{{ index .Params "subtitle" }}`.

## Components

**The problem:** the same card markup is copy-pasted across pages.

**The fix:** put it in its own file. The file name becomes a tag.

`components/Card.html`

```html
<div class="card">
  {{ with .Props.title }}<h3>{{ . }}</h3>{{ end }}
  {{ children }}
</div>
```

`any page`

```html
<Card title="Hi">
  <p>body</p>
</Card>
```

- **Two ways to write the tag, same component:** `<Card>` (starts with a capital letter) or `<x-card>` (starts with `x-`). `x-post-list` means `PostList.html`. Lowercase tags are always plain HTML.
- **`x-` as a tag is not `x-` as an attribute.** `<x-card>` is brick's tag form; `x-data`, `x-on:click` and `x-show` are attributes for client-side libraries and pass through untouched. They sit in different places in the tag, so they never collide: `<x-card x-data="{ open: false }">` is the Card component receiving such an attribute as a prop, which it places with `{{ attrs }}`. Inside `Card.html` itself, `<x-card>` is the custom element, not a call to itself.
- **`<Card />`** with a slash means "no children".
- **A component file is the same shape as a page:** optional `---` data, then a Go template. No new syntax inside.
- **A component's output has no whitespace at its edges.** The newlines around its markup in the file are dropped, so `Press <Kbd k="K" /> now` has no stray spaces. A page's own unscoped `<brick:style>` ships too, for CSS that belongs to one page; `<brick:client>` and `<brick:style scoped>` belong in a component, and a page with either fails to compile.
- **Components can use components**, to any depth. Recursion is allowed up to a depth of 64.

## Loops around components

**The problem:** you loop over posts and put a component in the loop, and the component's content needs the loop variable.

**The fix:** write it the way Go templates always work. brick carries `$p` into the component's body for you.

`the page`

```html
{{ range $i, $p := .Params.posts }}
  <Card :title="$p.Title">
    #{{ $i }}: {{ $p.Summary }}
  </Card>
{{ end }}
```

`you get`

```html
<div class="card"><h3>Hello</h3>#0: The first post.</div>
<div class="card"><h3>Again</h3>#1: More words.</div>
```

- **Dot and `$` mean what they mean in Go.** Inside the body, `.` is the current post and `$` is the page's root, so `$.Params.title` still works.
- **A tag can't be opened in one branch and closed in another.** `{{ if .Boxed }}<Box>{{ else }}<div>{{ end }}…` is an error. Pass a flag instead: `<Wrapper :boxed=".Boxed">…</Wrapper>`, and let the wrapper decide.
- `{{ break }}` inside a component body can't leave a loop that sits outside the body; that's an error too.
- A `<Card>` written inside a string or a `{{/* comment */}}` is text, not a component.

## Finding components

**The problem:** brick has to know which file `<Card>` means.

**The fix:** four steps, first match wins.

1. **`use:` in this file** — an explicit name → path list. Always wins.
2. **Component folders** — every file under `components/` (the default) is available by its file name. Subfolders don't change the name: `components/ui/Tabs.html` is just `<Tabs>`.
3. **Libraries** — the component libraries listed under `libraries:` in `brick.yaml`, in that order (see [Component libraries](#component-libraries--use-a-ui-kit-by-name)).
4. **Not found** — an error naming the tag and the file.

`frontmatter of any file`

```yaml
---
use:
  Card: ~/content/pricing/PricingCard.html   # use a different Card, in this file only
  BlogCard: ~/content/blog/Card.html         # an alias: the key is the tag name
  pricing: ~/data/pricing.json               # a data file → .Data.pricing
  helpers: ~/lib/helpers.star                # a script → helpers.slugify
---
```

- **Two files with the same name?** The earlier folder in the list wins, and brick logs what it hid. That's a warning, not an error. Reorder the folders, or use `use:` to reach the other one.
- **Every path starts at the project root.** `~/components/Card.html`, never `./Card.html` or `../blog/Card.html` — so you always know where a path points, and moving a file never breaks it.
- **Web components:** set `unknown: passthrough` and unknown tags are left as they are instead of being an error. To keep typo checking but let one vendor element through, list it: `Passthrough: []string{"x-date-picker"}`.
- **File names are exact on every computer.** Components are found from the folder listing, so `card.html` (lowercase) is skipped with a warning on macOS and Linux alike. `<x-html-editor>` finds `HTMLEditor.html`.
- **Pasted uppercase HTML still works.** `<DIV>` or `<A>` with no component of that name passes through with a warning instead of failing.

## Passing values — props

**The problem:** a component needs values from the page.

**The fix:** attributes. There are three forms.

| You write | The component gets |
|---|---|
| `title="Hi"` | the text `Hi` |
| `:count="5"` | the number `5` — a leading `:` means "evaluate as Go" |
| `open` | `true` |
| `:post` (no value) | the variable `$post`, the same as `:post="$post"` (v22) |

```html
<PostList :items=".Posts" :limit="5" heading="Latest" />
```

Inside `PostList.html`, `.Props.items` is a real list you can `{{ range }}`, and `.Props.limit` is a real number.

**Passing a variable under its own name (v22).** `{{ range $post := .Posts }}<Card :post />{{ end }}` is `<Card :post="$post" />`. It only ever means the `$` variable: when no `$post` is declared where the tag is, it is a compile error, never a guess at `.post`. `:$post` and a name that is not a Go identifier (`:user-id`) are compile errors that name the fix.

**Checking props (optional):** declare them and brick checks every call.

`components/Callout.html`

```yaml
---
props:
  title: string
  count: {type: int, default: 0}
  items: {type: list, required: true}
  type: {type: string, default: note, oneof: [note, tip, warn]}
---
```

- `oneof` turns `<Callout type="tpi">` from a silently unstyled box into an error naming the allowed values.
- Undeclared props still pass through, so adding `props:` never breaks existing pages. `strictProps: true` rejects them instead.
- **Structured data goes in frontmatter, never in an attribute.** Write the list in YAML and pass it with `:data=".Params.chartData"`. If you're about to write `data='{"a":1}'`, it belongs in frontmatter.
- **Character references work as in HTML:** `title="Tom &amp; Jerry"` is the text "Tom & Jerry".
- **`{{ }}` inside a plain value works.** `title="{{ .name }}"` and `class="card {{ .variant }}"` are text props filled in where you wrote them.
- **Strings inside an expression use backticks:** ``:featured="eq .Section `blog`"``. HTML has no `\"`, so that form is an error.
- **`open="false"` is the text "false", which counts as true.** Declare the prop as `bool` and brick converts it, or just write `open` / leave it off. brick warns when it sees the trap.
- **One value per prop.** `title="a" title="b"`, or an attribute plus a `<brick:prop>` of the same name, is an error naming both.
- `children` and `key` are reserved prop names.
- **Documenting a component.** Each prop and slot can carry `doc:` (one line of text) and `examples:` (a list of snippets): `tone: {type: string, oneof: [plain, loud], doc: "How loud the card is", examples: [plain, loud]}`. Rendering never reads them. `Engine.ComponentDocs()` returns every component the engine can call, sorted by name, with its checked declarations and these docs, so the docs come from the same frontmatter the engine enforces and cannot drift from it. Run `brick docs` to read every component's contract (its props, slots, defaults and the `doc:` and `examples:` its frontmatter declares) without opening the files, or `brick docs -json` to feed it to a docs page or an editor.

**Declaring the shape of an object prop (v20).** A `map` prop usually has fields the component reads, `.Props.author.name`; misspell one and html/template prints `<no value>`. Declare them with `fields:` (field name → a type or `{type, required, oneof, doc, examples, fields, of}`), and the elements of a `list` or `map` with `of:`, nested to any depth, and `brick check` warns instead (code `prop-shape`):

```yaml
props:
  author:
    type: map
    required: true
    fields:
      name: {type: string, required: true}
      email: string
      social: {type: map, fields: {twitter: string, github: string}}
  posts: {type: list, of: {type: map, fields: {title: string, url: string}}}
```

```text
components/AuthorCard.html:14:36: warning: .Props.author.emial: "emial" is not a field of .Props.author (fields: email, name, social)
components/AuthorCard.html:16:60: warning: .Props.posts[].tilte: "tilte" is not a field of .Props.posts[] (fields: title, url)
content/index.html:7:13: warning: <AuthorCard> author: the required field "name" is missing
content/index.html:8:13: warning: <AuthorCard> author: "emial" is not a field of author (fields: email, name, social)
```

Reads are followed through `{{ with }}`, `{{ range }}`, `$variables`, `index` and component bodies; call sites are checked when they pass a `dict`/`list` literal, `.Data`/`.Params` values, or the caller's own shaped prop, and a shaped prop's `default:` must fit. A field takes the prop grammar minus `default`, `client` and `reactive`. These are warnings at check time, never errors and never a render cost: a value from a loader or a Go program is never checked. `Schema.Validate` (content collections) checks nested `fields:` and `of:` too, allowing extra keys.

## Attributes on a component

**The problem:** you write `<Card id="plans" hx-get="/more">` and expect the id and the `hx-get` attribute to land on the card's HTML. But `<Card>` isn't an element; it's a component, so they would silently vanish.

**The fix:** every attribute is a prop, and the ones the component didn't declare are collected so it can place them with `{{ attrs }}`.

`components/Card.html`

```html
---
props:
  title: string
---
<div class="card" {{ attrs }}>
  <h3>{{ .Props.title }}</h3>
</div>
```

`page → you get`

```html
<Card title="Hi" id="plans" hx-get="/more" />

<div class="card" id="plans" hx-get="/more">
  <h3>Hi</h3>
</div>
```

- The component chooses where they go; nothing is injected into your markup behind your back.
- **Merging a passed `class`:** write ``class="{{ cx `card` .Attrs.class }}" {{ attrs `class` }}``. `cx` joins the class names that are set, skipping empty ones, and `{{ attrs "class" }}` leaves `class` out so the root never gets two class attributes. Any names can be left out: `{{ attrs "class" "id" }}`.
- Names are kept exactly as written: `postId` and `post-id` are different props. Read a hyphenated one with `index .Props "post-id"`.
- A client library's `:class` shorthand clashes with brick's `:` prefix; on a component, write `x-bind:class`.
- **Passing them on to an inner component:** `<Button brick:spread=".Attrs">` gives every entry of a map as props. An attribute written on the tag wins over the same key, and `key`, `children` and `src` never arrive through a spread.

`components/Primary.html → <Primary id=&quot;buy&quot; hx-post=&quot;/buy&quot;>Buy</Primary>`

```html
<Button class="primary" brick:spread=".Attrs">{{ children }}</Button>

<button class="primary" hx-post="/buy" id="buy">Buy</button>
```

## Passing markup — `<brick:prop>`

**The problem:** a card needs a header that is itself markup — maybe another component. Putting markup inside an attribute means escaping quotes, which is exactly what brick never wants you to do.

**The fix:** a named block. Anything not inside a block is `children`.

`the page`

```html
<Card>
  <brick:prop name="header">
    <Title size="lg">{{ .Params.heading }}</Title>
  </brick:prop>

  This is the children.

  <brick:prop name="footer"><a href="/buy">Buy now</a></brick:prop>
</Card>
```

`components/Card.html`

```html
<div class="card">
  <header>{{ node "header" }}</header>
  <div class="body">{{ children }}</div>
  {{ if hasNode "footer" }}
    <footer>{{ node "footer" }}</footer>
  {{ end }}
</div>
```

> [!TIP]
> **The one rule**
>
> **An attribute holds text or a reference. Anything with angle brackets, quotes, or newlines goes in a block.** Follow it and you never escape anything.

- A block prop is read with `{{ node "name" }}`, not `.Props.name`. It renders only when read, so an unread block costs nothing. (Go only inserts markup unescaped once it's already rendered, so a lazy block has to be read through a function.)
- `{{ hasNode "footer" }}` checks whether one was passed.
- Most components never need blocks: `title="…"` plus `{{ children }}` covers most cases.
- **A block must sit directly inside its component, outside any `{{ if }}`.** To make one conditional, put the condition *inside* the block.
- **Declared slots.** A component can say which blocks it takes: `brick: {slots: {title: {required: true, doc: "The heading"}, footer:, children: {required: true}}}`. `children` is the content between the tags; every other name is a `<brick:prop name="…">` block. Once a component declares slots, `<Card>body</Card>` without a title fails at compile with `<Card> requires a <brick:prop name="title"> block` at the caller's line, and `<brick:prop name="aside">` fails with `<Card> declares no such slot (declared blocks: footer, title)`. A component that declares no slots accepts any block, as before. Raw blocks (`<brick:prop raw>`) are string props and are checked by `props:`.

## Code and other raw text

**The problem:** you want to show code that contains `<`, `{{ }}` or quotes, and none of it should be interpreted.

**The fix:** a *raw body*. The component says `raw: true`; everything between its tags arrives untouched as `.Raw`.

`components/CodeBlock.html`

```html
---
raw: true
props:
  lang: {type: string, default: text}
---
<pre><code class="language-{{ .Props.lang }}">{{ .Raw }}</code></pre>
```

`the page`

```html
<CodeBlock lang="go">
if x < 10 && y > 2 {
    fmt.Println("{{ not a template }}")
}
</CodeBlock>
```

- Output is escaped automatically and correctly, because `{{ .Raw }}` is a normal template action.
- Leading indentation is removed (`dedent: false` keeps it).
- **Better still, keep code in a real file:** `<CodeBlock lang="go" src="~/examples/main.go" />`. Then the example compiles and can be tested.
- `src=` must be written literally in the file, and can't reach outside the project. `:src=".Params.file"` is an error, so data can never choose which file is read.
- One limit: a raw body ends at its first closing tag, so it can't contain `</CodeBlock>` itself. The text after it is left over and the file fails to compile with `</CodeBlock> has no opening tag`, never a silent cut-off. Keep such text in a file and include it with `src=`.
- A raw block prop works the same way: `<brick:prop name="sql" raw>…</brick:prop>` arrives as the plain string `.Props.sql`.

> [!NOTE]
> **JSON, specifically**
>
> To **use** JSON as data, put it in frontmatter, or pass `:config="fromJSON .Params.blob"`. To **show** JSON as text, use a raw block or `src=`.

## Children

**The problem:** a tab bar has to know its tabs' titles *before* drawing them.

**The fix:** a component sees its children two ways.

| | What it does |
|---|---|
| `{{ children }}` | Renders the children right there. If you never call it, they never render. |
| `.Children` | Lists the child tags as data (`.Name`, `.Props`) **without** rendering them. |

`components/Tabs.html`

```html
<div class="tabs">
  <div role="tablist">
    {{ range $i, $c := .Children }}
      <button role="tab">{{ $c.Props.title }}</button>
    {{ end }}
  </div>
  <div class="panels">{{ children }}</div>
</div>
```

> [!WARNING]
> **`.Children` only sees tags you wrote literally**
>
> `<Tabs>{{ range .Sections }}<Tab title="{{ .Name }}" />{{ end }}</Tabs>` gives an empty tab bar, because those tabs don't exist until render time. Pass the data as a prop instead: `<Tabs :sections=".Sections">`. The reverse trap: a `<Tab>` inside `{{ if }}` is listed even when the condition is false, and brick warns about it.

- `{{ child $c }}` renders one child, so a tab set can render only the active panel.
- `{{ if hasNode "children" }}` tests whether anything was written between the tags; whitespace doesn't count.

## Scoped slots — let the page draw each row

**The problem:** a `<PostList>` owns the loop over posts, but each page wants rows to look different: cards on the home page, plain links in the sidebar. The page writing the row has no "current post" to use.

**The fix:** the component hands each item to the block, and the block reads it as `.Slot`.

`components/PostList.html`

```html
<ul>
  {{ range .Props.posts }}
    <li>{{ node "item" . }}</li>
  {{ end }}
</ul>
```

`sidebar`

```html
<PostList :posts=".Params.posts">
  <brick:prop name="item">
    <a href="{{ .Slot.URL }}">{{ .Slot.Title }}</a>
  </brick:prop>
</PostList>
```

One list component, any row: `PostList` controls the loop; the page controls the look.

- Only one value can be passed. For several, pass a map: `{{ node "item" (dict "post" . "index" $i) }}`, read as `.Slot.post`.
- Everything else (`.Params`, `.Site`…) still works inside the block. Inside a `range` in the block, read the slot as `$.Slot`.
- This is a *scoped slot*, or a *render prop*: the component hands a value back to the caller's markup.

## Layouts

**The problem:** every page needs the same `<html>`, `<head>` and header around it.

**The fix:** a layout is just a component that wraps the page. The page is its `children`.

`layouts/default.html`

```html
<!doctype html>
<html>
  <head><title>{{ .Title }}</title></head>
  <body>{{ children }}</body>
</html>
```

`which layout a page gets`

```text
1. layout: post  → layouts/post.html
2. otherwise     → layouts/default.html
3. no default    → no layout
layout: none     → never a layout
```

- A page never has to name a layout.
- **Layouts can have layouts.** `layouts/post.html` can say `layout: default`; a loop is an error. The default layout never wraps another layout.
- In a layout, `.Params` and `.Title` are the **page's**; the layout's own frontmatter is its `.Props`.
- For a fragment that a script on the page fetches and inserts, render with `brick.WithLayout("none")`.
- No `{{ define }}` / `{{ template }}` pairs like plain Go needs. brick writes those behind the scenes; you never see one.

## Where values come from

**The problem:** in a big template it's unclear where a value came from, and globals make that worse.

**The fix:** every value comes from one of six places, and only one of them is global.

| Write | Comes from | Visible in |
|---|---|---|
| `.Props` | whoever called you: the tag's attributes for a component, your Go app's `Render` data for a page | this component or page |
| `.Params` | the page's frontmatter | this page |
| `.Data` | data files imported with `use:` | this file |
| `.Ctx` | a value an ancestor *provided* | everything beneath it |
| `.Slot` | the value passed to `{{ node "x" $v }}` | that block |
| `.Site` | site config, loaded once | everywhere — **read-only** |

Inside a component, `.Page.Params.title` reaches the page's data from any depth.

> [!TIP]
> **Where you wrote it, or where it's shown?**
>
> Everything is read from **where you wrote it** — except `.Ctx`, which is read from **where it's shown**. That's what lets a layout's `provide "locale"` reach the text you wrote in the page.

`site.yaml`

```yaml
title: My Blog
baseURL: ${BASE_URL}
```

```html
<a href="{{ .Site.baseURL }}">{{ .Site.title }}</a>
{{ range .Site.Data.pricing.plans }}<Card :plan="." />{{ end }}
```

- Files in `data/` become `.Site.Data.<filename>`.
- **There is no `.Env`.** Environment variables reach templates only through `${VAR}` in `site.yaml`, so that file is the list of what's allowed. One stray `{{ .Env }}` can never leak a database password.
- Nothing is found automatically: your app loads the site config explicitly (see [For your Go app](#for-your-go-app)).

## Shared values for a section — `.Ctx`

**The problem:** the blog section is blue and the docs section is green. Every component deep inside needs that colour, and passing it through five layers of props is painful.

**The fix:** a layout *provides* a value; anything beneath it reads it from `.Ctx`.

`layouts/blog.html`

```html
---
provide:
  accent: blue
---
<html><body>{{ children }}</body></html>
```

`components/Card.html`

```html
---
inject:
  accent: {default: gray}
---
<div class="card card--{{ .Ctx.accent }}">…</div>
```

- The nearest provider wins, so a section can override its layout.
- To provide something computed, call `{{ provide "accent" .Page.Params.accent }}` **before** `{{ children }}`.
- **`inject:` is optional but recommended.** It makes the hidden dependency visible and gives it a default. `required: true` turns a missing value into an error instead of a blank.
- Rule of thumb: **if it can be a prop without pain, make it a prop.** Keep `.Ctx` for things like theme, locale or current user.
- `provide` inside a `range` works per item; your Go app can seed a value for the whole page with `brick.Provide("user", u)`.

## Functions and scripts

**The problem:** Go templates can't define functions. Every new function normally means changing Go code and rebuilding.

**The fix:** pick by what you need.

| You need | Use | Rebuild? |
|---|---|---|
| Reusable markup | a component file | no |
| Markup used in one file | `<brick:component name="Price">` | no |
| A function that returns a value | a Starlark script | no |
| A database or API call | Starlark, through `db` / `http` | no |
| A brand-new capability | Go, via `ScriptGlobals` | yes |

**Built-ins cover most needs first:** `truncate`, `dateFormat`, `pluralize`, `default`, `slugify`, `add`, `dict`, `fromJSON`, `toJSON` and more. Two exist for class lists: `cx "btn" (cond .Props.block "btn--block" "") .Attrs.class` joins the parts that are set, and `cond test a b` is the inline if. `sortBy "Title" .Site.Pages` returns a list of maps or structs ordered by one key (`"-Date"` for newest first; numbers and dates compare as such, text ignores case).

**Element names from data.** A template cannot write `<{{ .Tag }}>`: html/template escapes the `<`, because it cannot know what kind of tag it would be opening. Use `tag` and `endtag` instead: `{{ $h := print "h" .Props.level }}{{ tag $h "class" "title" }}{{ children }}{{ endtag $h }}` renders `<h3 class="title">…</h3>`. Attributes come as name, value pairs; `true` writes a bare attribute and `false` or nil leaves it out. Because html/template cannot see inside the tag it writes, `tag` accepts only elements with no URL, script or raw-text content (headings, p, div, span, section, list and table elements and the like) and refuses `on*`, URL and `style` attributes; anything else is a render error.

**Scripts** are Starlark, a small, safe Python-like language:

`helpers.star`

```python
def slugify(s):
    return s.lower().replace(" ", "-")
```

`the page`

```html
---
use:
  helpers: ~/lib/helpers.star
---
{{ helpers.slugify .Params.title }}
```

Or inline, for one file:

```html
<brick:script>
def price(n):
    return "$%d" % n
</brick:script>

{{ price .Params.amount }}
```

> [!CAUTION]
> **Scripts are sealed**
>
> A script can compute, but it **can never open a file while it runs** — not now, not later. Imports with `load()` are fixed paths brick checks and loads before running. It has no network, clock or randomness either. It can reach a database or the web only if your Go app turns that on: `db` is read-only (writes need a separate `db.exec`), and `http` only reaches hosts on your allowlist. Every call has a step limit and a timeout.

- A local component (`<brick:component>`) can't declare `props:`, `inject:` or `load:`. When you need checking, move it to its own file.
- An inline function can't reuse a built-in's name, so a theme can't redefine `default` by accident.
- `{{ helpers.slugify x }}` isn't valid Go on its own; brick rewrites it for you so it works as written.
- **Scripts can import each other and data files:** `load("~/lib/text.star", "shout")` or `load("~/data/prices.yaml", prices = "data")` at the top of a `.star` file. A data file binds the single name `data`, so rename it on load as shown (`load("~/data/prices.yaml", "prices")` fails with `name prices not found`). The path must be fixed text inside your project; brick loads everything before any script runs.
- **Formatting:** Starlark's `%` has no width or precision flags, so `"%02d" % n` and `"%.1f" % x` fail when the script runs. Pad and round by hand, or format in the template with `numberFormat`.
- Limits have numbers: 1,000,000 steps and 2 seconds per call, less if the request's deadline is sooner.
- **SQL must be written as a literal.** `db.query("select … where slug = ?", slug)` compiles; building the SQL by adding strings does not. `db.query_dynamic` is the named exception.

## Your own functions and prop types

**The problem:** your Go program already has a `money` formatter and knows what a valid email is, but a theme can only reach Go code through a Starlark script, so you'd have to adopt Starlark to share one function.

**The fix:** hand them to the engine when you create it. `Options.Funcs` adds template functions; `Options.PropTypes` adds type names a `props:` block can use.

`main.go`

```go
e, err := brick.New(brick.Options{
	FS: os.DirFS("site"),
	Funcs: template.FuncMap{
		"money": func(cents int64) string {
			return fmt.Sprintf("$%d.%02d", cents/100, cents%100)
		},
	},
	PropTypes: map[string]brick.PropType{
		"email": func(v any) (any, error) {
			s, _ := v.(string)
			if _, err := mail.ParseAddress(s); err != nil {
				return nil, fmt.Errorf("%q is not an email address", s)
			}
			return strings.ToLower(s), nil
		},
	},
})
```

`components/Contact.html`

```html
---
props:
  to: email
  price: int
---
<a href="mailto:{{ .Props.to }}">{{ .Props.to }}</a>
<span>{{ money .Props.price }}</span>
```

`<Contact to="Ann@Example.com" price="1999" />` renders `ann@example.com` and `$19.99`; `<Contact to="nope" />` is a prop error naming `"nope"`. A prop declared `int` arrives as `int64` (and `float` as `float64`), so a function that takes one must take that type: `func(cents int)` fails at render with `wrong type for value; expected int; got int64`.

- **Built-ins win, by refusal.** A function named `len`, `upper`, `sanitize` or `children`, or a type named `int` or `string`, is an error from `New`, so a theme's `{{ upper }}` means the same thing under every host. The reverse holds too: an inline `<brick:script>` function named `money` is a compile error when the host registered `money`.
- **A theme now needs its host.** Moved to a program without `money`, the file fails to compile with `function "money" not defined`; an unregistered type fails with `unknown type "email"`. Same trade-off as a Starlark global: loud at compile, never a surprise at render.
- **Keep them pure.** Every render shares them, so they must be safe to call at once and give the same output for the same input.
- A prop type runs on the call-site value, the loader's input and the default (a bad default fails at compile), never on a prop that is absent. `oneof:` is checked after it.

## Components that fetch their own data

**The problem:** a `<RecentPosts>` widget sits in the sidebar of forty pages. Without help, every page must fetch the posts and pass them in, and a page that forgets shows an empty box.

**The fix:** `load:` names a script function. Whatever it returns becomes props.

`components/RecentPosts.html`

```html
---
use:
  posts: ~/lib/posts.star
load: posts.recent
props:
  limit: {type: int, default: 5}
---
<ul>{{ range .Props.items }}
  <li><a href="{{ .url }}">{{ .title }}</a></li>
{{ end }}</ul>
```

`posts.star`

```python
def recent(input):
    rows = db.query(
        "select title, url from posts order by date desc limit ?",
        input["props"]["limit"])
    return {"items": rows}
```

Now `<RecentPosts />` works anywhere, with nothing passed in.

- **The call site always wins:** `<RecentPosts :items=".Params.pinned" />` uses the pinned list instead. Handy for tests and previews.
- Ten identical `<RecentPosts />` on one page call the function **once**.
- Pages and layouts can use `load:` too; the result merges into `.Params`.
- **Order of precedence: what you write at the call site → what the loader returns → the default.** A `required` prop is checked after the loader runs.
- A loader inside a `{{ range }}` runs once per different input. Load the list once in the parent and pass it down, or make it a batch loader (below).
- A loader can answer instead of returning data: `brick.not_found()`, `brick.redirect(url)`, `brick.error(status)`, and `brick.header(name, value)` beside its data ([Protect a page](#protect-a-page-redirect-and-answer-with-a-status)). Answers that depend on the visitor belong in an island's loader.
- Data is fixed when the page renders. If the database changes, the page is stale until something re-renders it. Cache tags will handle that in a later phase.

**Batch loaders.** A loader runs once per distinct input, so a component used in a range calls its data source once per row: `{{ range .Params.ids }}<UserCard id="{{ . }}" />{{ end }}` with five ids makes five calls. Name a batch loader instead with `brick: {load_many: users.cards}`. brick then holds those calls until the page has run, calls `cards` once with a list of every distinct input, and renders each card from its answer. The function must return a list of dicts, one per input, in the same order. The output, ids included, is what five single loads would have produced. Components nested inside batched ones batch again at their own level, so a team list with member cards costs two calls, not 1 + N. A file has one loader (`load:` or `load_many`, never both), and only a component can batch; a page or layout renders once.

`lib/users.star`

```python
USERS = {"1": "Ada", "2": "Grace", "3": "Linus"}
def cards(inputs):
    return [{"name": USERS[i["props"]["id"]]} for i in inputs]
```

`components/UserCard.html`

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

`content/team.html`

```html
---
ids: ["2", "1", "2", "3"]
---
<ul>{{ range .Params.ids }}<UserCard id="{{ . }}" />{{ end }}</ul>
```

Output: `<ul><li>Grace</li><li>Ada</li><li>Grace</li><li>Linus</li></ul>`, from one call to `cards` with the three distinct inputs. Inside a `<brick:boundary>`, a slot or an island's own root, a call is answered at once by a one-element batch.

## Choosing a component from data

**The problem:** a CMS sends a list of blocks — Hero, Gallery, Quote — and you'd need a growing `{{ if eq .type "Hero" }}…{{ else if }}` chain.

**The fix:** `<brick:render>` picks the component by name.

`frontmatter`

```yaml
blocks:
  - {type: Hero, title: Welcome}
  - {type: Quote, text: "Less, but better."}
```

`the page`

```html
{{ range .Params.blocks }}
  <brick:render :name=".type" :props="." />
{{ end }}
```

- **A name, never a path.** Anything with `/`, `.` or `~` is rejected, so CMS data can only reach components your theme already has.
- An unknown name is an error at render time. Wrap the list in a `<brick:boundary>` (next section) so one bad block doesn't break the page.
- The map still has its `type` key; if a component rejects unknown props, drop it with ``:props="omit . `type`"``.

## When things go wrong

**The problem:** a typo should fail loudly, but one slow widget shouldn't take the whole page down.

**The fix:** two things.

**1. Every error says where.** File, line and component, always. Typos in tags, missing required props, bad prop values and unbalanced tags are caught when brick reads the file, before anything renders. The spec lists about 35 error cases; none is silent.

**2. `<brick:boundary>` contains failures that happen while rendering.**

`the page`

```html
<brick:boundary>
  <RecentPosts />
  <brick:prop name="fallback">
    <p>Recent posts are unavailable.</p>
  </brick:prop>
</brick:boundary>
```

`if the query fails`

```html
<p>Recent posts are unavailable.</p>
```

- Only render-time failures are caught (a failed query, a script error). A typo still fails the build, as it should.
- Nothing is swallowed: a contained error is still logged and reported to your app.
- The fallback can show the error as `.Slot.Message`. In production that's a generic message plus an error id, so a database password in an error can't reach the page.
- **Dev vs prod:** in `brick.Dev` mode errors show as a visible block in the page **and** are returned to your app; in `brick.Prod` (the default) you get the error and no page. Never a blank page, never half a page.
- **A cancelled request stops.** A boundary never swallows cancellation, so you don't get a page full of fallbacks nobody is waiting for.
- **A loader's answer passes through (v15).** `brick.not_found()`, `brick.redirect` and `brick.error` are never contained: a fallback in their place would send the page around it with status 200.
- **A failed boundary cleans up:** the failed component's JavaScript isn't loaded for the fallback.
- **Limits have defaults:** depth 64, 100,000 components, 32 MiB of output per render.

**Unbalanced HTML.** brick passes your HTML through untouched, and html/template does not track elements, so a forgotten `</div>` renders silently and the browser closes it somewhere you did not expect. `brick check` now reports it as an `html-balance` warning with the file and line: `content/a.html:6:1: warning: </main> closes <div> (line 5), which is still open`. The check is lenient on purpose: `<br>` and `<img>` need no close, `<li>`, `<td>`, `<p>` and `<option>` may stay open, and a wrapper opened in one `{{ if }}` and closed in a matching `{{ if }}` later is fine, because a problem is reported only when it holds on every path. A `{{ range }}` body and a component's body must each close what they open. It is a warning, never an error, and it never changes what renders.

**Where did this element come from?** In Dev mode every component with a single root element marks it with its source: `<span class="bk-badge" data-brick-loc="components/Badge.html:45">`. Inspect any element in the browser and the attribute names the file and line to open. The line is recorded when the component compiles, so it costs nothing at render time. Components with several top-level elements (or a root chosen by `{{ if }}`) are not marked, the same rule as `brick:on:` listeners. Nothing is added outside Dev mode.

## Styles and browser JavaScript

**The problem:** a card's `.title { color: red }` turns every title on the page red, and a component's JavaScript shouldn't load three times for three copies.

**The fix:** blocks inside the component file.

`components/Card.html`

```html
<brick:style scoped>
.title { color: red }
</brick:style>

<div class="card">
  <h3 class="title">{{ .Props.title }}</h3>
  {{ children }}
</div>
```

- **`scoped`** confines the CSS to this component, using the browser's own `@scope` (about 92% of browsers; older ones just skip those rules). The component must have exactly one top-level element.
- A scoped block stops at the root of any nested component that has scoped styles of its own, so a parent never restyles a child. **`scoped="inclusive"`** (v18) lets the parent's rules reach that child's root element, to place or size it (a margin, a width, a grid area), but still nothing inside it: with `<brick:style scoped="inclusive">.card { margin-block: 1rem }</brick:style>` around `<Card />`, the `.card` rule reaches Card's `<article class="card">` root and nothing below. As in any `@scope` block, write `:scope { … }` to style the component's own root. Bare `scoped` is the same as `scoped="exclusive"`; any other value is a compile error at the block's line. `@scope` with its `to ()` clause is in Chrome and Edge 118+, Safari 17.4+ and Firefox 146+.
- **`Options.StyleTransform`** (v18) receives each `<brick:style>` block (`brick.StyleBlock`: file, content hash, scoped, bound, the source, and the CSS the engine would ship) and returns the CSS that ships instead: a minifier, a fallback for browsers without `@scope`, or another scoping strategy. It runs when the file compiles and must be deterministic, because its output is hashed into the asset URL; an error fails the compile, naming the file and line.
- **A stylesheet next to the component (v22).** Drop `Card.css` beside `components/Card.html` and it ships as the component's CSS, exactly like a `<brick:style>` block: global unless the component says `brick: {css: scoped}` (or `inclusive`, or the explicit default `global`). With both, the file comes first and the inline blocks after, in one hashed stylesheet, so an inline rule wins a tie. The file stays plain CSS any editor or linter reads. Editing it live-reloads the pages that use it; language variants (`Card.fr.html`) share it; pages and layouts never pick up a `.css` beside them. `brick: {css: …}` without a file is a warning, on a page an error. `Options.StyleTransform` sees it as one more block with `StyleBlock.Sheet` naming the file.
- **`<brick:client>`** holds browser JavaScript that adds behaviour to HTML that already exists. brick sends each component type's JS and CSS **once**, as a cacheable file, however many times the component appears.
- Put `{{ assets }}` in your layout (usually at the end of `<body>`); that's where those files are linked.
- Only props marked `client: true` are sent to the browser; everything else stays on the server.
- **Progressive by default:** a component must work with no JavaScript (an accordion renders open). JS only enhances.
- **`brick.mount("Accordion", (el, props) => { … return cleanup })`** runs once per accordion — those on the page and any inserted later — and runs the returned cleanup when one is removed. The small runtime (about 5 KB gzipped) is linked automatically by `{{ assets }}`. `brick.emit` / `brick.on` pass events between components.
- **`brick:mount="visible"`** (or `"idle"`) on the tag waits until the component scrolls into view, or until the browser is idle.
- One component's client code can use another's: `import { open } from "brick:Modal"` becomes a relative import of the Modal module's hashed file, so it works wherever the assets are served.

## Navigation without reloads

**The problem:** every click reloads the whole page: the header flickers, a playing video stops, the scroll jumps.

**The fix:** turn on navigation. Internal links fetch the next page and patch only what changed.

`brick.yaml`

```yaml
navigation: true
```

- **No server change:** the browser fetches the same page a normal visit gets. Anything that goes wrong falls back to a normal page load.
- Unchanged parts — the header, a mounted component — stay as they are. `brick:persist` on an element with an `id` keeps it across pages even when the new page moves it (a music player, a chat box).
- `brick:prefetch` on a link loads the page when the pointer rests on it (`brick:prefetch="visible"`: when it scrolls into view), so the click is instant. Back and forward reuse recent pages.
- Forms work too: `GET` search forms and `POST` forms, whose redirect is followed. Any form submission forgets cached pages.
- `brick:nav="off"` on a link, a form, or anything around them keeps the normal behaviour (downloads, sign-in). `brick:nav="replace"` on a link swaps the history entry instead of adding one; from code, `brick.navigate(url, {replace: true})`.
- `brick.on("navigated", ({url}) => …)` for analytics; the new page's title is announced to screen readers and focus moves to `<main>`.
- `brick:current` on a link marks it while its page is shown: the runtime sets `aria-current="page"` and `data-current` when the link's path is the page's or a segment prefix of it (`/docs` on `/docs/intro`, never on `/docsearch`; a link to `/` only on `/`), and removes them after a navigation away. `brick:current.exact` needs the paths to be equal. Style `[data-current]`; a link without `brick:current` keeps what the server rendered.
- **Only the page travels (v22).** When the next page has the same layout, the site layer answers a navigation with only the page: the title, the description, the asset blocks and the part between the layout's marks, spliced in so the layout's nodes (typed inputs, mounted components, scroll) are never touched. Nothing to configure. The layout is compared by its rendered bytes, so a sidebar that marks the current page on the server makes every page's layout different; mark it with `brick:current` (it may also render `aria-current` for readers without JavaScript) and the saving comes back. On the kit showcase a docs navigation drops from 146.9 KB to 29.1 KB (20.5 KB to 6.2 KB gzipped). A different layout, a streamed page, a form post or any doubt gets the full page, as before.
- A navigation slower than 150 ms puts `data-navigating` on `<html>`, for a progress bar of your own: `html[data-navigating] .bar { display: block }`.
- `brick.on("navigate", (d) => { if (unsaved && !confirm("Leave?")) d.preventDefault(); })` cancels a navigation; a cancelled Back puts the shown URL back in the address bar.
- `brick:scroll="<name>"` on a scrolling element (a sidebar, a long filter list) keeps its scroll position across navigation (v20): the next page's element with the same name starts where this one was, back and forward put it back where it was on that page, and the dev server's live reload keeps it. It is keyed by the name, not the `id`, so two layouts can share one position. The name is required and compile-checked (letters, digits, `-` and `_`, no modifiers, once per file); on a page that ends up with it twice, the first element wins. The kit `Sidebar` takes `keepScroll="docs-nav"`.
- Back and forward restore the scroll position the reader left (a `#hash` still wins). `brick:prefetch` links prefetch after 65 ms of hover, and at once on keyboard focus or a press.
- **Prefetch, natively where it helps.** A page with a `brick:prefetch` link also carries speculation rules, so browsers that support them prefetch with their own machinery, and others ignore the script. Each link is prefetched once, by whatever will load it. Links nav.js intercepts are fetched into its snapshot cache, because the browser's prefetch cache only serves full page loads. Links that are full page loads (`brick:nav="off"`, or any link on a page without nav.js) are left to the browser. `brick:prefetch="visible"` becomes eagerness `eager` and the default hover becomes `moderate`. Rules only match links inside the site's base path, and exported pages carry them too.

## Actions — a button that changes server state

**The problem:** a like button, a counter, a cart line: the page needs to change something on the server and show the result, without a reload and without writing an API and client code for it.

**The fix:** give the component `state:` and a script function; a `brick:click` calls it.

`components/Likes.html`

```html
---
state:
  likes: 0
---
<brick:script>
def like(state, props):
    return dict(state, likes = state["likes"] + 1)
</brick:script>
<div class="likes"><button brick:click="like">♥ {{ .State.likes }}</button></div>
```

- The action gets the current state and the component's props, and returns the new state. brick saves it, re-renders **only this component**, and the page patches it in place.
- Arguments are JSON values: `brick:click="bump(10)"`. A string needs double quotes, so write the attribute in single quotes: `brick:click='pick("red")'`. When it reads the file, brick checks each literal call's shape (a name, then JSON arguments); whether the name is one of the component's actions is checked when it is called, so a misspelt `brick:submit="sve"` compiles and the click answers 400 `brick: unknown action "sve"`. `brick:submit` on a form passes its fields; `brick:change` on an input passes its value.
- **From code that is not a click** (a drag and drop, a keyboard shortcut): `brick.action(el, "move", [id, "done"])` calls the action of the component around `el` and resolves when the patch is applied.
- **Where state lives is your choice** (next section): sealed into the page, so it survives a restart, or kept on the server. Either way the client cannot read or change it, and a lost state is rebuilt in place, keeping what the user typed.
- **Forms bind both ways:** `<input brick:model="name">` updates the field as the user types (sent after a short pause; `brick:model.lazy` waits for change). `<span brick:text="name">` and `<p brick:show="agree">` follow it instantly, with no request. An action returning `[state, errors]` makes `{{ .Errors.name }}` available for that render — validation messages.
- A `brick:submit` form is handled at the document, after listeners on the form and its ancestors have run, so a component's own submit listener (clearing a composer, say) still fires. Register it on the form or the component, not on `document`.
- **With scripts off** the same form still works (v16): it is rendered with `method="post"` and posts to its own page, and the site runs the action and answers `303` or `422`. See [Forms without JavaScript](#forms-without-javascript).
- `<span brick:loading hidden>Saving…</span>` shows while a request runs; submit buttons are disabled meanwhile. `brick:poll="10s"` on the root refreshes the component on a timer (paused in background tabs). Deferred fields, list and nested bindings, modifiers, targeted loading, confirm and poll actions are in the [next section](#forms--deferred-fields-arrays-modifiers-loading-and-confirm).
- A stateful component renders per request (it is an island), so every visitor gets their own. `brick serve` and the site layer handle `/_brick/action`; with the engine alone, mount `eng.ActionHandler()` and set `Options.States` (`brick.NewMemoryStates(0, 0)`).
- **A stateful component inside another one keeps its state** when the outer one acts. An open `<Accordion>` inside a `<ContactsApp>` stays open, and a half-typed child form keeps its text: the runtime tells the server which children the page already has, the server answers `<brick-keep data-brick="b1-3"></brick-keep>` in their place instead of rendering them, and the page keeps the element it has. Only direct children are kept; each one speaks for its own.
- **A stateful component can wrap the caller's markup:** `<Modal title="Edit"><form>…</form></Modal>`, with `{{ children }}` inside Modal's `<dialog>`. brick renders the children and blocks once, where you wrote them, with your page's data, and stores the HTML with the instance, so opening and closing the modal never empties the form. Because the HTML is stored, three things are refused at compile time: `{{ node "x" value }}` (a block cannot depend on the component's data), `.Children` and `{{ child }}` (use `{{ children }}`), and a stateful component inside the slot (put it in the component's own file). What you pass stays as first rendered until your page renders again, so a CSRF token or a timestamp inside it does not refresh on actions. Large children count toward the state size, so pass ids instead.
- **Props a kept child must follow** opt in with `reactive: true`. When the outer component passes a different value, the child re-renders with its state kept and the new value; otherwise it is kept as is. A prop without it keeps the value of the child's first render, which is cheaper: a plain keep reads nothing from the store.

`components/ContactRow.html (frontmatter)`

```yaml
state:
  open: false
props:
  contact: {type: map, reactive: true}
```

- **In a `{{ range }}`, give a stateful child `key=`** (`<ContactRow :contact="." :key=".id" />`): without it the child's identity is its position, so after a row is deleted every later row would keep its neighbour's state. Dev warns when a stateful child repeats without a key.

## Forms — deferred fields, arrays, modifiers, loading and confirm

**The problem:** a signup form with name, e-mail and three topic checkboxes. With plain `brick:model` every field sends its own `$set` after each pause in typing, so filling the form costs five requests and five re-renders before *Sign up* is even clicked. The checkboxes bind as one true/false each, not as the list of topics; the spinner flashes on every keystroke's `$set`; and asking "Clear everything?" before a destructive button needs a dialog component wrapped around it.

**The fix:** mark the fields `.defer`. They update the page at once but send nothing; their values ride along with the next action of the component, in the same request.

`components/Signup.html`

```html
---
state:
  name: ""
  email: ""
  tags: []
  saves: 0
---
<brick:script>
def save(state, props):
    return dict(state, saves = state["saves"] + 1)

def clear(state, props):
    return dict(state, name = "", email = "", tags = [], saves = 0)
</brick:script>
<form brick:submit="save">
  <input brick:model.defer="name" value="{{ .State.name }}">
  <input brick:model.defer="email" value="{{ .State.email }}">
  <label><input type="checkbox" brick:model.defer="tags" value="go"> Go</label>
  <label><input type="checkbox" brick:model.defer="tags" value="js"> JS</label>
  <p>Hello, <b brick:text="name">{{ .State.name }}</b></p>
  <p brick:dirty hidden>Unsaved changes.</p>
  <span brick:loading.delay="save" hidden>Still saving…</span>
  <button type="button" brick:click="clear" brick:confirm="Clear the form and everything saved?">Clear</button>
  <button brick:disable-with="Saving…">Save</button>
</form>
```

```text
type "Ada", "ada@x.io", tick Go     → no request; "Hello, Ada" and "Unsaved changes." show at once
click Save
POST /_brick/action  {action: "save", sets: [["name","Ada"],["email","ada@x.io"],["tags",["go"]]]}
← the server applies the three sets, then runs save: one request, one re-render
click Clear  → "Clear the form and everything saved?"  → Cancel sends nothing
```

**Deferred fields and the `sets` wire field**

- `brick:model.defer` keeps the value in the browser until the component's next request of any kind: a click, a submit, a key, an event listener or a poll. That request carries every held value as `sets`, and the server applies them **before** the action, through the same checks as `$set`, all or nothing. One bad value refuses the whole request with **422** and leaves state unchanged; the values stay held and go out with the next request.
- A live `brick:model` on a field drops its held value (the live one is newer). Without any following action a deferred field is never sent: that is what the mode means, and `brick:dirty` is how you show it.
- Go middleware sees the values in `ActionCall.Sets` and may drop or rewrite entries (forbid setting `role`, log them).
- After a lost state, recovery sends **one** request: the failed action with every held field as `sets`.

**Lists and nested fields**

- A checkbox bound to a field whose state is a list binds as a list: its value is the checked boxes' `value`s, in page order, even when there is one box. `<select multiple>` is always a list. brick knows which fields are lists from the state (it writes `data-brick-arrays` on the component's element), so nothing guesses from the markup.
- `brick:model`, `brick:text`, `brick:show` and `brick:dirty` take a dotted path: `address.city`, `items.0.qty`. The first part must be a declared state field; every later part must already exist in the current state (a key that is there, an index in range). The value is converted to the type of what is there now.
- **The client changes values, never shape.** `$set` and `sets` cannot add a key, grow a list, or replace a whole dict or list of dicts; set the parts by path and add rows in an action. A refused value is a 422 that names the field.

**Modifiers and keys**

| Attribute | Modifiers |
|---|---|
| `brick:model` | `.lazy` (send on change, the blur mode), `.defer`, `.debounce.300ms`, `.throttle.300ms` (send steadily while typing, and once more at the end) |
| `brick:click`, `brick:submit`, `brick:change`, `brick:input`, `brick:focus`, `brick:blur`, `brick:keydown`, `brick:keyup` | `.once` (the first time only, per rendered element), `.debounce.300ms`, `.throttle.300ms` |
| `brick:focus`, `brick:blur` | `.window` (v21: when the browser tab gains or loses focus, with no value) |
| `brick:keydown`, `brick:keyup` | one key: `enter escape tab space up down left right backspace delete`, a letter `a`-`z` or a digit `0`-`9`; `.ctrl` `.meta` `.shift` `.alt` before it make a combo (exact match); `.window` listens on the whole window (a page-level Escape) |
| `brick:keydown` | `.norepeat` (v22: a held key runs the action once per press; its repeats are still matched, so nothing else takes them) |
| `brick:click` | `.outside` (a click outside the element, while it is shown), `.details` (v22: the click's keys, button, count and position as the call's last argument) |
| `brick:loading` | `.delay` (show only after 200 ms; `.delay.500ms` to change it), `.remove` (hide instead of show), `.except` (v21: show for every action but those named) |
| `brick:dirty` | `.remove` |
| `brick:poll` | the interval (`.10s`, `.500ms`, `.1m`), `.visible`, `.keep-alive` (keep polling while the tab is hidden) |
| `brick:intersect` | `.leave`, `.once`, `.half`, `.full`, `.margin.200px`, `.dwell` (only after the element stayed in view 1000 ms; `.dwell.2000ms` to change it) |

`<input brick:model.defer="q" brick:keydown.enter="search">` is a search box that sends nothing while typing and one request on Enter, with `q` in `sets`. `brick:keydown.enter` cancels the key's default, so it never also submits the form around it. `.prevent`, `.stop` and generic events are left out on purpose: clicks and submits already prevent the default, and client code calling `brick.action` covers the rest.

**More events and key combos (v14).** `brick:input` runs on every edit, `brick:focus` and `brick:blur` when the element itself gains or loses focus, and `brick:keyup` on a key's release with the same keys and `.window` as `brick:keydown`. Put `.ctrl`, `.meta`, `.shift` or `.alt` before a key for a combo: the attribute matches only when exactly those modifier keys are held, so `.enter` and `.ctrl.enter` on one textarea never both fire. `brick:click.outside` runs for a click anywhere outside the element while it is shown, which is how a dropdown closes itself.

```html
<div brick:click.outside="close">
  <input brick:input.debounce.300ms="search" brick:blur="validate" value="{{ .State.q }}">
  <textarea brick:keydown.enter="send" brick:keydown.ctrl.enter="sendAll" brick:keyup.escape="clear"></textarea>
</div>
```

Typing `ada` quickly sends one `search("ada")`; leaving the field sends `validate("ada")`. Enter sends `send()`, ctrl+Enter sends `sendAll()`, shift+Enter sends nothing and types a newline, and releasing Escape sends `clear()`. A click on the page outside the `div` sends `close()`; a click inside it does not. `brick:keydown.ctrl` with no key, `brick:keydown.cmd.k` and `brick:change.outside` are compile errors that name the file and line. Since v14, `brick:keydown.<key>` matches only when no modifier key is held, so `brick:keydown.enter` no longer fires for shift+Enter; a bare `brick:keydown` still fires for every key.

**Once per press, and where it was clicked (v22).** `brick:keydown.norepeat.enter="send"` runs once however long Enter is held (the default still repeats; keydown only). `brick:click.details="pick"` calls `pick(state, props, details)` with `{alt, ctrl, meta, shift, button, count, x, y, clientX, clientY}`: `x` and `y` are relative to the element carrying the attribute, `count` is 2 on a double click and 0 for a keyboard click. Without `.details` nothing about the click is sent. In `bricktest`, `ClickWith("#bar", bricktest.ClickDetails{Shift: true, X: 10, Y: 20})`.

**When the tab comes back (v21).** `brick:focus.window="$refresh"` re-renders the component when the visitor returns to the tab, and `brick:blur.window="pause"` runs when they leave it. A `.window` attribute listens to the browser window, not to its element, sends no value and does not fire when the element itself is focused; `brick:js.focus.window` and `brick:js.blur.window` do the same for client commands. In `bricktest`, `live.WindowFocus()` and `live.WindowBlur()` fire them.

**Loading, dirty and reset**

- `brick:loading="save delete"` shows only while those actions run; with no value it shows for any action. `$set` matches every `$set` and deferred send, `$refresh` every refresh. `.delay` stops fast actions from flashing it.
- **Narrower targets (v21).** Name a call to light one row: `brick:loading="remove({{ .id }})"` shows only while `remove` runs with that argument (arguments match as a prefix, so a `.prompt` answer or an event value added to the call still matches). Name one field's change with `brick:loading='$set("query")'`: it lights while a `$set` of `query` (or a path under it) runs, and when `query`'s deferred value rides along another action. `brick:loading.except="download"` shows for every action but those. The compiler checks each target: the name, the arguments as JSON and the `$set` field against `state:`.
- The element that started a request, and its form, carry `data-loading` while it waits (Tailwind: `data-loading:opacity-50`); the component's element carries `data-loading="save"`, the names of the actions in flight, so CSS can dim one part: `[data-loading~="save"] .row { opacity: .5 }`.
- `brick:disable-with="Saving…"` disables a button and swaps its label while its own click or its form's submit is in flight, then puts the label back (unless a re-render changed it). With no value (v21), `<button brick:disable-with brick:click="save">`, it disables the button and keeps its label.
- **Polls are quiet:** a poll sets no `aria-busy` or `data-loading` and shows only a `brick:loading` that names its action, so nothing flashes every few seconds.
- `brick:dirty` shows while any field differs from what the server last rendered; `brick:dirty="email address.city"` watches only those. The component's element carries `data-dirty`, and `brick.on("dirty")` / `brick.on("clean")` fire when that flips, enough for a "leave this page?" guard.
- `<button type="reset">` works: the inputs go back to what the server rendered, held values are dropped, and no request is sent, unless a live `$set` already reached the server; then one `$refresh` carries the reset values so the two agree.

**Confirm and poll**

- `brick:confirm="Delete this contact?"` on any element with an event attribute (`brick:click`, `brick:submit`, `brick:change`, `brick:input`, `brick:focus`, `brick:blur`, `brick:keydown` or `brick:keyup`) asks first; on Cancel nothing is sent and no loading state starts. The message is plain text, never HTML.
- The browser's `confirm()` asks by default. On a page that has a [kit](#component-libraries--use-a-ui-kit-by-name) `<ConfirmDialog>`, that dialog asks instead: a real alert dialog with Cancel focused and Escape cancelling (`data-confirm-label="Delete"` names its button). Its script loads only on such pages, so a page never pays for a dialog it does not show. Any app can plug in its own with `brick.confirmWith((message, el) => Promise<boolean>)`, which returns a function that puts the previous one back.
- **Ask for input (v18).** `brick:confirm.prompt="New name?"` with `brick:click="rename(3)"` asks the visitor for text first and calls `rename(state, props, 3, "the answer")`: the answer is the call's last argument, after its own arguments and any event value. Cancel sends nothing; an empty answer is still sent. `brick.promptWith((message, el) => …)` replaces `window.prompt` (return the text, `null` for cancel, or a promise of either) and returns a restore function, like `brick.confirmWith`. In `bricktest`, `ClickPrompt(selector, answer)` answers it, and `Click` refuses a `.prompt` element so a test cannot skip the answer. A typed "type DELETE to confirm" check is not grammar: write it in your prompter or your action.
- **Method links (v23).** A link can send a verb other than GET, for a Delete or Log out that has no form around it: `<a href="/posts/{{ .ID }}" brick:method="delete" brick:confirm="Delete this post?">Delete</a>`. A plain click (after the confirm) submits a hidden form to the href; `post` posts as is, and `put`, `patch` and `delete` post with a `_method` field that `brick.MethodOverride` turns back into the verb, so wrap your router once: `http.ListenAndServe(addr, brick.MethodOverride(mux))` and route `DELETE /posts/{id}` as usual. It rewrites only a same-origin, form-encoded POST, so a cross-site form can still never reach your DELETE route. `brick:disable-with="Deleting…"` marks the link busy. brick has no CSRF token for plain forms; if your app has one, add it with `brick.methodFormWith((form, link) => { /* append a hidden input */ })`. It works in any file (no `state:` needed) and on a component call that places `{{ attrs }}` on its `<a>`; the compiler refuses it anywhere else, and refuses a verb other than those four. Without JavaScript, and on a ctrl- or cmd-click, the link is an ordinary GET to its href, so that GET must never change anything.
- `brick:poll="5s"` refreshes the component, as before. `brick:poll.10s="refreshStats"` runs an action instead, `.visible` polls only while the element is on screen, and a poll may sit on any element inside the component. Polls pause in background tabs (unless `.keep-alive`) and stop when their element is removed.

**Polling in a background tab (v15).** Polls skip their ticks while the tab is hidden, which spares the server work nobody is looking at. `brick:poll.keep-alive` keeps polling anyway, for a chat that must deliver a notification or a job whose session must stay warm. With `.visible` too, the element must still be on screen as last reported.

```html
<div brick:poll.15s.keep-alive="heartbeat"></div>
```

**Typos fail when the file is read.** `brick:click.onec`, a modifier given twice, `.defer` with `.lazy`/`.debounce`/`.throttle`, `.debounce` with `.throttle`, two keys, `.debounce` without a duration, `brick:model="nmae"` (not a declared field), `brick:loading="sve"` (not an action of the component, nor a built-in: `$set $refresh $toggle $upload $uploaded $removeUpload`), `brick:poll.5s` with no action, `brick:poll="soon"`, `.dwell` on anything but `brick:intersect`, `.dwell` with `.leave`, `.dwell.2s` (durations are `<n>ms`) and `.keep-alive` on anything but `brick:poll` are all errors with the file, the line and the allowed list:

```text
components/Signup.html:21:11: brick:click.onec: unknown modifier "onec" (brick:click accepts .once, .debounce.<ms>, .throttle.<ms>)
```

A path built with `{{ }}` (`items.{{ $i }}.qty`) is checked when it is set instead.

## Forms without JavaScript

**The problem:** without the runtime, `<form brick:submit="save">` is a plain form with no `method` and no `action`. Scripts switched off, a script blocked, or a slow first paint before the runtime loads, and the browser submits it as a GET to the same page: the fields land in the URL and `save` never runs.

**The fix:** nothing to write. Every rendered stateful component's `brick:submit` form gets `method="post"` (and no `action`, so it posts to the page's own URL) and, before its `</form>`, the fields the runtime would send. The site answers the post by running the action through the same pipeline as a JSON action.

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

`the rendered form (handle and token shortened)`

```html
<form data-brick="b5eff8f26" data-brick-state="default.37c6…" data-brick-token="3c98…" data-brick-name="Signup" brick:submit="save" method="post">
  <input name="name" value="">
  <label><input type="checkbox" brick:model="agree" name="brick:model:agree"> I agree</label>
  <button>Sign up</button>
<input type="hidden" name="brick:state" value="default.37c6…"><input type="hidden" name="brick:token" value="3c98…"><input type="hidden" name="brick:action" value="save"><input type="hidden" name="brick:models" value="[[&#34;c&#34;,&#34;agree&#34;,&#34;&#34;]]"></form>
```

```text
scripts off, submit empty
POST /signup/   name=&brick:state=…&brick:token=…&brick:action=save&brick:models=[["c","agree",""]]
→ 422, the page itself: <p class="error">Enter a name.</p>, the box as it was

type "Ada", tick the box, submit
POST /signup/   name=Ada&brick:model:agree=on&…
→ 303 See Other, Location: /signup/, Set-Cookie: brick-form=…; Max-Age=60; HttpOnly; SameSite=Lax
GET /signup/    (with the cookie)
→ 200: <p>Welcome, Ada.</p>, the box ticked; the cookie is cleared

scripts on
POST /_brick/action  {"action":"save","args":[{"name":"Ada"}],…}   (every brick: field left out)
```

**How the fields map**, the same way the runtime maps them:

- Named fields become the action's last argument, one dict, the last value of a repeated name (what `Object.fromEntries(new FormData(form))` keeps); none when the form has no named fields. A file field is not included: its file goes through the upload path first (see *Uploads with scripts off* below).
- Every `brick:model` control in the form becomes a set, applied before the action through the `$set` gate (declared path, coerced type, locked fields refused): a checkbox on a bool is `true` when sent and `false` when not; a checkbox group or `<select multiple>` is the list of sent values (`[]` when none); a radio group, a select and a text field take the sent value, and leave the field alone when nothing was sent. A control that already has a `name` keeps it and is read by that name.
- The written arguments of the call (`brick:submit="bump(10)"`) come first, as JSON.

**What the answer is**

| Outcome | Answer |
|---|---|
| The action succeeded | `303` to the page's own URL (query-synced field changes applied to it), the new state in a 60-second cookie the page consumes |
| The action returned errors (`[state, errors]`) | `422` with the page rendered with that state and those errors |
| The action recorded `brick.redirect` or `brick.navigate` | `303` to that URL (already checked by the open-redirect lock) |
| The state is too big for a cookie (a large sealed handle) | `200` with the page rendered with the new state |
| The instance was gone (expired, restarted) | `303` to the page, which renders fresh |
| A bad token, a locked field, an unknown action | the action endpoint's status and message (`403`, `422`, `400`) |

Effects other than redirect and navigate are dropped with scripts off: flash, title, dispatch, rows, download, URL history and scripts have nobody to run them.

- **Safety.** A form post can come from another site, unlike the JSON action with its custom header, so it is refused with `403` unless `Sec-Fetch-Site` is `same-origin` or, when a browser does not send that header, `Origin` names the request's host; `same-site` is refused too, since a sibling subdomain may be hostile. The per-instance token is checked after that. The cookie that carries the state across the redirect is `HttpOnly`, `SameSite=Lax`, `__Host-brick-form` on HTTPS, and is loaded with the same session and token checks as an action, so a planted cookie renders a fresh form.
- **Uploads with scripts off (v19).** A file input with `brick:upload` inside the form uploads too. The rewrite adds `enctype="multipart/form-data"` and puts the instance fields first; without the runtime the browser sends the file with the form, and the engine streams each file part through the `$upload` pseudo-action (your middleware, the field's `uploads:` limits counted while streaming, the type sniffed from the bytes, `max_files`, your `UploadSink`) before the form's action runs, so the action already sees `.State.avatar` filled. A file the field refuses (too large, wrong type, no room) does not run the action: the page comes back `422` with the reason in `.Errors.avatar`. File bytes never count against `MaxActionBytes` and are never spooled; a cross-site post is refused before a byte reaches the sink.

`components/Profile.html`

```html
---
state:
  name: ""
  avatar: null
brick:
  uploads:
    avatar: {accept: [image/png, image/jpeg], max_size: 2MB}
---
<brick:script>
def save(state, props, form):
    name = str(form.get("name", "")).strip()
    if not name:
        return [state, {"name": "Enter a name."}]
    return dict(state, name = name)
</brick:script>
<form brick:submit="save">
  <input name="name" value="{{ .State.name }}">
  <input type="file" brick:upload="avatar" accept="image/png,image/jpeg">
  {{ with .State.avatar }}<img src="{{ .url }}" alt="{{ .name }}">{{ end }}
  <p>{{ with .Errors.avatar }}{{ . }}{{ end }}</p>
  <button>Save</button>
</form>
```

```text
scripts off, name "Ada", pick me.png (7.8 KB)
POST /profile/   multipart: brick:state, brick:token, brick:action, brick:uploads, name=Ada, brick:upload:avatar=<me.png>
→ $upload streams me.png into the sink → save runs → 303 See Other, Location: /profile/
GET /profile/    → 200: <img src="/uploads/3f…" alt="me.png">

pick notes.txt renamed .png
→ 422, the page itself: <p>this type of file is not accepted</p>; save did not run
```
- **Left alone on purpose** (they stay inert without scripts): a form that writes its own `action` or `method` (a classic post to a host route), a form with `brick:confirm` (nobody can be asked, and a destructive action must not run unasked), and a call only the browser can make (`$dispatch`, `$parent.fn`, `$cancelUpload`).
- **Limits.** `brick: {temporary:}` fields show their declared value after the redirect. Bodies are capped by `Options.MaxActionBytes`.
- **Turning it off and other routers.** `Options.NoFormPosts` turns off both the rewrite and the endpoint. A host with its own router sends a POST to a page URL for which `eng.IsFormPost(r)` is true to `eng.FormPost(r)`, answers `303` after `eng.SetFormResume(w, r, out.Resume)` (or renders the page with `brick.WithFormResume(out.Resume)` and `422` when `out.Failed`), and passes `eng.FormResumeOption(w, r)` when it renders pages.

## Validation — rules declared once

**The problem:** a signup form checks its fields in a Starlark action that returns `[state, errors]`. Live fields run no action, so a per-field error needs one; the errors last one render, so with live fields the error under *e-mail* disappears the moment *name* is set; and nothing knows which fields the user has actually filled in, so a submit-time "required" lights up every empty field the instant the page loads if you validate early.

**The fix:** declare the rules once under `brick:`. The engine checks them at every render, but only for the fields the user has touched; the submit action validates everything with one call.

`components/Signup.html`

```html
---
state:
  name: ""
  email: ""
  password: ""
  confirm: ""
brick:
  rules:
    name: [required, {min: 2}]
    email: [required, email, {max: 120}]
    password: [required, {min: 12}]
    confirm: [required, {same: password}]
  messages:
    confirm.same: "The passwords do not match."
---
<brick:script>
def signup(state, props):
    errors = brick.validate()
    if errors:
        return [state, errors]
    if state["email"].endswith("@taken.example"):
        return [state, {"email": "That e-mail already has an account."}]
    brick.untouch()
    brick.flash("Welcome!", "success")
    return dict(state, name = "", email = "", password = "", confirm = "")
</brick:script>
<form brick:submit="signup" novalidate>
  <input brick:model.lazy="name" value="{{ .State.name }}">
  {{ with .Errors.name }}<p class="error">{{ . }}</p>{{ end }}
  <input type="email" brick:model.lazy="email" value="{{ .State.email }}">
  {{ with .Errors.email }}<p class="error">{{ . }}</p>{{ end }}
  <input type="password" brick:model.lazy="password" value="{{ .State.password }}">
  {{ with .Errors.password }}<p class="error">{{ . }}</p>{{ end }}
  <input type="password" brick:model.lazy="confirm" value="{{ .State.confirm }}">
  {{ with .Errors.confirm }}<p class="error">{{ . }}</p>{{ end }}
  <button>Create account</button>
</form>
```

```text
page loads                          → no errors: nothing is touched
type "nope" in e-mail, leave it     → $set email; e-mail is touched → "Enter a valid email address."
                                      (name, password, confirm stay quiet)
click Create account                → brick.validate() touches every field with rules
                                      → "This field is required." under name, password and confirm
fill in the rest, use ada@taken.example → "That e-mail already has an account." (the action's error wins)
use ada@example.com                 → account created; brick.untouch() → the emptied form shows no errors
```

**Rules**

| Rule | Argument | Fails when |
|---|---|---|
| `required` | — | the value is missing, blank, or an empty list (`false` and `0` are values) |
| `min`, `max` | a number | a string has fewer/more characters, a list fewer/more items, a number is smaller/larger |
| `email` | — | the value is not a plain address (`ada@example.com`, no display name) |
| `url` | — | the value is not an `http`/`https` URL with a host |
| `pattern` | a Go regexp | the whole value does not match (`{pattern: "[a-z0-9-]+"}`) |
| `in` | a list | the value (or any element of a list) is not one of them |
| `same` | another path | the value differs from that field's (`{same: password}`) |

Every rule but `required` and `same` passes an empty value, so a field that may be left blank says so by not being `required`. A rule is a name or a one-key map; the first failing rule of a field gives its message. `messages:` replaces one rule's message by `path.rule`. Paths reach inside state (`address.city`, `items.0.qty`).

**When errors show**

- A field becomes **touched** when the browser first sends it: a `$set` from `brick:model`, or a deferred value in `sets`. With `.lazy` that is when the user leaves the field; with a live binding, after the debounce. A `.defer` field is touched by the next action that carries it.
- `.Errors` is computed at **every render** from the state, the rules and the touched set, so no error map is stored and `confirm` re-checks the moment `password` changes. `.Touched` (path → true) is there too, for styling a valid field.
- An action's own errors (`[state, errors]`) are laid over the rule errors for that render and win for their fields.
- `brick.validate(fields = None)` in an action checks the named fields (all with rules when omitted), touches them, and returns path → message for the ones that fail. It sees the state as the request delivered it, with deferred values applied. `brick.untouch(fields = None)` forgets touched fields, after a save that empties the form.
- Nothing validates automatically before an action: a *Cancel* or *Add row* button must not be blocked by a half-filled form.

**One value per field — `.Form` (v22).** Every input repeats its path three times (`brick:model="email"`, `.State.email`, `.Errors.email`), and a list row needs `printf "todos.%d.title"`. `.Form` gives each top-level state field one value with `.Name` (the path `brick:model` takes), `.ID` (instance id plus the path, dots as dashes, for `id=` and `for=`), `.Value`, `.Error`, `.Touched` and `.Invalid`; `.Fields` walks a dict's keys and `.Items` a list's elements:

```html
{{ with .Form.email }}<label for="{{ .ID }}">Email</label>
<input id="{{ .ID }}" brick:model.lazy="{{ .Name }}" value="{{ .Value }}">{{ with .Error }}<p>{{ . }}</p>{{ end }}{{ end }}
{{ range .Form.todos.Items }}{{ with .Fields.title }}<input brick:model.lazy="{{ .Name }}" value="{{ .Value }}">{{ end }}{{ end }}
```

The second input is `todos.0.title`, `todos.1.title` …, and its error is the `todos.*.title` rule's message for that item. `.Form` is computed from `.State`, `.Errors` and `.Touched` on every read, so it never disagrees with them and nothing new is stored or sent; a missing key behaves like `.State.x` (an error under `strictParams`). There is no form-level `action` or prefix: the submit stays `brick:submit`. Example: `examples/ui/demos/FormObjectTodos.html`.

**Rules on list items (v19).** A list of todos needs every title checked, and its length changes while the user edits it; declaring `todos.0.title`, `todos.1.title` … does not scale. Write `*` where the index goes:

```yaml
state:
  todos: [{title: "", tags: []}]
brick:
  rules:
    todos.*.title: [required, {min: 2}]
    todos.*.tags.*: [{min: 3}]
  messages:
    todos.*.title.required: "Each todo needs a title."
```

```html
{{ range $i, $t := .State.todos }}
  <input brick:model.lazy="todos.{{ $i }}.title" value="{{ $t.title }}">
  {{ with index $.Errors (printf "todos.%d.title" $i) }}<p class="error">{{ . }}</p>{{ end }}
{{ end }}
```

```text
save on three todos: "", "x" with tags ["ab", "good"], "Fine"
→ todos.0.title=Each todo needs a title.; todos.1.tags.0=Must be at least 3 characters.; todos.1.title=Must be at least 2 characters.
```

- `*` matches one list index, never a dict key: a dict has named fields, so name them. Errors are keyed by the concrete path (`todos.2.title`), and `messages:` names the pattern.
- `brick.validate()` checks every item that exists when the action runs, so an empty list checks nothing; to require at least one item put a rule on the list itself, `todos: [{min: 1}]`. `brick.validate(["todos.*.title"])` and `brick.untouch(["todos.*.title"])` take a pattern, or a concrete path a pattern covers.
- Where `*` stands, the declared value must be a list (or `null`); a dict or a scalar there is a compile error, and `*` cannot be the first segment. A value an action later writes that is not a list (None, a string, a dict) has no elements, so nothing is checked, as with an empty list. An item that is not a dict still has its path checked: `todos.*.title` on an item `"text"` reports `required` for `todos.N.title`.
- An exact declaration and a wildcard that both cover a path both apply, the exact one first; the first failing rule's message wins. A field touched and then removed (row deleted) shows no error, since the path names nothing.
- A custom `Options.Validator` gets the declared pattern in `Check` and the concrete path in `Validate`; `validate.Wildcard`, `validate.Match`, `validate.Expand` and `validate.Exists` help it do the same.

**Typos fail when the file is read.** An unknown rule (`requird`), a bad argument (`{min: two}`), a pattern that does not compile, a path not in `state:`, a message that names no declared rule, and `rules:` on a component without `state:` are errors with the file and the line. `brick.validate(["emial"])` fails the action.

**Your own rules.** `Options.Validator` takes any `brick.Validator` (package `validate` holds the interface and the default, and imports nothing from brick): `Check(path, rule)` at compile time, `Validate(ctx, path, value, rules, state, locale)` at render. Wrap `validate.Default()` to translate messages (it gets the render locale), or plug in go-playground/validator, ozzo or CUE. A component without `rules:` costs nothing.

**Reading errors in client code (v20).** `brick.errors(el)` reads the errors of el's stateful component as its last render left them, with no request: `brick.errors(input).has("email")`, `.first()`, `.get("email")`, `.all()`, `.clear("email")`. The server writes them on the component's root as `data-brick-errors` (JSON, keys sorted), only when `.Errors` is non-empty, so a root without errors keeps its bytes. After an action it reads the new errors; `clear` empties this view only and fires `errors-cleared`, and the messages the server rendered change with the next render.

The browser runs no rule engine. Add native `required`, `minlength` or `type="email"` yourself for instant hints if you want them; brick never injects attributes.

## Uploads — files into state, bytes into your storage

**The problem:** a profile form needs an avatar. The action endpoint takes JSON, so a file input's contents never reach it; and scripts must never touch the filesystem, so the bytes cannot go to the script either. Writing an upload route by hand means repeating the session check, the token check, the size limit and the type check that actions already do.

**The fix:** name the state fields that take files under `brick: {uploads:}`, put `brick:upload` on a file input, and give the engine an `UploadSink` — the place the bytes go. Each file streams to `/_brick/upload` as the pseudo-action `$upload`, through your action middleware; the sink stores it; a small ref lands in state.

`components/Avatar.html`

```html
---
state:
  avatar: null          # one file: a ref dict, or null
brick:
  uploads:
    avatar: {accept: [image/png, image/jpeg], max_size: 2MB}
---
<div>
  {{ with .State.avatar }}
    <img src="{{ .url }}" alt="{{ .name }}" width="72" height="72">
    <button brick:click='$removeUpload("avatar", "{{ .id }}")'>Remove</button>
  {{ else }}
    <span>No photo</span>
  {{ end }}
  <input type="file" accept="image/png,image/jpeg" brick:upload="avatar">
  <progress brick:upload-progress="avatar" value="0" max="1"></progress>
  <button brick:click='$cancelUpload("avatar")'>Cancel</button>
</div>
```

`main.go`

```go
sink, _ := disk.New(disk.Options{Dir: "/var/tmp/uploads", URLPrefix: "/uploads/"})
mux.Handle("/uploads/", http.StripPrefix("/uploads/", sink.Handler()))
eng, _ := brick.New(brick.Options{Uploads: sink /* , … */})
```

```text
pick me.png (20 KB)        → PUT /_brick/upload  Brick-Upload: avatar
                             → middleware sees Action "$upload", Args ["avatar", {name, type, size}]
                             → bytes counted while streaming, type sniffed: image/png
                             → sink stores it → .State.avatar = {id, name: "me.png", type: "image/png", size: 20480, url: "/uploads/3f…"}
                             → the component re-renders: <img src="/uploads/3f…">
pick notes.txt renamed .png → 415 "this type of file is not accepted" (the bytes are text)
pick a 5 MB photo          → refused in the browser (data-brick-uploads); a client that lies gets 413 mid-stream
click Remove               → $removeUpload → the sink deletes it → .State.avatar = null
```

**Declaring fields.** Each key of `uploads:` is a declared state field: `null` for one file (a new upload replaces it), `[]` for several (`max_files`, default 10, caps the list). `accept` lists media types (`image/*` works); `max_size` is bytes or `KB`/`MB`/`GB` (1024-based), 10 MB by default. An undeclared field, a field of another type, `max_files` above 1 on a `null` field, a bad size or type, and `uploads:` with no `Options.Uploads` are errors with the file and line.

**What the server checks.** The handle, token and session, as for an action; then your `ActionMiddleware` (refuse `$upload` for a signed-out user, cap uploads per session); then the declared count and size, the size again while the bytes stream, and the type sniffed from the first 512 bytes, never the browser's claim (text formats such as CSV take the claimed `text/*` type, because they all sniff as `text/plain`). Every refusal aborts the sink's writer, so nothing half-written is kept. `$set` cannot write an upload field.

**In the browser.** `<progress brick:upload-progress="avatar">` fills while the file is sent and the input carries `data-uploading`; `brick.on("upload-progress" | "upload-done" | "upload-error", fn)` reports each file, `upload-error` with `gone` when the state expired (the file is not replayed; ask again) and `cancelled` after `$cancelUpload("avatar")`. Uploads wait their turn in the component's queue like any action. `brick.upload(el, "avatar", files)` sends files from a drop or a paste; the kit `Dropzone` passes `brick:upload` to its input. Since v20 the Dropzone shows a thumbnail of a chosen image before it is sent: the browser makes it from the file (a `blob:` URL), nothing is uploaded for it, and the URL is released as soon as the file leaves the list. With `brick:upload`, files show in the list while they upload and disappear when the component's state takes over.

**Scripts see refs, not files.** An action reads `state["avatar"]["id"]` like any field, and validation rules apply (`avatar: [required]`). Keeping the file — moving it out of the temporary area, attaching it to a record — is a capability your program grants through `ScriptGlobals`, as with `db` and `http`.

**Your own storage.** `Options.Uploads` takes any `brick.UploadSink`: `Open(ctx, meta)` returns a writer with `Write`, `Close() → UploadRef` and `Abort(err)`. Add `Remove` (`UploadRemover`) to support `$removeUpload`; without it the call is refused rather than leaving the bytes behind. Add `Presign` and `Confirm` (`UploadPresigner`) and the browser PUTs straight to storage, then confirms with `$uploaded`; the engine accepts only an id it issued to that component within the hour, and checks the limits again on the size and type `Confirm` reads from storage. `upload/disk` is the reference sink (random ids, a TTL `Sweep`, a file handler that serves images inline and everything else as a download); `contrib/upload-s3` is the S3 one (ids are 32 lowercase hex, format-checked, and `Remove` rebuilds the object key from the id alone; the bucket needs a CORS rule allowing `PUT` from your origin, see its README); `bricktest.NewMemoryUploads()` is for tests.

**Your own check (v21).** `Options.UploadCheck` sees each file's meta and its first 512 bytes (`UploadCheckBytes`) after the `uploads:` checks and before the sink stores anything; return `brick.RefuseUpload("The image must be a PNG")` to refuse it with that message on the field (422, also in a form without JavaScript), or any other error for a server failure. On a presigned upload it runs with no bytes (the claims only), so a content check there belongs in your sink's `Confirm`. It is an option rather than a sink method, so adding a check never hides your sink's `Remove` or `Presign`.

**With scripts off.** A file input with `brick:upload` inside a `brick:submit` form also uploads when the runtime is not there: the form posts multipart and each file goes through this same path before the action runs (see *Forms without JavaScript*).

**A limit of presigned uploads: no sniffing.** The bytes go straight to storage, so the engine never sees them. `Confirm` reports the size storage holds, but the type is the stored `Content-Type` the client signed for, so a client can store any bytes under an accepted type. If you need the content checked, use the proxied path (a sink without `UploadPresigner`) or scan the file when your app keeps it.

## The browser runtime — keys, transitions, viewport actions, hooks

**The problem:** a todo list without ids re-renders every row when one is added at the top (a focused input moves to the wrong row); a third-party map inside a component is wiped by every action; switching a tab snaps; infinite scroll and a "like" that feels instant need hand-written JavaScript; and a site cannot add a header to actions or re-initialise a widget after a re-render.

**The fix:** a few attributes the runtime reads, and two hook words for the rest. None of them adds anything to the wire.

`components/Feed.html`

```html
---
state: {rows: [], more: true, liked: false}
---
<section brick:transition="slide">
  <ul>{{ range .State.rows }}<li brick:key="{{ .id }}">{{ .title }}</li>{{ end }}</ul>
  {{ if .State.more }}<div brick:intersect.margin.200px="load_more">Loading…</div>{{ end }}
  <div id="map" brick:ignore></div>
  <button brick:click.optimistic='$toggle("liked")'>
    <span brick:show="liked" {{ if not .State.liked }}hidden{{ end }}>♥ Liked</span>
    <span brick:show="!liked" {{ if .State.liked }}hidden{{ end }}>♡ Like</span>
  </button>
</section>
```

```text
load_more adds rows 21–40     → 20 new <li> appended; rows 1–20 keep their elements (brick:key)
the sentinel is still visible → the runtime re-arms it after the morph and it loads again, until more is false
click Like                    → "♥ Liked" shows at once; the action runs; a 500 puts "♡ Like" back
any action                    → the map's tiles stay (brick:ignore); the morph runs inside a view transition
```

**Morph keys and controls.** `brick:key` keys an element among its siblings, so it need not be unique in the document (two copies of a component can reuse keys); `id` still wins when both are present, and two siblings with one key warn once in the console. `brick:ignore` keeps an element exactly as it is across morphs (when the old and the new markup both carry it); `brick:ignore.self` keeps only its attributes and still patches its children; `brick:replace` swaps the element for a fresh copy instead of patching it, which remounts its client code. All three work in any file, because a navigation morphs the whole page; `brick:ignore` on a stateful component's root is a compile error (its own actions could never show their result). In a stateful file, a `{{ range .State.… }}` whose rows are plain elements with no `id` or `brick:key` gets a Dev warning.

**View transitions.** `brick:transition` on a stateful root wraps its action morphs in `document.startViewTransition`; the value is optional transition types (`brick:transition="slide"`) for `:active-view-transition-type(slide)` in CSS. Polls, `$set` and a recovery never animate, and a morph runs plainly when the browser lacks the API, the user prefers reduced motion, or another transition is running. Naming elements (`view-transition-name`) is your CSS.

**An action chooses its transition (v22).** `brick.transition("slide")` (Go: `Effects.Transition`) animates this answer with the `slide` type, on any root; `brick.transition("")` animates it untyped, and `brick.transition("none")` skips the transition on a root that opts in with `brick:transition`. The choice also wins over the rule that polls never animate. Types are CSS identifiers, at most 8; the last call in an action wins. Reduced motion, a missing API or a running transition still skip it.

**Viewport actions.** `brick:intersect="call"` runs a call when the element scrolls into view: `.leave` when it leaves instead, `.once`, `.half` / `.full` for the visible share, `.margin.200px` to start early. A new report is ignored while the element's own action is queued or in flight, and the runtime re-arms the element after every morph, which is what makes infinite scroll load the next page while the sentinel is still on screen. The template's `{{ if }}` is the stop condition.

**Impressions (v15).** `brick:intersect.dwell` waits until the element has stayed in view for a second before its call runs, so a card scrolled straight past is not counted. `.dwell.3000ms` changes the wait. Leaving the viewport or switching tabs cancels it, and the next stay starts from zero. It fires once per stay; add `.once` to count it only the first time, and `.half` or `.full` to require that share of the element on screen.

```html
<article brick:intersect.dwell.2000ms.once.half='seen({{ .ID }})'>
  …
</article>
```

**Optimistic writes.** `.optimistic` on `brick:click`, `brick:change`, `brick:input`, `brick:blur`, `brick:keydown` or `brick:keyup` with a literal `$set(path, value)` or `$toggle(path)` writes the signal before the request is sent, so `brick:show`, `brick:text` and `brick:dirty` change at once; when the action fails the value goes back and `action-error` fires. On success the server's render decides, as always. Anything else with `.optimistic` is a compile error: counting (`likes + 1`) would need code in the browser, so render the count on the server.

**Hooks decide, events tell.** `brick.on` stays a notification; `brick.hook(name, fn)` registers code that can change what happens next, and returns its unregister function:

```js
brick.hook("request", (r) => { r.headers["X-Trace-Id"] = crypto.randomUUID(); });   // or r.cancel()
brick.hook("morph", ({ from }) => !from.classList.contains("cm-editor"));            // false: leave it this time
brick.hook("morphed", ({ root }) => initTooltips(root));
brick.watch(document.querySelector("#cart"), "count", (n, old) => { document.title = `(${n}) Shop`; });
```

`request` runs before every action and upload request (it may be async); changing `Content-Type`, `Accept` or any `Brick-*` header throws, because they are the CSRF floor and the protocol, and `r.cancel()` ends the job quietly with an `action-cancelled` event; `r.retry` is 0 on the first send and counts the retries after a 503 or 429. `response` sees `{root, action, status, envelope}` before the morph. `morph` runs synchronously for each element about to be patched (a stateful root it refuses still takes its new handle). `morphed` follows each action's morph, and `ready` runs once the page is bound (registered later, at once). `brick.watch(el, path, fn)` follows a signal of `el`'s component — a bound field, or a value an optimistic write set — and calls `fn(value, old)` only when it changes.

**Client commands (v18).** A menu that opens and closes needs no server. `brick:js.<event>` runs a short, fixed list of DOM commands in the browser, on any page, with no request:

```html
<div brick:js.click.outside='hide("#menu")' brick:js.keydown.escape.window='hide("#menu")'>
  <button type="button" aria-expanded="false" brick:js.click='toggle("#menu"); toggleAttr("this", "aria-expanded", "true", "false")'>Menu</button>
  <ul id="menu" hidden>…</ul>
</div>
```

The commands are `show`, `hide` and `toggle` (the `hidden` attribute, as `brick:show` uses), `addClass`, `removeClass`, `toggleClass`, `setAttr`, `removeAttr`, `toggleAttr(target, name[, on[, off]])`, `focus` and `dispatch(target, name[, detail])`. They are separated by `;` and every argument is a JSON literal, so the attribute takes single quotes. Each takes its target first: a CSS selector (every match), `"@name"` (a `brick:ref` in the element's own component), `"this"` or `"closest .card"`. Events: `click dblclick input change submit focus blur keydown keyup mouseenter mouseleave`, plus `.once`, `.outside` (click only), and `.window` and key combos (keydown and keyup only). The list is closed and has no variables or conditions: commands only change the DOM, the way a CSS class toggle would, and state still changes only in actions. What a command changed survives the next morph, so a menu it opened stays open when an action re-renders the component. The compiler checks the event, the modifiers and every literal command; `setAttr` and `toggleAttr` refuse event handlers (`on*`), `srcdoc`, engine attributes and script URLs, and `dispatch` refuses brick's own event names. The runtime repeats those checks, because `{{ }}` can fill a value. Put `.outside` on a wrapper that holds both the button and the menu, or the button's own click counts as outside; commands never prevent the default, so use `type="button"` inside a form.

**Commands from code, and finding components (v21).** `brick.js(el, "hide('#menu'); addClass('this', 'open')")` runs the same command list from client code (a `brick.mount` hook, an effect handler, a stored attribute: `brick.js(el, el.getAttribute("data-on-close"))`), with `el` as `this`; a bad list throws. `brick.find("Counter")` returns the first stateful `Counter` on the page (or `null`), `brick.all("Counter")` every one, and `brick.all()` every stateful component, matched by component name (the file stem): `brick.action(brick.find("Cart"), "reload")`.

**Exit transitions (v18).** `brick:leave="transition-opacity duration-300 opacity-0"` plays its classes before the element goes: when a morph drops it, a `rows` delete or `limit` trim removes it, or a JS `hide` hides it. The runtime waits for the transition or animation those classes start, at most 2 s, and skips the wait under `prefers-reduced-motion`. While it plays the element is `inert` and carries `data-brick-leaving`; a second morph never reuses it, and a `show` cancels a `hide` that is still playing. One attribute is enough for a CSS transition, because a transition takes its timing from the style after the change. Only the element being removed animates; a `brick:leave` inside it goes with its parent. Not covered: `brick:replace` and navigation swaps.

**Enter transitions and `brick:show` (v21).** `brick:enter` is the look an element starts from when it is shown, and `brick:leave` the look it goes to when it is hidden or removed. `brick:show` plays both, and so does the JS `show` command:

```html
<div brick:show="open" class="transition-opacity duration-300" brick:enter="opacity-0" brick:leave="opacity-0" hidden>…</div>
```

When `open` turns true the element is un-hidden with `opacity-0`, which comes off on the next frame, so it fades in (`data-brick-entering` while it plays, at most 2 s); when `open` turns false it fades out and then takes `hidden`. Nothing plays on the first render or under `prefers-reduced-motion`. An element without these attributes shows and hides at once, as before.

**Small things.**

- `brick:ref="answer" names an element; `brick.ref(el, "answer")` finds it in `el`'s stateful component (or on the page outside every component). A name used twice in one file is a compile error.
- `brick:offline` elements show while the browser is offline (`brick:offline.remove`: hide), and `<html>` carries `data-offline`.
- `brick:submit.lock="save"` makes the form's text fields read-only and its choices `aria-disabled` while the action is queued or in flight; the default stays buttons-only.
- A `brick:show` element can be hidden on first paint by the template itself: `<p brick:show="open" {{ if not .State.open }}hidden{{ end }}>`.
- `brick new component Counter -state` (or `-client`, `-island`, `-lazy`) and `brick new page blog/hello` write a starter file; they never overwrite. `brick new site blog` writes a whole starter project into a new or empty folder: `brick.yaml` with `libraries: [brick-kit]`, `site.yaml` with a title from the folder name, `layouts/default.html` with `{{ assets }}`, and `content/index.html` calling the kit's `<Card>` and `<Button>`; `brick check blog` passes and `brick serve blog` renders it as written, and a folder that holds any file is refused. Each command takes only its own flags: `brick serve -check` or `brick vendor -o x` is a usage error rather than a flag silently ignored, `brick build -h` lists only build's flags, and `brick build -json` / `brick vendor -json` print one JSON report on stdout for scripts. `brick new component Revenue -lazy` writes a component the page is sent without; the browser fetches it when it scrolls into view, and the starter shows the `<brick:boundary>` loading block that stands in until then. `brick new page blog/[slug]` writes one file that answers every `/blog/<slug>/`, reading the value as `.Page.Route.slug`, and explains how to add a loader and `brick: {paths:}` so `brick build` knows which pages to write.

## Server state — replies, first values, the URL and memory

**The problem:** an autocomplete pays for a full render to get eight names back; an edit form's `state:` is a literal, so it cannot start from the record it edits; a typed search cannot be trimmed without turning it into an action; a filtered table cannot be shared, and Back does not undo the filter; a user's column layout is gone when the instance expires.

**The fix:** two script calls, two hook words, one built-in, and two frontmatter keys.

`components/Orders.html`

```html
---
props: {status: {type: string, default: open}}
state: {q: "", page: 1, columns: [id, total]}
use: {orders: ~/scripts/orders.star}
brick: {init: orders.setup, updated: orders.tidy, query: {q: {as: search}, page: {history: push}}, remember: [columns]}
---
<input brick:model.debounce.300ms="q" value="{{ .State.q }}">
<button brick:click='$reset("q", "page")'>Clear</button>
<button brick:click='$set("page", {{ add .State.page 1 }})'>Next</button>
```

`scripts/orders.star`

```python
def setup(state, props):
    return dict(state, page = 1)

def tidy(state, props, paths):
    page = 1 if "q" in paths else state["page"]
    return dict(state, q = state["q"].strip().lower(), page = page)

def suggest(state, props, q):
    brick.skip_render()
    brick.reply([o["name"] for o in db.query("select name from customers where name like ?", q + "%")][:8])
    return state
```

```text
open /orders?search=acme&page=3  → page 3 of the "acme" list, columns as this user left them
type " Bob"                      → q = "bob", page = 1 (updated:), the URL becomes ?search=bob (replace)
click Next                       → ?search=bob&page=2 (push); Back → page 1 again, restored in place
click Clear                      → q = "" and page = 1 (the declared values); both parameters leave the URL
await brick.action(input, "suggest", ["a"]) → ["ada", "al"]; no render, the root's handle updated
```

**Replies and renderless actions.** `brick.reply(value)` attaches a JSON value to the answer, and `brick.action(el, name, args)` resolves with it (`undefined` when there is none; `brick:click` ignores it). `brick.skip_render()` keeps the state change and bumps the version but skips the render: the answer carries the root's new handle instead of HTML, because a sealed store's handle is the state itself. Only the server decides, and never automatically: a template may read `.Ctx`, time or loaders. Dev warns when a renderless action changes a state key the template reads, or returns errors no render will show. In Go, `brick.EffectsFrom(ctx).Reply(v)` and `.SkipRender()`; middleware sees `ActionResult.Reply` and `.SkipRender`.

**First values.** `init:` names `fn(state, props)`, run once when an instance is created, before its first save; it sees the declared state, the call's props with their defaults and the script's globals, and cannot dispatch (it is drawing, not acting). A failing `init` fails the render like a loader, which a `<brick:boundary>` contains. Request values are never there in a cached page: a component that needs the signed-in user is already an island.

**Field hooks.** `updated:` names `fn(state, props, paths)`, run after `$set`, deferred sets, `$toggle` or `$reset` change fields, before validation and the re-render; `paths` are the fields whose value changed. It may return `[state, errors]`, and the errors join `.Errors`. It never runs after a named action (the action owns its state), and its own changes do not trigger it again. `$reset(path…)` puts each path back to its value in `state:` (not `init`'s output) and untouches it.

**State in the URL.** `query:` lists top-level fields holding a scalar or a list of scalars, or maps them to `{as: param, history: replace | push, keep: true}`. The page's URL seeds a new instance through the same gate as `$set`, so `?page=abc` is ignored (a Dev warning), never an error; repeated parameters fill a list. After an action, every queried field that changed is written to the URL (a field back at its declared value leaves the URL unless `keep: true`), other parameters untouched. Back and Forward that change only parameters a component owns restore its fields in place; anything else is a navigation. A component with `query:` is stateful, so it is always an island and the site's cache never serves one visitor's query to another; `site` passes the request's query, and a Go host passes `brick.WithQuery(r.URL.Query())` to `Fill`. A static export shows the declared state.

**When the URL changes (v22).** Give a component with `query:` a `params:` hook to respond when the URL changes its parameters: a link to `?tab=books`, Back, Forward, `brick.navigate`. A same-path link whose changed parameters all belong to components on the page is a **patch**: no page fetch, `pushState`, and only the owning components re-render, through the `$params` built-in. A link that also changes a parameter nobody owns, or the path, stays a navigation; `brick:nav="off"` still forces a full load.

```html
---
state: {tab: all, page: 1}
brick: {query: [tab, page], params: on_params}
---
<brick:script>
def on_params(state, props, params):
    if state["page"] > 5:
        return dict(state, page = 5)   # the URL is corrected with replaceState
    return state
</brick:script>
<a href="?tab=books">Books</a>   <!-- a patch: no page fetch, only this component re-renders -->
```

The hook is `fn(state, props, params)` and returns the state or `[state, errors]`, like `updated:`; `params` is the component's own fields as the address bar shows them (a field at its declared value is absent unless `keep:`). It runs when the instance is created (after the URL seeded it) and on every `$params`, never after the component's own actions. A correction it makes on `$params` replaces the history entry, so Back never lands on the uncorrected address; a correction on the first page load shows in the component but not in the address bar. `params:` needs `query:` and must name a script function.

**Remembered fields.** `remember:` lists fields kept per session beyond the instance's life, in your store: `Options.Remember` is a `Rememberer` (`Load` and `Save` by `RememberKey{Session, Path, Key, PagePath}`), `brick.NewMemoryRemember()` serves development, and without one `remember:` is a compile error. Values are loaded after `init` and before the URL (a shared link shows what it says), saved after an action that changed one, and coerced like `$set` on the way in. With no session nothing is loaded or saved. The call's `key=` shares one set across pages; without it, each page path has its own.

**Dynamic routes.** Name a page file or folder with a parameter in brackets: `content/posts/[slug].html` answers `/posts/hello/`, `content/docs/[...path].html` answers any depth under `/docs/`, and `[[...path]]` also answers `/docs/` itself. A static file always wins over a pattern. Between patterns, the more specific one wins segment by segment (a literal, then `[x]`, then `[...x]`). The template reads `{{ .Page.Route.slug }}` and the loader reads `input["page"]["route"]["slug"]`, always as a query parameter, never spliced into SQL. When the record does not exist, call `brick.not_found()` and the visitor gets your 404 page; `brick.redirect`, `brick.error` and `brick.header` answer a moved or removed record (v15, [Protect a page](#protect-a-page-redirect-and-answer-with-a-status)). Add `brick: {paths: posts.slugs}`, a function returning `[{"slug": "hello"}, …]`, to export one page per entry with `brick build`. In serve, a value that list does not name is then a 404 without running the page. A stateful component on such a page keeps the route values across its actions.

**Temporary state.** A field listed in `brick: {temporary: [rows]}` shows the value an action set on the render that follows, and is reset to its `state:` value before the instance is saved. Use it for data that is large and cheap to load again, such as a page of streamed rows, so it is never written to the state store; the next action starts from the declared value. Each entry must be a top-level `state:` field.

**Locked fields.** Every field in `state:` can be changed by the browser: that is what `$set` and `brick:model` are for. For a field the server chose, that is a hole. Here `editing_id` is the row the server opened for editing, and `save` trusts it:

`components/RowEditor.html`

```html
---
props: {owner: {type: int, required: true}}
state: {editing_id: 0, draft: {title: ""}, note: ""}
use: {rows: ~/scripts/rows.star}
brick: {locked: [editing_id, draft]}
---
<div>
  <button brick:click="open(7)">Edit row 7</button>
  <input brick:model.defer="note" value="{{ .State.note }}">
  <button brick:click="save">Save</button>
  <i>{{ .State.editing_id }}|{{ .State.draft.title }}</i>
</div>
```

`scripts/rows.star`

```python
def open(state, props, id):
    row = db.query("select title from rows where id = ? and owner = ?", id, props["owner"])[0]
    return dict(state, editing_id = id, draft = {"title": row["title"]})

def save(state, props):
    db.exec("update rows set note = ? where id = ? and owner = ?", state["note"], state["editing_id"], props["owner"])
    return state
```

```text
click "Edit row 7"                                 → 7|Row 7   (an action writes a locked field)
type in the note, click Save                       → saved; note is not locked, so its deferred set is applied
POST {"action":"$set","args":["editing_id",9]}      → 422 "brick: field value refused: locked field: state.editing_id is locked (brick: {locked:}); only the server writes it"
POST {"action":"save","sets":[["draft.title","x"]]} → 422 "… state.draft is locked …"; save did not run, the state is unchanged
```

List top-level `state:` fields; locking one locks every path below it. The gate covers every way a client writes state: `$set`, `$toggle`, `$reset`, the deferred sets an action carries, and `brick:model`. Actions, `init:`, `updated:` and remembered values are server writes and are never refused. `brick check` reports a `brick:model` bound to a locked field and a literal `$set("editing_id", …)` in the same file, since the server would refuse it on every click; a value built with `{{ }}` is left to the runtime gate. A locked field cannot also be in `query:` or `uploads:`, because the URL and an upload are client writes. In Go, `errors.Is(err, brick.ErrLockedField)`; it is also an `ErrBadValue`, so existing 422 handling applies. Keep scoping by owner in the action too (`and owner = ?` above): the lock stops a client choosing another row, not a stale id from a row that was since moved.

**Closed by default (v18).** `locked:` names the fields only the server writes. A site that wants the opposite default, where the client writes nothing unless a component says so, sets `Options.LockState` (or `lockState: true` in `brick.yaml`) and lists the client-writable fields per component:

```yaml
state:
  editing_id: 0
  title: ""
  q: ""
brick:
  writable: [title]
  query: [q]
```

Here `title` is writable because it is listed, `q` because `query:` already makes the URL its writer (upload fields work the same way), and `editing_id` only through the server. A `$set("editing_id", 7)` from the browser answers 422 (`state.editing_id is closed to the client …`, an `ErrLockedField`), and a literal `brick:model="editing_id"` fails to compile, naming the fix: add the field to `writable:`. Per component, `brick: {lock_state: false}` keeps one component open under a closed engine, and `brick: {lock_state: true}` or a `writable:` list alone closes one under an open engine. It is off by default, so existing sites are unchanged. `locked:` keeps working and wins over `writable:`; listing a field in both is a compile error.

## Lazy islands and streamed replies

**The problem:** a dashboard has one slow panel, and every visitor waits for the slowest loader before the first byte arrives. And a chat reply arrives over several seconds, but an action answers once, so the user stares at a spinner and then sees the whole text at once.

**The fix:** `brick:lazy` on the call renders the panel after the page, and `brick.stream` sends text into a `brick:ref` while the action runs.

`content/dashboard.html`

```html
<brick:boundary>
  <SalesChart brick:lazy="visible" :range=".Params.range" />
  <brick:prop name="loading"><div class="skeleton h-64"></div></brick:prop>
  <brick:prop name="fallback"><p>Chart unavailable.</p></brick:prop>
</brick:boundary>
```

`components/Ask.html`

```html
---
state: {q: "", answer: ""}
---
<brick:script>
def ask(state, props):
    text = ""
    for part in ai.complete(state["q"]):   # a global your Go host gives scripts
        brick.stream("answer", part)
        text += part
    return dict(state, answer = text)
</brick:script>
<div><p brick:ref="answer">{{ .State.answer }}</p>
<input brick:model.defer="q"><button brick:click="ask">Ask</button></div>
```

```text
GET /dashboard/   → the page, with <brick-lazy data-brick-lazy="s1.…" data-when="visible"><div class="skeleton …"></div></brick-lazy>
scroll down       → GET /_brick/island?t=s1.… → {"v":1,"html":"<figure>…</figure>","assets":[…]}; the skeleton is replaced
click Ask         → {"stream":{"ref":"answer","text":"Hel"}} … then {"v":1,"version":1,"html":"…"}; the text grows as it arrives
```

**Lazy islands.** `brick:lazy` (or `brick:lazy="load"`) fetches the component right after the page loads, `"visible"` when it scrolls into view, `"idle"` when the browser is idle; `brick: {lazy: visible}` in a component's frontmatter makes it the default and the call site wins. Any component can be lazy, stateful or not, and lazy implies island. The placeholder shows the enclosing boundary's `loading` block (nothing without one), and a failure shows the boundary's `fallback` in production and the error block in development. The page carries an opaque **ticket**, never the component's name or props: it is sealed with your state keys (`site.Options.StateKeys` or a `NewSealedStates` store), bound to the visitor's session and valid for an hour, so a forged, foreign or expired ticket answers 410 and the runtime reloads the page once. Without state keys tickets live in the process's memory, which development warns about. The island renders with the page's route and query and with your request values (`site.Options.Request`, or `Options.ActionRequest` for a Go host), and its answer appends the stylesheets and modules the page does not have yet. A page with a lazy island is private, like a stateful one, because its tickets belong to one visitor. `brick build` renders lazy islands in place, since an exported site has no endpoint. Use it below the fold: search engines see the placeholder. Go hosts mount `eng.IslandHandler()` at `{AssetBase}island`, and `Options.IslandTickets` replaces the ticket store.

**Streamed replies.** `brick.stream(ref, text, replace = False)` appends text to the element with `brick:ref="ref"` in the component's root, or replaces its text, before the action returns; the action's normal render then lands as usual. Chunks are always text, set with `textContent`, so they never need escaping; markdown or HTML is the final render's job. The answer is NDJSON only when the action streams; otherwise it is the usual envelope. A failure after streaming ends the stream with an error line, `action-error` fires, and the streamed text stays until the next render. In Go, `brick.StreamFrom(ctx).Write(ref, text, replace)`; a middleware can wrap the stream with `brick.WithActionStream` before calling `next` to filter it, and `bricktest`'s `LiveRoot.Stream` returns the chunks. A long stream holds its component's queue, so give a streaming component a longer `actionmw.Timeout`, and turn off proxy buffering (the answer sends `X-Accel-Buffering: no`).

**Streamed islands (v20).** `brick:lazy="stream"` (or `brick: {lazy: stream}`) renders the island in the same response, out of order: the visitor gets the page at once with the boundary's `loading` block, and the island's HTML follows when it is ready, without a second request. Use it for the slowest panel on an important page, where a lazy island's round trip is the wait.

```html
<brick:boundary>
  <Revenue brick:lazy="stream" />
  <brick:prop name="loading"><div class="skeleton"></div></brick:prop>
  <brick:prop name="fallback"><p>Revenue is unavailable.</p></brick:prop>
</brick:boundary>
```

What the browser receives first, then about a second later in the same response:

```html
<brick-lazy data-brick-stream="s1"><div class="skeleton"></div></brick-lazy>
…rest of the page…
<noscript><style>brick-lazy[data-brick-stream="s1"]{display:none}brick-fill[data-brick-fill="s1"]{display:contents}</style></noscript><brick-fill data-brick-fill="s1" hidden>…the island…</brick-fill><script>…__brickFill("s1")</script>
</body></html>
```

- The islands render concurrently, at most `Options.StreamWorkers` at a time (default 4), and are written in the order they finish; a tiny inline script moves each into place (with the page's nonce under `WithNonce`); when the runtime places a fill instead (the script was blocked, or the fill brings new assets) it fires `island-loaded` with `stream: true`.
- Where it streams: a browser loading the page (`Sec-Fetch-Mode: navigate`) on a server whose writer can flush; the site sends `X-Accel-Buffering: no` for nginx. Everywhere else (curl, crawlers, client navigation, a static export, a Go host calling `Engine.Fill`) the island renders in place, with the same bytes as a plain island. A Go host streams with `eng.Stream(ctx, w, flush, res)`, or `eng.FillStream` and `Fills.Each` to write the parts itself; `Result.HasStream()` says whether there is anything to stream.
- A stream island that fails never fails the page, whose status is already sent: Prod writes the boundary's `fallback`, Dev the error block and the fallback, and the error reaches `OnError` hooks and `Fills.Failed()`. A loader's redirect or status shows the fallback; a loader's `brick.header` is dropped (the headers are gone), with a Dev warning. A stream island inside an island being filled loads as `load` instead.
- Gotchas: a Content-Security-Policy without the page's nonce blocks the inline script (the runtime then places the islands when the whole response has arrived); without JavaScript each streamed island shows at the end of the body, not in its place, so a page whose layout must hold without JavaScript uses a plain island; like a lazy island, a stream island cannot sit where only table rows or `<option>` are allowed.

## Server push — live updates from elsewhere

**The problem:** two people look at the same order list. One marks an order shipped; the other's list still says "pending" until they reload, because an action re-renders only the component that ran it, in the tab that ran it. `brick:poll="5s"` works, but it asks every five seconds whether anything changed, and nearly always nothing did.

**The fix:** the list says which topics it cares about, the action publishes one when it changed something, and every open page holding a listening component refreshes it.

`components/Orders.html`

```html
---
state: {filter: open}
brick: {listen: [orders, "shop:{{ .Props.shop }}"]}
props: {shop: {type: int}}
---
<brick:script>
def ship(state, props, id):
    db.exec("update orders set status = 'shipped' where id = ?", id)
    brick.publish("orders")          # after this action succeeds and is saved
    return state
</brick:script>
<ul>{{ range orders .State.filter }}<li>{{ .id }} {{ .status }}
  <button brick:click="ship({{ .id }})">Ship</button></li>{{ end }}</ul>
```

`main.go`

```go
push, err := brick.NewPush(brick.PushOptions{}) // in-memory: one process
opts.Push = push
// From any Go code too, e.g. a webhook or a job:
eng.Publish(ctx, "orders")
```

```text
GET /orders/        → <ul data-brick-state="…" data-brick-listen="orders shop:7" data-brick-listen-token="AUBN…">
the runtime         → GET /_brick/events?l=AUBN…   (one EventSource for every listening component on the page)
                    ← retry: 2000 / : open
someone clicks Ship → POST /_brick/action … then the server publishes "orders"
every open page     ← event: refresh / data: orders   → a quiet $refresh of each component listening to "orders"
```

**Listening.** `brick: {listen:}` takes a topic, a list, or `{topics: [...], keep_alive: true, presence: …}` (`presence`, v22, below; `topics` may then be omitted), and needs `state:` (a refresh is an action) and `Options.Push` (without it the file fails to compile). A topic is 1–200 characters from `A-Z a-z 0-9 _ . : / @ -`, and may use `{{ .Props.x }}`, `{{ .State.x }}`, `{{ .Params.x }}` and `{{ .Key }}`; a literal topic is checked when the file is read, a templated one when it renders.

**Publishing.** `brick.publish(topic)` in an action waits until the action succeeded and its state is saved, so a listener reads what the action wrote; a failed action publishes nothing, and a topic named twice is published once. `engine.Publish(ctx, topic)` publishes from anywhere in Go.

**Who may listen.** A topic name is not a secret, so the browser never names one. Each listening root carries a listen token the server sealed when it rendered: the topics, the session and an expiry (24 hours). The events endpoint opens the tokens and subscribes to exactly those topics; a forged token or one from another session is refused with 403, an expired one with 410. `PushOptions.AuthorizeTopic(r, topic)` adds a check when the stream opens and again on every recheck, for access that can be revoked while a page is open. An event carries the topic and nothing else: the component reads its data again through its own render, with every check that render has.

**In the browser.** One stream per page, reopened only when the page's set of topics changes. Each listening component refreshes at most once per tick however many events arrive. A hidden tab waits until it is shown, unless the component says `keep_alive: true`. After a dropped connection the browser reconnects by itself and every listener refreshes once, because what was published meanwhile is not replayed; a refused stream is retried with backoff (1 s doubling to 30 s, 8 times, then a `push-error` event). The stream closes when the page is left.

**When a stream ends (v19).** An open stream is trusted no longer than what opened it. It ends in four ways. The client goes away. Its earliest listen token expires (`PushOptions.TokenTTL`): the server first sends one `refresh` per topic, so every listening component re-renders and the page holds fresh tokens, then closes; the browser's reconnect with the old URL is refused `410`, and the runtime's retry a second later opens with the fresh tokens, with no reload. A recheck every `PushOptions.Recheck` (`DefaultPushRecheck`, 1 minute; it runs when `Options.Sessions` or `AuthorizeTopic` is set) asks both again; a different session, an error, or a denied topic ends the stream, the reconnect is refused `403`, and the runtime gives up with `push-error`. And `Push.RevokeSession(ctx, sessionID)` ends that session's streams at once on every server sharing the broadcaster; `Engine.RevokeSession` calls it too, so one sign-out call, made after ending the session in your store, ends its live components and its streams.

```go
func signOut(w http.ResponseWriter, r *http.Request) {
	sid := sessions.ID(r)
	sessions.Destroy(sid)                          // the store no longer answers it
	_ = eng.RevokeSession(r.Context(), sid)        // state instances and push streams, on every server
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
```

Topics starting with `brick:` are the engine's own (revocations travel as `brick:revoke:<session hash>`): `brick: {listen:}`, `brick.publish`, `Engine.Publish` and job topics refuse them, and a custom `Broadcaster` that filters topics must use `brick.ValidBroadcastTopic`, not `ValidTopic`.

**Who is here — presence (v22).** A chat room or a shared document wants to show who else is on it, and update the list as people come and go. Publishing a topic from your own join and leave hooks would need per-connection bookkeeping and a store every server shares. Instead, declare `presence:` under `listen:` and read `{{ .Presence "topic" }}`:

```html
---
state: {n: 0}
brick: {listen: {presence: "room:{{ .Props.id }}"}}
---
{{ $p := .Presence (printf "room:%v" .Props.id) }}
<p>{{ $p.Total }} here</p>
<ul>{{ range $p.Entries }}<li>{{ index .Data "name" }}{{ if gt .Conns 1 }} ({{ .Conns }} tabs){{ end }}</li>{{ end }}</ul>
```

```go
push, _ := brick.NewPush(brick.PushOptions{Presence: brick.PresenceOptions{
	Identify: func(r *http.Request, topic string) (brick.PresenceEntry, bool) {
		u := currentUser(r)
		if u == nil {
			return brick.PresenceEntry{}, false // not listed; the stream still works
		}
		return brick.PresenceEntry{ID: u.ID, Data: map[string]string{"name": u.DisplayName}}, true
	},
}})
```

Each open page is one connection on the list; entries with the same `ID` are one viewer, with `Conns` counting the tabs. A join or a leave refreshes every listening viewer. Without `Identify` an entry is an anonymous random id with no data, so nothing about a viewer is shown unless you put it there. `.Presence` answers only for topics the component itself declares, sorted by `ID` and capped at 100 entries (`Total` counts all), and `AuthorizeTopic` is asked about presence topics too. An entry leaves when its stream closes and lapses after `PresenceOptions.TTL` (90 s) if its server dies. One server keeps the list in memory; with any other `Broadcaster` set `PresenceOptions.Store` to `pushredis.NewPresenceStore(rdb, pushredis.Options{Prefix: "app1:"})`, or the component fails to render with an error that names it, so a per-process list never passes for the whole site. An entry's `Data` is fixed at join.

**Several servers.** `contrib/push-redis` carries topics between processes over Redis pub/sub (`PushOptions.Broadcaster`). The servers must share `PushOptions.Sealer`, or a token rendered by one is refused by another. Limits: `MaxConnections` (1 000 streams per process, then 503), `MaxTopics` (64 per stream), `Heartbeat` (25 s). The `brick` CLI turns on in-memory push for every site.

## Background jobs — slow work that reports back

**The problem:** an action runs inside its request, under the script's step and time limits. A monthly report or a slow third-party call either keeps the button spinning for a minute or is killed halfway.

**The fix:** the action starts a job and answers at once; the job runs after the response, writes its result through your own data layer, and publishes a topic when it ends, so every component listening to that topic ([server push](guide.md#server-push--live-updates-from-elsewhere)) refreshes and reads the result through its own render.

`components/Report.html`

```html
---
state:
  job: ""
props: {month: {type: string}}
brick: {listen: reports}
---
<brick:script>
def start(state, props):
    id = brick.start_job("report", {"month": props["month"]}, topic = "reports")
    db.exec("insert into reports (id, status) values (?, 'running')", id)
    return dict(state, job = id)
</brick:script>
<div>{{ with report .State.job }}{{ .status }}{{ else }}<button brick:click="start">Build report</button>{{ end }}</div>
```

`main.go`

```go
jobs, err := brick.NewJobs(brick.JobsOptions{
    Push: push,
    Funcs: map[string]brick.JobFunc{
        "report": func(ctx context.Context, job brick.Job) error {
            return store.FinishReport(ctx, job.ID, job.Args["month"]) // slow work, your data layer
        },
    },
    OnError: func(ctx context.Context, job brick.Job, err error) {
        store.FailReport(ctx, job.ID, err) // runs before the topic is published
    },
})
opts.Push, opts.Jobs = push, jobs
defer jobs.Close(ctx)
```

```text
click "Build report" → POST /_brick/action … ← at once: "running"
the job runs         → store.FinishReport(…) after the response
the job ends         → publish "reports" → every listening component refreshes → "done"
```

`report` in the template stands for your own read of the row (a function in `Options.Funcs`, or a loader); the job's result reaches the page only that way.

**Rules.** Only server code starts a job: `brick.start_job(name, args = None, topic = None)` in an action (Go: `Effects.StartJob`), or `Engine.StartJob` from any Go code. The name must be one of `JobsOptions.Funcs`, a closed set of Go functions, so a script never chooses the code that runs. Args are JSON values up to 16 KiB (`MaxJobArgsBytes`): ids, not records. A job starts only after the action succeeded and its state was saved. It never sees the component's state, the session or the request, and runs under its own context, which ends at `JobsOptions.Timeout` (10 minutes by default) or when `Jobs.Close` cancels it.

**Failures.** An error, a panic, the timeout, a refused queue, a job still waiting at `Close` and a failed publish all reach `JobsOptions.OnError` (default `slog.Error`) before the topic is published, so the host can record the failure where the listening components will read it. A refused queue also reaches the engine's own `OnError` hooks; the action itself has already answered and is not failed.

**Cancel a job (v21).** Keep the id `brick.start_job` returned in state and call `brick.cancel_job(state["job"])` from another action (Go: `Effects.CancelJob`). It applies only if that action succeeds, before any job the same action starts, so one action can cancel an old export and start a new one. An action may cancel only a job an action of the same component started in the same session; anything else is refused with `ErrJobNotYours`, reported to the `OnError` hooks, and keeps running. A job that already ended is ignored. Go code (`eng.CancelJob(ctx, id)`, `Jobs.Cancel`) can cancel any job. A waiting job never runs, and a running job's context ends with `brick.ErrJobCancelled` as its cause; either way it ends through `OnError` with `ErrJobCancelled` and its topic is published, so record it there and let the page refresh. A real queue cancels only when it implements `brick.JobCanceller`; otherwise the cancel returns `ErrJobCancelUnsupported`.

**The queue.** By default jobs run in an in-process pool: `Workers` (4) at once and `QueueSize` (256) waiting, and a full pool refuses with `ErrJobQueueFull` rather than stall the action. A real queue (Redis, Hatchet, SQS) plugs in through `JobsOptions.Queue`, one method, `Enqueue(ctx, job)`; its worker calls `jobs.Run(ctx, job)`, which looks the name up again, reports a failure and publishes the topic.

## Events and effects — components talking to each other

**The problem:** the Contacts page shows "64 contacts" in its top bar and a table below. The user deletes a contact: the table re-renders, but the top bar still says 64, because an action re-renders only its own component. And the action has no way to say "show the message *Deleted Ada*" or "now go to `/contacts/42`" except by keeping extra fields in state.

**The fix:** an action may record **effects** besides returning state. `brick.dispatch` sends an event to every other component on the page; a component lists the events it listens to under `brick: {on:}`, and each one runs an ordinary action there.

`apps/contacts/contacts.star`

```python
def remove(state, props, id):
    found = db.query(Q_ONE, id)
    if not found:
        brick.flash("That contact was already deleted.", "warning")
        return state
    db.exec(X_DELETE, id)
    brick.dispatch("contact-deleted", {"ids": [id]})
    brick.flash("Deleted " + found[0]["name"] + ".", "success")
    return dict(state, selected = [])
```

`apps/contacts/ContactCount.html`

```html
---
use:
  contacts: ~/apps/contacts/contacts.star
state: {}
brick:
  on:
    contact-added: $refresh
    contact-deleted: $refresh
---
<span class="ct-total">{{ contacts.total }} contacts</span>
```

```text
POST /_brick/action  ContactsApp  contacts.remove(42)
← html: the table without Ada
  effects: [dispatch contact-deleted {"ids":[42]}, flash success "Deleted Ada Lovelace."]
runtime: morph ContactsApp → the Toaster shows "Deleted Ada Lovelace."
         → ContactCount listens to contact-deleted → runs $refresh
POST /_brick/action  ContactCount  $refresh
← html: <span class="ct-total">63 contacts</span>
```

Neither component names the other: they share only the event name. Every effect, from Starlark:

| Call | What the browser does |
|---|---|
| `brick.dispatch(name, data = None, to = None)` | Emits `name` on `brick.on`, then runs each listener's action. `to = "ContactCount"` reaches only that component; `to = "self"` only the sender. |
| `brick.flash(message, level = "info")` | Shows a message. `level` is `neutral`, `info`, `success`, `warning` or `danger`. A kit `<Toaster>` on the page shows it as a toast; without one the runtime shows a small polite message. It survives a following `navigate` or `redirect`. |
| `brick.clear_flash(level = None)` | Dismisses the flashes on screen, every one or one level (v21). The kit `<Toaster>` dismisses its toasts; a flash recorded later in the same action still shows. `brick.effect("flash-clear", fn)` replaces how it is done. |
| `brick.redirect(url)` | A full page load of `url`. |
| `brick.navigate(url)` | Goes to `url` through navigation without a reload (a full load when navigation is off). |
| `brick.title(text)` | Sets `document.title`. |
| `brick.download(url, filename = None)` | Downloads the file your app serves at `url`. |
| `brick.url(url, replace = False)` | Changes the address bar without a request. |
| `brick.rows(ref, op = "insert", component = …, items = [], key = "id", at = "bottom", limit = 0, keys = [])` | Inserts, updates, deletes or resets the rows of the `brick:ref` list, each row a render of `component`. |
| `brick.region(ref, op = "replace")` | Replaces, appends to or prepends to the `brick:ref` + `brick:ignore` element with the same element of this action's render (v22). |
| `brick.transition(types = "")` | Runs this answer's morph in a view transition with those types, or skips it with `"none"` (v22). |
| `brick.submit(form)` | Natively submits the `brick:ref` form after the morph, leaving the page. |
| `brick.publish(topic)` | Nothing here: once the action succeeded and saved, every component listening to `topic`, on every open page, refreshes ([Server push](#server-push--live-updates-from-elsewhere), v17). |
| `brick.start_job(name, args = None, topic = None)` | Nothing yet: it returns the job's id, the job runs after the response, and its `topic` refreshes the listeners when it ends ([Background jobs](#background-jobs--slow-work-that-reports-back), v18). |
| `brick.cancel_job(id)` | Nothing here: once the action succeeded, the job is cancelled if this component started it in this session (v21). |

- **A listener is an ordinary action** of the listening component, with its own handle and token, through the same middleware: an event never grants anything a click could not. It gets the event's data as its last argument: `contact-saved: reload` runs `def reload(state, props, event)`. `$refresh` re-renders with no change. Listener names are checked when the file is read, like `brick:click`.
- **Dynamic names:** a key may be a template, `"contact-updated.{{ .Props.id }}": reload`, so each row hears only its own record's updates.
- **One child only:** `<ContactRow brick:on:deleted="row_deleted" />` runs `row_deleted` on the component that wrote the tag, only when *that* child dispatches `deleted`. The writing file must be stateful.
- **Without a request:** `brick:click='$dispatch("contact-selected", {"id": 42})'` dispatches from the browser; client code calls `brick.dispatch(el, "contact-selected", {id: 42})`. `brick:click="$parent.save"` runs `save` on the stateful component around this one (prefer events: they keep the child reusable).
- **Client code listens too:** `brick.on("contact-deleted", (d) => …)` in a `<brick:client>` block. A server that wants browser code to run dispatches an event; there is no way to send JavaScript.
- **Same page only.** An event never reaches another tab, another user, or a listener that was not on the page when it was sent. Events carry ids, not records: effects for one action are capped at 32 KiB (`Options.MaxEffectBytes`).
- **Safe by default:** effects are recorded only while an action runs (calling `brick.flash` from a loader or a template function is an error), an action that fails sends none, the sender does not hear its own event, and a chain of events stops after 4 hops, at a repeated name, or past 50 listeners. Redirect, navigate, download and url accept a path or your own host; add others with `Options.RedirectHosts`.
- **Your own effects:** Go middleware adds them with `brick.EffectsFrom(ctx).Add(brick.Effect{Kind: "x-confetti"})` and reads them in `res.Effects`; the browser handles a kind with `brick.effect("x-confetti", fn)`. `brick.effect("flash", fn)` replaces how flashes are shown.
- **Testing:** `bricktest.Live(t, eng, "content/apps/contacts.html")` fills a page and holds its components; `Call` runs an action, `Deliver` runs the listeners the way the browser would, and `AssertDispatched`, `AssertFlash`, `AssertRedirect` check the effects.

**Row streams.** A chat log or activity feed should not keep every row in the component's state. `brick.rows("log", component="Msg", items=[msg], at="top", limit=50)` renders `<Msg>` once per item, with the item as its props, and sends the rows to the element with `brick:ref="log"`. Give that element `brick:ignore` so later renders leave its rows alone. Rows are keyed by the item's `id` (choose another field with `key=`). A key already in the list is updated where it stands, and `limit` trims the list from the end the rows were not added to. `op="delete", keys=[…]` removes rows, and `op="reset"` empties the list before inserting. There is no `html=` argument: a row is always a component's render, so a user's text arrives escaped. Keep the items themselves in `temporary:` state so they leave the instance after the action. In Go: `brick.EffectsFrom(ctx).Rows(ctx, brick.RowsSpec{Ref: "log", Component: "Msg", Items: items, At: brick.RowsTop, Limit: 50})`.

**Regions (v22).** A "load more" list written inline in the template, where each answer brings only the next page and the earlier pages must stay: a normal re-render would morph the list back to the latest page. Mark the list `brick:ref="feed" brick:ignore` and call `brick.region("feed", "append")` in the action (`prepend` for older items; `replace`, the default, refreshes a region only when asked). The content is that element in the action's own render, escaped like any render; ordinary re-renders leave the region alone. Unlike `brick.rows` there is no row component and no keys; unlike `brick:lazy` it is driven by actions. A region after `brick.skip_render` is refused, and with scripts off a region is just part of the page. In Go: `Effects.Region(ref, brick.RegionAppend)`. Example: `examples/ui/demos/RegionFeed.html`.

**A child bound to a parent field — Modelable (v22).** A stepper or picker owns its own UI, but the parent needs its value. In the child, `brick: {model: value}` names one top-level state field; in a stateful parent, `<Stepper brick:model="qty">` binds a field of its own. The parent's value is written into the child whenever the parent renders it (before and after the child's `init`), and when one of the child's own actions changes it, the runtime sends the parent a `$set` of `qty`, so a total that reads `qty` updates. Each direction has one owner per round trip, so a change stops after one trip each way. On a presentational component `brick:model` is still an attribute placed with `{{ attrs }}`; modifiers on a component binding are an error; the parent's field must be client-writable. Example: `examples/ui/demos/V22Cart.html` with `V22Stepper.html`.

**Handing off to a classic POST.** Some steps must be a real form post, such as a login that sets a cookie or a payment provider's endpoint. `brick.submit("login")` asks the browser to submit the form with `brick:ref="login"` natively once the morph is in, bypassing brick's own submit handling and navigation. It leaves the page, so it runs after every other effect, a later redirect replaces it, and flashes are carried to the next page.

**Recovering from a crashed action.** When an action fails, `action-error` now says which component failed and how: `{action, message, root, id, status}`. `brick.reloadComponent(el)` fetches the page again and morphs that component in fresh, replaying nothing, so a component whose state broke the server starts over:

```js
brick.on("action-error", (d) => { if (d.status >= 500) brick.reloadComponent(d.root); });
```

The listener chooses which statuses to recover from. A failed script action answers 422 (`brick.ActionStatus` maps script failures to 422), so a 500 comes from your own `ActionMiddleware` or the server around it; recover from whatever your host sends. The showcase's `/runtime-extras/` page does this with a middleware that turns one component's failures into 500s.

It waits for actions already queued on the component, resolves when the fresh render is in, and rejects (without firing `action-error`) when the page cannot be fetched.

**Debugging the runtime.** `brick.debug(true)` in the console logs every action request and every answer the runtime lands. `brick.latency(800)` delays each request by 800 ms, so you can watch loading states and races on a fast local server. Both start off and are not in the `brick-config` meta, because they are for your own browser, not the page.

**Testing a stateful component without scraping HTML.** `bricktest.Live` fills a page and gives you each stateful component as a `LiveRoot`. `Click` and `Submit` act on it the way the browser runtime does: Click finds the `brick:click` on or above the element a selector matches, and Submit sends a `brick:submit` form's named fields as FormData would and its `brick:model` fields as deferred sets. `Change`, `Keydown` and `Intersect` drive the other event attributes the same way: `Change("select#plan", "pro")` runs the nearest `brick:change` with the control's new value (a checkbox takes a bool), `Keydown("input#q", "Enter")` runs the innermost `brick:keydown` whose key matches, `live.Keydown("Escape")` presses a key for the `.window` listeners, and `Intersect("#more")` / `Leave("#more")` report an element entering or leaving the viewport (a `.leave` one fires only after it was seen). Since v14, `Input("#q", "ada")` runs the nearest `brick:input` with the value, `Focus("#name")` / `Blur("#name", "Ada")` run the element's own attribute only, `Keyup` and `live.Keyup` mirror the keydown drivers, combos are written `Keydown("#t", "ctrl+Enter")` (an unknown modifier such as `cmd+k` fails the test), and `ClickOutside("#menu")` runs that element's `.outside` unless it is hidden (a real click elsewhere would also run every other shown `.outside`, so drive those separately). Modifiers come from the runtime's own table: `.once` runs once per element (the driver returns nil after that), and timing and geometry modifiers let each call through. Then assert on the state itself: `AssertSet` compares a dotted path on its JSON form (so `3` matches `int64(3)`), and `AssertErrors` / `AssertNoErrors` check the rule errors of the fields the user has touched, the same `.Errors` the next render shows.

`signup_test.go`

```go
func TestSignup(t *testing.T) {
	eng := bricktest.Engine(t, brick.Options{Scripting: sc, States: brick.NewMemoryStates(0, 0)}, bricktest.Theme(
		"components/Signup.html", signupHTML,
		"content/signup.html", `<Signup />`,
	))
	live := bricktest.Live(t, eng, "content/signup.html")
	form := live.Component("Signup")

	form.Submit("form#signup", map[string]any{"name": "A", "email": "nope"})
	form.AssertErrors(t, "name", "email")

	form.Submit("form#signup", map[string]any{"name": "Ada", "email": "ada@example.com"})
	form.AssertNoErrors(t)
	form.AssertSet(t, "name", "Ada")

	form.Click("button.reset")
	form.AssertSet(t, "name", "")
	if t.Failed() {
		t.Log(live.Dump()) // the page as it stands, to open in a browser
	}
}
```

Nested and renderless components (v20): `live.Component("Board").Children()` lists the stateful components directly inside a root and `.Child("Badge")` finds one at any depth (failing the test when there is none), with `ID()` and `Name()` to tell them apart. `bricktest.AssertRenderSkipped(t, res)` / `AssertRendered` check `brick.skip_render`, `AssertEffect(t, res, brick.Effect{Kind: "x-confetti"})` matches a queued effect on the fields the test sets, and `AssertNoEffect` / `AssertEffects` (exact kinds, in order) cover the rest; `brick:js.*` client commands never reach the server, so the effects are everything an action sends the client.

More drivers (v21): `form.SubmitWith("#edit", "#delete", values)` submits as if that button was pressed, posting its `name=value` with the fields exactly as the browser does (a button that cannot submit fails the test). `live.AssertViewHas(t, "Card", "currency", "EUR")` checks a value reached the component's template even when the markup never prints it (a class switch, a loader key), on its latest render; `live.Views("Card")` lists every render, oldest first, and outside `bricktest` `brick.WithViewLog(&log)` records them for `Render`, `Fill` or `RunAction`. `bricktest.Live(t, eng, page, brick.WithLazyInline())` renders `brick:lazy` islands in place, so the test sees and drives them without a second request; without it the page holds the placeholders the browser first gets. `live.WindowFocus()` / `WindowBlur()` fire `.window` focus and blur attributes. `ClickWith("#chart", bricktest.ClickDetails{Shift: true, X: 10, Y: 20})` (v22) clicks with keys and a position for a `brick:click.details` attribute; `Click` sends a plain left click.

Errors an action returns itself (`return [state, errors]`) are shown by that action's render and never stored, so assert them on `form.HTML()`. Outside tests, `Engine.Instance(ctx, handle, brick.WithSession(id))` gives a host's own tools the same read-only view. It is a Go API only, never served over HTTP: it checks the session as an action does and writes nothing.

## State that survives a restart — `BRICK_SECRET`

**The problem:** a user fills in a form, you deploy, the server restarts, and their next click finds no state: the server forgot it. Two servers behind a load balancer have the same problem on every request that lands on the other one.

**The fix:** seal the state into the page. Give the server a key; each stateful component's state is encrypted into its handle, so any process with the same key opens it.

```sh
$ brick keygen
Gx3v…base64…Q=
$ BRICK_SECRET=Gx3v…Q= brick serve
```

`your app`

```go
keys, err := brick.KeysFromEnv(os.LookupEnv, "BRICK_SECRET") // or a KMS-backed brick.KeyRing
s, err := site.New(ctx, site.Options{FS: fsys, StateKeys: keys})
```

- **The key is a secret.** `brick keygen` prints one key on stdout and nothing else, so it pipes straight into a secret manager (`brick keygen | gopass insert -f app/brick-secret`). Never put it in code or `brick.yaml`.
- **Keys in a KMS.** Instead of the plain key, store data keys encrypted by AWS KMS, Vault transit or Google Cloud KMS, and build the ring with `contrib/keys-awskms`, `keys-vault` or `keys-gcpkms`: `keys, err := keysawskms.New(ctx, kms.NewFromConfig(cfg), keysawskms.Options{KeyID: "alias/brick-state", Wrapped: wrapped})`. The KMS decrypts each key once at start, so requests never wait on it; a failure at start is an error, never a server without a key. Rotation is the same `new,old` order over the wrapped keys.
- **Rotation:** `BRICK_SECRET=new,old`. The first key seals, every key opens. Ship the new key second, then promote it to first in a later deploy, then drop the old one a TTL (12 hours) later.
- **Encrypted, not just signed.** View-source shows an opaque `default.s1.…` blob. A changed byte, a blob from another component, or one from another session does not open; the component then recovers in place under the current code and session.
- **Without a key:** `brick serve` makes a key for the process and warns that a restart invalidates state (it recovers in place). A Go host that sets neither `StateKeys` nor `Engine.States` keeps state in memory and logs a warning on the first stateful response.
- **Export needs the key.** `brick build` (or `site.Export`) refuses a page with a stateful component when there is no shared key, because the files would carry handles no server can open. With the key, exported pages hold sealed initial state with no expiry; the first action re-seals it with a TTL. Point the pages at an action server that has the same key.
- **Responses that hold state are private.** The site sends `Cache-Control: private, no-cache` and `Vary: Brick-Recover` on them (`site.Options.StateCacheControl` changes the header); the cached page itself never holds a handle.
- **Signed-in users.** Set `Engine.Sessions` (`brick.SessionFunc(func(r) (string, error))`) to answer with the app's session id; a blob from another session does not open, so rotating the id on sign-out ends every live component. `site.Options.Request` reaches actions too, so an action sees the user signed in now.
- **Other stores.** Large state, or state that must be single-use, goes in a server store: set `Engine.States` (or a named store in `Engine.StateStores` picked with `brick: {store: name}`). A store that needs pruning (a SQL table) is pruned every `site.Options.PruneEvery`.
- **When an instance ends (v19).** A component that holds something outside brick (a draft upload, a reserved seat, a lock row) can release it when nobody will use the component again: put a `brick.InstanceEndHook` in `Options.Hooks`. `OnInstanceEnd(ctx, e brick.InstanceEnd)` gets `e.Store`, `e.Reason` (`brick.EndExpired`, `brick.EndEvicted`, `brick.EndRevoked`), `e.At` and `e.Instance`, a copy of the last saved instance (`Name`, `Path`, `ID`, `Handle`, hashed `Session`, `State`, `Pending` uploads), never a request. It runs on the goroutine that noticed the end (the prune timer, the `Put` that evicted, your `RevokeSession` call), after the store released its locks, and a panic is recovered and logged. An expiry is reported when the store notices it, so up to one `PruneEvery` after the deadline; a closed tab is not an end the server can see. What each store can see: `NewMemoryStates` reports expired, evicted and revoked; `contrib/states-sql` expired (at `Prune`, each row once across servers) and revoked; `contrib/states-redis` revoked only, since Redis deletes an expired key itself; `NewSealedStates` nothing, since the state lives in the page. A store says which it reports through the optional `brick.EndWatcher` (`brick.BytesEndWatcher` under `brick.Bytes`), and the engine logs the reasons it will never hear.

```go
type cleanup struct{ bucket *s3.Client }

func (c cleanup) OnInstanceEnd(ctx context.Context, e brick.InstanceEnd) {
	for _, p := range e.Instance.Pending {
		_ = deleteObject(ctx, c.bucket, p.ID) // presigned, never confirmed
	}
	slog.Info("instance ended", "component", e.Instance.Name, "id", e.Instance.ID, "reason", e.Reason)
}

eng, err := brick.New(brick.Options{States: brick.NewMemoryStates(0, 0), Hooks: []any{cleanup{bucket}}})
```

```text
level=INFO msg="instance ended" store=default reason=expired component=Counter id=b7ab6400e handle=63503d8a… state=map[n:0]
level=INFO msg="site: pruned state" removed=3
```

- **State is UI state.** A sealed blob can be replayed: an old copy opens to the old state. An action that changes something real (redeeming a coupon) re-checks the database.

**State in Redis or SQL.** For state that must survive restarts or be shared by several servers on a database you already run, wrap a byte store with `brick.Bytes`, which owns the TTL, the codec and optional encryption at rest. On Redis, `brick.Bytes(statesredis.New(rdb, statesredis.Options{Prefix: "app1:"}), brick.BytesOptions{TTL: 30 * time.Minute})`: Redis expires the state itself. On SQL, wrap `statessql.New(statessql.Options{DB: db, Dialect: statessql.Postgres()})` with `brick.Bytes`, call its `Migrate` once at start, and the site prunes expired rows every `PruneEvery`. Both end a session's live components on `Engine.RevokeSession`, and a store outage answers 503 (the page retries), never 410 (the page would recover every component).

**Retries and tuning the runtime.** A 503 or 429 means the action did not run, so the runtime sends it again after the answer's `Retry-After` (seconds or an HTTP date; 1 s without one), once by default, then fires `action-error`. The component keeps its place in the queue and shows `data-loading` while it waits, each retry fires `action-retry` and carries `Brick-Retry`, and a request hook sees `r.retry` and may `r.cancel()` it. A 410 is never retried (it recovers in place), and neither is a started stream or an upload's bytes. `Options.Runtime` sets the retries and the page-wide defaults from Go, written into the page as a `brick-config` meta:

```go
brick.Options{Runtime: brick.RuntimeConfig{
    Debounce:     400 * time.Millisecond, // brick:model with no .debounce
    PollMin:      500 * time.Millisecond, // floor for brick:poll, never below 250 ms
    Retries:      2,                      // brick.NoRetry turns retrying off
    RetryMaxWait: 10 * time.Second,       // a longer Retry-After fails at once
    ActionURL:    "https://api.example.com/_brick/action", // a separate action server
}}
```

With `ActionURL` set, actions, uploads and lazy islands go to that server with the session cookie, so it must answer CORS with credentials for the page's origin and allow the `Brick-*` headers.

**Rate-limiting actions.** `actionmw.RateLimit(limiter, actionmw.BySession)` in `ActionMiddleware` refuses an action over budget with 429 and `Retry-After`, which the runtime waits out and retries. `actionmw.MemoryLimiter(20, time.Minute)` is a token bucket per key in one process; behind a load balancer each server would have its own budget, so use `ratelimitredis.New(rdb, 20, time.Minute, ratelimitredis.Options{Prefix: "app1:"})` from `contrib/ratelimit-redis` for one shared budget. It is the same bucket, decided in one Lua script on Redis's clock, with keys that expire after one period. If Redis is unreachable it returns the error, never an allow, and `RateLimit` answers 503, so an outage never silently removes the limit.

**Action logging per component (v20).** `actionmw.Slog` logs every action at Info. `brick: {log: false}` keeps a chatty component (a typing indicator, a poll) out of the log, and `brick: {log: debug}` (or `info`, `warn`, `error`) moves its successful actions to that level. A failed or refused action is still logged at Warn whatever the component says, because a refusal is a security signal that one file must not be able to hide. The setting reaches every action middleware as `ActionCall.Log` (`*brick.ActionLog`, nil when unset), so a host's own logger or tracer can honour it too; any other value is a compile error listing the accepted words.

**Tracing actions.** To trace actions, add `otelactions.Middleware(tracerProvider, otelactions.WithMeterProvider(meterProvider))` from `contrib/actions-otel` to `ActionMiddleware`. Every action and upload then gets a span named `<Component>.<action>`, plus a duration histogram and a count by status. State, arguments, tokens and session ids never reach the telemetry.

## Islands — per-request parts of a cached page

**The problem:** the page is rendered once and cached, but the header must say "Hi, Sara" to Sara and "Sign in" to everyone else.

**The fix:** mark the component an island. It renders for every request; the rest of the page stays cached.

`components/Account.html`

```html
---
brick:
  island: true
---
{{ with .Ctx.user }}<a href="/account">Hi, {{ .Name }}</a>
{{ else }}<a href="/login">Sign in</a>{{ end }}
```

`your app`

```go
s, err := site.New(ctx, site.Options{FS: fsys,
    Request: func(r *http.Request) []brick.RenderOption {
        return []brick.RenderOption{brick.Provide("user", currentUser(r))}
    }})
```

- The cached page holds a hole where the island goes; each response fills it. `eng.Fill(ctx, result, brick.Provide(…))` does the same outside the site layer.
- An island takes **props only** — no children and no `<brick:prop>` blocks, because it renders after the page's template is gone.
- It still sees what a layout provided (`.Ctx.accent`); the request's values go on top.
- A failing island fails the response, unless a `<brick:boundary>` around it shows its fallback instead. A loader's answer (a redirect, an error status, not found) always passes through: see the next section.

**A whole page per visitor (v22).** A page whose body depends on who is looking (a dashboard, "my orders") sets `brick: {per_request: true}`. It skips the page cache and renders on every request with the values `site.Options.Request` provides, so `{{ .Ctx.user }}` works in the page itself and its loaders run each time. It is sent with `Cache-Control: private, no-cache` (`site.PerRequestCacheControl`) unless a loader sets its own; a loader's redirect, error status or not-found from it is sent `private, no-store`. `brick build` leaves it out with a warning and lists it (`ExportReport.Skipped`), because a static file has no visitor; `Route.PerRequest` marks it for a host with its own cache. Prefer islands when only a corner of the page is personal: the rest then stays cached.

## Protect a page, redirect, and answer with a status

**The problem:** a loader could only call `brick.not_found()`. It could not send a signed-out visitor to `/login/`, refuse a non-admin with 403, say a post was removed (410) or moved (308), or set `Cache-Control`.

**The attempt that fails:** put the guard in the page's own loader. The site caches a page's render and shares it between visitors, so the page loader never sees who is asking: it runs once, for nobody, and its answer would be served to everyone.

**The fix:** three loader calls, `brick.redirect(url, status=303)`, `brick.error(status, message="")` and `brick.header(name, value)`, answered from the place that matches who the answer is for.

- A **page or layout loader** answers what is the same for every visitor: a moved slug, a removed post, caching hints.
- An **island's loader** answers what depends on the visitor. Islands are filled on every response with your request values (`site.Options.Request`), so the guard sees the user, and the site sends its answer with `Cache-Control: private, no-store`.

`scripts/posts.star`

```python
POSTS = {"hello": "Hello world"}
MOVED = {"old-hello": "/posts/hello/"}
REMOVED = ["gone"]

def one(input):
    slug = input["page"]["route"]["slug"]
    if slug in MOVED:
        brick.redirect(MOVED[slug], status = 308)
    if slug in REMOVED:
        brick.error(410, "This post was removed.")
    if slug not in POSTS:
        brick.not_found()
    brick.header("Cache-Control", "public, max-age=300")
    return {"title": POSTS[slug]}

def all():
    return [{"slug": "hello"}, {"slug": "old-hello"}]
```

`content/posts/[slug].html`

```html
---
use: {posts: ~/scripts/posts.star}
load: posts.one
brick: {paths: posts.all}
---
<h1>{{ .Params.title }}</h1>
```

`scripts/auth.star`

```python
def guard(input):
    user = input["ctx"].get("user")
    if not user:
        brick.redirect("/login/")
    if user != "admin":
        brick.error(403, "Admins only.")
    return {"name": user}
```

`components/AdminPanel.html`

```html
---
brick: {island: true}
inject: {user: {default: ""}}
use: {auth: ~/scripts/auth.star}
load: auth.guard
---
<section>Welcome, {{ .Props.name }}. Secret numbers: 42.</section>
```

`content/admin.html`

```html
<h1>Admin</h1>
<AdminPanel />
```

`content/error.html`

```html
<h1>Error {{ .Ctx.error.status }}</h1>
<p>{{ .Ctx.error.message }}</p>
```

Your Go program supplies the user from its session, here a `user` cookie, through `site.Options.Request` returning `brick.Provide("user", c.Value)` (the full host is in the [cookbook](cookbook.md#protecting-a-page)). What the server answers:

```text
GET /posts/hello/                 200 OK, Cache-Control: public, max-age=300, <h1>Hello world</h1>
GET /posts/old-hello/             308 Permanent Redirect, Location: /posts/hello/
GET /posts/gone/                  410 Gone, <h1>Error 410</h1><p>This post was removed.</p>
GET /posts/nope/                  404 Not Found
GET /admin/  (no cookie)          303 See Other, Cache-Control: private, no-store, Location: /login/
GET /admin/  Cookie: user=bob     403 Forbidden, Cache-Control: private, no-store, <h1>Error 403</h1><p>Admins only.</p>
GET /admin/  Cookie: user=admin   200 OK, <h1>Admin</h1> … Secret numbers: 42.
GET /admin/  (no cookie, again)   303 See Other, Location: /login/ (the admin's page is never served from the cache)
```

Under `brick serve -base /docs`, the same redirects become `Location: /docs/login/` and `Location: /docs/posts/hello/`: a path target gets the base like every site URL.

- **Redirect targets.** A path (`/login/`) always works. An absolute URL works only on the request's own host or on a host listed in `brick.Options.RedirectHosts` (the same list action redirects use); anything else (`//evil.example`, `javascript:`, a backslash, a newline) fails, so loader data can never make an open redirect. A refused target is a logged 500, never followed.
- **Statuses.** A redirect is 302, 303, 307 or 308, 303 by default (the next request is a GET, right for "go and sign in"); 301 is refused because browsers cache it forever, and 308 is the permanent one. An error is any 4xx or 5xx, and `brick.error(404)` is the same as `brick.not_found()`.
- **Headers.** `brick.header` takes only `Cache-Control`, `Content-Language`, `Expires`, `Last-Modified`, `Link`, `Vary` and `X-Robots-Tag`. `Set-Cookie` is refused because a page's render is shared (sessions belong to your Go program, before the page renders), and so are hop-by-hop headers, `Content-Type`, `Content-Length` and `Location`. A page or layout loader's headers are cached with the page (`Result.Headers`) and sent on every response; a stateful page's own `Cache-Control` still wins.
- **The error page.** `content/error.html` (or `.md`) renders every `brick.error`, with `.Ctx.error.status` and `.Ctx.error.message`; like `404.html` it is not a page of the site. Without one, the status is sent as plain text (the message, else the status text). `Site.RenderError` renders it from Go.
- **Nothing contains an answer.** A `<brick:boundary>` never turns an answer into its fallback, and `Engine.Fill` returns an island's answer at once, because a fallback would send the page around it with status 200 to a visitor it meant to turn away. Answers are not faults: they are never reported to `OnError` hooks, and `brick check` does not report a page whose loader redirects or answers an error.
- **Actions.** When a component's loader redirects during an action's re-render (signed out since the page loaded), the action answers with that redirect as its one effect and no HTML; a loader's `brick.error` keeps its status on the action response, with the author's message only. `brick.redirect` inside an action function is unchanged (a client navigation), and `status=` is refused there.
- **Lazy islands** render after the page was sent, so their answer cannot change the response: it counts as a failed island and shows the boundary's fallback.
- **`brick build`** has no visitor. A page whose loader, or an island's loader, redirects is written as a meta-refresh page to the target (with `rel=canonical` and `noindex`) and nothing of the page itself, so protected content never reaches the export. An error status stops the build (`site: export /posts/gone/: its loader answered 410, which a static file cannot send`), and loader headers are dropped: the static host sets its own.
- **From Go.** The answers are typed errors beside `brick.ErrNotFound`: `*brick.Redirect` and `*brick.HTTPError`, made with `brick.NewRedirect` / `brick.NewHTTPError` and recognised with `brick.IsAnswer`, so a host with its own router uses `errors.As`. `Engine.RedirectLocation(r, rd)` applies the open-redirect lock, `brick.SetHeader(ctx, name, value)` serves a Go script backend, and `brick.WithResponseHeaders(ctx, h)` collects an island's headers on `Fill`.

## Translations

**The problem:** the same button needs to say "Add to cart" or "Ajouter au panier".

**The fix:** `t` looks up the text for the current language.

`i18n/en.yaml`

```yaml
cart:
  add: Add to cart
  items:
    one: "{count} item"
    other: "{count} items"
```

`any template`

```html
<button>{{ t "cart.add" }}</button>
<p>{{ t "cart.items" (dict "count" .Props.n) }}</p>
```

- The layout sets the language with `{{ provide "locale" .Page.Params.lang }}`.
- Plurals follow each language's real rules (`one`, `few`, `many`, `other`…).
- Fallback order: `pt-BR` → `pt` → the default language. A missing key shows as the key itself plus a warning, never as a blank.
- `dateFormat` and `numberFormat` follow the same language automatically: `{{ dateFormat "Monday 2 January" .Date }}` gives `dimanche 27 septembre` in French. Month and day names are built in for fr, de, es, it, pt and nl; other languages keep Go's English names.
- **One language needs its own file? Add a variant.** Put `about.fr.html` next to `about.html`, or `default.ar.html` next to `layouts/default.html` for a right-to-left layout. Render with `brick.WithLocale("fr")` and brick picks the French file when there is one, the shared file when there isn't. Works for pages, layouts and components.

## Markdown

**The problem:** long text is nicer to write as markdown.

**The fix:** an opt-in `<Markdown>` component.

```html
<Markdown>
## Why we built this

Pages are **files**. Drop one in and it renders.
</Markdown>

<Markdown src="~/posts/hello.md" />
```

- Available only when your app turns it on (`Options.Markdown`).
- **No components or `{{ }}` inside it.** They stay as plain text. That rule is what keeps markdown simple.
- Raw HTML inside markdown is dropped for safety; write HTML outside the block.
- Whole `.md` pages work in the site layer: a file under `content/` with the `.md` extension is a page with a URL like any `.html` page.

**Heading ids and highlighting (v14).** The goldmark adapter can give headings ids and highlight code. Both are off by default, so turning neither on changes nothing:

```go
md := markdowngoldmark.New(markdowngoldmark.WithHeadingIDs(), markdowngoldmark.WithHighlight())
eng, err := brick.New(brick.Options{FS: fsys, Markdown: md})
```

With the `brick` CLI, turn them on in `brick.yaml` instead: `markdown: {headingIDs: true, highlight: true}`. Both keys are off by default, so without the section `.md` output is unchanged.

For a `<Markdown src="~/posts/intro.md" />` whose file holds `# Intro`, then `## Intro`, then a fenced `go` block containing `x := 1`, you get:

`output`

```html
<h1 id="intro">Intro</h1>
<h2 id="intro-1">Intro</h2>
<pre class="chroma"><code><span class="line"><span class="cl"><span class="nx">x</span><span class="w"> </span><span class="o">:=</span><span class="w"> </span><span class="mi">1</span><span class="w">
</span></span></span></code></pre>
```

An id is the heading's plain text lowercased, with letters and digits kept and every other run turned into one `-`; a repeat gets `-1`, `-2`. Ids are unique within one `<Markdown>` block, not across a page. Highlighting writes classes, never `style=`, so your CSS and Content-Security-Policy decide the look: generate the stylesheet once with `markdowngoldmark.WriteHighlightCSS(f, "github")`. A fence with no language, or one chroma does not know, stays plain `<pre><code class="language-x">`. A markdown backend reports headings only if it implements the optional `brick.MarkdownHeadings` interface (`ConvertHeadings(src, w)`, same bytes as `Convert` plus the heading list); the goldmark adapter does, and a backend without it is never asked to fake an outline.

## Safety

**The problem:** user content (comments, bios) must never be able to inject scripts.

**The fix:** three layers.

`a user comment, shown safely`

```html
{{ sanitize .Params.comment }}
```

`you get, for a comment of <p>Hi <b>there</b></p><script>steal()</script>`

```html
<p>Hi <b>there</b></p>
```

- **Everything is escaped by Go.** `{{ .Props.title }}` is always safe: `html/template` escapes it correctly for wherever it lands (HTML, attribute, URL, JavaScript, CSS).
- **`{{ sanitize .bio }}`** is the only way to show user HTML. It keeps safe formatting (paragraphs, bold, lists, links, images) and strips scripts, event handlers, styles, iframes and `javascript:` links. There is deliberately no "trust this data" function.
- **Strict Content Security Policy works.** Pass `brick.WithNonce(n)` when rendering, and every script and style brick emits carries the nonce. Script files also carry integrity hashes.

- **Paths can't escape the project.** `use:` and `src=` resolve inside your app's `Options.FS`; `../../etc/passwd`, absolute paths and symlinks leading out are errors. `~/` means the project root, never your home folder.
- **Nonces are checked**, and `javascript:` links that Go blanks out as `#ZgotmplZ` are reported in dev mode.

> [!NOTE]
> **What is trusted**
>
> Every file — pages, layouts, components and `.star` scripts — is trusted like Go code. Data is never trusted: what your app passes in, frontmatter values, `.Site`, data files and loader results. User content reaches a page as data, never as the page itself. A theme's scripts are sandboxed, but its templates can print any file in the project folder — so keep secrets out of it.

## For your Go app

**The problem:** your app needs to load site data, choose what the theme may do, and render pages.

**The fix:** everything is explicit, in Go.

```go
fsys, _ := os.OpenRoot("theme")                     // the project folder; nothing outside it is reachable
site, err := brick.LoadSite(fsys.FS(), "site.yaml", "data/")
msgs, err := brick.LoadMessages(fsys.FS(), "i18n/")

sc, err := scriptstarlark.New(scriptstarlark.Options{DB: readOnlyDB})   // scripts may query; nothing else

eng, err := brick.New(brick.Options{
    FS:            fsys.FS(),
    Site:          site,
    Messages:      msgs,
    DefaultLocale: "en",
    Mode:          brick.Dev,                // default is brick.Prod
    Scripting:     sc,
    Hooks:         []any{errorReporter{}},   // your own type; brick ships no hooks
})

html, err := eng.Render(ctx, "content/about.html", props, brick.WithNonce(n))

// errorReporter is an ErrorHook: it hears every error, even one a boundary contained
// (a loader's redirect or error status is an answer, not an error, and never reaches it).
type errorReporter struct{}

func (errorReporter) OnError(ctx context.Context, err error) { slog.ErrorContext(ctx, "brick", "err", err) }
```

- **A page is a path.** brick reads it from `FS`, so every `~/` path has something to resolve against. Bytes from a database go in with `brick.WithSource(src)`, still under a path.
- **What you pass to `Render` is the page's `.Props`**, just like a tag's attributes are a component's `.Props`. Pass them on to components explicitly (`:plan=".Props.plan"`), or read `.Page.Props` from deep inside.
- **Safe to share.** One engine serves many requests at once. It never re-reads a file it has compiled; after an edit, call `eng.Reload(ctx)` and swap in the new engine.

- **`RenderChunks`** takes the same arguments and returns a `Result`: the output pieces, which components were used, cache tags, contained errors and warnings, and render stats. **Cache a `Result`, never the bytes** — `Result.Bytes(brick.WithNonce(n))` gives each response a fresh nonce.
- **`Result.Stats`** (`brick.RenderStats`) counts what the render did: components (in total and per name, `Stats.Top(5)`), islands and lazy islands, loader and `load_many` calls. They are counts, never durations, so a cached `Result` stays deterministic; time the render yourself. `eng.Fill(ctx, r, brick.WithStats(&s))` adds each island's components and loader calls to a copy `s` of `r.Stats`. The Dev toolbar shows these numbers.
- **Same input, same bytes**, so golden-file tests work. `eng.RenderComponent(ctx, "Card", props, children)` renders one component alone for a test.
- **`ctx`** lets a cancelled request stop a slow loader's database query.
- **Hooks** extend brick without forking it: an error reporter like the one above, or an `AfterRender` hook that edits static chunks (the demo site's `generator` adds a meta tag). A hook implements any of `BeforeRender`, `AfterRender`, `OnCompile`, `OnError` or `OnInstanceEnd` (v19, see *When an instance ends*). A value that implements none is an error from `New`, so a misspelled method can't silently never run.
- **Check a whole theme at once:**

```go
report, err := eng.CompileAll(ctx, "content")
for _, d := range report.Diagnostics {
    fmt.Println(d)   // content/pricing.html:12:5: error: unknown component <Crad>
}
```

Use it in tests or a pre-commit check — or run `brick check` (next section).

## A whole site — URLs, serving, building

**The problem:** a folder of pages should be a website: every page at a URL, a blog index that lists its posts, a dev server that reloads as you type, and static files to deploy.

**The fix:** the `site` package and the `brick` command, on top of the engine.

`the files → their URLs`

```text
content/index.html        →  /
content/about.html        →  /about/
content/blog/index.html   →  /blog/
content/blog/hello.md     →  /blog/hello/
content/legal.html        →  /terms/   (brick: {url: /terms/})
content/404.html          →  served for unknown URLs
static/logo.svg           →  /logo.svg
```

`content/blog/index.html — list the posts`

```html
<ul>
{{ range .Site.Pages }}{{ if eq .Section "blog" }}
  <li><a href="{{ .URL }}">{{ .Title }}</a> · {{ dateFormat "2 Jan 2006" .Date }}</li>
{{ end }}{{ end }}
</ul>
```

`the command`

```text
brick check              every problem at once; exit 1 on errors (-json for tools)
brick routes             every URL and the file behind it
brick serve              http://localhost:8080 with drafts, live reload and /_brick/play/Card?title=Hi
brick new site blog      a working project using the kit: brick.yaml, a layout and a home page
brick build -o public    the static site, ready for any host (-json for tools)
brick fmt -w             lay every .html file out one way, without changing what it renders
brick docs               every component with its props and slots (-json for tools)
brick schema -o f.json   the JSON Schema of frontmatter, for editors
brick lsp                a language server for editors (editors/vscode is a VS Code client)
brick version            the module version and VCS revision this binary was built from (-json for tools)
```

- **`.Site.Pages`** lists every page, newest first: `.URL`, `.Title`, `.Date`, `.Section`, `.Params`. Pages with `draft: true` appear only in `brick serve`.
- **Routes for a host (v20).** `Site.Routes()` and `Site.Patterns()` (and `brick routes -json`) return each page's `Title`, `Description`, `Date`, `Draft`, `Section` and `Meta` (its frontmatter without `brick:`, what templates read as `.Params`), plus `Dynamic` for a `[param]` pattern route, so a menu is `for _, r := range s.Routes() { if m, ok := r.Meta["menu"].(map[string]any); ok { … } }`. Route names and menu entries are ordinary frontmatter keys (`name: blog.hello`); the engine adds none of its own. Both return copies, so a caller cannot change the index.
- **`.md` files are pages too**, in the same layouts. Components and `{{ }}` inside markdown stay plain text.
- **Per-language URLs:** `brick serve -locales en,fr` serves `/about/` and `/fr/about/`; the French one uses `about.fr.html` when it exists.
- **Linking a page's languages.** `.Page.Translations` is the same page in the other locales, each with `.Locale` and `.URL`, in the order `Locales` lists them; `.Page.URL` is the URL being rendered. One layout gives every page its `hreflang` alternates:

`layouts/default.html`

```html
<html lang="{{ .Page.Locale }}">
<head>
  <link rel="alternate" hreflang="{{ .Page.Locale }}" href="{{ .Page.URL }}">
  {{ range .Page.Translations }}<link rel="alternate" hreflang="{{ .Locale }}" href="{{ .URL }}">{{ end }}
</head>
<body>{{ children }}</body>
</html>
```

With `brick serve -locales en,fr,de`, `content/about.html`, `content/team.fr.html` (with `brick: {url: /equipe/}`), `content/team.de.html` and `content/solo.fr.html`:

```text
/fr/about/   → fr /fr/about/, en /about/, de /de/about/
/de/team/    → de /de/team/, fr /fr/equipe/        (variant-only files of one stem pair up; each keeps its own URL)
/fr/solo/    → fr /fr/solo/                        (no other language: .Page.Translations is empty)
/fr/posts/hello/ (content/posts/[slug].html) → fr /fr/posts/hello/, en /posts/hello/
```

  A language switcher is the same range with `<a href="{{ .URL }}">{{ .Locale }}</a>`. Under a `BasePath` the URLs are rebased like every other root URL (`/b/fr/about/`). A dynamic page's other-language URL carries the same values; it is not checked against that locale's `paths:` list. A Go host with its own router passes `brick.WithURL(u)` and `brick.WithTranslations([]brick.Translation{{Locale: "fr", URL: "/fr/about/"}})` to `Render`.
- **A section can require fields:** `content/blog/_collection.yaml` with `fields: {title: {type: string, required: true}}` makes `brick check` report any post without a title.
- **Editors.** `brick schema -o .vscode/brick-frontmatter.schema.json` writes a JSON Schema for frontmatter (`brick.FrontmatterSchema()` from Go), generated from the engine's own key lists so it cannot drift from what `brick check` accepts. The Red Hat YAML extension applies it to YAML files through `yaml.schemas` or a `# yaml-language-server: $schema=…` modeline. For `.html` files, `brick lsp` is a language server: it publishes `brick check`'s diagnostics on each save, completes component tags (`<Card>`, `<x-card>`), a component's props and `oneof` values and frontmatter keys (from the same schema, `brick:` settings included), shows a component's or prop's docs on hover, and jumps from a tag to its component's file. Find All References on a component tag, or anywhere in its file, lists every call of it across the site in both tag forms and through `use:` aliases, and on a prop lists its declaration, its uses inside the component and every place it is passed (`title=` and `:title=`), from any of those positions; Rename on a prop (v19: F2 on its `props:` key, a `.Props.title` use, a `title=` at a call, or a `<brick:prop name>` block) renames the declaration, every use in the component and its locale variants (`.Props.x`, `$.Props.x`, `.Attrs.x`, `index .Props "x"`, `attrs "x"`, `node "x"`, `hasNode "x"`, `props["x"]` in its script) and every call site, and refuses the whole rename, naming the sites, on a clash or where a site cannot be followed (`brick:spread`, `.Props` handed on whole, `load:`, a computed `props[k]`); uses it cannot tell are this prop are left alone and listed in a warning; Rename on a component tag renames every call, the `use:` paths naming its file and the file itself (with `Card.fr.html` variants) in one edit, and refuses a kit component, a taken name or an editor that cannot rename files. Both read files with brick's own scanner, so a `<Card>` inside a `{{ }}` string, a comment or a raw body is not a call. It also formats a document exactly as `brick fmt` does (Format Document, format-on-save), as one whole-document edit. `editors/vscode` is a VS Code client with one setting, `brick.path`; it also highlights `{{ }}` actions, component tags and `brick:*` attributes in `.html` files, with or without the server, and (v20) highlights the frontmatter block as YAML and checks it as you type against `brick schema` (unknown `brick:` keys, wrong types, values outside an enum, YAML syntax errors). The schema checks one file's shape; cross-file rules (a `load:` function that exists, a `rules:` path in `state:`) stay with `brick check`, which is what the language server runs.
- **Two pages on one URL is an error** naming both — never a silent winner.
- **Fast by construction:** each page is rendered once and cached; editing a file re-renders only the pages that read it.
- From Go: `s, err := site.New(ctx, site.Options{FS: fsys, Engine: opts})`, then `s.Handler()`, `s.Export(ctx, "public")` or `s.Watch(ctx, …)`.
- **Live reload without losing your place.** When a file changes, the dev server tells each open page. With navigation on, the page fetches itself again and morphs the new HTML in, the way a link click does, so you stay scrolled where you were and what you typed stays in its field. When the new page needs code the old one never ran (a stylesheet or script whose content hash changed, or a changed inline `<style>` in `<head>`), or when the render failed and the error page answers 500, it reloads fully instead. Without navigation it always reloads fully. Code can call `brick.refresh()` itself, and listen for the `refreshed` event.
- **Seeing what is alive.** In Dev, open `/_brick/instances` (under the base path, if the site has one) to see every stateful component instance the state stores hold: which component, on which page, how old, roughly how many bytes, how many actions it has taken, and the first eight digits of its session hash. It is a glance for the developer, never an export: state values, handles and session ids are not shown. A sealed store keeps nothing on the server, so with `StateKeys` the page answers 404; the memory store and any store that implements `brick.Lister` (such as a listing byte store) show up. With Dev off the path does not exist.
- **Dev toolbar.** Run the site with `Dev: true` and every page shows a small "brick" pill in the bottom-left corner. Open it to see what this request did: the render time, whether the page came from the site cache, how many components rendered (the five most used link to their playground), how many islands and lazy islands it holds, and how many loader and load_many calls ran, with a link to the instance inspector. A host that serves pages itself reads the same counts from `Result.Stats`, and passes `brick.WithStats(&stats)` to `Fill` to include island renders. Production pages never contain it. Since v20 a "Lazy islands" section lists each lazy island as it lands, with its component (linked to the playground), render time, components and loader calls: in Dev the island answer carries its counts in a `Server-Timing: brick-island;…` entry (`brick.ParseIslandTiming` reads it), which the toolbar takes from the browser's resource timing. Browsers expose it only on a secure origin; `localhost` is one, but a Dev server opened over plain http on a LAN address shows only how many are still waiting.
- **Formatting.** `brick fmt` lays every brick file out one way, so a team never argues about indentation. Run `brick fmt -d` to see what would change and `brick fmt -w` to apply it; `brick fmt -l` lists unformatted files for CI. It only lengthens or shortens whitespace that HTML already collapses, so a page renders the same before and after, and it leaves alone anything where whitespace matters: `{{ }}` actions, `<pre>`, `<textarea>`, `<script>`, `<style>`, raw component bodies and comments. When a file's tags cannot be followed with certainty (a conditional that opens a tag in one branch only), its indentation is left as written and `brick fmt -v` says which line. To keep a region exactly as you wrote it, put it between `{{/* brick:no-format */}}` and `{{/* brick:format */}}`. Options (v20), all off by default, in brick.yaml: `fmt: {lineLength: 100, attrOrder: ["brick:*", id, class], formatters: {"brick:style": "prettier --stdin-filepath x.css"}}`. `lineLength` (`-line-length`) wraps a markup line longer than N characters, one attribute per line with `>` on the last; only whitespace inside tags changes. `attrOrder` (`-attr-order`) puts the named attributes first, the one option that does more than whitespace (it changes order, never values), and leaves a tag alone when it spans lines, holds `{{ }}` between attributes or repeats one. `formatters` hands `<brick:client>`, `<brick:style>` and `<brick:script>` bodies to a command, run without a shell in the site's folder with a 30 s limit, and re-indents the result under the tag; a body holding `{{` is skipped, a failing command leaves the file untouched, and the command must be idempotent itself (prettier is). `inline` (v21, `-inline a,em,Icon`) names elements and component tags whose content keeps the parent's indentation, so prose wrapped in `<a>` or `<em>` is not pushed a level deeper (HTML names without case, component tags exactly; none by default). The language server's formatting reads the same section.
- **Checking a site whose Go program grants capabilities.** When your own Go program supplies script globals (`now`, `db`, a `sanitize_html`) or an upload sink, `brick check` and `brick lsp` do not have them, so every script naming one fails with `undefined: now`. List them in `brick.yaml` as `host: {globals: [db, now], uploads: true}`. The CLI then puts a stand-in in each place: the site compiles and everything else is checked, a page that actually calls a stand-in while the check renders it becomes a `host-only` warning (`not rendered by this check: lib/clock.star:4:21: now …`), and a name you did not list is still an error, so a typo is caught. Your own program ignores the section and renders with the real values.
- **Tailwind:** add `tailwind: {input: styles/app.css, output: static/app.css}` to `brick.yaml`. `brick build` and `brick serve` run the standalone `tailwindcss` binary — only when a class in your files (or the input CSS) actually changed, so text edits reload instantly.
- **Hosting under a path:** `brick build -base /docs` (or `site.Options{BasePath: "/docs"}`) serves or exports the site under `/docs/`, for example a GitHub Pages project site. The site stays written as if it were at the root. brick adds the base to every root-absolute `href`, `src`, `action`, `srcset`, `poster` and `formaction` in the HTML it serves or exports, and to redirects, assets and actions. Text and code samples are left alone.
- **Several sites on one server:** give each site its own `BasePath`, then `site.ListenAndServeAll(ctx, ":8080", docs, shop)`, or `site.Mount(docs, shop)` for your own `net/http` server. Each site keeps its own directory, engine, cache and live reload. A link from one site to another is a normal page load. List it in `KeepPaths: []string{"/shop/"}` so the rewrite leaves it alone. The repository's two examples run this way with `task demo:both`: the kit showcase at `/ui/` and the engine demo at `/engine/`, with no edits to either site.

## Feeds and robots.txt — output pages

**The problem:** a site needs an RSS feed and a `robots.txt`, but every page URL ends in a slash and gets the layout.

**The fix:** since v14, name the file with the output extension before `.html`. It is still a brick page: frontmatter, `load:`, `.Site.Pages` and components all work.

`content/feed.xml.html`

```html
---
title: Feed
---
<?xml version="1.0" encoding="utf-8"?>
<rss version="2.0">
<channel>
<title>{{ .Site.title }}</title>
<link>{{ .Site.baseURL }}/</link>
{{ range .Site.Pages }}{{ if eq .Section "blog" }}
<item><title>{{ .Title }}</title><link>{{ $.Site.baseURL }}{{ .URL }}</link><description>{{ .Description }}</description></item>
{{ end }}{{ end }}
</channel>
</rss>
```

`content/robots.txt.html`

```html
User-agent: *
Disallow: /private/
Sitemap: {{ .Site.baseURL }}/sitemap.xml
```

`site.yaml`

```yaml
title: Demo Blog
baseURL: https://example.com
```

`content/blog/hello.html`

```html
---
title: Hello & welcome
date: 2026-10-01
description: The first post
---
<p>Hi</p>
```

`output: curl -i localhost:8080/feed.xml`

```text
HTTP/1.1 200 OK
Content-Type: application/xml; charset=utf-8

<?xml version="1.0" encoding="utf-8"?>
<rss version="2.0">
<channel>
<title>Demo Blog</title>
<link>https://example.com/</link>

<item><title>Hello &amp; welcome</title><link>https://example.com/blog/hello/</link><description>The first post</description></item>

</channel>
</rss>
```

`output: curl -i localhost:8080/robots.txt`

```text
HTTP/1.1 200 OK
Content-Type: text/plain; charset=utf-8

User-agent: *
Disallow: /private/
Sitemap: https://example.com/sitemap.xml
```

`brick build` writes `public/feed.xml` and `public/robots.txt`, with no `index.html` folder.

- **Allowed:** `.xml`, `.rss`, `.atom`, `.txt`, served as `application/xml`, `application/rss+xml`, `application/atom+xml` and `text/plain` (all `; charset=utf-8`). html/template's escaping (`&lt; &amp; &#34; &#39;`) is valid XML and harmless in a text file; it is still HTML escaping, so a `&` in a `robots.txt` value is written `&amp;`.
- **Refused with an error:** `.json`, `.js`, `.mjs`, `.css` and `.csv` (html/template escapes them wrongly, so a quote becomes `&#34;` and JSON breaks; serve JSON from a Go handler), a markdown output page (`feed.xml.md`), and a `[param]` in an output page's file name (`[tag].xml.html`). Any other second extension (`v1.2.html`) is an ordinary page, as before.
- **No layout** unless the frontmatter names one (`layout: feed`), and nothing HTML is added: no Dev toolbar, live reload, `<base>`, URL rewrite or speculation rules. Every leading `<?…?>` prolog is written as is.
- `brick: {url: /feeds/main.rss}` moves it; the override keeps the format's extension. With `-locales en,fr` it is written once per locale like any shared page (`/feed.xml`, `/fr/feed.xml`), and `feed.xml.fr.html` is its French variant.
- Output pages are not in `.Site.Pages`, so a feed that ranges over it never lists itself, and they are not in the sitemap. `Route.Output` and `Page.Output` name the format (`brick routes -json` shows it). Under `-base /docs` nothing in an output page is rewritten, so feeds use absolute URLs from `.Site.baseURL`.
- **One per value (v22).** Put the parameter in a directory: `content/tags/[tag]/feed.xml.html` with `brick: {paths: tags.list}` answers `/tags/go/feed.xml`, `/tags/web/feed.xml`, and `brick build` writes `tags/go/feed.xml`. The page reads `{{ .Page.Route.tag }}`; a value not on the list is a 404 without running the page, and `/tags/go/feed.xml/` is a 404 too (a file URL only matches an output page). `brick routes` lists it as `/tags/[tag]/feed.xml`.

## The sitemap

**The problem:** search engines want `/sitemap.xml` with absolute URLs and language alternates, and writing it by hand means re-listing every route, translation and dynamic page.

**The fix:** since v14, turn it on in `brick.yaml` with `sitemap: {}`. The base URL comes from `site.yaml`'s `baseURL`, or from `sitemap: {baseURL: https://example.com}`; without an absolute http(s) base URL the site refuses to start, naming both places to set it.

`brick.yaml`

```yaml
sitemap: {}
```

`content/private.html`

```html
---
title: Private
brick: {sitemap: false}
---
<p>no index</p>
```

With the files of the previous section plus `content/about.html`, `content/about.fr.html` and a draft, `brick build -locales en,fr` writes `public/sitemap.xml` (excerpt):

`output: public/sitemap.xml`

```xml
<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9" xmlns:xhtml="http://www.w3.org/1999/xhtml">
  <url>
    <loc>https://example.com/about/</loc>
    <xhtml:link rel="alternate" hreflang="en" href="https://example.com/about/"/>
    <xhtml:link rel="alternate" hreflang="fr" href="https://example.com/fr/about/"/>
  </url>
  <url>
    <loc>https://example.com/blog/hello/</loc>
    <lastmod>2026-10-01</lastmod>
    <xhtml:link rel="alternate" hreflang="en" href="https://example.com/blog/hello/"/>
    <xhtml:link rel="alternate" hreflang="fr" href="https://example.com/fr/blog/hello/"/>
  </url>
  …
</urlset>
```

- **Listed:** every static route, and every URL a dynamic page's `brick: {paths:}` function returns.
- **Left out:** drafts (even in `brick serve`, which shows them), output pages, the 404 page, dynamic pages without `paths:`, and pages with `brick: {sitemap: false}`.
- `<lastmod>` is the `lastmod:` frontmatter date, else `date:`, else none. `hreflang` lists the page's translations plus itself, in `-locales` order. Under `-base /docs` locations become `https://example.com/docs/…` (not added twice when `baseURL` already ends in `/docs`).
- Your own `content/sitemap.xml.html` or `static/sitemap.xml` wins; the built-in one steps aside (logged at info level). Output is sorted by URL, so the same site gives the same bytes.
- `brick build` prints `wrote public/sitemap.xml`, and `-json` has `"sitemap": true`. Go hosts pass `site.Options{Sitemap: &site.Sitemap{BaseURL: "https://example.com"}}`; `Export` reports `ExportReport.Sitemap`.

## A table of contents for markdown pages

**The problem:** a docs layout wants a table of contents of the page's headings, but the markdown is converted to HTML before the layout runs.

**The fix:** since v14, `.Page.Headings` lists them: `Level` (1 to 6), `ID` (the element's id, or empty) and `Text` (plain text).

`layouts/doc.html`

```html
<!doctype html><html><head><title>{{ .Title }}</title></head><body>
<nav class="toc">{{ range .Page.Headings }}{{ if .ID }}<a class="h{{ .Level }}" href="#{{ .ID }}">{{ .Text }}</a>{{ else }}<span class="h{{ .Level }}">{{ .Text }}</span>{{ end }}
{{ end }}</nav>
<main>{{ children }}</main></body></html>
```

`content/guide.md`

```markdown
---
title: Guide
layout: doc
---
# Getting started

Intro.

## Install `brick`

Run it.

## Configure

Done.
```

`brick.yaml`

```yaml
markdown: {headingIDs: true}
```

`output: brick build → public/guide/index.html`

```html
<!doctype html><html><head><title>Guide</title></head><body>
<nav class="toc"><a class="h1" href="#getting-started">Getting started</a>
<a class="h2" href="#install-brick">Install brick</a>
<a class="h2" href="#configure">Configure</a>
</nav>
<main><h1 id="getting-started">Getting started</h1>
<p>Intro.</p>
<h2 id="install-brick">Install <code>brick</code></h2>
<p>Run it.</p>
<h2 id="configure">Configure</h2>
<p>Done.</p>
</main></body></html>
```

Without `markdown: {headingIDs: true}` the same build writes no ids, every `.ID` is empty, and the layout's `{{ else }}` branch prints `<span class="h1">Getting started</span>` and so on: the table of contents lists the headings but cannot link.

- The list comes from the Markdown backend through the optional `brick.MarkdownHeadings` interface ([Markdown](#markdown)); a backend without it gives an empty list (`{{ len .Page.Headings }}` is 0), never a guess. Only `.md` pages have headings; an `.html` page's list is empty.
- `ID` is empty unless the backend writes ids: `brick.yaml` `markdown: {headingIDs: true}` for the CLI, `markdowngoldmark.WithHeadingIDs()` for a Go host. Ids are a slug of the text, deduplicated as `intro`, `intro-1`.
- A Go host with its own router passes `brick.WithHeadings(hs)` to `Render`.

## Images

**The problem:** a 4000-pixel photo sent to a phone wastes seconds, and an `<img>` without a size makes the page jump as it loads.

**The fix:** `image` makes resized copies and gives you everything the tag needs.

`any template`

```html
{{ with image "~/static/team.jpg" }}
<img src="{{ .URL }}" srcset="{{ .SrcSet }}" sizes="(max-width: 800px) 100vw, 800px"
     width="{{ .Width }}" height="{{ .Height }}" alt="Our team" loading="lazy">
{{ end }}
```

- Copies are 480, 960 and 1440 pixels wide by default (never wider than the original); `image "~/static/p.jpg" 320 640` picks others.
- JPEG and PNG, resized in pure Go and served from `/_brick/img/…` with a content hash in the name, so browsers cache them forever. `brick build` writes them out.
- **The path may come from data** — `image .Params.cover` — but only files inside `static/` (`Options.ImageDirs`) that really are images can be read, and huge images are refused before decoding.

**Other formats (v16).** Name a format after the widths. JPEG and PNG are built in (a PNG can be turned into a JPEG with `"jpeg"`); WebP comes from `contrib/image-webp`, which the `brick` CLI registers for you.

`any template`

```html
{{ with image "~/static/diagram.png" 480 960 "webp" }}
<img src="{{ .URL }}" srcset="{{ .SrcSet }}" width="{{ .Width }}" height="{{ .Height }}" alt="How it fits together">
{{ end }}
```

`output (diagram.png is 1200×675)`

```html
<img src="/_brick/img/diagram.3f9c2a1b-960.webp" srcset="/_brick/img/diagram.3f9c2a1b-480.webp 480w, /_brick/img/diagram.3f9c2a1b-960.webp 960w" width="960" height="540" alt="How it fits together">
```

- The WebP is **lossless**: every pixel survives exactly, transparency included, and there is no quality setting. That suits screenshots, diagrams and illustrations; for photographs JPEG is usually smaller, so leave those as they are.
- A format with no encoder fails the render: `image "~/static/diagram.png": no encoder for format "avif" (have jpeg, png, webp); register one in Options.ImageEncoders, for example contrib/image-webp`.
- A Go host registers encoders itself: `brick.Options{ImageEncoders: []brick.ImageEncoder{imagewebp.New()}}`. Any type with `Format() string` and `Encode(w io.Writer, img image.Image) error` is an encoder; its output must be deterministic, because file names carry the source's hash. The `8` hex digits in a copy's name are the start of the source file's SHA-256.

## Component libraries — use a UI kit by name

**The problem:** you want the kit's `Hero`, `Card` and `Dialog` on every page. Copying the kit's folder means never getting its fixes, and importing each component with `use:` in every page is a line per component per page.

**The fix:** list the library once in `brick.yaml`. Every page can then use its components by name, with no imports.

`brick.yaml`

```yaml
libraries:
  - brick-kit                                    # a library the host offers by name (the brick CLI offers the kit)
  - github.com/acme/charts@v1.4.0                # a library in a repository, fetched by brick vendor
  - {from: github.com/acme/ui@v2.1.0, prefix: Acme, only: [DatePicker]}
  - ~/ui                                         # a folder inside this project
```

`any page — no use:, no copies, no theme line`

```html
<Hero title="Build pages, not pipelines" />
<AcmeDatePicker name="due" />
```

- **A library is a folder with a `brick-library.yaml` manifest.** Its components register by file name, exactly like your own `components/`: `components/Hero.html` is `<Hero>` and `<x-hero>`.
- **Four places a library can come from:** a name the host offers (`brick-kit`; a Go program passes its own with `cfg.Apply(&opts, acmeui.Library())`), a repository at a version (run `brick vendor`, as for single components), a folder of your project (`~/ui`), or — for one component — `use:` as before.
- **Your project always wins.** A tag is looked up in `use:` first, then your own component folders, then the libraries in the order you listed them. A library can never replace one of your components; brick logs every name it hid, naming both files.
- **Two libraries with the same name:** the earlier one wins. Keep both with `prefix:` — `prefix: Acme` makes `DatePicker.html` `<AcmeDatePicker>` (and `<x-acme-date-picker>`). `only: [DatePicker]` takes just those components, named as the library names them.
- **Inside a library, `~/` means the library's own root** (the folder with `brick-library.yaml`). Its components import each other as `~/components/Icon.html` and read `~/data/icons.yaml`, wherever the library sits, and a library can never read your project's files.
- **No theme line:** a manifest's `global: [Theme]` links that component's CSS and JavaScript on every page that uses the library. The layout needs only `{{ assets }}`. Writing `<Theme />` still works; assets are never sent twice.
- A missing folder, a missing manifest, an unvendored remote library or an unknown name in `only` is an error naming the library, never an empty library.

## Shipping your own library

**The problem:** your team has a set of components every site should share, and copying them into each project means every fix lands N times.

**The fix:** make the folder a library, then publish it the way people will use it: a repository tag, a Go package, or both.

`acme-ui/`

```text
brick-library.yaml
components/Button.html
components/DatePicker.html
data/icons.yaml          (optional; read as ~/data/icons.yaml)
library.go               (optional; for Go programs)
```

`acme-ui/brick-library.yaml`

```yaml
name: acme-ui
description: Acme's UI components
components: components   # the folder of components (this is the default)
global: [Theme]          # components whose CSS/JS every page using the library gets
```

`acme-ui/library.go — embed the folder for Go users`

```go
package acmeui

import (
	"embed"
	"io/fs"

	"github.com/ubgo/brick"
)

//go:embed brick-library.yaml components data
var files embed.FS

// Library is the Acme UI library, ready for brick.Options.Libraries.
func Library() brick.Library {
	return brick.Library{Name: "acme-ui", FS: files}
}
```

- **Publish by tagging the repository.** Users write `github.com/acme/acme-ui@v1.0.0` (or `github.com/acme/mono/ui@v1.0.0` for a folder) in `brick.yaml` and run `brick vendor`, which fetches the whole folder into `brick_modules/` and pins every file in `brick.lock`.
- **GitHub is listed out of the box** (through its git-trees API, since vendoring a folder needs its file list). For another host or a mirror, a Go program passes its own lister in `remote.Options.List`.
- `brick vendor -check` verifies libraries offline like single imports, and also reports a library that is not vendored, a file in a vendored library that is missing from `brick.lock`, and lock entries nothing uses any more.
- **For Go programs, embed it.** `brick.Options{Libraries: []brick.Library{acmeui.Library()}}` uses it with nothing to vendor, and `go.sum` pins it. An embedded library is mounted read-only at `brick_libs/<name>/`.
- **While you work on the library,** give the same `brick.Library` a folder on disk instead of the embedded files (`lib.FS = root.FS()` from `os.OpenRoot`). The site's watcher follows any library whose files can change (everything but an `embed.FS`), so an edit reloads the page with no restart. The kit's showcase runs this way under `task ui:serve`.
- **Write imports from the library's root:** `use: Icon: ~/components/Icon.html`. That one spelling works copied, vendored, embedded or nested.
- A library is code, like every file: its scripts run in the same sandbox with the capabilities the host granted, and nothing more.
- The brick kit is published both ways: `github.com/ubgo/brick/kit@main`, and the Go package `github.com/ubgo/brick/kit` with `kit.Library()`.

## Using someone else's components

**The problem:** you need one component from someone's kit, not the whole library.

**The fix:** import it by a versioned path, then vendor it once. For a whole kit, list it under `libraries:` instead (above).

`frontmatter`

```yaml
---
use:
  Card: github.com/acme/brick-kit/components/Card.html@v1.2.0
---
```

`the command`

```text
brick vendor           fetch into brick_modules/ and pin each file's hash in brick.lock
brick vendor -check    CI: fail unless brick_modules/ matches brick.lock (no network)
```

- Rendering never goes to the network: the page reads the vendored copy. Commit `brick_modules/` and `brick.lock`.
- Vendoring one component also fetches its library's `brick-library.yaml`, so its `~/` imports resolve to the library's root and its sibling components come along.
- `use:` names exactly the components you import; it adds no other names. To make a whole kit available by name, use `libraries:`.

## What comes later

v1 is the renderer and v2 the site layer (above). The roadmap shows the interactive phases: islands, navigation, actions, bindings. v1 already reserves what those need (`brick:` attributes, stable component ids, output in pieces), so none of them will be a rewrite.
