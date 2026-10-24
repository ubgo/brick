package main

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/ubgo/brick"
)

// TestOutput compares what the program prints with testdata/output.txt,
// the text docs/guides/components/errors.md shows.
func TestOutput(t *testing.T) {
	want, err := os.ReadFile("testdata/output.txt")
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := run(context.Background(), &out, "theme"); err != nil {
		t.Fatal(err)
	}
	if out.String() != string(want) {
		t.Errorf("output:\n%s\nwant:\n%s", out.String(), want)
	}
}

// A contained error still reaches the hook, in Prod as in Dev, and the
// Prod fallback shows a generic message with an error id, never the error.
func TestBoundaryInProd(t *testing.T) {
	hook := &errorLog{}
	eng, err := newEngine("theme", brick.Prod, hook)
	if err != nil {
		t.Fatal(err)
	}
	html, err := eng.Render(context.Background(), page, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(html), "database") || !strings.Contains(string(html), "This part of the page could not be shown. (error ") {
		t.Errorf("prod page:\n%s", html)
	}
	if len(hook.lines) != 1 || !strings.Contains(hook.lines[0], "the posts database is down") {
		t.Errorf("hook heard %q", hook.lines)
	}
}

// The dynamic-component traps the guide's Pitfalls list names.
func TestRenderPitfalls(t *testing.T) {
	eng, err := newEngine("theme", brick.Prod, &errorLog{})
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		`<brick:render name="Gallery" />`:   `content/probe.html:1:1: <brick:render>: no component named "Gallery" (written in content/probe.html)`,
		"<brick:render :name=\"`../x`\" />": `content/probe.html:1:1: <brick:render>: name "../x": <brick:render> takes a component name, never a path`,
		`<brick:boundary><Crad /><brick:prop name="fallback">fb</brick:prop></brick:boundary>`: `content/probe.html:1:17: unknown component <Crad> — no file in use: or the roots (components) defines it`,
	}
	for src, msg := range cases {
		_, err := eng.Render(context.Background(), "content/probe.html", nil, brick.WithSource([]byte(src)))
		if err == nil || err.Error() != msg {
			t.Errorf("%s:\n got %v\nwant %s", src, err, msg)
		}
	}
	// An unknown name is a render-time failure, so a boundary contains it.
	got, err := eng.Render(context.Background(), "content/probe.html", nil, brick.WithSource([]byte(`<brick:boundary><brick:render name="Gallery" /><brick:prop name="fallback">fb</brick:prop></brick:boundary>`)))
	if err != nil || string(got) != "fb" {
		t.Errorf("contained: %q %v", got, err)
	}
}

// Recursion with no condition is a compile error, and a hook value that
// implements no hook method is refused by New.
func TestCompileTimeRefusals(t *testing.T) {
	fsys := fstest.MapFS{"components/Loop.html": {Data: []byte("<Loop />")}, "content/a.html": {Data: []byte("<Loop />")}}
	eng, err := brick.New(brick.Options{FS: fsys})
	if err != nil {
		t.Fatal(err)
	}
	_, err = eng.Render(context.Background(), "content/a.html", nil)
	const loop = "components/Loop.html:1:1: <Loop> renders itself outside every {{ if }}/{{ range }}, so it can never stop (C14); put the recursion under a condition"
	if err == nil || err.Error() != loop {
		t.Errorf("loop: %v", err)
	}
	type notAHook struct{}
	if _, err := brick.New(brick.Options{FS: fsys, Hooks: []any{notAHook{}}}); err == nil {
		t.Error("New accepted a hook with no hook method")
	}
}
