package brick

import (
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"text/template/parse"

	"github.com/ubgo/brick/internal/transpile"
)

// Shape checks (G15): a `map` prop with `fields:` or a `list`/`map` prop
// with `of:` declares what is inside the value, and `brick check` warns when
// a template reads a field the shape does not list, or when a call site,
// or a default, passes a value that does not fit.
//
// Everything here runs from CompileAll only. Rendering never reads a shape:
// a loader's output or a Go value the engine cannot see is never checked,
// so a shape can never turn a working page into a failing one. Anything the
// walker cannot type statically (a function result, a template call, .State)
// is "unknown", and unknown never warns: a false warning costs the author's
// trust in every other warning, a missed one costs only what brick did
// before shapes existed.

// sType is the static type of a template value as far as the walker can
// tell; nil is unknown.
type sType any

type (
	// tScope is a file's root Scope: .Props reaches decls, .Data the
	// file's use: data, and .Params the page frontmatter (pages only; a
	// component's .Params are its caller's, which are not known here).
	tScope struct {
		decls  map[string]*propDecl
		params map[string]any
		data   map[string]any
	}
	// tProps is .Props of a tScope.
	tProps struct{ decls map[string]*propDecl }
	// tDecl is a value declared by d; path names it in messages
	// (".Props.user.address", ".Props.posts[]").
	tDecl struct {
		d    *propDecl
		path string
	}
	// tValue is a value known at compile: frontmatter, data, a constant.
	tValue struct{ v any }
	// tDict is a `dict "k" v …` literal with constant keys.
	tDict struct {
		keys []string
		vals map[string]sType
	}
	// tList is a `list a b …` literal.
	tList struct{ elems []sType }
	// tBody is the bodyScope a component body, kid or node define runs
	// with: the call site's $, dot and $variables (transpile prologue).
	tBody struct {
		root, dot sType
		vars      map[string]sType
	}
	// tVars is tBody.Vars, read with `index .Vars "name"`.
	tVars struct{ vars map[string]sType }
	// tOnce is `__once x`: a one-element list, so range binds dot to x.
	tOnce struct{ elem sType }
)

// shapeEnv is the walker's lexical state: the dot and the $variables.
type shapeEnv struct {
	dot  sType
	vars map[string]sType
}

func (env shapeEnv) child() shapeEnv {
	return shapeEnv{dot: env.dot, vars: maps.Clone(env.vars)}
}

// shapeChecker walks one compiled file.
type shapeChecker struct {
	c     *compiled
	diags []Diagnostic
	// seeds are the environments of body, kid and node defines, taken
	// from the call site that renders them.
	seeds map[string]shapeEnv
	done  map[string]bool
}

// checkShapes returns the shape warnings of one compiled file. It is cheap
// when nothing in the file or its callees declares a shape.
func (e *Engine) checkShapes(c *compiled) []Diagnostic {
	if !c.usesShapes() {
		return nil
	}
	sc := &shapeChecker{c: c, seeds: map[string]shapeEnv{}, done: map[string]bool{}}
	// src only places a default's warning on its line; a page given with
	// WithSource has no file to read, and its warnings then point at line 1.
	src, _ := e.readFile(c.path)
	sc.checkDefaults(src)
	root := tScope{decls: c.meta.props, data: c.data}
	if c.kind == kindPage {
		root.params = c.meta.params
	}
	sc.walkTemplate(c.path, shapeEnv{dot: root, vars: map[string]sType{"$": root}})
	// Bodies are seeded while their caller is walked; a body may hold
	// further calls, so loop until no seeded define is left.
	for {
		name := ""
		for _, n := range sortedKeys(sc.seeds) {
			if !sc.done[n] {
				name = n
				break
			}
		}
		if name == "" {
			break
		}
		sc.walkTemplate(name, sc.seeds[name])
	}
	// A local component (<brick:component>) has its own .Props, never
	// declared; walking it with an empty scope still checks the literals
	// it passes to shaped components.
	for _, l := range c.out.Locals {
		empty := tScope{data: c.data}
		sc.walkTemplate(l.Define, shapeEnv{dot: empty, vars: map[string]sType{"$": empty}})
	}
	return sc.diags
}

// usesShapes reports whether the file declares a shape or calls a component
// that does.
func (c *compiled) usesShapes() bool {
	for _, d := range c.meta.props {
		if d.hasShape() {
			return true
		}
	}
	for _, t := range c.targets {
		if t.callee == nil {
			continue
		}
		for _, d := range t.callee.props {
			if d.hasShape() {
				return true
			}
		}
	}
	return false
}

