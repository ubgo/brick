package statessql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ubgo/brick"
	"github.com/ubgo/brick/statetest"
	_ "modernc.org/sqlite"
)

// ttl is the expiry every test saves with.
const ttl = 30 * time.Minute

// fakeClock is a settable clock, so a test moves time past a TTL without
// sleeping.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func newClock() *fakeClock {
	return &fakeClock{t: time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// tableSeq makes a fresh table per store, so each conformance subtest
// starts empty on a database the backends share.
var tableSeq atomic.Int64

// backend opens a store on a fresh, migrated table of one database.
type backend func(t *testing.T, clock func() time.Time) *Store

// openSQLite is the backend every run uses: a temp-file database through
// the pure-Go driver, so the default gate needs no service. One connection
// serializes writers, as SQLite does anyway, without SQLITE_BUSY errors.
func openSQLite(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "states.db"))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// storeOn returns a backend that makes a fresh table in db for each store
// and drops it when the test ends.
func storeOn(db *sql.DB, d Dialect) backend {
	return func(t *testing.T, clock func() time.Time) *Store {
		t.Helper()
		table := fmt.Sprintf("brick_states_t%d", tableSeq.Add(1))
		s, err := New(Options{DB: db, Dialect: d, Table: table, Clock: clock})
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Migrate(context.Background()); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _, _ = db.Exec("DROP TABLE IF EXISTS " + table) })
		return s
	}
}

func TestSQLite(t *testing.T) {
	runSuite(t, storeOn(openSQLite(t), SQLite()))
}

// runSuite is everything a backend must pass: the root's conformance
// suite through brick.Bytes, plus this store's own guarantees.
func runSuite(t *testing.T, open backend) {
	t.Run("Conformance", func(t *testing.T) {
		statetest.Store(t, func(t *testing.T) brick.StateStore {
			return brick.Bytes(open(t, nil), brick.BytesOptions{TTL: ttl})
		}, statetest.StoreOptions{})
	})
	t.Run("ExpiredIsNeverReturned", func(t *testing.T) { testExpired(t, open) })
	t.Run("RevokeSessionDeletesExactlyThatSession", func(t *testing.T) { testRevoke(t, open) })
	t.Run("Prune", func(t *testing.T) { testPrune(t, open) })
	t.Run("Ends", func(t *testing.T) { testEnds(t, open) })
	t.Run("EachPagesPastOneBatch", func(t *testing.T) { testEachBatches(t, open) })
	t.Run("MigrateTwice", func(t *testing.T) {
		if err := open(t, nil).Migrate(context.Background()); err != nil {
			t.Fatalf("Migrate must be idempotent: %v", err)
		}
	})
	t.Run("OutageIsAnErrorNotNotFound", func(t *testing.T) { testOutage(t, open) })
}

func testExpired(t *testing.T, open backend) {
	ctx := context.Background()
	clock := newClock()
	s := open(t, clock.Now)
	if err := s.Save(ctx, "k1", []byte("v"), time.Minute, ""); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := s.Load(ctx, "k1"); !ok || err != nil {
		t.Fatalf("fresh row: %v %v", ok, err)
	}
	clock.Add(time.Minute) // exactly at expiry: gone
	if data, ok, err := s.Load(ctx, "k1"); ok || err != nil || data != nil {
		t.Fatalf("an expired row must never be returned: %q %v %v", data, ok, err)
	}
	n := 0
	if err := s.EachBytes(ctx, func(string, []byte) bool { n++; return true }); err != nil || n != 0 {
		t.Fatalf("EachBytes must skip expired rows: saw %d, %v", n, err)
	}
	// A later save renews it.
	if err := s.Save(ctx, "k1", []byte("v2"), time.Minute, ""); err != nil {
		t.Fatal(err)
	}
	if data, ok, _ := s.Load(ctx, "k1"); !ok || string(data) != "v2" {
		t.Fatalf("a save must renew the expiry: %q %v", data, ok)
	}
}

