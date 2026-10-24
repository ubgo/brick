package brick

import (
	"context"
	"slices"
	"strings"
	"testing"
)

// InheritAssets keeps a replaced engine's asset files servable, so a page
// cached before a rebuild still finds its stylesheet; the new engine's own
// files are never shadowed, and nil or the same engine is a no-op.
func TestInheritAssets(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	oldEng := newEngine(t, Options{FS: theme(
		"components/S.html", "<brick:style>.s { color: red }</brick:style><b class=\"s\">s</b>",
		"content/a.html", "{{ assets }}<S />",
	)})
	if _, err := oldEng.Render(ctx, "content/a.html", nil); err != nil {
		t.Fatal(err)
	}
	newEng := newEngine(t, Options{FS: theme(
		"components/S.html", "<brick:style>.s { color: blue }</brick:style><b class=\"s\">s</b>",
		"content/a.html", "{{ assets }}<S />",
	)})
	if _, err := newEng.Render(ctx, "content/a.html", nil); err != nil {
		t.Fatal(err)
	}
	oldFiles, newFiles := listAssetFiles(t, oldEng), listAssetFiles(t, newEng)
	if !slices.ContainsFunc(oldFiles, func(f string) bool { return !slices.Contains(newFiles, f) }) {
		t.Fatalf("setup: the old engine has no file the new one lacks: %v / %v", oldFiles, newFiles)
	}
	newEng.InheritAssets(nil)
	newEng.InheritAssets(newEng)
	if got := listAssetFiles(t, newEng); !slices.Equal(got, newFiles) {
		t.Fatalf("nil or self changed the files: %v, want %v", got, newFiles)
	}
	newEng.InheritAssets(oldEng)
	got := listAssetFiles(t, newEng)
	for _, f := range append(slices.Clone(oldFiles), newFiles...) {
		if !slices.Contains(got, f) {
			t.Errorf("%s is not served after InheritAssets: %v", f, got)
		}
	}
	for _, f := range newFiles {
		b, err := newEng.Assets().Open(f)
		if err != nil {
			t.Fatal(err)
		}
		_ = b.Close()
	}
}

// A component chosen from data at fill time that carries a script brings
// the browser runtime with it, even onto a page that linked no script.
func TestFillLinksRuntimeForPickedScript(t *testing.T) {
	t.Parallel()
	e := newEngine(t, Options{FS: theme(
		"components/Picked.html", "<brick:client>brick.mount(\"Picked\", () => {})</brick:client><u>p</u>",
		"components/Isl.html", "---\nbrick:\n  island: true\n---\n<div><brick:render :name=\"`Picked`\" /></div>",
		"content/a.html", "{{ assets }}<Isl />",
	)})
	ctx := context.Background()
	r, err := e.RenderChunks(ctx, "content/a.html", nil)
	if err != nil {
		t.Fatal(err)
	}
	out, err := e.Fill(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"/_brick/c/runtime.", "/_brick/c/Picked."} {
		if strings.Count(string(out), want) != 1 {
			t.Errorf("want %q linked once in\n%s", want, out)
		}
	}
}

// With navigation on, a Fill that adds a script links runtime.js before
// nav.js, each once: the order every page uses.
func TestFillRuntimeOrderWithNavigation(t *testing.T) {
	t.Parallel()
	e := newEngine(t, Options{Navigation: true, FS: theme(
		"components/Picked.html", "<brick:client>brick.mount(\"Picked\", () => {})</brick:client><u>p</u>",
		"components/Isl.html", "---\nbrick:\n  island: true\n---\n<div><brick:render :name=\"`Picked`\" /></div>",
		"content/a.html", "<p>no script here</p>{{ assets }}<Isl />",
	)})
	ctx := context.Background()
	r, err := e.RenderChunks(ctx, "content/a.html", nil)
	if err != nil {
		t.Fatal(err)
	}
	out, err := e.Fill(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	page := string(out)
	for _, want := range []string{"/_brick/c/runtime.", "/_brick/c/nav.", "/_brick/c/Picked."} {
		if strings.Count(page, want) != 1 {
			t.Errorf("want %q linked once in\n%s", want, page)
		}
	}
	if rt, nav := strings.Index(page, "/_brick/c/runtime."), strings.Index(page, "/_brick/c/nav."); rt > nav {
		t.Errorf("nav.js is linked before runtime.js:\n%s", page)
	}
}

// mergeAssets itself: when both runtime files are missing (a Result whose
// page linked no runtime), they are prepended as one block in
// runtimeAssets' order — mount runtime, then navigation — before the
// added component's files. (Render always links the runtime on a
// navigation page, so only a direct call reaches this path today.)
func TestMergeAssetsPrependsRuntimeInOrder(t *testing.T) {
	t.Parallel()
	e := newEngine(t, Options{Navigation: true, FS: theme(
		"components/Picked.html", "<brick:client>brick.mount(\"Picked\", () => {})</brick:client><u>p</u>",
		"content/a.html", "<Picked />",
	)})
	if _, err := e.Render(context.Background(), "content/a.html", nil); err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, a := range e.mergeAssets(nil, []string{"components/Picked.html"}) {
		name, _, _ := strings.Cut(a.URL[strings.LastIndex(a.URL, "/")+1:], ".")
		names = append(names, name)
	}
	if want := []string{"runtime", "nav", "Picked"}; !slices.Equal(names, want) {
		t.Fatalf("order %v, want %v", names, want)
	}
}
