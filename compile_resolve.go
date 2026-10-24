// Resolving a component tag to its file: path lookup, tag resolution, and the callee metadata (props, children) a caller is checked against.

package brick

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/ubgo/brick/internal/frontmatter"
	"github.com/ubgo/brick/internal/tree"
)

// lookupPath resolves a tag to a component file through use: and the roots.
func (e *Engine) lookupPath(tag string, aliases map[string]string) (string, bool) {
	name := canonicalName(tag)
	if p, ok := aliases[tag]; ok {
		return p, true
	}
	for k, p := range aliases {
		if strings.HasPrefix(tag, "x-") && strings.EqualFold(k, name) {
			return p, true
		}
	}
	r, err := e.reg()
	if err != nil {
		return "", false
	}
	if en, ok := r.lookup(tag); ok {
		return en.path, true
	}
	return "", false
}

// markdownTag is the built-in markdown component's name.
const markdownTag = "Markdown"

// Engine-owned names on engine tags (C4, C13).
const (
	// nodeFallback and nodeLoading are the blocks a boundary takes.
	nodeFallback = "fallback"
	nodeLoading  = "loading"
	// renderName and renderProps are <brick:render>'s engine-owned props.
	renderName  = "name"
	renderProps = "props"
	// slotMessage and slotComponent are what a boundary fallback reads as
	// .Slot.Message and .Slot.Component.
	slotMessage   = "Message"
	slotComponent = "Component"
)

// resolveTag resolves one element, lexically (C11, S7, S8).
func (e *Engine) resolveTag(c *compiled, el *tree.Element, aliases map[string]string, line0 int) (target, error) {
	pos := func() (int, int) { return el.Pos.Line + line0 - 1, el.Pos.Col }
	fail := func(code Code, format string, a ...any) (target, error) {
		l, col := pos()
		return target{}, &Error{File: c.path, Line: l, Col: col, Code: code, Err: fmt.Errorf(format, a...)}
	}
	switch el.Name {
	case tree.TagRender:
		return target{kind: tRender, name: el.Name}, nil
	case tree.TagBoundary:
		for _, p := range el.Props {
			n, _ := p.Attr("name")
			switch n.Value {
			case nodeFallback:
			case nodeLoading:
				// The placeholder of a lazy island inside.
			default:
				return fail(CodeSyntax, "<brick:boundary> takes only fallback and loading blocks, not %q", n.Value)
			}
		}
		return target{kind: tBoundary, name: el.Name}, nil
	}
	syntax := e.opts.Syntax
	if c.meta.syntax != nil {
		syntax = *c.meta.syntax
	}
	if syntaxRejects(syntax, el.Name) {
		return fail(CodeSyntax, "<%s>: this file accepts only the %s tag form", el.Name, syntaxWords[syntax])
	}
	if slices.Contains(e.opts.Passthrough, el.Name) || slices.Contains(c.meta.passthrough, el.Name) {
		return target{kind: tPassthrough, name: el.Name}, nil
	}
	name := canonicalName(el.Name)
	if _, ok := c.locals[name]; ok {
		return target{kind: tLocal, name: name}, nil
	}
	// A broken root or library (a missing manifest, an unknown only-name)
	// is reported as itself, never as an unknown component.
	if _, err := e.reg(); err != nil {
		return fail(CodeImport, "%v", err)
	}
	p, ok := e.lookupPath(el.Name, aliases)
	if !ok && name == markdownTag && e.opts.Markdown != nil {
		return target{kind: tMarkdown, name: markdownTag}, nil
	}
	if !ok {
		if knownHTMLTag(el.Name) {
			l, col := pos()
			e.warn(Diagnostic{File: c.path, Line: l, Col: col, Code: CodeHTMLTag, Message: fmt.Sprintf("<%s> is an HTML element written in capitals; it is emitted as HTML (S7)", el.Name)})
			return target{kind: tPassthrough, name: el.Name}, nil
		}
		policy := e.opts.Unknown
		if c.meta.unknown != nil {
			policy = *c.meta.unknown
		}
		if policy == UnknownPassthrough {
			return target{kind: tPassthrough, name: el.Name}, nil
		}
		return fail(CodeUnknownTag, "unknown component <%s> — no file in use: or the roots (%s) defines it", el.Name, strings.Join(e.opts.Roots, ", "))
	}
	if p == c.path && strings.HasPrefix(el.Name, "x-") {
		// Inside its own file, the hyphen form is the custom element of the
		// same name — the progressive-enhancement hook the x- form exists
		// for — never a call to itself.
		return target{kind: tPassthrough, name: el.Name}, nil
	}
	cm, err := e.calleeMeta(p)
	if err != nil {
		return target{}, err
	}
	t := target{kind: tComponent, name: name, path: p, callee: cm}
	if cm.raw {
		for _, a := range el.Attrs {
			switch a.Name {
			case ":" + propSrc:
				return fail(CodeImport, ":src is never allowed: a file is chosen only by a literal src= written in the file (X3)")
			case propSrc:
				fp, err := e.resolveFrom(c.path, a.Value)
				if err != nil {
					return fail(CodePath, "src=: %v", err)
				}
				b, err := e.readFile(fp)
				if err != nil {
					return fail(CodeImport, "src=: %v", err)
				}
				t.src, t.hasSrc = b, true
				c.deps = append(c.deps, fp)
			}
		}
	}
	return t, nil
}

// calleeMeta reads a component's frontmatter only.
func (e *Engine) calleeMeta(path string) (*meta, error) {
	e.mu.Lock()
	ent, ok := e.metas[path]
	e.mu.Unlock()
	if ok {
		return ent.meta, ent.err
	}
	ent = &metaEntry{}
	if src, err := e.readFile(path); err != nil {
		ent.err = &Error{File: path, Code: CodeImport, Err: err}
	} else {
		ent.meta, ent.err = e.readCalleeMeta(path, src)
	}
	e.mu.Lock()
	e.metas[path] = ent
	e.mu.Unlock()
	return ent.meta, ent.err
}

// readCalleeMeta parses a component's frontmatter for a caller. Its errors
// carry the same file, line and code as compiling the component directly
// would report (a brick: key error on that key's line), so `brick check`
// lands on the mistake whether the file was reached directly or through a
// page.
func (e *Engine) readCalleeMeta(path string, src []byte) (*meta, error) {
	doc, err := frontmatter.Parse(src)
	if err != nil {
		var pe *frontmatter.PosError
		if errors.As(err, &pe) {
			return nil, &Error{File: path, Line: pe.Line, Code: CodeFrontmatter, Err: pe.Err}
		}
		return nil, &Error{File: path, Code: CodeFrontmatter, Err: err}
	}
	m, err := parseMeta(doc, path, e.resolveFrom, e.opts.PropTypes, e.opts.DataFormats)
	if err != nil {
		return nil, &Error{File: path, Line: metaErrLine(src, err), Code: CodeFrontmatter, Err: err}
	}
	if err := e.checkStoreName(m); err != nil {
		return nil, &Error{File: path, Line: 1, Code: CodeFrontmatter, Err: err}
	}
	m.readsChildren = readsChildrenRE.Match(tmplCommentRE.ReplaceAll(src, nil))
	return m, nil
}
