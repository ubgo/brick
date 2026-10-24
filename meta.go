package brick

import (
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"

	"github.com/ubgo/brick/internal/frontmatter"
)

// Engine frontmatter keys (A13). They are listed and frozen: every future
// engine key goes under the `brick:` map, never at the top level, so it can
// never collide with an author's params. Engine keys are not copied into
// .Params.
const (
	keyLayout       = "layout"
	keyUse          = "use"
	keyProps        = "props"
	keyLoad         = "load"
	keyProvide      = "provide"
	keyInject       = "inject"
	keyRaw          = "raw"
	keyDedent       = "dedent"
	keyStrictParams = "strictParams"
	keyStrictProps  = "strictProps"
	keyCache        = "cache"
	keyState        = "state"
	keyClient       = "client"
	keySyntax       = "syntax"
	keyUnknown      = "unknown"
	keyPassthrough  = "passthrough"
	keyBrick        = "brick"
)

// Keys under the `brick:` frontmatter map, where every engine key added
// after A13 lives.
const (
	// brickIsland makes a component an island: rendered per request by
	// Engine.Fill, never inside the cached page.
	brickIsland = "island"
	// brickURL is the site layer's URL override.
	brickURL = "url"
	// brickStore names the state store a stateful component uses
	//; the engine checks the name at compile.
	brickStore = "store"
	// brickOn lists the events a stateful component listens to and the
	// action each runs. It lives under brick:, not at
	// the top level, because A13 froze the top-level keys.
	brickOn = "on"
	// brickRules and brickMessages declare a stateful component's field
	// validation. Under brick:, like on:, because A13
	// froze the top-level keys.
	brickRules    = "rules"
	brickMessages = "messages"
	// brickUploads declares which state fields take files and their limits
	//. Under brick:, like rules:, because A13 froze the
	// top-level keys.
	brickUploads = "uploads"
	// brickInit and brickUpdated name a stateful component's script hooks
	//: init seeds a new instance's state, updated normalises
	// fields a built-in changed. Under brick:, because A13 froze the
	// top-level keys.
	brickInit    = "init"
	brickUpdated = "updated"
	// brickQuery syncs state fields with URL parameters.
	brickQuery = "query"
	// brickParams names the hook that runs when the URL sets a component's
	// query: fields: on instance
	// creation and on a patch link or Back/Forward that changed them.
	brickParams = "params"
	// brickRemember lists state fields kept per session beyond an
	// instance's life through Options.Remember.
	brickRemember = "remember"
	// brickPaths is read by the site layer.
	brickPaths = "paths"
	// brickLazy makes lazy loading a component's default; a
	// call site's brick:lazy wins.
	brickLazy = "lazy"
	// brickSlots declares the blocks a component takes:
	// once declared, a call that misses a required one or passes one not
	// declared fails at compile.
	brickSlots = "slots"
	// brickTemporary lists state fields rendered with their new value and
	// then reset to the declared one before the instance is saved, so large transient data never reaches the store.
	brickTemporary = "temporary"
	// brickLoadMany names a component's batch loader: one
	// call per render level gets every distinct input of the component's
	// calls, so a component in a range costs one data call, not N. It
	// replaces load: on that file, and only a component may have one.
	brickLoadMany = "load_many"
	// brickLocked lists state fields only the server writes (actions, init,
	// hooks): a client $set, $toggle, $reset, deferred set or brick:model
	// on one is refused, so an id the server chose cannot be swapped by the
	// browser before an action trusts it.
	brickLocked = "locked"
	// brickSitemap: false leaves a page out of the site layer's built-in
	// sitemap.xml. Read by the site layer.
	brickSitemap = "sitemap"
	// brickPerRequest: true makes the site layer render a page on every
	// request instead of serving it from its page cache, so the page's
	// loaders and body see the visitor (site.Options.Request). Read by the
	// site layer.
	brickPerRequest = "per_request"
	// brickLog is a component's action log setting: false, true or a level word, carried on ActionCall.Log for
	// action loggers (actionlog.go).
	brickLog = "log"
	// brickListen lists the topics a stateful component refreshes on
	//. Under brick:, because A13 froze the top-level
	// keys, and a frontmatter key rather than a brick:listen attribute
	// because subscribing is a fact about the component, like on:, and
	// its root is where the engine writes the signed listen token.
	brickListen = "listen"
	// brickLockState overrides Options.LockState for this component: true
	// closes its state to the client except the brick: {writable:} fields,
	// false keeps it open under a closed engine.
	brickLockState = "lock_state"
	// brickWritable lists the state fields the client may write in a
	// closed component; listing it alone closes the component, since a
	// list of what is writable means "only these".
	brickWritable = "writable"
	// brickCSS says how a component's sibling stylesheet (Card.css next to
	// Card.html) ships: global (the default, like an inline <brick:style>),
	// scoped or inclusive (like <brick:style scoped="…">). It is a
	// frontmatter key rather than a marker inside the .css because the
	// .css stays plain CSS that any editor and linter reads as-is.
	brickCSS = "css"
	// brickModel names the state field a stateful child exposes as its
	// model: a parent binds a field of its own
	// to it with <Child brick:model="qty"> (modelable.go).
	brickModel = "model"
)

