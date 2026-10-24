// Package format rewrites a brick file into its canonical layout: the
// frontmatter fences, the indentation of nested elements and component tags,
// trailing whitespace, blank-line runs and the spacing between attributes.
//
// Why it exists: brick files are written by hand and by tools, and a team
// wants one layout without arguing about it, the way gofmt settles Go.
//
// The invariant that shapes everything here: formatting never changes what
// a file renders, beyond the length of a whitespace run that HTML already
// collapses. So the formatter only ever lengthens or shortens a run of
// spaces and tabs that holds at least one byte before and after (or that
// sits inside a tag, between attributes), and it never creates or removes a
// run in text. It never touches the bytes of a `{{ }}` action, a raw body
// (`<brick:script>`, `<brick:prop raw>`, a raw component), `<pre>`,
// `<textarea>`, `<script>`, `<style>`, `<title>`, an HTML comment, a quoted
// attribute value, the body of a `brick:` tag, or a region opted out with
// `{{/* brick:no-format */}}` … `{{/* brick:format */}}`. When the element
// structure of a file cannot be followed with certainty (an unclosed tag, a
// conditional that opens a tag in one branch only), indentation is left
// exactly as written and only the safe local rules apply.
//
// It reads the file with internal/lex, the engine's own scanner, never an
// HTML parser: an HTML5 parser would normalize markup and mangle `{{ }}`.
package format

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/ubgo/brick/internal/lex"
)

// DefaultIndent is one level of indentation: two spaces, the layout the
// kit and the examples are written in.
const DefaultIndent = "  "

// The template comments that turn formatting off and back on. A file whose
// body starts with the off marker and never turns it back on is left
// exactly as it is, frontmatter fences included.
const (
	MarkerOff = "brick:no-format"
	MarkerOn  = "brick:format"
)

// fence is the line that opens and closes frontmatter (the same grammar as
// internal/frontmatter: "---", trailing spaces and tabs allowed).
const fence = "---"

// The engine tags whose bodies are captured verbatim (compile.go's isRaw).
const (
	tagScript = "brick:script"
	tagClient = "brick:client"
	tagStyle  = "brick:style"
	tagProp   = "brick:prop"
	attrRaw   = "raw"
	enginePfx = "brick:"
)

// ErrUnclosedFrontmatter reports an opening fence with no closing one.
var ErrUnclosedFrontmatter = errors.New("frontmatter: opening --- has no closing ---")

// ErrInvalidUTF8 reports a file that is not UTF-8, which the engine refuses.
var ErrInvalidUTF8 = errors.New("file is not valid UTF-8")

// ErrTagFormatter reports a TagFormatters key that is not a raw engine tag.
var ErrTagFormatter = errors.New("a tag formatter is keyed by brick:client, brick:style or brick:script")

// formattable are the tags a TagFormatters entry may name: the engine
// tags whose bodies are another language, captured verbatim.
var formattable = map[string]bool{tagClient: true, tagStyle: true, tagScript: true}

// Options configures Format.
type Options struct {
	// IsRaw reports whether a component tag's body is raw: captured
	// verbatim by the engine (a component with `raw: true`, or <Markdown>
	// when a Markdown renderer is set). The engine tags are known already.
	// It must answer true for every raw component the file uses, because a
	// raw body taken for markup would be re-indented. Nil means none.
	IsRaw func(name string, attrs []lex.Attr) bool
	// Indent is one level of indentation; empty means DefaultIndent.
	Indent string

	// The options below are off at their zero value, which is the layout
	// every earlier brick fmt wrote, byte for byte.

	// LineLength wraps a markup line longer than this many runes: each tag
	// lying whole on the line with two or more attributes gets one
	// attribute per line, one level deeper than the tag. Only whitespace
	// inside a tag changes. 0 never wraps.
	LineLength int
	// AttrOrder moves the attributes it names to the front of each tag, in
	// its order; an entry ending in '*' names a prefix ("brick:*",
	// "hx-*"). Other attributes keep their order after them. A tag that
	// spans lines, holds an action between its attributes, or repeats an
	// attribute name is left as written. Unlike every other rule this
	// changes the attribute order of the output (the elements mean the
	// same); nil keeps every tag's order.
	AttrOrder []string
	// TagFormatters format the bodies of <brick:client>, <brick:style> and
	// <brick:script>, keyed by that tag name (any other key is an error):
	// the function gets the body without its common indentation and
	// returns the new one, which is indented one level under its opening
	// tag, with the closing tag on its own line. A body holding "{{" is
	// left alone, since an external formatter would mangle it. This is
	// how a site hands its JavaScript and CSS to its own formatter; the
	// result must be stable when formatted again (prettier is), or brick
	// fmt is not idempotent.
	TagFormatters map[string]func(body []byte) ([]byte, error)
	// Inline names the elements whose content does not take an
	// indentation level, so prose broken inside <a> or <em> stays at its
	// paragraph's level: HTML names compare without case, component tags
	// exactly (<Icon>, x-icon). Only the leading whitespace of lines
	// inside them changes, which HTML collapses. nil makes every element
	// but html, head and body indent its content, as brick fmt always has.
	Inline []string
}

