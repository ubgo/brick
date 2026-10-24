# Task board

A kanban board over the tasks table — drag cards between columns, or use each card's menu; add, edit, delete and filter tasks, or see them as a table.

Open it at http://localhost:8002/apps/tasks/ (`task ui:serve`).

## Files

| File | What it holds |
|---|---|
| `TaskBoard.html` | The Task board app — a kanban board over the tasks table, and the same tasks as a table. |
| `tasks.star` | The queries and actions, in Starlark (loaded with `use:` as `~/apps/tasks/tasks.star`). |
| `../../content/apps/tasks.html` | The page: `layout: app`, `AppShell` with the shared `AppNav`, this app's component, and the View source tabs. It stays in `content/` because brick derives URLs from there. |
| `../../demodb/db_tasks.go` | The app's tables and their deterministic seed. It is Go, so it lives in the importable `demodb` package; `seed()` in `demodb/db.go` runs it. |
| `../../apps_tasks_test.go` | The app's tests: they load the page and post to `/_brick/action` the way the runtime does. |

## Test

```sh
task ui:test:app APP=tasks   # only this app's tests
task ui:test                # every showcase test
```