// The values of brick: {css:}.
const (
	cssGlobal    = "global"
	cssScoped    = "scoped"
	cssInclusive = "inclusive"
)

// cssModes lists brick: {css:}'s values, for its error and schema.
var cssModes = []string{cssGlobal, cssScoped, cssInclusive}

// brickKeys lists every key the brick: map accepts, for the warning about
// an unknown one.
var brickKeys = []string{brickIsland, brickURL, brickStore, brickOn, brickRules, brickMessages, brickUploads, brickInit, brickUpdated, brickQuery, brickRemember, brickPaths, brickLazy, brickSlots, brickTemporary, brickLoadMany, brickLocked, brickSitemap, brickListen, brickLockState, brickWritable, brickLog, brickCSS, brickModel, brickParams, brickPerRequest}

// cacheTagsKey is the one key `cache:` accepts in v1; the rest is reserved.
const cacheTagsKey = "tags"

// engineKeys is the canonical list of A13 keys.
var engineKeys = []string{keyLayout, keyUse, keyProps, keyLoad, keyProvide, keyInject, keyRaw, keyDedent, keyStrictParams, keyStrictProps, keyCache, keyState, keyClient, keySyntax, keyUnknown, keyPassthrough, keyBrick}

// Prop types a declaration may name.
const (
	typeString = "string"
	typeInt    = "int"
	typeFloat  = "float"
	typeBool   = "bool"
	typeList   = "list"
	typeMap    = "map"
	typeAny    = "any"
)

var propTypes = []string{typeString, typeInt, typeFloat, typeBool, typeList, typeMap, typeAny}

// PropType checks and converts one prop value for a type the host program
// adds through Options.PropTypes (`email: email` in a `props:` block). It
// returns the value the component will see, or an error saying why the
// value is not of the type; `oneof:` is checked after it. It runs for the
// call site, the loader input and the default, never for an absent prop,
// and it is shared by concurrent renders, so it must be safe for concurrent
// use and pure: same input, same output (T8, E6).
type PropType func(v any) (any, error)

// checkPropTypes vets Options.PropTypes once, at New: a built-in type name
// cannot be redefined, because a theme reading `count: int` must mean the
// same thing under every host.
func checkPropTypes(types map[string]PropType) error {
	for _, name := range slices.Sorted(maps.Keys(types)) {
		switch {
		case slices.Contains(propTypes, name):
			return fmt.Errorf("Options.PropTypes: %q is a built-in prop type and cannot be redefined", name)
		case name == "":
			return fmt.Errorf("Options.PropTypes: a type needs a name")
		case types[name] == nil:
			return fmt.Errorf("Options.PropTypes: %q has no check function", name)
		}
	}
	return nil
}

