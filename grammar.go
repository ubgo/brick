package brick

import (
	"encoding/json"
	"fmt"
	"html"
	"regexp"
	"slices"
	"sort"
	"strings"
)

// The brick: attribute grammar is this one table.
// The compiler checks every brick: attribute of a stateful file against
// it, so a modifier typo (brick:click.onec) is a compile error with a file
// and line instead of a click that silently ignores it; the browser runtime
// carries the same table as one generated line (GrammarJS), and
// TestGrammarMatchesRuntime fails when the two drift.

// Modifier tokens that take an argument, as the table writes them: the
// name, a dot, then the argument's placeholder.
const (
	// modMS is a duration token, \d+ms, that must follow its modifier.
	modMS = "<ms>"
	// modOptMS is a duration token that may follow its modifier.
	modOptMS = "<ms>?"
	// modInterval is a poll interval token, \d+(ms|s|m).
	modInterval = "<interval>"
	// modKey is one key from grammarKeys.
	modKey = "<key>"
	// modPX is a length token, \d+px, that must follow its modifier
	// (brick:intersect.margin.200px).
	modPX = "<px>"
)

// Attribute names, after the brick: prefix, that code outside the grammar
// and plain-attribute tables branches on: brick:model is the one binding a
// locked field must refuse (locked.go), and brick:method has its own
// per-tag check (methodlink.go) on plain elements and component calls.
const (
	attrModel  = "model"
	attrMethod = "method"
)

// Modifier names the compiler gives a meaning beyond the table.
const (
	// modOptimistic writes a literal $set/$toggle into the signals before
	// the server answers, so it is valid only with those calls.
	modOptimistic = "optimistic"
)

// argRE is the pattern each argument token accepts, and argExample the
// sample its error message shows.
var (
	argRE      = map[string]*regexp.Regexp{modMS: msRE, modOptMS: msRE, modPX: pxRE}
	argExample = map[string]string{modMS: "a duration such as %s.300ms", modOptMS: "a duration such as %s.300ms", modPX: "a length such as %s.200px"}
)

// valueKind is what an attribute's value holds.
type valueKind int

const (
	vCall    valueKind = iota // an action call: name or name(JSON args)
	vPath                     // one state path
	vShow                     // one state path, optionally negated with !
	vPaths                    // space-separated state paths, or empty
	vActions                  // space-separated action names, or empty
	vText                     // plain text
	vPoll                     // an interval, or a call when the interval is a modifier
	vUpload                   // one field declared in brick: {uploads:}
)

// attrRule is one brick: attribute: its modifiers, as table tokens, and
// what its value holds.
type attrRule struct {
	mods  []string
	value valueKind
}

// Shared modifier lists.
var (
	eventMods = []string{"once", "debounce." + modMS, "throttle." + modMS}
	// optimisticMods are the events whose literal $set/$toggle may be
	// written client-side first.
	optimisticMods = append(slices.Clone(eventMods), modOptimistic)
	// keyMods are brick:keydown's and brick:keyup's: .window listens on
	// the whole page, a key names the one key that matches, and the
	// modifier keys (keyModifiers) make it a combo.
	keyMods = append(append(slices.Clone(optimisticMods), "window"), append(slices.Clone(keyModifiers), modKey)...)
	// keydownMods adds .norepeat (v22): a held key fires the call once,
	// on its first keydown, instead of once per auto-repeat. Repeating
	// stays the default because a held arrow key stepping through a list
	// is what most key bindings want; keyup never repeats.
	keydownMods = append(slices.Clone(keyMods), "norepeat")
	// clickMods adds .outside: the call runs for a click that lands
	// outside the element,
	// and .details: the call gets one more argument, a dict of the
	// click's modifier keys, button, count and pointer position (the
	// runtime's clickDetails). Opt-in, so an action that never asked for
	// it keeps its argument list and nothing about the pointer is sent.
	clickMods = append(slices.Clone(optimisticMods), "outside", "details")
)

// keyModifiers are the modifier keys a key combo names
// (brick:keydown.ctrl.enter). A combo matches only when exactly those
// modifier keys are held, so .enter does not also fire for ctrl+enter
// and .ctrl.enter not for ctrl+shift+enter; each needs a key, because a
// modifier alone would fire on every key pressed while it is held.
var keyModifiers = []string{"ctrl", "meta", "shift", "alt"}

