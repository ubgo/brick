package format

import (
	"bytes"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/ubgo/brick/internal/lex"
)

// mode is what the rebuild may do to one line's leading whitespace.
type mode uint8

const (
	// modeKeep leaves the line exactly as written: it starts inside an
	// action, a raw body, a comment, a quoted value, <pre> and the like.
	modeKeep mode = iota
	// modeIndent gives the line the indentation of its nesting level.
	modeIndent
	// modeShift moves a line inside a multi-line tag (an attribute line) by
	// as much as the tag's first line moved, so its alignment is kept.
	modeShift
)

// line is one line of the body and what the walk learned about it.
type line struct {
	start   int
	mode    mode
	tagLine int // modeShift: the line the tag started on
	level   int // modeIndent: nesting level
	// prefix is true while the line holds only whitespace, closing tags and
	// template control actions; level is the lowest level seen in it, so
	// `</div>` and `{{ else }}` lines sit at the level they close to.
	prefix bool
	// strip: the newline ending this line is in a context where the
	// spaces before it carry no meaning, so they may be removed.
	strip bool
	delta int // new indentation width minus the old one
	// lastOpen is the offset of the last indenting element opened on this
	// line and still open at its end, or -1. Wrapping a tag before it
	// would move it to a later line, a level deeper on the next run.
	lastOpen int
}

// entry is one open element, component or template frame.
type entry struct {
	// names the element may be (one, except after a conditional whose
	// branches opened different tags); nil for a frame.
	names  []string
	html   bool // a plain HTML element (names compare without case)
	line   int  // the line it opened on
	off    int  // the offset of its '<'
	indent bool // its content is one level deeper
	frame  *frame
}

// frame is an open {{ if }}, {{ range }}, {{ with }}, {{ block }} or
// {{ define }}: the stack it opened on and the result of each branch so far.
type frame struct {
	act      lex.ActKind
	below    []*entry
	branches []branch
}

// branch is the stack a branch of a frame ended with: the entries below the
// frame (a branch may close an element opened before it) and above it.
type branch struct{ below, above []*entry }

// htmlState is where the plain-HTML scan is between tokens.
type htmlState uint8

const (
	stFlow    htmlState = iota // text and markup
	stTag                      // inside a plain tag, between < and >
	stComment                  // inside <!-- -->
	stRaw                      // inside script/style/textarea/title
)

// voidElements never have content or a closing tag.
var voidElements = map[string]bool{
	"area": true, "base": true, "br": true, "col": true, "embed": true, "hr": true, "img": true,
	"input": true, "link": true, "meta": true, "param": true, "source": true, "track": true, "wbr": true,
}

// rawTextElements hold text, not markup (lex treats them the same way).
var rawTextElements = map[string]bool{"script": true, "style": true, "textarea": true, "title": true}

// keepElements are whitespace-significant: every line inside is kept.
var keepElements = map[string]bool{"pre": true, "textarea": true, "listing": true, "plaintext": true, "xmp": true}

// flatElements do not indent their content: a page's skeleton is laid
// out flat, as the examples' layouts write it.
var flatElements = map[string]bool{"html": true, "head": true, "body": true}

// optionalEnd are the elements whose closing tag HTML lets an author omit;
// a parent's closing tag or a sibling's opening tag closes them.
var optionalEnd = map[string]bool{
	"li": true, "dt": true, "dd": true, "p": true, "tr": true, "td": true, "th": true,
	"option": true, "optgroup": true, "thead": true, "tbody": true, "tfoot": true, "colgroup": true, "rt": true, "rp": true,
}

// siblingCloses: opening the key closes an open element of any listed name.
var siblingCloses = map[string][]string{
	"li": {"li"}, "dt": {"dt", "dd"}, "dd": {"dt", "dd"}, "tr": {"tr", "td", "th"},
	"td": {"td", "th"}, "th": {"td", "th"}, "option": {"option"}, "p": {"p"},
}

// whiteSpaceKeep matches an inline style that makes whitespace significant.
var whiteSpaceKeep = []string{"pre", "pre-wrap", "pre-line", "break-spaces"}

