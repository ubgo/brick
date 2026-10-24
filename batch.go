package brick

import (
	"fmt"
	"maps"
	"slices"

	"github.com/ubgo/brick/internal/transpile"
)

// Batch loaders.
//
// A component's loader runs when its tag executes, and html/template runs
// top-down, so `{{ range .Users }}<UserCard id=.ID>` would call it once per
// row: no single moment knows every input. A component whose loader is
// brick: {load_many:} therefore does not render where it is called. It
// reserves its instance-path segment there (so its id is the one it would
// have had), leaves a placeholder chunk, and records what it needs to
// render later. When the page — or the island — has finished executing,
// drain calls each batch loader once with every distinct input of that
// level, then renders the deferred calls in call order; calls they defer in
// turn form the next level. The placeholders are then replaced by what the
// calls wrote.
//
// This saves data calls, not template time: every instance still executes
// its template once, so it is not the template-speed optimisation the
// non-goals rule out. The page is still one template: only when
// a body runs moves, never how it compiles. Output is deterministic,
// because levels, batches and renders all follow call order.

// chunkPending is a deferred call's place in a builder. It never leaves the
// engine: drain replaces every one before the Result is built, and Bytes
// skips any left by a render that failed first.
const chunkPending ChunkKind = -1

// deferred is one batch-loaded call waiting for its level's batch.
type deferred struct {
	callee *compiled
	mod    ScriptModule
	fn     string
	input  map[string]any
	key    string
	// render repeats the component call; its input is memoised by then, so
	// it renders in place.
	render func() error
	// path is the instance path including the reserved segment; ctx,
	// chain, depth and stateDepth are the render stack at the call.
	path       []string
	ctx        []*ctxFrame
	chain      []string
	depth      int
	stateDepth int
	// chunks is what the call wrote, and kids are the calls it deferred,
	// in the order of their placeholders in chunks.
	chunks []Chunk
	kids   []*deferred
}

// callSeg is a call site's instance-path segment (C10).
func callSeg(file *compiled, call *transpile.Call) string {
	return fmt.Sprintf("%s:%d", file.hash, call.ID)
}

// enterCall enters a component call's segment, or the one reserved for it
// when the call is a deferred one resuming.
func (st *renderState) enterCall(seg string, key any) error {
	if st.reserved != "" {
		st.path = append(st.path, st.reserved)
		st.reserved = ""
		return nil
	}
	return st.enter(seg, key)
}

// canDefer says whether a call may wait for a batch. Not inside a boundary
// (its rollback and fallback act on output that exists now), not where the
// output is captured into a slot or fallback buffer, not for the root an
// island or action renders, and never for the resumed call itself. Such a
// call still gets its answer, from a one-element batch.
func (st *renderState) canDefer() bool {
	return !st.dry && st.liveOut != nil && st.out == st.liveOut && len(st.fallbacks) == 0 && st.slotDepth == 0 && st.nextID == "" && st.reserved == ""
}

// deferBatch defers a batch-loaded call whose input is not loaded yet. It
// reports false, and does nothing, when the call should render now.
func (st *renderState) deferBatch(file *compiled, call *transpile.Call, t target, callee *compiled, caller bodyScope, given []propValue, key any, kids []any) (bool, error) {
	if !st.canDefer() {
		return false, nil
	}
	mod, fn, err := loaderTarget(callee)
	if err != nil {
		return false, st.fail(&Error{File: callee.path, Code: CodeScript, Err: err})
	}
	input, memoKey, err := st.loaderInput(callee, fn, given)
	if err != nil {
		return false, err
	}
	if _, ok := st.memo[memoKey]; ok {
		return false, nil
	}
	if err := st.enter(callSeg(file, call), key); err != nil {
		return false, st.fail(&Error{File: file.path, Line: call.Pos.Line, Col: call.Pos.Col, Component: call.Name, Code: CodeRuntime, Err: err})
	}
	path := slices.Clone(st.path)
	st.leave()
	d := &deferred{
		callee: callee, mod: mod, fn: fn, input: input, key: memoKey,
		path: path, ctx: cloneFrames(st.ctxStack), chain: slices.Clone(st.chain), depth: st.depth, stateDepth: st.stateDepth,
	}
	d.render = func() error { return st.component(file, call, t, caller, given, key, kids) }
	// The resumed call counts itself against MaxComponents.
	st.count--
	st.out.flush()
	st.out.chunks = append(st.out.chunks, Chunk{Kind: chunkPending})
	st.pending = append(st.pending, d)
	return true, nil
}

