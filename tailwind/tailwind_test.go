package tailwind

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"testing/fstest"
)

func TestCandidatesAndStamp(t *testing.T) {
	t.Parallel()
	fsys := fstest.MapFS{
		"content/a.html":  {Data: []byte(`<div class="p-4 text-lg"><span class='font-bold p-4'>x</span></div>`)},
		"content/b.md":    {Data: []byte(`<b class="{{ .X }} mt-2">`)},
		"notes.txt":       {Data: []byte(`class="ignored"`)},
		".git/x.html":     {Data: []byte(`class="hidden-dir"`)},
		"components/C.go": {Data: []byte(`class="go"`)},
	}
	got, err := Candidates(fsys, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Template tokens are kept as they are; they only make the hash more
	// careful. Files outside the source extensions and dot-directories are
	// not read.
	if strings.Join(got, " ") != ".X font-bold mt-2 p-4 text-lg {{ }}" {
		t.Fatalf("candidates: %q", got)
	}
	if Stamp([]string{"a"}, []byte("x")) == Stamp([]string{"a"}, []byte("y")) || Stamp([]string{"a", "b"}, nil) == Stamp([]string{"ab"}, nil) {
		t.Fatal("stamp collisions")
	}
}

// A fake tailwindcss: writes its argument list as CSS, and counts runs.
const fakeBinary = `#!/bin/sh
out=""
while [ $# -gt 0 ]; do
  case "$1" in -o) out="$2"; shift;; esac
  shift
done
echo ".built{}" > "$out"
echo run >> runs.log
`

func TestBuildRunsOnlyWhenClassesChange(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("the fake binary is a shell script")
	}
	dir := t.TempDir()
	write := func(p, s string) {
		full := filepath.Join(dir, p)
		_ = os.MkdirAll(filepath.Dir(full), 0o755)
		if err := os.WriteFile(full, []byte(s), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write("bin/tailwindcss", fakeBinary)
	write("styles/app.css", `@import "tailwindcss";`)
	write("content/a.html", `<p class="p-4">one</p>`)
	opts := Options{Dir: dir, Input: "styles/app.css", Output: "static/app.css", Binary: filepath.Join(dir, "bin/tailwindcss")}
	write("static/.keep", "")
	runs := func() int {
		b, _ := os.ReadFile(filepath.Join(dir, "runs.log"))
		return strings.Count(string(b), "run")
	}
	ctx := context.Background()
	if ran, err := Build(ctx, opts); err != nil || !ran {
		t.Fatalf("first build: %v %v", ran, err)
	}
	css, _ := os.ReadFile(filepath.Join(dir, "static/app.css"))
	if !strings.HasPrefix(string(css), stampPrefix) || !strings.Contains(string(css), ".built{}") {
		t.Fatalf("output: %q", css)
	}
	write("content/a.html", `<p class="p-4">text changed, classes did not</p>`)
	if ran, err := Build(ctx, opts); err != nil || ran || runs() != 1 {
		t.Fatalf("unchanged classes: ran=%v err=%v runs=%d", ran, err, runs())
	}
	write("content/a.html", `<p class="p-8">new class</p>`)
	if ran, err := Build(ctx, opts); err != nil || !ran || runs() != 2 {
		t.Fatalf("new class: ran=%v err=%v runs=%d", ran, err, runs())
	}
	write("styles/app.css", `@import "tailwindcss"; @theme { --color-x: red; }`)
	if ran, _ := Build(ctx, opts); !ran {
		t.Fatal("an input change did not rebuild")
	}
	opts.Binary = "definitely-not-a-binary"
	write("content/a.html", `<p class="p-9">x</p>`)
	if _, err := Build(ctx, opts); !errors.Is(err, ErrNoBinary) {
		t.Fatalf("missing binary: %v", err)
	}
}