// edit replaces body[start:end] with text; edits never cross a newline.
type edit struct {
	start, end int
	text       string
}

// walker walks a lexed body once, recording each line's mode and level and
// the attribute-spacing edits.
type walker struct {
	src  []byte
	toks []lex.Token
	act  []bool // act[i]: byte i belongs to a {{ }} action

	lines []line
	edits []edit

	stack []*entry
	// keeps are the open elements whose lines are kept as written (<pre>,
	// a brick: body, white-space: pre), tracked apart from the stack so
	// they still hold when the structure could not be followed.
	keeps    []keep
	st       htmlState
	quote    byte
	tagStart int // offset of the plain tag's '<'
	tagLine  int
	tagClose bool
	tagName  string
	rawName  string
	noformat bool

	// broken is why the structure could not be followed, at brokenLine
	// (1-based in the body); indentation is then left as written.
	broken     string
	brokenLine int

	// order is Options.AttrOrder; tags are the single-line tags normTag
	// saw, which a long line may be wrapped at (Options.LineLength).
	order []string
	tags  []tagInfo
	// inlineHTML (lower-cased) and inlineTag (as written) are
	// Options.Inline: elements whose content is not indented.
	inlineHTML, inlineTag map[string]bool
}

// inlineSets splits Options.Inline into the HTML names, compared without
// case as HTML does, and the component tags, compared exactly as the
// engine resolves them.
func inlineSets(names []string) (html, tag map[string]bool) {
	html, tag = map[string]bool{}, map[string]bool{}
	for _, n := range names {
		html[strings.ToLower(n)], tag[n] = true, true
	}
	return html, tag
}

// tagInfo is one tag that sits on a single line: where it is and the runs
// between its attributes, the only places a long line may break.
type tagInfo struct {
	start, end int
	seps       []span
}

// span is src[a:b].
type span struct{ a, b int }

func newWalker(src []byte, toks []lex.Token) *walker {
	w := &walker{src: src, toks: toks, act: make([]bool, len(src)+1)}
	for _, t := range toks {
		if t.Kind == lex.Action {
			for i := range t.Raw {
				w.act[t.Pos.Offset+i] = true
			}
		}
	}
	w.lines = []line{{start: 0, mode: modeIndent, prefix: true, lastOpen: -1}}
	return w
}

func (w *walker) cur() *line { return &w.lines[len(w.lines)-1] }

func (w *walker) fail(msg string, a ...any) {
	if w.broken == "" {
		w.broken = fmt.Sprintf(msg, a...)
		w.brokenLine = len(w.lines)
	}
}

// walk visits every token in order.
func (w *walker) walk() {
	for _, t := range w.toks {
		off := t.Pos.Offset
		switch t.Kind {
		case lex.Action:
			w.action(t, off)
		case lex.Open:
			w.open(t, off)
		case lex.Close:
			w.newlines(off, t.Raw, modeKeep)
			w.unkeep(t.Name)
			w.pop(t.Name, false)
			w.structural()
		case lex.Text:
			if t.RawBody {
				w.newlines(off, t.Raw, modeKeep)
			} else {
				w.text(off, t.Raw)
			}
		}
	}
	if w.st == stFlow && w.broken == "" && len(w.stack) > 0 {
		top := w.stack[len(w.stack)-1]
		if top.frame != nil {
			w.fail("a {{ }} block is not closed")
		} else if !top.html || !optionalEnd[top.names[0]] {
			w.fail("<%s> (line %d) is not closed", top.names[0], top.line+1)
		}
	}
}

// newlines starts a line, in mode m, after every newline in raw.
func (w *walker) newlines(off int, raw []byte, m mode) {
	for i, c := range raw {
		if c == '\n' {
			w.newline(off+i, m, 0)
		}
	}
}

