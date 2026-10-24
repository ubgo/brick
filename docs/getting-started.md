# Getting started

This page takes you from nothing to a running site with a page, a component and a button that changes something on the server. Every file shown is copied from a tested example in this repository, and every command was run to check its output. It takes about ten minutes. Afterwards, the [learning path](README.md) shows where to go next.

## Install

The `brick` command builds, checks, serves and formats sites. It needs Go 1.25.

```sh
go install github.com/ubgo/brick/cmd/brick@latest
brick version
```

To use the engine inside your own Go program instead, add the library (Go 1.24, one dependency: a YAML parser):

```sh
go get github.com/ubgo/brick
```

Adapters such as Starlark scripting, markdown or S3 are separate modules under [`contrib/`](../contrib) so their dependencies never enter yours; the `brick` command already includes the ones a site needs.

## Make a site

```sh
brick new site blog
```

```console
wrote blog/brick.yaml
wrote blog/site.yaml
wrote blog/layouts/default.html
wrote blog/content/index.html

Next:
  brick serve blog    http://localhost:8080
  brick check blog
```

`brick check blog` reports every problem at once and prints `ok: 1 pages` for a healthy site. `brick serve blog` starts the development server with live reload at http://localhost:8080. Leave it running: a saved file reloads the page.

The project has four files. `brick.yaml` lists `libraries: [brick-kit]`, which makes the 133-component UI kit available by name. `layouts/default.html` wraps every page and contains `{{ assets }}`, the line that links the components' CSS and the browser runtime. `content/index.html` is the home page, and a page's path is its URL.

## Your first page

A page is an `.html` file. The optional block between `---` lines is data about the page, read in the template as `.Params.<key>`, and `.Title` is a short name for `title`. Everything else is Go's `html/template`. Replace `content/index.html` with:

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

It already uses a component called `Card`. In the next step you write it.

## Your first component

A component is a file under `components/` whose name is the tag: `components/Card.html` is `<Card>` and also `<x-card>`. Its frontmatter declares typed props, so a wrong value is an error naming the file and line instead of a blank card.

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

Save it and the page shows two cards for the loop and a third written as `<x-card>`. The dev server also logs `<Card> from library brick-kit is shadowed by ~/components/Card.html`: the kit has a `Card` too, and your project's file wins over a library. That is brick's rule for every clash: one wins, and the engine tells you what it shadowed. Call yours `PostCard` if you want the kit's.

More on values reaching a component: [Components and props](guides/components/props.md), and [children, blocks and slots](guides/components/slots.md) for markup passed in.

## Your first action

A component that declares `state:` keeps that data on the server and changes it when a user clicks. You write the change as a small function, and `brick:click` names it. No API route and no JavaScript of your own.

Create `components/Counter.html`:

<!-- snippet: examples/production-sealed-state/theme/components/Counter.html -->
```html
---
state:
  count: 0
---
<brick:script>
def bump(state, props):
    return dict(state, count = state["count"] + 1)
</brick:script>
<div class="counter"><button brick:click="bump">Clicked {{ .State.count }} times</button></div>
```

Add `<Counter />` to a page, with the layout's `{{ assets }}` in place:

<!-- snippet: examples/production-sealed-state/theme/content/index.html -->
```html
<h1>Sealed counter</h1>
<Counter />
{{ assets }}
```

Click the button: the browser posts `bump` to `/_brick/action`, the server saves the new state, re-renders only this component, and the page patches it in place. Without JavaScript the rest of the page still works; a plain button does nothing, which is why forms use `brick:submit` ([forms without JavaScript](guides/forms/without-javascript.md)).

The state is sealed into the page with AES-256-GCM. When `BRICK_SECRET` is not set, the dev server warns that it made a key for this process, so a restart invalidates open pages (a component recovers in place). Keep state across restarts and servers with one key:

```sh
export BRICK_SECRET=$(brick keygen)
brick serve blog
```

Where state can live instead (memory, SQL, Redis) is a decision with consequences: see [choosing a state store](choosing/state-store.md) and the [sealed state guide](guides/production/sealed-state.md).

## Use the engine from Go

`brick serve` is the quickest way to a site. To render components inside your own handlers, build an engine over a file system and render a page. This is the repository's own `Example`, checked by `go test` against its output:

<!-- snippet: example_test.go lines=12-28 -->
```go
func Example() {
	fsys := fstest.MapFS{
		"components/Card.html": {Data: []byte(`<div class="card"><h3>{{ .Props.title }}</h3>{{ children }}</div>`)},
		"layouts/default.html": {Data: []byte(`<main>{{ children }}</main>`)},
		"content/about.html":   {Data: []byte("---\ntitle: About\n---\n<h1>{{ .Title }}</h1><Card title=\"Hi {{ .Props.user }}\"><p>body</p></Card>")},
	}
	eng, err := brick.New(brick.Options{FS: fsys})
	if err != nil {
		panic(err)
	}
	html, err := eng.Render(context.Background(), "content/about.html", map[string]any{"user": "Sara"})
	if err != nil {
		panic(err)
	}
	fmt.Println(string(html))
	// Output: <main><h1>About</h1><div class="card"><h3>Hi Sara</h3><p>body</p></div></main>
}
```

A page that uses a stateful component needs `Options.States` and the action endpoint mounted; the [host API guide](guides/production/host-api.md) and [examples/engineonly](../examples/engineonly/) show the full set.

## Where to go next

- The [learning path](README.md): twelve steps from here to a production setup, each with a guide and a runnable example.
- [Choosing guides](choosing/README.md), when you have a decision to make: state store, interactivity, forms, hosting.
- [Examples](../examples/README.md): every feature in a project with a test.
- The [feature index](reference/features.md): every feature with its guide and example.
- The long single-page [guide](guide.md) and the [cookbook](cookbook.md) of complete tasks.
