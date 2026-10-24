package site

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

// An exported page carries the same speculation rules the server would
// send (a static host is where native prefetch matters most), scoped to
// the base path, and a page without a brick:prefetch link carries none.
func TestExportSpeculationRules(t *testing.T) {
	t.Parallel()
	fsys := blog()
	fsys["layouts/default.html"] = &fstest.MapFile{Data: []byte("<html><body>{{ children }}{{ assets }}</body></html>")}
	fsys["content/pf.html"] = &fstest.MapFile{Data: []byte(`<a href="/about/" brick:prefetch>a</a>`)}
	s := newSite(t, fsys, Options{BasePath: "/docs"})
	dir := filepath.Join(t.TempDir(), "out")
	if _, err := s.Export(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	read := func(p string) string {
		b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(p)))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	const open = `<script type="speculationrules">`
	pf := read("pf/index.html")
	i := strings.Index(pf, open)
	if i < 0 {
		t.Fatalf("exported page has no speculation rules:\n%s", pf)
	}
	if !strings.Contains(pf[i:], `"href_matches":"/docs/*"`) || !strings.HasSuffix(strings.TrimSpace(pf), "</body></html>") {
		t.Errorf("rules are not scoped to the base or not before </body>:\n%s", pf)
	}
	rec, _, _ := get(t, s.Handler(), "/docs/pf/")
	served := rec.Body.String()
	if j := strings.Index(served, open); j < 0 || served[j:] != pf[i:] {
		t.Errorf("exported rules differ from served ones\nexport: %s\nserved: %s", pf[i:], served)
	}
	if about := read("about/index.html"); strings.Contains(about, open) {
		t.Errorf("a page without a prefetch link got rules:\n%s", about)
	}
}
