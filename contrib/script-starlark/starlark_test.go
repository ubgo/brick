package scriptstarlark

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/ubgo/brick"
)

func files(kv ...string) fstest.MapFS {
	fs := fstest.MapFS{}
	for i := 0; i+1 < len(kv); i += 2 {
		fs[kv[i]] = &fstest.MapFile{Data: []byte(kv[i+1])}
	}
	return fs
}

func engine(t *testing.T, opts Options, fsys fstest.MapFS) *brick.Engine {
	t.Helper()
	sc, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	e, err := brick.New(brick.Options{FS: fsys, Scripting: sc})
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func render(t *testing.T, e *brick.Engine, page string) (string, error) {
	t.Helper()
	out, err := e.Render(context.Background(), page, nil)
	return string(out), err
}

func TestHelpersInlineAndLoad(t *testing.T) {
	t.Parallel()
	e := engine(t, Options{}, files(
		"lib/helpers.star", "load(\"~/lib/strings.star\", \"lower\")\nload(\"~/data/cfg.yaml\", cfg = \"data\")\ndef slugify(s):\n    return lower(s).replace(\" \", \"-\")\ndef top(posts, n):\n    return sorted(posts, key = lambda p: p[\"views\"], reverse = True)[:n]\ndef site():\n    return cfg[\"name\"]\n",
		"lib/strings.star", "def lower(s):\n    return s.lower()\n",
		"data/cfg.yaml", "name: Brick\n",
		"content/a.html", "---\nuse:\n  helpers: ~/lib/helpers.star\nposts:\n  - {t: a, views: 1}\n  - {t: b, views: 9}\n---\n<brick:script>\ndef price(n):\n    return \"$%d\" % n\n</brick:script>{{ helpers.slugify \"Hi There\" }} {{ price 5 }} {{ range helpers.top .Params.posts 1 }}{{ .t }}{{ end }} {{ helpers.site }}",
	))
	got, err := render(t, e, "content/a.html")
	if err != nil {
		t.Fatal(err)
	}
	if got != "hi-there $5 b Brick" {
		t.Fatalf("got %q", got)
	}
}

func TestLoaderThroughBrick(t *testing.T) {
	t.Parallel()
	e := engine(t, Options{}, files(
		"lib/posts.star", "def recent(input):\n    n = input[\"props\"][\"limit\"]\n    return {\"items\": [\"p%d\" % i for i in range(n)], \"who\": input[\"ctx\"].get(\"user\", \"anon\")}\n",
		"components/Recent.html", "---\nuse:\n  posts: ~/lib/posts.star\nload: posts.recent\ninject:\n  user: {default: guest}\nprops:\n  limit: {type: int, default: 2}\n---\n{{ range .Props.items }}{{ . }},{{ end }}{{ .Props.who }}",
		"content/a.html", `<Recent /> <Recent limit="3" />`,
	))
	got, err := render(t, e, "content/a.html")
	if err != nil {
		t.Fatal(err)
	}
	if got != "p0,p1,guest p0,p1,p2,guest" {
		t.Fatalf("got %q", got)
	}
}

func TestSandboxAndLimits(t *testing.T) {
	t.Parallel()
	e := engine(t, Options{Steps: 10_000}, files(
		"lib/spin.star", "def spin():\n    n = 0\n    for i in range(100000000):\n        n += i\n    return n\ndef rec(n):\n    return rec(n)\n",
		"content/steps.html", "---\nuse:\n  s: ~/lib/spin.star\n---\n{{ s.spin }}",
		"content/rec.html", "---\nuse:\n  s: ~/lib/spin.star\n---\n{{ s.rec 1 }}",
		"content/while.html", "<brick:script>\ndef f():\n    while True:\n        pass\n</brick:script>",
		"content/set.html", "<brick:script>\ndef f():\n    return set([1])\n</brick:script>",
		"content/open.html", "<brick:script>\ndef f():\n    return open(\"x\")\n</brick:script>",
		"content/loadrel.html", "<brick:script>\nload(\"./x.star\", \"y\")\n</brick:script>",
		"content/cycle.html", "---\nuse:\n  a: ~/lib/a.star\n---\n",
		"lib/a.star", "load(\"~/lib/b.star\", \"x\")\n",
		"lib/b.star", "load(\"~/lib/a.star\", \"y\")\nx = 1\n",
		"content/shadow.html", "<brick:script>\ndef default(x):\n    return x\n</brick:script>",
	))
	for page, want := range map[string]string{
		"content/steps.html":   "too many steps",
		"content/rec.html":     "called recursively",
		"content/while.html":   "while",
		"content/set.html":     "does not support sets",
		"content/open.html":    "undefined: open",
		"content/loadrel.html": "start from the project root",
		"content/cycle.html":   "load cycle",
		"content/shadow.html":  "shadows a built-in",
	} {
		_, err := render(t, e, page)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err = %v, want %q", page, err, want)
		}
	}
}

