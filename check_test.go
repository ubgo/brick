package brick

import (
	"context"
	"strings"
	"testing"
	"testing/fstest"
)

func TestCompileAll(t *testing.T) {
	t.Parallel()
	e := newEngine(t, Options{FS: theme(
		"components/Card.html", `<div>{{ children }}</div>`,
		"components/Unused.html", `<Crad />`,
		"content/ok.html", `<Card>x</Card>`,
		"content/typo.html", "line1\n<Crad />",
		"content/escaper.html", `<div class="{{ .X }}`,
		"content/pricing/PricingCard.html", `card`,
		"content/pricing/index.html", "---\nuse:\n  PricingCard: ~/content/pricing/PricingCard.html\n---\n<PricingCard />",
		"content/about.fr.html", `bonjour`,
		"layouts/default.html", `{{ children }}`,
	)})
	r, err := e.CompileAll(context.Background(), "content")
	if err != nil {
		t.Fatal(err)
	}
	var msgs []string
	for _, d := range r.Diagnostics {
		msgs = append(msgs, d.String())
	}
	all := strings.Join(msgs, "\n")
	for _, want := range []string{
		"components/Unused.html:1:1: error: unknown component <Crad>",
		"content/typo.html:2:1: error: unknown component <Crad>",
		"content/escaper.html",
	} {
		if !strings.Contains(all, want) {
			t.Fatalf("missing %q in\n%s", want, all)
		}
	}
	if strings.Contains(strings.Join(r.Pages, " "), "PricingCard") || strings.Contains(strings.Join(r.Pages, " "), "about.fr") {
		t.Fatalf("an imported component or a variant was checked as a page: %v", r.Pages)
	}
	if strings.Contains(all, "content/ok.html") || strings.Contains(all, "pricing/index.html") {
		t.Fatalf("a valid file reported:\n%s", all)
	}
	if r.Err() == nil {
		t.Fatal("Err() is nil with errors present")
	}
	r2, err := e.CompileAll(context.Background(), "content", PageFilter(func(p string) bool { return strings.HasSuffix(p, "ok.html") }))
	if err != nil || len(r2.Pages) != 1 {
		t.Fatalf("PageFilter: %v %v", r2.Pages, err)
	}
}

