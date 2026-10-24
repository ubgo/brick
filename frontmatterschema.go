package brick

import (
	"encoding/json"
	"maps"
	"slices"
)

// FrontmatterSchema returns a JSON Schema (draft 2020-12) for a file's
// YAML frontmatter, for editors: completion, hover docs and validation of
// every engine key while typing. `brick schema` prints it.
//
// Invariant: it is generated from the same key lists parseMeta reads
// (engineKeys, brickKeys, propSettings, slotSettings), so a key the engine
// gains is a key the schema has; frontmatterschema_test.go fails when a
// list holds a key the tables below do not describe. The schema is never
// stricter than parseMeta: a file brick accepts must validate. It cannot
// see what needs more than one file (a load: function that exists, a
// rules: path declared in state:, a host's Options.PropTypes), which stays
// `brick check`'s job. Top-level keys that are not engine keys are the
// author's params, so the top level allows any other key.
func FrontmatterSchema() []byte {
	b, err := json.MarshalIndent(frontmatterSchema(), "", "  ")
	if err != nil {
		// The schema is built from maps of strings, bools and numbers only.
		panic("brick: frontmatter schema does not marshal: " + err.Error())
	}
	return append(b, '\n')
}

// FrontmatterSchemaID is the schema's $id: a stable name editors and
// tools can key on. It is a name, not a fetchable URL.
const FrontmatterSchemaID = "https://github.com/ubgo/brick/frontmatter.schema.json"

// jsonSchemaDialect is the draft the schema is written in.
const jsonSchemaDialect = "https://json-schema.org/draft/2020-12/schema"

// keySchema is one frontmatter key: its hover text and the shape of its
// value. shape returns a fresh map each call, so no two places in the
// schema share (and could mutate) one value.
type keySchema struct {
	doc   string
	shape func() obj
}

// obj is one JSON Schema node.
type obj = map[string]any

// Reusable fragments, under $defs.
const (
	defScriptFunc = "scriptFunction"
	defNameList   = "fieldList"
	defExamples   = "examples"
	defFieldDecl  = "fieldDecl"
	defDocs       = "#/$defs/"
)

func ref(def string) obj { return obj{"$ref": defDocs + def} }

func str() obj      { return obj{"type": "string"} }
func boolean() obj  { return obj{"type": "boolean"} }
func nonEmpty() obj { return obj{"type": "string", "minLength": 1} }

// stringMap is a map whose every value has the shape v.
func stringMap(v obj) obj { return obj{"type": "object", "additionalProperties": v} }

// enumOf is a string limited to words; the words come from the engine's
// own lists, never retyped here.
func enumOf(words []string) obj {
	return obj{"type": "string", "enum": slices.Clone(words)}
}

// orNull lets a value be written bare (`title:`), which every map entry
// parseMeta reads as "all defaults" accepts.
func orNull(v obj) obj { return obj{"anyOf": []any{obj{"type": "null"}, v}} }

// closedObject is a map of known settings, nothing else, built from a key
// list and its table in the list's order.
func closedObject(keys []string, table map[string]keySchema) obj {
	return obj{"type": "object", "properties": propertiesOf(keys, table), "additionalProperties": false}
}

// propertiesOf builds a properties map from a key list. A key the table
// does not describe still appears, as an open schema, so the schema never
// rejects a key the engine accepts; the test reports the missing entry.
func propertiesOf(keys []string, table map[string]keySchema) obj {
	out := obj{}
	for _, k := range keys {
		ks, ok := table[k]
		if !ok {
			out[k] = obj{}
			continue
		}
		s := ks.shape()
		s["description"] = ks.doc
		out[k] = s
	}
	return out
}

// syntaxWordList is the syntax: values in a fixed order.
func syntaxWordList() []string {
	return slices.Sorted(maps.Values(syntaxWords))
}

