package brick

import (
	"context"
	"errors"
	"fmt"
	"html/template"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/ubgo/brick/internal/transpile"
	"github.com/ubgo/brick/internal/tree"
)

// Scope is the value every template executes with, and what `.` and `$` are
// at the top of a file (T3). Its fields are what an author reads:
//
//	.Params   this file's frontmatter (in a layout: the page's, C3)
//	.Props    what the caller gave: tag attributes, or Render's props (A2)
//	.Data     this file's use: data imports
//	.Site     the one read-only global
//	.Page     the page being rendered, from any depth (C15: never nil)
//	.Slot     the value passed to {{ node "x" v }}, inside that node's body
//	.Ctx      values provided by render-tree ancestors (C1)
//
// Invariant: a Scope is never shared between renders; it points at its own
// render's state, which is how render state rides in the data and never in
// functions (T8).
type Scope struct {
	Params map[string]any
	Props  any
	Data   map[string]any
	Site   any
	Page   *Page
	Slot   any
	// Key is the instance key given with key= (C4, E5).
	Key any
	// State is a stateful component's current state.
	State map[string]any
	// Errors are a stateful component's validation errors: rule errors for touched fields, overlaid by the errors the last
	// action returned, which win; empty otherwise.
	Errors map[string]any
	// Touched are the field paths the user has touched, path → true, for
	// styling a valid field; nil outside a stateful component.
	Touched map[string]bool
	// ID is this component instance's deterministic id, from its dynamic
	// path (E5, C10); empty outside a component.
	ID string
	// Attrs are the props the component did not declare (C5); {{ attrs }}
	// emits them.
	Attrs map[string]any
	// Children are the statically visible child components (C7).
	Children []Child
	// Raw is a raw component's body text.
	Raw string
	// Promoted well-known frontmatter keys (sugar for .Params.title etc.).
	Title       string
	Description string
	Layout      string
	Date        time.Time
	Draft       bool

	st   *renderState
	file *compiled
	inst *instance
	// injects are this file's inject: declarations, applied by Ctx.
	injects map[string]injectDecl
}

// Ctx returns the values provided by the render-tree ancestors of the point
// where it is read (C1), with this file's inject: defaults for missing keys.
func (s *Scope) Ctx() map[string]any {
	out := map[string]any{}
	if s.st != nil {
		for _, f := range s.st.ctxStack {
			for k, v := range f.vals {
				out[k] = v
			}
		}
	}
	for k, d := range s.injects {
		if _, ok := out[k]; !ok && d.HasDefault {
			out[k] = d.Default
		}
	}
	return out
}

// Page is the page being rendered. It always exists (C15): outside a page
// render it is a zero page with empty Params.
type Page struct {
	// Path is the page's identity, always set (A9).
	Path   string
	Params map[string]any
	// Props is what the host passed to Render (A2).
	Props  any
	Locale string
	Title  string
	// Route holds the values a dynamic route matched (`[slug].html` →
	// Route.slug); a catch-all's value is its segments joined with "/",
	// which RouteParts lists one by one. Nil on a static page.
	Route      map[string]string
	RouteParts []string
	// URL is the URL being rendered, root-absolute ("/fr/about/"), as the
	// host passed it with WithURL; "" when the host gave none (a bare
	// Render, an action's re-render).
	URL string
	// Translations are the same page in the site's other locales, in the
	// site's locale order, never including this one: what a
	// <link rel="alternate" hreflang> list or a language switcher reads.
	// Empty when the page has no other variant or the host passed none
	// (WithTranslations).
	Translations []Translation
	// Headings are a markdown page's headings in document order, what a
	// layout builds a table of contents from. Empty unless the host passed
	// them (WithHeadings): the site layer does for a .md page when its
	// Markdown backend reports headings, and leaves it empty otherwise.
	Headings []Heading
}

// Heading is one heading of a markdown page, in document order: what a
// layout builds a table of contents from (Page.Headings).
type Heading struct {
	// Level is 1 for <h1> through 6 for <h6>.
	Level int
	// ID is the id attribute written on the heading element, unique within
	// one document, or "" when the backend writes no ids, so a TOC links
	// only the headings that have one.
	ID string
	// Text is the heading's plain text with inline markup removed.
	Text string
}

// Translation is one other-language variant of the page being rendered:
// its locale tag and its URL, root-absolute like Page.URL.
type Translation struct {
	Locale string
	URL    string
}

// Child is one entry of .Children (C7): a statically written child
// component, inspectable without rendering it. Render it with {{ child $c }}.
type Child struct {
	Name  string
	Props map[string]any
	// Key is the child's key=, if any.
	Key any

	parent *instance
	index  int
}

// instance is one component render: what its lexical functions reach.
type instance struct {
	name        string
	children    func() error
	hasChildren bool
	nodes       map[string]func(slot any) error
	attrs       []propValue
	kids        []kidRef
	// mount is brick:mount from the call site: "", visible or idle.
	mount string
	// state is a stateful component's instance.
	state *Instance
	// model is the call site's brick:model binding to a modelable child
	// (modelable.go); nil when the call binds nothing or the child renders
	// for its own action.
	model *modelBind
}

// kidRef renders one child alone.
type kidRef struct {
	file   *compiled
	define string
	scope  bodyScope
}

// bodyScope is the data a generated body executes with; its prologue
// restores `$`, the variables and dot (T2, T3). Authors never see it.
type bodyScope struct {
	Root *Scope
	Dot  any
	Vars map[string]any
}

// ctxFrame is one provider level of the render tree.
type ctxFrame struct {
	vals map[string]any
	// fromMeta marks keys set by frontmatter provide:, which {{ provide }}
	// overrides with a warning (C2).
	fromMeta map[string]bool
	// req marks request-scoped keys (Provide), which a stateful instance
	// never stores.
	req map[string]bool
}

