package brick

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"regexp"
	"strings"
	"sync"
	"testing"
)

func TestRawBodiesAndSrc(t *testing.T) {
	t.Parallel()
	e := newEngine(t, Options{FS: theme(
		"components/CodeBlock.html", "---\nraw: true\nprops:\n  lang: {type: string, default: text}\n---\n<pre class=\"{{ .Props.lang }}\">{{ .Raw }}</pre>",
		"examples/main.go", "package main\n",
		"content/a.html", "<CodeBlock lang=\"go\">\n    if x < 10 { {{ not }} }\n      y\n</CodeBlock>|<CodeBlock src=\"~/examples/main.go\" />|<x-diff><brick:prop name=\"before\" raw>\n  a \"q\" <b>\n</brick:prop></x-diff>",
		"components/Diff.html", "{{ .Props.before }}",
	)})
	got := render(t, e, "content/a.html", nil)
	want := "<pre class=\"go\">if x &lt; 10 { {{ not }} }\n  y</pre>|<pre class=\"text\">package main\n</pre>|a &#34;q&#34; &lt;b&gt;"
	if got != want {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
}

func TestKeyAttrsAndReservedProps(t *testing.T) {
	t.Parallel()
	e := newEngine(t, Options{FS: theme(
		"components/Btn.html", "---\nprops:\n  label: string\n---\n<button {{ attrs }}>{{ .Props.label }}{{ .Key }}</button>",
		"content/a.html", `<Btn label="Go" key="k1" class="x y" disabled data-id="{{ .Params.id }}" hidden="{{ .Params.no }}" onclick="evil()" />`+"\n---",
		"content/children.html", `<Btn children="x" />`,
		"content/dup.html", `<Btn label="a" :label=".X" />`,
	)})
	got := render(t, e, "content/a.html", nil)
	if !strings.HasPrefix(got, `<button class="x y" disabled data-id="" hidden="">Gok1</button>`) {
		t.Fatalf("got %q", got)
	}
	if err := renderErr(t, e, "content/children.html"); !strings.Contains(err.Error(), "children is reserved") {
		t.Fatalf("children: %v", err)
	}
	if err := renderErr(t, e, "content/dup.html"); !strings.Contains(err.Error(), "given twice") {
		t.Fatalf("dup: %v", err)
	}
}

func TestPropDeclarations(t *testing.T) {
	t.Parallel()
	e := newEngine(t, Options{FS: theme(
		"components/Callout.html", "---\nprops:\n  type: {type: string, oneof: [tip, warn], default: tip}\n  items: {type: list, required: true}\n  n: int\n---\n{{ .Props.type }}{{ len .Props.items }}{{ .Props.n }}",
		"components/Strict.html", "---\nstrictProps: true\nprops:\n  a: string\n---\n{{ .Props.a }}",
		"content/ok.html", `<Callout :items="list" n="3" />`+"\n---",
		"content/oneof.html", `<Callout type="tpi" :items="list 1" />`,
		"content/required.html", `<Callout />`,
		"content/badint.html", `<Callout :items="list" n="x" />`,
		"content/runtime.html", `<Callout :items="list" :n="print `+"`x`"+`" />`,
		"content/strict.html", `<Strict b="1" />`,
	)})
	if got := render(t, e, "content/ok.html", nil); !strings.HasPrefix(got, "tip03") {
		t.Fatalf("ok: %q", got)
	}
	for page, want := range map[string]string{
		"content/oneof.html":    "not one of tip, warn",
		"content/required.html": `requires the prop "items"`,
		"content/badint.html":   `"x" is not a int`,
		"content/runtime.html":  `"x" is not a int`,
		"content/strict.html":   "strictProps",
	} {
		if err := renderErr(t, e, page); err.Code != CodeProp || !strings.Contains(err.Error(), want) {
			t.Fatalf("%s: %v", page, err)
		}
	}
}

func TestUseDataAliasesAndPaths(t *testing.T) {
	t.Parallel()
	e := newEngine(t, Options{FS: theme(
		"shared/PricingCard.html", `<b>{{ .Props.plan.name }}</b>`,
		"data/pricing.json", `{"plans":[{"name":"Pro","price":10}]}`,
		"data/team.yaml", "lead: Sara\n",
		"content/a.html", "---\nuse:\n  Card: ~/shared/PricingCard.html\n  pricing: ~/data/pricing.json\n  team: ~/data/team.yaml\n---\n{{ range .Data.pricing.plans }}<Card :plan=\".\" />{{ printf \"%T\" .price }}{{ end }}{{ .Data.team.lead }}",
		"content/rel.html", "---\nuse:\n  Card: ./PricingCard.html\n---\n",
		"content/up.html", "---\nuse:\n  Card: ~/../x.html\n---\n",
		"content/missing.html", "---\nuse:\n  Card: ~/shared/Nope.html\n---\n",
		"content/case.html", "---\nuse:\n  Card: ~/shared/pricingcard.html\n---\n",
	)})
	if got := render(t, e, "content/a.html", nil); got != "<b>Pro</b>int64Sara" {
		t.Fatalf("got %q", got)
	}
	for page, code := range map[string]Code{"content/rel.html": CodeFrontmatter, "content/up.html": CodeFrontmatter, "content/missing.html": CodeImport, "content/case.html": CodeImport} {
		if err := renderErr(t, e, page); err.Code != code {
			t.Fatalf("%s: %+v", page, err)
		}
	}
}

func TestLanguageVariants(t *testing.T) {
	t.Parallel()
	msgs := Messages{
		"en": {"cart": map[string]any{"add": "Add", "items": map[string]any{"one": "{count} item", "other": "{count} items"}}},
		"fr": {"cart": map[string]any{"add": "Ajouter", "items": map[string]any{"one": "{count} article", "other": "{count} articles"}}},
	}
	e := newEngine(t, Options{DefaultLocale: "en", Messages: msgs, FS: theme(
		"layouts/default.html", `<html lang="{{ .Page.Locale }}">{{ children }}</html>`,
		"layouts/default.ar.html", `<html dir="rtl">{{ children }}</html>`,
		"components/Card.html", `shared`,
		"components/Card.pt.html", `pt`,
		"content/a.html", `<Card /> {{ t "cart.add" }} {{ t "cart.items" (dict "count" 1) }} {{ t "cart.items" (dict "count" 0) }} {{ t "nope" }}`,
	)})
	if got := render(t, e, "content/a.html", nil); got != `<html lang="en">shared Add 1 item 0 items nope</html>` {
		t.Fatalf("en: %s", got)
	}
	if got := render(t, e, "content/a.html", nil, WithLocale("fr_FR")); got != `<html lang="fr-FR">shared Ajouter 1 article 0 article nope</html>` {
		t.Fatalf("fr: %s", got)
	}
	if got := render(t, e, "content/a.html", nil, WithLocale("pt-BR")); !strings.HasPrefix(got, `<html lang="pt-BR">pt `) {
		t.Fatalf("pt: %s", got)
	}
	if got := render(t, e, "content/a.html", nil, WithLocale("ar")); !strings.HasPrefix(got, `<html dir="rtl">shared`) {
		t.Fatalf("ar: %s", got)
	}
}

func TestDevAndProdErrors(t *testing.T) {
	t.Parallel()
	fsys := theme("content/a.html", "ok{{ index .Params.x 1 }}")
	prod := newEngine(t, Options{FS: fsys})
	out, err := prod.Render(context.Background(), "content/a.html", nil)
	if err == nil || out != nil {
		t.Fatalf("prod: out=%q err=%v", out, err)
	}
	dev := newEngine(t, Options{FS: fsys, Mode: Dev})
	out, err = dev.Render(context.Background(), "content/a.html", nil)
	if err == nil || !strings.HasPrefix(string(out), "ok<pre class=\"brick-error\"") {
		t.Fatalf("dev: out=%q err=%v", out, err)
	}
}

func TestLimitsAndCancellation(t *testing.T) {
	t.Parallel()
	e := newEngine(t, Options{MaxDepth: 8, MaxOutputBytes: 64, FS: theme(
		"components/Loop.html", `<Loop />`,
		"content/loop.html", `<Loop />`,
		"content/big.html", `{{ range seq 100 }}xxxxxxxxxx{{ end }}`,
		"content/boundary.html", `<brick:boundary><Loop /><brick:prop name="fallback">F</brick:prop></brick:boundary>`,
	)})
	if err := renderErr(t, e, "content/loop.html"); err.Code != CodeLimit {
		t.Fatalf("loop: %+v", err)
	}
	if err := renderErr(t, e, "content/big.html"); err.Code != CodeLimit {
		t.Fatalf("big: %+v", err)
	}
	if got := render(t, e, "content/boundary.html", nil); got != "F" {
		t.Fatalf("boundary: %q", got)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := e.Render(ctx, "content/boundary.html", nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
}

func TestNonceAssetsAndMarkers(t *testing.T) {
	t.Parallel()
	e := newEngine(t, Options{FS: theme(
		"components/Accordion.html", "---\nprops:\n  open: {type: bool, default: false, client: true}\n  secret: string\n---\n<brick:client>\ncustomElements.define('x-a', class extends HTMLElement {})\n</brick:client>\n<brick:style scoped>\n.p { color: red }\n</brick:style>\n<div class=\"acc\">{{ children }}</div>",
		"content/a.html", `<script nonce="{{ nonce }}">x</script><Accordion open secret="s">1</Accordion><Accordion>2</Accordion>{{ assets }}`,
	)})
	r, err := e.RenderChunks(context.Background(), "content/a.html", nil)
	if err != nil {
		t.Fatal(err)
	}
	out, err := r.Bytes(WithNonce("abc123=="))
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, want := range []string{`<script nonce="abc123==">x</script>`, `data-brick-type="Accordion" data-brick-props="{&#34;open&#34;:true}"`, `data-brick-s="`, `<brick-assets><script type="module" src="/_brick/c/runtime.`, `<script type="module" src="/_brick/c/Accordion.`, `integrity="sha384-`, `nonce="abc123=="></script><link rel="stylesheet"`} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q in\n%s", want, s)
		}
	}
	if strings.Contains(s, "secret") || strings.Count(s, "<script type=\"module\"") != 2 {
		t.Fatalf("assets or props wrong:\n%s", s)
	}
	if _, err := r.Bytes(WithNonce(`x" onload="`)); err == nil {
		t.Fatal("bad nonce accepted")
	}
	f, err := e.Assets().Open("c/Accordion" + strings.Split(strings.Split(s, `src="/_brick/c/Accordion`)[1], `"`)[0])
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(f)
	if !strings.Contains(string(b), "customElements") {
		t.Fatalf("asset body %q", b)
	}
}

type hookRec struct {
	mu     sync.Mutex
	events []string
}

func (h *hookRec) BeforeRender(_ context.Context, p *Page) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.events = append(h.events, "before:"+p.Path)
	return nil
}

