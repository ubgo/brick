// Package brick is a component-based, top-down HTML render engine on top of
// html/template.
//
// Pages, layouts and components are .html files with optional YAML
// frontmatter. Components are tags — <Card> or <x-card> — that resolve to
// files. brick adds a calling convention to Go templates, never a new
// template language: every {{ }} is Go's, evaluated and escaped by
// html/template.
//
//	eng, err := brick.New(brick.Options{FS: os.DirFS("site")})
//	html, err := eng.Render(ctx, "content/about.html", map[string]any{"user": "Sara"})
//
// The design and every rule referenced in comments (T2, C7, X7, …) live in
// the project's.
package brick

import (
	"context"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"log/slog"
	"maps"
	"net/http"
	"reflect"
	"slices"
	"sync"
	"time"

	"github.com/ubgo/brick/validate"
)

// Mode selects how errors are shown.
type Mode int

// Modes. The zero value is Prod: forgetting to set the mode must never print
// file paths and source lines into a public page.
const (
	Prod Mode = iota
	Dev
)

// UnknownPolicy says what an unresolved component tag does.
type UnknownPolicy int

// Unknown-tag policies.
const (
	// UnknownError makes an unresolved tag a compile error (the default): a
	// typo must not ship as an unstyled element.
	UnknownError UnknownPolicy = iota
	// UnknownPassthrough emits unresolved tags verbatim, for native web
	// components.
	UnknownPassthrough
)

// Words for policies and syntaxes as written in brick.yaml and frontmatter
// (`unknown: passthrough`, `syntax: capitalized`).
const (
	wordError       = "error"
	wordPassthrough = "passthrough"
	wordBoth        = "both"
	wordCapitalized = "capitalized"
	wordHyphenated  = "hyphenated"
)

// syntaxWords names each Syntax for messages and brick.yaml.
var syntaxWords = map[Syntax]string{SyntaxBoth: wordBoth, SyntaxCapitalized: wordCapitalized, SyntaxHyphenated: wordHyphenated}

// Syntax restricts which tag spellings a theme accepts.
type Syntax int

// Tag syntaxes.
const (
	SyntaxBoth Syntax = iota
	SyntaxCapitalized
	SyntaxHyphenated
)

// Default limits. Zero in Options means these, never "unlimited".
const (
	DefaultMaxDepth       = 64
	DefaultMaxComponents  = 100_000
	DefaultMaxOutputBytes = 32 << 20
	DefaultMaxFileBytes   = 8 << 20
	DefaultAssetBase      = "/_brick/"
	DefaultLayoutDir      = "layouts"
	// defaultLayoutName is the layout applied when a page names none.
	defaultLayoutName = "default"
	// layoutNone opts a page out of every layout.
	layoutNone = "none"
)

// DefaultImageDirs is the list of directories {{ image }} may read from when
// Options.ImageDirs is empty. It returns a fresh slice on
// every call: a package-level slice would let any importer change the default
// of every engine built afterwards in the same process.
func DefaultImageDirs() []string { return []string{"static"} }

// DefaultImageWidths is the set of widths {{ image }} resizes to when
// Options.ImageWidths is empty. Like DefaultImageDirs it
// returns a fresh slice, so callers may sort or append to it freely.
func DefaultImageWidths() []int { return []int{480, 960, 1440} }

// DefaultMaxImagePixels bounds the pixels {{ image }} will decode.
const DefaultMaxImagePixels = 40_000_000

// DefaultRoots is the component root list when Options.Roots is empty. It
// returns a fresh slice on every call so no importer can change the default
// of every engine built afterwards in the same process.
func DefaultRoots() []string { return []string{"components"} }

