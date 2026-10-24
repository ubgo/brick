package bricktest

import (
	"context"
	"errors"
	"fmt"
	"html"
	"maps"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/ubgo/brick"
)

// Live holds one filled page and its stateful components, so a test runs
// actions and delivers events the way the browser runtime does
// without scraping handles by hand:
//
//	live := bricktest.Live(t, eng, "content/apps/contacts.html")
//	res := live.Component("ContactsApp").Call("contacts.remove", 42)
//	bricktest.AssertDispatched(t, res, "contact-deleted", map[string]any{"id": 42})
//	live.Deliver(res)
//	live.Component("ContactCount").Text() // "127 contacts"
//
// The engine must have a state store (Options.States). Each Call replaces
// the component's held HTML with the action's, as the morph would.
type LivePage struct {
	t    testing.TB
	eng  *brick.Engine
	opts []brick.RenderOption
	html string
	// source remembers which root produced a result, for Deliver.
	source map[*brick.ActionResult]string
	// events is the runtime's per-element memory of brick: attributes
	// (.once fired, brick:intersect seen), keyed by eventKey.
	events map[string]*eventRecord
	// views logs the data every component rendered with, across the
	// page's render, its fill and every Call (AssertViewHas).
	views *brick.ViewLog
}

// LiveRoot is one stateful component on a LivePage, found by its
// data-brick id.
type LiveRoot struct {
	live *LivePage
	id   string
}

// Delivery guards, the runtime's constants.
const (
	maxEventHops    = 4
	maxEventFanOut  = 50
	eventChainSplit = ">"
)

// Live renders and fills page (a file path such as "content/a.html").
// opts are the render options for the fill and every action (WithSession,
// Provide; brick.WithLazyInline to render lazy islands in the page). Live
// adds its own brick.WithViewLog after them, for Views and AssertViewHas.
func Live(t testing.TB, eng *brick.Engine, page string, opts ...brick.RenderOption) *LivePage {
	t.Helper()
	ctx := context.Background()
	views := &brick.ViewLog{}
	opts = append(slices.Clip(opts), brick.WithViewLog(views))
	r, err := eng.RenderChunks(ctx, page, nil, opts...)
	if err != nil {
		t.Fatalf("bricktest.Live: render %s: %v", page, err)
	}
	b, err := eng.Fill(ctx, r, opts...)
	if err != nil {
		t.Fatalf("bricktest.Live: fill %s: %v", page, err)
	}
	return &LivePage{t: t, eng: eng, opts: opts, html: string(b), source: map[*brick.ActionResult]string{}, views: views}
}

// Views returns the data each render of the component name saw, oldest
// first: the page's render (or its island's fill), then each Call's
// re-render.
func (l *LivePage) Views(name string) []brick.ComponentView {
	var out []brick.ComponentView
	for _, v := range l.views.Views() {
		if v.Name == name {
			out = append(out, v)
		}
	}
	return out
}

// AssertViewHas fails unless the latest render of component saw key with
// the value want (compared as JSON, so 3 matches int64(3)): key is looked
// up in the props the template saw, which include the loader's output.
// After a Call, the latest render is the action's.
func (l *LivePage) AssertViewHas(t testing.TB, component, key string, want any) {
	t.Helper()
	views := l.Views(component)
	if len(views) == 0 {
		t.Fatalf("bricktest: AssertViewHas: <%s> did not render", component)
	}
	v := views[len(views)-1]
	got, ok := v.Props[key]
	if !ok {
		t.Fatalf("bricktest: AssertViewHas: <%s> rendered without %q; it saw %v", component, key, slices.Sorted(maps.Keys(v.Props)))
	}
	if !sameJSON(got, want) {
		t.Fatalf("bricktest: AssertViewHas: <%s> saw %s = %#v, want %#v", component, key, got, want)
	}
}

// HTML is the page as it stands after every Call.
func (l *LivePage) HTML() string { return l.html }

// Component returns the first stateful root whose component is name (its
// file stem, as data-brick-name carries it); the test fails when none is.
func (l *LivePage) Component(name string) *LiveRoot {
	l.t.Helper()
	for _, el := range scan(l.html) {
		if el.stateful() && el.attrs["data-brick-name"] == name {
			return &LiveRoot{live: l, id: el.attrs["data-brick"]}
		}
	}
	l.t.Fatalf("bricktest: no stateful <%s> on the page", name)
	return nil
}