// Reserved prop names (C4).
const (
	propChildren = "children"
	propKey      = "key"
	propSrc      = "src"
)

// propDecl is one entry of a `props:` block.
type propDecl struct {
	Name       string
	Type       string
	Default    any
	HasDefault bool
	Required   bool
	OneOf      []any
	Client     bool
	// Reactive makes a stateful component re-render, state kept, when a
	// parent's re-render passes it a different value; otherwise the prop
	// stays as the component's first render saw it.
	Reactive bool
	// Doc and Examples are documentation only: rendering
	// never reads them; ComponentDocs reports them.
	Doc      string
	Examples []string
	// custom is the host's check for a type from Options.PropTypes; nil
	// for a built-in type, which coerce handles.
	custom PropType
	// Fields is the shape of a `map` prop (G15): field name → declaration,
	// in the same grammar. A shape is closed: `brick check` warns on a read
	// of a field it does not list and on a literal that does not fit. It is
	// a check-time contract only — rendering never consults it, so loader
	// output and Go values pass exactly as an undeclared map would.
	Fields map[string]*propDecl
	// FieldOrder is Fields' names, sorted, for deterministic messages.
	FieldOrder []string
	// Of is the element declaration of a `list` prop, or the value
	// declaration of a `map` prop whose keys are data (`byId`); nil when
	// unconstrained. Like Fields, only `brick check` reads it.
	Of *propDecl
}

// hasShape reports whether a declaration constrains what is inside the
// value (fields or elements), which is what the shape checks look at.
func (d *propDecl) hasShape() bool { return d != nil && (d.Fields != nil || d.Of != nil) }

// injectDecl is one entry of an `inject:` block.
type injectDecl struct {
	Default    any
	HasDefault bool
	Required   bool
}

// useEntry is one `use:` import, classified by extension.
type useEntry struct {
	Key  string
	Path string // FS path
	Kind useKind
	// Remote is the spec as written for a remote import;
	// Path is then its vendored copy.
	Remote string
}

type useKind int

const (
	useComponent useKind = iota
	useData
	useScript
)

// Data file extensions `use:` and LoadSite accept (D1).
const (
	extJSON = ".json"
	extYAML = ".yaml"
	extYML  = ".yml"
	extTOML = ".toml"
	extStar = ".star"
)

// meta is a file's frontmatter, split into engine settings and .Params.
type meta struct {
	// readsChildren is set when the file mentions .Children, so a caller's
	// "listed in .Children even when false" warning (C7) only fires for a
	// component that inspects its children.
	readsChildren bool
	params        map[string]any
	layout        string
	hasLay        bool
	use           []useEntry
	props         map[string]*propDecl
	order         []string // declared prop names, in sorted order (deterministic)
	load          string
	loadMany      string // brick: {load_many:}, the batch form of load:
	provide       map[string]any
	inject        map[string]injectDecl
	raw           bool
	dedent        bool
	// strictParams is nil when the file does not say, so the engine default applies.
	strictParams *bool
	strictProps  bool
	cacheTags    []string
	unknown      *UnknownPolicy
	syntax       *Syntax
	passthrough  []string
	warnings     []string
	// css is brick: {css:}, how the sibling .css ships ("" = global).
	css string
	// island marks a component rendered per request (brick: {island: true}).
	island bool
	// lazy is brick: {lazy:}, the component's default lazy mode; "" when
	// it renders with its page.
	lazy string
	// state is a stateful component's initial state; nil for
	// a stateless file. A stateful component is always an island.
	state map[string]any
	// store is the named state store from brick: {store: name}; "" means
	// the default store.
	store string
	// on are the events a stateful component listens to, from
	// brick: {on: {event: call}}, in key order.
	on []listener
	// rules are brick: {rules:}, sorted by path; messages are brick:
	// {messages:}, "path.rule" → text. Both empty for a
	// component without validation, which then costs nothing.
	rules    []fieldRules
	messages map[string]string
	// uploads are brick: {uploads:}, field → limits;
	// nil for a component that takes no files.
	uploads map[string]uploadSpec
	// init and updated are brick: {init:, updated:}, and paramsHook is
	// brick: {params:} (named apart from the frontmatter params map):
	// function names as written ("posts.load_into"); "" when absent
	//.
	init, updated, paramsHook string
	// query are brick: {query:} in field order; remember are
	// brick: {remember:} fields. Both empty for most files.
	query    []queryParam
	remember []string
	// slots are brick: {slots:}, block name → declaration, with slotOrder
	// their sorted names; nil when the component declares none, which keeps
	// every call's blocks unchecked.
	slots     map[string]*slotDecl
	slotOrder []string
	// temporary are brick: {temporary:} state fields.
	temporary []string
	// locked are brick: {locked:} top-level state fields; locking a field
	// locks every path under it.
	locked []string
	// lockState is brick: {lock_state:} (nil when absent) and writable is
	// brick: {writable:} (nil when absent, which differs from an empty
	// list). closed is the resolved answer, set at compile from these and
	// Options.LockState: when true, every client write outside writable
	// (and the upload and query fields) is refused.
	lockState *bool
	writable  []string
	closed    bool
	// listen is brick: {listen:}; nil when absent.
	listen *listenDecl
	// actionLog is brick: {log:}; nil when absent or true (the logger's
	// default).
	actionLog *ActionLog
	// model is brick: {model:}, one top-level state field; "" when absent.
	model string
}