func testRevoke(t *testing.T, open backend) {
	ctx := context.Background()
	s := open(t, nil)
	rows := map[string]string{"a1": "sessA", "a2": "sessA", "b1": "sessB", "anon": ""}
	for k, sess := range rows {
		if err := s.Save(ctx, k, []byte(k), ttl, sess); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.RevokeSession(ctx, "sessA"); err != nil {
		t.Fatal(err)
	}
	if err := s.RevokeSession(ctx, ""); err != nil {
		t.Fatal(err)
	}
	for k, sess := range rows {
		_, ok, err := s.Load(ctx, k)
		if err != nil {
			t.Fatal(err)
		}
		if want := sess != "sessA"; ok != want {
			t.Errorf("row %s (session %q): present=%v, want %v", k, sess, ok, want)
		}
	}
}

func testPrune(t *testing.T, open backend) {
	ctx := context.Background()
	clock := newClock()
	s := open(t, clock.Now)
	_ = s.Save(ctx, "short", []byte("x"), time.Minute, "")
	_ = s.Save(ctx, "long", []byte("x"), time.Hour, "")
	n, err := s.Prune(ctx, clock.Now())
	if err != nil || n != 0 {
		t.Fatalf("nothing expired yet: %d %v", n, err)
	}
	clock.Add(2 * time.Minute)
	if n, err = s.Prune(ctx, clock.Now()); err != nil || n != 1 {
		t.Fatalf("Prune removed %d (want 1): %v", n, err)
	}
	if _, ok, _ := s.Load(ctx, "long"); !ok {
		t.Fatal("Prune must keep a live row")
	}
	// Through brick.Bytes, as the site layer calls it.
	clock.Add(2 * time.Hour)
	if n, err = brick.Bytes(s, brick.BytesOptions{}).Prune(ctx, clock.Now()); err != nil || n != 1 {
		t.Fatalf("Bytes.Prune removed %d (want 1): %v", n, err)
	}
}

func testEachBatches(t *testing.T, open backend) {
	ctx := context.Background()
	s := open(t, nil)
	const total = eachBatch*2 + 7
	for i := range total {
		if err := s.Save(ctx, fmt.Sprintf("k%04d", i), []byte{byte(i)}, ttl, ""); err != nil {
			t.Fatal(err)
		}
	}
	seen := map[string]bool{}
	err := s.EachBytes(ctx, func(k string, _ []byte) bool {
		// Using the store inside the callback must not deadlock, even on a
		// pool of one connection.
		if _, _, err := s.Load(ctx, k); err != nil {
			t.Error(err)
		}
		seen[k] = true
		return true
	})
	if err != nil || len(seen) != total {
		t.Fatalf("EachBytes saw %d of %d: %v", len(seen), total, err)
	}
}

// testOutage closes the pool under a live store: every operation must
// fail with an error, and brick.Bytes must pass it on as an error (the
// engine's 503), never as "not found" (its 410).
func testOutage(t *testing.T, open backend) {
	ctx := context.Background()
	s := open(t, nil)
	if err := s.Save(ctx, "k", []byte("v"), ttl, "sess"); err != nil {
		t.Fatal(err)
	}
	down, err := New(Options{DB: closedDB(t), Dialect: s.dialect, Table: s.table})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := down.Load(ctx, "k"); err == nil || ok {
		t.Fatalf("Load during an outage: ok=%v err=%v; want an error", ok, err)
	}
	inst, ok, err := brick.Bytes(down, brick.BytesOptions{}).Get(ctx, "k")
	if err == nil || ok || inst != nil {
		t.Fatalf("Bytes.Get during an outage: %v %v %v; want an error, so the engine answers 503", inst, ok, err)
	}
	checks := map[string]error{
		"Save":          down.Save(ctx, "k", []byte("v"), ttl, ""),
		"Remove":        down.Remove(ctx, "k"),
		"RevokeSession": down.RevokeSession(ctx, "sess"),
		"EachBytes":     down.EachBytes(ctx, func(string, []byte) bool { return true }),
	}
	_, checks["Prune"] = down.Prune(ctx, time.Now())
	for name, err := range checks {
		if err == nil {
			t.Errorf("%s during an outage must report an error", name)
		}
	}
	// A missing table (a skipped Migrate) is a store failure too, not an
	// empty store.
	nomig, _ := New(Options{DB: s.db, Dialect: s.dialect, Table: "brick_states_never_migrated"})
	if _, _, err := nomig.Load(ctx, "k"); err == nil {
		t.Error("Load from a missing table must report an error")
	}
}

// closedDB is a pool that fails every query, standing in for a database
// that is down.
func closedDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "down.db"))
	if err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	return db
}

