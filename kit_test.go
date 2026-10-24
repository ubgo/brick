package brick

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

// kitOptions is an engine over the kit used as a library, as a site uses it
// — its manifest, kit/components and kit/data, so the kit's ~/components/…
// imports resolve against the library root — plus one page, in memory.
func kitOptions(t *testing.T, page string) Options {
	t.Helper()
	fsys := fstest.MapFS{"content/a.html": {Data: []byte(page)}}
	b, err := os.ReadFile("kit/" + LibraryManifest)
	if err != nil {
		t.Fatal(err)
	}
	fsys["kit/"+LibraryManifest] = &fstest.MapFile{Data: b}
	for _, dir := range []string{"kit/components", "kit/data"} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, en := range entries {
			b, err := os.ReadFile(filepath.Join(dir, en.Name()))
			if err != nil {
				t.Fatal(err)
			}
			fsys[dir+"/"+en.Name()] = &fstest.MapFile{Data: b}
		}
	}
	return Options{FS: fsys, Libraries: []Library{{Dir: "kit"}}}
}

// Without JavaScript every kit component is complete HTML.
func TestKitRendersWithoutJS(t *testing.T) {
	t.Parallel()
	e := newEngine(t, kitOptions(t, `<Tabs label="Install"><Tab title="Go">go get</Tab><Tab title="CLI">go install</Tab></Tabs><Accordion title="Why?" group="faq" open>Because.</Accordion><Carousel><img alt="a"><img alt="b"></Carousel>{{ assets }}`))
	got := render(t, e, "content/a.html", nil)
	for _, want := range []string{
		`role="tablist" aria-label="Install"`, `>Go</button>`, `>CLI</button>`,
		`role="tabpanel"`, `>go get`, `>go install`,
		`class="bk-accordion" open name="faq">`, `<summary>Why?</summary>`,
		`class="bk-track" tabindex="0"><img alt="a"><img alt="b"></div>`,
		`/_brick/c/Tabs.`, `/_brick/c/Carousel.`, `/_brick/c/runtime.`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
	if strings.Contains(got, "/_brick/c/Accordion.") && strings.Contains(got, "Accordion.js") {
		t.Error("Accordion needs no script")
	}
	if err := renderErr(t, newEngine(t, kitOptions(t, `<Tabs><Tab>x</Tab></Tabs>`)), "content/a.html"); !strings.Contains(err.Error(), "requires the prop \"title\"") {
		t.Fatalf("Tab without a title: %v", err)
	}
}

// With JavaScript, Tabs shows one panel and answers the keyboard: the real
// rendered page and the real modules, run in jsdom.
func TestKitTabsInBrowser(t *testing.T) {
	runKitJS(t, `<Tabs label="Install"><Tab title="Go">go get</Tab><Tab title="CLI">go install</Tab><Tab title="Docker">docker run</Tab></Tabs><Carousel><b>1</b><b>2</b></Carousel>{{ assets }}`, "kit_tabs.mjs")
}

// runKitJS renders page with the kit, writes it and every asset file to a
// temporary directory, and runs runtime/test/<script> on that directory in
// node. The script loads what it needs with kit_harness.mjs and must print
// "<script without .mjs> ok" at the end. Skipped without node or jsdom
// (`task js:deps`).
func runKitJS(t *testing.T, page, script string) {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	if _, err := os.Stat("runtime/test/node_modules/jsdom"); err != nil {
		t.Skip("jsdom is not installed: run `task js:deps`")
	}
	e := newEngine(t, kitOptions(t, page))
	out := render(t, e, "content/a.html", nil)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "page.html"), []byte(out), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, p := range listAssetFiles(t, e) {
		f, err := e.Assets().Open(p)
		if err != nil {
			t.Fatal(err)
		}
		b, err := io.ReadAll(f)
		_ = f.Close()
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, filepath.Base(p)), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.CommandContext(t.Context(), node, script, dir)
	cmd.Dir = "runtime/test"
	res, err := cmd.CombinedOutput()
	if want := strings.TrimSuffix(script, ".mjs") + " ok"; err != nil || !strings.Contains(string(res), want) {
		t.Fatalf("%s: %v\n%s", script, err, res)
	}
}
