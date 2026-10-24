package brick

import (
	"encoding/json"
	"fmt"
	"html"
	"regexp"
	"slices"
	"strings"
)

// brick:js.<event>="cmd(args); cmd(args)" runs a fixed list of DOM
// commands in the browser when the event fires: show, hide or toggle an element, change its classes or
// attributes, focus it, or dispatch a DOM event. These commands only change
// the page's DOM, the way a CSS class toggle would; they never read or
// write component state and never reach the server, so they are not the
// declarative action DSL brick deliberately does not have: state still changes only in
// Starlark actions. For the same reason the list is closed: no variables,
// expressions or conditionals, and every argument is a JSON literal.

// jsAttr is the attribute's name after "brick:".
const jsAttr = "js"

// jsEvents are the DOM events brick:js listens for; an attribute names
// exactly one, as its first modifier or any other.
var jsEvents = []string{"click", "dblclick", "input", "change", "submit", "focus", "blur", "keydown", "keyup", "mouseenter", "mouseleave"}

// jsKeyEvents are the events that take a key combo.
var jsKeyEvents = []string{"keydown", "keyup"}

// jsWindowEvents are the events that take .window: a key pressed anywhere,
// and the browser window (the tab) gaining or losing focus.
var jsWindowEvents = []string{"keydown", "keyup", "focus", "blur"}

// jsRule is brick:js's modifier table: one event, .once, .outside (click
// only), .window for key, focus and blur events, and for key events a key
// combo, as brick:keydown has.
var jsRule = attrRule{
	mods:  append(append(slices.Clone(jsEvents), "once", "outside", "window"), append(slices.Clone(keyModifiers), modKey)...),
	value: vText,
}

// jsCommands is every command with its argument count, the target
// included: [min, max]. The runtime reads the same table from GrammarJS.
//
//	show(t) hide(t) toggle(t)         the hidden attribute, as brick:show uses
//	addClass(t, "a b") removeClass(t, "a b") toggleClass(t, "a b")
//	setAttr(t, name, value) removeAttr(t, name)
//	toggleAttr(t, name[, on[, off]])  presence, or on ⇄ off (off absent: removed)
//	focus(t)
//	dispatch(t, name[, detail])       a bubbling CustomEvent on the target
var jsCommands = map[string][2]int{
	"show": {1, 1}, "hide": {1, 1}, "toggle": {1, 1}, "focus": {1, 1},
	"addClass": {2, 2}, "removeClass": {2, 2}, "toggleClass": {2, 2},
	"setAttr": {3, 3}, "removeAttr": {2, 2}, "toggleAttr": {2, 4},
	"dispatch": {2, 3},
}

var (
	// jsCallRE is one command: a name and its parenthesised arguments.
	jsCallRE = regexp.MustCompile(`(?s)^\s*([A-Za-z]+)\s*\((.*)\)\s*$`)
	// jsAttrNameRE is an attribute name a command may set.
	jsAttrNameRE = regexp.MustCompile(`^[A-Za-z_][\w.:-]*$`)
	// jsBadURLRE is a URL scheme that runs or embeds code.
	jsBadURLRE = regexp.MustCompile(`(?i)^\s*(javascript|vbscript|data):`)
)

// jsURLAttrs are the attributes that hold a URL, where a command may not
// write a script URL (the runtime keeps the same list).
var jsURLAttrs = map[string]bool{"href": true, "src": true, "action": true, "formaction": true, "xlink:href": true, "poster": true}

// jsTargetSelf and jsTargetClosest are the target forms that are not a CSS
// selector; "@name" is a brick:ref.
const (
	jsTargetSelf    = "this"
	jsTargetClosest = "closest "
)

// splitJSCommands splits a command list on the semicolons outside JSON
// strings, so a selector or class in quotes may hold one.
func splitJSCommands(v string) []string {
	var out []string
	start, inStr := 0, false
	for i := 0; i < len(v); i++ {
		switch c := v[i]; {
		case inStr && c == '\\':
			i++
		case c == '"':
			inStr = !inStr
		case !inStr && c == ';':
			out = append(out, v[start:i])
			start = i + 1
		}
	}
	out = append(out, v[start:])
	kept := out[:0]
	for _, s := range out {
		if strings.TrimSpace(s) != "" {
			kept = append(kept, s)
		}
	}
	return kept
}

// jsAttrRefused reports why a command may not set the attribute name, or
// "". Event handlers and srcdoc would run code the page did not ship, and
// the engine's own attributes are the morph's and the runtime's to write.
// The runtime refuses the same names, because {{ }} can fill one at
// render time.
func jsAttrRefused(name string) string {
	n := strings.ToLower(name)
	switch {
	case !jsAttrNameRE.MatchString(name):
		return "is not an attribute name"
	case strings.HasPrefix(n, "on"):
		return "is an event handler, which would run code the page did not ship"
	case n == "srcdoc":
		return "would load a document the page did not ship"
	case strings.HasPrefix(n, "brick:") || strings.HasPrefix(n, "data-brick"):
		return "belongs to the engine"
	}
	return ""
}

