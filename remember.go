package brick

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
)

// Rememberer keeps chosen state fields beyond an instance's life, per
// session. Values are the fields' JSON-safe values. Where they
// live is the host's choice — memory for development, Redis or SQL in
// contrib — so brick never grows a database of its own. It must be safe for
// concurrent use.
type Rememberer interface {
	// Load returns the fields saved under key; none saved is an empty map
	// and a nil error.
	Load(ctx context.Context, key RememberKey) (map[string]any, error)
	// Save replaces the fields saved under key.
	Save(ctx context.Context, key RememberKey, fields map[string]any) error
}

// RememberKey names one remembered set. Session is the hex SHA-256 of the
// session id (the engine never hands out the id itself), Path the
// component file. With the call site's key= set, Key carries it and
// PagePath is "", so a component with key="orders" remembers one layout on
// every page; without one, Key is "" and PagePath scopes it to the page.
type RememberKey struct{ Session, Path, Key, PagePath string }

// rememberKeyOf is the key an instance's remembered fields live under.
func rememberKeyOf(inst *Instance) RememberKey {
	if inst.Key != "" {
		return RememberKey{Session: inst.Session, Path: inst.Path, Key: inst.Key}
	}
	return RememberKey{Session: inst.Session, Path: inst.Path, PagePath: inst.PagePath}
}

// checkRemember is the compile check of brick: {remember:} (D22): it needs
// state:, a configured Rememberer, and declared top-level fields.
func checkRemember(src []byte, m *meta, have bool) (int, error) {
	if len(m.remember) == 0 {
		return 0, nil
	}
	line := brickLine(src, brickRemember)
	if m.state == nil {
		return line, errors.New("brick: remember: needs state: — it keeps state fields")
	}
	if !have {
		return line, errors.New("brick: remember: no Rememberer configured (Options.Remember); set one, such as brick.NewMemoryRemember() for development, so remembered fields are never silently forgotten")
	}
	for _, f := range m.remember {
		if _, ok := m.state[f]; !ok {
			return line, fmt.Errorf("brick: remember: %s is not a top-level field of state: (declared: %s)", f, strings.Join(sortedKeys(m.state), ", "))
		}
	}
	return 0, nil
}

// parseRemember reads brick: {remember: [field…]}.
func parseRemember(v any) ([]string, error) {
	list, ok := v.([]any)
	if !ok {
		return nil, errors.New("brick: remember: must list state fields ([view, sort])")
	}
	out := make([]string, 0, len(list))
	for _, x := range list {
		s, ok := x.(string)
		if !ok || s == "" {
			return nil, errors.New("brick: remember: must list state fields ([view, sort])")
		}
		out = append(out, s)
	}
	return out, nil
}

// loadRemembered applies the remembered fields to a new instance, each
// through the $set gate: a field whose type changed since it was saved
// keeps its declared value, as shape migration does. With no session
// nothing is loaded (a Dev warning once per component): remembering is
// per user, never shared by anonymous visitors. A Load error is reported
// and the instance starts from its declared state.
func (e *Engine) loadRemembered(ctx context.Context, c *compiled, inst *Instance) {
	if inst.Session == "" {
		e.warnNoSession(c)
		return
	}
	saved, err := e.opts.Remember.Load(ctx, rememberKeyOf(inst))
	if err != nil {
		e.reportError(ctx, fmt.Errorf("brick: <%s> remember: load: %w", inst.Name, err))
		return
	}
	for _, f := range c.meta.remember {
		v, ok := saved[f]
		if !ok {
			continue
		}
		if err := applySets(inst, c.meta.state, []FieldSet{{Path: f, Value: v}}); err != nil && e.opts.Mode == Dev {
			e.warn(Diagnostic{File: c.path, Code: CodeRuntime, Message: fmt.Sprintf("<%s> remembered %s no longer fits its declaration; it keeps the declared value: %v", inst.Name, f, err)})
		}
	}
}

// rememberValues snapshots the remembered fields of a state.
func rememberValues(m *meta, state map[string]any) map[string]any {
	if len(m.remember) == 0 {
		return nil
	}
	out := make(map[string]any, len(m.remember))
	for _, f := range m.remember {
		out[f] = deepCopy(state[f])
	}
	return out
}

// saveRemembered saves the remembered fields after a successful action
// that changed one of them. A Save error is reported to ErrorHooks and
// does not fail the action: the action already happened.
func (e *Engine) saveRemembered(ctx context.Context, c *compiled, inst *Instance, before map[string]any) {
	if len(c.meta.remember) == 0 {
		return
	}
	now := rememberValues(c.meta, inst.State)
	if reflect.DeepEqual(before, now) {
		return
	}
	if inst.Session == "" {
		e.warnNoSession(c)
		return
	}
	if err := e.opts.Remember.Save(context.WithoutCancel(ctx), rememberKeyOf(inst), now); err != nil {
		e.reportError(ctx, fmt.Errorf("brick: <%s> remember: save: %w", inst.Name, err))
	}
}

// warnNoSession is the Dev warning for a remember: component rendered
// without a session; Engine.warn shows it once.
func (e *Engine) warnNoSession(c *compiled) {
	if e.opts.Mode == Dev {
		e.warn(Diagnostic{File: c.path, Code: CodeRuntime, Message: "brick: remember: this render has no session (Options.Sessions or WithSession), so remembered fields are neither loaded nor saved"})
	}
}

// MemoryRemember is an in-process Rememberer for development and tests:
// unbounded and lost on restart. Production hosts keep remembered fields
// in their own store.
type MemoryRemember struct {
	mu   sync.Mutex
	sets map[RememberKey]map[string]any
}

// NewMemoryRemember returns an empty in-process Rememberer.
func NewMemoryRemember() *MemoryRemember {
	return &MemoryRemember{sets: map[RememberKey]map[string]any{}}
}

// Load returns a copy of the fields saved under key.
func (m *MemoryRemember) Load(_ context.Context, key RememberKey) (map[string]any, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out, _ := deepCopy(m.sets[key]).(map[string]any)
	if out == nil {
		out = map[string]any{}
	}
	return out, nil
}

// Save stores a copy of fields under key.
func (m *MemoryRemember) Save(_ context.Context, key RememberKey, fields map[string]any) error {
	cp, _ := deepCopy(fields).(map[string]any)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sets[key] = cp
	return nil
}
