# Contacts

A CRUD app over the contacts table — search, filter, sort, pages, selection with bulk actions, a validated create/edit dialog, confirmations and a detail drawer, built only from kit components and Starlark actions. Messages are `brick.flash` effects shown by the kit `Toaster`, and every element id is built from the instance id (`$.ID`), so two copies can share a page.

Open it at http://localhost:8002/apps/contacts/ (`task ui:serve`).

## Files

| File | What it holds |
|---|---|
| `ContactsApp.html` | The Contacts CRUD app — a searchable, filterable, sortable, paged table over the contacts table, with row selection and bulk actions, a create/edit dialog validated on the server, delete confirmations and a detail drawer. |
| `ContactCount.html` | The "64 contacts" count in the page's top bar. It listens to `contact-added` and `contact-deleted` (`brick: {on:}`), which `contacts.star` dispatches, and runs `$refresh` on each, so it stays current while neither component names the other. |
| `contacts.star` | The queries and actions, in Starlark (loaded with `use:` as `~/apps/contacts/contacts.star`). |
| `../../content/apps/contacts.html` | The page: `layout: app`, `AppShell` with the shared `AppNav`, this app's component, and the View source tabs. It stays in `content/` because brick derives URLs from there. |
| `../../demodb/db_contacts.go` | The app's tables and their deterministic seed. It is Go, so it lives in the importable `demodb` package; `seed()` in `demodb/db.go` runs it. |
| `../../apps_contacts_test.go` | The app's tests: they load the page and post to `/_brick/action` the way the runtime does. |

## Test

```sh
task ui:test:app APP=contacts   # only this app's tests
task ui:test                # every showcase test
```