// renderState is one render's mutable state. It is reached only through a
// Scope, never through a function's closure (T8).
type renderState struct {
	e        *Engine
	ctx      context.Context
	out      *builder
	locale   string
	page     *Page
	depth    int
	count    int
	chain    []string
	ctxStack []*ctxFrame
	types    []string
	tags     map[string]bool
	errors   []error
	warnings []Diagnostic
	assets   map[string]bool
	// stats counts what the render did, for Result.Stats (Dev toolbar).
	stats renderCounts
	// failure is the innermost structured error, kept because text/template
	// wraps each level's error in its own message.
	failure *Error
	memo    map[string]any
	// path is the dynamic instance path (C10, E5); seen counts repeated
	// segments under one parent, and keys catches duplicate sibling keys.
	path []string
	seen map[string]int
	keys map[string]bool
	// files are every file the render read (Result.Files).
	files map[string]bool
	// filling is true inside Engine.Fill, where islands render instead of
	// being captured; nextID is the id the next component takes (the
	// island's id from the page).
	filling bool
	nextID  string
	// rootSlots are the stored slots of the island or instance Fill or an
	// action renders, taken by its root with nextID; slotDepth counts the
	// slots being pre-rendered, inside which no island may render
	//.
	rootSlots slotRoot
	slotDepth int
	// resume is the stateful instance an action re-renders;
	// formErrors are that action's validation errors.
	resume     *Instance
	formErrors map[string]any
	// keep are the stateful children the page already shows inside the
	// resumed root; stateDepth counts the stateful
	// components open on the render stack, so only the root's direct
	// children (depth 1) are ever kept.
	keep       map[string]ChildRef
	stateDepth int
	// session is the render's session id (WithSession); staticState marks
	// a static export (WithStaticState).
	session     string
	staticState bool
	// query and queryOwners are the render options'.
	query       url.Values
	queryOwners map[string]string
	// fallbacks renders the enclosing boundaries' fallbacks, innermost last,
	// for islands captured inside a boundary.
	fallbacks []func(node, component string) ([]Chunk, error)
	// needRuntime marks a render that captured a lazy island
	// or used a file with a brick:method link (v23), whose page links the
	// runtime even with no client code.
	needRuntime bool
	// lazyRoot is set while Fill or the island endpoint renders an island's
	// own root, which renders now even when it is lazy; a lazy component
	// inside it gets a placeholder of its own. inAction marks an action's
	// re-render, which renders lazy children in place so a morph never
	// swaps a loaded panel back to its placeholder.
	lazyRoot bool
	inAction bool
	// lazyInline renders every lazy island in place (WithLazyInline), as
	// a static export does but with state stored as usual.
	lazyInline bool
	// views is WithViewLog's log; nil (the usual case) records nothing.
	views *ViewLog
	// formResume is the instance a no-JavaScript form post left, shown
	// instead of a fresh one for the component with its id;
	// consumed by the first match.
	formResume *FormResume
	// dry marks CompileAll's dry run: every engine function is inert, so only
	// html/template's escaper runs (T10).
	dry bool
	// liveOut is the builder batch-loaded components may defer into: the
	// page's or island's own output, or a deferred component's while it
	// renders. A builder swapped in for a slot or a fallback is not live,
	// so nothing deferred there is lost. pending are the
	// deferred calls of the level being rendered, in call order; reserved
	// is the instance-path segment a resumed call takes instead of
	// counting a new occurrence, so its id is the one it would have had.
	liveOut  *builder
	pending  []*deferred
	reserved string
}

// enter pushes an instance-path segment. A segment rendered again under
// the same parent gets an occurrence number, so N renders of one written tag
// get N distinct, deterministic ids; key= replaces the number (C10, E5).
func (st *renderState) enter(seg string, key any) error {
	parent := strings.Join(st.path, "/")
	if key != nil {
		k := parent + "|" + seg + "|" + fmt.Sprint(key)
		if st.keys[k] {
			return fmt.Errorf("two siblings have key=%v; keys must be unique among siblings (E5)", key)
		}
		st.keys[k] = true
		st.path = append(st.path, seg+"="+fmt.Sprint(key))
		return nil
	}
	c := parent + "|" + seg
	n := st.seen[c]
	st.seen[c] = n + 1
	st.path = append(st.path, fmt.Sprintf("%s#%d", seg, n))
	return nil
}

func (st *renderState) leave() { st.path = st.path[:len(st.path)-1] }

// errStop marks an error already recorded in renderState.failure.
var errStop = errors.New("brick: render stopped")

// fail records err (the first, innermost one wins) and returns it for the
// template engine to unwind with.
func (st *renderState) fail(err *Error) error {
	if len(err.Chain) == 0 {
		err.Chain = slices.Clone(st.chain)
	}
	if st.failure == nil {
		st.failure = err
	}
	return err
}

// check runs at every component, children and node render: cancellation
// always wins (E2), then the depth limit (C14, E4).
func (st *renderState) check(file string) error {
	if err := st.ctx.Err(); err != nil {
		return st.fail(&Error{File: file, Code: CodeCanceled, Err: err})
	}
	if st.depth >= st.e.opts.MaxDepth {
		return st.fail(&Error{File: file, Code: CodeLimit, Err: fmt.Errorf("render depth passed MaxDepth (%d) — a component that renders itself without stopping?", st.e.opts.MaxDepth)})
	}
	return nil
}

