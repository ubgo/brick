package brick

import (
	"bytes"
	"fmt"
	"strings"
)

// ScopeBound is where a scoped stylesheet stops when it meets a nested
// component with scoped styles of its own: the value of
// <brick:style scoped="…">.
type ScopeBound string

const (
	// ScopeExclusive (bare `scoped`, the default) stops at the nested
	// scoped root: an outer component never restyles an inner one at all.
	ScopeExclusive ScopeBound = "exclusive"
	// ScopeInclusive lets the outer rules reach the nested scoped root
	// itself but not its insides, so a parent can place or size a child
	// (margin, width, grid area) without restyling what the child draws.
	ScopeInclusive ScopeBound = "inclusive"
)

// StyleBlock is one <brick:style> block, or a component's sibling .css,
// as Options.StyleTransform sees it.
type StyleBlock struct {
	// File is the component's path ("~/components/Card.html").
	File string
	// Sheet is the sibling stylesheet's path ("components/Card.css") when
	// the block is that file, empty for an inline <brick:style>.
	Sheet string
	// Hash is the content hash written on the component root as
	// data-brick-s when the block is scoped.
	Hash string
	// Scoped says the block was written <brick:style scoped>; Bound is its
	// lower bound (empty when not scoped).
	Scoped bool
	Bound  ScopeBound
	// Source is the block as written; CSS is what the engine ships for it
	// (wrapped in @scope when scoped). Returning CSS unchanged is the
	// identity transform.
	Source []byte
	CSS    []byte
}

// parseScopeBound reads the scoped attribute: bare or "exclusive" keeps
// the fixed lower bound, "inclusive" opens it by one element. Anything
// else is an error rather than a silent default, since a typo would ship
// rules that never apply where the author expects.
func parseScopeBound(v string) (ScopeBound, error) {
	switch ScopeBound(v) {
	case "", ScopeExclusive:
		return ScopeExclusive, nil
	case ScopeInclusive:
		return ScopeInclusive, nil
	}
	return "", fmt.Errorf("<brick:style scoped=%q>: scoped is bare, %q or %q", v, ScopeExclusive, ScopeInclusive)
}

// scopeCSS wraps a scoped block in native CSS @scope, anchored on the
// root's data-brick-s marker; no selector of the author's is rewritten
// (attribute hashing is a non-goal).
//
// The lower bound is the root of any nested scoped component. Exclusive
// stops at that root (today's bytes, unchanged). Inclusive stops at its
// children. A `to ()` selector is relative to the scoping root (an
// implicit `:scope ` in front), so `[data-brick-s] > *` never matches this
// component's own children even though its root carries the marker too;
// checked in headless Chrome. As in any @scope, a plain selector matches
// below the root, and `:scope { … }` styles the root itself.
//
// @scope, including :scope inside to (), is in Chrome and Edge 118+,
// Safari 17.4+ and Firefox 146+. An older browser skips the whole block:
// the component still works, unstyled by it.
func scopeCSS(hash string, bound ScopeBound, src []byte) []byte {
	limit := "[" + scopeAttr + "]"
	if bound == ScopeInclusive {
		limit += " > *"
	}
	return fmt.Appendf(nil, "@scope ([%s=%q]) to (%s) {\n%s\n}\n", scopeAttr, hash, limit, bytes.TrimSpace(src))
}

// styleCSS is the CSS one block ships: global blocks as written, scoped
// ones wrapped, then Options.StyleTransform when the host set one.
func (e *Engine) styleCSS(c *compiled, s styleBlock) ([]byte, error) {
	var css []byte
	if s.scoped {
		css = scopeCSS(c.hash, s.bound, s.src)
	} else {
		css = append(bytes.Clone(bytes.TrimSpace(s.src)), '\n')
	}
	if e.opts.StyleTransform == nil {
		return css, nil
	}
	b := StyleBlock{File: c.path, Sheet: s.file, Hash: c.hash, Scoped: s.scoped, Source: bytes.Clone(s.src), CSS: css}
	if s.scoped {
		b.Bound = s.bound
	}
	out, err := e.opts.StyleTransform(b)
	if err != nil {
		file := c.path
		if s.file != "" {
			file = s.file
		}
		return nil, &Error{File: file, Line: s.line, Code: CodeSyntax, Err: fmt.Errorf("Options.StyleTransform: %w", err)}
	}
	return out, nil
}

// extCSSFile is a sibling stylesheet's extension: components/Card.css
// belongs to components/Card.html.
const extCSSFile = ".css"

// siblingCSS adds a component's sibling stylesheet, if one exists, as the
// first style block: Card.css next to Card.html (language variants such as
// Card.fr.html share it). It ships exactly like an inline <brick:style>,
// in the same content-hashed sheet, ahead of the inline blocks so an
// inline rule wins a tie. It is global unless brick: {css:} says scoped or
// inclusive — one default for both spellings of a component's style.
//
// The path comes from the component's own (already confined) path, never
// from data, so this reads no file a template could choose (X1, X2). The
// file is a dependency of the component, so editing it reaches
// Result.Files and the dev server's live reload.
func (e *Engine) siblingCSS(c *compiled) error {
	dir, base := "", c.path
	if i := strings.LastIndexByte(c.path, '/'); i >= 0 {
		dir, base = c.path[:i+1], c.path[i+1:]
	}
	stem, _, ok := splitStem(strings.TrimSuffix(base, htmlExt))
	if !ok {
		return nil
	}
	sheet := dir + stem + extCSSFile
	found, err := e.exists(sheet)
	if err != nil {
		return &Error{File: c.path, Code: CodeImport, Err: fmt.Errorf("sibling stylesheet %s: %w", displayPath(sheet), err)}
	}
	if !found {
		if c.meta.css != "" {
			e.warn(Diagnostic{File: c.path, Line: 1, Code: CodeEngineKey, Message: fmt.Sprintf("brick: css: %s is set, but there is no %s next to this component; nothing ships", c.meta.css, displayPath(sheet))})
		}
		return nil
	}
	src, err := e.readFile(sheet)
	if err != nil {
		return &Error{File: sheet, Code: CodeImport, Err: err}
	}
	sb := styleBlock{src: src, line: 1, file: sheet}
	if c.meta.css == cssScoped || c.meta.css == cssInclusive {
		sb.scoped, sb.bound = true, ScopeExclusive
		if c.meta.css == cssInclusive {
			sb.bound = ScopeInclusive
		}
	}
	c.styles = append([]styleBlock{sb}, c.styles...)
	c.deps = append(c.deps, sheet)
	return nil
}
