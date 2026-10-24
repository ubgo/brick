package brick

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
)

// keepChild decides what an action's re-render does with a stateful
// child the page already shows:
//
//   - keep: write a <brick-keep> placeholder; the runtime keeps the
//     element it has. A child without reactive props is always kept, at
//     no cost: its handle is never read, so a forged one changes nothing
//     but "do not render this", which the client could do by deleting DOM.
//   - adopted: a reactive prop changed, or the parent field bound to the
//     child's model (bind, modelable.go) no longer equals it, so the child
//     re-renders from its own verified instance (state kept, props and
//     model updated).
//   - neither: render fresh, as on a first visit — the child is new in
//     this render, is not the root's direct child, or its instance does
//     not verify (gone, another session, a wrong token, another
//     component's handle). A handle the browser sent is never adopted
//     without the checks an action makes.
func (st *renderState) keepChild(callee *compiled, id string, callSite map[string]any, bind *modelBind) (keep bool, adopted *Instance, err error) {
	ref, ok := st.keep[id]
	if !ok || st.stateDepth != 1 {
		return false, nil, nil
	}
	reactive := callee.meta.reactiveProps()
	if len(reactive) == 0 && bind == nil {
		return true, nil, nil
	}
	inst, err := st.loadChild(ref, callee.path, id)
	if err != nil || inst == nil {
		return false, nil, err
	}
	changed := bind.differs(inst)
	for _, name := range reactive {
		if !sameJSONValue(callSite[name], inst.Props[name]) {
			changed = true
			break
		}
	}
	if !changed {
		return true, nil, nil
	}
	// The child takes the props it is rendered with, so its own next
	// action renders with the same ones.
	inst.Props = callSite
	st.e.migrateState(inst, callee)
	bind.apply(inst)
	return false, inst, nil
}

// loadChild loads and verifies a kept child's instance exactly as an
// action does: the store the handle names, the live session, the token —
// and, beyond an action, that it is this component at this position, so a
// valid handle of another component on the page cannot be swapped in. nil
// means "does not verify"; an error is the store failing.
func (st *renderState) loadChild(ref ChildRef, path, id string) (*Instance, error) {
	name, handle, ok := strings.Cut(ref.Handle, ".")
	store := st.e.stores[name]
	if !ok || store == nil {
		return nil, nil
	}
	hash := hashSession(st.session)
	inst, ok, err := store.Get(WithStateScope(st.ctx, StateScope{Store: name, Session: hash}), handle)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	if !ok {
		return nil, nil
	}
	inst.Handle, inst.Store = handle, name
	if subtle.ConstantTimeCompare([]byte(inst.Session), []byte(hash)) != 1 ||
		subtle.ConstantTimeCompare([]byte(inst.Token), []byte(ref.Token)) != 1 ||
		inst.Path != path || inst.ID != id {
		return nil, nil
	}
	return inst, nil
}

// sameJSONValue compares two prop values on their JSON form, so a stored
// float64(2) (a sealed store's round trip) equals a call site's int 2.
func sameJSONValue(a, b any) bool {
	ja, err1 := json.Marshal(a)
	jb, err2 := json.Marshal(b)
	if err1 != nil || err2 != nil {
		return reflect.DeepEqual(a, b)
	}
	var va, vb any
	_ = json.Unmarshal(ja, &va)
	_ = json.Unmarshal(jb, &vb)
	return reflect.DeepEqual(va, vb)
}