// Components returns every stateful root of the component name, in
// document order.
func (l *LivePage) Components(name string) []*LiveRoot {
	var out []*LiveRoot
	for _, el := range scan(l.html) {
		if el.stateful() && el.attrs["data-brick-name"] == name {
			out = append(out, &LiveRoot{live: l, id: el.attrs["data-brick"]})
		}
	}
	return out
}

// element finds the root's element in the current HTML.
func (r *LiveRoot) element() (element, bool) {
	for _, el := range scan(r.live.html) {
		if el.stateful() && el.attrs["data-brick"] == r.id {
			return el, true
		}
	}
	return element{}, false
}

// directChildren returns the stateful roots inside el with no other
// stateful root between them and el.
func (l *LivePage) directChildren(el element) []element {
	els := scan(l.html)
	var out []element
	for _, c := range els {
		if !c.stateful() || c.start <= el.start || c.end > el.end {
			continue
		}
		p := c.parent
		for p >= 0 && !els[p].stateful() {
			p = els[p].parent
		}
		if p >= 0 && els[p].start == el.start {
			out = append(out, c)
		}
	}
	return out
}

// ID is the root's data-brick id, which tells two instances of one
// component apart.
func (r *LiveRoot) ID() string { return r.id }

// Name is the root's component name, as data-brick-name carries it.
func (r *LiveRoot) Name() string {
	r.live.t.Helper()
	el, ok := r.element()
	if !ok {
		r.live.t.Fatalf("bricktest: component %s is no longer on the page", r.id)
	}
	return el.attrs["data-brick-name"]
}

// Children returns the stateful components directly inside this one (no
// other stateful root between), in document order. They are the instances an action on this root keeps.
func (r *LiveRoot) Children() []*LiveRoot {
	r.live.t.Helper()
	el, ok := r.element()
	if !ok {
		r.live.t.Fatalf("bricktest: component %s is no longer on the page", r.id)
	}
	var out []*LiveRoot
	for _, c := range r.live.directChildren(el) {
		out = append(out, &LiveRoot{live: r.live, id: c.attrs["data-brick"]})
	}
	return out
}

// Child returns the first stateful component named name anywhere inside
// this one, at any depth. The test fails when
// there is none, so a missing child is never a nil handle used later.
func (r *LiveRoot) Child(name string) *LiveRoot {
	r.live.t.Helper()
	el, ok := r.element()
	if !ok {
		r.live.t.Fatalf("bricktest: component %s is no longer on the page", r.id)
	}
	for _, c := range scan(r.live.html) {
		if c.stateful() && c.start > el.start && c.end <= el.end && c.attrs["data-brick-name"] == name {
			return &LiveRoot{live: r.live, id: c.attrs["data-brick"]}
		}
	}
	r.live.t.Fatalf("bricktest: no stateful <%s> inside %s", name, r.id)
	return nil
}

// HTML is the root's current outer HTML.
func (r *LiveRoot) HTML() string {
	r.live.t.Helper()
	el, ok := r.element()
	if !ok {
		r.live.t.Fatalf("bricktest: component %s is no longer on the page", r.id)
	}
	return r.live.html[el.start:el.end]
}

var tagStripRE = regexp.MustCompile(`<[^>]*>`)

// Text is the root's text content, tags removed, entities decoded and
// whitespace collapsed.
func (r *LiveRoot) Text() string {
	return strings.Join(strings.Fields(html.UnescapeString(tagStripRE.ReplaceAllString(r.HTML(), " "))), " ")
}

// Call runs action on this component with args, through the engine's
// whole pipeline (middleware, token, store), and replaces the held HTML.
// The test fails on an error; TryCall returns it.
func (r *LiveRoot) Call(action string, args ...any) *brick.ActionResult {
	r.live.t.Helper()
	res, err := r.TryCall(action, args...)
	if err != nil {
		r.live.t.Fatalf("bricktest: %s on %s: %v", action, r.id, err)
	}
	return res
}

// TryCall is Call returning the action's error instead of failing.
func (r *LiveRoot) TryCall(action string, args ...any) (*brick.ActionResult, error) {
	return r.tryCall(context.Background(), action, args, nil)
}

