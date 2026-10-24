package format

import (
	"errors"
	"strings"
	"testing"

	"github.com/ubgo/brick/internal/lex"
)

// rawCode marks <Code> as a raw-body component, as a callee with raw: true.
func rawCode(name string, _ []lex.Attr) bool { return name == "Code" }

func TestFormat(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"fences lose trailing spaces, YAML is untouched",
			"---  \ntitle: x\nbody: |\n  keep  \n---\t\n<h1>Hi</h1>\n",
			"---\ntitle: x\nbody: |\n  keep  \n---\n<h1>Hi</h1>\n"},
		{"nested elements indent by two spaces",
			"<div>\n<p>a</p>\n<ul>\n<li>x</li>\n    </ul>\n</div>\n",
			"<div>\n  <p>a</p>\n  <ul>\n    <li>x</li>\n  </ul>\n</div>\n"},
		{"component tags nest like elements; self-closing ones do not open",
			"<Card>\n<Icon name=\"x\" />\n<Stack>\n<b>a</b>\n</Stack>\n</Card>\n",
			"<Card>\n  <Icon name=\"x\" />\n  <Stack>\n    <b>a</b>\n  </Stack>\n</Card>\n"},
		{"tags opened on one line indent once",
			"<ul><li>\nx\n</li></ul>\n",
			"<ul><li>\n  x\n</li></ul>\n"},
		{"void elements do not open",
			"<div>\n<br>\n<input type=\"text\">\n<img src=\"a\"/>\nx\n</div>\n",
			"<div>\n  <br>\n  <input type=\"text\">\n  <img src=\"a\"/>\n  x\n</div>\n"},
		{"html, head and body stay flat",
			"<!doctype html>\n<html>\n<head>\n  <meta charset=\"utf-8\">\n</head>\n<body>\n<main>\nx\n</main>\n</body>\n</html>\n",
			"<!doctype html>\n<html>\n<head>\n<meta charset=\"utf-8\">\n</head>\n<body>\n<main>\n  x\n</main>\n</body>\n</html>\n"},
		{"pre keeps every line, including its closing tag",
			"<div>\n<pre>\n  a\n      b   \n</pre>\n</div>\n",
			"<div>\n  <pre>\n  a\n      b   \n</pre>\n</div>\n"},
		{"white-space: pre in a style keeps lines",
			"<div>\n<span style=\"white-space: pre-wrap\">a\n     b</span>\n</div>\n",
			"<div>\n  <span style=\"white-space: pre-wrap\">a\n     b</span>\n</div>\n"},
		{"textarea, script and style keep their content",
			"<div>\n<textarea>\n   x  \n</textarea>\n<script>\n    if (a) {\n  b()\n    }\n</script>\n<style>\n      p { }\n</style>\n</div>\n",
			"<div>\n  <textarea>\n   x  \n</textarea>\n  <script>\n    if (a) {\n  b()\n    }\n</script>\n  <style>\n      p { }\n</style>\n</div>\n"},
		{"a multi-line action is never touched",
			"<div>\n{{ $x := dict\n      \"a\" `raw\n   text   `\n   }}\n</div>\n",
			"<div>\n  {{ $x := dict\n      \"a\" `raw\n   text   `\n   }}\n</div>\n"},
		{"control actions do not indent; else and end sit with their if",
			"<ul>\n{{ range .Items }}\n<li>{{ . }}</li>\n{{ else }}\n<li>none</li>\n{{ end }}\n</ul>\n",
			"<ul>\n  {{ range .Items }}\n  <li>{{ . }}</li>\n  {{ else }}\n  <li>none</li>\n  {{ end }}\n</ul>\n"},
		{"a conditional opening different tags in each branch",
			"{{ if .A }}<a href=\"x\">{{ else }}<div>{{ end }}\n<b>in</b>\n{{ if .A }}</a>{{ else }}</div>{{ end }}\n",
			"{{ if .A }}<a href=\"x\">{{ else }}<div>{{ end }}\n  <b>in</b>\n{{ if .A }}</a>{{ else }}</div>{{ end }}\n"},
		{"optional end tags close at the parent",
			"<ul>\n<li>a\n<li>b\n</ul>\n<p>x\n",
			"<ul>\n  <li>a\n  <li>b\n</ul>\n<p>x\n"},
		{"attribute lines move with their tag",
			"<section>\n<Card\n      title=\"a\"\n      big>\nx\n</Card>\n</section>\n",
			"<section>\n  <Card\n        title=\"a\"\n        big>\n    x\n  </Card>\n</section>\n"},
		{"a quoted value spanning lines is kept",
			"<section>\n<div title=\"a\n   b\">x</div>\n</section>\n",
			"<section>\n  <div title=\"a\n   b\">x</div>\n</section>\n"},
		{"attribute spacing in plain and component tags",
			"<div  class = \"a  b\"\tid=\"x\"  >\n<Card  title=\"t\"   />\n</div >\n",
			"<div class=\"a  b\" id=\"x\">\n  <Card title=\"t\" />\n</div>\n"},
		{"spacing next to an action in a tag is the action's business",
			"<div {{ attrs }}   class=\"x\"  {{ if .A }}hidden{{ end }}>y</div>\n",
			"<div {{ attrs }}   class=\"x\"  {{ if .A }}hidden{{ end }}>y</div>\n"},
		{"trailing spaces go and blank runs become one blank line",
			"<div>   \n\n\n\n<p>a</p>\t\n</div>\n",
			"<div>\n\n  <p>a</p>\n</div>\n"},
		{"comments keep their lines",
			"<div>\n<!--\n      note   \n-->\n</div>\n",
			"<div>\n  <!--\n      note   \n-->\n</div>\n"},
		{"a raw component's body is kept",
			"<div>\n<Code>\n  x   \n      <b>\n</Code>\n</div>\n",
			"<div>\n  <Code>\n  x   \n      <b>\n</Code>\n</div>\n"},
		{"a raw brick:prop body is kept",
			"<Card>\n<brick:prop name=\"code\" raw>\n   <b>\n</brick:prop>\n</Card>\n",
			"<Card>\n  <brick:prop name=\"code\" raw>\n   <b>\n</brick:prop>\n</Card>\n"},
		{"a brick:prop body moves as a block with its tag",
			"<div>\n<Card>\n<brick:prop name=\"actions\">\n    <b>x</b>   \n  </brick:prop>\n</Card>\n</div>\n",
			"<div>\n  <Card>\n    <brick:prop name=\"actions\">\n        <b>x</b>   \n      </brick:prop>\n  </Card>\n</div>\n"},
		{"brick:script, brick:client and brick:style are raw",
			"<div>\n<brick:style>\n   .a {}\n</brick:style>\n</div>\n",
			"<div>\n  <brick:style>\n   .a {}\n</brick:style>\n</div>\n"},
		{"a no-format region is kept, the rest formats",
			"<div>\n{{/* brick:no-format */}}\n<section   a=\"1\">\n      x   \n{{/* brick:format */}}\n<p>y</p>\n</section>\n</div>\n",
			"<div>\n  {{/* brick:no-format */}}\n<section   a=\"1\">\n      x   \n{{/* brick:format */}}\n    <p>y</p>\n  </section>\n</div>\n"},
		{"a file that starts with no-format is kept whole",
			"---   \nt: 1\n---\n{{/* brick:no-format */}}\n<div>\n<p   a=\"1\">x</p>   \n</div>\n",
			"---   \nt: 1\n---\n{{/* brick:no-format */}}\n<div>\n<p   a=\"1\">x</p>   \n</div>\n"},
		{"tabs become spaces",
			"<div>\n\t<p>a</p>\n</div>\n",
			"<div>\n  <p>a</p>\n</div>\n"},
		{"no newline at the end stays so",
			"<div>\n<p>a</p>\n</div>",
			"<div>\n  <p>a</p>\n</div>"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res, err := Format([]byte(c.in), Options{IsRaw: rawCode})
			if err != nil {
				t.Fatal(err)
			}
			if got := string(res.Src); got != c.want {
				t.Fatalf("got:\n%s\nwant:\n%s\nindent skipped: %s", got, c.want, res.IndentSkipped)
			}
			again, err := Format(res.Src, Options{IsRaw: rawCode})
			if err != nil || string(again.Src) != c.want {
				t.Fatalf("not idempotent:\n%s", again.Src)
			}
		})
	}
}

