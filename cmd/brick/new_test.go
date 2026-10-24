package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// brick new writes starters that compile: every flag combination passes
// brick check, and an existing file is never overwritten.
func TestNew(t *testing.T) {
	dir := project(t,
		"brick.yaml", "roots: [components]\n",
		"layouts/default.html", "<main>{{ children }}</main>{{ assets }}",
	)
	for _, args := range [][]string{
		{"component", "Card"},
		{"component", "Counter", "-state"},
		{"component", "Chart", "-client"},
		{"component", "Stock", "-island"},
		{"component", "Ticker", "-state", "-client"},
		{"component", "Revenue", "-lazy"},
		{"component", "Feed", "-lazy", "-state"},
		{"page", "blog/hello-world"},
		{"page", "blog/[slug]"},
		{"page", "docs/[...path]"},
	} {
		if code, out, errs := cli(t, append(append([]string{"new"}, args...), "-dir", dir)...); code != 0 || !strings.HasPrefix(out, "wrote ") {
			t.Fatalf("new %v: %d %s %s", args, code, out, errs)
		}
	}
	calls := `<Card title="a"><p>child</p></Card><Counter /><Chart /><Stock /><Ticker /><x-card title="b"></x-card>` +
		`<brick:boundary><Revenue /><brick:prop name="loading"><p>Loading…</p></brick:prop></brick:boundary><Feed />`
	if err := os.WriteFile(filepath.Join(dir, "content", "index.html"), []byte(calls), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, out, errs := cli(t, "check", dir); code != 0 || !strings.Contains(out, "ok: 2 pages") {
		t.Fatalf("check: %d %s %s", code, out, errs)
	}
	// The dynamic starters are patterns with their parameter.
	if code, out, errs := cli(t, "routes", dir); code != 0 || !strings.Contains(out, "(slug)") || !strings.Contains(out, "(path)") {
		t.Fatalf("routes: %d %s %s", code, out, errs)
	}
	for file, want := range map[string]string{
		"components/Revenue.html":     "brick:\n  lazy: visible\n",
		"content/blog/[slug].html":    "<h1>{{ .Page.Route.slug }}</h1>",
		"content/docs/[...path].html": "/docs/<path>/",
	} {
		if b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(file))); err != nil || !strings.Contains(string(b), want) {
			t.Errorf("%s: %v, want %q in:\n%s", file, err, want, b)
		}
	}
	b, err := os.ReadFile(filepath.Join(dir, "components", "Counter.html"))
	if err != nil || !strings.Contains(string(b), `brick:click="increment"`) {
		t.Fatalf("Counter: %v\n%s", err, b)
	}
	if code, _, errs := cli(t, "new", "component", "Card", "-dir", dir); code != exitFail || !strings.Contains(errs, "never overwrites") {
		t.Fatalf("overwrite: %d %s", code, errs)
	}
	for _, args := range [][]string{
		{"component", "card"},
		{"component", "x-card"},
		{"component"},
		{"widget", "X"},
		{"page", "posts/[Slug]"},
		{"page", "posts/[slug]]"},
		{"page", "posts/[[slug]]"},
		{"page", "[...all]/x"},
		{"page", "[a]/[b]"},
		{"page", "x", "-lazy"},
		{"page", "Blog/x"},
		{"page", "x", "-state"},
	} {
		if code, _, _ := cli(t, append([]string{"new"}, args...)...); code != exitUsage {
			t.Errorf("new %v: exit %d, want usage", args, code)
		}
	}
}

// brick new site writes a project that brick check passes, brick fmt
// leaves alone and brick build renders with the kit; it refuses a folder
// that holds anything.
func TestNewSite(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "my-blog")
	code, out, errs := cli(t, "new", "site", dir)
	if code != exitOK || !strings.Contains(out, "content/index.html") || !strings.Contains(out, "brick serve") {
		t.Fatalf("new site: %d %s %s", code, out, errs)
	}
	if code, out, errs := cli(t, "check", dir); code != exitOK || !strings.Contains(out, "ok: 1 pages") {
		t.Fatalf("check: %d %s %s", code, out, errs)
	}
	if code, out, errs := cli(t, "fmt", "-l", dir); code != exitOK || out != "" {
		t.Fatalf("the starter is not brick fmt clean: %d %q %s", code, out, errs)
	}
	outDir := filepath.Join(t.TempDir(), "public")
	if code, out, errs := cli(t, "build", "-o", outDir, dir); code != exitOK {
		t.Fatalf("build: %d %s %s", code, out, errs)
	}
	b, err := os.ReadFile(filepath.Join(outDir, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"<title>Home · My blog</title>", `class="bk-card`, "Read the guide", "/_brick/c/Card."} {
		if !strings.Contains(string(b), want) {
			t.Errorf("the starter page lacks %q:\n%s", want, b)
		}
	}
	if code, _, errs := cli(t, "new", "site", dir); code != exitFail || !strings.Contains(errs, "not empty") {
		t.Fatalf("non-empty folder: %d %s", code, errs)
	}
	// An empty folder is fine; a stray file makes it someone's work.
	empty := t.TempDir()
	if code, _, errs := cli(t, "new", "site", empty); code != exitOK {
		t.Fatalf("empty folder: %d %s", code, errs)
	}
	stray := project(t, "notes.txt", "mine")
	if code, _, _ := cli(t, "new", "site", stray); code != exitFail {
		t.Fatalf("folder with a file: %d", code)
	}
	if b, _ := os.ReadFile(filepath.Join(stray, "notes.txt")); string(b) != "mine" {
		t.Fatal("new site touched an existing file")
	}
	for _, args := range [][]string{{"site"}, {"site", "a", "b"}, {"site", "-state", "x"}} {
		if code, _, _ := cli(t, append([]string{"new"}, args...)...); code != exitUsage {
			t.Errorf("new %v: exit %d, want usage", args, code)
		}
	}
}
