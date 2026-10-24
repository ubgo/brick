package lsp

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/ubgo/brick"
	"github.com/ubgo/brick/internal/frontmatter"
	"github.com/ubgo/brick/internal/lex"
)

// Prop references and rename. A prop has three kinds of site: where it is
// declared (the component's `props:` or `brick: {slots:}`), where the
// component reads it (`.Props.x` and its relatives, in the component's
// file and its locale variants), and where a caller passes it (an
// attribute, plain or `:`-prefixed, or a `<brick:prop name>` block). A
// rename edits all three or nothing: a site brick cannot follow (a spread,
// `.Props` handed on whole, a load: script that returns props by name)
// refuses the rename with its location, because a partial rename leaves a
// page that compiles and silently renders an empty prop. Sites that may or
// may not be this prop (a `<brick:render>` choosing its component at run
// time, `.Props.x` where dot is something else) are left alone and
// reported, so nothing is skipped silently.

// Names the prop scanner recognises.
const (
	fieldProps    = "Props"
	fieldAttrs    = "Attrs"
	funcIndex     = "index"
	funcAttrs     = "attrs"
	funcNode      = "node"
	funcHasNode   = "hasNode"
	attrSpread    = "brick:spread"
	tagRender     = "brick:render"
	exprPrefix    = ":"
	childrenProp  = "children"
	loadKey       = "load"
	brickKey      = "brick"
	slotsKey      = "slots"
	messageWarn   = 2
	methodShowMsg = "window/showMessage"
	// reportMax is how many locations a refusal or report lists before
	// it says "and N more": enough to find the pattern, short enough to
	// read in an editor's notification.
	reportMax = 3
)

// propNameRE is a prop name that works everywhere a prop is written: as
// an attribute, a `<brick:prop name>` and a `.Props.x` field (a Go
// template field is a Go identifier).
var propNameRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// nameArgs are the template functions whose leading string arguments
// name a prop or block: how many (-1 for all of them).
var nameArgs = map[string]int{funcAttrs: -1, funcNode: 1, funcHasNode: 1}

// Script access to props in a component's own <brick:script>: the
// literal-key forms are renamed, a computed key cannot be followed.
var (
	scriptPropKeyRE = regexp.MustCompile(`\bprops\s*(?:\[\s*|\.get\(\s*)["']([A-Za-z_][A-Za-z0-9_]*)["']`)
	scriptPropDynRE = regexp.MustCompile(`\bprops\s*\[\s*[^"'\s]`)
)

// useKind is how a component's code touches a prop.
type useKind int

const (
	// useField is `.Props.x`, `$.Props.x`, `.Attrs.x` where dot is the
	// file's root scope: the prop, renamed.
	useField useKind = iota
	// useIndex is the "x" of `index .Props "x"`: renamed.
	useIndex
	// useAttrsArg is the "x" of `{{ attrs "x" }}`, `{{ node "x" }}` or
	// `{{ hasNode "x" }}` (a `<brick:prop name="x">` block is read with
	// node): renamed.
	useAttrsArg
	// useScript is the "x" of `props["x"]` in the file's <brick:script>.
	useScript
	// useRebound is `.Props.x` inside a range/with/define body, where dot
	// is not the component: reported, not renamed.
	useRebound
	// useVar is `$v.Props.x` through a variable: reported, not renamed.
	useVar
	// useWhole is `.Props` handed on whole: blocks a rename.
	useWhole
	// useAttrsWhole is `.Attrs` handed on whole: blocks renaming an
	// undeclared prop (undeclared props travel in .Attrs).
	useAttrsWhole
	// useDynamic is `props[expr]` in a script: blocks a rename.
	useDynamic
	// useString is a string literal equal to the prop's name that none of
	// the forms above explains (`dict "x" …`, a script helper): reported.
	useString
)

// propUse is one place a component's own file touches a prop.
type propUse struct {
	kind useKind
	name string // "" for the whole kinds
	file *siteFile
	// start and end cover the name (inside the quotes for a string).
	start, end int
}

// renamed says the use is edited by a rename.
func (u propUse) renamed() bool {
	return u.kind == useField || u.kind == useIndex || u.kind == useAttrsArg || u.kind == useScript
}

// chainRoot is what a field chain starts from.
type chainRoot int