// parseMeta splits frontmatter into engine settings and params. It checks
// the shape of every engine key, so a typo in a declaration fails loudly.
// resolve maps a path written in file to its place in the engine's
// filesystem (Engine.resolveFrom); it is a parameter so parsing stays a
// pure function of its inputs. types are the host's extra prop types
// (Options.PropTypes), passed for the same reason; formats are the host's
// Options.DataFormats, so use: accepts any extension whose parser the host
// registered (the same rule parseData and script load() follow).
func parseMeta(doc frontmatter.Doc, file string, resolve func(file, p string) (string, error), types map[string]PropType, formats map[string]DataFormat) (*meta, error) {
	m := &meta{params: map[string]any{}, dedent: true}
	for k, v := range doc.Params {
		if !slices.Contains(engineKeys, k) {
			m.params[k] = v
		}
	}
	p := doc.Params
	if v, ok := p[keyLayout]; ok {
		s, ok := v.(string)
		if !ok || s == "" {
			return nil, fmt.Errorf("layout: must be a layout name, a ~/ path or none")
		}
		m.layout, m.hasLay = s, true
	}
	if v, ok := p[keyUse]; ok {
		mp, ok := v.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("use: must map names to ~/ paths")
		}
		keys := sortedKeys(mp)
		for _, k := range keys {
			s, ok := mp[k].(string)
			if !ok {
				return nil, fmt.Errorf("use: %s must be a ~/ path", k)
			}
			if strings.HasPrefix(k, "__") {
				return nil, fmt.Errorf("use: %q: names starting with __ are reserved for the engine (T5)", k)
			}
			fp, err := resolve(file, s)
			if err != nil {
				return nil, fmt.Errorf("use: %s: %w", k, err)
			}
			en := useEntry{Key: k, Path: fp}
			if _, remote := ParseRemote(s); remote {
				en.Remote = s
			}
			_, customData := formats[strings.ToLower(pathExt(fp))]
			switch ext := strings.ToLower(pathExt(fp)); {
			case ext == htmlExt:
				en.Kind = useComponent
				if !isComponentStem(k) {
					m.warnings = append(m.warnings, fmt.Sprintf("use: %s points at a component but does not start with an uppercase letter; it is reachable only as <x-%s>", k, strings.ToLower(k)))
				}
			case customData:
				en.Kind = useData
			case ext == extJSON, ext == extYAML, ext == extYML, ext == extTOML:
				en.Kind = useData
			case ext == extStar:
				en.Kind = useScript
			default:
				return nil, fmt.Errorf("use: %s: %q is not a component (.html), data (.json .yaml .toml%s) or script (.star) file", k, ext, customExts(formats))
			}
			m.use = append(m.use, en)
		}
	}
	if v, ok := p[keyProps]; ok {
		decls, err := parseProps(v, types)
		if err != nil {
			return nil, err
		}
		m.props = decls
		m.order = sortedKeys(decls)
	}
	if v, ok := p[keyLoad]; ok {
		s, ok := v.(string)
		if !ok || s == "" {
			return nil, fmt.Errorf("load: must name a script function, e.g. posts.recent")
		}
		m.load = s
	}
	if v, ok := p[keyProvide]; ok {
		mp, ok := v.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("provide: must be a map of keys to values")
		}
		m.provide = mp
	}
	if v, ok := p[keyInject]; ok {
		decls, err := parseInject(v)
		if err != nil {
			return nil, err
		}
		m.inject = decls
	}
	var err error
	if m.raw, err = boolKey(p, keyRaw, false); err != nil {
		return nil, err
	}
	if m.dedent, err = boolKey(p, keyDedent, true); err != nil {
		return nil, err
	}
	if m.strictProps, err = boolKey(p, keyStrictProps, false); err != nil {
		return nil, err
	}
	if _, ok := p[keyStrictParams]; ok {
		b, err := boolKey(p, keyStrictParams, false)
		if err != nil {
			return nil, err
		}
		m.strictParams = &b
	}
	if v, ok := p[keyCache]; ok {
		mp, ok := v.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("cache: must be a map with a tags list")
		}
		for k, tv := range mp {
			if k != cacheTagsKey {
				return nil, fmt.Errorf("cache: %q is reserved for the cache phase; only tags is accepted", k)
			}
			list, ok := tv.([]any)
			if !ok {
				return nil, fmt.Errorf("cache: tags must be a list of strings")
			}
			for _, t := range list {
				s, ok := t.(string)
				if !ok {
					return nil, fmt.Errorf("cache: tags must be a list of strings")
				}
				m.cacheTags = append(m.cacheTags, s)
			}
		}
	}
	if v, ok := p[keyUnknown]; ok {
		switch v {
		case wordError:
			u := UnknownError
			m.unknown = &u
		case wordPassthrough:
			u := UnknownPassthrough
			m.unknown = &u
		default:
			return nil, fmt.Errorf("unknown: must be error or passthrough")
		}
	}
	if v, ok := p[keySyntax]; ok {
		found := false
		for sx, w := range syntaxWords {
			if v == w {
				m.syntax, found = &sx, true
			}
		}
		if !found {
			return nil, fmt.Errorf("syntax: must be capitalized, hyphenated or both")
		}
	}
	if v, ok := p[keyPassthrough]; ok {
		list, ok := v.([]any)
		if !ok {
			return nil, fmt.Errorf("passthrough: must be a list of tag names")
		}
		for _, t := range list {
			s, ok := t.(string)
			if !ok {
				return nil, fmt.Errorf("passthrough: must be a list of tag names")
			}
			m.passthrough = append(m.passthrough, s)
		}
	}
	if v, ok := p[keyState]; ok {
		mp, ok := v.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("state: must be a map of initial values")
		}
		m.state = mp
	}
	if v, ok := p[keyBrick]; ok {
		mp, ok := v.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("brick: must be a map of engine settings")
		}
		for _, k := range sortedKeys(mp) {
			switch k {
			case brickIsland:
				b, ok := mp[k].(bool)
				if !ok {
					return nil, fmt.Errorf("brick: island: must be true or false")
				}
				m.island = b
			case brickLazy:
				s, ok := mp[k].(string)
				if !ok {
					return nil, fmt.Errorf("brick: lazy: must be one of %s", strings.Join(lazyModes, ", "))
				}
				if err := checkLazyValue(s); err != nil {
					return nil, fmt.Errorf("brick: lazy: %w", err)
				}
				m.lazy = s
			case brickURL:
				// Read by the site layer.
			case brickStore:
				s, ok := mp[k].(string)
				if !ok || s == "" {
					return nil, fmt.Errorf("brick: store: must be a store name")
				}
				m.store = s
			case brickOn:
				on, err := parseListeners(mp[k])
				if err != nil {
					return nil, err
				}
				m.on = on
			case brickRules:
				rules, err := parseRules(mp[k])
				if err != nil {
					return nil, err
				}
				m.rules = rules
			case brickMessages:
				msgs, err := parseMessages(mp[k])
				if err != nil {
					return nil, err
				}
				m.messages = msgs
			case brickUploads:
				ups, err := parseUploads(mp[k])
				if err != nil {
					return nil, err
				}
				m.uploads = ups
			case brickInit, brickUpdated, brickParams:
				s, ok := mp[k].(string)
				if !ok || !actionRE.MatchString(s) || strings.HasPrefix(s, "$") {
					return nil, fmt.Errorf("brick: %s: must name one of the component's script functions (fn or ns.fn)", k)
				}
				switch k {
				case brickInit:
					m.init = s
				case brickUpdated:
					m.updated = s
				default:
					m.paramsHook = s
				}
			case brickQuery:
				q, err := parseQuery(mp[k])
				if err != nil {
					return nil, err
				}
				m.query = q
			case brickRemember:
				r, err := parseRemember(mp[k])
				if err != nil {
					return nil, err
				}
				m.remember = r
			case brickPaths, brickSitemap, brickPerRequest:
				// Read by the site layer.
			case brickSlots:
				slots, err := parseSlots(mp[k])
				if err != nil {
					return nil, err
				}
				m.slots, m.slotOrder = slots, sortedKeys(slots)
			case brickTemporary:
				t, err := parseTemporary(mp[k])
				if err != nil {
					return nil, err
				}
				m.temporary = t
			case brickLocked:
				l, err := parseLocked(mp[k])
				if err != nil {
					return nil, err
				}
				m.locked = l
			case brickLockState:
				b, ok := mp[k].(bool)
				if !ok {
					return nil, fmt.Errorf("brick: lock_state: must be true or false")
				}
				m.lockState = &b
			case brickWritable:
				w, err := parseFieldList(mp[k], brickWritable)
				if err != nil {
					return nil, err
				}
				m.writable = w
			case brickListen:
				l, err := parseListen(mp[k])
				if err != nil {
					return nil, err
				}
				m.listen = l
			case brickLoadMany:
				s, ok := mp[k].(string)
				if !ok || s == "" {
					return nil, fmt.Errorf("brick: load_many: must name a script function, e.g. users.cards")
				}
				m.loadMany = s
			case brickModel:
				s, err := parseModel(mp[k])
				if err != nil {
					return nil, err
				}
				m.model = s
			case brickCSS:
				s, ok := mp[k].(string)
				if !ok || !slices.Contains(cssModes, s) {
					return nil, fmt.Errorf("brick: css: must be one of %s", strings.Join(cssModes, ", "))
				}
				m.css = s
			case brickLog:
				l, err := parseActionLog(mp[k])
				if err != nil {
					return nil, err
				}
				m.actionLog = l
			default:
				m.warnings = append(m.warnings, fmt.Sprintf("brick: %s: not an engine setting (known: %s); ignored", k, strings.Join(brickKeys, ", ")))
			}
		}
	}
	if m.load != "" && m.loadMany != "" {
		return nil, fmt.Errorf("brick: load_many: a file has one loader; remove load: or load_many")
	}
	// reactive: decides whether a kept stateful child re-renders, so it
	// means nothing without state:.
	if m.state == nil {
		for _, name := range m.order {
			if m.props[name].Reactive {
				return nil, fmt.Errorf("props: %s: reactive: only applies to a stateful component (one with state:)", name)
			}
		}
	}
	if err := checkTemporary(m); err != nil {
		return nil, err
	}
	if err := checkModel(m); err != nil {
		return nil, err
	}
	return m, nil
}

