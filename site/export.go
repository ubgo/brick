package site

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/ubgo/brick"
)

// exportMarker marks a directory as a brick export, so Export only ever
// clears a directory it made itself.
const exportMarker = ".brick-export"

// indexFile is what a directory-style URL is written as.
const indexFile = "index.html"

// ExportReport says what Export wrote.
type ExportReport struct {
	Pages  int
	Static int
	Assets int
	// Sitemap says the built-in sitemap.xml was written (Options.Sitemap).
	Sitemap bool `json:",omitempty"`
	// Skipped lists the URLs (patterns for a dynamic page) of per-request
	// pages, which Export does not write (skipPerRequest).
	Skipped []string `json:",omitempty"`
}

// Export writes the whole site to dir: every route as
// <url>/index.html, the static directory, component assets under the
// asset base, and 404.html. Output is deterministic, so exporting an
// unchanged site twice gives identical files.
//
// dir must be empty, missing, or a previous export (it holds the
// .brick-export marker); anything else is refused, so Export never deletes
// files it did not create. A render error stops the export.
//
// A stateful component's state is sealed with no expiry and no session
// (brick.WithStaticState): it holds only the initial state anyone gets by
// loading the page, and the first action re-seals it with a TTL. Such a page needs keys the action server shares; without
// them Export returns ErrExportNeedsKeys rather than write dead handles.
func (s *Site) Export(ctx context.Context, dir string) (ExportReport, error) {
	var rep ExportReport
	if err := prepareDir(dir); err != nil {
		return rep, err
	}
	st := s.current()
	for _, u := range st.order {
		if r := st.routes[u]; r.PerRequest {
			s.skipPerRequest(&rep, r)
			continue
		}
		if err := s.exportPage(ctx, st, dir, u); err != nil {
			return rep, err
		}
		rep.Pages++
	}
	// A dynamic page is written once per entry of its paths: list. Without the list export cannot know its pages, so it refuses
	// rather than ship a site with silent holes. A static page on the same
	// URL wins, as it does in serve.
	for _, p := range st.patterns {
		if p.route.PerRequest {
			s.skipPerRequest(&rep, p.route)
			continue
		}
		if p.paths == "" {
			return rep, fmt.Errorf("site: export %s: %w", p.route.Path, ErrExportNeedsPaths)
		}
		urls, err := s.pathEntries(ctx, st, p)
		if err != nil {
			return rep, fmt.Errorf("site: export %s: %w", p.route.Path, err)
		}
		for _, u := range urls {
			if _, static := st.routes[u]; static {
				continue
			}
			if m, ok := st.match(u); !ok || m.pat != p {
				continue // an earlier pattern takes this URL; it exports it if listed
			}
			if err := s.exportPage(ctx, st, dir, u); err != nil {
				return rep, err
			}
			rep.Pages++
		}
	}
	if res, ok, err := s.RenderNotFound(ctx); ok {
		if err != nil {
			return rep, fmt.Errorf("site: export 404: %w", err)
		}
		if res.HasState() && !s.statesShared() {
			return rep, fmt.Errorf("site: export 404: %w", ErrExportNeedsKeys)
		}
		b, err := st.eng.Fill(ctx, res, brick.WithStaticState())
		if err != nil {
			return rep, err
		}
		if err := write(dir, "404.html", s.exportHTML(b)); err != nil {
			return rep, err
		}
	}
	if s.opts.Sitemap != nil {
		if s.ownSitemap(st) == "" {
			b, err := s.sitemap(ctx, st)
			if err != nil {
				return rep, err
			}
			if err := write(dir, sitemapFile, b); err != nil {
				return rep, err
			}
			rep.Sitemap = true
		}
	}
	if isDir(s.opts.FS, s.opts.StaticDir) {
		err := fs.WalkDir(s.opts.FS, s.opts.StaticDir, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			b, err := fs.ReadFile(s.opts.FS, p)
			if err != nil {
				return err
			}
			rep.Static++
			return write(dir, strings.TrimPrefix(p, s.opts.StaticDir+"/"), b)
		})
		if err != nil {
			return rep, err
		}
	}
	assets := st.eng.Assets()
	base := strings.Trim(st.eng.AssetBase(), "/")
	err := fs.WalkDir(assets, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := fs.ReadFile(assets, p)
		if err != nil {
			return err
		}
		rep.Assets++
		return write(dir, path.Join(base, p), b)
	})
	return rep, err
}

