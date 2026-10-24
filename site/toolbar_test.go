package site

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/ubgo/brick"
)

// toolbarSite is blog() with a page that renders Card three times and
// Badge once, so the toolbar has a count and an order to show.
func toolbarSite() fstest.MapFS {
	fsys := blog()
	fsys["components/Badge.html"] = &fstest.MapFile{Data: []byte("<i>{{ .Props.t }}</i>")}
	fsys["content/many.html"] = &fstest.MapFile{Data: []byte("---\ntitle: Many\n---\n<Card t=\"a\" /><Card t=\"b\" /><Card t=\"c\" /><Badge t=\"d\" />")}
	return fsys
}

func serve(h http.Handler, url string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url, nil))
	return rec
}

// Production output is byte-identical to the page without a toolbar: the
// exact bytes are pinned, on the rendering request and on the cache hit.
func TestToolbarNeverInProduction(t *testing.T) {
	t.Parallel()
	s := newSite(t, toolbarSite(), Options{})
	h := s.Handler()
	const want = `<html>Many|<b>a</b><b>b</b><b>c</b><i>d</i></html>`
	for i, label := range []string{"render", "cache hit"} {
		rec := serve(h, "/many/")
		if rec.Code != http.StatusOK || rec.Body.String() != want {
			t.Fatalf("%s (%d): %d %q", label, i, rec.Code, rec.Body)
		}
	}
	if rec := serve(h, "/nope/"); strings.Contains(rec.Body.String(), toolbarID) {
		t.Fatalf("404 page has the toolbar: %s", rec.Body)
	}
}

// In Dev every page gets the toolbar with its counts, the cache state of
// this request, and links to the inspector and the playground; the cached
// Result and an export never carry it, so no clock reading is cached.
func TestToolbarInDev(t *testing.T) {
	t.Parallel()
	s := newSite(t, toolbarSite(), Options{Dev: true})
	h := s.Handler()
	first := serve(h, "/many/").Body.String()
	for _, want := range []string{
		`<details id="brick-devbar" data-brick-devbar>`,
		`<span class="bd-more"> · 4 comp · cache miss</span>`,
		`<td class="bd-k">Components</td><td>4</td>`,
		`<td class="bd-k">Islands</td><td>0 (0 lazy)</td>`,
		`<td class="bd-k">Loader calls</td><td>0</td>`,
		`<a href="/_brick/play/Card" title="Open in the playground">Card</a></td><td>3</td></tr><tr><td><a href="/_brick/play/Badge" title="Open in the playground">Badge</a></td><td>1</td>`,
		`<a href="/_brick/instances">Instances</a>`,
		`<a href="/_brick/play/Card">Playground</a>`,
	} {
		if !strings.Contains(first, want) {
			t.Errorf("missing %q in:\n%s", want, first)
		}
	}
	// The toolbar sits before the live-reload script, inside the body.
	if i, j := strings.Index(first, toolbarID), strings.Index(first, "EventSource"); i < 0 || j < i {
		t.Errorf("toolbar after live script or missing:\n%s", first)
	}
	if second := serve(h, "/many/").Body.String(); !strings.Contains(second, "cache hit") {
		t.Errorf("second request not a cache hit:\n%s", second)
	}
	if rec := serve(h, "/nope/"); !strings.Contains(rec.Body.String(), toolbarID) {
		t.Errorf("404 page has no toolbar: %s", rec.Body)
	}
	res, err := s.Render(context.Background(), "/many/")
	if err != nil {
		t.Fatal(err)
	}
	b, err := res.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), toolbarID) {
		t.Errorf("cached Result carries the toolbar: %s", b)
	}
	dir := filepath.Join(t.TempDir(), "out")
	if _, err := s.Export(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	out, _ := os.ReadFile(filepath.Join(dir, "many", "index.html"))
	if len(out) == 0 || strings.Contains(string(out), toolbarID) {
		t.Errorf("export carries the toolbar (or is empty): %s", out)
	}
}

// Under a base path the toolbar's links point inside the site.
func TestToolbarUnderBasePath(t *testing.T) {
	t.Parallel()
	s := newSite(t, toolbarSite(), Options{Dev: true, BasePath: "/docs"})
	body := serve(s.Handler(), "/docs/many/").Body.String()
	for _, want := range []string{`href="/docs/_brick/instances"`, `href="/docs/_brick/play/Card"`} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in:\n%s", want, body)
		}
	}
}

