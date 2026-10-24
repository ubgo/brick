package site

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// Routes and Patterns carry each page's listing fields and frontmatter
// (G17), the brick: map left out, and hand out copies.
func TestRouteMetadata(t *testing.T) {
	s := newSite(t, project(
		"layouts/default.html", "<html>{{ children }}</html>",
		"content/index.html", "---\ntitle: Home\nmenu: {name: home, weight: 1}\n---\nhi",
		"content/blog/hello.html", "---\ntitle: Hello\ndescription: First post\ndate: 2026-10-01\ndraft: true\nname: blog.hello\ntags: [go, web]\nbrick: {sitemap: false}\n---\nhello",
		"content/posts/[slug].html", "---\ntitle: A post\nmenu: {name: posts}\n---\npost",
		"content/feed.xml.html", "<feed/>",
	), Options{Drafts: true}) // a draft is routed only with Drafts

	byURL := map[string]Route{}
	for _, r := range append(s.Routes(), s.Patterns()...) {
		byURL[r.URL] = r
	}
	home := byURL["/"]
	if home.Title != "Home" || home.Dynamic || home.Output != "" {
		t.Fatalf("home route: %+v", home)
	}
	if menu, _ := home.Meta["menu"].(map[string]any); menu["name"] != "home" {
		t.Fatalf("home Meta: %v", home.Meta)
	}
	hello := byURL["/blog/hello/"]
	if hello.Title != "Hello" || hello.Description != "First post" || !hello.Draft || hello.Section != "blog" || hello.Meta["name"] != "blog.hello" {
		t.Fatalf("hello route: %+v", hello)
	}
	if !hello.Date.Equal(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("hello Date: %v", hello.Date)
	}
	if _, ok := hello.Meta[engineKey]; ok {
		t.Fatalf("Meta carries the engine's brick: map: %v", hello.Meta)
	}
	post := byURL["/posts/[slug]/"]
	if !post.Dynamic || strings.Join(post.Params, ",") != "slug" || post.Title != "A post" || post.Section != "posts" {
		t.Fatalf("pattern route: %+v", post)
	}
	if feed := byURL["/feed.xml"]; feed.Output != "xml" || feed.Dynamic {
		t.Fatalf("output route: %+v", feed)
	}

	// A caller changing what it got never reaches the index.
	hello.Meta["name"] = "changed"
	hello.Meta["tags"].([]any)[0] = "changed"
	post.Meta["menu"].(map[string]any)["name"] = "changed"
	post.Params[0] = "changed"
	for _, r := range append(s.Routes(), s.Patterns()...) {
		switch r.URL {
		case "/blog/hello/":
			if r.Meta["name"] != "blog.hello" || r.Meta["tags"].([]any)[0] != "go" {
				t.Fatalf("Routes shares its Meta with the caller: %v", r.Meta)
			}
		case "/posts/[slug]/":
			if r.Meta["menu"].(map[string]any)["name"] != "posts" || r.Params[0] != "slug" {
				t.Fatalf("Patterns shares its Meta or Params with the caller: %v %v", r.Meta, r.Params)
			}
		}
	}

	// The JSON (brick routes --json) gains the fields and keeps the old ones.
	b, err := json.Marshal(s.Patterns()[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"URL":"/posts/[slug]/"`, `"Params":["slug"]`, `"Dynamic":true`, `"Title":"A post"`, `"Meta":{`} {
		if !strings.Contains(string(b), want) {
			t.Fatalf("route JSON lacks %s: %s", want, b)
		}
	}
	if b, _ := json.Marshal(byURL["/feed.xml"]); strings.Contains(string(b), "Date") || strings.Contains(string(b), "Dynamic") {
		t.Fatalf("zero fields are not omitted: %s", b)
	}
}
