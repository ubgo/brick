// What a test does to a live component: click, submit, and the form fields and client dispatch behind them.

package bricktest

import (
	"cmp"
	"context"
	"fmt"
	"html"
	"maps"
	"slices"
	"strings"

	"github.com/ubgo/brick"
)

// Click clicks the first element under this root matching selector, as
// the runtime handles the click: the nearest brick:click on it or above
// it runs on the stateful root around that element ($parent.<name> on the
// one above), after a brick:confirm says yes. A $dispatch(...) call is
// returned as a result holding that one dispatch effect, which Deliver
// delivers like an action's. Modifiers resolve as in the runtime (see
// fire): a second click on a .once element returns nil. The test fails on
// an unknown selector, an element with no brick:click, or the action's
// error.
//
// selector is a small CSS subset: tag, #id, .class, [attr], [attr=value]
// (quoted or not), combined into compounds and separated by spaces for
// descendants: `button.primary`, `li[data-id=3] [brick:click]`.
func (r *LiveRoot) Click(selector string) *brick.ActionResult {
	r.live.t.Helper()
	l := r.live
	els := scan(l.html)
	j, a := closestWith(els, r.find(els, selector), "click", func(eventAttr) bool { return true })
	if j < 0 {
		l.t.Fatalf("bricktest: %q in %s has no brick:click on it or above it", selector, r.id)
	}
	if prompts(els[j]) {
		l.t.Fatalf("bricktest: %q in %s has brick:confirm.prompt: use ClickPrompt(selector, answer), as the visitor must answer", selector, r.id)
	}
	x, has := a.clickArg(ClickDetails{Count: 1})
	return l.fire(els, j, a, x, has)
}

// ClickPrompt is Click for an element with brick:confirm.prompt, answered
// with answer: the runtime appends the visitor's text as the call's last
// argument. A cancelled prompt sends nothing, so there is
// nothing to test for it here. The test fails as Click does, and when the
// element has no brick:confirm.prompt.
func (r *LiveRoot) ClickPrompt(selector, answer string) *brick.ActionResult {
	r.live.t.Helper()
	l := r.live
	els := scan(l.html)
	j, a := closestWith(els, r.find(els, selector), "click", func(eventAttr) bool { return true })
	if j < 0 {
		l.t.Fatalf("bricktest: %q in %s has no brick:click on it or above it", selector, r.id)
	}
	if !prompts(els[j]) {
		l.t.Fatalf("bricktest: %q in %s has no brick:confirm.prompt; use Click", selector, r.id)
	}
	return l.fire(els, j, a, answer, true)
}

// prompts reports an element whose brick:confirm asks for text.
func prompts(el element) bool {
	for _, c := range attrsOf(el, "confirm") {
		if c.flags["prompt"] {
			return true
		}
	}
	return false
}

// Submit submits the form under this root matching formSelector, as the
// runtime does: the form's named fields (their rendered values, then
// values over them, every value a string as FormData gives it) are the
// action's last argument when there are any, and each brick:model field
// of the root named in values rides with the action as a deferred set
// (keeping its Go type, as the runtime sends a number or checkbox typed).
// The test fails on an unknown selector, a form without brick:submit, a
// name in values that is neither a field of the form nor a brick:model
// path of the component, or the action's error; nil means a .once form
// already submitted.
func (r *LiveRoot) Submit(formSelector string, values map[string]any) *brick.ActionResult {
	r.live.t.Helper()
	return r.submit(formSelector, "", values)
}

// SubmitWith is Submit as if the submit button matching buttonSelector
// (inside the form) was pressed: a named button's name and value go with
// the fields, before values, as FormData(form, submitter) sends them, so
// an action can tell "save" from "delete". The test fails
// when the button is not a submit button of the form: a type=button or
// reset, a disabled one (the browser would not submit), or an image
// button, whose click coordinates a test cannot supply.
func (r *LiveRoot) SubmitWith(formSelector, buttonSelector string, values map[string]any) *brick.ActionResult {
	r.live.t.Helper()
	if buttonSelector == "" {
		r.live.t.Fatalf("bricktest: SubmitWith needs a button selector; use Submit for none")
	}
	return r.submit(formSelector, buttonSelector, values)
}

