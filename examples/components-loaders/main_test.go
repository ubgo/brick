package main

import (
	"bytes"
	"context"
	scriptstarlark "github.com/ubgo/brick/contrib/script-starlark"
	"os"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/ubgo/brick"
)

// responses are the handler's exact answers, as `go run .` prints them;
// docs/guides/components/loaders.md shows these lines.
var responses = []struct{ path, want string }{
	{"/", `GET / -> 200 OK

<h1>Recent posts</h1>
<ul><li><a href="/posts/hello">Hello world</a></li><li><a href="/posts/again">Writing again</a></li></ul>
<ul><li><a href="/posts/hello">Hello world</a></li></ul>
<ul><li><a href="/pinned">Pinned</a></li></ul>
<p>Pro costs $12.</p>`},
	{"/team", `GET /team -> 200 OK
<ul><li>Grace</li><li>Ada</li><li>Grace</li><li>Linus</li></ul>`},
	{"/posts/hello", `GET /posts/hello -> 200 OK
Cache-Control: public, max-age=300
<h1>HELLO WORLD!</h1>
<p>Written by the team · READ ON!</p>`},
	{"/posts/old-hello", `GET /posts/old-hello -> 308 Permanent Redirect
Location: /posts/hello
<a href="/posts/hello">Permanent Redirect</a>.`},
	{"/posts/gone", `GET /posts/gone -> 410 Gone
This post was removed.`},
	{"/posts/nope", `GET /posts/nope -> 404 Not Found
404 page not found`},
}

func TestResponses(t *testing.T) {
	eng, err := newEngine("theme")
	if err != nil {
		t.Fatal(err)
	}
	h := handler(eng)
	for _, r := range responses {
		if got := fetch(h, r.path); got != r.want {
			t.Errorf("%s:\n got %q\nwant %q", r.path, got, r.want)
		}
	}
}

// The batch loader answers four <UserCard> calls with one call holding
// the three distinct inputs; the plain loader runs once per distinct input.
func TestLoaderCalls(t *testing.T) {
	eng, err := newEngine("theme")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	team, err := eng.RenderChunks(ctx, "content/team.html", nil)
	if err != nil {
		t.Fatal(err)
	}
	if s := team.Stats; s.BatchCalls != 1 || s.BatchItems != 3 || s.LoaderCalls != 0 {
		t.Errorf("team stats: %+v", s)
	}
	home, err := eng.RenderChunks(ctx, "content/index.html", nil)
	if err != nil {
		t.Fatal(err)
	}
	if s := home.Stats; s.LoaderCalls != 3 {
		t.Errorf("home loader calls: %d", s.LoaderCalls)
	}
}

// The script traps the guide's Pitfalls list names.
func TestScriptErrors(t *testing.T) {
	eng, err := newEngine("theme")
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"<brick:script>\ndef default(x):\n    return x\n</brick:script>\nx":                        `content/probe.html:1: <brick:script> function "default" shadows a built-in or an import (C12)`,
		"---\nload: bad\n---\n<brick:script>\ndef bad(input):\n    return [1]\n</brick:script>\nx": `content/probe.html: load: bad returned []interface {}; a loader must return a dict`,
		"---\nload: nope\n---\nx": `content/probe.html:1: load: nope: this file has no <brick:script> defining it`,
		"<brick:script>\ndef pad(n):\n    return \"%02d\" % n\n</brick:script>\n{{ pad 3 }}": `unknown conversion %0`,
	}
	for src, msg := range cases {
		_, err := eng.Render(context.Background(), "content/probe.html", nil, brick.WithSource([]byte(src)))
		if err == nil || !strings.Contains(err.Error(), msg) {
			t.Errorf("%q:\n got %v\nwant %s", src, err, msg)
		}
	}
}

// tryPaths are the paths the guide's "Try it" command passes.
var tryPaths = []string{"/", "/team", "/posts/hello", "/posts/old-hello", "/posts/gone", "/posts/nope"}

// TestOutput compares what `go run . <tryPaths>` prints with
// testdata/output.txt, the text the guide's "Try it" section shows.
func TestOutput(t *testing.T) {
	want, err := os.ReadFile("testdata/output.txt")
	if err != nil {
		t.Fatal(err)
	}
	eng, err := newEngine("theme")
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := run(&out, eng, tryPaths); err != nil {
		t.Fatal(err)
	}
	if out.String() != string(want) {
		t.Errorf("output:\n%s\nwant:\n%s", out.String(), want)
	}
}

// A loaded data file binds one name, data; asking for any other name is
// an error naming it and the file.
func TestDataFileBindsData(t *testing.T) {
	fsys := fstest.MapFS{
		"data/posts.yaml":     {Data: []byte("- {title: x}\n")},
		"lib/bad.star":        {Data: []byte("load(\"~/data/posts.yaml\", \"posts\")\ndef f(input):\n    return {}\n")},
		"components/Bad.html": {Data: []byte("---\nuse: {bad: ~/lib/bad.star}\nload: bad.f\n---\nx")},
		"content/a.html":      {Data: []byte("<Bad />")},
	}
	sc, err := scriptstarlark.New(scriptstarlark.Options{})
	if err != nil {
		t.Fatal(err)
	}
	eng, err := brick.New(brick.Options{FS: fsys, Scripting: sc})
	if err != nil {
		t.Fatal(err)
	}
	_, err = eng.Render(context.Background(), "content/a.html", nil)
	if err == nil || !strings.Contains(err.Error(), "load: name posts not found in module ~/data/posts.yaml") {
		t.Errorf("got %v", err)
	}
}
