package brick

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ActionRequest is one action call from the browser.
type ActionRequest struct {
	Handle string `json:"handle"`
	Token  string `json:"token"`
	Action string `json:"action"`
	Args   []any  `json:"args"`
	// Children are the stateful components the page already shows directly
	// inside this one, by data-brick id. The
	// re-render writes a <brick-keep> placeholder for each instead of
	// rendering it, so its state survives; the runtime keeps the element it
	// has. Untrusted: an id the re-render does not produce is ignored, and a
	// handle is only ever loaded, with its token and session checked, for a
	// child with reactive props.
	Children map[string]ChildRef `json:"children,omitempty"`
	// Sets are deferred field values (brick:model.defer) that ride along
	// with this action: applied in order through the same
	// gate as $set, before the action runs, all or nothing. Untrusted: a
	// refused set fails the whole request with ErrBadValue.
	Sets []FieldSet `json:"sets,omitempty"`
}

// ChildRef is a kept child's handle and token as its root carries them.
type ChildRef struct {
	Handle string `json:"handle"`
	Token  string `json:"token"`
}

// Action errors, matched by the HTTP adapter to a status.
var (
	// ErrGone means the instance could not be loaded — expired, evicted,
	// sealed with a retired key, from another session, or its store or
	// component is gone. The action never ran, so the runtime recovers the
	// component in place and runs it again. 410.
	ErrGone = errors.New("brick: this component's state was not found; the page recovers it")
	// ErrStoreUnavailable means the state store failed (Redis down, a KMS
	// unreachable). It is never ErrGone: turning an outage into 410s would
	// make every open tab recover at once. 503 with Retry-After.
	ErrStoreUnavailable = errors.New("brick: the state store is unavailable")
	// ErrForbidden means the token did not match, or a middleware refused.
	// 403.
	ErrForbidden = errors.New("brick: action refused")
	// ErrUnknownAction means the name is not one of the component's
	// actions, or a built-in's arguments are wrong. 400.
	ErrUnknownAction = errors.New("brick: unknown action")
)

// ActionError is a refusal with its own HTTP status — 429 with RetryAfter
// for a rate limit. Message is what the client sees; Err, when set, is
// reported to ErrorHooks.
type ActionError struct {
	Status     int
	Message    string
	RetryAfter time.Duration
	Err        error
}

// Error returns the message the browser is shown: Message when set,
// otherwise the standard text of Status.
func (a *ActionError) Error() string {
	if a.Message != "" {
		return a.Message
	}
	if a.Err != nil {
		return a.Err.Error()
	}
	return http.StatusText(a.Status)
}

// Unwrap exposes the cause, so errors.Is and errors.As see through the
// status mapping to the error the action returned.
func (a *ActionError) Unwrap() error { return a.Err }

// ActionCall is what the action pipeline sees about one action. The
// engine resolves the store, loads the instance and checks its token and
// session before the chain runs, so every middleware sees a real,
// authorised component; a lost or forged handle never reaches it.
type ActionCall struct {
	// Request is the live request; nil when Engine.Action is called
	// directly.
	Request *http.Request
	// Component is the tag name, Path the component file, Store the name
	// of the store holding it.
	Component string
	Path      string
	Store     string
	// Action is the called name ("save", "$set", "$refresh", "$toggle")
	// and Args its arguments.
	Action string
	Args   []any
	// Session is the live session id (Options.Sessions or WithSession),
	// "" when none.
	Session string
	// State is a copy of the state before the action; changing it changes
	// nothing.
	State map[string]any
	// Sets are the deferred field values the client sent with the action
	//. A middleware may drop or rewrite entries (a policy
	// that forbids setting role); what it leaves is what the core applies,
	// through the same checks as $set, before the action.
	Sets []FieldSet
	// Log is the component's brick: {log:} setting for action loggers, nil
	// when the component does not set one (ActionLog).
	Log *ActionLog
}

