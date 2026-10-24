package bricktest

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ubgo/brick"
)

// The event drivers (Click, ClickOutside, Change, Input, Focus, Blur,
// Keydown, Keyup, Intersect, IntersectFor, Leave) resolve a
// brick: attribute the way runtime.js does: its modifiers are parsed from
// the same table (brick.GrammarJS, which the browser carries as one
// generated line), its value is the runtime's action-call grammar, and it
// runs on the stateful root around the element. What has no meaning
// without a clock or a viewport collapses: each driver call is one event
// far enough from the last that .debounce and .throttle let it through,
// and .half, .full and .margin need no geometry because the test says the
// element is visible. Time is simulated only where it decides the
// outcome: Intersect is a stay long enough for any .dwell, IntersectFor
// a stay of a given length that then ends.

// runtimeGrammar is the parsed GRAMMAR line.
type runtimeGrammar struct {
	Attrs map[string][]string `json:"attrs"`
	Keys  []string            `json:"keys"`
}

var grammarTable = sync.OnceValue(func() runtimeGrammar {
	var g runtimeGrammar
	line := strings.TrimSuffix(strings.TrimPrefix(brick.GrammarJS(), "const GRAMMAR = "), ";")
	if err := json.Unmarshal([]byte(line), &g); err != nil {
		panic("bricktest: brick.GrammarJS is not the expected line: " + err.Error())
	}
	return g
})

// keyEvent is the runtime's KEY_EVENT: each named brick:keydown.<key>
// and the KeyboardEvent key it matches; a letter or digit matches itself.
// TestKeyEventMatchesRuntime compares it with runtime.js.
var keyEvent = map[string]string{
	"enter": "Enter", "escape": "Escape", "tab": "Tab", "space": " ",
	"up": "ArrowUp", "down": "ArrowDown", "left": "ArrowLeft", "right": "ArrowRight",
	"backspace": "Backspace", "delete": "Delete",
}

// optimisticActions is the runtime's OPTIMISTIC: the built-ins that
// .optimistic applies in the browser, which then do not take the event's
// value as an extra argument.
var optimisticActions = []string{"$set", "$toggle"}

// eventAttr is one brick:<kind> attribute with its parsed modifiers:
// flags holds every modifier without an argument that the table knows
// (once, window, leave, optimistic…), key the KeyboardEvent key of a
// brick:keydown.<key>.
type eventAttr struct {
	name, value string
	flags       map[string]bool
	key         string
	// dwell is brick:intersect.dwell's wait (flags["dwell"] says it is
	// there): its duration, or brick.DwellDefaultMS when none follows.
	dwell time.Duration
}

// parseMods is the runtime's parseMods: unknown segments are ignored
// (the engine refuses them at compile time), and a timed modifier's
// argument is consumed with it.
func parseMods(kind string, segs []string) (map[string]bool, string, time.Duration) {
	g := grammarTable()
	tokens := g.Attrs[kind]
	flags := map[string]bool{}
	key := ""
	dwell := time.Duration(brick.DwellDefaultMS) * time.Millisecond
	for i := 0; i < len(segs); i++ {
		s := segs[i]
		switch {
		case slices.ContainsFunc(tokens, func(t string) bool { return strings.HasPrefix(t, s+".") }):
			flags[s] = true
			if i+1 < len(segs) && startsWithDigit(segs[i+1]) {
				if n, err := strconv.Atoi(strings.TrimSuffix(segs[i+1], "ms")); err == nil && s == "dwell" {
					dwell = time.Duration(n) * time.Millisecond
				}
				i++
			}
		case slices.Contains(tokens, "<key>") && slices.Contains(g.Keys, s):
			key = keyEvent[s]
			if key == "" {
				key = s // a letter or digit matches itself
			}
		case slices.Contains(tokens, s):
			flags[s] = true
		}
	}
	return flags, key, dwell
}

func startsWithDigit(s string) bool { return s != "" && s[0] >= '0' && s[0] <= '9' }