// newline starts the line after the newline at p.
func (w *walker) newline(p int, m mode, tagLine int) {
	switch {
	case w.noformat:
		m = modeKeep
	case w.keeping() && m != modeKeep:
		m, tagLine = w.keepMode()
	}
	w.cur().strip = m != modeKeep && !w.keeping()
	w.cur().lastOpen = w.lastOpenOn(len(w.lines) - 1)
	l := line{start: p + 1, mode: m, tagLine: tagLine, lastOpen: -1}
	if m == modeIndent {
		l.level, l.prefix = w.level(), true
	}
	w.lines = append(w.lines, l)
}

// lastOpenOn is the offset of the last indenting element opened on line i
// that is still open, or -1.
func (w *walker) lastOpenOn(i int) int {
	last := -1
	for _, e := range w.stack {
		if e.frame == nil && e.indent && e.line == i {
			last = max(last, e.off)
		}
	}
	return last
}

// keep is an open element whose lines are kept, and how many elements of
// the same name are open inside it (so their closing tags do not end it).
type keep struct {
	name   string
	nested int
	// shift: the body is moved as a block with its opening line (a brick:
	// body) rather than kept in place (<pre>); line is that opening line.
	shift bool
	line  int
}

// keeping reports whether an open element keeps its lines as written.
func (w *walker) keeping() bool { return len(w.keeps) > 0 }

// keepMode is how a line inside the open keep elements moves: not at all
// when any of them is whitespace-significant, else as a block with the
// outermost one's opening line.
func (w *walker) keepMode() (mode, int) {
	for _, k := range w.keeps {
		if !k.shift {
			return modeKeep, 0
		}
	}
	return modeShift, w.keeps[0].line
}

// innerKeep is the innermost keep element named name, or nil.
func (w *walker) innerKeep(name string) *keep {
	for i := len(w.keeps) - 1; i >= 0; i-- {
		if strings.EqualFold(w.keeps[i].name, name) {
			return &w.keeps[i]
		}
	}
	return nil
}

// openKeep notes an opening tag: a new keep element when isKeep, else one
// more same-named element inside an open keep element.
func (w *walker) openKeep(name string, isKeep, shift bool, line int) {
	if isKeep {
		w.keeps = append(w.keeps, keep{name: name, shift: shift, line: line})
	} else if k := w.innerKeep(name); k != nil {
		k.nested++
	}
}

// unkeep notes a closing tag.
func (w *walker) unkeep(name string) {
	k := w.innerKeep(name)
	switch {
	case k == nil:
	case k.nested > 0:
		k.nested--
	default:
		i := len(w.keeps) - 1
		for !strings.EqualFold(w.keeps[i].name, name) {
			i--
		}
		w.keeps = slices.Delete(w.keeps, i, i+1)
	}
}

// level is the indentation level: the number of distinct lines that opened
// an indenting element still open, so `<ul><li>` on one line indents once.
func (w *walker) level() int {
	n, last := 0, -1
	for _, e := range w.stack {
		if e.frame == nil && e.indent && e.line != last {
			n++
			last = e.line
		}
	}
	return n
}

// content ends the line's structural prefix.
func (w *walker) content() { w.cur().prefix = false }

// structural lowers the line's level to the current one while the line is
// still in its prefix.
func (w *walker) structural() {
	if l := w.cur(); l.prefix && l.mode == modeIndent {
		l.level = min(l.level, w.level())
	}
}

// action handles one {{ }}: markers, frames, and lines inside it.
func (w *walker) action(t lex.Token, off int) {
	w.newlines(off, t.Raw, modeKeep)
	switch marker(t) {
	case MarkerOff:
		w.noformat = true
		return
	case MarkerOn:
		w.noformat = false
		return
	}
	if t.Act == lex.ActComment {
		return
	}
	control := t.Act != lex.ActOther
	if w.st == stFlow && !control {
		w.content()
	}
	switch {
	case t.Act.Opens():
		w.stack = append(w.stack, &entry{frame: &frame{act: t.Act, below: slices.Clone(w.stack)}, line: len(w.lines) - 1})
	case t.Act == lex.ActElse:
		w.elseFrame()
	case t.Act == lex.ActEnd:
		w.endFrame()
	}
	if control && w.st == stFlow {
		w.structural()
	}
}