func (st *renderState) pushCtx(vals map[string]any) {
	f := &ctxFrame{vals: map[string]any{}, fromMeta: map[string]bool{}}
	for k, v := range vals {
		f.vals[k] = v
		f.fromMeta[k] = true
	}
	st.ctxStack = append(st.ctxStack, f)
}

func (st *renderState) popCtx() { st.ctxStack = st.ctxStack[:len(st.ctxStack)-1] }

// markRequest marks the top frame's keys in vals as request-scoped.
func (st *renderState) markRequest(vals map[string]any) {
	if len(vals) == 0 {
		return
	}
	f := st.ctxStack[len(st.ctxStack)-1]
	f.req = make(map[string]bool, len(vals))
	for k := range vals {
		f.req[k] = true
	}
}

// requestKeys lists the keys of the current .Ctx whose value is
// request-scoped, for an island captured here.
func (st *renderState) requestKeys() map[string]bool {
	out := map[string]bool{}
	for _, f := range st.ctxStack {
		for k := range f.vals {
			out[k] = f.req[k]
		}
	}
	for k, r := range out {
		if !r {
			delete(out, k)
		}
	}
	return out
}

// recordType notes a rendered component type, first-render order (E6).
func (st *renderState) recordType(name string) {
	st.stats.component(name)
	if !slices.Contains(st.types, name) {
		st.types = append(st.types, name)
	}
}

// scopeOf unwraps `$` in a runtime function. A value that is not a Scope
// means a function was reached outside brick's calling convention.
func scopeOf(v any) (*Scope, error) {
	s, ok := v.(*Scope)
	if !ok || s == nil || s.st == nil {
		return nil, fmt.Errorf("brick: internal: expected the file scope, got %T", v)
	}
	return s, nil
}

// newScope builds a file's root scope.
func (st *renderState) newScope(file *compiled, params map[string]any, props any) *Scope {
	s := &Scope{Params: params, Props: props, Data: file.data, Site: st.e.opts.Site, Page: st.page, st: st, file: file, injects: file.meta.inject}
	if s.Data == nil {
		s.Data = map[string]any{}
	}
	promote(s, params)
	return s
}

// promote fills the well-known keys from params.
func promote(s *Scope, params map[string]any) {
	if v, ok := params["title"]; ok && v != nil {
		s.Title = fmt.Sprint(v)
	}
	if v, ok := params["description"]; ok && v != nil {
		s.Description = fmt.Sprint(v)
	}
	if v, ok := params["layout"]; ok && v != nil {
		s.Layout = fmt.Sprint(v)
	}
	if v, ok := params["date"].(time.Time); ok {
		s.Date = v
	}
	if v, ok := params["draft"].(bool); ok {
		s.Draft = v
	}
}

// exec runs a template of file into the render's output, turning errors
// into mapped *Error values.
func (st *renderState) exec(file *compiled, name string, data any) error {
	for _, d := range file.deps {
		st.files[d] = true
	}
	err := file.set.ExecuteTemplate(st.out, name, data)
	if err == nil {
		return nil
	}
	if st.failure != nil {
		return errStop
	}
	if errors.Is(err, errOutputLimit) {
		return st.fail(&Error{File: file.path, Code: CodeLimit, Err: fmt.Errorf("output passed MaxOutputBytes (%d)", st.e.opts.MaxOutputBytes)})
	}
	me := file.mapError(err, CodeRuntime)
	if me.Component == "" && len(st.chain) > 0 {
		me.Component = st.chain[len(st.chain)-1]
	}
	return st.fail(me)
}

// render is __render: one component call site.
func (e *Engine) render(root any, id int, dot any, vars, dyn, kids []any) (string, error) {
	s, err := scopeOf(root)
	if err != nil {
		return "", err
	}
	st, file := s.st, s.file
	if st.dry {
		return "", nil
	}
	call := file.out.Calls[id]
	t := file.targets[id]
	if err := st.check(file.path); err != nil {
		return "", err
	}
	varMap := make(map[string]any, len(call.Vars))
	for i, v := range call.Vars {
		varMap[v] = vars[i]
	}
	caller := bodyScope{Root: s, Dot: dot, Vars: varMap}

	switch t.kind {
	case tBoundary:
		return "", st.boundary(file, call, caller)
	case tRender:
		return "", st.dynamicRender(file, call, caller, dyn)
	case tMarkdown:
		return "", st.markdown(file, call, t)
	}
	given, key, err := callProps(call, dyn, t)
	if err != nil {
		return "", st.fail(&Error{File: file.path, Line: call.Pos.Line, Col: call.Pos.Col, Component: call.Name, Code: CodeProp, Err: err})
	}
	return "", st.component(file, call, t, caller, given, key, kids)
}

// callProps assembles a call site's props in source order: literal, bare,
// evaluated, raw blocks (S5 guarantees one value per name).
func callProps(call *transpile.Call, dyn []any, t target) ([]propValue, any, error) {
	var out []propValue
	var key any
	var spread map[string]any
	for _, p := range call.Props {
		var v any
		switch p.Kind {
		case transpile.Literal:
			v = p.Text
		case transpile.Bare:
			v = true
		default:
			v = dyn[p.Dyn]
		}
		switch {
		case p.Name == propKey:
			key = v
			continue
		case p.Name == propSrc && t.callee != nil && t.callee.raw:
			continue
		case p.Name == mountAttr, p.Name == lazyAttr:
			continue
		case p.Name == transpile.SpreadAttr:
			m, err := toStringMap(v)
			if err != nil {
				return nil, nil, fmt.Errorf("%s: %w", transpile.SpreadAttr, err)
			}
			spread = m
			continue
		}
		out = append(out, propValue{name: p.Name, value: v})
	}
	for _, rp := range call.RawProps {
		out = append(out, propValue{name: rp.Name, value: dedent(rp.Text)})
	}
	return withSpread(out, spread), key, nil
}

