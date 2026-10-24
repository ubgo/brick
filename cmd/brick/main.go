// Command brick checks, lists, builds and serves a brick site.
//
//	brick check  [-json] [-drafts] [-locales en,fr] [dir]
//	brick routes [-json] [-drafts] [-locales en,fr] [dir]
//	brick build  [-json] [-drafts] [-o out] [-base /path] [-keep /a/,/b/] [-locales en,fr] [dir]
//	brick serve  [-addr :8080] [-base /path] [-keep /a/,/b/] [-locales en,fr] [dir]
//	brick vendor [-check] [-json] [dir]
//	brick docs   [-json] [dir]
//	brick keygen
//	brick new    site <dir>
//	brick new    component <Name> [-state] [-client] [-island] [-lazy] [-dir site]
//	brick new    page <path> [-dir site]
//	brick fmt    [-l] [-w] [-d] [-v] [-line-length N] [-attr-order a,b] [-inline a,em] [path ...]
//	brick schema [-o file]
//	brick lsp
//	brick version [-json]
//
// Each command has its own flags: a flag another command takes is a usage
// error, never silently ignored, and "brick <command> -h" lists only the
// command's own.
//
// It reads brick.yaml, site.yaml, data/ and i18n/ when present, and turns
// on the goldmark, bluemonday and Starlark adapters. Scripts get no db or
// http capability: granting one is a decision for a host's Go code.
//
// Stateful components' state is sealed into the page with the keys in
// BRICK_SECRET; brick keygen prints a new one.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/ubgo/brick"
	imagewebp "github.com/ubgo/brick/contrib/image-webp"
	markdowngoldmark "github.com/ubgo/brick/contrib/markdown-goldmark"
	sanitizebluemonday "github.com/ubgo/brick/contrib/sanitize-bluemonday"
	scriptstarlark "github.com/ubgo/brick/contrib/script-starlark"
	"github.com/ubgo/brick/kit"
	"github.com/ubgo/brick/remote"
	"github.com/ubgo/brick/site"
	"github.com/ubgo/brick/tailwind"
	"github.com/ubgo/brick/watch"
)

// Exit codes: success, a failure the command reports, a usage mistake.
const (
	exitOK    = 0
	exitFail  = 1
	exitUsage = 2
)

// Defaults for flags.
const (
	defaultOut  = "public"
	defaultAddr = ":8080"
	configFile  = "brick.yaml"
	// secretEnv holds the state keys: comma-separated base64, newest first
	// (the first seals, every one opens), read by brick.KeysFromEnv.
	secretEnv = "BRICK_SECRET"
)

const usage = `brick — check, build and serve a brick site

Usage:
  brick check   [-json] [-drafts] [-locales en,fr] [dir]
                report every problem; exit 1 on errors
  brick routes  [-json] [-drafts] [-locales en,fr] [dir]
                list every URL and the file behind it
  brick build   [-json] [-drafts] [-o dir] [-base /path] [-keep /a/,/b/] [-locales en,fr] [dir]
                write the static site (default: public/)
  brick serve   [-addr :8080] [-base /path] [-keep /a/,/b/] [-locales en,fr] [dir]
                dev server with drafts and live reload
  brick vendor  [-check] [-json] [dir]
                fetch remote use: imports and libraries into brick_modules/ and pin brick.lock
  brick docs    [-json] [dir]
                list every component with its props and slots
  brick new     site <dir> | component <Name> | page <path>
                write a starter project or file (brick new -h for -state, -client, -island, -lazy, -dir)
  brick fmt     [-l] [-w] [-d] [-v] [-line-length N] [-attr-order a,b] [-inline a,em] [path ...]
                format .html files without changing what they render
  brick keygen  print a new state key for BRICK_SECRET
  brick schema  [-o file]
                print the JSON Schema of frontmatter, for editors
  brick lsp     language server for editors, on stdin/stdout (editors/vscode)
  brick version [-json]
                the module version and VCS revision this binary was built from

Flags:
  -json           print JSON instead of text (check, routes, build, vendor, docs, version)
  -drafts         include draft pages (check, routes, build; serve always does)
  -locales en,fr  locales with their own URLs, default first (check, routes, build, serve)
  -base /path     host the site under this path; links and assets are rewritten (build, serve)
  -keep /a/,/b/   with -base, root-absolute prefixes left unrewritten, e.g. links to a sibling site (build, serve)
  -o dir          build: the output directory (default public)
  -addr :8080     serve: the listen address
  -check          vendor: verify brick_modules/ against brick.lock without fetching

Examples:
  brick new site blog && brick serve blog   a working project using the kit, at http://localhost:8080
  brick check                  check the site in the current directory
  brick check -json site | jq  machine-readable diagnostics
  brick build -o dist          static export into dist/
  brick build -json | jq .pages   how many pages an export wrote
  brick build -base /docs      an export for a site hosted under /docs/ (a GitHub Pages project)
  brick serve -addr :3000      http://localhost:3000, /_brick/play/Card?title=Hi
  brick vendor -check          CI: fail unless brick_modules/ matches brick.lock (no network)
  brick keygen | gopass insert -f app/brick-secret   a key, straight into a secret manager
  brick docs -json | jq '.[] | select(.name == "Card")'   one component's contract

State: stateful components seal their state into the page with the keys in
BRICK_SECRET (comma-separated base64, newest first), so it survives a
restart and any server with the same keys answers an action. serve without
it uses a key made for the process (a restart invalidates state, which
recovers in place); build needs it when a page has a stateful component.

The kit is built in: brick.yaml's "libraries: [brick-kit]" makes every kit
component (<Button>, <Dialog>, …) available on every page, with no copy and
no vendoring.

dir defaults to the current directory. Run "brick <command> -h" for flags.
`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(run(ctx, os.Args[1:], os.Stdout, os.Stderr))
}

