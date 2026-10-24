package brick

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/ubgo/brick/internal/transpile"
)

// Island is a component rendered per request. A page's cached
// Result holds one ChunkIsland per island instead of its output; Engine.Fill
// renders them for each request.
//
// Invariant: everything needed to render the island later is here, because
// the page's template is gone by then — which is why the caller's children
// and blocks travel as HTML rendered at page time (Slots), never as
// templates.
type Island struct {
	// Name is the tag as written, for errors; Path is the component file.
	Name string
	Path string
	// Props are the call site's props, evaluated at page render.
	Props map[string]any
	// Key is the call site's key=, if any.
	Key any
	// ID is the instance id the island had in the page (E5), kept so the
	// same element has the same id on every request.
	ID string
	// Ctx is the .Ctx visible at the island's place in the page; the
	// request's Provide values go on top of it.
	Ctx map[string]any
	// Fallback is the enclosing boundary's fallback, rendered at page time,
	// shown when the island fails; HasFallback says a boundary encloses it.
	Fallback    []Chunk
	HasFallback bool
	// Stateful says the island's component declares state:, so its filled
	// response holds a live handle and must not be shared (Result.HasState).
	Stateful bool
	// Slots are the call's children and <brick:prop> blocks, rendered with
	// the page's scope when the page rendered; nil when it
	// wrote none. A stateful island stores them in its instance.
	Slots map[string]string
	// Lazy is the island's brick:lazy mode (LazyLoad, LazyVisible,
	// LazyIdle, LazyStream), "" when it renders with the page; Loading is
	// the enclosing boundary's loading block, rendered at page time, which
	// the placeholder shows until the island arrives.
	Lazy    string
	Loading []Chunk
	// Session, PagePath, Locale, Route and RouteParts are set on an island
	// a ticket carries (IslandTickets): the hashed session it was issued
	// to, and the page values its later render needs, since the page's
	// Result is gone by then. Session is never a raw session id.
	Session    string
	PagePath   string
	Locale     string
	Route      map[string]string
	RouteParts []string
	// reqKeys are the Ctx keys whose values were request-scoped (Provide)
	// at page render; a stateful instance never stores them.
	reqKeys map[string]bool
}

// ErrIslands is returned by Result.Bytes for a result with islands, which
// only Engine.Fill can render.
var ErrIslands = errors.New("brick: this Result has islands; render it with Engine.Fill")

// captureIsland records an island in place of rendering it.
func (st *renderState) captureIsland(file *compiled, call *transpile.Call, callee *compiled, caller bodyScope, given []propValue, key any) error {
	isl, err := st.makeIsland(file, call, callee, caller, given, key)
	if err != nil {
		return err
	}
	st.out.island(isl)
	return nil
}

// makeIsland describes a call as an island: everything its later render
// needs, and the files and assets the page records for it now.
func (st *renderState) makeIsland(file *compiled, call *transpile.Call, callee *compiled, caller bodyScope, given []propValue, key any) (*Island, error) {
	props := make(map[string]any, len(given))
	for _, g := range given {
		props[g.name] = g.value
	}
	if err := st.enter(fmt.Sprintf("%s:%d", file.hash, call.ID), key); err != nil {
		return nil, st.fail(&Error{File: file.path, Line: call.Pos.Line, Col: call.Pos.Col, Component: call.Name, Code: CodeRuntime, Err: err})
	}
	id := st.instanceID()
	slots, err := st.renderSlots(file, call, caller)
	st.leave()
	if err != nil {
		return nil, err
	}
	isl := &Island{Name: call.Name, Path: callee.path, Props: props, Key: key, ID: id, Ctx: (&Scope{st: st}).Ctx(), Stateful: callee.meta.state != nil, Slots: slots, Lazy: lazyOf(call, callee.meta), reqKeys: st.requestKeys()}
	if n := len(st.fallbacks); n > 0 {
		chunks, err := st.fallbacks[n-1](nodeFallback, call.Name)
		if err != nil {
			return nil, err
		}
		isl.Fallback, isl.HasFallback = chunks, true
		if isl.Lazy != "" {
			// The placeholder is the boundary's loading block (D16).
			if isl.Loading, err = st.fallbacks[n-1](nodeLoading, call.Name); err != nil {
				return nil, err
			}
		}
	}
	// A lazy island needs the runtime to fetch it, even on a page with no
	// client code of its own.
	st.needRuntime = st.needRuntime || isl.Lazy != ""
	// The page depends on the island's files and ships its assets, even
	// though its output is rendered later (recordSubtree).
	st.recordType(canonicalName(call.Name))
	if err := st.recordSubtree(callee, map[string]bool{}); err != nil {
		return nil, err
	}
	return isl, nil
}

