package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// brickBin builds the brick command once per test run. cmd/brick is its own
// Go module, so the build runs inside it (-C) rather than importing it.
func brickBin(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "brick")
	out, err := exec.Command("go", "build", "-C", "../../cmd/brick", "-o", bin, ".").CombinedOutput()
	if err != nil {
		t.Fatalf("building brick: %v\n%s", err, out)
	}
	return bin
}

// copyTheme copies the example's theme into a temporary folder, because
// `brick fmt -w` rewrites files.
func copyTheme(t *testing.T) string {
	t.Helper()
	dst := t.TempDir()
	err := filepath.WalkDir("theme", func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel("theme", p)
		out := filepath.Join(dst, rel)
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return err
		}
		return os.WriteFile(out, b, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	return dst
}

// The hand-written Panel is listed, the diff shows what changes, -w
// settles it, and a second -l prints nothing.
func TestFormatGate(t *testing.T) {
	bin := brickBin(t)
	dir := copyTheme(t)
	got, err := unformatted(bin, dir)
	if err != nil {
		t.Fatal(err)
	}
	panel := filepath.Join(dir, "components/Panel.html")
	if !slices.Equal(got, []string{panel}) {
		t.Fatalf("unformatted: %v", got)
	}
	diff, err := formatDiff(bin, panel)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"-<section   class=\"panel\"  id=\"main\">", "+<section class=\"panel\" id=\"main\">", "+  <h2>{{ .Props.title }}</h2>", "+  <pre>"} {
		if !strings.Contains(diff, want) {
			t.Fatalf("diff lacks %q:\n%s", want, diff)
		}
	}
	if out, err := run(bin, "fmt", "-w", panel); err != nil || out != "" {
		t.Fatalf("fmt -w: %q %v", out, err)
	}
	if again, err := unformatted(bin, dir); err != nil || len(again) != 0 {
		t.Fatalf("after -w: %v %v", again, err)
	}
}

// The binary reports the build it is.
func TestBinaryVersion(t *testing.T) {
	v, err := binaryVersion(brickBin(t))
	if err != nil {
		t.Fatal(err)
	}
	if v.Version == "" || !strings.HasPrefix(v.Go, "go") {
		t.Fatalf("version: %+v", v)
	}
	t.Logf("version: %+v", v)
}

// A file brick cannot parse is an error from the gate, not a listing: the
// reason is brick's own message.
func TestSyntaxErrorIsAnError(t *testing.T) {
	bin := brickBin(t)
	dir := copyTheme(t)
	if err := os.WriteFile(filepath.Join(dir, "components/Bad.html"), []byte("<p>{{ .x </p>\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := unformatted(bin, dir)
	var ee *exec.ExitError
	if err == nil || !errors.As(err, &ee) || !strings.Contains(err.Error(), "unclosed {{") {
		t.Fatalf("want an exit error naming the syntax error, got %v", err)
	}
	t.Logf("error: %v", err)
}