// Options configures an Engine. Every field is optional: the zero
// value renders pages given with WithSource and imports nothing.
type Options struct {
	// FS is the project root. `~/` in every path means its root (A1, X7).
	// Pass an os.Root-backed FS to block symlink escape (X2).
	FS fs.FS
	// Roots are directories whose components register by file name; the
	// earlier root wins a name clash. Default: ["components"].
	Roots []string
	// Libraries are component libraries used by name with no import in each
	// page: each is a folder with a brick-library.yaml,
	// in Options.FS (Dir) or embedded (FS). Their components register after
	// Roots, in this order, so the project always wins a name and an
	// earlier library wins over a later one; every shadowing is logged.
	Libraries []Library
	// LayoutDir holds named layouts. Default "layouts".
	LayoutDir string
	// Unknown is what an unresolved component tag does.
	Unknown UnknownPolicy
	// Syntax restricts tag spellings; the default accepts both.
	Syntax Syntax
	// Navigation turns internal links and forms into in-place page updates
	//: {{ assets }} then links the runtime on every page.
	// Off by default.
	Navigation bool
	// Runtime tunes the browser runtime (debounce, poll floor, action URL,
	// retries), written into the page as a brick-config meta. The zero
	// value writes nothing and keeps every default.
	Runtime RuntimeConfig
	// States holds stateful components' state between actions
	//, registered as the store named
	// "default". Nil makes a page with a stateful component an error;
	// NewMemoryStates keeps state in this process, NewSealedStates seals it
	// into the page so it survives restarts.
	States StateStore
	// StateStores are named stores a component picks with
	// brick: {store: name}; an unknown name is a compile error. "default"
	// is reserved for States.
	StateStores map[string]StateStore
	// Codec encodes instances for byte and sealed stores that set none;
	// nil means JSONCodec (the stable v1 encoding).
	Codec Codec
	// Sessions answers "who is this request?":
	// stateful instances are bound to the SHA-256 of its answer, and an
	// action from another session finds them "not found". Nil means no
	// binding; renders take the id through WithSession.
	Sessions SessionBinder
	// ActionMiddleware wraps every action, outermost first: auth re-checks,
	// rate limits, audit, timeouts (package actionmw has the common ones).
	ActionMiddleware []ActionMiddleware
	// ActionRequest gives every action its request-scoped render options
	// (Provide the signed-in user, WithLocale), applied before the
	// re-render, so an action sees the user signed in now; request values
	// are never stored with an instance (4.5).
	ActionRequest func(r *http.Request) []RenderOption
	// ActionErrors replaces the action endpoint's error envelope (status
	// and body) for a host whose gateway expects its own; nil is the
	// default mapping (ActionMiddleware documents it).
	ActionErrors func(w http.ResponseWriter, r *http.Request, err error)
	// MaxActionBytes bounds an action request's JSON, and a form post's
	// body. Zero means DefaultMaxActionBytes.
	MaxActionBytes int
	// NoFormPosts turns the no-JavaScript fallback for brick:submit off
	//: forms are not rewritten to post, and FormPost refuses.
	// Off by default, so a form works before the runtime runs.
	NoFormPosts bool
	// MaxEffectBytes caps the encoded effects one action records
	//: events carry ids, not records. Zero means
	// DefaultMaxEffectBytes; over it the action fails.
	MaxEffectBytes int
	// RedirectHosts are the extra hosts an absolute redirect, navigate or
	// download URL may name ("example.com", "app.example.com:8443"): an
	// action's effects and a loader's brick.redirect alike
	// (Engine.RedirectLocation). Empty means same origin only, so
	// brick.redirect(props["next"]) can never be an open redirect (4.7).
	RedirectHosts []string
	// Validator checks brick: {rules:}: Check at
	// compile, Validate at render and from brick.validate. nil means
	// validate.Default(), stdlib only, English messages.
	Validator Validator
	// Uploads stores the files brick:upload sends.
	// nil means uploads are off: a component declaring brick: {uploads:}
	// fails to compile, so a theme never silently drops files.
	Uploads UploadSink
	// UploadCheck is an optional per-file check run on the
	// first bytes of every proxied file, after the accept/size checks and
	// before the sink opens, so a refused file is never stored. It is an
	// option rather than a sink upgrade because a host wrapping its sink
	// to add a check would hide the sink's UploadRemover and
	// UploadPresigner. nil means no check.
	UploadCheck UploadCheck
	// Remember keeps brick: {remember:} fields per session beyond an
	// instance's life. nil means remembering is off: a
	// component declaring remember: fails to compile, so nothing is ever
	// silently forgotten. NewMemoryRemember serves development and tests.
	Remember Rememberer
	// Push is server push: components that declare
	// brick: {listen:} refresh when a topic they listen to is published.
	// nil means push is off: a component declaring listen: fails to
	// compile, so a page never silently stops updating. NewPush makes one;
	// it is a pointer so a site's reloaded engines share its connections.
	Push *Push
	// Jobs runs background jobs that actions start with brick.start_job and Go
	// code with Engine.StartJob: the action answers at once and
	// the job publishes its topic through Push when it ends. Nil means jobs are
	// off and starting one is an error (ErrJobsOff). A pointer, like Push, so a
	// site's reload keeps the one running pool instead of orphaning its workers.
	Jobs *Jobs
	// LockState closes every stateful component's state to the client: a
	// browser may write only the fields a component lists in
	// brick: {writable:} (plus its upload and query fields, which the
	// client writes by declaration). A component overrides it with
	// brick: {lock_state:}. Off by default, so existing sites keep their
	// open fields; turning it on makes the write surface explicit, and a
	// literal binding to a field left out fails to compile naming the fix.
	LockState bool
	// StyleTransform rewrites each <brick:style> block's CSS at compile
	// time (a minifier, a fallback for browsers without @scope, another
	// scoping strategy). It receives the block and the CSS the engine would
	// ship and returns what ships instead. Nil ships the engine's CSS. It
	// must be deterministic: its output is content-hashed into the asset
	// URL (same input, same bytes).
	StyleTransform func(StyleBlock) ([]byte, error)
	// IslandTickets issues and redeems lazy islands' tickets.
	// nil means sealed with the Sealer of a NewSealedStates Options.States,
	// otherwise kept in this process's memory (a Dev warning says so).
	IslandTickets IslandTickets
	// Rand replaces crypto/rand for tokens, and Clock time.Now for issue
	// times — for tests only.
	Rand  io.Reader
	Clock Clock
	// EmitIDs writes each component's instance id on its root element as
	// data-brick="…". Off by default: it costs bytes on every component for
	// a feature v1 does not use; client components always carry it.
	EmitIDs bool
	// Passthrough names tags always emitted verbatim (S8).
	Passthrough []string
	// StrictParams makes map lookups of missing keys an error by default.
	StrictParams bool
	// Mode selects Dev or Prod error display.
	Mode Mode
	// Logger receives warnings; nil discards (A11). Warnings are also
	// returned as values, so dropping logs never hides one.
	Logger *slog.Logger
	// Site is the one read-only global, reachable as .Site. It is
	// deep-copied by New (A6).
	Site any
	// Messages are translation catalogs, by locale (see LoadMessages).
	Messages Messages
	// DefaultLocale is the locale when a render names none.
	DefaultLocale string
	// DataFormats parses data files by extension (".toml" → a TOML parser),
	// for use: imports and load(); JSON and YAML are built in.
	DataFormats map[string]DataFormat
	// Scripting runs `.star` modules and <brick:script> blocks. Nil means a
	// file using a script fails to compile with a clear error.
	Scripting Scripting
	// Markdown enables the built-in <Markdown> component.
	Markdown Markdown
	// Sanitizer is the policy behind the `sanitize` built-in. Nil makes
	// `sanitize` an error: there is no unsafe default.
	Sanitizer func(string) string
	// Funcs are template functions the host program adds in Go, e.g.
	// {{ money .Price }}. They join the built-ins for every file. A name a
	// built-in owns is an error from New (C12), as is a `__` name (T5) or a
	// value html/template cannot call; an inline <brick:script> function
	// with a host function's name is a compile error. A theme that calls a
	// function its host did not register fails to compile with an error
	// naming it, the same portability trade-off as a Starlark global.
	// Functions are shared by concurrent renders, so they must be safe for
	// concurrent use and pure: same input, same output (T8, E6).
	Funcs template.FuncMap
	// PropTypes are extra type names a `props:` declaration may use
	// (`to: email`), each a function that checks and converts one value.
	// A built-in type name is an error from New. A theme naming a type its
	// host did not register fails to compile with an error naming it.
	PropTypes map[string]PropType
	// Hooks are values implementing one or more hook interfaces
	// (BeforeRenderHook, AfterRenderHook, CompileHook, ErrorHook,
	// InstanceEndHook).
	Hooks []any
	// Limits (E4); zero means the default.
	MaxDepth       int
	MaxComponents  int
	MaxOutputBytes int
	MaxFileBytes   int
	// StreamWorkers bounds how many brick:lazy="stream" islands of one
	// response render at once: each holds a render's memory
	// and its loaders' connections, so a page with many slow islands must
	// not fan out without limit. Default DefaultStreamWorkers.
	StreamWorkers int
	// TimeZone is used by dateFormat. Default UTC (E6).
	TimeZone *time.Location
	// AssetBase is the URL prefix for component assets. Default "/_brick/".
	AssetBase string
	// ImageDirs are where {{ image }} may read from, even with a path that
	// came from data. Default ["static"].
	ImageDirs []string
	// ImageWidths are the widths {{ image }} makes by default.
	// Default 480, 960, 1440.
	ImageWidths []int
	// MaxImagePixels refuses larger images before decoding their pixels.
	// Default 40 million.
	MaxImagePixels int
	// ImageEncoders add output formats to {{ image }}, which a call names
	// after its widths: {{ image "~/static/p.png" 480 "webp" }}. JPEG and
	// PNG are built in; a call naming any other format needs its encoder
	// here (for example contrib/image-webp), or the render fails.
	ImageEncoders []ImageEncoder
}