// topFrame is the index of the innermost open frame, or -1.
func (w *walker) topFrame() int {
	for i := len(w.stack) - 1; i >= 0; i-- {
		if w.stack[i].frame != nil {
			return i
		}
	}
	return -1
}

// record saves the branch that just ended and returns the frame.
func (w *walker) record() (*frame, int) {
	b := w.topFrame()
	if b < 0 {
		w.fail("{{ else }} or {{ end }} without an open block")
		return nil, -1
	}
	f := w.stack[b].frame
	if b+1 < len(w.stack) && f.act != lex.ActIf && f.act != lex.ActWith {
		w.fail("an element opened inside a {{ range }} or {{ define }} is not closed in it")
	}
	f.branches = append(f.branches, branch{below: slices.Clone(w.stack[:b]), above: slices.Clone(w.stack[b+1:])})
	return f, b
}

func (w *walker) elseFrame() {
	if w.broken != "" {
		return
	}
	f, b := w.record()
	if f == nil {
		return
	}
	fe := w.stack[b]
	w.stack = append(slices.Clone(f.below), fe)
}

// endFrame closes a frame. Every branch must leave the same shape: the same
// number of entries below and above, so elements a conditional opens or
// closes in each branch (`{{ if }}<a>{{ else }}<div>{{ end }}`) line up.
// A frame without an {{ else }} has an implicit empty branch, so it must
// leave the stack as it found it.
func (w *walker) endFrame() {
	if w.broken != "" {
		return
	}
	f, _ := w.record()
	if f == nil {
		return
	}
	bs := f.branches
	if len(bs) == 1 || f.act == lex.ActRange || f.act == lex.ActDefine || f.act == lex.ActBlock {
		bs = append(bs, branch{below: f.below})
	}
	for _, b := range bs[1:] {
		if len(b.below) != len(bs[0].below) || len(b.above) != len(bs[0].above) {
			w.fail("the branches of a {{ }} block open or close different numbers of tags")
			return
		}
	}
	merged := make([]*entry, 0, len(bs[0].below)+len(bs[0].above))
	for k := range bs[0].below {
		merged = append(merged, mergeEntries(bs, func(b branch) *entry { return b.below[k] }))
	}
	for k := range bs[0].above {
		merged = append(merged, mergeEntries(bs, func(b branch) *entry { return b.above[k] }))
	}
	w.stack = merged
}

// mergeEntries is the entry at one position after a conditional: the same
// entry when every branch has it, otherwise one that may close as any of
// the branches' names.
func mergeEntries(bs []branch, at func(branch) *entry) *entry {
	first := at(bs[0])
	same := true
	for _, b := range bs[1:] {
		same = same && at(b) == first
	}
	if same {
		return first
	}
	m := *first
	m.names = slices.Clone(first.names)
	for _, b := range bs[1:] {
		e := at(b)
		for _, n := range e.names {
			if !slices.Contains(m.names, n) {
				m.names = append(m.names, n)
			}
		}
	}
	return &m
}

// open handles a component or engine opening tag.
func (w *walker) open(t lex.Token, off int) {
	w.content()
	tl := len(w.lines) - 1
	keep := w.noformat || w.keeping()
	var q byte
	for i := 0; i < len(t.Raw); i++ {
		c := t.Raw[i]
		switch {
		case c == '\n':
			m := modeShift
			if q != 0 {
				m = modeKeep
			}
			w.newline(off+i, m, tl)
		case q != 0 && c == '{' && i+1 < len(t.Raw) && t.Raw[i+1] == '{':
			if k := bytes.Index(t.Raw[i:], []byte("}}")); k > 0 {
				w.newlines(off+i, t.Raw[i:i+k], modeKeep)
				i += k + 1
			}
		case q != 0 && c == q:
			q = 0
		case q == 0 && (c == '"' || c == '\''):
			q = c
		}
	}
	if !keep {
		w.normTag(off, off+len(t.Raw)-1, 1+len(t.Name))
	}
	if !t.SelfClosing {
		w.openKeep(t.Name, strings.HasPrefix(t.Name, enginePfx), true, tl)
		w.push(&entry{names: []string{t.Name}, line: tl, off: off, indent: !w.inlineTag[t.Name]})
	}
}

