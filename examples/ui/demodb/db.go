// Package demodb is the showcase's demo database: an in-memory SQLite
// database seeded with the same rows on every run, which the demo apps'
// Starlark modules query through the script db capability.
//
// It is its own package, not part of the showcase's package main, so any
// program that renders the showcase's pages can open the same database: the
// showcase itself (examples/ui) and the formatter's render comparison
// (cmd/brick's TestFmtPreservesRendering), which renders every page before
// and after formatting. Every Open returns a private database, so callers
// never see each other's writes.
package demodb

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	_ "modernc.org/sqlite" // the pure-Go SQLite driver: no cgo (a brick non-goal)
)

// dbDSN is a private in-memory database. One connection keeps it alive and
// serializes writers, which SQLite needs anyway; the demo's traffic is one
// person clicking.
const dbDSN = "file:brickui?mode=memory"

// seedEpoch is the first seeded date. Seed rows count forward from it, so
// every run produces the same rows and the pages that show them the same
// bytes.
var seedEpoch = time.Date(2026, time.January, 5, 0, 0, 0, 0, time.UTC)

// The seed's shared name parts: several apps' rows draw people from them.
var (
	firstNames = []string{"Ava", "Liam", "Maya", "Noah", "Zara", "Ethan", "Iris", "Leo", "Nora", "Omar", "Priya", "Ravi", "Sara", "Theo", "Uma", "Vik", "Wen", "Yara"}
	lastNames  = []string{"Patel", "Kim", "Garcia", "Chen", "Singh", "Müller", "Rossi", "Silva", "Novak", "Haddad", "Okafor", "Tanaka"}
)

// Open creates a fresh demo database and fills it with deterministic rows.
// The caller closes it.
func Open(ctx context.Context) (*sql.DB, error) {
	db, err := sql.Open("sqlite", dbDSN)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err := seed(ctx, db); err != nil {
		return nil, errors.Join(err, db.Close())
	}
	return db, nil
}

func day(n int) string { return seedEpoch.AddDate(0, 0, n).Format(time.DateOnly) }

// seedApps is every app's schema-and-seed step, each in its app's own
// db_<app>.go, in the order they run. Invoices reads the seeded contacts,
// so it runs after Contacts.
var seedApps = []func(context.Context, *sql.DB) error{
	seedContacts,  // db_contacts.go
	seedTasks,     // db_tasks.go
	seedInbox,     // db_inbox.go
	seedDashboard, // db_dashboard.go
	seedStore,     // db_store.go
	seedInvoices,  // db_invoices.go
	seedChat,      // db_chat.go
	seedCalendar,  // db_calendar.go
	seedCMS,       // db_cms.go
	seedPush,      // db_push.go
}

func seed(ctx context.Context, db *sql.DB) error {
	for _, f := range seedApps {
		if err := f(ctx, db); err != nil {
			return err
		}
	}
	return nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func lower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 'a' - 'A'
		}
	}
	return string(b)
}

// asciiFold spells the accented letters of the seed names in ASCII, the
// way people write them in e-mail addresses (Müller → Mueller).
var asciiFold = map[rune]string{'ä': "ae", 'ö': "oe", 'ü': "ue", 'Ä': "Ae", 'Ö': "Oe", 'Ü': "Ue", 'ß': "ss", 'é': "e", 'è': "e", 'á': "a", 'í': "i", 'ó': "o", 'ñ': "n", 'ç': "c"}

// asciiOnly makes a name safe for a seeded e-mail address: accented
// letters are folded (asciiFold), anything else outside ASCII is dropped.
func asciiOnly(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r < 128:
			b.WriteRune(r)
		case asciiFold[r] != "":
			b.WriteString(asciiFold[r])
		}
	}
	return b.String()
}