// topLevelSchemas describes the A13 engine keys.
var topLevelSchemas = map[string]keySchema{
	keyLayout: {"The layout this page renders inside: a layout name, a ~/ path, or none.", nonEmpty},
	keyUse: {"Imports, name → ~/ path: a component (.html), data (.json .yaml .yml .toml) or script (.star) file, or a remote import. A component name starts with an uppercase letter.", func() obj {
		return obj{"type": "object", "additionalProperties": str(), "propertyNames": obj{"not": obj{"pattern": "^__"}}}
	}},
	keyProps:        {"The props a component takes, name → a type (`title: string`) or a declaration {type, default, required, oneof, client, reactive, doc, examples}. children and key are reserved.", propsSchema},
	keyLoad:         {"The script function that loads this file's data (posts.recent, or an inline <brick:script> function). It receives props, page and the injected context and returns a dict.", nonEmpty},
	keyProvide:      {"Context values this file provides to every component below it, key → value. A component reads one by declaring it in inject:.", func() obj { return obj{"type": "object"} }},
	keyInject:       {"Context keys this component reads from an ancestor's provide:, key → {default, required} (or bare for neither).", injectSchema},
	keyRaw:          {"true renders the body as-is: no component tags, no {{ }} actions.", boolean},
	keyDedent:       {"Strip the common leading indentation of the body (default true).", boolean},
	keyStrictParams: {"true makes a .Params lookup of a key the frontmatter does not set an error, instead of an empty value. Unset uses the engine's default.", boolean},
	keyStrictProps:  {"true makes passing a prop this component does not declare an error.", boolean},
	keyCache:        {"Cache settings. Only tags is accepted; the rest is reserved for the cache phase.", cacheSchema},
	keyState:        {"A stateful component's initial state, field → value. A stateful component is always an island and its actions change this state.", func() obj { return obj{"type": "object"} }},
	keyClient:       {"Reserved engine key (A13); the engine does not read it. A prop is sent to the browser with props: {name: {client: true}}.", func() obj { return obj{} }},
	keySyntax:       {"Which tag spellings this file accepts: capitalized (<Card>), hyphenated (<x-card>) or both.", func() obj { return enumOf(syntaxWordList()) }},
	keyUnknown:      {"What an unknown tag means here: error, or passthrough (written out as plain HTML).", func() obj { return enumOf([]string{wordError, wordPassthrough}) }},
	keyPassthrough:  {"Tag names written out as plain HTML even though they look like components.", func() obj { return obj{"type": "array", "items": str()} }},
	keyBrick:        {"Engine settings added after the top-level keys were frozen (A13).", func() obj { return closedObject(brickKeys, brickSchemas) }},
}

// brickSchemas describes the keys of the brick: map.
var brickSchemas = map[string]keySchema{
	brickIsland:     {"true makes this component an island: rendered per request by Engine.Fill, never inside the cached page.", boolean},
	brickURL:        {"Site layer: this page's URL, a path starting with / (overrides the one its file path gives).", func() obj { return obj{"type": "string", "pattern": `^/[^?#\\ ]*$`} }},
	brickStore:      {"The named state store this stateful component uses (default store when unset). Checked against the engine's stores at compile.", nonEmpty},
	brickOn:         {"Events this stateful component listens to, event name → action ($refresh, a function, or name(args)).", func() obj { return stringMap(str()) }},
	brickRules:      {"Field validation, state path → rule or list of rules; a rule is a name (required) or a one-key map ({min: 2}); a * segment covers every list item (todos.*.title).", rulesSchema},
	brickMessages:   {"Validation messages, \"path.rule\" → text, overriding a declared rule's default message.", func() obj { return stringMap(str()) }},
	brickUploads:    {"State fields that take files, field → limits {accept, max_size, max_files}.", uploadsSchema},
	brickInit:       {"Script function that seeds a new instance's state (fn or ns.fn).", func() obj { return ref(defScriptFunc) }},
	brickUpdated:    {"Script function that normalizes fields after a built-in action changed them (fn or ns.fn).", func() obj { return ref(defScriptFunc) }},
	brickParams:     {"Script function that runs when the URL sets the component's query: fields: on instance creation and on a patch link or Back/Forward (fn or ns.fn).", func() obj { return ref(defScriptFunc) }},
	brickQuery:      {"State fields synced with URL parameters: a list ([page, q]) or field → {as, history, keep}.", querySchema},
	brickRemember:   {"State fields kept per session beyond an instance's life (needs Options.Remember).", func() obj { return ref(defNameList) }},
	brickPaths:      {"Site layer, dynamic pages: the script function listing the values of the [param] segments (posts.slugs).", nonEmpty},
	brickLazy:       {"This component's default lazy mode; a call site's brick:lazy wins.", func() obj { return enumOf(lazyModes) }},
	brickSlots:      {"The blocks this component takes, name → {required, doc, examples} (or bare for an optional one). children names the call's body.", func() obj { return stringMap(orNull(closedObject(slotSettings, slotSettingSchemas))) }},
	brickTemporary:  {"State fields rendered with their new value and then reset to the declared one before the instance is saved.", func() obj { return ref(defNameList) }},
	brickLocked:     {"State fields only the server writes (actions, init, hooks); a client $set, $toggle, $reset or brick:model on one is refused.", func() obj { return ref(defNameList) }},
	brickLockState:  {"true closes this component's state to the client except the writable: fields (and upload and query fields); false keeps it open when Options.LockState closes every component.", boolean},
	brickWritable:   {"The state fields the client may write when the state is closed; listing it closes this component by itself. An empty list makes every field server-only.", func() obj { return ref(defNameList) }},
	brickListen:     {"Topics this stateful component refreshes on (server push, needs Options.Push): a topic, a list, or {topics, keep_alive, presence}. A topic may use {{ .Props.x }} and {{ .State.x }}.", listenSchema},
	brickSitemap:    {"Site layer: false leaves this page out of the built-in sitemap.xml.", boolean},
	brickPerRequest: {"Site layer: true renders this page on every request instead of from the page cache, so its loaders and body see the visitor (site.Options.Request); brick build skips it with a warning.", boolean},
	brickModel:      {"The state field this stateful component exposes as its model: a parent binds a field of its own to it with <Child brick:model=\"field\">.", nonEmpty},
	brickLog:        {"Action logging for this component: false stops logging its successful actions (a failed or refused one is still logged), a level (debug, info, warn, error) sets the level they log at; true or unset keeps the logger's default.", func() obj { return obj{"anyOf": []any{boolean(), enumOf(actionLogWords)}} }},
	brickCSS:        {"How the component's sibling stylesheet (Card.css next to Card.html) ships: global (default), scoped or inclusive, as with <brick:style scoped>.", func() obj { return enumOf(cssModes) }},
	brickLoadMany:   {"The batch loader: one call per render level gets every distinct input of this component's calls. Replaces load: on this file.", nonEmpty},
}