// run is the whole CLI, returning the exit code, so tests drive it directly.
func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		fmt.Fprint(stdout, usage)
		if len(args) == 0 {
			return exitUsage
		}
		return exitOK
	}
	cmd, rest := args[0], args[1:]
	if cmd == "keygen" {
		return keygen(rest, stdout, stderr)
	}
	if cmd == "new" {
		return newCmd(rest, stdout, stderr)
	}
	if cmd == "fmt" {
		return fmtCmd(rest, os.Stdin, stdout, stderr)
	}
	if cmd == "schema" {
		return schemaCmd(rest, stdout, stderr)
	}
	if cmd == "lsp" {
		return lspCmd(ctx, rest, os.Stdin, stdout, stderr)
	}
	if cmd == "version" {
		return versionCmd(rest, stdout, stderr)
	}
	var a siteArgs
	fl, ok := siteFlagSet(cmd, &a, stderr)
	if !ok {
		fmt.Fprintf(stderr, "brick: unknown command %q\n\n%s", cmd, usage)
		return exitUsage
	}
	if err := fl.Parse(rest); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK
		}
		return exitUsage
	}
	dir := "."
	if fl.NArg() > 1 {
		fmt.Fprintln(stderr, "brick: one directory at most")
		return exitUsage
	}
	if fl.NArg() == 1 {
		dir = fl.Arg(0)
	}
	if cmd == "vendor" {
		return vendor(ctx, dir, a.checkOnly, a.asJSON, stdout, stderr)
	}
	keys, err := stateKeys(cmd, os.LookupEnv, stderr)
	if err != nil {
		fmt.Fprintln(stderr, "brick:", err)
		return exitFail
	}
	s, tw, err := open(ctx, dir, siteFlags{drafts: a.drafts || cmd == "serve", dev: cmd == "serve", locales: a.locales, base: a.base, keep: splitList(a.keep), log: stderr, keys: keys})
	if err != nil {
		fmt.Fprintln(stderr, "brick:", err)
		return exitFail
	}
	if tw != nil && (cmd == "build" || cmd == "serve") {
		if ran, err := tailwind.Build(ctx, *tw); err != nil {
			fmt.Fprintln(stderr, "brick:", err)
			return exitFail
		} else if ran {
			// With -json, stdout is the report alone, so a tool can parse it.
			note := stdout
			if a.asJSON {
				note = stderr
			}
			fmt.Fprintln(note, "tailwind: rebuilt", tw.Output)
		}
	}
	switch cmd {
	case "check":
		return check(ctx, s, a.asJSON, stdout)
	case "routes":
		return routes(s, a.asJSON, stdout)
	case "docs":
		return docs(s, a.asJSON, stdout, stderr)
	case "build":
		rep, err := s.Export(ctx, a.out)
		if err != nil {
			fmt.Fprintln(stderr, "brick:", err)
			if errors.Is(err, site.ErrExportNeedsKeys) {
				fmt.Fprintf(stderr, "brick: set %s to the key the action server uses (brick keygen prints a new one)\n", secretEnv)
			}
			return exitFail
		}
		if a.asJSON {
			return writeJSON(stdout, stderr, buildReport{Pages: rep.Pages, Static: rep.Static, Assets: rep.Assets, Sitemap: rep.Sitemap, Out: a.out, Skipped: rep.Skipped})
		}
		fmt.Fprintf(stdout, "wrote %d pages, %d static files and %d assets to %s\n", rep.Pages, rep.Static, rep.Assets, a.out)
		if rep.Sitemap {
			fmt.Fprintf(stdout, "wrote %s/sitemap.xml\n", a.out)
		}
		for _, u := range rep.Skipped {
			fmt.Fprintf(stdout, "skipped %s: brick: {per_request: true} pages are served, not built\n", u)
		}
		return exitOK
	default: // serve
		host := a.addr
		if strings.HasPrefix(host, ":") {
			host = "localhost" + host
		}
		fmt.Fprintf(stdout, "serving %s on http://%s (Ctrl-C to stop)\n", dir, host)
		if err := s.ListenAndServe(ctx, a.addr); err != nil && !errors.Is(err, context.Canceled) {
			fmt.Fprintln(stderr, "brick:", err)
			return exitFail
		}
		return exitOK
	}
}

