package brick

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"regexp"
	"slices"
	"strings"
	"sync"
)

// Effect is one thing an action asks the browser to do after the morph
// : data, never code. Kind says which fields are used:
//
//	dispatch  Name, Data, To      emit an event, then wake its listeners
//	redirect  URL                 location.assign (terminal)
//	navigate  URL                 brick.navigate (terminal)
//	flash     Message, Level      a one-shot message for the toaster
//	title     Text                document.title
//	download  URL, Filename       a temporary <a download>
//	url       URL, Replace        history push/replace, no fetch
//	query     Data, Replace       URL parameters of brick: {query:} fields
//	rows      Ref, Op, At, Limit, Rows, Keys
//	                              rows of a brick:ref list
//	submit    Ref                 a native form.submit() of a brick:ref form (terminal)
//	flash-clear Level             dismiss shown flashes: every one, or one level
//	transition Name               this answer's view transition: types, "" or "none" (v22)
//	region    Ref, Op             apply a brick:ref region of the fresh render:
//	                              replace, append or prepend
//	x-…       Data                a host's own kind, handled by brick.effect
type Effect struct {
	Kind     string         `json:"kind"`
	Name     string         `json:"name,omitempty"`
	Data     map[string]any `json:"data,omitempty"`
	To       string         `json:"to,omitempty"`
	URL      string         `json:"url,omitempty"`
	Filename string         `json:"filename,omitempty"`
	Replace  bool           `json:"replace,omitempty"`
	Message  string         `json:"message,omitempty"`
	Level    string         `json:"level,omitempty"`
	Text     string         `json:"text,omitempty"`
	// Ref names a brick:ref element of the component's root (rows, submit).
	Ref string `json:"ref,omitempty"`
	// Op, At and Limit are a rows effect's operation (RowsInsert,
	// RowsDelete, RowsReset), end (RowsTop, RowsBottom) and cap.
	Op    string `json:"op,omitempty"`
	At    string `json:"at,omitempty"`
	Limit int    `json:"limit,omitempty"`
	// Rows are the rendered rows a rows effect inserts; Keys the rows a
	// delete removes.
	Rows []Row    `json:"rows,omitempty"`
	Keys []string `json:"keys,omitempty"`
}

// Row is one server-rendered row of a rows effect: one root element, which
// the runtime marks with brick:key=Key. HTML is template.HTML because it
// goes into the page as markup: Effects.Rows fills it only from a
// component render, where html/template has escaped every value; a host
// that builds a Row by hand through Add vouches for its HTML the same way
// it would for any template.HTML.
type Row struct {
	Key  string        `json:"key"`
	HTML template.HTML `json:"html"`
}

// The engine's effect kinds. A host's own kind starts with CustomEffectPrefix,
// so no future engine kind can collide with it (4.6).
const (
	EffectDispatch = "dispatch"
	EffectRedirect = "redirect"
	EffectNavigate = "navigate"
	EffectFlash    = "flash"
	EffectTitle    = "title"
	EffectDownload = "download"
	EffectURL      = "url"
	EffectRows     = "rows"
	EffectSubmit   = "submit"
	// EffectFlashClear dismisses flashes already shown; a
	// Level of "" means every level.
	EffectFlashClear = "flash-clear"
	// EffectTransition chooses the view transition of this answer's own
	// morph: Name holds space-separated
	// transition types, "" for an untyped one, or TransitionNone to skip.
	EffectTransition = "transition"
	// CustomEffectPrefix starts every host-defined kind ("x-confetti").
	CustomEffectPrefix = "x-"
)

// Row operations and ends of a rows effect. Insert adds or
// replaces rows by key, Delete removes rows by key, and Reset empties the
// list before inserting the rows it carries.
const (
	RowsInsert = "insert"
	RowsDelete = "delete"
	RowsReset  = "reset"
	RowsTop    = "top"
	RowsBottom = "bottom"
)

var rowOps = []string{RowsInsert, RowsDelete, RowsReset}

// maxRowKey is the longest row key: a key is an id, never a record.
const maxRowKey = 128