func (sc *shapeChecker) walkTemplate(name string, env shapeEnv) {
	if sc.done[name] {
		return
	}
	sc.done[name] = true
	t := sc.c.set.Lookup(name)
	if t == nil || t.Tree == nil || t.Tree.Root == nil {
		return
	}
	sc.walkList(t.Tree.Root, env)
}

func (sc *shapeChecker) walkList(l *parse.ListNode, env shapeEnv) {
	if l == nil {
		return
	}
	for _, n := range l.Nodes {
		sc.walkNode(n, &env)
	}
}

// walkNode walks one node; env is a pointer so a {{ $x := … }} declared
// at this level stays visible to the following nodes.
func (sc *shapeChecker) walkNode(n parse.Node, env *shapeEnv) {
	switch n := n.(type) {
	case *parse.ActionNode:
		sc.declare(n.Pipe, env)
	case *parse.IfNode:
		sc.evalPipe(n.Pipe, *env)
		sc.walkList(n.List, env.child())
		sc.walkList(n.ElseList, env.child())
	case *parse.RangeNode:
		t := sc.evalPipe(n.Pipe, *env)
		elem := rangeElem(t)
		inner := env.child()
		inner.dot = elem
		switch len(n.Pipe.Decl) {
		case 1:
			inner.vars[n.Pipe.Decl[0].Ident[0]] = elem
		case 2:
			inner.vars[n.Pipe.Decl[0].Ident[0]] = nil
			inner.vars[n.Pipe.Decl[1].Ident[0]] = elem
		}
		sc.walkList(n.List, inner)
		sc.walkList(n.ElseList, env.child())
	case *parse.WithNode:
		t := sc.evalPipe(n.Pipe, *env)
		inner := env.child()
		inner.dot = t
		for _, d := range n.Pipe.Decl {
			inner.vars[d.Ident[0]] = t
		}
		sc.walkList(n.List, inner)
		sc.walkList(n.ElseList, env.child())
	case *parse.TemplateNode:
		sc.evalPipe(n.Pipe, *env)
	case *parse.ListNode:
		sc.walkList(n, env.child())
	}
}

// declare evaluates an action's pipeline and binds its $variables.
func (sc *shapeChecker) declare(p *parse.PipeNode, env *shapeEnv) {
	if p == nil {
		return
	}
	t := sc.evalPipe(p, *env)
	for _, d := range p.Decl {
		env.vars[d.Ident[0]] = t
	}
}

// evalPipe checks every argument of every command and returns the
// pipeline's type when it is a single command.
func (sc *shapeChecker) evalPipe(p *parse.PipeNode, env shapeEnv) sType {
	if p == nil {
		return nil
	}
	var first sType
	for i, cmd := range p.Cmds {
		t := sc.evalCmd(cmd, env)
		if i == 0 {
			first = t
		}
	}
	if len(p.Cmds) != 1 {
		return nil
	}
	return first
}

func (sc *shapeChecker) evalCmd(cmd *parse.CommandNode, env shapeEnv) sType {
	if len(cmd.Args) == 0 {
		return nil
	}
	if id, ok := cmd.Args[0].(*parse.IdentifierNode); ok {
		args := cmd.Args[1:]
		switch id.Ident {
		case "dict":
			return sc.evalDict(args, env)
		case "list":
			var l tList
			for _, a := range args {
				l.elems = append(l.elems, sc.evalArg(a, env))
			}
			return l
		case "index":
			if len(args) == 0 {
				return nil
			}
			t := sc.evalArg(args[0], env)
			for _, a := range args[1:] {
				sc.evalArg(a, env)
				switch k := a.(type) {
				case *parse.StringNode:
					t = sc.step(t, k.Text, a)
				case *parse.NumberNode:
					t = rangeElem(t)
				default:
					t = nil
				}
			}
			return t
		case transpile.FnOnce:
			if len(args) == 1 {
				return tOnce{elem: sc.evalArg(args[0], env)}
			}
		case transpile.FnRender:
			sc.renderCall(args, env)
			return nil
		}
		for _, a := range args {
			sc.evalArg(a, env)
		}
		return nil
	}
	if len(cmd.Args) == 1 {
		return sc.evalArg(cmd.Args[0], env)
	}
	for _, a := range cmd.Args {
		sc.evalArg(a, env)
	}
	return nil
}