// attrsOf lists el's brick:<kind> attributes, bare or with modifiers, in
// name order.
func attrsOf(el element, kind string) []eventAttr {
	base := "brick:" + kind
	var out []eventAttr
	for _, name := range slices.Sorted(maps.Keys(el.attrs)) {
		if name != base && !strings.HasPrefix(name, base+".") {
			continue
		}
		flags, key, dwell := parseMods(kind, strings.Split(name, ".")[1:])
		out = append(out, eventAttr{name: name, value: el.attrs[name], flags: flags, key: key, dwell: dwell})
	}
	return out
}

// closestWith walks up from element i to the first element with a
// brick:<kind> attribute that accept takes; -1 when there is none. A
// brick:click.outside is never taken, because a click inside its element
// is exactly the click it ignores.
func closestWith(els []element, i int, kind string, accept func(eventAttr) bool) (int, eventAttr) {
	for ; i >= 0; i = els[i].parent {
		for _, a := range attrsOf(els[i], kind) {
			if kind == "click" && a.flags["outside"] {
				continue
			}
			if accept(a) {
				return i, a
			}
		}
	}
	return -1, eventAttr{}
}

// eventRecord is what the runtime remembers about one attribute of one
// element: whether its .once already ran, whether a brick:intersect has
// seen the element visible, and whether a .dwell already fired during the
// current stay in view (it fires once per stay; Leave ends the stay).
type eventRecord struct{ fired, seen, dwelt bool }

// record returns the memory of attribute a on element i. The runtime keys
// it by DOM node, which a morph keeps; here an element is its stateful
// root plus its position among that root's elements carrying the same
// attribute, which holds across re-renders that keep the markup's shape.
func (l *LivePage) record(els []element, i int, a eventAttr) *eventRecord {
	root := closestStateful(els, i)
	n := 0
	for k := root + 1; k < i; k++ {
		if _, ok := els[k].attrs[a.name]; ok && closestStateful(els, k) == root {
			n++
		}
	}
	id := ""
	if root >= 0 {
		id = els[root].attrs["data-brick"]
	}
	key := fmt.Sprintf("%s|%s|%d", id, a.name, n)
	if l.events == nil {
		l.events = map[string]*eventRecord{}
	}
	rec := l.events[key]
	if rec == nil {
		rec = &eventRecord{}
		l.events[key] = rec
	}
	return rec
}

// once reports whether attribute a on element i may run, marking a .once
// attribute as fired.
func (l *LivePage) once(els []element, i int, a eventAttr) bool {
	if !a.flags["once"] {
		return true
	}
	rec := l.record(els, i, a)
	if rec.fired {
		return false
	}
	rec.fired = true
	return true
}

// fire is the runtime's fire + act for one event attribute on element i:
// .once is checked, the call parsed, $dispatch delivered client-side, the
// target root resolved ($parent.<name> goes up one), and extra (the
// event's value, when has is set) appended as the last argument unless
// .optimistic applies the built-in in the browser. It returns nil when a
// modifier kept the action from running.
func (l *LivePage) fire(els []element, i int, a eventAttr, extra any, has bool) *brick.ActionResult {
	l.t.Helper()
	if !l.once(els, i, a) {
		return nil
	}
	name, args, err := parseCall(a.value)
	if err != nil {
		l.t.Fatalf("bricktest: %s %q: %v", a.name, a.value, err)
	}
	switch name {
	case "$dispatch":
		return l.clientDispatch(els, i, args)
	case "$cancelUpload":
		l.t.Fatalf("bricktest: %s: $cancelUpload is browser-only; there is nothing to cancel in a test", a.name)
	}
	root, name := l.target(els, i, name)
	appliedInBrowser := a.flags["optimistic"] && slices.Contains(optimisticActions, name)
	if has && !appliedInBrowser {
		args = append(args, extra)
	}
	return root.call(name, args, nil)
}