// StreamChunk is one piece of text an action streamed.
type StreamChunk struct {
	Ref     string
	Text    string
	Replace bool
}

// chunkSink collects an action's stream.
type chunkSink struct{ chunks []StreamChunk }

func (s *chunkSink) Write(ref, text string, replace bool) error {
	s.chunks = append(s.chunks, StreamChunk{Ref: ref, Text: text, Replace: replace})
	return nil
}

// Stream is Call that also returns the text the action streamed, in
// order, as the runtime would apply it to the brick:ref elements.
func (r *LiveRoot) Stream(action string, args ...any) (*brick.ActionResult, []StreamChunk) {
	r.live.t.Helper()
	sink := &chunkSink{}
	res, err := r.tryCall(brick.WithActionStream(context.Background(), sink), action, args, nil)
	if err != nil {
		r.live.t.Fatalf("bricktest: %s on %s: %v", action, r.id, err)
	}
	return res, sink.chunks
}

func (r *LiveRoot) tryCall(ctx context.Context, action string, args []any, sets []brick.FieldSet) (*brick.ActionResult, error) {
	el, ok := r.element()
	if !ok {
		return nil, fmt.Errorf("bricktest: component %s is no longer on the page", r.id)
	}
	// The stateful children the page holds go with the call, as the
	// runtime sends them, and each <brick-keep> in the answer is replaced
	// by the child's held HTML.
	kids := r.live.directChildren(el)
	children := map[string]brick.ChildRef{}
	for _, k := range kids {
		children[k.attrs["data-brick"]] = brick.ChildRef{Handle: k.attrs["data-brick-state"], Token: k.attrs["data-brick-token"]}
	}
	res, err := r.live.eng.RunAction(ctx, brick.ActionRequest{
		Handle: el.attrs["data-brick-state"], Token: el.attrs["data-brick-token"], Action: action, Args: args, Children: children, Sets: sets,
	}, r.live.opts...)
	if err != nil {
		return nil, err
	}
	r.live.source[res] = r.id
	if res.SkipRender {
		// A renderless action (brick.skip_render) answers no HTML and the
		// runtime keeps the element as it is, so the held HTML stays.
		return res, nil
	}
	out := string(res.HTML)
	for _, k := range kids {
		out = strings.Replace(out, `<brick-keep data-brick="`+k.attrs["data-brick"]+`"></brick-keep>`, r.live.html[k.start:k.end], 1)
	}
	r.live.html = r.live.html[:el.start] + out + r.live.html[el.end:]
	return res, nil
}

// Set runs $set(field, value).
func (r *LiveRoot) Set(field string, value any) *brick.ActionResult {
	r.live.t.Helper()
	return r.Call("$set", field, value)
}

// View is the root's instance as its store holds it now, read through
// Engine.Instance with the page's render options (so WithSession opens a
// sealed handle): state, touched fields and their rule errors.
func (r *LiveRoot) View() *brick.InstanceView {
	r.live.t.Helper()
	el, ok := r.element()
	if !ok {
		r.live.t.Fatalf("bricktest: component %s is no longer on the page", r.id)
	}
	v, err := r.live.eng.Instance(context.Background(), el.attrs["data-brick-state"], r.live.opts...)
	if err != nil {
		r.live.t.Fatalf("bricktest: instance of %s: %v", r.id, err)
	}
	return v
}

// State is a copy of the root's current state; changing it changes
// nothing.
func (r *LiveRoot) State() map[string]any {
	r.live.t.Helper()
	return r.View().State
}

// Dump writes the page as it stands to a temporary file, logs its path and
// returns it, for opening a failing test's page in a browser. The file is
// left in place on purpose.
func (l *LivePage) Dump() string {
	l.t.Helper()
	f, err := os.CreateTemp("", "bricktest-*.html")
	if err != nil {
		l.t.Fatalf("bricktest: Dump: %v", err)
	}
	_, werr := f.WriteString(l.html)
	if err := errors.Join(werr, f.Close()); err != nil {
		l.t.Fatalf("bricktest: Dump: %v", err)
	}
	l.t.Logf("bricktest: page dumped to %s", f.Name())
	return f.Name()
}
