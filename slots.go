package brick

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strings"

	"github.com/ubgo/brick/internal/transpile"
	"github.com/ubgo/brick/internal/tree"
)

// Slots into stateful components and islands.
//
// An island or stateful component renders after the caller's template is
// gone: Engine.Fill renders it per request, and an action re-renders it
// from its stored instance. So the caller's children and <brick:prop>
// blocks are rendered once, at the call site, and carried as HTML
// (Island.Slots, Instance.Slots). The component's {{ children }} and
// {{ node "x" }} then emit that HTML on every render, first or not.
//
// Three things cannot be pre-rendered, and each is refused with a file and
// line rather than rendered wrongly later:
//   - a node given a value ({{ node "x" v }}): its body depends on data the
//     component computes at render;
//   - .Children and {{ child $c }}: they render the caller's components
//     live, one by one;
//   - an island or stateful component inside the slot: its instance would
//     be frozen into the stored HTML, with a handle that never changes.

// slotRoot carries the stored slots of the island or instance being
// rendered by Fill or an action to the component() call that renders its
// root; set says there is one, even when it holds no slots.
type slotRoot struct {
	slots map[string]string
	set   bool
}

// renderSlots renders a call's children and blocks with the caller's scope
// and returns their HTML, keyed by "children" and the block names. It
// returns nil when the call wrote neither. Raw <brick:prop raw> blocks are
// string props, carried with the other props, so they are not slots.
func (st *renderState) renderSlots(file *compiled, call *transpile.Call, caller bodyScope) (map[string]string, error) {
	if call.Body == "" && len(call.Nodes) == 0 {
		return nil, nil
	}
	saved := st.out
	st.slotDepth++
	defer func() {
		st.out = saved
		st.slotDepth--
	}()
	capture := func(define string, b bodyScope) (string, error) {
		st.out = &builder{limit: st.e.opts.MaxOutputBytes}
		if err := st.body(file, define, b); err != nil {
			return "", err
		}
		var buf bytes.Buffer
		for _, c := range st.out.result() {
			if c.Kind != ChunkBytes {
				// The island guard in component() makes this unreachable;
				// it stays so a new chunk kind cannot be dropped silently.
				return "", st.fail(&Error{File: file.path, Line: call.Pos.Line, Col: call.Pos.Col, Component: call.Name, Code: CodeRuntime, Err: errors.New("a slot of an island or stateful component must render to plain HTML")})
			}
			buf.Write(c.Bytes)
		}
		return buf.String(), nil
	}
	slots := map[string]string{}
	if call.Body != "" {
		html, err := capture(call.Body, caller)
		if err != nil {
			return nil, err
		}
		slots[propChildren] = html
	}
	for _, n := range call.Nodes {
		html, err := capture(n.Define, withSlot(caller, nil))
		if err != nil {
			return nil, err
		}
		slots[n.Name] = html
	}
	return slots, nil
}

// useSlots makes a component instance emit stored slot HTML for
// {{ children }} and {{ node }}, in place of rendering the caller's
// bodies. A node given a value is refused: its body was rendered once,
// without one (the compile check catches the literal case).
func (st *renderState) useSlots(file *compiled, inst *instance, slots map[string]string) {
	inst.children = nil
	inst.nodes = make(map[string]func(any) error, len(slots))
	html, ok := slots[propChildren]
	inst.hasChildren = ok
	if ok {
		inst.children = func() error {
			_, err := io.WriteString(st.out, html)
			return err
		}
	}
	for name, html := range slots {
		if name == propChildren {
			continue
		}
		inst.nodes[name] = func(v any) error {
			if v != nil {
				return st.fail(&Error{File: file.path, Code: CodeRuntime, Err: fmt.Errorf("node %q is given a value, but <%s> is an island or stateful component, whose blocks are rendered once by the caller", name, inst.name)})
			}
			_, err := io.WriteString(st.out, html)
			return err
		}
	}
}