func (w *walker) push(e *entry) {
	if w.broken == "" {
		w.stack = append(w.stack, e)
	}
}

// pop closes the element named name.
func (w *walker) pop(name string, html bool) {
	if w.broken != "" {
		return
	}
	match := func(e *entry) bool {
		return e.frame == nil && slices.ContainsFunc(e.names, func(n string) bool {
			return n == name || (html && e.html && strings.EqualFold(n, name))
		})
	}
	for {
		n := len(w.stack)
		if n == 0 {
			w.fail("</%s> closes nothing", name)
			return
		}
		top := w.stack[n-1]
		if top.frame != nil {
			// A branch closing an element opened before it: the frame's
			// branches must agree, which endFrame checks.
			if n >= 2 && match(w.stack[n-2]) {
				w.stack = append(w.stack[:n-2], top)
				return
			}
			w.fail("</%s> inside a {{ }} block closes a tag opened outside it", name)
			return
		}
		if match(top) {
			w.stack = w.stack[:n-1]
			return
		}
		if top.html && optionalEnd[top.names[0]] {
			w.stack = w.stack[:n-1]
			continue
		}
		w.fail("</%s> does not match <%s> (line %d)", name, top.names[0], top.line+1)
		return
	}
}

// text walks plain bytes: HTML tags, comments and raw-text elements.
func (w *walker) text(off int, b []byte) {
	for k := 0; k < len(b); k++ {
		c, p := b[k], off+k
		if c == '\n' {
			m, tl := w.textMode()
			w.newline(p, m, tl)
			continue
		}
		switch w.st {
		case stFlow:
			if c != '<' {
				if !isSpace(c) {
					w.content()
				}
				continue
			}
			rest := b[k:]
			switch {
			case bytes.HasPrefix(rest, []byte("<!--")):
				w.content()
				w.st = stComment
				k += 3
			case len(rest) > 2 && rest[1] == '/' && isNameStart(rest[2]):
				n := tagNameLen(rest[2:])
				w.st, w.tagStart, w.tagLine, w.tagClose, w.tagName = stTag, p, len(w.lines)-1, true, strings.ToLower(string(rest[2:2+n]))
				k += 1 + n
			case len(rest) > 1 && (isNameStart(rest[1]) || rest[1] == '!'):
				n := tagNameLen(rest[1:])
				if rest[1] == '!' {
					n = 1 + tagNameLen(rest[2:])
				}
				w.st, w.tagStart, w.tagLine, w.tagClose, w.tagName = stTag, p, len(w.lines)-1, false, strings.ToLower(string(rest[1:1+n]))
				// A sibling's opening tag closes an open <li>, <td>, <p>…
				// before it, so the line sits at the closed element's level.
				w.closeSibling(w.tagName)
				w.structural()
				w.content()
				k += n
			default:
				w.content()
			}
		case stTag:
			switch {
			case w.quote != 0:
				if c == w.quote {
					w.quote = 0
				}
			case c == '"' || c == '\'':
				w.quote = c
			case c == '>':
				w.endTag(p)
			}
		case stComment:
			if bytes.HasPrefix(b[k:], []byte("-->")) {
				w.st = stFlow
				k += 2
			}
		case stRaw:
			cl := "</" + w.rawName
			if c == '<' && len(b)-k >= len(cl) && strings.EqualFold(string(b[k:k+len(cl)]), cl) {
				w.st = stFlow
				k--
			}
		}
	}
}

// textMode is the mode of a line that starts at the current scan position.
func (w *walker) textMode() (mode, int) {
	switch w.st {
	case stTag:
		if w.quote != 0 {
			return modeKeep, 0
		}
		return modeShift, w.tagLine
	case stComment, stRaw:
		return modeKeep, 0
	}
	return modeIndent, 0
}

