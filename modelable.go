package brick

import (
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"strings"
)

// Modelable: a stateful child declares one state field as its model with
// brick: {model: value}, and a stateful parent binds a field of its own to
// it with <Stepper brick:model="qty">.
//
// 6.5 deferred this because the same effect is a reactive prop plus a
// child event and a parent action, and two-way binding across components
// is where surprises live: two copies of one value, each
// side able to change it. So the binding here IS that composition, with
// one owner per direction and no new sync channel:
//
//   - down: the parent's field is the source of truth whenever the parent
//     renders the child. A fresh child starts from it (init sees it), and
//     a kept child whose model differs from it re-renders adopted with the
//     parent's value, exactly as a changed reactive prop does (keep.go).
//   - up: the child's root carries its model value (data-brick-model-value)
//     and the parent field it is bound to (data-brick-model). When one of
//     the child's own answers changes the value, the runtime sends the
//     parent a $set of that field: an ordinary client write, so a locked
//     or closed parent field refuses it as it refuses any brick:model.
//
// Neither side ever writes the other's state on the server, and a change
// travels one direction per round trip, so there is no cycle: the
// parent's $set re-renders it with the child's value, which keeps the
// child (the values agree).

// modelAttr is the call-site attribute that binds a parent field to a
// modelable child. It is the same name as an input's brick:model on
// purpose: on a presentational component it is still placed with
// {{ attrs }} (an <Input brick:model="name">), and only a stateful callee
// that declares brick: {model:} takes it as a binding.
const modelAttr = "brick:model"

// The child root attributes the runtime reads: the parent field the child
// is bound to (written by the call site, carried across the child's own
// re-renders by the runtime, like data-brick-up) and the child's current
// model value as JSON (written by the child on every render).
const (
	modelFieldAttr = "data-brick-model"
	modelValueAttr = "data-brick-model-value"
)

// modelBind is one live binding: the parent's state path and its value at
// this render, and the child's model field.
type modelBind struct {
	parent string
	field  string
	value  any
}

// parseModel reads brick: {model:}: one top-level state field name.
func parseModel(v any) (string, error) {
	s, ok := v.(string)
	if !ok || s == "" || strings.ContainsAny(s, ". \t\n") || !pathRE.MatchString(s) {
		return "", errors.New("brick: model: names one top-level state field (brick: {model: value})")
	}
	return s, nil
}

// checkModel is the parse-time check of brick: {model:}: it needs state:,
// and the field must be declared there, so a typo fails at compile and the
// parent's value always has a declared field to land in.
func checkModel(m *meta) error {
	if m.model == "" {
		return nil
	}
	if m.state == nil {
		return errors.New("brick: model: needs state: — the model is one of the component's state fields")
	}
	if _, ok := m.state[m.model]; !ok {
		return fmt.Errorf("brick: model: %q is not a field of state: (have: %s)", m.model, strings.Join(sortedKeys(m.state), ", "))
	}
	return nil
}

// takeModel finds a modelable binding at a call site and removes the
// attribute from the undeclared ones, so {{ attrs }} never places it on
// the child's markup (where the runtime would read it as an input
// binding). The binding is live only when the call renders inside its
// parent (live): a child re-rendering for its own action has the attribute
// in its stored props but no parent state to read, and keeps the parent
// field the runtime carried on its root instead.
func takeModel(callee *meta, undeclared []propValue, parent map[string]any, live bool) ([]propValue, *modelBind, error) {
	if callee == nil || callee.model == "" || callee.state == nil {
		return undeclared, nil, nil
	}
	var bind *modelBind
	out := undeclared[:0:0]
	for _, p := range undeclared {
		if p.name != modelAttr && !strings.HasPrefix(p.name, modelAttr+".") {
			out = append(out, p)
			continue
		}
		if p.name != modelAttr {
			return nil, nil, fmt.Errorf("%s on a modelable component takes no modifiers (the child decides when its model changes)", p.name)
		}
		path, ok := p.value.(string)
		if !ok || !pathRE.MatchString(path) {
			return nil, nil, fmt.Errorf("%s=%v: name one of the parent's state fields", modelAttr, p.value)
		}
		if live {
			bind = &modelBind{parent: path, field: callee.model, value: deepCopy(lookupPath(parent, path))}
		}
	}
	return out, bind, nil
}

// apply writes the parent's value into the child's model field.
func (b *modelBind) apply(inst *Instance) {
	if b == nil || inst == nil {
		return
	}
	if inst.State == nil {
		inst.State = map[string]any{}
	}
	inst.State[b.field] = deepCopy(b.value)
}

// differs reports whether the child's model field no longer holds the
// parent's value, which is when a kept child must re-render.
func (b *modelBind) differs(inst *Instance) bool {
	return b != nil && !sameJSONValue(inst.State[b.field], b.value)
}

// modelAttrs writes a stateful root's model attributes: the parent field
// when this render is bound, and the model value whenever the component
// declares one, so the runtime can tell when the child's own answer
// changed it. Both values are escaped; the result has its own leading
// space, like upAttrs.
func modelAttrs(s *Scope) (template.HTMLAttr, error) {
	if s.file == nil || s.file.meta.model == "" || s.State == nil {
		return "", nil
	}
	var b strings.Builder
	if s.inst != nil && s.inst.model != nil {
		b.WriteString(` ` + modelFieldAttr + `="` + template.HTMLEscapeString(s.inst.model.parent) + `"`)
	}
	j, err := json.Marshal(s.State[s.file.meta.model])
	if err != nil {
		return "", fmt.Errorf("brick: model: %s: %w", s.file.meta.model, err)
	}
	b.WriteString(` ` + modelValueAttr + `="` + template.HTMLEscapeString(string(j)) + `"`)
	return template.HTMLAttr(b.String()), nil
}
