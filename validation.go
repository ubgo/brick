package brick

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/ubgo/brick/validate"
)

// Rule is one declared check from brick: {rules:}.
// It lives in package validate, which imports nothing from brick, so the
// default Validator is reachable from the root without an import cycle.
type Rule = validate.Rule

// Validator checks fields against declared rules: Check at compile time,
// Validate at run time. Options.Validator replaces the default
// (validate.Default()) with go-playground, ozzo, CUE or an i18n wrapper.
type Validator = validate.Validator

// fieldRules is one path's declared rules, in the order written.
type fieldRules struct {
	path  string
	rules []Rule
}

// parseRules reads brick: {rules:}: a map of state path to a list of rules,
// each a name or a one-key map {name: arg}. A single name stands for a
// one-rule list. Paths are returned sorted, so errors and touch order are
// deterministic.
func parseRules(v any) ([]fieldRules, error) {
	mp, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("brick: rules: must map state paths to lists of rules (email: [required, email])")
	}
	out := make([]fieldRules, 0, len(mp))
	for _, path := range sortedKeys(mp) {
		raw := mp[path]
		list, ok := raw.([]any)
		if !ok {
			list = []any{raw}
		}
		fr := fieldRules{path: path}
		for _, item := range list {
			switch x := item.(type) {
			case string:
				fr.rules = append(fr.rules, Rule{Name: x})
			case map[string]any:
				if len(x) != 1 {
					return nil, fmt.Errorf("brick: rules: %s: a rule with an argument is a one-key map ({min: 2}), got %d keys", path, len(x))
				}
				for k, a := range x {
					fr.rules = append(fr.rules, Rule{Name: k, Arg: a})
				}
			default:
				return nil, fmt.Errorf("brick: rules: %s: a rule is a name or a one-key map ({min: 2}), not %T", path, item)
			}
		}
		if len(fr.rules) == 0 {
			return nil, fmt.Errorf("brick: rules: %s: the list is empty", path)
		}
		out = append(out, fr)
	}
	return out, nil
}

// parseMessages reads brick: {messages:}: "path.rule" → message.
func parseMessages(v any) (map[string]string, error) {
	mp, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf(`brick: messages: must map "path.rule" to a message (confirm.same: "The passwords do not match.")`)
	}
	out := make(map[string]string, len(mp))
	for k, m := range mp {
		s, ok := m.(string)
		if !ok {
			return nil, fmt.Errorf("brick: messages: %s: a message is text", k)
		}
		out[k] = s
	}
	return out, nil
}

// checkRules is the compile check of rules: and messages: (5.1): rules
// need state:, every path's first segment must be declared, a deeper
// segment must exist when the initial value at that point is a dict (a list
// may be empty at first, so indices into it are not checked), a * segment
// must stand where the declared value is a list, every rule must pass the
// Validator's Check, and every message must name a declared path and one of
// its rules ("todos.*.title.required" names the wildcard declaration). It
// returns the frontmatter line to report.
func checkRules(src []byte, m *meta, v Validator) (int, error) {
	if len(m.rules) == 0 {
		if len(m.messages) > 0 {
			return lineOf(src, "", brickMessages), fmt.Errorf("brick: messages: needs rules: — a message overrides a declared rule")
		}
		return 0, nil
	}
	if m.state == nil {
		return lineOf(src, "", brickRules), fmt.Errorf("brick: rules: needs state: — rules check a stateful component's fields")
	}
	for _, fr := range m.rules {
		line := lineOf(src, brickRules, fr.path)
		if err := checkStatePath(m.state, fr.path); err != nil {
			return line, fmt.Errorf("brick: rules: %s: %w", fr.path, err)
		}
		for _, r := range fr.rules {
			if err := v.Check(fr.path, r); err != nil {
				return line, fmt.Errorf("brick: rules: %s: %w", fr.path, err)
			}
		}
	}
	for _, k := range sortedKeys(m.messages) {
		i := strings.LastIndex(k, ".")
		ok := false
		if i > 0 {
			if fr := m.fieldRules(k[:i]); fr != nil {
				ok = slices.ContainsFunc(fr.rules, func(r Rule) bool { return r.Name == k[i+1:] })
			}
		}
		if !ok {
			return lineOf(src, brickMessages, k), fmt.Errorf(`brick: messages: %q names no declared rule (write "path.rule", e.g. "confirm.same")`, k)
		}
	}
	return 0, nil
}

