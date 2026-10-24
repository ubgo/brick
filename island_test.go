package brick

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type user struct{ Name string }

func islandTheme() map[string]string {
	return map[string]string{
		"layouts/default.html":  `<html>{{ provide "accent" "blue" }}{{ children }}</html>`,
		"components/Hello.html": "---\nbrick:\n  island: true\nprops:\n  greeting: {type: string, default: Hi}\n---\n<span data-id=\"{{ .ID }}\">{{ .Props.greeting }}, {{ with .Ctx.user }}{{ .Name }}{{ else }}guest{{ end }} ({{ .Ctx.accent }})</span>",
		"components/Boom.html":  "---\nbrick:\n  island: true\n---\n{{ if .Ctx.explode }}{{ index .Ctx.nope 1 }}{{ end }}ok",
		"content/a.html":        `<p>static</p><Hello greeting="Hello" />`,
		"content/b.html":        `<brick:boundary><Boom /><brick:prop name="fallback">F</brick:prop></brick:boundary>|<Boom />`,
		"content/bad.html":      `<Hello><Hello /></Hello>`,
	}
}

func islandEngine(t *testing.T, mode Mode) *Engine {
	t.Helper()
	var pairs []string
	for k, v := range islandTheme() {
		pairs = append(pairs, k, v)
	}
	return newEngine(t, Options{Mode: mode, FS: theme(pairs...)})
}

func TestIslandsRenderPerRequest(t *testing.T) {
	t.Parallel()
	e := islandEngine(t, Prod)
	ctx := context.Background()
	r, err := e.RenderChunks(ctx, "content/a.html", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Bytes(); !errors.Is(err, ErrIslands) {
		t.Fatalf("Bytes on a result with islands: %v", err)
	}
	var isl *Island
	for _, c := range r.Chunks {
		if c.Kind == ChunkIsland {
			isl = c.Island
		}
	}
	if isl == nil || isl.Props["greeting"] != "Hello" || isl.Ctx["accent"] != "blue" || !strings.Contains(strings.Join(r.Files, " "), "components/Hello.html") {
		t.Fatalf("island record: %+v files=%v", isl, r.Files)
	}
	// The same cached Result, filled for two different requests.
	sara, err := e.Fill(ctx, r, Provide("user", user{"Sara"}))
	if err != nil {
		t.Fatal(err)
	}
	anon, err := e.Fill(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	want := `<html><p>static</p><span data-id="` + isl.ID + `">Hello, Sara (blue)</span></html>`
	if string(sara) != want || !strings.Contains(string(anon), "Hello, guest (blue)") {
		t.Fatalf("fill:\n%s\n%s", sara, anon)
	}
	// Render is RenderChunks then Fill.
	direct, err := e.Render(ctx, "content/a.html", nil, Provide("user", user{"Sara"}))
	if err != nil || string(direct) != want {
		t.Fatalf("render: %s %v", direct, err)
	}
	if err := renderErr(t, e, "content/bad.html"); !strings.Contains(err.Error(), "inside the children of another one") {
		t.Fatalf("an island inside an island's children: %v", err)
	}
}

func TestIslandFailures(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	prod := islandEngine(t, Prod)
	r, err := prod.RenderChunks(ctx, "content/b.html", nil)
	if err != nil {
		t.Fatal(err)
	}
	if out, err := prod.Fill(ctx, r); err != nil || string(out) != "<html>ok|ok</html>" {
		t.Fatalf("healthy: %q %v", out, err)
	}
	// The unguarded island fails the fill in Prod.
	if out, err := prod.Fill(ctx, r, Provide("explode", true)); err == nil || out != nil {
		t.Fatalf("prod failure: %q %v", out, err)
	}
	// Guard both: only the boundary's island falls back.
	dev := islandEngine(t, Dev)
	r2, _ := dev.RenderChunks(ctx, "content/b.html", nil)
	out, err := dev.Fill(ctx, r2, Provide("explode", true))
	if err == nil || !strings.Contains(string(out), `class="brick-error"`) || !strings.Contains(string(out), "F|") {
		t.Fatalf("dev failure: %q %v", out, err)
	}
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := prod.Fill(cctx, r); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
}