func (sc *shapeChecker) evalDict(args []parse.Node, env shapeEnv) sType {
	d := tDict{vals: map[string]sType{}}
	constKeys := len(args)%2 == 0
	for i := 0; i+1 < len(args); i += 2 {
		v := sc.evalArg(args[i+1], env)
		k, ok := args[i].(*parse.StringNode)
		if !ok {
			sc.evalArg(args[i], env)
			constKeys = false
			continue
		}
		d.keys = append(d.keys, k.Text)
		d.vals[k.Text] = v
	}
	if !constKeys {
		return nil
	}
	return d
}

func (sc *shapeChecker) evalArg(n parse.Node, env shapeEnv) sType {
	switch n := n.(type) {
	case *parse.FieldNode:
		return sc.chain(env.dot, n.Ident, n)
	case *parse.VariableNode:
		return sc.chain(env.vars[n.Ident[0]], n.Ident[1:], n)
	case *parse.ChainNode:
		return sc.chain(sc.evalArg(n.Node, env), n.Field, n)
	case *parse.PipeNode:
		return sc.evalPipe(n, env)
	case *parse.DotNode:
		return env.dot
	case *parse.StringNode:
		return tValue{v: n.Text}
	case *parse.BoolNode:
		return tValue{v: n.True}
	case *parse.NumberNode:
		switch {
		case n.IsInt:
			return tValue{v: n.Int64}
		case n.IsFloat:
			return tValue{v: n.Float64}
		}
	}
	return nil
}

// chain follows .a.b.c from t, warning at the first field a shape lacks.
func (sc *shapeChecker) chain(t sType, idents []string, at parse.Node) sType {
	for _, id := range idents {
		if t == nil {
			return nil
		}
		t = sc.step(t, id, at)
	}
	return t
}

// step reads one field (or one constant index key) of t.
func (sc *shapeChecker) step(t sType, name string, at parse.Node) sType {
	switch t := t.(type) {
	case tScope:
		switch name {
		case "Props":
			return tProps{decls: t.decls}
		case "Data":
			return tValue{v: t.data}
		case "Params":
			if t.params != nil {
				return tValue{v: t.params}
			}
		}
	case tProps:
		if d := t.decls[name]; d != nil {
			return tDecl{d: d, path: ".Props." + name}
		}
	case tDecl:
		d := t.d
		switch {
		case d.Fields != nil:
			if f := d.Fields[name]; f != nil {
				return tDecl{d: f, path: t.path + "." + name}
			}
			sc.warnAt(at, name, fmt.Sprintf("%s.%s: %q is not a field of %s (fields: %s)", t.path, name, name, t.path, strings.Join(d.FieldOrder, ", ")))
		case d.Type == typeMap && d.Of != nil:
			return tDecl{d: d.Of, path: t.path + "[" + strconv.Quote(name) + "]"}
		}
	case tValue:
		if m, ok := t.v.(map[string]any); ok {
			if v, ok := m[name]; ok {
				return tValue{v: v}
			}
		}
	case tDict:
		return t.vals[name]
	case tBody:
		switch name {
		case "Root":
			return t.root
		case "Dot":
			return t.dot
		case "Vars":
			return tVars{vars: t.vars}
		}
	case tVars:
		return t.vars[name]
	}
	return nil
}

// rangeElem is the dot inside {{ range t }}.
func rangeElem(t sType) sType {
	switch t := t.(type) {
	case tDecl:
		if t.d.Of != nil {
			return tDecl{d: t.d.Of, path: t.path + "[]"}
		}
	case tOnce:
		return t.elem
	}
	return nil
}

