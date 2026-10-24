# Store

A shop built only from kit components — filters, sort and search held in one stateful component, a cart drawer over SQLite, and a checkout with validation.

Open it at http://localhost:8002/apps/store/ (`task ui:serve`).

## Files

| File | What it holds |
|---|---|
| `StoreCatalog.html` | The whole Store app as one stateful component — the filters and sort (state), the product grid (store.products over the products table), the cart drawer (the cart table) and the checkout steps. |
| `store.star` | The queries and actions, in Starlark (loaded with `use:` as `~/apps/store/store.star`). |
| `../../content/apps/store.html` | The page: `layout: app`, `AppShell` with the shared `AppNav`, this app's component, and the View source tabs. It stays in `content/` because brick derives URLs from there. |
| `../../demodb/db_store.go` | The app's tables and their deterministic seed. It is Go, so it lives in the importable `demodb` package; `seed()` in `demodb/db.go` runs it. |
| `../../apps_store_test.go` | The app's tests: they load the page and post to `/_brick/action` the way the runtime does. |

## Test

```sh
task ui:test:app APP=store   # only this app's tests
task ui:test                # every showcase test
```