// siteFlags are the command-line choices that shape the site.
type siteFlags struct {
	drafts  bool
	dev     bool
	locales string
	// base is site.Options.BasePath: the path the site is hosted under.
	base string
	// keep is site.Options.KeepPaths: prefixes the base rewrite skips.
	keep []string
	log  io.Writer
	// keys seal stateful components' state (site.Options.StateKeys); nil
	// keeps it in the process.
	keys brick.KeyRing
}

// open builds the site in dir with the standard adapters, and returns the
// Tailwind settings when brick.yaml has them.
func open(ctx context.Context, dir string, f siteFlags) (*site.Site, *tailwind.Options, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, nil, err
	}
	fsys := root.FS()
	// WebP is registered always: it costs nothing until a page names
	// "webp" in {{ image }}, and pages without it render byte-identical.
	opts := brick.Options{Markdown: markdowngoldmark.New(), Sanitizer: sanitizebluemonday.New(), ImageEncoders: []brick.ImageEncoder{imagewebp.New()}}
	var tw *tailwind.Options
	var sitemap *site.Sitemap
	var hostOnly []string
	if _, err := os.Stat(dir + "/" + configFile); err == nil {
		cfg, err := brick.LoadConfig(fsys, configFile)
		if err != nil {
			return nil, nil, err
		}
		// This program is not the site's host: stand in for what the host
		// declares it supplies, so the site compiles and the rest is checked.
		if h := cfg.Host; h != nil {
			hostOnly = h.Globals
			if h.Uploads {
				opts.Uploads = hostOnlyUploads{}
			}
		}
		// The CLI offers the kit, so `libraries: [brick-kit]` needs no copy.
		if err := cfg.Apply(&opts, kit.Library()); err != nil {
			return nil, nil, err
		}
		if t := cfg.Tailwind; t != nil {
			tw = &tailwind.Options{Dir: dir, Input: t.Input, Output: t.Output, Binary: t.Binary}
		}
		if sm := cfg.Sitemap; sm != nil {
			sitemap = &site.Sitemap{BaseURL: sm.BaseURL}
		}
		if m := cfg.Markdown; m != nil {
			opts.Markdown = markdowngoldmark.New(markdownOptions(*m)...)
		}
	}
	sc, err := scriptstarlark.New(scriptstarlark.Options{HostOnly: hostOnly})
	if err != nil {
		return nil, nil, err
	}
	opts.Scripting = sc
	// Server push needs nothing from a host for one process: an
	// in-memory broadcaster, and listen tokens sealed with a key that lives
	// as long as the process. So brick serve runs brick: {listen:} and
	// brick.publish for real, and brick check compiles them.
	push, err := brick.NewPush(brick.PushOptions{})
	if err != nil {
		return nil, nil, err
	}
	opts.Push = push
	locs := splitList(f.locales)
	logger := slog.New(slog.NewTextHandler(f.log, &slog.HandlerOptions{Level: slog.LevelWarn}))
	opts.Logger = logger
	so := site.Options{FS: fsys, Engine: opts, Drafts: f.drafts, Dev: f.dev, Locales: locs, Logger: logger, BasePath: f.base, KeepPaths: f.keep, StateKeys: f.keys, Sitemap: sitemap}
	if tw != nil {
		// Rebuild the stylesheet when markup changes; the watcher then sees
		// the new CSS in static/ and reloads the browser.
		so.OnChange = func(ctx context.Context, changes []watch.Change) error {
			for _, c := range changes {
				if strings.HasSuffix(c.Path, ".html") || strings.HasSuffix(c.Path, ".md") || c.Path == tw.Input {
					_, err := tailwind.Build(ctx, *tw)
					return err
				}
			}
			return nil
		}
	}
	s, err := site.New(ctx, so)
	return s, tw, err
}

