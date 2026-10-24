// Package statessql is a brick.BytesStore on database/sql: one table of
// handle, session, data and expiry, for Postgres, MySQL, SQLite or any
// database a host describes with a Dialect. Wrap it
// with brick.Bytes, which brings the TTL, the codec and optional
// encryption at rest, so this package only moves bytes.
//
//	db, _ := sql.Open("pgx", dsn) // your driver, your pool settings
//	st, err := statessql.New(statessql.Options{DB: db, Dialect: statessql.Postgres()})
//	err = st.Migrate(ctx)         // once at start; idempotent
//	eng, err := brick.New(brick.Options{States: brick.Bytes(st, brick.BytesOptions{TTL: 30 * time.Minute})})
//
// The package imports no driver: the host opens the *sql.DB, so the
// choice of driver, DSN and pool stays with the application.
//
// A database keeps an expired row until something deletes it, so every
// read filters on the expiry and Prune deletes the rest. The engine starts
// no goroutine; the site layer calls Prune on a timer (site.Options.
// PruneEvery), and a host serving brick its own way calls it itself.
//
// A failed query is returned as an error, never as "not found": the
// engine answers an action 503 for a store error and 410 only for a
// missing instance, so a database outage must not make every live
// component on every page recover from scratch.
package statessql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/ubgo/brick"
)

// DefaultTable is the table name when Options.Table is empty.
const DefaultTable = "brick_states"

// eachBatch is how many rows EachBytes reads per query. It reads in
// batches (keyset pagination on handle) rather than holding one cursor
// open while the callback runs, so a callback that uses the same pool, or
// a pool of one connection (common with SQLite), cannot deadlock.
const eachBatch = 100

// tableName is the identifiers a table may be named with. The name is
// spliced into SQL text (no driver binds identifiers), so anything else
// is refused rather than quoted.
var tableName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// Options configures New.
type Options struct {
	// DB is the host's pool. Required.
	DB *sql.DB
	// Table is the table name; empty means DefaultTable. Letters, digits
	// and underscores only. Two apps sharing a database use two tables.
	Table string
	// Dialect describes the database. Required: Postgres(), MySQL(), SQLite(),
	// or a host's own value. There is no default, because a wrong guess
	// fails only at the first query.
	Dialect Dialect
	// Clock is the time reads compare expiries against and saves add the
	// TTL to; nil means time.Now (others for tests).
	Clock func() time.Time
}

// Store is the BytesStore. It is also a brick.Revoker, brick.Pruner,
// brick.BytesLister and brick.BytesEndWatcher, which brick.Bytes passes
// through. Safe for concurrent use, as *sql.DB is.
//
// Instance ends it reports: expired (found by Prune) and revoked. It never
// reports evicted, because a table has no size bound.
type Store struct {
	db      *sql.DB
	dialect Dialect
	table   string
	clock   func() time.Time

	load, upsert, remove, revoke, prune, each string
	// The watched paths: read the rows first, then delete each one only if
	// it still qualifies, and report it only when that delete removed it.
	expired, pruneOne, bySession, revokeOne string

	mu   sync.Mutex
	ends brick.BytesEndFunc
}

// Interface checks: the upgrades are detected by type assertion, so a
// missing method would silently turn RevokeSession or Prune off.
var (
	_ brick.BytesStore  = (*Store)(nil)
	_ brick.BytesLister = (*Store)(nil)
	_ brick.Revoker     = (*Store)(nil)
	_ brick.Pruner      = (*Store)(nil)

	_ brick.BytesEndWatcher = (*Store)(nil)
)

// ErrOptions reports a missing or invalid option.
var ErrOptions = errors.New("statessql: invalid options")

