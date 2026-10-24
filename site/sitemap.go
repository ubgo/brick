package site

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/ubgo/brick"

	"github.com/ubgo/brick/internal/weburl"
)

// The built-in sitemap (v14): opt-in through Options.Sitemap (brick.yaml
// `sitemap:` for the CLI). It lists every page a crawler should index, with
// absolute URLs and, for a translated page, its hreflang alternates from the
// same pairing .Page.Translations uses. Left out: drafts (even when serve
// shows them), output pages (feed.xml, robots.txt), pages with brick:
// {sitemap: false}, the 404 page, and dynamic pages without brick: {paths:}
// (nothing says which URLs they have). Same site, same bytes: entries are
// sorted by URL.

// Sitemap configures the built-in sitemap.
type Sitemap struct {
	// BaseURL is the absolute URL every location starts with, such as
	// https://example.com. Empty reads baseURL from .Site (site.yaml), the
	// key a theme's templates already use for absolute links. A BasePath is
	// added after it unless BaseURL already ends with it.
	BaseURL string
}

// sitemapFile is the built-in sitemap's URL and exported file name.
const sitemapFile = "sitemap.xml"

// siteBaseURLKey is the .Site key read when Sitemap.BaseURL is empty.
const siteBaseURLKey = "baseURL"

// lastmodKey is the frontmatter key a page's <lastmod> comes from; date:
// is the fallback.
const lastmodKey = "lastmod"

// sitemapXMLNS and xhtmlXMLNS are the sitemap protocol's namespace and the
// one its hreflang links use.
const (
	sitemapXMLNS = "http://www.sitemaps.org/schemas/sitemap/0.9"
	xhtmlXMLNS   = "http://www.w3.org/1999/xhtml"
)

// sitemapBase resolves the base URL for a build: the option, else .Site's
// baseURL. It must be absolute http(s), since a sitemap with relative
// locations is invalid; an empty or relative one is an error from New.
func sitemapBase(opt *Sitemap, siteVal map[string]any, basePath string) (string, error) {
	raw := opt.BaseURL
	if raw == "" {
		raw, _ = siteVal[siteBaseURLKey].(string)
	}
	u, err := url.Parse(raw)
	if raw == "" || err != nil || !weburl.Is(u) {
		return "", fmt.Errorf("site: the sitemap needs an absolute base URL (https://example.com): set Options.Sitemap.BaseURL (brick.yaml sitemap: {baseURL: …}) or baseURL in site.yaml; got %q", raw)
	}
	base := strings.TrimSuffix(raw, "/")
	if basePath != "" && !strings.HasSuffix(u.Path, basePath) && !strings.HasSuffix(u.Path, basePath+"/") {
		base += basePath
	}
	return base, nil
}

// sitemapEntry is one <url>.
type sitemapEntry struct {
	loc     string
	lastmod time.Time
	alts    []brick.Translation
}

// sitemap renders sitemap.xml for a build. Dynamic pages run their paths:
// function, as Export does.
func (s *Site) sitemap(ctx context.Context, st *state) ([]byte, error) {
	byPath := map[string]Page{}
	for _, pg := range st.pages {
		byPath[pg.Path] = pg
	}
	listed := func(p string) bool {
		pg, ok := byPath[p]
		return !ok || (!pg.Draft && !pg.noSitemap && pg.Output == "")
	}
	var entries []sitemapEntry
	for _, u := range st.order {
		r := st.routes[u]
		if !listed(r.page) {
			continue
		}
		entries = append(entries, sitemapEntry{loc: u, lastmod: lastmodOf(byPath[r.page]), alts: st.trans[u]})
	}
	for _, p := range st.patterns {
		if p.paths == "" || p.route.Output != "" || st.skipSitemap[p.route.page] {
			continue
		}
		urls, err := s.pathEntries(ctx, st, p)
		if err != nil {
			return nil, fmt.Errorf("site: sitemap %s: %w", p.route.Path, err)
		}
		for _, u := range urls {
			if _, static := st.routes[u]; static {
				continue
			}
			m, ok := st.match(u)
			if !ok || m.pat != p {
				continue // an earlier pattern takes this URL
			}
			entries = append(entries, sitemapEntry{loc: u, alts: st.translationsOf(m.route(u), false)})
		}
	}
	slices.SortFunc(entries, func(a, b sitemapEntry) int { return strings.Compare(a.loc, b.loc) })
	var b bytes.Buffer
	b.WriteString(xml.Header)
	b.WriteString(`<urlset xmlns="` + sitemapXMLNS + `" xmlns:xhtml="` + xhtmlXMLNS + `">` + "\n")
	for _, e := range entries {
		b.WriteString("  <url>\n    <loc>")
		xmlText(&b, st.sitemapBase+e.loc)
		b.WriteString("</loc>\n")
		if !e.lastmod.IsZero() {
			b.WriteString("    <lastmod>" + e.lastmod.UTC().Format(time.DateOnly) + "</lastmod>\n")
		}
		if len(e.alts) > 0 {
			// The set names every language including this one, as hreflang
			// requires, in the site's locale order.
			self, _ := st.localeOfURL(e.loc)
			alts := append([]brick.Translation{{Locale: self, URL: e.loc}}, e.alts...)
			slices.SortStableFunc(alts, func(x, y brick.Translation) int { return compareLocale(x.Locale, y.Locale, st.locales) })
			for _, a := range alts {
				b.WriteString(`    <xhtml:link rel="alternate" hreflang="`)
				xmlText(&b, a.Locale)
				b.WriteString(`" href="`)
				xmlText(&b, st.sitemapBase+a.URL)
				b.WriteString("\"/>\n")
			}
		}
		b.WriteString("  </url>\n")
	}
	b.WriteString("</urlset>\n")
	return b.Bytes(), nil
}

// localeOfURL is the locale a URL renders in: its static route's, or its
// pattern's.
func (st *state) localeOfURL(u string) (string, bool) {
	if r, ok := st.routes[u]; ok {
		return r.Locale, true
	}
	if m, ok := st.match(u); ok {
		return m.pat.route.Locale, true
	}
	return "", false
}

// lastmodOf is a page's lastmod: frontmatter, else its date.
func lastmodOf(pg Page) time.Time {
	if t, ok := pg.Params[lastmodKey].(time.Time); ok {
		return t
	}
	return pg.Date
}

// xmlText writes s escaped for XML text and attribute values.
func xmlText(b *bytes.Buffer, s string) { _ = xml.EscapeText(b, []byte(s)) }

// ownSitemap names the project file that shadows the built-in sitemap, a
// content/sitemap.xml.html page or a static/sitemap.xml file, or "".
func (s *Site) ownSitemap(st *state) string {
	if r, ok := st.routes["/"+sitemapFile]; ok {
		return r.Path
	}
	if p := path.Join(s.opts.StaticDir, sitemapFile); exists(s.opts.FS, p) {
		return p
	}
	return ""
}

// serveSitemap answers /sitemap.xml with the built-in sitemap.
func (s *Site) serveSitemap(w http.ResponseWriter, r *http.Request, st *state) {
	b, err := s.sitemap(r.Context(), st)
	if err != nil {
		s.opts.Logger.Error("site: sitemap", "err", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", outputTypes["xml"])
	_, _ = w.Write(b)
}
