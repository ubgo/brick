package brick

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Version is this engine's version, checked against a theme's `brick:`
// pin in brick.yaml (A14).
const Version = "0.1.0"

// siteDataKey is where LoadSite puts the data directory: .Site.Data.
const siteDataKey = "Data"

// LoadOption tunes LoadSite and LoadMessages.
type LoadOption func(*loadOptions)

type loadOptions struct {
	lookup  func(string) (string, bool)
	formats map[string]DataFormat
}

// WithDataFormat adds a parser for a data file extension (".toml"), the
// same hook as Options.DataFormats, for LoadSite and LoadMessages.
func WithDataFormat(ext string, parse DataFormat) LoadOption {
	return func(o *loadOptions) {
		if o.formats == nil {
			o.formats = map[string]DataFormat{}
		}
		o.formats[strings.ToLower(ext)] = parse
	}
}

// WithEnv replaces the environment lookup used for `${VAR}` (for tests, or a
// host that keeps configuration elsewhere). The default is os.LookupEnv.
func WithEnv(lookup func(string) (string, bool)) LoadOption {
	return func(o *loadOptions) { o.lookup = lookup }
}

// LoadSite reads the site config and data directory into the value for
// Options.Site. Nothing is discovered: the host names both.
//
//   - `${VAR}` and `${VAR:-default}` are interpolated in config string values
//     after parsing, one pass, no recursion, so an environment value can never
//     inject YAML keys; a missing variable with no default is an error. Only
//     the config file is interpolated, never data files (D10).
//   - Every data file becomes .Site.Data.<name>; subdirectories nest; the same
//     name in two formats is an error; dotfiles are ignored; any other file
//     is an error (D11).
//
// Either argument may be "" to skip it.
func LoadSite(fsys fs.FS, configPath, dataDir string, opts ...LoadOption) (map[string]any, error) {
	so := loadOptions{lookup: os.LookupEnv}
	for _, o := range opts {
		o(&so)
	}
	site := map[string]any{}
	if configPath != "" {
		p := fsDir(configPath)
		b, err := fs.ReadFile(fsys, p)
		if err != nil {
			return nil, fmt.Errorf("brick: LoadSite: %w", err)
		}
		v, err := parseData(p, b, so.formats)
		if err != nil {
			return nil, fmt.Errorf("brick: LoadSite: %w", err)
		}
		m, ok := v.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("brick: LoadSite: %s must be a map", p)
		}
		iv, err := interpolate(m, so.lookup)
		if err != nil {
			return nil, fmt.Errorf("brick: LoadSite: %s: %w", p, err)
		}
		site, _ = iv.(map[string]any)
	}
	if dataDir != "" {
		if _, clash := site[siteDataKey]; clash {
			return nil, fmt.Errorf("brick: LoadSite: the config has a %q key, which is where the data directory goes", siteDataKey)
		}
		data, err := loadDataDir(fsys, fsDir(dataDir), so.formats)
		if err != nil {
			return nil, fmt.Errorf("brick: LoadSite: %w", err)
		}
		site[siteDataKey] = data
	}
	return site, nil
}

