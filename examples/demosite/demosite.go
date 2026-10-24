// Package demosite builds the engine feature demo in examples/site, wired
// the way a real host would: an os.Root-confined project, Starlark with one
// host capability, goldmark, bluemonday, islands filled per request,
// actions and navigation. It is a package, not just the examples command,
// so another host can mount the demo beside other sites (see ./ui -engine).
package demosite

import (
	"bytes"
	"context"
	"net/http"
	"os"
	"strings"
	"time"

	"go.starlark.net/starlark"

	"github.com/ubgo/brick"
	markdowngoldmark "github.com/ubgo/brick/contrib/markdown-goldmark"
	sanitizebluemonday "github.com/ubgo/brick/contrib/sanitize-bluemonday"
	scriptstarlark "github.com/ubgo/brick/contrib/script-starlark"
	"github.com/ubgo/brick/kit"
	"github.com/ubgo/brick/site"
)

// configFile is the project's brick.yaml, read and applied the way the
// brick CLI does (libraries, locale, navigation).
const configFile = "brick.yaml"

// defaultDir is the project directory New uses when Options.Dir is empty:
// examples/site, relative to the examples module.
const defaultDir = "site"

// headOpen is where generator inserts its meta tag.
const headOpen = "<head>"

// UserParam is the query parameter that stands in for a signed-in user in
// the demo (a real host reads its session): /?as=Sara.
const UserParam = "as"

// Options are what a host chooses about the demo; nothing is required.
type Options struct {
	// Dir is the project directory. Default defaultDir ("site").
	Dir string
	// Dev turns on live reload, the playground and inline errors.
	Dev bool
	// BasePath and KeepPaths host the demo under a path (site.Options).
	BasePath  string
	KeepPaths []string
	// StateKeys seal stateful components' state into the page
	// (site.Options.StateKeys); StateKeys(…) reads them from BRICK_SECRET.
	StateKeys brick.KeyRing
}

// SecretEnv holds the state keys, as for the brick CLI: comma-separated
// base64, newest first.
const SecretEnv = "BRICK_SECRET"

// StateKeys returns the keys for stateful components, as
// the brick CLI chooses them: BRICK_SECRET when set; when serving without
// it, a key made for this process (never written anywhere) with a warning
// through logf, since a restart then invalidates state (it recovers in
// place); otherwise none, which keeps state in memory and makes Export
// refuse stateful pages. lookup is the environment (os.LookupEnv), passed
// in so the package reads no global state.
func StateKeys(lookup func(string) (string, bool), serving bool, logf func(format string, args ...any)) (brick.KeyRing, error) {
	if v, ok := lookup(SecretEnv); ok && strings.TrimSpace(v) != "" {
		return brick.KeysFromEnv(lookup, SecretEnv)
	}
	if !serving {
		return nil, nil
	}
	logf("warning: %s is not set: state is sealed with a key made for this process, so a restart invalidates it (recovered in place); brick keygen prints a key to keep", SecretEnv)
	k, err := brick.NewStateKey()
	if err != nil {
		return nil, err
	}
	return brick.StaticKeys(k)
}

// generator is an AfterRender hook: it adds a meta tag to every page. Hooks
// may change static chunks only, never the nonce or asset markers (A9).
type generator struct{}

func (generator) AfterRender(_ context.Context, _ *brick.Page, r *brick.Result) error {
	for i, c := range r.Chunks {
		if c.Kind == brick.ChunkBytes && bytes.Contains(c.Bytes, []byte(headOpen)) {
			r.Chunks[i].Bytes = bytes.Replace(c.Bytes, []byte(headOpen), []byte(headOpen+`<meta name="generator" content="brick">`), 1)
			break
		}
	}
	return nil
}

// now is the one capability the demo grants scripts: the clock, which a
// script has no other way to read.
var now = starlark.NewBuiltin("now", func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
	return starlark.String(time.Now().UTC().Format(time.TimeOnly)), nil
})

// New builds the demo site exactly as the examples tests do.
func New(ctx context.Context, o Options) (*site.Site, error) {
	if o.Dir == "" {
		o.Dir = defaultDir
	}
	root, err := os.OpenRoot(o.Dir)
	if err != nil {
		return nil, err
	}
	sc, err := scriptstarlark.New(scriptstarlark.Options{Globals: starlark.StringDict{"now": now}})
	if err != nil {
		return nil, err
	}
	eng := brick.Options{
		Scripting:     sc,
		Markdown:      markdowngoldmark.New(),
		Sanitizer:     sanitizebluemonday.New(),
		DefaultLocale: "en",
		Navigation:    true,
		Hooks:         []any{generator{}},
	}
	// brick.yaml adds what the site declares — its component libraries —
	// on top; the host offers the kit by name, as the CLI does.
	cfg, err := brick.LoadConfig(root.FS(), configFile)
	if err != nil {
		return nil, err
	}
	if err := cfg.Apply(&eng, kit.Library()); err != nil {
		return nil, err
	}
	return site.New(ctx, site.Options{
		FS:        root.FS(),
		Engine:    eng,
		Locales:   []string{"en", "fr"},
		Dev:       o.Dev,
		BasePath:  o.BasePath,
		KeepPaths: o.KeepPaths,
		StateKeys: o.StateKeys,
		// Islands see the signed-in user; the rest of each page is cached.
		Request: func(r *http.Request) []brick.RenderOption {
			return []brick.RenderOption{brick.Provide("user", r.URL.Query().Get(UserParam))}
		},
	})
}