// ActionResult is what an action returns: the component's new HTML, the
// instance's version after it, and the effects it recorded in order
// . A middleware after next may edit Effects (audit,
// filter, add); what it leaves there is what the client receives.
type ActionResult struct {
	HTML    []byte
	Version int64
	Effects []Effect
	// Presigned is set by a presign request ($upload with presign): where
	// the browser sends the file. The envelope carries it as "upload".
	Presigned *PresignedUpload
	// Reply is brick.reply's value, nil when none. A
	// middleware may clear or rewrite it; it must stay JSON values.
	Reply any
	// SkipRender is set when the action called brick.skip_render: HTML is
	// empty and Handle and Token are the root's new attributes, because a
	// sealed store's handle changes on every save even without a morph.
	SkipRender bool
	Handle     string
	Token      string
	// component and action name the call, for the Dev line about effects a
	// client did not accept.
	component, action string
}

// ActionFunc runs one action.
type ActionFunc func(ctx context.Context, a *ActionCall) (*ActionResult, error)

// ActionMiddleware wraps the next step, the shape of net/http middleware.
// Return an error without calling next to refuse: ErrForbidden → 403,
// ErrGone → 410 (the client recovers under the current session),
// *ActionError → its status, anything else → 422. Options.ActionMiddleware
// lists them outermost first. Package actionmw has the common ones.
type ActionMiddleware func(next ActionFunc) ActionFunc

// SessionBinder answers "who is this request?" — the app's session id,
// never brick's. "" means no session (signed out,
// public). The engine keeps only its SHA-256.
type SessionBinder interface {
	Session(r *http.Request) (string, error)
}

// SessionFunc adapts a function to SessionBinder.
type SessionFunc func(r *http.Request) (string, error)

// Session calls f.
func (f SessionFunc) Session(r *http.Request) (string, error) { return f(r) }

// Clock is a source of time: time.Now, or a test's fixed clock.
type Clock func() time.Time

// Action runs one action and returns the component's new HTML
// . Only the component's own script functions and the
// built-ins can be called; the only client-chosen input is a name, checked
// against that closed set. opts are the request's render options — Provide
// values for the re-render, WithSession for the session check — for a host
// calling it without ActionHandler.
func (e *Engine) Action(ctx context.Context, req ActionRequest, opts ...RenderOption) ([]byte, error) {
	var ro renderOptions
	for _, o := range opts {
		o(&ro)
	}
	if ro.err != nil {
		return nil, ro.err
	}
	res, err := e.action(ctx, nil, req, ro)
	if err != nil {
		return nil, err
	}
	return res.HTML, nil
}

// RunAction is Action returning the whole result — the HTML, the version
// and the effects — for hosts with their own client and for tests.
func (e *Engine) RunAction(ctx context.Context, req ActionRequest, opts ...RenderOption) (*ActionResult, error) {
	var ro renderOptions
	for _, o := range opts {
		o(&ro)
	}
	if ro.err != nil {
		return nil, ro.err
	}
	return e.action(ctx, nil, req, ro)
}

// pending is one loaded, verified action: what the core step needs beyond
// the ActionCall the middleware sees.
type pending struct {
	req  ActionRequest
	ro   renderOptions
	inst *Instance
	c    *compiled
	// formErrors are the errors the action returned ([state, errors]),
	// for a form post that must show them on a page render.
	formErrors map[string]any
}

type pendingKey struct{}

// action loads and verifies the instance, then runs the middleware chain
// around the core step.
func (e *Engine) action(ctx context.Context, r *http.Request, req ActionRequest, ro renderOptions) (*ActionResult, error) {
	res, _, err := e.actionWith(ctx, r, req, ro)
	return res, err
}

