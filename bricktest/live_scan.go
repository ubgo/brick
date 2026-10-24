// Scanning rendered HTML for the stateful elements a LivePage drives.

package bricktest

import (
	"html"
	"regexp"
	"slices"
	"strings"
)

// element is one start tag in held HTML, with its outer range and the
// index of the element it sits in (-1 at the top).
type element struct {
	name       string
	start, end int
	// open is where the start tag ends: the inner HTML is [open, end).
	open   int
	attrs  map[string]string
	parent int
}

func (e element) stateful() bool { return e.attrs["data-brick-state"] != "" }

var (
	liveTagRE  = regexp.MustCompile(`<(/?)([a-zA-Z][a-zA-Z0-9-]*)((?:\s+[^\s=/>]+(?:\s*=\s*(?:"[^"]*"|'[^']*'|[^\s>]+))?)*)\s*(/?)>`)
	liveAttrRE = regexp.MustCompile(`([^\s=/>]+)(?:\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s>]+)))?`)
	liveVoid   = []string{"area", "base", "br", "col", "embed", "hr", "img", "input", "link", "meta", "source", "track", "wbr"}
)

// scan is a small tag walker — enough to find stateful roots and their
// nesting in brick's own deterministic output; it skips script and style
// bodies.
func scan(src string) []element {
	var els []element
	var stack []int
	pos := 0
	for {
		m := liveTagRE.FindStringSubmatchIndex(src[pos:])
		if m == nil {
			break
		}
		for i := range m {
			if m[i] >= 0 {
				m[i] += pos
			}
		}
		pos = m[1]
		closing := m[3] > m[2]
		name := strings.ToLower(src[m[4]:m[5]])
		if closing {
			for i := len(stack) - 1; i >= 0; i-- {
				if els[stack[i]].name == name {
					for _, j := range stack[i:] {
						els[j].end = m[1]
					}
					stack = stack[:i]
					break
				}
			}
			continue
		}
		el := element{name: name, start: m[0], end: m[1], open: m[1], attrs: map[string]string{}, parent: -1}
		if len(stack) > 0 {
			el.parent = stack[len(stack)-1]
		}
		for _, a := range liveAttrRE.FindAllStringSubmatch(src[m[6]:m[7]], -1) {
			v := a[2] + a[3] + a[4]
			el.attrs[strings.ToLower(a[1])] = html.UnescapeString(v)
		}
		els = append(els, el)
		selfClose := m[9] > m[8]
		if name == "script" || name == "style" {
			if end := strings.Index(strings.ToLower(src[pos:]), "</"+name); end >= 0 {
				pos += end
			}
		}
		if !selfClose && !slices.Contains(liveVoid, name) {
			stack = append(stack, len(els)-1)
		}
	}
	return els
}
