package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// fmtLong is a page whose one line is long enough to wrap and whose tag
// has its attributes out of the order the tests ask for.
const fmtLong = "<a href=\"/x\" class=\"btn btn-primary\" id=\"go\">Go</a>\n"

// TestFmtConfigOptions: brick.yaml's fmt: section reaches brick fmt, and
// the flags override it for one run.
func TestFmtConfigOptions(t *testing.T) {
	dir := project(t,
		"brick.yaml", "fmt:\n  lineLength: 20\n  attrOrder: [id]\n",
		"content/index.html", fmtLong,
	)
	page := filepath.Join(dir, "content", "index.html")
	want := "<a\n  id=\"go\"\n  href=\"/x\"\n  class=\"btn btn-primary\">Go</a>\n"
	if code, out, errs := cli(t, "fmt", page); code != 0 || out != want {
		t.Fatalf("brick.yaml options: %d\n%s\n%s", code, out, errs)
	}
	// The flags win: no wrapping, class first.
	want = "<a class=\"btn btn-primary\" href=\"/x\" id=\"go\">Go</a>\n"
	if code, out, errs := cli(t, "fmt", "-line-length", "0", "-attr-order", "class", page); code != 0 || out != want {
		t.Fatalf("flags: %d\n%s\n%s", code, out, errs)
	}
	// An empty -attr-order turns the file's order off.
	if code, out, errs := cli(t, "fmt", "-line-length", "0", "-attr-order", "", page); code != 0 || out != fmtLong {
		t.Fatalf("empty -attr-order: %d\n%s\n%s", code, out, errs)
	}
	// The language server's formatting reads the same section.
	got, err := formatDocument(page, []byte(fmtLong))
	if err != nil || string(got) != "<a\n  id=\"go\"\n  href=\"/x\"\n  class=\"btn btn-primary\">Go</a>\n" {
		t.Fatalf("formatDocument: %v\n%s", err, got)
	}
}

// TestFmtNoConfigUnchanged: without a fmt: section brick fmt writes the
// layout it always has (no wrapping, no reordering).
func TestFmtNoConfigUnchanged(t *testing.T) {
	dir := project(t, "brick.yaml", "roots: [components]\n", "content/index.html", fmtLong)
	if code, out, errs := cli(t, "fmt", filepath.Join(dir, "content", "index.html")); code != 0 || out != fmtLong {
		t.Fatalf("%d\n%s\n%s", code, out, errs)
	}
	if code, _, errs := cli(t, "fmt", "-line-length", "-2", dir); code != exitUsage {
		t.Fatalf("a negative -line-length: exit %d, %s", code, errs)
	}
}

// TestFmtExternalFormatter: a body formatter named in brick.yaml gets the
// body on stdin and its output is laid under the tag; a failing command
// fails the file, names the command, and leaves the file as it was.
func TestFmtExternalFormatter(t *testing.T) {
	tr, err := exec.LookPath("tr")
	if err != nil {
		t.Skip("needs tr on PATH")
	}
	comp := "<div>\n<brick:style>\n      .a{color:red}\n</brick:style>\n</div>\n"
	dir := project(t,
		"brick.yaml", "roots: [components]\nfmt:\n  formatters:\n    brick:style: "+tr+" a-z A-Z\n",
		"components/Card.html", comp,
	)
	file := filepath.Join(dir, "components", "Card.html")
	want := "<div>\n  <brick:style>\n    .A{COLOR:RED}\n  </brick:style>\n</div>\n"
	if code, out, errs := cli(t, "fmt", file); code != 0 || out != want {
		t.Fatalf("formatter: %d\n%s\n%s", code, out, errs)
	}

	bad := project(t,
		"brick.yaml", "roots: [components]\nfmt:\n  formatters:\n    brick:style: "+tr+" --no-such-flag\n",
		"components/Card.html", comp,
	)
	file = filepath.Join(bad, "components", "Card.html")
	code, _, errs := cli(t, "fmt", "-w", file)
	if code != exitFail || !strings.Contains(errs, "brick:style") || !strings.Contains(errs, "--no-such-flag") {
		t.Fatalf("failing formatter: exit %d, %s", code, errs)
	}
	if b, _ := os.ReadFile(file); string(b) != comp {
		t.Fatalf("a failing formatter changed the file:\n%s", b)
	}

	unknown := project(t, "brick.yaml", "fmt:\n  formatters:\n    script: cat\n", "a.html", "<p>x</p>\n")
	if code, _, errs := cli(t, "fmt", filepath.Join(unknown, "a.html")); code != exitFail || !strings.Contains(errs, "brick:client") {
		t.Fatalf("a formatter keyed by a non-engine tag: exit %d, %s", code, errs)
	}
}

// TestFmtConfigInline: brick.yaml's fmt.inline reaches brick fmt, and
// -inline overrides it.
func TestFmtConfigInline(t *testing.T) {
	src := "<p>\n  Read <em>\n  this</em>\n</p>\n"
	indented := "<p>\n  Read <em>\n    this</em>\n</p>\n"
	dir := project(t,
		"brick.yaml", "fmt:\n  inline: [em]\n",
		"content/index.html", src,
	)
	page := filepath.Join(dir, "content", "index.html")
	if code, out, errs := cli(t, "fmt", page); code != 0 || out != src {
		t.Fatalf("fmt.inline: %d\n%s\n%s", code, out, errs)
	}
	if code, out, errs := cli(t, "fmt", "-inline", "", page); code != 0 || out != indented {
		t.Fatalf("empty -inline: %d\n%s\n%s", code, out, errs)
	}
	if got, err := formatDocument(page, []byte(indented)); err != nil || string(got) != src {
		t.Fatalf("formatDocument: %v\n%s", err, got)
	}
}
