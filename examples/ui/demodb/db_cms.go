package demodb

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// cmsSchema is the Blog CMS app's table (content/apps/cms*.html,
// apps/cms/cms.star). body_html holds HTML that was sanitized before it was
// stored, and every page sanitizes it again when it renders it: the stored
// copy is never trusted on its own.
const cmsSchema = `
create table posts (
  id integer primary key,
  title text not null,
  slug text not null unique,  -- lowercase a-z, 0-9 and single hyphens
  excerpt text not null,
  body_html text not null,    -- sanitized on save and again on render
  status text not null,       -- draft | scheduled | published
  tags text not null,         -- comma-separated, lowercase
  author text not null,
  cover text not null,        -- primary | success | warning | danger | info | neutral
  published text not null,    -- ISO "YYYY-MM-DDTHH:MM", "" for a draft
  updated text not null       -- ISO date
);
`

// cmsSeedPost is one seeded post; the seed is deterministic so pages that
// list posts render the same bytes on every run.
type cmsSeedPost struct {
	title, excerpt, tags, cover, status, published string
	author                                         int
}

var (
	cmsAuthors = []string{"Ada Lovelace", "Grace Hopper", "Alan Turing", "Katherine Johnson"}
	cmsPosts   = []cmsSeedPost{
		{"Introducing brick", "A component engine for Go: pages, layouts and components as HTML files, with no build step.", "release,go", tonePrimary, postPublished, "2026-01-12T09:00", 0},
		{"Why server state wins", "Actions keep state on the server and patch the page in place. Here is why that is simpler than it sounds.", "architecture,actions", toneInfo, postPublished, "2026-01-26T09:00", 1},
		{"Designing a compact UI kit", "Density over whitespace: how the kit stays readable at 13 pixels.", "design,kit", toneDanger, postPublished, "2026-02-09T09:00", 2},
		{"Islands, explained", "Cache the page, render the per-visitor parts on every request.", "architecture,performance", toneSuccess, postPublished, "2026-02-23T09:00", 0},
		{"Forms that validate on the server", "Return [state, errors] from an action and the fields show their messages.", "forms,actions", toneWarning, postPublished, "2026-03-09T09:00", 3},
		{"Dark mode in one attribute", "Tokens, not overrides: how the theme switches without touching a component.", "design,css", toneNeutral, postPublished, "2026-03-23T09:00", 2},
		{"Accessible menus from details", "Progressive enhancement turns a native details element into a full menu button.", "accessibility,kit", tonePrimary, postPublished, "2026-04-06T09:00", 1},
		{"SQLite in the showcase", "Every demo app reads a real database, seeded the same way on every run.", "databases,go", toneInfo, postPublished, "2026-04-20T09:00", 3},
		{"Sanitize on the way in and out", "User HTML is data. It is cleaned before it is stored and again before it is shown.", "security,cms", toneDanger, postPublished, "2026-05-04T09:00", 0},
		{"Navigation without reloads", "Links fetch the next page and patch only what changed.", "performance,navigation", toneSuccess, postPublished, "2026-05-18T09:00", 1},
		{"Starlark for page logic", "A small, sealed language for loaders and actions — no file access, ever.", "scripting,go", toneNeutral, postPublished, "2026-06-01T09:00", 2},
		{"The v1 roadmap", "What ships next: charts, a data grid and remote components.", "roadmap", toneWarning, postScheduled, "2026-11-02T09:00", 0},
		{"Testing components in jsdom", "Render with the engine, run the real modules, assert the DOM.", "testing,kit", toneNeutral, postScheduled, "2026-11-16T09:00", 3},
		{"Draft: a theme gallery", "Notes for a gallery of community themes.", "design", tonePrimary, postDraft, "", 2},
		{"Draft: migrating from a static site generator", "What maps one to one, and what does not.", "migration,go", toneInfo, postDraft, "", 1},
	}
)

// cmsSlug turns a title into the post's slug: lowercase ASCII letters and
// digits joined by single hyphens (the same rule apps/cms/cms.star applies).
func cmsSlug(title string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(title) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		case b.Len() > 0 && !dash:
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.TrimSuffix(b.String(), "-")
}

// cmsBody is a seeded post's body: plain, already-safe HTML.
func cmsBody(p cmsSeedPost) string {
	return fmt.Sprintf("<p>%s is a note from the brick team, seeded into SQLite and served through the Blog CMS demo.</p><h2>What changes</h2><p>brick renders HTML on the server and sends only what the page needs. Components are files, and every one of them works without JavaScript.</p><ul><li>One file per component</li><li>No build step</li><li>State stays on the server</li></ul><blockquote>Write the page you want to see, then make it faster.</blockquote><p>Read the <a href=\"/components/button/\">kit docs</a> to try it.</p>", p.title)
}

// Seeded posts that are not published carry an updated date counted from
// seedEpoch: day cmsUpdatedDay for the first post, cmsUpdatedStep days apart,
// so drafts and scheduled posts sort after the published ones by date.
const (
	cmsUpdatedDay  = 200
	cmsUpdatedStep = 3
)

// seedCMS creates and fills the posts table. It is called at the end of
// seed() in db.go.
func seedCMS(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, cmsSchema); err != nil {
		return fmt.Errorf("cms schema: %w", err)
	}
	for i, p := range cmsPosts {
		updated := day(cmsUpdatedDay + i*cmsUpdatedStep)
		if p.published != "" && p.status == postPublished {
			updated = p.published[:10]
		}
		if _, err := db.ExecContext(ctx, "insert into posts (title, slug, excerpt, body_html, status, tags, author, cover, published, updated) values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
			p.title, cmsSlug(p.title), p.excerpt, cmsBody(p), p.status, p.tags, cmsAuthors[p.author], p.cover, p.published, updated); err != nil {
			return fmt.Errorf("cms seed: %w", err)
		}
	}
	return nil
}
