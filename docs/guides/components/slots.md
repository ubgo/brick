# Children, blocks and slots

## What it is and when you want it

Props carry values; this page is about passing **markup**. Whatever you write between a component's tags is its `children`, placed with `{{ children }}`. When a component needs more than one piece of markup (a header and a footer, a row template), the page writes named blocks with `<brick:prop name="…">` and the component reads them with `{{ node "…" }}`. A component can also read its child tags as data before drawing them (a tab bar needs its tabs' titles first), and a component that shows code takes its body untouched with `raw: true`.

## Before you start

- Read [Components and props](props.md) first: a block is a prop whose value is markup.
- The rule behind all of this: **an attribute holds text or a reference; anything with angle brackets, quotes or newlines goes in a block.** Follow it and you never escape anything.

## Step 1 — Place children and named blocks

`{{ node "header" }}` renders the block the caller named `header`; `{{ hasNode "footer" }}` asks whether one was passed. Everything outside a block is `children`.

<!-- snippet: examples/components-slots/theme/components/Card.html -->
```html
<div class="card">
  <header>{{ node "header" }}</header>
  <div class="body">{{ children }}</div>
  {{- if hasNode "footer" }}
  <footer>{{ node "footer" }}</footer>
  {{- end }}
</div>
```

A block renders only when the component reads it, so an unread block costs nothing.

## Step 2 — Let the page draw each row (a scoped slot)

A list component owns the loop, but each page wants its rows to look different. The component hands each item to the block with `{{ node "item" . }}`, and the block reads it as `.Slot`.

<!-- snippet: examples/components-slots/theme/components/PostList.html -->
```html
---
props:
  posts: {type: list, required: true}
---
<ul>
  {{- range .Props.posts }}
  <li>{{ node "item" . }}</li>
  {{- end }}
</ul>
```

This is a scoped slot: the component hands a value back to the caller's markup. Only one value can be passed; pass a map for several: `{{ node "item" (dict "post" . "index" $i) }}`, read as `.Slot.post`.

## Step 3 — Declare which blocks a component takes

Without a declaration a component accepts any block. `brick: {slots:}` names the blocks it takes, so a missing required one or a misspelled name fails at compile, at the caller's line. `children` names the content between the tags.

<!-- snippet: examples/components-slots/theme/components/Panel.html -->
```html
---
brick:
  slots:
    title: {required: true, doc: "The panel heading"}
    children: {required: true}
---
<section class="panel"><h2>{{ node "title" }}</h2>{{ children }}</section>
```

The page that uses all three:

<!-- snippet: examples/components-slots/theme/content/index.html -->
```html
---
posts:
  - {title: Hello, url: /hello/}
  - {title: Again, url: /again/}
---
<Card>
  <brick:prop name="header"><strong>Plans</strong></brick:prop>
  <brick:prop name="footer"><a href="/buy">Buy now</a></brick:prop>
  <p>This is the children.</p>
</Card>
<Card><brick:prop name="header">No footer</brick:prop><p>Only a body.</p></Card>
<PostList :posts=".Params.posts">
  <brick:prop name="item"><a href="{{ .Slot.url }}">{{ .Slot.title }}</a></brick:prop>
</PostList>
<Panel><brick:prop name="title">Settings</brick:prop><p>Panel body.</p></Panel>
```

## Step 4 — Read child tags as data

`.Children` lists the child component tags (`.Name`, `.Props`) **without** rendering them, and `{{ child $c }}` renders one of them. That is how a tab bar writes every tab's title but renders only the first panel.

<!-- snippet: examples/components-slots/theme/components/Tabs.html -->
```html
<div class="tabs">
  <div role="tablist">
    {{- range .Children }}
    <button role="tab">{{ .Props.title }}</button>
    {{- end }}
  </div>
  <div class="panel">{{ with .Children }}{{ child (index . 0) }}{{ end }}</div>
</div>
```

<!-- snippet: examples/components-slots/theme/content/tabs.html -->
```html
<Tabs>
  <Tab title="One">First panel.</Tab>
  <Tab title="Two">Second panel.</Tab>
</Tabs>
```

## Step 5 — Show code and other raw text

A component with `raw: true` receives everything between its tags untouched as `.Raw`: no component tags, no `{{ }}` actions. The output is escaped by `html/template`, so `<` and `"` arrive safely. Leading indentation is removed (`dedent: false` keeps it). `src=` reads the body from a file instead, so the code you show can be code that compiles.

<!-- snippet: examples/components-slots/theme/components/CodeBlock.html -->
```html
---
raw: true
props:
  lang: {type: string, default: text}
---
<pre><code class="language-{{ .Props.lang }}">{{ .Raw }}</code></pre>
```

<!-- snippet: examples/components-slots/theme/content/code.html -->
```html
<CodeBlock lang="go">
    if x < 10 && y > 2 {
        fmt.Println("{{ not a template }}")
    }
</CodeBlock>
<CodeBlock lang="go" src="~/snippets/hello.go" />
```

## Step 6 — Keep the indentation of a raw body

