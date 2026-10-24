package bricktest_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/ubgo/brick"
	"github.com/ubgo/brick/bricktest"
)

// fakeT records a fatal instead of stopping the test, so a test can check
// that a harness assertion fails.
type fakeT struct {
	testing.TB
	failed string
}

func (f *fakeT) Helper() {}

func (f *fakeT) Fatalf(format string, args ...any) {
	f.failed = fmt.Sprintf(format, args...)
	panic(f)
}

func (f *fakeT) Logf(string, ...any) {}

// fails reports whether fn calls t.Fatalf, logging the message.
func fails(t *testing.T, fn func(t testing.TB)) (failed bool) {
	ft := &fakeT{}
	defer func() {
		if r := recover(); r != nil {
			if r != ft {
				panic(r)
			}
			t.Log(ft.failed)
			failed = true
		}
	}()
	fn(ft)
	return false
}

const signupComp = `---
state:
  name: ""
  email: ""
  count: 0
  got: {}
  tags: [a, b]
brick:
  rules:
    name: [required, {min: 2}]
    email: [required, email]
---
<brick:script>signup</brick:script>
<div>
<form id="signup" brick:submit="save">
  <input brick:model.defer="name" value="{{ .State.name }}">
  <input brick:model.defer="email" value="{{ .State.email }}">
  <input name="note" value="hi">
  <input type="checkbox" name="news" value="yes">
  <select name="plan"><option value="a">A</option><option value="b" selected>B</option></select>
  <textarea name="bio">x &amp; y</textarea>
  <button type="submit">Go</button>
</form>
<button id="three" class="btn primary" brick:click='$set("count", 3)'>three</button>
<span class="n">{{ .State.count }}</span>
<Child />
</div>`

const childComp = `---
state:
  n: 0
---
<p>
  <button class="up" brick:click="$parent.bump">up</button>
  <button class="ping" brick:click='$dispatch("pinged", {"id": 7})'>ping</button>
  <a data-x="1" brick:click.once="inc"><i>inc</i></a>
  {{ .State.n }}
</p>`

func signupEngine(t *testing.T) *brick.Engine {
	t.Helper()
	st := func(a []any) map[string]any { s, _ := a[0].(map[string]any); return s }
	sc := scripting{
		"signup": {
			"save": func(_ context.Context, a ...any) (any, error) {
				s := st(a)
				if len(a) > 2 {
					s["got"] = a[2]
				}
				return s, nil
			},
			"bump": func(_ context.Context, a ...any) (any, error) {
				s := st(a)
				s["count"] = toInt(s["count"]) + 10
				return s, nil
			},
			"heard": func(_ context.Context, a ...any) (any, error) {
				s := st(a)
				s["count"] = -1
				return s, nil
			},
		},
		"child": {
			"inc": func(_ context.Context, a ...any) (any, error) {
				s := st(a)
				s["n"] = toInt(s["n"]) + 1
				return s, nil
			},
		},
	}
	return bricktest.Engine(t, brick.Options{Scripting: sc, States: brick.NewMemoryStates(0, 0)}, bricktest.Theme(
		"components/Signup.html", strings.Replace(signupComp, "brick:\n  rules:", "brick:\n  on:\n    pinged: heard\n  rules:", 1),
		"components/Child.html", strings.Replace(childComp, "---\n<p>", "---\n<brick:script>child</brick:script>\n<p>", 1),
		"content/a.html", `<main><Signup /></main>`,
	))
}

func toInt(v any) int64 {
	switch x := v.(type) {
	case int64:
		return x
	case int:
		return int64(x)
	case float64:
		return int64(x)
	}
	return 0
}