// reactiveProps lists the props declared reactive: true, in order.
func (m *meta) reactiveProps() []string {
	var out []string
	for _, name := range m.order {
		if m.props[name].Reactive {
			out = append(out, name)
		}
	}
	return out
}

// parseProps reads a `props:` block: `title: string` or a map with type,
// default, required, oneof, client, reactive, doc, examples, fields and of.
// types are the host's extra prop types; a name that is neither built in nor
// among them is an error here, at compile, so a theme moved to a host that
// lacks a type fails loudly.
func parseProps(v any, types map[string]PropType) (map[string]*propDecl, error) {
	mp, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("props: must map prop names to declarations")
	}
	out := map[string]*propDecl{}
	for name, dv := range mp {
		switch name {
		case propChildren:
			return nil, fmt.Errorf("props: children is reserved; a component reads its content with {{ children }} (C4)")
		case propKey:
			return nil, fmt.Errorf("props: key is reserved for instance identity and read as .Key; rename the prop (C4)")
		}
		d, err := parseDecl(name, dv, types, false)
		if err != nil {
			return nil, fmt.Errorf("props: %w", err)
		}
		out[name] = d
	}
	return out, nil
}

// parseDecl reads one declaration. A field (inside fields: or of:) takes
// the same grammar minus the settings that only mean something on a prop
// the engine applies at render: default, client and reactive. A field's
// default would never be applied, because shapes are never enforced at
// render, so accepting one would promise behaviour that does not exist.
func parseDecl(name string, dv any, types map[string]PropType, field bool) (*propDecl, error) {
	d := &propDecl{Name: name, Type: typeAny}
	settings := propSettings
	if field {
		settings = fieldSettings
	}
	switch x := dv.(type) {
	case string:
		d.Type = x
	case map[string]any:
		for k := range x {
			if !slices.Contains(settings, k) {
				return nil, fmt.Errorf("%s: unknown setting %q (%s)", name, k, strings.Join(settings, ", "))
			}
		}
		if t, ok := x["type"]; ok {
			s, ok := t.(string)
			if !ok {
				return nil, fmt.Errorf("%s: type must be a string", name)
			}
			d.Type = s
		}
		if dv, ok := x["default"]; ok {
			d.Default, d.HasDefault = dv, true
		}
		if r, ok := x["required"]; ok {
			b, ok := r.(bool)
			if !ok {
				return nil, fmt.Errorf("%s: required must be true or false", name)
			}
			d.Required = b
		}
		if o, ok := x["oneof"]; ok {
			list, ok := o.([]any)
			if !ok || len(list) == 0 {
				return nil, fmt.Errorf("%s: oneof must be a non-empty list", name)
			}
			d.OneOf = list
		}
		if c, ok := x["client"]; ok {
			b, ok := c.(bool)
			if !ok {
				return nil, fmt.Errorf("%s: client must be true or false", name)
			}
			d.Client = b
		}
		if r, ok := x["reactive"]; ok {
			b, ok := r.(bool)
			if !ok {
				return nil, fmt.Errorf("%s: reactive must be true or false", name)
			}
			d.Reactive = b
		}
		doc, examples, err := parseDocs(x)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		d.Doc, d.Examples = doc, examples
		if err := parseShape(d, x, types); err != nil {
			return nil, err
		}
	case nil:
	default:
		return nil, fmt.Errorf("%s: must be a type name or a map", name)
	}
	if !slices.Contains(propTypes, d.Type) {
		check, ok := types[d.Type]
		if !ok {
			known := append(slices.Clone(propTypes), slices.Sorted(maps.Keys(types))...)
			return nil, fmt.Errorf("%s: unknown type %q (one of %s; the host program adds more with Options.PropTypes)", name, d.Type, strings.Join(known, ", "))
		}
		d.custom = check
	}
	if d.HasDefault {
		cv, err := d.coerce(d.Default)
		if err != nil {
			return nil, fmt.Errorf("%s: default: %w", name, err)
		}
		d.Default = cv
	}
	return d, nil
}