// endTag finishes a plain tag at its '>'.
func (w *walker) endTag(p int) {
	w.st = stFlow
	name := w.tagName
	if !w.noformat && !w.keeping() {
		w.normTag(w.tagStart, p, 1+len(name)+boolInt(w.tagClose))
	}
	if w.tagClose {
		w.unkeep(name)
		w.pop(name, true)
		w.structural()
		return
	}
	selfClosing := p > 0 && w.src[p-1] == '/'
	if strings.HasPrefix(name, "!") || voidElements[name] || selfClosing {
		return
	}
	w.openKeep(name, keepElements[name] || keepsWhitespace(w.src[w.tagStart:p]), false, w.tagLine)
	w.push(&entry{names: []string{name}, html: true, line: w.tagLine, off: w.tagStart, indent: !flatElements[name] && !w.inlineHTML[strings.ToLower(name)]})
	if rawTextElements[name] {
		w.st, w.rawName = stRaw, name
	}
}

// closeSibling pops the open element an opening tag of name implicitly
// closes (HTML's optional end tags: a new <li> ends the open <li>).
func (w *walker) closeSibling(name string) {
	closes, ok := siblingCloses[name]
	if !ok || w.broken != "" || len(w.stack) == 0 {
		return
	}
	if top := w.stack[len(w.stack)-1]; top.frame == nil && top.html && slices.Contains(closes, top.names[0]) {
		w.stack = w.stack[:len(w.stack)-1]
	}
}

// keepsWhitespace reports a tag whose inline style makes whitespace
// significant (white-space: pre and its kin).
func keepsWhitespace(tag []byte) bool {
	s := strings.ToLower(string(tag))
	i := strings.Index(s, "white-space")
	for i >= 0 {
		v := strings.TrimLeft(s[i+len("white-space"):], " \t\n:")
		for _, k := range whiteSpaceKeep {
			if strings.HasPrefix(v, k) {
				return true
			}
		}
		j := strings.Index(s[i+1:], "white-space")
		if j < 0 {
			break
		}
		i += 1 + j
	}
	return false
}

// normTag records the attribute-spacing edits for the tag at src[start:end]
// (end is its '>'), skipping the name (nameLen bytes after start), quoted
// values and actions. A run of spaces and tabs between attributes becomes
// one space; around '=' and before '>' it goes. A run touching an action, a
// line start or a line end is left alone: an action may print its own
// spacing, and line ends are the indentation's business.
//
// The single-space runs are the tag's separators: a single-line tag keeps
// them for wrapping (tagInfo), and reorders its attributes across them when
// Options.AttrOrder asks.
func (w *walker) normTag(start, end, nameLen int) {
	var q byte
	e0 := len(w.edits)
	var seps []span
	actionOutside, multiLine := false, false
	for i := start + nameLen; i < end; i++ {
		c := w.src[i]
		if c == '\n' {
			multiLine = true
		}
		if w.act[i] {
			if q == 0 {
				actionOutside = true
			}
			continue
		}
		if q != 0 {
			if c == q {
				q = 0
			}
			continue
		}
		if c == '"' || c == '\'' {
			q = c
			continue
		}
		if c != ' ' && c != '\t' {
			continue
		}
		j := i
		for j < end && (w.src[j] == ' ' || w.src[j] == '\t') && !w.act[j] {
			j++
		}
		a, b := i, j
		i = j - 1
		prev, next := w.src[a-1], w.src[b]
		if w.act[a-1] || w.act[b] || prev == '\n' || next == '\n' || next == '\r' {
			continue
		}
		run := string(w.src[a:b])
		want := " "
		switch {
		case next == '=' || prev == '=':
			want = ""
		case b == end && prev != '/':
			want = ""
		}
		if run != want {
			w.edits = append(w.edits, edit{start: a, end: b, text: want})
		}
		// The run before a self-closing "/>" is not between attributes.
		if want == " " && (next != '/' || b+1 != end) {
			seps = append(seps, span{a, b})
		}
	}
	if multiLine || len(seps) == 0 {
		return
	}
	w.tags = append(w.tags, tagInfo{start: start, end: end, seps: seps})
	if len(w.order) > 0 && !actionOutside {
		w.reorder(start, end, seps, e0)
	}
}