// actionWith is action, also returning the verified call: its instance
// (whose handle the save changed) and the errors the action returned, which
// a form post carries to the page.
func (e *Engine) actionWith(ctx context.Context, r *http.Request, req ActionRequest, ro renderOptions) (*ActionResult, *pending, error) {
	// The handle names its store; the engine asks exactly that one, never
	// guessing or trying stores in turn.
	name, handle, ok := strings.Cut(req.Handle, ".")
	store := e.stores[name]
	if !ok || store == nil {
		return nil, nil, ErrGone
	}
	session := ro.session
	if r != nil && e.opts.Sessions != nil {
		s, err := e.opts.Sessions.Session(r)
		if err != nil {
			return nil, nil, fmt.Errorf("%w: session: %v", ErrStoreUnavailable, err)
		}
		session = s
	}
	hash := hashSession(session)
	inst, ok, err := store.Get(WithStateScope(ctx, StateScope{Store: name, Session: hash}), handle)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	if !ok {
		return nil, nil, ErrGone
	}
	inst.Handle, inst.Store = handle, name
	// Another session (signed out, signed in as someone else) finds the
	// instance "not found", so the component recovers under the session the
	// user has now (4.6).
	if subtle.ConstantTimeCompare([]byte(inst.Session), []byte(hash)) != 1 {
		return nil, nil, ErrGone
	}
	if subtle.ConstantTimeCompare([]byte(inst.Token), []byte(req.Token)) != 1 {
		return nil, nil, ErrForbidden
	}
	c, err := e.load(ctx, inst.Path, kindComponent, inst.Locale)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			// The deploy removed or renamed the component (4.2).
			return nil, nil, ErrGone
		}
		return nil, nil, err
	}
	if c.meta.state == nil {
		return nil, nil, ErrGone
	}
	e.migrateState(inst, c)
	call := &ActionCall{Request: r, Component: inst.Name, Path: inst.Path, Store: name, Action: req.Action, Args: req.Args, Session: session, Sets: req.Sets, Log: c.meta.actionLog}
	call.State, _ = deepCopy(inst.State).(map[string]any)
	p := &pending{req: req, ro: ro, inst: inst, c: c}
	host := ""
	if r != nil {
		host = r.Host
	}
	eff := e.newEffects(host)
	ctx = withEffects(context.WithValue(ctx, pendingKey{}, p), eff)
	res, err := e.chain(ctx, call)
	if err != nil {
		eff.reset()
		return nil, nil, err
	}
	// The cap applies to what is sent, middleware additions included; a
	// reply rides in the same envelope and counts too.
	if len(res.Effects) > 0 || res.Reply != nil {
		if err := checkJSONValue("reply", res.Reply); err != nil {
			return nil, nil, fmt.Errorf("<%s>.%s: reply must be JSON values: %w", inst.Name, req.Action, err)
		}
		b, err := json.Marshal([]any{res.Effects, res.Reply})
		if err != nil {
			return nil, nil, fmt.Errorf("<%s>.%s: effects: %w", inst.Name, req.Action, err)
		}
		if len(b) > e.opts.MaxEffectBytes {
			return nil, nil, fmt.Errorf("<%s>.%s recorded %d bytes of effects and reply, over Options.MaxEffectBytes (%d): events carry ids, not records", inst.Name, req.Action, len(b), e.opts.MaxEffectBytes)
		}
	}
	res.component, res.action = inst.Name, req.Action
	// brick.publish fires only now: the action succeeded and its state is
	// saved. The answer is already decided, so a broadcaster failure is
	// reported, never turned into a failed action that did run.
	for _, t := range eff.published() {
		if err := e.Publish(ctx, t); err != nil {
			e.reportError(ctx, fmt.Errorf("brick: <%s>.%s: publish %s: %w", inst.Name, req.Action, t, err))
		}
	}
	// Background jobs start, and recorded cancels apply, at the same point,
	// for the same reason: a job never runs, or stops, for an action that
	// failed or state that was not kept. The job's context is not the
	// request's, so the request ending does not cancel it; a refused queue
	// is reported by the job's own failure path.
	e.applyJobs(ctx, eff, inst, req.Action)
	return res, p, nil
}

// chain runs Options.ActionMiddleware, outermost first, around the core.
func (e *Engine) chain(ctx context.Context, call *ActionCall) (*ActionResult, error) {
	next := ActionFunc(e.coreAction)
	for i := len(e.opts.ActionMiddleware) - 1; i >= 0; i-- {
		next = e.opts.ActionMiddleware[i](next)
	}
	return next(ctx, call)
}

