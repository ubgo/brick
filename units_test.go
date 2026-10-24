package brick

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"testing"

	"github.com/ubgo/brick/internal/frontmatter"
)

// The builder's checkpoint/rollback is what makes a boundary discard
// exactly its own output (E3), across flushed chunks and markers.
func TestBuilderRollback(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		run  func(b *builder)
		want string
	}{
		{"within the buffer", func(b *builder) { _, _ = b.Write([]byte("DROP")) }, "a|"},
		{"across a marker", func(b *builder) {
			_, _ = b.Write([]byte("x"))
			b.mark(ChunkNonce)
			_, _ = b.Write([]byte("y"))
		}, "a|"},
		{"nothing written", func(*builder) {}, "a|"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := &builder{}
			_, _ = b.Write([]byte("a|"))
			cp := b.checkpoint()
			tc.run(b)
			b.rollback(cp)
			_, _ = b.Write([]byte("z"))
			var got strings.Builder
			for _, c := range b.result() {
				if c.Kind != ChunkBytes {
					got.WriteString("<marker>")
				}
				got.Write(c.Bytes)
			}
			if got.String() != tc.want+"z" {
				t.Fatalf("got %q", got.String())
			}
		})
	}
	// A checkpoint taken after a marker keeps the marker.
	b := &builder{}
	_, _ = b.Write([]byte("a"))
	b.mark(ChunkAssets)
	cp := b.checkpoint()
	_, _ = b.Write([]byte("b"))
	b.mark(ChunkNonce)
	b.rollback(cp)
	if kinds := markerKinds(b.result()); len(kinds) != 1 || kinds[0] != ChunkAssets {
		t.Fatalf("markers %v", kinds)
	}
	// The output limit applies to the running total.
	lim := &builder{limit: 3}
	if _, err := lim.Write([]byte("abcd")); !errors.Is(err, errOutputLimit) {
		t.Fatalf("limit: %v", err)
	}
}

func TestCoerce(t *testing.T) {
	t.Parallel()
	ok := []struct {
		in   any
		typ  string
		want any
	}{
		{"x", typeString, "x"}, {int64(3), typeString, "3"}, {true, typeString, "true"},
		{"true", typeBool, true}, {"0", typeBool, false}, {false, typeBool, false},
		{"42", typeInt, int64(42)}, {3, typeInt, int64(3)}, {int32(3), typeInt, int64(3)}, {float64(7), typeInt, int64(7)},
		{"1.5", typeFloat, 1.5}, {2, typeFloat, 2.0}, {int64(2), typeFloat, 2.0}, {float32(0.5), typeFloat, 0.5},
		{[]string{"a"}, typeList, []string{"a"}}, {map[string]int{"a": 1}, typeMap, map[string]int{"a": 1}},
		{nil, typeInt, nil}, {struct{}{}, typeAny, struct{}{}},
	}
	for _, tc := range ok {
		got, err := coerce(tc.in, tc.typ)
		if err != nil || fmt.Sprint(got) != fmt.Sprint(tc.want) {
			t.Errorf("coerce(%#v, %s) = %#v, %v", tc.in, tc.typ, got, err)
		}
	}
	for _, tc := range []struct {
		in  any
		typ string
	}{
		{"yes", typeBool}, {"1.5", typeInt}, {1.5, typeInt}, {"x", typeFloat}, {"a", typeList}, {map[int]int{}, typeMap}, {[]int{1}, typeString},
	} {
		if _, err := coerce(tc.in, tc.typ); err == nil {
			t.Errorf("coerce(%#v, %s) accepted", tc.in, tc.typ)
		}
	}
	if d := describe(strings.Repeat("x", 100)); !strings.HasPrefix(d, `"`) {
		t.Errorf("describe string: %s", d)
	}
	if d := describe([]int{1}); !strings.Contains(d, "[]int") {
		t.Errorf("describe value: %s", d)
	}
	if describe(nil) != "nothing" {
		t.Error("describe nil")
	}
}