// New validates the options and prepares the statements. It does not
// touch the database; call Migrate to create the table.
func New(opts Options) (*Store, error) {
	if opts.DB == nil {
		return nil, fmt.Errorf("%w: DB is required", ErrOptions)
	}
	d := opts.Dialect
	if d.Placeholder == nil || d.Schema == nil || d.Upsert == nil || d.TimeArg == nil {
		return nil, fmt.Errorf("%w: Dialect is required (statessql.Postgres(), MySQL(), SQLite() or your own, with every field set)", ErrOptions)
	}
	table := opts.Table
	if table == "" {
		table = DefaultTable
	}
	if !tableName.MatchString(table) {
		return nil, fmt.Errorf("%w: table %q: use letters, digits and underscores", ErrOptions, table)
	}
	clock := opts.Clock
	if clock == nil {
		clock = time.Now
	}
	s := &Store{db: opts.DB, dialect: d, table: table, clock: clock}
	s.load = s.bind("SELECT data FROM " + table + " WHERE handle = ? AND expires > ?")
	s.upsert = s.bind(d.Upsert(table))
	s.remove = s.bind("DELETE FROM " + table + " WHERE handle = ?")
	s.revoke = s.bind("DELETE FROM " + table + " WHERE session = ?")
	s.prune = s.bind("DELETE FROM " + table + " WHERE expires <= ?")
	s.each = s.bind(fmt.Sprintf("SELECT handle, data FROM %s WHERE expires > ? AND handle > ? ORDER BY handle LIMIT %d", table, eachBatch))
	s.expired = s.bind(fmt.Sprintf("SELECT handle, data FROM %s WHERE expires <= ? AND handle > ? ORDER BY handle LIMIT %d", table, eachBatch))
	s.pruneOne = s.bind("DELETE FROM " + table + " WHERE handle = ? AND expires <= ?")
	s.bySession = s.bind(fmt.Sprintf("SELECT handle, data FROM %s WHERE session = ? AND handle > ? ORDER BY handle LIMIT %d", table, eachBatch))
	s.revokeOne = s.bind("DELETE FROM " + table + " WHERE handle = ? AND session = ?")
	return s, nil
}

