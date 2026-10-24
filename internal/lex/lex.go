// Package lex scans the body of a brick file into a flat token stream:
// text, template actions, and component open/close tags.
//
// Why it exists: brick must find component tags without parsing HTML (an HTML5
// parser normalizes markup and mangles `{{ }}`), yet it must *lex* template
// actions rather than skip them: a `<Card>` inside a string literal
// is text, `{{ if }}`/`{{ end }}` nesting decides where a tag may close, and
// the transpiler needs every `$variable` a body uses (T2) and every identifier
// it rewrites (T4, T7). This is tokenizing, not parsing: plain HTML and
// action code pass through as opaque bytes with positions.
//
// Invariant: concatenating every token's Raw bytes reproduces the input
// exactly.
package lex

import (
	"bytes"
	"fmt"
	"strings"
)

// Kind is the kind of a token.
type Kind int

// Token kinds.
const (
	// Text is opaque bytes: HTML, comments, plain tags, raw bodies.
	Text Kind = iota
	// Action is one `{{ … }}` template action, including its delimiters.
	Action
	// Open is a component or `brick:` tag opening (or self-closing).
	Open
	// Close is a component or `brick:` closing tag.
	Close
)

// ActKind classifies an action by its leading keyword.
type ActKind int

// Action kinds. Opening keywords start a frame that `end` closes.
const (
	ActOther ActKind = iota
	ActIf
	ActRange
	ActWith
	ActBlock
	ActDefine
	ActElse
	ActEnd
	ActBreak
	ActContinue
	ActComment
)

// Opens reports whether the action starts a frame that needs an `end`.
func (k ActKind) Opens() bool {
	return k == ActIf || k == ActRange || k == ActWith || k == ActBlock || k == ActDefine
}

// Pos is a 1-based line and column plus a 0-based byte offset in the body.
type Pos struct {
	Offset int
	Line   int
	Col    int
}

// String renders line:col, the prefix every scanner and compile error uses.
func (p Pos) String() string { return fmt.Sprintf("%d:%d", p.Line, p.Col) }

// Attr is one attribute on a component tag, exactly as written.
type Attr struct {
	Name     string
	Value    string // raw value text, without quotes; may contain `{{ }}`
	HasValue bool   // false for a bare attribute such as `open`
	Quote    byte   // '"', '\'' or 0 when unquoted
	Pos      Pos
}

// Ident is a bare identifier inside an action (not a field `.x`, variable
// `$x` or part of a string), with byte offsets relative to Token.Raw.
type Ident struct {
	Name       string
	Start, End int
}

// Token is one scanned piece of the body.
type Token struct {
	Kind Kind
	Pos  Pos
	Raw  []byte

	// Action fields.
	Act    ActKind
	Decls  []string // $variables declared by this action (`$p := …`, `range $i, $p :=`)
	Refs   []string // $variables referenced by this action, including declared ones
	Idents []Ident

	// Open/Close fields.
	Name        string
	Attrs       []Attr
	SelfClosing bool
	RawBody     bool // Text token that is a verbatim raw body (no actions inside)
}

// Error is a scan error with its position.
type Error struct {
	Pos Pos
	Msg string
}

// Error renders line:col: message; the compiler adds the file and the
// body's line offset when it reports it.
func (e *Error) Error() string { return e.Pos.String() + ": " + e.Msg }

// Options tells the scanner which tags capture their body verbatim.
type Options struct {
	// IsRaw reports whether an opening tag's body is captured verbatim until
	// its matching close tag (raw-body components, `<brick:prop raw>`,
	// `<brick:script>`, `<brick:client>`, `<brick:style>`). May be nil.
	IsRaw func(name string, attrs []Attr) bool
}

// rawTextElements hold text in which HTML markup is not parsed, so no
// component is recognized inside them. Template actions still are.
var rawTextElements = map[string]bool{"script": true, "style": true, "textarea": true, "title": true}

// IsComponentName reports whether a tag name is a component or engine tag by
// its spelling alone: an uppercase ASCII first letter, the `x-` prefix, or
// the `brick:` prefix. Resolution (registered or not) happens later.
func IsComponentName(name string) bool {
	if name == "" {
		return false
	}
	if strings.HasPrefix(name, "brick:") || (strings.HasPrefix(name, "x-") && len(name) > 2) {
		return true
	}
	return name[0] >= 'A' && name[0] <= 'Z'
}

