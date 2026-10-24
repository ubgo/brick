package main

import (
	"bytes"
	"context"
	"os"
	"testing"

	"github.com/ubgo/brick"
)

// want is the exact HTML each page renders; docs/guides/components/slots.md
// shows these bytes.
var want = map[string]string{
	"content/index.html": `<div class="card">
  <header><strong>Plans</strong></header>
  <div class="body">
  
  
  <p>This is the children.</p>
</div>
  <footer><a href="/buy">Buy now</a></footer>
</div>
<div class="card">
  <header>No footer</header>
  <div class="body"><p>Only a body.</p></div>
</div>
<ul>
  <li><a href="/hello/">Hello</a></li>
  <li><a href="/again/">Again</a></li>
</ul>
<section class="panel"><h2>Settings</h2><p>Panel body.</p></section>
`,
	"content/tabs.html": `<div class="tabs">
  <div role="tablist">
    <button role="tab">One</button>
    <button role="tab">Two</button>
  </div>
  <div class="panel"><section>First panel.</section></div>
</div>
`,
	"content/code.html": `<pre><code class="language-go">if x &lt; 10 &amp;&amp; y &gt; 2 {
    fmt.Println(&#34;{{ not a template }}&#34;)
}</code></pre>
<pre><code class="language-go">package main

func main() { println(&#34;hello&#34;) }
</code></pre>
`,
}

func TestPages(t *testing.T) {
	eng, err := newEngine("theme")
	if err != nil {
		t.Fatal(err)
	}
	got, err := renderAll(context.Background(), eng)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range pages {
		if got[p] != want[p] {
			t.Errorf("%s:\n got %q\nwant %q", p, got[p], want[p])
		}
	}
}

// Each block mistake the guide's Pitfalls list names fails at compile,
// with the caller's file and line and the words below.
func TestSlotErrors(t *testing.T) {
	eng, err := newEngine("theme")
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		`<Panel><p>x</p></Panel>`: `content/probe.html:1:1: <Panel>: <Panel> requires a <brick:prop name="title"> block`,
		`<Panel><brick:prop name="title">T</brick:prop><brick:prop name="aside">A</brick:prop><p>x</p></Panel>`: `content/probe.html:1:47: <Panel>: <brick:prop name="aside">: <Panel> declares no such slot (declared blocks: title)`,
		`<Panel><brick:prop name="title">T</brick:prop></Panel>`:                                                `content/probe.html:1:1: <Panel>: <Panel> requires children (content between its tags)`,
		`<CodeBlock :src=".Params.f" />`:                                                                        `content/probe.html:1:1: :src is never allowed: a file is chosen only by a literal src= written in the file (X3)`,
		`<CodeBlock src="../x.go" />`:                                                                           `content/probe.html:1:1: src=: path "../x.go": start from the project root, e.g. ~/components/Card.html (X7)`,
		`<Card>{{ if true }}<brick:prop name="header">H</brick:prop>{{ end }}</Card>`:                           `content/probe.html:1:20: <brick:prop> inside a {{ }} block of <Card> — put the condition inside the block instead`,
	}
	cases["<CodeBlock>a </CodeBlock> b</CodeBlock>"] = `content/probe.html:1:28: </CodeBlock> has no opening tag`
	for src, msg := range cases {
		_, err := eng.Render(context.Background(), "content/probe.html", nil, brick.WithSource([]byte(src)))
		if err == nil || err.Error() != msg {
			t.Errorf("%s:\n got %v\nwant %s", src, err, msg)
		}
	}
}

// .Children lists only the tags written literally between the tags: tabs
// made by a {{ range }} do not exist until render time, so the tab bar is
// empty.
func TestChildrenAreLiteral(t *testing.T) {
	eng, err := newEngine("theme")
	if err != nil {
		t.Fatal(err)
	}
	src := "---\nnames: [a, b]\n---\n<Tabs>{{ range .Params.names }}<Tab title=\"{{ . }}\">x</Tab>{{ end }}</Tabs>"
	got, err := eng.Render(context.Background(), "content/probe.html", nil, brick.WithSource([]byte(src)))
	if err != nil {
		t.Fatal(err)
	}
	const empty = "<div class=\"tabs\">\n  <div role=\"tablist\">\n  </div>\n  <div class=\"panel\"></div>\n</div>"
	if string(got) != empty {
		t.Errorf("got %q\nwant %q", got, empty)
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