// withSpread adds a spread map's entries that no written prop gave, in key
// order. Reserved names never arrive through a spread: key and children
// would change identity and content, src would let data choose a file (X3).
func withSpread(given []propValue, spread map[string]any) []propValue {
	if len(spread) == 0 {
		return given
	}
	have := map[string]bool{}
	for _, g := range given {
		have[g.name] = true
	}
	for _, k := range sortedKeys(spread) {
		if have[k] || k == propKey || k == propChildren || k == propSrc {
			continue
		}
		given = append(given, propValue{name: k, value: spread[k]})
	}
	return given
}

// component renders a resolved component (file or local).
func (st *renderState) component(file *compiled, call *transpile.Call, t target, caller bodyScope, given []propValue, key any, kids []any) error {
	st.count++
	if st.count > st.e.opts.MaxComponents {
		return st.fail(&Error{File: file.path, Line: call.Pos.Line, Component: call.Name, Code: CodeLimit, Err: fmt.Errorf("more than MaxComponents (%d) components in one render", st.e.opts.MaxComponents)})
	}
	callee, tmplName := file, t.local
	calleeMeta := &meta{params: map[string]any{}, dedent: true}
	if t.kind == tComponent {
		c, err := st.e.load(st.ctx, t.path, kindComponent, st.locale)
		if err != nil {
			return st.fail(asError(err, CodeImport, file.path))
		}
		callee, tmplName, calleeMeta = c, c.path, c.meta
		if err := st.slotGuard(file, call, c); err != nil {
			return err
		}
		// A stateful component is always an island: every page view gets
		// its own state.
		lazy := lazyOf(call, c.meta)
		if (c.meta.island || c.meta.state != nil || lazy != "") && !st.filling {
			return st.captureIsland(file, call, c, caller, given, key)
		}
		// Inside a filled island, a lazy component gets its own
		// placeholder, so a lazy answer may hold further lazy islands —
		// except the island being filled itself, an action's re-render and
		// a static export, which render it in place.
		if lazy != "" && st.filling && !st.inAction && !st.staticState && !st.lazyInline && !st.lazyRoot {
			return st.nestedLazy(file, call, c, caller, given, key)
		}
		st.lazyRoot = false
	}

	// The call site's own props, before any loader output: what a stateful
	// instance keeps, so a later re-render (an action, $refresh) runs the
	// loader again instead of reusing its old result.
	callSite := make(map[string]any, len(given))
	for _, g := range given {
		callSite[g.name] = g.value
	}
	// A batch-loaded component whose input is not loaded yet renders later,
	// after one load_many call for every such call of the level.
	if calleeMeta.loadMany != "" {
		if deferred, err := st.deferBatch(file, call, t, callee, caller, given, key, kids); deferred || err != nil {
			return err
		}
	}
	// Loader output merges under the call site (D4).
	var loaded map[string]any
	if calleeMeta.load != "" || calleeMeta.loadMany != "" {
		var err error
		if loaded, err = st.runLoader(callee, given); err != nil {
			return err
		}
		given = mergeLoaded(given, loaded)
	}
	props, undeclared, err := applyDecls(calleeMeta, given, call.Name)
	if err != nil {
		return st.fail(&Error{File: file.path, Line: call.Pos.Line, Col: call.Pos.Col, Component: call.Name, Code: CodeProp, Err: err})
	}
	if st.views != nil && !st.dry {
		st.views.record(t.name, callee.path, props, loaded)
	}

	if hasUp(undeclared) && (t.kind != tComponent || callee.multiRoot) {
		return st.fail(&Error{File: file.path, Line: call.Pos.Line, Col: call.Pos.Col, Component: call.Name, Code: CodeSyntax, Err: fmt.Errorf("brick:on: on <%s>: the component needs a single root element to carry its listeners", call.Name)})
	}
	var bind *modelBind
	if t.kind == tComponent {
		// A parent field bound to a modelable child (v22): taken off the
		// attributes, read from the caller's state while the parent renders.
		var parentState map[string]any
		if caller.Root != nil {
			parentState = caller.Root.State
		}
		if undeclared, bind, err = takeModel(calleeMeta, undeclared, parentState, st.nextID == ""); err != nil {
			return st.fail(&Error{File: file.path, Line: call.Pos.Line, Col: call.Pos.Col, Component: call.Name, Code: CodeSyntax, Err: err})
		}
	}
	inst := &instance{name: t.name, hasChildren: call.HasChildren, attrs: undeclared, nodes: map[string]func(any) error{}, mount: mountOf(call), model: bind}
	if call.Body != "" {
		inst.children = func() error { return st.body(file, call.Body, caller) }
	}
	for _, n := range call.Nodes {
		def := n.Define
		inst.nodes[n.Name] = func(slot any) error { return st.body(file, def, withSlot(caller, slot)) }
	}
	var params map[string]any
	if t.kind == tComponent {
		params = calleeMeta.params
	} else {
		params = map[string]any{}
	}
	var scope *Scope
	if t.kind == tLocal {
		// A local component sees its file lexically (C11): .Params and .Data
		// are the file's.
		scope = st.newScope(file, caller.Root.Params, props)
		scope.Params = caller.Root.Params
	} else {
		scope = st.newScope(callee, params, props)
	}
	scope.Key = key
	scope.inst = inst
	scope.Attrs = attrMap(undeclared)
	for i, k := range call.Kids {
		kc := file.out.Calls[k.Call]
		desc := Child{Name: canonicalName(kc.Name), Props: map[string]any{}, parent: inst, index: i}
		var dynVals []any
		if i < len(kids) {
			dynVals, _ = kids[i].([]any)
		}
		j := 0
		for _, p := range kc.Props {
			var v any
			switch p.Kind {
			case transpile.Literal:
				v = p.Text
			case transpile.Bare:
				v = true
			default:
				if j < len(dynVals) {
					v = dynVals[j]
				}
				j++
			}
			if p.Name == propKey {
				desc.Key = v
				continue
			}
			desc.Props[p.Name] = v
		}
		for _, rp := range kc.RawProps {
			desc.Props[rp.Name] = dedent(rp.Text)
		}
		inst.kids = append(inst.kids, kidRef{file: file, define: k.Define, scope: caller})
		scope.Children = append(scope.Children, desc)
	}
	if t.kind == tComponent && calleeMeta.raw {
		switch {
		case t.hasSrc:
			scope.Raw = string(t.src)
		case calleeMeta.dedent:
			scope.Raw = dedent(call.Raw)
		default:
			scope.Raw = call.Raw
		}
	}

	if err := st.enterCall(callSeg(file, call), key); err != nil {
		return st.fail(&Error{File: file.path, Line: call.Pos.Line, Col: call.Pos.Col, Component: call.Name, Code: CodeRuntime, Err: err})
	}
	if st.e.opts.Mode == Dev && t.kind == tComponent && calleeMeta.state != nil && key == nil && st.stateDepth > 0 && !strings.HasSuffix(st.path[len(st.path)-1], "#0") {
		// The same call rendered again under one parent is a loop; without
		// key= its id is its position, so a kept child would land on the
		// wrong row once rows move.
		st.e.warn(Diagnostic{File: file.path, Line: call.Pos.Line, Col: call.Pos.Col, Code: CodeRuntime, Message: fmt.Sprintf("<%s> is stateful and rendered in a range; give it key= so it keeps its state when rows move", call.Name)})
	}
	scope.ID = st.instanceID()
	rooted := t.kind == tComponent && calleeMeta.state != nil
	// slots are what the caller wrote, as stored HTML: taken from the
	// island or instance when Fill or an action renders this root, else
	// rendered below for a stateful call made with a live caller.
	var slots map[string]string
	stored := false
	if st.nextID != "" {
		scope.ID, st.nextID = st.nextID, ""
		rooted = true
		if st.rootSlots.set {
			slots, stored = st.rootSlots.slots, true
			st.useSlots(callee, inst, slots)
		}
		st.rootSlots = slotRoot{}
	}
	// An island or stateful root renders on its own later (Fill, an
	// action) from an empty path, so its children's ids would hash the
	// same in every copy on the page. Rooting the path at the root's id
	// makes them unique per copy, and identical on every re-render of it.
	if rooted {
		st.path[len(st.path)-1] = "@" + scope.ID
	}
	stateful := t.kind == tComponent && calleeMeta.state != nil
	if stateful {
		keep, adopted, err := st.keepChild(callee, scope.ID, callSite, bind)
		if err != nil {
			st.leave()
			return st.fail(&Error{File: callee.path, Component: call.Name, Code: CodeRuntime, Err: err})
		}
		if keep {
			// The page has this child; the runtime keeps its element, state
			// and all. The id is hex, so it needs no escaping.
			st.leave()
			_, err := fmt.Fprintf(st.out, `<brick-keep data-brick=%q></brick-keep>`, scope.ID)
			return err
		}
		if !stored {
			// Called from inside another stateful component, with the
			// caller's live scope: store what it wrote, for this instance's
			// own actions, which render without that scope.
			if slots, err = st.renderSlots(file, call, caller); err != nil {
				st.leave()
				return err
			}
			if slots != nil {
				st.useSlots(callee, scope.inst, slots)
			}
		}
		inst := adopted
		if inst == nil && st.formResume != nil {
			var errs map[string]any
			if inst, errs = st.resumeFormInstance(callee, scope.ID); inst != nil {
				st.formErrors = errs
			}
		}
		if inst == nil {
			if inst, err = st.newInstance(callee, call, callSite); err != nil {
				st.leave()
				return st.fail(&Error{File: callee.path, Component: call.Name, Code: CodeRuntime, Err: err})
			}
			// A fresh instance only: a resumed one already has its values,
			// and init runs once per instance.
			if inst.Handle == "" {
				if key != nil {
					inst.Key = fmt.Sprint(key)
				}
				// A bound model starts from the parent's value, which init
				// sees and cannot override: the parent owns it on render.
				bind.apply(inst)
				if err := st.seedInstance(callee, inst); err != nil {
					st.leave()
					return st.fail(asError(err, CodeScript, callee.path))
				}
				bind.apply(inst)
			}
		}
		inst.ID = scope.ID
		inst.Slots = slots
		if err := st.e.putInstance(st.ctx, inst, st.staticState); err != nil {
			st.leave()
			return st.fail(&Error{File: callee.path, Component: call.Name, Code: CodeRuntime, Err: err})
		}
		scope.inst.state = inst
		scope.State = inst.State
		errs, touched, err := st.renderErrors(calleeMeta, inst, st.formErrors)
		st.formErrors = nil
		if err != nil {
			st.leave()
			return st.fail(&Error{File: callee.path, Component: call.Name, Code: CodeRuntime, Err: err})
		}
		scope.Errors, scope.Touched = errs, touched
		st.stateDepth++
	}
	st.recordType(t.name)
	st.depth++
	st.chain = append(st.chain, call.Name)
	st.pushCtx(calleeMeta.provide)
	defer func() {
		if stateful {
			st.stateDepth--
		}
		st.popCtx()
		st.chain = st.chain[:len(st.chain)-1]
		st.depth--
		st.leave()
	}()
	if err := st.checkInjects(callee, calleeMeta, call.Name); err != nil {
		return err
	}
	if t.kind == tComponent && !callee.dynamicNodes {
		for _, n := range call.Nodes {
			if !callee.nodeNames[n.Name] {
				st.e.warn(Diagnostic{File: file.path, Line: n.Pos.Line, Col: n.Pos.Col, Code: CodeUnusedNode, Message: fmt.Sprintf("<brick:prop name=%q> is never read: <%s> has no {{ node %q }}", n.Name, call.Name, n.Name)})
			}
		}
	}
	if t.kind == tComponent {
		for _, tag := range calleeMeta.cacheTags {
			st.tags[tag] = true
		}
		if len(callee.client) > 0 || len(callee.styles) > 0 {
			st.assets[callee.path] = true
		}
		st.needRuntime = st.needRuntime || callee.methodLinks
	}
	cp := st.out.checkpoint()
	if err := st.exec(callee, tmplName, scope); err != nil {
		return err
	}
	st.out.trimSince(cp)
	return nil
}

