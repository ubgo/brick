package demodb

import (
	"context"
	"database/sql"
	"fmt"
)

// contactsSchema is the Contacts app's table (content/apps/contacts.html, apps/contacts/contacts.star).
// Its closed sets are documented beside the columns because the Starlark
// actions and the pages both write them.
const contactsSchema = `
create table contacts (
  id integer primary key,
  name text not null,
  email text not null,
  company text not null,
  role text not null,
  status text not null,   -- active | lead | churned
  created text not null   -- ISO date
);
`

// The contact seed's parts; each row picks from them by its index.
var (
	companies = []string{"Acme", "Globex", "Initech", "Umbrella", "Hooli", "Stark", "Wayne", "Tyrell", "Cyberdyne", "Soylent"}
	roles     = []string{"CEO", "CTO", "Engineer", "Designer", "Product Manager", "Sales Lead", "Marketing", "Support"}
	statuses  = []string{contactActive, contactActive, contactLead, contactActive, contactChurned, contactLead}
)

// seedContacts creates and fills the Contacts app's tables. seed() in db.go calls it.
func seedContacts(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, contactsSchema); err != nil {
		return fmt.Errorf("contacts schema: %w", err)
	}
	const contactsN = 64
	for i := range contactsN {
		f, l := firstNames[i%len(firstNames)], lastNames[(i*7)%len(lastNames)]
		c := companies[(i*3)%len(companies)]
		args := []any{f + " " + l, fmt.Sprintf("%s.%s@%s.com", lower(f), lower(asciiOnly(l)), lower(c)), c, roles[(i*5)%len(roles)], statuses[i%len(statuses)], day(i * 2)}
		if _, err := db.ExecContext(ctx, "insert into contacts (name, email, company, role, status, created) values (?, ?, ?, ?, ?, ?)", args...); err != nil {
			return fmt.Errorf("contacts seed: %w", err)
		}
	}
	return nil
}
