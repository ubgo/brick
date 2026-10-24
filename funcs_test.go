package brick

import (
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

// Every built-in, exercised through a real template so the calling
// convention (argument order, pipelines) is what is pinned.
func TestBuiltins(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		`{{ "" | default "d" }}|{{ "x" | default "d" }}|{{ 0 | default 7 }}`: "d|x|7",
		`{{ coalesce "" nil "c" }}`: "c",
		`{{ $m := dict "a" 1 "b" 2 }}{{ keys $m }}{{ has $m "a" }}{{ keys (omit $m "a") }}`:                 "[a b]true[b]",
		`{{ keys (pick (dict "a" 1 "b" 2) "b" "z") }}`:                                                      "[b]",
		`{{ first (list 1 2) }}{{ last (list 1 2) }}{{ first (list) }}`:                                     "12",
		`{{ seq 3 }}{{ seq 2 4 }}`:                                                                          "[1 2 3][2 3 4]",
		`{{ upper "a" }}{{ lower "B" }}{{ trim " c " }}`:                                                    "Abc",
		`{{ "x.y" | trimPrefix "x" }}{{ "a.b" | trimSuffix ".b" }}{{ "aa" | replace "a" "b" }}`:             ".yabb",
		`{{ split "," "a,b" }}{{ join "-" (list 1 2) }}`:                                                    "[a b]1-2",
		`{{ contains "b" "abc" }}{{ hasPrefix "a" "abc" }}{{ hasSuffix "c" "abc" }}`:                        "truetruetrue",
		`{{ truncate 5 "hello world" }}|{{ truncate 20 "short" }}`:                                          "hell…|short",
		`{{ slugify "Hello,  World! Ünï" }}`:                                                                "hello-world-ünï",
		`{{ pluralize 1 "item" "items" }} {{ pluralize 2 "item" "items" }}`:                                 "item items",
		`{{ add 1 2 }} {{ sub 5 2 }} {{ mul 2 3 }} {{ div 7 2 }} {{ mod 7 2 }} {{ max 1 9 }} {{ min 1 9 }}`: "3 3 6 3 1 9 1",
		`{{ add 1 1.5 }} {{ div 1.0 4 }}`:                                                                   "2.5 0.25",
		`{{ (fromJSON "{\"a\":[1,2.5]}").a }}`:                                                              "[1 2.5]",
		`<script>var x = {{ toJSON (dict "a" "</script>") }};</script>`:                                     `<script>var x = {"a":"\u003c/script\u003e"};</script>`,
		`{{ dateFormat "2 Jan 2006" "2026-09-27" }}`:                                                        "27 Sep 2026",
		`{{ numberFormat 1234567.5 }} {{ numberFormat 1234 2 }} {{ numberFormat -1000 }}`:                   "1,234,567.5 1,234.00 -1,000",
	}
	var files []string
	i := 0
	want := map[string]string{}
	for src, w := range cases {
		p := "content/" + string(rune('a'+i/26)) + string(rune('a'+i%26)) + ".html"
		files = append(files, p, src)
		want[p] = w
		i++
	}
	e := newEngine(t, Options{FS: theme(files...)})
	for p, w := range want {
		if got := render(t, e, p, nil); got != w {
			t.Errorf("%s: got %q, want %q", strings.Join([]string{p}, ""), got, w)
		}
	}
}

func TestBuiltinErrors(t *testing.T) {
	t.Parallel()
	e := newEngine(t, Options{FS: theme(
		"content/a.html", `{{ dict "a" }}`,
		"content/b.html", `{{ div 1 0 }}`,
		"content/c.html", `{{ fromJSON "{" }}`,
		"content/d.html", `{{ seq 1000000 }}`,
		"content/e.html", `{{ dateFormat "2006" "nope" }}`,
	)})
	for _, p := range []string{"content/a.html", "content/b.html", "content/c.html", "content/d.html", "content/e.html"} {
		if err := renderErr(t, e, p); err.Code != CodeRuntime {
			t.Errorf("%s: %+v", p, err)
		}
	}
}

func TestNumberFormatLocales(t *testing.T) {
	t.Parallel()
	e := newEngine(t, Options{FS: theme("content/a.html", `{{ numberFormat 1234.5 }}`)})
	if got := render(t, e, "content/a.html", nil, WithLocale("de")); got != "1.234,5" {
		t.Fatalf("de: %q", got)
	}
	if got := render(t, e, "content/a.html", nil, WithLocale("fr")); got != "1 234,5" {
		t.Fatalf("fr: %q", got)
	}
}

