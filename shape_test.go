package brick

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

// shapeCard declares every kind of shape G15 supports.
const shapeCard = `---
props:
  user:
    type: map
    required: true
    fields:
      name: {type: string, required: true}
      email: string
      age: int
      address: {type: map, fields: {city: string}}
  posts: {type: list, of: {type: map, fields: {title: string}}}
  byId: {type: map, of: {type: map, fields: {name: string}}}
  fallback: {type: map, fields: {name: string}, default: {name: x, nmae: y}}
---
<p>{{ .Props.user.name }} {{ .Props.user.emial }}</p>
<p>{{ .Props.user.address.city }} {{ .Props.user.address.ciy }}</p>
{{ range .Props.posts }}{{ .title }}{{ .titel }}{{ end }}
{{ range $i, $p := .Props.posts }}{{ $p.titl }}{{ end }}
{{ with .Props.user }}{{ .name }}{{ .nam }}{{ end }}
{{ $u := .Props.user }}{{ $u.agee }}
{{ index .Props.user "mail" }}
{{ .Props.byId.anyKey.name }}{{ .Props.byId.anyKey.nme }}
<Box>{{ .Props.user.bodyBad }}</Box>
{{ .Props.undeclared.whatever }}{{ .Props.user.name.len }}{{ $.Props.user.rootBad }}
`

// shapePage passes UserCard values the checker can see, and some it cannot.
const shapePage = `---
use:
  people: ~/data/people.yaml
author: {name: Ann, email: a@x}
who: bob
---
<UserCard :user="dict ` + "`name` `Ann` `emial` `x`" + `" />
<UserCard :user="dict ` + "`email` `x`" + `" />
<UserCard :user="dict ` + "`name` `Ann` `age` `old`" + `" />
<UserCard :user=".Params.author" />
<UserCard :user=".Data.people.bob" />
<UserCard :user="index .Data.people .Params.who" />
<UserCard :user="dict ` + "`name` `A`" + `" :posts="list (dict ` + "`title` `t`" + `) (dict ` + "`titel` `u`" + `)" />
<UserCard :user="dict ` + "`name` `A` `address` (dict `town` `x`)" + `" />
`

// shapeCaller is a component whose own shape is passed on.
const shapeCaller = `---
props:
  author: {type: map, fields: {email: string}}
  full: {type: map, fields: {name: string, extra: string}}
---
<UserCard :user=".Props.author" /><UserCard :user=".Props.full" />
`

func shapeReport(t *testing.T) (string, []Diagnostic) {
	t.Helper()
	e := newEngine(t, Options{FS: theme(
		"components/UserCard.html", shapeCard,
		"components/Box.html", `<div>{{ children }}</div>`,
		"components/Caller.html", shapeCaller,
		"data/people.yaml", "bob: {email: b@x}\nann: {name: Ann}\n",
		"content/index.html", shapePage,
		"content/caller.html", `<Caller />`,
	)})
	r, err := e.CompileAll(context.Background(), "content")
	if err != nil {
		t.Fatal(err)
	}
	var msgs []string
	var shapes []Diagnostic
	for _, d := range r.Diagnostics {
		msgs = append(msgs, d.String())
		if d.Code == CodeShape {
			shapes = append(shapes, d)
		}
	}
	return strings.Join(msgs, "\n"), shapes
}

// shapeLine is the 1-based line of the first occurrence of s in src.
func shapeLine(t *testing.T, src, s string) int {
	t.Helper()
	i := strings.Index(src, s)
	if i < 0 {
		t.Fatalf("%q not in source", s)
	}
	return strings.Count(src[:i], "\n") + 1
}

func TestShapeReads(t *testing.T) {
	t.Parallel()
	all, shapes := shapeReport(t)
	for _, d := range shapes {
		if d.Severity != SeverityWarning {
			t.Fatalf("a shape finding is not a warning: %s", d)
		}
	}
	for _, c := range []struct{ token, msg string }{
		{"emial", `.Props.user.emial: "emial" is not a field of .Props.user (fields: address, age, email, name)`},
		{"ciy", `.Props.user.address.ciy: "ciy" is not a field of .Props.user.address (fields: city)`},
		{"titel", `.Props.posts[].titel`},
		{"$p.titl", `.Props.posts[].titl`},
		{"nam }}", `.Props.user.nam:`},
		{"agee", `.Props.user.agee:`},
		{`"mail"`, `.Props.user.mail:`},
		{"bodyBad", `.Props.user.bodyBad:`},
		{"rootBad", `.Props.user.rootBad:`},
	} {
		want := "components/UserCard.html:" + itoa(shapeLine(t, shapeCard, c.token)) + ":"
		found := false
		for _, d := range shapes {
			if d.File == "components/UserCard.html" && strings.Contains(d.String(), want) && strings.Contains(d.Message, strings.TrimSuffix(c.msg, "(fields")) {
				found = true
			}
		}
		if !found {
			t.Errorf("no warning %q at %s in\n%s", c.msg, want, all)
		}
	}
	for _, valid := range []string{".Props.user.name:", ".Props.user.address.city:", "[].title:", "undeclared", "name.len", ".anyKey.name:"} {
		if strings.Contains(all, valid) {
			t.Errorf("a valid or unknown read warned (%s):\n%s", valid, all)
		}
	}
}

