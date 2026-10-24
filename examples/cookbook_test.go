package main

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"html"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/ubgo/brick"
	"github.com/ubgo/brick/bricktest"
	scriptstarlark "github.com/ubgo/brick/contrib/script-starlark"
	"github.com/ubgo/brick/site"
)

// cookbookPath is the public cookbook; every recipe in it is executed here,
// so a documented output can never drift from what brick really renders.
const cookbookPath = "../docs/cookbook.md"

// recipe is one cookbook section, split into its titled code blocks.
type recipe struct {
	blocks map[string]string
}

// titledBlockRE matches "`title`", a blank line, then a fenced block.
var titledBlockRE = regexp.MustCompile("(?s)`([^`\n]+)`\n\n```[a-z]*\n(.*?)\n```")

// parseCookbook splits the cookbook into recipes by `## ` heading.
func parseCookbook(t *testing.T) map[string]recipe {
	t.Helper()
	b, err := os.ReadFile(cookbookPath)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]recipe{}
	for _, sec := range strings.Split(string(b), "\n## ")[1:] {
		head, body, _ := strings.Cut(sec, "\n")
		r := recipe{blocks: map[string]string{}}
		for _, m := range titledBlockRE.FindAllStringSubmatch(body, -1) {
			r.blocks[m[1]] = m[2]
		}
		out[head] = r
	}
	return out
}

// fileTitleRE is a block title that names a project file.
var fileTitleRE = regexp.MustCompile(`^[A-Za-z0-9_./\[\]-]+\.(html|yaml|json|star|go)$`)

// theme builds the recipe's project from its file blocks.
func (r recipe) theme(extra ...string) fstest.MapFS {
	fsys := fstest.MapFS{}
	for title, body := range r.blocks {
		if fileTitleRE.MatchString(title) && title != "main.go" && !strings.HasSuffix(title, "_test.go") {
			fsys[title] = &fstest.MapFile{Data: []byte(body + "\n")}
		}
	}
	for i := 0; i+1 < len(extra); i += 2 {
		fsys[extra[i]] = &fstest.MapFile{Data: []byte(extra[i+1])}
	}
	return fsys
}

// tagSpaceRE and spaceRE tidy whitespace the way the cookbook does.
var (
	tagSpaceRE = regexp.MustCompile(`>\s+`)
	preSpaceRE = regexp.MustCompile(`\s+<`)
	spaceRE    = regexp.MustCompile(`\s+`)
)

// tidy removes whitespace next to tags and collapses the rest, so a
// documented output indented for reading equals the real bytes.
func tidy(s string) string {
	s = tagSpaceRE.ReplaceAllString(s, ">")
	s = preSpaceRE.ReplaceAllString(s, "<")
	return strings.TrimSpace(spaceRE.ReplaceAllString(s, " "))
}

func expectOutput(t *testing.T, name string, got []byte, err error, want string) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if tidy(string(got)) != tidy(want) {
		t.Fatalf("%s:\n got  %s\n want %s", name, tidy(string(got)), tidy(want))
	}
}

