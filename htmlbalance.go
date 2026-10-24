package brick

import (
	"fmt"
	"slices"
	"strings"

	"github.com/ubgo/brick/internal/frontmatter"
	"github.com/ubgo/brick/internal/lex"
	"github.com/ubgo/brick/internal/tree"
)

// HTML balance warnings.
//
// Why it exists: brick passes plain HTML through as opaque bytes (it never
// parses HTML into a tree), and html/template tracks escaping contexts, not
// elements, so a `<div>` that is never closed renders without a word and the
// browser repairs it somewhere surprising. This pass walks the scanner's
// token stream once per file, keeps a stack of open elements and reports an
// unclosed or stray tag with its file:line.
//
// Invariants:
//   - Warning-only. It never fails a check and never changes output; it reads
//     the source, it is not part of the compile pipeline.
//   - No false positives on valid markup, so it is lenient by design: void
//     elements and `/>` close themselves, optional-close elements (p, li, td,
//     tr, option, …) may be left open, a dynamic name (`<{{ tag .L }}>`)
//     matches any close, and a problem is reported only when it holds on
//     every way the template can run.
//   - `{{ if }}`/`{{ else }}` branches are followed separately: each branch
//     starts from the stack at the `if`, and the paths that survive meet at
//     `{{ end }}`. That is what keeps the conditional-wrapper idiom
//     (`{{ if .Href }}<a>{{ else }}<span>{{ end }}` … the matching closes)
//     quiet, since a path that fails is dropped when another one succeeds.
//   - A `{{ range }}` body, a `{{ define }}`/`{{ block }}` body and a component
//     body are scopes of their own: what opens inside must close inside.
//   - Anything the pass cannot follow (a block keyword inside a tag, a frame
//     that does not line up) makes it give up on the file silently: a missed
//     warning costs nothing, a wrong one teaches people to ignore warnings.

// balanceMaxPaths caps how many branch paths are followed at once. Past it the
// scope goes quiet rather than guess: ten independent ifs that each leave a
// wrapper open would otherwise mean a thousand paths.
const balanceMaxPaths = 32

// optionalClose elements may omit their close tag (§13.1.2.4); the parser
// closes them implicitly, so an unclosed one is not a mistake.
var optionalClose = setOf("p li dt dd tr td th thead tbody tfoot option optgroup colgroup caption rb rt rtc rp html head body")

// rawTextTags hold text, not markup: the pass skips to their close tag, as
// the scanner does.
var rawTextTags = setOf("script style textarea title")

// dynamicTag stands for an element whose name is computed (`<{{ tag .L }}>`):
// it matches any close tag, and a computed close matches any element.
const dynamicTag = "*"

func setOf(words string) map[string]bool {
	m := map[string]bool{}
	for w := range strings.FieldsSeq(words) {
		m[w] = true
	}
	return m
}

// htmlBalance returns the balance warnings for one file's source. A file
// that does not scan returns none: the compile reports that error itself.
func (e *Engine) htmlBalance(p string, src []byte) []Diagnostic {
	doc, err := frontmatter.Parse(src)
	if err != nil {
		return nil
	}
	toks, err := lex.Scan(doc.Body, lex.Options{IsRaw: e.balanceIsRaw(p, doc.Body)})
	if err != nil {
		return nil
	}
	return checkBalance(p, doc.BodyLine, toks)
}

// balanceIsRaw mirrors the compile's raw-body rule, so a raw component's
// body (code, markdown) is never read as markup.
func (e *Engine) balanceIsRaw(p string, body []byte) func(string, []lex.Attr) bool {
	aliases := map[string]string{}
	if m, err := e.calleeMeta(p); err == nil {
		for _, u := range m.use {
			if u.Kind == useComponent {
				aliases[u.Key] = u.Path
			}
		}
	}
	localNames := map[string]bool{}
	for _, mm := range localNameRE.FindAllSubmatch(body, -1) {
		localNames[string(mm[1])] = true
	}
	return func(name string, attrs []lex.Attr) bool {
		switch name {
		case tree.TagScript, tree.TagClient, tree.TagStyle:
			return true
		case tree.TagProp:
			return slices.ContainsFunc(attrs, func(a lex.Attr) bool { return a.Name == tree.AttrRaw })
		}
		if localNames[name] || strings.HasPrefix(name, "brick:") {
			return false
		}
		path, ok := e.lookupPath(name, aliases)
		if !ok || path == "" {
			return name == markdownTag && e.opts.Markdown != nil
		}
		cm, err := e.calleeMeta(path)
		return err == nil && cm.raw
	}
}