// coreAction is the bottom of the chain: run the action, then save and
// re-render (the render's Put saves the instance and makes its next
// handle). It reads the action name and arguments from the call, so a
// middleware may rewrite them; the instance comes from the engine, never
// from the call.
func (e *Engine) coreAction(ctx context.Context, call *ActionCall) (*ActionResult, error) {
	p, _ := ctx.Value(pendingKey{}).(*pending)
	if p == nil {
		return nil, errors.New("brick: an ActionFunc was called outside Engine.Action")
	}
	inst, c, ro := p.inst, p.c, p.ro
	req := ActionRequest{Handle: p.req.Handle, Token: p.req.Token, Action: call.Action, Args: call.Args, Children: p.req.Children}
	var formErrors map[string]any
	// An upload request runs only $upload: its bytes are read here, from
	// the request, never through a renamed call.
	ub, _ := ctx.Value(uploadBodyKey{}).(*uploadBody)
	if (ub != nil) != (req.Action == actionUpload) {
		return nil, fmt.Errorf("%w: %s runs only as an upload request, and an upload request only as %s", ErrUnknownAction, actionUpload, actionUpload)
	}
	// Deferred values land first, so the action sees what was typed; one
	// refused value refuses the request before anything runs. An upload
	// field changes only through uploads.
	if err := refuseUploadSets(c.meta, call.Sets); err != nil {
		return nil, err
	}
	// Every client write, deferred sets and the built-ins that name a
	// field, passes the brick: {locked:} gate before anything is applied;
	// the action and hooks below are server writes and never do.
	for _, s := range call.Sets {
		if err := refuseLocked(c.meta, s.Path); err != nil {
			return nil, err
		}
	}
	if err := refuseLocked(c.meta, lockedArgPaths(req.Action, req.Args)...); err != nil {
		return nil, err
	}
	// Snapshots for what the action changed: queried fields write the URL
	//, remembered ones are saved (11), and updated: sees the
	// paths a built-in changed (9).
	queryBefore := queryValues(c.meta, inst.State)
	rememberBefore := rememberValues(c.meta, inst.State)
	var stateBefore map[string]any
	if c.meta.updated != "" {
		stateBefore, _ = deepCopy(inst.State).(map[string]any)
	}
	if err := applySets(inst, c.meta.state, call.Sets); err != nil {
		return nil, err
	}
	// written are the paths a built-in wrote, for updated:; a named action
	// owns its state, so it clears them and updated: does not run.
	written := make([]string, 0, len(call.Sets)+1)
	for _, s := range call.Sets {
		written = append(written, s.Path)
	}
	// A field the client sent is touched, so its rule errors show from
	// now on; without rules: nothing is recorded.
	if len(c.meta.rules) > 0 {
		for _, s := range call.Sets {
			touch(inst, s.Path)
		}
	}
	// committed is a file the sink already holds; if a later step fails it
	// is removed (when the sink can), so a failed request leaves no file
	// behind a ref that was never saved.
	var committed *UploadRef
	var presigned *PresignedUpload
	discard := func() {
		if rm, ok := e.opts.Uploads.(UploadRemover); ok && committed != nil {
			_ = rm.Remove(context.WithoutCancel(ctx), *committed)
		}
	}
	switch req.Action {
	case actionUpload:
		ref, pre, err := e.receiveUpload(ctx, inst, c, ub)
		if err != nil {
			return nil, err
		}
		presigned = pre
		if ref != nil {
			committed = ref
			placeRef(inst, ub.field, c.meta.uploads[ub.field], *ref)
			if len(c.meta.rules) > 0 {
				touch(inst, ub.field)
			}
		}
	case actionUploaded:
		field, ref, err := e.confirmUpload(ctx, inst, c, req.Args)
		if err != nil {
			return nil, err
		}
		committed = ref
		placeRef(inst, field, c.meta.uploads[field], *ref)
		if len(c.meta.rules) > 0 {
			touch(inst, field)
		}
	case actionRemoveUpload:
		field, err := e.removeUpload(ctx, inst, c, req.Args)
		if err != nil {
			return nil, err
		}
		if len(c.meta.rules) > 0 {
			touch(inst, field)
		}
	case actionSet:
		if len(req.Args) == 2 {
			if field, ok := req.Args[0].(string); ok {
				if err := refuseUploadSets(c.meta, []FieldSet{{Path: field}}); err != nil {
					return nil, err
				}
			}
		}
		if err := setField(inst, c.meta.state, req.Args); err != nil {
			return nil, err
		}
		field, _ := req.Args[0].(string)
		if len(c.meta.rules) > 0 {
			touch(inst, field)
		}
		written = append(written, field)
	case actionToggle:
		if err := toggleField(inst, c.meta.state, req.Args); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrUnknownAction, err)
		}
		field, _ := req.Args[0].(string)
		written = append(written, field)
	case actionReset:
		paths, err := resetFields(inst, c.meta.state, req.Args)
		if err != nil {
			return nil, err
		}
		written = append(written, paths...)
	case actionRefresh:
	case actionParams:
		// The URL set the queried fields (the sets above); only the hook's
		// own corrections may write the URL back.
		queryBefore = queryValues(c.meta, inst.State)
		errs, err := e.runParams(ctx, c, inst)
		if err != nil {
			EffectsFrom(ctx).reset()
			return nil, err
		}
		formErrors = errs
	default:
		// brick.validate and brick.untouch reach the rules through the
		// action's context only; the re-render below never sees it.
		actx := ctx
		if len(c.meta.rules) > 0 {
			st, _ := deepCopy(inst.State).(map[string]any)
			actx = withValidation(ctx, &Validation{v: e.opts.Validator, m: c.meta, inst: inst, state: st, locale: inst.Locale})
		}
		next, errs, err := callAction(actx, c, inst, req)
		if err != nil {
			// A failed action records nothing.
			EffectsFrom(ctx).reset()
			return nil, err
		}
		inst.State, formErrors = next, errs
		p.formErrors = errs
		written = nil
	}
	if stateBefore != nil {
		errs, err := e.runUpdated(ctx, c, inst, changedPaths(stateBefore, inst.State, written))
		if err != nil {
			EffectsFrom(ctx).reset()
			discard()
			return nil, err
		}
		if len(errs) > 0 {
			if formErrors == nil {
				formErrors = map[string]any{}
			}
			maps.Copy(formErrors, errs)
		}
	}
	if ef, changed := queryEffect(c.meta, queryBefore, inst.State); changed {
		if req.Action == actionParams {
			// A correction to an address the visitor just went to
			// replaces it; Back must not land on the uncorrected one.
			ef.Replace = true
		}
		if err := EffectsFrom(ctx).Add(ef); err != nil {
			EffectsFrom(ctx).reset()
			discard()
			return nil, err
		}
	}
	inst.Version++
	reply, _, skip := EffectsFrom(ctx).replyState()
	if skip {
		// Renderless: the state is saved and the version bumped
		// as usual; the answer carries the new handle instead of HTML.
		e.warnRenderless(c, inst, call.State, formErrors)
		if err := e.putInstance(ctx, inst, false); err != nil {
			EffectsFrom(ctx).reset()
			discard()
			return nil, err
		}
		e.saveRemembered(ctx, c, inst, rememberBefore)
		return &ActionResult{Version: inst.Version, Effects: EffectsFrom(ctx).List(), Presigned: presigned, Reply: reply, SkipRender: true, Handle: inst.Store + "." + inst.Handle, Token: inst.Token}, nil
	}
	// Request-scoped .Ctx comes from the live request, never from the
	// first render: an action sees the user signed in now (4.5).
	if call.Request != nil && e.opts.ActionRequest != nil {
		for _, o := range e.opts.ActionRequest(call.Request) {
			o(&ro)
		}
		if ro.err != nil {
			discard()
			return nil, ro.err
		}
	}
	ro.session = call.Session
	isl := &Island{Name: inst.Name, Path: inst.Path, Props: inst.Props, ID: inst.ID, Ctx: inst.Ctx}
	res := &Result{page: &Page{Path: inst.PagePath, Params: map[string]any{}, Route: inst.Route, RouteParts: inst.RouteParts}, locale: inst.Locale}
	// The re-render runs with the collector hidden: a loader or template
	// function calling brick.dispatch is drawing the page, not acting, and
	// fails (4.1).
	chunks, _, _, err := e.renderIslandWith(withEffects(ctx, nil), res, isl, ro, inst, formErrors, req.Children)
	if err != nil {
		EffectsFrom(ctx).reset()
		discard()
		// The component's loader turned the visitor away while it
		// re-rendered (signed out since the page loaded): the action
		// answers with that redirect as its one effect, checked by the
		// same open-redirect lock, and saves nothing.
		var rd *Redirect
		if errors.As(err, &rd) {
			eff := EffectsFrom(ctx)
			if rerr := eff.Redirect(rd.URL); rerr != nil {
				eff.reset()
				return nil, rerr
			}
			return &ActionResult{Version: inst.Version, Effects: eff.List()}, nil
		}
		return nil, err
	}
	html, err := (&Result{Chunks: chunks}).Bytes()
	if err != nil {
		EffectsFrom(ctx).reset()
		discard()
		return nil, err
	}
	e.saveRemembered(ctx, c, inst, rememberBefore)
	return &ActionResult{HTML: html, Version: inst.Version, Effects: EffectsFrom(ctx).List(), Presigned: presigned, Reply: reply}, nil
}