// withSlot makes a node body's root carry .Slot. When the node was written
// where dot was the root, dot carries it too, so `.Slot` works there (T3).
func withSlot(b bodyScope, slot any) bodyScope {
	root := *b.Root
	root.Slot = slot
	nb := bodyScope{Root: &root, Dot: b.Dot, Vars: b.Vars}
	if d, ok := b.Dot.(*Scope); ok && d == b.Root {
		nb.Dot = &root
	}
	return nb
}

// body executes a hoisted body define (children, a node, a kid), as its own
// render-tree level: provide inside it scopes to what it renders (C2).
func (st *renderState) body(file *compiled, define string, b bodyScope) error {
	if err := st.check(file.path); err != nil {
		return err
	}
	_ = st.enter(define, nil) // only a duplicate key fails, and a body has none
	st.depth++
	st.pushCtx(nil)
	defer func() {
		st.popCtx()
		st.depth--
		st.leave()
	}()
	return st.exec(file, define, b)
}

// checkInjects enforces required inject: keys.
func (st *renderState) checkInjects(file *compiled, m *meta, component string) error {
	if len(m.inject) == 0 {
		return nil
	}
	have := map[string]bool{}
	for _, f := range st.ctxStack {
		for k := range f.vals {
			have[k] = true
		}
	}
	for _, k := range sortedKeys(m.inject) {
		if d := m.inject[k]; d.Required && !have[k] {
			return st.fail(&Error{File: file.path, Component: component, Code: CodeInject, Err: fmt.Errorf("inject: %q is required, but no ancestor provides it (searched %s)", k, strings.Join(append([]string{"page"}, st.chain...), " → "))})
		}
	}
	return nil
}