// Result is a formatted file.
type Result struct {
	// Src is the formatted file.
	Src []byte
	// IndentSkipped explains why indentation was left as written, with the
	// 1-based line where the structure stopped being certain; empty when
	// the file was re-indented.
	IndentSkipped string
}

// Error is a file the scanner cannot read, with its 1-based position in
// the whole file (frontmatter included).
type Error struct {
	Line, Col int
	Msg       string
}

// Error renders line:col: message, the shape editors parse for a jump link.
func (e *Error) Error() string { return fmt.Sprintf("%d:%d: %s", e.Line, e.Col, e.Msg) }

// Format formats one brick file. It fails only when the engine could not
// read the file either (unclosed frontmatter, a malformed action or tag),
// so a file that renders always formats.
func Format(src []byte, opts Options) (Result, error) {
	if !utf8.Valid(src) {
		return Result{}, ErrInvalidUTF8
	}
	for name := range opts.TagFormatters {
		if !formattable[name] {
			return Result{}, fmt.Errorf("%w: %q", ErrTagFormatter, name)
		}
	}
	head, body, bodyLine, err := splitFrontmatter(src)
	if err != nil {
		return Result{}, err
	}
	toks, err := lex.Scan(body, lex.Options{IsRaw: rawFunc(opts.IsRaw)})
	if err != nil {
		var le *lex.Error
		if errors.As(err, &le) {
			return Result{}, &Error{Line: le.Pos.Line + bodyLine - 1, Col: le.Pos.Col, Msg: le.Msg}
		}
		return Result{}, err
	}
	if wholeFileOff(toks) {
		return Result{Src: slices.Clone(src)}, nil
	}
	indent := opts.Indent
	if indent == "" {
		indent = DefaultIndent
	}
	w := newWalker(body, toks)
	w.order = opts.AttrOrder
	w.inlineHTML, w.inlineTag = inlineSets(opts.Inline)
	w.walk()
	if bodyLine > 1 {
		head = normalizeFences(head)
	}
	newBody := w.rebuild(indent, opts.LineLength)
	if len(opts.TagFormatters) > 0 {
		newBody, err = formatBodies(newBody, bodyLine, indent, opts)
		if err != nil {
			return Result{}, err
		}
	}
	out := append(slices.Clone(head), newBody...)
	res := Result{Src: out}
	if w.broken != "" {
		res.IndentSkipped = fmt.Sprintf("line %d: %s", w.brokenLine+bodyLine-1, w.broken)
	}
	return res, nil
}

// rawFunc is the engine's raw-body rule (compile.go) plus the caller's
// knowledge of raw components.
func rawFunc(user func(string, []lex.Attr) bool) func(string, []lex.Attr) bool {
	return func(name string, attrs []lex.Attr) bool {
		switch name {
		case tagScript, tagClient, tagStyle:
			return true
		case tagProp:
			return slices.ContainsFunc(attrs, func(a lex.Attr) bool { return a.Name == attrRaw })
		}
		if strings.HasPrefix(name, enginePfx) {
			return false
		}
		return user != nil && user(name, attrs)
	}
}

// wholeFileOff reports a body that starts with the off marker (only
// whitespace before it) and never turns formatting back on.
func wholeFileOff(toks []lex.Token) bool {
	i := 0
	for i < len(toks) && toks[i].Kind == lex.Text && len(bytes.TrimSpace(toks[i].Raw)) == 0 {
		i++
	}
	if i == len(toks) || marker(toks[i]) != MarkerOff {
		return false
	}
	for _, t := range toks[i+1:] {
		if marker(t) == MarkerOn {
			return false
		}
	}
	return true
}

// marker returns the text of a template comment action, trimmed; empty
// for any other token.
func marker(t lex.Token) string {
	if t.Kind != lex.Action || t.Act != lex.ActComment {
		return ""
	}
	s := string(t.Raw)
	i, j := strings.Index(s, "/*"), strings.LastIndex(s, "*/")
	if i < 0 || j < i+2 {
		return ""
	}
	return strings.TrimSpace(s[i+2 : j])
}

// splitFrontmatter returns the frontmatter block (BOM, fences and YAML,
// through the newline after the closing fence), the body after it, and the
// 1-based file line the body starts on. It follows internal/frontmatter's
// grammar exactly, so the split is the one the engine makes.
func splitFrontmatter(src []byte) (head, body []byte, bodyLine int, err error) {
	start := 0
	if bytes.HasPrefix(src, []byte("\xEF\xBB\xBF")) {
		start = 3
	}
	first, rest, ok := bytes.Cut(src[start:], []byte("\n"))
	if !ok || !isFence(first) {
		return src[:start], src[start:], 1, nil
	}
	off := len(src) - len(rest)
	line := 2
	for {
		l, r, more := bytes.Cut(src[off:], []byte("\n"))
		if isFence(l) {
			end := len(src) - len(r)
			if !more {
				end = len(src)
			}
			return src[:end], src[end:], line + 1, nil
		}
		if !more {
			return nil, nil, 0, &Error{Line: 1, Col: 1, Msg: ErrUnclosedFrontmatter.Error()}
		}
		off = len(src) - len(r)
		line++
	}
}