func TestDeepCopy(t *testing.T) {
	t.Parallel()
	src := map[string]any{"a": []any{map[string]any{"b": 1}}, "typed": map[string][]string{"k": {"v"}}, "ints": []int{1}}
	cp, ok := deepCopy(src).(map[string]any)
	if !ok {
		t.Fatal("not a map")
	}
	src["a"].([]any)[0].(map[string]any)["b"] = 2
	src["typed"].(map[string][]string)["k"][0] = "changed"
	src["ints"].([]int)[0] = 9
	if fmt.Sprint(cp) != "map[a:[map[b:1]] ints:[1] typed:map[k:[v]]]" {
		t.Fatalf("copy shares memory: %v", cp)
	}
	type site struct{ Title string }
	if deepCopy(site{"x"}).(site).Title != "x" || deepCopy(nil) != nil {
		t.Fatal("values pass through")
	}
}

func TestParseMetaErrors(t *testing.T) {
	t.Parallel()
	for _, fm := range []string{
		"layout: 3", "use: [a]", "use:\n  A: 3", "use:\n  __x: ~/a.html", "use:\n  A: ./a.html", "use:\n  A: ~/a.txt",
		"props: 3", "props:\n  children: string", "props:\n  key: string", "props:\n  a: {tpye: int}", "props:\n  a: {type: 3}",
		"props:\n  a: {required: yes}", "props:\n  a: {oneof: []}", "props:\n  a: {client: 1}", "props:\n  a: 3", "props:\n  a: widget",
		"props:\n  a: {type: int, default: x}", "load: 3", "provide: [a]", "inject: 3", "inject:\n  a: 3", "inject:\n  a: {required: 1}",
		"raw: yes", "strictParams: 1", "cache: 3", "cache:\n  ttl: 5", "cache:\n  tags: 3", "cache:\n  tags: [1]",
		"unknown: maybe", "state: 3", "brick: 3", "brick:\n  island: yes", "passthrough: x", "passthrough: [1]", "syntax: odd",
	} {
		doc, err := frontmatter.Parse([]byte("---\n" + fm + "\n---\n"))
		if err != nil {
			t.Fatalf("%q: %v", fm, err)
		}
		if _, err := parseMeta(doc, "content/x.html", (&Engine{}).resolveFrom, nil, nil); err == nil {
			t.Errorf("%q: accepted", fm)
		}
	}
	doc, _ := frontmatter.Parse([]byte("---\nuse:\n  lower: ~/components/Card.html\nstate: {n: 1}\ntitle: T\n---\n"))
	m, err := parseMeta(doc, "content/x.html", (&Engine{}).resolveFrom, nil, nil)
	if err != nil || len(m.warnings) != 1 || m.state["n"] != int64(1) || m.params["title"] != "T" || m.params["use"] != nil {
		t.Fatalf("meta: %+v %v", m, err)
	}
}

func TestLayoutErrors(t *testing.T) {
	t.Parallel()
	e := newEngine(t, Options{FS: theme(
		"layouts/a.html", "---\nlayout: b\n---\nA{{ children }}",
		"layouts/b.html", "---\nlayout: a\n---\nB{{ children }}",
		"shared/wrap.html", "[{{ children }}]",
		"content/cycle.html", "---\nlayout: a\n---\nx",
		"content/missing.html", "---\nlayout: nope\n---\nx",
		"content/path.html", "---\nlayout: ~/shared/wrap.html\n---\nx",
		"content/escape.html", "---\nlayout: ../x\n---\nx",
		"components/C.html", "---\nlayout: a\n---\nc",
		"content/comp.html", "<C />",
	)})
	if err := renderErr(t, e, "content/cycle.html"); err.Code != CodeLayout || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("cycle: %v", err)
	}
	if err := renderErr(t, e, "content/missing.html"); err.Code != CodeLayout {
		t.Fatalf("missing: %v", err)
	}
	if err := renderErr(t, e, "content/escape.html"); err.Code != CodeLayout {
		t.Fatalf("escape: %v", err)
	}
	if got := render(t, e, "content/path.html", nil); got != "[x]" {
		t.Fatalf("path layout: %q", got)
	}
	if got := render(t, e, "content/comp.html", nil); got != "c" {
		t.Fatalf("component layout ignored: %q", got)
	}
	var ignored bool
	for _, w := range e.Warnings() {
		ignored = ignored || w.Code == CodeLayoutIgnored
	}
	if !ignored {
		t.Fatal("no warning for layout: in a component")
	}
}