// TransitionNone, as a transition effect's name, morphs this answer
// without a view transition even on a root with brick:transition.
const TransitionNone = "none"

// maxTransitionTypes caps the types one transition effect names: a few
// CSS hooks, never a list built from data.
const maxTransitionTypes = 8

// transitionTypeRE is one view transition type: a CSS identifier of at
// most 64 characters, since the browser matches it with
// :active-view-transition-type(<type>) and a longer name is data, not a
// stylesheet hook.
var transitionTypeRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]{0,63}$`)

// DispatchSelf is the To of an event meant only for the dispatching
// component's own listeners (5.4).
const DispatchSelf = "self"

// Flash levels: the kit Toast variants. A flash without one is FlashInfo.
const (
	FlashNeutral = "neutral"
	FlashInfo    = "info"
	FlashSuccess = "success"
	FlashWarning = "warning"
	FlashDanger  = "danger"
)

var flashLevels = []string{FlashNeutral, FlashInfo, FlashSuccess, FlashWarning, FlashDanger}

// DefaultMaxEffectBytes caps one action's encoded effects when
// Options.MaxEffectBytes is zero: events carry ids, not records (4.7).
const DefaultMaxEffectBytes = 32 << 10

// EffectsContentType is the action response the runtime asks for with
// Accept: a JSON envelope {v, version, html, effects} (4.3). A client that
// does not ask gets the component's HTML alone.
const EffectsContentType = "application/vnd.brick.action+json"

// effectsEnvelopeVersion is the envelope's "v"; it changes only when a
// field changes meaning.
const effectsEnvelopeVersion = 1

// eventNameRE is the event-name grammar (5.1): lowercase, because the same
// name is written as an attribute (brick:on:<name>) and browsers lowercase
// attribute names.
var eventNameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9._:-]{0,63}$`)

// reservedEventPrefix and reservedEvents are names the runtime emits
// itself on brick.on; an action dispatching one could fake a recovery or a
// navigation, so recording one fails (5.1, 5.8). The list must equal the
// runtime's RESERVED set (runtime/runtime.js), which refuses the same
// names in the browser; TestReservedEventsMatchRuntime keeps the two in
// step, so a name the runtime adds is refused here too rather than
// failing only once it reaches a page.
const reservedEventPrefix = "brick:"

var reservedEvents = []string{"action-error", "action-cancelled", "action-retry", "recovered", "state-gone", "state-lost", "navigate", "navigated", "flash", "flash-clear", "effect", "toast", "event-loop", "island-loaded", "island-error", "errors-cleared", "upload-progress", "upload-done", "upload-error", "push-error", "refreshed", "dirty", "clean"}

