package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/ubgo/brick"
	"github.com/ubgo/brick/bricktest"
)

// contactsURL is the Contacts app's page.
const contactsURL = "/apps/contacts/"

// contactsSession drives the Contacts app as the runtime does: one page
// load (one state handle), then actions posted to /_brick/action asking
// for the effects envelope, each returning the re-rendered ContactsApp and
// keeping the action's effects (flashes, dispatched events) in fx.
type contactsSession struct {
	t             *testing.T
	h             http.Handler
	handle, token string
	// id is the ContactsApp instance's data-brick id: every element id in
	// the app is built from it ($.ID), so tests look ids up through it.
	id string
	fx []brick.Effect
}

// contactsRootRe finds a stateful root's opening tag by component name.
func contactsRootRe(name string) *regexp.Regexp {
	return regexp.MustCompile(`<[^>]*\bdata-brick-name="` + name + `"[^>]*>`)
}

// contactsAttr reads one attribute from an opening tag.
func contactsAttr(tag, name string) string {
	m := regexp.MustCompile(`\s` + name + `="([^"]*)"`).FindStringSubmatch(tag)
	if m == nil {
		return ""
	}
	return m[1]
}

// contactsStart builds the showcase over a fresh database and loads the
// app, returning the session, the first render and the database, so a
// test can check what an action really wrote.
func contactsStart(t *testing.T) (*contactsSession, string, *sql.DB) {
	t.Helper()
	s, db, err := newSite(context.Background(), ".", false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	h := s.Handler()
	code, page := get(t, h, contactsURL)
	if code != http.StatusOK {
		t.Fatalf("%s: status %d\n%s", contactsURL, code, page)
	}
	tags := contactsRootRe("ContactsApp").FindAllString(page, -1)
	if len(tags) != 1 {
		t.Fatalf("want one stateful ContactsApp, got %d", len(tags))
	}
	c := &contactsSession{t: t, h: h, handle: contactsAttr(tags[0], "data-brick-state"), token: contactsAttr(tags[0], "data-brick-token"), id: contactsAttr(tags[0], "data-brick")}
	if c.handle == "" || c.token == "" || c.id == "" {
		t.Fatalf("ContactsApp root without handle, token or id: %s", tags[0])
	}
	return c, page, db
}

// act posts one action as the runtime does (with the effects Accept) and
// returns the re-rendered component; the action's effects land in c.fx.
func (c *contactsSession) act(action string, args ...any) string {
	c.t.Helper()
	if args == nil {
		args = []any{}
	}
	body, err := json.Marshal(map[string]any{"handle": c.handle, "token": c.token, "action": action, "args": args})
	if err != nil {
		c.t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, actionPath, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Brick-Action", "1")
	req.Header.Set("Accept", brick.EffectsContentType)
	rec := httptest.NewRecorder()
	c.h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		c.t.Fatalf("%s%v: status %d\n%s", action, args, rec.Code, rec.Body.String())
	}
	var env struct {
		HTML    string         `json:"html"`
		Effects []brick.Effect `json:"effects"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		c.t.Fatalf("%s: not an effects envelope: %v\n%s", action, err, rec.Body.String())
	}
	c.fx = env.Effects
	return env.HTML
}

// flashed fails unless the last action flashed message at level.
func (c *contactsSession) flashed(level, message string) {
	c.t.Helper()
	for _, ef := range c.fx {
		if ef.Kind == brick.EffectFlash && ef.Level == level && ef.Message == message {
			return
		}
	}
	c.t.Errorf("want flash %s %q, got effects %+v", level, message, c.fx)
}

// dispatched returns the data of the last action's event name, failing
// when it was not dispatched.
func (c *contactsSession) dispatched(name string) map[string]any {
	c.t.Helper()
	for _, ef := range c.fx {
		if ef.Kind == brick.EffectDispatch && ef.Name == name {
			return ef.Data
		}
	}
	c.t.Errorf("want event %q dispatched, got effects %+v", name, c.fx)
	return nil
}

// contactsRowRe finds each rendered row's id and name, in order.
var contactsRowRe = regexp.MustCompile(`id="[^"]*-row-(\d+)">[\s\S]*?class="ct-name"[^>]*>([^<]+)</button>`)

// contactsNames lists the names of the rendered rows, in order.
func contactsNames(html string) []string {
	var out []string
	for _, m := range contactsRowRe.FindAllStringSubmatch(html, -1) {
		out = append(out, m[2])
	}
	return out
}

// contactsIDs lists the ids of the rendered rows, in order.
func contactsIDs(html string) []string {
	var out []string
	for _, m := range contactsRowRe.FindAllStringSubmatch(html, -1) {
		out = append(out, m[1])
	}
	return out
}

// contactsCount counts rows in the database matching where (a fixed
// clause written by the test).
func contactsCount(t *testing.T, db *sql.DB, where string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow("select count(*) from contacts where "+where, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// The first render is the newest ten of 64 contacts, with the header
// count, status counts, sort state and pager.
func TestContactsFirstRender(t *testing.T) {
	c, page, _ := contactsStart(t)
	mustContain(t, "first render", page,
		`<h1 class="bk-page-header__title">Contacts</h1>`, `64 people in your workspace`,
		`aria-pressed="true"`, `All<span class="ct-count">64</span>`,
		`aria-sort="descending"`, `<b>1</b>–<b>10</b> of <b>64</b>`,
		`placeholder="Search name, e-mail, company…"`, `brick:model.debounce.300ms="q"`,
		`role="menu" aria-label="Actions for `, `role="alertdialog"`, `data-bk-close-action="contacts.close_form"`,
		`<dialog id="`+c.id+`-form"`, `<form id="`+c.id+`-form-fields"`, `form="`+c.id+`-form-fields"`,
		`class="ct-total"`, `<b>64</b> contacts`, `data-brick-on="{&#34;contact-added&#34;:&#34;$refresh&#34;,&#34;contact-deleted&#34;:&#34;$refresh&#34;}"`,
	)
	if strings.Contains(page, `id="ct-`) {
		t.Error("an element id is not built from .ID")
	}
	if n := len(contactsNames(page)); n != 10 {
		t.Fatalf("want 10 rows, got %d", n)
	}
	if strings.Contains(page, `class="ct-bulk"`) || strings.Contains(page, `data-bk-close-action="contacts.close_form" open`) {
		t.Error("nothing is selected and the form is closed at first")
	}
}

// Create: a valid contact is inserted, shown first, and announced; an
// invalid one returns every error and inserts nothing; a duplicate e-mail
// is refused by the database check.
func TestContactsCreate(t *testing.T) {
	c, _, db := contactsStart(t)
	out := c.act("contacts.new_contact")
	mustContain(t, "new", out, `<h2 class="bk-dialog__title" id="`+c.id+`-form-title">New contact</h2>`, `data-bk-close-action="contacts.close_form" open`)

	out = c.act("contacts.save", map[string]string{"name": "A", "email": "not-an-email", "company": "", "role": "", "status": "ghost"})
	mustContain(t, "invalid", out, "Enter a name of at least two letters.", "Enter an e-mail address like ada@example.com.", "Choose active, lead or churned.", `value="not-an-email"`, `data-bk-close-action="contacts.close_form" open`)
	if n := contactsCount(t, db, "1 = 1"); n != 64 {
		t.Fatalf("an invalid save inserted a row: %d contacts", n)
	}

	taken := ""
	if err := db.QueryRow("select email from contacts where id = 7").Scan(&taken); err != nil {
		t.Fatal(err)
	}
	out = c.act("contacts.save", map[string]string{"name": "Grace Hopper", "email": strings.ToUpper(taken), "status": "lead"})
	mustContain(t, "duplicate", out, "Another contact already uses this e-mail.")
	if n := contactsCount(t, db, "1 = 1"); n != 64 {
		t.Fatalf("a duplicate e-mail was inserted: %d contacts", n)
	}

	out = c.act("contacts.save", map[string]string{"name": "Grace Hopper", "email": "grace@navy.mil", "company": "US Navy", "role": "Rear Admiral", "status": "active"})
	if n := contactsCount(t, db, "email = ? and name = ? and status = 'active'", "grace@navy.mil", "Grace Hopper"); n != 1 {
		t.Fatal("the contact was not inserted")
	}
	mustContain(t, "created", out, `65 people in your workspace`)
	c.flashed("success", "Added Grace Hopper to your contacts.")
	c.dispatched("contact-added")
	if strings.Contains(out, "bk-toast") {
		t.Error("a flash is an effect now; the component must not render a toast")
	}
	if names := contactsNames(out); len(names) == 0 || names[0] != "Grace Hopper" {
		t.Errorf("the new contact should be the first row (newest first), got %v", names)
	}
	if strings.Contains(out, `data-bk-close-action="contacts.close_form" open`) {
		t.Error("a valid save must close the form")
	}
}

// Edit loads the row into the form and saves changes; Duplicate prefills a
// new contact without the e-mail; the form's close action closes it.
func TestContactsEdit(t *testing.T) {
	c, _, db := contactsStart(t)
	var name, email string
	if err := db.QueryRow("select name, email from contacts where id = 3").Scan(&name, &email); err != nil {
		t.Fatal(err)
	}
	out := c.act("contacts.edit", 3)
	mustContain(t, "edit", out, `>Edit contact</h2>`, `value="`+name+`"`, `value="`+email+`"`, "Save changes")

	out = c.act("contacts.save", map[string]string{"name": "Renamed Person", "email": email, "company": "Brick", "role": "Founder", "status": "churned"})
	if n := contactsCount(t, db, "id = 3 and name = 'Renamed Person' and company = 'Brick' and status = 'churned'"); n != 1 {
		t.Fatal("the edit did not reach the database")
	}
	c.flashed("success", "Saved changes to Renamed Person.")
	if strings.Contains(out, `data-bk-close-action="contacts.close_form" open`) {
		t.Error("a valid edit must close the form")
	}
	if len(c.fx) != 1 {
		t.Errorf("an edit only flashes (the count is unchanged), got %+v", c.fx)
	}
	if n := contactsCount(t, db, "1 = 1"); n != 64 {
		t.Errorf("an edit changed the row count: %d", n)
	}

	out = c.act("contacts.duplicate", 3)
	mustContain(t, "duplicate", out, `>New contact</h2>`, `value="Renamed Person (copy)"`, `name="email" value=""`)
	out = c.act("contacts.close_form")
	if strings.Contains(out, `data-bk-close-action="contacts.close_form" open`) {
		t.Error("close_form must close the dialog")
	}
}

// Delete removes the row and names it in a toast; bulk delete removes every
// selected row; "Mark active" updates them.
func TestContactsDelete(t *testing.T) {
	c, page, db := contactsStart(t)
	first := contactsIDs(page)[0]
	var name string
	if err := db.QueryRow("select name from contacts where id = ?", first).Scan(&name); err != nil {
		t.Fatal(err)
	}
	out := c.act("contacts.remove", json.Number(first))
	if n := contactsCount(t, db, "id = ?", first); n != 0 {
		t.Fatal("the contact is still in the database")
	}
	mustContain(t, "deleted", out, "63 people in your workspace")
	c.flashed("success", "Deleted "+name+".")
	if ids, _ := json.Marshal(c.dispatched("contact-deleted")["ids"]); string(ids) != "["+first+"]" {
		t.Errorf("contact-deleted ids = %s, want [%s]", ids, first)
	}
	if strings.Contains(out, `-row-`+first+`"`) {
		t.Error("the deleted row is still rendered")
	}

	ids := contactsIDs(out)[:3]
	for _, id := range ids {
		out = c.act("contacts.toggle", json.Number(id), true)
	}
	mustContain(t, "selected", out, `<span class="ct-bulk__count">3 selected</span>`, `id="`+c.id+`-bulk-delete"`, `data-bk-open="`+c.id+`-bulk-delete"`, `Delete 3 contacts?`, `class="ct-row--selected"`)
	out = c.act("contacts.toggle", json.Number(ids[2]), false)
	mustContain(t, "unselected", out, "2 selected")
	out = c.act("contacts.activate_selected")
	c.flashed("success", "Marked 2 contacts active.")
	if n := contactsCount(t, db, "id in (?, ?) and status = 'active'", ids[0], ids[1]); n != 2 {
		t.Error("Mark active did not update both rows")
	}
	if strings.Contains(out, `class="ct-bulk"`) {
		t.Error("the selection is cleared after a bulk action")
	}

	for _, id := range ids {
		out = c.act("contacts.toggle", json.Number(id), true)
	}
	mustContain(t, "three selected", out, `<span class="ct-bulk__count">3 selected</span>`)
	out = c.act("contacts.remove_selected")
	if n := contactsCount(t, db, "id in (?, ?, ?)", ids[0], ids[1], ids[2]); n != 0 {
		t.Fatal("bulk delete left rows behind")
	}
	mustContain(t, "bulk deleted", out, "60 people in your workspace")
	c.flashed("success", "Deleted 3 contacts.")
	if got := c.dispatched("contact-deleted")["ids"]; len(got.([]any)) != 3 {
		t.Errorf("one contact-deleted event carries every id, got %v", got)
	}

	out = c.act("contacts.toggle_page", true)
	mustContain(t, "page selected", out, "10 selected", "Select all 60 matching")
	out = c.act("contacts.select_matching")
	mustContain(t, "all selected", out, "60 selected")
	out = c.act("contacts.clear_selection")
	if strings.Contains(out, `class="ct-bulk"`) {
		t.Error("clear_selection leaves the bulk bar")
	}
}

// Search, the status filter, sorting and paging change the rendered rows
// exactly as the same query on the database would.
func TestContactsListing(t *testing.T) {
	c, _, db := contactsStart(t)

	// Search (the brick:model $set) filters on the server; a miss shows the
	// empty state, and clearing the filters brings everyone back.
	out := c.act("$set", "q", "acme")
	want := contactsCount(t, db, "name like '%acme%' or email like '%acme%' or company like '%acme%' or role like '%acme%'")
	if n := len(contactsNames(out)); n != min(want, 10) || want == 0 {
		t.Fatalf("search acme: %d rows, want %d", n, want)
	}
	out = c.act("$set", "q", "zzzz-nobody")
	mustContain(t, "no match", out, `No contacts match “zzzz-nobody”`, `brick:click="contacts.clear_filters"`,
		"64 people in your workspace · "+strconv.Itoa(contactsCount(t, db, "status = 'active'"))+" active")
	out = c.act("contacts.clear_filters")
	if n := len(contactsNames(out)); n != 10 {
		t.Fatalf("clear_filters: %d rows", n)
	}

	// Status filter: every row has the chosen status, and the count matches.
	out = c.act("contacts.filter_status", "churned")
	churned := contactsCount(t, db, "status = 'churned'")
	mustContain(t, "churned", out, `<b>1</b>–<b>10</b> of <b>`)
	if strings.Count(out, `class="bk-badge`) == 0 || strings.Contains(out, ">Active</span></td>") {
		t.Error("the churned filter shows other statuses")
	}
	if !strings.Contains(out, `of <b>`+strconv.Itoa(churned)+`</b>`) {
		t.Errorf("churned filter: the pager does not say %d", churned)
	}
	out = c.act("contacts.filter_status", "all")
	mustContain(t, "all statuses", out, `of <b>64</b>`)

	// Sort by name ascending, then descending: the database's order.
	out = c.act("contacts.sort_by", "name")
	mustContain(t, "name asc", out, `aria-sort="ascending"`)
	if got, want := contactsNames(out), contactsColumn(t, db, "select name from contacts order by name collate nocase asc, id asc limit 10"); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("name asc:\n got  %v\n want %v", got, want)
	}
	out = c.act("contacts.sort_by", "name")
	mustContain(t, "name desc", out, `aria-sort="descending"`)
	if got, want := contactsNames(out), contactsColumn(t, db, "select name from contacts order by name collate nocase desc, id desc limit 10"); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("name desc:\n got  %v\n want %v", got, want)
	}

	// Pages: page 3 of the name-descending order, then 25 per page.
	mustContain(t, "pager", out, `brick:click="contacts.goto(2)"`, `brick:click="contacts.goto(2)" rel="next"`)
	out = c.act("contacts.goto", 3)
	mustContain(t, "page 3", out, `<b>21</b>–<b>30</b> of <b>64</b>`, `aria-current="page"`, `brick:click="contacts.goto(4)"`)
	if got, want := contactsNames(out), contactsColumn(t, db, "select name from contacts order by name collate nocase desc, id desc limit 10 offset 20"); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("page 3:\n got  %v\n want %v", got, want)
	}
	out = c.act("contacts.per_page", "25")
	mustContain(t, "25 per page", out, `<b>1</b>–<b>25</b> of <b>64</b>`)
	if n := len(contactsNames(out)); n != 25 {
		t.Errorf("25 per page: %d rows", n)
	}
	// A page chosen for one search does not carry over to the next.
	out = c.act("contacts.goto", 2)
	mustContain(t, "page 2", out, `<b>26</b>–<b>50</b> of <b>64</b>`)
	out = c.act("$set", "q", "a")
	mustContain(t, "new search", out, `<b>1</b>–<b>`)

	// The detail drawer opens on a record and closes.
	out = c.act("contacts.show", 5)
	var name string
	if err := db.QueryRow("select name from contacts where id = 5").Scan(&name); err != nil {
		t.Fatal(err)
	}
	mustContain(t, "detail", out, `<h2 class="bk-drawer__title" id="`+c.id+`-detail-title">`+name+`</h2>`, `data-bk-close-action="contacts.close_detail" open`, `Recent activity`, `Added to Contacts`)
	out = c.act("contacts.close_detail")
	if strings.Contains(out, `data-bk-close-action="contacts.close_detail" open`) {
		t.Error("close_detail must close the drawer")
	}
}

// contactsColumn runs a fixed query and returns its first column.
func contactsColumn(t *testing.T, db *sql.DB, q string) []string {
	t.Helper()
	rows, err := db.Query(q)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		out = append(out, s)
	}
	return out
}

