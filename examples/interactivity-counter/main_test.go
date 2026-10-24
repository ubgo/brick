package main

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/ubgo/brick/bricktest"
)

// handleRE reads the stateful root's handle and token, which the runtime
// sends back with every action.
var handleRE = regexp.MustCompile(`data-brick-state="([^"]+)" data-brick-token="([0-9a-f]+)"`)

// The real HTTP path: GET the page, then POST one action to /_brick/action
// exactly as the runtime does for a click on "+10".
func TestCounterOverHTTP(t *testing.T) {
	a, err := newApp("theme")
	if err != nil {
		t.Fatal(err)
	}
	h := a.handler()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	page := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(page, `<output id="count">5</output>`) {
		t.Fatalf("GET /: %d\n%s", rec.Code, page)
	}
	m := handleRE.FindStringSubmatch(page)
	if m == nil {
		t.Fatalf("no stateful root:\n%s", page)
	}
	body := `{"handle":"` + m[1] + `","token":"` + m[2] + `","action":"bump","args":[10]}`
	req := httptest.NewRequest(http.MethodPost, "/_brick/action", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Brick-Action", "1")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `<output id="count">15</output>`) {
		t.Fatalf("POST action: %d %s", rec.Code, rec.Body)
	}
}

// The same component driven the way the browser runtime drives it, with
// bricktest: clicks, a typed field, the built-ins and a renderless reply.
func TestCounterLive(t *testing.T) {
	a, err := newApp("theme")
	if err != nil {
		t.Fatal(err)
	}
	live := bricktest.Live(t, a.eng, page)
	c := live.Component("Counter")
	c.AssertSet(t, "count", 5) // init: seeded it from start="5"

	c.Click("#plus")
	c.Click("#plus10")
	c.AssertSet(t, "count", 16)

	c.Click("#details") // $toggle("open")
	c.AssertSet(t, "open", true)
	if !strings.Contains(c.HTML(), `<p id="panel" brick:show="open" >`) {
		t.Fatalf("panel still hidden:\n%s", c.HTML())
	}

	c.Set("label", "  Fruit  ")      // what brick:model sends as the user types
	c.AssertSet(t, "label", "Fruit") // updated: trimmed it

	res := c.Call("peek")
	bricktest.AssertRenderSkipped(t, res)
	if got, ok := res.Reply.(map[string]any); !ok || got["double"] != int64(32) {
		t.Fatalf("reply = %#v", res.Reply)
	}

	c.Click("#reset") // $reset("count") goes back to state:'s 0, not init's 5
	c.AssertSet(t, "count", 0)
}

// A Modelable child: the Stepper owns its buttons, the Cart owns qty.
// The parent's value is written into the child on every parent render; a
// child action changes the child, and the runtime then sends the parent a
// $set of qty (simulated here with Set, because that hop is the browser's).
func TestCartModelable(t *testing.T) {
	a, err := newApp("theme")
	if err != nil {
		t.Fatal(err)
	}
	live := bricktest.Live(t, a.eng, "content/cart.html")
	cart := live.Component("Cart")
	step := cart.Child("Stepper")
	step.AssertSet(t, "value", 2) // the parent's qty, not the stepper's own 1

	step.Click("#inc")
	step.AssertSet(t, "value", 3)
	cart.Set("qty", 3) // the runtime's $set to the parent
	if !strings.Contains(cart.HTML(), "Total: $36") {
		t.Fatalf("cart:\n%s", cart.HTML())
	}

	cart.Click("#one") // the parent writes qty; the child follows on this render
	if !strings.Contains(cart.HTML(), `<b id="value">1</b>`) || !strings.Contains(cart.HTML(), "Total: $12") {
		t.Fatalf("cart after reset:\n%s", cart.HTML())
	}
}

// {{ assets }} links the browser runtime on a page whose only interactive
// part is a stateful component, so its brick:click buttons work with the
// default Options (no navigation, no client code).
func TestCounterLinksRuntime(t *testing.T) {
	a, err := newApp("theme")
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	a.handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if !regexp.MustCompile(`<brick-assets><script type="module" src="/_brick/c/runtime\.[0-9a-f]+\.js"`).MatchString(rec.Body.String()) {
		t.Fatalf("runtime not linked:\n%s", rec.Body)
	}
}