// renderCall handles `__render $ ID . (__list vars) (__list dyn) (__list
// kids)`: it seeds the call's body, node and kid defines with this call
// site's environment, and checks the dynamic props given to a shaped prop.
func (sc *shapeChecker) renderCall(args []parse.Node, env shapeEnv) {
	const wantArgs = 6 // $, ID, dot, vars, dyn, kids — transpile.Call.action
	for _, a := range args {
		sc.evalArg(a, env)
	}
	if len(args) != wantArgs {
		return
	}
	num, ok := args[1].(*parse.NumberNode)
	if !ok || !num.IsInt || num.Int64 < 0 || int(num.Int64) >= len(sc.c.out.Calls) {
		return
	}
	id := int(num.Int64)
	call := sc.c.out.Calls[id]
	vars := map[string]sType{}
	if l := listItems(args[3]); len(l) == len(call.Vars) {
		for i, v := range call.Vars {
			vars[v] = sc.evalArg(l[i], env)
		}
	}
	body := shapeEnv{dot: tBody{root: sc.evalArg(args[0], env), dot: sc.evalArg(args[2], env), vars: vars}, vars: map[string]sType{}}
	seed := func(name string) {
		if _, ok := sc.seeds[name]; name != "" && !ok {
			sc.seeds[name] = body
		}
	}
	seed(call.Body)
	for _, n := range call.Nodes {
		seed(n.Define)
	}
	for _, k := range call.Kids {
		seed(k.Define)
	}
	if id >= len(sc.c.targets) || sc.c.targets[id].callee == nil {
		return
	}
	callee := sc.c.targets[id].callee
	dyn := listItems(args[4])
	for _, p := range call.Props {
		if p.Kind != transpile.Expr && p.Kind != transpile.Interp {
			continue
		}
		d := callee.props[p.Name]
		if !d.hasShape() || p.Dyn >= len(dyn) {
			continue
		}
		given := sc.evalArg(dyn[p.Dyn], env)
		for _, msg := range fitShape(given, d, p.Name) {
			sc.diags = append(sc.diags, Diagnostic{File: sc.c.path, Line: p.Pos.Line, Col: p.Pos.Col, Severity: SeverityWarning, Code: CodeShape, Message: fmt.Sprintf("<%s> %s", call.Name, msg)})
		}
	}
}

// listItems returns the arguments of a `(__list a b …)` node.
func listItems(n parse.Node) []parse.Node {
	p, ok := n.(*parse.PipeNode)
	if !ok || len(p.Cmds) != 1 || len(p.Cmds[0].Args) == 0 {
		return nil
	}
	if id, ok := p.Cmds[0].Args[0].(*parse.IdentifierNode); !ok || id.Ident != transpile.FnList {
		return nil
	}
	return p.Cmds[0].Args[1:]
}

// fitShape compares a value the checker could type with a shaped
// declaration. A literal (dict, list, a default) is closed like the shape:
// an extra key is a typo. A known value from data or a caller's declared
// shape may carry more than this component reads, so only what is missing
// or mistyped is reported.
func fitShape(given sType, d *propDecl, path string) []string {
	switch g := given.(type) {
	case tDict:
		if d.Type != typeMap && d.Type != typeAny {
			return []string{fmt.Sprintf("%s: a dict is not a %s", path, d.Type)}
		}
		var out []string
		if d.Fields != nil {
			for _, k := range g.keys {
				if d.Fields[k] == nil {
					out = append(out, fmt.Sprintf("%s: %q is not a field of %s (fields: %s)", path, k, path, strings.Join(d.FieldOrder, ", ")))
				}
			}
			for _, f := range d.FieldOrder {
				fd := d.Fields[f]
				v, given := g.vals[f]
				if !given {
					if fd.Required {
						out = append(out, fmt.Sprintf("%s: the required field %q is missing", path, f))
					}
					continue
				}
				out = append(out, fitShape(v, fd, path+"."+f)...)
			}
		}
		if d.Of != nil && d.Type == typeMap {
			for _, k := range g.keys {
				out = append(out, fitShape(g.vals[k], d.Of, path+"["+strconv.Quote(k)+"]")...)
			}
		}
		return out
	case tList:
		if d.Type != typeList && d.Type != typeAny {
			return []string{fmt.Sprintf("%s: a list is not a %s", path, d.Type)}
		}
		var out []string
		if d.Of != nil {
			for i, el := range g.elems {
				out = append(out, fitShape(el, d.Of, fmt.Sprintf("%s[%d]", path, i))...)
			}
		}
		return out
	case tValue:
		return fitValue(g.v, d, path, false)
	case tDecl:
		return fitDecl(g, d, path)
	}
	return nil
}