A raw body loses its common leading indentation by default, so code written indented inside a page does not arrive indented twice. `dedent: false` keeps the body exactly as written, for output where the spaces matter (`<pre>`, a diff, a poem).

<!-- snippet: examples/components-options/theme/components/Pre.html -->
```html
---
raw: true
dedent: false
---
<pre>{{ .Raw }}</pre>
```

<!-- snippet: examples/components-options/theme/components/Dedented.html -->
```html
---
raw: true
---
<pre>{{ .Raw }}</pre>
```

Called with the same indented body, the first keeps its two-space indent and the second drops the common one (lines 4–9 of the program's output):

<!-- snippet: examples/components-options/testdata/output.txt lines=4-9 -->
```text
<pre>
  keeps
    its indentation
</pre>
<pre>loses
  the common indent</pre>
```

## Try it

```sh
cd examples/components-slots
go run .
go test .
```

<!-- snippet: examples/components-slots/testdata/output.txt -->
```text
== content/index.html
<div class="card">
  <header><strong>Plans</strong></header>
  <div class="body">
  
  
  <p>This is the children.</p>
</div>
  <footer><a href="/buy">Buy now</a></footer>
</div>
<div class="card">
  <header>No footer</header>
  <div class="body"><p>Only a body.</p></div>
</div>
<ul>
  <li><a href="/hello/">Hello</a></li>
  <li><a href="/again/">Again</a></li>
</ul>
<section class="panel"><h2>Settings</h2><p>Panel body.</p></section>

== content/tabs.html
<div class="tabs">
  <div role="tablist">
    <button role="tab">One</button>
    <button role="tab">Two</button>
  </div>
  <div class="panel"><section>First panel.</section></div>
</div>

== content/code.html
<pre><code class="language-go">if x &lt; 10 &amp;&amp; y &gt; 2 {
    fmt.Println(&#34;{{ not a template }}&#34;)
}</code></pre>
<pre><code class="language-go">package main

func main() { println(&#34;hello&#34;) }
</code></pre>

```

## Pitfalls

- **Whitespace around a block stays in the children.** In the first card above, the two `<brick:prop>` lines leave their newlines and indentation behind in `{{ children }}` (the blank lines inside `<div class="body">`). Write the blocks on the same line as the children, as the second card does, when the bytes matter.
- **A declared slot is enforced at the caller.** `<Panel><p>x</p></Panel>` fails with `<Panel>: <Panel> requires a <brick:prop name="title"> block`, `<brick:prop name="aside">` fails with `<Panel> declares no such slot (declared blocks: title)`, and a Panel with only its title fails with `<Panel> requires children (content between its tags)`.
- **A block must sit directly inside its component, outside any `{{ if }}`.** `<Card>{{ if true }}<brick:prop name="header">H</brick:prop>{{ end }}</Card>` fails with `<brick:prop> inside a {{ }} block of <Card> — put the condition inside the block instead`. Put the `{{ if }}` inside the block.
- **`.Children` sees only the tags you wrote literally.** `<Tabs>{{ range .Params.names }}<Tab title="{{ . }}">x</Tab>{{ end }}</Tabs>` gives an empty tab bar, because those tabs do not exist until render time. Pass the data as a prop instead (`<Tabs :sections=".Sections">`). The example's Tabs guards its `index` with `{{ with .Children }}`, so an empty list renders an empty bar instead of failing the render.
- **`src=` is a literal path inside the project.** `:src=".Params.f"` fails with `:src is never allowed: a file is chosen only by a literal src= written in the file (X3)`, and `src="../x.go"` with `start from the project root, e.g. ~/components/Card.html (X7)`. Data can never choose which file is read.
- **A raw body ends at the first closing tag.** `<CodeBlock>a </CodeBlock> b</CodeBlock>` fails with `</CodeBlock> has no opening tag` at the second one, never a silent cut-off. Code that contains the tag goes in a file read with `src=`.
- **A block prop is read with `node`, a raw block prop with `.Props`.** `<brick:prop name="sql" raw>…</brick:prop>` arrives as the plain string `.Props.sql`; an ordinary block is never in `.Props`.
- **To show JSON as text, use a raw block or `src=`;** to use JSON as data, put it in frontmatter or pass `:config="fromJSON .Params.blob"`.
- **`dedent` removes only what every line shares.** A body whose lines start at different depths keeps the differences (`loses` / `  the common indent` above); a body that is flush left is unchanged.

## Related

- [Components and props](props.md): passing values.
- [Layouts and data](layouts-and-data.md): a layout is a component whose children are the page.
- [Frontmatter reference](../../reference/frontmatter.md): `raw`, `dedent` and every `brick.slots` setting.
- [Feature index](../../reference/features.md): the "Blocks, children and slots" rows.
- The single-page read: [Passing markup — `<brick:prop>`](../../guide.md#passing-markup--brickprop), [Children](../../guide.md#children), [Scoped slots](../../guide.md#scoped-slots--let-the-page-draw-each-row), [Code and other raw text](../../guide.md#code-and-other-raw-text).
- Example: [examples/components-slots](../../../examples/components-slots/).
- Example: [examples/components-options](../../../examples/components-options/) for the settings added in the later steps.