// openEl is one open element on a path's stack, with where it opened.
type openEl struct {
	name      string
	line, col int
}

// balPath is one way the template can run so far: its stack of open elements.
type balPath []openEl

// frameKind says what a balance frame was opened by.
type frameKind int

const (
	frameBranch    frameKind = iota // if / with: branches meet at end
	frameScope                      // range / define / block: own scope
	frameComponent                  // a component or brick: tag body
)

type balFrame struct {
	kind    frameKind
	entry   []balPath // branch: the paths at the if
	results []balPath // branch: the paths each finished branch left
	hasElse bool
	saved   []balPath // scope: the paths outside, restored at end
	muted   bool      // scope: the outer scope's muted flag
}

// lexState is the HTML tokenizer's state; it carries across tokens because an
// action can sit inside a tag (`<a href="{{ .U }}">`).
type lexState int

const (
	sData     lexState = iota
	sLt                // after `<`
	sLtSlash           // after `</`
	sName              // in a tag name
	sTag               // between attributes
	sQuote             // in a quoted attribute value
	sBang              // after `<!`
	sBangDash          // after `<!-`
	sComment           // in `<!-- … -->`
	sDecl              // in `<!DOCTYPE …>`, `<? … >` or a bogus close
	sRaw               // in a raw-text element's content
)

type balancer struct {
	file  string
	line0 int

	paths  []balPath
	frames []balFrame
	muted  bool
	broken bool

	diags []Diagnostic
	seen  map[string]bool

	// tokenizer
	st               lexState
	name             strings.Builder
	closing, dynamic bool
	tagLine, tagCol  int
	quote, lastNonSp byte
	dashes           int
	rawTarget        string // "</script" while in sRaw
	rawMatch         int
	closingRaw       bool // the tag being read closes a raw-text element
	line, col        int
	opaque           int // blocks open inside a tag, comment or raw text
}

func checkBalance(file string, line0 int, toks []lex.Token) []Diagnostic {
	b := &balancer{file: file, line0: line0, paths: []balPath{{}}, seen: map[string]bool{}}
	for _, t := range toks {
		if b.broken {
			return nil
		}
		switch t.Kind {
		case lex.Text:
			if !t.RawBody {
				b.text(t)
			}
		case lex.Action:
			b.action(t)
		case lex.Open:
			b.st = sData
			if !t.SelfClosing {
				b.pushScope(frameComponent)
			}
		case lex.Close:
			b.st = sData
			b.popScope(frameComponent)
		}
	}
	if b.broken || len(b.frames) > 0 {
		return nil
	}
	b.endScope()
	return b.diags
}

func (b *balancer) text(t lex.Token) {
	b.line, b.col = t.Pos.Line, t.Pos.Col
	for _, c := range t.Raw {
		b.char(c)
		if c == '\n' {
			b.line++
			b.col = 1
		} else {
			b.col++
		}
	}
}

func isTagNameByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == ':' || c == '.' || c == '_'
}

func isLetter(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }

func (b *balancer) startTag(closing bool) {
	b.st = sName
	b.closing, b.dynamic, b.closingRaw = closing, false, false
	b.name.Reset()
	b.lastNonSp = 0
}

