package scriptstarlark

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/ubgo/brick"
)

// brick.not_found() in a loader fails the render with brick.ErrNotFound,
// through Module.Call's error flattening.
func TestNotFoundFromALoader(t *testing.T) {
	t.Parallel()
	e := engine(t, Options{}, files(
		"s/posts.star", "POSTS = {\"hello\": \"Hello\"}\ndef one(input):\n    slug = input[\"page\"][\"route\"][\"slug\"]\n    if slug not in POSTS:\n        brick.not_found()\n    return {\"title\": POSTS[slug]}\n",
		"content/post.html", "---\nuse: {posts: ~/s/posts.star}\nload: posts.one\n---\n{{ .Params.title }}",
	))
	ctx := context.Background()
	out, err := e.Render(ctx, "content/post.html", nil, brick.WithRoute(map[string]string{"slug": "hello"}, nil))
	if err != nil || string(out) != "Hello" {
		t.Fatalf("found: %q %v", out, err)
	}
	if _, err := e.RenderChunks(ctx, "content/post.html", nil, brick.WithRoute(map[string]string{"slug": "nope"}, nil)); !errors.Is(err, brick.ErrNotFound) {
		t.Fatalf("not found: %v", err)
	}
}

// brick.redirect, brick.error and brick.header from a loader reach the
// root's typed answers through Module.Call; bad arguments fail where the
// loader wrote them.
func TestLoaderAnswersFromStarlark(t *testing.T) {
	t.Parallel()
	src := `def one(input):
    do = input["page"]["route"]["do"]
    if do == "login":
        brick.redirect("/login/")
    if do == "moved":
        brick.redirect("/new/", status = 308)
    if do == "gone":
        brick.error(410, "Removed.")
    if do == "badstatus":
        brick.redirect("/x/", status = 301)
    if do == "open":
        brick.redirect("//evil.example/")
    if do == "badcode":
        brick.error(200)
    if do == "cookie":
        brick.header("Set-Cookie", "a=1")
    brick.header("cache-control", "public, max-age=60")
    return {"title": "ok"}
`
	e := engine(t, Options{}, files(
		"s/p.star", src,
		"content/p.html", "---\nuse: {p: ~/s/p.star}\nload: p.one\n---\n{{ .Params.title }}",
	))
	ctx := context.Background()
	run := func(do string) (*brick.Result, error) {
		return e.RenderChunks(ctx, "content/p.html", nil, brick.WithRoute(map[string]string{"do": do}, nil))
	}
	var rd *brick.Redirect
	if _, err := run("login"); !errors.As(err, &rd) || rd.URL != "/login/" || rd.Status != 303 {
		t.Fatalf("login: %v", err)
	}
	if _, err := run("moved"); !errors.As(err, &rd) || rd.Status != 308 {
		t.Fatalf("moved: %v", err)
	}
	var he *brick.HTTPError
	if _, err := run("gone"); !errors.As(err, &he) || he.Status != 410 || he.Message != "Removed." {
		t.Fatalf("gone: %v", err)
	}
	for _, do := range []string{"badstatus", "open", "badcode", "cookie"} {
		_, err := run(do)
		if err == nil || brick.IsAnswer(err) {
			t.Fatalf("%s: want a script error, got %v", do, err)
		}
	}
	res, err := run("ok")
	if err != nil || res.Headers.Get("Cache-Control") != "public, max-age=60" {
		t.Fatalf("header: %v %v", res, err)
	}
}

// Inside an action function brick.redirect is still the effect; status=
// is a loader's argument and is refused there.
func TestRedirectInAnActionStaysAnEffect(t *testing.T) {
	t.Parallel()
	sc, err := New(Options{})
	if err != nil {
		t.Fatal(err)
	}
	e, err := brick.New(brick.Options{Scripting: sc, States: brick.NewMemoryStates(0, 0), FS: files(
		"components/Go.html", "---\nstate: {n: 0}\n---\n<brick:script>\ndef go(state, props):\n    brick.redirect(\"/next/\")\n    return state\n\ndef bad(state, props):\n    brick.redirect(\"/next/\", status = 302)\n    return state\n</brick:script><div>x</div>",
		"content/a.html", "<Go />",
	)})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	page, err := e.Render(ctx, "content/a.html", nil)
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`data-brick-state="([^"]+)" data-brick-token="([0-9a-f]+)"`).FindStringSubmatch(string(page))
	res, err := e.RunAction(ctx, brick.ActionRequest{Handle: m[1], Token: m[2], Action: "go"})
	if err != nil || len(res.Effects) != 1 || res.Effects[0].Kind != brick.EffectRedirect || res.Effects[0].URL != "/next/" {
		t.Fatalf("go: %+v %v", res, err)
	}
	m = regexp.MustCompile(`data-brick-state="([^"]+)" data-brick-token="([0-9a-f]+)"`).FindStringSubmatch(string(res.HTML))
	if _, err := e.RunAction(ctx, brick.ActionRequest{Handle: m[1], Token: m[2], Action: "bad"}); err == nil || !strings.Contains(err.Error(), "status= is for a loader") {
		t.Fatalf("bad: %v", err)
	}
}