// Change fires a change event on the first element under this root
// matching selector, as the runtime does: the nearest brick:change on it
// or above it runs, and when that element is a form control its new value
// is the action's last argument — a checkbox's checked state (value must
// be a bool), else the control's value as a string (value is formatted
// with fmt.Sprint, as the browser holds every value as text). On any other
// element value must be nil, because the runtime sends no value there.
// The test fails on an unknown selector, no brick:change, a wrong value
// type, or the action's error; nil means a modifier (.once) kept it from
// running.
func (r *LiveRoot) Change(selector string, value any) *brick.ActionResult {
	r.live.t.Helper()
	return r.valued("change", selector, value, true)
}

// Input fires an input event (one edit) on the first element under this
// root matching selector, with the same rules as Change: the nearest
// brick:input on it or above it runs, with the control's new value as
// the last argument. .debounce collapses as it does for every driver:
// each call is one edit far enough from the last to be sent.
func (r *LiveRoot) Input(selector string, value any) *brick.ActionResult {
	r.live.t.Helper()
	return r.valued("input", selector, value, true)
}

// Blur moves focus away from the first element under this root matching
// selector: its own brick:blur runs (an ancestor's does not, as blur does
// not bubble), with the value rules of Change. The test fails when the
// element itself has no brick:blur.
func (r *LiveRoot) Blur(selector string, value any) *brick.ActionResult {
	r.live.t.Helper()
	return r.valued("blur", selector, value, false)
}

// Focus gives focus to the first element under this root matching
// selector: its own brick:focus runs, with no extra argument. The test
// fails when the element itself has no brick:focus.
func (r *LiveRoot) Focus(selector string) *brick.ActionResult {
	r.live.t.Helper()
	return r.valued("focus", selector, nil, false)
}

// valued runs the brick:<kind> attribute for an event that carries the
// control's value (change, input, blur; focus carries none). up says
// whether the event bubbles: then the nearest attribute on the element
// or above it runs; otherwise every brick:<kind> on the element itself
// does, as the runtime's capturing focus and blur listeners run them,
// and the first result is returned.
func (r *LiveRoot) valued(kind, selector string, value any, up bool) *brick.ActionResult {
	r.live.t.Helper()
	l := r.live
	els := scan(l.html)
	i := r.find(els, selector)
	// A .window focus or blur belongs to the browser window, not the
	// element (LivePage.WindowFocus runs those).
	var own []eventAttr
	for _, a := range attrsOf(els[i], kind) {
		if !a.flags["window"] {
			own = append(own, a)
		}
	}
	j, attrs, where := i, own, "on it"
	if up {
		var a eventAttr
		j, a = closestWith(els, i, kind, func(eventAttr) bool { return true })
		attrs, where = []eventAttr{a}, "on it or above it"
	}
	if j < 0 || len(attrs) == 0 {
		l.t.Fatalf("bricktest: %q in %s has no brick:%s %s", selector, r.id, kind, where)
	}
	el := els[j]
	driver := strings.ToUpper(kind[:1]) + kind[1:]
	var extra any
	has := true
	switch {
	case kind == "focus":
		has = false
	case el.name == "input" && strings.EqualFold(el.attrs["type"], "checkbox"):
		b, ok := value.(bool)
		if !ok {
			l.t.Fatalf("bricktest: %s: %q is a checkbox; value must be a bool, not %T", driver, selector, value)
		}
		extra = b
	case el.name == "input" || el.name == "select" || el.name == "textarea":
		extra = fmt.Sprint(value)
	default:
		has = false
	}
	if !has && value != nil {
		l.t.Fatalf("bricktest: %s: the brick:%s element of %q is a <%s>, which takes no value; pass nil", driver, kind, selector, el.name)
	}
	var first *brick.ActionResult
	for _, a := range attrs {
		if res := l.fire(els, j, a, extra, has); first == nil {
			first = res
		}
	}
	return first
}