func (h *hookRec) AfterRender(_ context.Context, _ *Page, r *Result) error {
	for i, c := range r.Chunks {
		if c.Kind == ChunkBytes {
			r.Chunks[i].Bytes = []byte(strings.ToUpper(string(c.Bytes)))
		}
	}
	return nil
}

func (h *hookRec) OnError(_ context.Context, err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.events = append(h.events, "error")
}

type markerBreaker struct{}

func (markerBreaker) AfterRender(_ context.Context, _ *Page, r *Result) error {
	r.Chunks = r.Chunks[:1]
	return nil
}

func TestHooks(t *testing.T) {
	t.Parallel()
	if _, err := New(Options{Hooks: []any{struct{}{}}}); err == nil {
		t.Fatal("a non-hook was accepted")
	}
	h := &hookRec{}
	e := newEngine(t, Options{Hooks: []any{h}, FS: theme("content/a.html", "hi{{ nonce }}there")})
	if got := render(t, e, "content/a.html", nil); got != "HITHERE" {
		t.Fatalf("got %q", got)
	}
	if h.events[0] != "before:content/a.html" {
		t.Fatalf("events %v", h.events)
	}
	e2 := newEngine(t, Options{Hooks: []any{markerBreaker{}}, FS: theme("content/a.html", "hi{{ nonce }}there")})
	if err := renderErr(t, e2, "content/a.html"); err.Code != CodeHook {
		t.Fatalf("marker guard: %+v", err)
	}
}