const (
	rootDot    chainRoot = iota // `.Props`
	rootDollar                  // `$.Props`
	rootVar                     // `$v.Props`
	rootExpr                    // `(…).Props`
)

type ctokKind int

const (
	ctStr ctokKind = iota
	ctIdent
	ctChain
	ctPunct
)

// ctok is one token of a template action's code.
type ctok struct {
	kind ctokKind
	// text is a string's value or an identifier.
	text string
	// start and end are absolute file offsets: a string's value inside
	// its quotes, otherwise the whole token.
	start, end int
	root       chainRoot
	fields     []cfield
}

// cfield is one `.Name` of a chain, its range covering Name.
type cfield struct {
	name       string
	start, end int
}

func isNameStartByte(c byte) bool { return c == '_' || (c|0x20 >= 'a' && c|0x20 <= 'z') }
func isIdentByte(c byte) bool     { return isNameStartByte(c) || (c >= '0' && c <= '9') }

// tokenizeCode splits template code into the tokens the prop scanner
// needs, skipping comments and character constants the way the engine's
// lexer does (internal/lex scanCode), so text inside a string is never a
// field. base is code's offset in the file.
func tokenizeCode(code string, base int) []ctok {
	var out []ctok
	n := len(code)
	fields := func(i int) ([]cfield, int) {
		var fs []cfield
		for i+1 < n && code[i] == '.' && isNameStartByte(code[i+1]) {
			j := i + 1
			for j < n && isIdentByte(code[j]) {
				j++
			}
			fs = append(fs, cfield{name: code[i+1 : j], start: base + i + 1, end: base + j})
			i = j
		}
		return fs, i
	}
	for i := 0; i < n; {
		c := code[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case c == '"':
			j := i + 1
			for j < n && code[j] != '"' {
				if code[j] == '\\' {
					j++
				}
				j++
			}
			if j >= n {
				return out
			}
			out = append(out, ctok{kind: ctStr, text: code[i+1 : j], start: base + i + 1, end: base + j})
			i = j + 1
		case c == '`':
			j := strings.IndexByte(code[i+1:], '`')
			if j < 0 {
				return out
			}
			out = append(out, ctok{kind: ctStr, text: code[i+1 : i+1+j], start: base + i + 1, end: base + i + 1 + j})
			i += j + 2
		case c == '\'':
			j := i + 1
			for j < n && code[j] != '\'' {
				if code[j] == '\\' {
					j++
				}
				j++
			}
			i = j + 1
		case c == '/' && i+1 < n && code[i+1] == '*':
			j := strings.Index(code[i+2:], "*/")
			if j < 0 {
				return out
			}
			i += j + 4
		case c == '$':
			j := i + 1
			for j < n && isIdentByte(code[j]) {
				j++
			}
			root := rootDollar
			if j > i+1 {
				root = rootVar
			}
			fs, k := fields(j)
			out = append(out, ctok{kind: ctChain, text: code[i+1 : j], start: base + i, end: base + k, root: root, fields: fs})
			i = k
		case c == '.':
			root := rootDot
			if i > 0 && (code[i-1] == ')' || isIdentByte(code[i-1])) {
				root = rootExpr
			}
			fs, k := fields(i)
			if len(fs) == 0 {
				out = append(out, ctok{kind: ctPunct, text: ".", start: base + i, end: base + i + 1})
				i++
				continue
			}
			out = append(out, ctok{kind: ctChain, start: base + i, end: base + k, root: root, fields: fs})
			i = k
		case isNameStartByte(c):
			j := i
			for j < n && (isIdentByte(code[j]) || (code[j] == '.' && j+1 < n && isNameStartByte(code[j+1]))) {
				j++
			}
			out = append(out, ctok{kind: ctIdent, text: code[i:j], start: base + i, end: base + j})
			i = j
		case c >= '0' && c <= '9':
			j := i
			for j < n && (isIdentByte(code[j]) || code[j] == '.') {
				j++
			}
			i = j
		default:
			out = append(out, ctok{kind: ctPunct, text: string(c), start: base + i, end: base + i + 1})
			i++
		}
	}
	return out
}

