// Package tree turns a lex token stream into a tree of text, actions and
// elements (components and `brick:` tags), enforcing the structural rules the
// transpiler relies on.
//
// Why it exists: the transpiler hoists every component body into its own Go
// `define`. That is only valid if each body is balanced with respect
// to template actions — a tag opened in one `{{ if }}` branch and closed in
// another would split a block across two defines. This package is
// the single place those rules are checked, so later stages can assume a
// well-formed tree.
package tree

import (
	"fmt"
	"slices"
	"strings"

	"github.com/ubgo/brick/internal/lex"
)

// Engine tag names (the reserved `brick:` namespace).
const (
	TagProp      = "brick:prop"
	TagComponent = "brick:component"
	TagScript    = "brick:script"
	TagClient    = "brick:client"
	TagStyle     = "brick:style"
	TagBoundary  = "brick:boundary"
	TagRender    = "brick:render"
)

// Attributes of <brick:prop>: name says which prop the block fills, and
// raw makes its body verbatim text (no {{ }} actions, no component tags),
// for a prop holding code or markup shown as-is.
const (
	AttrName = "name"
	AttrRaw  = "raw"
)

// EngineTags is every reserved `brick:` tag; any other `<brick:…>` is an error.
var EngineTags = []string{TagProp, TagComponent, TagScript, TagClient, TagStyle, TagBoundary, TagRender}

// topLevelOnly tags must appear at the top level of a file, outside every
// element and every template block.
var topLevelOnly = map[string]bool{TagComponent: true, TagScript: true, TagClient: true, TagStyle: true}

// Node is Text, Action or *Element.
type Node interface{ node() }

// Text is opaque bytes. RawBody marks a verbatim raw body (no actions).
type Text struct {
	Raw     []byte
	Pos     lex.Pos
	RawBody bool
}

// Action is one template action, kept whole for the transpiler.
type Action struct{ Tok lex.Token }

// Element is a component or engine tag with its body.
//
// Children holds the body excluding `<brick:prop>` blocks, which are lifted
// into Props (in source order). For `<brick:prop>` itself, Children is the
// block's content.
type Element struct {
	Name        string
	Attrs       []lex.Attr
	Pos         lex.Pos
	SelfClosing bool
	OpenRaw     []byte
	CloseRaw    []byte
	Children    []Node
	Props       []*Element
}

func (Text) node()     {}
func (Action) node()   {}
func (*Element) node() {}

// Attr returns the value of a literal attribute and whether it is present.
func (e *Element) Attr(name string) (lex.Attr, bool) {
	for _, a := range e.Attrs {
		if a.Name == name {
			return a, true
		}
	}
	return lex.Attr{}, false
}

// File is the tree of one file body.
type File struct {
	// Nodes is the top-level body, excluding the lifted top-level blocks below.
	Nodes []Node
	// Locals are `<brick:component>` definitions, in source order.
	Locals []*Element
	// Blocks are top-level `<brick:script>`, `<brick:client>` and
	// `<brick:style>` elements, in source order.
	Blocks []*Element
}

// Error is a structural error with its position.
type Error struct {
	Pos lex.Pos
	Msg string
}

// Error renders line:col: message; the compiler adds the file and the
// body's line offset when it reports it.
func (e *Error) Error() string { return e.Pos.String() + ": " + e.Msg }

func errAt(p lex.Pos, format string, a ...any) *Error {
	return &Error{Pos: p, Msg: fmt.Sprintf(format, a...)}
}

// frame is an open template block (`if`, `range`, `with`, `block`, `define`).
type frame struct {
	act    lex.ActKind
	pos    lex.Pos
	branch int
}

// open is an element being built, remembering the template frame it opened in.
type open struct {
	el         *Element
	frameDepth int
	frame      *frame
	branch     int
}