func TestInjectProvideAndStrictParams(t *testing.T) {
	t.Parallel()
	e := newEngine(t, Options{FS: theme(
		"components/Themed.html", "---\ninject:\n  accent: {default: gray}\n  user: {required: true}\n---\n{{ .Ctx.accent }}/{{ .Ctx.user }}",
		"content/a.html", `<Themed />`,
		"content/b.html", "---\nstrictParams: true\ntitle: x\n---\n{{ .Params.titel }}",
		"content/c.html", "---\nstrictParams: true\n---\n{{ with index .Params `subtitle` }}{{ . }}{{ end }}ok",
	)})
	if got := render(t, e, "content/a.html", nil, Provide("user", "sara")); got != "gray/sara" {
		t.Fatalf("got %q", got)
	}
	if err := renderErr(t, e, "content/a.html"); err.Code != CodeInject {
		t.Fatalf("inject: %+v", err)
	}
	if err := renderErr(t, e, "content/b.html"); !strings.Contains(err.Error(), "titel") {
		t.Fatalf("strict: %v", err)
	}
	if got := render(t, e, "content/c.html", nil); got != "ok" {
		t.Fatalf("strict index: %q", got)
	}
}

func TestAuthorDefinesAndPassthrough(t *testing.T) {
	t.Parallel()
	e := newEngine(t, Options{Passthrough: []string{"x-date-picker"}, FS: theme(
		"components/Card.html", `[{{ children }}]`,
		"content/a.html", `{{ define "row" }}<Card>{{ .N }}{{ $.Params.t }}</Card>{{ end }}{{ range .Props.rows }}{{ template "row" . }}{{ end }}<DIV>x</DIV><x-date-picker value="1" />`+"\n---\nt: T\n",
		"content/reserved.html", `<button brick:click="inc">+</button>`,
		"content/under.html", `{{ __render }}`,
	)})
	got := render(t, e, "content/a.html", map[string]any{"rows": []map[string]int{{"N": 1}, {"N": 2}}})
	if !strings.HasPrefix(got, "[1][2]<DIV>x</DIV><x-date-picker value=\"1\"></x-date-picker>") {
		t.Fatalf("got %q", got)
	}
	if err := renderErr(t, e, "content/reserved.html"); err.Code != CodeReserved {
		t.Fatalf("reserved: %+v", err)
	}
	if err := renderErr(t, e, "content/under.html"); err.Code != CodeSyntax {
		t.Fatalf("under: %+v", err)
	}
}

