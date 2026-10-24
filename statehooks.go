package brick

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
)

// actionReset is the $reset(path…) built-in: each path goes
// back to its value in the declared state: — never init's output (D9), so
// the target is predictable and nothing extra is stored.
const actionReset = "$reset"

// checkStateHooks is the compile check of brick: {init:, updated:}: both need state:, and each must name one of the file's functions, so
// a typo fails at compile and never as a page that silently skips its
// seeding.
func checkStateHooks(c *compiled, src []byte) (int, error) {
	for _, h := range []struct{ key, fn string }{{brickInit, c.meta.init}, {brickUpdated, c.meta.updated}, {brickParams, c.meta.paramsHook}} {
		if h.fn == "" {
			continue
		}
		line := brickLine(src, h.key)
		if c.meta.state == nil {
			return line, fmt.Errorf("brick: %s: needs state: — it runs on a stateful component's instance", h.key)
		}
		if err := checkFunction(c, h.fn); err != nil {
			return line, fmt.Errorf("brick: %s: %w", h.key, err)
		}
	}
	return 0, nil
}

// brickLine is the line of a brick: key, or of brick: itself when the map
// is written inline (brick: {init: setup}).
func brickLine(src []byte, key string) int {
	if l := lineOf(src, keyBrick, key); l > 1 {
		return l
	}
	return lineOf(src, "", keyBrick)
}

// scriptTarget splits a hook's "ns.fn" into its module and function; the
// compile check has already proved it exists.
func scriptTarget(c *compiled, name string) (ScriptModule, string) {
	ns, fn, qualified := strings.Cut(name, ".")
	if !qualified {
		ns, fn = "", name
	}
	return c.scripts[ns], fn
}

// seedInstance fills a new instance's first values in the order D14
// fixes: declared state: (already copied) → init → remembered → query. The
// URL wins over memory because a shared link must show what it says. It
// runs before the first Put, so the stored instance and the first render
// agree.
func (st *renderState) seedInstance(c *compiled, inst *Instance) error {
	if c.meta.init != "" {
		if err := st.runInit(c, inst); err != nil {
			return err
		}
	}
	if len(c.meta.remember) > 0 {
		st.e.loadRemembered(st.ctx, c, inst)
	}
	if len(c.meta.query) > 0 {
		st.seedQuery(c, inst)
		// params: runs once the URL has spoken, as handle_params follows
		// mount; like init it cannot dispatch, and errors it returns are
		// not shown before the visitor touched anything.
		if _, err := st.e.runParams(withEffects(st.ctx, nil), c, inst); err != nil {
			return err
		}
		st.e.migrateState(inst, c)
	}
	return nil
}

// runInit calls brick: {init: fn}: fn(state, props) -> state, once, when
// the instance is created. It runs while the page draws, so the effects
// collector is hidden (brick.dispatch fails); its result goes through the
// same shape reconciliation an action's state gets on its next load.
func (st *renderState) runInit(c *compiled, inst *Instance) error {
	mod, fn := scriptTarget(c, c.meta.init)
	props, err := instanceProps(c, inst)
	if err != nil {
		return err
	}
	out, err := mod.Call(withEffects(st.ctx, nil), fn, inst.State, props)
	if err != nil {
		return &Error{File: c.path, Component: inst.Name, Code: CodeScript, Err: fmt.Errorf("init %s: %w", c.meta.init, err)}
	}
	next, ok := out.(map[string]any)
	if !ok {
		return &Error{File: c.path, Component: inst.Name, Code: CodeScript, Err: fmt.Errorf("init %s returned %T; init returns the state as a dict", c.meta.init, out)}
	}
	inst.State = next
	st.e.migrateState(inst, c)
	return nil
}

// runUpdated calls brick: {updated: fn}: fn(state, props, paths) -> state
// | [state, errors], after a built-in ($set, sets, $toggle, $reset)
// changed fields, before validation and the re-render. paths are the
// field paths whose value changed; with none it does not run. It runs once
// per request: its own changes never trigger it again.
func (e *Engine) runUpdated(ctx context.Context, c *compiled, inst *Instance, paths []string) (map[string]any, error) {
	if c.meta.updated == "" || len(paths) == 0 {
		return nil, nil
	}
	mod, fn := scriptTarget(c, c.meta.updated)
	props, err := instanceProps(c, inst)
	if err != nil {
		return nil, err
	}
	list := make([]any, len(paths))
	for i, p := range paths {
		list[i] = p
	}
	out, err := mod.Call(ctx, fn, inst.State, props, list)
	if err != nil {
		return nil, &Error{File: c.path, Component: inst.Name, Code: CodeScript, Err: fmt.Errorf("updated %s: %w", c.meta.updated, err)}
	}
	next, errs, ok := stateResult(out)
	if !ok {
		return nil, &Error{File: c.path, Component: inst.Name, Code: CodeScript, Err: fmt.Errorf("updated %s returned %T; it returns the state as a dict, or [state, errors]", c.meta.updated, out)}
	}
	inst.State = next
	return errs, nil
}

// changedPaths returns the paths whose value differs between before and
// after, deduplicated, in the order given.
func changedPaths(before, after map[string]any, paths []string) []string {
	var out []string
	for _, p := range paths {
		if slices.Contains(out, p) {
			continue
		}
		if !reflect.DeepEqual(lookupPath(before, p), lookupPath(after, p)) {
			out = append(out, p)
		}
	}
	return out
}

// lookupPath reads a dotted path, nil when any step is missing.
func lookupPath(state map[string]any, path string) any {
	var cur any = state
	for _, seg := range strings.Split(path, ".") {
		next, err := child(cur, seg)
		if err != nil {
			return nil
		}
		cur = next
	}
	return cur
}

// resetFields applies $reset(path…): each path takes its value in the
// declared state: again and is untouched, all or nothing. A path must
// exist both in the declaration and in the current state; anything else
// is ErrBadValue, the same refusal a bad $set gets.
func resetFields(inst *Instance, declared map[string]any, args []any) ([]string, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("%w: %s takes one or more field paths", ErrUnknownAction, actionReset)
	}
	st, _ := deepCopy(inst.State).(map[string]any)
	if st == nil {
		st = map[string]any{}
	}
	paths := make([]string, 0, len(args))
	for _, a := range args {
		path, ok := a.(string)
		if !ok || path == "" {
			return nil, fmt.Errorf("%w: %s: a field path is a string", ErrUnknownAction, actionReset)
		}
		segs := strings.Split(path, ".")
		var decl any = declared
		for _, seg := range segs {
			next, err := child(decl, seg)
			if err != nil {
				return nil, fmt.Errorf("%w: %s(%q): not a declared field: %v", ErrBadValue, actionReset, path, err)
			}
			decl = next
		}
		if len(segs) == 1 {
			st[path] = deepCopy(decl)
		} else {
			var parent any = st
			for _, seg := range segs[:len(segs)-1] {
				next, err := child(parent, seg)
				if err != nil {
					return nil, fmt.Errorf("%w: %s(%q): %v", ErrBadValue, actionReset, path, err)
				}
				parent = next
			}
			last := segs[len(segs)-1]
			if _, err := child(parent, last); err != nil {
				return nil, fmt.Errorf("%w: %s(%q): %v", ErrBadValue, actionReset, path, err)
			}
			switch p := parent.(type) {
			case map[string]any:
				p[last] = deepCopy(decl)
			case []any:
				n, _ := strconv.Atoi(last) // child checked it
				p[n] = deepCopy(decl)
			}
		}
		paths = append(paths, path)
	}
	inst.State = st
	untouch(inst, paths...)
	return paths, nil
}
