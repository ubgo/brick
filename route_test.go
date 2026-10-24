package brick

import (
	"context"
	"errors"
	"strings"
	"testing"
	"testing/fstest"
)

// routeScripting is a module whose loader echoes page.route.slug, or
// answers ErrNotFound for "gone"; slugs() is a paths: list.
type routeScripting struct{}

func (routeScripting) Compile(context.Context, ScriptSource) (ScriptModule, error) {
	return routeModule{}, nil
}

type routeModule struct{}

func (routeModule) Functions() []string { return []string{"one", "slugs"} }

func (routeModule) Call(_ context.Context, fn string, args ...any) (any, error) {
	if fn == "slugs" {
		return []any{map[string]any{"slug": "a"}}, nil
	}
	in, _ := args[0].(map[string]any)
	pg, _ := in["page"].(map[string]any)
	route, _ := pg["route"].(map[string]any)
	if route["slug"] == "gone" {
		return nil, ErrNotFound
	}
	return map[string]any{"seen": route["slug"], "title": pg["title"]}, nil
}

// errorRecorder is an ErrorHook that records what it is told.
type errorRecorder struct{ errs *[]error }

func (r errorRecorder) OnError(_ context.Context, err error) { *r.errs = append(*r.errs, err) }

// WithRoute reaches .Page.Route, .Page.RouteParts and the loader's
// page.route; a loader's ErrNotFound fails the render with that sentinel
// and is not reported as an error.
func TestWithRouteAndNotFound(t *testing.T) {
	t.Parallel()
	var reported []error
	e, err := New(Options{
		FS: fstest.MapFS{
			"s/posts.star":        {Data: []byte("#")},
			"content/[slug].html": {Data: []byte("---\ntitle: T\nuse: {posts: ~/s/posts.star}\nload: posts.one\n---\n{{ .Params.seen }}|{{ .Params.title }}|{{ .Page.Route.slug }}|{{ .Page.RouteParts }}")},
		},
		Scripting: routeScripting{},
		Hooks:     []any{errorRecorder{errs: &reported}},
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	out, err := e.Render(ctx, "content/[slug].html", nil, WithRoute(map[string]string{"slug": "hi"}, []string{"hi"}))
	if err != nil || string(out) != "hi|T|hi|[hi]" {
		t.Fatalf("got %q, %v", out, err)
	}
	_, err = e.RenderChunks(ctx, "content/[slug].html", nil, WithRoute(map[string]string{"slug": "gone"}, nil))
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("not found: %v", err)
	}
	if len(reported) != 0 {
		t.Fatalf("a not-found was reported as an error: %v", reported)
	}
	entries, err := e.RoutePaths(ctx, "content/[slug].html", "posts.slugs")
	if err != nil || len(entries) != 1 || entries[0]["slug"] != "a" {
		t.Fatalf("RoutePaths: %v %v", entries, err)
	}
	if _, err := e.RoutePaths(ctx, "content/[slug].html", "posts.nope"); err == nil || !strings.Contains(err.Error(), "posts.nope") {
		t.Fatalf("RoutePaths of a missing function: %v", err)
	}
}
