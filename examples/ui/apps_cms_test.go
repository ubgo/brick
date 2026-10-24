package main

import (
	"context"
	"database/sql"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"
)

// cmsURL is the Blog CMS admin; cmsBlogURL and cmsPostURL its public side.
const (
	cmsURL     = "/apps/cms/"
	cmsBlogURL = "/apps/cms/blog/"
	cmsPostURL = "/apps/cms/blog/post/?slug="
)

// cmsPayload is user HTML carrying every classic injection: a script, an
// event handler, a javascript: URL, an iframe and a style attribute.
const cmsPayload = `<p style="color:red" onclick="steal()">Hello <b>world</b></p><script>alert(1)</script><img src="x" onerror="alert(2)"><a href="javascript:alert(3)">click</a><iframe src="https://evil.test"></iframe><a href="https://ok.test/">ok</a>`

// cmsBad lists what must never survive sanitizing cmsPayload.
var cmsBad = []string{"<script", "alert(1)", "onerror", "javascript:", "<iframe", "onclick", "style=\"color"}

// cmsSession drives the CMS admin as the runtime does: one page load, then
// actions posted to /_brick/action.
type cmsSession struct {
	t             *testing.T
	h             http.Handler
	handle, token string
}

// cmsStart builds the showcase over a fresh database and opens the admin.
func cmsStart(t *testing.T) (*cmsSession, string, *sql.DB) {
	t.Helper()
	s, db, err := newSite(context.Background(), ".", false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	h := s.Handler()
	code, page := get(t, h, cmsURL)
	if code != http.StatusOK {
		t.Fatalf("%s: status %d\n%s", cmsURL, code, page)
	}
	handle := regexp.MustCompile(`data-brick-state="([^"]+)"`).FindAllStringSubmatch(page, -1)
	token := regexp.MustCompile(`data-brick-token="([^"]+)"`).FindStringSubmatch(page)
	if len(handle) != 1 || token == nil {
		t.Fatalf("want one stateful CmsApp, got %d", len(handle))
	}
	return &cmsSession{t: t, h: h, handle: handle[0][1], token: token[1]}, page, db
}

// act posts one action and returns the re-rendered component.
func (c *cmsSession) act(action string, args ...any) string {
	c.t.Helper()
	code, out := postAction(c.t, c.h, c.handle, c.token, action, args...)
	if code != http.StatusOK {
		c.t.Fatalf("%s: status %d\n%s", action, code, out)
	}
	return out
}

// cmsForm is a complete editor submission; override fields per test.
func cmsForm(over map[string]string) map[string]string {
	f := map[string]string{"title": "Hello brick", "slug": "", "excerpt": "An excerpt.", "body": "<p>Body text.</p>", "tags": "go, Kit, go", "status": "draft", "author": "Grace Hopper", "cover": "#16a34a", "date": "", "time": "09:00", "intent": "save"}
	for k, v := range over {
		f[k] = v
	}
	return f
}

// cmsRow reads one post by slug; ok is false when there is none.
func cmsRow(t *testing.T, db *sql.DB, slug string) (row map[string]string, ok bool) {
	t.Helper()
	var id int
	var title, excerpt, body, status, tags, author, cover, published string
	err := db.QueryRow("select id, title, excerpt, body_html, status, tags, author, cover, published from posts where slug = ?", slug).Scan(&id, &title, &excerpt, &body, &status, &tags, &author, &cover, &published)
	if err == sql.ErrNoRows {
		return nil, false
	}
	if err != nil {
		t.Fatal(err)
	}
	return map[string]string{"title": title, "excerpt": excerpt, "body": body, "status": status, "tags": tags, "author": author, "cover": cover, "published": published}, true
}

// cmsCount counts posts matching a fixed where clause written by the test.
func cmsCount(t *testing.T, db *sql.DB, where string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow("select count(*) from posts where "+where, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// cmsTitles lists the titles of the rendered list rows, in order.
func cmsTitles(html string) []string {
	var out []string
	for _, m := range regexp.MustCompile(`class="cms-post__title"[^>]*>([^<]+)</button>`).FindAllStringSubmatch(html, -1) {
		out = append(out, m[1])
	}
	return out
}

// The list: counts per status, filters, search, sort and pages.
func TestCmsList(t *testing.T) {
	c, page, db := cmsStart(t)
	mustContain(t, "first render", page, "15 posts · 11 published · 2 scheduled",
		`Drafts<span class="cms-count">2</span>`, `Published<span class="cms-count">11</span>`,
		`<b>1</b>–<b>10</b> of <b>15</b>`, `brick:click="cms.goto(2)"`)

	for status, n := range map[string]int{"draft": 2, "scheduled": 2, "published": 11} {
		out := c.act("cms.filter_status", status)
		if got := len(cmsTitles(out)); got != min(n, 10) {
			t.Errorf("filter %s: %d rows, want %d", status, got, min(n, 10))
		}
		for _, other := range []string{"draft", "scheduled", "published"} {
			if other != status && strings.Contains(out, `data-status="`+other+`"`) {
				t.Errorf("filter %s shows a %s post", status, other)
			}
		}
	}
	c.act("cms.filter_status", "all")

	out := c.act("$set", "q", "sanitize")
	if got := cmsTitles(out); len(got) != 1 || got[0] != "Sanitize on the way in and out" {
		t.Errorf("search: %v", got)
	}
	c.act("cms.clear_filters")

	out = c.act("cms.sort_by", "title")
	var want []string
	rows, err := db.Query("select title from posts order by title collate nocase asc, id asc limit 10")
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var s string
		_ = rows.Scan(&s)
		want = append(want, s)
	}
	_ = rows.Close()
	if got := cmsTitles(out); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("sort by title:\n got  %v\n want %v", got, want)
	}
	out = c.act("cms.goto", 2)
	mustContain(t, "page 2", out, `<b>11</b>–<b>15</b> of <b>15</b>`)
}

// Create and edit: validation errors insert nothing, the slug comes from
// the title and must be unique, tags are normalized, and an edit updates.
func TestCmsCreateEdit(t *testing.T) {
	c, _, db := cmsStart(t)
	c.act("cms.new_post")

	out := c.act("cms.save", cmsForm(map[string]string{"title": "Hi", "slug": "Bad Slug!", "status": "gone", "author": "Nobody", "tags": "a,b,c,d,e,f"}))
	mustContain(t, "invalid", out, "Give the post a title of at least three characters.", "Use lowercase letters, digits and single hyphens.",
		"Choose draft, scheduled or published.", "Choose an author from the list.", "Use at most 5 tags.")
	if n := cmsCount(t, db, "1 = 1"); n != 15 {
		t.Fatalf("an invalid save inserted a post: %d", n)
	}

	out = c.act("cms.save", cmsForm(nil))
	row, ok := cmsRow(t, db, "hello-brick")
	if !ok {
		t.Fatal("the post was not inserted with the slug from its title")
	}
	if row["tags"] != "go,kit" || row["cover"] != "success" || row["status"] != "draft" || row["published"] != "" || row["author"] != "Grace Hopper" {
		t.Errorf("stored fields: %v", row)
	}
	mustContain(t, "saved", out, "Saved “Hello brick”.", `value="hello-brick"`, "Save draft")

	// A second post with the same title needs its own address.
	c.act("cms.back")
	c.act("cms.new_post")
	out = c.act("cms.save", cmsForm(map[string]string{"excerpt": "Another."}))
	mustContain(t, "duplicate slug", out, "Another post already uses this address.")
	if n := cmsCount(t, db, "slug like 'hello-brick%'"); n != 1 {
		t.Fatalf("a duplicate slug was stored: %d", n)
	}

	// Edit an existing post.
	var id int
	if err := db.QueryRow("select id from posts where slug = 'islands-explained'").Scan(&id); err != nil {
		t.Fatal(err)
	}
	out = c.act("cms.edit", id)
	mustContain(t, "edit", out, `value="Islands, explained"`, `value="islands-explained"`, "Update")
	c.act("cms.save", cmsForm(map[string]string{"title": "Islands, explained again", "slug": "islands-explained", "status": "published", "date": "2026-02-23", "body": "<p>New body.</p>"}))
	row, _ = cmsRow(t, db, "islands-explained")
	if row["title"] != "Islands, explained again" || row["body"] != "<p>New body.</p>" || row["published"] != "2026-02-23T09:00" {
		t.Errorf("edit did not reach the database: %v", row)
	}

	// Duplicate makes a draft copy with its own address.
	c.act("cms.back")
	c.act("cms.duplicate", id)
	if copyRow, ok := cmsRow(t, db, "islands-explained-copy"); !ok || copyRow["status"] != "draft" {
		t.Errorf("duplicate: %v %v", copyRow, ok)
	}
	c.act("cms.duplicate", id)
	if _, ok := cmsRow(t, db, "islands-explained-copy-2"); !ok {
		t.Error("a second duplicate needs a new address")
	}
}

// Autosave: a new post is written as a draft as the visitor types, and a
// live post's edits wait in the session until the visitor saves.
func TestCmsAutosave(t *testing.T) {
	c, _, db := cmsStart(t)
	c.act("cms.new_post")
	out := c.act("cms.autosave", cmsForm(map[string]string{"title": "Work in progress", "body": "<p>First words</p>"}))
	mustContain(t, "autosave", out, "Draft saved")
	if row, ok := cmsRow(t, db, "work-in-progress"); !ok || row["status"] != "draft" {
		t.Fatal("autosave did not store the draft")
	}
	c.act("cms.autosave", cmsForm(map[string]string{"title": "Work in progress", "slug": "work-in-progress", "body": "<p>More words</p>"}))
	if row, _ := cmsRow(t, db, "work-in-progress"); row["body"] != "<p>More words</p>" {
		t.Errorf("a second autosave updates the same draft: %q", row["body"])
	}
	if n := cmsCount(t, db, "slug like 'work-in-progress%'"); n != 1 {
		t.Errorf("autosave made %d posts", n)
	}

	var id int
	if err := db.QueryRow("select id from posts where slug = 'introducing-brick'").Scan(&id); err != nil {
		t.Fatal(err)
	}
	c.act("cms.edit", id)
	out = c.act("cms.autosave", cmsForm(map[string]string{"title": "Introducing brick", "slug": "introducing-brick", "status": "published", "body": "<p>Half-written change</p>"}))
	mustContain(t, "live autosave", out, "Unsaved changes — Update to publish them", "Half-written change")
	if row, _ := cmsRow(t, db, "introducing-brick"); strings.Contains(row["body"], "Half-written") {
		t.Error("autosave changed a published post before the visitor saved")
	}
}

// Security: user HTML is sanitized before it is stored (save and autosave),
// in the Preview, and again when the public page renders a stored body —
// even one that reached the database some other way.
func TestCmsSanitize(t *testing.T) {
	c, _, db := cmsStart(t)
	c.act("cms.new_post")
	out := c.act("cms.save", cmsForm(map[string]string{"title": "Payload", "body": cmsPayload, "status": "published"}))
	row, ok := cmsRow(t, db, "payload")
	if !ok {
		t.Fatalf("not saved:\n%s", out)
	}
	for _, bad := range cmsBad {
		if strings.Contains(row["body"], bad) {
			t.Errorf("stored body kept %q: %s", bad, row["body"])
		}
	}
	if !strings.Contains(row["body"], "Hello <b>world</b>") || !strings.Contains(row["body"], `href="https://ok.test/"`) {
		t.Errorf("sanitizing lost the safe parts: %s", row["body"])
	}

	out = c.act("cms.autosave", cmsForm(map[string]string{"title": "Payload", "slug": "payload", "status": "published", "body": cmsPayload}))
	preview := out[strings.Index(out, `class="cms-preview"`):]
	for _, bad := range cmsBad {
		if strings.Contains(preview, bad) {
			t.Errorf("preview kept %q", bad)
		}
	}

	// A body written straight to the database (a migration, another app)
	// is still cleaned on the way out.
	if _, err := db.Exec("update posts set body_html = ? where slug = 'payload'", cmsPayload); err != nil {
		t.Fatal(err)
	}
	code, page := get(t, c.h, cmsPostURL+"payload")
	if code != http.StatusOK {
		t.Fatalf("post page: %d", code)
	}
	article := page[strings.Index(page, "<article>"):]
	article = article[:strings.Index(article, "</article>")]
	for _, bad := range cmsBad {
		if strings.Contains(article, bad) {
			t.Errorf("public page rendered %q", bad)
		}
	}
	mustContain(t, "public page", article, "Hello <b>world</b>", "noreferrer")
}

// Publishing: a draft is private; publishing (the confirm dialog's submit)
// makes it live on the blog with today's date; scheduling needs a future
// date and stays private until then; unpublish and delete take it down.
func TestCmsPublishFlow(t *testing.T) {
	c, _, db := cmsStart(t)
	c.act("cms.new_post")
	c.act("cms.save", cmsForm(map[string]string{"title": "Launch day", "body": "<p>We shipped.</p>"}))
	if _, page := get(t, c.h, cmsPostURL+"launch-day"); !strings.Contains(page, "No post at “launch-day”") {
		t.Error("a draft is visible on the blog")
	}

	out := c.act("cms.save", cmsForm(map[string]string{"title": "Launch day", "slug": "launch-day", "body": "", "intent": "publish"}))
	mustContain(t, "empty publish", out, "Write something before it goes live.")
	out = c.act("cms.save", cmsForm(map[string]string{"title": "Launch day", "slug": "launch-day", "body": "<p>We shipped.</p>", "intent": "publish"}))
	mustContain(t, "published", out, "Published “Launch day”.", `View</a>`)
	today := time.Now().UTC().Format(time.DateOnly)
	if row, _ := cmsRow(t, db, "launch-day"); row["status"] != "published" || row["published"] != today+"T09:00" {
		t.Fatalf("publish: %v", row)
	}
	_, blog := get(t, c.h, cmsBlogURL)
	mustContain(t, "blog list", blog, `href="/apps/cms/blog/launch-day/"`)
	_, post := get(t, c.h, cmsPostURL+"launch-day")
	mustContain(t, "post page", post, `<span class="cms-cover__title">Launch day</span>`, "We shipped.", "More posts")

	// Scheduling: a past date is refused; a future one hides the post.
	out = c.act("cms.save", cmsForm(map[string]string{"title": "Launch day", "slug": "launch-day", "status": "scheduled", "date": "2020-01-01"}))
	mustContain(t, "past schedule", out, "Pick a date after today, or publish now.")
	c.act("cms.save", cmsForm(map[string]string{"title": "Launch day", "slug": "launch-day", "status": "scheduled", "date": "2099-06-01", "time": "10:30"}))
	if row, _ := cmsRow(t, db, "launch-day"); row["status"] != "scheduled" || row["published"] != "2099-06-01T10:30" {
		t.Fatalf("schedule: %v", row)
	}
	if _, blog := get(t, c.h, cmsBlogURL); strings.Contains(blog, "slug=launch-day") {
		t.Error("a post scheduled for the future is on the blog")
	}

	// From the list: publish, move to drafts, bulk, delete.
	c.act("cms.back")
	var id int
	if err := db.QueryRow("select id from posts where slug = 'launch-day'").Scan(&id); err != nil {
		t.Fatal(err)
	}
	out = c.act("cms.publish", id)
	mustContain(t, "row publish", out, "Published “Launch day”.")
	out = c.act("cms.unpublish", id)
	mustContain(t, "row unpublish", out, "Moved “Launch day” back to drafts.")
	if row, _ := cmsRow(t, db, "launch-day"); row["status"] != "draft" || row["published"] != "" {
		t.Errorf("unpublish: %v", row)
	}
	var drafts []int
	r, err := db.Query("select id from posts where status = 'draft' order by id")
	if err != nil {
		t.Fatal(err)
	}
	for r.Next() {
		var d int
		_ = r.Scan(&d)
		drafts = append(drafts, d)
	}
	_ = r.Close()
	for _, d := range drafts {
		c.act("cms.toggle", d, true)
	}
	out = c.act("cms.publish_selected")
	mustContain(t, "bulk publish", out, "Published 3 posts.")
	if n := cmsCount(t, db, "status = 'draft'"); n != 0 {
		t.Errorf("bulk publish left %d drafts", n)
	}
	out = c.act("cms.remove", id)
	mustContain(t, "delete", out, "Deleted “Launch day”.")
	if _, ok := cmsRow(t, db, "launch-day"); ok {
		t.Error("the post is still in the database")
	}
}

// The dynamic post page, content/apps/cms/blog/[slug].html:
// a live post at its own URL, a 404 page for a slug that is not live, and
// a draft's slug is a 404 too.
func TestCmsPostRoute(t *testing.T) {
	_, h := showcase(t)
	code, page := get(t, h, cmsBlogURL+"islands-explained/")
	if code != http.StatusOK {
		t.Fatalf("live slug: status %d", code)
	}
	mustContain(t, "live slug", page, `<span class="cms-cover__title">Islands, explained</span>`, "More posts")
	for _, slug := range []string{"no-such-post", "draft-a-theme-gallery", "..", "Bad%3Cscript%3E"} {
		if code, page := get(t, h, cmsBlogURL+slug+"/"); code != http.StatusNotFound || strings.Contains(page, "<article>") {
			t.Errorf("slug %q: status %d, want the 404 page", slug, code)
		}
	}
}

// The public post page picks its post from ?slug= inside an island: a
// seeded slug renders that post, and an unknown, malformed or missing slug
// renders the not-found state with 200, never an error. Nothing outside the
// island reads the query, so the cached page is the same for every slug.
func TestCmsPostBySlug(t *testing.T) {
	_, h := showcase(t)
	code, page := get(t, h, cmsPostURL+"islands-explained")
	if code != http.StatusOK {
		t.Fatalf("seeded slug: status %d", code)
	}
	mustContain(t, "seeded slug", page, `<span class="cms-cover__title">Islands, explained</span>`, "Cache the page, render the per-visitor parts on every request.", "More posts")
	for _, slug := range []string{"no-such-post", "Bad%20Slug%3Cscript%3E", "draft-a-theme-gallery", ""} {
		code, page := get(t, h, cmsPostURL+slug)
		if code != http.StatusOK {
			t.Errorf("slug %q: status %d", slug, code)
			continue
		}
		if !strings.Contains(page, "It may be a draft, scheduled for later, or deleted.") || !strings.Contains(page, `<h1 class="bk-sr-only">`) || strings.Contains(page, "<article>") {
			t.Errorf("slug %q: want the not-found state", slug)
		}
		if strings.Contains(page, "Slug<script>") {
			t.Errorf("slug %q: the query reached the page unescaped", slug)
		}
	}
	// Outside the island the page is the same for every slug.
	strip := func(s string) string {
		return regexp.MustCompile(`(?s)<div class="cms-article.*?</main>`).ReplaceAllString(s, "")
	}
	_, a := get(t, h, cmsPostURL+"islands-explained")
	_, b := get(t, h, cmsPostURL+"no-such-post")
	if strip(a) != strip(b) {
		t.Error("something outside the CmsPost island depends on the query")
	}
}