func TestRenderComponentPagePropsAndReload(t *testing.T) {
	t.Parallel()
	fsys := theme(
		"components/Badge.html", `<b>{{ .Props.n }}:{{ printf "%T" .Props.n }}:{{ children }}:{{ .Page.Props }}</b>`,
		"content/a.html", `<Badge :n=".Props.plan" />`,
	)
	e := newEngine(t, Options{FS: fsys})
	out, err := e.RenderComponent(context.Background(), "Badge", map[string]any{"n": 3}, "<i>kid</i>")
	if err != nil || string(out) != "<b>3:int:<i>kid</i>:map[n:3]</b>" {
		t.Fatalf("RenderComponent: %q %v", out, err)
	}
	if got := render(t, e, "content/a.html", map[string]any{"plan": "pro"}); got != "<b>pro:string::map[plan:pro]</b>" {
		t.Fatalf("page props: %q", got)
	}
	fsys["components/Badge.html"].Data = []byte("new")
	if got := render(t, e, "content/a.html", nil); !strings.HasPrefix(got, "<b>") {
		t.Fatalf("snapshot changed without Reload: %q", got)
	}
	e2, err := e.Reload(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := render(t, e2, "content/a.html", nil); got != "new" {
		t.Fatalf("reload: %q", got)
	}
}

func TestDeterministicAndConcurrent(t *testing.T) {
	t.Parallel()
	e := newEngine(t, Options{FS: theme(
		"components/Card.html", `<div>{{ .Props.i }}{{ range $k, $v := .Props.m }}{{ $k }}={{ $v }};{{ end }}{{ children }}</div>`,
		"content/a.html", `{{ range $i, $x := seq 50 }}<Card :i="$i" :m="dict `+"`b` 2 `a` 1"+`">{{ $x }}</Card>{{ end }}`,
	)})
	first := render(t, e, "content/a.html", nil)
	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out, err := e.Render(context.Background(), "content/a.html", nil)
			if err != nil {
				errs <- err
				return
			}
			if string(out) != first {
				errs <- fmt.Errorf("output differs")
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
}

func TestWithSourceAndNoFS(t *testing.T) {
	t.Parallel()
	e := newEngine(t, Options{})
	out, err := e.Render(context.Background(), "cms/page-7.html", map[string]any{"n": 1}, WithSource([]byte("<p>{{ .Props.n }}</p>")))
	if err != nil || string(out) != "<p>1</p>" {
		t.Fatalf("%q %v", out, err)
	}
	_, err = e.Render(context.Background(), "cms/x.html", nil, WithSource([]byte("<Card />")))
	if err == nil {
		t.Fatal("import without FS succeeded")
	}
	if _, err := e.Render(context.Background(), "../x.html", nil); err == nil {
		t.Fatal("page path climbed out")
	}
}

// fakeScripting is a Scripting whose modules are Go functions, so engine
// behaviour is tested without a script language.
type fakeScripting struct {
	modules map[string]map[string]func(args ...any) (any, error)
}

type fakeModule map[string]func(args ...any) (any, error)

func (f fakeModule) Functions() []string { return sortedKeys(f) }
func (f fakeModule) Call(ctx context.Context, fn string, args ...any) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return f[fn](args...)
}

func (s fakeScripting) Compile(_ context.Context, src ScriptSource) (ScriptModule, error) {
	m, ok := s.modules[strings.TrimSpace(string(src.Src))]
	if !ok {
		return nil, fmt.Errorf("%s: no fake module %q", src.Path, src.Src)
	}
	return fakeModule(m), nil
}

func TestScriptsAndLoaders(t *testing.T) {
	t.Parallel()
	calls := 0
	var mu sync.Mutex
	sc := fakeScripting{modules: map[string]map[string]func(args ...any) (any, error){
		"helpers": {"slugify": func(a ...any) (any, error) { return strings.ToLower(fmt.Sprint(a[0])), nil }},
		"posts": {"recent": func(a ...any) (any, error) {
			mu.Lock()
			calls++
			mu.Unlock()
			in, _ := a[0].(map[string]any)
			props, _ := in["props"].(map[string]any)
			return map[string]any{"items": []string{"p1", "p2"}, "limit": props["limit"]}, nil
		}},
		"inline": {"price": func(a ...any) (any, error) { return fmt.Sprintf("$%v", a[0]), nil }},
	}}
	e := newEngine(t, Options{Scripting: sc, FS: theme(
		"lib/helpers.star", "helpers",
		"lib/posts.star", "posts",
		"components/Recent.html", "---\nuse:\n  posts: ~/lib/posts.star\nload: posts.recent\nprops:\n  limit: {type: int, default: 5}\n---\n{{ range .Props.items }}{{ . }},{{ end }}{{ .Props.limit }}",
		"content/a.html", "---\nuse:\n  helpers: ~/lib/helpers.star\n---\n<brick:script>inline</brick:script>{{ helpers.slugify \"Hi There\" }} {{ price 5 }} <Recent /><Recent /><Recent :items=\"list `x`\" />",
		"content/nofn.html", "---\nuse:\n  helpers: ~/lib/helpers.star\n---\n{{ helpers.nope 1 }}",
	)})
	got := render(t, e, "content/a.html", nil)
	if got != "hi there $5 p1,p2,5p1,p2,5x,5" {
		t.Fatalf("got %q", got)
	}
	if calls != 2 {
		// Two distinct inputs: the third call site passed items, so the
		// loader input differs from the first two, which share one memo entry.
		t.Fatalf("loader ran %d times, want 2", calls)
	}
	if err := renderErr(t, e, "content/nofn.html"); err.Code != CodeScript {
		t.Fatalf("nofn: %+v", err)
	}
	if _, err := newEngine(t, Options{FS: theme("content/a.html", "<brick:script>x</brick:script>")}).Render(context.Background(), "content/a.html", nil); err == nil {
		t.Fatal("script without Scripting compiled")
	}
}

type upperMarkdown struct{}

func (upperMarkdown) Convert(src []byte, w io.Writer) error {
	_, err := w.Write([]byte("<md>" + strings.ToUpper(string(src)) + "</md>"))
	return err
}

func TestMarkdownAndSanitize(t *testing.T) {
	t.Parallel()
	e := newEngine(t, Options{Markdown: upperMarkdown{}, Sanitizer: func(s string) string { return strings.ReplaceAll(s, "<script>", "") }, FS: theme(
		"content/a.html", "<Markdown>\n  # hi <Card> {{ x }}\n</Markdown>{{ sanitize \"<script><b>ok</b>\" }}",
	)})
	if got := render(t, e, "content/a.html", nil); got != "<md># HI <CARD> {{ X }}</md><b>ok</b>" {
		t.Fatalf("got %q", got)
	}
	e2 := newEngine(t, Options{FS: theme("content/a.html", `{{ sanitize "x" }}`)})
	if err := renderErr(t, e2, "content/a.html"); !strings.Contains(err.Error(), "Options.Sanitizer") {
		t.Fatalf("sanitize: %v", err)
	}
}

func TestRegistryShadowAndHyphenFold(t *testing.T) {
	t.Parallel()
	e := newEngine(t, Options{Roots: []string{"components", "vendor"}, FS: theme(
		"components/ui/Card.html", "mine",
		"vendor/Card.html", "vendor",
		"components/HTMLEditor.html", "ed",
		"components/card.html", "lower",
		"content/a.html", "<Card /><x-card /><x-html-editor />",
	)})
	if got := render(t, e, "content/a.html", nil); got != "minemineed" {
		t.Fatalf("got %q", got)
	}
	var shadow, skipped bool
	for _, w := range e.Warnings() {
		shadow = shadow || strings.Contains(w.Message, "shadowed")
		skipped = skipped || strings.Contains(w.Message, "card.html is not a component name")
	}
	if !shadow || !skipped {
		t.Fatalf("warnings: %v", e.Warnings())
	}
}

var _ fs.FS = assetFS{}

func TestAuthoringWarnings(t *testing.T) {
	t.Parallel()
	e := newEngine(t, Options{FS: theme(
		"components/Card.html", `<div>{{ node "header" }}{{ children }}</div>`,
		"components/Late.html", `{{ children }}{{ provide "k" 1 }}`,
		"components/Js.html", "<brick:client>x</brick:client><div></div>",
		"content/a.html", `<Card><brick:prop name="header">h</brick:prop><brick:prop name="footr">f</brick:prop></Card><Late>x</Late><Js />`,
	)})
	r, err := e.RenderChunks(context.Background(), "content/a.html", nil)
	if err != nil {
		t.Fatal(err)
	}
	var codes []string
	for _, w := range append(e.Warnings(), r.Warnings...) {
		codes = append(codes, string(w.Code)+":"+w.Message)
	}
	all := strings.Join(codes, "\n")
	for _, want := range []string{`unused-node:<brick:prop name="footr"> is never read`, "provide:{{ provide }} after {{ children }}", "assets:a component on this page"} {
		if !strings.Contains(all, want) {
			t.Fatalf("missing %q in\n%s", want, all)
		}
	}
	if strings.Contains(all, `name="header"`) {
		t.Fatalf("a read node was reported:\n%s", all)
	}
}

func TestSelfReference(t *testing.T) {
	t.Parallel()
	e := newEngine(t, Options{FS: theme(
		"components/Counter.html", `<div><x-counter><b>{{ .Props.n }}</b></x-counter></div>`,
		"components/Loop.html", `<p><Loop /></p>`,
		"components/Tree.html", `<li>{{ .Props.n }}{{ if gt .Props.n 0 }}<Tree :n="sub .Props.n 1" />{{ end }}</li>`,
		"content/a.html", `<Counter n="1" /><Tree :n="2" />`,
		"content/b.html", `<Loop />`,
		"content/c.html", `<brick:component name="L"><L /></brick:component><L />`,
	)})
	if got := render(t, e, "content/a.html", nil); got != "<div><x-counter><b>1</b></x-counter></div><li>2<li>1<li>0</li></li></li>" {
		t.Fatalf("got %q", got)
	}
	for _, p := range []string{"content/b.html", "content/c.html"} {
		if err := renderErr(t, e, p); !strings.Contains(err.Error(), "can never stop") {
			t.Fatalf("%s: %v", p, err)
		}
	}
}

func TestIDsSyntaxAndMoreWarnings(t *testing.T) {
	t.Parallel()
	e := newEngine(t, Options{EmitIDs: true, FS: theme(
		"components/Card.html", `<div class="c">{{ .ID }}</div>`,
		"components/Two.html", `<a></a><b></b>`,
		"components/Old.html", "<brick:client>brick.mount(\"Old\", () => {})</brick:client><i></i>",
		"components/Lazy.html", "<brick:client>brick.mount(\"Lazy\", () => {})</brick:client><i></i>",
		"content/lazy.html", `<Lazy brick:mount="visible" />`,
		"content/badmount.html", `<Lazy brick:mount="later" />`,
		"content/a.html", `{{ range seq 2 }}<Card />{{ end }}<Two /><title><Card /></title>{{ .Slot }}<Old />`,
		"content/strict.html", "---\nsyntax: hyphenated\n---\n<Card />",
	)})
	got := render(t, e, "content/a.html", nil)
	ids := regexp.MustCompile(`data-brick="(b[0-9a-f]{8})" class="c">(b[0-9a-f]{8})</div>`).FindAllStringSubmatch(got, -1)
	if len(ids) != 2 || ids[0][1] != ids[0][2] || ids[0][1] == ids[1][1] {
		t.Fatalf("ids: %q", got)
	}
	if again := render(t, e, "content/a.html", nil); again != got {
		t.Fatal("ids are not deterministic")
	}
	var all []string
	for _, w := range e.Warnings() {
		all = append(all, w.Message)
	}
	joined := strings.Join(all, "\n")
	for _, want := range []string{"no single root element", "inside <title> is text", ".Slot is read outside"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing warning %q in\n%s", want, joined)
		}
	}
	if got := render(t, e, "content/lazy.html", nil); !strings.Contains(got, `data-brick-type="Lazy" data-brick-props="{}" data-brick-mount="visible"`) {
		t.Fatalf("brick:mount: %s", got)
	}
	if err := renderErr(t, e, "content/badmount.html"); !strings.Contains(err.Error(), "visible or idle") {
		t.Fatalf("bad brick:mount: %v", err)
	}
	if err := renderErr(t, e, "content/strict.html"); !strings.Contains(err.Error(), "only the hyphenated tag form") {
		t.Fatalf("syntax: %v", err)
	}
}

