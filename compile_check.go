// Compile-time checks on the calls and attributes a file makes: action and dispatch calls, string-valued attributes, and Engine.checkCall.

package brick

import (
	"cmp"
	"encoding/json"
	"fmt"
	"html"
	"regexp"
	"slices"
	"strings"

	"github.com/ubgo/brick/internal/lex"
	"github.com/ubgo/brick/internal/transpile"
)

// syntaxRejects applies a Syntax (Options.Syntax, or a file's syntax: key).
func syntaxRejects(sx Syntax, tag string) bool {
	hyphen := strings.HasPrefix(tag, "x-")
	switch sx {
	case SyntaxCapitalized:
		return hyphen
	case SyntaxHyphenated:
		return !hyphen
	}
	return false
}

// stringValuedPrefixes are the attribute families whose values are always
// strings the browser or a library reads: aria-expanded="false",
// data-count="0", hx-swap="0".
var stringValuedPrefixes = []string{"aria-", "data-", "hx-"}

// falseStrings are the literal values that look like a boolean false but,
// as an attribute value, are non-empty text and so truthy (C6).
var falseStrings = []string{"false", "0"}

// stringValuedAttrs are attribute names whose "false" or "0" is a real
// value the browser reads — aria-expanded="false", tabindex="0",
// hx-swap="0" — never a boolean flag, so C6 does not warn about them.
var stringValuedAttrs = []string{"value", "tabindex", "spellcheck", "autocorrect", "autocapitalize", "translate", "draggable", "contenteditable", "min", "max", "step", "start", "rows", "cols", "span", "colspan", "rowspan", "width", "height"}

// stringValuedAttr reports a name C6 leaves alone: a string-valued HTML
// attribute or one of the aria-, data- and hx- families.
func stringValuedAttr(name string) bool {
	return slices.ContainsFunc(stringValuedPrefixes, func(p string) bool { return strings.HasPrefix(name, p) }) || slices.Contains(stringValuedAttrs, name)
}