func (b *balancer) char(c byte) {
	switch b.st {
	case sData:
		if c == '<' {
			b.st = sLt
			b.tagLine, b.tagCol = b.line, b.col
		}
	case sLt:
		switch {
		case isLetter(c):
			b.startTag(false)
			b.name.WriteByte(c)
		case c == '/':
			b.st = sLtSlash
		case c == '!':
			b.st = sBang
		case c == '?':
			b.st = sDecl
		case c == '<':
			b.tagLine, b.tagCol = b.line, b.col
		default:
			b.st = sData
		}
	case sLtSlash:
		if isLetter(c) {
			b.startTag(true)
			b.name.WriteByte(c)
		} else {
			b.st = sDecl
			b.char(c)
		}
	case sName:
		if isTagNameByte(c) {
			b.name.WriteByte(c)
			return
		}
		b.st = sTag
		b.char(c)
	case sTag:
		switch {
		case c == '"' || c == '\'':
			b.st, b.quote = sQuote, c
		case c == '>':
			b.st = sData
			b.tag(b.lastNonSp == '/')
		case c != ' ' && c != '\t' && c != '\n' && c != '\r' && c != '\f':
			b.lastNonSp = c
		}
	case sQuote:
		if c == b.quote {
			b.st = sTag
			b.lastNonSp = c
		}
	case sBang:
		if c == '-' {
			b.st = sBangDash
		} else {
			b.st = sDecl
			b.char(c)
		}
	case sBangDash:
		if c == '-' {
			b.st, b.dashes = sComment, 0
		} else {
			b.st = sDecl
			b.char(c)
		}
	case sComment:
		switch {
		case c == '-':
			b.dashes++
		case c == '>' && b.dashes >= 2:
			b.st = sData
		default:
			b.dashes = 0
		}
	case sDecl:
		if c == '>' {
			b.st = sData
		}
	case sRaw:
		lc := c
		if lc >= 'A' && lc <= 'Z' {
			lc += 'a' - 'A'
		}
		if lc == b.rawTarget[b.rawMatch] {
			if b.rawMatch == 0 {
				b.tagLine, b.tagCol = b.line, b.col
			}
			b.rawMatch++
			if b.rawMatch == len(b.rawTarget) {
				// The close tag of the raw-text element: read it as a tag,
				// with no effect on the stack.
				b.startTag(true)
				b.closingRaw = true
				b.name.WriteString(b.rawTarget[2:])
			}
			return
		}
		b.rawMatch = 0
		if c == '<' {
			b.tagLine, b.tagCol = b.line, b.col
			b.rawMatch = 1
		}
	}
}

// action handles one template action. Inside a tag, a comment or raw text it
// is opaque; in a tag name it makes the name dynamic; in text, a block
// keyword moves the frames.
func (b *balancer) action(t lex.Token) {
	switch b.st {
	case sLt, sLtSlash:
		b.startTag(b.st == sLtSlash)
		b.dynamic = true
		b.st = sTag
		return
	case sName:
		// `<td{{ with .A }} class="x"{{ end }}>`: the name is complete and
		// the action belongs to the attributes. (`<h{{ .N }}>` closes as
		// `</h{{ .N }}>`, so both read as h and still match.)
		b.st = sTag
	}
	switch b.st {
	case sData:
		if b.opaque > 0 && (t.Act == lex.ActElse || t.Act == lex.ActEnd) {
			b.broken = true
			return
		}
	default:
		// Inside a tag, a comment or raw text a block is opaque, which is
		// sound only while it opens and ends there too; one that crosses
		// the boundary makes the pass give up on this file rather than
		// guess which branch a tag is in.
		switch {
		case t.Act.Opens():
			b.opaque++
		case t.Act == lex.ActElse && b.opaque == 0:
			b.broken = true
		case t.Act == lex.ActEnd:
			if b.opaque == 0 {
				b.broken = true
			}
			b.opaque--
		}
		return
	}
	switch t.Act {
	case lex.ActIf, lex.ActWith:
		b.frames = append(b.frames, balFrame{kind: frameBranch, entry: clonePaths(b.paths)})
	case lex.ActRange, lex.ActDefine, lex.ActBlock:
		b.pushScope(frameScope)
	case lex.ActElse:
		if len(b.frames) == 0 {
			b.broken = true
			return
		}
		f := &b.frames[len(b.frames)-1]
		switch f.kind {
		case frameBranch:
			f.results = append(f.results, b.paths...)
			f.hasElse = true
			b.paths = clonePaths(f.entry)
		case frameScope:
			// A range's else runs instead of the body: its own scope too.
			b.endScope()
			b.paths, b.muted = []balPath{{}}, false
		default:
			b.broken = true
		}
	case lex.ActEnd:
		if len(b.frames) == 0 {
			b.broken = true
			return
		}
		f := b.frames[len(b.frames)-1]
		switch f.kind {
		case frameBranch:
			b.frames = b.frames[:len(b.frames)-1]
			res := append(f.results, b.paths...)
			if !f.hasElse {
				res = append(res, f.entry...)
			}
			b.setPaths(res)
		case frameScope:
			b.popScope(frameScope)
		default:
			b.broken = true
		}
	}
}

