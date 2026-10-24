// Package site turns a brick project folder into a site: every page gets a
// URL, pages are cached and invalidated by the files they read, the site is
// served with live reload in development, and it can be exported as static
// files.
//
//	s, err := site.New(ctx, site.Options{FS: root.FS(), Engine: brick.Options{Markdown: md}})
//	http.ListenAndServe(":8080", s.Handler())
//
// The engine does not change shape: site is a layer on top of brick.Engine.
// It owns building the engine, because .Site.Pages — the page index — is
// part of the engine's read-only .Site and is rebuilt, never mutated, when
// files change.
package site

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"maps"
	"net/http"
	"path"
	"reflect"
	"slices"
	"sync"
	"time"

	"github.com/ubgo/brick"
	"github.com/ubgo/brick/watch"
)

// Default locations inside the project. Each is an Options field.
const (
	DefaultContentDir  = "content"
	DefaultStaticDir   = "static"
	DefaultConfigFile  = "site.yaml"
	DefaultDataDir     = "data"
	DefaultMessagesDir = "i18n"
	// pagesKey is where the page index appears in .Site.
	pagesKey = "Pages"
)

// Options configures a Site. FS is the only required field.
type Options struct {
	// FS is the project root, as for brick.Options.FS.
	FS fs.FS
	// Engine is the engine configuration. FS, Site and Messages are filled
	// by the site layer; everything else (Markdown, Scripting, Sanitizer,
	// Hooks, limits…) passes through.
	Engine brick.Options
	// ContentDir holds the pages. Default "content".
	ContentDir string
	// StaticDir is served as is at the site root. Default "static".
	StaticDir string
	// ConfigFile and DataDir are loaded into .Site with brick.LoadSite when
	// they exist. Defaults "site.yaml" and "data".
	ConfigFile string
	DataDir    string
	// MessagesDir is loaded with brick.LoadMessages when it exists.
	MessagesDir string
	// Locales lists the languages with their own URLs; the first is the
	// default and has no prefix (/about/, /fr/about/). Empty means one
	// language and no prefixes.
	Locales []string
	// Drafts includes pages with draft: true.
	Drafts bool
	// Dev turns on live reload, the component playground and inline errors.
	Dev bool
	// Logger receives change and reload events. Nil discards.
	Logger *slog.Logger
	// Request returns per-request render options — typically
	// brick.Provide("user", currentUser(r)) — used to fill islands on every
	// response. The cached page is shared; only islands see
	// these values, and a page marked brick: {per_request: true}, which is
	// rendered for each request with them instead of cached (v22). Nil means none. Actions get them too: when
	// Engine.ActionRequest is nil it is set to Request, so an action's
	// re-render sees the user signed in now, not the one from first render
	//.
	Request func(r *http.Request) []brick.RenderOption
	// StateKeys seal stateful components' state into the page with the default AES-GCM sealer: the ring's current key seals,
	// every key it can look up opens, so a restart or a second server with
	// the same ring answers any action. Used when Engine.States is nil. Nil
	// keeps state in this process, and the first stateful response logs a
	// warning saying what that costs. Keys never come from code:
	// brick.KeysFromEnv(os.LookupEnv, "BRICK_SECRET"), a KMS-backed ring, or
	// brick.StaticKeys fed from a secret manager. For another sealer or
	// store, set Engine.States.
	StateKeys brick.KeyRing
	// StateCacheControl is the Cache-Control header of a response that
	// holds a stateful component, which carries a handle or blob made for
	// that request. Empty means DefaultStateCacheControl.
	StateCacheControl string
	// PruneEvery is how often ListenAndServe asks each store that does not
	// expire instances itself (a brick.Pruner, such as a SQL table) to
	// delete expired ones; the engine starts no goroutine of its own. Zero
	// means DefaultPruneEvery; negative turns it off.
	PruneEvery time.Duration
	// BasePath hosts the site under a path, e.g. "/docs" or "/ui", so it can
	// live under a sub-path (a GitHub Pages project site) or beside other
	// sites on one server (Mount). The site is written as if at the root:
	// the site layer strips the base from requests and rewrites every
	// root-absolute URL in the HTML it serves and exports (base.go). Empty
	// or "/" is the root.
	BasePath string
	// KeepPaths are root-absolute URL prefixes the base rewrite leaves
	// alone — links to a sibling site on the same server ("/engine/"). The
	// one escape hatch for BasePath.
	KeepPaths []string
	// OnChange runs in Watch before each batch of changes is applied — the
	// hook for a build step such as a Tailwind rebuild. Its error is logged
	// and the batch is still applied.
	OnChange func(ctx context.Context, changes []watch.Change) error
	// Sitemap turns on the built-in /sitemap.xml (v14): served by Handler
	// and written by Export, listing every page with an absolute URL and
	// its hreflang alternates. Nil leaves it off. A project's own
	// content/sitemap.xml.html wins over it.
	Sitemap *Sitemap
}

