# brick/contrib/states-sql

A `brick.BytesStore` on `database/sql`: live component state in one table of Postgres, MySQL, SQLite, or any database you describe with a `Dialect`. Wrap it with `brick.Bytes`, which brings the TTL, the codec and optional encryption at rest, so this module only moves bytes.

## Use it

```go
db, _ := sql.Open("pgx", dsn)                     // your driver, your pool
st, err := statessql.New(statessql.Options{DB: db, Dialect: statessql.Postgres()})
if err := st.Migrate(ctx); err != nil { … }       // once at start; idempotent
eng, err := brick.New(brick.Options{
    States: brick.Bytes(st, brick.BytesOptions{TTL: 30 * time.Minute}),
})
```

Runnable version: `Example` runs the whole path on in-memory SQLite: `Migrate`, a render for a signed-in session saving one row, `Engine.Instance` reading it back, and `Engine.RevokeSession` ending it. `task test:sql` runs the same store against Postgres. Run it with `go test -run Example .` in this folder ([example_test.go](example_test.go)).

Guide: [state-stores.md](../../docs/guides/production/state-stores.md).

The module imports no driver; the host opens the `*sql.DB`.

| Option | Default | Meaning |
|---|---|---|
| `DB` | required | the host's pool |
| `Dialect` | required | `statessql.Postgres()`, `MySQL()`, `SQLite()`, or your own `Dialect` value |
| `Table` | `brick_states` | letters, digits and underscores only; two apps sharing a database use two tables |
| `Clock` | `time.Now` | what expiries are compared against (tests move it) |

**The table.** `handle` (primary key), `session` (the hashed session id, indexed for revocation), `data`, `expires` (indexed for pruning). `Dialect.Schema(table)` returns the DDL, so a host with its own migration tool can copy it instead of calling `Migrate`. MySQL differs from the neutral DDL on purpose: `MEDIUMBLOB` (a `BLOB` stops at 64 KiB), `DATETIME` (a `TIMESTAMP` is zone-converted, ends in 2038 and can update itself on every row change), and indexes inside `CREATE TABLE` (MySQL has no `CREATE INDEX IF NOT EXISTS`).

**Expiry.** Every save writes `expires = now + TTL`; every read filters on it, so an expired row is never returned. A database keeps the row until something deletes it: the store is a `brick.Pruner`, and `site` calls `Prune` every `site.Options.PruneEvery` (10 minutes by default). A host serving brick without `site` calls `Prune` on its own timer. Reads do not extend the expiry; every action saves the instance again, which does.

**Revocation and listing.** The store is a `brick.Revoker` (`DELETE … WHERE session = ?`, used by `Engine.RevokeSession`; the empty, signed-out session is never revoked, since every anonymous visitor shares it) and a `brick.BytesLister`, which `brick.Bytes` turns into `Lister` for the Dev inspector. Listing reads in batches of 100, so a callback may use the same pool.

**Instance ends (v19).** The store is a `brick.BytesEndWatcher`: it reports expired instances (when `Prune` deletes them) and revoked ones (`RevokeSession`) to `OnInstanceEnd` hooks. With a watcher, `Prune` deletes row by row and reports a row only when its own delete removed it, so two servers pruning one table report each end once; without one it keeps the single bulk `DELETE`. Evictions do not happen in a table, so none are reported.

**Outages.** A failed query is returned as an error, never as "not found": the engine answers 503 (and the runtime retries) for a store error and 410 only for a missing instance, so a database outage does not make every live component recover from scratch. A missing table (a skipped `Migrate`) is an error too.

## Tests

`go test ./...` runs the root's `statetest` conformance suite and this store's own tests against SQLite in a temp file (the pure-Go `modernc.org/sqlite`, a test-only dependency), so it needs no service. `task test:sql` adds Postgres: `BRICK_PG_TEST=1` uses the local Postgres.app socket, creates the database `brick_test` if it is missing, and drops only the tables it creates (`BRICK_PG_DSN` points it elsewhere). MySQL runs with `BRICK_MYSQL_TEST=1` and `BRICK_MYSQL_DSN` naming a database the test may create tables in; otherwise its statements are pinned by a unit test.
