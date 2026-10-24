// Assertions on a live component and on the effects an action returned.

package bricktest

import (
	"encoding/json"
	"reflect"
	"slices"
	"testing"

	"github.com/ubgo/brick"
)

// AssertSet fails unless the state at the dotted path (a key, or a list
// index: "items.0.name") equals want on its JSON form, so 42, int64(42)
// and float64(42) match.
func (r *LiveRoot) AssertSet(t testing.TB, path string, want any) {
	t.Helper()
	got, ok := r.View().Get(path)
	if !ok {
		t.Fatalf("bricktest: %s has no state at %q", r.id, path)
	}
	if !sameJSON(got, want) {
		t.Fatalf("bricktest: %s state %q = %#v, want %#v", r.id, path, got, want)
	}
}

// AssertErrors fails unless every path has a rule error now; with no
// paths, unless there is at least one. These are the errors the next
// render shows as .Errors: rules: over touched fields.
// Errors an action returned are shown by its own render and never stored,
// so assert those on the HTML.
func (r *LiveRoot) AssertErrors(t testing.TB, paths ...string) {
	t.Helper()
	errs := r.View().Errors
	if len(paths) == 0 && len(errs) == 0 {
		t.Fatalf("bricktest: %s has no errors", r.id)
	}
	for _, p := range paths {
		if _, ok := errs[p]; !ok {
			t.Fatalf("bricktest: %s has no error at %q; errors: %v", r.id, p, errs)
		}
	}
}

// AssertNoErrors fails when any touched field has a rule error.
func (r *LiveRoot) AssertNoErrors(t testing.TB) {
	t.Helper()
	if errs := r.View().Errors; len(errs) > 0 {
		t.Fatalf("bricktest: %s has errors: %v", r.id, errs)
	}
}

// Effects returns the result's effects of one kind.
func Effects(res *brick.ActionResult, kind string) []brick.Effect {
	var out []brick.Effect
	if res == nil {
		return nil
	}
	for _, ef := range res.Effects {
		if ef.Kind == kind {
			out = append(out, ef)
		}
	}
	return out
}

// sameJSON compares two values on their JSON form, so 42 and int64(42)
// and float64(42) are equal.
func sameJSON(a, b any) bool {
	ja, err1 := json.Marshal(a)
	jb, err2 := json.Marshal(b)
	if err1 != nil || err2 != nil {
		return reflect.DeepEqual(a, b)
	}
	var va, vb any
	_ = json.Unmarshal(ja, &va)
	_ = json.Unmarshal(jb, &vb)
	return reflect.DeepEqual(va, vb)
}

// AssertDispatched fails unless res dispatched name; data, when not nil,
// must match on its JSON form.
func AssertDispatched(t testing.TB, res *brick.ActionResult, name string, data map[string]any) {
	t.Helper()
	for _, ef := range Effects(res, brick.EffectDispatch) {
		if ef.Name == name && (data == nil || sameJSON(ef.Data, data)) {
			return
		}
	}
	t.Fatalf("bricktest: %q with %v was not dispatched; effects: %+v", name, data, effectsOf(res))
}

// AssertNotDispatched fails when res dispatched name.
func AssertNotDispatched(t testing.TB, res *brick.ActionResult, name string) {
	t.Helper()
	for _, ef := range Effects(res, brick.EffectDispatch) {
		if ef.Name == name {
			t.Fatalf("bricktest: %q was dispatched: %+v", name, ef)
		}
	}
}

// AssertRedirect fails unless res redirects to url.
func AssertRedirect(t testing.TB, res *brick.ActionResult, url string) {
	t.Helper()
	assertURL(t, res, brick.EffectRedirect, url)
}

// AssertNavigate fails unless res navigates to url.
func AssertNavigate(t testing.TB, res *brick.ActionResult, url string) {
	t.Helper()
	assertURL(t, res, brick.EffectNavigate, url)
}

// AssertDownload fails unless res downloads url.
func AssertDownload(t testing.TB, res *brick.ActionResult, url string) {
	t.Helper()
	assertURL(t, res, brick.EffectDownload, url)
}

func assertURL(t testing.TB, res *brick.ActionResult, kind, url string) {
	t.Helper()
	for _, ef := range Effects(res, kind) {
		if ef.URL == url {
			return
		}
	}
	t.Fatalf("bricktest: no %s to %q; effects: %+v", kind, url, effectsOf(res))
}