// grammar is every action attribute a stateful file accepts (eventAttrs is
// derived from it). Modifier tokens are listed in a fixed order; the
// runtime reads the same table.
var grammar = map[string]attrRule{
	attrModel: {mods: []string{"lazy", "defer", "debounce." + modMS, "throttle." + modMS}, value: vPath},
	"click":   {mods: clickMods, value: vCall},
	// .lock makes the form's fields read-only while its action is queued
	// or in flight; the default stays buttons-only.
	"submit":  {mods: append(slices.Clone(eventMods), "lock"), value: vCall},
	"change":  {mods: optimisticMods, value: vCall},
	"keydown": {mods: keydownMods, value: vCall},
	"keyup":   {mods: keyMods, value: vCall},
	// brick:input runs on every edit of a control (pair it with
	// .debounce); brick:focus and brick:blur when the element itself gains
	// or loses focus. input and blur pass the control's value, as change
	// does; focus passes none. .window runs them when the browser window
	// (the tab) gains or loses focus instead, with no value: a refresh
	// when the user comes back.
	"input": {mods: optimisticMods, value: vCall},
	"focus": {mods: append(slices.Clone(eventMods), "window"), value: vCall},
	"blur":  {mods: append(slices.Clone(optimisticMods), "window"), value: vCall},
	"text":  {value: vPath},
	"show":  {value: vShow},
	// brick:loading's value names the jobs it shows for (loadingTargets);
	// .except shows it for every job but those.
	"loading": {mods: []string{"delay." + modOptMS, "remove", "except"}, value: vActions},
	"dirty":   {mods: []string{"remove"}, value: vPaths},
	// .visible pauses a poll while its element is off screen; .keep-alive
	// keeps it running while the tab is hidden, which every other poll
	// skips.
	"poll":         {mods: []string{modInterval, "visible", "keep-alive"}, value: vPoll},
	"disable-with": {value: vText},
	// brick:confirm asks the confirmer before the call runs; .prompt asks
	// the prompter (brick.promptWith, window.prompt by default) for text
	// instead, and the answer becomes the call's last argument. A cancel
	// sends nothing.
	"confirm": {mods: []string{"prompt"}, value: vText},
	// brick:upload goes on <input type=file>; brick:upload-progress on a
	// <progress> the runtime fills while that field's files are sent
	//.
	"upload":          {value: vUpload},
	"upload-progress": {value: vUpload},
	// brick:intersect runs a call when the element enters the viewport
	// (.leave: when it leaves); .half/.full are thresholds, .margin.<px>
	// the root margin. .dwell[.<ms>] waits until the element
	// has stayed visible that long (DwellDefaultMS when no duration is
	// given), so a row scrolled past is not an impression.
	"intersect": {mods: []string{"leave", "once", "half", "full", "margin." + modPX, "dwell." + modOptMS}, value: vCall},
}

// namedKeys are the keys brick:keydown.<key> and brick:keyup.<key> accept
// by name; grammarKeys adds the letters a-z and digits 0-9, which match
// themselves (brick:keydown.meta.k). No key means any key.
var namedKeys = []string{"enter", "escape", "tab", "space", "up", "down", "left", "right", "backspace", "delete"}

var grammarKeys = func() []string {
	out := slices.Clone(namedKeys)
	for c := 'a'; c <= 'z'; c++ {
		out = append(out, string(c))
	}
	for c := '0'; c <= '9'; c++ {
		out = append(out, string(c))
	}
	return out
}()

// modConflicts are modifier pairs that cannot be combined: defer means
// "never send alone", the others say when to send.
var modConflicts = [][2]string{
	{"defer", "lazy"}, {"defer", "debounce"}, {"defer", "throttle"},
	{"debounce", "throttle"},
	{"half", "full"},
	// .dwell times a stay in view; leaving ends the stay rather than
	// completing it, so the two have no combined meaning.
	{"dwell", "leave"},
}

// DwellDefaultMS is how long brick:intersect.dwell waits when no duration
// follows it: one second, the usual bar for counting an impression. The
// runtime carries the same number as DWELL_MS, and bricktest reads it.
const DwellDefaultMS = 1000