// attrMap turns undeclared props into .Attrs.
func attrMap(list []propValue) map[string]any {
	m := make(map[string]any, len(list))
	for _, p := range list {
		m[p.name] = p.value
	}
	return m
}

// mergeLoaded adds loader keys the call site did not give (D4).
func mergeLoaded(given []propValue, loaded map[string]any) []propValue {
	have := map[string]bool{}
	for _, g := range given {
		have[g.name] = true
	}
	for _, k := range sortedKeys(loaded) {
		if !have[k] {
			given = append(given, propValue{name: k, value: loaded[k]})
		}
	}
	return given
}

// children is __children: render the caller's body now (lazy, top-down).
func (st *renderState) children(s *Scope) error {
	if s.inst == nil || s.inst.children == nil {
		return nil
	}
	return s.inst.children()
}

// node is __node: render a named block, optionally with a slot value.
func (st *renderState) node(s *Scope, name string, slot ...any) error {
	if len(slot) > 1 {
		return st.fail(&Error{File: s.file.path, Code: CodeRuntime, Err: fmt.Errorf("node %q takes at most one value; pass several with dict", name)})
	}
	if s.inst == nil {
		return nil
	}
	fn, ok := s.inst.nodes[name]
	if !ok {
		return nil
	}
	var v any
	if len(slot) == 1 {
		v = slot[0]
	}
	return fn(v)
}

// hasNode reports whether a block (or "children") was written (C8).
func hasNode(s *Scope, name string) bool {
	if s.inst == nil {
		return false
	}
	if name == propChildren {
		return s.inst.hasChildren
	}
	_, ok := s.inst.nodes[name]
	return ok
}

// child is __child: render one statically written child (C7).
func (st *renderState) child(s *Scope, c Child) error {
	if c.parent == nil || c.index >= len(c.parent.kids) {
		return st.fail(&Error{File: s.file.path, Code: CodeRuntime, Err: errors.New("child takes an entry of .Children")})
	}
	k := c.parent.kids[c.index]
	return st.body(k.file, k.define, k.scope)
}

// provide is __provide: set a context value for the render-tree below (C2).
func (st *renderState) provide(s *Scope, key string, v any) {
	f := st.ctxStack[len(st.ctxStack)-1]
	if f.fromMeta[key] {
		st.warn(Diagnostic{File: s.file.path, Code: CodeProvide, Message: fmt.Sprintf("{{ provide %q }} overrides the same key from provide: in frontmatter", key)})
		delete(f.fromMeta, key)
	}
	f.vals[key] = v
}

// warn records a render warning.
func (st *renderState) warn(d Diagnostic) {
	d.Severity = SeverityWarning
	for _, w := range st.warnings {
		if w == d {
			return
		}
	}
	st.warnings = append(st.warnings, d)
	st.e.opts.Logger.Warn(d.Message, "file", d.File, "code", string(d.Code))
}

