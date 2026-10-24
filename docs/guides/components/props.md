# Components and props

## What it is and when you want it

A page is an `.html` file, and a component is an `.html` file under `components/` whose file name becomes a tag: `components/Card.html` is `<Card>` (or `<x-card>`). The page passes values to it as attributes, which the component reads as `.Props`. Reach for a component the moment the same markup appears twice; reach for `props:` the moment you want brick to catch a wrong or missing value instead of rendering an empty box.

## Before you start

- You have a Go program that builds an engine with `brick.New` and renders a page with `Engine.Render`. The [example program](../../../examples/components-props/) is one; copy its `main.go` if you are starting from nothing.
- Templates are Go's `html/template`. brick adds a calling convention (tags, props, blocks), never a new template language, so `{{ range }}`, `{{ with }}` and `{{ if }}` work as they always do.
- Every path in a theme starts at the project root: `~/components/Card.html`, never `./Card.html`.

## Step 1 — Write a page with frontmatter

The optional YAML block between `---` lines is the page's own data, read as `.Params.<key>`; a few keys also have short names (`.Title`, `.Date`, `.Description`). The rest of the file is a normal Go template.

<!-- snippet: examples/components-props/theme/content/index.html -->
```html
---
title: Latest posts
posts:
  - {title: Hello, summary: The first post.}
  - {title: Again, summary: More words.}
---
<h1>{{ .Title }}</h1>
{{ range $i, $post := .Params.posts }}<Card :post :index="$i" tone="loud" />{{ end }}
<x-card :post="dict `title` `Spelled with x-` `summary` `Same file.`" />
```

`<Card :post …>` and `<x-card …>` are the same component. A tag that starts with a capital letter or with `x-` is a component; a lowercase tag is always plain HTML.

## Step 2 — Declare the component's props

`props:` lists what the component takes. Each prop is a type (`summary: string`) or a declaration with `type`, `default`, `required` and `oneof`. A `map` prop can also declare its `fields:`, and a `list` prop the shape of its items with `of:`.

<!-- snippet: examples/components-props/theme/components/Card.html -->
```html
---
props:
  post:
    type: map
    required: true
    fields:
      title: {type: string, required: true}
      summary: string
  index: {type: int, default: 0}
  tone: {type: string, default: plain, oneof: [plain, loud]}
---
<article class="card card--{{ .Props.tone }}">
  <h3>#{{ .Props.index }} {{ .Props.post.title }}</h3>
  <p>{{ .Props.post.summary }}</p>
</article>
```

What each form of attribute passes:

| You write | The component gets |
|---|---|
| `tone="loud"` | the text `loud` |
| `:index="$i"` | the value of the Go expression `$i` (a leading `:` means "evaluate") |
| `open` | `true` |
| `:post` (no value) | the variable `$post`, the same as `:post="$post"` |

The declared types convert what arrives: `:index="$i"` is an `int`, and `tone` falls back to `plain` when the call leaves it out, as the `<x-card>` call in step 1 does.

## Step 3 — Place attributes the component did not declare

Every attribute is a prop. The ones the component did not declare are collected in `.Attrs` and written out where the component puts `{{ attrs }}`, so `id`, `hx-post` or `data-*` on a component tag reach its HTML instead of vanishing.

<!-- snippet: examples/components-props/theme/components/Button.html -->
```html
---
props:
  block: bool
---
<button class="{{ cx `btn` (cond .Props.block `btn--block` ``) .Attrs.class }}" {{ attrs `class` }}>{{ children }}</button>
```

`cx` joins the class names that are set and skips empty ones; `{{ attrs "class" }}` writes every collected attribute except `class`, so the button never gets two `class` attributes. To hand a component's attributes on to an inner component, spread them:

<!-- snippet: examples/components-props/theme/components/Primary.html -->
```html
<Button class="primary" brick:spread=".Attrs">{{ children }}</Button>
```

<!-- snippet: examples/components-props/theme/content/attrs.html -->
```html
<Button class="wide" id="save" hx-post="/save">Save</Button>
<Primary id="buy" hx-post="/buy">Buy</Primary>
```

An attribute written on the tag wins over the same key in the spread map, and `key`, `children` and `src` never arrive through a spread.

## Step 4 — Add your own function and prop type from Go

When the Go program already has a formatter or a validator, hand it to the engine instead of rewriting it in a script. `Options.Funcs` adds template functions, `Options.PropTypes` adds type names a `props:` block can use.