func mustEngine(t *testing.T, opts brick.Options) *brick.Engine {
	t.Helper()
	e, err := brick.New(opts)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func TestCookbook(t *testing.T) {
	book := parseCookbook(t)
	ctx := context.Background()
	get := func(t *testing.T, name string) recipe {
		t.Helper()
		r, ok := book[name]
		if !ok {
			t.Fatalf("no recipe %q in %s", name, cookbookPath)
		}
		return r
	}

	t.Run("smallest", func(t *testing.T) {
		r := get(t, "The smallest possible site")
		out, err := mustEngine(t, brick.Options{FS: r.theme()}).Render(ctx, "content/index.html", nil)
		expectOutput(t, "index", out, err, r.blocks["output"])
	})

	t.Run("blog", func(t *testing.T) {
		r := get(t, "A blog home page")
		fsys := r.theme()
		site, err := brick.LoadSite(fsys, "site.yaml", "")
		if err != nil {
			t.Fatal(err)
		}
		out, err := mustEngine(t, brick.Options{FS: fsys, Site: site}).Render(ctx, "content/index.html", nil)
		expectOutput(t, "home", out, err, r.blocks["output"])
	})

	t.Run("widget", func(t *testing.T) {
		r := get(t, "A sidebar widget that fetches its own data")
		fsys := r.theme("content/page.html", r.blocks["any page"])
		for _, tc := range []struct {
			name string
			db   *sql.DB
			want string
		}{
			{"up", sql.OpenDB(postsDB{}), r.blocks["output: database up"]},
			{"down", sql.OpenDB(postsDB{down: true}), r.blocks["output: database down"]},
		} {
			sc, err := scriptstarlark.New(scriptstarlark.Options{DB: tc.db})
			if err != nil {
				t.Fatal(err)
			}
			res, err := mustEngine(t, brick.Options{FS: fsys, Scripting: sc}).RenderChunks(ctx, "content/page.html", nil)
			if err != nil {
				t.Fatal(err)
			}
			out, err := res.Bytes()
			expectOutput(t, tc.name, out, err, tc.want)
			if tc.name == "down" && len(res.Errors) != 1 {
				t.Fatalf("down: the contained error was not reported: %v", res.Errors)
			}
		}
	})

	t.Run("cms", func(t *testing.T) {
		r := get(t, "A page built from CMS blocks")
		out, err := mustEngine(t, brick.Options{FS: r.theme()}).Render(ctx, "content/about.html", nil)
		expectOutput(t, "about", out, err, r.blocks["output"])
	})

	t.Run("colour", func(t *testing.T) {
		r := get(t, "A section with its own colour")
		e := mustEngine(t, brick.Options{FS: r.theme()})
		out, err := e.Render(ctx, "content/docs/install.html", nil)
		expectOutput(t, "install", out, err, r.blocks["output: docs/install"])
		out, err = e.Render(ctx, "content/contact.html", nil)
		expectOutput(t, "contact", out, err, r.blocks["output: contact"])
	})

	t.Run("two languages", func(t *testing.T) {
		r := get(t, "A button in two languages")
		fsys := r.theme()
		msgs, err := brick.LoadMessages(fsys, "i18n/")
		if err != nil {
			t.Fatal(err)
		}
		out, err := mustEngine(t, brick.Options{FS: fsys, Messages: msgs, DefaultLocale: "en"}).Render(ctx, "content/fr/panier.html", nil)
		expectOutput(t, "panier", out, err, r.blocks["output"])
	})

	t.Run("hand-written page", func(t *testing.T) {
		r := get(t, "One hand-written page for one language")
		fsys := r.theme()
		msgs := brick.Messages{"en": {"about": map[string]any{"title": "About us", "body": "…"}}, "ar": {"about": map[string]any{"title": "من نحن", "body": "…"}}, "de": {"about": map[string]any{"title": "Über uns", "body": "…"}}}
		e := mustEngine(t, brick.Options{FS: fsys, Messages: msgs, DefaultLocale: "en"})
		outputs := strings.Split(r.blocks["output"], "\n\n")
		for i, loc := range []string{"fr", "ar", "de"} {
			want := outputs[i][strings.Index(outputs[i], "\n")+1:] // drop the <!-- locale --> comment line
			out, err := e.Render(ctx, "content/about.html", nil, brick.WithLocale(loc))
			expectOutput(t, loc, out, err, want)
		}
	})

	t.Run("code", func(t *testing.T) {
		r := get(t, "A page that shows code")
		out, err := mustEngine(t, brick.Options{FS: r.theme()}).Render(ctx, "content/tutorial.html", nil)
		expectOutput(t, "tutorial", out, err, r.blocks["output"])
	})

	t.Run("wrapper", func(t *testing.T) {
		r := get(t, "A wrapper that passes its content through")
		out, err := mustEngine(t, brick.Options{FS: r.theme()}).Render(ctx, "content/pricing.html", nil)
		expectOutput(t, "pricing", out, err, r.blocks["output"])
	})

	t.Run("pagination", func(t *testing.T) {
		r := get(t, "A list split into pages")
		sc, err := scriptstarlark.New(scriptstarlark.Options{})
		if err != nil {
			t.Fatal(err)
		}
		s, err := site.New(ctx, site.Options{FS: r.theme("content/404.html", "missing page"), Engine: brick.Options{Scripting: sc}})
		if err != nil {
			t.Fatal(err)
		}
		// brick build: exactly one file per page that pages() names.
		dir := t.TempDir()
		rep, err := s.Export(ctx, dir)
		if err != nil {
			t.Fatal(err)
		}
		if rep.Pages != 3 {
			t.Fatalf("exported %d pages, want 3 (pages 1-3)", rep.Pages)
		}
		for _, n := range []string{"1", "3"} {
			out, err := os.ReadFile(filepath.Join(dir, "blog", "page", n, "index.html"))
			expectOutput(t, "page "+n, out, err, r.blocks["output: /blog/page/"+n+"/"])
		}
		// brick serve: a page past the end is the 404 page.
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/blog/page/4/", nil))
		if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "missing page") {
			t.Fatalf("/blog/page/4/: %d %q", rec.Code, rec.Body.String())
		}
	})

	t.Run("protecting", func(t *testing.T) {
		r := get(t, "Protecting a page")
		sc, err := scriptstarlark.New(scriptstarlark.Options{})
		if err != nil {
			t.Fatal(err)
		}
		// The host from main.go: the visitor is whoever the user cookie names.
		s, err := site.New(ctx, site.Options{FS: r.theme(), Engine: brick.Options{Scripting: sc},
			Request: func(r *http.Request) []brick.RenderOption {
				if c, err := r.Cookie("user"); err == nil {
					return []brick.RenderOption{brick.Provide("user", c.Value)}
				}
				return nil
			}})
		if err != nil {
			t.Fatal(err)
		}
		get := func(user string) *httptest.ResponseRecorder {
			req := httptest.NewRequest(http.MethodGet, "/account/", nil)
			if user != "" {
				req.AddCookie(&http.Cookie{Name: "user", Value: user})
			}
			rec := httptest.NewRecorder()
			s.Handler().ServeHTTP(rec, req)
			return rec
		}
		// The documented responses block, as status line and headers.
		want := map[string]string{}
		lines := strings.Split(r.blocks["responses: /account/ signed out"], "\n")
		wantCode := lines[0]
		for _, l := range lines[1:] {
			k, v, _ := strings.Cut(l, ": ")
			want[k] = v
		}
		signedOut := func(when string) {
			t.Helper()
			rec := get("")
			if got := "HTTP/1.1 " + strconv.Itoa(rec.Code) + " " + http.StatusText(rec.Code); got != wantCode {
				t.Fatalf("%s: %q, want %q", when, got, wantCode)
			}
			for k, v := range want {
				if got := rec.Header().Get(k); got != v {
					t.Fatalf("%s: %s = %q, want %q", when, k, got, v)
				}
			}
			if strings.Contains(rec.Body.String(), "Signed in") {
				t.Fatalf("%s: a signed-out visitor saw an account: %q", when, rec.Body.String())
			}
		}
		signedOut("before")
		rec := get("ada")
		if rec.Code != http.StatusOK {
			t.Fatalf("ada: %d %q", rec.Code, rec.Body.String())
		}
		expectOutput(t, "ada", rec.Body.Bytes(), nil, r.blocks["output: /account/ as ada"])
		// The cached shell never carries ada's island to the next visitor.
		signedOut("after ada")

		// brick build: the account is a redirect page, never the account.
		dir := t.TempDir()
		if _, err := s.Export(ctx, dir); err != nil {
			t.Fatal(err)
		}
		out, err := os.ReadFile(filepath.Join(dir, "account", "index.html"))
		expectOutput(t, "build", out, err, r.blocks["output: brick build account/index.html"])
	})

	t.Run("no javascript", func(t *testing.T) {
		r := get(t, "A form that works without JavaScript")
		sc, err := scriptstarlark.New(scriptstarlark.Options{})
		if err != nil {
			t.Fatal(err)
		}
		s, err := site.New(ctx, site.Options{FS: r.theme(), Engine: brick.Options{Scripting: sc}})
		if err != nil {
			t.Fatal(err)
		}
		h := s.Handler()
		send := func(method string, form url.Values, cookies []*http.Cookie) *httptest.ResponseRecorder {
			var body io.Reader
			if form != nil {
				body = strings.NewReader(form.Encode())
			}
			req := httptest.NewRequest(method, "/signup/", body)
			if form != nil {
				// A browser with scripts off posting its own page's form.
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				req.Header.Set("Sec-Fetch-Site", "same-origin")
			}
			for _, c := range cookies {
				req.AddCookie(c)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			return rec
		}
		// contains checks every line of a documented block against the page.
		contains := func(name, body string) {
			t.Helper()
			if r.blocks[name] == "" {
				t.Fatalf("no block %q in the recipe", name)
			}
			for _, line := range strings.Split(r.blocks[name], "\n") {
				if line = tidy(line); line != "" && !strings.Contains(tidy(body), line) {
					t.Fatalf("%s: page lacks %q:\n%s", name, line, tidy(body))
				}
			}
		}
		status := func(name string, rec *httptest.ResponseRecorder) {
			t.Helper()
			if r.blocks[name] == "" {
				t.Fatalf("no block %q in the recipe", name)
			}
			lines := strings.Split(r.blocks[name], "\n")
			if got := "HTTP/1.1 " + strconv.Itoa(rec.Code) + " " + http.StatusText(rec.Code); got != lines[0] {
				t.Fatalf("%s: %q, want %q", name, got, lines[0])
			}
			for _, l := range lines[1:] {
				k, v, _ := strings.Cut(l, ": ")
				if got := rec.Header().Get(k); got != v {
					t.Fatalf("%s: %s = %q, want %q", name, k, got, v)
				}
			}
		}

		page := send(http.MethodGet, nil, nil)
		if page.Code != http.StatusOK {
			t.Fatalf("GET: %d %q", page.Code, page.Body.String())
		}
		contains("output: the form's new parts", page.Body.String())
		// The hidden fields the page carries, posted back as the browser would.
		form := url.Values{}
		for _, m := range regexp.MustCompile(`<input type="hidden" name="(brick:[a-z]+)" value="([^"]*)">`).FindAllStringSubmatch(page.Body.String(), -1) {
			form.Set(m[1], html.UnescapeString(m[2]))
		}

		form.Set("name", "")
		rec := send(http.MethodPost, form, nil)
		status("response: an empty name", rec)
		contains("output: the error in the 422 page", rec.Body.String())

		form.Set("name", "Ada")
		form.Set("brick:model:agree", "on")
		rec = send(http.MethodPost, form, nil)
		status("response: Ada", rec)
		after := send(http.MethodGet, nil, rec.Result().Cookies())
		contains("output: /signup/ after the redirect", after.Body.String())
	})

	t.Run("url list", func(t *testing.T) {
		r := get(t, "A list whose page and filter live in the URL")
		sc, err := scriptstarlark.New(scriptstarlark.Options{})
		if err != nil {
			t.Fatal(err)
		}
		s, err := site.New(ctx, site.Options{FS: r.theme(), Engine: brick.Options{Scripting: sc}})
		if err != nil {
			t.Fatal(err)
		}
		for u, block := range map[string]string{
			"/blog/":               "output: /blog/",
			"/blog/?tag=go&page=2": "output: /blog/?tag=go&page=2",
			"/blog/?tag=go&page=9": "output: /blog/?tag=go&page=2", // corrected by on_params
			"/blog/?tag=rust":      "output: /blog/?tag=rust",
		} {
			rec := httptest.NewRecorder()
			s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, u, nil))
			if rec.Code != http.StatusOK {
				t.Fatalf("%s: %d %q", u, rec.Code, rec.Body.String())
			}
			expectOutput(t, u, []byte(stripRootAttrs(rec.Body.String())), nil, r.blocks[block])
		}
	})

	t.Run("presence", func(t *testing.T) {
		r := get(t, "Who is here right now")
		// The host from main.go: a viewer is listed by their user cookie.
		push, err := brick.NewPush(brick.PushOptions{Presence: brick.PresenceOptions{
			Identify: func(r *http.Request, topic string) (brick.PresenceEntry, bool) {
				c, err := r.Cookie("user")
				if err != nil {
					return brick.PresenceEntry{}, false
				}
				return brick.PresenceEntry{ID: c.Value, Data: map[string]string{"name": c.Value}}, true
			},
		}})
		if err != nil {
			t.Fatal(err)
		}
		s, err := site.New(ctx, site.Options{FS: r.theme(), Engine: brick.Options{Push: push}})
		if err != nil {
			t.Fatal(err)
		}
		srv := httptest.NewServer(s.Handler())
		t.Cleanup(srv.Close)
		send := func(user, path string) *http.Response {
			t.Helper()
			req, err := http.NewRequest(http.MethodGet, srv.URL+path, nil)
			if err != nil {
				t.Fatal(err)
			}
			req.AddCookie(&http.Cookie{Name: "user", Value: user})
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("%s %s: %d", user, path, resp.StatusCode)
			}
			return resp
		}
		lobby := func(user string) string {
			t.Helper()
			resp := send(user, "/lobby/")
			defer resp.Body.Close()
			b, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatal(err)
			}
			return string(b)
		}
		// connect opens the page's push stream, as the runtime does.
		connect := func(user, page string) *http.Response {
			t.Helper()
			m := listenTokenRE.FindStringSubmatch(page)
			if m == nil {
				t.Fatalf("no listen token on the page: %s", page)
			}
			resp := send(user, "/_brick/events?"+url.Values{"l": {html.UnescapeString(m[1])}}.Encode())
			t.Cleanup(func() { resp.Body.Close() })
			return resp
		}
		// until waits for the lobby, as user sees it, to equal block: joins
		// and leaves arrive over the stream, not with the request.
		until := func(user, block string) {
			t.Helper()
			want := tidy(r.blocks[block])
			got := ""
			for range 500 {
				if got = tidy(stripRootAttrs(lobby(user))); got == want {
					return
				}
				time.Sleep(10 * time.Millisecond)
			}
			t.Fatalf("%s:\n got  %s\n want %s", block, got, want)
		}

		ada := lobby("Ada")
		expectOutput(t, "Ada first", []byte(stripRootAttrs(ada)), nil, r.blocks["output: /lobby/ as Ada, first"])
		connect("Ada", ada)
		until("Bob", "output: /lobby/ as Bob")
		bob := connect("Bob", lobby("Bob"))
		connect("Ada", ada) // a second tab
		until("Ada", "output: /lobby/ with Bob connected and Ada in two tabs")
		bob.Body.Close()
		until("Ada", "output: /lobby/ after Bob leaves")
	})

	t.Run("testing", func(t *testing.T) {
		r := get(t, "Testing a component on its own")
		eng := bricktest.Engine(t, brick.Options{}, bricktest.Theme("components/Card.html", `<article class="card"><h3>{{ .Props.title }}</h3>{{ children }}</article>`))
		out, err := eng.RenderComponent(ctx, "Card", map[string]any{"title": "Hi"}, `<p>body</p>`)
		expectOutput(t, "card", out, err, r.blocks["testdata/card.golden"])
	})
}