// Engine renders pages. It is a snapshot (A5): each file is compiled once, on
// first use, and never re-read; call Reload to pick up edits. An Engine is
// safe for concurrent use (T8).
type Engine struct {
	opts  Options
	hooks hookSet
	// stores are the state stores by name ("default" = Options.States).
	stores map[string]StateStore
	// tickets are the lazy islands' tickets (Options.IslandTickets or the
	// default); ticketsMemory says the default keeps them in memory.
	tickets       IslandTickets
	ticketsMemory bool
	registry      *registry
	regErr        error
	regOnce       sync.Once
	// libs are the resolved Options.Libraries (manifests read), loaded once
	// on first use; libErr is why they could not be.
	libs     []*library
	libErr   error
	libsOnce sync.Once
	// manifests caches whether a folder holds a brick-library.yaml.
	manifests map[string]bool

	funcs     template.FuncMap
	funcsOnce sync.Once

	mu      sync.Mutex
	files   map[fileKey]*fileEntry
	metas   map[string]*metaEntry
	scripts map[string]*scriptEntry

	assetFiles   map[string]assetFile
	assetsByFile map[string][]asset
	images       map[string]*imageEntry
	// imageEncoders are Options.ImageEncoders by format, checked at New.
	imageEncoders map[string]ImageEncoder
	runtime       []asset
	runtimeOnce   sync.Once

	warnMu   sync.Mutex
	warnings []Diagnostic
	warned   map[string]bool
}

