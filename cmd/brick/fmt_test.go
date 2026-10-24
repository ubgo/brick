package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	fmtMessy = "<div>\n<p   class=\"a\">x</p>   \n</div>\n"
	fmtTidy  = "<div>\n  <p class=\"a\">x</p>\n</div>\n"
)

func TestFmtCLI(t *testing.T) {
	dir := project(t,
		"brick.yaml", "roots: [components]\n",
		"content/index.html", fmtMessy,
		"content/ok.html", fmtTidy,
		"brick_modules/x/components/Vendored.html", fmtMessy,
		".hidden/a.html", fmtMessy,
	)
	index := filepath.Join(dir, "content", "index.html")

	// Default: print the formatted file, change nothing.
	if code, out, errs := cli(t, "fmt", index); code != 0 || out != fmtTidy {
		t.Fatalf("print: %d %q %s", code, out, errs)
	}
	// -l lists only what differs, skipping vendored and hidden folders.
	code, out, _ := cli(t, "fmt", "-l", dir)
	if code != 0 || strings.TrimSpace(out) != index {
		t.Fatalf("-l: %d %q", code, out)
	}
	// -d prints a diff.
	if code, out, _ := cli(t, "fmt", "-d", index); code != 0 || !strings.Contains(out, "-<p   class=\"a\">x</p>   \n+  <p class=\"a\">x</p>\n") {
		t.Fatalf("-d: %d %s", code, out)
	}
	// -w writes, and a second run finds nothing to do.
	if code, out, _ := cli(t, "fmt", "-w", dir); code != 0 || out != "" {
		t.Fatalf("-w: %d %q", code, out)
	}
	if b, _ := os.ReadFile(index); string(b) != fmtTidy {
		t.Fatalf("written: %q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "brick_modules", "x", "components", "Vendored.html")); string(b) != fmtMessy {
		t.Fatalf("a vendored file was written: %q", b)
	}
	if code, out, _ := cli(t, "fmt", "-l", dir); code != 0 || out != "" {
		t.Fatalf("-l after -w: %d %q", code, out)
	}
	// Standard input.
	var stdout, stderr bytes.Buffer
	if code := fmtCmd([]string{"-"}, strings.NewReader(fmtMessy), &stdout, &stderr); code != 0 || stdout.String() != fmtTidy {
		t.Fatalf("stdin: %d %q %s", code, stdout.String(), stderr.String())
	}
	// Help.
	if code, _, errs := cli(t, "fmt", "-h"); code != 0 || !strings.Contains(errs, "Examples:") {
		t.Fatalf("-h: %d %s", code, errs)
	}
}

func TestFmtCLIFailures(t *testing.T) {
	dir := project(t,
		"content/bad.html", "<p>{{ if .A }</p>\n",
		"content/fm.html", "---\ntitle: x\n<p>a</p>\n",
		"content/good.html", fmtMessy,
	)
	code, _, errs := cli(t, "fmt", "-l", dir)
	if code != exitFail || !strings.Contains(errs, "bad.html: 1:") || !strings.Contains(errs, "fm.html") {
		t.Fatalf("parse errors: %d %s", code, errs)
	}
	if code, _, errs := cli(t, "fmt", filepath.Join(dir, "missing.html")); code != exitFail || errs == "" {
		t.Fatalf("missing file: %d %s", code, errs)
	}
	if code, _, _ := cli(t, "fmt", "-nope"); code != exitUsage {
		t.Fatalf("bad flag: %d", code)
	}
	// -v says why a file's indentation was left alone.
	odd := project(t, "a.html", "<div>\n   <b>x</i>\n</div>\n")
	if code, _, errs := cli(t, "fmt", "-l", "-v", odd); code != 0 || !strings.Contains(errs, "indentation left as written: line 2") {
		t.Fatalf("-v: %d %s", code, errs)
	}
}

// TestFmtRawComponents: a component whose frontmatter says raw: true keeps
// its callers' bodies, by name, under a library prefix, as x-kebab and
// through a use: alias; the kit's raw components are known without a site.
func TestFmtRawComponents(t *testing.T) {
	body := "\n      keep   me\n"
	dir := project(t,
		"brick.yaml", "roots: [components]\n",
		"components/Snippet.html", "---\nraw: true\n---\n<pre>{{ children }}</pre>",
		"content/index.html", "---\nuse:\n  Code: ~/components/Snippet.html\n---\n<div>\n"+
			"<Snippet>"+body+"</Snippet>\n"+
			"<AcmeSnippet>"+body+"</AcmeSnippet>\n"+
			"<x-snippet>"+body+"</x-snippet>\n"+
			"<Code>"+body+"</Code>\n"+
			"<CodeBlock>"+body+"</CodeBlock>\n"+
			"</div>\n",
	)
	code, out, errs := cli(t, "fmt", filepath.Join(dir, "content", "index.html"))
	if code != 0 {
		t.Fatalf("%d %s", code, errs)
	}
	if n := strings.Count(out, body); n != 5 {
		t.Fatalf("raw bodies kept: %d of 5\n%s", n, out)
	}
	if !strings.Contains(out, "\n  <Snippet>") {
		t.Fatalf("the tags themselves still indent:\n%s", out)
	}
}
