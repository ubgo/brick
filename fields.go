package brick

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// ErrBadValue means a client-sent field value was refused: an undeclared
// field, a path that does not exist in the current state, a list index out
// of range, or a value that does not fit the field's type. Nothing was
// applied and the action did not run. 422.
var ErrBadValue = errors.New("brick: field value refused")

// FieldSet is one deferred field value riding along with an action
// : the dotted path of a state field and the value the
// browser holds. On the wire it is a two-element array, [path, value].
type FieldSet struct {
	Path  string
	Value any
}

// UnmarshalJSON reads [path, value]. Anything else is a bad request, never
// a silently dropped value.
func (f *FieldSet) UnmarshalJSON(b []byte) error {
	var pair []json.RawMessage
	if err := json.Unmarshal(b, &pair); err != nil || len(pair) != 2 {
		return errors.New("a set is [path, value]")
	}
	if err := json.Unmarshal(pair[0], &f.Path); err != nil {
		return errors.New("a set's path is a string")
	}
	dec := json.NewDecoder(strings.NewReader(string(pair[1])))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return err
	}
	f.Value = normalizeJSON(v)
	return nil
}

// MarshalJSON writes [path, value], the shape the runtime sends.
func (f FieldSet) MarshalJSON() ([]byte, error) {
	return json.Marshal([]any{f.Path, f.Value})
}

// applySets applies every set to the instance through setPath, all or
// nothing: they are applied to a copy, and the instance's state is replaced
// only when every one passes, so a refused set leaves it untouched.
func applySets(inst *Instance, declared map[string]any, sets []FieldSet) error {
	if len(sets) == 0 {
		return nil
	}
	st, _ := deepCopy(inst.State).(map[string]any)
	if st == nil {
		st = map[string]any{}
	}
	for _, s := range sets {
		if err := setPath(st, declared, s.Path, s.Value); err != nil {
			return fmt.Errorf("%w: %v", ErrBadValue, err)
		}
	}
	inst.State = st
	return nil
}

// setPath writes value at a dotted path of state.
// The client can change a value but never add a key, grow a list or change
// a type: the first segment must be declared in state:, every later one
// must already exist in the current state (a dict key, or a list index in
// range), and the value is coerced to the type at that path — the declared
// type for a top-level field, the current value's for a nested one. A dict,
// or a list of dicts, is never replaced wholesale: its parts are set by
// path, and rows are added by an action.
func setPath(state, declared map[string]any, path string, value any) error {
	segs := strings.Split(path, ".")
	for _, s := range segs {
		if s == "" {
			return fmt.Errorf("%q is not a field path", path)
		}
	}
	init, ok := declared[segs[0]]
	if !ok {
		return fmt.Errorf("%q is not a field of this component's state", segs[0])
	}
	if len(segs) == 1 {
		v, err := coerceField(value, init)
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		state[segs[0]] = v
		return nil
	}
	// Walk the current state down to the parent of the last segment.
	var parent any = state
	for i, seg := range segs[:len(segs)-1] {
		next, err := child(parent, seg)
		if err != nil {
			return fmt.Errorf("%s: %s: %w", path, strings.Join(segs[:i+1], "."), err)
		}
		parent = next
	}
	last := segs[len(segs)-1]
	cur, err := child(parent, last)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	v, err := coerceField(value, cur)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	switch p := parent.(type) {
	case map[string]any:
		p[last] = v
	case []any:
		n, _ := strconv.Atoi(last) // child checked it
		p[n] = v
	}
	return nil
}

// child is one step of a path: a key that exists in a dict, or an index in
// range of a list. The current value's kind decides, so "2024" is a key on
// a dict and an index on a list.
func child(v any, seg string) (any, error) {
	switch x := v.(type) {
	case map[string]any:
		c, ok := x[seg]
		if !ok {
			return nil, fmt.Errorf("no key %q (a client cannot add one)", seg)
		}
		return c, nil
	case []any:
		n, err := strconv.Atoi(seg)
		if err != nil || n < 0 || n >= len(x) || strconv.Itoa(n) != seg {
			return nil, fmt.Errorf("index %q is not in range 0..%d (a client cannot grow a list)", seg, len(x)-1)
		}
		return x[n], nil
	}
	return nil, fmt.Errorf("%s has no field %q", describe(v), seg)
}

// coerceField converts a client value to the shape of like (the declared
// or current value at the path). A scalar coerces as $set always has; nil
// (a field declared null) takes a scalar or null; a list of scalars takes
// a list, each element coerced to the list's element type (string when the
// list is empty), which is what a checkbox group or a multi-select sends.
func coerceField(value, like any) (any, error) {
	switch x := like.(type) {
	case map[string]any:
		return nil, errors.New("a dict is not replaced from the client; set its keys by path")
	case []any:
		elem := any("")
		if len(x) > 0 {
			elem = x[0]
		}
		if !isScalar(elem) {
			return nil, errors.New("a list of dicts or lists is not replaced from the client; set its elements by path, add rows with an action")
		}
		in, ok := value.([]any)
		if !ok {
			return nil, fmt.Errorf("%s is not a list", describe(value))
		}
		out := make([]any, len(in))
		for i, v := range in {
			cv, err := coerce(v, typeNameOf(elem))
			if err != nil || !isScalar(cv) {
				return nil, fmt.Errorf("element %d: %s is not a %s", i, describe(v), typeNameOf(elem))
			}
			out[i] = cv
		}
		return out, nil
	case nil:
		if !isScalar(value) {
			return nil, fmt.Errorf("%s: a field declared null takes a string, number, bool or null", describe(value))
		}
		return value, nil
	}
	return coerce(value, typeNameOf(like))
}

// isScalar reports a JSON scalar: string, number, bool or null.
func isScalar(v any) bool {
	switch v.(type) {
	case nil, string, bool, int, int64, float64:
		return true
	}
	return false
}

// arrayPaths lists the dotted paths of the state values that are lists,
// walking nested dicts but not into lists, sorted. The root carries them as
// data-brick-arrays so a checkbox bound to one
// binds as a list even when the group has a single option.
func arrayPaths(state map[string]any) []string {
	var out []string
	var walk func(prefix string, m map[string]any)
	walk = func(prefix string, m map[string]any) {
		for k, v := range m {
			// A key no path can name (a dot or a space in it) is skipped:
			// the list is space-separated and paths are dotted.
			if k == "" || strings.ContainsAny(k, ". \t\n") {
				continue
			}
			switch x := v.(type) {
			case []any:
				out = append(out, prefix+k)
			case map[string]any:
				walk(prefix+k+".", x)
			}
		}
	}
	walk("", state)
	sort.Strings(out)
	return out
}
