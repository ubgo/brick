package brick

import (
	"bytes"
	"fmt"
	"html"
	"regexp"
	"slices"
	"strings"

	"github.com/ubgo/brick/internal/lex"
)

// The engine-owned attributes that are not actions:
// morph controls, navigation and small runtime hooks. They are allowed in
// any file (the navigation morph patches every page), except
// brick:transition, which only a stateful root can use. Unlike the action
// grammar they carry no call, so they need no browser-side table; the
// compiler still checks their modifiers and values, so a typo is an error
// with a file and line instead of an attribute the runtime ignores.

// plainBrickAttrs are the brick: attributes a plain element may carry; they
// pass through to the page for the runtime. nav, prefetch, persist and keep
// predate plainRules and are not checked further; method has its own check
// (checkMethodLinks), because it depends on the tag it sits on.
var plainBrickAttrs = []string{"nav", "prefetch", "persist", "keep", "key", "ignore", "replace", "ref", "offline", "current", "leave", "enter", "scroll", attrMethod, jsAttr}

// statefulBrickAttrs are non-action attributes only a stateful file may
// carry, because only a stateful root's morph reads them.
var statefulBrickAttrs = []string{"transition"}

// plainValue is what a checked plain attribute's value may hold.
type plainValue int

const (
	pvNone     plainValue = iota // no value: the attribute is a flag
	pvRequired                   // any non-empty text
	pvOptional                   // any text, or none
	pvName                       // a name: letters, digits, - and _
)

// plainRule is one checked non-action attribute: its modifiers and value.
type plainRule struct {
	mods  []string
	value plainValue
}

// plainRules are the non-action attributes the compiler checks.
var plainRules = map[string]plainRule{
	// brick:key is a sibling-scoped morph key; id still wins (D1).
	"key": {value: pvRequired},
	// brick:ignore keeps the element as it is across morphs; .self keeps
	// only its attributes and still patches its children.
	"ignore": {mods: []string{"self"}, value: pvNone},
	// brick:replace swaps the element for a fresh copy instead of morphing.
	"replace": {value: pvNone},
	// brick:ref names an element for brick.ref and streaming.
	"ref": {value: pvName},
	// brick:offline shows the element while the browser is offline;
	// .remove hides it instead.
	"offline": {mods: []string{"remove"}, value: pvNone},
	// brick:current marks the link to the page being shown; .exact needs
	// the paths to be equal instead of a segment prefix (D7).
	"current": {mods: []string{"exact"}, value: pvNone},
	// brick:leave holds the classes that animate an element out before it
	// is removed (by a morph, a rows delete or a JS hide); the runtime
	// waits for the transition or animation, capped.
	"leave": {value: pvRequired},
	// brick:enter holds the classes an element starts from when brick:show
	// or a JS show reveals it: they come off on the next frame, so its
	// transition plays in.
	"enter": {value: pvRequired},
	// brick:scroll="<name>" keeps a scroll container's position across
	// navigations: the next page's container of the same name gets it, and
	// back/forward restore the one that entry had (G20). It is keyed by a
	// name, not by id, so the compiler can require it here, and so two
	// layouts can share one position (a docs and an app sidebar both "nav").
	"scroll": {value: pvName},
	// brick:transition opts a stateful root's action morphs into a view
	// transition; the value is optional transition types (D3).
	"transition": {value: pvOptional},
}

var (
	// refNameRE is a brick:ref name.
	refNameRE = regexp.MustCompile(`^[A-Za-z_][\w-]*$`)
	// keyedTagRE finds an attribute that keys a morph row: id or brick:key.
	keyedTagRE = regexp.MustCompile(`\s(?:id|brick:key)\s*=`)
	// leadingTagRE is a plain start tag at the start of a range body.
	leadingTagRE = regexp.MustCompile(`^<[a-zA-Z][^<>]*>`)
	// stateRangeRE is a range over a state list: the rows an action inserts
	// into and reorders. A range over a helper's output or a literal list
	// (weekdays, swatches) is left alone; its rows do not move.
	stateRangeRE = regexp.MustCompile(`^\{\{-?\s*range\s+(?:\$\w+\s*(?:,\s*\$\w+\s*)?:=\s*)?\$?\.State\.`)
)