// fileKey identifies one compiled file: its path and the locale whose
// variants were chosen (C16).
type fileKey struct {
	path   string
	locale string
}

// fileEntry compiles one file exactly once, even under concurrent renders.
type fileEntry struct {
	once sync.Once
	file *compiled
	err  error
}

// New validates options and returns an engine (A4).
func New(opts Options) (*Engine, error) {
	if opts.LayoutDir == "" {
		opts.LayoutDir = DefaultLayoutDir
	}
	if len(opts.Roots) == 0 {
		opts.Roots = DefaultRoots()
	}
	if opts.MaxDepth <= 0 {
		opts.MaxDepth = DefaultMaxDepth
	}
	if opts.MaxComponents <= 0 {
		opts.MaxComponents = DefaultMaxComponents
	}
	if opts.MaxOutputBytes <= 0 {
		opts.MaxOutputBytes = DefaultMaxOutputBytes
	}
	if opts.StreamWorkers <= 0 {
		opts.StreamWorkers = DefaultStreamWorkers
	}
	if err := opts.Runtime.check(); err != nil {
		return nil, err
	}
	if opts.MaxActionBytes <= 0 {
		opts.MaxActionBytes = DefaultMaxActionBytes
	}
	if opts.MaxEffectBytes <= 0 {
		opts.MaxEffectBytes = DefaultMaxEffectBytes
	}
	if opts.Validator == nil {
		opts.Validator = validate.Default()
	}
	if opts.Clock == nil {
		opts.Clock = time.Now
	}
	if opts.MaxFileBytes <= 0 {
		opts.MaxFileBytes = DefaultMaxFileBytes
	}
	if opts.TimeZone == nil {
		opts.TimeZone = time.UTC
	}
	if opts.AssetBase == "" {
		opts.AssetBase = DefaultAssetBase
	}
	if len(opts.ImageDirs) == 0 {
		opts.ImageDirs = DefaultImageDirs()
	}
	for _, d := range opts.ImageDirs {
		if err := checkDir(d); err != nil {
			return nil, fmt.Errorf("brick: Options.ImageDirs: %w", err)
		}
	}
	if len(opts.ImageWidths) == 0 {
		opts.ImageWidths = DefaultImageWidths()
	}
	if opts.MaxImagePixels <= 0 {
		opts.MaxImagePixels = DefaultMaxImagePixels
	}
	imageEncoders, err := checkImageEncoders(opts.ImageEncoders)
	if err != nil {
		return nil, err
	}
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	for _, r := range opts.Roots {
		if err := checkDir(r); err != nil {
			return nil, fmt.Errorf("brick: Options.Roots: %w", err)
		}
	}
	if err := checkDir(opts.LayoutDir); err != nil {
		return nil, fmt.Errorf("brick: Options.LayoutDir: %w", err)
	}
	// Cloned before prepareLibraries rewrites each entry (an FS library
	// becomes a Dir under brick_libs/), so the caller's slice is untouched
	// and the same Options build a second engine — the site layer rebuilds
	// on every change.
	opts.Libraries = slices.Clone(opts.Libraries)
	if err := prepareLibraries(&opts); err != nil {
		return nil, err
	}
	if opts.DefaultLocale != "" {
		tag, ok := normalizeLocale(opts.DefaultLocale)
		if !ok {
			return nil, fmt.Errorf("brick: Options.DefaultLocale %q is not a language tag", opts.DefaultLocale)
		}
		opts.DefaultLocale = tag
		if opts.Messages != nil && !opts.Messages.has(tag) {
			return nil, fmt.Errorf("brick: Options.DefaultLocale %q has no message catalog (E9)", tag)
		}
	}
	hooks, err := newHookSet(opts.Hooks)
	if err != nil {
		return nil, err
	}
	stores, err := buildStores(&opts)
	if err != nil {
		return nil, err
	}
	if err := checkHostFuncs(opts.Funcs); err != nil {
		return nil, err
	}
	if err := checkPropTypes(opts.PropTypes); err != nil {
		return nil, err
	}
	opts.ActionMiddleware = slices.Clone(opts.ActionMiddleware)
	opts.Funcs = maps.Clone(opts.Funcs)
	opts.PropTypes = maps.Clone(opts.PropTypes)
	opts.Site = deepCopy(opts.Site)
	opts.Roots = slices.Clone(opts.Roots)
	opts.Passthrough = slices.Clone(opts.Passthrough)
	tickets, memory := opts.IslandTickets, false
	if tickets == nil {
		tickets, memory = defaultTickets(&opts)
	}
	e := &Engine{
		opts:          opts,
		hooks:         hooks,
		stores:        stores,
		tickets:       tickets,
		ticketsMemory: memory,
		files:         map[fileKey]*fileEntry{},
		metas:         map[string]*metaEntry{},
		warned:        map[string]bool{},
		imageEncoders: imageEncoders,
	}
	// Last, so only an engine that was built takes the stores' end reports.
	e.watchEnds()
	return e, nil
}

