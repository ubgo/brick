# Calendar

A Google Calendar-style app built only from kit components — month, week and agenda views over SQLite, a mini month, and an event dialog with dates, times, colours and guests.

Open it at http://localhost:8002/apps/calendar/ (`task ui:serve`).

## Files

| File | What it holds |
|---|---|
| `CalDelete.html` | The event dialog's Delete button and its ConfirmDialog, or nothing for a new event (id 0). |
| `CalendarApp.html` | The Calendar demo as one stateful component — month, week and agenda views of the events table, a side panel with a mini month and the next events, and an event dialog for create, edit and delete (apps/calendar/calendar.star). |
| `calendar.star` | The queries and actions, in Starlark (loaded with `use:` as `~/apps/calendar/calendar.star`). |
| `../../content/apps/calendar.html` | The page: `layout: app`, `AppShell` with the shared `AppNav`, this app's component, and the View source tabs. It stays in `content/` because brick derives URLs from there. |
| `../../demodb/db_calendar.go` | The app's tables and their deterministic seed. It is Go, so it lives in the importable `demodb` package; `seed()` in `demodb/db.go` runs it. |
| `../../apps_calendar_test.go` | The app's tests: they load the page and post to `/_brick/action` the way the runtime does. |

## Test

```sh
task ui:test:app APP=calendar   # only this app's tests
task ui:test                # every showcase test
```
