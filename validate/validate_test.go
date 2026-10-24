package validate

import (
	"context"
	"strings"
	"testing"
)

// The default rules, one row per pass and fail edge.
func TestDefaultRules(t *testing.T) {
	t.Parallel()
	v := Default()
	state := map[string]any{"password": "hunter2", "items": []any{map[string]any{"qty": int64(3)}}}
	cases := []struct {
		name  string
		value any
		rule  Rule
		want  string
	}{
		{"required empty", "", Rule{Name: Required}, "This field is required."},
		{"required blank", "  ", Rule{Name: Required}, "This field is required."},
		{"required nil", nil, Rule{Name: Required}, "This field is required."},
		{"required empty list", []any{}, Rule{Name: Required}, "This field is required."},
		{"required false is a value", false, Rule{Name: Required}, ""},
		{"required zero is a value", int64(0), Rule{Name: Required}, ""},
		{"min string", "A", Rule{Name: Min, Arg: 2}, "Must be at least 2 characters."},
		{"min runes", "éé", Rule{Name: Min, Arg: 2}, ""},
		{"min empty passes", "", Rule{Name: Min, Arg: 2}, ""},
		{"min number", int64(1), Rule{Name: Min, Arg: 2}, "Must be at least 2."},
		{"min float", 2.5, Rule{Name: Min, Arg: 2}, ""},
		{"min list", []any{"a"}, Rule{Name: Min, Arg: 2}, "Must be at least 2 items."},
		{"max string", "abcd", Rule{Name: Max, Arg: 3}, "Must be at most 3 characters."},
		{"max number", 3.5, Rule{Name: Max, Arg: 3}, "Must be at most 3."},
		{"max bool passes", true, Rule{Name: Max, Arg: 0}, ""},
		{"email ok", "ada@example.com", Rule{Name: Email}, ""},
		{"email no domain dot", "ada@example", Rule{Name: Email}, "Enter a valid email address."},
		{"email display name", "Ada <ada@example.com>", Rule{Name: Email}, "Enter a valid email address."},
		{"email junk", "nope", Rule{Name: Email}, "Enter a valid email address."},
		{"url ok", "https://example.com/x", Rule{Name: URL}, ""},
		{"url scheme", "javascript:alert(1)", Rule{Name: URL}, "Enter a valid URL."},
		{"url no host", "https://", Rule{Name: URL}, "Enter a valid URL."},
		{"pattern whole value", "abc1", Rule{Name: Pattern, Arg: "[a-z]+"}, "The format is not valid."},
		{"pattern ok", "abc", Rule{Name: Pattern, Arg: "[a-z]+"}, ""},
		{"in ok", "b", Rule{Name: In, Arg: []any{"a", "b"}}, ""},
		{"in number as text", int64(2), Rule{Name: In, Arg: []any{1, 2}}, ""},
		{"in bad", "c", Rule{Name: In, Arg: []any{"a", "b"}}, "Choose one of the listed values."},
		{"in list", []any{"a", "c"}, Rule{Name: In, Arg: []any{"a", "b"}}, "Choose one of the listed values."},
		{"same ok", "hunter2", Rule{Name: Same, Arg: "password"}, ""},
		{"same bad", "hunter3", Rule{Name: Same, Arg: "password"}, "Must match password."},
		{"same empty vs set", "", Rule{Name: Same, Arg: "password"}, "Must match password."},
		{"same nested", int64(3), Rule{Name: Same, Arg: "items.0.qty"}, ""},
	}
	for _, tc := range cases {
		if err := v.Check("f", tc.rule); err != nil {
			t.Errorf("%s: Check: %v", tc.name, err)
			continue
		}
		if got := v.Validate(context.Background(), "f", tc.value, []Rule{tc.rule}, state, "en"); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

// The first failing rule wins, in the order written.
func TestFirstFailingRule(t *testing.T) {
	t.Parallel()
	got := Default().Validate(context.Background(), "f", "", []Rule{{Name: Required}, {Name: Min, Arg: 2}}, nil, "")
	if got != "This field is required." {
		t.Fatal(got)
	}
}

func TestCheck(t *testing.T) {
	t.Parallel()
	v := Default()
	bad := []Rule{
		{Name: "requird"}, {Name: Required, Arg: true}, {Name: Min}, {Name: Min, Arg: -1}, {Name: Max, Arg: "3"},
		{Name: Pattern}, {Name: Pattern, Arg: "("}, {Name: In}, {Name: In, Arg: []any{}}, {Name: Same}, {Name: Same, Arg: "f"},
	}
	for _, r := range bad {
		if err := v.Check("f", r); err == nil {
			t.Errorf("Check(%+v) passed", r)
		}
	}
}

func TestLookup(t *testing.T) {
	t.Parallel()
	st := map[string]any{"a": map[string]any{"b": []any{"x", "y"}}}
	if Lookup(st, "a.b.1") != "y" || Lookup(st, "a.b.2") != nil || Lookup(st, "a.c") != nil || Lookup(st, "a.b.-1") != nil {
		t.Fatal("lookup")
	}
}

// Match covers one list index per *, nothing else.
func TestMatch(t *testing.T) {
	t.Parallel()
	cases := []struct {
		pattern, path string
		want          bool
	}{
		{"todos.*.title", "todos.2.title", true},
		{"todos.*.title", "todos.12.title", true},
		{"todos.*.title", "todos.x.title", false},
		{"todos.*.title", "todos.2", false},
		{"todos.*.title", "todos.2.title.x", false},
		{"todos.*.tags.*", "todos.0.tags.3", true},
		{"todos.*.title", "todos.*.title", true},
		{"name", "name", true},
		{"name", "other", false},
	}
	for _, c := range cases {
		if got := Match(c.pattern, c.path); got != c.want {
			t.Errorf("Match(%q, %q) = %v, want %v", c.pattern, c.path, got, c.want)
		}
	}
}

// Expand: one path per element, nested lists multiply, an empty list and a
// non-list under * give nothing, a non-dict element still names its field.
func TestExpand(t *testing.T) {
	t.Parallel()
	state := map[string]any{
		"todos": []any{
			map[string]any{"title": "a", "tags": []any{"x", "y"}},
			"scalar",
			map[string]any{"tags": "not a list"},
		},
		"empty": []any{},
		"dict":  map[string]any{"a": 1},
		"name":  "x",
	}
	cases := map[string]string{
		"todos.*.title":  "todos.0.title todos.1.title todos.2.title",
		"todos.*.tags.*": "todos.0.tags.0 todos.0.tags.1",
		"empty.*":        "",
		"empty.*.title":  "",
		"dict.*":         "",
		"name.*":         "",
		"missing.*":      "",
		"name":           "name",
	}
	for pattern, want := range cases {
		if got := strings.Join(Expand(state, pattern), " "); got != want {
			t.Errorf("Expand(%q) = %q, want %q", pattern, got, want)
		}
	}
}

// Exists: an index past the end is gone; a missing dict key is not.
func TestExists(t *testing.T) {
	t.Parallel()
	state := map[string]any{"todos": []any{map[string]any{"title": "a"}}}
	for path, want := range map[string]bool{
		"todos.0.title": true,
		"todos.0.other": true,
		"todos.1.title": false,
		"todos.x.title": false,
		"name":          true,
	} {
		if got := Exists(state, path); got != want {
			t.Errorf("Exists(%q) = %v, want %v", path, got, want)
		}
	}
}
