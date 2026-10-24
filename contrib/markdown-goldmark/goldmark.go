// Package markdowngoldmark converts markdown for brick's <Markdown>
// component with goldmark.
//
//	eng, err := brick.New(brick.Options{Markdown: markdowngoldmark.New()})
//
// The default extension set is fixed and documented, because output bytes
// are part of brick's compatibility promise (A14): CommonMark plus GFM
// tables, task lists and strikethrough. Raw HTML inside markdown is dropped
// (goldmark's safe default). Heading ids and syntax highlighting are opt-in
// options, so New() with no options writes the same bytes it always has.
package markdowngoldmark

import (
	"bytes"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode"

	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/yuin/goldmark"
	highlighting "github.com/yuin/goldmark-highlighting/v2"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/text"

	"github.com/ubgo/brick"
)

// Converter is a brick.Markdown backed by goldmark. It also implements
// brick.MarkdownHeadings; heading ids are written only with WithHeadingIDs.
// A Converter is safe for concurrent use.
type Converter struct {
	md         goldmark.Markdown
	headingIDs bool
}

// Option configures a Converter. Every option is off by default.
type Option func(*config)

type config struct {
	headingIDs bool
	highlight  bool
}

// WithHeadingIDs writes an id on every heading: a slug of its plain text
// (lowercased; letters and digits kept; every other run becomes one "-"),
// deduplicated within one document as "intro", "intro-1", "intro-2". Ids
// are unique per converted document, not per page: two <Markdown> blocks
// on one page with the same heading get the same id, which is why the
// option is off by default.
func WithHeadingIDs() Option { return func(c *config) { c.headingIDs = true } }

// WithHighlight highlights fenced code blocks whose language chroma knows,
// emitting CSS classes (class="chroma", class="k", ...) and never inline
// styles, so the site's stylesheet and Content-Security-Policy decide how
// code looks. Pair it with WriteHighlightCSS. A block with no language or
// an unknown one keeps the plain <pre><code class="language-x"> output.
func WithHighlight() Option { return func(c *config) { c.highlight = true } }

// New returns a converter with the fixed extension set plus any options.
func New(opts ...Option) *Converter {
	var cfg config
	for _, o := range opts {
		o(&cfg)
	}
	exts := []goldmark.Extender{extension.Table, extension.TaskList, extension.Strikethrough}
	if cfg.highlight {
		exts = append(exts, highlighting.NewHighlighting(highlighting.WithFormatOptions(chromahtml.WithClasses(true))))
	}
	return &Converter{md: goldmark.New(goldmark.WithExtensions(exts...)), headingIDs: cfg.headingIDs}
}

// Convert writes the HTML for src.
func (c *Converter) Convert(src []byte, w io.Writer) error {
	_, err := c.ConvertHeadings(src, w)
	return err
}

// ConvertHeadings writes the HTML for src, byte-identical to Convert, and
// returns its headings in document order. Heading.ID is "" unless
// WithHeadingIDs is on.
func (c *Converter) ConvertHeadings(src []byte, w io.Writer) ([]brick.Heading, error) {
	doc := c.md.Parser().Parse(text.NewReader(src))
	var heads []brick.Heading
	used := map[string]bool{}
	err := ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		h, ok := n.(*ast.Heading)
		if !ok || !entering {
			return ast.WalkContinue, nil
		}
		var b strings.Builder
		plainText(&b, h, src)
		hd := brick.Heading{Level: h.Level, Text: strings.TrimSpace(b.String())}
		if c.headingIDs {
			hd.ID = uniqueID(Slug(hd.Text), used)
			h.SetAttributeString("id", []byte(hd.ID))
		}
		heads = append(heads, hd)
		return ast.WalkSkipChildren, nil
	})
	if err != nil {
		return nil, err
	}
	if err := c.md.Renderer().Render(w, src, doc); err != nil {
		return nil, err
	}
	return heads, nil
}

// plainText appends the visible text of n's inline children: text, code
// spans, link labels and image alt text, never raw HTML (which the
// renderer drops too).
func plainText(b *strings.Builder, n ast.Node, src []byte) {
	for ch := n.FirstChild(); ch != nil; ch = ch.NextSibling() {
		switch t := ch.(type) {
		case *ast.Text:
			b.Write(t.Segment.Value(src))
			if t.SoftLineBreak() || t.HardLineBreak() {
				b.WriteByte(' ')
			}
		case *ast.String:
			b.Write(t.Value)
		case *ast.AutoLink:
			b.Write(t.Label(src))
		case *ast.RawHTML:
		default:
			plainText(b, ch, src)
		}
	}
}

// fallbackID names a heading whose text has no letter or digit.
const fallbackID = "heading"

// Slug is the deterministic id for heading text: Unicode letters and
// digits lowercased and kept, every other run collapsed to one "-", and
// leading or trailing "-" trimmed. Text with no letter or digit gives
// "heading". Exported so a site can link to a heading it knows by text.
func Slug(s string) string {
	var b strings.Builder
	dash := false
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			dash = false
			b.WriteRune(unicode.ToLower(r))
			continue
		}
		dash = true
	}
	if b.Len() == 0 {
		return fallbackID
	}
	return b.String()
}

// uniqueID returns base, or base-1, base-2, ... skipping any id already
// used, so a heading literally titled "Intro 1" never collides with the
// second "Intro".
func uniqueID(base string, used map[string]bool) string {
	id := base
	for i := 1; used[id]; i++ {
		id = base + "-" + strconv.Itoa(i)
	}
	used[id] = true
	return id
}

// WriteHighlightCSS writes the stylesheet for WithHighlight's classes in
// the named chroma style ("github", "monokai", "dracula", ...; the full
// list is chroma's styles.Names()). Generate it once into a static file:
//
//	f, _ := os.Create("static/chroma.css")
//	err := markdowngoldmark.WriteHighlightCSS(f, "github")
func WriteHighlightCSS(w io.Writer, style string) error {
	s, ok := styles.Registry[style]
	if !ok {
		return fmt.Errorf("markdowngoldmark: unknown chroma style %q", style)
	}
	var buf bytes.Buffer
	if err := chromahtml.New(chromahtml.WithClasses(true)).WriteCSS(&buf, s); err != nil {
		return err
	}
	_, err := w.Write(buf.Bytes())
	return err
}

var (
	_ brick.Markdown         = (*Converter)(nil)
	_ brick.MarkdownHeadings = (*Converter)(nil)
)
