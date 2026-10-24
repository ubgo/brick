// Command ui serves, checks and exports the kit's showcase: a page for every
// component in ../../kit and demo apps (a CRUD contacts manager, a task
// board, a dashboard, an inbox, settings, sign-in and a landing page) built
// only from kit components, Starlark actions and a SQLite database.
//
//	go run ./ui -serve :8002     serve with live reload
//	go run ./ui -out build       write the static site (needs BRICK_SECRET; apps act through a server with the same key)
//	go run ./ui -check           report every problem in the site
//	go run ./ui -serve :8000 -engine site
//	                             one server for both examples: the showcase
//	                             at /ui/, the engine demo (examples/site) at
//	                             /engine/, an index at / (site.Mount)
//
// The kit is the library github.com/ubgo/brick/kit, offered to brick.yaml's
// `libraries: [brick-kit]`, so the site uses the repository's kit directly:
// no copy to keep in sync. -serve reads it from disk so kit edits reload;
// everything else uses the embedded copy.
//
// Stateful components seal their state into the page with the keys in
// BRICK_SECRET (task ui:serve reads it from gopass), so an open demo app
// keeps working across a restart of the server. Without it, -serve seals
// with a key made for the process (a restart invalidates state, which
// recovers in place) and tests keep state in memory.
package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"go.starlark.net/starlark"

	"github.com/ubgo/brick"
	markdowngoldmark "github.com/ubgo/brick/contrib/markdown-goldmark"
	sanitizebluemonday "github.com/ubgo/brick/contrib/sanitize-bluemonday"
	scriptstarlark "github.com/ubgo/brick/contrib/script-starlark"
	"github.com/ubgo/brick/examples/demosite"
	"github.com/ubgo/brick/examples/ui/demodb"
	"github.com/ubgo/brick/kit"
	"github.com/ubgo/brick/site"
	"github.com/ubgo/brick/upload/disk"
)

// configFile is the project's engine configuration (roots, navigation).
const configFile = "brick.yaml"

func main() {
	siteDir := flag.String("site", "ui", "the project directory")
	out := flag.String("out", "", "write the static site into this directory")
	serve := flag.String("serve", "", "serve the site on this address, e.g. :8002")
	check := flag.Bool("check", false, "report every problem in the site")
	engine := flag.String("engine", "", "with -serve: also mount the engine demo in this directory; the showcase moves to "+showcaseBase+"/")
	flag.Parse()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	var err error
	if *engine != "" {
		err = serveBoth(ctx, *siteDir, *engine, *serve)
	} else {
		err = run(ctx, *siteDir, *out, *serve, *check)
	}
	if err != nil {
		log.Fatal(err)
	}
}

// The paths the two examples are mounted at when one server serves both.
const (
	showcaseBase = "/ui"
	engineBase   = "/engine"
)

// bothIndex is the page at / when both examples share a server.
const bothIndex = `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>brick examples</title>
<style>body{font:15px/1.5 system-ui,sans-serif;max-width:40rem;margin:4rem auto;padding:0 1rem;color:#18181b}a{display:block;padding:1rem 1.25rem;margin:.75rem 0;border:1px solid #e4e4e7;border-radius:.6rem;color:inherit;text-decoration:none}a:hover{border-color:#4f46e5}b{display:block;font-size:1.05rem}span{color:#71717a}</style></head>
<body><h1>brick examples</h1><p>Two brick sites, each from its own directory, served by one server under their own paths (site.Mount).</p>
<a href="` + showcaseBase + `/"><b>UI kit showcase →</b><span>every kit component with live demos and code, and twelve demo apps (examples/ui)</span></a>
<a href="` + engineBase + `/"><b>Engine feature demo →</b><span>each engine feature on its own page: islands, actions, i18n, markdown, remote imports… (examples/site)</span></a>
</body></html>`