// envRE matches `${NAME}` and `${NAME:-default}`.
var envRE = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)(?::-([^}]*))?\}`)

// interpolate replaces `${VAR}` in every string value, once (D10).
func interpolate(v any, lookup func(string) (string, bool)) (any, error) {
	switch x := v.(type) {
	case string:
		var missing []string
		out := envRE.ReplaceAllStringFunc(x, func(m string) string {
			sub := envRE.FindStringSubmatch(m)
			if val, ok := lookup(sub[1]); ok {
				return val
			}
			if strings.Contains(m, ":-") {
				return sub[2]
			}
			missing = append(missing, sub[1])
			return m
		})
		if len(missing) > 0 {
			return nil, fmt.Errorf("${%s} is not set and has no default (write ${%s:-value})", missing[0], missing[0])
		}
		return out, nil
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			iv, err := interpolate(val, lookup)
			if err != nil {
				return nil, err
			}
			out[k] = iv
		}
		return out, nil
	case []any:
		out := make([]any, len(x))
		for i, val := range x {
			iv, err := interpolate(val, lookup)
			if err != nil {
				return nil, err
			}
			out[i] = iv
		}
		return out, nil
	}
	return v, nil
}

// loadDataDir reads a data directory into nested maps (D11).
func loadDataDir(fsys fs.FS, dir string, formats map[string]DataFormat) (map[string]any, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, err
	}
	out := map[string]any{}
	from := map[string]string{}
	for _, en := range entries {
		name := en.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		p := path.Join(dir, name)
		key := strings.TrimSuffix(name, pathExt(name))
		if en.IsDir() {
			key = name
		}
		if prev, ok := from[key]; ok {
			return nil, fmt.Errorf("%s and %s both define .Data.%s", displayPath(prev), displayPath(p), key)
		}
		from[key] = p
		if en.IsDir() {
			sub, err := loadDataDir(fsys, p, formats)
			if err != nil {
				return nil, err
			}
			out[key] = sub
			continue
		}
		ext := strings.ToLower(pathExt(name))
		if _, custom := formats[ext]; !custom && ext != extJSON && ext != extYAML && ext != extYML {
			return nil, fmt.Errorf("%s is not a data file (.json, .yaml, or a WithDataFormat extension)", displayPath(p))
		}
		b, err := fs.ReadFile(fsys, p)
		if err != nil {
			return nil, err
		}
		v, err := parseData(p, b, formats)
		if err != nil {
			return nil, err
		}
		out[key] = v
	}
	return out, nil
}

// Config is the optional brick.yaml (A3): engine settings written as a file.
// Nothing reads it unless the host calls LoadConfig and Apply.
type Config struct {
	Roots        []string `yaml:"roots"`
	LayoutDir    string   `yaml:"layoutDir"`
	Unknown      string   `yaml:"unknown"`
	Syntax       string   `yaml:"syntax"`
	Passthrough  []string `yaml:"passthrough"`
	StrictParams *bool    `yaml:"strictParams"`
	// LockState closes every stateful component's state to the client
	// except its brick: {writable:} fields (Options.LockState).
	LockState     *bool  `yaml:"lockState"`
	DefaultLocale string `yaml:"defaultLocale"`
	AssetBase     string `yaml:"assetBase"`
	// Brick pins the engine version a theme needs, e.g. ">=0.1, <2" (A14).
	Brick string `yaml:"brick"`
	// Navigation turns on in-place page navigation.
	Navigation *bool `yaml:"navigation"`
	// Tailwind is read by the brick CLI (build and serve), not the engine.
	Tailwind *TailwindConfig `yaml:"tailwind"`
	// Libraries are the component libraries every page may use by name
	//, in precedence order. Each entry is a string or
	// a map with from/prefix/only; see LibrarySpec.
	Libraries []LibrarySpec `yaml:"libraries"`
	// Host names what the site's own Go program supplies, for programs that
	// are not that host (brick check, brick lsp). Read by the brick CLI, not
	// the engine: Apply ignores it, so a real render is unchanged.
	Host *HostConfig `yaml:"host"`
	// Sitemap turns on the site layer's built-in /sitemap.xml. Read by the
	// brick CLI (build and serve), not the engine.
	Sitemap *SitemapConfig `yaml:"sitemap"`
	// Markdown configures the brick CLI's markdown converter. Read by the
	// CLI, not the engine: a Go host picks its own Options.Markdown.
	Markdown *MarkdownConfig `yaml:"markdown"`
	// Fmt configures brick fmt (and the language server's formatting).
	// Read by the CLI, not the engine.
	Fmt *FmtConfig `yaml:"fmt"`
}

// MarkdownConfig is the `markdown:` section of brick.yaml. Every switch is
// off by default, so a site without the section converts markdown to the
// same bytes as before.
type MarkdownConfig struct {
	// HeadingIDs writes an id on every heading, so .Page.Headings has IDs
	// and a table of contents can link to them.
	HeadingIDs bool `yaml:"headingIDs"`
	// Highlight highlights fenced code blocks with CSS classes.
	Highlight bool `yaml:"highlight"`
}

// SitemapConfig is the `sitemap:` section of brick.yaml. The section being
// present turns the sitemap on; `sitemap: {}` takes every default.
type SitemapConfig struct {
	// BaseURL is the absolute URL the sitemap's locations start with, e.g.
	// https://example.com. Empty reads baseURL from site.yaml, the key a
	// theme's own templates already use for absolute links.
	BaseURL string `yaml:"baseURL"`
}

// HostConfig is the `host:` section of brick.yaml. A site whose Go program
// grants capabilities (scriptstarlark.Options.Globals, Options.Uploads) cannot
// be checked by a program that lacks them: every script naming one fails to
// resolve. Declaring them lets that program put a stand-in in each place,
// so the site compiles and everything else is checked; a stand-in that is
// actually used fails with ErrHostOnly. Only names listed here resolve, so
// a misspelled global is still an error.
type HostConfig struct {
	// Globals are the script globals the host predeclares, e.g. [db, now].
	Globals []string `yaml:"globals"`
	// Uploads says the host configures an UploadSink (Options.Uploads), so
	// components with brick: {uploads:} compile.
	Uploads bool `yaml:"uploads"`
}

// LibrarySpec is one `libraries:` entry of brick.yaml. From is one of:
//
//   - "~/ui": a folder inside the project;
//   - "github.com/acme/ui@v1.2.0" (or ".../path@v1.2.0"): a remote
//     library, vendored by `brick vendor`;
//   - "brick-kit": a bare name, one of the libraries the host offers to
//     Apply (the brick CLI offers the kit). Nothing is looked up anywhere
//     else, so a name never reaches a library the host did not pass.
//
// Written as a plain string, the entry is just From.
type LibrarySpec struct {
	From   string   `yaml:"from"`
	Prefix string   `yaml:"prefix"`
	Only   []string `yaml:"only"`
}

// UnmarshalYAML accepts a plain string (From alone) or the full map.
func (l *LibrarySpec) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind == yaml.ScalarNode {
		l.From = n.Value
		return nil
	}
	type plain LibrarySpec
	var p plain
	if err := n.Decode(&p); err != nil {
		return err
	}
	*l = LibrarySpec(p)
	return nil
}

// library turns the entry into a Library, choosing among offered for a
// bare name.
func (l LibrarySpec) library(offered []Library) (Library, error) {
	var lib Library
	switch {
	case l.From == "":
		return lib, fmt.Errorf("brick.yaml: libraries: an entry has no from")
	case strings.HasPrefix(l.From, rootPrefix):
		lib = Library{Dir: strings.TrimPrefix(l.From, rootPrefix)}
	case strings.Contains(l.From, "@"):
		r, err := RemoteLibrary(l.From)
		if err != nil {
			return lib, fmt.Errorf("brick.yaml: libraries: %w", err)
		}
		lib = r
	default:
		i := slices.IndexFunc(offered, func(o Library) bool { return o.Name == l.From })
		if i < 0 {
			names := make([]string, 0, len(offered))
			for _, o := range offered {
				names = append(names, o.Name)
			}
			return lib, fmt.Errorf("brick.yaml: libraries: %q is not a ~/ folder, a remote library (host/owner/repo@version), or a library this program offers (%s)", l.From, cmpOrStr(strings.Join(names, ", "), "none"))
		}
		lib = offered[i]
	}
	if l.Prefix != "" {
		lib.Prefix = l.Prefix
	}
	if len(l.Only) > 0 {
		lib.Only = slices.Clone(l.Only)
	}
	return lib, nil
}

// TailwindConfig is the `tailwind:` section of brick.yaml.
type TailwindConfig struct {
	// Input is the source CSS, e.g. styles/app.css.
	Input string `yaml:"input"`
	// Output is the built CSS, normally under static/.
	Output string `yaml:"output"`
	// Binary is the tailwindcss executable; empty looks it up on PATH.
	Binary string `yaml:"binary"`
}

// LoadConfig reads brick.yaml. Unknown keys are an error, so a typo in a
// setting never silently does nothing.
func LoadConfig(fsys fs.FS, p string) (Config, error) {
	var c Config
	b, err := fs.ReadFile(fsys, fsDir(p))
	if err != nil {
		return c, fmt.Errorf("brick: LoadConfig: %w", err)
	}
	dec := yaml.NewDecoder(strings.NewReader(string(b)))
	dec.KnownFields(true)
	if err := dec.Decode(&c); err != nil && !errors.Is(err, io.EOF) {
		return c, fmt.Errorf("brick: LoadConfig: %s: %w", p, err)
	}
	if c.Brick != "" {
		ok, err := versionSatisfies(Version, c.Brick)
		if err != nil {
			return c, fmt.Errorf("brick: LoadConfig: brick: %w", err)
		}
		if !ok {
			return c, fmt.Errorf("brick: LoadConfig: the theme needs brick %s, this is %s (A14)", c.Brick, Version)
		}
	}
	return c, nil
}

// Apply copies the settings that are set into opts.
//
// offered are the libraries a bare name in `libraries:` may pick (the brick
// CLI offers the kit, as "brick-kit"); a host offering none can still use
// ~/ folders and remote libraries.
func (c Config) Apply(opts *Options, offered ...Library) error {
	if len(c.Roots) > 0 {
		opts.Roots = c.Roots
	}
	for _, spec := range c.Libraries {
		lib, err := spec.library(offered)
		if err != nil {
			return err
		}
		opts.Libraries = append(opts.Libraries, lib)
	}
	if c.LayoutDir != "" {
		opts.LayoutDir = c.LayoutDir
	}
	switch c.Unknown {
	case "":
	case wordError:
		opts.Unknown = UnknownError
	case wordPassthrough:
		opts.Unknown = UnknownPassthrough
	default:
		return fmt.Errorf("brick.yaml: unknown: must be error or passthrough")
	}
	switch c.Syntax {
	case "":
	case wordBoth:
		opts.Syntax = SyntaxBoth
	case wordCapitalized:
		opts.Syntax = SyntaxCapitalized
	case wordHyphenated:
		opts.Syntax = SyntaxHyphenated
	default:
		return fmt.Errorf("brick.yaml: syntax: must be capitalized, hyphenated or both")
	}
	opts.Passthrough = append(opts.Passthrough, c.Passthrough...)
	if c.StrictParams != nil {
		opts.StrictParams = *c.StrictParams
	}
	if c.Navigation != nil {
		opts.Navigation = *c.Navigation
	}
	if c.LockState != nil {
		opts.LockState = *c.LockState
	}
	if c.DefaultLocale != "" {
		opts.DefaultLocale = c.DefaultLocale
	}
	if c.AssetBase != "" {
		opts.AssetBase = c.AssetBase
	}
	return nil
}

// versionSatisfies checks a comma-separated constraint list (">=1.2, <2").
func versionSatisfies(v, constraint string) (bool, error) {
	have, err := parseVersion(v)
	if err != nil {
		return false, err
	}
	for part := range strings.SplitSeq(constraint, ",") {
		part = strings.TrimSpace(part)
		op := ""
		for _, o := range []string{">=", "<=", ">", "<", "="} {
			if strings.HasPrefix(part, o) {
				op = o
				break
			}
		}
		if op == "" {
			return false, fmt.Errorf("%q: start each constraint with >=, >, <=, < or =", part)
		}
		want, err := parseVersion(strings.TrimSpace(strings.TrimPrefix(part, op)))
		if err != nil {
			return false, err
		}
		c := compareVersions(have, want)
		ok := map[string]bool{">=": c >= 0, "<=": c <= 0, ">": c > 0, "<": c < 0, "=": c == 0}[op]
		if !ok {
			return false, nil
		}
	}
	return true, nil
}

func parseVersion(s string) ([3]int, error) {
	var out [3]int
	parts := strings.Split(strings.TrimPrefix(s, "v"), ".")
	if len(parts) == 0 || len(parts) > 3 {
		return out, fmt.Errorf("%q is not a version", s)
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return out, fmt.Errorf("%q is not a version", s)
		}
		out[i] = n
	}
	return out, nil
}

func compareVersions(a, b [3]int) int {
	for i := range a {
		if a[i] != b[i] {
			if a[i] < b[i] {
				return -1
			}
			return 1
		}
	}
	return 0
}
