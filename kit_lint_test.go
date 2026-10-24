package brick

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

// kitDir is the kit's component folder; kitIcons is its icon set.
const (
	kitDir   = "kit/components"
	kitIcons = "kit/data/icons.yaml"
)

// kitUseDirs are the only places a kit `use:` may point: ~/ inside a
// library is the library's root, so the kit's own components and data. A
// remote or project path would make the kit depend on something it does not
// ship.
var kitUseDirs = []string{"~/components/", "~/data/"}

// kitNoAttrs lists the components whose root takes no caller attributes,
// each for a stated reason. Every other component must place {{ attrs }}.
var kitNoAttrs = map[string]string{
	"Theme.html": "a stylesheet carrier: its root is an inert <template>",
	"Tab.html":   "a data child of <Tabs>: it renders no element of its own",
}

var (
	kitStyleRe   = regexp.MustCompile(`(?s)<brick:style([^>]*)>(.*?)</brick:style>`)
	kitColorRe   = regexp.MustCompile(`#[0-9a-fA-F]{3,8}\b|\brgba?\(|\bhsla?\(`)
	kitUseRe     = regexp.MustCompile(`(?m)^  \w+: (\S+)$`)
	kitIconRe    = regexp.MustCompile(`<Icon\s+name="([^"{]+)"|\s(?:icon|iconRight)="([^"{]+)"`)
	cssCommentRe = regexp.MustCompile(`(?s)/\*.*?\*/`)
	kitCommentR  = regexp.MustCompile(`(?s)\{\{/\*.*?\*/\}\}`)
	// kitSpreadRe finds a wrapper forwarding the caller's attributes to an
	// inner kit component, the other legitimate way to honour rule 4.
	kitSpreadRe = regexp.MustCompile(`brick:spread="[^"]*\.Attrs`)
)

// kitFiles reads every kit component.
func kitFiles(t *testing.T) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(kitDir)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join(kitDir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		out[e.Name()] = string(b)
	}
	return out
}

// selectors returns the selector of every rule in a stylesheet, skipping
// at-rule headers (@media, @keyframes …) and keyframe steps.
func selectors(css string) []string {
	css = cssCommentRe.ReplaceAllString(css, " ")
	var out []string
	depth, start := 0, 0
	inFrames := -1
	for i := 0; i < len(css); i++ {
		switch css[i] {
		case '{':
			sel := strings.TrimSpace(css[start:i])
			if strings.HasPrefix(sel, "@keyframes") {
				inFrames = depth
			} else if !strings.HasPrefix(sel, "@") && inFrames < 0 {
				out = append(out, sel)
			}
			depth++
			start = i + 1
		case '}':
			depth--
			if depth == inFrames {
				inFrames = -1
			}
			start = i + 1
		case ';':
			if depth == 0 {
				start = i + 1
			}
		}
	}
	return out
}

// The kit's authoring rules (kit/README.md → Authoring rules), enforced on
// the source so they hold for every component, not by review.
func TestKitAuthoringRules(t *testing.T) {
	t.Parallel()
	for name, src := range kitFiles(t) {
		t.Run(name, func(t *testing.T) {
			if name != "Tab.html" && !kitCommentR.MatchString(src) {
				t.Error("no {{/* … */}} doc comment saying what it is and how to call it")
			}
			for _, m := range kitUseRe.FindAllStringSubmatch(frontmatterOf(src), -1) {
				if !strings.Contains(m[1], "/") {
					continue // not a path (a prop default, a oneof list)
				}
				if !slices.ContainsFunc(kitUseDirs, func(d string) bool { return strings.HasPrefix(m[1], d) }) {
					t.Errorf("use: %s is not under %s; ~/ inside the kit is the library root (brick-library.yaml), so the kit stays self-contained wherever it is mounted", m[1], strings.Join(kitUseDirs, " or "))
				}
			}
			for _, block := range kitStyleRe.FindAllStringSubmatch(src, -1) {
				css := block[2]
				if strings.Contains(block[1], "scoped") {
					t.Error("<brick:style scoped>: kit CSS is global under .bk- classes, so a class works on any element and on browsers without @scope")
				}
				if name == "Theme.html" {
					continue
				}
				if c := kitColorRe.FindString(css); c != "" {
					t.Errorf("literal colour %q: use a --bk-* token so themes and dark mode apply", c)
				}
				for _, sel := range selectors(css) {
					for _, part := range topLevelCommas(sel) {
						if !strings.Contains(part, ".bk-") {
							t.Errorf("selector %q is not under a .bk- class: kit CSS is global and must not touch the host's markup", strings.TrimSpace(part))
						}
					}
				}
			}
			if _, ok := kitNoAttrs[name]; !ok && !strings.Contains(src, "{{ attrs") && !kitSpreadRe.MatchString(src) {
				t.Error("no {{ attrs }} and no brick:spread of .Attrs: the caller's id, class, aria-* and hx-* would vanish (or list it in kitNoAttrs with a reason)")
			}
			if strings.Contains(src, "{{ attrs }}") && strings.Contains(src, ".Attrs.class") {
				t.Error("merges .Attrs.class but writes {{ attrs }}: the root gets two class attributes; write {{ attrs `class` }}")
			}
		})
	}
}

