package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/ubgo/brick"
	"github.com/ubgo/brick/examples/demosite"
	"github.com/ubgo/brick/remote"
	"github.com/ubgo/brick/site"
)

// get fetches a URL from the demo's handler.
func get(t *testing.T, h http.Handler, url string) (int, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url, nil))
	return rec.Code, rec.Body.String()
}

func mustContain(t *testing.T, page, body string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(body, w) {
			t.Errorf("%s is missing %q", page, w)
		}
	}
}

// The demo site is the worked example of every feature: each page is
// checked for the output that shows its feature working.
func TestDemoSite(t *testing.T) {
	ctx := context.Background()
	s, err := newSite(ctx, "site", false)
	if err != nil {
		t.Fatal(err)
	}
	diags, err := s.Check(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range diags {
		t.Errorf("check: %s", d)
	}
	h := s.Handler()

	t.Run("home: components, slots, raw, i18n, scripts, hook", func(t *testing.T) {
		code, body := get(t, h, "/")
		if code != 200 {
			t.Fatalf("status %d", code)
		}
		mustContain(t, "/", body,
			`<meta name="generator" content="brick">`,
			`class="card card--featured" id="pro" data-plan="pro"`, "12,000 per month, billed-yearly.",
			`<a href="/blog/hello/">Hello</a>`, `<button role="tab">Go</button><button role="tab">Rust</button>`,
			"if x &lt; 10 &amp;&amp; y &gt; 2 {", `data-brick-type="Counter"`, "♥ 0",
			`<brick-assets><script type="module" src="/_brick/c/runtime.`, `/_brick/c/nav.`,
			`brick:prefetch`, `id="player" brick:persist`, `brick:nav="off"`)
	})

	t.Run("islands: the cached page, filled per request", func(t *testing.T) {
		_, anon := get(t, h, "/features/islands/")
		_, sara := get(t, h, "/features/islands/?as=Sara")
		mustContain(t, "anonymous", anon, `<a href="?as=Sara">Sign in</a>`)
		mustContain(t, "signed in", sara, "Hi, Sara")
	})

	t.Run("blog: .Site.Pages, markdown, collections, dates", func(t *testing.T) {
		_, list := get(t, h, "/blog/")
		mustContain(t, "/blog/", list, `<a href="/blog/second/">A markdown post</a> · 28 September 2026`, `<a href="/blog/hello/">Hello</a> · 27 September 2026`)
		if strings.Index(list, "second") > strings.Index(list, "/blog/hello/") {
			t.Error("newest first")
		}
		_, md := get(t, h, "/blog/second/")
		mustContain(t, "markdown page", md, "<strong>markdown</strong>", "{{ .Title }}")
		if strings.Contains(md, "<Card>") {
			t.Error("raw HTML inside markdown is dropped, never rendered")
		}
		_, hello := get(t, h, "/blog/hello/")
		mustContain(t, "/blog/hello/", hello, "Posted 27 Sep 2026.", "<blockquote>Less, but better.</blockquote>")
	})

	t.Run("locales: per-language URLs and variants", func(t *testing.T) {
		_, en := get(t, h, "/about/")
		_, fr := get(t, h, "/fr/about/")
		_, frHome := get(t, h, "/fr/")
		mustContain(t, "/about/", en, `<html lang="en">`, "About us")
		mustContain(t, "/fr/about/", fr, `<html lang="fr">`, "Page écrite à la main")
		mustContain(t, "/fr/", frHome, "Bienvenue")
	})

	t.Run("live: a polled stateful component with a host capability", func(t *testing.T) {
		_, body := get(t, h, "/features/live/")
		mustContain(t, "/features/live/", body, `brick:poll="2s"`, "<time>", "data-brick-state=")
	})

	t.Run("data: loader, boundary, spread, sanitize", func(t *testing.T) {
		_, body := get(t, h, "/features/data/")
		mustContain(t, "/features/data/", body,
			`<ul class="recent"><li>Hello</li><li>Second post</li></ul>`,
			`<p class="fallback">This widget is unavailable.</p>`,
			`<button class="primary" hx-post="/buy" id="buy">Buy</button>`,
			"<b>Sara</b>")
		if strings.Contains(body, "<script>alert") {
			t.Error("sanitize kept a script")
		}
	})

	t.Run("kit: a remote library, vendored, prefixed", func(t *testing.T) {
		_, body := get(t, h, "/features/kit/")
		mustContain(t, "/features/kit/", body, `role="tablist" aria-label="Install"`, `<summary>Why brick?</summary>`, `name="faq"`, "bk-track", "/_brick/c/Tabs.", "/_brick/c/Carousel.",
			// The manifest's global Theme: the kit's tokens reach the page with no <Theme /> in the layout.
			"/_brick/c/Theme.")
		if err := remote.Check(remote.Options{Dir: "site"}); err != nil {
			t.Fatalf("brick_modules does not match brick.lock: %v", err)
		}
	})

	t.Run("images: resized copies and srcset", func(t *testing.T) {
		_, body := get(t, h, "/features/images/")
		mustContain(t, "/features/images/", body, `-160.png 160w, /_brick/img/team.`, `-320.png 320w"`, `width="320" height="160"`)
		url := regexp.MustCompile(`src="(/_brick/img/[^"]+)"`).FindStringSubmatch(body)
		if url == nil {
			t.Fatal("no image URL")
		}
		if code, _ := get(t, h, url[1]); code != 200 {
			t.Fatalf("resized image not served: %d", code)
		}
	})

	t.Run("client: one component's module imports another's", func(t *testing.T) {
		_, body := get(t, h, "/features/client/")
		url := regexp.MustCompile(`src="(/_brick/c/Opener\.[^"]+)"`).FindStringSubmatch(body)
		if url == nil {
			t.Fatalf("no Opener module in %s", body)
		}
		_, js := get(t, h, url[1])
		mustContain(t, "Opener module", js, `from "./Modal.`)
	})

	t.Run("static, 404", func(t *testing.T) {
		if code, body := get(t, h, "/robots.txt"); code != 200 || !strings.Contains(body, "User-agent") {
			t.Fatalf("robots.txt: %d", code)
		}
		if code, body := get(t, h, "/no/such/page/"); code != 404 || !strings.Contains(body, "There is no page here") {
			t.Fatalf("404: %d", code)
		}
	})
}

// kitVendored is where the demo's remote kit library is vendored, and
// kitSource the repository's kit it must match.
const (
	kitVendored = "site/brick_modules/github.com/ubgo/brick@main/kit"
	kitSource   = "../kit"
)

// The vendored kit library is the repository's current kit, file for file
// — manifest, components and data; not the Go wrapper, which vendorkit
// leaves out — and brick.lock pins exactly that (remote.Check). A change to
// kit/ must be re-vendored (task examples:vendor), or this fails.
func TestVendoredKitIsCurrent(t *testing.T) {
	files := func(root string) map[string]string {
		t.Helper()
		out := map[string]string{}
		err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || filepath.Ext(p) == ".go" || strings.HasPrefix(d.Name(), ".") {
				return nil
			}
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(root, p)
			if err != nil {
				return err
			}
			out[filepath.ToSlash(rel)] = string(b)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	want, got := files(kitSource), files(kitVendored)
	for name, b := range want {
		if got[name] != b {
			t.Errorf("%s differs from ../kit: run `task examples:vendor`", name)
		}
	}
	for name := range got {
		if _, ok := want[name]; !ok {
			t.Errorf("%s is vendored but no longer in ../kit: run `task examples:vendor`", name)
		}
	}
	if _, ok := got["brick-library.yaml"]; !ok {
		t.Error("the vendored kit has no brick-library.yaml: it is not a library")
	}
	if err := remote.Check(remote.Options{Dir: "site"}); err != nil {
		t.Errorf("brick.lock: %v", err)
	}
}

// Export writes the whole site; the output is deterministic.
// Its stateful pages are sealed with BRICK_SECRET; without it Export
// refuses them rather than write handles no server opens.
func TestDemoExport(t *testing.T) {
	ctx := context.Background()
	t.Setenv(demosite.SecretEnv, "")
	if s, err := newSite(ctx, "site", false); err != nil {
		t.Fatal(err)
	} else if _, err := s.Export(ctx, filepath.Join(t.TempDir(), "nokey")); !errors.Is(err, site.ErrExportNeedsKeys) {
		t.Fatalf("export without a key: %v", err)
	}
	key, err := brick.NewStateKey()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(demosite.SecretEnv, brick.EncodeStateKey(key))
	s, err := newSite(ctx, "site", false)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "out")
	rep, err := s.Export(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Pages != 22 || rep.Static < 2 {
		t.Fatalf("report: %+v", rep)
	}
	f, err := os.Open(filepath.Join(dir, "fr", "about", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(f)
	_ = f.Close()
	if !strings.Contains(string(b), "Page écrite à la main") {
		t.Fatalf("fr/about: %s", b)
	}
}