// codeUses finds the prop uses in one piece of template code evaluated
// where dot is (rebound) or is not (the file's root scope) rebound.
func codeUses(f *siteFile, code string, base int, rebound bool) []propUse {
	toks := tokenizeCode(code, base)
	var out []propUse
	add := func(k useKind, name string, start, end int) {
		out = append(out, propUse{kind: k, name: name, file: f, start: start, end: end})
	}
	explained := map[int]bool{}
	for k, t := range toks {
		switch t.kind {
		case ctChain:
			if len(t.fields) == 0 || (t.fields[0].name != fieldProps && t.fields[0].name != fieldAttrs) {
				continue
			}
			isAttrs := t.fields[0].name == fieldAttrs
			atRoot := t.root == rootDollar || (t.root == rootDot && !rebound)
			if len(t.fields) >= 2 {
				fd := t.fields[1]
				switch {
				case atRoot:
					add(useField, fd.name, fd.start, fd.end)
				case t.root == rootDot && !isAttrs:
					add(useRebound, fd.name, fd.start, fd.end)
				case t.root == rootVar && !isAttrs:
					add(useVar, fd.name, fd.start, fd.end)
				}
				continue
			}
			if !atRoot {
				continue
			}
			if k > 0 && toks[k-1].kind == ctIdent && toks[k-1].text == funcIndex && k+1 < len(toks) && toks[k+1].kind == ctStr {
				s := toks[k+1]
				add(useIndex, s.text, s.start, s.end)
				explained[k+1] = true
				continue
			}
			if isAttrs {
				add(useAttrsWhole, "", t.fields[0].start, t.fields[0].end)
			} else {
				add(useWhole, "", t.fields[0].start, t.fields[0].end)
			}
		case ctIdent:
			// attrs names any number of attributes; node and hasNode name
			// one block, their first argument.
			args, ok := nameArgs[t.text]
			if !ok {
				continue
			}
			for j := k + 1; j < len(toks) && toks[j].kind == ctStr && (args < 0 || j-k <= args); j++ {
				add(useAttrsArg, toks[j].text, toks[j].start, toks[j].end)
				explained[j] = true
			}
		}
	}
	for k, t := range toks {
		if t.kind == ctStr && !explained[k] && propNameRE.MatchString(t.text) {
			add(useString, t.text, t.start, t.end)
		}
	}
	return out
}

// dotFrame is one open range/with/if/define/block in a file's actions.
type dotFrame struct {
	act lex.ActKind
	// parent is whether dot was rebound outside the frame; rebound is
	// whether it is inside the frame's current branch.
	parent, rebound bool
}

// actionCode is an action token's code and its file offset, without the
// delimiters and trim markers.
func actionCode(f *siteFile, t lex.Token) (string, int) {
	raw := string(t.Raw)
	if len(raw) < 4 {
		return "", 0
	}
	code, base := raw[2:len(raw)-2], f.body+t.Pos.Offset+2
	if strings.HasPrefix(code, "-") {
		code, base = code[1:], base+1
	}
	code = strings.TrimSuffix(code, "-")
	return code, base
}