// cloneFrames copies the provide/inject stack, so a later {{ provide }} in
// the caller cannot change what a deferred call sees.
func cloneFrames(fs []*ctxFrame) []*ctxFrame {
	out := make([]*ctxFrame, len(fs))
	for i, f := range fs {
		out[i] = &ctxFrame{vals: maps.Clone(f.vals), fromMeta: maps.Clone(f.fromMeta), req: maps.Clone(f.req)}
	}
	return out
}

// drain renders every deferred call, level by level, and splices the
// output into the live builder.
func (st *renderState) drain() error {
	if len(st.pending) == 0 {
		return nil
	}
	root := st.out
	root.flush()
	top := st.pending
	level := top
	size := root.size
	for len(level) > 0 {
		st.pending = nil
		if err := st.batchLevel(level); err != nil {
			return err
		}
		for _, d := range level {
			var err error
			if size, err = st.resumeDeferred(d, root.limit, size); err != nil {
				return err
			}
		}
		level = st.pending
	}
	st.pending = nil
	root.chunks = expandPending(nil, root.chunks, top)
	root.size = size
	return nil
}

// batchLevel calls each batch loader of a level once, with its distinct
// unloaded inputs in first-call order.
func (st *renderState) batchLevel(level []*deferred) error {
	type batch struct {
		d      *deferred
		inputs []map[string]any
		keys   []string
	}
	var order []string
	groups := map[string]*batch{}
	seen := map[string]bool{}
	for _, d := range level {
		if _, ok := st.memo[d.key]; ok || seen[d.key] {
			continue
		}
		seen[d.key] = true
		g := d.callee.hash + "\x00" + d.fn
		b, ok := groups[g]
		if !ok {
			b = &batch{d: d}
			groups[g] = b
			order = append(order, g)
		}
		b.inputs = append(b.inputs, d.input)
		b.keys = append(b.keys, d.key)
	}
	for _, g := range order {
		b := groups[g]
		saved := st.chain
		st.chain = b.d.chain
		err := st.runBatch(b.d.callee, b.d.mod, b.d.fn, b.inputs, b.keys)
		st.chain = saved
		if err != nil {
			return err
		}
	}
	return nil
}

// resumeDeferred renders one deferred call into its own builder, with the render
// stack it was called with. size is the output written so far, so
// MaxOutputBytes still bounds the whole render.
func (st *renderState) resumeDeferred(d *deferred, limit, size int) (int, error) {
	if err := st.check(d.callee.path); err != nil {
		return size, err
	}
	path, ctx, chain, depth, stateDepth, out, live := st.path, st.ctxStack, st.chain, st.depth, st.stateDepth, st.out, st.liveOut
	defer func() {
		st.path, st.ctxStack, st.chain, st.depth, st.stateDepth, st.out, st.liveOut = path, ctx, chain, depth, stateDepth, out, live
		st.reserved = ""
	}()
	b := &builder{limit: limit, size: size}
	st.path = slices.Clone(d.path[:len(d.path)-1])
	st.reserved = d.path[len(d.path)-1]
	st.ctxStack, st.chain, st.depth, st.stateDepth = d.ctx, slices.Clone(d.chain), d.depth, d.stateDepth
	st.out, st.liveOut = b, b
	start := len(st.pending)
	if err := d.render(); err != nil {
		return size, err
	}
	d.chunks = b.result()
	d.kids = slices.Clone(st.pending[start:])
	return b.size, nil
}

// expandPending appends chunks to out with each placeholder replaced by its
// call's output, recursively. Adjacent byte chunks merge, so the result has
// the shape an unbatched render would have.
func expandPending(out, chunks []Chunk, calls []*deferred) []Chunk {
	i := 0
	for _, c := range chunks {
		if c.Kind == chunkPending {
			if i < len(calls) {
				out = expandPending(out, calls[i].chunks, calls[i].kids)
				i++
			}
			continue
		}
		if n := len(out); c.Kind == ChunkBytes && n > 0 && out[n-1].Kind == ChunkBytes {
			out[n-1].Bytes = append(out[n-1].Bytes, c.Bytes...)
			continue
		}
		if c.Kind == ChunkBytes {
			c.Bytes = slices.Clone(c.Bytes)
		}
		out = append(out, c)
	}
	return out
}
