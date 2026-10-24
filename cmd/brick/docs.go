package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/ubgo/brick"
	"github.com/ubgo/brick/site"
)

// brick docs prints every component's contract — props and slots with
// their types, defaults and docs — from the components' own frontmatter
// (brick.Engine.ComponentDocs), so the listing cannot drift from what the
// engine checks.

const docsUsage = `brick docs — list every component with its props and slots

Usage:
  brick docs [-json] [dir]

It loads the site in dir (default: the current directory) the way check
and build do, brick.yaml and vendored libraries included, and prints each
component the site can call: its file, how it renders (stateful, island),
each prop with its type, default, required flag, allowed values, doc and
examples, and each slot. A component whose frontmatter does not parse is
an error, exit 1.

Examples:
  brick docs                          read every component's contract
  brick docs site | less              a site in another folder
  brick docs -json | jq '.[] | select(.name == "Card")'
                                      one component, machine-readable

-json prints the list as JSON with markup in examples left unescaped.
`

// docsIndent is the nesting step of the text listing.
const docsIndent = "  "

// docs prints the components of s.
func docs(s *site.Site, asJSON bool, stdout, stderr io.Writer) int {
	list, err := s.Engine().ComponentDocs()
	if err != nil {
		fmt.Fprintln(stderr, "brick:", err)
		return exitFail
	}
	if asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		// Examples are markup (<Card title="x" />); escaping < and > as
		// < would make them unreadable for no gain outside a <script>.
		enc.SetEscapeHTML(false)
		if list == nil {
			list = []brick.ComponentDoc{}
		}
		if err := enc.Encode(list); err != nil {
			fmt.Fprintln(stderr, "brick:", err)
			return exitFail
		}
		return exitOK
	}
	for i, c := range list {
		if i > 0 {
			fmt.Fprintln(stdout)
		}
		writeComponentDoc(stdout, c)
	}
	return exitOK
}

// writeComponentDoc writes one component as indented text.
func writeComponentDoc(w io.Writer, c brick.ComponentDoc) {
	var kinds []string
	if c.Stateful {
		kinds = append(kinds, "stateful")
	}
	if c.Island {
		kinds = append(kinds, "island")
	}
	head := c.Name + "  " + c.Path
	if len(kinds) > 0 {
		head += "  (" + strings.Join(kinds, ", ") + ")"
	}
	fmt.Fprintln(w, head)
	if len(c.Props) > 0 {
		fmt.Fprintln(w, docsIndent+"props:")
		for _, p := range c.Props {
			line := []string{p.Name, p.Type}
			if p.Required {
				line = append(line, "required")
			}
			if p.HasDefault {
				line = append(line, "default "+docValue(p.Default))
			}
			if len(p.OneOf) > 0 {
				vals := make([]string, len(p.OneOf))
				for i, v := range p.OneOf {
					vals[i] = docValue(v)
				}
				line = append(line, "one of "+strings.Join(vals, ", "))
			}
			fmt.Fprintln(w, docsIndent+docsIndent+strings.Join(line, "  "))
			writeDocText(w, p.Doc, p.Examples)
		}
	}
	if len(c.Slots) > 0 {
		fmt.Fprintln(w, docsIndent+"slots:")
		for _, sl := range c.Slots {
			line := sl.Name
			if sl.Required {
				line += "  required"
			}
			fmt.Fprintln(w, docsIndent+docsIndent+line)
			writeDocText(w, sl.Doc, sl.Examples)
		}
	}
}

// writeDocText writes a prop's or slot's doc and examples under it.
func writeDocText(w io.Writer, doc string, examples []string) {
	pad := strings.Repeat(docsIndent, 3)
	for l := range strings.SplitSeq(strings.TrimSpace(doc), "\n") {
		if l != "" {
			fmt.Fprintln(w, pad+l)
		}
	}
	for _, ex := range examples {
		fmt.Fprintln(w, pad+"e.g. "+strings.ReplaceAll(ex, "\n", "\n"+pad+"     "))
	}
}

// docValue shows a default or allowed value: strings quoted, so an empty
// default is visible.
func docValue(v any) string {
	if s, ok := v.(string); ok {
		return fmt.Sprintf("%q", s)
	}
	return fmt.Sprint(v)
}