// componentUses lists every prop use in one of a component's own files.
// Dot is tracked through range/with/define/block frames, so
// `.Props.x` inside `{{ range }}` is told apart from the component's own.
// Bodies of calls inside the file are lexical (T3) and need no frame.
func componentUses(f *siteFile) []propUse {
	var out []propUse
	var stack []dotFrame
	rebound := func() bool { return len(stack) > 0 && stack[len(stack)-1].rebound }
	var lastOpen string
	for _, t := range f.toks {
		switch t.Kind {
		case lex.Action:
			code, base := actionCode(f, t)
			switch t.Act {
			case lex.ActComment:
			case lex.ActRange, lex.ActWith:
				out = append(out, codeUses(f, code, base, rebound())...)
				stack = append(stack, dotFrame{act: t.Act, parent: rebound(), rebound: true})
			case lex.ActIf:
				out = append(out, codeUses(f, code, base, rebound())...)
				stack = append(stack, dotFrame{act: t.Act, parent: rebound(), rebound: rebound()})
			case lex.ActDefine, lex.ActBlock:
				out = append(out, codeUses(f, code, base, rebound())...)
				stack = append(stack, dotFrame{act: t.Act, parent: rebound(), rebound: true})
			case lex.ActElse:
				if len(stack) == 0 {
					continue
				}
				top := &stack[len(stack)-1]
				out = append(out, codeUses(f, code, base, top.parent)...)
				top.rebound = top.parent || strings.HasPrefix(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(code), "else")), "with")
			case lex.ActEnd:
				if len(stack) > 0 {
					stack = stack[:len(stack)-1]
				}
			default:
				out = append(out, codeUses(f, code, base, rebound())...)
			}
		case lex.Open:
			lastOpen = t.Name
			for _, a := range t.Attrs {
				start, _, ok := f.valueRange(t, a)
				if !ok {
					if strings.Contains(a.Value, fieldProps) || strings.Contains(a.Value, fieldAttrs) {
						// An unquoted expression we cannot place: treat it
						// as a whole use so a rename refuses, not guesses.
						s, e := f.attrRange(a)
						out = append(out, propUse{kind: useWhole, file: f, start: s, end: e})
					}
					continue
				}
				switch {
				case strings.HasPrefix(a.Name, exprPrefix) || a.Name == attrSpread:
					out = append(out, codeUses(f, a.Value, start, rebound())...)
				case strings.Contains(a.Value, "{{"):
					toks, err := lex.Scan([]byte(a.Value), lex.Options{})
					if err != nil {
						continue
					}
					for _, vt := range toks {
						if vt.Kind != lex.Action || vt.Act == lex.ActComment {
							continue
						}
						raw := string(vt.Raw)
						code, b := raw[2:len(raw)-2], start+vt.Pos.Offset+2
						if strings.HasPrefix(code, "-") {
							code, b = code[1:], b+1
						}
						out = append(out, codeUses(f, strings.TrimSuffix(code, "-"), b, rebound())...)
					}
				}
			}
		case lex.Close:
			lastOpen = ""
		case lex.Text:
			if t.RawBody && lastOpen == tagScript {
				out = append(out, scriptUses(f, string(t.Raw), f.body+t.Pos.Offset)...)
			}
		}
	}
	return out
}

// scriptUses finds props read by name in a <brick:script> body, plus any
// other string literal that looks like a name, which a rename reports
// because the script may pass it on (a helper reading props[k]).
func scriptUses(f *siteFile, body string, base int) []propUse {
	var out []propUse
	covered := map[int]bool{}
	for _, m := range scriptPropKeyRE.FindAllStringSubmatchIndex(body, -1) {
		out = append(out, propUse{kind: useScript, name: body[m[2]:m[3]], file: f, start: base + m[2], end: base + m[3]})
		covered[m[2]] = true
	}
	for _, m := range scriptPropDynRE.FindAllStringIndex(body, -1) {
		out = append(out, propUse{kind: useDynamic, file: f, start: base + m[0], end: base + m[1]})
	}
	for i := 0; i < len(body); i++ {
		q := body[i]
		if q != '"' && q != '\'' {
			continue
		}
		j := strings.IndexByte(body[i+1:], q)
		if j < 0 {
			break
		}
		if s := body[i+1 : i+1+j]; !covered[i+1] && propNameRE.MatchString(s) {
			out = append(out, propUse{kind: useString, name: s, file: f, start: base + i + 1, end: base + i + 1 + j})
		}
		i += j + 1
	}
	return out
}

// ownFiles are the component's file and its locale variants
// (`Card.fr.html`), the files whose `.Props` is this component's.
func ownFiles(files []*siteFile, root string, comp brick.ComponentDoc) []*siteFile {
	main := filepath.Join(root, filepath.FromSlash(comp.Path))
	dir, stem := filepath.Dir(main), strings.TrimSuffix(path.Base(comp.Path), htmlExt)
	var out []*siteFile
	for _, f := range files {
		base := filepath.Base(f.path)
		if f.path == main || (filepath.Dir(f.path) == dir && strings.HasPrefix(base, stem+".")) {
			out = append(out, f)
		}
	}
	return out
}

// declSite is the declaration of name in one of the component's files:
// under `props:` (block or flow form) or under `brick: {slots:}`.
func declSites(f *siteFile, name string) []tagRef {
	var out []tagRef
	if start, end, ok := propDeclaration(f, name); ok {
		out = append(out, tagRef{file: f, start: start, end: end})
	} else if s, e, ok := fmSection(f, strings.TrimSuffix(propsKey, ":")); ok {
		if at, ok := keyIn(f.text, s+len(propsKey), e, name); ok {
			out = append(out, tagRef{file: f, start: at, end: at + len(name)})
		}
	}
	if s, e, ok := fmSection(f, brickKey); ok {
		if sl, ok := keyIn(f.text, s, e, slotsKey); ok {
			if at, ok := keyIn(f.text, sl+len(slotsKey)+1, e, name); ok {
				out = append(out, tagRef{file: f, start: at, end: at + len(name)})
			}
		}
	}
	return out
}