var (
	msRE       = regexp.MustCompile(`^\d+ms$`)
	pxRE       = regexp.MustCompile(`^\d+px$`)
	intervalRE = regexp.MustCompile(`^\d+(ms|s|m)$`)
	pathRE     = regexp.MustCompile(`^[\w-]+(\.[\w-]+)*$`)
	actionRE   = regexp.MustCompile(`^(\$(?:set|refresh|toggle|reset|upload|uploaded|removeUpload)|[\w]+(\.[\w]+)?)$`)
)

// builtinLoading are the names brick:loading accepts besides the
// component's functions.
var builtinLoading = []string{actionSet, actionRefresh, actionToggle, actionReset, actionUpload, actionUploaded, actionRemoveUpload}

// eventAttrs are the action attributes, accepted only inside a component
// that declares state: — every key of grammar, sorted.
var eventAttrs = func() []string {
	out := make([]string, 0, len(grammar))
	for k := range grammar {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}()

// GrammarJS is the line runtime.js carries so the browser parses
// modifiers from the same table the compiler checks: attribute → modifier
// tokens (brick:js included, though it is not an action attribute), the
// key list, and the JS commands with their argument counts. Deterministic
// (sorted keys), so a test can compare it byte for byte.
func GrammarJS() string {
	attrs := map[string][]string{}
	for k, r := range grammar {
		attrs[k] = append([]string{}, r.mods...)
	}
	attrs[jsAttr] = append([]string{}, jsRule.mods...)
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false) // the line is JavaScript, not HTML
	_ = enc.Encode(struct {
		Attrs map[string][]string `json:"attrs"`
		Keys  []string            `json:"keys"`
		JS    map[string][2]int   `json:"js"`
	}{attrs, grammarKeys, jsCommands})
	return "const GRAMMAR = " + strings.TrimSpace(b.String()) + ";"
}

// brickAttr is one brick: attribute as the scan found it.
type brickAttr struct {
	name     string   // "click"
	mods     []string // ["debounce", "300ms"]
	value    string   // unescaped
	hasValue bool
}

// fullName is the attribute as written, for messages.
func (a brickAttr) fullName() string {
	return strings.Join(append([]string{"brick:" + a.name}, a.mods...), ".")
}

// parseBrickAttrName splits "click.debounce.300ms" into its name and
// modifier tokens.
func parseBrickAttrName(s string) brickAttr {
	parts := strings.Split(s, ".")
	return brickAttr{name: parts[0], mods: parts[1:]}
}

// checkMods checks an attribute's modifiers against its rule. It returns
// the modifier names used (arguments dropped), for the value check.
func checkMods(a brickAttr, r attrRule) (map[string]bool, error) {
	used := map[string]bool{}
	bad := func(format string, args ...any) error {
		return fmt.Errorf("%s: "+format+" (%s accepts %s)", append(append([]any{a.fullName()}, args...), "brick:"+a.name, describeMods(r))...)
	}
	keys := 0
	for i := 0; i < len(a.mods); i++ {
		tok := a.mods[i]
		matched := false
		for _, m := range r.mods {
			name, arg, hasArg := strings.Cut(m, ".")
			switch {
			case m == modInterval && intervalRE.MatchString(tok):
				name = modInterval
			case m == modKey && slices.Contains(grammarKeys, tok):
				name = modKey
				keys++
			case name == tok && !hasArg:
			case name == tok && hasArg:
				next := ""
				if i+1 < len(a.mods) {
					next = a.mods[i+1]
				}
				if argRE[arg] != nil && argRE[arg].MatchString(next) {
					i++
				} else if arg != modOptMS {
					return nil, bad("%s needs "+argExample[arg], tok, tok)
				}
			default:
				continue
			}
			if used[name] && name != modKey {
				return nil, bad("%s is given twice", tok)
			}
			used[name] = true
			matched = true
			break
		}
		if !matched {
			return nil, bad("unknown modifier %q", tok)
		}
	}
	if keys > 1 {
		return nil, bad("one key at most")
	}
	for _, m := range keyModifiers {
		if used[m] && keys == 0 {
			return nil, bad(".%s needs a key, such as .%s.enter", m, m)
		}
	}
	for _, c := range modConflicts {
		if used[c[0]] && used[c[1]] {
			return nil, bad(".%s cannot be combined with .%s", c[0], c[1])
		}
	}
	return used, nil
}

