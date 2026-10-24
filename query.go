package brick

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net/url"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// EffectQuery is the engine-generated effect that writes queried state
// fields into the URL: Data is {set: {param: value |
// [values]}, del: [param…]} and Replace says replaceState over pushState.
// The runtime merges it into location.search, leaving parameters it does
// not own untouched.
const EffectQuery = "query"

// actionParams is the $params built-in: $refresh for a URL
// that changed a component's query: fields without the component asking
// (a patch link, brick.navigate, Back/Forward), plus its brick: {params:}
// hook. The runtime sends it with the new values as deferred sets.
const actionParams = "$params"

// History modes of a queried field: replace (the default) keeps one entry
// per page; push lets Back undo a change.
const (
	queryReplace = "replace"
	queryPush    = "push"
)

// Keys of a brick: {query: {field: {…}}} entry.
const (
	queryKeyAs      = "as"
	queryKeyHistory = "history"
	queryKeyKeep    = "keep"
)

// queryParamRE is what a URL parameter name may be: plain enough to need
// no escaping in a query string or an attribute.
var queryParamRE = regexp.MustCompile(`^[A-Za-z_][\w.-]{0,63}$`)

// queryParam is one brick: {query:} entry: the state field, the URL
// parameter it is written as, its history mode, and whether it stays in
// the URL when it equals the declared value.
type queryParam struct {
	Field string
	Param string
	Push  bool
	Keep  bool
}

// parseQuery reads brick: {query:}: a list of field names, or a map of
// field → {as, history, keep} (an empty map or null for the defaults).
func parseQuery(v any) ([]queryParam, error) {
	bad := errors.New("brick: query: must list state fields ([page, q]) or map them to {as:, history:, keep:}")
	var out []queryParam
	switch x := v.(type) {
	case []any:
		for _, f := range x {
			s, ok := f.(string)
			if !ok || s == "" {
				return nil, bad
			}
			out = append(out, queryParam{Field: s, Param: s})
		}
	case map[string]any:
		for _, field := range sortedKeys(x) {
			q := queryParam{Field: field, Param: field}
			if x[field] != nil {
				opts, ok := x[field].(map[string]any)
				if !ok {
					return nil, fmt.Errorf("brick: query: %s: must be a map of as:, history:, keep: (or {})", field)
				}
				for _, k := range sortedKeys(opts) {
					switch k {
					case queryKeyAs:
						s, ok := opts[k].(string)
						if !ok || s == "" {
							return nil, fmt.Errorf("brick: query: %s: as: must be a parameter name", field)
						}
						q.Param = s
					case queryKeyHistory:
						switch opts[k] {
						case queryReplace:
						case queryPush:
							q.Push = true
						default:
							return nil, fmt.Errorf("brick: query: %s: history: must be %s or %s", field, queryReplace, queryPush)
						}
					case queryKeyKeep:
						b, ok := opts[k].(bool)
						if !ok {
							return nil, fmt.Errorf("brick: query: %s: keep: must be true or false", field)
						}
						q.Keep = b
					default:
						return nil, fmt.Errorf("brick: query: %s: unknown key %q (as, history, keep)", field, k)
					}
				}
			}
			out = append(out, q)
		}
	default:
		return nil, bad
	}
	return out, nil
}

