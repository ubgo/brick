package brick

import (
	"fmt"
)

// Schema is a set of field declarations in the `props:` grammar, usable on
// any map of values — a component's props, or a section's frontmatter in a
// content collection.
type Schema struct {
	decls map[string]*propDecl
	order []string
}

// ParseSchema reads declarations written like a component's `props:`
// block: `title: string` or `{type, default, required, oneof}`.
func ParseSchema(v any) (*Schema, error) {
	decls, err := parseProps(v, nil)
	if err != nil {
		return nil, err
	}
	return &Schema{decls: decls, order: sortedKeys(decls)}, nil
}

// Validate checks values against the schema and returns one message per
// problem, in field order; nil means valid. Undeclared keys are allowed,
// the same rule as undeclared props.
func (s *Schema) Validate(values map[string]any) []string {
	var out []string
	for _, name := range s.order {
		d := s.decls[name]
		v, ok := values[name]
		if !ok || v == nil {
			if d.Required && !d.HasDefault {
				out = append(out, fmt.Sprintf("%s is required", name))
			}
			continue
		}
		if _, err := checkProp(d, v); err != nil {
			out = append(out, fmt.Sprintf("%s: %v", name, err))
			continue
		}
		// A nested shape (fields:/of:) is checked like a component's
		// literal default, except that extra keys are allowed: the same
		// rule as undeclared top-level keys (G15).
		if d.hasShape() {
			out = append(out, fitValue(v, d, name, false)...)
		}
	}
	return out
}