// slotGuard refuses an island or stateful component rendered inside a
// slot being pre-rendered: a call chosen from data, or one inside another
// component the slot renders, which the compile check cannot see.
func (st *renderState) slotGuard(file *compiled, call *transpile.Call, callee *compiled) error {
	if st.slotDepth == 0 || (!callee.meta.island && callee.meta.state == nil) {
		return nil
	}
	return st.fail(&Error{File: file.path, Line: call.Pos.Line, Col: call.Pos.Col, Component: call.Name, Code: CodeSyntax, Err: errSlotIsland(call.Name)})
}

func errSlotIsland(name string) error {
	return fmt.Errorf("<%s> is an island or stateful component inside the children of another one, whose children are stored as HTML; put <%s> inside that component's own file", name, name)
}

// nodeValueRE finds a generated {{ node }} call and what follows its name:
// anything other than the action's end, a closing paren or a pipe is a
// value.
var nodeValueRE = regexp.MustCompile(`__node \$ (?:"(?:[^"\\]|\\.)*"|[^\s)}"][^\s)}]*)[ \t]+([^\s)}|-]|-[^}])`)

// liveChildrenRE finds .Children reads and {{ child }} calls in generated
// source.
var liveChildrenRE = regexp.MustCompile(`(?:^|[^\w)\]])\$?\.Children\b|__child \$`)

// checkSlots runs the compile-time slot rules and the declared slots: every
// call's blocks match its callee's brick: {slots:}; a component that is an
// island or stateful reads its slots only in forms that can be stored, and
// no call site puts an island or stateful component inside such a
// component's children or blocks. It needs c.lines set.
func (e *Engine) checkSlots(c *compiled, resolved map[*tree.Element]target, line0 int) error {
	if c.meta.island || c.meta.state != nil {
		src := withoutLocals(c)
		if m := nodeValueRE.FindStringSubmatchIndex(src); m != nil {
			line, col := c.posAt(m[0])
			return &Error{File: c.path, Line: line, Col: col, Code: CodeSyntax, Err: errors.New("{{ node }} with a value in an island or stateful component: its blocks are rendered once by the caller and stored, so they cannot depend on a value")}
		}
		if m := liveChildrenRE.FindStringIndex(src); m != nil {
			line, col := c.posAt(m[0])
			return &Error{File: c.path, Line: line, Col: col, Code: CodeSyntax, Err: errors.New(".Children and {{ child }} in an island or stateful component: its children are rendered once by the caller and stored as HTML; use {{ children }}")}
		}
	}
	for _, call := range c.out.Calls {
		t := resolved[call.Element]
		if t.kind == tComponent {
			if err := checkDeclaredSlots(c, call, t.callee); err != nil {
				return err
			}
		}
		if t.kind != tComponent || t.callee == nil || (!t.callee.island && t.callee.state == nil) || call.Element == nil {
			continue
		}
		var bad *tree.Element
		var walk func(nodes []tree.Node)
		walk = func(nodes []tree.Node) {
			for _, n := range nodes {
				el, ok := n.(*tree.Element)
				if !ok || bad != nil {
					continue
				}
				if in := resolved[el]; in.kind == tComponent && in.callee != nil && (in.callee.island || in.callee.state != nil) {
					bad = el
					return
				}
				for _, p := range el.Props {
					walk(p.Children)
				}
				walk(el.Children)
			}
		}
		for _, p := range call.Element.Props {
			walk(p.Children)
		}
		walk(call.Element.Children)
		if bad != nil {
			return &Error{File: c.path, Line: bad.Pos.Line + line0 - 1, Col: bad.Pos.Col, Component: bad.Name, Code: CodeSyntax, Err: errSlotIsland(bad.Name)}
		}
	}
	return nil
}

// slotDecl is one entry of brick: {slots:}.
type slotDecl struct {
	Required bool
	Doc      string
	Examples []string
}

// slotSettings are the keys a slot's map declaration accepts.
var slotSettings = []string{"required", docKey, examplesKey}