// componentNameRE is what a dispatch's To may name besides "self": a
// component file's stem, as data-brick-name carries it.
var componentNameRE = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]*$`)

// checkEventName reports why name cannot be an event, or nil.
func checkEventName(name string) error {
	if !eventNameRE.MatchString(name) {
		if lower := strings.ToLower(name); lower != name && eventNameRE.MatchString(lower) {
			return fmt.Errorf("event %q: event names are lowercase (browsers lowercase attribute names); write %q", name, lower)
		}
		return fmt.Errorf("event %q: an event name is lowercase letters, digits and . _ : - (at most 64)", name)
	}
	if strings.HasPrefix(name, reservedEventPrefix) || slices.Contains(reservedEvents, name) {
		return fmt.Errorf("event %q is reserved for the runtime", name)
	}
	return nil
}

// Effects collects what one action asks the browser to do after the
// morph. The engine puts one in every action's
// context before the middleware chain; EffectsFrom finds it. It is safe for
// concurrent use, and effects are applied in the order they were added.
// Every method validates when it is called, so a bad effect fails the
// action that recorded it — never a response the browser has to guess
// about. An action that fails records nothing: the engine discards the
// collector on any error.
type Effects struct {
	mu    sync.Mutex
	list  []Effect
	size  int
	max   int
	host  string   // the request's own host; "" outside a request
	hosts []string // Options.RedirectHosts
	warn  func(string)
	// eng renders a rows effect's rows; nil for a collector made outside
	// an engine, where Rows refuses.
	eng *Engine
	// reply is brick.reply's value and replySize its encoded size, which
	// counts toward max with the effects; hasReply tells a None reply
	// from none. skip is brick.skip_render.
	reply     any
	replySize int
	hasReply  bool
	skip      bool
	// publish are the topics brick.publish named, published once the
	// action succeeded; never sent to the client.
	publish []string
	// jobs are the background jobs brick.start_job recorded, started once
	// the action succeeded; never sent to the client.
	jobs []Job
	// cancels are the job ids brick.cancel_job recorded, cancelled once
	// the action succeeded; never sent to the client.
	cancels []string
}

type effectsKey struct{}

// EffectsFrom returns the action's collector, or nil outside an action —
// including while a component renders, even the re-render after an action:
// effects come from what the user did, never from drawing the page, so
// rendering twice can never fire an event twice.
func EffectsFrom(ctx context.Context) *Effects {
	e, _ := ctx.Value(effectsKey{}).(*Effects)
	return e
}

// withEffects sets (or, with nil, hides) the collector on ctx.
func withEffects(ctx context.Context, e *Effects) context.Context {
	return context.WithValue(ctx, effectsKey{}, e)
}

// newEffects makes the collector for one action. host is the request's
// Host header, the one origin an absolute URL may always name.
func (e *Engine) newEffects(host string) *Effects {
	eff := &Effects{eng: e, max: e.opts.MaxEffectBytes, host: strings.ToLower(host), hosts: make([]string, 0, len(e.opts.RedirectHosts))}
	for _, h := range e.opts.RedirectHosts {
		eff.hosts = append(eff.hosts, strings.ToLower(h))
	}
	if e.opts.Mode == Dev {
		eff.warn = func(msg string) { e.warn(Diagnostic{Code: CodeRuntime, Message: msg}) }
	}
	return eff
}

// Dispatch records an event (5.4). data is JSON values only; to is "" (every
// listener but the dispatching component), a component name, or
// DispatchSelf.
func (e *Effects) Dispatch(name string, data map[string]any, to string) error {
	return e.Add(Effect{Kind: EffectDispatch, Name: name, Data: data, To: to})
}

// Redirect records a full page load of url after the morph. It is
// terminal: a later Redirect or Navigate replaces it.
func (e *Effects) Redirect(url string) error {
	return e.Add(Effect{Kind: EffectRedirect, URL: url})
}

// Navigate records a client navigation to url (nav.js, keeping
// brick:persist). Terminal, like Redirect.
func (e *Effects) Navigate(url string) error {
	return e.Add(Effect{Kind: EffectNavigate, URL: url})
}

// Flash records a one-shot message; level is one of the Flash* levels, ""
// meaning FlashInfo.
func (e *Effects) Flash(message, level string) error {
	return e.Add(Effect{Kind: EffectFlash, Message: message, Level: level})
}

// ClearFlash records the dismissal of flashes already on screen: level "" clears every one, otherwise only that Flash* level. A
// flash recorded later in the same action still shows: effects apply in
// order.
func (e *Effects) ClearFlash(level string) error {
	return e.Add(Effect{Kind: EffectFlashClear, Level: level})
}

// Title records a new document title; the last one wins.
func (e *Effects) Title(text string) error {
	return e.Add(Effect{Kind: EffectTitle, Text: text})
}

// Download records a file download from url, which the host serves; an
// action never streams a file.
func (e *Effects) Download(url, filename string) error {
	return e.Add(Effect{Kind: EffectDownload, URL: url, Filename: filename})
}

// Transition chooses the view transition of this action's own morph
// (v22): types are space-separated transition types ("slide", "slide
// back"), "" an untyped transition, and TransitionNone none at all. The
// action decides because only it knows what changed: a reorder can slide
// while a validation re-render of the same root stays still. A choice
// applies whether or not the root has brick:transition, and the last call
// wins. The browser still skips it under reduced motion, without the API,
// or while another transition runs.
func (e *Effects) Transition(types string) error {
	return e.Add(Effect{Kind: EffectTransition, Name: types})
}

// URL records a history entry (push, or replace) without a fetch. Same
// origin only: the browser refuses anything else.
func (e *Effects) URL(url string, replace bool) error {
	return e.Add(Effect{Kind: EffectURL, URL: url, Replace: replace})
}

// RowsSpec is one rows effect: a list the server appends
// to, trims and deletes from without keeping its rows in the component's
// state. Ref names the list's brick:ref element; Op is RowsInsert (the
// default), RowsDelete or RowsReset. Insert and reset render Component
// once per item, the item being its props, and key each row by the item's
// Key field ("id" by default); a row whose key is already in the list
// replaces it in place. At is the end new rows go to (RowsBottom by
// default), and a Limit above zero trims the list from the other end.
// Delete removes the rows named by Keys.
type RowsSpec struct {
	Ref       string
	Op        string
	Component string
	Items     []map[string]any
	Key       string
	At        string
	Limit     int
	Keys      []string
}

// Rows records a rows effect, rendering its rows now. The rows are always
// a component's render, never a string, so a value from a user reaches
// the page escaped exactly as it would in any template; that is why there
// is no html argument. The list element should carry brick:ignore, so the
// component's next morph leaves the rows the effects put there alone.
func (e *Effects) Rows(ctx context.Context, s RowsSpec) error {
	if e == nil {
		return errors.New("brick: effects can only be recorded inside an action")
	}
	ef := Effect{Kind: EffectRows, Ref: s.Ref, Op: s.Op, At: s.At, Limit: s.Limit, Keys: s.Keys}
	if ef.Op == "" {
		ef.Op = RowsInsert
	}
	if ef.Op == RowsDelete {
		if s.Component != "" || len(s.Items) > 0 {
			return errors.New("brick: rows effect: a delete takes keys, not items")
		}
		return e.Add(ef)
	}
	if len(s.Keys) > 0 {
		return fmt.Errorf("brick: rows effect: keys are for a delete; an %s keys its rows by the items' %q field", ef.Op, cmp.Or(s.Key, "id"))
	}
	if len(s.Items) > 0 && s.Component == "" {
		return fmt.Errorf("brick: rows effect: an %s needs the component that renders each item", ef.Op)
	}
	if len(s.Items) > 0 && e.eng == nil {
		return errors.New("brick: rows effect: this collector has no engine to render rows with")
	}
	field := cmp.Or(s.Key, "id")
	var opts []RenderOption
	if p, _ := ctx.Value(pendingKey{}).(*pending); p != nil && p.ro.locale != "" {
		opts = append(opts, WithLocale(p.ro.locale))
	}
	// A row is drawn, not acted on: its render sees no collector, as every
	// render does.
	rctx := withEffects(ctx, nil)
	for i, item := range s.Items {
		raw, ok := item[field]
		if !ok || raw == nil {
			return fmt.Errorf("brick: rows effect: item %d has no %q field to key its row by", i, field)
		}
		b, err := e.eng.RenderComponent(rctx, s.Component, item, "", opts...)
		if err != nil {
			return fmt.Errorf("brick: rows effect: item %d: %w", i, err)
		}
		ef.Rows = append(ef.Rows, Row{Key: fmt.Sprint(raw), HTML: template.HTML(strings.TrimSpace(string(b)))}) // html/template's own output
	}
	return e.Add(ef)
}

// Submit records a native submit of the brick:ref form ref after the morph
// : the browser posts it as a plain form, bypassing brick's
// own handling, to hand off to a classic endpoint (a login that sets a
// cookie, a payment provider's POST). It leaves the page, so it is
// terminal like Redirect: a later Redirect, Navigate or Submit replaces it.
func (e *Effects) Submit(ref string) error {
	return e.Add(Effect{Kind: EffectSubmit, Ref: ref})
}

// Reply attaches a JSON value to the action's answer: the
// browser's brick.action() resolves with it. The last call wins. It must
// encode as JSON and counts toward Options.MaxEffectBytes, because it rides
// in the same envelope as the effects.
func (e *Effects) Reply(v any) error {
	if e == nil {
		return errors.New("brick: a reply can only be given inside an action")
	}
	v = deepCopy(v)
	if err := checkJSONValue("reply", v); err != nil {
		return fmt.Errorf("brick: reply must be JSON values: %w", err)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("brick: reply: %w", err)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.max > 0 && e.size+len(b) > e.max {
		return fmt.Errorf("brick: reply and effects over %d bytes (Options.MaxEffectBytes)", e.max)
	}
	e.size += len(b) - e.replySize
	e.reply, e.replySize, e.hasReply = v, len(b), true
	return nil
}

// SkipRender asks the engine not to re-render after this action: the state is still saved and the version bumped, and the answer
// carries the new handle instead of HTML. Only the server can decide this,
// because only it knows whether the view changed.
func (e *Effects) SkipRender() error {
	if e == nil {
		return errors.New("brick: skip_render can only be called inside an action")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if slices.ContainsFunc(e.list, func(x Effect) bool { return x.Kind == EffectRegion }) {
		return errRegionSkipped
	}
	e.skip = true
	return nil
}

// replyState returns the reply (and whether one was given) and the
// skip-render flag.
func (e *Effects) replyState() (any, bool, bool) {
	if e == nil {
		return nil, false, false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return deepCopy(e.reply), e.hasReply, e.skip
}

// Add records any effect, the engine's kinds or a host's own x- kind: the
// one escape hatch every other method goes through.
func (e *Effects) Add(ef Effect) error {
	if e == nil {
		return errors.New("brick: effects can only be recorded inside an action")
	}
	if err := e.check(&ef); err != nil {
		return err
	}
	if ef.Data != nil {
		ef.Data, _ = deepCopy(ef.Data).(map[string]any)
	}
	b, err := json.Marshal(ef)
	if err != nil {
		return fmt.Errorf("brick: %s effect: %w", ef.Kind, err)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if ef.Kind == EffectRegion && e.skip {
		return errRegionSkipped
	}
	if e.max > 0 && e.size+len(b) > e.max {
		return fmt.Errorf("brick: effects over %d bytes (Options.MaxEffectBytes): events carry ids, not records", e.max)
	}
	if ef.Kind == EffectTransition {
		// One morph has one transition: the last choice replaces the earlier.
		if i := slices.IndexFunc(e.list, func(x Effect) bool { return x.Kind == EffectTransition }); i >= 0 {
			old, _ := json.Marshal(e.list[i]) // it marshalled when it was added
			e.size -= len(old)
			e.list = slices.Delete(e.list, i, i+1)
		}
	}
	if terminal(ef.Kind) {
		if i := slices.IndexFunc(e.list, func(x Effect) bool { return terminal(x.Kind) }); i >= 0 {
			if e.warn != nil {
				e.warn(fmt.Sprintf("an action recorded %s %q after %s %q; the later one wins", ef.Kind, ef.target(), e.list[i].Kind, e.list[i].target()))
			}
			e.list = slices.Delete(e.list, i, i+1)
		}
	}
	e.size += len(b)
	e.list = append(e.list, ef)
	return nil
}

// List returns a copy of the recorded effects, in order.
func (e *Effects) List() []Effect {
	if e == nil {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]Effect, len(e.list))
	for i, ef := range e.list {
		if ef.Data != nil {
			ef.Data, _ = deepCopy(ef.Data).(map[string]any)
		}
		out[i] = ef
	}
	return out
}

// Filter keeps only the effects keep returns true for — a policy
// middleware's tool ("no redirects from <Widget>").
func (e *Effects) Filter(keep func(Effect) bool) {
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.list = slices.DeleteFunc(e.list, func(ef Effect) bool { return !keep(ef) })
	e.size = e.replySize
	for _, ef := range e.list {
		b, _ := json.Marshal(ef)
		e.size += len(b)
	}
}

// reset forgets everything: a failed action records nothing (4.4).
func (e *Effects) reset() {
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.list, e.size = nil, 0
	e.reply, e.replySize, e.hasReply, e.skip = nil, 0, false, false
	e.publish = nil
	e.jobs = nil
	e.cancels = nil
}

// Publish records topic to publish once the action has succeeded and its
// state is saved, so a listener's refresh reads what the action
// wrote, and a failed action publishes nothing. A topic named twice is
// published once. It needs Options.Push, and a valid topic.
func (e *Effects) Publish(topic string) error {
	if e.eng == nil || e.eng.opts.Push == nil {
		return ErrPushOff
	}
	if strings.HasPrefix(topic, engineTopicPrefix) {
		return fmt.Errorf("%q: topics starting with %q are the engine's own", topic, engineTopicPrefix)
	}
	if !ValidTopic(topic) {
		return fmt.Errorf("%q is not a topic (1-%d characters from A-Z a-z 0-9 _ . : / @ -)", topic, MaxTopicLen)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if !slices.Contains(e.publish, topic) {
		e.publish = append(e.publish, topic)
	}
	return nil
}

// published returns the recorded topics.
func (e *Effects) published() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return slices.Clone(e.publish)
}

// terminal reports a kind that leaves the page.
func terminal(kind string) bool {
	return kind == EffectRedirect || kind == EffectNavigate || kind == EffectSubmit
}

// target is what a terminal effect leaves for, for a Dev warning.
func (ef Effect) target() string { return cmp.Or(ef.URL, ef.Ref) }

// check validates one effect for its kind and fills defaults.
func (e *Effects) check(ef *Effect) error {
	fail := func(format string, a ...any) error {
		return fmt.Errorf("brick: %s effect: %s", ef.Kind, fmt.Sprintf(format, a...))
	}
	if err := checkJSONValue("data", ef.Data); err != nil {
		return fail("data must be JSON values: %v", err)
	}
	switch ef.Kind {
	case EffectDispatch:
		if err := checkEventName(ef.Name); err != nil {
			return fail("%v", err)
		}
		if ef.To != "" && ef.To != DispatchSelf && !componentNameRE.MatchString(ef.To) {
			return fail("to %q must be a component name or %q", ef.To, DispatchSelf)
		}
	case EffectRedirect, EffectNavigate, EffectDownload:
		if err := e.checkURL(ef.URL, true); err != nil {
			return fail("%v", err)
		}
	case EffectURL:
		if err := e.checkURL(ef.URL, false); err != nil {
			return fail("%v", err)
		}
	case EffectFlash:
		if ef.Message == "" {
			return fail("the message is empty")
		}
		if ef.Level == "" {
			ef.Level = FlashInfo
		}
		if !slices.Contains(flashLevels, ef.Level) {
			return fail("level %q is not one of %s", ef.Level, strings.Join(flashLevels, ", "))
		}
	case EffectFlashClear:
		if ef.Level != "" && !slices.Contains(flashLevels, ef.Level) {
			return fail("level %q is not one of %s (or empty for every level)", ef.Level, strings.Join(flashLevels, ", "))
		}
	case EffectTitle:
	case EffectTransition:
		if err := checkTransition(ef.Name); err != nil {
			return fail("%v", err)
		}
	case EffectRows:
		return checkRows(ef)
	case EffectRegion:
		return checkRegion(ef)
	case EffectSubmit:
		if !validRef(ef.Ref) {
			return fail("ref %q must be a brick:ref name", ef.Ref)
		}
	case EffectQuery:
		if ef.Data == nil {
			return fail("data is {set:, del:}")
		}
	default:
		if !strings.HasPrefix(ef.Kind, CustomEffectPrefix) || len(ef.Kind) == len(CustomEffectPrefix) {
			return fmt.Errorf("brick: effect kind %q is not an engine kind; a host's own kind starts with %q", ef.Kind, CustomEffectPrefix)
		}
	}
	return nil
}

// checkTransition validates a transition effect's name: TransitionNone,
// empty, or up to maxTransitionTypes CSS identifiers.
func checkTransition(name string) error {
	if name == TransitionNone {
		return nil
	}
	types := strings.Fields(name)
	if len(types) > maxTransitionTypes {
		return fmt.Errorf("%d types; at most %d", len(types), maxTransitionTypes)
	}
	for _, t := range types {
		if t == TransitionNone {
			return fmt.Errorf("%q is not a type: write it alone to skip the transition", TransitionNone)
		}
		if !transitionTypeRE.MatchString(t) {
			return fmt.Errorf("type %q must be a CSS identifier", t)
		}
	}
	return nil
}

// validRef reports a usable brick:ref name: what stream refs allow.
func validRef(ref string) bool {
	return ref != "" && !strings.ContainsAny(ref, " \t\n\"'<>")
}

// checkRows validates a rows effect and fills its defaults.
func checkRows(ef *Effect) error {
	fail := func(format string, a ...any) error {
		return fmt.Errorf("brick: rows effect: %s", fmt.Sprintf(format, a...))
	}
	if !validRef(ef.Ref) {
		return fail("ref %q must be a brick:ref name", ef.Ref)
	}
	if !slices.Contains(rowOps, ef.Op) {
		return fail("op %q is not one of %s", ef.Op, strings.Join(rowOps, ", "))
	}
	if ef.Limit < 0 {
		return fail("limit %d is negative", ef.Limit)
	}
	keys := ef.Keys
	if ef.Op == RowsDelete {
		if len(ef.Rows) > 0 || len(ef.Keys) == 0 {
			return fail("a delete names the keys of the rows it removes")
		}
	} else {
		if len(ef.Keys) > 0 {
			return fail("keys are for a delete")
		}
		if ef.Op == RowsInsert && len(ef.Rows) == 0 {
			return fail("an insert has no rows")
		}
		if ef.At == "" {
			ef.At = RowsBottom
		}
		if ef.At != RowsTop && ef.At != RowsBottom {
			return fail("at %q is not %q or %q", ef.At, RowsTop, RowsBottom)
		}
		keys = make([]string, 0, len(ef.Rows))
		for _, r := range ef.Rows {
			if strings.TrimSpace(string(r.HTML)) == "" {
				return fail("row %q rendered nothing", r.Key)
			}
			keys = append(keys, r.Key)
		}
	}
	seen := make(map[string]bool, len(keys))
	for _, k := range keys {
		if k == "" || len(k) > maxRowKey || strings.ContainsAny(k, "\x00\t\r\n") {
			return fail("key %q must be 1 to %d characters with no control characters", k, maxRowKey)
		}
		if seen[k] {
			return fail("key %q appears twice", k)
		}
		seen[k] = true
	}
	return nil
}

// checkURL is the open-redirect lock (4.7): a path on this site, or an
// absolute http(s) URL on the request's own host — or, when others is set,
// on one of Options.RedirectHosts. javascript:, data:, protocol-relative
// //host and backslash tricks all fail.
func (e *Effects) checkURL(raw string, others bool) error {
	return checkRedirectURL(raw, e.host, e.hosts, others, false)
}

// actionEnvelope is the JSON action response (4.3).
type actionEnvelope struct {
	V       int      `json:"v"`
	Version int64    `json:"version"`
	HTML    string   `json:"html,omitempty"`
	Effects []Effect `json:"effects"`
	// Upload is a presign request's target.
	Upload *PresignedUpload `json:"upload,omitempty"`
	// Reply is brick.reply's value; absent when none.
	Reply any `json:"reply,omitempty"`
	// Handle and Token are the root's new data-brick-state and
	// data-brick-token when the action skipped its render, so HTML is
	// absent: a sealed store's handle is the state itself and changes on
	// every save, so the client must learn it even without a morph.
	Handle string `json:"handle,omitempty"`
	Token  string `json:"token,omitempty"`
}

// acceptsEffects reports whether the client asked for the envelope.
func acceptsEffects(accept string) bool {
	for _, part := range strings.Split(accept, ",") {
		mt, _, _ := strings.Cut(part, ";")
		if strings.EqualFold(strings.TrimSpace(mt), EffectsContentType) {
			return true
		}
	}
	return false
}
