// Command components-frontmatter writes docs/reference/frontmatter.md, the
// table of every frontmatter key and brick: setting. The keys, their value
// shapes and their descriptions come from brick.FrontmatterSchema(), the
// JSON Schema `brick schema` prints, which is built from the same key lists
// the engine parses; only where a key applies, its default and its guide
// link are written here, in notes. The test fails when the schema gains or
// loses a key that notes does not describe, or when the committed page
// differs from what this program prints.
//
//	go run . > ../../docs/reference/frontmatter.md      regenerate the page
//	go test .                                           check it is current
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"slices"
	"sort"
	"strings"

	"github.com/ubgo/brick"
)

// note is what the schema cannot say about a key: which files it applies
// to, its default, and where it is explained.
type note struct {
	where string
	def   string
	guide string
}

// Guide pages the notes link to, relative to docs/reference/.
const (
	gProps   = "../guides/components/props.md"
	gSlots   = "../guides/components/slots.md"
	gLayouts = "../guides/components/layouts-and-data.md"
	gLoaders = "../guides/components/loaders.md"
	gStyles  = "../guides/components/styles-and-client.md"
	gErrors  = "../guides/components/errors.md"
	gGuide   = "../guide.md"
)

// Where a key applies.
const (
	wAny       = "any file"
	wPage      = "page"
	wComponent = "component"
	wStateful  = "stateful component"
	wPageLay   = "page, layout"
)

// topNotes describes the top-level keys.
var topNotes = map[string]note{
	"brick":        {wAny, "none", "[brick: settings](#brick-settings)"},
	"cache":        {wPage, "none", gGuide + "#a-whole-site--urls-serving-building"},
	"client":       {"reserved", "—", gStyles},
	"dedent":       {wComponent, "`true`", gSlots},
	"inject":       {wComponent, "none", gLayouts},
	"layout":       {wPageLay, "`layouts/default.html` when it exists, else none", gLayouts},
	"load":         {wAny, "none", gLoaders},
	"passthrough":  {wAny, "`Options.Passthrough`", gProps + "#step-6--choose-how-a-files-tags-are-read"},
	"props":        {wComponent, "none (undeclared props still pass through)", gProps},
	"provide":      {wAny, "none", gLayouts},
	"raw":          {wComponent, "`false`", gSlots},
	"state":        {wStateful, "none (not stateful)", gGuide + "#actions--a-button-that-changes-server-state"},
	"strictParams": {wPage, "`Options.StrictParams` (`false`)", gLayouts},
	"strictProps":  {wComponent, "`false`", gProps},
	"syntax":       {wAny, "`Options.Syntax` (`both`)", gProps + "#step-6--choose-how-a-files-tags-are-read"},
	"unknown":      {wAny, "`Options.Unknown` (`error`)", gProps + "#step-6--choose-how-a-files-tags-are-read"},
	"use":          {wAny, "none", gLoaders},
}