// checkPlainAttr checks one non-action brick: attribute against plainRules.
// A value with {{ }} is filled at render time and only checked for
// presence. refs collects literal brick:ref and brick:scroll names, so a
// duplicate in one file is an error: the runtime would act on the first only.
func checkPlainAttr(a brickAttr, refs map[string]bool) error {
	if a.name == jsAttr {
		// brick:js has its own modifier table and command grammar.
		return checkJSAttr(a)
	}
	r, ok := plainRules[a.name]
	if !ok {
		return nil
	}
	for i, m := range a.mods {
		if !slices.Contains(r.mods, m) || slices.Contains(a.mods[:i], m) {
			allowed := "no modifiers"
			if len(r.mods) > 0 {
				allowed = "." + strings.Join(r.mods, ", .")
			}
			return fmt.Errorf("%s: unknown modifier %q (brick:%s accepts %s)", a.fullName(), m, a.name, allowed)
		}
	}
	v := strings.TrimSpace(html.UnescapeString(a.value))
	switch r.value {
	case pvNone:
		if a.hasValue && v != "" {
			return fmt.Errorf("%s=%q: brick:%s takes no value", a.fullName(), v, a.name)
		}
	case pvRequired:
		if v == "" {
			return fmt.Errorf("%s needs a value", a.fullName())
		}
	case pvName:
		if strings.Contains(v, "{{") {
			return nil
		}
		if !refNameRE.MatchString(v) {
			return fmt.Errorf("%s=%q: a %s is a name (letters, digits, - and _)", a.fullName(), v, a.name)
		}
		// Names are unique per attribute in one file: brick.ref and the
		// scroll carry-over both act on the first element with a name only.
		key := a.name + "\x00" + v
		if refs[key] {
			return fmt.Errorf("%s=%q: the name is used twice in this file; brick:%s acts on the first only", a.fullName(), v, a.name)
		}
		refs[key] = true
	}
	return nil
}

// checkIgnoredRoot refuses brick:ignore on a stateful file's root element:
// the morph would keep the old root, so the component's own actions could
// never show their result. The root is the first plain start
// tag of the body (markRoot requires exactly one).
func checkIgnoredRoot(toks []lex.Token) (lex.Pos, error) {
	for _, t := range toks {
		if t.Kind == lex.Open {
			return lex.Pos{}, nil
		}
		if t.Kind != lex.Text || t.RawBody {
			continue
		}
		loc := plainTagRE.FindIndex(t.Raw)
		if loc == nil {
			continue
		}
		for _, mm := range brickAttrRE.FindAllSubmatch(t.Raw[loc[0]:loc[1]], -1) {
			if a := parseBrickAttrName(string(mm[1])); a.name == "ignore" && len(a.mods) == 0 {
				return advancePos(t.Pos, t.Raw[:loc[0]]), fmt.Errorf("brick:ignore on a stateful component's root would keep the old root after every action, so the component could never show its own result; put it on an element inside, or use brick:ignore.self")
			}
		}
		return lex.Pos{}, nil
	}
	return lex.Pos{}, nil
}

// unkeyedRanges finds each {{ range .State.… }} of a stateful file whose
// body starts with a plain element that has neither id nor brick:key
// :
// inserting a row then re-renders every row after it, and a focused input
// moves to the wrong row. A component call is left to the render-time
// key= warning. It returns the position of each such range.
func unkeyedRanges(toks []lex.Token) []lex.Pos {
	var out []lex.Pos
	for i, t := range toks {
		if t.Kind != lex.Action || t.Act != lex.ActRange || !stateRangeRE.Match(t.Raw) {
			continue
		}
		// The row's start tag may hold actions (brick:key="{{ .id }}"), so
		// join the following text and blanked actions up to the first
		// component tag.
		var b bytes.Buffer
		for _, n := range toks[i+1:] {
			if n.Kind == lex.Open || n.Kind == lex.Close || n.RawBody {
				break
			}
			if n.Kind == lex.Action {
				if n.Act == lex.ActEnd {
					break
				}
				b.WriteString(strings.Repeat("_", len(n.Raw)))
				continue
			}
			b.Write(n.Raw)
			if bytes.Contains(n.Raw, []byte(">")) {
				break
			}
		}
		body := bytes.TrimLeft(b.Bytes(), " \t\r\n")
		tag := leadingTagRE.Find(body)
		if tag == nil || keyedTagRE.Match(tag) {
			continue
		}
		out = append(out, t.Pos)
	}
	return out
}