// describeMods lists a rule's modifiers for an error message.
func describeMods(r attrRule) string {
	if len(r.mods) == 0 {
		return "no modifiers"
	}
	out := make([]string, 0, len(r.mods))
	for _, m := range r.mods {
		if m == modKey {
			m = "<key>: " + strings.Join(namedKeys, " ") + " a-z 0-9"
		}
		out = append(out, "."+m)
	}
	return strings.Join(out, ", ")
}

// checkBrickAttr checks one action attribute of a stateful file: its
// modifiers against the grammar, and a literal value against its kind.
// state is the file's declared state (the first segment of a path must be
// declared); loading collects brick:loading action names, which are
// checked once the file's scripts are compiled. A value with {{ }} is
// filled at render time and checked by the server ($set) or runtime.
func checkBrickAttr(a brickAttr, m *meta, loading *[]string) error {
	state := m.state
	r, ok := grammar[a.name]
	if !ok {
		return fmt.Errorf("brick:%s is not an action attribute", a.name)
	}
	used, err := checkMods(a, r)
	if err != nil {
		return err
	}
	v := html.UnescapeString(a.value)
	if used[modOptimistic] {
		if err := checkOptimistic(a, v); err != nil {
			return err
		}
	}
	if strings.Contains(v, "{{") {
		return nil
	}
	v = strings.TrimSpace(v)
	if err := checkLockedAttr(a, v, m); err != nil {
		return err
	}
	path := func(p string) error {
		if !pathRE.MatchString(p) {
			return fmt.Errorf("%s=%q: %q is not a state path (field or field.key.0)", a.fullName(), v, p)
		}
		first, _, _ := strings.Cut(p, ".")
		if _, ok := state[first]; !ok {
			return fmt.Errorf("%s=%q: %q is not a field of this component's state (declared: %s)", a.fullName(), v, first, strings.Join(sortedKeys(state), ", "))
		}
		return nil
	}
	switch r.value {
	case vCall:
		if err := checkActionCall(v); err != nil {
			return err
		}
		return checkUploadCall(a, v, m)
	case vUpload:
		if _, ok := m.uploads[v]; !ok {
			return fmt.Errorf("%s=%q: %q is not an upload field (declare it in brick: {uploads:}; declared: %s)", a.fullName(), v, v, strings.Join(sortedKeys(m.uploads), ", "))
		}
	case vPath:
		return path(v)
	case vShow:
		return path(strings.TrimPrefix(v, "!"))
	case vPaths:
		for _, p := range strings.Fields(v) {
			if err := path(p); err != nil {
				return err
			}
		}
	case vActions:
		targets, err := loadingTargets(v)
		if err != nil {
			return fmt.Errorf("%s=%q: %w", a.fullName(), v, err)
		}
		if used["except"] && len(targets) == 0 {
			return fmt.Errorf("%s needs the actions it does not show for, such as %s=\"download\"", a.fullName(), a.fullName())
		}
		for _, t := range targets {
			if !actionRE.MatchString(t.name) {
				return fmt.Errorf("%s=%q: %q is not an action name", a.fullName(), v, t.name)
			}
			if t.name == actionSet && t.hasArgs {
				f, ok := firstString(t.args)
				if !ok {
					return fmt.Errorf("%s=%q: $set targets a field by name, such as $set(\"query\")", a.fullName(), v)
				}
				if err := path(f); err != nil {
					return err
				}
			}
			*loading = append(*loading, t.name)
		}
	case vPoll:
		if used[modInterval] {
			if v == "" {
				return fmt.Errorf("%s needs an action call as its value, or write brick:poll=%q for $refresh", a.fullName(), "5s")
			}
			return checkActionCall(v)
		}
		if !intervalRE.MatchString(v) {
			return fmt.Errorf("%s=%q: the value is an interval such as 5s, or write brick:poll.5s=\"action\" to run an action", a.fullName(), v)
		}
	}
	return nil
}

// loadingTarget is one token of a brick:loading value: an action name,
// and when written as a call, the arguments a job's must start with.
type loadingTarget struct {
	name    string
	args    []any
	hasArgs bool
}