// brickNotes describes the keys of the brick: map.
var brickNotes = map[string]note{
	"css":         {wComponent, "`global`", gStyles},
	"init":        {wStateful, "none", gGuide + "#server-state--replies-first-values-the-url-and-memory"},
	"island":      {wComponent, "`false`", gGuide + "#islands--per-request-parts-of-a-cached-page"},
	"lazy":        {wComponent, "none (the call site's `brick:lazy`)", gGuide + "#lazy-islands-and-streamed-replies"},
	"listen":      {wStateful, "none", gGuide + "#server-push--live-updates-from-elsewhere"},
	"load_many":   {wComponent, "none", gLoaders},
	"lock_state":  {wStateful, "`Options.LockState`", gGuide + "#server-state--replies-first-values-the-url-and-memory"},
	"locked":      {wStateful, "none", gGuide + "#server-state--replies-first-values-the-url-and-memory"},
	"log":         {wStateful, "the logger's default", gGuide + "#state-that-survives-a-restart--brick_secret"},
	"messages":    {wStateful, "each rule's own message", gGuide + "#validation--rules-declared-once"},
	"model":       {wStateful, "none", gGuide + "#events-and-effects--components-talking-to-each-other"},
	"on":          {wStateful, "none", gGuide + "#events-and-effects--components-talking-to-each-other"},
	"params":      {wStateful, "none", gGuide + "#server-state--replies-first-values-the-url-and-memory"},
	"paths":       {wPage, "none", gGuide + "#a-whole-site--urls-serving-building"},
	"per_request": {wPage, "`false`", gGuide + "#a-whole-site--urls-serving-building"},
	"query":       {wStateful, "none", gGuide + "#server-state--replies-first-values-the-url-and-memory"},
	"remember":    {wStateful, "none", gGuide + "#server-state--replies-first-values-the-url-and-memory"},
	"rules":       {wStateful, "none", gGuide + "#validation--rules-declared-once"},
	"sitemap":     {wPage, "`true`", gGuide + "#the-sitemap"},
	"slots":       {wComponent, "none (any block is accepted)", gSlots},
	"store":       {wStateful, "the default store (`Options.States`)", gGuide + "#state-that-survives-a-restart--brick_secret"},
	"temporary":   {wStateful, "none", gGuide + "#server-state--replies-first-values-the-url-and-memory"},
	"updated":     {wStateful, "none", gGuide + "#server-state--replies-first-values-the-url-and-memory"},
	"uploads":     {wStateful, "none", gGuide + "#uploads--files-into-state-bytes-into-your-storage"},
	"url":         {wPage, "the file's path", gGuide + "#a-whole-site--urls-serving-building"},
	"writable":    {wStateful, "none", gGuide + "#server-state--replies-first-values-the-url-and-memory"},
}

// propNotes and slotNotes describe one prop's and one slot's declaration.
var propNotes = map[string]note{
	"type":     {"prop", "`any`", gProps},
	"default":  {"prop", "none", gProps},
	"required": {"prop", "`false`", gProps},
	"oneof":    {"prop", "none", gProps},
	"client":   {"prop", "`false`", gStyles},
	"reactive": {"prop of a stateful component", "`false`", gGuide + "#actions--a-button-that-changes-server-state"},
	"doc":      {"prop", "none", gProps},
	"examples": {"prop", "none", gProps},
	"fields":   {"`type: map` prop", "none", gProps},
	"of":       {"`type: list` or `map` prop", "none", gProps},
}

var slotNotes = map[string]note{
	"required": {"slot", "`false`", gSlots},
	"doc":      {"slot", "none", gSlots},
	"examples": {"slot", "none", gSlots},
}

// node is one JSON Schema node, decoded.
type node = map[string]any

// refNames turns a $ref into the words a reader needs.
var refNames = map[string]string{
	"#/$defs/scriptFunction": "script function (`fn` or `ns.fn`)",
	"#/$defs/fieldList":      "list of state fields",
	"#/$defs/examples":       "list of examples",
	"#/$defs/fieldDecl":      "type name or field declaration",
}

// typeWords names the JSON Schema types the way brick's docs do.
var typeWords = map[string]string{"string": "text", "boolean": "bool", "object": "map", "array": "list", "integer": "integer", "number": "number"}

// shape describes a schema node's value in a few words.
func shape(n node) string {
	if r, ok := n["$ref"].(string); ok {
		if w, ok := refNames[r]; ok {
			return w
		}
		return r
	}
	if e, ok := n["enum"].([]any); ok {
		words := make([]string, len(e))
		for i, v := range e {
			words[i] = fmt.Sprintf("`%v`", v)
		}
		return strings.Join(words, " · ")
	}
	if alts, ok := n["anyOf"].([]any); ok {
		var out []string
		for _, a := range alts {
			an, _ := a.(node)
			if an["type"] == "null" {
				continue
			}
			if s := shape(an); !slices.Contains(out, s) {
				out = append(out, s)
			}
		}
		return strings.Join(out, ", or ")
	}
	t, _ := n["type"].(string)
	w, ok := typeWords[t]
	if !ok {
		return "any value"
	}
	switch t {
	case "object":
		if v, ok := n["additionalProperties"].(node); ok {
			return "map of " + group(shape(v))
		}
		if _, ok := n["properties"]; ok {
			return "map of settings"
		}
	case "array":
		if v, ok := n["items"].(node); ok {
			return "list of " + group(shape(v))
		}
	}
	return w
}