func TestTimeoutAndCancel(t *testing.T) {
	t.Parallel()
	e := engine(t, Options{Timeout: 50 * time.Millisecond, Steps: math.MaxUint64}, files(
		"lib/spin.star", "def spin():\n    n = 0\n    for i in range(1000000000):\n        n += i\n    return n\n",
		"content/a.html", "---\nuse:\n  s: ~/lib/spin.star\n---\n{{ s.spin }}",
	))
	start := time.Now()
	_, err := render(t, e, "content/a.html")
	if err == nil || time.Since(start) > 5*time.Second {
		t.Fatalf("timeout not enforced: %v after %s", err, time.Since(start))
	}
	if !strings.Contains(err.Error(), "over the time limit") || errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want a containable time-limit error, got %v", err)
	}
	// The render's own deadline is cancellation, which always wins (E2).
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	e2 := engine(t, Options{Timeout: time.Minute, Steps: math.MaxUint64}, files(
		"lib/spin.star", "def spin():\n    n = 0\n    for i in range(1000000000):\n        n += i\n    return n\n",
		"content/a.html", "---\nuse:\n  s: ~/lib/spin.star\n---\n<brick:boundary>{{ s.spin }}<brick:prop name=\"fallback\">F</brick:prop></brick:boundary>",
	))
	if _, err := e2.Render(ctx, "content/a.html", nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("render deadline: %v", err)
	}
}

func TestConversionTable(t *testing.T) {
	t.Parallel()
	type post struct {
		Title  string
		hidden string
		Tags   []string
	}
	when := time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		in   any
		want string
	}{
		{nil, "None"}, {true, "True"}, {int64(1 << 40), "1099511627776"}, {2.5, "2.5"},
		{"s", `"s"`}, {[]byte("b"), `b"b"`}, {when, `"2026-09-27T10:00:00Z"`},
		{map[string]any{"b": 1, "a": 2}, `{"a": 2, "b": 1}`},
		{[]int{1, 2}, "[1, 2]"}, {post{Title: "T", hidden: "h", Tags: []string{"x"}}, `{"Title": "T", "Tags": ["x"]}`},
		{&post{Title: "P"}, `{"Title": "P", "Tags": []}`},
	} {
		v, err := toStarlark(tc.in, 0, nil)
		if err != nil {
			t.Fatalf("%v: %v", tc.in, err)
		}
		if v.String() != tc.want {
			t.Errorf("%T: got %s, want %s", tc.in, v, tc.want)
		}
	}
	if _, err := toStarlark(map[int]int{1: 1}, 0, nil); err == nil {
		t.Error("int-keyed map entered a script")
	}
	cyc := map[string]any{}
	cyc["self"] = cyc
	if _, err := toStarlark(cyc, 0, nil); err == nil {
		t.Error("a cycle entered a script")
	}

	sc, _ := New(Options{})
	mod, err := sc.Compile(context.Background(), brick.ScriptSource{Path: "t.star", Src: []byte("def tup():\n    return (1, 2)\ndef nan():\n    return float(\"nan\")\ndef big():\n    return 1 << 70\ndef fn():\n    return tup\ndef d():\n    return {1: 2}\n")})
	if err != nil {
		t.Fatal(err)
	}
	if v, err := mod.Call(context.Background(), "tup"); err != nil || fmt.Sprint(v) != "[1 2]" {
		t.Errorf("tuple: %v %v", v, err)
	}
	for _, fn := range []string{"nan", "big", "fn", "d"} {
		if _, err := mod.Call(context.Background(), fn); err == nil {
			t.Errorf("%s: expected a conversion error", fn)
		}
	}
	if got := mod.Functions(); strings.Join(got, ",") != "big,d,fn,nan,tup" {
		t.Errorf("functions: %v", got)
	}
}