// serveBoth serves the showcase and the engine demo from one server, each
// under its own base path — the proof that BasePath needs no edits to a
// site: neither site knows it is mounted.
func serveBoth(ctx context.Context, uiDir, engineDir, addr string) error {
	if addr == "" {
		return errors.New("-engine needs -serve ADDR")
	}
	ui, db, err := newSiteAt(ctx, uiDir, true, showcaseBase, []string{engineBase})
	if err != nil {
		return err
	}
	defer db.Close()
	keys, err := demosite.StateKeys(os.LookupEnv, true, log.Printf)
	if err != nil {
		return err
	}
	eng, err := demosite.New(ctx, demosite.Options{Dir: engineDir, Dev: true, BasePath: engineBase, KeepPaths: []string{showcaseBase}, StateKeys: keys})
	if err != nil {
		return err
	}
	mounted, err := site.Mount(ui, eng)
	if err != nil {
		return err
	}
	h, err := withUploads(withDemoSignIn(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = io.WriteString(w, bothIndex)
			return
		}
		mounted.ServeHTTP(w, r)
	}), showcaseBase), showcaseBase)
	if err != nil {
		return err
	}
	log.Printf("serving %s/ and %s/ on %s", showcaseBase, engineBase, addr)
	return site.ListenAndServeHandler(ctx, addr, h, ui, eng)
}

// today is the clock the demo grants scripts, as an ISO date: new records
// are stamped with it. Seeded records carry fixed dates so pages that show
// them render the same bytes on every run.
var today = starlark.NewBuiltin("today", func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
	return starlark.String(time.Now().UTC().Format(time.DateOnly)), nil
})

// maxPause bounds the pause capability: a demo slows a panel or a stream
// down enough to watch, never long enough to tie up a server.
const maxPause = 2 * time.Second

// pause is the demo's stand-in for slow work (a slow query, a model
// producing words): pause(ms) sleeps up to maxPause. The lazy panel and the
// streamed reply on the Live interactivity page use it.
var pause = starlark.NewBuiltin("pause", func(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var ms int
	if err := starlark.UnpackPositionalArgs(b.Name(), args, kwargs, 1, &ms); err != nil {
		return nil, err
	}
	time.Sleep(min(time.Duration(max(ms, 0))*time.Millisecond, maxPause))
	return starlark.None, nil
})

// sanitizeHTML is the sanitize_html script capability: the site's HTML
// policy, so an action can clean user HTML (a rich-text editor's value)
// before storing it. Pages sanitize again on render; storing clean HTML
// means a record read elsewhere — an export, an API — is safe too.
func sanitizeHTML(policy func(string) string) *starlark.Builtin {
	return starlark.NewBuiltin("sanitize_html", func(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var html string
		if err := starlark.UnpackPositionalArgs(b.Name(), args, kwargs, 1, &html); err != nil {
			return nil, err
		}
		return starlark.String(policy(html)), nil
	})
}

// newSite builds the showcase over a freshly seeded database, exactly as the
// tests do.
func newSite(ctx context.Context, siteDir string, dev bool) (*site.Site, *sql.DB, error) {
	return newSiteAt(ctx, siteDir, dev, "", nil)
}

// jobsCloseWait bounds how long shutdown waits for the demo's background
// jobs; the report job takes about a second and a half.
const jobsCloseWait = 5 * time.Second