func TestFormatTook(t *testing.T) {
	t.Parallel()
	for d, want := range map[time.Duration]string{
		420 * time.Microsecond:  "420µs",
		1234 * time.Microsecond: "1.23ms",
		1500 * time.Millisecond: "1.50s",
	} {
		if got := formatTook(d); got != want {
			t.Errorf("formatTook(%v) = %q, want %q", d, got, want)
		}
	}
}

// lazySite is toolbarSite with a page whose one component, Panel, is
// lazy and renders Card twice and Badge once.
func lazySite() fstest.MapFS {
	fsys := toolbarSite()
	fsys["components/Panel.html"] = &fstest.MapFile{Data: []byte(`<div><Card t="x" /><Card t="y" /><Badge t="z" /></div>`)}
	fsys["content/lazy.html"] = &fstest.MapFile{Data: []byte("---\ntitle: Lazy\n---\n<Panel brick:lazy />")}
	return fsys
}

// lazyTicket is the ticket of the page's first lazy island.
var lazyTicket = regexp.MustCompile(`data-brick-lazy="([^"]+)"`)

// TestToolbarLazyIslands: in Dev the page's toolbar has
// a Lazy islands section waiting for the island, and the island's answer
// carries what it rendered in a Server-Timing entry the toolbar reads.
func TestToolbarLazyIslands(t *testing.T) {
	t.Parallel()
	h := newSite(t, lazySite(), Options{Dev: true}).Handler()
	page := serve(h, "/lazy/").Body.String()
	for _, want := range []string{
		`<td class="bd-k">Islands</td><td>1 (1 lazy)</td>`,
		`<div class="bd-h">Lazy islands</div><table data-brick-devbar-lazy><tr class="bd-wait"><td class="bd-k">waiting</td><td>1</td></tr></table>`,
		`s.name!=="brick-island"`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("missing %q in:\n%s", want, page)
		}
	}
	m := lazyTicket.FindStringSubmatch(page)
	if m == nil {
		t.Fatalf("no lazy ticket in:\n%s", page)
	}
	rec := serve(h, "/_brick/island?t="+m[1])
	got, ok := brick.ParseIslandTiming(rec.Header().Get(brick.ServerTimingHeader))
	if rec.Code != http.StatusOK || !ok {
		t.Fatalf("island answer: %d %v", rec.Code, rec.Header())
	}
	want := brick.IslandTiming{Component: "Panel", Components: 3, Types: []brick.TypeCount{{Name: "Card", Count: 2}, {Name: "Badge", Count: 1}}}
	if got.Component != want.Component || got.Components != want.Components || len(got.Types) != 2 || got.Types[0] != want.Types[0] || got.Types[1] != want.Types[1] {
		t.Errorf("island timing = %+v, want %+v", got, want)
	}
	if !strings.Contains(rec.Header().Get(brick.ServerTimingHeader), ";dur=") {
		t.Errorf("no duration in %q", rec.Header().Get(brick.ServerTimingHeader))
	}

	// A page without lazy islands has no section to fill.
	if many := serve(h, "/many/").Body.String(); strings.Contains(many, "<table data-brick-devbar-lazy>") {
		t.Errorf("a page without lazy islands has the section:\n%s", many)
	}
}

// TestIslandTimingNeverInProduction: the counts name the components a page
// is built from, so a Prod island answer never carries them.
func TestIslandTimingNeverInProduction(t *testing.T) {
	t.Parallel()
	h := newSite(t, lazySite(), Options{}).Handler()
	page := serve(h, "/lazy/").Body.String()
	m := lazyTicket.FindStringSubmatch(page)
	if m == nil {
		t.Fatalf("no lazy ticket in:\n%s", page)
	}
	rec := serve(h, "/_brick/island?t="+m[1])
	if rec.Code != http.StatusOK || rec.Header().Get(brick.ServerTimingHeader) != "" {
		t.Fatalf("Prod island answer: %d, Server-Timing %q", rec.Code, rec.Header().Get(brick.ServerTimingHeader))
	}
}