// markdownOptions turns brick.yaml's markdown: switches into converter
// options; none set gives the default converter.
func markdownOptions(m brick.MarkdownConfig) []markdowngoldmark.Option {
	var out []markdowngoldmark.Option
	if m.HeadingIDs {
		out = append(out, markdowngoldmark.WithHeadingIDs())
	}
	if m.Highlight {
		out = append(out, markdowngoldmark.WithHighlight())
	}
	return out
}

// hostOnlyUploads stands in for the UploadSink a site's host configures
// (brick.yaml host: {uploads: true}): components with uploads compile, and
// an upload sent to this program fails with brick.ErrHostOnly instead of
// being kept somewhere the host never chose.
type hostOnlyUploads struct{}

func (hostOnlyUploads) Open(context.Context, brick.UploadMeta) (brick.UploadWriter, error) {
	return nil, fmt.Errorf("uploads (declared in brick.yaml host: uploads): %w", brick.ErrHostOnly)
}

// check prints diagnostics and fails when any is an error.
func check(ctx context.Context, s *site.Site, asJSON bool, w io.Writer) int {
	diags, err := s.Check(ctx)
	if err != nil {
		fmt.Fprintln(w, "brick:", err)
		return exitFail
	}
	failed := false
	for _, d := range diags {
		failed = failed || d.Severity == brick.SeverityError
	}
	if asJSON {
		type jd struct {
			File     string `json:"file"`
			Line     int    `json:"line,omitempty"`
			Col      int    `json:"col,omitempty"`
			Severity string `json:"severity"`
			Code     string `json:"code"`
			Message  string `json:"message"`
		}
		list := make([]jd, 0, len(diags))
		for _, d := range diags {
			list = append(list, jd{d.File, d.Line, d.Col, d.Severity.String(), string(d.Code), d.Message})
		}
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		_ = enc.Encode(list)
	} else {
		for _, d := range diags {
			fmt.Fprintln(w, d)
		}
		if !failed {
			fmt.Fprintf(w, "ok: %d pages\n", len(s.Routes()))
		}
	}
	if failed {
		return exitFail
	}
	return exitOK
}

// routes prints every URL and its file, then the dynamic pages with their
// parameters.
func routes(s *site.Site, asJSON bool, w io.Writer) int {
	rs := append(s.Routes(), s.Patterns()...)
	if asJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		_ = enc.Encode(rs)
		return exitOK
	}
	width := 0
	for _, r := range rs {
		width = max(width, len(r.URL))
	}
	for _, r := range rs {
		line := fmt.Sprintf("%-*s  %s", width, r.URL, r.Path)
		if len(r.Params) > 0 {
			line += "  (" + strings.Join(r.Params, ", ") + ")"
		}
		fmt.Fprintln(w, line)
	}
	return exitOK
}

// vendorReport is brick vendor -json: what was vendored, or with -check
// that the folder matches the lock (a mismatch is exit 1 and stderr, as
// in text mode, so Files is never a partial list).
type vendorReport struct {
	// Files are every vendored file, by spec, sorted; empty with -check.
	Files []string `json:"files"`
	// Fetched counts the files downloaded this run.
	Fetched int `json:"fetched"`
	// Checked is true for -check, which fetches nothing.
	Checked bool `json:"checked"`
}