<!-- ds:block id=money-k25fr6em title="examples/components-props/main.go" -->
**examples/components-props/main.go** · [`examples/components-props/main.go:37-39`](../../../examples/components-props/main.go#L37-L39)

```go
func money(cents int64) string {
	return fmt.Sprintf("$%d.%02d", cents/centsPerDollar, cents%centsPerDollar)
}
```
<!-- /ds:block hash=ae421a -->

<!-- ds:block id=email-kataxkeu title="examples/components-props/main.go" -->
**examples/components-props/main.go** · [`examples/components-props/main.go:45-51`](../../../examples/components-props/main.go#L45-L51)

```go
func email(v any) (any, error) {
	s, _ := v.(string) // a non-string becomes "", which ParseAddress rejects below
	if _, err := mail.ParseAddress(s); err != nil {
		return nil, fmt.Errorf("%q is not an email address", s)
	}
	return strings.ToLower(s), nil
}
```
<!-- /ds:block hash=534c48 -->

<!-- ds:block id=newengine-cmy9re2s title="examples/components-props/main.go" -->
**examples/components-props/main.go** · [`examples/components-props/main.go:56-66`](../../../examples/components-props/main.go#L56-L66)

```go
func newEngine(dir string) (*brick.Engine, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	return brick.New(brick.Options{
		FS:        root.FS(),
		Funcs:     template.FuncMap{"money": money},
		PropTypes: map[string]brick.PropType{"email": email},
	})
}
```
<!-- /ds:block hash=0e86a7 -->

<!-- snippet: examples/components-props/theme/components/Contact.html -->
```html
---
props:
  to: email
  price: int
---
<a href="mailto:{{ .Props.to }}">{{ .Props.to }}</a> <span>{{ money .Props.price }}</span>
```

<!-- snippet: examples/components-props/theme/content/contact.html -->
```html
<Contact to="Ann@Example.com" price="1999" />
```

`email` lowercases the address on the way in and rejects one that does not parse; `money` formats the price.

## Step 5 — Find components in more than one folder

`Options.Roots` is the list of folders whose files become tags; the default is `["components"]`. When two folders define the same name the earlier one wins and brick logs what it hid, so a project can sit on top of a shared folder and replace one piece.

<!-- ds:block id=newengine-ntjvm83k title="examples/components-options/main.go" -->
**examples/components-options/main.go** · [`examples/components-options/main.go:91-108`](../../../examples/components-options/main.go#L91-L108)

```go
func newEngine(dir string, mode brick.Mode, hooks ...any) (*brick.Engine, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	sc, err := scriptstarlark.New(scriptstarlark.Options{Steps: scriptSteps})
	if err != nil {
		return nil, err
	}
	return brick.New(brick.Options{
		FS:          root.FS(),
		Roots:       roots,
		Scripting:   sc,
		DataFormats: map[string]brick.DataFormat{".kv": parseKV},
		Mode:        mode,
		Hooks:       hooks,
	})
}
```
<!-- /ds:block hash=20d34a -->

`shared/Note.html` is `<Note>` on every page, with no import:

<!-- snippet: examples/components-options/theme/shared/Note.html -->
```html
<aside class="note">{{ children }}</aside>
```

## Step 6 — Choose how a file's tags are read

Three frontmatter keys change what a tag means in one file, and each has an `Options` twin for the whole site (`Options.Syntax`, `Options.Passthrough`, `Options.Unknown`, or `syntax:`, `passthrough:`, `unknown:` in `brick.yaml`).

- `syntax:` restricts the spelling to `capitalized` (`<Card>`), `hyphenated` (`<x-card>`) or `both` (the default).
- `passthrough:` lists tag names written out as plain HTML even though they look like components, for a web component such as `<my-element>`; a typo elsewhere is still an error.
- `unknown: passthrough` writes every unknown tag out as plain HTML instead of failing.

<!-- snippet: examples/components-options/theme/content/hyphen.html -->
```html
---
syntax: hyphenated
---
<x-heading level="3">Only x- here</x-heading>
```

<!-- snippet: examples/components-options/theme/content/hyphen-bad.html -->
```html
---
syntax: hyphenated
---
<Heading>Capitalized</Heading>
```

<!-- snippet: examples/components-options/theme/content/widget.html -->
```html
---
passthrough: [my-element]
---
<my-element size="2">kept as written</my-element>
```

<!-- snippet: examples/components-options/theme/content/typo-ok.html -->
```html
---
unknown: passthrough
---
<Crad>a typo</Crad>
```

The pages above print, in order (`<x-heading>` renders because the file allows only that spelling; `<Heading>` there is the error; the web component and the typo with `unknown: passthrough` come out as written):

<!-- snippet: examples/components-options/testdata/output.txt lines=12-19 -->
```text
== content/hyphen.html
<h3 class="title">Only x- here</h3>

== content/hyphen-bad.html
error: content/hyphen-bad.html:4:1: <Heading>: this file accepts only the hyphenated tag form

== content/widget.html
<my-element size="2">kept as written</my-element>
```

## Step 7 — Write an element whose name is data

A heading component takes its level as a prop, so the element name is data. `tag` writes a whole start tag from a name and attribute pairs and `endtag` the matching end tag; `tag` accepts only elements without URL, script or raw-text content (`h1`–`h6`, `p`, `div`, `span`, `section`, `li`, `td`, and so on) and no `on*`, URL or `style` attributes, so data can pick a heading level but never a `<script>`.

<!-- snippet: examples/components-options/theme/components/Heading.html -->
```html
---
props:
  level: {type: int, default: 2}
---
{{ $h := print "h" .Props.level }}{{ tag $h "class" "title" }}{{ children }}{{ endtag $h }}
```

`<Heading level="1">Options</Heading>` renders `<h1 class="title">Options</h1>` (line 2 of the program's output).

## Step 8 — Document a component in its own file

`doc:` and `examples:` on a prop, and on each declared slot, are never read while rendering. They are checked with the rest of the declarations and handed out by `Engine.ComponentDocs`, so the docs cannot drift from the contract. The `brick docs` command prints the same thing for a site.

<!-- snippet: examples/components-options/theme/components/Card.html -->
```html
---
props:
  title:
    type: string
    required: true
    doc: The card's heading.
    examples: [Welcome, Release notes]
  tone:
    type: string
    default: calm
    oneof: [calm, loud]
    doc: How strongly the card is drawn.
brick:
  slots:
    children: {doc: The card's body.}
    footer: {doc: Optional links under the body., examples: ["<a href=\"/more\">More</a>"]}
---
<section class="card card-{{ .Props.tone }}"><h3>{{ .Props.title }}</h3>{{ children }}{{ if hasNode "footer" }}<footer>{{ node "footer" }}</footer>{{ end }}</section>
```

`ComponentDocs` returns one `ComponentDoc` per tag, sorted by name, and fails with the file name when a component's frontmatter does not parse (nothing is silently left out). The program prints Card's entry as JSON:

<!-- snippet: examples/components-options/testdata/output.txt lines=32-72 -->
```text
== ComponentDocs("Card")
{
  "name": "Card",
  "path": "components/Card.html",
  "props": [
    {
      "name": "title",
      "type": "string",
      "required": true,
      "doc": "The card's heading.",
      "examples": [
        "Welcome",
        "Release notes"
      ]
    },
    {
      "name": "tone",
      "type": "string",
      "default": "calm",
      "hasDefault": true,
      "oneof": [
        "calm",
        "loud"
      ],
      "doc": "How strongly the card is drawn."
    }
  ],
  "slots": [
    {
      "name": "children",
      "doc": "The card's body."
    },
    {
      "name": "footer",
      "doc": "Optional links under the body.",
      "examples": [
        "\u003ca href=\"/more\"\u003eMore\u003c/a\u003e"
      ]
    }
  ]
}
```

## Step 9 — Refuse attributes a component did not declare

Adding `props:` never breaks an existing call, so a misspelled attribute (`tone` instead of `title`) normally renders an unstyled component and nothing says why. A component that wants the opposite sets `strictProps: true` in its own frontmatter: any attribute it does not declare becomes an error at the call. Only `key=` is exempt, so a component that takes `class` or `id` under `strictProps` must declare them in `props:`.

`components/Strict.html`

```html
---
strictProps: true
props:
  title: string
---
<p>{{ .Props.title }}</p>
```

`content/b.html`

```html
<Strict title="x" tone="loud" />
```

`brick check` names the call, not the component:

```console
$ brick check .
content/b.html:1:19: error: <Strict>: prop "tone" is not declared by <Strict>, which sets strictProps
```

The same switch exists for pages as `strictParams: true`, which makes a misspelled `.Params.titel` an error instead of an empty string. `examples/components-props` pins both messages in `TestShapeAndStrict`.

## Try it

```sh
cd examples/components-props
go run .
go test .
```

`go run .` prints every page; the test checks these exact bytes.

<!-- snippet: examples/components-props/testdata/output.txt -->
```text
== content/index.html
<h1>Latest posts</h1>
<article class="card card--loud">
  <h3>#0 Hello</h3>
  <p>The first post.</p>
</article><article class="card card--loud">
  <h3>#1 Again</h3>
  <p>More words.</p>
</article>
<article class="card card--plain">
  <h3>#0 Spelled with x-</h3>
  <p>Same file.</p>
</article>

== content/attrs.html
<button class="btn wide" id="save" hx-post="/save">Save</button>
<button class="btn primary" hx-post="/buy" id="buy">Buy</button>

== content/contact.html
<a href="mailto:ann@example.com">ann@example.com</a> <span>$19.99</span>

```

## Pitfalls

- **An `int` prop arrives as `int64`.** A host function written `func(cents int)` and called with `{{ money .Props.price }}` fails at render with `at <.Props.price>: wrong type for value; expected int; got int64`. Take `int64` (and `float64` for a `float` prop), as `money` does above.
- **`oneof` names the bad value.** `tone="lowd"` fails with `content/probe.html:1:32: <Card>: prop "tone": "lowd" is not one of plain, loud`, where an undeclared prop would have rendered an unstyled card.
- **`required` is checked at the call.** `<Card tone="loud" />` fails with `<Card>: <Card> requires the prop "post"`, naming the file and line of the call, not of the component.
- **`:post` with no value only ever means `$post`.** Outside a `{{ range $i, $post := … }}` it fails with `:post with no value passes $post, but no $post is declared here; declare it or write :post="…"`; it never guesses `.post`.
- **`open="false"` is the text "false", which is true.** brick warns: `open="false" on <Card> is the text "false", which is truthy; declare the prop as bool or write a bare attribute (C6)`. Declare the prop as `bool`, or write `open` / leave it off.
- **`fields:` is checked by `brick check`, never at render.** A misspelled field read is a warning, `.Props.author.emial: "emial" is not a field of .Props.author (fields: email, name)`, and so is a call that leaves out a required field. A value from a loader or from Go is never checked.
- **Undeclared props still pass through.** Adding `props:` never breaks an existing call. To refuse unknown attributes, set `strictProps: true`; then `<Strict title="x" tone="loud" />` fails with `<Strict>: prop "tone" is not declared by <Strict>, which sets strictProps`.
- **A host prop type's error is the prop error.** `<Contact to="nope" />` fails with `<Contact>: prop "to": "nope" is not an email address`. A theme that uses `money` or `email` now needs a host that registers them: under an engine without `Options.PropTypes` the same file fails to compile with `props: to: unknown type "email" (one of string, int, float, bool, list, map, any; the host program adds more with Options.PropTypes)`.
- **Strings inside an expression use backticks.** Write ``:post="dict `title` `x`"``. HTML has no `\"`, so `:post="dict "title" "x""` fails with `unexpected '"' in <Card> tag`.
- **Structured data belongs in frontmatter, not in an attribute.** Write the list in YAML (as `posts:` in step 1) and pass it with `:items=".Params.posts"`, rather than `data='{"a":1}'`.
- **A file in the wrong folder is not found.** With `Roots: ["components", "shared"]` a typo reads `unknown component <Crad> — no file in use: or the roots (components, shared) defines it`; the message lists the roots so a missing one is easy to spot.
- **`syntax:` is checked per file.** A file with `syntax: hyphenated` that writes `<Heading>` fails with `<Heading>: this file accepts only the hyphenated tag form`.
- **`tag` is not a way around escaping.** `{{ tag "script" }}` fails with `tag: "script" is not an element tag can write`; the program's test pins it.
- **`examples:` takes text, numbers and bools.** A list or a map inside `examples:` is an error from `ComponentDocs`, as is a `doc:` that is not text.

## Related

- [Children, blocks and slots](slots.md): passing markup instead of values.
- [Layouts and data](layouts-and-data.md): `.Params`, `.Site`, `.Ctx` and where each value comes from.
- [Scripts and loaders](loaders.md): props a component fetches for itself.
- [Frontmatter reference](../../reference/frontmatter.md): every `props:` setting, with its default.
- [Feature index](../../reference/features.md): the "Pages and components" rows.
- The single-page read: [Passing values — props](../../guide.md#passing-values--props) and [Attributes on a component](../../guide.md#attributes-on-a-component).
- Example: [examples/components-props](../../../examples/components-props/).
- Example: [examples/components-options](../../../examples/components-options/) for the settings added in the later steps.