// Every icon a kit file names exists in the icon set.
func TestKitIconsExist(t *testing.T) {
	t.Parallel()
	b, err := os.ReadFile(kitIcons)
	if err != nil {
		t.Fatal(err)
	}
	var icons map[string][]string
	if err := yaml.Unmarshal(b, &icons); err != nil {
		t.Fatal(err)
	}
	for name, src := range kitFiles(t) {
		for _, m := range kitIconRe.FindAllStringSubmatch(src, -1) {
			icon := m[1] + m[2]
			if _, ok := icons[icon]; !ok {
				t.Errorf("%s: unknown icon %q: add it to %s", name, icon, kitIcons)
			}
		}
	}
}

// The selector check itself: a rule outside .bk- is caught, keyframes and
// at-rules are not rules.
func TestKitSelectorParser(t *testing.T) {
	t.Parallel()
	got := selectors(`/* a, comment { } */ .bk-a { x: 1 } @media (max-width: 1px) { .bk-b, p { y: 2 } } @keyframes k { from { o: 0 } to { o: 1 } } .bk-c:hover{}`)
	want := []string{".bk-a", ".bk-b, p", ".bk-c:hover"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
	if parts := topLevelCommas(".bk-a :where(b, i), p"); !slices.Equal(parts, []string{".bk-a :where(b, i)", " p"}) {
		t.Fatalf("topLevelCommas: %q", parts)
	}
}

func frontmatterOf(src string) string {
	if !strings.HasPrefix(src, "---\n") {
		return ""
	}
	end := strings.Index(src[4:], "\n---")
	if end < 0 {
		return ""
	}
	return src[4 : 4+end]
}

// topLevelCommas splits a selector list on the commas outside parentheses,
// so :where(a, b) stays one selector.
func topLevelCommas(sel string) []string {
	var out []string
	depth, start := 0, 0
	for i, c := range sel {
		switch c {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, sel[start:i])
				start = i + 1
			}
		}
	}
	return append(out, sel[start:])
}

// kitSharedRoots are root classes more than one component may claim, each
// with the reason.
var kitSharedRoots = map[string]string{
	"bk-chart": "the charts' shared base (axes, tooltip, legend); each chart adds its own bk-chart--<kind> modifier",
}

var (
	kitBlockRe = regexp.MustCompile(`(?s)<brick:(style|client|script)>.*?</brick:(style|client|script)>`)
	kitRootRe  = regexp.MustCompile("cx\\s+[`\"](bk-[a-z0-9_-]+)")
)

// Two components must not claim the same root class: each ships global CSS
// for it, so one component's rules would restyle the other's elements —
// how CodeBlock once inherited Code's inline-code look (rule 1).
func TestKitRootClassesUnique(t *testing.T) {
	t.Parallel()
	claims := map[string][]string{}
	for name, src := range kitFiles(t) {
		if m := kitRootRe.FindStringSubmatch(kitBlockRe.ReplaceAllString(src, "")); m != nil {
			claims[m[1]] = append(claims[m[1]], name)
		}
	}
	for class, files := range claims {
		if _, ok := kitSharedRoots[class]; len(files) > 1 && !ok {
			slices.Sort(files)
			t.Errorf("root class %s is claimed by %s: give each its own bk-<kebab-name> root", class, strings.Join(files, ", "))
		}
	}
}