// Site is a built site. It is safe for concurrent use; Apply swaps a new
// build in atomically while renders continue on the old one.
type Site struct {
	opts Options

	mu    sync.RWMutex
	state *state

	cacheMu sync.Mutex
	cache   map[cacheKey]*brick.Result

	live *hub
	// states holds stateful components' state when the engine options
	// name no store, made once so a rebuild keeps state.
	states brick.StateStore
	// tickets are the lazy islands' tickets when the engine options name
	// none, made once for the same reason.
	tickets brick.IslandTickets
	// warnMemory logs memoryWarning once.
	warnMemory sync.Once
}

// state is one immutable build: engine, index and routes.
type state struct {
	eng    *brick.Engine
	pages  []Page
	routes map[string]Route
	order  []string // URLs, sorted
	// patterns are the dynamic pages, in match order.
	patterns []*pattern
	diags    []brick.Diagnostic
	// trans are the static routes' translations by URL, and locales the
	// configured Options.Locales, for .Page.Translations.
	trans   map[string][]brick.Translation
	locales []string
	// sitemapBase is the resolved Sitemap base URL ("" with no sitemap),
	// and skipSitemap the dynamic page files left out of it (drafts and
	// brick: {sitemap: false}).
	sitemapBase string
	skipSitemap map[string]bool
}

// New builds a site: loads .Site, messages and the page index, routes every
// page, and creates the engine.
func New(ctx context.Context, opts Options) (*Site, error) {
	if opts.FS == nil {
		return nil, errors.New("site: Options.FS is required")
	}
	if opts.ContentDir == "" {
		opts.ContentDir = DefaultContentDir
	}
	if opts.StaticDir == "" {
		opts.StaticDir = DefaultStaticDir
	}
	if opts.ConfigFile == "" {
		opts.ConfigFile = DefaultConfigFile
	}
	if opts.DataDir == "" {
		opts.DataDir = DefaultDataDir
	}
	if opts.MessagesDir == "" {
		opts.MessagesDir = DefaultMessagesDir
	}
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	base, err := normalizeBase(opts.BasePath)
	if err != nil {
		return nil, err
	}
	opts.BasePath = base
	states, err := defaultStates(opts)
	if err != nil {
		return nil, err
	}
	tickets, err := defaultTickets(opts)
	if err != nil {
		return nil, err
	}
	s := &Site{opts: opts, cache: map[cacheKey]*brick.Result{}, live: newHub(), states: states, tickets: tickets}
	st, err := s.build(ctx)
	if err != nil {
		return nil, err
	}
	s.state = st
	return s, nil
}