// bind rewrites each "?" into the dialect's placeholder. The statements
// here hold no literal "?", so every one is a parameter.
func (s *Store) bind(q string) string {
	var b strings.Builder
	n := 0
	for _, r := range q {
		if r == '?' {
			n++
			b.WriteString(s.dialect.Placeholder(n))
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// Migrate creates the table and its indexes when they are missing. The
// host calls it explicitly (at start, or from its own migration tool with
// the statements from Dialect.Schema); the store never changes a schema
// on its own.
func (s *Store) Migrate(ctx context.Context) error {
	for _, stmt := range s.dialect.Schema(s.table) {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("statessql: migrate %s (%s): %w", s.table, s.dialect.Name, err)
		}
	}
	return nil
}

// Load returns the bytes under key unless the row is missing or expired.
// It does not extend the expiry: every action saves the instance again,
// which does.
func (s *Store) Load(ctx context.Context, key string) ([]byte, bool, error) {
	var data []byte
	err := s.db.QueryRowContext(ctx, s.load, key, s.now()).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, s.fail("load", err)
	}
	return data, true, nil
}

// Save inserts or replaces the row, expiring ttl from now.
func (s *Store) Save(ctx context.Context, key string, data []byte, ttl time.Duration, session string) error {
	exp := s.dialect.TimeArg(s.clock().Add(ttl))
	if _, err := s.db.ExecContext(ctx, s.upsert, key, session, data, exp); err != nil {
		return s.fail("save", err)
	}
	return nil
}

// Remove deletes the row; a missing one is not an error.
func (s *Store) Remove(ctx context.Context, key string) error {
	if _, err := s.db.ExecContext(ctx, s.remove, key); err != nil {
		return s.fail("remove", err)
	}
	return nil
}

// RevokeSession deletes every row bound to the hashed session. The empty
// session is the signed-out one, shared by every anonymous visitor, so
// revoking it would end strangers' components: it is a no-op.
//
// With an end watcher it first deletes and reports the session's rows one
// by one, then runs the same bulk delete as without one: a row saved while
// the revoke runs is still deleted, unreported, because revocation must
// never miss a row.
func (s *Store) RevokeSession(ctx context.Context, session string) error {
	if session == "" {
		return nil
	}
	if fn := s.watcher(); fn != nil {
		if _, err := s.deleteEach(ctx, s.bySession, session, s.revokeOne, session, brick.EndRevoked, fn); err != nil {
			return s.fail("revoke session", err)
		}
	}
	if _, err := s.db.ExecContext(ctx, s.revoke, session); err != nil {
		return s.fail("revoke session", err)
	}
	return nil
}

// WatchEndsBytes installs fn (nil removes it). The table sees expiry only
// when Prune runs, so an expired end is reported up to one prune interval
// late; it never reports evicted.
func (s *Store) WatchEndsBytes(fn brick.BytesEndFunc) []brick.EndReason {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ends = fn
	return []brick.EndReason{brick.EndExpired, brick.EndRevoked}
}

func (s *Store) watcher() brick.BytesEndFunc {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ends
}

// deleteEach reads the rows sel matches in handle order, deletes each
// with one (handle, arg), and reports a row only when that delete removed
// it: a row saved again since the read no longer matches, and a second
// server sweeping at the same time removes it first, so every end is
// reported once. Rows are read in batches and never under an open cursor,
// as in EachBytes.
func (s *Store) deleteEach(ctx context.Context, sel string, selArg any, one string, oneArg any, reason brick.EndReason, fn brick.BytesEndFunc) (int, error) {
	type row struct {
		key  string
		data []byte
	}
	removed, after := 0, ""
	for {
		var batch []row
		rows, err := s.db.QueryContext(ctx, sel, selArg, after)
		if err != nil {
			return removed, err
		}
		for rows.Next() {
			var r row
			if err := rows.Scan(&r.key, &r.data); err != nil {
				_ = rows.Close()
				return removed, err
			}
			batch = append(batch, r)
		}
		if err := errors.Join(rows.Err(), rows.Close()); err != nil {
			return removed, err
		}
		for _, r := range batch {
			res, err := s.db.ExecContext(ctx, one, r.key, oneArg)
			if err != nil {
				return removed, err
			}
			if n, err := res.RowsAffected(); err != nil {
				return removed, err
			} else if n == 1 {
				removed++
				fn(ctx, r.key, r.data, reason)
			}
		}
		if len(batch) < eachBatch {
			return removed, nil
		}
		after = batch[len(batch)-1].key
	}
}

// Prune deletes every row expired at now and reports how many went. With
// an end watcher it deletes them one by one (deleteEach) so each is
// reported; without one, a single DELETE.
func (s *Store) Prune(ctx context.Context, now time.Time) (int, error) {
	if fn := s.watcher(); fn != nil {
		at := s.dialect.TimeArg(now)
		n, err := s.deleteEach(ctx, s.expired, at, s.pruneOne, at, brick.EndExpired, fn)
		if err != nil {
			return n, s.fail("prune", err)
		}
		return n, nil
	}
	res, err := s.db.ExecContext(ctx, s.prune, s.dialect.TimeArg(now))
	if err != nil {
		return 0, s.fail("prune", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, s.fail("prune", err)
	}
	return int(n), nil
}

// EachBytes walks every live row in handle order; fn returning false
// stops. Rows saved or removed during the walk may or may not be seen.
func (s *Store) EachBytes(ctx context.Context, fn func(key string, data []byte) bool) error {
	type row struct {
		key  string
		data []byte
	}
	after := ""
	for {
		var batch []row
		rows, err := s.db.QueryContext(ctx, s.each, s.now(), after)
		if err != nil {
			return s.fail("list", err)
		}
		for rows.Next() {
			var r row
			if err := rows.Scan(&r.key, &r.data); err != nil {
				_ = rows.Close()
				return s.fail("list", err)
			}
			batch = append(batch, r)
		}
		if err := errors.Join(rows.Err(), rows.Close()); err != nil {
			return s.fail("list", err)
		}
		for _, r := range batch {
			if !fn(r.key, r.data) {
				return nil
			}
		}
		if len(batch) < eachBatch {
			return nil
		}
		after = batch[len(batch)-1].key
	}
}

func (s *Store) now() any { return s.dialect.TimeArg(s.clock()) }

// fail names the operation and table, keeping the driver error wrapped so
// a host can still inspect it with errors.As.
func (s *Store) fail(op string, err error) error {
	return fmt.Errorf("statessql: %s %s: %w", op, s.table, err)
}
