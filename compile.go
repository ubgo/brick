package brick

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"html/template"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/ubgo/brick/internal/frontmatter"
	"github.com/ubgo/brick/internal/lex"
	"github.com/ubgo/brick/internal/transpile"
	"github.com/ubgo/brick/internal/tree"
)

// fileKind is the role a file is compiled in; a few engine keys are valid
// only in some kinds (A13).
type fileKind int

const (
	kindPage fileKind = iota
	kindLayout
	kindComponent
)

func (k fileKind) String() string {
	switch k {
	case kindLayout:
		return "layout"
	case kindComponent:
		return "component"
	}
	return "page"
}

// hashLen is how many hex characters of a content hash name generated
// defines and asset URLs. Pinned: changing it changes output (A14).
const hashLen = 8

// compiled is one file, compiled for one locale. It is immutable once built
// and shared by every render of the snapshot (T8).
type compiled struct {
	path    string // the FS path read (a language variant, C16)
	kind    fileKind
	hash    string
	meta    *meta
	set     *template.Template
	out     *transpile.Output
	targets []target
	locals  map[string]string // local component name → define
	data    map[string]any
	scripts map[string]ScriptModule // use: namespace → module; "" is inline
	client  []byte                  // <brick:client> source
	styles  []styleBlock
	strict  bool
	lines   []int // generated source line starts, for the source map
	// deps are the files this file's compile read besides itself: data
	// imports, scripts and their loads, src= files (Result.Files).
	deps []string
	// nodeNames are the literal names this file renders with {{ node }};
	// dynamicNodes is set when a name is computed, so no warning can be sure.
	nodeNames    map[string]bool
	dynamicNodes bool
	// multiRoot is set for a component without a single root element,
	// which cannot carry a caller's brick:on: listeners.
	multiRoot bool
	// rootLine is the file line of a single-root component's root element,
	// written as data-brick-loc in Dev mode; 0 when there is none.
	rootLine int
	// stateReads are the top-level .State keys the template reads, and
	// readsWholeState says it reads .State whole; they feed the renderless
	// action's Dev warning.
	stateReads      map[string]bool
	readsWholeState bool
	// methodLinks is set when the file has a brick:method link (plain or on
	// a component call), whose click needs the runtime on the page.
	methodLinks bool
}

// nodeCallRE finds `node` calls in generated source: a literal name, or not.
var nodeCallRE = regexp.MustCompile(`__node \$ ("(?:[^"\\]|\\.)*"|\S)`)

// provideAfterChildrenRE finds a provide that runs after children in the
// same template text, where the value reaches nothing (best-effort: it
// cannot see branches, C2).
var provideAfterChildrenRE = regexp.MustCompile(`__children \$[\s\S]*__provide \$`)

// styleBlock is one <brick:style> block, or the component's sibling .css.
type styleBlock struct {
	src    []byte
	scoped bool
	// bound is the scoped block's lower bound; line is the block's file
	// line, for a StyleTransform error.
	bound ScopeBound
	line  int
	// file is the sibling stylesheet's path; empty for an inline block.
	file string
}

// targetKind is what a call site resolved to.
type targetKind int

const (
	tComponent targetKind = iota
	tLocal
	tRender
	tBoundary
	tMarkdown
	tPassthrough
)

// target is a call site's resolution, fixed at compile time (lexical, C11).
type target struct {
	kind targetKind
	// name is the canonical component name, as reported in Result.Types.
	name string
	// path is the component file (tComponent).
	path string
	// local is the define of a local component (tLocal).
	local string
	// callee is the callee's frontmatter (tComponent), for raw and props.
	callee *meta
	// src is the bytes of a literal src= attribute on a raw component.
	src    []byte
	hasSrc bool
}

// metaEntry caches a file's frontmatter, read once per snapshot. Callers
// need a callee's `raw:` and `props:` before scanning their own body.
type metaEntry struct {
	meta *meta
	err  error
}