// skipPerRequest leaves a per-request page (brick: {per_request: true},
// v22) out of an export, with a warning and an entry in the report. Such a
// page is made for each visitor, and a static file has no visitor: writing
// the anonymous render would ship what the page exists to replace, and
// refusing would stop every site that exports its public pages and serves
// the rest. A dynamic one is skipped whole, without running its paths:.
func (s *Site) skipPerRequest(rep *ExportReport, r Route) {
	if slices.Contains(rep.Skipped, r.URL) {
		return
	}
	rep.Skipped = append(rep.Skipped, r.URL)
	s.opts.Logger.Warn("site: export skips a per-request page; serve it with brick serve or site.Handler", "page", r.Path, "url", r.URL)
}

// ErrExportNeedsPaths is Export refusing a dynamic page ([slug].html)
// without brick: {paths:}: nothing says which pages it has.
var ErrExportNeedsPaths = errors.New("a dynamic page needs brick: {paths: …} to be exported, naming the script function that lists its route values")

// exportPage renders one URL and writes it as <url>/index.html. A dynamic
// URL comes from its own paths: list, so the closed-world check is skipped.
//
// A loader's answer has no HTTP response to become on a static host: a
// redirect (the page's or an island's) is written as a meta-refresh page
// to its target, and an error status stops the export, naming the page,
// because a static file cannot carry a 403 or 410. Loader headers are
// dropped: the static host sets its own.
func (s *Site) exportPage(ctx context.Context, st *state, dir, u string) error {
	res, _, err := s.render(ctx, st, u, false)
	var rd *brick.Redirect
	if errors.As(err, &rd) {
		return s.writeExportRedirect(st, dir, u, rd)
	}
	if err != nil {
		return exportErr(u, err)
	}
	if res.HasState() && !s.statesShared() {
		return fmt.Errorf("site: export %s: %w", u, ErrExportNeedsKeys)
	}
	b, err := st.eng.Fill(ctx, res, brick.WithStaticState())
	if errors.As(err, &rd) {
		return s.writeExportRedirect(st, dir, u, rd)
	}
	if err != nil {
		return exportErr(u, err)
	}
	if r := st.routeAt(u); r.Output == "" {
		b = s.exportHTML(b)
	} else {
		b = withProlog(r.prolog, b)
	}
	return write(dir, exportTarget(u), b)
}

// exportErr names the page an export stopped at; a loader's error status
// says why a static file cannot answer it.
func exportErr(u string, err error) error {
	if he := (*brick.HTTPError)(nil); errors.As(err, &he) {
		return fmt.Errorf("site: export %s: its loader answered %d, which a static file cannot send: %w", u, he.Status, err)
	}
	return fmt.Errorf("site: export %s: %w", u, err)
}

// writeExportRedirect writes u's meta-refresh page (exportRedirect).
func (s *Site) writeExportRedirect(st *state, dir, u string, rd *brick.Redirect) error {
	b, err := s.exportRedirect(st, u, rd)
	if err != nil {
		return err
	}
	return write(dir, exportTarget(u), b)
}

// exportTarget is the file a URL is written as.
func exportTarget(u string) string {
	target := strings.TrimPrefix(u, "/")
	if strings.HasSuffix(u, "/") {
		target = path.Join(target, indexFile)
	}
	return target
}

// exportHTML is an exported page as the site under its BasePath serves it
// (base meta, rewritten URLs and speculation rules), without Dev's live
// reload: an export for /docs/ is copied to the directory a web server
// serves at /docs/, with the assets at /docs/_brick/…, where the rewritten
// URLs point. A static host has no nav.js fallback of its own, so an
// exported brick:prefetch link needs the browser's rules as much as a
// served one does.
func (s *Site) exportHTML(b []byte) []byte {
	return s.speculate(rebase(injectBase(b, s.opts.BasePath), s.opts.BasePath, s.opts.KeepPaths))
}

// prepareDir makes dir ready: created when missing, cleared when it is a
// previous export, refused when it holds anything else.
func prepareDir(dir string) error {
	entries, err := os.ReadDir(dir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	case err != nil:
		return err
	case len(entries) > 0:
		if _, err := os.Stat(filepath.Join(dir, exportMarker)); err != nil {
			return fmt.Errorf("site: %s is not empty and is not a previous export (no %s); refusing to overwrite it", dir, exportMarker)
		}
		for _, e := range entries {
			if err := os.RemoveAll(filepath.Join(dir, e.Name())); err != nil {
				return err
			}
		}
	}
	return os.WriteFile(filepath.Join(dir, exportMarker), []byte("written by brick; the whole directory is replaced on the next export\n"), 0o644)
}

// write writes one file under dir, refusing any path that leaves it.
func write(dir, rel string, b []byte) error {
	if !fs.ValidPath(rel) {
		return fmt.Errorf("site: export path %q leaves the output directory", rel)
	}
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, b, 0o644)
}
