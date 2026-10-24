package demodb

import (
	"context"
	"database/sql"
	"fmt"
)

// invoicesSchema is the Invoices app's tables (content/apps/invoices.html,
// apps/invoices/invoices.star). Its closed sets are documented beside the
// columns because the Starlark actions and the pages both write them.
const invoicesSchema = `
create table invoices (
  id integer primary key,
  number text not null unique, -- INV-0001 style, never reused
  customer text not null,      -- a contact's name when created
  email text not null,
  issued text not null,        -- ISO date
  due text not null,           -- ISO date, never before issued
  status text not null,        -- draft | sent | paid | overdue
  notes text not null,
  tax_rate real not null       -- percent of the subtotal, e.g. 10 or 7.5
);
create table invoice_lines (
  id integer primary key,
  invoice_id integer not null,  -- invoices.id; apps/invoices/invoices.star deletes lines with their invoice (SQLite foreign keys are off)
  description text not null,
  qty integer not null,
  unit_price integer not null  -- cents
);
`

// invoiceServices are the seeded invoice lines' catalog: description and
// unit price in cents.
var invoiceServices = []struct {
	desc  string
	cents int
}{{"Design sprint (5 days)", 120000}, {"Website hosting, monthly", 4900}, {"Logo and brand kit", 45000}, {"Support hours", 9500}, {"Copywriting, per page", 30000}, {"SEO audit", 65000}, {"Stock photo licence", 1250}}

// seedInvoices writes invoicesN invoices for seeded contacts, each with one
// to four lines. The status cycles through invoiceStatuses, and a sent
// invoice whose due date is before invoicesToday is overdue, as the app's
// own rule would make it. It reads the seeded contacts, so seed() runs it
// after seedContacts.
func seedInvoices(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, invoicesSchema); err != nil {
		return fmt.Errorf("invoices schema: %w", err)
	}
	const invoicesN = 24
	const invoicesToday = "2026-09-27" // apps/invoices/invoices.star TODAY
	invoiceStatuses := []string{invoicePaid, invoiceSent, invoicePaid, invoiceDraft, invoicePaid, invoiceSent, invoicePaid, invoiceSent}
	taxRates := []float64{10, 0, 7.5, 20}
	notes := []string{"Thank you for your business.", "Payment by bank transfer within 30 days, please.", ""}
	for i := range invoicesN {
		var name, email string
		if err := db.QueryRowContext(ctx, "select name, email from contacts where id = ?", (i*7)%64+1).Scan(&name, &email); err != nil {
			return fmt.Errorf("seed invoices: %w", err)
		}
		issued, due := day(172+i*4), day(202+i*4)
		status := invoiceStatuses[i%len(invoiceStatuses)]
		if status == invoiceSent && due < invoicesToday {
			status = invoiceOverdue
		}
		res, err := db.ExecContext(ctx, "insert into invoices (number, customer, email, issued, due, status, notes, tax_rate) values (?, ?, ?, ?, ?, ?, ?, ?)",
			fmt.Sprintf("INV-%04d", 1001+i), name, email, issued, due, status, notes[i%len(notes)], taxRates[i%len(taxRates)])
		if err != nil {
			return fmt.Errorf("seed invoices: %w", err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			return fmt.Errorf("seed invoices: %w", err)
		}
		for j := range 1 + i%4 {
			svc := invoiceServices[(i*3+j)%len(invoiceServices)]
			if _, err := db.ExecContext(ctx, "insert into invoice_lines (invoice_id, description, qty, unit_price) values (?, ?, ?, ?)", id, svc.desc, 1+(i+j)%3, svc.cents); err != nil {
				return fmt.Errorf("seed invoice lines: %w", err)
			}
		}
	}
	return nil
}