// checkCall runs the compile-time checks of one call site: reserved
// attributes, and props written literally against the callee's declarations.
func (e *Engine) checkCall(c *compiled, call *transpile.Call, t target) error {
	fail := func(p lex.Pos, code Code, format string, a ...any) error {
		return &Error{File: c.path, Line: p.Line, Col: p.Col, Component: call.Name, Code: code, Err: fmt.Errorf(format, a...)}
	}
	for _, p := range call.Props {
		if p.Name == mountAttr {
			if p.Kind != transpile.Literal || !slices.Contains(mountModes, p.Text) {
				return fail(p.Pos, CodeSyntax, "%s takes %s, written literally", mountAttr, strings.Join(mountModes, " or "))
			}
			continue
		}
		if p.Name == lazyAttr {
			if t.kind != tComponent && t.kind != tRender {
				return fail(p.Pos, CodeReserved, "%s on <%s>: only a component file can load lazily", lazyAttr, call.Name)
			}
			if p.Kind == transpile.Bare {
				continue
			}
			if p.Kind != transpile.Literal {
				return fail(p.Pos, CodeSyntax, "%s takes %s, written literally", lazyAttr, strings.Join(lazyModes, ", "))
			}
			if err := checkLazyValue(cmp.Or(p.Text, LazyLoad)); err != nil {
				return fail(p.Pos, CodeSyntax, "%v", err)
			}
			continue
		}
		if strings.HasPrefix(p.Name, upPrefix) {
			if t.kind != tComponent {
				return fail(p.Pos, CodeReserved, "%s on <%s>: call-site listeners go on a component", p.Name, call.Name)
			}
			if err := checkUpListener(c, call.Name, p.Name, p.Text, p.Kind == transpile.Literal); err != nil {
				return fail(p.Pos, CodeSyntax, "%v", err)
			}
			continue
		}
		if strings.HasPrefix(p.Name, "brick:") && p.Name != transpile.SpreadAttr {
			// A navigation attribute, or an action event inside a stateful
			// file, is an ordinary undeclared attribute on a component call:
			// the callee places it with {{ attrs }}, so <Input brick:model="name">
			// binds the real <input> the Input component renders.
			a := parseBrickAttrName(strings.TrimPrefix(p.Name, "brick:"))
			base := a.name
			if base == attrMethod {
				// The callee places it on its <a href> with {{ attrs }};
				// that file's check sees only {{ attrs }}, so the verb is
				// checked here and this file loads the runtime.
				a.value, a.hasValue = "{{ dynamic }}", true // filled at render time
				if p.Kind == transpile.Literal {
					a.value = p.Text
				}
				if err := checkMethodAttr(a); err != nil {
					return fail(p.Pos, CodeSyntax, "%v", err)
				}
				c.methodLinks = true
				continue
			}
			if slices.Contains(plainBrickAttrs, base) {
				continue
			}
			if c.meta.state == nil && slices.Contains(methodCompanions, base) && slices.ContainsFunc(call.Props, func(q transpile.Prop) bool { return q.Name == "brick:method" }) {
				if len(a.mods) > 0 {
					return fail(p.Pos, CodeSyntax, "%s: on a method link brick:%s takes no modifier (.prompt's answer has no call to go to)", a.fullName(), base)
				}
				continue
			}
			if c.meta.state != nil && slices.Contains(eventAttrs, base) {
				if p.Kind == transpile.Literal {
					a.value, a.hasValue = p.Text, true
				} else {
					a.value = "{{ dynamic }}" // checked at render time
				}
				var loading []string
				if err := checkBrickAttr(a, c.meta, &loading); err != nil {
					return fail(p.Pos, CodeSyntax, "%v", err)
				}
				if err := checkLoadingNames(c, loading); err != nil {
					return fail(p.Pos, CodeSyntax, "%v", err)
				}
				continue
			}
			hint := ""
			if slices.Contains(eventAttrs, base) {
				hint = "; action events need a component that declares state:"
			}
			return fail(p.Pos, CodeReserved, "brick:%s is reserved on a component call (brick accepts %s, and action events inside a stateful component)%s", strings.TrimPrefix(p.Name, "brick:"), strings.Join(plainBrickAttrs, ", "), hint)
		}
	}
	switch t.kind {
	case tRender:
		hasName := false
		for _, p := range call.Props {
			if p.Name == renderName {
				hasName = true
			}
			if p.Name == propSrc {
				return fail(p.Pos, CodeImport, "src cannot reach a component chosen from data (X3)")
			}
		}
		if !hasName {
			return fail(call.Pos, CodeSyntax, "<brick:render> needs :name (the component name, from data)")
		}
		return nil
	case tBoundary, tPassthrough, tMarkdown:
		return nil
	case tLocal:
		return nil
	}
	cm := t.callee
	if call.HasRaw && !cm.raw {
		return fail(call.Pos, CodeSyntax, "<%s> is not a raw component", call.Name)
	}
	if cm.raw && t.hasSrc && call.HasRaw && strings.TrimSpace(call.Raw) != "" {
		return fail(call.Pos, CodeSyntax, "<%s> has both src= and a body; use one", call.Name)
	}
	given := map[string]bool{}
	for _, p := range call.Props {
		given[p.Name] = true
		if p.Name == propSrc && cm.raw {
			continue
		}
		if p.Kind == transpile.Literal && p.Name != propKey {
			if slices.Contains(falseStrings, p.Text) && cm.props[p.Name] == nil && !stringValuedAttr(p.Name) {
				e.warn(Diagnostic{File: c.path, Line: p.Pos.Line, Col: p.Pos.Col, Code: CodeFalseString, Message: fmt.Sprintf("%s=%q on <%s> is the text %q, which is truthy; declare the prop as bool or write a bare attribute (C6)", p.Name, p.Text, call.Name, p.Text)})
			}
		}
		d := cm.props[p.Name]
		if d == nil {
			if cm.strictProps && p.Name != propKey && !strings.HasPrefix(p.Name, upPrefix) {
				return fail(p.Pos, CodeProp, "prop %q is not declared by <%s>, which sets strictProps", p.Name, call.Name)
			}
			continue
		}
		var v any
		switch p.Kind {
		case transpile.Literal:
			v = p.Text
		case transpile.Bare:
			v = true
		default:
			continue
		}
		if _, err := checkProp(d, v); err != nil {
			return fail(p.Pos, CodeProp, "prop %q: %v", p.Name, err)
		}
	}
	for _, rp := range call.RawProps {
		given[rp.Name] = true
	}
	for _, n := range call.Nodes {
		given[n.Name] = true
		if d := cm.props[n.Name]; d != nil && d.Client {
			return fail(n.Pos, CodeProp, "prop %q is a node, so it cannot be client: true (D12)", n.Name)
		}
	}
	if cm.load == "" && cm.loadMany == "" {
		for _, name := range cm.order {
			if d := cm.props[name]; d.Required && !d.HasDefault && !given[name] {
				return fail(call.Pos, CodeProp, "<%s> requires the prop %q", call.Name, name)
			}
		}
	}
	return nil
}

