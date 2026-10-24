// Package validate holds the rule types behind brick's declared validation
// and the default, stdlib-only Validator.
//
// It imports nothing from the root brick package (only the internal weburl leaf), so the root package can default to it
// without an import cycle, and a host can wrap or replace the default
// importing only this package. brick re-exports Rule and Validator under
// the same names.
package validate

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/mail"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/ubgo/brick/internal/weburl"
)

// Rule is one declared check: a name and its optional argument, as written
// in rules: (`required` is {Name: "required"}, `{min: 2}` is
// {Name: "min", Arg: 2}).
type Rule struct {
	Name string
	Arg  any
}

// Validator checks fields against declared rules. The engine calls Check
// once per rule at compile time, so an unknown name or a bad argument is a
// compile error with file and line, never a silent pass at run time; and
// Validate at every render of a component with touched fields, and from
// brick.validate in an action.
type Validator interface {
	// Check reports why r cannot be used on path, or nil.
	Check(path string, r Rule) error
	// Validate returns the message for the first failing rule of one
	// field, "" when it passes. state is the whole component state, for
	// cross-field rules such as same; locale is the render's locale, for a
	// Validator that localises its messages.
	Validate(ctx context.Context, path string, value any, rules []Rule, state map[string]any, locale string) string
}

// The default rule names. A host Validator may accept others.
const (
	Required = "required"
	Min      = "min"
	Max      = "max"
	Email    = "email"
	URL      = "url"
	Pattern  = "pattern"
	In       = "in"
	Same     = "same"
)

// names is every rule the default knows, for the "unknown rule" message.
var names = []string{Required, Min, Max, Email, URL, Pattern, In, Same}

// Default returns the stdlib-only Validator: required, min, max (length
// for strings, count for lists, value for numbers), email, url, pattern (a
// Go regexp matched against the whole value), in (a list), same (another
// state path). Messages are English; a host localises by wrapping it or by
// its own Validator.
//
// Every rule but required and same passes an empty value (nil, "", an empty
// list): a field that may be left blank says so by not being required.
func Default() Validator { return &stdValidator{} }

type stdValidator struct {
	// patterns caches compiled pattern: args, filled by Check so Validate
	// never compiles on a keystroke.
	patterns sync.Map // string → *regexp.Regexp
}

func (v *stdValidator) Check(path string, r Rule) error {
	switch r.Name {
	case Required, Email, URL:
		if r.Arg != nil {
			return fmt.Errorf("%s takes no argument; write it as a bare name", r.Name)
		}
	case Min, Max:
		n, ok := number(r.Arg)
		if !ok || n < 0 || math.IsNaN(n) || math.IsInf(n, 0) {
			return fmt.Errorf("%s takes a number that is not negative ({%s: 2})", r.Name, r.Name)
		}
	case Pattern:
		s, ok := r.Arg.(string)
		if !ok || s == "" {
			return errors.New(`pattern takes a Go regular expression ({pattern: "^[a-z]+$"})`)
		}
		if _, err := v.regexp(s); err != nil {
			return fmt.Errorf("pattern: %v", err)
		}
	case In:
		list, ok := r.Arg.([]any)
		if !ok || len(list) == 0 {
			return errors.New("in takes a list of allowed values ({in: [a, b]})")
		}
	case Same:
		s, ok := r.Arg.(string)
		if !ok || s == "" {
			return errors.New("same takes another field's path ({same: password})")
		}
		if s == path {
			return errors.New("same names the field itself")
		}
	default:
		return fmt.Errorf("unknown rule %q (known: %s)", r.Name, strings.Join(names, ", "))
	}
	return nil
}

func (v *stdValidator) Validate(_ context.Context, _ string, value any, rules []Rule, state map[string]any, _ string) string {
	for _, r := range rules {
		if msg := v.one(value, r, state); msg != "" {
			return msg
		}
	}
	return ""
}

// one runs a single rule; "" means it passes.
func (v *stdValidator) one(value any, r Rule, state map[string]any) string {
	switch r.Name {
	case Required:
		if empty(value) {
			return "This field is required."
		}
		return ""
	case Same:
		if fmt.Sprint(value) != fmt.Sprint(Lookup(state, r.Arg.(string))) {
			return "Must match " + r.Arg.(string) + "."
		}
		return ""
	}
	if empty(value) {
		return ""
	}
	switch r.Name {
	case Min, Max:
		limit, _ := number(r.Arg)
		size, unit, ok := measure(value)
		if !ok {
			return ""
		}
		if r.Name == Min && size < limit {
			return fmt.Sprintf("Must be at least %s%s.", fmtNum(limit), unit)
		}
		if r.Name == Max && size > limit {
			return fmt.Sprintf("Must be at most %s%s.", fmtNum(limit), unit)
		}
	case Email:
		s := fmt.Sprint(value)
		a, err := mail.ParseAddress(s)
		if err != nil || a.Address != s || !strings.Contains(s[strings.LastIndex(s, "@")+1:], ".") {
			return "Enter a valid email address."
		}
	case URL:
		u, err := url.Parse(fmt.Sprint(value))
		if err != nil || !weburl.Is(u) {
			return "Enter a valid URL."
		}
	case Pattern:
		re, err := v.regexp(r.Arg.(string))
		if err != nil || !re.MatchString(fmt.Sprint(value)) {
			return "The format is not valid."
		}
	case In:
		allowed := r.Arg.([]any)
		vals := []any{value}
		if list, ok := value.([]any); ok {
			vals = list
		}
		for _, x := range vals {
			if !slices.ContainsFunc(allowed, func(a any) bool { return fmt.Sprint(a) == fmt.Sprint(x) }) {
				return "Choose one of the listed values."
			}
		}
	}
	return ""
}

