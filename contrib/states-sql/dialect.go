package statessql

import (
	"fmt"
	"strconv"
	"time"
)

// Dialect is everything that differs between SQL databases for this one
// table: the placeholder style, the DDL, the insert-or-replace statement
// and how a time is bound. Postgres, MySQL and SQLite ship; any other
// database (CockroachDB, MariaDB, SQL Server) is a Dialect value the host
// writes, with no change to this package.
//
// Statements are written with "?" placeholders; the store rewrites them
// through Placeholder, so a dialect never repeats the rebinding.
type Dialect struct {
	// Name labels the dialect in errors.
	Name string
	// Placeholder returns the bind marker for the n-th argument, 1-based
	// ("$1" on Postgres, "?" elsewhere).
	Placeholder func(n int) string
	// Schema returns the statements that create the table and its two
	// indexes. Each must be idempotent (IF NOT EXISTS), because Migrate
	// runs them on every start.
	Schema func(table string) []string
	// Upsert returns the statement that inserts one row or replaces the
	// row with the same handle, binding handle, session, data and expires
	// in that order.
	Upsert func(table string) string
	// TimeArg turns an expiry into the value bound for the expires column.
	// Every comparison binds through it too, so a database that compares
	// times as text (SQLite) sees one fixed-width, sortable format.
	TimeArg func(t time.Time) any
}

// Column sizes shared by every dialect. A handle is a 128-bit hex key and
// a session is a SHA-256 hex hash (brick), so 64 characters hold both with
// room for a host's own handle format.
const (
	handleChars  = 64
	sessionChars = 64
	// sqliteTime is fixed-width (milliseconds always present), so SQLite's
	// text comparison of two expiries orders them by time.
	sqliteTime = "2006-01-02 15:04:05.000"
)

// questionMark is the placeholder of MySQL and SQLite.
func questionMark(int) string { return "?" }

// utc binds a time as UTC, so a TIMESTAMP column without a zone stores the
// same instant whatever the server's or the process's zone is.
func utc(t time.Time) any { return t.UTC() }

// indexes are the two lookups every dialect needs: by session for
// RevokeSession, by expiry for Prune.
func indexes(table string) []string {
	return []string{
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS %s_session ON %s (session)", table, table),
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS %s_expires ON %s (expires)", table, table),
	}
}

// conflictUpsert is the ON CONFLICT form Postgres and SQLite share.
func conflictUpsert(table string) string {
	return "INSERT INTO " + table + " (handle, session, data, expires) VALUES (?, ?, ?, ?) " +
		"ON CONFLICT (handle) DO UPDATE SET session = excluded.session, data = excluded.data, expires = excluded.expires"
}

// Postgres stores the data as BYTEA and binds "$n". Like SQLite and MySQL it
// is a function returning a fresh Dialect: a package-level Dialect would let
// any importer reassign a field (Upsert, TimeArg) for every store in the process.
func Postgres() Dialect {
	return Dialect{
		Name:        "postgres",
		Placeholder: func(n int) string { return "$" + strconv.Itoa(n) },
		Schema: func(table string) []string {
			return append([]string{fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
  handle   VARCHAR(%d) PRIMARY KEY,
  session  VARCHAR(%d) NOT NULL DEFAULT '',
  data     BYTEA NOT NULL,
  expires  TIMESTAMP NOT NULL
)`, table, handleChars, sessionChars)}, indexes(table)...)
		},
		Upsert:  conflictUpsert,
		TimeArg: utc,
	}
}

// SQLite keeps the spec's DDL as written and binds expiries as fixed-width
// text (see TimeArg).
func SQLite() Dialect {
	return Dialect{
		Name:        "sqlite",
		Placeholder: questionMark,
		Schema: func(table string) []string {
			return append([]string{fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
  handle   VARCHAR(%d) PRIMARY KEY,
  session  VARCHAR(%d) NOT NULL DEFAULT '',
  data     BLOB NOT NULL,
  expires  TIMESTAMP NOT NULL
)`, table, handleChars, sessionChars)}, indexes(table)...)
		},
		Upsert:  conflictUpsert,
		TimeArg: func(t time.Time) any { return t.UTC().Format(sqliteTime) },
	}
}

// MySQL differs from the spec's neutral DDL in three places, each to avoid
// a MySQL-only trap: MEDIUMBLOB, because BLOB stops at 64 KiB and a state
// near the sealed store's default limit would be refused or truncated;
// DATETIME, because a TIMESTAMP column is converted through the session
// zone, ends in 2038 and, on servers without explicit_defaults_for_
// timestamp, updates itself on every row change; and the indexes inside
// CREATE TABLE, because MySQL has no CREATE INDEX IF NOT EXISTS.
func MySQL() Dialect {
	return Dialect{
		Name:        "mysql",
		Placeholder: questionMark,
		Schema: func(table string) []string {
			return []string{fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
  handle   VARCHAR(%d) PRIMARY KEY,
  session  VARCHAR(%d) NOT NULL DEFAULT '',
  data     MEDIUMBLOB NOT NULL,
  expires  DATETIME NOT NULL,
  INDEX %s_session (session),
  INDEX %s_expires (expires)
)`, table, handleChars, sessionChars, table, table)}
		},
		Upsert: func(table string) string {
			return "INSERT INTO " + table + " (handle, session, data, expires) VALUES (?, ?, ?, ?) " +
				"ON DUPLICATE KEY UPDATE session = VALUES(session), data = VALUES(data), expires = VALUES(expires)"
		},
		TimeArg: utc,
	}
}