// checkStatePath reports a rules: path that cannot exist in the state.
//
// A * segment (validate.Wildcard) covers every element of a list, so the
// declared value where it stands must be a list (or nil, filled later): a
// dict or a scalar there is an error, because * never ranges over dict keys
// and a scalar has no elements, and either way the rule would check
// nothing. Past a *, the rest of the path is checked against the list's
// first element when that element is a dict; an empty list is not checked
// further, since its items arrive at run time.
func checkStatePath(state map[string]any, path string) error {
	segs := strings.Split(path, ".")
	if slices.Contains(segs, "") {
		return fmt.Errorf("%q is not a field path", path)
	}
	if segs[0] == validate.Wildcard {
		return fmt.Errorf("%q cannot start with *: the state's top level is named fields", path)
	}
	cur, ok := state[segs[0]]
	if !ok {
		return fmt.Errorf("%q is not declared in the state", segs[0])
	}
	for i, seg := range segs[1:] {
		at := strings.Join(segs[:i+1], ".")
		if seg == validate.Wildcard {
			switch x := cur.(type) {
			case nil:
				return nil
			case []any:
				if len(x) == 0 {
					return nil
				}
				cur = x[0]
				continue
			case map[string]any:
				return fmt.Errorf("%s is a dict; * matches list elements only, so name the key", at)
			default:
				return fmt.Errorf("%s is a %T, not a list; * matches list elements only", at, cur)
			}
		}
		d, isDict := cur.(map[string]any)
		if !isDict {
			// A list may start empty; its indices are checked at run time.
			return nil
		}
		if cur, ok = d[seg]; !ok {
			return fmt.Errorf("%s has no key %q in the declared state", at, seg)
		}
	}
	return nil
}

// lineOf is the line of the first YAML key named key at or after the line
// of the key after (none when after is ""), or 1: a frontmatter error
// points at the key that is wrong when it can be found.
func lineOf(src []byte, after, key string) int {
	lines := strings.Split(string(src), "\n")
	start := 0
	if after != "" {
		start = lineOf(src, "", after) - 1
	}
	for i := start; i < len(lines); i++ {
		t := strings.TrimSpace(lines[i])
		for _, form := range []string{key + ":", `"` + key + `":`, `'` + key + `':`} {
			if strings.HasPrefix(t, form) {
				return i + 1
			}
		}
	}
	return 1
}

// fieldRules returns the declaration written exactly as path (todos.*.title
// for a wildcard one), or nil. Messages and brick.validate's typo check name
// declarations; a concrete field path is matched with rulesFor.
func (m *meta) fieldRules(path string) *fieldRules {
	for i := range m.rules {
		if m.rules[i].path == path {
			return &m.rules[i]
		}
	}
	return nil
}

// rulesFor returns every declaration that covers a concrete field path:
// the one written exactly as path first, then the wildcard ones that match
// it (todos.*.title for todos.2.title) in their sorted order. All of them
// apply, so a list item can carry both a shared rule and one of its own,
// and the first failing rule's message wins.
func (m *meta) rulesFor(path string) []*fieldRules {
	var out []*fieldRules
	if fr := m.fieldRules(path); fr != nil {
		out = append(out, fr)
	}
	for i := range m.rules {
		if fr := &m.rules[i]; fr.path != path && validate.Match(fr.path, path) {
			out = append(out, fr)
		}
	}
	return out
}

// isWildcard reports whether a declared path has a * segment.
func isWildcard(path string) bool {
	return slices.Contains(strings.Split(path, "."), validate.Wildcard)
}

// ruleErrors runs the Validator over concrete paths against state, one
// rule at a time so a messages: override (keyed by the declaration's path,
// "todos.*.title.required") can replace the message of the exact rule that
// failed. Errors are keyed by the concrete path (todos.2.title), which is
// what the template's .Errors and the client's error display look up.
//
// A path covered only by wildcard declarations whose list item no longer
// exists (touched, then removed) is skipped: it names nothing, so it has
// no error. A Validator that panics fails the render like a template
// panic, never the process.
func ruleErrors(ctx context.Context, v Validator, m *meta, state map[string]any, paths []string, locale string) (out map[string]string, err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("brick: Validator panicked: %v", p)
		}
	}()
	out = map[string]string{}
	for _, path := range paths {
		frs := m.rulesFor(path)
		if len(frs) == 0 {
			continue
		}
		if m.fieldRules(path) == nil && !validate.Exists(state, path) {
			continue
		}
		value := validate.Lookup(state, path)
	rules:
		for _, fr := range frs {
			for _, r := range fr.rules {
				msg := v.Validate(ctx, path, value, []Rule{r}, state, locale)
				if msg == "" {
					continue
				}
				if o, ok := m.messages[fr.path+"."+r.Name]; ok {
					msg = o
				}
				out[path] = msg
				break rules
			}
		}
	}
	return out, nil
}