// ClickOutside clicks somewhere outside the first element under this root
// matching selector: each of its brick:click.outside attributes runs, as
// the runtime's do for a click that lands outside the element, unless the
// element is hidden (a [hidden] on it or an ancestor), when nothing runs.
// A real click elsewhere also runs that element's own brick:click and
// every other displayed .outside on the page; drive those with Click and
// further ClickOutside calls. It returns the first result; nil when
// nothing ran. The test fails when the element has no
// brick:click.outside.
func (r *LiveRoot) ClickOutside(selector string) *brick.ActionResult {
	r.live.t.Helper()
	l := r.live
	els := scan(l.html)
	i := r.find(els, selector)
	var outs []eventAttr
	for _, a := range attrsOf(els[i], "click") {
		if a.flags["outside"] {
			outs = append(outs, a)
		}
	}
	if len(outs) == 0 {
		l.t.Fatalf("bricktest: %q in %s has no brick:click.outside", selector, r.id)
	}
	for k := i; k >= 0; k = els[k].parent {
		if _, hidden := els[k].attrs["hidden"]; hidden {
			return nil
		}
	}
	var first *brick.ActionResult
	for _, a := range outs {
		x, has := a.clickArg(ClickDetails{Count: 1})
		if res := l.fire(els, i, a, x, has); first == nil {
			first = res
		}
	}
	return first
}

// ClickDetails is the click a test describes for brick:click.details
// (v22): the dict the runtime passes as the call's last argument, with
// the same keys (clickDetails in runtime.js). X and Y are relative to the
// element carrying the attribute, ClientX and ClientY to the viewport;
// Count is the click count (0 for a keyboard click).
type ClickDetails struct {
	Alt, Ctrl, Meta, Shift bool
	Button, Count          int
	X, Y, ClientX, ClientY int
}

// arg is the dict the runtime sends.
func (d ClickDetails) arg() map[string]any {
	return map[string]any{
		"alt": d.Alt, "ctrl": d.Ctrl, "meta": d.Meta, "shift": d.Shift,
		"button": d.Button, "count": d.Count,
		"x": d.X, "y": d.Y, "clientX": d.ClientX, "clientY": d.ClientY,
	}
}

// clickArg is a click attribute's extra argument: d's dict with
// .details, nothing without it.
func (a eventAttr) clickArg(d ClickDetails) (any, bool) {
	if !a.flags["details"] {
		return nil, false
	}
	return d.arg(), true
}

// ClickWith is Click with the click described: an attribute with .details
// gets d as its last argument, the way the runtime passes the real
// click's keys and position. Click itself sends a plain left click at
// 0,0 to a .details attribute.
func (r *LiveRoot) ClickWith(selector string, d ClickDetails) *brick.ActionResult {
	r.live.t.Helper()
	l := r.live
	els := scan(l.html)
	j, a := closestWith(els, r.find(els, selector), "click", func(eventAttr) bool { return true })
	if j < 0 {
		l.t.Fatalf("bricktest: %q in %s has no brick:click on it or above it", selector, r.id)
	}
	x, has := a.clickArg(d)
	return l.fire(els, j, a, x, has)
}

// keyModifiers are the runtime's KEY_MODIFIERS: the combo modifiers a key
// press may hold, written before the key and joined with "+" in a
// driver's key ("ctrl+Enter", "meta+shift+k").
var keyModifiers = []string{"ctrl", "meta", "shift", "alt"}

// keyPress is one key press: the KeyboardEvent key and the modifier keys
// held.
type keyPress struct {
	key  string
	held map[string]bool
}

// parseKeyPress reads a driver's key: modifiers from keyModifiers, then
// the key, joined with "+" ("+" alone, or a trailing "++", is the plus
// key). An unknown modifier fails the test, so "cmd+k" is not silently
// a press of "cmd+k".
func (l *LivePage) parseKeyPress(s string) keyPress {
	l.t.Helper()
	p := keyPress{key: s, held: map[string]bool{}}
	if s == "+" || !strings.Contains(s, "+") {
		return p
	}
	var rest string
	if strings.HasSuffix(s, "++") {
		rest, p.key = strings.TrimSuffix(s, "++"), "+"
	} else {
		i := strings.LastIndex(s, "+")
		rest, p.key = s[:i], s[i+1:]
	}
	for _, m := range strings.Split(rest, "+") {
		if !slices.Contains(keyModifiers, m) {
			l.t.Fatalf("bricktest: key %q: unknown modifier %q (use %s)", s, m, strings.Join(keyModifiers, ", "))
		}
		p.held[m] = true
	}
	return p
}