// reorder moves the attributes Options.AttrOrder names to the front of the
// tag whose separators are seps, keeping every other attribute's relative
// order. Each attribute slot is rewritten with another attribute's text,
// so no edit crosses a separator or a line. A tag that repeats an
// attribute name is left alone: HTML keeps the first, so reordering would
// change which value wins. The caller has excluded tags with an action
// between attributes ({{ if }}class="a"{{ end }}), whose order is logic.
func (w *walker) reorder(start, end int, seps []span, e0 int) {
	last := end
	if w.src[last-1] == '/' {
		last--
	}
	for last > start && (w.src[last-1] == ' ' || w.src[last-1] == '\t') {
		last--
	}
	slots := make([]span, len(seps))
	for k, sp := range seps {
		to := last
		if k+1 < len(seps) {
			to = seps[k+1].a
		}
		slots[k] = span{sp.b, to}
	}
	texts := make([]string, len(slots))
	names := map[string]bool{}
	for k, sl := range slots {
		texts[k] = applyEdits(w.src, w.edits[e0:], sl.a, sl.b)
		name := strings.ToLower(attrName(texts[k]))
		if names[name] {
			return
		}
		names[name] = true
	}
	perm := make([]int, len(slots))
	for k := range perm {
		perm[k] = k
	}
	slices.SortStableFunc(perm, func(x, y int) int { return attrRank(w.order, texts[x]) - attrRank(w.order, texts[y]) })
	if slices.IsSorted(perm) {
		return
	}
	kept := w.edits[:e0]
	for _, e := range w.edits[e0:] {
		if !slices.ContainsFunc(slots, func(sl span) bool { return e.start >= sl.a && e.start < sl.b }) {
			kept = append(kept, e)
		}
	}
	for k, sl := range slots {
		kept = append(kept, edit{start: sl.a, end: sl.b, text: texts[perm[k]]})
	}
	slices.SortFunc(kept[e0:], func(x, y edit) int { return x.start - y.start })
	w.edits = kept
}

// attrName is the name of an attribute written as text: up to '='.
func attrName(text string) string {
	if i := strings.IndexByte(text, '='); i >= 0 {
		return text[:i]
	}
	return text
}

// attrRank is the attribute's place in order: the index of the first
// entry that names it (a trailing '*' matches a prefix), or len(order) for
// an attribute order does not name.
func attrRank(order []string, text string) int {
	name := strings.ToLower(attrName(text))
	for k, o := range order {
		o = strings.ToLower(o)
		if p, ok := strings.CutSuffix(o, "*"); ok {
			if strings.HasPrefix(name, p) {
				return k
			}
		} else if name == o {
			return k
		}
	}
	return len(order)
}

// applyEdits is src[from:to] with the edits that start inside it applied;
// edits are sorted by start and never cross from or to.
func applyEdits(src []byte, edits []edit, from, to int) string {
	var b strings.Builder
	for _, e := range edits {
		if e.start < from || e.start >= to {
			continue
		}
		b.Write(src[from:e.start])
		b.WriteString(e.text)
		from = e.end
	}
	if from < to {
		b.Write(src[from:to])
	}
	return b.String()
}