func TestShapeMapOfRead(t *testing.T) {
	t.Parallel()
	all, _ := shapeReport(t)
	if !strings.Contains(all, `.Props.byId["anyKey"].nme: "nme" is not a field`) {
		t.Fatalf("map of: read not checked:\n%s", all)
	}
}

func TestShapeCallSites(t *testing.T) {
	t.Parallel()
	all, _ := shapeReport(t)
	at := func(token string) string { return "content/index.html:" + itoa(shapeLine(t, shapePage, token)) + ":" }
	for _, c := range []struct{ at, msg string }{
		{at("`emial`"), `<UserCard> user: "emial" is not a field of user`},
		{at("`email` `x`\""), `<UserCard> user: the required field "name" is missing`},
		{at("`old`"), `<UserCard> user.age: "old" is not a int`},
		{at(".Data.people.bob"), `<UserCard> user: the required field "name" is missing`},
		{at("`titel`"), `<UserCard> posts[1]: "titel" is not a field of posts[1]`},
		{at("`town`"), `<UserCard> user.address: "town" is not a field`},
	} {
		if !strings.Contains(all, c.at) || !lineHas(all, c.at, c.msg) {
			t.Errorf("missing %s %s in\n%s", c.at, c.msg, all)
		}
	}
	for _, quiet := range []string{at(".Params.author"), at("index .Data.people")} {
		if strings.Contains(all, quiet) {
			t.Errorf("a fitting or unknown value warned at %s:\n%s", quiet, all)
		}
	}
}

func TestShapeCallerShape(t *testing.T) {
	t.Parallel()
	all, _ := shapeReport(t)
	if !strings.Contains(all, "components/Caller.html:"+itoa(shapeLine(t, shapeCaller, ".Props.author\""))+":") || !strings.Contains(all, `user: the required field "name" is not in the shape of .Props.author (fields: email)`) {
		t.Fatalf("caller shape missing a required field not reported:\n%s", all)
	}
	if strings.Contains(all, ".Props.full") {
		t.Fatalf("a caller shape with extra fields warned:\n%s", all)
	}
}

func TestShapeDefault(t *testing.T) {
	t.Parallel()
	all, _ := shapeReport(t)
	if !strings.Contains(all, "components/UserCard.html:"+itoa(shapeLine(t, shapeCard, "fallback:"))+": warning: default of fallback: \"nmae\" is not a field of fallback (fields: name)") {
		t.Fatalf("a default with a misspelled field not reported:\n%s", all)
	}
}

// TestShapeNoRuntimeCost pins that a shape never changes a render: a value
// missing fields, and a loader-free Go value, render as an undeclared map
// would.
func TestShapeNoRuntimeCost(t *testing.T) {
	t.Parallel()
	e := newEngine(t, Options{FS: theme(
		"components/U.html", "---\nprops:\n  user: {type: map, fields: {name: {type: string, required: true}}}\n---\n<b>{{ .Props.user.name }}</b>",
		"content/p.html", "<U :user=\"dict `other` 1\" />",
	)})
	if got := render(t, e, "content/p.html", nil); !strings.Contains(got, "<b>") {
		t.Fatalf("render changed by a shape: %q", got)
	}
}

func TestShapeParseErrors(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ decl, want string }{
		{"{type: string, fields: {a: string}}", "fields: needs type: map"},
		{"{type: int, of: string}", "of: needs type: list or type: map"},
		{"{type: map, fields: {a: string}, of: string}", "not both"},
		{"{type: map, fields: {a: {type: string, default: x}}}", `x.a: unknown setting "default"`},
		{"{type: map, fields: {a: {type: string, client: true}}}", `unknown setting "client"`},
		{"{type: map, fields: {a: strng}}", `x.a: unknown type "strng"`},
		{"{type: map, fields: []}", "fields: must map field names"},
		{"{type: list, of: {type: map, fields: {b: nope}}}", `unknown type "nope"`},
	} {
		_, err := parseProps(map[string]any{"x": mustYAML(t, c.decl)}, nil)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: got %v, want %q", c.decl, err, c.want)
		}
	}
	d, err := parseProps(map[string]any{"x": mustYAML(t, "{type: list, of: {type: map, fields: {a: {type: int, required: true}}}}")}, nil)
	if err != nil || d["x"].Of == nil || !d["x"].Of.Fields["a"].Required || d["x"].Of.FieldOrder[0] != "a" {
		t.Fatalf("nested of/fields: %+v %v", d["x"], err)
	}
}

func TestSchemaValidateShape(t *testing.T) {
	t.Parallel()
	s, err := ParseSchema(mustYAML(t, "author: {type: map, fields: {name: {type: string, required: true}, age: int}}\ntags: {type: list, of: string}"))
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(s.Validate(map[string]any{"author": map[string]any{"age": "x", "extra": 1}, "tags": []any{"a", true}}), "\n")
	for _, want := range []string{`author.age: "x" is not a int`, `author: the required field "name" is missing`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
	if strings.Contains(got, "extra") {
		t.Errorf("an extra key in collection data was reported:\n%s", got)
	}
}

func lineHas(all, at, msg string) bool {
	for _, l := range strings.Split(all, "\n") {
		if strings.Contains(l, at) && strings.Contains(l, msg) {
			return true
		}
	}
	return false
}

func itoa(n int) string { return strconv.Itoa(n) }

func mustYAML(t *testing.T, src string) any {
	t.Helper()
	var v any
	if err := yaml.Unmarshal([]byte(src), &v); err != nil {
		t.Fatal(err)
	}
	return v
}