// propSettingSchemas describes the keys of one prop declaration.
var propSettingSchemas = map[string]keySchema{
	"type":      {"The prop's type: a built-in one or one the host adds with Options.PropTypes.", func() obj { return obj{"anyOf": []any{enumOf(propTypes), str()}} }},
	"default":   {"The value used when a call does not pass the prop; converted to the declared type.", func() obj { return obj{} }},
	"required":  {"true makes a call that omits the prop (and has no default) an error.", boolean},
	"oneof":     {"The only values the prop accepts.", func() obj { return obj{"type": "array", "minItems": 1} }},
	"client":    {"true sends the prop to the browser for the component's mount script.", boolean},
	"reactive":  {"Stateful components only: re-render, state kept, when a parent passes a different value.", boolean},
	docKey:      {"Documentation for brick docs; rendering never reads it.", str},
	examplesKey: {"Example values or call snippets for brick docs.", func() obj { return ref(defExamples) }},
	fieldsKey: {"type: map only: the shape of the map, field name → a type or {type, required, oneof, doc, examples, fields, of}. brick check warns on a read of an unlisted field and on a literal that does not fit; rendering never reads it.", func() obj {
		return obj{"type": "object", "minProperties": 1, "additionalProperties": ref(defFieldDecl)}
	}},
	ofKey: {"type: list: the element declaration; type: map: the value under any key. A type or a field declaration. Checked by brick check only.", func() obj { return ref(defFieldDecl) }},
}

// slotSettingSchemas describes the keys of one slot declaration.
var slotSettingSchemas = map[string]keySchema{
	"required":  {"true makes a call that does not pass this block an error.", boolean},
	docKey:      {"Documentation for brick docs.", str},
	examplesKey: {"Example blocks for brick docs.", func() obj { return ref(defExamples) }},
}

func propsSchema() obj {
	decl := obj{"anyOf": []any{obj{"type": "null"}, str(), closedObject(propSettings, propSettingSchemas)}}
	return obj{
		"type":                 "object",
		"additionalProperties": decl,
		"propertyNames":        obj{"not": obj{"enum": []any{propChildren, propKey}}},
	}
}

// injectSchema leaves a declaration's other keys open, as parseInject
// ignores them.
func injectSchema() obj {
	return stringMap(orNull(obj{"type": "object", "properties": obj{"default": obj{}, "required": boolean()}}))
}

func cacheSchema() obj {
	return obj{"type": "object", "properties": obj{cacheTagsKey: obj{"type": "array", "items": str()}}, "additionalProperties": false}
}