// touch adds paths to an instance's touched set, kept sorted so the
// encoded instance is deterministic.
func touch(inst *Instance, paths ...string) {
	for _, p := range paths {
		if i, found := slices.BinarySearch(inst.Touched, p); !found {
			inst.Touched = slices.Insert(inst.Touched, i, p)
		}
	}
}

// untouch removes paths from the touched set; none means all of them.
func untouch(inst *Instance, paths ...string) {
	if len(paths) == 0 {
		inst.Touched = nil
		return
	}
	inst.Touched = slices.DeleteFunc(inst.Touched, func(p string) bool { return slices.Contains(paths, p) })
	if len(inst.Touched) == 0 {
		inst.Touched = nil
	}
}

// renderErrors is what a stateful component's template sees as .Errors
// and .Touched (5.3): rule errors for touched paths with rules, overlaid by
// the errors the action returned this render, which win. A component
// without rules: computes nothing.
func (st *renderState) renderErrors(m *meta, inst *Instance, actionErrs map[string]any) (map[string]any, map[string]bool, error) {
	errs := map[string]any{}
	touched := make(map[string]bool, len(inst.Touched))
	for _, p := range inst.Touched {
		touched[p] = true
	}
	if len(m.rules) > 0 && len(inst.Touched) > 0 {
		re, err := ruleErrors(st.ctx, st.e.opts.Validator, m, inst.State, inst.Touched, st.locale)
		if err != nil {
			return nil, nil, err
		}
		for k, v := range re {
			errs[k] = v
		}
	}
	for k, v := range actionErrs {
		errs[k] = v
	}
	return errs, touched, nil
}

// Validation is the declared rules of the component an action runs on,
// for script bindings (brick.validate, brick.untouch in Starlark). The
// engine puts one in the action's context; ValidationFrom finds it. It
// validates the state as it was when the action started (after deferred
// values were applied), which is what the user submitted.
type Validation struct {
	mu     sync.Mutex
	v      Validator
	m      *meta
	inst   *Instance
	state  map[string]any
	locale string
}

type validationKey struct{}

// ValidationFrom returns the action's Validation, or nil outside an action
// — including while a component renders: drawing a page never touches a
// field.
func ValidationFrom(ctx context.Context) *Validation {
	v, _ := ctx.Value(validationKey{}).(*Validation)
	return v
}

func withValidation(ctx context.Context, v *Validation) context.Context {
	return context.WithValue(ctx, validationKey{}, v)
}

// paths resolves a field list to concrete paths: none means every
// declaration; a wildcard declaration (todos.*.title) expands against the
// submitted state to one path per list item, so an empty list adds none.
// A field may name a declaration or a concrete path one covers
// (todos.2.title); anything else is an error, so a typo in
// brick.validate(["emial"]) fails the action instead of passing silently.
func (v *Validation) paths(fields []string) ([]string, error) {
	if len(fields) == 0 {
		for _, fr := range v.m.rules {
			fields = append(fields, fr.path)
		}
	} else {
		for _, f := range fields {
			if v.m.fieldRules(f) == nil && len(v.m.rulesFor(f)) == 0 {
				return nil, fmt.Errorf("%q has no rules in brick: {rules:}", f)
			}
		}
	}
	var out []string
	for _, f := range fields {
		for _, p := range validate.Expand(v.state, f) {
			if !slices.Contains(out, p) {
				out = append(out, p)
			}
		}
	}
	return out, nil
}

// Validate checks fields (all paths with rules when none are named),
// touches them so the re-render shows their errors, and returns path →
// message for the ones that fail.
func (v *Validation) Validate(ctx context.Context, fields ...string) (map[string]string, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	paths, err := v.paths(fields)
	if err != nil {
		return nil, err
	}
	touch(v.inst, paths...)
	return ruleErrors(ctx, v.v, v.m, v.state, paths, v.locale)
}

// Untouch forgets fields (all when none are named; a path that was never
// touched is ignored), so a form reset after
// a save does not light up "required" on its now-empty fields. A wildcard
// field (todos.*.title) forgets every touched path it covers.
func (v *Validation) Untouch(fields ...string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if len(fields) == 0 {
		untouch(v.inst)
		return
	}
	var paths []string
	for _, f := range fields {
		if !isWildcard(f) {
			paths = append(paths, f)
			continue
		}
		for _, p := range v.inst.Touched {
			if validate.Match(f, p) {
				paths = append(paths, p)
			}
		}
	}
	if len(paths) > 0 {
		untouch(v.inst, paths...)
	}
}
