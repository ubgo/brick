package main

import (
	"net/http"
	"regexp"
	"strings"
	"testing"
)

// storeAction posts one action to the stateful component on page, the way
// the browser runtime does, and returns the status and the re-rendered HTML.
func storeAction(t *testing.T, h http.Handler, page, action string, args ...any) (int, string) {
	t.Helper()
	handle := regexp.MustCompile(`data-brick-state="([^"]+)"`).FindStringSubmatch(page)
	token := regexp.MustCompile(`data-brick-token="([^"]+)"`).FindStringSubmatch(page)
	if handle == nil || token == nil {
		t.Fatalf("no state handle or token in the page")
	}
	code, out := postAction(t, h, handle[1], token[1], action, args...)
	return code, out
}

// storeCartCount reads the cart button's count from the Indicator's label.
func storeCartCount(html string) string {
	m := regexp.MustCompile(`<span class="bk-sr-only">(\d+) items in the cart</span>`).FindStringSubmatch(html)
	if m == nil {
		return "0"
	}
	return m[1]
}

// storeMust fails unless the action succeeded, and returns its HTML.
func storeMust(t *testing.T, h http.Handler, page, action string, args ...any) string {
	t.Helper()
	code, out := storeAction(t, h, page, action, args...)
	if code != http.StatusOK {
		t.Fatalf("%s: status %d\n%s", action, code, out)
	}
	return out
}

// The Store page renders the catalog from the products table, inside the
// shared app shell.
func TestStorePage(t *testing.T) {
	_, h := showcase(t)
	code, page := get(t, h, "/apps/store/")
	if code != http.StatusOK {
		t.Fatalf("status %d\n%s", code, page)
	}
	mustContain(t, "store", page,
		`class="bk-appshell"`, `href="/apps/store/" aria-current="page"`, // AppNav marks the app
		`<b>8</b> of 8 products`, `Linen shirt`, `Fountain pen`, `$59.00`,
		`brick:click="store.add(2)"`, `data-bk-open="store-cart"`, `id="store-cart"`,
		`brick:model="q"`, `brick:change="store.toggle_cat(0)"`, `brick:change="store.set_sort"`,
		`Sold out`, `Only 5 left`, `role="img" aria-label="4 out of 5"`,
	)
}

// Filters and sort are state, and the grid follows them.
func TestStoreFilters(t *testing.T) {
	_, h := showcase(t)
	_, page := get(t, h, "/apps/store/")
	out := storeMust(t, h, page, "store.toggle_cat", 1, true) // Clothing
	mustContain(t, "clothing", out, `<b>2</b> of 8 products`, `Linen shirt`, `Wool beanie`, `brick:click="store.toggle_cat(1)"`)
	if strings.Contains(out, "Desk lamp") {
		t.Error("the Clothing filter still shows the desk lamp")
	}
	out = storeMust(t, h, page, "store.set_sort", "price_desc")
	if a, b := strings.Index(out, "Linen shirt"), strings.Index(out, "Wool beanie"); a < 0 || b < 0 || a > b {
		t.Error("price high to low: the $59 shirt is not before the $29 beanie")
	}
	out = storeMust(t, h, page, "$set", "in_stock", true)
	mustContain(t, "in stock", out, `<b>1</b> of 8 products`, `Wool beanie`)
	out = storeMust(t, h, page, "$set", "q", "zzz")
	mustContain(t, "no match", out, `No products match`)
	out = storeMust(t, h, page, "store.clear_filters")
	mustContain(t, "cleared", out, `<b>8</b> of 8 products`)
}

// Add writes the cart table: the count and the drawer follow, quantities
// are capped by stock, the drawer's own actions keep it open, and checkout
// validates, writes orders and empties the cart.
func TestStoreCartAndCheckout(t *testing.T) {
	_, h := showcase(t)
	_, page := get(t, h, "/apps/store/")
	storeMust(t, h, page, "store.add", 2)
	out := storeMust(t, h, page, "store.add", 2)
	if got := storeCartCount(out); got != "2" {
		t.Fatalf("after adding the tote twice the cart holds %s", got)
	}
	mustContain(t, "cart", out, `Canvas tote`, `$48.00`, `brick:click="store.set_qty(2, 3)"`)
	if strings.Contains(out, `id="store-cart" open`) || strings.Contains(out, `open id="store-cart"`) {
		t.Error("adding from the grid opened the drawer")
	}
	out = storeMust(t, h, page, "store.set_qty", 2, 99)
	if got := storeCartCount(out); got != "7" {
		t.Errorf("qty 99 of a product with 7 in stock: cart holds %s", got)
	}
	if !regexp.MustCompile(`<dialog[^>]*id="store-cart"[^>]*\sopen`).MatchString(out) && !regexp.MustCompile(`<dialog[^>]*\sopen[^>]*id="store-cart"`).MatchString(out) {
		t.Error("a quantity change closed the drawer")
	}
	storeMust(t, h, page, "store.set_qty", 2, 1)
	storeMust(t, h, page, "store.add", 1) // sold out: ignored
	out = storeMust(t, h, page, "store.add", 7)
	if got := storeCartCount(out); got != "2" {
		t.Errorf("tote + notebook, sold-out shirt ignored: cart holds %s", got)
	}
	out = storeMust(t, h, page, "store.remove", 7)
	if got := storeCartCount(out); got != "1" {
		t.Errorf("after removing the notebook the cart holds %s", got)
	}

	out = storeMust(t, h, page, "store.checkout")
	mustContain(t, "checkout", out, `Shipping address`, `brick:submit="store.place_order"`, `Place order · $30.00`)
	out = storeMust(t, h, page, "store.place_order", map[string]string{"name": "A", "email": "nope"})
	mustContain(t, "invalid order", out, `Enter the name for the delivery.`, `Enter an e-mail address like ada@example.com.`, `Choose a country.`, `value="nope"`)
	out = storeMust(t, h, page, "store.place_order", map[string]string{"name": "Ada Lovelace", "email": "ada@example.com", "address": "12 Analytical Row", "city": "London", "zip": "EC1A 1BB", "country": "United Kingdom"})
	mustContain(t, "confirmed", out, `Order #1121 confirmed`, `1 item · $30.00`, `ada@example.com`)
	if got := storeCartCount(out); got != "0" {
		t.Errorf("after the order the cart holds %s", got)
	}
}