// fmSection is the byte range of a top-level frontmatter key's section:
// its line to the next top-level line.
func fmSection(f *siteFile, key string) (int, int, bool) {
	start, off := -1, f.fmStart
	for line := range strings.SplitSeq(f.text[f.fmStart:f.fmEnd], "\n") {
		lineStart := off
		off += len(line) + 1
		t := strings.TrimRight(line, "\r")
		if t == "" || t[0] == ' ' || t[0] == '\t' || t[0] == '#' {
			continue
		}
		if start >= 0 {
			return start, lineStart, true
		}
		if strings.HasPrefix(t, key+":") {
			start = lineStart
		}
	}
	if start >= 0 {
		return start, min(off, f.fmEnd), true
	}
	return 0, 0, false
}

// keyIn finds `name:` as a whole word in text[from:to], outside quotes.
func keyIn(text string, from, to int, name string) (int, bool) {
	var quote byte
	for i := from; i < to && i < len(text); i++ {
		c := text[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case strings.HasPrefix(text[i:], name) && (i == 0 || !isIdentByte(text[i-1])):
			j := i + len(name)
			if j < len(text) && isIdentByte(text[j]) {
				continue
			}
			for j < to && (text[j] == ' ' || text[j] == '\t') {
				j++
			}
			if j < to && text[j] == ':' {
				return i, true
			}
		}
	}
	return 0, false
}

// propPlan is everything a prop rename (or its references) touches, and
// what stops or qualifies it.
type propPlan struct {
	// decls, calls and uses are the renamed sites.
	decls, calls []tagRef
	uses         []propUse
	// blocked sites refuse the rename; clashes are where the new name is
	// already taken; skipped sites are reported after a rename.
	blocked, clashes, skipped []string
}

// where is a site as `path:line:col` relative to the site root.
func where(root string, f *siteFile, off int) string {
	rel, err := filepath.Rel(root, f.path)
	if err != nil {
		rel = f.path
	}
	l, c := PositionOf(f.text, off)
	return fmt.Sprintf("%s:%d:%d", filepath.ToSlash(rel), l+1, c+1)
}