// checkQuery is the compile check of brick: {query:} against state:: each
// field is declared at the top level and holds a scalar or a list of
// scalars (a URL parameter cannot carry a dict), and no two fields share a
// parameter.
func checkQuery(src []byte, m *meta) (int, error) {
	if m.paramsHook != "" && len(m.query) == 0 {
		// The hook runs when the URL sets queried fields; without any it
		// would never run, which is a typo, not a choice.
		return brickLine(src, brickParams), errors.New("brick: params: needs query: — it runs when the URL sets the component's queried fields")
	}
	if len(m.query) == 0 {
		return 0, nil
	}
	if m.state == nil {
		return brickLine(src, brickQuery), errors.New("brick: query: needs state: — a URL parameter is synced with a state field")
	}
	params := map[string]string{}
	for _, q := range m.query {
		line := lineOf(src, brickQuery, q.Field)
		decl, ok := m.state[q.Field]
		if !ok {
			return line, fmt.Errorf("brick: query: %s is not a top-level field of state: (declared: %s)", q.Field, strings.Join(sortedKeys(m.state), ", "))
		}
		if !queryable(decl) {
			return line, fmt.Errorf("brick: query: state.%s is %s; a URL parameter holds a scalar or a list of scalars, never a dict", q.Field, describe(decl))
		}
		if !queryParamRE.MatchString(q.Param) {
			return line, fmt.Errorf("brick: query: %s: %q is not a parameter name (letters, digits, _ . -)", q.Field, q.Param)
		}
		if other, dup := params[q.Param]; dup {
			return line, fmt.Errorf("brick: query: %s and %s both use the parameter %q", other, q.Field, q.Param)
		}
		params[q.Param] = q.Field
	}
	return 0, nil
}

// queryable reports a declared value a URL parameter can carry.
func queryable(v any) bool {
	if l, ok := v.([]any); ok {
		return !slices.ContainsFunc(l, func(x any) bool { return !isScalar(x) })
	}
	return isScalar(v)
}

// WithQuery passes the request's URL parameters to a render:
// a component with brick: {query:} seeds its queried fields from them when
// its instance is created. The site layer passes r.URL.Query() to every
// Fill; another host passes its own router's values. The values are
// untrusted: each goes through the $set gate, and one that does not fit is
// ignored, never failing the page.
func WithQuery(q url.Values) RenderOption {
	return func(o *renderOptions) { o.query = q }
}

// seedQuery applies the render's URL parameters to a new instance's
// queried fields, one at a time through the same gate as $set: a value of
// the wrong type, or a list where a scalar is declared, is ignored with a
// Dev warning, so a hand-edited link can never fail a page.
func (st *renderState) seedQuery(c *compiled, inst *Instance) {
	st.claimParams(c)
	for _, q := range c.meta.query {
		vals, ok := st.query[q.Param]
		if !ok || refuseLocked(c.meta, q.Field) != nil {
			// A locked field is never queried (checkLocked); the skip
			// keeps the URL out of it even so.
			continue
		}
		var value any
		if _, list := c.meta.state[q.Field].([]any); list {
			l := make([]any, len(vals))
			for i, v := range vals {
				l[i] = v
			}
			value = l
		} else {
			if len(vals) == 0 {
				continue
			}
			value = vals[0]
		}
		if err := applySets(inst, c.meta.state, []FieldSet{{Path: q.Field, Value: value}}); err != nil {
			if st.e.opts.Mode == Dev {
				st.e.warn(Diagnostic{File: c.path, Code: CodeRuntime, Message: fmt.Sprintf("<%s> ignored the URL parameter %s=%q: %v", inst.Name, q.Param, vals, err)})
			}
		}
	}
}

// claimParams warns in Dev when two components on one render claim the
// same URL parameter: both would write it and the last writer would win.
func (st *renderState) claimParams(c *compiled) {
	if st.e.opts.Mode != Dev || st.queryOwners == nil {
		return
	}
	for _, q := range c.meta.query {
		if owner, ok := st.queryOwners[q.Param]; ok && owner != c.path {
			st.e.warn(Diagnostic{File: c.path, Code: CodeRuntime, Message: fmt.Sprintf("the URL parameter %q is claimed by both %s and %s; give one of them brick: {query: {field: {as: other}}}", q.Param, displayPath(owner), displayPath(c.path))})
			continue
		}
		st.queryOwners[q.Param] = c.path
	}
}

// queryValues snapshots the queried fields of a state, for the
// before/after comparison that decides the query effect.
func queryValues(m *meta, state map[string]any) map[string]any {
	if len(m.query) == 0 {
		return nil
	}
	out := make(map[string]any, len(m.query))
	for _, q := range m.query {
		out[q.Field] = deepCopy(state[q.Field])
	}
	return out
}