func TestSQLMustBeFixed(t *testing.T) {
	t.Parallel()
	db := sql.OpenDB(fakeConnector{})
	sc, err := New(Options{DB: db})
	if err != nil {
		t.Fatal(err)
	}
	ok := "Q = \"select name from t\"\ndef a():\n    return db.query(\"select name from t where id = ?\", 1)\ndef b():\n    return db.query(Q)\ndef c(col):\n    return db.query_dynamic(\"select \" + col + \" from t\")\n"
	if _, err := sc.Compile(context.Background(), brick.ScriptSource{Path: "ok.star", Src: []byte(ok)}); err != nil {
		t.Fatalf("fixed SQL rejected: %v", err)
	}
	bad := "def a(col):\n    return db.query(\"select \" + col)\n"
	if _, err := sc.Compile(context.Background(), brick.ScriptSource{Path: "bad.star", Src: []byte(bad)}); err == nil || !strings.Contains(err.Error(), "query_dynamic") {
		t.Fatalf("dynamic SQL accepted: %v", err)
	}
	mod, _ := sc.Compile(context.Background(), brick.ScriptSource{Path: "ok.star", Src: []byte(ok + "def multi():\n    return db.query(\"select 1; drop table t\")\n")})
	rows, err := mod.Call(context.Background(), "a")
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(rows) != "[map[name:ana] map[name:<nil>]]" {
		t.Fatalf("rows: %v", rows)
	}
	if _, err := mod.Call(context.Background(), "multi"); err == nil || !strings.Contains(err.Error(), "one statement") {
		t.Fatalf("multi: %v", err)
	}
	noDB, _ := New(Options{})
	if _, err := noDB.Compile(context.Background(), brick.ScriptSource{Path: "x.star", Src: []byte("def a():\n    return db.query(\"select 1\")\n")}); err == nil {
		t.Fatal("db exists without Options.DB")
	}
}

func TestHTTPAllowlist(t *testing.T) {
	t.Parallel()
	var hits sync.Map
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Store(r.URL.Path, true)
		switch r.URL.Path {
		case "/json":
			_, _ = io.WriteString(w, `{"n": 3, "f": 1.5}`)
		case "/redirect":
			http.Redirect(w, r, "https://evil.test/steal", http.StatusFound)
		case "/big":
			_, _ = io.WriteString(w, strings.Repeat("x", 100))
		default:
			w.Header().Set("X-A", "1")
			_, _ = io.WriteString(w, "hello")
		}
	}))
	defer srv.Close()
	sc, err := New(Options{HTTPAllow: []string{srv.URL}, HTTPClient: srv.Client(), HTTPMaxBytes: 50})
	if err != nil {
		t.Fatal(err)
	}
	mod, err := sc.Compile(context.Background(), brick.ScriptSource{Path: "h.star", Src: []byte(
		"def get(u):\n    return http.get(u)\ndef js(u):\n    return http.get_json(u)\n")})
	if err != nil {
		t.Fatal(err)
	}
	v, err := mod.Call(context.Background(), "get", srv.URL+"/")
	if err != nil {
		t.Fatal(err)
	}
	m, _ := v.(map[string]any)
	if m["text"] != "hello" || m["status"] != int64(200) {
		t.Fatalf("get: %v", v)
	}
	if v, err := mod.Call(context.Background(), "js", srv.URL+"/json"); err != nil || fmt.Sprint(v) != "map[f:1.5 n:3]" {
		t.Fatalf("json: %v %v", v, err)
	}
	for _, u := range []string{"https://other.test/", srv.URL + "/redirect", srv.URL + "/big", strings.Replace(srv.URL, "https://", "https://user:pw@", 1) + "/"} {
		if _, err := mod.Call(context.Background(), "get", u); err == nil {
			t.Errorf("%s: expected refusal", u)
		}
	}
	for _, bad := range []string{"https://*.example.com", "ftp://x.test", "https://x.test/path"} {
		if _, err := New(Options{HTTPAllow: []string{bad}}); err == nil {
			t.Errorf("allowlist %q accepted", bad)
		}
	}
}

