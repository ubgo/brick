package brick

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html"
	"mime"
	"net/http"
	"regexp"
	"slices"
	"strings"

	"github.com/ubgo/brick/internal/lex"
)

// Method links <a href="/posts/7" brick:method="delete"> sends
// a DELETE instead of a GET. The
// runtime turns the click into a hidden form that posts to the href,
// because a link can only GET and an HTML form can only GET or POST; a
// verb other than POST rides in the MethodField field, and MethodOverride
// turns such a post back into that verb for the host's router.
//
// Invariants:
//   - Without JS the link is an ordinary GET to its href so
//     a GET route must never change anything; the method link only adds a
//     way to reach the other verbs.
//   - MethodOverride rewrites only a same-origin POST, by the same check a
//     no-JS brick form post passes (formOriginOK). A host that relied on
//     "a cross-site form cannot send DELETE" keeps that guarantee: a
//     cross-site POST carrying _method=delete stays a POST.
//   - brick has no CSRF token for plain forms (the instance token belongs
//     to one component), so the hidden form carries none; a host that has
//     one adds it with the runtime's brick.methodFormWith seam.

// MethodField is the form field a method link's hidden form carries its
// verb in when the verb is not POST. Other server frameworks use the same
// name, so a host that already has an override middleware keeps working; the
// runtime carries the same constant (TestMethodLinkMatchesRuntime).
const MethodField = "_method"

// methodVerbs are the verbs brick:method accepts, lower-case as written.
// GET is missing on purpose: an ordinary link already sends it.
var methodVerbs = []string{"post", "put", "patch", "delete"}

// overrideVerbs are the verbs MethodOverride turns a POST into: every
// methodVerbs entry but POST itself.
var overrideVerbs = []string{http.MethodPut, http.MethodPatch, http.MethodDelete}

// MethodJS is the line runtime.js carries so the browser accepts the same
// verbs and field name the compiler and MethodOverride use.
func MethodJS() string {
	verbs, _ := json.Marshal(methodVerbs)
	return fmt.Sprintf("const METHOD_FIELD = %q; const METHOD_VERBS = %s;", MethodField, verbs)
}

// MethodOverride wraps h so a same-origin, form-encoded POST whose
// MethodField names PUT, PATCH or DELETE (any case) reaches h with that
// method, which is how a brick:method link's verb arrives at the host's
// router. Everything else passes through untouched: another method, a
// POST without the field or with any other verb, a multipart or JSON
// body (never parsed here, so an upload is not read twice), and a
// cross-site post. The field stays in r.PostForm.
func MethodOverride(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if verb, ok := overrideVerb(r); ok {
			r.Method = verb
		}
		h.ServeHTTP(w, r)
	})
}

// overrideVerb reports the verb a POST asks for in MethodField, when
// MethodOverride may apply it.
func overrideVerb(r *http.Request) (string, bool) {
	if r.Method != http.MethodPost {
		return "", false
	}
	ct, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if ct != "application/x-www-form-urlencoded" || !formOriginOK(r) {
		return "", false
	}
	if err := r.ParseForm(); err != nil {
		return "", false
	}
	verb := strings.ToUpper(strings.TrimSpace(r.PostForm.Get(MethodField)))
	return verb, slices.Contains(overrideVerbs, verb)
}

// methodTagNameRE is the element name at the start of a plain start tag.
var methodTagNameRE = regexp.MustCompile(`^<([a-zA-Z][\w-]*)`)

// hrefAttrRE finds an href attribute in a start tag.
var hrefAttrRE = regexp.MustCompile(`(?i)\shref\s*=`)

// dynamicMark stands in for the bytes of a {{ }} action when checkMethodLinks
// joins a start tag split by actions, so a value that holds one is known to
// be filled at render time.
const dynamicMark = '\x00'

// methodCompanions are the brick: attributes a method link may carry in a
// file without state: they configure the link's own submit, not an action.
var methodCompanions = []string{"confirm", "disable-with"}