// loadingTargets splits a brick:loading value into its targets: "save",
// "remove(5)" (only the job removing 5),
// "$set(\"query\")" (a change of that field). Tokens are separated by
// spaces outside parentheses and strings, so remove(5, "a b") is one
// token. Arguments are JSON literals, as an action call's are; the
// runtime matches them as a prefix of the job's arguments, so a value an
// event or .prompt appends still matches.
func loadingTargets(v string) ([]loadingTarget, error) {
	var toks []string
	depth, inStr, start := 0, false, -1
	flush := func(i int) {
		if start >= 0 {
			toks = append(toks, v[start:i])
			start = -1
		}
	}
	for i := 0; i < len(v); i++ {
		c := v[i]
		switch {
		case inStr && c == '\\':
			i++
			continue
		case c == '"':
			inStr = !inStr
		case inStr:
		case c == '(':
			depth++
		case c == ')':
			depth--
		case depth == 0 && (c == ' ' || c == '\t' || c == '\n' || c == '\r'):
			flush(i)
			continue
		}
		if start < 0 {
			start = i
		}
	}
	flush(len(v))
	if inStr || depth != 0 {
		return nil, fmt.Errorf("unbalanced parentheses or quotes")
	}
	out := make([]loadingTarget, 0, len(toks))
	for _, tok := range toks {
		name, rest, call := strings.Cut(tok, "(")
		t := loadingTarget{name: name}
		if call {
			body, ok := strings.CutSuffix(rest, ")")
			if !ok {
				return nil, fmt.Errorf("%q: a call ends with )", tok)
			}
			t.hasArgs = true
			if strings.TrimSpace(body) != "" {
				if err := json.Unmarshal([]byte("["+body+"]"), &t.args); err != nil {
					return nil, fmt.Errorf("%q: arguments are JSON literals: %w", tok, err)
				}
			}
		}
		out = append(out, t)
	}
	return out, nil
}

// checkLoadingNames checks brick:loading's action names against the file's
// functions, once its scripts are compiled.
func checkLoadingNames(c *compiled, names []string) error {
	for _, n := range names {
		if slices.Contains(builtinLoading, n) {
			continue
		}
		if err := checkFunction(c, n); err != nil {
			return fmt.Errorf("brick:loading: %w", err)
		}
	}
	return nil
}

// checkUploadCall checks the field of a literal $removeUpload or
// $cancelUpload call against brick: {uploads:}, so a typo fails at compile
// instead of answering 422 (or doing nothing) when clicked.
func checkUploadCall(a brickAttr, v string, m *meta) error {
	mm := actionCallRE.FindStringSubmatch(v)
	if mm == nil || (mm[1] != actionRemoveUpload && mm[1] != actionCancelUpload) {
		return nil
	}
	var args []any
	if s := strings.TrimSpace(mm[2]); s != "" {
		if err := json.Unmarshal([]byte("["+s+"]"), &args); err != nil {
			return err
		}
	}
	want := 2
	if mm[1] == actionCancelUpload {
		want = 1
	}
	field, _ := firstString(args)
	if len(args) != want || field == "" {
		return fmt.Errorf("%s=%q: %s takes %s", a.fullName(), v, mm[1], map[int]string{1: "an upload field", 2: "an upload field and a file id"}[want])
	}
	if _, ok := m.uploads[field]; !ok {
		return fmt.Errorf("%s=%q: %q is not an upload field (declared: %s)", a.fullName(), v, field, strings.Join(sortedKeys(m.uploads), ", "))
	}
	return nil
}

// firstString returns args[0] when it is a string.
func firstString(args []any) (string, bool) {
	if len(args) == 0 {
		return "", false
	}
	s, ok := args[0].(string)
	return s, ok
}

// optimisticRE is the only call shape .optimistic accepts: a literal
// built-in name, because the browser can compute $set and $toggle without
// running code and nothing else.
var optimisticRE = regexp.MustCompile(`^\s*\$(?:set|toggle)\s*\(`)

// checkOptimistic refuses .optimistic on anything but a $set or $toggle
// call; the arguments are checked as any call's are.
func checkOptimistic(a brickAttr, v string) error {
	if !optimisticRE.MatchString(v) {
		return fmt.Errorf("%s=%q: .optimistic needs a literal $set(path, value) or $toggle(path) call, the only ones the browser can apply before the server answers", a.fullName(), v)
	}
	return nil
}