func TestDevErrorBlockContext(t *testing.T) {
	t.Parallel()
	e := newEngine(t, Options{Mode: Dev, FS: theme(
		"content/text.html", `<p>ok</p>{{ index .Params.x 1 }}`,
		"content/script.html", `<script>var a = 1; {{ index .Params.x 1 }}</script>`,
		"content/boundary.html", `<brick:boundary><script>{{ index .Params.x 1 }}</script><brick:prop name="fallback">F</brick:prop></brick:boundary>`,
	)})
	for page, want := range map[string]string{
		"content/text.html":     `<p>ok</p><pre class="brick-error"`,
		"content/script.html":   "<script>var a = 1; <!-- brick error: ",
		"content/boundary.html": `<pre class="brick-error"`,
	} {
		out, _ := e.Render(context.Background(), page, nil)
		if !strings.Contains(string(out), want) {
			t.Errorf("%s: got %q, want %q", page, out, want)
		}
	}
}

func TestSpreadForwardsAttrs(t *testing.T) {
	t.Parallel()
	e := newEngine(t, Options{FS: theme(
		"components/Button.html", `<button {{ attrs }}>{{ children }}</button>`,
		"components/Primary.html", "---\nprops:\n  size: string\n---\n<Button class=\"primary\" brick:spread=\".Attrs\">{{ children }}</Button>",
		"content/a.html", `<Primary size="lg" class="ignored" id="buy" data-x="1" hx-post="/buy">Buy</Primary>`,
		"content/bad.html", `<Button brick:spread="list 1">x</Button>`,
	)})
	if got := render(t, e, "content/a.html", nil); got != `<button class="primary" data-x="1" hx-post="/buy" id="buy">Buy</button>` {
		t.Fatalf("got %q", got)
	}
	if err := renderErr(t, e, "content/bad.html"); !strings.Contains(err.Error(), "brick:spread") {
		t.Fatalf("bad: %v", err)
	}
}

