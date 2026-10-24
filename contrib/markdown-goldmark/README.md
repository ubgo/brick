# brick/contrib/markdown-goldmark

Turns on brick's `<Markdown>` component using [goldmark](https://github.com/yuin/goldmark).

```go
eng, err := brick.New(brick.Options{FS: fsys, Markdown: markdowngoldmark.New()})
```

```html
<Markdown>
## Why

Pages are **files**.
</Markdown>

<Markdown src="~/posts/hello.md" />
```

The extension set is fixed so output never changes under you: CommonMark plus GFM tables, task lists and strikethrough. Raw HTML inside markdown is dropped, and `New()` with no options writes no heading ids and no highlighting, so the bytes never change under you. `<Card>` and `{{ }}` inside a block are literal text.

## Options

Both are off by default.

```go
md := markdowngoldmark.New(markdowngoldmark.WithHeadingIDs(), markdowngoldmark.WithHighlight())
```

`WithHeadingIDs()` gives every heading an `id`: its plain text lowercased, letters and digits kept, every other run turned into one `-`. A repeated heading gets `-1`, `-2`, and so on, skipping any id already taken. Ids are unique within one converted document, not across a page, so two `<Markdown>` blocks with the same heading share an id.

```markdown
# Intro
## Intro
## Setext *em* `c`
```

```html
<h1 id="intro">Intro</h1>
<h2 id="intro-1">Intro</h2>
<h2 id="setext-em-c">Setext <em>em</em> <code>c</code></h2>
```

`markdowngoldmark.Slug(text)` returns the same slug, for linking to a heading you know by its text.

`WithHighlight()` highlights fenced code whose language [chroma](https://github.com/alecthomas/chroma) knows, writing CSS classes and never inline styles, so your stylesheet and Content-Security-Policy stay in control. A block with no language, or one chroma does not know, keeps the plain `<pre><code class="language-x">` output.

````markdown
```go
x := 1
```
````

```html
<pre class="chroma"><code><span class="line"><span class="cl"><span class="nx">x</span><span class="w"> </span><span class="o">:=</span><span class="w"> </span><span class="mi">1</span><span class="w">
</span></span></span></code></pre>
```

Write the stylesheet once with `WriteHighlightCSS`, naming any chroma style (`github`, `monokai`, `dracula`, ...); an unknown name is an error:

```go
f, _ := os.Create("static/chroma.css")
defer f.Close()
err := markdowngoldmark.WriteHighlightCSS(f, "github")
```

## Headings for a table of contents

`Converter` implements `brick.MarkdownHeadings`, an optional upgrade of `brick.Markdown`: `ConvertHeadings(src, w)` writes the same bytes as `Convert` and also returns `[]brick.Heading` (`Level`, `ID`, `Text`) in document order. The site layer uses it to fill `.Page.Headings`. With `WithHeadingIDs()` off the headings are still listed, with an empty `ID`.
