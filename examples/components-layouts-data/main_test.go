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

// want is the exact HTML of each render; docs/guides/components/layouts-and-data.md
// shows these bytes.
var want = map[string]string{
	"content/index.html": `<!doctype html>
<html>
<head><title>Pricing · My Blog</title></head>
<body>
<a href="https://example.com/">My Blog</a>
<h1>Pricing</h1>
<div class="card card--gray">Free: $0</div><div class="card card--gray">Pro: $12</div>

</body>
</html>
`,
	"content/index.html as a fragment": `<h1>Pricing</h1>
<div class="card card--gray">Free: $0</div><div class="card card--gray">Pro: $12</div>
`,
	"content/blog/hello.html": `<!doctype html>
<html>
<head><title>Hello · My Blog</title></head>
<body>
<a href="https://example.com/">My Blog</a>
<article class="post"><h1>Hello</h1>
<div class="card card--blue">Team: $30</div>
</article>

</body>
</html>
`,
	"content/plans.html": `<ul><li>Free</li><li>Pro</li></ul>
`,
	"content/cart.html (en)": `<button>Add to cart</button>
<p>1 item / 3 items</p>
<p>Sunday 27 September</p>
`,
	"content/cart.html (fr)": `<button>Ajouter au panier</button>
<p>1 article / 3 articles</p>
<p>dimanche 27 septembre</p>
`,
	"content/about.html (en)": `<p>About us.</p>
`,
	"content/about.html (fr)": `<p>À propos.</p>
`,
}

func TestRenders(t *testing.T) {
	eng, err := newEngine("theme")
	if err != nil {
		t.Fatal(err)
	}
	got, err := renderAll(context.Background(), eng)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range renders {
		if got[r.name] != want[r.name] {
			t.Errorf("%s:\n got %q\nwant %q", r.name, got[r.name], want[r.name])
		}
	}
}

// A value the Go program provides reaches every component's .Ctx, and a
// provide: nearer the component (the post layout's) wins over it.
func TestProvideFromGo(t *testing.T) {
	eng, err := newEngine("theme")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	home, err := eng.Render(ctx, "content/index.html", nil, brick.Provide("accent", "green"), brick.WithLayout("none"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(home), `<div class="card card--green">Free: $0</div>`) {
		t.Errorf("home:\n%s", home)
	}
	post, err := eng.Render(ctx, "content/blog/hello.html", nil, brick.Provide("accent", "green"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(post), `<div class="card card--blue">Team: $30</div>`) {
		t.Errorf("post:\n%s", post)
	}
}

// The traps the guide's Pitfalls list names, with the words users see.
func TestPitfalls(t *testing.T) {
	eng, err := newEngine("theme")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	src := func(s string) brick.RenderOption { return brick.WithSource([]byte(s)) }

	res, err := eng.RenderChunks(ctx, "content/probe.html", nil, src("---\nlayout: none\n---\n{{ t \"cart.remove\" }}"))
	if err != nil {
		t.Fatal(err)
	}
	out, err := res.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "cart.remove" || len(res.Warnings) != 1 || res.Warnings[0].String() != `content/probe.html: warning: translation "cart.remove" is missing for en` {
		t.Errorf("missing key: %q %v", out, res.Warnings)
	}

	for s, msg := range map[string]string{
		"---\nlayout: blog\n---\nx":                                       `content/probe.html:1: layout "blog": ~/layouts/blog.html does not exist`,
		"---\nlayout: none\n---\n{{ .Env }}":                              `content/probe.html:4:4: at <.Env>: can't evaluate field Env in type *brick.Scope`,
		"---\nlayout: none\nstrictParams: true\n---\n{{ .Params.titel }}": `content/probe.html:5:11: at <.Params.titel>: map has no entry for key "titel"`,
	} {
		_, err := eng.Render(ctx, "content/probe.html", nil, src(s))
		if err == nil || err.Error() != msg {
			t.Errorf("%q:\n got %v\nwant %s", s, err, msg)
		}
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
	if out.String() != string(want) {
		t.Errorf("output:\n%s\nwant:\n%s", out.String(), want)
	}
}

// A layout chain that loops and a required inject: with no provider are
// both errors that name the path they followed.
func TestChainErrors(t *testing.T) {
	fsys := fstest.MapFS{
		"layouts/a.html":       {Data: []byte("---\nlayout: b\n---\n{{ children }}")},
		"layouts/b.html":       {Data: []byte("---\nlayout: a\n---\n{{ children }}")},
		"components/Need.html": {Data: []byte("---\ninject:\n  user: {required: true}\n---\n<p>{{ .Ctx.user }}</p>")},
		"content/x.html":       {Data: []byte("---\nlayout: a\n---\nx")},
		"content/y.html":       {Data: []byte("<Need />")},
	}
	eng, err := brick.New(brick.Options{FS: fsys})
	if err != nil {
		t.Fatal(err)
	}
	for page, msg := range map[string]string{
		"content/x.html": `content/x.html: layout cycle: content/x.html → layouts/a.html → layouts/b.html → layouts/a.html`,
		"content/y.html": `components/Need.html: <Need>: inject: "user" is required, but no ancestor provides it (searched page → Need)`,
	} {
		_, err := eng.Render(context.Background(), page, nil)
		if err == nil || err.Error() != msg {
			t.Errorf("%s:\n got %v\nwant %s", page, err, msg)
		}
	}
}
