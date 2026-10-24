package statessql_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"testing/fstest"
	"time"

	"github.com/ubgo/brick"
	statessql "github.com/ubgo/brick/contrib/states-sql"
	_ "modernc.org/sqlite"
)

// stateTTL is how long an instance lives after its last save.
const stateTTL = 30 * time.Minute

// stateAttr finds the handle a stateful component carries in the page.
var stateAttr = regexp.MustCompile(`data-brick-state="([^"]+)"`)

// counterTheme is a page with one stateful component.
var counterTheme = fstest.MapFS{
	"components/Counter.html": {Data: []byte("---\nstate:\n  n: 0\n---\n<p>{{ .State.n }}</p>\n")},
	"content/index.html":      {Data: []byte("<Counter />\n")},
}

// The host opens the *sql.DB with its own driver (pgx, mysql, sqlite), runs
// Migrate once at start, and wraps the store with brick.Bytes. Here SQLite
// in memory stands in for Postgres; `task test:sql` runs the store against
// a real Postgres. Sign-out ends every instance of a session at once.
func Example() {
	ctx := context.Background()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		fmt.Println(err)
		return
	}
	defer db.Close()
	db.SetMaxOpenConns(1) // one connection, so every query sees the one in-memory database

	st, err := statessql.New(statessql.Options{DB: db, Dialect: statessql.SQLite()})
	if err != nil {
		fmt.Println(err)
		return
	}
	if err := st.Migrate(ctx); err != nil { // idempotent
		fmt.Println(err)
		return
	}
	eng, err := brick.New(brick.Options{FS: counterTheme, States: brick.Bytes(st, brick.BytesOptions{TTL: stateTTL})})
	if err != nil {
		fmt.Println(err)
		return
	}

	// A render for a signed-in user saves one row bound to that session.
	html, err := eng.Render(ctx, "content/index.html", nil, brick.WithSession("user-42"))
	if err != nil {
		fmt.Println(err)
		return
	}
	m := stateAttr.FindSubmatch(html)
	if m == nil {
		fmt.Println("no stateful component")
		return
	}
	handle := string(m[1])
	var rows int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM "+statessql.DefaultTable).Scan(&rows); err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println("rows:", rows)
	view, err := eng.Instance(ctx, handle, brick.WithSession("user-42"))
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(view.Component, view.State["n"])

	// Sign-out: one DELETE … WHERE session = ? ends the session's instances.
	if err := eng.RevokeSession(ctx, "user-42"); err != nil {
		fmt.Println(err)
		return
	}
	_, err = eng.Instance(ctx, handle, brick.WithSession("user-42"))
	fmt.Println("after sign-out, gone:", errors.Is(err, brick.ErrGone))
	// Output:
	// rows: 1
	// Counter 0
	// after sign-out, gone: true
}
