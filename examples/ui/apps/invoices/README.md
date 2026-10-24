# Invoices

A CRUD app over SQLite — list, filter, search and page invoices, edit line items with server-computed totals, and print a clean invoice.

Open it at http://localhost:8002/apps/invoices/ (`task ui:serve`).

## Files

| File | What it holds |
|---|---|
| `InvoicesApp.html` | The Invoices app — one stateful component with three views: the list (status tabs with counts, search, sort, pages, totals), the editor (create and edit, with a line-items table whose totals the server recomputes on every change) and the document (a clean printable invoice with its actions). |
| `invoices.star` | The queries and actions, in Starlark (loaded with `use:` as `~/apps/invoices/invoices.star`). |
| `../../content/apps/invoices.html` | The page: `layout: app`, `AppShell` with the shared `AppNav`, this app's component, and the View source tabs. It stays in `content/` because brick derives URLs from there. |
| `../../demodb/db_invoices.go` | The app's tables and their deterministic seed. It is Go, so it lives in the importable `demodb` package; `seed()` in `demodb/db.go` runs it. |
| `../../apps_invoices_test.go` | The app's tests: they load the page and post to `/_brick/action` the way the runtime does. |

## Test

```sh
task ui:test:app APP=invoices   # only this app's tests
task ui:test                # every showcase test
```
