package brick

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
)

// ErrLockedField means a client tried to write a field the component
// declared in brick: {locked:}. It wraps ErrBadValue, so it answers 422
// with a message naming the field, like every other refused client value;
// nothing was applied and the action did not run.
var ErrLockedField = fmt.Errorf("%w: locked field", ErrBadValue)

// parseLocked reads brick: {locked: [field…]}.
func parseLocked(v any) ([]string, error) {
	return parseFieldList(v, brickLocked)
}

// parseFieldList reads a brick: key that lists state fields. An empty list
// is kept non-nil, because writable: [] (nothing writable) differs from
// no writable: at all.
func parseFieldList(v any, key string) ([]string, error) {
	bad := fmt.Errorf("brick: %s: must list state fields ([editing_id])", key)
	list, ok := v.([]any)
	if !ok {
		return nil, bad
	}
	out := make([]string, 0, len(list))
	for _, x := range list {
		s, ok := x.(string)
		if !ok || s == "" {
			return nil, bad
		}
		out = append(out, s)
	}
	return out, nil
}

// checkLocked is the compile check of brick: {locked:}: it needs state:,
// and each entry is a declared top-level field. A locked field cannot also
// be an upload field or a queried one, because uploads and the URL are
// written by the client: the two settings contradict each other, so the
// file fails instead of one silently winning.
//
// It also resolves whether the component is closed (lockAll is
// Options.LockState): brick: {lock_state:} wins, then a writable: list
// (which closes the component by itself), then the engine default. The
// answer is fixed here, at compile, so the runtime gate reads one bool.
func checkLocked(src []byte, m *meta, lockAll bool) (int, error) {
	if line, err := checkClosed(src, m, lockAll); err != nil {
		return line, err
	}
	if len(m.locked) == 0 {
		return 0, nil
	}
	line := brickLine(src, brickLocked)
	if m.state == nil {
		return line, errors.New("brick: locked: needs state: — it locks state fields")
	}
	for _, f := range m.locked {
		if _, ok := m.state[f]; !ok {
			return line, fmt.Errorf("brick: locked: %s is not a top-level field of state: (declared: %s)", f, strings.Join(sortedKeys(m.state), ", "))
		}
		if _, ok := m.uploads[f]; ok {
			return line, fmt.Errorf("brick: locked: %s is an upload field, which the client writes by uploading; remove it from locked or from uploads", f)
		}
		for _, q := range m.query {
			if q.Field == f {
				return line, fmt.Errorf("brick: locked: %s is a query field, which the URL writes; remove it from locked or from query", f)
			}
		}
	}
	return 0, nil
}

// checkClosed checks brick: {lock_state:, writable:} and sets m.closed.
func checkClosed(src []byte, m *meta, lockAll bool) (int, error) {
	declared := m.lockState != nil || m.writable != nil
	if declared && m.state == nil {
		key := brickWritable
		if m.lockState != nil {
			key = brickLockState
		}
		return brickLine(src, key), fmt.Errorf("brick: %s: needs state: — it decides which state fields the client may write", key)
	}
	if m.lockState != nil && !*m.lockState && m.writable != nil {
		return brickLine(src, brickWritable), errors.New("brick: writable: means nothing with lock_state: false, where every field is client-writable; remove one of them")
	}
	switch {
	case m.state == nil:
		m.closed = false
	case m.lockState != nil:
		m.closed = *m.lockState
	case m.writable != nil:
		m.closed = true
	default:
		m.closed = lockAll
	}
	line := brickLine(src, brickWritable)
	for _, f := range m.writable {
		if _, ok := m.state[f]; !ok {
			return line, fmt.Errorf("brick: writable: %s is not a top-level field of state: (declared: %s)", f, strings.Join(sortedKeys(m.state), ", "))
		}
		if slices.Contains(m.locked, f) {
			return line, fmt.Errorf("brick: writable: %s is also in locked:; a field is either client-writable or not, so remove it from one", f)
		}
	}
	return 0, nil
}

// clientWritable says a closed component lets the client write field:
// it is listed in writable:, or the frontmatter already declares the
// client as its writer (an upload field, written by uploading; a query
// field, written by the URL). Without the second half a closed component
// would silently stop reading its URL.
func clientWritable(m *meta, field string) bool {
	if slices.Contains(m.writable, field) {
		return true
	}
	if _, ok := m.uploads[field]; ok {
		return true
	}
	return slices.ContainsFunc(m.query, func(q queryParam) bool { return q.Field == field })
}

// lockedRoot returns the field a state path falls under (its first
// segment), whether the client may not write it, and why, as a phrase
// for the error. Locking a field locks every path below it (locking draft
// locks draft.title), since a nested write changes the field as surely as
// a whole one. brick: {locked:} wins over everything; a closed component
// then refuses every field it does not let the client write.
func lockedRoot(m *meta, path string) (field, why string, locked bool) {
	first, _, _ := strings.Cut(path, ".")
	if slices.Contains(m.locked, first) {
		return first, "is locked (brick: {locked:})", true
	}
	if m.closed && !clientWritable(m, first) {
		return first, "is closed to the client (Options.LockState or brick: {lock_state:}; list it in brick: {writable:} to allow it)", true
	}
	return first, "", false
}

// lockActive says the component refuses any client write at all, so the
// gates skip the work for the common open component.
func lockActive(m *meta) bool {
	return len(m.locked) > 0 || m.closed
}

// refuseLocked is the runtime gate every client write passes through:
// deferred sets, $set, $toggle and $reset. Server writes (actions, init,
// updated:, remembered values the server saved) never call it.
func refuseLocked(m *meta, paths ...string) error {
	if !lockActive(m) {
		return nil
	}
	for _, p := range paths {
		if f, why, ok := lockedRoot(m, p); ok {
			return fmt.Errorf("%w: state.%s %s; only the server writes it", ErrLockedField, f, why)
		}
	}
	return nil
}

// lockedArgPaths are the paths a client built-in names in its arguments:
// the field of $set and $toggle, every path of $reset. Anything else names
// no state path.
func lockedArgPaths(action string, args []any) []string {
	switch action {
	case actionSet, actionToggle:
		if s, ok := firstString(args); ok {
			return []string{s}
		}
	case actionReset:
		var out []string
		for _, a := range args {
			if s, ok := a.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

// checkLockedAttr is the compile half of the lock: a brick:model bound to
// a locked field, or a literal $set/$toggle/$reset call naming one, fails
// with the file and line, because the server would refuse every such write
// at run time. A value with {{ }} is filled at render and left to the
// runtime gate.
func checkLockedAttr(a brickAttr, v string, m *meta) error {
	if !lockActive(m) {
		return nil
	}
	if a.name == attrModel {
		if f, why, ok := lockedRoot(m, v); ok {
			return fmt.Errorf("%s=%q: state.%s %s; a client binding cannot write it", a.fullName(), v, f, why)
		}
		return nil
	}
	mm := actionCallRE.FindStringSubmatch(v)
	if mm == nil || (mm[1] != actionSet && mm[1] != actionToggle && mm[1] != actionReset) {
		return nil
	}
	var args []any
	if s := strings.TrimSpace(mm[2]); s != "" {
		if err := json.Unmarshal([]byte("["+s+"]"), &args); err != nil {
			return nil // checkActionCall reports a malformed call
		}
	}
	for _, p := range lockedArgPaths(mm[1], args) {
		if f, why, ok := lockedRoot(m, p); ok {
			return fmt.Errorf("%s=%q: state.%s %s; %s from the client cannot write it", a.fullName(), v, f, why, mm[1])
		}
	}
	return nil
}
