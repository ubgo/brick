package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/ubgo/brick"
)

func project(t *testing.T, files ...string) string {
	t.Helper()
	dir := t.TempDir()
	for i := 0; i+1 < len(files); i += 2 {
		p := filepath.Join(dir, filepath.FromSlash(files[i]))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(files[i+1]), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func cli(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := run(context.Background(), args, &out, &errb)
	return code, out.String(), errb.String()
}

func TestCLI(t *testing.T) {
	dir := project(t,
		"brick.yaml", "roots: [components]\n",
		"layouts/default.html", "<main>{{ children }}</main>",
		"components/Card.html", "<b>{{ .Props.t }}</b>",
		"content/index.html", "<Card t=\"home\" />",
		"content/post.md", "---\ntitle: Post\n---\n# Hi *there*",
		"static/robots.txt", "ok",
	)
	if code, out, _ := cli(t, "check", dir); code != 0 || !strings.Contains(out, "ok: 2 pages") {
		t.Fatalf("check: %d %s", code, out)
	}
	code, out, _ := cli(t, "routes", "-json", dir)
	var rs []map[string]any
	if code != 0 || json.Unmarshal([]byte(out), &rs) != nil || len(rs) != 2 {
		t.Fatalf("routes: %d %s", code, out)
	}
	outDir := filepath.Join(t.TempDir(), "public")
	if code, out, errs := cli(t, "build", "-o", outDir, dir); code != 0 || !strings.Contains(out, "wrote 2 pages, 1 static files") {
		t.Fatalf("build: %d %s %s", code, out, errs)
	}
	b, err := os.ReadFile(filepath.Join(outDir, "post", "index.html"))
	if err != nil || !strings.Contains(string(b), "<h1>Hi <em>there</em></h1>") {
		t.Fatalf("markdown page: %s %v", b, err)
	}
}

func TestCLIFailures(t *testing.T) {
	broken := project(t, "content/index.html", "<Crad />")
	code, out, _ := cli(t, "check", broken)
	if code != 1 || !strings.Contains(out, "unknown component <Crad>") {
		t.Fatalf("check broken: %d %s", code, out)
	}
	code, out, _ = cli(t, "check", "-json", broken)
	if code != 1 || !strings.Contains(out, `"code": "unknown-tag"`) {
		t.Fatalf("check json: %d %s", code, out)
	}
	if code, _, _ := cli(t); code != 2 {
		t.Fatalf("no args: %d", code)
	}
	if code, _, _ := cli(t, "frobnicate"); code != 2 {
		t.Fatalf("unknown command: %d", code)
	}
	if code, out, _ := cli(t, "-h"); code != 0 || !strings.Contains(out, "Examples:") {
		t.Fatalf("help: %d %s", code, out)
	}
	if code, _, errs := cli(t, "check", "/definitely/not/here"); code != 1 || errs == "" {
		t.Fatalf("missing dir: %d %s", code, errs)
	}
}

func TestCLITailwind(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake binary is a shell script")
	}
	dir := project(t,
		"brick.yaml", "tailwind:\n  input: styles/app.css\n  output: static/app.css\n  binary: ./bin/tw\n",
		"bin/tw", "#!/bin/sh\nwhile [ $# -gt 0 ]; do case \"$1\" in -o) out=\"$2\"; shift;; esac; shift; done\necho '.p-4{padding:1rem}' > \"$out\"\n",
		"styles/app.css", `@import "tailwindcss";`,
		"content/index.html", `<p class="p-4">hi</p>`,
		"static/.keep", "",
	)
	if err := os.Chmod(filepath.Join(dir, "bin/tw"), 0o755); err != nil {
		t.Fatal(err)
	}
	outDir := filepath.Join(t.TempDir(), "public")
	code, out, errs := cli(t, "build", "-o", outDir, dir)
	if code != 0 || !strings.Contains(out, "tailwind: rebuilt static/app.css") {
		t.Fatalf("build: %d %s %s", code, out, errs)
	}
	css, err := os.ReadFile(filepath.Join(outDir, "app.css"))
	if err != nil || !strings.Contains(string(css), ".p-4{padding:1rem}") {
		t.Fatalf("exported css: %s %v", css, err)
	}
	if _, out, _ := cli(t, "build", "-o", outDir, dir); strings.Contains(out, "tailwind: rebuilt") {
		t.Fatal("rebuilt with no class change")
	}
}

func TestCLIVendorCheck(t *testing.T) {
	clean := project(t, "content/index.html", "<p>no remote imports</p>")
	if code, out, _ := cli(t, "vendor", "-check", clean); code != 0 || !strings.Contains(out, "matches") {
		t.Fatalf("clean: %d %s", code, out)
	}
	needs := project(t, "content/index.html", "---\nuse:\n  Card: github.com/acme/kit/Card.html@v1\n---\n<Card />")
	if code, _, errs := cli(t, "vendor", "-check", needs); code != 1 || !strings.Contains(errs, "not vendored") {
		t.Fatalf("unvendored: %d %s", code, errs)
	}
}

// build -base writes an export for a site hosted under a path: its links
// carry the base, the page files stay where the server maps them.
func TestCLIBuildBase(t *testing.T) {
	dir := project(t,
		"layouts/default.html", `<html><head></head><body><a href="/post/">post</a>{{ children }}</body></html>`,
		"content/index.html", "home",
		"content/post.html", "post",
	)
	outDir := filepath.Join(t.TempDir(), "public")
	if code, out, errs := cli(t, "build", "-base", "/docs", "-o", outDir, dir); code != 0 {
		t.Fatalf("build: %d %s %s", code, out, errs)
	}
	b, err := os.ReadFile(filepath.Join(outDir, "index.html"))
	if err != nil || !strings.Contains(string(b), `<a href="/docs/post/">post</a>`) || !strings.Contains(string(b), `<meta name="brick-base" content="/docs">`) {
		t.Fatalf("index: %s %v", b, err)
	}
	if code, _, errs := cli(t, "build", "-base", "docs", "-o", outDir, dir); code == 0 || !strings.Contains(errs, "BasePath") {
		t.Fatalf("a base without a leading slash: %d %s", code, errs)
	}
}

// build -keep leaves the named root-absolute prefixes out of the -base
// rewrite: a link to a sibling site stays as written, the site's own links
// still get the base.
func TestCLIBuildKeep(t *testing.T) {
	dir := project(t,
		"layouts/default.html", `<html><head></head><body><a href="/post/">post</a><a href="/engine/x/">sibling</a><a href="/api/y/">api</a>{{ children }}</body></html>`,
		"content/index.html", "home",
		"content/post.html", "post",
	)
	outDir := filepath.Join(t.TempDir(), "public")
	if code, out, errs := cli(t, "build", "-base", "/docs", "-keep", "/engine/, /api/", "-o", outDir, dir); code != 0 {
		t.Fatalf("build: %d %s %s", code, out, errs)
	}
	b, err := os.ReadFile(filepath.Join(outDir, "index.html"))
	for _, want := range []string{`<a href="/docs/post/">post</a>`, `<a href="/engine/x/">sibling</a>`, `<a href="/api/y/">api</a>`} {
		if err != nil || !strings.Contains(string(b), want) {
			t.Fatalf("index lacks %s: %s %v", want, b, err)
		}
	}
	// Without -keep the same link is rebased (BV: ignoring the flag fails above).
	out2 := filepath.Join(t.TempDir(), "public")
	if code, _, errs := cli(t, "build", "-base", "/docs", "-o", out2, dir); code != 0 {
		t.Fatalf("build: %d %s", code, errs)
	}
	if b, _ := os.ReadFile(filepath.Join(out2, "index.html")); !strings.Contains(string(b), `href="/docs/engine/x/"`) {
		t.Fatalf("without -keep: %s", b)
	}
	if code, _, _ := cli(t, "check", "-keep", "/x/", dir); code != exitUsage {
		t.Fatalf("check -keep: exit %d, want a usage error", code)
	}
}

// The CLI offers the kit: a brick.yaml naming brick-kit makes every kit
// component usable by name, with its design tokens linked, and no copy or
// vendoring.
func TestCLIKitLibrary(t *testing.T) {
	dir := project(t,
		"brick.yaml", "libraries: [brick-kit]\n",
		"layouts/default.html", `<html><head></head><body>{{ assets }}{{ children }}</body></html>`,
		"content/index.html", `<Button variant="outline">Hi</Button>`,
	)
	outDir := filepath.Join(t.TempDir(), "public")
	if code, out, errs := cli(t, "build", "-o", outDir, dir); code != 0 {
		t.Fatalf("build: %d %s %s", code, out, errs)
	}
	b, err := os.ReadFile(filepath.Join(outDir, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	page := string(b)
	for _, want := range []string{`class="bk-btn bk-btn--outline"`, ">Hi</button>", "/_brick/c/Button.", "/_brick/c/Theme."} {
		if !strings.Contains(page, want) {
			t.Errorf("the kit page lacks %q:\n%s", want, page)
		}
	}
	if code, _, errs := cli(t, "check", project(t, "brick.yaml", "libraries: [not-offered]\n", "content/index.html", "x")); code == 0 || !strings.Contains(errs, "brick-kit") {
		t.Errorf("an unknown library name must list what the CLI offers: %d %s", code, errs)
	}
}

// keygen prints one 32-byte base64 key on stdout and nothing else, so it
// pipes into a secret manager; the advice goes to stderr.
func TestKeygen(t *testing.T) {
	code, out, errs := cli(t, "keygen")
	k, err := base64.StdEncoding.DecodeString(strings.TrimSpace(out))
	if code != 0 || err != nil || len(k) != brick.StateKeySize || strings.Count(out, "\n") != 1 {
		t.Fatalf("keygen: %d %q %v", code, out, err)
	}
	if !strings.Contains(errs, secretEnv) {
		t.Errorf("no advice on stderr: %q", errs)
	}
	if _, again, _ := cli(t, "keygen"); again == out {
		t.Error("two keys were the same")
	}
	if code, _, _ := cli(t, "keygen", "extra"); code != exitUsage {
		t.Errorf("keygen with an argument: %d", code)
	}
}

// build seals a stateful page with BRICK_SECRET, and refuses without it:
// the export would carry a handle no server can open.
func TestBuildStatefulNeedsSecret(t *testing.T) {
	dir := project(t,
		"components/Count.html", "---\nstate:\n  n: 0\n---\n<b>{{ .State.n }}</b>",
		"content/index.html", "<Count />",
	)
	t.Setenv(secretEnv, "")
	code, _, errs := cli(t, "build", "-o", filepath.Join(t.TempDir(), "out"), dir)
	if code != exitFail || !strings.Contains(errs, secretEnv) {
		t.Fatalf("build without a key: %d %s", code, errs)
	}
	_, key, _ := cli(t, "keygen")
	t.Setenv(secretEnv, strings.TrimSpace(key))
	outDir := filepath.Join(t.TempDir(), "out")
	if code, out, errs := cli(t, "build", "-o", outDir, dir); code != exitOK {
		t.Fatalf("build with a key: %d %s %s", code, out, errs)
	}
	b, _ := os.ReadFile(filepath.Join(outDir, "index.html"))
	if !strings.Contains(string(b), "data-brick-state=") {
		t.Fatalf("no sealed state in the export: %s", b)
	}
	t.Setenv(secretEnv, "not base64!")
	if code, _, errs := cli(t, "build", "-o", filepath.Join(t.TempDir(), "o2"), dir); code != exitFail || !strings.Contains(errs, secretEnv) {
		t.Fatalf("bad key: %d %s", code, errs)
	}
}

// serve without BRICK_SECRET seals with a key made for the process, and
// says what that costs.
func TestServeKeyWithoutSecret(t *testing.T) {
	var errs bytes.Buffer
	keys, err := stateKeys("serve", func(string) (string, bool) { return "", false }, &errs)
	if err != nil || keys == nil || !strings.Contains(errs.String(), "warning") {
		t.Fatalf("serve: %v %v %q", keys, err, errs.String())
	}
	errs.Reset()
	if keys, err := stateKeys("check", func(string) (string, bool) { return "", false }, &errs); keys != nil || err != nil || errs.Len() != 0 {
		t.Fatalf("check: %v %v %q", keys, err, errs.String())
	}
}

// brick routes lists dynamic pages after the URLs, with their parameters
// .
func TestCLIRoutesPatterns(t *testing.T) {
	dir := project(t,
		"content/index.html", "home",
		"content/tags/[tag].html", "{{ .Page.Route.tag }}",
		"content/docs/[...path].html", "{{ .Page.Route.path }}",
	)
	code, out, _ := cli(t, "routes", dir)
	want := "/                 content/index.html\n/tags/[tag]/      content/tags/[tag].html  (tag)\n/docs/[...path]/  content/docs/[...path].html  (path)\n"
	if code != 0 || out != want {
		t.Fatalf("routes: %d\n%s", code, out)
	}
}

// brick.yaml host: lets the CLI check a site whose Go host grants script
// globals and an upload sink: declared names compile, a page that runs one
// is a host-only warning (exit 0), and an undeclared name is still an error.
func TestCLIHostDeclared(t *testing.T) {
	files := []string{
		"lib/clock.star", "def stamp():\n    return now()\n",
		"content/index.html", "---\nuse:\n  clock: ~/lib/clock.star\n---\n{{ clock.stamp }}",
		"content/ok.html", "<p>plain</p>",
		"components/Upload.html", "---\nstate:\n  avatar: null\nbrick:\n  uploads:\n    avatar: {accept: [image/png], max_size: 1MB}\n---\n<input type=\"file\" brick:upload=\"avatar\">",
		"content/up.html", "<Upload />",
	}
	declared := project(t, append([]string{"brick.yaml", "roots: [components]\nhost:\n  globals: [now]\n  uploads: true\n"}, files...)...)
	code, out, errs := cli(t, "check", declared)
	if code != 0 || !strings.Contains(out, "warning: not rendered by this check") || !strings.Contains(out, "lib/clock.star:2:") {
		t.Fatalf("declared: %d %s %s", code, out, errs)
	}
	if _, out, _ := cli(t, "check", "-json", declared); !strings.Contains(out, `"code": "host-only"`) {
		t.Fatalf("json code: %s", out)
	}
	undeclared := project(t, append([]string{"brick.yaml", "roots: [components]\nhost:\n  globals: [nowz]\n"}, files...)...)
	code, out, _ = cli(t, "check", undeclared)
	if code != 1 || !strings.Contains(out, "undefined: now") || !strings.Contains(out, "no UploadSink configured") {
		t.Fatalf("undeclared: %d %s", code, out)
	}
}

// brick.yaml markdown: {headingIDs: true} gives headings ids, so a layout's
// .Page.Headings TOC links; without it the output has no ids.
func TestBuildMarkdownHeadingIDs(t *testing.T) {
	files := []string{
		"layouts/default.html", `<nav>{{ range .Page.Headings }}{{ if .ID }}<a href="#{{ .ID }}">{{ .Text }}</a>{{ else }}<span>{{ .Text }}</span>{{ end }}{{ end }}</nav>{{ children }}`,
		"content/guide.md", "---\ntitle: Guide\n---\n## Install `brick`\n\n## Configure\n",
	}
	build := func(yaml string) string {
		t.Helper()
		dir := project(t, append([]string{"brick.yaml", yaml}, files...)...)
		out := filepath.Join(t.TempDir(), "public")
		if code, stdout, errs := cli(t, "build", "-o", out, dir); code != exitOK {
			t.Fatalf("build: %d %s %s", code, stdout, errs)
		}
		b, err := os.ReadFile(filepath.Join(out, "guide", "index.html"))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	on := build("markdown: {headingIDs: true}\n")
	if want := `<nav><a href="#install-brick">Install brick</a><a href="#configure">Configure</a></nav><h2 id="install-brick">Install <code>brick</code></h2>`; !strings.Contains(on, want) {
		t.Fatalf("headingIDs on:\n%s", on)
	}
	off := build("roots: [components]\n")
	if strings.Contains(off, "id=") || !strings.Contains(off, "<nav><span>Install brick</span><span>Configure</span></nav><h2>Install <code>brick</code></h2>") {
		t.Fatalf("headingIDs off:\n%s", off)
	}
}

// brick build registers the WebP encoder: a page that names "webp" gets
// .webp files in the output, and a page that does not is unchanged.
func TestBuildImageWebP(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 40, 20))
	for i := range src.Pix {
		src.Pix[i] = uint8(i)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, src); err != nil {
		t.Fatal(err)
	}
	dir := project(t,
		"static/p.png", buf.String(),
		"content/index.html", `<img src="{{ (image "~/static/p.png" 20 "webp").URL }}">`,
	)
	out := filepath.Join(t.TempDir(), "public")
	if code, stdout, errs := cli(t, "build", "-o", out, dir); code != exitOK {
		t.Fatalf("build: %d %s %s", code, stdout, errs)
	}
	page, err := os.ReadFile(filepath.Join(out, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`src="/(_brick/img/p\.[0-9a-f]+-20\.webp)"`).FindSubmatch(page)
	if m == nil {
		t.Fatalf("page: %s", page)
	}
	b, err := os.ReadFile(filepath.Join(out, string(m[1])))
	if err != nil {
		t.Fatal(err)
	}
	img, format, err := image.Decode(bytes.NewReader(b))
	if err != nil || format != "webp" || img.Bounds().Dx() != 20 || img.Bounds().Dy() != 10 {
		t.Fatalf("written copy: %v %s", err, format)
	}
}
