package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// instanceRE reads the hidden instance fields the rewrite adds before
// </form>, which a browser without scripts posts back.
var instanceRE = regexp.MustCompile(`name="brick:state" value="([^"]+)"><input type="hidden" name="brick:token" value="([0-9a-f]+)">`)

// A brick:submit form posted with scripts off: an empty name answers 422
// with the error on the page, a cross-site post is refused before the
// action, and a good one answers 303 with the new state in a one-minute
// cookie that the next GET consumes.
func TestSignupWithoutJavaScript(t *testing.T) {
	eng, err := newApp("theme")
	if err != nil {
		t.Fatal(err)
	}
	h := handler(eng)
	get := func(cookies []*http.Cookie) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, "/signup", nil)
		for _, c := range cookies {
			r.AddCookie(c)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		return rec
	}
	page := get(nil).Body.String()
	if !strings.Contains(page, `brick:submit="save" method="post">`) || !strings.Contains(page, `<input type="checkbox" brick:model="agree" name="brick:model:agree"> I agree</label>`) {
		t.Fatalf("form not rewritten:\n%s", page)
	}
	m := instanceRE.FindStringSubmatch(page)
	if m == nil {
		t.Fatalf("no instance fields:\n%s", page)
	}
	post := func(name, agree, site string) *httptest.ResponseRecorder {
		v := url.Values{"name": {name}, "brick:state": {m[1]}, "brick:token": {m[2]}, "brick:action": {"save"}, "brick:models": {`[["c","agree",""]]`}}
		if agree != "" {
			v.Set("brick:model:agree", agree)
		}
		r := httptest.NewRequest(http.MethodPost, "/signup", strings.NewReader(v.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("Sec-Fetch-Site", site)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		return rec
	}

	rec := post("", "", "same-origin")
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), `<p class="error">Enter a name.</p>`) {
		t.Fatalf("empty name: %d\n%s", rec.Code, rec.Body)
	}
	rec = post("Ada", "on", "cross-site")
	if rec.Code != http.StatusForbidden || strings.TrimSpace(rec.Body.String()) != "brick: action refused" {
		t.Fatalf("cross-site: %d %q", rec.Code, rec.Body)
	}

	rec = post("Ada", "on", "same-origin")
	cookie := rec.Header().Get("Set-Cookie")
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/signup" || !strings.HasPrefix(cookie, "brick-form=") || !strings.HasSuffix(cookie, "; Path=/; Max-Age=60; HttpOnly; SameSite=Lax") {
		t.Fatalf("good post: %d %q %q", rec.Code, rec.Header().Get("Location"), cookie)
	}
	after := get(rec.Result().Cookies())
	body := after.Body.String()
	if !strings.Contains(body, "<p>Welcome, Ada.</p>") || !strings.Contains(body, `brick:model="agree" checked name="brick:model:agree"`) || after.Header().Get("Set-Cookie") != "brick-form=; Path=/; Max-Age=0; HttpOnly; SameSite=Lax" {
		t.Fatalf("resume: %q\n%s", after.Header().Get("Set-Cookie"), body)
	}
	if strings.Contains(get(nil).Body.String(), "Welcome") {
		t.Fatal("a page without the cookie must render fresh")
	}
}

// A call only the browser can make stays inert without scripts: the Cancel
// button's $dispatch is left in the markup as written, is not turned into
// a post, and is not among the fields the form posts back.
func TestDispatchStaysInert(t *testing.T) {
	eng, err := newApp("theme")
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	handler(eng).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/signup", nil))
	page := rec.Body.String()
	if want := `<button type="button" brick:click='$dispatch("signup-cancelled")'>Cancel</button>`; !strings.Contains(page, want) {
		t.Fatalf("Cancel button missing or rewritten, want %s:\n%s", want, page)
	}
	if strings.Contains(page, `name="brick:action" value="signup-cancelled"`) {
		t.Fatalf("a $dispatch became a form field:\n%s", page)
	}
}