// matches is the runtime's keyMatches: an attribute without a key takes
// every press; one with a key takes it only when exactly its modifier
// keys are held, and a letter matches in either case (shift+k is "K").
func (p keyPress) matches(a eventAttr) bool {
	if a.key == "" {
		return true
	}
	for _, m := range keyModifiers {
		if a.flags[m] != p.held[m] {
			return false
		}
	}
	if len(a.key) == 1 {
		return a.key == strings.ToLower(p.key)
	}
	return a.key == p.key
}

// Keydown presses key (a KeyboardEvent key: "Enter", "Escape", "ArrowUp",
// " ", "a", with any modifiers before it: "ctrl+Enter", "meta+k") with
// focus on the first element under this root matching selector, as the
// runtime does: the innermost brick:keydown on it or above it whose key
// and modifiers match (a bare brick:keydown matches every key) runs,
// without an extra argument. .window attributes are not part of it;
// LivePage.Keydown presses a key for those. The test fails on an unknown
// selector, no matching brick:keydown, or the action's error; nil means a
// modifier (.once) kept it from running.
func (r *LiveRoot) Keydown(selector, key string) *brick.ActionResult {
	r.live.t.Helper()
	return r.press("keydown", selector, key)
}

// Keyup releases key with focus on the element, with the rules of
// Keydown, for brick:keyup.
func (r *LiveRoot) Keyup(selector, key string) *brick.ActionResult {
	r.live.t.Helper()
	return r.press("keyup", selector, key)
}

func (r *LiveRoot) press(kind, selector, key string) *brick.ActionResult {
	r.live.t.Helper()
	l := r.live
	p := l.parseKeyPress(key)
	els := scan(l.html)
	j, a := closestWith(els, r.find(els, selector), kind, func(a eventAttr) bool {
		return !a.flags["window"] && p.matches(a)
	})
	if j < 0 {
		l.t.Fatalf("bricktest: %q in %s has no brick:%s for %q on it or above it", selector, r.id, kind, key)
	}
	return l.fire(els, j, a, nil, false)
}

// Keydown presses key anywhere on the page: every brick:keydown.window
// attribute inside a stateful component whose key matches runs, in
// document order (of the page before the key), as the runtime's window
// listeners do. It returns the
// results of the actions that ran (none is not a failure: a key nobody
// listens for does nothing).
func (l *LivePage) Keydown(key string) []*brick.ActionResult {
	l.t.Helper()
	return l.pressWindow("keydown", key)
}

// Keyup releases key anywhere on the page, for brick:keyup.window, with
// the rules of LivePage.Keydown.
func (l *LivePage) Keyup(key string) []*brick.ActionResult {
	l.t.Helper()
	return l.pressWindow("keyup", key)
}

func (l *LivePage) pressWindow(kind, key string) []*brick.ActionResult {
	l.t.Helper()
	p := l.parseKeyPress(key)
	var out []*brick.ActionResult
	els := scan(l.html)
	for i, el := range els {
		if closestStateful(els, i) < 0 {
			continue
		}
		for _, a := range attrsOf(el, kind) {
			if !a.flags["window"] || !p.matches(a) {
				continue
			}
			if res := l.fire(els, i, a, nil, false); res != nil {
				out = append(out, res)
			}
		}
	}
	return out
}

// WindowFocus reports that the browser window (the tab) gained focus:
// every brick:focus.window inside a stateful component runs, in document
// order, with no value, as the runtime's window listener does. It
// returns the results of the actions that ran; none is not a failure.
func (l *LivePage) WindowFocus() []*brick.ActionResult {
	l.t.Helper()
	return l.windowFocus("focus")
}

