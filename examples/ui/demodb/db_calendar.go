package demodb

import (
	"context"
	"database/sql"
	"fmt"
)

// calendarSchema is the Calendar app's table (content/apps/calendar.html,
// apps/calendar/calendar.star). seed runs it after schema.
const calendarSchema = `
create table events (
  id integer primary key,
  title text not null,
  start text not null,      -- YYYY-MM-DDTHH:MM; YYYY-MM-DD when all_day
  end text not null,        -- same shape as start and never before it; an all-day end is its last day, inclusive
  all_day integer not null, -- 0 | 1
  tone text not null,       -- primary | info | success | warning | danger | neutral (apps/calendar/calendar.star TONES)
  location text not null,
  notes text not null,
  attendees text not null   -- names joined by commas, as TagInput posts them
);
`

// calendarSeed is the seeded events around the demo's today, 2026-09-27
// (apps/calendar/calendar.star TODAY): a normal working week with overlapping
// meetings on Monday the 28th, all-day and multi-day events, and enough on
// one day (Sep 30) to need "+N more" in the month grid.
var calendarSeed = []struct {
	title, start, end string
	allDay            bool
	tone, location    string
	notes, attendees  string
}{
	{"Design review", "2026-09-01T10:00", "2026-09-01T11:00", false, tonePrimary, "Studio", "Pricing page, round two.", "Maya Chen,Zara Singh"},
	{"Quarterly planning", "2026-09-03", "2026-09-04", true, toneInfo, "Main office", "", "Everyone"},
	{"Labor Day", "2026-09-07", "2026-09-07", true, toneNeutral, "", "Office closed.", ""},
	{"1:1 with Liam", "2026-09-09T15:00", "2026-09-09T15:30", false, toneSuccess, "Room 2", "", "Liam Kim"},
	{"Customer call — Northwind", "2026-09-14T11:00", "2026-09-14T12:00", false, toneWarning, "Zoom", "Renewal and the SSO question.", "Omar Haddad"},
	{"Team lunch", "2026-09-18T12:30", "2026-09-18T13:30", false, toneSuccess, "Ramen place", "", "Liam Kim,Noah Silva"},
	{"Security audit", "2026-09-21T09:00", "2026-09-21T12:00", false, toneDanger, "Room 4", "Bring the access logs.", "Noah Silva"},
	{"Hackathon", "2026-09-23", "2026-09-25", true, tonePrimary, "Everywhere", "Build anything with the kit.", "Everyone"},
	{"Standup", "2026-09-28T09:00", "2026-09-28T09:15", false, toneNeutral, "Zoom", "", "Engineering"},
	{"Launch rehearsal", "2026-09-28T10:00", "2026-09-28T11:30", false, tonePrimary, "Studio", "Full run-through with slides.", "Maya Chen,Liam Kim"},
	{"Press briefing", "2026-09-28T10:30", "2026-09-28T11:00", false, toneWarning, "Zoom", "", "Zara Singh"},
	{"Lunch with Maya", "2026-09-28T12:30", "2026-09-28T13:30", false, toneSuccess, "Café Nero", "", "Maya Chen"},
	{"Standup", "2026-09-29T09:00", "2026-09-29T09:15", false, toneNeutral, "Zoom", "", "Engineering"},
	{"Hiring panel", "2026-09-29T14:00", "2026-09-29T16:00", false, toneInfo, "Room 2", "Senior designer, final round.", "Maya Chen,Omar Haddad"},
	{"Standup", "2026-09-30T09:00", "2026-09-30T09:15", false, toneNeutral, "Zoom", "", "Engineering"},
	{"Launch go/no-go", "2026-09-30T11:00", "2026-09-30T11:30", false, toneDanger, "Studio", "", "Leads"},
	{"Docs freeze", "2026-09-30T13:00", "2026-09-30T13:30", false, toneInfo, "", "", "Zara Singh"},
	{"Demo prep", "2026-09-30T15:00", "2026-09-30T17:00", false, tonePrimary, "Studio", "", "Maya Chen,Liam Kim"},
	{"Launch day 🚀", "2026-10-01", "2026-10-01", true, toneSuccess, "", "v2 goes live at 9:00 PT.", "Everyone"},
	{"Launch party", "2026-10-01T18:00", "2026-10-01T21:00", false, toneWarning, "Rooftop", "", "Everyone"},
	{"Standup", "2026-10-02T09:00", "2026-10-02T09:15", false, toneNeutral, "Zoom", "", "Engineering"},
	{"Retro", "2026-10-02T16:00", "2026-10-02T17:00", false, toneInfo, "Room 4", "What went well, what didn't.", "Engineering"},
	{"Team offsite", "2026-10-05", "2026-10-06", true, tonePrimary, "Lake house", "Agenda to follow.", "Everyone"},
	{"Board meeting", "2026-10-14T14:00", "2026-10-14T16:00", false, toneDanger, "Main office", "", "Leads"},
	{"Conference talk", "2026-10-21T10:00", "2026-10-21T10:45", false, toneWarning, "Berlin", "Server-rendered UIs in Go.", "Ada Lovelace"},
}

// seedCalendar creates the events table and fills it with calendarSeed.
func seedCalendar(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, calendarSchema); err != nil {
		return fmt.Errorf("calendar schema: %w", err)
	}
	for _, e := range calendarSeed {
		if _, err := db.ExecContext(ctx, "insert into events (title, start, end, all_day, tone, location, notes, attendees) values (?, ?, ?, ?, ?, ?, ?, ?)",
			e.title, e.start, e.end, boolInt(e.allDay), e.tone, e.location, e.notes, e.attendees); err != nil {
			return fmt.Errorf("seed events: %w", err)
		}
	}
	return nil
}