// parseShape reads fields: and of: (G15). fields: belongs to a map (its
// named keys); of: to a list (its elements) or a map (the value under any
// key). A map cannot take both: a closed set of names and "any key" are
// two different shapes, and picking one silently would hide a typo.
func parseShape(d *propDecl, x map[string]any, types map[string]PropType) error {
	if fv, ok := x[fieldsKey]; ok {
		if d.Type != typeMap {
			return fmt.Errorf("%s: fields: needs type: map (it is %s)", d.Name, d.Type)
		}
		mp, ok := fv.(map[string]any)
		if !ok || len(mp) == 0 {
			return fmt.Errorf("%s: fields: must map field names to declarations", d.Name)
		}
		d.Fields = make(map[string]*propDecl, len(mp))
		for fname, fdv := range mp {
			fd, err := parseDecl(fname, fdv, types, true)
			if err != nil {
				return fmt.Errorf("%s.%w", d.Name, err)
			}
			d.Fields[fname] = fd
		}
		d.FieldOrder = sortedKeys(d.Fields)
	}
	if ov, ok := x[ofKey]; ok {
		if d.Type != typeList && d.Type != typeMap {
			return fmt.Errorf("%s: of: needs type: list or type: map (it is %s)", d.Name, d.Type)
		}
		if d.Fields != nil {
			return fmt.Errorf("%s: a map takes fields: (named keys) or of: (any key), not both", d.Name)
		}
		od, err := parseDecl(d.Name+"[]", ov, types, true)
		if err != nil {
			return err
		}
		d.Of = od
	}
	return nil
}