// rebuild writes the formatted body.
//
// With lineLength > 0, a re-indented line longer than lineLength runes
// breaks every tag on it that sits whole on the line and has two or more
// separators: each separator becomes a newline and the line's indentation
// plus one level, so attributes stand one per line and '>' stays on the
// last. Only an existing run inside a tag changes, so what the page
// renders does not; on the next run those lines are attribute lines of a
// multi-line tag, which move with it, so the result is stable.
func (w *walker) rebuild(indent string, lineLength int) []byte {
	var out bytes.Buffer
	ei := 0
	// copyEdited writes src[from:to] with the edits inside it applied.
	copyEdited := func(from, to int) string {
		var b strings.Builder
		for from < to {
			for ei < len(w.edits) && w.edits[ei].start < from {
				ei++
			}
			if ei < len(w.edits) && w.edits[ei].start < to {
				e := w.edits[ei]
				b.Write(w.src[from:e.start])
				b.WriteString(e.text)
				from = e.end
				ei++
				continue
			}
			b.Write(w.src[from:to])
			break
		}
		return b.String()
	}
	reindent := w.broken == ""
	blankRun := 0
	for i := range w.lines {
		l := &w.lines[i]
		end := len(w.src)
		if i+1 < len(w.lines) {
			end = w.lines[i+1].start
		}
		nl := end > l.start && w.src[end-1] == '\n'
		textEnd := end
		if nl {
			textEnd--
		}
		lead := l.start
		for lead < textEnd && (w.src[lead] == ' ' || w.src[lead] == '\t') {
			lead++
		}
		oldLead := string(w.src[l.start:lead])
		var s string
		switch l.mode {
		case modeKeep:
			s = copyEdited(l.start, textEnd)
		default:
			rest := copyEdited(lead, textEnd)
			if l.strip && nl {
				rest = strings.TrimRight(rest, " \t")
			}
			switch {
			case rest == "" || rest == "\r":
				s = rest
			case l.mode == modeIndent && reindent:
				newLead := strings.Repeat(indent, l.level)
				l.delta = len(newLead) - len(oldLead)
				s = newLead + rest
				if lineLength > 0 && utf8.RuneCountInString(strings.TrimRight(s, "\r")) > lineLength {
					if wrapped, ok := w.wrap(lead, textEnd, l.lastOpen, newLead+indent, l.strip && nl); ok {
						s = newLead + wrapped
					}
				}
			case l.mode == modeShift && reindent && !strings.Contains(oldLead, "\t"):
				newLead := strings.Repeat(" ", max(0, len(oldLead)+w.lines[l.tagLine].delta))
				l.delta = len(newLead) - len(oldLead)
				s = newLead + rest
			default:
				s = oldLead + rest
			}
		}
		// Runs of blank lines in markup become one blank line.
		if l.mode == modeIndent && (s == "" || s == "\r") && nl {
			blankRun++
			if blankRun > 1 {
				continue
			}
		} else {
			blankRun = 0
		}
		out.WriteString(s)
		if nl {
			out.WriteByte('\n')
		}
	}
	return out.Bytes()
}

// wrap is src[from:to] with the separators of every tag lying whole in it
// (with two or more separators) replaced by a newline and lead, and the
// line's other edits applied; ok is false when no tag can break. A tag
// before lastOpen (the line's last element left open) does not break:
// that element would land on a later line and indent its content one
// level deeper next time, so the layout would not be stable.
func (w *walker) wrap(from, to, lastOpen int, lead string, strip bool) (string, bool) {
	var breaks []edit
	for _, t := range w.tags {
		if t.start < from || t.end >= to || len(t.seps) < 2 || t.end < lastOpen {
			continue
		}
		for _, sp := range t.seps {
			breaks = append(breaks, edit{start: sp.a, end: sp.b, text: "\n" + lead})
		}
	}
	if len(breaks) == 0 {
		return "", false
	}
	var line []edit
	for _, e := range w.edits {
		if e.start >= from && e.start < to && !slices.ContainsFunc(breaks, func(b edit) bool { return b.start == e.start }) {
			line = append(line, e)
		}
	}
	line = append(line, breaks...)
	slices.SortFunc(line, func(x, y edit) int { return x.start - y.start })
	s := applyEdits(w.src, line, from, to)
	if strip {
		s = strings.TrimRight(s, " \t")
	}
	return s, true
}

func tagNameLen(b []byte) int {
	n := 0
	for n < len(b) && isTagNameChar(b[n]) {
		n++
	}
	return n
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func isSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' }

func isNameStart(c byte) bool { return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') }

func isTagNameChar(c byte) bool {
	return isNameStart(c) || (c >= '0' && c <= '9') || c == '-' || c == ':' || c == '.' || c == '_'
}