// regexp compiles a pattern anchored to the whole value, once.
func (v *stdValidator) regexp(p string) (*regexp.Regexp, error) {
	if re, ok := v.patterns.Load(p); ok {
		return re.(*regexp.Regexp), nil
	}
	re, err := regexp.Compile(`^(?:` + p + `)$`)
	if err != nil {
		return nil, err
	}
	v.patterns.Store(p, re)
	return re, nil
}

// empty is "nothing was entered": nil, a blank string, an empty list or
// dict. false and 0 are values.
func empty(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(x) == ""
	case []any:
		return len(x) == 0
	case map[string]any:
		return len(x) == 0
	}
	return false
}

// measure is what min and max compare: a string's length in characters, a
// list's count, a number's value, with the unit the message names. ok is
// false for anything else (a bool), which min and max pass.
func measure(v any) (size float64, unit string, ok bool) {
	switch x := v.(type) {
	case string:
		return float64(utf8.RuneCountInString(x)), " characters", true
	case []any:
		return float64(len(x)), " items", true
	}
	if n, ok := number(v); ok {
		return n, "", true
	}
	return 0, "", false
}

// number reads the numeric kinds state and YAML produce.
func number(v any) (float64, bool) {
	switch x := v.(type) {
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case uint64:
		return float64(x), true
	case float64:
		return x, true
	}
	return 0, false
}

func fmtNum(f float64) string {
	if f == math.Trunc(f) {
		return fmt.Sprintf("%d", int64(f))
	}
	return fmt.Sprint(f)
}

// Lookup returns the value at a dotted path of state (a dict key, or a
// list index), nil when any step is missing. Validators use it for
// cross-field rules; the engine uses it to find each field's value.
func Lookup(state map[string]any, path string) any {
	var cur any = state
	for _, seg := range strings.Split(path, ".") {
		if cur = step(cur, seg); cur == nil {
			return nil
		}
	}
	return cur
}

// Wildcard is the rules: path segment that stands for every element of a
// list: todos.*.title declares rules for todos.0.title, todos.1.title and
// so on. It matches list indices only, never dict keys, so a declaration
// cannot silently fan out over a dict's arbitrary keys; a dict has named
// fields and rules name them.
const Wildcard = "*"

// Match reports whether a concrete path (todos.2.title) is one that the
// declared path pattern (todos.*.title) covers: the same number of
// segments, each equal, except that a Wildcard matches one list index (a
// run of ASCII digits). A pattern without a Wildcard matches only itself.
func Match(pattern, path string) bool {
	if pattern == path {
		return true
	}
	ps, cs := strings.Split(pattern, "."), strings.Split(path, ".")
	if len(ps) != len(cs) {
		return false
	}
	for i, p := range ps {
		if p == Wildcard {
			if !isIndex(cs[i]) {
				return false
			}
			continue
		}
		if p != cs[i] {
			return false
		}
	}
	return true
}

// Expand lists the concrete paths a pattern covers in state, in index
// order: todos.*.title over a two-item list is [todos.0.title,
// todos.1.title]. A value that is not a list where a Wildcard stands (nil,
// a scalar, a dict) has no elements, so that branch expands to nothing:
// there is nothing to check, exactly as with an empty list. A rule that
// wants the list itself present says so on the list's own path (todos:
// [required]). The path after the last Wildcard need not exist; Lookup
// gives nil for it, which is what required reports on. A pattern without a
// Wildcard expands to itself.
func Expand(state map[string]any, pattern string) []string {
	segs := strings.Split(pattern, ".")
	if !slices.Contains(segs, Wildcard) {
		return []string{pattern}
	}
	var out []string
	var walk func(cur any, i int, prefix []string)
	walk = func(cur any, i int, prefix []string) {
		if i == len(segs) {
			out = append(out, strings.Join(prefix, "."))
			return
		}
		seg := segs[i]
		if seg != Wildcard {
			next := prefix[:len(prefix):len(prefix)]
			walk(step(cur, seg), i+1, append(next, seg))
			return
		}
		list, ok := cur.([]any)
		if !ok {
			return
		}
		for n, item := range list {
			next := prefix[:len(prefix):len(prefix)]
			walk(item, i+1, append(next, strconv.Itoa(n)))
		}
	}
	walk(state, 0, nil)
	return out
}

// Exists reports whether every list index in path is inside its list, so
// the element the path names is still there. The engine uses it to skip a
// touched path whose item was removed after it was touched: todos.5.title
// after todos shrank to two items is not "required", it is gone. A missing
// dict key is not a missing element (the field is just empty), so only
// indices are checked.
func Exists(state map[string]any, path string) bool {
	var cur any = state
	for _, seg := range strings.Split(path, ".") {
		if list, ok := cur.([]any); ok {
			n, err := strconv.Atoi(seg)
			if err != nil || n < 0 || n >= len(list) {
				return false
			}
			cur = list[n]
			continue
		}
		cur = step(cur, seg)
	}
	return true
}

// step is one Lookup step: a dict key or a list index, nil when missing.
func step(cur any, seg string) any {
	switch x := cur.(type) {
	case map[string]any:
		return x[seg]
	case []any:
		n, err := strconv.Atoi(seg)
		if err != nil || n < 0 || n >= len(x) {
			return nil
		}
		return x[n]
	}
	return nil
}

// isIndex is a list index as it appears in a path: one or more ASCII
// digits.
func isIndex(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