// build makes a fresh state from the files on disk.
func (s *Site) build(ctx context.Context) (*state, error) {
	o := s.opts
	siteVal := map[string]any{}
	if exists(o.FS, o.ConfigFile) || isDir(o.FS, o.DataDir) {
		cfg, data := "", ""
		if exists(o.FS, o.ConfigFile) {
			cfg = o.ConfigFile
		}
		if isDir(o.FS, o.DataDir) {
			data = o.DataDir
		}
		v, err := brick.LoadSite(o.FS, cfg, data)
		if err != nil {
			return nil, err
		}
		siteVal = v
	}
	if base, ok := o.Engine.Site.(map[string]any); ok {
		merged := maps.Clone(base)
		maps.Copy(merged, siteVal)
		siteVal = merged
	}
	if _, clash := siteVal[pagesKey]; clash {
		return nil, fmt.Errorf("site: .Site already has a %q key, which is where the page index goes", pagesKey)
	}
	eopts := o.Engine
	eopts.FS = o.FS
	if eopts.States == nil {
		// One store for the life of the Site, so a rebuild keeps state.
		eopts.States = s.states
	}
	if eopts.ActionRequest == nil {
		eopts.ActionRequest = o.Request
	}
	// The site's tickets go with the site's own state store: a host that
	// gave its own store lets the engine derive tickets from it.
	if eopts.IslandTickets == nil && o.Engine.States == nil {
		eopts.IslandTickets = s.tickets
	}
	if isDir(o.FS, o.MessagesDir) && eopts.Messages == nil {
		msgs, err := brick.LoadMessages(o.FS, o.MessagesDir)
		if err != nil {
			return nil, err
		}
		eopts.Messages = msgs
	}
	if o.Dev {
		eopts.Mode = brick.Dev
	}
	if eopts.DefaultLocale == "" && len(o.Locales) > 0 {
		eopts.DefaultLocale = o.Locales[0]
	}
	pages, dynamic, diags, err := index(o)
	if err != nil {
		return nil, err
	}
	routes, order, patterns, err := route(pages, dynamic, o.Locales)
	if err != nil {
		return nil, err
	}
	var smBase string
	skip := map[string]bool{}
	if o.Sitemap != nil {
		if smBase, err = sitemapBase(o.Sitemap, siteVal, o.BasePath); err != nil {
			return nil, err
		}
		for _, dp := range dynamic {
			if dp.page.Draft || dp.page.noSitemap {
				skip[dp.page.Path] = true
			}
		}
	}
	siteVal[pagesKey] = listed(pages)
	eopts.Site = siteVal
	eng, err := brick.New(eopts)
	if err != nil {
		return nil, err
	}
	_ = ctx
	st := &state{eng: eng, pages: pages, routes: routes, order: order, patterns: patterns, diags: diags, trans: translations(routes, o.Locales), locales: o.Locales, sitemapBase: smBase, skipSitemap: skip}
	if o.Sitemap != nil {
		if own := s.ownSitemap(st); own != "" {
			// Precedence, not prohibition: the project's file wins.
			o.Logger.Info("site: "+own+" shadows the built-in sitemap", "file", own)
		}
	}
	return st, nil
}

// current returns the live build.
func (s *Site) current() *state {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state
}

// Engine returns the current engine, for rendering outside the site's
// routes (a host's own handlers, tests).
func (s *Site) Engine() *brick.Engine { return s.current().eng }

// Pages returns the page index: what templates read as .Site.Pages.
func (s *Site) Pages() []Page { return listed(s.current().pages) }

// Routes returns every URL and the page behind it, sorted by URL. Every
// entry is a URL a browser can fetch; dynamic pages are listed by Patterns.
func (s *Site) Routes() []Route {
	st := s.current()
	out := make([]Route, 0, len(st.order))
	for _, u := range st.order {
		out = append(out, cloneRoute(st.routes[u]))
	}
	return out
}

// Patterns returns the dynamic pages ([slug].html) in match order, one per
// locale: URL is the pattern (/posts/[slug]/) and Params names its
// parameters. A static page on a URL a pattern also matches
// wins.
func (s *Site) Patterns() []Route {
	st := s.current()
	out := make([]Route, 0, len(st.patterns))
	for _, p := range st.patterns {
		out = append(out, cloneRoute(p.route))
	}
	return out
}

// Diagnostics are the index problems: collection violations and skipped
// files. Routing collisions are errors from New, not diagnostics.
func (s *Site) Diagnostics() []brick.Diagnostic {
	return append([]brick.Diagnostic(nil), s.current().diags...)
}

// Apply updates the site after files changed (from watch.Watch). The index
// and engine are always rebuilt (cheap: files compile lazily). The cache is
// cleared when a file was created or deleted, when config, data or messages
// changed, or when the page index changed; otherwise only the cached pages
// that read an edited file are evicted.
// It returns the URLs whose cache entries were dropped ("*" for all).
func (s *Site) Apply(ctx context.Context, changes []watch.Change) ([]string, error) {
	if len(changes) == 0 {
		return nil, nil
	}
	full := false
	for _, c := range changes {
		if c.Op != watch.Modified || s.structural(c.Path) {
			full = true
		}
	}
	st, err := s.build(ctx)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	prev := s.state
	// Cached pages rendered by the previous engine stay in the cache when
	// their files did not change, and link that engine's asset files.
	st.eng.InheritAssets(prev.eng)
	s.state = st
	s.mu.Unlock()
	// A page edit that changed the index (a title, a date, a URL) can change
	// every page that lists pages, so it counts as structural too.
	if !reflect.DeepEqual(prev.pages, st.pages) || !reflect.DeepEqual(prev.order, st.order) || !samePatterns(prev.patterns, st.patterns) {
		full = true
	}
	var dropped []string
	static := false
	for _, c := range changes {
		static = static || under(c.Path, s.opts.StaticDir)
	}
	if full {
		s.clear()
		dropped = []string{"*"}
	} else {
		paths := make([]string, len(changes))
		for i, c := range changes {
			paths[i] = c.Path
		}
		dropped = s.evictFiles(paths)
		// Static files are not cached, but a page may show them (a
		// stylesheet, an image), so a browser reloads anyway.
		if static {
			dropped = append(dropped, "*")
		}
	}
	s.live.publish(dropped)
	s.opts.Logger.Info("site: applied changes", "files", len(changes), "full", full, "dropped", len(dropped))
	return dropped, nil
}