// The topbar's ContactCount listens to the events ContactsApp dispatches:
// deleting one contact, then three, then adding one, each refreshes the
// count through an ordinary $refresh action, and nothing else on the page
// listens. Driven through bricktest.Live, which delivers events the way
// the runtime does.
func TestContactsCountFollowsEvents(t *testing.T) {
	s, db, err := newSite(context.Background(), ".", false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	live := bricktest.Live(t, s.Engine(), "content/apps/contacts.html")
	app, count := live.Component("ContactsApp"), live.Component("ContactCount")
	if got := count.Text(); got != "64 contacts" {
		t.Fatalf("first count = %q", got)
	}

	var first int
	var name string
	if err := db.QueryRow("select id, name from contacts order by created desc, id desc limit 1").Scan(&first, &name); err != nil {
		t.Fatal(err)
	}
	res := app.Call("contacts.remove", first)
	bricktest.AssertDispatched(t, res, "contact-deleted", map[string]any{"ids": []any{first}})
	bricktest.AssertFlash(t, res, "success", "Deleted "+name+".")
	bricktest.AssertNoRedirect(t, res)
	if got := count.Text(); got != "64 contacts" {
		t.Fatalf("the count changed before delivery: %q", got)
	}
	if ran := live.Deliver(res); len(ran) != 1 {
		t.Fatalf("one listener (ContactCount) should run, ran %d", len(ran))
	}
	if got := count.Text(); got != "63 contacts" {
		t.Fatalf("after one delete: %q", got)
	}

	rows := contactsColumn(t, db, "select id from contacts order by created desc, id desc limit 3")
	for _, id := range rows {
		n, _ := strconv.Atoi(id)
		app.Call("contacts.toggle", n, true)
	}
	res = app.Call("contacts.remove_selected")
	if ran := live.Deliver(res); len(ran) != 1 {
		t.Fatalf("a bulk delete is one event, so one refresh; ran %d", len(ran))
	}
	if got := count.Text(); got != "60 contacts" {
		t.Fatalf("after a bulk delete: %q", got)
	}

	app.Call("contacts.new_contact")
	res = app.Call("contacts.save", map[string]any{"name": "Grace Hopper", "email": "grace@navy.mil", "status": "active"})
	bricktest.AssertDispatched(t, res, "contact-added", nil)
	live.Deliver(res)
	if got := count.Text(); got != "61 contacts" {
		t.Fatalf("after an add: %q", got)
	}

	// A plain navigation action dispatches nothing and wakes nobody.
	res = app.Call("contacts.goto", 2)
	bricktest.AssertNotDispatched(t, res, "contact-deleted")
	if ran := live.Deliver(res); len(ran) != 0 {
		t.Errorf("goto woke %d listeners", len(ran))
	}
}

// Two ContactsApps on one page (testdata/two-contacts.html) render no
// duplicate element ids, and each copy's buttons open its own dialogs.
func TestContactsTwoCopies(t *testing.T) {
	s, db, err := newSite(context.Background(), ".", false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	live := bricktest.Live(t, s.Engine(), "testdata/two-contacts.html")
	apps := live.Components("ContactsApp")
	if len(apps) != 2 {
		t.Fatalf("want two ContactsApps, got %d", len(apps))
	}
	dupes := map[string]int{}
	for _, m := range regexp.MustCompile(`\sid="([^"]+)"`).FindAllStringSubmatch(live.HTML(), -1) {
		dupes[m[1]]++
	}
	for id, n := range dupes {
		if n > 1 {
			t.Errorf("id %q appears %d times", id, n)
		}
	}
	for _, a := range apps {
		a.Call("contacts.toggle", 1, true)
	}
	for _, a := range apps {
		h := a.HTML()
		own := contactsAttr(contactsRootRe("ContactsApp").FindString(h), "data-brick")
		mustContain(t, "copy "+own, h, `data-bk-open="`+own+`-bulk-delete"`, `id="`+own+`-bulk-delete"`, `<dialog id="`+own+`-form"`)
	}
}
