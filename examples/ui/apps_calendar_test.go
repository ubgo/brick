package main

import (
	"context"
	"database/sql"
	"net/http"
	"regexp"
	"strings"
	"testing"
)

// calSite builds the showcase with its database in reach, so a test can
// check what an action wrote.
func calSite(t *testing.T) (http.Handler, *sql.DB) {
	t.Helper()
	s, db, err := newSite(context.Background(), ".", false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return s.Handler(), db
}

var calCellRe = regexp.MustCompile(`role="gridcell" data-date="(\d{4}-\d\d-\d\d)"`)

// calCells splits a month grid into its cells' HTML, keyed by date, in
// order.
func calCells(t *testing.T, page string) ([]string, map[string]string) {
	t.Helper()
	locs := calCellRe.FindAllStringSubmatchIndex(page, -1)
	var order []string
	cells := map[string]string{}
	for i, l := range locs {
		end := len(page)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		d := page[l[2]:l[3]]
		order = append(order, d)
		cells[d] = page[l[1]:end]
	}
	return order, cells
}

// calDays is apps/calendar/calendar.star days(): days since 1970-01-01.
func calDays(t *testing.T, db *sql.DB, date string) int {
	t.Helper()
	var n int
	if err := db.QueryRow("select cast(julianday(?) - julianday('1970-01-01') as integer)", date).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// The month grid: September 2026 starts on a Tuesday, so the six weeks
// run from Sunday Aug 30 to Saturday Oct 10; events land on their days,
// multi-day events on each, and a full day folds into "+N more".
func TestCalendarMonthGrid(t *testing.T) {
	h, _ := calSite(t)
	code, page := get(t, h, "/apps/calendar/")
	if code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	landCallsParse(t, page)
	order, cells := calCells(t, page)
	if len(order) != 42 || order[0] != "2026-08-30" || order[2] != "2026-09-01" || order[41] != "2026-10-10" {
		t.Fatalf("grid: %d cells, first %q, third %q, last %q", len(order), order[0], order[2], order[len(order)-1])
	}
	mustContain(t, "today", page, `data-date="2026-09-27" aria-current="date"`, `<h1>September 2026</h1>`, `aria-pressed="true" brick:click="cal.set_view(0)"`)
	mustContain(t, "Sep 1", cells["2026-09-01"], "Design review", "<time>10:00</time>", "cal-tone--primary")
	for _, d := range []string{"2026-09-23", "2026-09-24", "2026-09-25"} {
		mustContain(t, d, cells[d], "Hackathon", "cal-chip--all")
	}
	if strings.Contains(cells["2026-09-22"], "Hackathon") || strings.Contains(cells["2026-09-26"], "Hackathon") {
		t.Error("Hackathon spills outside Sep 23–25")
	}
	mustContain(t, "Sep 30", cells["2026-09-30"], "2 more</button>", "Demo prep")
	if strings.Contains(cells["2026-09-29"], "more") {
		t.Error("Sep 29 has two events and needs no +N more")
	}
	mustContain(t, "out of month", page, `cal-m__cell cal-m__cell--out cal-m__cell--weekend" role="gridcell" data-date="2026-08-30"`)
}

// The week view places overlapping meetings side by side and keeps
// all-day events in their own row.
func TestCalendarWeek(t *testing.T) {
	h, _ := calSite(t)
	s := landOpen(t, h, "/apps/calendar/")
	out := s.act("cal.set_view", 1)
	mustContain(t, "week", out, "Sep 27 – Oct 3, 2026", `cal-w__today`,
		`--lane: 0; --lanes: 2" brick:click="cal.edit(10)"`,                    // Launch rehearsal 10:00–11:30
		`--lane: 1; --lanes: 2" brick:click="cal.edit(11)"`,                    // Press briefing 10:30–11:00, beside it
		`--lane: 0; --lanes: 1" brick:click="cal.edit(12)"`,                    // Lunch with Maya, alone
		`cal-chip cal-chip--all cal-tone--success" brick:click="cal.edit(19)"`) // Launch day, all day
	out = s.act("cal.next")
	mustContain(t, "next week", out, "Oct 4 – Oct 10, 2026", "Team offsite")
	out = s.act("cal.set_view", 2)
	mustContain(t, "agenda", out, "October 2026", "Board meeting", "Conference talk")
	out = s.act("cal.go_today")
	mustContain(t, "today", out, "September 2026", "Design review", "cal-a__day--past")
}

// Create, edit and delete through the actions, checked in the database;
// invalid input is refused with the reason and writes nothing.
func TestCalendarCRUD(t *testing.T) {
	h, db := calSite(t)
	count := func() int {
		var n int
		if err := db.QueryRow("select count(*) from events").Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	before := count()
	s := landOpen(t, h, "/apps/calendar/")

	out := s.act("cal.new_on", calDays(t, db, "2026-10-08"))
	mustContain(t, "new on a day", out, "New event", `value="2026-10-08"`, `value="09:00"`, `value="10:00"`)

	bad := map[string]string{"title": "", "start_date": "2026-10-08", "start_time": "15:00", "end_date": "2026-10-08", "end_time": "14:00", "tone": "info"}
	out = s.act("cal.save", bad)
	mustContain(t, "invalid", out, "Give the event a title.", "The event must end after it starts.")
	bad["title"], bad["end_date"], bad["tone"] = "Bad", "2026-02-30", "pink"
	out = s.act("cal.save", bad)
	mustContain(t, "invalid date and tone", out, "Enter an end date and time.", "Pick a colour.")
	if count() != before {
		t.Fatal("an invalid event was written")
	}

	out = s.act("cal.save", map[string]string{"title": "Roadmap review", "start_date": "2026-10-08", "start_time": "15:00", "end_date": "2026-10-08", "end_time": "16:30", "tone": "success", "location": "Room 2", "attendees": "Maya Chen, ,Liam Kim", "notes": "Q4"})
	mustContain(t, "created", out, "Event created", "Roadmap review")
	var id int
	var start, end, tone, attendees string
	var allDay int
	if err := db.QueryRow("select id, start, end, all_day, tone, attendees from events where title = 'Roadmap review'").Scan(&id, &start, &end, &allDay, &tone, &attendees); err != nil {
		t.Fatal(err)
	}
	if start != "2026-10-08T15:00" || end != "2026-10-08T16:30" || allDay != 0 || tone != "success" || attendees != "Maya Chen,Liam Kim" {
		t.Errorf("stored %q %q %d %q %q", start, end, allDay, tone, attendees)
	}

	out = s.act("cal.edit", id)
	mustContain(t, "edit", out, "Edit event", `value="Roadmap review"`, `value="15:00"`, `value="Maya Chen,Liam Kim"`, "Delete event?")
	out = s.act("cal.save", map[string]string{"title": "Roadmap review (all day)", "all_day": "on", "start_date": "2026-10-08", "end_date": "2026-10-09", "tone": "warning"})
	mustContain(t, "updated", out, "Event updated")
	if err := db.QueryRow("select start, end, all_day from events where id = ?", id).Scan(&start, &end, &allDay); err != nil {
		t.Fatal(err)
	}
	if start != "2026-10-08" || end != "2026-10-09" || allDay != 1 {
		t.Errorf("updated to %q %q %d", start, end, allDay)
	}
	if count() != before+1 {
		t.Errorf("edit changed the row count: %d", count())
	}

	s.act("cal.edit", id)
	out = s.act("cal.save", map[string]string{"title": "Backwards", "all_day": "on", "start_date": "2026-10-09", "end_date": "2026-10-08", "tone": "info"})
	mustContain(t, "all-day backwards", out, "The last day can&#39;t be before the first.")

	out = s.act("cal.delete", id)
	mustContain(t, "deleted", out, "Event deleted")
	if count() != before {
		t.Errorf("delete left %d rows, want %d", count(), before)
	}
	out = s.act("cal.close_form")
	if strings.Contains(out, "Event deleted") {
		t.Error("the toast came back on the next render")
	}
}
