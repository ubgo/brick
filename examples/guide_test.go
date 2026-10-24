package main

import (
	"context"
	"os"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/ubgo/brick"
	sanitizebluemonday "github.com/ubgo/brick/contrib/sanitize-bluemonday"
	"github.com/ubgo/brick/internal/lex"
	"github.com/ubgo/brick/internal/tree"
)

// guidePath is the public guide. Its snippets that show an output are
// rendered here; every other HTML snippet must at least be valid brick.
const guidePath = "../docs/guide.md"

// anyBlockRE matches an optional "`title`" line and the fenced block after it.
var anyBlockRE = regexp.MustCompile("(?s)(?:`([^`\n]+)`\n\n)?```(\\w*)\n(.*?)\n```")

// block is one fenced code block with its title (possibly empty).
type block struct{ title, lang, body string }

func blocksOf(t *testing.T, path string) []block {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out []block
	for _, m := range anyBlockRE.FindAllStringSubmatch(string(b), -1) {
		out = append(out, block{title: m[1], lang: m[2], body: m[3]})
	}
	return out
}

// guideBlock finds the block with a title, failing when the guide lost it.
func guideBlock(t *testing.T, blocks []block, title string, nth int) string {
	t.Helper()
	for _, b := range blocks {
		if b.title == title {
			if nth == 0 {
				return b.body
			}
			nth--
		}
	}
	t.Fatalf("the guide has no block titled %q", title)
	return ""
}

// Every html snippet in the guide and the cookbook scans and builds a tree:
// no documented example can have a syntax error. Blocks that pair an input
// with its output ("page → you get") hold two documents and are split.
func TestDocSnippetsAreValidBrick(t *testing.T) {
	raw := func(n string, a []lex.Attr) bool {
		if n == "CodeBlock" || n == "Markdown" {
			return true
		}
		for _, x := range a {
			if x.Name == "raw" {
				return true
			}
		}
		return n == tree.TagScript || n == tree.TagClient || n == tree.TagStyle
	}
	checked := 0
	for _, path := range []string{guidePath, cookbookPath} {
		for _, b := range blocksOf(t, path) {
			if b.lang != "html" || strings.HasPrefix(b.title, "output") || strings.HasPrefix(b.title, "you get") || b.title == "testdata/card.golden" {
				continue
			}
			docs := []string{b.body}
			if strings.Contains(b.title, "→") {
				in, _, _ := strings.Cut(b.body, "\n\n") // the input half; the output half is plain HTML
				docs = []string{in}
			}
			for _, doc := range docs {
				toks, err := lex.Scan([]byte(doc), lex.Options{IsRaw: raw})
				if err == nil {
					_, err = tree.Build(toks)
				}
				if err != nil {
					t.Errorf("%s, block %q: %v\n%s", path, b.title, err, doc)
				}
				checked++
			}
		}
	}
	if checked < 40 {
		t.Fatalf("only %d snippets checked; the block pattern no longer matches the docs", checked)
	}
}

// The guide's snippets that show an output render to exactly that output.
func TestGuideOutputs(t *testing.T) {
	blocks := blocksOf(t, guidePath)
	ctx := context.Background()
	render := func(t *testing.T, opts brick.Options, files ...string) string {
		t.Helper()
		fsys := fstest.MapFS{}
		for i := 0; i+1 < len(files); i += 2 {
			fsys[files[i]] = &fstest.MapFile{Data: []byte(files[i+1])}
		}
		opts.FS = fsys
		e := mustEngine(t, opts)
		out, err := e.Render(ctx, "content/page.html", nil)
		if err != nil {
			t.Fatal(err)
		}
		return string(out)
	}
	pair := func(title string) (string, string) {
		in, out, _ := strings.Cut(guideBlock(t, blocks, title, 0), "\n\n")
		return in, out
	}

	t.Run("pages", func(t *testing.T) {
		got := render(t, brick.Options{}, "content/page.html", guideBlock(t, blocks, "content/about.html", 0))
		expectOutput(t, "about", []byte(got), nil, guideBlock(t, blocks, "you get", 0))
	})

	t.Run("loops", func(t *testing.T) {
		page := "---\nposts:\n  - {Title: Hello, Summary: The first post.}\n  - {Title: Again, Summary: More words.}\n---\n" + guideBlock(t, blocks, "the page", 0)
		got := render(t, brick.Options{}, "components/Card.html", guideBlock(t, blocks, "components/Card.html", 0), "content/page.html", page)
		expectOutput(t, "loop", []byte(got), nil, guideBlock(t, blocks, "you get", 1))
	})

	t.Run("attributes", func(t *testing.T) {
		in, want := pair("page → you get")
		got := render(t, brick.Options{}, "components/Card.html", guideBlock(t, blocks, "components/Card.html", 1), "content/page.html", in)
		expectOutput(t, "attrs", []byte(got), nil, want)
	})

	t.Run("spread", func(t *testing.T) {
		title := "components/Primary.html → <Primary id=&quot;buy&quot; hx-post=&quot;/buy&quot;>Buy</Primary>"
		primary, want := pair(title)
		got := render(t, brick.Options{},
			"components/Button.html", `<button {{ attrs }}>{{ children }}</button>`,
			"components/Primary.html", primary,
			"content/page.html", `<Primary id="buy" hx-post="/buy">Buy</Primary>`)
		expectOutput(t, "spread", []byte(got), nil, want)
	})

	t.Run("sanitize", func(t *testing.T) {
		page := "---\ncomment: \"<p>Hi <b>there</b></p><script>steal()</script>\"\n---\n" + guideBlock(t, blocks, "a user comment, shown safely", 0)
		got := render(t, brick.Options{Sanitizer: sanitizebluemonday.New()}, "content/page.html", page)
		expectOutput(t, "sanitize", []byte(got), nil, guideBlock(t, blocks, "you get, for a comment of <p>Hi <b>there</b></p><script>steal()</script>", 0))
	})
}