// Scan tokenizes a body.
func Scan(src []byte, opts Options) ([]Token, error) {
	s := &scanner{src: src, line: 1, col: 1, opts: opts}
	if err := s.run(); err != nil {
		return nil, err
	}
	return s.toks, nil
}

type scanner struct {
	src       []byte
	i         int
	line, col int
	textStart int
	textPos   Pos
	inText    bool
	toks      []Token
	opts      Options
}

func (s *scanner) pos() Pos { return Pos{Offset: s.i, Line: s.line, Col: s.col} }

// advance moves n bytes forward, keeping line/col in step.
func (s *scanner) advance(n int) {
	for k := 0; k < n && s.i < len(s.src); k++ {
		if s.src[s.i] == '\n' {
			s.line++
			s.col = 1
		} else {
			s.col++
		}
		s.i++
	}
}

func (s *scanner) startText() {
	if !s.inText {
		s.inText = true
		s.textStart = s.i
		s.textPos = s.pos()
	}
}

func (s *scanner) flushText() {
	if s.inText && s.i > s.textStart {
		s.toks = append(s.toks, Token{Kind: Text, Pos: s.textPos, Raw: s.src[s.textStart:s.i]})
	}
	s.inText = false
}

func (s *scanner) errf(p Pos, format string, a ...any) error {
	return &Error{Pos: p, Msg: fmt.Sprintf(format, a...)}
}

func (s *scanner) run() error {
	for s.i < len(s.src) {
		switch {
		case s.hasPrefix("{{"):
			if err := s.action(); err != nil {
				return err
			}
		case s.hasPrefix("<!--"):
			if err := s.comment(); err != nil {
				return err
			}
		case s.hasPrefix("<![CDATA["):
			s.startText()
			end := bytes.Index(s.src[s.i:], []byte("]]>"))
			if end < 0 {
				return s.errf(s.pos(), "unclosed <![CDATA[")
			}
			s.advance(end + 3)
		case s.hasPrefix("</"):
			if err := s.closeTag(); err != nil {
				return err
			}
		case s.src[s.i] == '<' && s.i+1 < len(s.src) && isNameStart(s.src[s.i+1]):
			if err := s.openTag(); err != nil {
				return err
			}
		default:
			s.startText()
			s.advance(1)
		}
	}
	s.flushText()
	return nil
}

func (s *scanner) hasPrefix(p string) bool { return bytes.HasPrefix(s.src[s.i:], []byte(p)) }

// comment scans `<!-- … -->` as text; actions inside are still lexed so
// nesting stays correct, but no component is recognized.
func (s *scanner) comment() error {
	start := s.pos()
	s.startText()
	s.advance(4)
	for s.i < len(s.src) {
		if s.hasPrefix("-->") {
			s.advance(3)
			return nil
		}
		if s.hasPrefix("{{") {
			if err := s.action(); err != nil {
				return err
			}
			s.startText()
			continue
		}
		s.advance(1)
	}
	return s.errf(start, "unclosed <!-- comment")
}

// action lexes one `{{ … }}` into an Action token.
func (s *scanner) action() error {
	s.flushText()
	start := s.pos()
	begin := s.i
	j := s.i + 2
	for j < len(s.src) {
		c := s.src[j]
		switch {
		case c == '"':
			k, ok := skipQuoted(s.src, j, '"', true)
			if !ok {
				return s.errf(start, "unterminated string in {{ }}")
			}
			j = k
		case c == '`':
			k, ok := skipQuoted(s.src, j, '`', false)
			if !ok {
				return s.errf(start, "unterminated raw string in {{ }}")
			}
			j = k
		case c == '\'':
			k, ok := skipQuoted(s.src, j, '\'', true)
			if !ok {
				return s.errf(start, "unterminated character in {{ }}")
			}
			j = k
		case c == '/' && j+1 < len(s.src) && s.src[j+1] == '*':
			end := bytes.Index(s.src[j+2:], []byte("*/"))
			if end < 0 {
				return s.errf(start, "unterminated comment in {{ }}")
			}
			j += 2 + end + 2
		case c == '}' && j+1 < len(s.src) && s.src[j+1] == '}':
			raw := s.src[begin : j+2]
			tok := Token{Kind: Action, Pos: start, Raw: raw}
			classify(&tok)
			s.advance(j + 2 - begin)
			s.toks = append(s.toks, tok)
			return nil
		default:
			j++
		}
	}
	return s.errf(start, "unclosed {{")
}

