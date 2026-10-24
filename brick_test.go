package brick

import (
	"context"
	"errors"
	"strings"
	"testing"
	"testing/fstest"
)

// theme builds an in-memory project from path → content pairs.
func theme(files ...string) fstest.MapFS {
	fs := fstest.MapFS{}
	for i := 0; i+1 < len(files); i += 2 {
		fs[files[i]] = &fstest.MapFile{Data: []byte(files[i+1])}
	}
	return fs
}

func newEngine(t *testing.T, opts Options) *Engine {
	t.Helper()
	e, err := New(opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return e
}

// render renders a page and fails the test on error.
func render(t *testing.T, e *Engine, page string, props any, opts ...RenderOption) string {
	t.Helper()
	out, err := e.Render(context.Background(), page, props, opts...)
	if err != nil {
		t.Fatalf("Render %s: %v", page, err)
	}
	return string(out)
}

// renderErr renders and returns the error, failing when there is none.
func renderErr(t *testing.T, e *Engine, page string, opts ...RenderOption) *Error {
	t.Helper()
	_, err := e.Render(context.Background(), page, nil, opts...)
	if err == nil {
		t.Fatalf("Render %s: expected an error", page)
	}
	var be *Error
	if !errors.As(err, &be) {
		t.Fatalf("Render %s: error %T is not *brick.Error: %v", page, err, err)
	}
	return be
}

func TestMinimalPage(t *testing.T) {
	t.Parallel()
	e := newEngine(t, Options{FS: theme("content/a.html", "<h1>Hello</h1>")})
	if got := render(t, e, "content/a.html", nil); got != "<h1>Hello</h1>" {
		t.Fatalf("got %q", got)
	}
}

func TestComponentPropsChildren(t *testing.T) {
	t.Parallel()
	e := newEngine(t, Options{FS: theme(
		"components/Card.html", `<div class="card card--{{ .Props.variant | default "plain" }}"><h3>{{ .Props.title }}</h3>{{ children }}</div>`,
		"content/a.html", "---\ntitle: About\n---\n<h1>{{ .Title }}</h1><Card title=\"Hi\"><p>body</p></Card><x-card variant=\"big\" title=\"X\" />",
	)})
	got := render(t, e, "content/a.html", nil)
	want := `<h1>About</h1><div class="card card--plain"><h3>Hi</h3><p>body</p></div><div class="card card--big"><h3>X</h3></div>`
	if got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

func TestRangeVariablesReachBodies(t *testing.T) {
	t.Parallel()
	e := newEngine(t, Options{FS: theme(
		"components/Card.html", `<li>{{ .Props.n }}:{{ children }}</li>`,
		"content/a.html", `{{ range $i, $p := .Props.posts }}<Card :n="$i">{{ $p.T }}/{{ .T }}/{{ $.Params.x }}</Card>{{ end }}`+"\n",
	)})
	got := render(t, e, "content/a.html", map[string]any{"posts": []map[string]string{{"T": "A<b>"}, {"T": "B"}}})
	want := "<li>0:A&lt;b&gt;/A&lt;b&gt;/</li><li>1:B/B/</li>\n"
	if got != want {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
}

func TestExpressionAndInterpolatedProps(t *testing.T) {
	t.Parallel()
	e := newEngine(t, Options{FS: theme(
		"components/Show.html", "---\nprops:\n  count: int\n  open: bool\n---\n{{ printf \"%T\" .Props.count }} {{ .Props.count }} {{ .Props.label }} {{ .Props.open }} {{ printf \"%T\" .Props.items }}",
		"content/a.html", "---\nitems: [1, 2]\nname: Pro\n---\n<Show count=\"5\" label=\"plan {{ .Params.name }}!\" open :items=\".Params.items\" />",
	)})
	got := render(t, e, "content/a.html", nil)
	want := "int64 5 plan Pro! true []interface {}"
	if got != want {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
}

func TestNodesAndSlots(t *testing.T) {
	t.Parallel()
	e := newEngine(t, Options{FS: theme(
		"components/PostList.html", `<ul>{{ range .Props.posts }}<li>{{ node "item" . }}</li>{{ end }}</ul>{{ if hasNode "footer" }}F{{ end }}{{ if hasNode "children" }}C{{ end }}`,
		"content/a.html", `<PostList :posts=".Props.posts"><brick:prop name="item"><b>{{ .Slot.T }}</b>{{ range .Slot.Tags }}[{{ . }}{{ $.Slot.T }}]{{ end }}</brick:prop></PostList>`,
	)})
	got := render(t, e, "content/a.html", map[string]any{"posts": []map[string]any{{"T": "x", "Tags": []string{"a"}}, {"T": "y"}}})
	want := `<ul><li><b>x</b>[ax]</li><li><b>y</b></li></ul>`
	if got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

func TestChildrenDescriptorsAndChild(t *testing.T) {
	t.Parallel()
	e := newEngine(t, Options{FS: theme(
		"components/Tabs.html", `{{ range $i, $c := .Children }}<button>{{ $c.Props.title }}</button>{{ end }}|{{ child (index .Children 1) }}`,
		"components/Tab.html", `<section>{{ .Props.title }}:{{ children }}</section>`,
		"content/a.html", `<Tabs><Tab title="One">1</Tab><div><Tab :title=".Params.t">2</Tab></div></Tabs>`+"---",
	)})
	got := render(t, e, "content/a.html", nil)
	if !strings.HasPrefix(got, "<button>One</button><button></button>|<section>:2</section>") {
		t.Fatalf("got %s", got)
	}
}

func TestLayoutChainAndCtx(t *testing.T) {
	t.Parallel()
	e := newEngine(t, Options{FS: theme(
		"layouts/default.html", "<html>{{ .Title }}|{{ provide \"accent\" \"blue\" }}{{ children }}</html>",
		"layouts/post.html", "---\nlayout: default\nbanner: B\n---\n<article>{{ .Props.banner }}{{ children }}</article>",
		"components/Card.html", `<i>{{ .Ctx.accent }}</i>`,
		"content/a.html", "---\ntitle: T\nlayout: post\n---\n<Card />{{ .Ctx.accent }}",
		"content/b.html", "---\ntitle: B\n---\nbody",
		"content/c.html", "---\nlayout: none\n---\nfrag",
	)})
	if got := render(t, e, "content/a.html", nil); got != "<html>T|<article>B<i>blue</i>blue</article></html>" {
		t.Fatalf("a: %s", got)
	}
	if got := render(t, e, "content/b.html", nil); got != "<html>B|body</html>" {
		t.Fatalf("b: %s", got)
	}
	if got := render(t, e, "content/c.html", nil); got != "frag" {
		t.Fatalf("c: %s", got)
	}
	if got := render(t, e, "content/b.html", nil, WithLayout("none")); got != "body" {
		t.Fatalf("b none: %s", got)
	}
}

func TestLocalComponentAndRender(t *testing.T) {
	t.Parallel()
	e := newEngine(t, Options{FS: theme(
		"components/Hero.html", `<h1>{{ .Props.title }}</h1>`,
		"components/Quote.html", `<q>{{ .Props.text }}</q>`,
		"content/a.html", "---\nblocks:\n  - {type: Hero, title: Welcome}\n  - {type: Quote, text: Less}\n  - {type: Price, value: 5}\n---\n<brick:component name=\"Price\"><b>${{ .Props.value }}</b></brick:component>{{ range .Params.blocks }}<brick:render :name=\".type\" :props=\"omit . `type`\" />{{ end }}",
	)})
	got := render(t, e, "content/a.html", nil)
	if got != "<h1>Welcome</h1><q>Less</q><b>$5</b>" {
		t.Fatalf("got %q", got)
	}
}

func TestBoundary(t *testing.T) {
	t.Parallel()
	e := newEngine(t, Options{FS: theme(
		"components/Bad.html", `partial{{ index .Props.m "x" "y" }}`,
		"content/a.html", `a<brick:boundary><Bad :m="1" /><brick:prop name="fallback">[{{ .Slot.Component }}]</brick:prop></brick:boundary>b`,
	)})
	r, err := e.RenderChunks(context.Background(), "content/a.html", nil)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := r.Bytes()
	if string(got) != "a[Bad]b" || len(r.Errors) != 1 {
		t.Fatalf("got %q errors=%v", got, r.Errors)
	}
}

func TestErrorsPointAtAuthorLines(t *testing.T) {
	t.Parallel()
	e := newEngine(t, Options{FS: theme(
		"content/unknown.html", "---\nt: 1\n---\n<p>\n  <Crad />",
		"content/bad.html", "---\nt: 1\n---\nline4\n{{ .Params.t.x.y }}",
		"content/straddle.html", "{{ if .A }}<Box>{{ else }}</Box>{{ end }}",
	)})
	if err := renderErr(t, e, "content/unknown.html"); err.Code != CodeUnknownTag || err.Line != 5 || err.Col != 3 {
		t.Fatalf("unknown: %+v", err)
	}
	if err := renderErr(t, e, "content/bad.html"); err.Line != 5 {
		t.Fatalf("bad: line %d: %v", err.Line, err)
	}
	if err := renderErr(t, e, "content/straddle.html"); err.Code != CodeSyntax {
		t.Fatalf("straddle: %+v", err)
	}
}
