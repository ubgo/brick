// Command live-push-presence shows server push: an order list that
// refreshes on every open page when anyone ships an order (brick.publish
// from an action, Engine.Publish from Go code), a topic check that keeps
// signed-out viewers off the stream (PushOptions.AuthorizeTopic), and a
// lobby that lists who is on it (presence).
//
// A viewer is "signed in" by a user cookie, so two browsers (or a normal
// and a private window) are two people:
//
//	go run ./live-push-presence             serve on :8086
//	    open /login?as=Ada in one window and /login?as=Bob in another,
//	    then /orders/ and /room/ in both
//	go run ./live-push-presence -demo       print the same flow from Go
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"slices"
	"strconv"
	"sync"

	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"

	"github.com/ubgo/brick"
	assetshttp "github.com/ubgo/brick/contrib/assets-http"
	scriptstarlark "github.com/ubgo/brick/contrib/script-starlark"
)

const (
	// topicOrders is the topic Orders listens to and every change to an
	// order publishes. A topic is a name, never a secret: who may listen
	// is decided by the listen token and AuthorizeTopic.
	topicOrders = "orders"
	// userCookie names the viewer. A real app reads its own session here.
	userCookie = "user"
	// statusPending and statusShipped are an order's two states.
	statusPending = "pending"
	statusShipped = "shipped"
)

// order is one row of the shop's data; templates read ID and Status.
type order struct {
	ID     int
	Status string
}

// shop is the app's own data layer: brick never stores the orders, it
// reads them again through the Orders template on every refresh.
type shop struct {
	mu     sync.Mutex
	orders []order
}

// list returns a copy, so a render never sees a write half done.
func (s *shop) list() []order {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.orders)
}

// ship marks one order shipped; false when there is no such order.
func (s *shop) ship(id int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.orders {
		if s.orders[i].ID == id {
			s.orders[i].Status = statusShipped
			return true
		}
	}
	return false
}

// app is the engine, its push hub and the shop it shows.
type app struct {
	eng  *brick.Engine
	shop *shop
}

// viewer returns the signed-in user's name, or "" for a signed-out one.
func viewer(r *http.Request) string {
	c, err := r.Cookie(userCookie)
	if err != nil {
		return ""
	}
	return c.Value
}

// newApp wires push into the engine. The in-memory broadcaster serves one
// process; several servers need contrib/push-redis and a shared Sealer.
func newApp(dir string) (*app, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	s := &shop{orders: []order{{1, statusPending}, {2, statusPending}}}
	push, err := brick.NewPush(brick.PushOptions{
		// Only a signed-in viewer may follow a topic. It is asked when the
		// stream opens and again on every recheck, so a sign-out ends it.
		AuthorizeTopic: func(r *http.Request, topic string) bool { return viewer(r) != "" },
		Presence: brick.PresenceOptions{
			// Identify says how a viewer appears on a presence list. Without
			// it an entry is an anonymous id with no data.
			Identify: func(r *http.Request, topic string) (brick.PresenceEntry, bool) {
				name := viewer(r)
				if name == "" {
					return brick.PresenceEntry{}, false
				}
				return brick.PresenceEntry{ID: name, Data: map[string]string{"name": name}}, true
			},
		},
	})
	if err != nil {
		return nil, err
	}
	sc, err := scriptstarlark.New(scriptstarlark.Options{Globals: starlark.StringDict{"shop": shopModule(s)}})
	if err != nil {
		return nil, err
	}
	eng, err := brick.New(brick.Options{
		FS:        root.FS(),
		Scripting: sc,
		States:    brick.NewMemoryStates(0, 0),
		Push:      push,
		Funcs:     map[string]any{"orders": s.list},
	})
	if err != nil {
		return nil, err
	}
	return &app{eng: eng, shop: s}, nil
}

// shopModule is the one capability scripts get: shop.ship(id). Scripts
// reach nothing the host did not hand them.
// ds:def id=shopmodule-ctac59hf
func shopModule(s *shop) *starlarkstruct.Module {
	return &starlarkstruct.Module{Name: "shop", Members: starlark.StringDict{
		"ship": starlark.NewBuiltin("shop.ship", func(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
			var id int
			if err := starlark.UnpackArgs(b.Name(), args, kwargs, "id", &id); err != nil {
				return nil, err
			}
			if !s.ship(id) {
				return nil, fmt.Errorf("shop.ship: no order %d", id)
			}
			return starlark.None, nil
		}),
	}}
}

// handler routes the two pages, the engine's action, asset and events
// routes, a sign-in, and a webhook that changes an order from outside
// any action.
// ds:def id=app-handler-gqqj2b9h
func (a *app) handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle(brick.DefaultAssetBase, assetshttp.Handler(brick.DefaultAssetBase, func() assetshttp.Source { return a.eng }))
	mux.Handle("POST "+brick.DefaultAssetBase+brick.ActionEndpoint, a.eng.ActionHandler())
	mux.Handle("GET "+brick.DefaultAssetBase+brick.EventsEndpoint, a.eng.EventsHandler())
	mux.HandleFunc("GET /orders/{$}", a.page("content/orders/index.html"))
	mux.HandleFunc("GET /room/{$}", a.page("content/room/index.html"))
	mux.HandleFunc("GET /login", func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: userCookie, Value: r.URL.Query().Get("as"), Path: "/"})
		http.Redirect(w, r, "/orders/", http.StatusSeeOther)
	})
	mux.HandleFunc("POST /webhook/shipped/{id}", a.shipped)
	return mux
}

// page renders one page file.
func (a *app) page(path string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		html, err := a.eng.Render(r.Context(), path, nil)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(html)
	}
}

// shipped is a carrier's webhook: Go code changes the data and publishes
// the topic itself, and every open order list refreshes. A real webhook
// checks the sender's signature first; this one trusts its caller.
// ds:def id=app-shipped-r6s4gpsd
func (a *app) shipped(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil || !a.shop.ship(id) {
		http.Error(w, "no such order", http.StatusNotFound)
		return
	}
	// Engine.Publish refreshes every open listener of the topic. The
	// context is detached so a caller that hangs up cannot cancel the
	// publish after the data already changed.
	if err := a.eng.Publish(context.WithoutCancel(r.Context()), topicOrders); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// defaultAddr is this example's own port, so the examples of one guide can run
// side by side. Override it with -addr when the port is taken.
const defaultAddr = ":8086"

// main serves the same handler the tests drive, so the behaviour pinned in
// main_test.go is what a browser gets here. Flags only choose where, never what.
func main() {
	addr := flag.String("addr", defaultAddr, "listen address")
	dir := flag.String("theme", "live-push-presence/theme", "the project directory")
	demoRun := flag.Bool("demo", false, "print the push flow from Go, then exit")
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