// newSiteAt builds the showcase under a base path (site.Options.BasePath).
func newSiteAt(ctx context.Context, siteDir string, dev bool, base string, keep []string) (*site.Site, *sql.DB, error) {
	root, err := os.OpenRoot(siteDir)
	if err != nil {
		return nil, nil, err
	}
	db, err := demodb.Open(ctx)
	if err != nil {
		return nil, nil, err
	}
	// One HTML policy for both doors: the template's sanitize built-in
	// (Options.Sanitizer) and scripts' sanitize_html, so what an action
	// stores and what a page renders are cleaned the same way.
	policy := sanitizebluemonday.New()
	sc, err := scriptstarlark.New(scriptstarlark.Options{
		Globals: starlark.StringDict{"today": today, "sanitize_html": sanitizeHTML(policy), "pause": pause},
		DB:      db,
		DBExec:  true,
	})
	if err != nil {
		return nil, nil, errors.Join(err, db.Close())
	}
	uploads, err := demoUploads()
	if err != nil {
		return nil, nil, errors.Join(err, db.Close())
	}
	// Server push for the live board demo: one process, so the
	// in-memory broadcaster and a per-process token key are enough.
	push, err := brick.NewPush(brick.PushOptions{})
	if err != nil {
		return nil, nil, errors.Join(err, db.Close())
	}
	// Background jobs for the report demo: the in-process
	// pool, publishing through the same push as the live board.
	jobFuncs, jobFailed, err := demodb.Jobs(ctx, db)
	if err != nil {
		return nil, nil, errors.Join(err, db.Close())
	}
	jobs, err := brick.NewJobs(brick.JobsOptions{Funcs: jobFuncs, Push: push, OnError: jobFailed})
	if err != nil {
		return nil, nil, errors.Join(err, db.Close())
	}
	// The pool lives as long as ctx (the server's signal context, or a
	// test's): when it ends, running jobs are cancelled and the workers
	// get jobsCloseWait to finish, so a shutdown leaks no goroutines.
	context.AfterFunc(ctx, func() {
		wait, cancel := context.WithTimeout(context.Background(), jobsCloseWait)
		defer cancel()
		if err := jobs.Close(wait); err != nil {
			log.Printf("closing the jobs pool: %v", err)
		}
	})
	eng := brick.Options{
		Push:      push,
		Jobs:      jobs,
		Scripting: sc,
		Markdown:  markdowngoldmark.New(),
		Sanitizer: policy,
		Uploads:   uploads,
		// The reload demo's action fails as a server fault would.
		ActionMiddleware: []brick.ActionMiddleware{serverFaults(serverFaultDemo)},
	}
	cfg, err := brick.LoadConfig(root.FS(), configFile)
	if err != nil {
		return nil, nil, errors.Join(err, db.Close())
	}
	lib, err := kitLibrary(siteDir, dev)
	if err != nil {
		return nil, nil, errors.Join(err, db.Close())
	}
	if err := cfg.Apply(&eng, lib); err != nil {
		return nil, nil, errors.Join(err, db.Close())
	}
	keys, err := demosite.StateKeys(os.LookupEnv, dev, log.Printf)
	if err != nil {
		return nil, nil, errors.Join(err, db.Close())
	}
	s, err := site.New(ctx, site.Options{FS: root.FS(), Engine: eng, Dev: dev, Request: requestValues, BasePath: base, KeepPaths: keep, StateKeys: keys})
	if err != nil {
		return nil, nil, errors.Join(err, db.Close())
	}
	return s, db, nil
}

// The upload demo's files: the reference disk sink in
// one directory under the system temp dir, served at uploadsPrefix. Uploads
// are throwaway, so they are swept after uploadsTTL; the directory is
// fixed, not per process, so test runs do not pile up directories.
const (
	uploadsDirName = "brick-ui-uploads"
	uploadsPrefix  = "/uploads/"
	uploadsTTL     = time.Hour
)

// demoUploads is the disk sink over the demo's upload directory, swept of
// files older than uploadsTTL. The sink keeps no state besides the
// directory, so the engine's sink and the one serving the files can be two
// values over the same place.
func demoUploads() (*disk.Sink, error) {
	sink, err := disk.New(disk.Options{Dir: filepath.Join(os.TempDir(), uploadsDirName), URLPrefix: uploadsPrefix, TTL: uploadsTTL})
	if err != nil {
		return nil, err
	}
	if _, err := sink.Sweep(); err != nil {
		return nil, err
	}
	return sink, nil
}

// withUploads serves the demo's uploaded files under mount (the site's
// base path, "" at the root) + uploadsPrefix, and everything else through
// next. The refs carry uploadsPrefix; the site layer rebases them under
// the mount like every other root-absolute URL.
func withUploads(next http.Handler, mount string) (http.Handler, error) {
	sink, err := demoUploads()
	if err != nil {
		return nil, err
	}
	prefix := mount + uploadsPrefix
	files := http.StripPrefix(prefix, sink.Handler())
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, prefix) {
			files.ServeHTTP(w, r)
			return
		}
		next.ServeHTTP(w, r)
	}), nil
}

// serverFaultDemo is the component whose failed actions the showcase
// answers as server faults: the brick.reloadComponent demo
// (demos/RuntimeReload.html).
const serverFaultDemo = "RuntimeReload"

// serverFaults answers a failed action of the named component with a 500,
// as a host does when an action fails below the script (a dependency that
// crashed): the engine's default for a script failure is 422, which the
// visitor can correct, and the reload demo needs the fault the visitor
// cannot. The failure itself is kept as the error's cause.
func serverFaults(component string) brick.ActionMiddleware {
	return func(next brick.ActionFunc) brick.ActionFunc {
		return func(ctx context.Context, a *brick.ActionCall) (*brick.ActionResult, error) {
			res, err := next(ctx, a)
			if err == nil || a.Component != component {
				return res, err
			}
			var ae *brick.ActionError
			if errors.As(err, &ae) {
				return res, err
			}
			return nil, &brick.ActionError{Status: http.StatusInternalServerError, Message: "the server failed to run " + a.Action, Err: err}
		}
	}
}