// checkJSAttr checks a brick:js attribute: one event and the modifiers
// that event allows, then (for a literal value) every command, its
// argument count and types, its target and the attribute it writes.
func checkJSAttr(a brickAttr) error {
	used, err := checkMods(a, jsRule)
	if err != nil {
		return err
	}
	event := ""
	for _, e := range jsEvents {
		if used[e] {
			if event != "" {
				return fmt.Errorf("%s: one event per attribute (.%s and .%s); write a second brick:js attribute", a.fullName(), event, e)
			}
			event = e
		}
	}
	if event == "" {
		return fmt.Errorf("%s: name the event, such as brick:js.click (events: %s)", a.fullName(), strings.Join(jsEvents, ", "))
	}
	if used["outside"] && event != "click" {
		return fmt.Errorf("%s: .outside goes with .click only", a.fullName())
	}
	if used["window"] && !slices.Contains(jsWindowEvents, event) {
		return fmt.Errorf("%s: .window goes with .keydown, .keyup, .focus and .blur only", a.fullName())
	}
	if used[modKey] && !slices.Contains(jsKeyEvents, event) {
		return fmt.Errorf("%s: keys go with .keydown and .keyup only", a.fullName())
	}
	v := strings.TrimSpace(html.UnescapeString(a.value))
	if strings.Contains(v, "{{") {
		return nil
	}
	cmds := splitJSCommands(v)
	if len(cmds) == 0 {
		return fmt.Errorf("%s needs commands, such as toggle(\"#menu\")", a.fullName())
	}
	for _, c := range cmds {
		if err := checkJSCommand(c); err != nil {
			return fmt.Errorf("%s=%q: %w", a.fullName(), v, err)
		}
	}
	return nil
}

// checkJSCommand checks one command of a brick:js list.
func checkJSCommand(c string) error {
	m := jsCallRE.FindStringSubmatch(c)
	if m == nil {
		return fmt.Errorf("%q is not a command: write name(target, …)", strings.TrimSpace(c))
	}
	name := m[1]
	n, ok := jsCommands[name]
	if !ok {
		return fmt.Errorf("unknown command %q (commands: %s)", name, strings.Join(sortedKeys(jsCommands), ", "))
	}
	var args []any
	if s := strings.TrimSpace(m[2]); s != "" {
		if err := json.Unmarshal([]byte("["+s+"]"), &args); err != nil {
			return fmt.Errorf("%s: arguments must be JSON literals (strings in double quotes, so the attribute takes single quotes): %v", name, err)
		}
	}
	if len(args) < n[0] || len(args) > n[1] {
		want := fmt.Sprint(n[0])
		if n[1] != n[0] {
			want = fmt.Sprintf("%d to %d", n[0], n[1])
		}
		return fmt.Errorf("%s takes %s arguments, the target first; got %d", name, want, len(args))
	}
	str := func(i int, what string) (string, error) {
		s, ok := args[i].(string)
		if !ok || strings.TrimSpace(s) == "" {
			return "", fmt.Errorf("%s: argument %d (%s) must be a non-empty string", name, i+1, what)
		}
		return s, nil
	}
	t, err := str(0, "the target")
	if err != nil {
		return err
	}
	if err := checkJSTarget(t); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	switch name {
	case "addClass", "removeClass", "toggleClass":
		_, err = str(1, "the classes")
	case "setAttr", "removeAttr", "toggleAttr":
		var attr string
		if attr, err = str(1, "the attribute"); err == nil {
			if why := jsAttrRefused(attr); why != "" {
				err = fmt.Errorf("%s: %q %s", name, attr, why)
			}
		}
		for i := 2; err == nil && i < len(args); i++ {
			switch x := args[i].(type) {
			case string:
				if jsURLAttrs[strings.ToLower(attr)] && jsBadURLRE.MatchString(x) {
					err = fmt.Errorf("%s: %q would point %s at a script", name, x, attr)
				}
			case float64, bool:
			default:
				err = fmt.Errorf("%s: argument %d must be a string, number or boolean", name, i+1)
			}
		}
	case "dispatch":
		var ev string
		if ev, err = str(1, "the event name"); err == nil && slices.Contains(jsEvents, ev) {
			err = fmt.Errorf("dispatch: %q would re-trigger brick's own listeners; name a custom event", ev)
		}
	}
	return err
}

// checkJSTarget checks a command's target: "this", "@ref", "closest
// <selector>", or a CSS selector (which only the browser can parse).
func checkJSTarget(t string) error {
	switch {
	case strings.HasPrefix(t, "@"):
		if !refNameRE.MatchString(t[1:]) {
			return fmt.Errorf("target %q: a ref is @ and a name (letters, digits, - and _)", t)
		}
	case strings.HasPrefix(t, jsTargetClosest):
		if strings.TrimSpace(strings.TrimPrefix(t, jsTargetClosest)) == "" {
			return fmt.Errorf("target %q: closest needs a selector, such as \"closest .card\"", t)
		}
	}
	return nil
}
