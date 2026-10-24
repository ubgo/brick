// The small CSS selector subset LiveRoot and LivePage queries accept: its parser and matchers.

package bricktest

import (
	"fmt"
	"slices"
	"strings"
)

// selector is a descendant chain of compounds, outermost first.
type selector []compound

type compound struct {
	tag     string
	ids     []string
	classes []string
	attrs   []attrTest
}

type attrTest struct {
	name, value string
	hasValue    bool
}

// parseSelector reads the CSS subset Click and Submit take; anything else
// (combinators other than a space, pseudo-classes, lists) is an error
// rather than a silent mismatch.
func parseSelector(s string) (selector, error) {
	var out selector
	for _, part := range strings.Fields(s) {
		var c compound
		rest := part
		for rest != "" {
			switch rest[0] {
			case '#', '.':
				n := 1 + strings.IndexFunc(rest[1:], func(r rune) bool { return strings.ContainsRune("#.[", r) })
				if n == 0 {
					n = len(rest)
				}
				if n == 1 {
					return nil, fmt.Errorf("empty %q", rest[:1])
				}
				if rest[0] == '#' {
					c.ids = append(c.ids, rest[1:n])
				} else {
					c.classes = append(c.classes, rest[1:n])
				}
				rest = rest[n:]
			case '[':
				end := strings.IndexByte(rest, ']')
				if end < 0 {
					return nil, fmt.Errorf("unclosed [")
				}
				body := strings.ReplaceAll(rest[1:end], `\:`, ":")
				name, value, has := strings.Cut(body, "=")
				name = strings.ToLower(strings.TrimSpace(name))
				if name == "" || strings.ContainsAny(name, "~|^$*") {
					return nil, fmt.Errorf("unsupported attribute test [%s]", rest[1:end])
				}
				c.attrs = append(c.attrs, attrTest{name: name, value: strings.Trim(strings.TrimSpace(value), `"'`), hasValue: has})
				rest = rest[end+1:]
			default:
				if c.tag != "" || strings.ContainsAny(rest[:1], ">+~:,*()") {
					return nil, fmt.Errorf("unsupported syntax at %q (tag, #id, .class, [attr], [attr=value] and spaces only)", rest)
				}
				n := strings.IndexAny(rest, "#.[")
				if n < 0 {
					n = len(rest)
				}
				c.tag = strings.ToLower(rest[:n])
				if strings.ContainsAny(c.tag, ">+~:,*()") {
					return nil, fmt.Errorf("unsupported syntax in %q (tag, #id, .class, [attr], [attr=value] and spaces only)", c.tag)
				}
				rest = rest[n:]
			}
		}
		out = append(out, c)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("empty selector")
	}
	return out, nil
}

// matches reports whether element i matches the last compound and has
// ancestors matching the others, in order.
func (s selector) matches(els []element, i int) bool {
	if !s[len(s)-1].matches(els[i]) {
		return false
	}
	k := len(s) - 2
	for p := els[i].parent; p >= 0 && k >= 0; p = els[p].parent {
		if s[k].matches(els[p]) {
			k--
		}
	}
	return k < 0
}

func (c compound) matches(el element) bool {
	if c.tag != "" && c.tag != el.name {
		return false
	}
	for _, id := range c.ids {
		if el.attrs["id"] != id {
			return false
		}
	}
	classes := strings.Fields(el.attrs["class"])
	for _, cl := range c.classes {
		if !slices.Contains(classes, cl) {
			return false
		}
	}
	for _, a := range c.attrs {
		v, ok := el.attrs[a.name]
		if !ok || (a.hasValue && v != a.value) {
			return false
		}
	}
	return true
}