// The classic form post the brick.submit demo hands off to: a plain handler outside brick, standing for a
// login endpoint that sets a session cookie. Nothing is authenticated; the
// cookie only shows why the hand-off must be a top-level post.
const (
	signInPath    = "/demo/sign-in"
	signInBack    = "/runtime-extras/"
	signInCookie  = "brick_demo_user"
	signInNameMax = 60
)

// withDemoSignIn answers POST mount+signInPath: it sets signInCookie to the
// posted name and redirects (303) to the demo's page with ?signed=<name>,
// which the demo shows. Every other request goes to next.
func withDemoSignIn(next http.Handler, mount string) http.Handler {
	path := mount + signInPath
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != path {
			next.ServeHTTP(w, r)
			return
		}
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		name := strings.TrimSpace(r.PostFormValue("name"))
		if name == "" {
			http.Error(w, "name is required", http.StatusBadRequest)
			return
		}
		if runes := []rune(name); len(runes) > signInNameMax {
			name = string(runes[:signInNameMax])
		}
		http.SetCookie(w, &http.Cookie{Name: signInCookie, Value: url.QueryEscape(name), Path: mount + "/", HttpOnly: true, SameSite: http.SameSiteLaxMode})
		http.Redirect(w, r, mount+signInBack+"?"+url.Values{"signed": {name}}.Encode(), http.StatusSeeOther)
	})
}

// repoKitDir is the repository's kit relative to the site folder (ui/).
const repoKitDir = "../../kit"

// kitLibrary is the kit the site offers as brick-kit. Serving (dev) reads
// the repository's kit/ from disk, so an edit to a kit component reloads the
// page like an edit to the site's own files (the site layer watches a
// library's FS). Tests, -check and -out use the embedded kit, the one a
// Go program importing github.com/ubgo/brick/kit gets.
func kitLibrary(siteDir string, dev bool) (brick.Library, error) {
	lib := kit.Library()
	if !dev {
		return lib, nil
	}
	root, err := os.OpenRoot(filepath.Join(siteDir, repoKitDir))
	if err != nil {
		return lib, fmt.Errorf("the live kit: %w", err)
	}
	lib.FS = root.FS()
	return lib, nil
}

// queryKey is the .Ctx key under which islands read the request's query
// string, first value per key: an island with `inject: [query]` shows one
// record by ?slug=…, so a page like /apps/cms/blog/post/?slug=hello has a
// shareable URL while the page around the island stays cached.
const queryKey = "query"

// requestValues is the per-request input islands see (site.Options.Request).
//
// The map is map[string]any, not map[string]string, because it crosses
// into templates and Starlark loaders, which read .Ctx values as generic
// maps; every value is a string (the first value of its key).
func requestValues(r *http.Request) []brick.RenderOption {
	q := map[string]any{}
	for k, v := range r.URL.Query() {
		if len(v) > 0 {
			q[k] = v[0]
		}
	}
	return []brick.RenderOption{brick.Provide(queryKey, q)}
}

func run(ctx context.Context, siteDir, out, serve string, check bool) error {
	s, db, err := newSite(ctx, siteDir, serve != "")
	if err != nil {
		return err
	}
	defer db.Close()
	switch {
	case check:
		diags, err := s.Check(ctx)
		if err != nil {
			return err
		}
		failed := false
		for _, d := range diags {
			fmt.Println(d)
			failed = failed || d.Severity == brick.SeverityError
		}
		if failed {
			return errors.New("the site has errors")
		}
		fmt.Printf("ok: %d pages\n", len(s.Routes()))
		return nil
	case out != "":
		rep, err := s.Export(ctx, out)
		if err != nil {
			return err
		}
		fmt.Printf("wrote %d pages, %d static files and %d assets to %s\n", rep.Pages, rep.Static, rep.Assets, out)
		return nil
	case serve != "":
		log.Printf("serving on %s", serve)
		h, err := withUploads(withDemoSignIn(s.Handler(), ""), "")
		if err != nil {
			return err
		}
		return site.ListenAndServeHandler(ctx, serve, h, s)
	}
	return errors.New("pass -serve ADDR, -out DIR or -check")
}
