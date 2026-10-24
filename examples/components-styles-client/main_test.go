package main

import (
	"bytes"
	"context"
	"os"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/ubgo/brick"
)

// wantPage is the exact page; docs/guides/components/styles-and-client.md
// shows it. The runtime's file name and integrity change whenever the
// runtime does, so runtimeRef masks them on both sides; every component
// asset is compared byte for byte.
const wantPage = `<!doctype html>
<html><body>
<div data-brick-s="f2d1f4eb" class="grid"><article data-brick-s="5093e86a" class="card"><h3 class="title">Scoped</h3>Only this title is crimson.</article></div>
<span data-brick-s="a7055d23" class="badge">new</span>
<details data-brick="b9a47377c" data-brick-type="Accordion" data-brick-props="{&#34;open&#34;:false}" class="accordion" open><summary>More</summary>Shown without JavaScript.</details>
<button data-brick="bf35f94d6" data-brick-type="Opener" data-brick-props="{}" data-brick-mount="idle" class="opener" data-target="help">Help</button>
<dialog data-brick="b33f53269" data-brick-type="Modal" data-brick-props="{}" id="help">Help text.</dialog>
<brick-assets><script type="module" src="/_brick/c/runtime.0b4d3d08.js" integrity="sha384-1ojXDjndJ49gLs9LzFJ7HhqO9T6yLwLO3U8Y+zouOx6APv8CGhC1ifU41Z4HS9sK" nonce="r4nd0m"></script><script type="module" src="/_brick/c/Accordion.74b03d84.js" integrity="sha384-33wTT/AOjxrVSfXo3vkHCtpI+8KPOIMtAfZkkAI050ZQKv5hx/zmaJ6Jd8bo/Tx5" nonce="r4nd0m"></script><link rel="stylesheet" href="/_brick/c/Badge.556dd832.css" integrity="sha384-OAvP8Lt5XYxXEHuenmbELbQsadvenOdtaPb0psdDqZ1joTC6ufS5ldzaJF3rMXiA" nonce="r4nd0m"><link rel="stylesheet" href="/_brick/c/Card.5fb74083.css" integrity="sha384-KfR3uvGlnAMXFgLQpnQWN29p4vStT9KPfSFHEN78NM2SfMoCPLmHyNdF7n1AlyFZ" nonce="r4nd0m"><link rel="stylesheet" href="/_brick/c/Grid.b65e54ca.css" integrity="sha384-PgSzR9dz71C9EzvAhjKVVkF53Rms1BlFG8362zInXdQ0VsT1RchY86TqPcuahg/f" nonce="r4nd0m"><script type="module" src="/_brick/c/Modal.7604cf59.js" integrity="sha384-ROvYV8zhf2JHSOCvsmxa+ULz8EoUPUlqIIIiUciY1eACFMI8PcG9BghQnaxVYRxS" nonce="r4nd0m"></script><script type="module" src="/_brick/c/Opener.59907819.js" integrity="sha384-/q1TaB6EL65Cu5ZJWfG+ntdFVJlmPkntdEoBozZ1X3hszW7P1uvm7Gce4F0Jyz+/" nonce="r4nd0m"></script></brick-assets>
</body></html>
`

// wantAssets are the component files {{ assets }} links, exactly.
var wantAssets = map[string]string{
	"c/Accordion.74b03d84.js": `
brick.mount("Accordion", (el, props) => {
  const toggle = () => el.toggleAttribute("open")
  if (!props.open) el.removeAttribute("open")
  el.addEventListener("toggle", toggle)
  return () => el.removeEventListener("toggle", toggle)
})
`,
	"c/Badge.556dd832.css": `@scope ([data-brick-s="a7055d23"]) to ([data-brick-s]) { :scope { padding: 0 .25rem; border-radius: 4px } }`,
	"c/Card.5fb74083.css":  `@scope ([data-brick-s="5093e86a"]) to ([data-brick-s]) { .title { color: crimson } }`,
	"c/Grid.b65e54ca.css":  `@scope ([data-brick-s="f2d1f4eb"]) to ([data-brick-s] > *) { :scope { display: grid } .card { margin-block: 1rem } }`,
	"c/Modal.7604cf59.js": `
export function openModal(id) {
  document.getElementById(id)?.showModal()
}
`,
	"c/Opener.59907819.js": `
import { openModal } from "./Modal.7604cf59.js"
brick.mount("Opener", (el) => {
  const click = () => openModal(el.dataset.target)
  el.addEventListener("click", click)
  return () => el.removeEventListener("click", click)
})
`,
}