// TestFormatUncertainStructure: when the tags cannot be followed, the
// indentation stays exactly as written and the reason names the line; the
// safe local rules still apply.
func TestFormatUncertainStructure(t *testing.T) {
	cases := []struct{ name, in, want, reason string }{
		{"a conditional that opens a tag in one branch only",
			"<div>\n{{ if .A }}<section>{{ end }}\n   x   \n{{ if .A }}</section>{{ end }}\n</div>\n",
			"<div>\n{{ if .A }}<section>{{ end }}\n   x\n{{ if .A }}</section>{{ end }}\n</div>\n",
			"line 2: the branches"},
		{"a closing tag that matches nothing open",
			"<div>\n      <b>x</i>\n</div>\n",
			"<div>\n      <b>x</i>\n</div>\n",
			"line 2: </i> does not match <b>"},
		{"an unclosed element",
			"<div>\n    <section>\n x",
			"<div>\n    <section>\n x",
			"<section> (line 2) is not closed"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res, err := Format([]byte(c.in), Options{})
			if err != nil {
				t.Fatal(err)
			}
			if string(res.Src) != c.want {
				t.Fatalf("got:\n%s\nwant:\n%s", res.Src, c.want)
			}
			if !strings.Contains(res.IndentSkipped, c.reason) {
				t.Fatalf("reason %q, want it to contain %q", res.IndentSkipped, c.reason)
			}
		})
	}
}

