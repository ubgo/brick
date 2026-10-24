package lsp

import (
	"fmt"
	"strings"
)

// Walking the frontmatter JSON Schema (brick.FrontmatterSchema) for
// completion and hover. Only the parts the schema uses are read:
// properties, additionalProperties, items, anyOf, $ref into $defs, enum,
// type and description.

// schemaRefPrefix is how the schema points into its own $defs.
const schemaRefPrefix = "#/$defs/"

// resolve follows a $ref; anything that is not a schema object is nil.
func resolve(root map[string]any, node any) map[string]any {
	m, ok := node.(map[string]any)
	if !ok {
		return nil
	}
	if ref, ok := m["$ref"].(string); ok {
		if name, ok := strings.CutPrefix(ref, schemaRefPrefix); ok {
			if defs, ok := root["$defs"].(map[string]any); ok {
				if d, ok := defs[name].(map[string]any); ok {
					// The node's own description wins over the def's.
					out := map[string]any{}
					for k, v := range d {
						out[k] = v
					}
					if desc, ok := m["description"]; ok {
						out["description"] = desc
					}
					return out
				}
			}
		}
	}
	return m
}

// branches is a node and the object branches of its anyOf, so a key that
// takes `string` or `{type: …}` offers the mapping's keys.
func branches(root, node map[string]any) []map[string]any {
	if node == nil {
		return nil
	}
	out := []map[string]any{node}
	if alts, ok := node["anyOf"].([]any); ok {
		for _, a := range alts {
			if m := resolve(root, a); m != nil {
				out = append(out, branches(root, m)...)
			}
		}
	}
	return out
}

// schemaAt walks path from the root: a key through properties (or
// additionalProperties for a free-named key such as a prop's name), and
// ListItem through items.
func schemaAt(root map[string]any, path []string) map[string]any {
	node := root
	for _, seg := range path {
		var next map[string]any
		for _, b := range branches(root, node) {
			if seg == ListItem {
				next = resolve(root, b["items"])
			} else if props, ok := b["properties"].(map[string]any); ok && props[seg] != nil {
				next = resolve(root, props[seg])
			} else if ap := resolve(root, b["additionalProperties"]); ap != nil {
				next = ap
			}
			if next != nil {
				break
			}
		}
		if next == nil {
			return nil
		}
		node = next
	}
	return node
}

// schemaProperties is the named keys a node (or any of its branches)
// accepts.
func schemaProperties(root, node map[string]any) map[string]any {
	out := map[string]any{}
	for _, b := range branches(root, node) {
		if props, ok := b["properties"].(map[string]any); ok {
			for k, v := range props {
				out[k] = v
			}
		}
	}
	return out
}

// schemaValues is the fixed values a node accepts: its enum, or true and
// false for a boolean.
func schemaValues(root, node map[string]any) []string {
	var out []string
	seen := map[string]bool{}
	add := func(v string) {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	for _, b := range branches(root, node) {
		if enum, ok := b["enum"].([]any); ok {
			for _, v := range enum {
				add(fmt.Sprint(v))
			}
		}
		if b["type"] == "boolean" {
			add("true")
			add("false")
		}
	}
	return out
}

// description is a node's hover text.
func description(node map[string]any) string {
	if d, ok := node["description"].(string); ok {
		return d
	}
	return ""
}