func TestResultFiles(t *testing.T) {
	t.Parallel()
	sc := fakeScripting{modules: map[string]map[string]func(args ...any) (any, error){"h": {"f": func(...any) (any, error) { return "x", nil }}}}
	e := newEngine(t, Options{Scripting: sc, FS: theme(
		"layouts/default.html", `{{ children }}`,
		"components/Card.html", "---\nuse:\n  d: ~/data/d.yaml\n---\n{{ .Data.d.a }}",
		"components/Code.html", "---\nraw: true\n---\n{{ .Raw }}",
		"components/Unused.html", `u`,
		"data/d.yaml", "a: 1\n",
		"lib/h.star", "h",
		"ex/x.go", "package x",
		"content/a.html", "---\nuse:\n  h: ~/lib/h.star\n---\n<Card /><Code src=\"~/ex/x.go\" />{{ h.f }}{{ if false }}<Unused />{{ end }}",
	)})
	r, err := e.RenderChunks(context.Background(), "content/a.html", nil)
	if err != nil {
		t.Fatal(err)
	}
	want := "components/Card.html components/Code.html content/a.html data/d.yaml ex/x.go layouts/default.html lib/h.star"
	if got := strings.Join(r.Files, " "); got != want {
		t.Fatalf("files:\n got  %s\n want %s", got, want)
	}
}