// group brackets a description of alternatives, so "map of (text, or
// map of settings)" cannot be read as "(map of text), or …".
func group(s string) string {
	if strings.Contains(s, ", or ") {
		return "(" + s + ")"
	}
	return s
}

// row is one table line.
type row struct{ key, shape, where, def, desc, guide string }

// rows builds one table from a properties map and its notes. A key with
// no note is an error, so a new engine key cannot ship undocumented.
func rows(props node, notes map[string]note, prefix string) ([]row, error) {
	keys := make([]string, 0, len(props))
	for k := range props {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]row, 0, len(keys))
	for _, k := range keys {
		nt, ok := notes[k]
		if !ok {
			return nil, fmt.Errorf("frontmatter key %s%s has no note in components-frontmatter/main.go", prefix, k)
		}
		n, _ := props[k].(node)
		desc, _ := n["description"].(string)
		out = append(out, row{key: prefix + k, shape: shape(n), where: nt.where, def: nt.def, desc: desc, guide: nt.guide})
	}
	for k := range notes {
		if _, ok := props[k]; !ok {
			return nil, fmt.Errorf("components-frontmatter/main.go has a note for %s%s, which the schema no longer has", prefix, k)
		}
	}
	return out, nil
}

// cellEscapes keep a table cell on one line and literal: a pipe would end
// the cell early, and a schema description's <Card> or <brick:script>
// would be read as an HTML tag and vanish.
var cellEscapes = strings.NewReplacer("|", `\|`, "\n", " ", "<", "&lt;", ">", "&gt;")

// cell makes s safe for one Markdown table cell via cellEscapes.
func cell(s string) string { return cellEscapes.Replace(s) }

// pageTitles name the guide pages in the Guide column.
var pageTitles = map[string]string{
	gProps:   "Components and props",
	gSlots:   "Children, blocks and slots",
	gLayouts: "Layouts and data",
	gLoaders: "Scripts and loaders",
	gStyles:  "Styles and client code",
	gErrors:  "When things go wrong",
	gGuide:   "The guide",
}

// guideCell turns a guide path (with or without an #anchor) into a link
// named after its page. An in-page link is kept as written.
func guideCell(g string) string {
	if strings.HasPrefix(g, "[") {
		return g
	}
	page, _, _ := strings.Cut(g, "#")
	title, ok := pageTitles[page]
	if !ok {
		title = page
	}
	return fmt.Sprintf("[%s](%s)", title, g)
}

// table writes rs as a Markdown table in the order given; the caller sorts, so
// the generated page is byte-for-byte reproducible.
func table(w io.Writer, rs []row) {
	fmt.Fprintln(w, "| Key | Value | Applies to | Default | What it does | Guide |")
	fmt.Fprintln(w, "|---|---|---|---|---|---|")
	for _, r := range rs {
		fmt.Fprintf(w, "| `%s` | %s | %s | %s | %s | %s |\n", r.key, cell(r.shape), r.where, r.def, cell(r.desc), guideCell(r.guide))
	}
}

// dig walks a path of keys and anyOf indexes into the schema.
func dig(n node, path ...any) (node, error) {
	cur := any(n)
	for _, p := range path {
		switch k := p.(type) {
		case string:
			m, _ := cur.(node)
			cur = m[k]
		case int:
			l, _ := cur.([]any)
			if k >= len(l) {
				return nil, fmt.Errorf("schema path %v: index %d out of range", path, k)
			}
			cur = l[k]
		}
	}
	m, ok := cur.(node)
	if !ok {
		return nil, fmt.Errorf("schema path %v is not an object", path)
	}
	return m, nil
}

