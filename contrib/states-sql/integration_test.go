package statessql

import (
	"database/sql"
	"os"
	"testing"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// The integration tests run against real servers only when asked (task
// test:sql), so the default gate needs no service. Each works in its own
// database, creates one table per subtest and drops only those tables.
const (
	pgEnv    = "BRICK_PG_TEST"
	pgDSNEnv = "BRICK_PG_DSN"
	// pgAdminDSN reaches the local Postgres.app over its default socket to
	// create the test database when it is missing.
	pgAdminDSN   = "host=/tmp dbname=postgres"
	pgDatabase   = "brick_test"
	pgDefaultDSN = "host=/tmp dbname=" + pgDatabase

	mysqlEnv    = "BRICK_MYSQL_TEST"
	mysqlDSNEnv = "BRICK_MYSQL_DSN"
)

func TestIntegrationPostgres(t *testing.T) {
	if os.Getenv(pgEnv) != "1" {
		t.Skipf("set %s=1 to run against Postgres (database %s, or %s)", pgEnv, pgDatabase, pgDSNEnv)
	}
	dsn := os.Getenv(pgDSNEnv)
	if dsn == "" {
		ensurePGDatabase(t)
		dsn = pgDefaultDSN
	}
	runSuite(t, storeOn(openDB(t, "pgx", dsn), Postgres()))
}

// ensurePGDatabase creates brick_test unless it exists; it never touches
// any other database.
func ensurePGDatabase(t *testing.T) {
	t.Helper()
	admin := openDB(t, "pgx", pgAdminDSN)
	var exists bool
	if err := admin.QueryRow("SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)", pgDatabase).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if !exists {
		if _, err := admin.Exec("CREATE DATABASE " + pgDatabase); err != nil {
			t.Fatal(err)
		}
	}
}

// TestIntegrationMySQL needs a DSN naming a database the test may create
// tables in, with parseTime unnecessary (no time is ever scanned).
func TestIntegrationMySQL(t *testing.T) {
	dsn := os.Getenv(mysqlDSNEnv)
	if os.Getenv(mysqlEnv) != "1" || dsn == "" {
		t.Skipf("set %s=1 and %s (for example user:pass@tcp(127.0.0.1:3306)/brick_test) to run against MySQL", mysqlEnv, mysqlDSNEnv)
	}
	runSuite(t, storeOn(openDB(t, "mysql", dsn), MySQL()))
}

// maxTestConns bounds each test database's pool; see openDB.
const maxTestConns = 8

func openDB(t *testing.T, driver, dsn string) *sql.DB {
	t.Helper()
	db, err := sql.Open(driver, dsn)
	if err != nil {
		t.Fatal(err)
	}
	// The conformance suite runs 64 goroutines at once; without a cap each
	// opens its own connection, and a shared development server that other
	// projects already hold most connections of refuses the rest ("too many
	// clients"). A small pool still interleaves every goroutine's queries.
	db.SetMaxOpenConns(maxTestConns)
	if err := db.Ping(); err != nil {
		t.Fatalf("%s: %v", driver, err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}