func TestNavigationAssets(t *testing.T) {
	t.Parallel()
	e := newEngine(t, Options{Navigation: true, FS: theme(
		"content/a.html", `<a href="/b/" brick:prefetch>b</a><div brick:persist id="p"></div><a href="/x" brick:nav="off">x</a>{{ assets }}`,
		"content/bad.html", `<button brick:click="x">+</button>`,
	)})
	got := render(t, e, "content/a.html", nil)
	if !strings.Contains(got, `/_brick/c/runtime.`) || !strings.Contains(got, `/_brick/c/nav.`) || !strings.Contains(got, `brick:prefetch`) {
		t.Fatalf("navigation assets: %s", got)
	}
	if err := renderErr(t, e, "content/bad.html"); !strings.Contains(err.Error(), "brick:click is reserved") {
		t.Fatalf("other brick: names stay reserved: %v", err)
	}
	off := newEngine(t, Options{FS: theme("content/a.html", `<p>x</p>{{ assets }}`)})
	if got := render(t, off, "content/a.html", nil); strings.Contains(got, "runtime.") {
		t.Fatalf("runtime linked without navigation or client code: %s", got)
	}
}

// A root whose descendants have {{ }} actions inside their start tags is
// still one root (markRoot scans the top level with actions blanked out).
func TestSingleRootWithActionsInTags(t *testing.T) {
	t.Parallel()
	e := newEngine(t, Options{States: NewMemoryStates(0, 0), Scripting: counterScripting(), FS: theme(
		"components/Form.html", "---\nstate:\n  on: false\n---\n<brick:script>counter</brick:script>\n<form class=\"f\">\n  <p brick:show=\"on\" {{ if not .State.on }}hidden{{ end }}>x</p>\n  <input value=\"{{ .State.on }}\"><br>\n</form>",
		"components/Two.html", "---\nstate:\n  on: false\n---\n<brick:script>counter</brick:script>\n<p {{ if .State.on }}class=\"a\"{{ end }}>1</p><p>2</p>",
		"content/a.html", `<Form />`,
		"content/b.html", `<Two />`,
	)})
	if got := render(t, e, "content/a.html", nil); !strings.Contains(got, `<form data-brick="`) || !strings.Contains(got, `class="f">`) {
		t.Fatalf("marker not on the root: %s", got)
	}
	if err := renderErr(t, e, "content/b.html"); !strings.Contains(err.Error(), "exactly one top-level element") {
		t.Fatalf("two roots: %v", err)
	}
}