func TestLiveStateAndErrors(t *testing.T) {
	live := bricktest.Live(t, signupEngine(t), "content/a.html")
	s := live.Component("Signup")
	s.AssertSet(t, "count", 0)
	s.AssertSet(t, "tags.1", "b")
	s.AssertNoErrors(t)
	// The state is a copy.
	s.State()["count"] = 99
	s.AssertSet(t, "count", 0)

	s.Set("name", "A")
	s.AssertSet(t, "name", "A")
	s.AssertErrors(t, "name")
	s.AssertErrors(t)
	if v := s.View(); len(v.Touched) != 1 || v.Touched[0] != "name" || v.Component != "Signup" {
		t.Fatalf("view: %+v", v)
	}
	s.Set("name", "Ada")
	s.AssertNoErrors(t)

	// Each assertion fails when it should.
	for name, fn := range map[string]func(testing.TB){
		"wrong value":   func(ft testing.TB) { s.AssertSet(ft, "name", "Bob") },
		"missing path":  func(ft testing.TB) { s.AssertSet(ft, "nope.x", 1) },
		"bad index":     func(ft testing.TB) { s.AssertSet(ft, "tags.9", "a") },
		"no such error": func(ft testing.TB) { s.AssertErrors(ft, "email") },
		"no errors":     func(ft testing.TB) { s.AssertErrors(ft) },
	} {
		if !fails(t, fn) {
			t.Errorf("%s: the assertion passed", name)
		}
	}
	s.Set("email", "nope")
	if !fails(t, s.AssertNoErrors) {
		t.Fatal("AssertNoErrors passed with an email error")
	}
}

func TestLiveClick(t *testing.T) {
	live := bricktest.Live(t, signupEngine(t), "content/a.html")
	s := live.Component("Signup")
	s.Click("button#three.primary")
	s.AssertSet(t, "count", 3)
	// A click inside a nested root runs on that root; $parent goes up.
	s.Click("a[data-x=1] i")
	live.Component("Child").AssertSet(t, "n", 1)
	s.Click(`[class="up"]`)
	s.AssertSet(t, "count", 13)
	live.Component("Child").AssertSet(t, "n", 1)
	// $dispatch is delivered like an action's dispatch.
	res := s.Click("button.ping")
	bricktest.AssertDispatched(t, res, "pinged", map[string]any{"id": 7})
	live.Deliver(res)
	s.AssertSet(t, "count", -1)

	for name, sel := range map[string]string{
		"no match":     "button.missing",
		"no click":     "span.n",
		"unsupported":  "div > button",
		"pseudo":       "button:first-child",
		"empty":        "  ",
		"open bracket": "[brick:click",
	} {
		if !fails(t, func(ft testing.TB) {
			bricktest.Live(ft, signupEngine(t), "content/a.html").Component("Signup").Click(sel)
		}) {
			t.Errorf("%s: Click(%q) did not fail", name, sel)
		}
	}
}

func TestLiveSubmit(t *testing.T) {
	live := bricktest.Live(t, signupEngine(t), "content/a.html")
	s := live.Component("Signup")
	s.Submit("form#signup", map[string]any{"name": "Ada", "email": "ada@example.com", "note": 5, "news": "yes"})
	s.AssertSet(t, "name", "Ada")
	s.AssertSet(t, "email", "ada@example.com")
	// The named fields arrive as FormData would: strings, rendered values
	// under the given ones, an unchecked box only when set.
	s.AssertSet(t, "got", map[string]any{"note": "5", "news": "yes", "plan": "b", "bio": "x & y"})
	// The deferred sets touched their fields.
	s.AssertNoErrors(t)
	if v := s.View(); len(v.Touched) != 2 {
		t.Fatalf("touched %v", v.Touched)
	}

	s.Submit("form", map[string]any{"name": "A"})
	s.AssertErrors(t, "name")
	s.AssertSet(t, "got", map[string]any{"note": "hi", "plan": "b", "bio": "x & y"})

	for name, fn := range map[string]func(ft testing.TB){
		"unknown field": func(ft testing.TB) {
			bricktest.Live(ft, signupEngine(t), "content/a.html").Component("Signup").Submit("form", map[string]any{"nmae": "x"})
		},
		"not a form": func(ft testing.TB) {
			bricktest.Live(ft, signupEngine(t), "content/a.html").Component("Signup").Submit("button#three", nil)
		},
		"refused set": func(ft testing.TB) {
			bricktest.Live(ft, signupEngine(t), "content/a.html").Component("Signup").Submit("form", map[string]any{"name": []any{1}})
		},
	} {
		if !fails(t, fn) {
			t.Errorf("%s: Submit did not fail", name)
		}
	}
}

func TestLiveDump(t *testing.T) {
	live := bricktest.Live(t, signupEngine(t), "content/a.html")
	p := live.Dump()
	defer os.Remove(p)
	b, err := os.ReadFile(p)
	if err != nil || string(b) != live.HTML() {
		t.Fatalf("dump %s: %v", p, err)
	}
}
