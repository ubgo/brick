package main

import (
	"context"
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ubgo/brick/tailwind"
	"github.com/ubgo/brick/watch"
)

// fakeTailwind stands in for the standalone binary: it writes a fixed rule
// to the -o file and counts its runs in ran.log, so a test can see whether
// Build ran it. A shell script, so the test needs a Unix shell.
const fakeTailwind = `#!/bin/sh
while [ $# -gt 0 ]; do case "$1" in -o) out="$2"; shift;; esac; shift; done
echo '.text-3xl{font-size:1.875rem}' > "$out"
echo run >> "$(dirname "$0")/ran.log"
`

// copyTheme copies the theme to a temp dir, so builds never write into the
// repository, and adds the fake binary as bin/tw.
func copyTheme(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	err := filepath.WalkDir(themeDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(themeDir, p)
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(dir, rel), 0o755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dir, rel), b, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bin/tw"), []byte(fakeTailwind), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func runs(t *testing.T, dir string) int {
	b, _ := os.ReadFile(filepath.Join(dir, "bin/ran.log"))
	return strings.Count(string(b), "run")
}

// The class candidates are what the stamp hashes.
func TestCandidates(t *testing.T) {
	got, err := tailwind.Candidates(os.DirFS(themeDir), nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"bg-slate-50", "font-bold", "px-8", "text-3xl", "text-slate-600", "text-slate-800"}
	if !slices.Equal(got, want) {
		t.Fatalf("candidates %q", got)
	}
}

// Build runs the binary once, skips it while the class set is unchanged,
// and runs it again when a class is added; OnChange wires that to the
// watcher.
func TestBuildOnlyWhenClassesChange(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake tailwindcss is a shell script")
	}
	ctx := context.Background()
	dir := copyTheme(t)
	tw := css(dir, "./bin/tw")
	if ran, err := tailwind.Build(ctx, tw); err != nil || !ran || runs(t, dir) != 1 {
		t.Fatalf("first build: ran=%v err=%v runs=%d", ran, err, runs(t, dir))
	}
	out, _ := os.ReadFile(filepath.Join(dir, "static/app.css"))
	if !strings.HasPrefix(string(out), "/* brick-tailwind: ") || !strings.Contains(string(out), ".text-3xl{font-size:1.875rem}") {
		t.Fatalf("app.css:\n%s", out)
	}
	// A text edit keeps the class set: no run.
	page := filepath.Join(dir, "content/index.html")
	b, _ := os.ReadFile(page)
	if err := os.WriteFile(page, []byte(strings.Replace(string(b), "Hello", "Hello there", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	if ran, err := tailwind.Build(ctx, tw); err != nil || ran || runs(t, dir) != 1 {
		t.Fatalf("text edit: ran=%v err=%v runs=%d", ran, err, runs(t, dir))
	}
	// A new class: the stamp changes and the binary runs.
	b, _ = os.ReadFile(page)
	if err := os.WriteFile(page, []byte(strings.Replace(string(b), `class="px-8 `, `class="px-8 italic `, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	if ran, err := tailwind.Build(ctx, tw); err != nil || !ran || runs(t, dir) != 2 {
		t.Fatalf("class edit: ran=%v err=%v runs=%d", ran, err, runs(t, dir))
	}
}

// In the dev server the watcher calls OnChange, so a class added while it
// runs rebuilds the stylesheet, which the site then serves from static/.
func TestRebuildOnChange(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake tailwindcss is a shell script")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dir := copyTheme(t)
	tw := css(dir, "./bin/tw")
	if _, err := tailwind.Build(ctx, tw); err != nil {
		t.Fatal(err)
	}
	s, err := newSite(ctx, dir, tw)
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = s.Watch(ctx, watch.Options{Interval: watchInterval}) }()
	// Let the watcher take its first snapshot before the edit.
	time.Sleep(3 * watchInterval)
	page := filepath.Join(dir, "content/index.html")
	b, _ := os.ReadFile(page)
	if err := os.WriteFile(page, []byte(strings.Replace(string(b), `class="px-8 `, `class="px-8 underline `, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(5 * time.Second); runs(t, dir) < 2; {
		if time.Now().After(deadline) {
			t.Fatalf("OnChange did not rebuild: runs=%d", runs(t, dir))
		}
		time.Sleep(watchInterval)
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/app.css", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), ".text-3xl") {
		t.Fatalf("/app.css: %d %s", rec.Code, rec.Body)
	}
}

// watchInterval is the test watcher's poll period: short, so the test is quick.
const watchInterval = 20 * time.Millisecond

// Without the binary, Build says where to get it.
func TestNoBinary(t *testing.T) {
	_, err := tailwind.Build(context.Background(), css(copyTheme(t), "./bin/missing"))
	if !errors.Is(err, tailwind.ErrNoBinary) {
		t.Fatalf("err = %v", err)
	}
}

// The real binary, when it is installed; skipped otherwise, with a reason.
func TestRealTailwind(t *testing.T) {
	if _, err := exec.LookPath(tailwind.DefaultBinary); err != nil {
		t.Skip("tailwindcss is not on PATH; install the standalone CLI (https://tailwindcss.com/blog/standalone-cli) to run this test")
	}
	dir := copyTheme(t)
	if ran, err := tailwind.Build(context.Background(), css(dir, "")); err != nil || !ran {
		t.Fatalf("ran=%v err=%v", ran, err)
	}
	out, _ := os.ReadFile(filepath.Join(dir, "static/app.css"))
	if !strings.Contains(string(out), "text-3xl") {
		t.Fatalf("app.css lacks text-3xl:\n%.400s", out)
	}
}