// Build builds and validates the tree.
func Build(toks []lex.Token) (*File, error) {
	b := &builder{file: &File{}}
	for _, t := range toks {
		if err := b.add(t); err != nil {
			return nil, err
		}
	}
	if n := len(b.stack); n > 0 {
		o := b.stack[n-1]
		return nil, errAt(o.el.Pos, "<%s> has no closing </%s>", o.el.Name, o.el.Name)
	}
	if n := len(b.frames); n > 0 {
		f := b.frames[n-1]
		return nil, errAt(f.pos, "this {{ … }} block has no {{ end }}")
	}
	return b.file, nil
}

type builder struct {
	file   *File
	stack  []*open
	frames []*frame
}

func (b *builder) top() *open {
	if len(b.stack) == 0 {
		return nil
	}
	return b.stack[len(b.stack)-1]
}

// appendNode adds n to the innermost element's children or the file.
func (b *builder) appendNode(n Node) {
	if o := b.top(); o != nil {
		o.el.Children = append(o.el.Children, n)
		return
	}
	b.file.Nodes = append(b.file.Nodes, n)
}

func (b *builder) add(t lex.Token) error {
	switch t.Kind {
	case lex.Text:
		b.appendNode(Text{Raw: t.Raw, Pos: t.Pos, RawBody: t.RawBody})
	case lex.Action:
		if err := b.action(t); err != nil {
			return err
		}
		b.appendNode(Action{Tok: t})
	case lex.Open:
		return b.openEl(t)
	case lex.Close:
		return b.closeEl(t)
	}
	return nil
}

// action maintains the frame stack and enforces T1 across element bodies.
func (b *builder) action(t lex.Token) error {
	o := b.top()
	minDepth := 0
	if o != nil {
		minDepth = o.frameDepth
	}
	switch {
	case t.Act == lex.ActDefine && o != nil:
		return errAt(t.Pos, "{{ define }} inside <%s> — move it to the top level of the file", o.el.Name)
	case t.Act.Opens():
		b.frames = append(b.frames, &frame{act: t.Act, pos: t.Pos})
	case t.Act == lex.ActElse:
		if len(b.frames) <= minDepth {
			return b.crossing(t, o, "{{ else }}")
		}
		b.frames[len(b.frames)-1].branch++
	case t.Act == lex.ActEnd:
		if len(b.frames) <= minDepth {
			return b.crossing(t, o, "{{ end }}")
		}
		b.frames = b.frames[:len(b.frames)-1]
	case t.Act == lex.ActBreak || t.Act == lex.ActContinue:
		// A body or an author define is executed on its own, wrapped in a
		// one-item range, so a break with no range of its own would silently
		// leave that wrapper instead of failing.
		for i, f := range slices.Backward(b.frames) {
			if f.act == lex.ActDefine || f.act == lex.ActBlock {
				break
			}
			if f.act == lex.ActRange {
				if i < minDepth {
					return errAt(t.Pos, "{{ %s }} inside <%s> cannot leave the {{ range }} at %s, which is outside the component", actWord(t.Act), o.el.Name, f.pos)
				}
				return nil
			}
		}
		return errAt(t.Pos, "{{ %s }} outside any {{ range }}", actWord(t.Act))
	}
	return nil
}

func actWord(k lex.ActKind) string {
	if k == lex.ActBreak {
		return "break"
	}
	return "continue"
}

func (b *builder) crossing(t lex.Token, o *open, what string) error {
	if o == nil {
		return errAt(t.Pos, "%s without a matching block", what)
	}
	return errAt(t.Pos, "%s closes a block opened outside <%s> (line %d) — a component tag cannot straddle template branches; pass a flag instead, e.g. :boxed=\".Boxed\"", what, o.el.Name, o.el.Pos.Line)
}