// fitValue compares a value known at compile with a declaration. closed
// reports extra map keys (a default the author wrote next to the shape).
func fitValue(v any, d *propDecl, path string, closed bool) []string {
	if v == nil {
		return nil
	}
	if !d.hasShape() {
		if _, err := checkProp(d, v); err != nil {
			return []string{fmt.Sprintf("%s: %v", path, err)}
		}
		return nil
	}
	rv := reflect.ValueOf(v)
	var out []string
	switch {
	case d.Fields != nil:
		m, ok := v.(map[string]any)
		if !ok {
			return []string{fmt.Sprintf("%s: %s is not a map", path, describe(v))}
		}
		if closed {
			for _, k := range sortedKeys(m) {
				if d.Fields[k] == nil {
					out = append(out, fmt.Sprintf("%s: %q is not a field of %s (fields: %s)", path, k, path, strings.Join(d.FieldOrder, ", ")))
				}
			}
		}
		for _, f := range d.FieldOrder {
			fv, ok := m[f]
			if !ok || fv == nil {
				if d.Fields[f].Required {
					out = append(out, fmt.Sprintf("%s: the required field %q is missing", path, f))
				}
				continue
			}
			out = append(out, fitValue(fv, d.Fields[f], path+"."+f, closed)...)
		}
	case d.Type == typeList:
		if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
			return []string{fmt.Sprintf("%s: %s is not a list", path, describe(v))}
		}
		for i := 0; i < rv.Len(); i++ {
			out = append(out, fitValue(rv.Index(i).Interface(), d.Of, fmt.Sprintf("%s[%d]", path, i), closed)...)
		}
	case d.Type == typeMap:
		m, ok := v.(map[string]any)
		if !ok {
			return []string{fmt.Sprintf("%s: %s is not a map", path, describe(v))}
		}
		for _, k := range sortedKeys(m) {
			out = append(out, fitValue(m[k], d.Of, path+"["+strconv.Quote(k)+"]", closed)...)
		}
	}
	return out
}

// fitDecl compares a caller's declared shape with the callee's: a field
// the callee requires must exist in the caller's shape. Extra caller
// fields are fine, and a caller without a shape is unknown.
func fitDecl(g tDecl, d *propDecl, path string) []string {
	if !g.d.hasShape() || !d.hasShape() {
		return nil
	}
	var out []string
	switch {
	case d.Fields != nil && g.d.Fields != nil:
		for _, f := range d.FieldOrder {
			gf := g.d.Fields[f]
			if gf == nil {
				if d.Fields[f].Required {
					out = append(out, fmt.Sprintf("%s: the required field %q is not in the shape of %s (fields: %s)", path, f, g.path, strings.Join(g.d.FieldOrder, ", ")))
				}
				continue
			}
			out = append(out, fitDecl(tDecl{d: gf, path: g.path + "." + f}, d.Fields[f], path+"."+f)...)
		}
	case d.Of != nil && g.d.Of != nil && d.Type == g.d.Type:
		out = append(out, fitDecl(tDecl{d: g.d.Of, path: g.path + "[]"}, d.Of, path+"[]")...)
	}
	return out
}

// checkDefaults checks each shaped prop's default against its shape. The
// default is a literal written beside the shape, so it is closed: an
// extra key is reported like a misspelled field.
func (sc *shapeChecker) checkDefaults(src []byte) {
	for _, name := range sc.c.meta.order {
		d := sc.c.meta.props[name]
		if !d.hasShape() || !d.HasDefault {
			continue
		}
		for _, msg := range fitValue(d.Default, d, name, true) {
			sc.diags = append(sc.diags, Diagnostic{File: sc.c.path, Line: propLine(src, name), Severity: SeverityWarning, Code: CodeShape, Message: "default of " + msg})
		}
	}
}

// warnAt records a read warning at the bad field's name inside a template
// node's action, mapped back to the author's file through the compile's
// source map. A node's position is where its chain starts, so the column
// moves on to the field itself when it is found before the action ends.
func (sc *shapeChecker) warnAt(n parse.Node, field, msg string) {
	off := int(n.Position())
	if src := sc.c.out.Source; off < len(src) {
		rest := src[off:]
		if end := strings.Index(rest, "}}"); end >= 0 {
			rest = rest[:end]
		}
		if i := strings.Index(rest, field); i >= 0 {
			off += i
		}
	}
	i, _ := slices.BinarySearch(sc.c.lines, off+1)
	line := i // lines[i-1] <= off < lines[i]
	col := 0
	if line >= 1 {
		col = off - sc.c.lines[line-1]
	}
	fl, fc := sc.c.sourcePos(line, col)
	sc.diags = append(sc.diags, Diagnostic{File: sc.c.path, Line: fl, Col: fc, Severity: SeverityWarning, Code: CodeShape, Message: msg})
}

// propLine is the frontmatter line declaring a prop — the first line, after
// the opening fence, that starts with `name:` after indentation — or 1 when
// none does (a flow-style props: map). Best effort: it only places a
// warning.
func propLine(src []byte, name string) int {
	lines := strings.Split(string(src), "\n")
	for i, l := range lines {
		if i > 0 && strings.TrimSpace(l) == frontmatterFence {
			break
		}
		if strings.HasPrefix(strings.TrimLeft(l, " \t"), name+":") {
			return i + 1
		}
	}
	return 1
}

// frontmatterFence closes a YAML frontmatter block.
const frontmatterFence = "---"
