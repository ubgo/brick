# Dashboard

Revenue, orders and activity from the store's database — KPIs with deltas, charts, a paged orders table and a date range that re-queries through an action.

Open it at http://localhost:8002/apps/dashboard/ (`task ui:serve`).

## Files

| File | What it holds |
|---|---|
| `DashBoard.html` | The Dashboard app's stateful core. |
| `DashPanels.html` | Everything the Dashboard shows for one range and orders page — KPI cards with deltas against the previous period of the same length, the revenue line, orders by status, top products, recent orders and activity. |
| `dashboard.star` | The queries and actions, in Starlark (loaded with `use:` as `~/apps/dashboard/dashboard.star`). |
| `../../content/apps/dashboard.html` | The page: `layout: app`, `AppShell` with the shared `AppNav`, this app's component, and the View source tabs. It stays in `content/` because brick derives URLs from there. |
| `../../demodb/db_dashboard.go` | The app's tables and their deterministic seed. It is Go, so it lives in the importable `demodb` package; `seed()` in `demodb/db.go` runs it. |
| `../../apps_dashboard_test.go` | The app's tests: they load the page and post to `/_brick/action` the way the runtime does. |

## Test

```sh
task ui:test:app APP=dashboard   # only this app's tests
task ui:test                # every showcase test
```
