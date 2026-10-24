package main

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/ubgo/brick"
	"github.com/ubgo/brick/statetest"
)

// stateAttrs finds every stateful component's handle and token, in page
// order: Search, then Cart.
var stateAttrs = regexp.MustCompile(`data-brick-state="([^"]+)" data-brick-token="([0-9a-f]+)"`)

func request(method, url, body, user string) *http.Request {
	r := httptest.NewRequest(method, url, strings.NewReader(body))
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Brick-Action", "1")
	}
	if user != "" {
		r.AddCookie(&http.Cookie{Name: userCookie, Value: user})
	}
	return r
}

// Each component's handle names its store; the cart's bytes land in the
// host's BytesStore, and sign-out removes them.
func TestNamedStore(t *testing.T) {
	eng, carts, err := newApp("theme")
	if err != nil {
		t.Fatal(err)
	}
	h := handler(eng)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, request(http.MethodGet, "/", "", "ada"))
	m := stateAttrs.FindAllStringSubmatch(rec.Body.String(), -1)
	if len(m) != 2 {
		t.Fatalf("want two stateful components:\n%s", rec.Body)
	}
	search, cart := m[0], m[1]
	if !strings.HasPrefix(search[1], "default.") || !strings.HasPrefix(cart[1], "cart.") {
		t.Fatalf("handles: %s %s", search[1], cart[1])
	}
	if carts.Len() != 1 {
		t.Fatalf("cart rows: %d", carts.Len())
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, request(http.MethodPost, "/_brick/action", `{"handle":"`+cart[1]+`","token":"`+cart[2]+`","action":"put"}`, "ada"))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "1 items") {
		t.Fatalf("put: %d %s", rec.Code, rec.Body)
	}
	view, err := eng.Instance(t.Context(), cart[1], brick.WithSession("ada"))
	if err != nil {
		t.Fatal(err)
	}
	if n, _ := view.Get("items"); view.Store != "cart" || n != int64(1) {
		t.Fatalf("instance: %s %v", view.Store, view.State)
	}

	h.ServeHTTP(httptest.NewRecorder(), request(http.MethodPost, "/signout", "", "ada"))
	if carts.Len() != 0 {
		t.Fatalf("after sign-out, cart rows: %d", carts.Len())
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, request(http.MethodPost, "/_brick/action", `{"handle":"`+search[1]+`","token":"`+search[2]+`","action":"$set","args":["q","boots"]}`, "ada"))
	if rec.Code != http.StatusGone {
		t.Fatalf("memory store after sign-out: %d %s", rec.Code, rec.Body)
	}
}

// The host's store passes the same conformance suite as brick's own.
func TestMapStoreConforms(t *testing.T) {
	statetest.Store(t, func(*testing.T) brick.StateStore {
		return brick.Bytes(newMapStore(), brick.BytesOptions{TTL: cartTTL})
	}, statetest.StoreOptions{})
}

// A store name the engine does not have is a compile error naming the
// stores it does have, never a silent fallback to the default store.
func TestUnknownStore(t *testing.T) {
	eng, err := brick.New(brick.Options{
		FS: fstest.MapFS{
			"components/Wish.html": {Data: []byte("---\nstate:\n  n: 0\nbrick:\n  store: carts\n---\n<p>{{ .State.n }}</p>\n")},
			"content/index.html":   {Data: []byte("<Wish />\n")},
		},
		States:      brick.NewMemoryStates(0, 0),
		StateStores: map[string]brick.StateStore{"cart": brick.NewMemoryStates(0, 0)},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = eng.Render(t.Context(), "content/index.html", nil)
	if err == nil || !strings.Contains(err.Error(), `"carts" is not a state store of this engine (have: cart, default)`) {
		t.Fatalf("unknown store: %v", err)
	}
	t.Logf("unknown store: %v", err)
}
