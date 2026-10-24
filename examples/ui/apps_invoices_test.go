package main

import (
	"context"
	"database/sql"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// invURL is the Invoices demo app.
const invURL = "/apps/invoices/"

// invApp is one visitor's InvoicesApp instance on a fresh showcase, with
// the database behind it so a test can check what an action wrote.
type invApp struct {
	t             *testing.T
	h             http.Handler
	db            *sql.DB
	handle, token string
	page          string
}

// invOpen builds the showcase over a freshly seeded database and loads the
// Invoices page.
func invOpen(t *testing.T) *invApp {
	t.Helper()
	s, db, err := newSite(context.Background(), ".", false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	a := &invApp{t: t, h: s.Handler(), db: db}
	code, page := get(t, a.h, invURL)
	if code != http.StatusOK {
		t.Fatalf("%s: status %d", invURL, code)
	}
	m := settingsRootRe.FindStringSubmatch(page)
	if m == nil {
		t.Fatal("no stateful root on the Invoices page")
	}
	a.handle, a.token, a.page = m[1], m[2], page
	return a
}

// act posts one action and returns the re-rendered app; it fails on any
// status but 200.
func (a *invApp) act(action string, args ...any) string {
	a.t.Helper()
	code, out := postAction(a.t, a.h, a.handle, a.token, action, args...)
	if code != http.StatusOK {
		a.t.Fatalf("%s: status %d\n%s", action, code, out)
	}
	return out
}

// count runs a count(*) query.
func (a *invApp) count(q string, args ...any) int {
	a.t.Helper()
	var n int
	if err := a.db.QueryRow(q, args...).Scan(&n); err != nil {
		a.t.Fatal(err)
	}
	return n
}

var (
	invRowRe  = regexp.MustCompile(`class="inv-link" brick:click="open\((\d+)\)">(INV-\d+)<`)
	invEditRe = regexp.MustCompile(`brick:click="edit\((\d+)\)"`)
	invTabRe  = regexp.MustCompile(`href="#inv-[a-z]+"[^>]*>(All|Draft|Sent|Paid|Overdue)<span class="bk-tabs__badge">(\d+)<`)
)

// invTabs reads the status tabs' counts from the list.
func invTabs(t *testing.T, html string) map[string]int {
	t.Helper()
	out := map[string]int{}
	for _, m := range invTabRe.FindAllStringSubmatch(html, -1) {
		n, _ := strconv.Atoi(m[2])
		out[m[1]] = n
	}
	return out
}

// invAlerts are the inline error messages in a render.
func invAlerts(html string) []string {
	var out []string
	for _, m := range regexp.MustCompile(`(?s)role="alert">(.*?)</p>`).FindAllStringSubmatch(html, -1) {
		out = append(out, strings.TrimSpace(regexp.MustCompile(`(?s)<svg.*?</svg>`).ReplaceAllString(m[1], "")))
	}
	return out
}

// The list: stat cards, status tabs with the seeded counts, the first page
// of eight, newest first, and the pager.
func TestInvoicesListPage(t *testing.T) {
	a := invOpen(t)
	tabs := invTabs(t, a.page)
	want := map[string]int{"All": 24, "Draft": 3, "Sent": 3, "Paid": 12, "Overdue": 6}
	for k, v := range want {
		if tabs[k] != v {
			t.Errorf("tab %s: %d, want %d (all tabs %v)", k, tabs[k], v, tabs)
		}
	}
	rows := invRowRe.FindAllStringSubmatch(a.page, -1)
	if len(rows) != 8 || rows[0][2] != "INV-1024" || rows[7][2] != "INV-1017" {
		t.Errorf("first page: %d rows %v", len(rows), rows)
	}
	mustContain(t, invURL, a.page, "Outstanding", "Overdue", `aria-current="page"`, "<b>1</b>–<b>8</b> of <b>24</b>", `href="#inv-page-2"`, `class="bk-appshell`, `brick:click="newInvoice"`)
}

// Filtering, searching and paging: the tabs count the search, the list
// shows one status, and a page past the end is clamped.
func TestInvoicesFilterSearchPage(t *testing.T) {
	a := invOpen(t)
	out := a.act("filter", "overdue")
	rows := invRowRe.FindAllStringSubmatch(out, -1)
	if len(rows) != 6 {
		t.Fatalf("overdue: %d rows, want 6", len(rows))
	}
	for _, r := range rows {
		id, _ := strconv.Atoi(r[1])
		if a.count("select count(*) from invoices where id = ? and status = 'overdue'", id) != 1 {
			t.Errorf("%s is listed as overdue but is not", r[2])
		}
	}
	out = a.act("$set", "q", "sara")
	n := a.count("select count(*) from invoices where lower(customer) like '%sara%' or lower(email) like '%sara%'")
	if got := invTabs(t, out)["All"]; got != n || n == 0 {
		t.Errorf("search sara: All tab %d, want %d", got, n)
	}
	a.act("filter", "all")
	a.act("$set", "q", "")
	out = a.act("page", 3)
	if rows := invRowRe.FindAllStringSubmatch(out, -1); len(rows) != 8 || rows[7][2] != "INV-1001" {
		t.Errorf("page 3: %v", rows)
	}
	out = a.act("page", 99)
	if rows := invRowRe.FindAllStringSubmatch(out, -1); len(rows) != 8 {
		t.Errorf("page 99 is not clamped to the last page: %d rows", len(rows))
	}
	out = a.act("$set", "sort", "amount")
	rows = invRowRe.FindAllStringSubmatch(out, -1)
	if len(rows) == 0 {
		t.Fatal("no rows sorted by amount")
	}
}

// The editor's totals are computed on the server from the lines as typed:
// 3 × $12.50 + 1 × $99.99 = $137.49, 10% tax rounded half up = $13.75,
// total $151.24.
func TestInvoicesTotalsMath(t *testing.T) {
	a := invOpen(t)
	a.act("newInvoice")
	a.act("setQty", 0, "3")
	a.act("setPrice", 0, "12.50")
	a.act("addLine")
	out := a.act("setPrice", 1, "99.99")
	mustContain(t, "totals", out, "<dd>$137.49</dd>", "Tax (10%)", "<dd>$13.75</dd>", "<dd>$151.24</dd>", ">$37.50</span>", ">$99.99</span>")
	out = a.act("$set", "taxRate", "7.5")
	mustContain(t, "7.5% tax", out, "<dd>$10.31</dd>", "<dd>$147.80</dd>")
	out = a.act("removeLine", 0)
	mustContain(t, "after removing a line", out, "<dd>$99.99</dd>")
	if strings.Contains(out, `name="price_1"`) {
		t.Error("removeLine left two lines")
	}
}

// Save validates every field and writes nothing on an error.
func TestInvoicesValidation(t *testing.T) {
	a := invOpen(t)
	before := a.count("select count(*) from invoices")
	a.act("newInvoice")
	out := a.act("save", map[string]string{"customer": "Nobody Here", "issued": "2026-09-27", "due": "2026-09-01", "invStatus": "draft", "notes": "", "taxRate": "80", "desc_0": "", "qty_0": "0", "price_0": "12.345"})
	got := strings.Join(invAlerts(out), " | ")
	for _, w := range []string{"Choose a customer from your contacts.", "The due date cannot be before the issue date.", "Use a percent from 0 to 50, like 10 or 7.5.", "Describe the item."} {
		if !strings.Contains(got, w) {
			t.Errorf("missing error %q in %s", w, got)
		}
	}
	mustContain(t, "invalid lines", out, `aria-invalid="true" name="qty_0"`, `aria-invalid="true" name="price_0"`, `value="12.345"`)
	if a.count("select count(*) from invoices") != before {
		t.Error("an invalid invoice was written")
	}
	out = a.act("save", map[string]string{"customer": "", "issued": "not a date", "due": "", "invStatus": "void", "notes": "", "taxRate": "10"})
	got = strings.Join(invAlerts(out), " | ")
	for _, w := range []string{"Choose a customer.", "Enter the issue date.", "Enter the due date.", "Choose a status."} {
		if !strings.Contains(got, w) {
			t.Errorf("missing error %q in %s", w, got)
		}
	}
}

// Create, edit, mark paid, duplicate and delete, each checked in the
// database.
func TestInvoicesCRUD(t *testing.T) {
	a := invOpen(t)
	var customer, email string
	if err := a.db.QueryRow("select name, email from contacts order by name limit 1").Scan(&customer, &email); err != nil {
		t.Fatal(err)
	}
	a.act("newInvoice")
	a.act("addLine")
	out := a.act("save", map[string]string{"customer": customer, "issued": "2026-09-27", "due": "2026-10-27", "invStatus": "draft", "notes": "Net 30.", "taxRate": "10", "desc_0": "Photos", "qty_0": "3", "price_0": "12.50", "desc_1": "Audit", "qty_1": "1", "price_1": "99.99"})
	mustContain(t, "created", out, "Invoice created", "INV-1025", "<dd>$151.24</dd>", "Net 30.", email)
	m := invEditRe.FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("no edit action in the document view:\n%s", out)
	}
	id, _ := strconv.Atoi(m[1])
	if n := a.count("select count(*) from invoices where id = ? and number = 'INV-1025' and customer = ? and email = ? and tax_rate = 10", id, customer, email); n != 1 {
		t.Fatalf("the created invoice is not in the database")
	}
	if n := a.count("select count(*) from invoice_lines where invoice_id = ? and ((qty = 3 and unit_price = 1250) or (qty = 1 and unit_price = 9999))", id); n != 2 {
		t.Errorf("created lines: %d, want 2", n)
	}
	a.act("edit", id)
	out = a.act("save", map[string]string{"customer": customer, "issued": "2026-09-27", "due": "2026-10-27", "invStatus": "sent", "notes": "", "taxRate": "0", "desc_0": "Photos", "qty_0": "4", "price_0": "12.50", "desc_1": "Audit", "qty_1": "1", "price_1": "99.99"})
	mustContain(t, "edited", out, "Invoice saved", "<dd>$149.99</dd>")
	if a.count("select count(*) from invoice_lines where invoice_id = ?", id) != 2 || a.count("select count(*) from invoice_lines where invoice_id = ? and qty = 4", id) != 1 || a.count("select count(*) from invoices where id = ? and status = 'sent'", id) != 1 {
		t.Error("the edit did not replace the lines and the status")
	}
	a.act("markPaid", id)
	if a.count("select count(*) from invoices where id = ? and status = 'paid'", id) != 1 {
		t.Error("markPaid did not write")
	}
	before := a.count("select count(*) from invoices")
	out = a.act("duplicate", id)
	mustContain(t, "duplicate", out, "Duplicated as a draft", "INV-1026")
	if a.count("select count(*) from invoices") != before+1 || a.count("select count(*) from invoices where number = 'INV-1026' and status = 'draft' and issued = '2026-09-27'") != 1 {
		t.Error("duplicate did not write a draft copy")
	}
	out = a.act("remove", id)
	mustContain(t, "deleted", out, "Invoice deleted", "INV-1024")
	if a.count("select count(*) from invoices where id = ?", id) != 0 || a.count("select count(*) from invoice_lines where invoice_id = ?", id) != 0 {
		t.Error("remove left the invoice or its lines")
	}
}