// migrateState reconciles an instance made by an earlier version of the
// component with the file's state: now: an added key
// gets its default, a removed key is dropped, a retyped key is coerced as
// $set would (reset to the default when that fails, with a Dev warning),
// and a key an action added that was never declared is kept. The template
// then always sees every key it declares.
func (e *Engine) migrateState(inst *Instance, c *compiled) {
	decl := c.meta.state
	if inst.State == nil {
		inst.State = map[string]any{}
	}
	for _, k := range inst.Keys {
		if _, still := decl[k]; !still {
			delete(inst.State, k)
		}
	}
	for _, k := range sortedKeys(decl) {
		def := decl[k]
		v, has := inst.State[k]
		if !has {
			inst.State[k] = deepCopy(def)
			continue
		}
		typ := typeNameOf(def)
		if typ == typeAny || v == nil {
			continue
		}
		cv, err := coerce(v, typ)
		if err != nil {
			if e.opts.Mode == Dev {
				e.warn(Diagnostic{File: inst.Path, Code: CodeRuntime, Message: fmt.Sprintf("<%s> state.%s: the stored %T does not fit the declared %s; reset to the default", inst.Name, k, v, typ)})
			}
			cv = deepCopy(def)
		}
		inst.State[k] = cv
	}
	inst.Keys = sortedKeys(decl)
}