func rulesSchema() obj {
	rule := obj{"anyOf": []any{str(), obj{"type": "object", "minProperties": 1, "maxProperties": 1}}}
	list := obj{"type": "array", "minItems": 1, "items": rule}
	return stringMap(obj{"anyOf": []any{rule, list}})
}

// uploadKeys are the keys of one brick: {uploads:} entry, as parseUploads
// reads them.
var uploadKeys = []string{uploadKeyAccept, uploadKeyMaxSize, uploadKeyMaxFiles}

// Patterns for upload limits, mirroring acceptRE and sizeRE. parseUploads
// lowercases a media type before matching, and sizeRE is case-blind, so
// both patterns admit either case.
const (
	acceptPattern = `^[A-Za-z0-9][\w.+-]*/(\*|[A-Za-z0-9][\w.+-]*)$`
	sizePattern   = `^\s*\d+\s*([bB]|[kKmMgG][bB])?\s*$`
)

func uploadsSchema() obj {
	mediaType := obj{"type": "string", "pattern": acceptPattern}
	table := map[string]keySchema{
		uploadKeyAccept: {"Accepted media types (image/png, image/*); a single type or a list.", func() obj { return obj{"anyOf": []any{mediaType, obj{"type": "array", "items": mediaType}}} }},
		uploadKeyMaxSize: {"Size limit per file: bytes, or a number with B, KB, MB or GB (1024-based).", func() obj {
			return obj{"anyOf": []any{obj{"type": "integer", "minimum": 1}, obj{"type": "string", "pattern": sizePattern}}}
		}},
		uploadKeyMaxFiles: {"How many files the field holds, at least 1.", func() obj { return obj{"type": "integer", "minimum": 1} }},
	}
	return stringMap(orNull(closedObject(uploadKeys, table)))
}

// queryKeys are the keys of one brick: {query:} map entry.
var queryKeys = []string{queryKeyAs, queryKeyHistory, queryKeyKeep}

func querySchema() obj {
	table := map[string]keySchema{
		queryKeyAs:      {"The URL parameter name (default: the field name).", nonEmpty},
		queryKeyHistory: {"replace (default) keeps one history entry; push lets Back undo a change.", func() obj { return enumOf([]string{queryReplace, queryPush}) }},
		queryKeyKeep:    {"true keeps the parameter in the URL even when it equals the declared value.", boolean},
	}
	return obj{"anyOf": []any{
		obj{"type": "array", "items": nonEmpty()},
		stringMap(orNull(closedObject(queryKeys, table))),
	}}
}

func listenSchema() obj {
	topics := obj{"anyOf": []any{nonEmpty(), obj{"type": "array", "items": nonEmpty(), "minItems": 1}}}
	table := map[string]keySchema{
		listenKeyTopics:    {"The topics: one, or a list.", func() obj { return topics }},
		listenKeyPresence:  {"Presence topics: one or a list. The component shows who is on each with {{ .Presence \"topic\" }} and refreshes when someone joins or leaves (needs Options.Push). A topic may use {{ .Props.x }} and {{ .State.x }}.", func() obj { return topics }},
		listenKeyKeepAlive: {"true refreshes while the tab is hidden too; otherwise a hidden tab waits until it is shown.", boolean},
	}
	return obj{"anyOf": []any{nonEmpty(), obj{"type": "array", "items": nonEmpty(), "minItems": 1}, closedObject(listenKeys, table)}}
}

// frontmatterSchema builds the schema as a value.
func frontmatterSchema() obj {
	return obj{
		"$schema":              jsonSchemaDialect,
		"$id":                  FrontmatterSchemaID,
		"title":                "brick frontmatter",
		"description":          "The YAML frontmatter of a brick page, layout or component. Keys other than the engine's are the file's params (.Params).",
		"type":                 "object",
		"properties":           propertiesOf(engineKeys, topLevelSchemas),
		"additionalProperties": true,
		"$defs": obj{
			defScriptFunc: obj{"type": "string", "pattern": `^\w+(\.\w+)?$`, "description": "A script function: fn, or ns.fn from a use: module."},
			defNameList:   obj{"type": "array", "items": nonEmpty(), "description": "A list of top-level state fields."},
			defExamples:   obj{"type": "array", "items": obj{"type": []any{"string", "number", "boolean"}}},
			defFieldDecl:  obj{"anyOf": []any{obj{"type": "null"}, str(), closedObject(fieldSettings, propSettingSchemas)}},
		},
	}
}