func TestLoadSite(t *testing.T) {
	t.Parallel()
	fsys := fstest.MapFS{
		"site.yaml":              {Data: []byte("title: My Blog\nbaseURL: ${BASE_URL}\nmode: ${MODE:-live}\nlist: [\"${BASE_URL}/a\"]\n")},
		"data/pricing.yaml":      {Data: []byte("plans: [{name: Pro}]\n")},
		"data/team.json":         {Data: []byte(`{"lead":"Sara"}`)},
		"data/blog/authors.yaml": {Data: []byte("- ana\n")},
		"data/.DS_Store":         {Data: []byte("x")},
	}
	env := WithEnv(func(k string) (string, bool) {
		if k == "BASE_URL" {
			return "https://x.test", true
		}
		return "", false
	})
	site, err := LoadSite(fsys, "site.yaml", "data", env)
	if err != nil {
		t.Fatal(err)
	}
	e := newEngine(t, Options{Site: site, FS: theme("content/a.html", `{{ .Site.baseURL }} {{ .Site.mode }} {{ index .Site.list 0 }} {{ (index .Site.Data.pricing.plans 0).name }} {{ .Site.Data.team.lead }} {{ index .Site.Data.blog.authors 0 }}`)})
	if got := render(t, e, "content/a.html", nil); got != "https://x.test live https://x.test/a Pro Sara ana" {
		t.Fatalf("got %q", got)
	}
	// A6: mutating the host's map after New does not reach renders.
	site["title"] = "changed"
	e2 := newEngine(t, Options{Site: map[string]any{"t": "a"}, FS: theme("content/a.html", `{{ .Site.t }}`)})
	if got := render(t, e2, "content/a.html", nil); got != "a" {
		t.Fatalf("site copy: %q", got)
	}

	for name, files := range map[string]fstest.MapFS{
		"missing var": {"site.yaml": {Data: []byte("x: ${NOPE}\n")}},
		"clash":       {"site.yaml": {Data: []byte("x: 1\n")}, "data/a.yaml": {Data: []byte("a: 1\n")}, "data/a.json": {Data: []byte(`{}`)}},
		"stray file":  {"site.yaml": {Data: []byte("x: 1\n")}, "data/readme.txt": {Data: []byte("x")}},
		"data key":    {"site.yaml": {Data: []byte("Data: 1\n")}, "data/a.yaml": {Data: []byte("a: 1\n")}},
	} {
		if _, err := LoadSite(files, "site.yaml", "data", env); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
	// An env value is inserted as text, never parsed as YAML (D10).
	inj, err := LoadSite(fstest.MapFS{"site.yaml": {Data: []byte("x: ${V}\n")}}, "site.yaml", "", WithEnv(func(string) (string, bool) { return "a\ny: 1", true }))
	if err != nil || inj["x"] != "a\ny: 1" || inj["y"] != nil {
		t.Fatalf("injection: %v %v", inj, err)
	}
}

func TestLoadConfig(t *testing.T) {
	t.Parallel()
	c, err := LoadConfig(fstest.MapFS{"brick.yaml": {Data: []byte("roots: [components, ui]\nunknown: passthrough\nsyntax: capitalized\nbrick: \">=0.1, <2\"\n")}}, "brick.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var o Options
	if err := c.Apply(&o); err != nil || o.Unknown != UnknownPassthrough || o.Syntax != SyntaxCapitalized || len(o.Roots) != 2 {
		t.Fatalf("apply: %+v %v", o, err)
	}
	for name, src := range map[string]string{
		"typo":       "rots: [x]\n",
		"too new":    "brick: \">=9\"\n",
		"bad pin":    "brick: \"1.2\"\n",
		"bad syntax": "syntax: weird\n",
	} {
		c, err := LoadConfig(fstest.MapFS{"brick.yaml": {Data: []byte(src)}}, "brick.yaml")
		if err == nil {
			err = c.Apply(&Options{})
		}
		if err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

// A host-supplied parser reads a format the root does not bundle.
func TestDataFormats(t *testing.T) {
	t.Parallel()
	// A toy "key = value" format stands in for TOML.
	kv := func(b []byte) (any, error) {
		m := map[string]any{}
		for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
			k, v, _ := strings.Cut(line, "=")
			m[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
		return m, nil
	}
	e := newEngine(t, Options{DataFormats: map[string]DataFormat{".toml": kv}, FS: theme(
		"data/plan.toml", "name = Pro\n",
		"content/a.html", "---\nuse:\n  plan: ~/data/plan.toml\n---\n{{ .Data.plan.name }}",
	)})
	if got := render(t, e, "content/a.html", nil); got != "Pro" {
		t.Fatalf("got %q", got)
	}
	e2 := newEngine(t, Options{FS: theme("data/plan.toml", "x = 1", "content/a.html", "---\nuse:\n  plan: ~/data/plan.toml\n---\n")})
	if err := renderErr(t, e2, "content/a.html"); !strings.Contains(err.Error(), "Options.DataFormats") {
		t.Fatalf("no parser: %v", err)
	}
	site, err := LoadSite(fstest.MapFS{"data/plan.toml": {Data: []byte("name = Pro")}}, "", "data", WithDataFormat(".toml", kv))
	if err != nil || site["Data"].(map[string]any)["plan"].(map[string]any)["name"] != "Pro" {
		t.Fatalf("LoadSite: %v %v", site, err)
	}
	msgs, err := LoadMessages(fstest.MapFS{"i18n/en.toml": {Data: []byte("hi = Hello")}}, "i18n", WithDataFormat(".toml", kv))
	if err != nil || !msgs.has("en") {
		t.Fatalf("LoadMessages: %v %v", msgs, err)
	}
}