// samePatterns reports whether two builds route dynamic pages alike; a
// change (a new [slug] file, a different paths:) can change any URL.
func samePatterns(a, b []*pattern) bool {
	return slices.EqualFunc(a, b, func(x, y *pattern) bool { return reflect.DeepEqual(*x, *y) })
}

// structural reports whether a file feeds the index or .Site, so editing it
// can change every page.
func (s *Site) structural(p string) bool {
	o := s.opts
	return p == o.ConfigFile || under(p, o.DataDir) || under(p, o.MessagesDir)
}

// Watch polls the project and applies changes until ctx ends.
func (s *Site) Watch(ctx context.Context, opts watch.Options) error {
	if opts.Skip == nil {
		opts.Skip = func(p string, d fs.DirEntry) bool {
			n := d.Name()
			return len(n) > 1 && n[0] == '.' // .git, .DS_Store
		}
	}
	changes, errs, err := s.watchAll(ctx, opts)
	if err != nil {
		return err
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case batch, ok := <-changes:
			if !ok {
				return ctx.Err()
			}
			if s.opts.OnChange != nil {
				if err := s.opts.OnChange(ctx, batch); err != nil {
					s.opts.Logger.Error("site: OnChange", "err", err)
				}
			}
			if _, err := s.Apply(ctx, batch); err != nil {
				s.opts.Logger.Error("site: rebuild failed", "err", err)
				s.live.publishError(err)
			}
		case err := <-errs:
			if err != nil {
				s.opts.Logger.Error("site: watch", "err", err)
			}
		}
	}
}

// watchAll watches the project and every embedded library whose files can
// change (brick.Library.FS — an os.DirFS a dev server passes so edits to a
// library it is developing reload like the project's own), as one stream.
// A library's changes are reported at the path the engine reads it from,
// brick_libs/<name>/…, so eviction by Result.Files works unchanged. An
// embed.FS is skipped: it is fixed at compile time, so polling it would
// only burn cycles. A Dir library lives inside the project and is already
// watched with it.
func (s *Site) watchAll(ctx context.Context, opts watch.Options) (<-chan []watch.Change, <-chan error, error) {
	type source struct {
		fsys   fs.FS
		prefix string
	}
	srcs := []source{{fsys: s.opts.FS}}
	for _, l := range s.opts.Engine.Libraries {
		if l.FS == nil {
			continue
		}
		if _, fixed := l.FS.(embed.FS); fixed {
			continue
		}
		srcs = append(srcs, source{fsys: l.FS, prefix: path.Join(brick.LibraryMountDir, l.Name) + "/"})
	}
	// The watchers share one cancelable context, so a source that fails to
	// start also stops the ones already started: the caller gets an error
	// and no channels, so nothing else would ever end them before ctx does.
	ctx, cancel := context.WithCancel(ctx)
	changes := make(chan []watch.Change)
	errs := make(chan error, len(srcs))
	var wg sync.WaitGroup
	for _, src := range srcs {
		ch, errc, err := watch.Watch(ctx, src.fsys, ".", opts)
		if err != nil {
			cancel()
			return nil, nil, err
		}
		wg.Add(2)
		go func() {
			defer wg.Done()
			for batch := range ch {
				for i := range batch {
					batch[i].Path = src.prefix + batch[i].Path
				}
				select {
				case changes <- batch:
				case <-ctx.Done():
					return
				}
			}
		}()
		go func() {
			defer wg.Done()
			for err := range errc {
				select {
				case errs <- err:
				default: // one pending error is enough to report; the next poll retries
				}
			}
		}()
	}
	go func() {
		wg.Wait()
		cancel()
		close(changes)
		close(errs)
	}()
	return changes, errs, nil
}

// watchDefaults is the dev server's watcher configuration.
func watchDefaults() watch.Options { return watch.Options{} }