// submit is Submit, with the submitter's selector or "".
func (r *LiveRoot) submit(formSelector, buttonSelector string, values map[string]any) *brick.ActionResult {
	r.live.t.Helper()
	l := r.live
	els := scan(l.html)
	i := r.find(els, formSelector)
	form := els[i]
	a, ok := brickAttr(form, "submit")
	if form.name != "form" || !ok {
		l.t.Fatalf("bricktest: %q in %s is not a <form> with brick:submit", formSelector, r.id)
	}
	var submitter *element
	if buttonSelector != "" {
		b := els[r.find(els, buttonSelector)]
		if b.start <= form.start || b.end > form.end {
			l.t.Fatalf("bricktest: SubmitWith: %q is not inside the form %q", buttonSelector, formSelector)
		}
		if why := notSubmitter(b); why != "" {
			l.t.Fatalf("bricktest: SubmitWith: %q %s", buttonSelector, why)
		}
		submitter = &b
	}
	if !l.once(els, i, attrsOf(form, "submit")[0]) {
		return nil
	}
	name, args, err := parseCall(a)
	if err != nil {
		l.t.Fatalf("bricktest: brick:submit %q: %v", a, err)
	}
	root, name := l.target(els, i, name)
	rootEl, _ := root.element()
	fields, names := l.formFields(els, form)
	if submitter != nil {
		if n := submitter.attrs["name"]; n != "" {
			fields[n] = submitter.attrs["value"]
		}
	}
	models := map[string]bool{}
	for _, el := range els {
		if el.start >= rootEl.start && el.end <= rootEl.end {
			if f, ok := brickAttr(el, "model"); ok {
				models[f] = true
			}
		}
	}
	var sets []brick.FieldSet
	for _, k := range slices.Sorted(maps.Keys(values)) {
		named := names[k]
		if !named && !models[k] {
			l.t.Fatalf("bricktest: Submit: %q is neither a named field of the form nor a brick:model path of %s", k, root.id)
		}
		if named {
			fields[k] = fmt.Sprint(values[k])
		}
		if models[k] {
			sets = append(sets, brick.FieldSet{Path: k, Value: values[k]})
		}
	}
	if len(fields) > 0 {
		m := make(map[string]any, len(fields))
		for k, v := range fields {
			m[k] = v
		}
		args = append(args, m)
	}
	return root.call(name, args, sets)
}

// notSubmitter says why el cannot submit its form, or "" when it can: a
// <button> whose type is submit (the default) or an <input type=submit>,
// not disabled.
func notSubmitter(el element) string {
	typ := strings.ToLower(el.attrs["type"])
	switch {
	case el.name == "input" && typ == "image":
		return "is an image button: its click coordinates go with the post, which a test cannot supply"
	case el.name == "button" && (typ == "" || typ == "submit"), el.name == "input" && typ == "submit":
	default:
		return "is not a submit button (a <button> of type submit or an <input type=submit>)"
	}
	if _, off := el.attrs["disabled"]; off {
		return "is disabled, so the browser would not submit with it"
	}
	return ""
}

// call runs one action on the root, failing the test on an error.
func (r *LiveRoot) call(action string, args []any, sets []brick.FieldSet) *brick.ActionResult {
	r.live.t.Helper()
	res, err := r.tryCall(context.Background(), action, args, sets)
	if err != nil {
		r.live.t.Fatalf("bricktest: %s on %s: %v", action, r.id, err)
	}
	return res
}

// find returns the index of the first element under the root matching
// selector, failing the test when there is none.
func (r *LiveRoot) find(els []element, selector string) int {
	r.live.t.Helper()
	sel, err := parseSelector(selector)
	if err != nil {
		r.live.t.Fatalf("bricktest: selector %q: %v", selector, err)
	}
	el, ok := r.element()
	if !ok {
		r.live.t.Fatalf("bricktest: component %s is no longer on the page", r.id)
	}
	for i, c := range els {
		if c.start >= el.start && c.end <= el.end && sel.matches(els, i) {
			return i
		}
	}
	r.live.t.Fatalf("bricktest: nothing in %s matches %q", r.id, selector)
	return -1
}

