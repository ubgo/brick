// Command production-middleware wraps every action in a chain of
// middleware: panic recovery, a log line per action, a signed-in check,
// a per-session rate limit and an app rule that only admins may wipe the
// count. Each action also sees the user signed in now (ActionRequest),
// and errors answer in the app's own JSON envelope (ActionErrors).
//
// The "auth" here is two request headers so the example stays small: a
// real app reads its session cookie or token.
//
//	go run ./production-middleware      then open / with X-User / X-Role headers
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"log"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/ubgo/brick"
	"github.com/ubgo/brick/actionmw"
	assetshttp "github.com/ubgo/brick/contrib/assets-http"
	scriptstarlark "github.com/ubgo/brick/contrib/script-starlark"
)

// Stand-in auth: the user and role come from headers. A real app derives
// both from its session; only these two lines would change.
const (
	userHeader = "X-User"
	roleHeader = "X-Role"
	adminRole  = "admin"
)

// actionsPerMinute is each session's budget. Low on purpose, so the test
// reaches the limit in a few calls.
const actionsPerMinute = 3

// actionTimeout bounds one action, scripts and loaders included.
const actionTimeout = 2 * time.Second

// page is the one page this app serves.
const page = "content/index.html"

// errAdminOnly is the app's refusal; wrapping ErrForbidden makes it a 403.
var errAdminOnly = &brick.ActionError{Status: http.StatusForbidden, Message: "only admins may wipe", Err: brick.ErrForbidden}

// newApp builds the engine. logOut receives one log line per action.
func newApp(dir string, logOut io.Writer) (*brick.Engine, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	sc, err := scriptstarlark.New(scriptstarlark.Options{})
	if err != nil {
		return nil, err
	}
	logger := slog.New(slog.NewTextHandler(logOut, &slog.HandlerOptions{ReplaceAttr: dropTime}))
	return brick.New(brick.Options{
		FS:        root.FS(),
		Scripting: sc,
		States:    brick.NewMemoryStates(0, 0),
		Sessions:  brick.SessionFunc(func(r *http.Request) (string, error) { return r.Header.Get(userHeader), nil }),
		// Outermost first: Recover sees every panic, Slog logs every
		// outcome (refusals included), RequireSession turns guests away
		// before they get a rate-limit bucket, and the app's own rule runs
		// last, so a refused wipe still spends a token (no free probing).
		ActionMiddleware: []brick.ActionMiddleware{
			actionmw.Recover(),
			actionmw.Slog(logger),
			actionmw.RequireSession(),
			actionmw.RateLimit(actionmw.MemoryLimiter(actionsPerMinute, time.Minute), actionmw.BySession),
			actionmw.Timeout(actionTimeout),
			adminOnly("wipe"),
		},
		// Request values reach the re-render; they are never stored with
		// the instance, so a role change shows on the next click.
		ActionRequest: func(r *http.Request) []brick.RenderOption {
			if u := r.Header.Get(userHeader); u != "" {
				return []brick.RenderOption{brick.Provide("user", u)}
			}
			return nil
		},
		ActionErrors: writeError,
	})
}

// adminOnly refuses the named action unless the caller is an admin. It
// runs after the engine has loaded and checked the instance, so it only
// ever sees real components.
func adminOnly(action string) brick.ActionMiddleware {
	return func(next brick.ActionFunc) brick.ActionFunc {
		return func(ctx context.Context, a *brick.ActionCall) (*brick.ActionResult, error) {
			if a.Action == action && (a.Request == nil || a.Request.Header.Get(roleHeader) != adminRole) {
				return nil, errAdminOnly
			}
			return next(ctx, a)
		}
	}
}

// pageOptions binds the page's state to the user's session and provides
// the user, only when there is one, so the component's inject: default
// shows for a guest. Actions get the session from Options.Sessions.
func pageOptions(r *http.Request) []brick.RenderOption {
	u := r.Header.Get(userHeader)
	if u == "" {
		return nil
	}
	return []brick.RenderOption{brick.WithSession(u), brick.Provide("user", u)}
}

// errorBody is the app's error envelope, the shape its other APIs use.
type errorBody struct {
	Error struct {
		Status  int    `json:"status"`
		Message string `json:"message"`
	} `json:"error"`
}

// writeError is Options.ActionErrors. It keeps brick's status mapping
// (ActionStatus) and the Retry-After of a rate limit, and changes only
// the body. Only an ActionError's Message reaches the client: any other
// error may carry a file path or a store's detail, so it answers with the
// status text.
func writeError(w http.ResponseWriter, _ *http.Request, err error) {
	var body errorBody
	body.Error.Status = brick.ActionStatus(err)
	body.Error.Message = http.StatusText(body.Error.Status)
	var ae *brick.ActionError
	if errors.As(err, &ae) {
		body.Error.Message = ae.Message
		if ae.RetryAfter > 0 {
			// Rounded up, as brick's own envelope does: never invite a retry
			// before the limiter allows it.
			w.Header().Set("Retry-After", strconv.Itoa(int((ae.RetryAfter+time.Second-1)/time.Second)))
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(body.Error.Status)
	_ = json.NewEncoder(w).Encode(body)
}

// dropTime removes the timestamp so log lines are the same on every run.
func dropTime(_ []string, a slog.Attr) slog.Attr {
	if a.Key == slog.TimeKey {
		return slog.Attr{}
	}
	return a
}

// handler serves the page and the action endpoint.
func handler(eng *brick.Engine) http.Handler {
	mux := http.NewServeMux()
	mux.Handle(brick.DefaultAssetBase+brick.ActionEndpoint, eng.ActionHandler())
	mux.Handle(brick.DefaultAssetBase, assetshttp.Handler(brick.DefaultAssetBase, func() assetshttp.Source { return eng }))
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		html, err := eng.Render(r.Context(), page, nil, pageOptions(r)...)
		if err != nil {
			log.Printf("render %s: %v", page, err)
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
const defaultAddr = ":8085"

// main serves the same handler the tests drive, so the behaviour pinned in
// main_test.go is what a browser gets here. Flags only choose where, never what.
func main() {
	addr := flag.String("addr", defaultAddr, "listen address")
	dir := flag.String("theme", "production-middleware/theme", "the project directory")
	flag.Parse()
	eng, err := newApp(*dir, os.Stderr)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("serving on %s", *addr)
	log.Fatal(http.ListenAndServe(*addr, handler(eng)))
}
