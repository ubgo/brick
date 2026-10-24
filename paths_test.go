package brick

import (
	"strings"
	"testing"
)

// Every path rule of X2 and X7, one row each, so breaking any guard fails.
func TestResolvePath(t *testing.T) {
	t.Parallel()
	ok := map[string]string{
		"~/components/Card.html": "components/Card.html",
		"~/a//b.html":            "a/b.html",
	}
	for in, want := range ok {
		got, err := resolvePath(in)
		if err != nil || got != want {
			t.Errorf("%q: got %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "components/Card.html", "/etc/passwd", "./Card.html", "../Card.html", ".", "..", "~/../x.html", "~/a/../../x.html", "~/a/./b.html", `~/a\b.html`, "~/", "~"} {
		if got, err := resolvePath(bad); err == nil {
			t.Errorf("%q: accepted as %q", bad, got)
		}
	}
}

func TestRemoteImports(t *testing.T) {
	t.Parallel()
	r, ok := ParseRemote("github.com/acme/kit/components/Card.html@v1.2.0")
	if !ok || r.VendorPath() != "brick_modules/github.com/acme/kit@v1.2.0/components/Card.html" || r.String() != "github.com/acme/kit/components/Card.html@v1.2.0" {
		t.Fatalf("parse: %+v %v", r, ok)
	}
	for _, bad := range []string{"~/components/Card.html", "github.com/acme/kit/Card.html", "github.com/acme/kit/../x.html@v1", "acme/kit/Card.html@v1"} {
		if _, ok := ParseRemote(bad); ok {
			t.Errorf("%q parsed as remote", bad)
		}
	}
	mod := "brick_modules/github.com/acme/kit@v1.2.0"
	e := newEngine(t, Options{FS: theme(
		mod+"/components/Card.html", "---\nuse:\n  Icon: ~/components/Icon.html\n---\n<div class=\"card\"><Icon />{{ children }}</div>",
		mod+"/components/Icon.html", `<i>★</i>`,
		"components/Icon.html", `<i>project icon</i>`,
		"content/a.html", "---\nuse:\n  Card: github.com/acme/kit/components/Card.html@v1.2.0\n---\n<Card>hi</Card>",
		"content/missing.html", "---\nuse:\n  Card: github.com/acme/kit/components/Card.html@v9\n---\n<Card />",
	)})
	if got := render(t, e, "content/a.html", nil); got != `<div class="card"><i>★</i>hi</div>` {
		t.Fatalf("~/ inside a module means the module: %q", got)
	}
	if err := renderErr(t, e, "content/missing.html"); !strings.Contains(err.Error(), "run `brick vendor`") {
		t.Fatalf("not vendored: %v", err)
	}
	if moduleRoot("components/Card.html") != "" || moduleRoot(mod+"/components/Card.html") != mod {
		t.Fatal("moduleRoot")
	}
}