// Shape settings (G15).
const (
	fieldsKey = "fields"
	ofKey     = "of"
)

// propSettings are the keys a prop's map declaration accepts.
var propSettings = []string{"type", "default", "required", "oneof", "client", "reactive", docKey, examplesKey, fieldsKey, ofKey}

// fieldSettings are the keys a field declaration (inside fields: or of:)
// accepts; see parseDecl for why default, client and reactive are not.
var fieldSettings = []string{"type", "required", "oneof", docKey, examplesKey, fieldsKey, ofKey}

// parseInject reads an `inject:` block.
func parseInject(v any) (map[string]injectDecl, error) {
	mp, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("inject: must map context keys to {default, required}")
	}
	out := map[string]injectDecl{}
	for k, dv := range mp {
		var d injectDecl
		switch x := dv.(type) {
		case nil:
		case map[string]any:
			if def, ok := x["default"]; ok {
				d.Default, d.HasDefault = def, true
			}
			if r, ok := x["required"]; ok {
				b, ok := r.(bool)
				if !ok {
					return nil, fmt.Errorf("inject: %s: required must be true or false", k)
				}
				d.Required = b
			}
		default:
			return nil, fmt.Errorf("inject: %s: must be a map with default or required", k)
		}
		out[k] = d
	}
	return out, nil
}

func boolKey(p map[string]any, k string, def bool) (bool, error) {
	v, ok := p[k]
	if !ok {
		return def, nil
	}
	b, ok := v.(bool)
	if !ok {
		return false, fmt.Errorf("%s: must be true or false", k)
	}
	return b, nil
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func pathExt(p string) string {
	i := strings.LastIndexByte(p, '.')
	if i < 0 || strings.Contains(p[i:], "/") {
		return ""
	}
	return p[i:]
}

// coerce converts a value to the declared type: the host's check for a
// custom type, the built-in conversion otherwise. An absent value stays
// absent for both, so a custom check never sees nil.
func (d *propDecl) coerce(v any) (any, error) {
	if d.custom == nil || v == nil {
		return coerce(v, d.Type)
	}
	return d.custom(v)
}

// customExts lists the host-registered data extensions for the use: refusal
// message (", plus registered .a .b"), so the host sees what would have been
// accepted. Sorted for deterministic output (E6).
func customExts(formats map[string]DataFormat) string {
	if len(formats) == 0 {
		return ""
	}
	return ", plus registered " + strings.Join(sortedKeys(formats), " ")
}
