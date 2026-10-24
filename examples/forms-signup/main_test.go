package main

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

// instanceRE reads the handle and token a stateful root carries, as the
// runtime does before it sends an action.
var instanceRE = regexp.MustCompile(`data-brick-state="([^"]+)" data-brick-token="([0-9a-f]+)"`)

// post sends one JSON action the way runtime.js does and returns the
// status and the body.
func post(t *testing.T, h http.Handler, body string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/_brick/action", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Brick-Action", "1")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

// Deferred fields send nothing on their own: one Save carries every held
// value as sets, the server applies them before the action, and a refused
// set fails the whole request without touching state.
func TestSignupDeferredSets(t *testing.T) {
	eng, err := newApp("theme")
	if err != nil {
		t.Fatal(err)
	}
	h := handler(eng)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	page := rec.Body.String()
	for _, want := range []string{
		`data-brick-name="Signup" data-brick-arrays="tags" brick:submit="save" method="post">`,
		`<input brick:model.defer="address.city" value="" name="brick:model:address.city">`,
		`<p id="saved">Saves: 0, topics: [], city: </p>`,
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("page lacks %s:\n%s", want, page)
		}
	}
	m := instanceRE.FindStringSubmatch(page)
	if m == nil {
		t.Fatal("no stateful root")
	}
	inst := `{"handle":"` + m[1] + `","token":"` + m[2] + `",`

	code, body := post(t, h, inst+`"action":"save","sets":[["name","Ada"],["email","ada@x.io"],["tags",["go"]],["address.city","Oslo"]]}`)
	if code != http.StatusOK || !strings.Contains(body, `<p>Hello, <b brick:text="name">Ada</b></p>`) || !strings.Contains(body, `<p id="saved">Saves: 1, topics: [go], city: Oslo</p>`) {
		t.Fatalf("save with sets: %d\n%s", code, body)
	}

	for _, c := range []struct{ sets, want string }{
		{`[["name","Bo"],["address.zip","0150"]]`, `brick: field value refused: address.zip: no key "zip" (a client cannot add one)`},
		{`[["nmae","Bo"]]`, `brick: field value refused: "nmae" is not a field of this component's state`},
	} {
		code, body = post(t, h, inst+`"action":"save","sets":`+c.sets+`}`)
		if code != http.StatusUnprocessableEntity || strings.TrimSpace(body) != c.want {
			t.Fatalf("refused set %s: %d %q", c.sets, code, body)
		}
	}

	// The refused request left state alone: name is still Ada, not Bo.
	code, body = post(t, h, inst+`"action":"save"}`)
	if code != http.StatusOK || !strings.Contains(body, `<b brick:text="name">Ada</b>`) || !strings.Contains(body, `Saves: 2, topics: [go]`) {
		t.Fatalf("save after refusal: %d\n%s", code, body)
	}
	code, body = post(t, h, inst+`"action":"clear"}`)
	if code != http.StatusOK || !strings.Contains(body, `<p id="saved">Saves: 0, topics: [], city: </p>`) {
		t.Fatalf("clear: %d\n%s", code, body)
	}
}

// A list index is a path like any other: items.0.qty names the first
// row's qty, the held value arrives as the string an input holds, and the
// server converts it to the type there now (an int). A path past the end
// of the list is refused like any key the client cannot add.
func TestCartNestedPaths(t *testing.T) {
	eng, err := newApp("theme")
	if err != nil {
		t.Fatal(err)
	}
	h := handler(eng)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	page := rec.Body.String()
	ms := instanceRE.FindAllStringSubmatch(page, -1)
	if len(ms) != 2 {
		t.Fatalf("want Signup and Cart roots, got %d:\n%s", len(ms), page)
	}
	inst := `{"handle":"` + ms[1][1] + `","token":"` + ms[1][2] + `",`
	if !strings.Contains(page, `<input brick:model.defer="items.1.qty" value="2" name="brick:model:items.1.qty">`) {
		t.Fatalf("the list rows are not bound by index:\n%s", page)
	}

	code, body := post(t, h, inst+`"action":"checkout","sets":[["items.0.qty","3"],["items.1.qty","4"]]}`)
	if code != http.StatusOK || !strings.Contains(body, `<p id="total">Total: 7</p>`) {
		t.Fatalf("checkout with sets: %d\n%s", code, body)
	}
	code, body = post(t, h, inst+`"action":"checkout","sets":[["items.2.qty","1"]]}`)
	if code != http.StatusUnprocessableEntity || !strings.HasPrefix(strings.TrimSpace(body), "brick: field value refused: items.2.qty") {
		t.Fatalf("a row past the end: %d %q", code, body)
	}
}

// The Cart's confirmer and prompter are browser code: Go can only check
// that the module reaches the page and holds the calls. What the dialogs
// look like, and that a click waits for the answer, only a browser shows.
func TestCartAsksServed(t *testing.T) {
	eng, err := newApp("theme")
	if err != nil {
		t.Fatal(err)
	}
	h := handler(eng)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	page := rec.Body.String()
	for _, want := range []string{
		`brick:click="rename" brick:confirm.prompt="Name this cart?"`,
		`brick:click="empty_cart" brick:confirm="Empty the cart?"`,
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("page lacks %s:\n%s", want, page)
		}
	}
	m := regexp.MustCompile(`src="(/_brick/c/Cart\.[0-9a-f]+\.js)"`).FindStringSubmatch(page)
	if m == nil {
		t.Fatalf("no Cart module on the page:\n%s", page)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, m[1], nil))
	for _, want := range []string{`brick.confirmWith(`, `brick.promptWith(`} {
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), want) {
			t.Fatalf("module lacks %s (%d):\n%s", want, rec.Code, rec.Body)
		}
	}

	// The prompt's answer is the action's last argument.
	m2 := instanceRE.FindAllStringSubmatch(page, -1)
	inst := `{"handle":"` + m2[1][1] + `","token":"` + m2[1][2] + `",`
	code, body := post(t, h, inst+`"action":"rename","args":["Gifts"]}`)
	if code != http.StatusOK || !strings.Contains(body, `<h2 id="label">Gifts</h2>`) {
		t.Fatalf("rename: %d\n%s", code, body)
	}
}