// skipQuoted returns the index just past a quoted literal starting at i.
func skipQuoted(b []byte, i int, q byte, escapes bool) (int, bool) {
	for j := i + 1; j < len(b); j++ {
		if escapes && b[j] == '\\' {
			j++
			continue
		}
		if b[j] == q {
			return j + 1, true
		}
	}
	return 0, false
}

// classify fills Act, Decls, Refs and Idents from the action's code.
func classify(t *Token) {
	code := t.Raw[2 : len(t.Raw)-2]
	lead := 2
	if len(code) > 0 && code[0] == '-' {
		code = code[1:]
		lead++
	}
	if n := len(code); n > 0 && code[n-1] == '-' {
		code = code[:n-1]
	}
	trimmed := bytes.TrimSpace(code)
	if bytes.HasPrefix(trimmed, []byte("/*")) {
		t.Act = ActComment
		return
	}
	switch word := firstWord(trimmed); word {
	case "if":
		t.Act = ActIf
	case "range":
		t.Act = ActRange
	case "with":
		t.Act = ActWith
	case "block":
		t.Act = ActBlock
	case "define":
		t.Act = ActDefine
	case "else":
		t.Act = ActElse
	case "end":
		t.Act = ActEnd
	case "break":
		t.Act = ActBreak
	case "continue":
		t.Act = ActContinue
	}
	scanCode(t, code, lead)
}

func firstWord(b []byte) string {
	i := 0
	for i < len(b) && isIdentChar(b[i]) {
		i++
	}
	return string(b[:i])
}

// keywords are template words that are never function identifiers.
var keywords = map[string]bool{
	"if": true, "else": true, "end": true, "range": true, "with": true, "block": true,
	"define": true, "template": true, "break": true, "continue": true, "nil": true,
	"true": true, "false": true,
}

// scanCode walks action code outside literals, recording $variables and bare
// identifiers. offset is where code starts within Raw.
func scanCode(t *Token, code []byte, offset int) {
	seen := map[string]bool{}
	for i := 0; i < len(code); {
		c := code[i]
		switch {
		case c == '"' || c == '\'':
			k, ok := skipQuoted(code, i, c, true)
			if !ok {
				return
			}
			i = k
		case c == '`':
			k, ok := skipQuoted(code, i, '`', false)
			if !ok {
				return
			}
			i = k
		case c == '/' && i+1 < len(code) && code[i+1] == '*':
			end := bytes.Index(code[i+2:], []byte("*/"))
			if end < 0 {
				return
			}
			i += 2 + end + 2
		case c == '$':
			j := i + 1
			for j < len(code) && isIdentChar(code[j]) {
				j++
			}
			name := string(code[i+1 : j])
			if name != "" && !seen[name] {
				seen[name] = true
				t.Refs = append(t.Refs, name)
			}
			i = j
		case c == '.':
			// Skip a field chain `.Foo.bar` so `bar` is not taken as an identifier.
			j := i + 1
			for j < len(code) && (isIdentChar(code[j]) || code[j] == '.') {
				j++
			}
			i = j
		case isNameStart(c):
			j := i
			for j < len(code) && isIdentChar(code[j]) {
				j++
			}
			// A namespace call `helpers.slugify` is recorded as one identifier.
			if j < len(code) && code[j] == '.' && j+1 < len(code) && isNameStart(code[j+1]) {
				k := j + 1
				for k < len(code) && isIdentChar(code[k]) {
					k++
				}
				t.Idents = append(t.Idents, Ident{Name: string(code[i:k]), Start: offset + i, End: offset + k})
				i = k
				continue
			}
			name := string(code[i:j])
			if !keywords[name] {
				t.Idents = append(t.Idents, Ident{Name: name, Start: offset + i, End: offset + j})
			}
			i = j
		default:
			i++
		}
	}
	t.Decls = declarations(code)
}

// declarations finds `$a :=` and `$a, $b :=` (range) declarations.
func declarations(code []byte) []string {
	head, _, found := bytes.Cut(code, []byte(":="))
	if !found {
		return nil
	}
	var out []string
	for part := range bytes.SplitSeq(head, []byte(",")) {
		part = bytes.TrimSpace(part)
		if f := bytes.Fields(part); len(f) > 0 {
			last := f[len(f)-1]
			if len(last) > 1 && last[0] == '$' {
				out = append(out, string(last[1:]))
			}
		}
	}
	return out
}

