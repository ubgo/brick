package brick

import (
	"errors"
	"fmt"
)

// Component documentation. A component documents itself in
// its own frontmatter, beside the declarations the engine already checks:
// `doc:` and `examples:` on each prop and slot. The engine never reads them
// while rendering; ComponentDocs hands them, with the checked declarations,
// to whatever renders docs (a CLI, a site page), so the docs and the
// contract they describe cannot drift apart.

// Keys a prop or slot declaration accepts for documentation.
const (
	docKey      = "doc"
	examplesKey = "examples"
)

// parseDocs reads `doc:` (text) and `examples:` (a list of snippets) from a
// prop or slot declaration. A number or bool example is kept as written,
// so `examples: [3, 10]` documents an int prop without quoting.
func parseDocs(x map[string]any) (string, []string, error) {
	var doc string
	if v, ok := x[docKey]; ok {
		s, ok := v.(string)
		if !ok {
			return "", nil, errors.New("doc must be text")
		}
		doc = s
	}
	var examples []string
	if v, ok := x[examplesKey]; ok {
		list, ok := v.([]any)
		if !ok {
			return "", nil, errors.New("examples must be a list")
		}
		for _, ex := range list {
			switch ex.(type) {
			case string, bool, int, int64, float64:
				examples = append(examples, fmt.Sprint(ex))
			default:
				return "", nil, fmt.Errorf("examples: %s is not text, a number or a bool", describe(ex))
			}
		}
	}
	return doc, examples, nil
}

// ComponentDoc is one component as its frontmatter declares it: what a
// docs page or `brick docs` prints. Everything comes from the file, checked
// the same way a render checks it.
type ComponentDoc struct {
	// Name is the tag the component is called by (`Card`, or a library's
	// prefixed name); Path is its file in Options.FS.
	Name string `json:"name"`
	Path string `json:"path"`
	// Stateful and Island say how it renders (state:, brick: {island:}).
	Stateful bool `json:"stateful,omitempty"`
	Island   bool `json:"island,omitempty"`
	// Props and Slots are sorted by name; both are empty for a component
	// that declares none.
	Props []PropDoc `json:"props,omitempty"`
	Slots []SlotDoc `json:"slots,omitempty"`
}

// PropDoc is one declared prop. Default is set only when HasDefault is, so
// a declared `default: false` is told apart from no default.
type PropDoc struct {
	Name       string   `json:"name"`
	Type       string   `json:"type"`
	Default    any      `json:"default,omitempty"`
	HasDefault bool     `json:"hasDefault,omitempty"`
	Required   bool     `json:"required,omitempty"`
	OneOf      []any    `json:"oneof,omitempty"`
	Doc        string   `json:"doc,omitempty"`
	Examples   []string `json:"examples,omitempty"`
}

// SlotDoc is one declared slot: `children` is the call's body, any other
// name a `<brick:prop name="…">` block.
type SlotDoc struct {
	Name     string   `json:"name"`
	Required bool     `json:"required,omitempty"`
	Doc      string   `json:"doc,omitempty"`
	Examples []string `json:"examples,omitempty"`
}

// ComponentDocs lists every component the engine can call by name —
// project roots first-wins, then libraries — sorted by name. A component
// whose frontmatter does not parse fails the call with its file, the same
// error a render would give, rather than being left out of the docs.
func (e *Engine) ComponentDocs() ([]ComponentDoc, error) {
	r, err := e.reg()
	if err != nil {
		return nil, err
	}
	names := sortedKeys(r.byName)
	out := make([]ComponentDoc, 0, len(names))
	for _, name := range names {
		en := r.byName[name]
		m, err := e.calleeMeta(en.path)
		if err != nil {
			return nil, err
		}
		out = append(out, componentDoc(name, en.path, m))
	}
	return out, nil
}

// componentDoc turns one file's checked frontmatter into its doc entry.
func componentDoc(name, path string, m *meta) ComponentDoc {
	d := ComponentDoc{Name: name, Path: path, Stateful: m.state != nil, Island: m.island || m.state != nil}
	for _, pn := range m.order {
		p := m.props[pn]
		pd := PropDoc{Name: p.Name, Type: p.Type, HasDefault: p.HasDefault, Required: p.Required, OneOf: p.OneOf, Doc: p.Doc, Examples: p.Examples}
		if p.HasDefault {
			pd.Default = p.Default
		}
		d.Props = append(d.Props, pd)
	}
	for _, sn := range m.slotOrder {
		s := m.slots[sn]
		d.Slots = append(d.Slots, SlotDoc{Name: sn, Required: s.Required, Doc: s.Doc, Examples: s.Examples})
	}
	return d
}