// TestFormatReasonLineCountsFrontmatter: positions are file lines.
func TestFormatReasonLineCountsFrontmatter(t *testing.T) {
	res, err := Format([]byte("---\na: 1\n---\n<div>\n<b>x</i>\n</div>\n"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(res.IndentSkipped, "line 5:") {
		t.Fatalf("reason %q, want line 5", res.IndentSkipped)
	}
}

func TestFormatErrors(t *testing.T) {
	_, err := Format([]byte("---\na: 1\n<div>\n"), Options{})
	if err == nil || !strings.Contains(err.Error(), "no closing ---") {
		t.Fatalf("unclosed frontmatter: %v", err)
	}
	_, err = Format([]byte("---\na: 1\n---\n<p>\n{{ if }</p>\n"), Options{})
	var fe *Error
	if !errors.As(err, &fe) || fe.Line != 5 {
		t.Fatalf("unclosed action: %v, want a file line 5 error", err)
	}
	if _, err := Format([]byte{0xff, 0xfe}, Options{}); !errors.Is(err, ErrInvalidUTF8) {
		t.Fatalf("invalid UTF-8: %v", err)
	}
}

func TestFormatIndentOption(t *testing.T) {
	res, err := Format([]byte("<div>\n<p>a</p>\n</div>\n"), Options{Indent: "\t"})
	if err != nil {
		t.Fatal(err)
	}
	if string(res.Src) != "<div>\n\t<p>a</p>\n</div>\n" {
		t.Fatalf("got %q", res.Src)
	}
}

func TestDiff(t *testing.T) {
	if d := Diff("a.html", []byte("x\n"), []byte("x\n")); d != nil {
		t.Fatalf("equal files: %s", d)
	}
	old := "a\nb\nc\nd\ne\nf\ng\nh\n"
	new := "a\nb\nc\nD\ne\nf\ng\nh\n"
	want := "--- a.html\n+++ a.html (formatted)\n@@ -1,7 +1,7 @@\n a\n b\n c\n-d\n+D\n e\n f\n g\n"
	if d := string(Diff("a.html", []byte(old), []byte(new))); d != want {
		t.Fatalf("got:\n%s\nwant:\n%s", d, want)
	}
	d := string(Diff("b.html", []byte("x"), []byte("y")))
	if !strings.Contains(d, "-x\n\\ No newline at end of file\n+y\n\\ No newline at end of file\n") {
		t.Fatalf("no final newline:\n%s", d)
	}
}