// attrs is __attrs: the undeclared attributes, escaped, for a tag (C5).
// Names in except are left out, so a component that merges the caller's
// class into its own (cx "bk-card" .Attrs.class) writes {{ attrs "class" }}
// and the root never carries two class attributes.
func attrsHTML(s *Scope, except ...string) template.HTMLAttr {
	if s.inst == nil {
		return ""
	}
	var b strings.Builder
	for _, p := range s.inst.attrs {
		// brick:on: listeners go on the root as data-brick-up, never as
		// attributes of whatever element places {{ attrs }}.
		if !validAttrName(p.name) || slices.Contains(except, p.name) || strings.HasPrefix(p.name, upPrefix) {
			continue
		}
		switch v := p.value.(type) {
		case bool:
			if v {
				b.WriteString(" " + p.name)
			}
		case nil:
		default:
			fmt.Fprintf(&b, ` %s="%s"`, p.name, template.HTMLEscapeString(fmt.Sprint(v)))
		}
	}
	// html/template treats HTMLAttr as a trusted sequence of attributes: the
	// names are checked above and every value is escaped.
	return template.HTMLAttr(strings.TrimPrefix(b.String(), " "))
}

// validAttrName accepts names safe to emit unquoted in a tag.
func validAttrName(n string) bool {
	if n == "" || strings.HasPrefix(strings.ToLower(n), "on") {
		return false
	}
	for _, c := range n {
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '-' && c != '_' && c != ':' && c != '.' && c != '@' {
			return false
		}
	}
	return true
}

// boundary contains runtime failures of its content.
func (st *renderState) boundary(file *compiled, call *transpile.Call, caller bodyScope) error {
	cp := st.out.checkpoint()
	types, assets := len(st.types), cloneBoolMap(st.assets)
	chain, depth, ctxDepth, pathDepth := len(st.chain), st.depth, len(st.ctxStack), len(st.path)
	// An island inside this boundary renders later; it takes this
	// boundary's fallback, rendered now, for when it fails at Fill.
	st.fallbacks = append(st.fallbacks, func(node, component string) ([]Chunk, error) {
		saved := st.out
		st.out = &builder{limit: st.e.opts.MaxOutputBytes}
		defer func() { st.out = saved }()
		for _, n := range call.Nodes {
			if n.Name == node {
				slot := map[string]any{slotMessage: "", slotComponent: component}
				if err := st.body(file, n.Define, withSlot(caller, slot)); err != nil {
					return nil, err
				}
			}
		}
		return st.out.result(), nil
	})
	var err error
	if call.Body != "" {
		err = st.body(file, call.Body, caller)
	}
	st.fallbacks = st.fallbacks[:len(st.fallbacks)-1]
	if err == nil {
		return nil
	}
	cause := st.failure
	if cause == nil {
		cause = asError(err, CodeRuntime, file.path)
	}
	// Cancellation always wins (E2); an output-limit failure cannot be
	// contained by writing more.
	if cause.Code == CodeCanceled || errors.Is(cause, context.Canceled) || errors.Is(cause, context.DeadlineExceeded) {
		return err
	}
	// A loader's answer (not found, redirect, error status) is about the
	// whole response: a fallback in its place would still send the page
	// around it, with status 200, to a visitor it was meant to turn away.
	if IsAnswer(cause) {
		return err
	}
	st.failure = nil
	st.out.rollback(cp)
	st.types, st.assets = st.types[:types], assets
	st.chain, st.depth, st.ctxStack, st.path = st.chain[:chain], depth, st.ctxStack[:ctxDepth], st.path[:pathDepth]
	st.errors = append(st.errors, cause)
	st.e.reportError(st.ctx, cause)
	if st.e.opts.Mode == Dev {
		st.out.flush()
		if _, werr := st.out.Write([]byte(errorBlock(tailOf(st.out.chunks), cause))); werr != nil {
			return werr
		}
	}
	for _, n := range call.Nodes {
		if n.Name != nodeFallback {
			continue
		}
		msg := cause.Error()
		if st.e.opts.Mode == Prod {
			msg = "This part of the page could not be shown. (error " + errorID(cause) + ")"
		}
		slot := map[string]any{slotMessage: msg, slotComponent: cause.Component}
		return st.body(file, n.Define, withSlot(caller, slot))
	}
	return nil
}