func TestClientModuleImports(t *testing.T) {
	t.Parallel()
	e := newEngine(t, Options{FS: theme(
		"components/Modal.html", "<brick:client>\nexport const open = (id) => document.getElementById(id).showModal()\n</brick:client><dialog></dialog>",
		"components/Opener.html", "<brick:client>\nimport { open } from \"brick:Modal\"\nimport 'brick:x-modal'\nbrick.mount(\"Opener\", (el) => el.onclick = () => open(\"d\"))\n</brick:client><button>open</button>",
		"components/Bad.html", "<brick:client>import 'brick:Nope'</brick:client><i></i>",
		"components/Plain.html", "<b>no client</b>",
		"components/NoClient.html", "<brick:client>import 'brick:Plain'</brick:client><i></i>",
		"components/A.html", "<brick:client>import 'brick:B'</brick:client><i></i>",
		"components/B.html", "<brick:client>import 'brick:A'</brick:client><i></i>",
		"content/a.html", "<Opener />{{ assets }}",
		"content/bad.html", "<Bad />", "content/noclient.html", "<NoClient />", "content/cycle.html", "<A />",
	)})
	r, err := e.RenderChunks(context.Background(), "content/a.html", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(r.Files, " "), "components/Modal.html") {
		t.Fatalf("the imported component is a dependency: %v", r.Files)
	}
	var opener []byte
	var modalURL string
	for _, p := range listAssetFiles(t, e) {
		f, _ := e.Assets().Open(p)
		b, _ := io.ReadAll(f)
		if strings.HasPrefix(p, "c/Opener.") {
			opener = b
		}
		if strings.HasPrefix(p, "c/Modal.") {
			// A relative specifier, valid because both modules are in c/.
			modalURL = "./" + strings.TrimPrefix(p, "c/")
			if !strings.Contains(string(b), "export const open") {
				t.Fatalf("modal module: %s", b)
			}
		}
	}
	if modalURL == "" || !strings.Contains(string(opener), `from "`+modalURL+`"`) || !strings.Contains(string(opener), `import '`+modalURL+`'`) {
		t.Fatalf("opener not linked to %s:\n%s", modalURL, opener)
	}
	for page, want := range map[string]string{"content/bad.html": "no component of that name", "content/noclient.html": "has no <brick:client>", "content/cycle.html": "import cycle"} {
		if err := renderErr(t, e, page); !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v", page, err)
		}
	}
}

func listAssetFiles(t *testing.T, e *Engine) []string {
	t.Helper()
	var out []string
	if err := fs.WalkDir(e.Assets(), ".", func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			out = append(out, p)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return out
}

// data-brick-type is the component file's name, whatever alias imported it,
// because that is the name its client code mounts.
func TestClientTypeIgnoresAlias(t *testing.T) {
	t.Parallel()
	e := newEngine(t, Options{FS: theme(
		"shared/Tabs.html", "<brick:client>brick.mount(\"Tabs\", () => {})</brick:client><div></div>",
		"content/a.html", "---\nuse:\n  KitTabs: ~/shared/Tabs.html\n---\n<KitTabs />",
	)})
	if got := render(t, e, "content/a.html", nil); !strings.Contains(got, `data-brick-type="Tabs"`) {
		t.Fatalf("type: %s", got)
	}
}

// A frontmatter key and a loader key of one name: the frontmatter wins
// (D4) and the render says so, for a page and for a layout; a loader key
// nobody shadows warns of nothing. BV: dropping either st.warn fails this.
func TestLoaderKeyShadowWarns(t *testing.T) {
	t.Parallel()
	sc := fakeScripting{modules: map[string]map[string]func(args ...any) (any, error){
		"posts": {"meta": func(a ...any) (any, error) { return map[string]any{"title": "loaded", "extra": "x"}, nil }},
	}}
	e := newEngine(t, Options{Scripting: sc, FS: theme(
		"lib/posts.star", "posts",
		"layouts/default.html", "---\nuse:\n  posts: ~/lib/posts.star\nload: posts.meta\ntitle: lay\n---\n{{ children }}",
		"content/a.html", "---\nuse:\n  posts: ~/lib/posts.star\nload: posts.meta\ntitle: mine\n---\n{{ .Params.title }}{{ .Params.extra }}",
		"content/b.html", "---\nuse:\n  posts: ~/lib/posts.star\nload: posts.meta\n---\n{{ .Params.title }}",
	)})
	res, err := e.RenderChunks(context.Background(), "content/a.html", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := res.Bytes(); !strings.Contains(string(got), "minex") {
		t.Fatalf("frontmatter must still win: %q", got)
	}
	files := map[string]bool{}
	for _, w := range res.Warnings {
		if w.Code == CodeShadow && strings.Contains(w.Message, `"title"`) {
			files[w.File] = true
		}
	}
	if !files["content/a.html"] || !files["layouts/default.html"] || len(files) != 2 {
		t.Fatalf("shadow warnings: %v (all: %v)", files, res.Warnings)
	}
}
