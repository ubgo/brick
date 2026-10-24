package brick

import (
	"encoding/json"
	"fmt"
	"html/template"
	"path"
	"strings"
	texttemplate "text/template"
)

// listener is one entry of brick: {on: {event: call}}.
type listener struct {
	// key is the event name as written; tmpl is set when it holds {{ }},
	// executed with the component's scope at every render, so
	// "contact-updated.{{ .Props.id }}" follows the props.
	key  string
	tmpl *texttemplate.Template
	// call is the action run when the event arrives, in the brick:click
	// grammar: a function, ns.fn, name(args) or $refresh.
	call string
}

// upPrefix starts a call-site listener attribute: <Row brick:on:deleted="f">
// runs f on the stateful component around the call when that child
// dispatches deleted (5.3).
const upPrefix = "brick:on:"

// fnUpAttrs writes a component root's call-site listeners as
// data-brick-up; injected on every single-root component, it writes
// nothing unless a caller used brick:on:.
const fnUpAttrs = "__upAttrs"

// parseListeners reads brick: {on:}. Event names and calls are checked
// here; that the calls name the component's own functions is checked by
// checkListeners once the scripts are loaded.
func parseListeners(v any) ([]listener, error) {
	mp, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("brick: on: must map event names to actions (brick: {on: {contact-deleted: $refresh}})")
	}
	var out []listener
	for _, k := range sortedKeys(mp) {
		call, ok := mp[k].(string)
		if !ok {
			return nil, fmt.Errorf("brick: on: %s: the action must be a string, such as $refresh or reload", k)
		}
		l := listener{key: k, call: strings.TrimSpace(call)}
		if strings.Contains(k, "{{") {
			t, err := texttemplate.New("on: " + k).Option("missingkey=zero").Parse(k)
			if err != nil {
				return nil, fmt.Errorf("brick: on: %q: %w", k, err)
			}
			l.tmpl = t
		} else if err := checkEventName(k); err != nil {
			return nil, fmt.Errorf("brick: on: %w", err)
		}
		if err := checkListenerCall(l.call); err != nil {
			return nil, fmt.Errorf("brick: on: %s: %w", k, err)
		}
		out = append(out, l)
	}
	return out, nil
}

// checkListenerCall accepts what a listener may run: $refresh or one of the
// component's functions, with optional literal arguments before the event
// data. $set and $toggle take fixed arguments and $dispatch and $parent are
// client-only, so none of them can listen.
func checkListenerCall(call string) error {
	if err := checkActionCall(call); err != nil {
		return err
	}
	if strings.Contains(call, "{{") {
		return fmt.Errorf("%q: a listener's action is written literally", call)
	}
	name := actionCallRE.FindStringSubmatch(call)[1]
	if strings.HasPrefix(name, "$") && name != actionRefresh {
		return fmt.Errorf("%q: %s cannot be a listener (use $refresh or one of the component's functions)", call, name)
	}
	return nil
}

// checkFunction reports whether call names a function the file can run as
// an action, with a message listing the ones it has.
func checkFunction(c *compiled, call string) error {
	name := actionCallRE.FindStringSubmatch(call)[1]
	if name == actionRefresh {
		return nil
	}
	ns, fn, qualified := strings.Cut(name, ".")
	if !qualified {
		ns, fn = "", name
	}
	mod := c.scripts[ns]
	if mod != nil && !strings.HasPrefix(fn, "_") && contains(mod.Functions(), fn) {
		return nil
	}
	var have []string
	for _, k := range sortedKeys(c.scripts) {
		for _, f := range c.scripts[k].Functions() {
			if k != "" {
				f = k + "." + f
			}
			have = append(have, f)
		}
	}
	if len(have) == 0 {
		have = []string{"none"}
	}
	return fmt.Errorf("%q is not a function of this component (have: %s, $refresh)", name, strings.Join(have, ", "))
}

// checkListeners is the compile check of brick: {on:}: a listener needs an
// instance to run an action on, so on: without state: is an error, and
// each call must name one of the file's functions — a typo fails here,
// never as a silent no-op in the browser.
func checkListeners(c *compiled) error {
	if len(c.meta.on) == 0 {
		return nil
	}
	if c.meta.state == nil {
		return fmt.Errorf("brick: on: needs state: — a listener runs an action on the component's instance (client-only listening is brick.on in <brick:client>)")
	}
	for _, l := range c.meta.on {
		if err := checkFunction(c, l.call); err != nil {
			return fmt.Errorf("brick: on: %s: %w", l.key, err)
		}
	}
	return nil
}

