package brick_test

import (
	"context"
	"fmt"
	"testing/fstest"

	"github.com/ubgo/brick"
)

// A page, a component and a layout from an in-memory project.
func Example() {
	fsys := fstest.MapFS{
		"components/Card.html": {Data: []byte(`<div class="card"><h3>{{ .Props.title }}</h3>{{ children }}</div>`)},
		"layouts/default.html": {Data: []byte(`<main>{{ children }}</main>`)},
		"content/about.html":   {Data: []byte("---\ntitle: About\n---\n<h1>{{ .Title }}</h1><Card title=\"Hi {{ .Props.user }}\"><p>body</p></Card>")},
	}
	eng, err := brick.New(brick.Options{FS: fsys})
	if err != nil {
		panic(err)
	}
	html, err := eng.Render(context.Background(), "content/about.html", map[string]any{"user": "Sara"})
	if err != nil {
		panic(err)
	}
	fmt.Println(string(html))
	// Output: <main><h1>About</h1><div class="card"><h3>Hi Sara</h3><p>body</p></div></main>
}

// A loop around a component: the body sees the loop variable (T2).
func ExampleEngine_Render_loop() {
	fsys := fstest.MapFS{
		"components/Item.html": {Data: []byte(`<li>{{ children }}</li>`)},
		"content/list.html":    {Data: []byte(`<ul>{{ range $i, $p := .Props.posts }}<Item>{{ $i }}: {{ $p }}</Item>{{ end }}</ul>`)},
	}
	eng, _ := brick.New(brick.Options{FS: fsys})
	html, _ := eng.Render(context.Background(), "content/list.html", map[string]any{"posts": []string{"a", "b"}})
	fmt.Println(string(html))
	// Output: <ul><li>0: a</li><li>1: b</li></ul>
}

// A page from bytes (a CMS), with no project folder at all.
func ExampleWithSource() {
	eng, _ := brick.New(brick.Options{})
	html, _ := eng.Render(context.Background(), "cms/page-7.html", map[string]any{"n": 3}, brick.WithSource([]byte(`<p>{{ .Props.n }} items</p>`)))
	fmt.Println(string(html))
	// Output: <p>3 items</p>
}

// A cacheable Result with a fresh nonce per response (A7).
func ExampleResult_Bytes() {
	eng, _ := brick.New(brick.Options{})
	r, _ := eng.RenderChunks(context.Background(), "p.html", nil, brick.WithSource([]byte(`<script nonce="{{ nonce }}">go()</script>`)))
	a, _ := r.Bytes(brick.WithNonce("abc"))
	b, _ := r.Bytes(brick.WithNonce("xyz"))
	fmt.Println(string(a))
	fmt.Println(string(b))
	// Output:
	// <script nonce="abc">go()</script>
	// <script nonce="xyz">go()</script>
}

// Checking a whole theme before shipping it.
func ExampleEngine_CompileAll() {
	fsys := fstest.MapFS{"content/a.html": {Data: []byte("<p>\n<Crad />")}}
	eng, _ := brick.New(brick.Options{FS: fsys})
	r, _ := eng.CompileAll(context.Background(), "content")
	for _, d := range r.Diagnostics {
		fmt.Println(d)
	}
	// Output: content/a.html:2:1: error: unknown component <Crad> — no file in use: or the roots (components) defines it
}
