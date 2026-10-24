package brick

import (
	"bytes"
	"context"
	"errors"
	"html/template"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Tests for the streamed page render and the client errors
// attribute (data-brick-errors, G7).

// streamPage has two stream islands that wait on gates "a" and "b", each in
// a boundary with a loading block, and content between them.
const streamPage = `<main><brick:boundary><Gated brick:lazy="stream" gate="a" /><brick:prop name="loading"><i>loading a</i></brick:prop><brick:prop name="fallback"><p>a failed</p></brick:prop></brick:boundary>
<p>middle</p>
<brick:boundary><Gated brick:lazy="stream" gate="b" /><brick:prop name="loading"><i>loading b</i></brick:prop></brick:boundary></main>`

// gatedComponent prints "fail" through a missing field when its gate is
// "fail", which fails its render.
const gatedComponent = `---
props:
  gate: {type: string}
---
<b>{{ wait .Props.gate }}got {{ .Props.gate }}{{ if eq .Props.gate "fail" }}{{ .Props.gate.Field }}{{ end }}</b>`

// gates lets a test release each island's render; inFlight and peak count
// renders waiting at once.
type gates struct {
	mu       sync.Mutex
	ch       map[string]chan struct{}
	inFlight atomic.Int32
	peak     atomic.Int32
}

func (g *gates) get(name string) chan struct{} {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.ch[name] == nil {
		g.ch[name] = make(chan struct{})
	}
	return g.ch[name]
}

func (g *gates) open(name string) { close(g.get(name)) }

func (g *gates) wait(name string) string {
	n := g.inFlight.Add(1)
	for {
		p := g.peak.Load()
		if n <= p || g.peak.CompareAndSwap(p, n) {
			break
		}
	}
	defer g.inFlight.Add(-1)
	if name == "fail" || name == "free" {
		return ""
	}
	<-g.get(name)
	return ""
}

func streamEngineFor(t *testing.T, opts Options, page string) (*Engine, *gates, *Result) {
	t.Helper()
	g := &gates{ch: map[string]chan struct{}{}}
	opts.FS = theme("components/Gated.html", gatedComponent, "content/a.html", page)
	opts.Funcs = template.FuncMap{"wait": g.wait}
	e := newEngine(t, opts)
	r, err := e.RenderChunks(context.Background(), "content/a.html", nil)
	if err != nil {
		t.Fatal(err)
	}
	return e, g, r
}

var fillIDRE = regexp.MustCompile(`<brick-fill data-brick-fill="(s\d+)" hidden>(.*?)</brick-fill>`)

// The page goes out with each placeholder holding its loading block, and
// the fills follow in completion order, not page order.
func TestStreamPageOrder(t *testing.T) {
	t.Parallel()
	e, g, r := streamEngineFor(t, Options{}, streamPage)
	if !r.HasStream() || r.HasLazy() {
		t.Fatalf("HasStream %v HasLazy %v", r.HasStream(), r.HasLazy())
	}
	page, fills, err := e.FillStream(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	want := `<main><brick-lazy data-brick-stream="s1"><i>loading a</i></brick-lazy>
<p>middle</p>
<brick-lazy data-brick-stream="s2"><i>loading b</i></brick-lazy></main>`
	if string(page) != want {
		t.Fatalf("page:\n%s", page)
	}
	if fills.Len() != 2 {
		t.Fatalf("Len %d", fills.Len())
	}
	var got []string
	done := make(chan error)
	go func() {
		done <- fills.Each(context.Background(), func(b []byte) error {
			got = append(got, string(b))
			return nil
		})
	}()
	g.open("b")
	time.Sleep(20 * time.Millisecond)
	g.open("a")
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("fills: %q", got)
	}
	m1, m2 := fillIDRE.FindStringSubmatch(got[0]), fillIDRE.FindStringSubmatch(got[1])
	if m1 == nil || m2 == nil || m1[1] != "s2" || m1[2] != "<b>got b</b>" || m2[1] != "s1" || m2[2] != "<b>got a</b>" {
		t.Fatalf("completion order: %q", got)
	}
	// The applier is defined once, by the first fill; each fill calls it
	// and carries the no-JS style for its own ids.
	if !strings.Contains(got[0], "window.__brickFill=") || strings.Contains(got[1], "window.__brickFill=") {
		t.Fatalf("applier: %q", got)
	}
	for i, f := range got {
		id := []string{"s2", "s1"}[i]
		for _, s := range []string{`<noscript><style>brick-lazy[data-brick-stream="` + id + `"]{display:none}brick-fill[data-brick-fill="` + id + `"]{display:contents}</style></noscript>`, `__brickFill("` + id + `")</script>`} {
			if !strings.Contains(f, s) {
				t.Fatalf("fill %d lacks %s: %s", i, s, f)
			}
		}
	}
	if err := fills.Each(context.Background(), func([]byte) error { return nil }); err == nil {
		t.Fatal("a second Each must fail")
	}
}

// Where the response cannot stream, Fill renders a stream island in place:
// the same bytes as the same call without brick:lazy.
func TestStreamFillInline(t *testing.T) {
	t.Parallel()
	page := strings.ReplaceAll(streamPage, `gate="a"`, `gate="free"`)
	page = strings.ReplaceAll(page, `gate="b"`, `gate="free"`)
	e, _, r := streamEngineFor(t, Options{}, page)
	got, err := e.Fill(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	_, _, plain := streamEngineFor(t, Options{}, strings.ReplaceAll(page, ` brick:lazy="stream"`, ``))
	want, err := e.Fill(context.Background(), plain)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) || strings.Contains(string(got), "brick-lazy") {
		t.Fatalf("inline:\n%s\nplain:\n%s", got, want)
	}
	// An export renders it in place too.
	b, fills, err := e.FillStream(context.Background(), r, WithStaticState())
	if err != nil || fills.Len() != 0 || !bytes.Equal(b, want) {
		t.Fatalf("static: %v %d\n%s", err, fills.Len(), b)
	}
}

// A page without a stream island takes Fill's path: same bytes, no fills.
func TestStreamNoneIsFill(t *testing.T) {
	t.Parallel()
	e, _, r := streamEngineFor(t, Options{}, `<p>plain</p><Gated gate="free" />`)
	page, fills, err := e.FillStream(context.Background(), r)
	if err != nil || fills != nil {
		t.Fatalf("%v %v", err, fills)
	}
	want, _ := e.Fill(context.Background(), r)
	if !bytes.Equal(page, want) {
		t.Fatalf("%s != %s", page, want)
	}
	if err := fills.Each(context.Background(), func([]byte) error { t.Fatal("no fills"); return nil }); err != nil {
		t.Fatal(err)
	}
}

// StreamWorkers bounds the renders running at once.
func TestStreamWorkers(t *testing.T) {
	t.Parallel()
	var page strings.Builder
	for _, n := range []string{"a", "b", "c", "d"} {
		page.WriteString(`<Gated brick:lazy="stream" gate="` + n + `" />`)
	}
	e, g, r := streamEngineFor(t, Options{StreamWorkers: 2}, page.String())
	_, fills, err := e.FillStream(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error)
	go func() { done <- fills.Each(context.Background(), func([]byte) error { return nil }) }()
	time.Sleep(30 * time.Millisecond)
	for _, n := range []string{"a", "b", "c", "d"} {
		g.open(n)
		time.Sleep(5 * time.Millisecond)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if p := g.peak.Load(); p != 2 {
		t.Fatalf("peak concurrent renders %d, want 2", p)
	}
}

// A stream island that fails never fails the response: Prod writes the
// boundary's fallback, Dev the error block and the fallback, and Failed
// reports it for the log.
func TestStreamFailure(t *testing.T) {
	t.Parallel()
	page := strings.ReplaceAll(streamPage, `gate="a"`, `gate="fail"`)
	for _, mode := range []Mode{Prod, Dev} {
		e, g, r := streamEngineFor(t, Options{Mode: mode}, page)
		_, fills, err := e.FillStream(context.Background(), r)
		if err != nil {
			t.Fatal(err)
		}
		g.open("b")
		var got []string
		if err := fills.Each(context.Background(), func(b []byte) error { got = append(got, string(b)); return nil }); err != nil {
			t.Fatal(err)
		}
		all := strings.Join(got, "")
		if !strings.Contains(all, `hidden><p>a failed</p></brick-fill>`) && !strings.Contains(all, `</pre><p>a failed</p></brick-fill>`) {
			t.Fatalf("mode %v: %s", mode, all)
		}
		if hasBlock := strings.Contains(all, "brick-error"); hasBlock != (mode == Dev) {
			t.Fatalf("mode %v error block %v", mode, hasBlock)
		}
		if f := fills.Failed(); len(f) != 1 {
			t.Fatalf("mode %v Failed %v", mode, f)
		}
		if !strings.Contains(all, ">got b</b>") {
			t.Fatalf("the other island must still arrive: %s", all)
		}
	}
}

// A nonce reaches the inline script and the no-JS style; without one,
// neither has a nonce attribute.
func TestStreamNonce(t *testing.T) {
	t.Parallel()
	page := strings.ReplaceAll(streamPage, `gate="a"`, `gate="free"`)
	page = strings.ReplaceAll(page, `gate="b"`, `gate="free"`)
	e, _, r := streamEngineFor(t, Options{}, page)
	for _, nonce := range []string{"", "abc123"} {
		var opts []RenderOption
		if nonce != "" {
			opts = append(opts, WithNonce(nonce))
		}
		var buf bytes.Buffer
		flushes := 0
		if err := e.Stream(context.Background(), &buf, func() { flushes++ }, r, opts...); err != nil {
			t.Fatal(err)
		}
		out := buf.String()
		if flushes != 3 || !strings.HasPrefix(out, "<main><brick-lazy") {
			t.Fatalf("flushes %d: %s", flushes, out)
		}
		has := strings.Contains(out, `<script nonce="abc123">`) && strings.Contains(out, `<style nonce="abc123">`)
		if has != (nonce != "") || (nonce == "" && strings.Contains(out, "nonce=")) {
			t.Fatalf("nonce %q: %s", nonce, out)
		}
	}
}

// The client going away stops the stream with its error.
func TestStreamCancel(t *testing.T) {
	t.Parallel()
	e, _, r := streamEngineFor(t, Options{}, streamPage)
	_, fills, err := e.FillStream(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if err := fills.Each(ctx, func([]byte) error { return nil }); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v", err)
	}
}

// stream is a lazy mode the compile check accepts, and a stream island
// inside a filled island loads lazily instead (it cannot stream from
// inside an answer).
func TestStreamModeAndNesting(t *testing.T) {
	t.Parallel()
	if err := checkLazyValue(LazyStream); err != nil {
		t.Fatal(err)
	}
	e2 := newEngine(t, Options{States: NewMemoryStates(0, 0), Funcs: template.FuncMap{"wait": func(string) string { return "" }}, FS: theme("components/Gated.html", gatedComponent, "components/Outer.html", `<section><Gated brick:lazy="stream" gate="free" /></section>`, "content/a.html", `<Outer brick:lazy="stream" />`)})
	r2, err := e2.RenderChunks(context.Background(), "content/a.html", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, fills, err := e2.FillStream(context.Background(), r2)
	if err != nil {
		t.Fatal(err)
	}
	var got string
	if err := fills.Each(context.Background(), func(b []byte) error { got += string(b); return nil }); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, `<section><brick-lazy data-brick-lazy="`) || !strings.Contains(got, `data-when="load"`) {
		t.Fatalf("nested stream island: %s", got)
	}
}

// data-brick-errors carries the render's .Errors for brick.errors(el), and
// is absent when there are none, so a root without errors keeps its bytes.
func TestErrorsAttribute(t *testing.T) {
	t.Parallel()
	e, base := signupEngine(t, Options{})
	res, err := e.RunAction(context.Background(), ActionRequest{Handle: base.Handle, Token: base.Token, Action: "taken", Sets: []FieldSet{{Path: "email", Value: "bad"}}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(res.HTML), ` data-brick-errors="{&#34;email&#34;:&#34;That email is taken.&#34;}"`) {
		t.Fatalf("errors attribute: %s", res.HTML)
	}
	res, err = e.RunAction(context.Background(), ActionRequest{Handle: base.Handle, Token: base.Token, Action: "save"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(res.HTML), "data-brick-errors") {
		t.Fatalf("no errors, no attribute: %s", res.HTML)
	}
}