// listenerAttrs writes data-brick-name (on every stateful root, for a
// dispatch's to=) and data-brick-on (when it listens). A dynamic name that
// renders invalid fails in Dev; in Prod that listener is dropped with a
// warning, so a bad prop never breaks the page.
func listenerAttrs(s *Scope) (string, error) {
	out := fmt.Sprintf(` data-brick-name=%q`, componentType(s))
	if len(s.file.meta.on) == 0 {
		return out, nil
	}
	on := make(map[string]string, len(s.file.meta.on))
	for _, l := range s.file.meta.on {
		name := l.key
		if l.tmpl != nil {
			var b strings.Builder
			if err := l.tmpl.Execute(&b, s); err != nil {
				return "", fmt.Errorf("brick: on: %q: %w", l.key, err)
			}
			name = b.String()
			if err := checkEventName(name); err != nil {
				err = fmt.Errorf("brick: on: %q rendered %q: %w", l.key, name, err)
				if s.st.e.opts.Mode == Dev {
					return "", err
				}
				s.st.e.warn(Diagnostic{File: s.file.path, Code: CodeRuntime, Message: err.Error() + "; listener dropped"})
				continue
			}
		}
		on[name] = l.call
	}
	b, err := json.Marshal(on)
	if err != nil {
		return "", err
	}
	return out + ` data-brick-on="` + template.HTMLEscapeString(string(b)) + `"`, nil
}

// componentType is the component's file stem — what data-brick-type and
// data-brick-name carry — never the alias an importer gave it.
func componentType(s *Scope) string {
	typ, _, _ := splitStem(strings.TrimSuffix(path.Base(s.file.path), htmlExt))
	if typ == "" && s.inst != nil {
		typ = s.inst.name
	}
	return typ
}

// upAttrs writes data-brick-up from the call site's brick:on: attributes,
// with its own leading space: it is injected right after the root's tag
// name, so a component no caller listens to renders byte for byte as before.
func upAttrs(s *Scope) (template.HTMLAttr, error) {
	model, err := modelAttrs(s)
	if err != nil || s.inst == nil {
		return model, err
	}
	up := map[string]string{}
	for _, p := range s.inst.attrs {
		if ev, ok := strings.CutPrefix(p.name, upPrefix); ok {
			call, _ := p.value.(string)
			up[ev] = call
		}
	}
	if len(up) == 0 {
		return model, nil
	}
	b, err := json.Marshal(up)
	if err != nil {
		return "", err
	}
	// The value is escaped here; html/template trusts HTMLAttr as a
	// complete, safe attribute sequence.
	return template.HTMLAttr(` data-brick-up="`+template.HTMLEscapeString(string(b))+`"`) + model, nil
}

// hasUp reports whether a call passes call-site listeners.
func hasUp(attrs []propValue) bool {
	for _, p := range attrs {
		if strings.HasPrefix(p.name, upPrefix) {
			return true
		}
	}
	return false
}

// checkUpListener is the compile check of brick:on:<event>="call" on a
// component tag (5.3): a literal, lowercase event name; a call the file
// writing the tag can run, which needs that file to be stateful.
func checkUpListener(c *compiled, call string, name, value string, literal bool) error {
	ev := strings.TrimPrefix(name, upPrefix)
	if err := checkEventName(ev); err != nil {
		return fmt.Errorf("%s%s: %w", upPrefix, ev, err)
	}
	if !literal {
		return fmt.Errorf("%s%s on <%s>: the action is written literally", upPrefix, ev, call)
	}
	if c.meta.state == nil {
		return fmt.Errorf("%s%s on <%s> needs a stateful component around it, and %s declares no state", upPrefix, ev, call, path.Base(c.path))
	}
	if err := checkListenerCall(value); err != nil {
		return fmt.Errorf("%s%s: %w", upPrefix, ev, err)
	}
	if err := checkFunction(c, value); err != nil {
		return fmt.Errorf("%s%s: %w", upPrefix, ev, err)
	}
	return nil
}
