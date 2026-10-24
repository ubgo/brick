// Command live-effects-events is a small inbox whose actions ask the
// browser to do things besides re-rendering: show a flash, change the
// title, go to another page, download a file, change the address bar, and
// tell another component that something happened. The Badge component
// never names the Inbox; it listens to the "message-archived" event.
//
//	go run ./live-effects-events             serve on :8084, then open /
//	go run ./live-effects-events -demo       print what each action sends
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"slices"
	"time"

	"github.com/ubgo/brick"
	assetshttp "github.com/ubgo/brick/contrib/assets-http"
	scriptstarlark "github.com/ubgo/brick/contrib/script-starlark"
)

const (
	// inboxPage is the one page; it holds a Badge and an Inbox.
	inboxPage = "content/index.html"
	// eventArchived is the event Inbox.archive dispatches and Badge
	// listens to. The two files share only this name.
	eventArchived = "message-archived"
	// confettiKind is this app's own effect kind. Custom kinds start with
	// brick.CustomEffectPrefix ("x-") so no engine kind can collide.
	confettiKind = brick.CustomEffectPrefix + "confetti"
	// auditKind is the second custom kind, recorded from Go before the
	// action runs (see audit).
	auditKind = brick.CustomEffectPrefix + "audit"
	// payHost is the one outside host a redirect may name: the payment
	// provider. Every other absolute URL is refused (RedirectHosts).
	payHost = "pay.example"
	// maxEffectBytes is far below the 32 KiB default so the size limit is
	// easy to reach in a test; a real app keeps the default unless its
	// effects are legitimately larger.
	maxEffectBytes = 4 << 10
)

// runtimeConfig tunes the browser runtime through the brick-config meta:
// an action refused with 503 or 429 is sent again up to twice, waiting at
// most retryMaxWait for the answer's Retry-After.
// ds:def id=runtimeconfig-axe34jn4
var runtimeConfig = brick.RuntimeConfig{Retries: 2, RetryMaxWait: retryMaxWait}

// retryMaxWait is the longest Retry-After worth waiting for; past it the
// user sees the error instead of a frozen button.
// ds:def id=retrymaxwait-wcjb4a6k
const retryMaxWait = 3 * time.Second

// app is the engine; the inbox's data lives in the Inbox component's state.
type app struct {
	eng *brick.Engine
}

// newApp builds the engine over the project in dir. Actions need three
// things: a scripting language for the <brick:script> blocks, a state
// store for the components' state between requests, and (here) one
// middleware that adds a custom effect.
// ds:def id=newapp-hh3jnj8r
func newApp(dir string) (*app, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	sc, err := scriptstarlark.New(scriptstarlark.Options{})
	if err != nil {
		return nil, err
	}
	eng, err := brick.New(brick.Options{
		FS:               root.FS(),
		Scripting:        sc,
		States:           brick.NewMemoryStates(0, 0),
		ActionMiddleware: []brick.ActionMiddleware{audit, celebrate},
		RedirectHosts:    []string{payHost},
		MaxEffectBytes:   maxEffectBytes,
		Runtime:          runtimeConfig,
	})
	if err != nil {
		return nil, err
	}
	return &app{eng: eng}, nil
}

// audit records a custom effect from Go before the action runs, the way a
// script records brick.flash: EffectsFrom finds the action's collector in
// the context, and Add validates the effect when it is called, so a bad
// kind fails this action instead of producing an answer the browser has
// to guess about. Effects apply in the order they were recorded, so the
// audit effect arrives ahead of whatever the script adds. Only an export
// is audited here; a real app would choose by policy.
// ds:def id=audit-fv9qfz4v
func audit(next brick.ActionFunc) brick.ActionFunc {
	return func(ctx context.Context, call *brick.ActionCall) (*brick.ActionResult, error) {
		if call.Action == "export" {
			err := brick.EffectsFrom(ctx).Add(brick.Effect{Kind: auditKind, Data: map[string]any{"action": call.Action, "component": call.Component}})
			if err != nil {
				return nil, err
			}
		}
		return next(ctx, call)
	}
}

// celebrate adds a confetti effect to every action that archived a
// message. It runs after next, so it sees the effects the action recorded
// and adds its own only when the action succeeded: an action that fails
// sends no effects at all.
// ds:def id=celebrate-3xgk747s
func celebrate(next brick.ActionFunc) brick.ActionFunc {
	return func(ctx context.Context, call *brick.ActionCall) (*brick.ActionResult, error) {
		res, err := next(ctx, call)
		if err != nil {
			return nil, err
		}
		archived := slices.ContainsFunc(res.Effects, func(ef brick.Effect) bool {
			return ef.Kind == brick.EffectDispatch && ef.Name == eventArchived
		})
		if archived {
			res.Effects = append(res.Effects, brick.Effect{Kind: confettiKind, Data: map[string]any{"pieces": 40}})
		}
		return res, nil
	}
}

// handler is the host's router: the page, the engine's asset and action
// routes, and the pages and file the effects point at.
// ds:def id=app-handler-nr4g93km
func (a *app) handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle(brick.DefaultAssetBase, assetshttp.Handler(brick.DefaultAssetBase, func() assetshttp.Source { return a.eng }))
	mux.Handle("POST "+brick.DefaultAssetBase+brick.ActionEndpoint, a.eng.ActionHandler())
	mux.HandleFunc("GET /{$}", a.index)
	mux.HandleFunc("GET /messages/{id}", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "message %s", r.PathValue("id"))
	})
	mux.HandleFunc("GET /export.csv", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/csv")
		fmt.Fprint(w, "id,subject\n1,Welcome\n2,Invoice\n3,Lunch?\n")
	})
	mux.HandleFunc("GET /login", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "signed out")
	})
	return mux
}

// index renders the inbox page.
func (a *app) index(w http.ResponseWriter, r *http.Request) {
	html, err := a.eng.Render(r.Context(), inboxPage, nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(html)
}

// defaultAddr is this example's own port, so the examples of one guide can run
// side by side. Override it with -addr when the port is taken.
const defaultAddr = ":8084"

// main serves the same handler the tests drive, so the behaviour pinned in
// main_test.go is what a browser gets here. Flags only choose where, never what.
func main() {
	addr := flag.String("addr", defaultAddr, "listen address")
	dir := flag.String("theme", "live-effects-events/theme", "the project directory")
	demoRun := flag.Bool("demo", false, "print what each action sends, then exit")
	flag.Parse()
	a, err := newApp(*dir)
	if err != nil {
		log.Fatal(err)
	}
	if *demoRun {
		if err := demo(os.Stdout, a.handler()); err != nil {
			log.Fatal(err)
		}
		return
	}
	log.Printf("serving on %s", *addr)
	log.Fatal(http.ListenAndServe(*addr, a.handler()))
}
