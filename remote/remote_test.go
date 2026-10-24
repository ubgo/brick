package remote

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ubgo/brick"
)

// kit is a fake remote repository: path -> content, for one version.
var kit = map[string]string{
	"components/Card.html": "---\nuse:\n  Icon: ~/components/Icon.html\n  Badge: github.com/acme/badges/Badge.html@v2.0.0\n---\n<div class=\"card\"><Icon /><Badge />{{ children }}</div>",
	"components/Icon.html": `<i>★</i>`,
}

func project(t *testing.T, files ...string) string {
	t.Helper()
	dir := t.TempDir()
	for i := 0; i+1 < len(files); i += 2 {
		p := filepath.Join(dir, filepath.FromSlash(files[i]))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(files[i+1]), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestVendorAndRender(t *testing.T) {
	t.Parallel()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		switch {
		case r.URL.Path == "/acme/badges/v2.0.0/Badge.html":
			_, _ = w.Write([]byte(`<em>new</em>`))
		case strings.HasPrefix(r.URL.Path, "/acme/kit/v1.2.0/"):
			if b, ok := kit[strings.TrimPrefix(r.URL.Path, "/acme/kit/v1.2.0/")]; ok {
				_, _ = w.Write([]byte(b))
				return
			}
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	fetch := func(ctx context.Context, r brick.Remote) ([]byte, error) {
		return Get(ctx, srv.Client(), srv.URL+"/"+r.Owner+"/"+r.Repo+"/"+r.Version+"/"+r.Path)
	}
	dir := project(t,
		"content/index.html", "---\nuse:\n  Card: github.com/acme/kit/components/Card.html@v1.2.0\n---\n<Card>hi</Card>",
	)
	ctx := context.Background()
	rep, err := Vendor(ctx, Options{Dir: dir, Fetch: fetch})
	if err != nil {
		t.Fatal(err)
	}
	want := "github.com/acme/badges/Badge.html@v2.0.0 github.com/acme/kit/components/Card.html@v1.2.0 github.com/acme/kit/components/Icon.html@v1.2.0"
	if strings.Join(rep.Files, " ") != want || rep.Fetched != 3 {
		t.Fatalf("vendored: %v (%d fetched)", rep.Files, rep.Fetched)
	}
	// The engine renders from brick_modules, offline.
	eng, err := brick.New(brick.Options{FS: os.DirFS(dir)})
	if err != nil {
		t.Fatal(err)
	}
	out, err := eng.Render(ctx, "content/index.html", nil)
	if err != nil || string(out) != `<div class="card"><i>★</i><em>new</em>hi</div>` {
		t.Fatalf("render: %s %v", out, err)
	}
	if err := Check(Options{Dir: dir}); err != nil {
		t.Fatalf("check: %v", err)
	}
	// A second vendor fetches nothing.
	before := hits.Load()
	if rep, err := Vendor(ctx, Options{Dir: dir, Fetch: fetch}); err != nil || rep.Fetched != 0 || hits.Load() != before {
		t.Fatalf("re-vendor fetched: %+v %v", rep, err)
	}
	// Tampering with a vendored file fails the check.
	icon := filepath.Join(dir, "brick_modules/github.com/acme/kit@v1.2.0/components/Icon.html")
	if err := os.WriteFile(icon, []byte("<i>evil</i>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Check(Options{Dir: dir}); !errors.Is(err, ErrMismatch) || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("tamper: %v", err)
	}
	// Vendor restores it.
	if _, err := Vendor(ctx, Options{Dir: dir, Fetch: fetch}); err != nil {
		t.Fatal(err)
	}
	if err := Check(Options{Dir: dir}); err != nil {
		t.Fatalf("after re-vendor: %v", err)
	}
	// A new import not yet vendored fails the check.
	if err := os.WriteFile(filepath.Join(dir, "content/more.html"), []byte("---\nuse:\n  X: github.com/acme/kit/components/X.html@v1.2.0\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Check(Options{Dir: dir}); !errors.Is(err, ErrMismatch) || !strings.Contains(err.Error(), "not vendored") {
		t.Fatalf("new import: %v", err)
	}
	if _, err := Vendor(ctx, Options{Dir: dir, Fetch: fetch}); err == nil {
		t.Fatal("a 404 import vendored")
	}
}

func TestGitHubOnly(t *testing.T) {
	t.Parallel()
	_, err := GitHub(nil)(context.Background(), brick.Remote{Host: "gitlab.com", Owner: "a", Repo: "b", Path: "c.html", Version: "v1"})
	if err == nil || !strings.Contains(err.Error(), "only github.com") {
		t.Fatalf("other host: %v", err)
	}
}

func TestLockParse(t *testing.T) {
	t.Parallel()
	dir := project(t, "brick.lock", "# comment\ngithub.com/a/b/c.html@v1 sha256-00\n")
	m, err := readLock(dir)
	if err != nil || m["github.com/a/b/c.html@v1"] != "sha256-00" {
		t.Fatalf("%v %v", m, err)
	}
	bad := project(t, "brick.lock", "nonsense\n")
	if _, err := readLock(bad); err == nil {
		t.Fatal("bad lock accepted")
	}
}

// repo is a fake GitHub over HTTP: raw files at /raw/{owner}/{repo}/{version}/{path}
// and the git-trees API at /repos/{owner}/{repo}/git/trees/{version}, both
// served from files ("owner/repo@version" -> path -> content). hits counts
// every request.
type repo struct {
	files map[string]map[string]string
	hits  atomic.Int32
	srv   *httptest.Server
}

func newRepo(t *testing.T, files map[string]map[string]string) *repo {
	t.Helper()
	r := &repo{files: files}
	r.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.hits.Add(1)
		p := req.URL.Path
		if rest, ok := strings.CutPrefix(p, "/raw/"); ok {
			parts := strings.SplitN(rest, "/", 4)
			if len(parts) == 4 {
				if b, ok := r.files[parts[0]+"/"+parts[1]+"@"+parts[2]][parts[3]]; ok {
					_, _ = w.Write([]byte(b))
					return
				}
			}
			http.NotFound(w, req)
			return
		}
		if rest, ok := strings.CutPrefix(p, "/repos/"); ok {
			parts := strings.Split(rest, "/")
			if len(parts) == 5 && parts[2] == "git" && parts[3] == "trees" {
				repo, ok := r.files[parts[0]+"/"+parts[1]+"@"+parts[4]]
				if !ok {
					http.NotFound(w, req)
					return
				}
				var entries []string
				for f := range repo {
					entries = append(entries, `{"path":"`+f+`","type":"blob"}`, `{"path":"`+path.Dir(f)+`","type":"tree"}`)
				}
				_, _ = w.Write([]byte(`{"tree":[` + strings.Join(entries, ",") + `],"truncated":false}`))
				return
			}
		}
		http.NotFound(w, req)
	}))
	t.Cleanup(r.srv.Close)
	return r
}

// options are Vendor/Check options fetching and listing from the fake repo.
func (r *repo) options(dir string) Options {
	return Options{
		Dir: dir,
		Fetch: func(ctx context.Context, rm brick.Remote) ([]byte, error) {
			return Get(ctx, r.srv.Client(), r.srv.URL+"/raw/"+rm.Owner+"/"+rm.Repo+"/"+rm.Version+"/"+rm.Path)
		},
		List: gitHubList(r.srv.Client(), r.srv.URL),
	}
}

// render renders page with the project's brick.yaml applied, offline.
func render(t *testing.T, dir, page string) string {
	t.Helper()
	opts := brick.Options{FS: os.DirFS(dir)}
	if cfg, err := brick.LoadConfig(os.DirFS(dir), ConfigFile); err == nil {
		if err := cfg.Apply(&opts); err != nil {
			t.Fatal(err)
		}
	}
	eng, err := brick.New(opts)
	if err != nil {
		t.Fatal(err)
	}
	out, err := eng.Render(context.Background(), page, nil)
	if err != nil {
		t.Fatalf("render %s: %v", page, err)
	}
	return string(out)
}

// uiRepo is a monorepo whose ui/ folder is a library that imports a single
// file from another repository.
var uiRepo = map[string]map[string]string{
	"acme/mono@v1.0.0": {
		"ui/brick-library.yaml":     "name: acme-ui\nglobal: [Theme]\n",
		"ui/components/Button.html": "---\nuse:\n  Icon: ~/components/Icon.html\n  Star: github.com/acme/stars/Star.html@v2.0.0\n  icons: ~/data/icons.yaml\n---\n<button><Icon /><Star />{{ .Data.icons.x }}{{ children }}</button>",
		"ui/components/Icon.html":   "<i>i</i>",
		"ui/components/Theme.html":  "<brick:style>.t { color: red }</brick:style><template class=\"t\"></template>",
		"ui/data/icons.yaml":        "x: X\n",
		"docs/README.md":            "not part of the library",
		"components/Unrelated.html": "<b>outside the library folder</b>",
	},
	"acme/stars@v2.0.0": {"Star.html": "<em>*</em>"},
}

// A remote library in brick.yaml is vendored whole — every file under its
// folder, pinned — with its own remote imports, and pages use its
// components by name. Re-vendoring makes no requests; tampering, extra
// files and a new unvendored library all fail Check.
func TestVendorLibrary(t *testing.T) {
	t.Parallel()
	rp := newRepo(t, uiRepo)
	dir := project(t,
		"brick.yaml", "libraries: [github.com/acme/mono/ui@v1.0.0]\n",
		"content/index.html", "<Button>go</Button>",
	)
	ctx := context.Background()
	rep, err := Vendor(ctx, rp.options(dir))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"github.com/acme/mono/ui/brick-library.yaml@v1.0.0", "github.com/acme/mono/ui/components/Button.html@v1.0.0",
		"github.com/acme/mono/ui/components/Icon.html@v1.0.0", "github.com/acme/mono/ui/components/Theme.html@v1.0.0",
		"github.com/acme/mono/ui/data/icons.yaml@v1.0.0", "github.com/acme/stars/Star.html@v2.0.0",
	}
	if strings.Join(rep.Files, "\n") != strings.Join(want, "\n") || rep.Fetched != len(want) {
		t.Fatalf("vendored %d:\n%s", rep.Fetched, strings.Join(rep.Files, "\n"))
	}
	if got := render(t, dir, "content/index.html"); !strings.Contains(got, "<button><i>i</i><em>*</em>Xgo</button>") {
		t.Fatalf("render: %s", got)
	}
	if err := Check(Options{Dir: dir}); err != nil {
		t.Fatalf("check: %v", err)
	}

	before := rp.hits.Load()
	if rep, err := Vendor(ctx, rp.options(dir)); err != nil || rep.Fetched != 0 || rp.hits.Load() != before {
		t.Fatalf("re-vendor made %d requests: %+v %v", rp.hits.Load()-before, rep, err)
	}

	mod := filepath.Join(dir, "brick_modules/github.com/acme/mono@v1.0.0/ui")
	if err := os.WriteFile(filepath.Join(mod, "components/Icon.html"), []byte("<i>evil</i>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Check(Options{Dir: dir}); !errors.Is(err, ErrMismatch) || !strings.Contains(err.Error(), "ui/components/Icon.html@v1.0.0: vendored file does not match") {
		t.Fatalf("tamper: %v", err)
	}
	if _, err := Vendor(ctx, rp.options(dir)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mod, "components/Extra.html"), []byte("<b>x</b>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Check(Options{Dir: dir}); !errors.Is(err, ErrMismatch) || !strings.Contains(err.Error(), "Extra.html@v1.0.0: in a vendored library but not in brick.lock") {
		t.Fatalf("extra file: %v", err)
	}
	if err := os.Remove(filepath.Join(mod, "components/Extra.html")); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(dir, "brick.yaml"), []byte("libraries:\n  - github.com/acme/mono/ui@v1.0.0\n  - github.com/acme/other@v3.0.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Check(Options{Dir: dir}); !errors.Is(err, ErrMismatch) || !strings.Contains(err.Error(), "github.com/acme/other@v3.0.0: library not vendored") {
		t.Fatalf("new library: %v", err)
	}
	if _, err := Vendor(ctx, rp.options(dir)); err == nil || !strings.Contains(err.Error(), "list library github.com/acme/other@v3.0.0") {
		t.Fatalf("a library the host does not have: %v", err)
	}
}

// A folder listed as a library needs a manifest at its root.
func TestVendorLibraryNeedsManifest(t *testing.T) {
	t.Parallel()
	rp := newRepo(t, map[string]map[string]string{"acme/bare@v1": {"components/A.html": "<a></a>"}})
	dir := project(t, "brick.yaml", "libraries: [github.com/acme/bare@v1]\n")
	if _, err := Vendor(context.Background(), rp.options(dir)); err == nil || !strings.Contains(err.Error(), "has no brick-library.yaml at its root") {
		t.Fatalf("no manifest: %v", err)
	}
}

// A library folder that is also a Go package (kit.go embedding it) is
// vendored without its Go sources: brick never reads them, and a copy
// inside the host's Go module would break its build.
func TestVendorLibrarySkipsGoSources(t *testing.T) {
	t.Parallel()
	rp := newRepo(t, map[string]map[string]string{"acme/ui@v1": {
		"brick-library.yaml": "name: acme-ui\n",
		"components/A.html":  "<a></a>",
		"ui.go":              "package ui",
		"ui_test.go":         "package ui",
	}})
	dir := project(t, "brick.yaml", "libraries: [github.com/acme/ui@v1]\n")
	rep, err := Vendor(context.Background(), rp.options(dir))
	if err != nil {
		t.Fatal(err)
	}
	want := "github.com/acme/ui/brick-library.yaml@v1 github.com/acme/ui/components/A.html@v1"
	if got := strings.Join(rep.Files, " "); got != want {
		t.Fatalf("vendored:\n got  %s\n want %s", got, want)
	}
	if err := Check(Options{Dir: dir}); err != nil {
		t.Fatalf("check: %v", err)
	}
}

// A single imported component brings the nearest brick-library.yaml above
// it, so its ~/ imports resolve from its library root, as the engine
// resolves them; a repository with no manifest resolves from its root.
func TestSingleImportBringsManifest(t *testing.T) {
	t.Parallel()
	rp := newRepo(t, map[string]map[string]string{
		"acme/tools@v2.0.0": {
			"kit/brick-library.yaml":   "name: tools-kit\n",
			"kit/components/Card.html": "---\nuse:\n  Icon: ~/components/Icon.html\n---\n<div class=\"card\"><Icon />{{ children }}</div>",
			"kit/components/Icon.html": "<i>kit</i>",
			"components/Icon.html":     "<i>repo root — wrong</i>",
		},
	})
	dir := project(t, "content/index.html", "---\nuse:\n  Card: github.com/acme/tools/kit/components/Card.html@v2.0.0\n---\n<Card>hi</Card>")
	ctx := context.Background()
	rep, err := Vendor(ctx, rp.options(dir))
	if err != nil {
		t.Fatal(err)
	}
	want := "github.com/acme/tools/kit/brick-library.yaml@v2.0.0 github.com/acme/tools/kit/components/Card.html@v2.0.0 github.com/acme/tools/kit/components/Icon.html@v2.0.0"
	if got := strings.Join(rep.Files, " "); got != want {
		t.Fatalf("vendored:\n got  %s\n want %s", got, want)
	}
	if got := render(t, dir, "content/index.html"); got != `<div class="card"><i>kit</i>hi</div>` {
		t.Fatalf("render: %s", got)
	}
	if err := Check(Options{Dir: dir}); err != nil {
		t.Fatalf("check: %v", err)
	}
	before := rp.hits.Load()
	if rep, err := Vendor(ctx, rp.options(dir)); err != nil || rep.Fetched != 0 || rp.hits.Load() != before {
		t.Fatalf("re-vendor probed again: %d requests, %+v %v", rp.hits.Load()-before, rep, err)
	}
}

// The GitHub lister reads the git-trees API, keeps the files under the
// folder, and refuses a truncated tree or another host.
func TestGitHubList(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/acme/mono/git/trees/v1":
			if r.URL.Query().Get("recursive") != "1" {
				http.Error(w, "not recursive", http.StatusBadRequest)
				return
			}
			_, _ = w.Write([]byte(`{"tree":[{"path":"ui","type":"tree"},{"path":"ui/a.html","type":"blob"},{"path":"uix/b.html","type":"blob"},{"path":"c.html","type":"blob"}],"truncated":false}`))
		case "/repos/acme/huge/git/trees/v1":
			_, _ = w.Write([]byte(`{"tree":[],"truncated":true}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	list := gitHubList(srv.Client(), srv.URL)
	ctx := context.Background()
	got, err := list(ctx, brick.Remote{Host: "github.com", Owner: "acme", Repo: "mono", Path: "ui", Version: "v1"})
	if err != nil || strings.Join(got, ",") != "ui/a.html" {
		t.Fatalf("folder: %v %v", got, err)
	}
	if got, err := list(ctx, brick.Remote{Host: "github.com", Owner: "acme", Repo: "mono", Version: "v1"}); err != nil || len(got) != 3 {
		t.Fatalf("root: %v %v", got, err)
	}
	if _, err := list(ctx, brick.Remote{Host: "github.com", Owner: "acme", Repo: "huge", Version: "v1"}); err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("truncated: %v", err)
	}
	if _, err := list(ctx, brick.Remote{Host: "github.com", Owner: "acme", Repo: "gone", Version: "v1"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
	if _, err := GitHubList(nil)(ctx, brick.Remote{Host: "gitlab.com", Owner: "a", Repo: "b", Version: "v1"}); err == nil || !strings.Contains(err.Error(), "only github.com") {
		t.Fatalf("other host: %v", err)
	}
}

// A src= inside a template comment is an example, not an import (the
// engine reads a component in a comment as text); a real one is found.
func TestRefsSkipComments(t *testing.T) {
	t.Parallel()
	b := []byte("{{/*\n  <CodeBlock src=\"~/examples/main.go\" />\n*/}}\n{{- /* <X src=\"~/also/not.go\" /> */ -}}<CodeBlock src=\"~/real.go\" />")
	if got := strings.Join(refs(b, ".html"), ","); got != "~/real.go" {
		t.Fatalf("refs: %q", got)
	}
}