// parseSlots reads brick: {slots: {name: {required, doc, examples}}}. A
// bare `name:` declares an optional slot. `children` names the call's
// body; every other name is a <brick:prop name="…"> block.
func parseSlots(v any) (map[string]*slotDecl, error) {
	mp, ok := v.(map[string]any)
	if !ok {
		return nil, errors.New("brick: slots: must map block names to {required, doc, examples}")
	}
	out := make(map[string]*slotDecl, len(mp))
	for _, name := range sortedKeys(mp) {
		if name == "" {
			return nil, errors.New("brick: slots: a slot needs a name")
		}
		d := &slotDecl{}
		switch x := mp[name].(type) {
		case nil:
		case map[string]any:
			for k := range x {
				if !slices.Contains(slotSettings, k) {
					return nil, fmt.Errorf("brick: slots: %s: unknown setting %q (%s)", name, k, strings.Join(slotSettings, ", "))
				}
			}
			if r, ok := x["required"]; ok {
				b, ok := r.(bool)
				if !ok {
					return nil, fmt.Errorf("brick: slots: %s: required must be true or false", name)
				}
				d.Required = b
			}
			doc, examples, err := parseDocs(x)
			if err != nil {
				return nil, fmt.Errorf("brick: slots: %s: %w", name, err)
			}
			d.Doc, d.Examples = doc, examples
		default:
			return nil, fmt.Errorf("brick: slots: %s: must be a map with required, doc or examples", name)
		}
		out[name] = d
	}
	return out, nil
}

// checkDeclaredSlots applies a callee's brick: {slots:} to one call site
// : every block the call passes must be declared, and every
// required slot must be given. The call's body is the `children` slot; an
// undeclared body stays allowed, because most components take one without
// saying so. Raw blocks are string props, checked by props:, not here.
func checkDeclaredSlots(c *compiled, call *transpile.Call, callee *meta) error {
	if callee == nil || callee.slots == nil {
		return nil
	}
	for _, n := range call.Nodes {
		if _, ok := callee.slots[n.Name]; !ok {
			declared := slices.DeleteFunc(slices.Clone(callee.slotOrder), func(s string) bool { return s == propChildren })
			return &Error{File: c.path, Line: n.Pos.Line, Col: n.Pos.Col, Component: call.Name, Code: CodeSyntax, Err: fmt.Errorf("<brick:prop name=%q>: <%s> declares no such slot (declared blocks: %s)", n.Name, call.Name, orNone(declared))}
		}
	}
	for _, name := range callee.slotOrder {
		if !callee.slots[name].Required {
			continue
		}
		given := call.HasChildren
		if name != propChildren {
			given = slices.ContainsFunc(call.Nodes, func(n transpile.Node) bool { return n.Name == name })
		}
		if !given {
			what := fmt.Sprintf("a <brick:prop name=%q> block", name)
			if name == propChildren {
				what = "children (content between its tags)"
			}
			return &Error{File: c.path, Line: call.Pos.Line, Col: call.Pos.Col, Component: call.Name, Code: CodeSyntax, Err: fmt.Errorf("<%s> requires %s", call.Name, what)}
		}
	}
	return nil
}

// orNone joins names for a message, or says there are none.
func orNone(names []string) string {
	if len(names) == 0 {
		return "none"
	}
	return strings.Join(names, ", ")
}

// withoutLocals is a file's generated source with its local components'
// defines blanked (same length, so offsets still map): a local is its own
// component, and its nodes and .Children are its own.
func withoutLocals(c *compiled) string {
	src := c.out.Source
	for _, l := range c.out.Locals {
		start := strings.Index(src, `{{ define "`+l.Define+`" }}`)
		if start < 0 {
			continue
		}
		end := len(src) - start
		if next := strings.Index(src[start+1:], `{{ define "`); next >= 0 {
			end = next + 1
		}
		src = src[:start] + strings.Repeat(" ", end) + src[start+end:]
	}
	return src
}

// posAt maps a byte offset in the generated source to the author's line
// and column.
func (c *compiled) posAt(off int) (int, int) {
	line := 1
	for i, s := range c.lines {
		if s > off {
			break
		}
		line = i + 1
	}
	return c.sourcePos(line, off-c.lines[line-1])
}