func (b *balancer) pushScope(k frameKind) {
	b.frames = append(b.frames, balFrame{kind: k, saved: b.paths, muted: b.muted})
	b.paths, b.muted = []balPath{{}}, false
}

func (b *balancer) popScope(k frameKind) {
	if len(b.frames) == 0 || b.frames[len(b.frames)-1].kind != k {
		b.broken = true
		return
	}
	f := b.frames[len(b.frames)-1]
	b.frames = b.frames[:len(b.frames)-1]
	b.endScope()
	b.paths, b.muted = f.saved, f.muted
}

// endScope reports every element still open on every path at a scope's end.
func (b *balancer) endScope() {
	if b.muted || len(b.paths) == 0 {
		return
	}
	for _, el := range b.paths[0] {
		if optionalClose[el.name] || el.name == dynamicTag {
			continue
		}
		everywhere := true
		for _, p := range b.paths[1:] {
			if !slices.Contains(p, el) {
				everywhere = false
				break
			}
		}
		if everywhere {
			b.report(el.line, el.col, fmt.Sprintf("<%s> is never closed", el.name))
		}
	}
}

// tag applies one finished tag to every path.
func (b *balancer) tag(selfClosing bool) {
	name := strings.ToLower(b.name.String())
	if b.dynamic {
		name = dynamicTag
	}
	if b.closingRaw {
		return
	}
	if !b.closing {
		if voidElements[name] || selfClosing {
			return
		}
		if rawTextTags[name] {
			b.st, b.rawTarget, b.rawMatch = sRaw, "</"+name, 0
			return
		}
		el := openEl{name: name, line: b.tagLine, col: b.tagCol}
		for i := range b.paths {
			b.paths[i] = append(b.paths[i], el)
		}
		return
	}
	if voidElements[name] {
		return
	}
	var ok, failed []balPath
	var firstIssue string
	for _, p := range b.paths {
		np, issue := closeOn(p, name, b.line0)
		if issue == "" {
			ok = append(ok, np)
			continue
		}
		if firstIssue == "" {
			firstIssue = issue
		}
		failed = append(failed, np)
	}
	if len(ok) > 0 {
		b.setPaths(ok)
		return
	}
	b.report(b.tagLine, b.tagCol, firstIssue)
	b.setPaths(failed)
}

// closeOn applies a close tag to one path. Optional-close elements above the
// match are closed silently; anything else above it, or no match at all, is
// an issue. line0 turns body lines into file lines for the message.
func closeOn(p balPath, name string, line0 int) (balPath, string) {
	var blocker *openEl
	for k := len(p) - 1; k >= 0; k-- {
		e := p[k]
		if e.name == name || e.name == dynamicTag || name == dynamicTag {
			out := slices.Clone(p[:k])
			if blocker != nil {
				return out, fmt.Sprintf("</%s> closes <%s> (line %d), which is still open", name, blocker.name, blocker.line+line0-1)
			}
			return out, ""
		}
		if blocker == nil && !optionalClose[e.name] {
			blocker = &p[k]
		}
	}
	return p, fmt.Sprintf("</%s> has no open <%s> to close", name, name)
}

// setPaths keeps the distinct paths, muting the scope past the cap.
func (b *balancer) setPaths(ps []balPath) {
	out := make([]balPath, 0, len(ps))
	for _, p := range ps {
		if !slices.ContainsFunc(out, func(q balPath) bool { return slices.Equal(p, q) }) {
			out = append(out, p)
		}
	}
	if len(out) > balanceMaxPaths {
		out, b.muted = out[:balanceMaxPaths], true
	}
	b.paths = out
}

func clonePaths(ps []balPath) []balPath {
	out := make([]balPath, len(ps))
	for i, p := range ps {
		out[i] = slices.Clone(p)
	}
	return out
}

func (b *balancer) report(line, col int, msg string) {
	if b.muted {
		return
	}
	d := Diagnostic{File: b.file, Line: line + b.line0 - 1, Col: col, Severity: SeverityWarning, Code: CodeHTMLBalance, Message: msg}
	if k := d.String(); !b.seen[k] {
		b.seen[k] = true
		b.diags = append(b.diags, d)
	}
}