func TestNewRefusesBadOptions(t *testing.T) {
	db := openSQLite(t)
	for name, opts := range map[string]Options{
		"no DB":      {Dialect: SQLite()},
		"no dialect": {DB: db},
		"bad table":  {DB: db, Dialect: SQLite(), Table: "x; DROP TABLE y"},
		"dotted":     {DB: db, Dialect: SQLite(), Table: "a.b"},
	} {
		if _, err := New(opts); !errors.Is(err, ErrOptions) {
			t.Errorf("%s: %v, want ErrOptions", name, err)
		}
	}
	s, err := New(Options{DB: db, Dialect: SQLite()})
	if err != nil || s.table != DefaultTable {
		t.Fatalf("default table: %v %v", s, err)
	}
}

// TestDialects pins the statements each dialect sends, including MySQL's,
// which runs against a server only when BRICK_MYSQL_TEST=1.
func TestDialects(t *testing.T) {
	db := openSQLite(t)
	pg, _ := New(Options{DB: db, Dialect: Postgres(), Table: "st"})
	if want := "SELECT data FROM st WHERE handle = $1 AND expires > $2"; pg.load != want {
		t.Errorf("postgres load:\n got %s\nwant %s", pg.load, want)
	}
	if !strings.Contains(pg.upsert, "VALUES ($1, $2, $3, $4) ON CONFLICT (handle)") {
		t.Errorf("postgres upsert: %s", pg.upsert)
	}
	if !strings.Contains(pg.each, "expires > $1 AND handle > $2") {
		t.Errorf("postgres each: %s", pg.each)
	}

	my, _ := New(Options{DB: db, Dialect: MySQL(), Table: "st"})
	if !strings.Contains(my.upsert, "VALUES (?, ?, ?, ?) ON DUPLICATE KEY UPDATE session = VALUES(session)") {
		t.Errorf("mysql upsert: %s", my.upsert)
	}
	ddl := MySQL().Schema("st")
	if len(ddl) != 1 {
		t.Fatalf("mysql has no CREATE INDEX IF NOT EXISTS, so its indexes live in CREATE TABLE: %q", ddl)
	}
	for _, want := range []string{"CREATE TABLE IF NOT EXISTS st", "handle   VARCHAR(64) PRIMARY KEY", "MEDIUMBLOB NOT NULL", "DATETIME NOT NULL", "INDEX st_session (session)", "INDEX st_expires (expires)"} {
		if !strings.Contains(ddl[0], want) {
			t.Errorf("mysql DDL lacks %q:\n%s", want, ddl[0])
		}
	}
	if strings.Contains(ddl[0], "TIMESTAMP") {
		t.Errorf("mysql DDL must not use TIMESTAMP (zone conversion, 2038, auto-update):\n%s", ddl[0])
	}

	pgDDL := strings.Join(Postgres().Schema("st"), "\n")
	for _, want := range []string{"BYTEA NOT NULL", "TIMESTAMP NOT NULL", "CREATE INDEX IF NOT EXISTS st_session ON st (session)", "CREATE INDEX IF NOT EXISTS st_expires ON st (expires)"} {
		if !strings.Contains(pgDDL, want) {
			t.Errorf("postgres DDL lacks %q:\n%s", want, pgDDL)
		}
	}

	// SQLite compares the bound expiry as text, so the format must sort
	// as time does, across a fractional-second boundary too.
	a := SQLite().TimeArg(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)).(string)
	b := SQLite().TimeArg(time.Date(2026, 1, 2, 3, 4, 5, 5e8, time.FixedZone("x", 3600))).(string)
	c := SQLite().TimeArg(time.Date(2026, 1, 2, 3, 4, 5, 5e8, time.UTC)).(string)
	if len(a) != len(c) || a >= c || b >= a {
		t.Errorf("sqlite times must be fixed-width UTC and sort by instant: %q %q %q", a, b, c)
	}
}