func (b *builder) openEl(t lex.Token) error {
	name := t.Name
	if strings.HasPrefix(name, "brick:") && !isEngineTag(name) {
		return errAt(t.Pos, "<%s> is reserved — the brick: namespace only has %s", name, strings.Join(EngineTags, ", "))
	}
	o := b.top()
	if topLevelOnly[name] && (o != nil || len(b.frames) > 0) {
		return errAt(t.Pos, "<%s> must be at the top level of the file, outside every tag and {{ }} block", name)
	}
	if name == TagProp {
		if err := b.checkPropPlacement(t, o); err != nil {
			return err
		}
	}
	el := &Element{Name: name, Attrs: t.Attrs, Pos: t.Pos, SelfClosing: t.SelfClosing, OpenRaw: t.Raw}
	if t.SelfClosing {
		return b.attach(el, o)
	}
	var fr *frame
	branch := 0
	if n := len(b.frames); n > 0 {
		fr = b.frames[n-1]
		branch = fr.branch
	}
	b.stack = append(b.stack, &open{el: el, frameDepth: len(b.frames), frame: fr, branch: branch})
	return nil
}

// checkPropPlacement enforces C9: a block prop sits directly in its
// component's body, outside any template block relative to that component.
func (b *builder) checkPropPlacement(t lex.Token, o *open) error {
	if o == nil || o.el.Name == TagProp || topLevelOnly[o.el.Name] {
		return errAt(t.Pos, "<brick:prop> must be directly inside a component")
	}
	if len(b.frames) != o.frameDepth {
		return errAt(t.Pos, "<brick:prop> inside a {{ }} block of <%s> — put the condition inside the block instead", o.el.Name)
	}
	name := ""
	for _, a := range t.Attrs {
		if a.Name == AttrName {
			name = a.Value
		}
	}
	if !isIdentifier(name) {
		return errAt(t.Pos, "<brick:prop> needs name=\"…\" with a plain identifier, got %q", name)
	}
	if name == "children" {
		return errAt(t.Pos, "<brick:prop name=\"children\"> — children is reserved; write the content directly inside <%s>", o.el.Name)
	}
	return nil
}

func (b *builder) closeEl(t lex.Token) error {
	o := b.top()
	if o == nil {
		return errAt(t.Pos, "</%s> has no opening tag", t.Name)
	}
	if o.el.Name != t.Name {
		return errAt(t.Pos, "</%s> closes <%s> opened at line %d — a close tag must match its open tag exactly", t.Name, o.el.Name, o.el.Pos.Line)
	}
	var fr *frame
	branch := 0
	if n := len(b.frames); n > 0 {
		fr = b.frames[n-1]
		branch = fr.branch
	}
	if len(b.frames) != o.frameDepth || fr != o.frame || branch != o.branch {
		return errAt(t.Pos, "</%s> is in a different template branch than <%s> at line %d — a component tag cannot straddle {{ if }}/{{ else }}; pass a flag instead", t.Name, o.el.Name, o.el.Pos.Line)
	}
	o.el.CloseRaw = t.Raw
	b.stack = b.stack[:len(b.stack)-1]
	return b.attach(o.el, b.top())
}

// attach places a finished element: block props go to their component's
// Props, top-level blocks to the file, everything else into the body.
func (b *builder) attach(el *Element, parent *open) error {
	switch {
	case el.Name == TagProp:
		for _, p := range parent.el.Props {
			pn, _ := p.Attr(AttrName)
			en, _ := el.Attr(AttrName)
			if pn.Value == en.Value {
				return errAt(el.Pos, "<brick:prop name=%q> given twice in <%s>", en.Value, parent.el.Name)
			}
		}
		parent.el.Props = append(parent.el.Props, el)
	case el.Name == TagComponent:
		b.file.Locals = append(b.file.Locals, el)
	case topLevelOnly[el.Name]:
		b.file.Blocks = append(b.file.Blocks, el)
	default:
		b.appendNode(el)
	}
	return nil
}

func isEngineTag(name string) bool { return slices.Contains(EngineTags, name) }

func isIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for i, c := range s {
		if !(c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (i > 0 && c >= '0' && c <= '9')) {
			return false
		}
	}
	return true
}