// load returns the compiled file for (path, locale), compiling it once.
func (e *Engine) load(ctx context.Context, path string, kind fileKind, locale string) (*compiled, error) {
	key := fileKey{path: path, locale: locale}
	e.mu.Lock()
	ent, ok := e.files[key]
	if !ok {
		ent = &fileEntry{}
		e.files[key] = ent
	}
	e.mu.Unlock()
	ent.once.Do(func() {
		src, err := e.readFile(e.variant(path, locale))
		if err != nil {
			ent.err = &Error{File: path, Code: CodeImport, Err: err}
			return
		}
		ent.file, ent.err = e.compile(ctx, e.variant(path, locale), src, kind)
	})
	return ent.file, ent.err
}

// compile runs the pipeline for one file: frontmatter → use: → registry →
// scan → tree → transpile → parse (S11).
func (e *Engine) compile(ctx context.Context, path string, src []byte, kind fileKind) (*compiled, error) {
	doc, err := frontmatter.Parse(src)
	if err != nil {
		var pe *frontmatter.PosError
		if errors.As(err, &pe) {
			return nil, &Error{File: path, Line: pe.Line, Code: CodeFrontmatter, Err: pe.Err}
		}
		return nil, &Error{File: path, Code: CodeFrontmatter, Err: err}
	}
	for _, w := range doc.Warnings {
		e.warn(Diagnostic{File: path, Line: w.Line, Code: CodeFrontmatter, Message: w.Message})
	}
	m, err := parseMeta(doc, path, e.resolveFrom, e.opts.PropTypes, e.opts.DataFormats)
	if err != nil {
		return nil, &Error{File: path, Line: metaErrLine(src, err), Code: CodeFrontmatter, Err: err}
	}
	if err := e.checkStoreName(m); err != nil {
		return nil, &Error{File: path, Line: 1, Code: CodeFrontmatter, Err: err}
	}
	if line, err := checkRules(src, m, e.opts.Validator); err != nil {
		return nil, &Error{File: path, Line: line, Code: CodeFrontmatter, Err: err}
	}
	if line, err := checkUploads(src, m, e.opts.Uploads != nil); err != nil {
		return nil, &Error{File: path, Line: line, Code: CodeFrontmatter, Err: err}
	}
	if line, err := checkQuery(src, m); err != nil {
		return nil, &Error{File: path, Line: line, Code: CodeFrontmatter, Err: err}
	}
	if line, err := checkRemember(src, m, e.opts.Remember != nil); err != nil {
		return nil, &Error{File: path, Line: line, Code: CodeFrontmatter, Err: err}
	}
	if line, err := checkLocked(src, m, e.opts.LockState); err != nil {
		return nil, &Error{File: path, Line: line, Code: CodeFrontmatter, Err: err}
	}
	if line, err := checkListen(src, m, e.opts.Push != nil); err != nil {
		return nil, &Error{File: path, Line: line, Code: CodeFrontmatter, Err: err}
	}
	for _, w := range m.warnings {
		e.warn(Diagnostic{File: path, Line: 1, Code: CodeEngineKey, Message: w})
	}
	e.checkKeysForKind(path, m, kind)

	sum := sha256.Sum256(src)
	c := &compiled{path: path, kind: kind, hash: hex.EncodeToString(sum[:])[:hashLen], meta: m, locals: map[string]string{}, data: map[string]any{}, scripts: map[string]ScriptModule{}}
	c.strict = e.opts.StrictParams
	if m.state != nil {
		c.stateReads, c.readsWholeState = stateReads(src)
	}
	if m.strictParams != nil {
		c.strict = *m.strictParams
	}

	// use: data and scripts load before the body is scanned, because script
	// namespaces are rewritten during transpile (T7).
	namespaces := map[string]bool{}
	aliases := map[string]string{}
	for _, u := range m.use {
		switch u.Kind {
		case useData:
			v, err := e.loadData(u.Path)
			if err != nil {
				return nil, &Error{File: path, Line: 1, Code: CodeImport, Err: fmt.Errorf("use: %s: %w", u.Key, err)}
			}
			c.data[u.Key] = v
			c.deps = append(c.deps, u.Path)
		case useScript:
			if builtinNames[u.Key] || slices.Contains(transpile.ScopedFuncs, u.Key) {
				return nil, &Error{File: path, Line: 1, Code: CodeReserved, Err: fmt.Errorf("use: %s shadows a built-in function (C12)", u.Key)}
			}
			mod, sdeps, err := e.loadScript(ctx, u.Path)
			c.deps = append(c.deps, sdeps...)
			if err != nil {
				return nil, &Error{File: path, Line: 1, Code: CodeScript, Err: fmt.Errorf("use: %s: %w", u.Key, err)}
			}
			c.scripts[u.Key] = mod
			namespaces[u.Key] = true
		case useComponent:
			if ok, err := e.exists(u.Path); err != nil || !ok {
				if err == nil {
					err = fmt.Errorf("%s does not exist", displayPath(u.Path))
					if u.Remote != "" {
						err = fmt.Errorf("%s is not vendored: run `brick vendor`", u.Remote)
					}
				}
				return nil, &Error{File: path, Line: 1, Code: CodeImport, Err: fmt.Errorf("use: %s: %w", u.Key, err)}
			}
			aliases[u.Key] = u.Path
		}
	}

	localNames := map[string]bool{}
	for _, mm := range localNameRE.FindAllSubmatch(doc.Body, -1) {
		localNames[string(mm[1])] = true
	}
	body := doc.Body
	line0 := doc.BodyLine
	isRaw := func(name string, attrs []lex.Attr) bool {
		switch name {
		case tree.TagScript, tree.TagClient, tree.TagStyle:
			return true
		case tree.TagProp:
			return slices.ContainsFunc(attrs, func(a lex.Attr) bool { return a.Name == tree.AttrRaw })
		}
		if localNames[name] || strings.HasPrefix(name, "brick:") {
			return false
		}
		if name == markdownTag && e.opts.Markdown != nil {
			if p, ok := e.lookupPath(name, aliases); !ok || p == "" {
				return true
			}
		}
		p, ok := e.lookupPath(name, aliases)
		if !ok {
			return false
		}
		cm, err := e.calleeMeta(p)
		return err == nil && cm.raw
	}
	toks, err := lex.Scan(body, lex.Options{IsRaw: isRaw})
	if err != nil {
		var le *lex.Error
		if errors.As(err, &le) {
			return nil, &Error{File: path, Line: le.Pos.Line + line0 - 1, Col: le.Pos.Col, Code: CodeSyntax, Err: errors.New(le.Msg)}
		}
		return nil, &Error{File: path, Code: CodeSyntax, Err: err}
	}
	// loadingNames are brick:loading's action names, checked once the
	// file's script is compiled.
	var loadingNames []string
	// loadingPos[i] is where loadingNames[i]'s attribute sits, so an
	// unknown target is reported at its line:col, not at the file's first line.
	var loadingPos []lex.Pos
	refs := map[string]bool{}
	if m.state != nil {
		if p, err := checkIgnoredRoot(toks); err != nil {
			return nil, &Error{File: path, Line: p.Line + line0 - 1, Col: p.Col, Code: CodeSyntax, Err: err}
		}
		for _, p := range unkeyedRanges(toks) {
			e.warn(Diagnostic{File: path, Line: p.Line + line0 - 1, Col: p.Col, Code: CodeUnkeyed, Message: "rows of this range have no brick:key; inserting a row re-renders every row after it"})
		}
	}
	for _, t := range toks {
		if t.Kind != lex.Text || t.RawBody {
			continue
		}
		for _, tag := range plainTagRE.FindAllIndex(t.Raw, -1) {
			for _, mm := range brickAttrRE.FindAllSubmatchIndex(t.Raw[tag[0]:tag[1]], -1) {
				raw := t.Raw[tag[0]:tag[1]]
				a := parseBrickAttrName(string(raw[mm[2]:mm[3]]))
				name := a.name
				p := advancePos(t.Pos, t.Raw[:tag[0]+mm[2]])
				for g := 4; g <= 8; g += 2 {
					if mm[g] >= 0 {
						a.value, a.hasValue = string(raw[mm[g]:mm[g+1]]), true
					}
				}
				if slices.Contains(plainBrickAttrs, name) || (m.state != nil && slices.Contains(statefulBrickAttrs, name)) {
					if err := checkPlainAttr(a, refs); err != nil {
						return nil, &Error{File: path, Line: p.Line + line0 - 1, Col: p.Col, Code: CodeSyntax, Err: err}
					}
					continue
				}
				if m.state != nil && slices.Contains(eventAttrs, name) {
					before := len(loadingNames)
					if err := checkBrickAttr(a, m, &loadingNames); err != nil {
						return nil, &Error{File: path, Line: p.Line + line0 - 1, Col: p.Col, Code: CodeSyntax, Err: err}
					}
					for range loadingNames[before:] {
						loadingPos = append(loadingPos, p)
					}
					continue
				}
				if m.state == nil && slices.Contains(methodCompanions, name) && hasMethodAttr(raw) {
					// A method link's own confirm and busy label
					// (checkMethodLinks checks them).
					continue
				}
				hint := ""
				if slices.Contains(eventAttrs, name) || slices.Contains(statefulBrickAttrs, name) {
					hint = "; action events and brick:transition need a component that declares state:"
				}
				return nil, &Error{File: path, Line: p.Line + line0 - 1, Col: p.Col, Code: CodeReserved, Err: fmt.Errorf("brick:%s is reserved (on plain elements brick accepts %s)%s", name, strings.Join(plainBrickAttrs, ", "), hint)}
			}
		}
	}
	hasMethod, mp, err := checkMethodLinks(toks)
	if err != nil {
		return nil, &Error{File: path, Line: mp.Line + line0 - 1, Col: mp.Col, Code: CodeSyntax, Err: err}
	}
	c.methodLinks = c.methodLinks || hasMethod
	for _, t := range toks {
		if t.Kind != lex.Text || t.RawBody {
			continue
		}
		for _, m := range rawTextRE.FindAllSubmatch(t.Raw, -1) {
			for _, tag := range tagNameRE.FindAllSubmatch(m[2], -1) {
				if _, ok := e.lookupPath(string(tag[1]), aliases); ok {
					e.warn(Diagnostic{File: path, Line: t.Pos.Line + line0 - 1, Code: CodeHTMLTag, Message: fmt.Sprintf("<%s> inside <%s> is text, not a component — nothing is rendered there (S1)", tag[1], m[1])})
				}
			}
		}
	}
	ft, err := tree.Build(toks)
	if err != nil {
		var te *tree.Error
		if errors.As(err, &te) {
			return nil, &Error{File: path, Line: te.Pos.Line + line0 - 1, Col: te.Pos.Col, Code: CodeSyntax, Err: errors.New(te.Msg)}
		}
		return nil, &Error{File: path, Code: CodeSyntax, Err: err}
	}

	// Inline blocks: scripts compile now, client and style are kept.
	scriptFuncs := map[string]bool{}
	for _, b := range ft.Blocks {
		text := blockText(b)
		switch b.Name {
		case tree.TagScript:
			mod, err := e.compileInline(ctx, path, text, b.Pos.Line+line0-1, &c.deps)
			if err != nil {
				return nil, err
			}
			for _, fn := range mod.Functions() {
				if builtinNames[fn] || slices.Contains(transpile.ScopedFuncs, fn) || namespaces[fn] {
					return nil, &Error{File: path, Line: b.Pos.Line + line0 - 1, Code: CodeReserved, Err: fmt.Errorf("<brick:script> function %q shadows a built-in or an import (C12)", fn)}
				}
				if _, host := e.opts.Funcs[fn]; host {
					return nil, &Error{File: path, Line: b.Pos.Line + line0 - 1, Code: CodeReserved, Err: fmt.Errorf("<brick:script> function %q shadows a function the host program registered in Options.Funcs (C12)", fn)}
				}
				if strings.HasPrefix(fn, "__") {
					return nil, &Error{File: path, Line: b.Pos.Line + line0 - 1, Code: CodeReserved, Err: fmt.Errorf("<brick:script> function %q: names starting with __ are reserved (T5)", fn)}
				}
				scriptFuncs[fn] = true
			}
			if prev, ok := c.scripts[""]; ok && prev != nil {
				return nil, &Error{File: path, Line: b.Pos.Line + line0 - 1, Code: CodeSyntax, Err: errors.New("a file has at most one <brick:script> block")}
			}
			c.scripts[""] = mod
		case tree.TagClient:
			c.client = append(c.client, text...)
		case tree.TagStyle:
			sa, scoped := b.Attr("scoped")
			sb := styleBlock{src: text, scoped: scoped, line: b.Pos.Line + line0 - 1}
			if scoped {
				bound, err := parseScopeBound(sa.Value)
				if err != nil {
					return nil, &Error{File: path, Line: sb.line, Code: CodeSyntax, Err: err}
				}
				sb.bound = bound
			}
			c.styles = append(c.styles, sb)
		}
	}

	if kind == kindComponent {
		if err := e.siblingCSS(c); err != nil {
			return nil, err
		}
	} else if m.css != "" {
		return nil, &Error{File: path, Line: brickLine(src, brickCSS), Code: CodeFrontmatter, Err: fmt.Errorf("brick: css: only a component has a sibling stylesheet, not a %s", kind)}
	}

	if err := checkListeners(c); err != nil {
		return nil, &Error{File: path, Line: 1, Code: CodeFrontmatter, Err: err}
	}
	if line, err := checkStateHooks(c, src); err != nil {
		return nil, &Error{File: path, Line: line, Code: CodeFrontmatter, Err: err}
	}
	for i, n := range loadingNames {
		if err := checkLoadingNames(c, []string{n}); err != nil {
			p := loadingPos[i]
			return nil, &Error{File: path, Line: p.Line + line0 - 1, Col: p.Col, Code: CodeSyntax, Err: err}
		}
	}

	// A scoped-style or client component gets its root element marked.
	var rootAttrs []string
	if slices.ContainsFunc(c.styles, func(s styleBlock) bool { return s.scoped }) {
		rootAttrs = append(rootAttrs, fmt.Sprintf("%s=%q", scopeAttr, c.hash))
	}
	if len(c.client) > 0 {
		rootAttrs = append(rootAttrs, "{{ "+fnClientAttrs+" $ }}")
	}
	if m.state != nil {
		rootAttrs = append(rootAttrs, "{{ "+fnStateAttrs+" $ }}")
	}
	if e.opts.EmitIDs && kind == kindComponent && len(c.client) == 0 {
		idAttr := "{{ " + fnIDAttr + " $ }}"
		if marked, err := markRoot(ft.Nodes, strings.Join(append(slices.Clone(rootAttrs), idAttr), " ")); err == nil {
			ft.Nodes, rootAttrs = marked, nil
		} else {
			e.warn(Diagnostic{File: path, Line: line0, Code: CodeEngineKey, Message: "EmitIDs: this component has no single root element, so its id is not written"})
		}
	}
	if len(rootAttrs) > 0 {
		if kind != kindComponent {
			return nil, &Error{File: path, Line: 1, Code: CodeSyntax, Err: fmt.Errorf("<brick:client> and <brick:style scoped> belong in a component, not a %s", kind)}
		}
		marked, err := markRoot(ft.Nodes, strings.Join(rootAttrs, " "))
		if err != nil {
			return nil, &Error{File: path, Line: line0, Code: CodeSyntax, Err: err}
		}
		ft.Nodes = marked
	}

	// Last, because the tight marker hides the tag from markRoot's scan.
	// Every single-root component can carry its callers' brick:on:
	// listeners; the marker writes nothing otherwise.
	if kind == kindComponent {
		marker := "{{ " + fnUpAttrs + " $ }}"
		if marked, err := markRootTight(ft.Nodes, marker); err == nil {
			ft.Nodes, c.rootLine = marked, markerLine(marked, marker, line0)
		} else {
			c.multiRoot = true
		}
	}

	// Resolve every call site, lexically: local → use: → roots (C11).
	for _, l := range ft.Locals {
		n, _ := l.Attr("name")
		c.locals[n.Value] = "" // filled from the transpile output
	}
	passthrough := func(el *tree.Element) bool {
		t, err := e.resolveTag(c, el, aliases, line0)
		return err == nil && t.kind == tPassthrough
	}
	var resolveErr error
	resolved := map[*tree.Element]target{}
	// walk resolves every element. depth counts the {{ }} blocks around it,
	// so a self-reference outside all of them — which can never stop — is a
	// compile error (C14); self names the local component being walked.
	var walk func(nodes []tree.Node, depth int, self string)
	walk = func(nodes []tree.Node, depth int, self string) {
		for _, n := range nodes {
			if a, ok := n.(tree.Action); ok {
				switch {
				case a.Tok.Act.Opens():
					depth++
				case a.Tok.Act == lex.ActEnd:
					depth--
				}
				continue
			}
			el, ok := n.(*tree.Element)
			if !ok {
				continue
			}
			t, err := e.resolveTag(c, el, aliases, line0)
			if err == nil && depth == 0 && ((t.kind == tComponent && t.path == c.path) || (t.kind == tLocal && t.name == self)) {
				err = &Error{File: c.path, Line: el.Pos.Line + line0 - 1, Col: el.Pos.Col, Code: CodeLimit, Err: fmt.Errorf("<%s> renders itself outside every {{ if }}/{{ range }}, so it can never stop (C14); put the recursion under a condition", el.Name)}
			}
			if err != nil && resolveErr == nil {
				resolveErr = err
			}
			resolved[el] = t
			for _, p := range el.Props {
				walk(p.Children, depth, self)
			}
			walk(el.Children, depth, self)
		}
	}
	walk(ft.Nodes, 0, "")
	for _, l := range ft.Locals {
		name, _ := l.Attr("name")
		walk(l.Children, 0, name.Value)
	}
	if resolveErr != nil {
		return nil, resolveErr
	}

	out, err := transpile.Transpile(transpile.Input{File: ft, Name: path, Hash: c.hash, Line: line0, Passthrough: passthrough, Namespaces: namespaces, ScriptFuncs: scriptFuncs})
	if err != nil {
		var te *transpile.Error
		if errors.As(err, &te) {
			return nil, &Error{File: path, Line: te.Pos.Line, Col: te.Pos.Col, Code: CodeSyntax, Err: errors.New(te.Msg)}
		}
		return nil, &Error{File: path, Code: CodeSyntax, Err: err}
	}
	c.out = out
	for _, w := range out.Warnings {
		if w.Parent != nil {
			if t, ok := resolved[w.Parent]; ok && t.callee != nil && !t.callee.readsChildren {
				continue
			}
		}
		e.warn(Diagnostic{File: path, Line: w.Pos.Line, Col: w.Pos.Col, Code: CodeChildrenIf, Message: w.Msg})
	}
	for _, l := range out.Locals {
		c.locals[l.Name] = l.Define
	}
	c.targets = make([]target, len(out.Calls))
	for i, call := range out.Calls {
		t := resolved[call.Element]
		if t.kind == tLocal {
			t.local = c.locals[t.name]
		}
		if err := e.checkCall(c, call, t); err != nil {
			return nil, err
		}
		c.targets[i] = t
	}

	funcs := e.funcMap()
	fileFuncs := template.FuncMap{}
	for ns, mod := range c.scripts {
		for _, fn := range mod.Functions() {
			fileFuncs[transpile.CallName(ns, fn)] = scriptCaller(path, ns, fn, mod)
		}
	}
	for _, fn := range usedCalls(out.Source) {
		if _, ok := fileFuncs[fn]; !ok {
			ns, name, _ := strings.Cut(strings.TrimPrefix(fn, transpile.FnCallPrefix), "_")
			return nil, &Error{File: path, Line: 1, Code: CodeScript, Err: fmt.Errorf("%s.%s: the module has no function %q", ns, name, name)}
		}
	}
	set := template.New(path).Funcs(funcs).Funcs(fileFuncs)
	if c.strict {
		set = set.Option("missingkey=error")
	}
	c.lines = lineStarts(out.Source)
	if err := e.checkSlots(c, resolved, line0); err != nil {
		return nil, err
	}
	c.nodeNames = map[string]bool{}
	for _, m := range nodeCallRE.FindAllStringSubmatch(out.Source, -1) {
		if name, err := strconv.Unquote(m[1]); err == nil {
			c.nodeNames[name] = true
		} else {
			c.dynamicNodes = true
		}
	}
	mainSrc, _, _ := strings.Cut(out.Source, `{{ define "__brick_`)
	if slotRE.MatchString(mainSrc) {
		e.warn(Diagnostic{File: path, Code: CodeSlot, Message: ".Slot is read outside any <brick:prop> block, where it is always empty; a node body reads the value passed to {{ node \"x\" v }}"})
	}
	if provideAfterChildrenRE.MatchString(mainSrc) {
		e.warn(Diagnostic{File: path, Code: CodeProvide, Message: "{{ provide }} after {{ children }} reaches nothing: the children have already rendered (C2)"})
	}
	if _, err := set.Parse(out.Source); err != nil {
		return nil, c.mapError(err, CodeSyntax)
	}
	c.set = set
	c.deps = append(c.deps, path)
	slices.Sort(c.deps)
	c.deps = slices.Compact(c.deps)
	if len(c.client) > 0 || len(c.styles) > 0 {
		stem, _, _ := splitStem(strings.TrimSuffix(pathBase(path), htmlExt))
		if stem == "" {
			stem = "page"
		}
		if err := e.registerAssets(c, stem); err != nil {
			return nil, asError(err, CodeImport, path)
		}
	}
	if m.loadMany != "" && kind != kindComponent {
		return nil, &Error{File: path, Line: 1, Code: CodeScript, Err: fmt.Errorf("brick: load_many: only a component can batch its loader; a page or layout renders once, so use load: instead")}
	}
	if m.load != "" || m.loadMany != "" {
		if err := e.checkLoader(c); err != nil {
			return nil, err
		}
	}
	if len(e.hooks.compile) > 0 {
		cf := &CompiledFile{Path: path, Params: cloneMap(m.params)}
		for _, call := range out.Calls {
			cf.Components = append(cf.Components, call.Name)
		}
		for _, h := range e.hooks.compile {
			func() {
				defer func() { _ = recover() }()
				h.OnCompile(ctx, cf)
			}()
		}
	}
	return c, nil
}