// AssertNoRedirect fails when res leaves the page (redirect or navigate).
func AssertNoRedirect(t testing.TB, res *brick.ActionResult) {
	t.Helper()
	if n := len(Effects(res, brick.EffectRedirect)) + len(Effects(res, brick.EffectNavigate)); n > 0 {
		t.Fatalf("bricktest: the action leaves the page; effects: %+v", effectsOf(res))
	}
}

// AssertFlash fails unless res flashed message at level.
func AssertFlash(t testing.TB, res *brick.ActionResult, level, message string) {
	t.Helper()
	for _, ef := range Effects(res, brick.EffectFlash) {
		if ef.Level == level && ef.Message == message {
			return
		}
	}
	t.Fatalf("bricktest: no %s flash %q; effects: %+v", level, message, effectsOf(res))
}

// AssertTitle fails unless the last title res set is text.
func AssertTitle(t testing.TB, res *brick.ActionResult, text string) {
	t.Helper()
	titles := Effects(res, brick.EffectTitle)
	if len(titles) == 0 || titles[len(titles)-1].Text != text {
		t.Fatalf("bricktest: title is not %q; effects: %+v", text, effectsOf(res))
	}
}

// AssertRenderSkipped fails unless the action skipped its re-render
// (brick.skip_render).
func AssertRenderSkipped(t testing.TB, res *brick.ActionResult) {
	t.Helper()
	if res == nil || !res.SkipRender {
		t.Fatalf("bricktest: the action re-rendered; it was expected to skip the render")
	}
}

// AssertRendered fails when the action skipped its re-render.
func AssertRendered(t testing.TB, res *brick.ActionResult) {
	t.Helper()
	if res == nil || res.SkipRender {
		t.Fatalf("bricktest: the action skipped its render (brick.skip_render)")
	}
}

// AssertEffect fails unless res queued an effect matching want: Kind must
// be equal, and so must every other field want sets (a zero field is not
// compared; Data and Rows compare on their JSON form). It is the general
// form of AssertDispatched: brick's client-side
// commands (brick:js.*) run in the browser with no request, so what an
// action sends the client is exactly its effects, custom x- kinds
// included.
func AssertEffect(t testing.TB, res *brick.ActionResult, want brick.Effect) {
	t.Helper()
	if want.Kind == "" {
		t.Fatalf("bricktest: AssertEffect needs want.Kind")
	}
	for _, ef := range Effects(res, want.Kind) {
		if effectMatches(ef, want) {
			return
		}
	}
	t.Fatalf("bricktest: no effect matching %+v; effects: %+v", want, effectsOf(res))
}

// AssertNoEffect fails when res queued any effect of kind.
func AssertNoEffect(t testing.TB, res *brick.ActionResult, kind string) {
	t.Helper()
	if got := Effects(res, kind); len(got) > 0 {
		t.Fatalf("bricktest: the action queued %s: %+v", kind, got)
	}
}

// AssertEffects fails unless the kinds of res's effects are exactly kinds,
// in order; no kinds asserts the action queued nothing.
func AssertEffects(t testing.TB, res *brick.ActionResult, kinds ...string) {
	t.Helper()
	got := make([]string, 0, len(effectsOf(res)))
	for _, ef := range effectsOf(res) {
		got = append(got, ef.Kind)
	}
	if !slices.Equal(got, kinds) {
		t.Fatalf("bricktest: effect kinds are %v, want %v; effects: %+v", got, kinds, effectsOf(res))
	}
}

// effectMatches compares the fields want sets. The zero-means-unset rule
// is why a test cannot ask for Replace false or Limit 0 here; it checks
// those through Effects directly.
func effectMatches(ef, want brick.Effect) bool {
	strs := [][2]string{
		{ef.Name, want.Name}, {ef.To, want.To}, {ef.URL, want.URL}, {ef.Filename, want.Filename},
		{ef.Message, want.Message}, {ef.Level, want.Level}, {ef.Text, want.Text}, {ef.Ref, want.Ref},
		{ef.Op, want.Op}, {ef.At, want.At},
	}
	for _, p := range strs {
		if p[1] != "" && p[0] != p[1] {
			return false
		}
	}
	switch {
	case want.Replace && !ef.Replace,
		want.Limit != 0 && ef.Limit != want.Limit,
		want.Data != nil && !sameJSON(ef.Data, want.Data),
		want.Rows != nil && !sameJSON(ef.Rows, want.Rows),
		want.Keys != nil && !slices.Equal(ef.Keys, want.Keys):
		return false
	}
	return true
}

func effectsOf(res *brick.ActionResult) []brick.Effect {
	if res == nil {
		return nil
	}
	return res.Effects
}