// fakeConnector is a minimal database/sql driver returning two rows.
type fakeConnector struct{}

func (fakeConnector) Connect(context.Context) (driver.Conn, error) { return fakeConn{}, nil }
func (fakeConnector) Driver() driver.Driver                        { return nil }

type fakeConn struct{}

func (fakeConn) Prepare(q string) (driver.Stmt, error) { return fakeStmt{}, nil }
func (fakeConn) Close() error                          { return nil }
func (fakeConn) Begin() (driver.Tx, error)             { return fakeTx{}, nil }
func (fakeConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return fakeTx{}, nil
}

type fakeTx struct{}

func (fakeTx) Commit() error   { return nil }
func (fakeTx) Rollback() error { return nil }

type fakeStmt struct{}

func (fakeStmt) Close() error                               { return nil }
func (fakeStmt) NumInput() int                              { return -1 }
func (fakeStmt) Exec([]driver.Value) (driver.Result, error) { return nil, errors.New("read only") }
func (fakeStmt) Query([]driver.Value) (driver.Rows, error)  { return &fakeRows{}, nil }

type fakeRows struct{ i int }

func (*fakeRows) Columns() []string { return []string{"name"} }
func (*fakeRows) Close() error      { return nil }
func (r *fakeRows) Next(dest []driver.Value) error {
	if r.i >= 2 {
		return io.EOF
	}
	if r.i == 0 {
		dest[0] = "ana"
	} else {
		dest[0] = nil
	}
	r.i++
	return nil
}

func TestStarlarkActions(t *testing.T) {
	t.Parallel()
	sc, err := New(Options{})
	if err != nil {
		t.Fatal(err)
	}
	e, err := brick.New(brick.Options{Scripting: sc, States: brick.NewMemoryStates(0, 0), FS: files(
		"components/Counter.html", "---\nstate:\n  count: 0\nprops:\n  step: {type: int, default: 1}\n---\n<brick:script>\ndef increment(state, props):\n    return dict(state, count = state[\"count\"] + props[\"step\"])\n\ndef bump(state, props, n):\n    return dict(state, count = state[\"count\"] + n)\n\ndef _secret(state, props):\n    return {}\n</brick:script>\n<div><span>{{ .State.count }}</span><button brick:click=\"increment\">+</button></div>",
		"content/a.html", `<Counter step="3" />`,
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
	if m == nil {
		t.Fatalf("page: %s", page)
	}
	out, err := e.Action(ctx, brick.ActionRequest{Handle: m[1], Token: m[2], Action: "increment"})
	if err != nil || !strings.Contains(string(out), "<span>3</span>") {
		t.Fatalf("increment: %s %v", out, err)
	}
	out, err = e.Action(ctx, brick.ActionRequest{Handle: m[1], Token: m[2], Action: "bump", Args: []any{int64(10)}})
	if err != nil || !strings.Contains(string(out), "<span>13</span>") {
		t.Fatalf("bump: %s %v", out, err)
	}
	if _, err := e.Action(ctx, brick.ActionRequest{Handle: m[1], Token: m[2], Action: "_secret"}); !errors.Is(err, brick.ErrUnknownAction) {
		t.Fatalf("private function callable: %v", err)
	}
}