// openTag scans `<name …>`: a component/engine tag becomes an Open token; a
// plain HTML tag stays text (with its actions lexed) and, for raw-text
// elements, swallows content up to its close tag.
func (s *scanner) openTag() error {
	start := s.pos()
	j := s.i + 1
	for j < len(s.src) && isTagNameChar(s.src[j]) {
		j++
	}
	name := string(s.src[s.i+1 : j])
	if !IsComponentName(name) {
		return s.plainTag(strings.ToLower(name))
	}
	s.flushText()
	begin := s.i
	s.advance(j - s.i)
	attrs, selfClosing, err := s.attributes(name)
	if err != nil {
		return err
	}
	tok := Token{Kind: Open, Pos: start, Raw: s.src[begin:s.i], Name: name, Attrs: attrs, SelfClosing: selfClosing}
	s.toks = append(s.toks, tok)
	if !selfClosing && s.opts.IsRaw != nil && s.opts.IsRaw(name, attrs) {
		return s.rawBody(name, start)
	}
	return nil
}

// rawBody captures bytes verbatim until the close tag spelled exactly like
// the open tag, then emits the Close token.
func (s *scanner) rawBody(name string, open Pos) error {
	closeTag := []byte("</" + name)
	bodyPos := s.pos()
	begin := s.i
	for k := s.i; k < len(s.src); k++ {
		if bytes.HasPrefix(s.src[k:], closeTag) {
			end := k + len(closeTag)
			e := end
			for e < len(s.src) && isSpace(s.src[e]) {
				e++
			}
			if e < len(s.src) && s.src[e] == '>' {
				s.toks = append(s.toks, Token{Kind: Text, Pos: bodyPos, Raw: s.src[begin:k], RawBody: true})
				s.advance(k - s.i)
				closePos := s.pos()
				cbegin := s.i
				s.advance(e + 1 - s.i)
				s.toks = append(s.toks, Token{Kind: Close, Pos: closePos, Raw: s.src[cbegin:s.i], Name: name})
				return nil
			}
		}
	}
	return s.errf(open, "<%s> raw body has no closing </%s>", name, name)
}

// attributes scans component-tag attributes up to `>` or `/>`.
func (s *scanner) attributes(tag string) ([]Attr, bool, error) {
	var attrs []Attr
	for {
		for s.i < len(s.src) && isSpace(s.src[s.i]) {
			s.advance(1)
		}
		if s.i >= len(s.src) {
			return nil, false, s.errf(s.pos(), "unclosed <%s> tag", tag)
		}
		switch {
		case s.hasPrefix("/>"):
			s.advance(2)
			return attrs, true, nil
		case s.src[s.i] == '>':
			s.advance(1)
			return attrs, false, nil
		case s.hasPrefix("{{"):
			return nil, false, s.errf(s.pos(), "{{ }} between attributes of <%s> — use a :prop instead, e.g. :open=\".X\"", tag)
		}
		ap := s.pos()
		j := s.i
		for j < len(s.src) && isAttrNameChar(s.src[j]) {
			j++
		}
		if j == s.i {
			if s.src[s.i] == '$' && s.i > 0 && s.src[s.i-1] == ':' {
				// A `:$todos` spelling is not brick's: the shorthand is
				// `:todos`, which already means the variable $todos.
				k := s.i + 1
				for k < len(s.src) && isAttrNameChar(s.src[k]) {
					k++
				}
				v := string(s.src[s.i+1 : k])
				return nil, false, s.errf(ap, "<%s>: write :%s, not :$%s — the shorthand already means the variable $%s", tag, v, v, v)
			}
			return nil, false, s.errf(ap, "unexpected %q in <%s> tag", s.src[s.i], tag)
		}
		a := Attr{Name: string(s.src[s.i:j]), Pos: ap}
		s.advance(j - s.i)
		for s.i < len(s.src) && isSpace(s.src[s.i]) {
			s.advance(1)
		}
		if s.i < len(s.src) && s.src[s.i] == '=' {
			s.advance(1)
			for s.i < len(s.src) && isSpace(s.src[s.i]) {
				s.advance(1)
			}
			v, q, err := s.attrValue(tag, a.Name)
			if err != nil {
				return nil, false, err
			}
			a.Value, a.Quote, a.HasValue = v, q, true
		}
		attrs = append(attrs, a)
	}
}

