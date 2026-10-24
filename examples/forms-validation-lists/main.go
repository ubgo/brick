// Command forms-validation-lists serves two forms whose checks are
// declared once under brick: {rules:}: a signup form (required, email,
// min, same, a custom message, brick.validate and brick.untouch) and a
// todo list (wildcard rules on list items, .Form objects, and a "slug"
// rule that the host adds with its own Validator).
//
//	go run ./forms-validation-lists -addr :8086      then open /
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"regexp"

	"github.com/ubgo/brick"
	assetshttp "github.com/ubgo/brick/contrib/assets-http"
	scriptstarlark "github.com/ubgo/brick/contrib/script-starlark"
	"github.com/ubgo/brick/validate"
)

// page is the one page this app serves.
const page = "content/index.html"

// ruleSlug is the rule name this host adds. Components write it as a bare
// name in rules: (`slug: [required, slug]`).
// ds:def id=ruleslug-g8ueakes
const ruleSlug = "slug"

// slugRE is what a list address may look like: lowercase words joined by
// single dashes, so it can sit in a URL unescaped.
// ds:def id=slugre-h5d9nfzc
var slugRE = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// slugValidator adds the slug rule and hands every other rule to the
// default, so required, min, same and the rest keep their messages. The
// engine calls Check once per declared rule when it compiles a file (an
// unknown rule is an error with the file and line) and Validate with one
// rule at a time at render.
// ds:def id=slugvalidator-vb8rkb35
type slugValidator struct{ base validate.Validator }

// Check accepts a bare slug and hands every other rule to the default,
// which reports unknown names and bad arguments.
// ds:def id=slugvalidator-check-tqesyum2
func (v slugValidator) Check(path string, r validate.Rule) error {
	if r.Name != ruleSlug {
		return v.base.Check(path, r)
	}
	if r.Arg != nil {
		return fmt.Errorf("%s takes no argument; write it as a bare name", ruleSlug)
	}
	return nil
}

// Validate returns the first failing rule's message, running slug here
// and every other rule through the default one at a time.
// ds:def id=slugvalidator-validate-z8agdfan
func (v slugValidator) Validate(ctx context.Context, path string, value any, rules []validate.Rule, state map[string]any, locale string) string {
	for _, r := range rules {
		if r.Name != ruleSlug {
			if msg := v.base.Validate(ctx, path, value, []validate.Rule{r}, state, locale); msg != "" {
				return msg
			}
			continue
		}
		// Like every default rule but required, an empty value passes.
		if s, _ := value.(string); s != "" && !slugRE.MatchString(s) {
			return "Use lowercase letters, digits and single dashes."
		}
	}
	return ""
}

// newApp builds an engine with in-process state and the slug-aware
// validator.
// ds:def id=newapp-zjqcqrqh
func newApp(dir string) (*brick.Engine, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	sc, err := scriptstarlark.New(scriptstarlark.Options{})
	if err != nil {
		return nil, err
	}
	return brick.New(brick.Options{
		FS:        root.FS(),
		Scripting: sc,
		States:    brick.NewMemoryStates(0, 0),
		// Options.Validator is the one place the rules' meaning lives; the
		// wrapper keeps the default's rules and adds slug.
		Validator: slugValidator{base: validate.Default()},
	})
}

// handler mounts the action endpoint, the runtime assets and the page.
func handler(eng *brick.Engine) http.Handler {
	mux := http.NewServeMux()
	mux.Handle(brick.DefaultAssetBase+brick.ActionEndpoint, eng.ActionHandler())
	mux.Handle(brick.DefaultAssetBase, assetshttp.Handler(brick.DefaultAssetBase, func() assetshttp.Source { return eng }))
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		html, err := eng.Render(r.Context(), page, nil)
		if err != nil {
			http.Error(w, "render failed", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(html)
	})
	return mux
}

// defaultAddr is this example's own port, so the examples of one guide can run
// side by side. Override it with -addr when the port is taken.
const defaultAddr = ":8086"

// main serves the same handler the tests drive, so the behaviour pinned in
// main_test.go is what a browser gets here. Flags only choose where, never what.
func main() {
	addr := flag.String("addr", defaultAddr, "listen address")
	dir := flag.String("theme", "forms-validation-lists/theme", "the project directory")
	flag.Parse()
	eng, err := newApp(*dir)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("serving on %s", *addr)
	log.Fatal(http.ListenAndServe(*addr, handler(eng)))
}