// queryEffect builds the query effect for the queried fields that changed
// between before and the instance's state now, or reports none. A field
// equal to its declared value is deleted from the URL unless keep is set,
// so clean URLs stay clean; one push field among the changes makes the
// entry a push.
func queryEffect(m *meta, before map[string]any, state map[string]any) (Effect, bool) {
	set := map[string]any{}
	var del []any
	push := false
	for _, q := range m.query {
		now := state[q.Field]
		if reflect.DeepEqual(before[q.Field], now) {
			continue
		}
		push = push || q.Push
		if (!q.Keep && reflect.DeepEqual(now, m.state[q.Field])) || now == nil {
			del = append(del, q.Param)
			continue
		}
		set[q.Param] = paramValue(now)
	}
	if len(set) == 0 && len(del) == 0 {
		return Effect{}, false
	}
	data := map[string]any{"set": set}
	if len(del) > 0 {
		data["del"] = del
	}
	return Effect{Kind: EffectQuery, Data: data, Replace: !push}, true
}

// paramValue is a field's URL form: a string, or a list of strings for a
// list field (repeated parameters).
func paramValue(v any) any {
	if l, ok := v.([]any); ok {
		out := make([]any, len(l))
		for i, x := range l {
			out[i] = scalarString(x)
		}
		return out
	}
	return scalarString(v)
}

// scalarString writes a scalar the way the $set gate reads it back.
func scalarString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case bool:
		return strconv.FormatBool(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case int:
		return strconv.Itoa(x)
	case float64:
		return strconv.FormatFloat(x, 'g', -1, 64)
	case nil:
		return ""
	}
	return fmt.Sprint(v)
}

// queryAttr is the root's data-brick-query: parameter → [field, declared
// value], so Back and Forward can restore the fields from the URL in place
// (a missing parameter sends the declared value). Empty without query:.
func queryAttr(m *meta) (string, error) {
	if len(m.query) == 0 {
		return "", nil
	}
	out := make(map[string][2]any, len(m.query))
	for _, q := range m.query {
		out[q.Param] = [2]any{q.Field, m.state[q.Field]}
	}
	b, err := json.Marshal(out)
	if err != nil {
		return "", err
	}
	return ` data-brick-query="` + template.HTMLEscapeString(string(b)) + `"`, nil
}

// urlParams is the URL form of a component's queried fields, as the query
// effect writes them: parameter → string (or list of strings), and a field
// at its declared value is absent unless keep: is set. It is what the
// brick: {params:} hook receives, so the hook sees the address bar.
func urlParams(m *meta, state map[string]any) map[string]any {
	out := make(map[string]any, len(m.query))
	for _, q := range m.query {
		v := state[q.Field]
		if v == nil || (!q.Keep && reflect.DeepEqual(v, m.state[q.Field])) {
			continue
		}
		out[q.Param] = paramValue(v)
	}
	return out
}

// runParams calls brick: {params: fn}: fn(state, props, params) -> state
// | [state, errors]. The URL has
// already set the queried fields through the $set gate; the hook derives
// what depends on them (reset a cursor, clamp a page, load a selection).
// It runs when an instance is created (after the URL seeded it) and on
// $params, never after the component's own actions: those changed the
// fields themselves and the URL only follows.
func (e *Engine) runParams(ctx context.Context, c *compiled, inst *Instance) (map[string]any, error) {
	if c.meta.paramsHook == "" {
		return nil, nil
	}
	mod, fn := scriptTarget(c, c.meta.paramsHook)
	props, err := instanceProps(c, inst)
	if err != nil {
		return nil, err
	}
	out, err := mod.Call(ctx, fn, inst.State, props, urlParams(c.meta, inst.State))
	if err != nil {
		return nil, &Error{File: c.path, Component: inst.Name, Code: CodeScript, Err: fmt.Errorf("params %s: %w", c.meta.paramsHook, err)}
	}
	next, errs, ok := stateResult(out)
	if !ok {
		return nil, &Error{File: c.path, Component: inst.Name, Code: CodeScript, Err: fmt.Errorf("params %s returned %T; it returns the state as a dict, or [state, errors]", c.meta.paramsHook, out)}
	}
	inst.State = next
	return errs, nil
}