// rootAttrRE is one engine-owned attribute on a live component's root: the
// state handle, its token and the listen token change on every render, so
// the cookbook leaves them out and so does the comparison.
var rootAttrRE = regexp.MustCompile(` data-brick(-[a-z-]+)?="[^"]*"`)

// listenTokenRE reads the push stream token off a listening root.
var listenTokenRE = regexp.MustCompile(`data-brick-listen-token="([^"]+)"`)

func stripRootAttrs(s string) string { return rootAttrRE.ReplaceAllString(s, "") }

// postsDB is a database/sql connector for the widget recipe: two posts, or a
// failure when down.
type postsDB struct{ down bool }

func (p postsDB) Connect(context.Context) (driver.Conn, error) {
	if p.down {
		return nil, errors.New("connection refused")
	}
	return postsConn{}, nil
}
func (postsDB) Driver() driver.Driver { return nil }

type postsConn struct{}

func (postsConn) Prepare(string) (driver.Stmt, error) { return postsStmt{}, nil }
func (postsConn) Close() error                        { return nil }
func (postsConn) Begin() (driver.Tx, error)           { return postsTx{}, nil }
func (postsConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return postsTx{}, nil
}

type postsTx struct{}

func (postsTx) Commit() error   { return nil }
func (postsTx) Rollback() error { return nil }

type postsStmt struct{}

func (postsStmt) Close() error                               { return nil }
func (postsStmt) NumInput() int                              { return -1 }
func (postsStmt) Exec([]driver.Value) (driver.Result, error) { return nil, errors.New("read only") }
func (postsStmt) Query([]driver.Value) (driver.Rows, error)  { return &postsRows{}, nil }

type postsRows struct{ i int }

func (*postsRows) Columns() []string { return []string{"title", "url"} }
func (*postsRows) Close() error      { return nil }
func (r *postsRows) Next(dest []driver.Value) error {
	rows := [][2]string{{"Newest post", "/newest"}, {"Older post", "/older"}}
	if r.i >= len(rows) {
		return io.EOF
	}
	dest[0], dest[1] = rows[r.i][0], rows[r.i][1]
	r.i++
	return nil
}
