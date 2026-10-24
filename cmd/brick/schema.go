package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/ubgo/brick"
)

// brick schema prints the JSON Schema of a file's frontmatter
// (brick.FrontmatterSchema), generated from the engine's own key lists, so
// an editor's completion and checks cannot drift from what brick check
// accepts.

const schemaUsage = `brick schema — print the JSON Schema of brick frontmatter

Usage:
  brick schema [-o file]

It prints a JSON Schema (draft 2020-12) for the YAML frontmatter of a
page, layout or component: every engine key (layout, use, props, state,
brick: {island, lazy, query, slots, …}) with its shape and a description.
Keys that are not the engine's are the file's params and are allowed.
The schema checks one file's shape; what needs the whole site (a load:
function that exists, a rules: path declared in state:) stays brick
check's job.

Examples:
  brick schema                                   print it
  brick schema -o .vscode/brick-frontmatter.schema.json
                                                 write it where an editor setting can point
  brick schema | jq '.properties.brick.properties | keys'
                                                 every brick: setting

Editors validate YAML files against it; frontmatter embedded in .html is
not a YAML file to them, so brick lsp (a language server) completes and
checks those, from this same schema and brick check.
`

// schemaFileMode is the mode of a file written with -o.
const schemaFileMode = 0o644

// schemaCmd prints or writes the frontmatter schema.
func schemaCmd(args []string, stdout, stderr io.Writer) int {
	fl := flag.NewFlagSet("brick schema", flag.ContinueOnError)
	fl.SetOutput(stderr)
	fl.Usage = func() { fmt.Fprint(stderr, schemaUsage) }
	out := fl.String("o", "", "write the schema to this file instead of stdout")
	if err := fl.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK
		}
		return exitUsage
	}
	if fl.NArg() > 0 {
		fmt.Fprintf(stderr, "brick schema: takes no arguments\n\n%s", schemaUsage)
		return exitUsage
	}
	b := brick.FrontmatterSchema()
	if *out == "" {
		if _, err := stdout.Write(b); err != nil {
			fmt.Fprintln(stderr, "brick:", err)
			return exitFail
		}
		return exitOK
	}
	if err := os.WriteFile(*out, b, schemaFileMode); err != nil {
		fmt.Fprintln(stderr, "brick:", err)
		return exitFail
	}
	fmt.Fprintln(stdout, "wrote", *out)
	return exitOK
}