// planProp gathers every site of comp's prop old across the site. newName
// is empty for references.
func planProp(root string, files []*siteFile, list []brick.ComponentDoc, comp brick.ComponentDoc, old, newName string) *propPlan {
	pl := &propPlan{}
	declared := slices.ContainsFunc(comp.Props, func(p brick.PropDoc) bool { return p.Name == old }) ||
		slices.ContainsFunc(comp.Slots, func(s brick.SlotDoc) bool { return s.Name == old })
	if newName != "" && (slices.ContainsFunc(comp.Props, func(p brick.PropDoc) bool { return p.Name == newName }) ||
		slices.ContainsFunc(comp.Slots, func(s brick.SlotDoc) bool { return s.Name == newName })) {
		pl.clashes = append(pl.clashes, fmt.Sprintf("<%s> already declares %q (%s)", comp.Name, newName, comp.Path))
	}

	for _, f := range ownFiles(files, root, comp) {
		pl.decls = append(pl.decls, declSites(f, old)...)
		if doc, err := frontmatter.Parse([]byte(f.text)); err == nil {
			if _, ok := doc.Params[loadKey]; ok {
				pl.blocked = append(pl.blocked, where(root, f, f.fmStart)+" has load:, whose script returns props by name")
			}
		}
		for _, u := range componentUses(f) {
			at := where(root, f, u.start)
			switch {
			case u.kind == useWhole:
				pl.blocked = append(pl.blocked, at+" hands .Props on whole")
			case u.kind == useAttrsWhole && !declared:
				pl.blocked = append(pl.blocked, at+" hands .Attrs on whole (an undeclared prop travels in it)")
			case u.kind == useDynamic:
				pl.blocked = append(pl.blocked, at+" reads props with a computed key")
			case u.name == old && u.renamed():
				pl.uses = append(pl.uses, u)
			case u.name == old && u.kind == useRebound:
				pl.skipped = append(pl.skipped, at+" .Props."+old+" where dot is not the component")
			case u.name == old && u.kind == useVar:
				pl.skipped = append(pl.skipped, at+" "+fieldProps+"."+old+" through a variable")
			case u.name == old && u.kind == useString:
				pl.skipped = append(pl.skipped, at+fmt.Sprintf(" the string %q", old))
			case newName != "" && u.name == newName && u.renamed():
				pl.clashes = append(pl.clashes, at+" already reads "+newName)
			}
		}
	}

	for _, f := range files {
		var stack []lex.Token
		for _, t := range f.toks {
			switch t.Kind {
			case lex.Open:
				if t.Name == tagRender {
					pl.skipped = append(pl.skipped, where(root, f, f.body+t.Pos.Offset)+" <brick:render> picks its component at run time")
				}
				if t.Name == tagProp && len(stack) > 0 {
					if p, _, ok := f.resolve(list, stack[len(stack)-1].Name); ok && p == comp.Path {
						for _, a := range t.Attrs {
							if a.Name != attrName {
								continue
							}
							if start, end, ok := f.valueRange(t, a); ok && a.Value == old {
								pl.calls = append(pl.calls, tagRef{file: f, start: start, end: end})
							}
							if newName != "" && a.Value == newName {
								pl.clashes = append(pl.clashes, where(root, f, f.body+t.Pos.Offset)+" already passes "+newName)
							}
						}
					}
				}
				if p, _, ok := f.resolve(list, t.Name); ok && p == comp.Path {
					for _, a := range t.Attrs {
						name := strings.TrimPrefix(a.Name, exprPrefix)
						start, end := f.attrRange(a)
						switch {
						case a.Name == attrSpread:
							pl.blocked = append(pl.blocked, where(root, f, start)+" passes props through brick:spread")
						case name == old:
							pl.calls = append(pl.calls, tagRef{file: f, start: end - len(name), end: end})
						case newName != "" && name == newName:
							pl.clashes = append(pl.clashes, where(root, f, start)+" already passes "+newName)
						}
					}
				}
				if !t.SelfClosing {
					stack = append(stack, t)
				}
			case lex.Close:
				for i := len(stack) - 1; i >= 0; i-- {
					if stack[i].Name == t.Name {
						stack = stack[:i]
						break
					}
				}
			}
		}
	}
	return pl
}

// sites are the renamed ranges, declaration first.
func (pl *propPlan) sites() []tagRef {
	out := slices.Clone(pl.decls)
	out = append(out, pl.calls...)
	for _, u := range pl.uses {
		out = append(out, tagRef{file: u.file, start: u.start, end: u.end})
	}
	return out
}

// listSites joins up to reportMax sites and says how many more there are.
func listSites(s []string) string {
	if len(s) <= reportMax {
		return strings.Join(s, "; ")
	}
	return strings.Join(s[:reportMax], "; ") + fmt.Sprintf("; and %d more", len(s)-reportMax)
}

// propTargetAt finds a prop under the cursor in a component's own file:
// a `.Props.x` (or index/attrs/script) use or a declaration key.
func propTargetAt(f *siteFile, root string, list []brick.ComponentDoc, offset int) (target, bool) {
	for _, comp := range list {
		own := ownFiles([]*siteFile{f}, root, comp)
		if len(own) == 0 {
			continue
		}
		for _, u := range componentUses(f) {
			if u.renamed() && u.name != "" && offset >= u.start && offset <= u.end {
				return target{comp: comp, prop: u.name, start: u.start, end: u.end, word: u.name}, true
			}
		}
		if offset < f.fmStart || offset > f.fmEnd {
			return target{}, false
		}
		var names []string
		for _, p := range comp.Props {
			names = append(names, p.Name)
		}
		for _, s := range comp.Slots {
			names = append(names, s.Name)
		}
		for _, n := range names {
			for _, d := range declSites(f, n) {
				if offset >= d.start && offset <= d.end {
					return target{comp: comp, prop: n, start: d.start, end: d.end, word: n}, true
				}
			}
		}
		return target{}, false
	}
	return target{}, false
}