// Built-in actions. Names starting with $ are the engine's.
const (
	actionSet     = "$set"
	actionRefresh = "$refresh"
	actionToggle  = "$toggle"
)

// callAction runs one of the component's script functions. It returns the
// new state and, when the action returned [state, errors], the errors.
func callAction(ctx context.Context, c *compiled, inst *Instance, req ActionRequest) (map[string]any, map[string]any, error) {
	ns, fn, qualified := strings.Cut(req.Action, ".")
	if !qualified {
		ns, fn = "", req.Action
	}
	mod := c.scripts[ns]
	if mod == nil || strings.HasPrefix(fn, "_") || strings.HasPrefix(fn, "$") || !contains(mod.Functions(), fn) {
		return nil, nil, fmt.Errorf("%w %q on <%s>", ErrUnknownAction, req.Action, inst.Name)
	}
	props, err := instanceProps(c, inst)
	if err != nil {
		return nil, nil, err
	}
	args := append([]any{inst.State, props}, req.Args...)
	out, err := mod.Call(ctx, fn, args...)
	if err != nil {
		return nil, nil, &Error{File: inst.Path, Component: inst.Name, Code: CodeScript, Err: fmt.Errorf("action %s: %w", req.Action, err)}
	}
	st, errs, ok := stateResult(out)
	if !ok {
		return nil, nil, &Error{File: inst.Path, Component: inst.Name, Code: CodeScript, Err: fmt.Errorf("action %s returned %T; an action returns the new state as a dict, or [state, errors]", req.Action, out)}
	}
	return st, errs, nil
}

// instanceProps are the props a script function of a stateful component
// sees, as its template does: the call site's, checked and completed by
// the component's props: declarations.
func instanceProps(c *compiled, inst *Instance) (map[string]any, error) {
	given := make([]propValue, 0, len(inst.Props))
	for _, k := range sortedKeys(inst.Props) {
		given = append(given, propValue{name: k, value: inst.Props[k]})
	}
	props, _, err := applyDecls(c.meta, given, inst.Name)
	if err != nil {
		return nil, &Error{File: inst.Path, Component: inst.Name, Code: CodeProp, Err: err}
	}
	return props, nil
}

// stateResult reads what an action (or an updated: hook) returned: the new
// state as a dict, or [state, errors] with errors a dict or None.
func stateResult(out any) (map[string]any, map[string]any, bool) {
	switch x := out.(type) {
	case map[string]any:
		return x, nil, true
	case []any:
		if len(x) != 2 {
			return nil, nil, false
		}
		st, ok1 := x[0].(map[string]any)
		errs, ok2 := x[1].(map[string]any)
		if !ok1 || (!ok2 && x[1] != nil) {
			return nil, nil, false
		}
		return st, errs, true
	}
	return nil, nil, false
}