// recordSubtree records, at page time, the assets and files of a
// component that renders later (an island) and of every component its
// file can call, transitively. The cached page's {{ assets }} is written
// before any island renders, so without this an island's inner components
// — and one shown only after an action, such as an edit dialog inside
// {{ if }} — would reach the browser unstyled. Components chosen from data
// (<brick:render>) cannot be known here; Fill adds what they used.
func (st *renderState) recordSubtree(c *compiled, seen map[string]bool) error {
	if seen[c.path] {
		return nil
	}
	seen[c.path] = true
	st.files[c.path] = true
	for _, d := range c.deps {
		st.files[d] = true
	}
	if len(c.client) > 0 || len(c.styles) > 0 {
		st.assets[c.path] = true
	}
	// A listening root (brick: {listen:}) needs the runtime to open its
	// event stream, even when nothing on the page has a script of its own.
	// So does a stateful one: its brick:click, brick:submit and brick:model
	// are wired only by the runtime, and a counter whose component has no
	// client module would otherwise ship buttons that do nothing.
	if c.meta.listen != nil || c.meta.state != nil {
		st.needRuntime = true
	}
	for _, t := range c.targets {
		if t.kind != tComponent || seen[t.path] {
			continue
		}
		inner, err := st.e.load(st.ctx, t.path, kindComponent, st.locale)
		if err != nil {
			return err
		}
		if err := st.recordSubtree(inner, seen); err != nil {
			return err
		}
	}
	return nil
}

// Fill renders a Result's islands for one request and returns the page
// . opts are the request's options: Provide values (the
// signed-in user), WithLocale, WithNonce. A Result without islands is just
// Result.Bytes. In Dev mode a failing island is shown as an error block and
// the error is returned too; in Prod only the error is returned — unless a
// boundary encloses the island, whose fallback then takes its place.
func (e *Engine) Fill(ctx context.Context, r *Result, opts ...RenderOption) ([]byte, error) {
	var ro renderOptions
	for _, o := range opts {
		o(&ro)
	}
	if ro.err != nil {
		return nil, ro.err
	}
	if !r.hasIslands() {
		return r.Bytes(opts...)
	}
	out, _, firstErr, err := e.fill(ctx, r, &ro, nil)
	if err != nil {
		return nil, err
	}
	b, err := out.Bytes(opts...)
	if err != nil {
		return nil, err
	}
	return b, firstErr
}