func isFence(line []byte) bool { return string(bytes.TrimRight(line, " \t\r")) == fence }

// normalizeFences drops trailing spaces and tabs from the two fence lines;
// the YAML between them is never touched (a block scalar keeps its
// whitespace).
func normalizeFences(head []byte) []byte {
	var out []byte
	lines := bytes.SplitAfter(head, []byte("\n"))
	last := len(lines) - 1
	if len(lines[last]) == 0 {
		last--
	}
	for i, l := range lines {
		if i == 0 || i == last {
			bom := []byte{}
			if i == 0 && bytes.HasPrefix(l, []byte("\xEF\xBB\xBF")) {
				bom, l = l[:3], l[3:]
			}
			eol := l[len(bytes.TrimRight(l, "\r\n")):]
			out = append(out, bom...)
			out = append(out, fence...)
			out = append(out, eol...)
			continue
		}
		out = append(out, l...)
	}
	return out
}

// formatBodies hands each <brick:client>, <brick:style> and <brick:script>
// body that has a TagFormatters entry to it, in the already formatted
// body, so the indentation it lays the result under is the opening tag's
// final one. A region turned off with brick:no-format and a body holding
// "{{" are skipped.
func formatBodies(body []byte, bodyLine int, indent string, opts Options) ([]byte, error) {
	toks, err := lex.Scan(body, lex.Options{IsRaw: rawFunc(opts.IsRaw)})
	if err != nil {
		// The walk only changed whitespace the scanner skips, so this
		// cannot fail on a body that scanned before.
		return nil, err
	}
	var out []byte
	from, off := 0, false
	for i, t := range toks {
		switch marker(t) {
		case MarkerOff:
			off = true
		case MarkerOn:
			off = false
		}
		f := opts.TagFormatters[t.Name]
		if off || t.Kind != lex.Open || t.SelfClosing || f == nil || i+1 >= len(toks) || !toks[i+1].RawBody {
			continue
		}
		raw := toks[i+1].Raw
		if bytes.Contains(raw, []byte("{{")) {
			continue
		}
		got, err := f(dedent(raw))
		if err != nil {
			return nil, &Error{Line: t.Pos.Line + bodyLine - 1, Col: t.Pos.Col, Msg: fmt.Sprintf("<%s> formatter: %v", t.Name, err)}
		}
		lead := lineLead(body, t.Pos.Offset)
		start := toks[i+1].Pos.Offset
		out = append(out, body[from:start]...)
		out = append(out, reindent(got, lead, lead+indent)...)
		from = start + len(raw)
	}
	return append(out, body[from:]...), nil
}

// lineLead is the spaces and tabs that start the line holding offset p.
func lineLead(src []byte, p int) string {
	start := bytes.LastIndexByte(src[:p], '\n') + 1
	end := start
	for end < p && (src[end] == ' ' || src[end] == '\t') {
		end++
	}
	return string(src[start:end])
}

// dedent drops a body's leading and trailing blank lines and the
// indentation its lines share, so the formatter sees the code as written.
func dedent(raw []byte) []byte {
	lines := strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n")
	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	common, set := "", false
	for _, l := range lines {
		if strings.TrimSpace(l) == "" {
			continue
		}
		if !set {
			common, set = l[:len(l)-len(strings.TrimLeft(l, " \t"))], true
		}
		for !strings.HasPrefix(l, common) {
			common = common[:len(common)-1]
		}
	}
	for k, l := range lines {
		lines[k] = strings.TrimPrefix(l, common)
	}
	if len(lines) == 0 {
		return nil
	}
	return []byte(strings.Join(lines, "\n") + "\n")
}

// reindent lays a formatter's output under its opening tag: every
// non-blank line gets inner, and the closing tag gets its own line at
// outer. An empty result leaves the element empty.
func reindent(got []byte, outer, inner string) []byte {
	text := strings.TrimRight(strings.ReplaceAll(string(got), "\r\n", "\n"), "\n \t")
	if strings.TrimSpace(text) == "" {
		return nil
	}
	var b strings.Builder
	b.WriteByte('\n')
	for l := range strings.SplitSeq(text, "\n") {
		if strings.TrimSpace(l) != "" {
			b.WriteString(inner)
			b.WriteString(strings.TrimRight(l, " \t"))
		}
		b.WriteByte('\n')
	}
	b.WriteString(outer)
	return []byte(b.String())
}
