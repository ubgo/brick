package main

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"

	"github.com/ubgo/brick"
	"github.com/ubgo/brick/kit"
	"github.com/ubgo/brick/site"
)

// showcase builds the site once per test binary run.
func showcase(t *testing.T) (*site.Site, http.Handler) {
	t.Helper()
	s, db, err := newSite(context.Background(), ".", false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return s, s.Handler()
}

// appSourceStart opens an app page's View source drawer (AppSource), the
// last element of its body.
const appSourceStart = `<div class="app-src"`

// get fetches a URL from the showcase's handler, without an app page's View
// source drawer: the drawer shows the page's own source, so a text
// assertion could otherwise pass by matching the code instead of the
// rendered app. getRaw keeps it.
func get(t *testing.T, h http.Handler, url string) (int, string) {
	t.Helper()
	code, body := getRaw(t, h, url)
	if i := strings.Index(body, appSourceStart); i >= 0 {
		if j := strings.LastIndex(body, "</body>"); j > i {
			body = body[:i] + body[j:]
		}
	}
	return code, body
}

// getRaw fetches a URL from the showcase's handler, as served.
func getRaw(t *testing.T, h http.Handler, url string) (int, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url, nil))
	return rec.Code, rec.Body.String()
}

// actionPath is where the site layer serves actions: the engine's asset
// base ("/_brick/") plus "action".
const actionPath = "/_brick/action"

// postAction runs one action on the stateful component identified by
// handle and token, exactly as the browser runtime posts it (a JSON body and
// the Brick-Action header), and returns the status and the re-rendered HTML.
// Nil args are sent as [] — the runtime never sends null.
func postAction(t *testing.T, h http.Handler, handle, token, action string, args ...any) (int, string) {
	t.Helper()
	if args == nil {
		args = []any{}
	}
	body, err := json.Marshal(map[string]any{"handle": handle, "token": token, "action": action, "args": args})
	if err != nil {
		t.Fatalf("%s: %v", action, err)
	}
	req := httptest.NewRequest(http.MethodPost, actionPath, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Brick-Action", "1")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

// mustContain fails for each want missing from body.
func mustContain(t *testing.T, page, body string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(body, w) {
			t.Errorf("%s is missing %q", page, w)
		}
	}
}

// Every page renders, with no diagnostics, and a page without per-visitor
// state renders the same bytes twice.
func TestEveryPageRenders(t *testing.T) {
	s, h := showcase(t)
	diags, err := s.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range diags {
		t.Errorf("check: %s", d)
	}
	for _, r := range s.Routes() {
		code, a := get(t, h, r.URL)
		if code != http.StatusOK {
			t.Errorf("%s: status %d\n%s", r.URL, code, a)
			continue
		}
		if strings.Contains(a, "data-brick-state") {
			continue
		}
		if _, b := get(t, h, r.URL); a != b {
			t.Errorf("%s: two renders differ", r.URL)
		}
	}
}

// kebab turns a component name into its page's file name: DataTable →
// data-table.
func kebab(name string) string {
	return strings.ToLower(regexp.MustCompile(`([a-z0-9])([A-Z])`).ReplaceAllString(name, "$1-$2"))
}

// noPage lists kit components documented on another component's page.
var noPage = map[string]string{
	"Tab": "documented on the Tabs page",
}

// Every kit component has a page, in a category the sidebar lists.
func TestEveryComponentHasAPage(t *testing.T) {
	b, err := os.ReadFile("data/nav.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var nav struct {
		Categories []string `yaml:"categories"`
	}
	if err := yaml.Unmarshal(b, &nav); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(repoKitDir, "components"))
	if err != nil {
		t.Fatal(err)
	}
	catRe := regexp.MustCompile(`(?m)^category: (.+)$`)
	for _, e := range entries {
		name := strings.TrimSuffix(e.Name(), ".html")
		if _, ok := noPage[name]; ok {
			continue
		}
		page := filepath.Join("content", "components", kebab(name)+".html")
		src, err := os.ReadFile(page)
		if err != nil {
			t.Errorf("%s has no page %s", name, page)
			continue
		}
		m := catRe.FindSubmatch(src)
		if m == nil || !slices.Contains(nav.Categories, string(m[1])) {
			t.Errorf("%s: category missing or not in data/nav.yaml", page)
		}
	}
}

// Every example with source has Preview and Code tabs, its code panel and a
// copy button aimed at that panel — the tabbed example viewer.
func TestExamplesHaveCodeTabs(t *testing.T) {
	_, h := showcase(t)
	_, body := get(t, h, "/components/button/")
	for _, want := range []string{`role="tab" data-tab="preview"`, `role="tab" data-tab="code"`, `class="ex__code"`, `data-brick-type="CopyButton"`, `pc pre&#34;`} {
		if !strings.Contains(body, want) {
			t.Errorf("/components/button/ is missing %q", want)
		}
	}
	if n := strings.Count(body, `data-tab="code"`); n != strings.Count(body, `class="ex__code"`) || n == 0 {
		t.Errorf("code tabs %d, code panels %d", n, strings.Count(body, `class="ex__code"`))
	}
}

// Every demo app has a View source drawer listing its own page first, one
// highlighted, copyable CodeView per file.
func TestAppsHaveViewSource(t *testing.T) {
	_, h := showcase(t)
	apps := []string{"contacts", "invoices", "tasks", "dashboard", "inbox", "store", "chat", "calendar", "cms", "settings", "auth", "landing"}
	for _, app := range apps {
		url := "/apps/" + app + "/"
		code, body := getRaw(t, h, url)
		if code != http.StatusOK {
			t.Errorf("%s: status %d", url, code)
			continue
		}
		if _, stripped := get(t, h, url); strings.Contains(stripped, `class="cv__name"`) {
			t.Errorf("%s: get does not strip the source drawer", url)
		}
		mustContain(t, url, body, `class="app-src__open"`, `data-brick-type="CodeView"`, `<span class="cv__name">content/apps/`+app+`.html</span>`)
		if files, copies := strings.Count(body, `class="cv__name"`), strings.Count(body, `label="Copy `); files == 0 || files > copies {
			t.Errorf("%s: %d files but %d copy buttons", url, files, copies)
		}
	}
}

// Serving reads the repository's kit from disk, so kit edits reload; every
// other mode uses the embedded kit. Both are the same library, brick-kit.
func TestKitLibraryLiveWhenServing(t *testing.T) {
	embedded, err := kitLibrary(".", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := embedded.FS.(embed.FS); !ok || embedded.Name != kit.Name {
		t.Fatalf("not serving: want the embedded kit, got %T %q", embedded.FS, embedded.Name)
	}
	live, err := kitLibrary(".", true)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := live.FS.(embed.FS); ok || live.Name != kit.Name {
		t.Fatalf("serving: want the kit folder on disk, got %T %q", live.FS, live.Name)
	}
	onDisk, err := fs.ReadFile(live.FS, brick.LibraryManifest)
	if err != nil {
		t.Fatal(err)
	}
	shipped, err := fs.ReadFile(kit.FS(), brick.LibraryManifest)
	if err != nil || string(onDisk) != string(shipped) {
		t.Fatalf("the live kit is not the repository's kit: %v", err)
	}
}