// fill renders r's islands into a Result with none. stream, when set,
// takes each LazyStream island instead and returns the chunks of its
// placeholder, which FillStream writes in its place; Fill passes nil and
// renders every island in place. firstErr is a Dev-mode island failure
// shown in the page; err fails the whole response.
func (e *Engine) fill(ctx context.Context, r *Result, ro *renderOptions, stream func(*Island) []Chunk) (out *Result, deferred []*Island, firstErr, err error) {
	out = &Result{assets: r.assets}
	// One owner map per response, so two islands claiming one URL
	// parameter are seen together.
	ro.queryOwners = map[string]string{}
	for _, c := range r.Chunks {
		if c.Kind != ChunkIsland {
			out.Chunks = append(out.Chunks, c)
			continue
		}
		if stream != nil && c.Island.Lazy == LazyStream && !ro.staticState && !ro.lazyInline {
			deferred = append(deferred, c.Island)
			out.Chunks = append(out.Chunks, stream(c.Island)...)
			continue
		}
		var chunks []Chunk
		var used []string
		var needRuntime bool
		var err error
		if c.Island.ticketed() && !ro.staticState && !ro.lazyInline {
			// A static export has no endpoint to call: it renders in place.
			// A stream island renders in place here too: Fill is the
			// answer for a writer that cannot stream.
			chunks, err = e.lazyChunks(ctx, r.page, r.locale, c.Island, ro.session)
		} else {
			chunks, used, needRuntime, err = e.renderIsland(ctx, r, c.Island, *ro)
		}
		out.assets = e.mergeAssets(out.assets, used)
		if needRuntime {
			out.assets = e.withRuntime(out.assets)
		}
		switch {
		case err == nil:
			out.Chunks = append(out.Chunks, chunks...)
		case errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded):
			return nil, nil, nil, err
		case IsAnswer(err):
			// An island's loader answered for the whole response (signed
			// out → redirect, not allowed → 403): no fallback, no error
			// block, in Dev too; the host turns it into the response.
			return nil, nil, nil, err
		case c.Island.HasFallback:
			e.reportError(ctx, err)
			if e.opts.Mode == Dev {
				out.Chunks = append(out.Chunks, Chunk{Kind: ChunkBytes, Bytes: []byte(errorBlock(nil, err))})
			}
			out.Chunks = append(out.Chunks, c.Island.Fallback...)
		default:
			e.reportError(ctx, err)
			if e.opts.Mode != Dev {
				return nil, nil, nil, err
			}
			out.Chunks = append(out.Chunks, Chunk{Kind: ChunkBytes, Bytes: []byte(errorBlock(tailOf(out.Chunks), err))})
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return out, deferred, firstErr, nil
}

// renderIsland renders one island with the page's settings and the
// request's options.
func (e *Engine) renderIsland(ctx context.Context, r *Result, isl *Island, ro renderOptions) ([]Chunk, []string, bool, error) {
	return e.renderIslandWith(ctx, r, isl, ro, nil, nil, nil)
}

// mergeAssets adds the asset files of the components an island rendered
// that the page does not already link — components chosen from data at
// fill time, which recordSubtree could not see. The runtime comes first
// when any script is linked, as in a page's own list.
func (e *Engine) mergeAssets(have []asset, paths []string) []asset {
	linked := make(map[string]bool, len(have))
	for _, h := range have {
		linked[h.URL] = true
	}
	var add []asset
	script := false
	for _, p := range paths {
		for _, a := range e.assetsFor(p) {
			if !linked[a.URL] {
				linked[a.URL] = true
				add = append(add, a)
				script = script || !a.CSS
			}
		}
	}
	if len(add) == 0 {
		return have
	}
	out := append(slices.Clone(have), add...)
	if script {
		out = e.withRuntime(out)
	}
	return out
}

// withRuntime puts the runtime files a list lacks at its front, as one
// block in runtimeAssets' order (mount runtime, then navigation), exactly
// as a page that linked a script itself would list them. A list that
// already has them is returned as is.
func (e *Engine) withRuntime(have []asset) []asset {
	linked := make(map[string]bool, len(have))
	for _, h := range have {
		linked[h.URL] = true
	}
	var block []asset
	for _, rt := range e.runtimeAssets() {
		if !linked[rt.URL] {
			linked[rt.URL] = true
			block = append(block, rt)
		}
	}
	if len(block) == 0 {
		return have
	}
	return append(block, have...)
}

// renderIslandWith renders an island; resume, when set, is the stateful
// instance an action is re-rendering (its handle, token and new state),
// and keep the stateful children the page already shows inside it
// (ActionRequest.Children). It returns the chunks and the files of the components it rendered that
// carry assets, and whether the render wrote a stateful root (the page needs
// the runtime for it, even when no static walk found one).
func (e *Engine) renderIslandWith(ctx context.Context, r *Result, isl *Island, ro renderOptions, resume *Instance, formErrors map[string]any, keep map[string]ChildRef) ([]Chunk, []string, bool, error) {
	locale := r.locale
	if ro.locale != "" {
		if tag, ok := normalizeLocale(ro.locale); ok {
			locale = tag
		}
	}
	callee, err := e.load(ctx, isl.Path, kindComponent, locale)
	if err != nil {
		return nil, nil, false, err
	}
	st := &renderState{e: e, ctx: ctx, out: &builder{limit: e.opts.MaxOutputBytes}, locale: locale, page: r.page, tags: map[string]bool{}, assets: map[string]bool{}, memo: map[string]any{}, seen: map[string]int{}, keys: map[string]bool{}, files: map[string]bool{}, filling: true, lazyRoot: true, inAction: resume != nil, nextID: isl.ID, resume: resume, keep: keep, formErrors: formErrors, session: ro.session, staticState: ro.staticState, query: ro.query, queryOwners: ro.queryOwners, formResume: ro.formResume, lazyInline: ro.lazyInline, views: ro.views}
	st.liveOut = st.out
	if st.page == nil {
		st.page = &Page{Params: map[string]any{}}
	}
	// Two frames: the page's file values, then everything request-scoped
	// (the page's Provide values and this request's), so a stateful
	// instance stores the first and never the second.
	files, req := map[string]any{}, map[string]any{}
	for k, v := range isl.Ctx {
		if isl.reqKeys[k] {
			req[k] = v
		} else {
			files[k] = v
		}
	}
	maps.Copy(req, ro.provide)
	st.pushCtx(files)
	st.pushCtx(req)
	st.markRequest(req)
	// Cancellation always wins (E2), before any work.
	if err := st.check(isl.Path); err != nil {
		return nil, nil, false, err
	}
	// The root shows the slots stored when it was first rendered: the
	// page's, or the instance's on an action.
	st.rootSlots = slotRoot{slots: isl.Slots, set: true}
	if resume != nil {
		st.rootSlots.slots = resume.Slots
	}
	given := make([]propValue, 0, len(isl.Props))
	for _, k := range sortedKeys(isl.Props) {
		given = append(given, propValue{name: k, value: isl.Props[k]})
	}
	call := &transpile.Call{Name: isl.Name}
	t := target{kind: tComponent, name: canonicalName(isl.Name), path: isl.Path, callee: callee.meta}
	caller := bodyScope{Root: st.newScope(callee, map[string]any{}, map[string]any{})}
	err = st.component(callee, call, t, caller, given, isl.Key, nil)
	if err == nil {
		// The island's batch-loaded components, one load_many per level
		//.
		err = st.drain()
	}
	if err != nil {
		if st.failure != nil {
			return nil, nil, false, st.failure
		}
		return nil, nil, false, err
	}
	st.linkGlobals()
	if ro.stats != nil {
		ro.stats.merge(&st.stats, t.name)
	}
	out := st.out.result()
	if e.formsEnabled(ro) {
		// Forms post without the runtime too: the page's
		// first render and every action's re-render alike, so a morph
		// keeps the hidden fields current.
		out = rewriteForms(out)
	}
	return out, sortedKeys(st.assets), st.needRuntime, nil
}
