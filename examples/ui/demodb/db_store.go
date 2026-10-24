package demodb

import (
	"context"
	"database/sql"
	"fmt"
)

// storeSchema is the Store app's tables (content/apps/store.html, apps/store/store.star).
// Its closed sets are documented beside the columns because the Starlark
// actions and the pages both write them.
const storeSchema = `
create table products (
  id integer primary key,
  name text not null,
  price integer not null,  -- cents
  category text not null,
  rating real not null,
  stock integer not null
);
create table cart (
  product_id integer primary key references products(id),
  qty integer not null
);
`

// products is the Store's catalog. The Dashboard's seeded orders
// (db_dashboard.go) are for these products too.
var products = []struct {
	name, category string
	price          int
}{{"Linen shirt", "Clothing", 5900}, {"Canvas tote", "Bags", 2400}, {"Ceramic mug", "Home", 1800}, {"Wool beanie", "Clothing", 2900}, {"Desk lamp", "Home", 7900}, {"Leather wallet", "Bags", 4900}, {"Notebook set", "Stationery", 1500}, {"Fountain pen", "Stationery", 6500}}

// seedStore creates and fills the Store app's tables. seed() in db.go calls it.
func seedStore(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, storeSchema); err != nil {
		return fmt.Errorf("store schema: %w", err)
	}
	for i := range len(products) {
		p := products[i]
		args := []any{p.name, p.price, p.category, 3.5 + float64(i%4)*0.5, (i * 7) % 30}
		if _, err := db.ExecContext(ctx, "insert into products (name, price, category, rating, stock) values (?, ?, ?, ?, ?)", args...); err != nil {
			return fmt.Errorf("store seed: %w", err)
		}
	}
	return nil
}