// attrValue reads a quoted or unquoted value; actions inside quotes are
// skipped as units so a `"` inside `{{ "x" }}` does not end the value.
func (s *scanner) attrValue(tag, attr string) (string, byte, error) {
	if s.i >= len(s.src) {
		return "", 0, s.errf(s.pos(), "missing value for %s on <%s>", attr, tag)
	}
	q := s.src[s.i]
	if q != '"' && q != '\'' {
		j := s.i
		for j < len(s.src) && !isSpace(s.src[j]) && s.src[j] != '>' && !(s.src[j] == '/' && j+1 < len(s.src) && s.src[j+1] == '>') {
			j++
		}
		v := string(s.src[s.i:j])
		s.advance(j - s.i)
		return v, 0, nil
	}
	vp := s.pos()
	j := s.i + 1
	for j < len(s.src) {
		if bytes.HasPrefix(s.src[j:], []byte("{{")) {
			k := bytes.Index(s.src[j:], []byte("}}"))
			if k < 0 {
				return "", 0, s.errf(vp, "unclosed {{ in %s on <%s>", attr, tag)
			}
			j += k + 2
			continue
		}
		if s.src[j] == q {
			if j > 0 && s.src[j-1] == '\\' {
				return "", 0, s.errf(vp, "\\%c inside %s on <%s> — HTML has no backslash escapes; write strings with backticks, e.g. :x=\"eq .S `blog`\"", q, attr, tag)
			}
			v := string(s.src[s.i+1 : j])
			s.advance(j + 1 - s.i)
			return v, q, nil
		}
		j++
	}
	return "", 0, s.errf(vp, "unclosed quote in %s on <%s>", attr, tag)
}

// plainTag scans an HTML tag as text: quoted attribute values and actions are
// skipped as units (no component inside a quoted value), and a
// raw-text element's content is text up to its close tag.
func (s *scanner) plainTag(lower string) error {
	start := s.pos()
	s.startText()
	s.advance(1 + len(lower))
	selfClosing := false
	for s.i < len(s.src) {
		c := s.src[s.i]
		switch {
		case s.hasPrefix("{{"):
			if err := s.action(); err != nil {
				return err
			}
			s.startText()
		case c == '"' || c == '\'':
			k := s.i + 1
			for k < len(s.src) && s.src[k] != c {
				if bytes.HasPrefix(s.src[k:], []byte("{{")) {
					s.advance(k - s.i)
					if err := s.action(); err != nil {
						return err
					}
					s.startText()
					k = s.i
					continue
				}
				k++
			}
			if k >= len(s.src) {
				return s.errf(start, "unclosed quote in <%s>", lower)
			}
			s.advance(k + 1 - s.i)
		case c == '>':
			selfClosing = s.i > 0 && s.src[s.i-1] == '/'
			s.advance(1)
			if rawTextElements[lower] && !selfClosing {
				return s.rawText(lower)
			}
			return nil
		default:
			s.advance(1)
		}
	}
	return s.errf(start, "unclosed <%s> tag", lower)
}

// rawText consumes a raw-text element's content (script, style, textarea,
// title): actions are lexed, components are not recognized.
func (s *scanner) rawText(lower string) error {
	closeTag := []byte("</" + lower)
	for s.i < len(s.src) {
		if s.hasPrefix("{{") {
			if err := s.action(); err != nil {
				return err
			}
			s.startText()
			continue
		}
		if len(s.src)-s.i >= len(closeTag) && bytes.EqualFold(s.src[s.i:s.i+len(closeTag)], closeTag) {
			return nil
		}
		s.advance(1)
	}
	return nil
}

// closeTag scans `</name>`: a component name becomes a Close token, anything
// else is text.
func (s *scanner) closeTag() error {
	j := s.i + 2
	for j < len(s.src) && isTagNameChar(s.src[j]) {
		j++
	}
	name := string(s.src[s.i+2 : j])
	if !IsComponentName(name) {
		s.startText()
		s.advance(j - s.i)
		return nil
	}
	s.flushText()
	start := s.pos()
	begin := s.i
	e := j
	for e < len(s.src) && isSpace(s.src[e]) {
		e++
	}
	if e >= len(s.src) || s.src[e] != '>' {
		return s.errf(start, "malformed closing tag </%s", name)
	}
	s.advance(e + 1 - s.i)
	s.toks = append(s.toks, Token{Kind: Close, Pos: start, Raw: s.src[begin:s.i], Name: name})
	return nil
}

func isSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' }

func isNameStart(c byte) bool { return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '_' }

func isIdentChar(c byte) bool { return isNameStart(c) || (c >= '0' && c <= '9') }

func isTagNameChar(c byte) bool { return isIdentChar(c) || c == '-' || c == ':' || c == '.' }

func isAttrNameChar(c byte) bool {
	return isIdentChar(c) || c == '-' || c == ':' || c == '.' || c == '@'
}