var (
	runtimeRef = regexp.MustCompile(`runtime\.[0-9a-f]+\.js" integrity="sha384-[^"]+"`)
	// runtimeHead is the runtime's heading line in the asset listing.
	runtimeHead = regexp.MustCompile(`== c/runtime\.[0-9a-f]+\.js`)
)

func maskRuntime(s string) string {
	s = runtimeHead.ReplaceAllString(s, "== c/runtime.HASH.js")
	return runtimeRef.ReplaceAllString(s, `runtime.HASH.js" integrity="sha384-HASH"`)
}

func TestPageAndAssets(t *testing.T) {
	eng, err := newEngine("theme")
	if err != nil {
		t.Fatal(err)
	}
	got, err := renderPage(context.Background(), eng)
	if err != nil {
		t.Fatal(err)
	}
	if maskRuntime(got) != maskRuntime(wantPage) {
		t.Errorf("page:\n got %q\nwant %q", got, wantPage)
	}
	files, _, err := assetFiles(eng)
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range wantAssets {
		if files[name] != want {
			t.Errorf("%s:\n got %q\nwant %q", name, files[name], want)
		}
	}
	if !strings.Contains(got, `<link rel="stylesheet" href="/_brick/c/Card.`) || strings.Count(got, "Card.") != 1 {
		t.Errorf("Card's stylesheet should be linked once:\n%s", got)
	}
}

// The style traps the guide's Pitfalls list names, with the words users see.
func TestStylePitfalls(t *testing.T) {
	fsys := fstest.MapFS{
		"components/Two.html":   {Data: []byte("<brick:style scoped>\n.a{color:red}\n</brick:style>\n<p>a</p><p>b</p>\n")},
		"components/Bad.html":   {Data: []byte("<brick:style scoped=\"deep\">\n.a{color:red}\n</brick:style>\n<p>a</p>\n")},
		"components/NoCss.html": {Data: []byte("---\nbrick: {css: scoped}\n---\n<p>a</p>\n")},
		"content/a.html":        {Data: []byte("<Two />")},
		"content/b.html":        {Data: []byte("<Bad />")},
		"content/c.html":        {Data: []byte("<NoCss />")},
	}
	eng, err := brick.New(brick.Options{FS: fsys})
	if err != nil {
		t.Fatal(err)
	}
	rep, err := eng.CompileAll(context.Background(), "content")
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for _, d := range rep.Diagnostics {
		lines = append(lines, d.String())
	}
	got := strings.Join(lines, "\n")
	for _, w := range []string{
		`components/Bad.html:1: error: <brick:style scoped="deep">: scoped is bare, "exclusive" or "inclusive"`,
		`components/NoCss.html:1: warning: brick: css: scoped is set, but there is no ~/components/NoCss.css next to this component; nothing ships`,
		`components/Two.html:1: error: a component with <brick:style scoped>, brick: {css: scoped} or <brick:client> must have exactly one top-level element, so the engine has one place for its marker`,
	} {
		if !strings.Contains(got, w) {
			t.Errorf("diagnostics lack %q:\n%s", w, got)
		}
	}

	themed, err := newEngine("theme")
	if err != nil {
		t.Fatal(err)
	}
	res, err := themed.RenderChunks(context.Background(), "content/x.html", nil, brick.WithSource([]byte("<Badge>x</Badge>")))
	if err != nil {
		t.Fatal(err)
	}
	const noAssets = "content/x.html: warning: a component on this page has <brick:client> or <brick:style>, but no {{ assets }} was rendered, so its JS and CSS never load"
	if len(res.Warnings) != 1 || res.Warnings[0].String() != noAssets {
		t.Errorf("warnings: %v", res.Warnings)
	}
}

// TestOutput compares what `go run .` prints with testdata/output.txt,
// the text the guide's "Try it" section shows.
func TestOutput(t *testing.T) {
	want, err := os.ReadFile("testdata/output.txt")
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := run(context.Background(), &out, "theme"); err != nil {
		t.Fatal(err)
	}
	if maskRuntime(out.String()) != maskRuntime(string(want)) {
		t.Errorf("output:\n%s\nwant:\n%s", out.String(), want)
	}
}