func TestNormalizeLocale(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{"en": "en", "EN": "en", "pt_br": "pt-BR", "zh-hant-tw": "zh-Hant-TW", "es-419": "es-419", "fil": "fil"} {
		if got, ok := normalizeLocale(in); !ok || got != want {
			t.Errorf("%s: %s %v", in, got, ok)
		}
	}
	for _, bad := range []string{"", "e", "english", "en-1", "en-US-x", "en-USA", "12"} {
		if got, ok := normalizeLocale(bad); ok {
			t.Errorf("%q accepted as %q", bad, got)
		}
	}
}

type panicky struct{ hits *int }

func (p panicky) OnError(context.Context, error) {
	*p.hits++
	panic("reporter broke")
}

func (panicky) BeforeRender(context.Context, *Page) error { panic("before broke") }

// A panicking reporter never fails a render; a panicking BeforeRender is an
// error with code panic (E1).
func TestHookPanics(t *testing.T) {
	t.Parallel()
	hits := 0
	e := newEngine(t, Options{Hooks: []any{panicky{&hits}}, FS: theme("content/a.html", "x")})
	err := renderErr(t, e, "content/a.html")
	var inner *Error
	if !errors.As(err.Err, &inner) || inner.Code != CodePanic || hits != 1 {
		t.Fatalf("err=%v hits=%d", err, hits)
	}
}

func TestMiscHelpers(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		v    any
		want bool
	}{{nil, true}, {"", true}, {0, true}, {false, true}, {[]int{}, true}, {0.0, true}, {uint(0), true}, {"x", false}, {1, false}, {[]int{1}, false}} {
		if isEmpty(tc.v) != tc.want {
			t.Errorf("isEmpty(%#v)", tc.v)
		}
	}
	if m, err := toStringMap(map[string]int{"a": 1}); err != nil || m["a"] != 1 {
		t.Errorf("toStringMap typed: %v %v", m, err)
	}
	if _, err := toStringMap([]int{1}); err == nil {
		t.Error("toStringMap accepted a slice")
	}
	if l := (ErrorList{{File: "a", Err: errors.New("x")}, {File: "b", Err: errors.New("y")}}); !strings.Contains(l.Error(), "a: x\nb: y") || len(l.Unwrap()) != 2 {
		t.Errorf("ErrorList: %v", l)
	}
	if SeverityWarning.String() != "warning" || SeverityError.String() != "error" {
		t.Error("severity words")
	}
	if kindPage.String() != "page" || kindLayout.String() != "layout" || kindComponent.String() != "component" {
		t.Error("kind words")
	}
	if !isDigits("419") || isDigits("4a") || isDigits("") {
		t.Error("isDigits")
	}
}

func TestSchema(t *testing.T) {
	t.Parallel()
	s, err := ParseSchema(map[string]any{"title": map[string]any{"type": "string", "required": true}, "tags": "list", "kind": map[string]any{"oneof": []any{"a", "b"}}})
	if err != nil {
		t.Fatal(err)
	}
	if errs := s.Validate(map[string]any{"title": "x", "tags": []any{}, "extra": 1}); errs != nil {
		t.Fatalf("valid: %v", errs)
	}
	errs := s.Validate(map[string]any{"tags": "x", "kind": "c"})
	if len(errs) != 3 || !strings.Contains(strings.Join(errs, "|"), "title is required") {
		t.Fatalf("invalid: %v", errs)
	}
	if _, err := ParseSchema(3); err == nil {
		t.Fatal("bad schema accepted")
	}
}

func TestAssetsWalk(t *testing.T) {
	t.Parallel()
	e := newEngine(t, Options{FS: theme("components/A.html", "<brick:client>x</brick:client><i></i>", "content/a.html", "<A />")})
	if _, err := e.Render(context.Background(), "content/a.html", nil); err != nil {
		t.Fatal(err)
	}
	var files []string
	if err := fs.WalkDir(e.Assets(), ".", func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			files = append(files, p)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 || !strings.HasPrefix(files[0], "c/A.") || !strings.HasPrefix(files[1], "c/runtime.") {
		t.Fatalf("walk: %v", files)
	}
}
