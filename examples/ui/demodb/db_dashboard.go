package demodb

import (
	"context"
	"database/sql"
	"fmt"
)

// dashboardSchema is the Dashboard app's table (content/apps/dashboard.html, apps/dashboard/dashboard.star).
// Its closed sets are documented beside the columns because the Starlark
// actions and the pages both write them.
const dashboardSchema = `
create table orders (
  id integer primary key,
  customer text not null,
  product text not null,
  amount integer not null, -- cents
  status text not null,    -- paid | pending | refunded
  placed text not null     -- ISO date
);
`

// seedDashboard creates and fills the Dashboard app's tables. seed() in db.go calls it.
func seedDashboard(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, dashboardSchema); err != nil {
		return fmt.Errorf("dashboard schema: %w", err)
	}
	const ordersN = 120
	for i := range ordersN {
		p := products[(i*3)%len(products)] // the Store's catalog (db_store.go)
		args := []any{firstNames[(i*7)%len(firstNames)] + " " + lastNames[(i*5)%len(lastNames)], p.name, p.price * (1 + i%3), []string{orderPaid, orderPaid, orderPaid, orderPending, orderRefunded}[i%5], day(150 + i)}
		if _, err := db.ExecContext(ctx, "insert into orders (customer, product, amount, status, placed) values (?, ?, ?, ?, ?)", args...); err != nil {
			return fmt.Errorf("dashboard seed: %w", err)
		}
	}
	return nil
}