// blockPropTargetAt finds the prop named by a `<brick:prop name="x">`
// value directly inside a call.
func blockPropTargetAt(f *siteFile, list []brick.ComponentDoc, offset int) (target, bool) {
	var stack []lex.Token
	for _, t := range f.toks {
		switch t.Kind {
		case lex.Open:
			if t.Name == tagProp && len(stack) > 0 {
				if p, _, ok := f.resolve(list, stack[len(stack)-1].Name); ok {
					if comp, ok := compByPath(list, p); ok {
						for _, a := range t.Attrs {
							if start, end, ok := f.valueRange(t, a); ok && a.Name == attrName && offset >= start && offset <= end {
								return target{comp: comp, prop: a.Value, start: start, end: end, word: a.Value}, true
							}
						}
					}
				}
			}
			if !t.SelfClosing {
				stack = append(stack, t)
			}
		case lex.Close:
			for i := len(stack) - 1; i >= 0; i-- {
				if stack[i].Name == t.Name {
					stack = stack[:i]
					break
				}
			}
		}
	}
	return target{}, false
}

// propRenamable checks that a prop's component has a file in this site.
func propRenamable(c cursor, tg target) *rpcError {
	p := filepath.Join(c.root, filepath.FromSlash(tg.comp.Path))
	if _, err := os.Stat(p); err != nil {
		return renameRefusal("<%s> is built into brick (the kit); its props cannot be renamed from this site", tg.comp.Name)
	}
	if slices.Contains(strings.Split(tg.comp.Path, "/"), "brick_modules") {
		return renameRefusal("<%s> comes from a library in brick_modules; rename its props in the library", tg.comp.Name)
	}
	return nil
}

// renameProp builds the WorkspaceEdit renaming one prop everywhere, or
// refuses with every site that stops it.
func (s *Server) renameProp(ctx context.Context, c cursor, tg target, newName string) (any, *rpcError) {
	if rerr := propRenamable(c, tg); rerr != nil {
		return nil, rerr
	}
	newName = strings.TrimPrefix(newName, exprPrefix)
	if !propNameRE.MatchString(newName) || newName == childrenProp {
		return nil, renameRefusal("%q is not a prop name: use letters, digits and _, starting with a letter or _ (and not %q)", newName, childrenProp)
	}
	if newName == tg.prop {
		return s.workspaceEdit(map[string][]map[string]any{}), nil
	}
	list := s.components(ctx, c.root)
	pl := planProp(c.root, s.siteFiles(c.root), list, tg.comp, tg.prop, newName)
	if len(pl.clashes) > 0 {
		return nil, renameRefusal("cannot rename %s to %s on <%s>: %s", tg.prop, newName, tg.comp.Name, listSites(pl.clashes))
	}
	if len(pl.blocked) > 0 {
		return nil, renameRefusal("cannot rename %s on <%s>: %d site(s) brick cannot follow: %s", tg.prop, tg.comp.Name, len(pl.blocked), listSites(pl.blocked))
	}
	edits := map[string][]map[string]any{}
	seen := map[string]bool{}
	for _, r := range pl.sites() {
		key := fmt.Sprintf("%s#%d", r.file.uri, r.start)
		if seen[key] {
			continue
		}
		seen[key] = true
		edits[r.file.uri] = append(edits[r.file.uri], map[string]any{"range": rangeOf(r.file.text, r.start, r.end), "newText": newName})
	}
	if len(pl.skipped) > 0 {
		_ = s.conn.Notify(methodShowMsg, map[string]any{"type": messageWarn, "message": fmt.Sprintf(
			"brick: renamed %s to %s at %d site(s); skipped %d brick cannot tell are this prop: %s",
			tg.prop, newName, len(seen), len(pl.skipped), listSites(pl.skipped))})
	}
	return s.workspaceEdit(edits), nil
}

// workspaceEdit shapes text edits for the client: documentChanges when it
// takes them, else the plain changes map every client accepts.
func (s *Server) workspaceEdit(edits map[string][]map[string]any) map[string]any {
	if !s.docChanges {
		changes := map[string]any{}
		for uri, e := range edits {
			changes[uri] = e
		}
		return map[string]any{"changes": changes}
	}
	out := []any{}
	for _, uri := range sortedKeys(edits) {
		out = append(out, map[string]any{"textDocument": map[string]any{"uri": uri, "version": nil}, "edits": edits[uri]})
	}
	return map[string]any{"documentChanges": out}
}