// vendor fetches remote imports and remote libraries (or, with -check,
// verifies them offline).
func vendor(ctx context.Context, dir string, checkOnly, asJSON bool, stdout, stderr io.Writer) int {
	if checkOnly {
		if err := remote.Check(remote.Options{Dir: dir}); err != nil {
			fmt.Fprintln(stderr, "brick:", err)
			return exitFail
		}
		if asJSON {
			return writeJSON(stdout, stderr, vendorReport{Files: []string{}, Checked: true})
		}
		fmt.Fprintln(stdout, "ok: brick_modules/ matches brick.lock")
		return exitOK
	}
	rep, err := remote.Vendor(ctx, remote.Options{Dir: dir})
	if err != nil {
		fmt.Fprintln(stderr, "brick:", err)
		return exitFail
	}
	if asJSON {
		files := rep.Files
		if files == nil {
			files = []string{}
		}
		return writeJSON(stdout, stderr, vendorReport{Files: files, Fetched: rep.Fetched})
	}
	fmt.Fprintf(stdout, "vendored %d files (%d fetched) into brick_modules/; brick.lock written\n", len(rep.Files), rep.Fetched)
	return exitOK
}

// buildReport is brick build -json: ExportReport's counts and where the
// files went.
type buildReport struct {
	Pages  int `json:"pages"`
	Static int `json:"static"`
	Assets int `json:"assets"`
	// Sitemap says the built-in sitemap.xml was written (brick.yaml
	// sitemap:).
	Sitemap bool   `json:"sitemap"`
	Out     string `json:"out"`
	// Skipped lists the per-request pages left out (ExportReport.Skipped):
	// a static file has no visitor, so a build must say what it did not
	// write rather than leave a hole the reader finds in production.
	Skipped []string `json:"skipped,omitempty"`
}

// writeJSON prints v indented, the shape every -json output shares.
func writeJSON(stdout, stderr io.Writer, v any) int {
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		fmt.Fprintln(stderr, "brick:", err)
		return exitFail
	}
	return exitOK
}

// stateKeys is the key ring for stateful components:
// BRICK_SECRET when set; for serve without it, a key made for this process
// (never written anywhere) and a warning; otherwise none, so build refuses
// to export a stateful page with handles no server will ever open.
func stateKeys(cmd string, lookup func(string) (string, bool), stderr io.Writer) (brick.KeyRing, error) {
	if v, ok := lookup(secretEnv); ok && strings.TrimSpace(v) != "" {
		return brick.KeysFromEnv(lookup, secretEnv)
	}
	if cmd != "serve" {
		return nil, nil
	}
	k, err := brick.NewStateKey()
	if err != nil {
		return nil, err
	}
	fmt.Fprintf(stderr, "brick: warning: %s is not set, so state is sealed with a key made for this process: a restart invalidates it (components recover in place, keeping what was typed). Set %s (brick keygen prints one) to keep state across restarts.\n", secretEnv, secretEnv)
	return brick.StaticKeys(k)
}

// keygen prints a new state key on stdout, alone, so it pipes straight into
// a secret manager; how to use it goes to stderr. It never writes a file:
// a key on disk is a copy nobody tracks.
func keygen(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 {
		if args[0] == "-h" || args[0] == "--help" {
			fmt.Fprintf(stdout, "brick keygen prints a new %d-byte state key, base64, for %s.\n", brick.StateKeySize, secretEnv)
			return exitOK
		}
		fmt.Fprintln(stderr, "brick keygen: takes no arguments")
		return exitUsage
	}
	k, err := brick.NewStateKey()
	if err != nil {
		fmt.Fprintln(stderr, "brick:", err)
		return exitFail
	}
	fmt.Fprintln(stdout, brick.EncodeStateKey(k))
	fmt.Fprintf(stderr, `Store it in your secret manager, never in code or brick.yaml, and pass it as %[1]s:
  brick keygen | gopass insert -f <app>/brick-secret
  %[1]s=$(gopass show -o <app>/brick-secret) brick serve
Rotate: put the new key first and keep the old one second for one TTL: %[1]s=new,old
`, secretEnv)
	return exitOK
}