// setField applies $set(path, value): one set, through the same gate as
// the sets an action carries (setPath), so a client can change a value but
// never add a key, grow a list or change a type. A malformed call is an
// unknown action (400); a refused value is ErrBadValue (422).
func setField(inst *Instance, initial map[string]any, args []any) error {
	if len(args) != 2 {
		return fmt.Errorf("%w: %s takes a field and a value", ErrUnknownAction, actionSet)
	}
	field, ok := args[0].(string)
	if !ok {
		return fmt.Errorf("%w: %s: the field must be a name", ErrUnknownAction, actionSet)
	}
	return applySets(inst, initial, []FieldSet{{Path: field, Value: args[1]}})
}

// typeNameOf is the prop type matching an initial state value.
func typeNameOf(v any) string {
	switch v.(type) {
	case string:
		return typeString
	case bool:
		return typeBool
	case int, int64:
		return typeInt
	case float64:
		return typeFloat
	}
	return typeAny
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// toggleField applies $toggle(field): the field must be a declared bool,
// and its value flips.
func toggleField(inst *Instance, initial map[string]any, args []any) error {
	if len(args) != 1 {
		return fmt.Errorf("%s takes a field", actionToggle)
	}
	field, ok := args[0].(string)
	if !ok {
		return fmt.Errorf("%s: the field must be a name", actionToggle)
	}
	if _, isBool := initial[field].(bool); !isBool {
		return fmt.Errorf("%s: %q is not a bool field of this component's state", actionToggle, field)
	}
	cur, _ := inst.State[field].(bool)
	if inst.State == nil {
		inst.State = map[string]any{}
	}
	inst.State[field] = !cur
	return nil
}

// ActionHandler serves action requests: POST, JSON, with the Brick-Action
// header, at most Options.MaxActionBytes. Mount it at AssetBase +
// ActionEndpoint (the site layer does). Errors map to statuses as
// ActionMiddleware documents, or through Options.ActionErrors.
func (e *Engine) ActionHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if r.Header.Get(actionHeader) == "" || !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
			http.Error(w, "an action is a JSON POST with the Brick-Action header", http.StatusBadRequest)
			return
		}
		var req ActionRequest
		dec := json.NewDecoder(io.LimitReader(r.Body, int64(e.opts.MaxActionBytes)))
		dec.UseNumber()
		if err := dec.Decode(&req); err != nil {
			http.Error(w, "bad action request", http.StatusBadRequest)
			return
		}
		for i, a := range req.Args {
			req.Args[i] = normalizeJSON(a)
		}
		// A client that accepts a stream gets one only if the action writes
		// to it; the ordering per root is the runtime's queue,
		// which waits for the whole answer either way.
		ctx := r.Context()
		var stream *ndjsonStream
		if acceptsStream(r.Header.Get("Accept")) {
			stream = newNDJSONStream(w)
			ctx = WithActionStream(ctx, stream)
		}
		res, err := e.action(ctx, r, req, renderOptions{})
		if stream != nil && stream.started() {
			if !errors.Is(err, context.Canceled) {
				e.finishStream(stream, r, res, err)
			}
			return
		}
		if err == nil {
			e.writeActionResult(w, r, res)
			return
		}
		if errors.Is(err, context.Canceled) {
			return
		}
		if e.opts.ActionErrors != nil {
			e.opts.ActionErrors(w, r, err)
			return
		}
		e.writeActionError(w, r, err)
	})
}