// checkMethodLinks checks every brick:method in a file's markup: it must
// sit on an <a> with an href and name a verb from methodVerbs, and a
// brick:confirm or brick:disable-with beside it takes no modifier
// (.prompt's answer has no call to go to). Start tags split by {{ }} actions are joined first, so
// <a href="/p/{{ .ID }}" brick:method="delete"> is checked too. It reports
// whether the file has any method link, so its pages load the runtime.
func checkMethodLinks(toks []lex.Token) (bool, lex.Pos, error) {
	found := false
	var b []byte
	start := lex.Pos{}
	flush := func() (lex.Pos, error) {
		defer func() { b = b[:0] }()
		for _, tag := range plainTagRE.FindAllIndex(b, -1) {
			raw := b[tag[0]:tag[1]]
			if !bytes.Contains(raw, []byte("brick:method")) {
				continue
			}
			var method *brickAttr
			var methodAt int
			var companion *brickAttr
			for _, mm := range brickAttrRE.FindAllSubmatchIndex(raw, -1) {
				a := parseBrickAttrName(string(raw[mm[2]:mm[3]]))
				for g := 4; g <= 8; g += 2 {
					if mm[g] >= 0 {
						a.value, a.hasValue = string(raw[mm[g]:mm[g+1]]), true
					}
				}
				switch a.name {
				case attrMethod:
					method, methodAt = &a, mm[2]
				case "confirm", "disable-with":
					if len(a.mods) > 0 {
						companion = &a
					}
				}
			}
			if method == nil {
				continue
			}
			found = true
			pos := advancePos(start, b[:tag[0]+methodAt])
			if err := checkMethodAttr(*method); err != nil {
				return pos, err
			}
			name := methodTagNameRE.FindSubmatch(raw)
			if name == nil || !strings.EqualFold(string(name[1]), "a") {
				return pos, fmt.Errorf("brick:method goes on an <a href> link, not <%s>: a form already has method=, and a button can sit in a form with formmethod=", name[1])
			}
			if !hrefAttrRE.Match(raw) {
				return pos, fmt.Errorf("brick:method needs an href on its link: the href is where the request goes, and what a visitor without JS opens with a GET")
			}
			if companion != nil {
				return pos, fmt.Errorf("%s: on a method link brick:%s takes no modifier (.prompt's answer has no call to go to)", companion.fullName(), companion.name)
			}
		}
		return lex.Pos{}, nil
	}
	for _, t := range toks {
		switch {
		case t.Kind == lex.Text && !t.RawBody:
			if len(b) == 0 {
				start = t.Pos
			}
			b = append(b, t.Raw...)
			continue
		case t.Kind == lex.Action && len(b) > 0:
			// Blank the action but keep its newlines, so positions after
			// it still count lines right.
			for _, c := range t.Raw {
				if c == '\n' {
					b = append(b, c)
				} else {
					b = append(b, dynamicMark)
				}
			}
			continue
		}
		if p, err := flush(); err != nil {
			return found, p, err
		}
	}
	p, err := flush()
	return found, p, err
}

// checkMethodAttr checks brick:method's modifiers and verb. A value filled
// at render time is checked by the runtime instead, which refuses an
// unknown verb and treats an empty one as an ordinary link.
func checkMethodAttr(a brickAttr) error {
	if len(a.mods) > 0 {
		return fmt.Errorf("%s: brick:method takes no modifiers", a.fullName())
	}
	v := strings.TrimSpace(html.UnescapeString(a.value))
	if strings.ContainsRune(v, dynamicMark) || strings.Contains(v, "{{") {
		return nil
	}
	if !slices.Contains(methodVerbs, strings.ToLower(v)) {
		return fmt.Errorf("brick:method=%q: the verb is one of %s (an ordinary link already sends GET)", v, strings.Join(methodVerbs, ", "))
	}
	return nil
}

// hasMethodAttr reports whether a start tag carries brick:method, which
// lets brick:confirm and brick:disable-with stand on it in a file without
// state.
func hasMethodAttr(tag []byte) bool {
	for _, mm := range brickAttrRE.FindAllSubmatch(tag, -1) {
		if parseBrickAttrName(string(mm[1])).name == attrMethod {
			return true
		}
	}
	return false
}