// Reload returns a fresh engine with the same options, which re-reads every
// file on first use (A5). Renders in flight on the old engine finish on it.
func (e *Engine) Reload(ctx context.Context) (*Engine, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	opts := e.opts
	return New(opts)
}

// Warnings returns every compile warning this snapshot has produced so far,
// each once (A11).
func (e *Engine) Warnings() []Diagnostic {
	e.warnMu.Lock()
	defer e.warnMu.Unlock()
	return slices.Clone(e.warnings)
}

// warn records a compile warning once per snapshot and logs it.
func (e *Engine) warn(d Diagnostic) {
	d.Severity = SeverityWarning
	key := d.String()
	e.warnMu.Lock()
	if e.warned[key] {
		e.warnMu.Unlock()
		return
	}
	e.warned[key] = true
	e.warnings = append(e.warnings, d)
	e.warnMu.Unlock()
	e.opts.Logger.Warn(d.Message, "file", d.File, "line", d.Line, "code", string(d.Code))
}

// deepCopy copies maps and slices so a host mutating its value after New
// cannot race renders (A6). Other values are returned as they are.
func deepCopy(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			out[k] = deepCopy(val)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, val := range x {
			out[i] = deepCopy(val)
		}
		return out
	case nil:
		return nil
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Map:
		if rv.IsNil() {
			return v
		}
		out := reflect.MakeMapWithSize(rv.Type(), rv.Len())
		iter := rv.MapRange()
		for iter.Next() {
			out.SetMapIndex(iter.Key(), reflectCopy(iter.Value()))
		}
		return out.Interface()
	case reflect.Slice:
		if rv.IsNil() {
			return v
		}
		out := reflect.MakeSlice(rv.Type(), rv.Len(), rv.Len())
		for i := range rv.Len() {
			out.Index(i).Set(reflectCopy(rv.Index(i)))
		}
		return out.Interface()
	}
	return v
}

func reflectCopy(v reflect.Value) reflect.Value {
	if !v.IsValid() || !v.CanInterface() {
		return v
	}
	c := deepCopy(v.Interface())
	if c == nil {
		return reflect.Zero(v.Type())
	}
	return reflect.ValueOf(c).Convert(v.Type())
}

// cloneMap copies a map one level deep (nil stays empty).
func cloneMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	maps.Copy(out, m)
	return out
}

// AssetBase is the URL prefix component assets are served under
// (Options.AssetBase, default /_brick/), for a server that mounts Assets().
func (e *Engine) AssetBase() string { return e.opts.AssetBase }