// localNameRE finds local component names before scanning, so the scanner
// does not treat a local that shadows a raw registry component as raw.
var localNameRE = regexp.MustCompile(`<brick:component\s+name\s*=\s*["']([A-Za-z][A-Za-z0-9-]*)["']`)

// plainTagRE finds a plain start tag; brickAttrRE finds each `brick:`
// attribute inside one: its full name with every modifier (1), and its
// value double-quoted (2), single-quoted (3) or bare (4).
var (
	plainTagRE  = regexp.MustCompile(`<[a-zA-Z][^<>]*>`)
	brickAttrRE = regexp.MustCompile(`\sbrick:([a-zA-Z][\w-]*(?:\.[\w-]+)*)(?:\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s"'=<>` + "`" + `]+)))?`)
	// tmplCommentRE and readsChildrenRE tell whether a file's template
	// really uses .Children: inside an action, not in a doc comment.
	tmplCommentRE   = regexp.MustCompile(`(?s)\{\{-?\s*/\*.*?\*/\s*-?\}\}`)
	readsChildrenRE = regexp.MustCompile(`\{\{[^}]*\.Children\b`)
	// actionCallRE is the browser runtime's parseCall grammar: a name (or
	// one of the built-ins $set, $refresh, $toggle), and optionally JSON
	// arguments in parentheses.
	// $dispatch("event", data, to) and $parent.fn are client-side
	//.
	// $uploaded, $removeUpload and $cancelUpload are the upload built-ins
	//; $upload itself is never written in a call.
	// $reset(path…) is.
	actionCallRE = regexp.MustCompile(`(?s)^\s*(\$(?:set|refresh|toggle|reset|dispatch|uploaded|removeUpload|cancelUpload)|\$parent\.[\w.]+|[\w.]+)\s*(?:\((.*)\))?\s*$`)
)

// checkActionCall rejects an action call the browser could not parse —
// brick:click="mode('x')" would otherwise do nothing when clicked, with no
// error anywhere. Arguments are JSON literals: strings in double quotes. A
// value with {{ }} is filled at render time and checked by the runtime.
func checkActionCall(v string) error {
	v = html.UnescapeString(v)
	if strings.Contains(v, "{{") {
		return nil
	}
	m := actionCallRE.FindStringSubmatch(v)
	if m == nil {
		return fmt.Errorf("%q is not an action call: write name or name(args)", v)
	}
	var out []json.RawMessage
	if args := strings.TrimSpace(m[2]); args != "" {
		// Parsed only to validate: RawMessage keeps each argument unread.
		if err := json.Unmarshal([]byte("["+args+"]"), &out); err != nil {
			return fmt.Errorf("%q: arguments must be JSON literals (strings in double quotes, so the attribute takes single quotes: brick:click='f(\"x\")'): %v", v, err)
		}
	}
	if m[1] == actionDispatch {
		return checkDispatchCall(v, out)
	}
	return nil
}

// actionDispatch is the client-side event call.
const actionDispatch = "$dispatch"

// checkDispatchCall checks $dispatch("event", data?, to?): the event is a
// valid, unreserved name, data an object or null, to a string.
func checkDispatchCall(v string, args []json.RawMessage) error {
	if len(args) == 0 || len(args) > 3 {
		return fmt.Errorf("%q: %s takes an event name, then optional data and to", v, actionDispatch)
	}
	var name string
	if err := json.Unmarshal(args[0], &name); err != nil {
		return fmt.Errorf("%q: %s's first argument is the event name, a string", v, actionDispatch)
	}
	if err := checkEventName(name); err != nil {
		return fmt.Errorf("%q: %w", v, err)
	}
	if len(args) > 1 {
		var data map[string]any
		if err := json.Unmarshal(args[1], &data); err != nil {
			return fmt.Errorf("%q: %s's data is an object or null", v, actionDispatch)
		}
	}
	if len(args) > 2 {
		var to string
		if err := json.Unmarshal(args[2], &to); err != nil {
			return fmt.Errorf("%q: %s's to is a component name or \"self\"", v, actionDispatch)
		}
	}
	return nil
}