// propDeclIndex and slotDeclIndex are the anyOf alternatives that hold the
// map form of a prop and a slot declaration (after null and the bare type).
const (
	propDeclIndex = 2
	slotDeclIndex = 1
)

// write prints the whole reference page.
func write(w io.Writer) error {
	var s node
	if err := json.Unmarshal(brick.FrontmatterSchema(), &s); err != nil {
		return err
	}
	top, err := dig(s, "properties")
	if err != nil {
		return err
	}
	bk, err := dig(s, "properties", "brick", "properties")
	if err != nil {
		return err
	}
	prop, err := dig(s, "properties", "props", "additionalProperties", "anyOf", propDeclIndex, "properties")
	if err != nil {
		return err
	}
	slot, err := dig(s, "properties", "brick", "properties", "slots", "additionalProperties", "anyOf", slotDeclIndex, "properties")
	if err != nil {
		return err
	}
	sections := []struct {
		title, intro string
		props        node
		notes        map[string]note
		prefix       string
	}{
		{"Top-level keys", "Written at the top of the frontmatter. Any other top-level key is the file's own data, read as `.Params.<key>` on a page (and as `.Props` on a layout).", top, topNotes, ""},
		{"brick: settings", "Written under one `brick:` map. `brick:` holds the settings added after the top-level keys were frozen; an unknown key in it is a warning naming the known ones.", bk, brickNotes, "brick."},
		{"Prop declaration settings", "A prop is declared bare (`title: string`) or as a map of these settings (`tone: {type: string, default: plain, oneof: [plain, loud]}`). `children` and `key` are reserved names.", prop, propNotes, "props.<name>."},
		{"Slot declaration settings", "Under `brick: {slots: {<name>: …}}`. A slot written bare (`footer:`) is optional. `children` names the content between the tags.", slot, slotNotes, "brick.slots.<name>."},
	}
	fmt.Fprint(w, header)
	for _, sec := range sections {
		rs, err := rows(sec.props, sec.notes, sec.prefix)
		if err != nil {
			return err
		}
		fmt.Fprintf(w, "\n## %s\n\n%s\n\n", sec.title, sec.intro)
		table(w, rs)
	}
	fmt.Fprint(w, footer)
	return nil
}

const header = `<!-- Generated by examples/components-frontmatter from brick.FrontmatterSchema(); edit that program, then run it: go run . > ../../docs/reference/frontmatter.md -->

# Frontmatter reference

Every key brick reads from a file's YAML frontmatter, the block between ` + "`---`" + ` lines at the top of a page, layout or component. Nothing here is required: ` + "`<h1>Hello</h1>`" + ` with no frontmatter is a complete page.

The keys, value shapes and descriptions are generated from ` + "`brick.FrontmatterSchema()`" + `, the JSON Schema that ` + "`brick schema`" + ` prints for editors. That schema is built from the key lists the engine parses, so this page lists exactly the keys the engine accepts. The schema checks one file's shape; what needs more than one file (a ` + "`load:`" + ` function that exists, a host's ` + "`Options.PropTypes`" + `) is checked by ` + "`brick check`" + `.

Point an editor at the schema with ` + "`brick schema -o .vscode/brick-frontmatter.schema.json`" + ` for completion and hover text while typing.
`

const footer = `
## Related

- [Components and props](../guides/components/props.md), [Children, blocks and slots](../guides/components/slots.md), [Layouts, context, site data and translations](../guides/components/layouts-and-data.md), [Scripts and loaders](../guides/components/loaders.md), [Styles and client code](../guides/components/styles-and-client.md), [When things go wrong](../guides/components/errors.md)
- [Feature index](features.md)
- The generator: [examples/components-frontmatter](../../examples/components-frontmatter/)
`

// main writes the generated page to stdout so `task` can redirect it into docs/.
func main() {
	if err := write(os.Stdout); err != nil {
		log.Fatal(err)
	}
}