// writeActionResult answers a successful action: the JSON envelope when the
// client asked for it with Accept, else the HTML alone, as before v6 — a
// host's own client or curl keeps working, and only loses the effects
// (logged in Dev).
func (e *Engine) writeActionResult(w http.ResponseWriter, r *http.Request, res *ActionResult) {
	w.Header().Add("Vary", "Accept")
	// A presign answer is only useful as the envelope: the target rides
	// beside the HTML.
	if acceptsEffects(r.Header.Get("Accept")) || res.Presigned != nil || res.SkipRender {
		b, err := json.Marshal(e.envelopeOf(res))
		if err != nil {
			e.writeActionError(w, r, err)
			return
		}
		w.Header().Set("Content-Type", EffectsContentType)
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(b)
		return
	}
	if len(res.Effects) > 0 && e.opts.Mode == Dev {
		e.warn(Diagnostic{Code: CodeRuntime, Message: fmt.Sprintf("<%s>.%s recorded %d effects the client did not accept (send Accept: %s)", res.component, res.action, len(res.Effects), EffectsContentType)})
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(res.HTML)
}

// envelopeOf is an action's JSON envelope, as the plain answer and a
// stream's last line carry it.
func (e *Engine) envelopeOf(res *ActionResult) actionEnvelope {
	effects := res.Effects
	if effects == nil {
		effects = []Effect{}
	}
	env := actionEnvelope{V: effectsEnvelopeVersion, Version: res.Version, HTML: string(res.HTML), Effects: effects, Upload: res.Presigned, Reply: res.Reply}
	if res.SkipRender {
		env.HTML, env.Handle, env.Token = "", res.Handle, res.Token
	}
	return env
}

// ActionStatus is the default HTTP status for an action error: the one
// the engine's own error envelope answers with (ActionMiddleware documents
// the mapping), exported so middleware that reports outcomes (metrics,
// audit) agrees with what the client saw. nil is 200; an ActionError
// keeps its status when it is a 4xx or 5xx and is 403 otherwise; an error
// brick does not recognise is 422.
func ActionStatus(err error) int {
	var ae *ActionError
	var he *HTTPError
	switch {
	case err == nil:
		return http.StatusOK
	case errors.As(err, &ae):
		if !isErrorStatus(ae.Status) {
			return http.StatusForbidden
		}
		return ae.Status
	case errors.Is(err, ErrStoreUnavailable):
		return http.StatusServiceUnavailable
	case errors.Is(err, ErrGone):
		return http.StatusGone
	case errors.Is(err, ErrForbidden):
		return http.StatusForbidden
	case errors.As(err, &he):
		// A loader's brick.error during the re-render keeps its status.
		return he.Status
	case errors.Is(err, ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, ErrUnknownAction):
		return http.StatusBadRequest
	case errors.Is(err, ErrUploadTooLarge):
		return http.StatusRequestEntityTooLarge
	case errors.Is(err, ErrUploadType):
		return http.StatusUnsupportedMediaType
	default:
		// ErrBadValue and every script or host failure.
		return http.StatusUnprocessableEntity
	}
}

// writeActionError is the default error envelope: ActionStatus's status
// and a plain message, with no file paths or error text in Prod.
func (e *Engine) writeActionError(w http.ResponseWriter, r *http.Request, err error) {
	status := ActionStatus(err)
	var ae *ActionError
	switch {
	case errors.As(err, &ae):
		if ae.Err != nil {
			e.reportError(r.Context(), ae.Err)
		}
		if ae.RetryAfter > 0 {
			w.Header().Set("Retry-After", strconv.Itoa(int((ae.RetryAfter+time.Second-1)/time.Second)))
		}
		http.Error(w, ae.Error(), status)
	case errors.Is(err, ErrStoreUnavailable):
		// An outage, not a lost state: the runtime retries and never
		// recovers or reloads on it.
		e.reportError(r.Context(), err)
		w.Header().Set("Retry-After", "1")
		msg := ErrStoreUnavailable.Error()
		if e.opts.Mode == Dev {
			msg = err.Error()
		}
		http.Error(w, msg, status)
	case errors.Is(err, ErrGone):
		http.Error(w, ErrGone.Error(), status)
	case errors.Is(err, ErrForbidden):
		http.Error(w, ErrForbidden.Error(), status)
	case IsAnswer(err):
		// A loader's error status: its author-written message, never the
		// wrapping error's file path.
		msg := http.StatusText(status)
		if he := (*HTTPError)(nil); errors.As(err, &he) && he.Message != "" {
			msg = he.Message
		}
		http.Error(w, msg, status)
	case status != http.StatusUnprocessableEntity || errors.Is(err, ErrBadValue):
		// Unknown action, upload size and type, and ErrBadValue: the
		// message names the action, field or rule, never a file path, so
		// it is safe in Prod too.
		http.Error(w, err.Error(), status)
	default:
		e.reportError(r.Context(), err)
		msg := err.Error()
		if e.opts.Mode != Dev {
			msg = "the action failed (error " + errorID(err) + ")"
		}
		http.Error(w, msg, status)
	}
}