// WindowBlur reports that the browser window lost focus, for
// brick:blur.window, with the rules of WindowFocus.
func (l *LivePage) WindowBlur() []*brick.ActionResult {
	l.t.Helper()
	return l.windowFocus("blur")
}

func (l *LivePage) windowFocus(kind string) []*brick.ActionResult {
	l.t.Helper()
	var out []*brick.ActionResult
	els := scan(l.html)
	for i, el := range els {
		if closestStateful(els, i) < 0 {
			continue
		}
		for _, a := range attrsOf(el, kind) {
			if !a.flags["window"] {
				continue
			}
			if res := l.fire(els, i, a, nil, false); res != nil {
				out = append(out, res)
			}
		}
	}
	return out
}

// Intersect reports the first element under this root matching selector
// as visible, as the browser's IntersectionObserver does: each of its
// brick:intersect attributes without .leave runs (unless a .once one
// already did), and each .leave one remembers that the element was seen.
// The element stays in view long enough for any .dwell, which runs once
// per stay: a second Intersect without a Leave between runs no .dwell
// again, as the browser's element that never left counts one impression.
// The action runs on the root around the element, with no confirmation
// and no extra argument. It returns the first result; nil when nothing
// ran. The test fails on an unknown selector, an element without
// brick:intersect (the observer watches the element itself, never an
// ancestor), or the action's error.
func (r *LiveRoot) Intersect(selector string) *brick.ActionResult {
	r.live.t.Helper()
	return r.viewport(selector, false, -1)
}

// IntersectFor is a stay of length d: the element enters the viewport,
// stays d, then leaves, so a .dwell longer than d never runs (its timer
// is cancelled by the leave) and one of d or less does. The attributes
// without .dwell run as Intersect's do, then the .leave ones as Leave's
// do. It returns the first result; nil when nothing ran.
func (r *LiveRoot) IntersectFor(selector string, d time.Duration) *brick.ActionResult {
	r.live.t.Helper()
	if d < 0 {
		r.live.t.Fatalf("bricktest: IntersectFor %q: a stay cannot be negative (%s)", selector, d)
	}
	first := r.viewport(selector, false, d)
	if res := r.viewport(selector, true, d); first == nil {
		first = res
	}
	return first
}

// Leave reports the element as no longer visible: each .leave
// brick:intersect runs, but only after an Intersect saw the element, so
// an element that starts off screen never leaves. It ends the stay, so
// the next Intersect may run each .dwell again.
func (r *LiveRoot) Leave(selector string) *brick.ActionResult {
	r.live.t.Helper()
	return r.viewport(selector, true, -1)
}

// viewport reports one viewport change. stay is how long the element
// stayed in view before leaving; a negative stay is long enough for any
// .dwell.
func (r *LiveRoot) viewport(selector string, leaving bool, stay time.Duration) *brick.ActionResult {
	r.live.t.Helper()
	l := r.live
	els := scan(l.html)
	i := r.find(els, selector)
	attrs := attrsOf(els[i], "intersect")
	if len(attrs) == 0 {
		l.t.Fatalf("bricktest: %q in %s has no brick:intersect", selector, r.id)
	}
	var first *brick.ActionResult
	for _, a := range attrs {
		rec := l.record(els, i, a)
		if !leaving {
			rec.seen = true
		}
		if a.flags["dwell"] {
			if leaving {
				rec.dwelt = false
				continue
			}
			if rec.dwelt || (a.flags["once"] && rec.fired) || (stay >= 0 && stay < a.dwell) {
				continue
			}
			rec.dwelt = true
		} else if a.flags["leave"] != leaving || (leaving && !rec.seen) || (a.flags["once"] && rec.fired) {
			continue
		}
		name, args, err := parseCall(a.value)
		if err != nil {
			l.t.Fatalf("bricktest: %s %q: %v", a.name, a.value, err)
		}
		root, name := l.target(els, i, name)
		rec.fired = true
		res := root.call(name, args, nil)
		if first == nil {
			first = res
		}
	}
	return first
}