func cloneBoolMap(m map[string]bool) map[string]bool {
	out := make(map[string]bool, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// dynamicRender is <brick:render>: a component chosen by name from data
// . A name, never a path.
func (st *renderState) dynamicRender(file *compiled, call *transpile.Call, caller bodyScope, dyn []any) error {
	var name string
	var mapProps map[string]any
	var written []propValue
	var key any
	for _, p := range call.Props {
		var v any
		switch p.Kind {
		case transpile.Literal:
			v = p.Text
		case transpile.Bare:
			v = true
		default:
			v = dyn[p.Dyn]
		}
		switch p.Name {
		case renderName:
			name = fmt.Sprint(v)
		case renderProps:
			m, err := toStringMap(v)
			if err != nil {
				return st.fail(&Error{File: file.path, Line: call.Pos.Line, Component: call.Name, Code: CodeRender, Err: fmt.Errorf(":props: %w", err)})
			}
			mapProps = m
		case propKey:
			key = v
		case lazyAttr:
		default:
			written = append(written, propValue{name: p.Name, value: v})
		}
	}
	fail := func(format string, a ...any) error {
		return st.fail(&Error{File: file.path, Line: call.Pos.Line, Col: call.Pos.Col, Component: call.Name, Code: CodeRender, Err: fmt.Errorf(format, a...)})
	}
	if name == "" || strings.ContainsAny(name, "/.~") {
		return fail("name %q: <brick:render> takes a component name, never a path", name)
	}
	if !isComponentStem(name) && !strings.HasPrefix(name, "x-") {
		return fail("name %q is not a component name", name)
	}
	var t target
	canon := canonicalName(name)
	if def, ok := file.locals[canon]; ok {
		t = target{kind: tLocal, name: canon, local: def}
	} else {
		aliases := map[string]string{}
		for _, u := range file.meta.use {
			if u.Kind == useComponent {
				aliases[u.Key] = u.Path
			}
		}
		p, ok := st.e.lookupPath(name, aliases)
		if !ok {
			return fail("no component named %q (written in %s)", name, file.path)
		}
		cm, err := st.e.calleeMeta(p)
		if err != nil {
			return st.fail(asError(err, CodeImport, file.path))
		}
		if cm.raw {
			return fail("%q is a raw component, which <brick:render> cannot fill (S11)", name)
		}
		t = target{kind: tComponent, name: canon, path: p, callee: cm}
	}
	var given []propValue
	seen := map[string]bool{}
	for _, w := range written {
		seen[w.name] = true
	}
	for _, k := range sortedKeys(mapProps) {
		switch {
		case k == propSrc:
			return fail("src cannot reach a component chosen from data (X3)")
		case k == propKey:
			if key == nil {
				key = mapProps[k]
			}
		case !seen[k]:
			given = append(given, propValue{name: k, value: mapProps[k]})
		}
	}
	given = append(given, written...)
	for _, rp := range call.RawProps {
		given = append(given, propValue{name: rp.Name, value: dedent(rp.Text)})
	}
	return st.component(file, call, t, caller, given, key, nil)
}

// toStringMap accepts any string-keyed map (or struct-free nil).
func toStringMap(v any) (map[string]any, error) {
	if v == nil {
		return nil, nil
	}
	if m, ok := v.(map[string]any); ok {
		return m, nil
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Map || rv.Type().Key().Kind() != reflect.String {
		return nil, fmt.Errorf("want a map, got %T", v)
	}
	out := make(map[string]any, rv.Len())
	iter := rv.MapRange()
	for iter.Next() {
		out[iter.Key().String()] = iter.Value().Interface()
	}
	return out, nil
}

// markdown renders the built-in <Markdown> component (E10).
func (st *renderState) markdown(file *compiled, call *transpile.Call, t target) error {
	src := dedent(call.Raw)
	for _, p := range call.Props {
		if p.Name == propSrc && p.Kind == transpile.Literal {
			fp, err := st.e.resolveFrom(file.path, p.Text)
			if err != nil {
				return st.fail(&Error{File: file.path, Line: call.Pos.Line, Code: CodePath, Err: err})
			}
			b, err := st.e.readFile(fp)
			if err != nil {
				return st.fail(&Error{File: file.path, Line: call.Pos.Line, Code: CodeImport, Err: err})
			}
			src = string(b)
		}
	}
	st.recordType(t.name)
	for _, p := range call.Props {
		if p.Name == propSrc && p.Kind == transpile.Literal {
			if fp, err := st.e.resolveFrom(file.path, p.Text); err == nil {
				st.files[fp] = true
			}
		}
	}
	if err := st.e.opts.Markdown.Convert([]byte(src), st.out); err != nil {
		return st.fail(&Error{File: file.path, Line: call.Pos.Line, Component: markdownTag, Code: CodeRuntime, Err: err})
	}
	return nil
}

// dedent strips common leading indentation and the blank first and last
// lines authors add around a block.
func dedent(s string) string {
	lines := strings.Split(s, "\n")
	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	indent := -1
	for _, l := range lines {
		if strings.TrimSpace(l) == "" {
			continue
		}
		n := len(l) - len(strings.TrimLeft(l, " \t"))
		if indent < 0 || n < indent {
			indent = n
		}
	}
	for i, l := range lines {
		if len(l) >= indent && indent > 0 {
			lines[i] = l[indent:]
		} else if strings.TrimSpace(l) == "" {
			lines[i] = ""
		}
	}
	return strings.Join(lines, "\n")
}

// mountOf reads brick:mount from a call site (checked at compile time).
func mountOf(call *transpile.Call) string {
	for _, p := range call.Props {
		if p.Name == mountAttr && p.Kind == transpile.Literal {
			return p.Text
		}
	}
	return ""
}

// locAttrName is the Dev-only attribute that names the file and line of a
// component's root element, so the browser's inspector leads from
// any element straight to the source that wrote it.
const locAttrName = "data-brick-loc"

// locAttr is data-brick-loc for the component s renders, written by the
// same single-root marker as data-brick-up. It is empty outside Dev mode,
// so production bytes never change, and empty for a file with no root.
func locAttr(s *Scope) template.HTMLAttr {
	if s.st.e.opts.Mode != Dev || s.file == nil || s.file.rootLine == 0 {
		return ""
	}
	// The path is relative to Options.FS (X2), so it is safe to show.
	return template.HTMLAttr(" " + locAttrName + `="` + template.HTMLEscapeString(fmt.Sprintf("%s:%d", s.file.path, s.file.rootLine)) + `"`)
}

// markerLine is the file line of the root marker in marked nodes: the
// marker sits just after the root's tag name, so the newlines before it in
// its text node give the root's line. line0 is the body's first file line.
func markerLine(nodes []tree.Node, marker string, line0 int) int {
	for _, n := range nodes {
		if t, ok := n.(tree.Text); ok {
			if i := strings.Index(string(t.Raw), marker); i >= 0 {
				return t.Pos.Line + line0 - 1 + strings.Count(string(t.Raw[:i]), "\n")
			}
		}
	}
	return 0
}