// target is the runtime's: the stateful root around element i (itself
// included), or for "$parent.<name>" the stateful root above that one.
func (l *LivePage) target(els []element, i int, action string) (*LiveRoot, string) {
	l.t.Helper()
	root := closestStateful(els, i)
	if root >= 0 && strings.HasPrefix(action, "$parent.") {
		root = closestStateful(els, els[root].parent)
		action = strings.TrimPrefix(action, "$parent.")
	}
	if root < 0 {
		l.t.Fatalf("bricktest: %s: no stateful component around the element", action)
	}
	return &LiveRoot{live: l, id: els[root].attrs["data-brick"]}, action
}

func closestStateful(els []element, i int) int {
	for ; i >= 0; i = els[i].parent {
		if els[i].stateful() {
			return i
		}
	}
	return -1
}

// clientDispatch turns $dispatch(name, data, to) on element i into a
// result Deliver delivers from the root around it.
func (l *LivePage) clientDispatch(els []element, i int, args []any) *brick.ActionResult {
	l.t.Helper()
	ef := brick.Effect{Kind: brick.EffectDispatch}
	ok := len(args) >= 1 && len(args) <= 3
	if ok {
		ef.Name, ok = args[0].(string)
	}
	if ok && len(args) >= 2 && args[1] != nil {
		ef.Data, ok = args[1].(map[string]any)
	}
	if ok && len(args) == 3 {
		ef.To, ok = args[2].(string)
	}
	if !ok {
		l.t.Fatalf("bricktest: $dispatch takes (name, data, to): %v", args)
	}
	res := &brick.ActionResult{Effects: []brick.Effect{ef}}
	if root := closestStateful(els, i); root >= 0 {
		l.source[res] = els[root].attrs["data-brick"]
	}
	return res
}

// brickAttr returns el's brick:<kind> attribute value, with or without
// modifiers (brick:click.once), as the runtime's attrsOf finds it; the
// first by name when there are several.
func brickAttr(el element, kind string) (string, bool) {
	base := "brick:" + kind
	if v, ok := el.attrs[base]; ok {
		return v, true
	}
	for _, k := range slices.Sorted(maps.Keys(el.attrs)) {
		if strings.HasPrefix(k, base+".") {
			return el.attrs[k], true
		}
	}
	return "", false
}

// formFields is what new FormData(form) holds as the server rendered it,
// by name: enabled named controls, a checkbox or radio only when checked
// ("on" without a value), a select's selected option (else its first),
// a textarea's text. Buttons and file inputs are left out. names are
// every field name, checked or not.
func (l *LivePage) formFields(els []element, form element) (out map[string]string, names map[string]bool) {
	out, names = map[string]string{}, map[string]bool{}
	for i, el := range els {
		if el.start <= form.start || el.end > form.end {
			continue
		}
		name, named := el.attrs["name"]
		// Names starting with "brick:" are the engine's no-JavaScript
		// fallback, which the runtime leaves out too.
		if _, off := el.attrs["disabled"]; !named || name == "" || off || strings.HasPrefix(name, "brick:") {
			continue
		}
		if el.name == "input" || el.name == "textarea" || el.name == "select" {
			names[name] = true
		}
		switch el.name {
		case "input":
			switch strings.ToLower(el.attrs["type"]) {
			case "submit", "button", "reset", "image", "file":
				delete(names, name)
			case "checkbox", "radio":
				// An unchecked box is absent from FormData, but its name
				// is still a field the test may set.
				names[name] = true
				if _, on := el.attrs["checked"]; on {
					out[name] = cmp.Or(el.attrs["value"], "on")
				}
			default:
				out[name] = el.attrs["value"]
			}
		case "textarea":
			out[name] = html.UnescapeString(l.inner(el))
		case "select":
			first, picked := "", ""
			for j := i + 1; j < len(els) && els[j].end <= el.end; j++ {
				if els[j].name != "option" {
					continue
				}
				v, ok := els[j].attrs["value"]
				if !ok {
					v = strings.TrimSpace(html.UnescapeString(tagStripRE.ReplaceAllString(l.inner(els[j]), "")))
				}
				if first == "" {
					first = v
				}
				if _, sel := els[j].attrs["selected"]; sel && picked == "" {
					picked = v
				}
			}
			out[name] = cmp.Or(picked, first)
		}
	}
	return out, names
}

// inner is el's inner HTML, without its closing tag.
func (l *LivePage) inner(el element) string {
	body := l.html[el.open:el.end]
	if i := strings.LastIndex(body, "</"); i >= 0 {
		body = body[:i]
	}
	return body
}
