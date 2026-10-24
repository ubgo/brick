package brick

import (
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"strings"
)

// stateReadRE finds .State.field in a template; stateWholeRE finds .State
// used whole (index .State "x", {{ .State }}), after which no list of read
// keys can be trusted. Both are best effort, like readsChildrenRE: they
// feed a Dev warning, never a decision.
var (
	stateReadRE  = regexp.MustCompile(`\.State\.([A-Za-z_]\w*)`)
	stateWholeRE = regexp.MustCompile(`\.State(?:[^.\w]|$)`)
)

// stateReads lists the top-level .State keys a file's template reads, and
// whether it reads .State whole.
func stateReads(src []byte) (map[string]bool, bool) {
	src = tmplCommentRE.ReplaceAll(src, nil)
	out := map[string]bool{}
	for _, m := range stateReadRE.FindAllSubmatch(src, -1) {
		out[string(m[1])] = true
	}
	return out, stateWholeRE.Match(src)
}

// warnRenderless is the Dev warning for a renderless action
// whose view is now stale: it changed a state key the template reads, or
// returned errors that only a render could show. Skipping the render was
// the author's choice; the warning says what it costs.
func (e *Engine) warnRenderless(c *compiled, inst *Instance, before map[string]any, formErrors map[string]any) {
	if e.opts.Mode != Dev {
		return
	}
	if len(formErrors) > 0 {
		e.warn(Diagnostic{File: c.path, Code: CodeRuntime, Message: fmt.Sprintf("<%s>: a renderless action (brick.skip_render) returned errors; .Errors is only shown by a render", inst.Name)})
	}
	var stale []string
	for _, k := range sortedKeys(inst.State) {
		if reflect.DeepEqual(before[k], inst.State[k]) {
			continue
		}
		if c.readsWholeState || c.stateReads[k] {
			stale = append(stale, k)
		}
	}
	if len(stale) > 0 {
		slices.Sort(stale)
		e.warn(Diagnostic{File: c.path, Code: CodeRuntime, Message: fmt.Sprintf("<%s>: a renderless action (brick.skip_render) changed state the template shows (%s); the page stays stale until the next render", inst.Name, strings.Join(stale, ", "))})
	}
}