func TestDateFormatTimeZone(t *testing.T) {
	t.Parallel()
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skip("no tzdata")
	}
	e := newEngine(t, Options{TimeZone: ny, FS: theme("content/a.html", "---\ndate: 2026-09-27T02:00:00Z\n---\n{{ dateFormat \"2006-01-02 15:04\" .Date }}")})
	if got := render(t, e, "content/a.html", nil); got != "2026-09-26 22:00" {
		t.Fatalf("got %q", got)
	}
}

func TestPluralCategories(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		tag  string
		n    float64
		want string
	}{
		{"en", 1, "one"}, {"en", 0, "other"}, {"en", 1.5, "other"},
		{"fr", 0, "one"}, {"fr", 2, "other"},
		{"pt-BR", 0, "one"}, {"pt-PT", 0, "other"},
		{"ru", 1, "one"}, {"ru", 3, "few"}, {"ru", 5, "many"}, {"ru", 11, "many"}, {"ru", 21, "one"},
		{"pl", 22, "few"}, {"pl", 25, "many"},
		{"cs", 3, "few"}, {"ar", 0, "zero"}, {"ar", 2, "two"}, {"ar", 5, "few"}, {"ar", 11, "many"},
		{"ja", 1, "other"}, {"he", 2, "two"},
	} {
		if got := pluralCategory(tc.tag, tc.n); got != tc.want {
			t.Errorf("%s %v: got %s, want %s", tc.tag, tc.n, got, tc.want)
		}
	}
}

func TestLoadMessages(t *testing.T) {
	t.Parallel()
	good := fstest.MapFS{
		"i18n/en.yaml":    {Data: []byte("cart:\n  add: Add\n  items: {one: \"{count} item\", other: \"{count} items\"}\n")},
		"i18n/pt_BR.json": {Data: []byte(`{"cart":{"add":"Adicionar"}}`)},
		"i18n/.keep":      {Data: nil},
	}
	m, err := LoadMessages(good, "i18n")
	if err != nil {
		t.Fatal(err)
	}
	if !m.has("en") || !m.has("pt-BR") {
		t.Fatalf("locales: %v", sortedKeys(m))
	}
	for name, files := range map[string]fstest.MapFS{
		"no other":    {"i18n/en.yaml": {Data: []byte("a: {one: x}\n")}},
		"shape":       {"i18n/en.yaml": {Data: []byte("a: x\n")}, "i18n/fr.yaml": {Data: []byte("a: {one: x, other: y}\n")}},
		"bad name":    {"i18n/english.yaml": {Data: []byte("a: x\n")}},
		"not a map":   {"i18n/en.yaml": {Data: []byte("- x\n")}},
		"duplicate":   {"i18n/pt_BR.yaml": {Data: []byte("a: x\n")}, "i18n/pt-BR.yaml": {Data: []byte("a: x\n")}},
		"missing dir": {},
	} {
		if _, err := LoadMessages(files, "i18n"); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
	if _, err := New(Options{DefaultLocale: "de", Messages: m}); err == nil {
		t.Fatal("DefaultLocale without a catalog was accepted (E9)")
	}
}

func TestDateFormatNames(t *testing.T) {
	t.Parallel()
	d := time.Date(2026, 9, 27, 14, 5, 0, 0, time.UTC) // a Sunday
	for _, tc := range []struct{ locale, layout, want string }{
		{"en", "Monday 2 January 2006", "Sunday 27 September 2026"},
		{"fr-FR", "Monday 2 January 2006", "dimanche 27 septembre 2026"},
		{"de", "Mon, 2. Jan 2006 15:04", "So., 27. Sept. 2026 14:05"},
		{"pt-BR", "2 de January", "27 de setembro"},
		{"ja", "Jan 2", "Sep 27"},
	} {
		if got := formatLocal(d, tc.layout, tc.locale); got != tc.want {
			t.Errorf("%s %q: got %q, want %q", tc.locale, tc.layout, got, tc.want)
		}
	}
	e := newEngine(t, Options{FS: theme("content/a.html", `{{ dateFormat "2 January" "2026-09-27" }}`)})
	if got := render(t, e, "content/a.html", nil, WithLocale("es")); got != "27 septiembre" {
		t.Fatalf("es: %q", got)
	}
}

func TestTranslateNilPlaceholder(t *testing.T) {
	t.Parallel()
	e := newEngine(t, Options{DefaultLocale: "en", Messages: Messages{"en": {"hi": "Hi, {name}!"}}, FS: theme("content/a.html", `{{ t "hi" (dict "name" .Props.user) }}`)})
	if got := render(t, e, "content/a.html", nil); got != "Hi, !" {
		t.Fatalf("got %q", got)
	}
}
