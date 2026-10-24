package markdowngoldmark

import (
	"bytes"
	"context"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"

	"github.com/ubgo/brick"
)

func TestMarkdownComponent(t *testing.T) {
	t.Parallel()
	fsys := fstest.MapFS{
		"posts/hello.md": {Data: []byte("# Intro\n\n| a | b |\n|---|---|\n| 1 | 2 |\n")},
		"content/a.html": {Data: []byte("<Markdown>\n  ## Why\n\n  Pages are **files**. ~~old~~ <script>x</script> <Card> {{ .X }}\n\n  - [x] done\n</Markdown><Markdown src=\"~/posts/hello.md\" />")},
	}
	e, err := brick.New(brick.Options{FS: fsys, Markdown: New()})
	if err != nil {
		t.Fatal(err)
	}
	out, err := e.Render(context.Background(), "content/a.html", nil)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, want := range []string{"<h2>Why</h2>", "<strong>files</strong>", "<del>old</del>", "<!-- raw HTML omitted -->", "{{ .X }}", `type="checkbox"`, "<h1>Intro</h1>", "<table>"} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in\n%s", want, s)
		}
	}
	if strings.Contains(s, "<script>") || strings.Contains(s, `id="`) {
		t.Errorf("raw HTML kept or heading ids added:\n%s", s)
	}
}

// goldenSrc exercises every heading form, inline markup inside headings,
// duplicates and fenced code with a known, an unknown and no language.
const goldenSrc = "# Intro\n\n## Intro\n\n## Intro 1\n\nSetext *em* `c` [l](http://x)\n---\n\n# <b>x</b> !!!\n\n```go\nx := 1\n```\n\n```nolang\na<b\n```\n"

func convert(t *testing.T, c *Converter) (string, []brick.Heading) {
	t.Helper()
	var b bytes.Buffer
	hs, err := c.ConvertHeadings([]byte(goldenSrc), &b)
	if err != nil {
		t.Fatal(err)
	}
	var plain bytes.Buffer
	if err := c.Convert([]byte(goldenSrc), &plain); err != nil {
		t.Fatal(err)
	}
	if plain.String() != b.String() {
		t.Fatalf("Convert and ConvertHeadings differ:\n%s\n---\n%s", plain.String(), b.String())
	}
	return b.String(), hs
}

// TestDefaultBytesUnchanged pins A14: New() with no options writes exactly
// what the pre-options adapter (goldmark with the fixed extension set)
// wrote.
func TestDefaultBytesUnchanged(t *testing.T) {
	t.Parallel()
	var want bytes.Buffer
	ref := goldmark.New(goldmark.WithExtensions(extension.Table, extension.TaskList, extension.Strikethrough))
	if err := ref.Convert([]byte(goldenSrc), &want); err != nil {
		t.Fatal(err)
	}
	got, hs := convert(t, New())
	if got != want.String() {
		t.Fatalf("default output changed:\n%s\n--- want ---\n%s", got, want.String())
	}
	for _, h := range hs {
		if h.ID != "" {
			t.Errorf("heading id %q written with ids off", h.ID)
		}
	}
}

func TestHeadingIDs(t *testing.T) {
	t.Parallel()
	got, hs := convert(t, New(WithHeadingIDs()))
	const want = `<h1 id="intro">Intro</h1>
<h2 id="intro-1">Intro</h2>
<h2 id="intro-1-1">Intro 1</h2>
<h2 id="setext-em-c-l">Setext <em>em</em> <code>c</code> <a href="http://x">l</a></h2>
<h1 id="x"><!-- raw HTML omitted -->x<!-- raw HTML omitted --> !!!</h1>
<pre><code class="language-go">x := 1
</code></pre>
<pre><code class="language-nolang">a&lt;b
</code></pre>
`
	if got != want {
		t.Fatalf("got\n%s", got)
	}
	wantHs := []brick.Heading{{Level: 1, ID: "intro", Text: "Intro"}, {Level: 2, ID: "intro-1", Text: "Intro"}, {Level: 2, ID: "intro-1-1", Text: "Intro 1"}, {Level: 2, ID: "setext-em-c-l", Text: "Setext em c l"}, {Level: 1, ID: "x", Text: "x !!!"}}
	if !reflect.DeepEqual(hs, wantHs) {
		t.Fatalf("headings %+v", hs)
	}
	// Same input, same bytes: ids restart for every document.
	if again, _ := convert(t, New(WithHeadingIDs())); again != got {
		t.Fatal("second conversion differs")
	}
}

func TestSlug(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{"Hello, World!": "hello-world", "  --a__b--  ": "a-b", "Ünïcode 日本": "ünïcode-日本", "!!!": "heading", "": "heading", "v1.2": "v1-2"} {
		if got := Slug(in); got != want {
			t.Errorf("Slug(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestHighlight(t *testing.T) {
	t.Parallel()
	got, _ := convert(t, New(WithHighlight()))
	const wantCode = `<pre class="chroma"><code><span class="line"><span class="cl"><span class="nx">x</span><span class="w"> </span><span class="o">:=</span><span class="w"> </span><span class="mi">1</span><span class="w">
</span></span></span></code></pre><pre><code class="language-nolang">a&lt;b
</code></pre>
`
	if !strings.HasSuffix(got, wantCode) {
		t.Fatalf("got\n%s", got)
	}
	if strings.Contains(got, "style=") || strings.Contains(got, `id="`) {
		t.Fatalf("inline style or heading id in\n%s", got)
	}
}

func TestWriteHighlightCSS(t *testing.T) {
	t.Parallel()
	var b bytes.Buffer
	if err := WriteHighlightCSS(&b, "github"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), ".chroma .k ") {
		t.Fatalf("no keyword rule in\n%s", b.String())
	}
	if err := WriteHighlightCSS(&b, "no-such-style"); err == nil {
		t.Fatal("unknown style accepted")
	}
}