// rawTextRE finds <textarea> and <title> elements, whose content is text.
var rawTextRE = regexp.MustCompile(`(?is)<(textarea|title)\b[^>]*>(.*?)</(?:textarea|title)>`)

// tagNameRE finds component-looking tag names in text.
var tagNameRE = regexp.MustCompile(`<([A-Z][A-Za-z0-9]*|x-[a-z0-9-]+)\b`)

// slotRE finds a .Slot read (as a field of dot or of $).
var slotRE = regexp.MustCompile(`(?:^|[^\w)\]])\$?\.Slot\b`)

// callRE finds rewritten script calls in generated source.
var callRE = regexp.MustCompile(`\b` + transpile.FnCallPrefix + `[A-Za-z0-9_]+`)

func usedCalls(src string) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range callRE.FindAllString(src, -1) {
		if !seen[m] {
			seen[m] = true
			out = append(out, m)
		}
	}
	return out
}

// checkKeysForKind warns about an engine key in a kind of file that ignores
// it (A13, C3).
func (e *Engine) checkKeysForKind(path string, m *meta, kind fileKind) {
	warn := func(key, why string) {
		e.warn(Diagnostic{File: path, Line: 1, Code: CodeEngineKey, Message: fmt.Sprintf("%s: %s", key, why)})
	}
	switch kind {
	case kindComponent:
		if m.hasLay {
			e.warn(Diagnostic{File: path, Line: 1, Code: CodeLayoutIgnored, Message: "layout: is ignored in a component — components cannot use layouts"})
		}
	case kindPage, kindLayout:
		if m.raw {
			warn(keyRaw, "only a component can take a raw body; ignored in a "+kind.String())
		}
		if m.props != nil && kind == kindPage {
			warn(keyProps, "a page's props come from Render and are not declared; ignored")
		}
	}
}

// blockText returns a raw block's text.
func blockText(el *tree.Element) []byte {
	var b bytes.Buffer
	for _, n := range el.Children {
		if t, ok := n.(tree.Text); ok {
			b.Write(t.Raw)
		}
	}
	return b.Bytes()
}
