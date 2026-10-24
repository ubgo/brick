package brick

import (
	"strings"
	"testing"
)

// kvFormat is a toy "key = value" parser standing in for a host format.
func kvFormat(b []byte) (any, error) {
	m := map[string]any{}
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		k, v, _ := strings.Cut(line, "=")
		m[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}
	return m, nil
}

// A format registered in Options.DataFormats under a non-built-in extension
// is usable from use: (the parser decides, not a fixed extension list), an
// unregistered one is still refused, and the refusal names the registered
// custom extensions so the host sees what would have worked.
func TestUseCustomDataFormat(t *testing.T) {
	t.Parallel()
	files := []string{
		"data/x.kv", "name = Pro\n",
		"data/y.xyz", "a = b\n",
		"content/a.html", "---\nuse:\n  c: ~/data/x.kv\n---\n{{ .Data.c.name }}",
		"content/b.html", "---\nuse:\n  c: ~/data/y.xyz\n---\n",
	}
	e := newEngine(t, Options{DataFormats: map[string]DataFormat{".kv": kvFormat}, FS: theme(files...)})
	if got := render(t, e, "content/a.html", nil); got != "Pro" {
		t.Fatalf("custom .kv via use: got %q", got)
	}
	err := renderErr(t, e, "content/b.html")
	for _, want := range []string{`".xyz" is not a component`, ".kv"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("unregistered .xyz error missing %q: %v", want, err)
		}
	}
	// Without the registration the same .kv file is refused too.
	e2 := newEngine(t, Options{FS: theme(files...)})
	if err := renderErr(t, e2, "content/a.html"); !strings.Contains(err.Error(), `".kv" is not a component`) {
		t.Fatalf("unregistered .kv: %v", err)
	}
}
