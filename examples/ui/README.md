# brick kit showcase

This is the showcase for the brick kit (`../../kit`): a site with one documentation page for every kit component, and demo apps built only from kit components, Starlark actions and an in-memory SQLite database. It is an ordinary brick site (`site.New`), so it also shows how a real project is put together: component roots, a component library, layouts, data files, scripts, stateful components and a Go `main` that grants scripts a database.

The kit arrives as a [component library](../../README.md#component-libraries), the way any project would use it: `main.go` offers the kit (`kit.Library()` from `github.com/ubgo/brick/kit`, or the same library read from `../../kit` on disk under `-serve`) to `brick.yaml`, which lists `libraries: [brick-kit]`. Every kit tag is then available by name on every page, the kit's design tokens are linked through its manifest's `global: [Theme]`, and nothing is copied into this folder.

## Running it

| Command | What it does |
|---|---|
| `task ui:serve` | Serves the site with live reload on http://localhost:8002 (`go run ./ui -serve :8002`). In this mode the kit is read from the repository's `kit/` on disk rather than the embedded copy, and the watcher follows it, so a kit edit reloads the page with no restart. |
| `task ui:test` | Runs the kit's lint and jsdom tests in the root module (`TestKit…`) and the embedded library's tests (`./kit/`), then every showcase test in this package (`go test ./ui/`). |
| `task ui:e2e` | Builds the showcase, serves it on a free port and drives it in a real headless Chrome (`scripts/e2e_ui.sh`, `runtime/test/e2e_ui_chrome.mjs`). Needs Go, node and Chrome. |
| `task ui:test:app APP=contacts` | Runs one demo app's tests only (`go test ./ui/ -run '^TestContacts'`); `APP` is the folder name under `apps/`. |
| `task ui:check` | Reports every problem the site has (`go run ./ui -check`). |
| `task kit:catalog` | Regenerates the component catalog in `kit/README.md` from the pages here. |

`main.go` takes `-serve ADDR`, `-out DIR` (writes the static site; the apps become read-only because actions need a server) and `-check`. Run from `examples/`, since `-site` defaults to `ui`.

## Layout

| Path | Holds |
|---|---|
| `brick.yaml` | Engine configuration: the component roots `components`, `apps` and `demos`, `libraries: [brick-kit]` (the kit, which `main.go` offers as `kit.Library()`), `navigation: true`, and `host:`, the script globals and upload sink `main.go` supplies, so `brick check` and `brick lsp` can compile the site without it. |
| `site.yaml` | Site values (`.Site.title`). |
| `layouts/` | `default.html` (the docs chrome: `{{ assets }}`, `<DocsShell>`; the kit's Theme tokens arrive through its `global:` list, so no `<Theme />`) and `app.html` (a bare page for the demo apps). Both apply a saved theme and accent before first paint. |
| `components/` | The docs chrome (`DocsShell`, `Example`, `Source`, `PropsTable`) and `AppNav`, the menu every demo app shares. |
| `apps/<app>/` | One folder per demo app: its components (`ContactsApp.html` …), its Starlark module (`contacts.star`) and a `README.md` naming the app's other files. `apps` is a component root, so the tags resolve by name. |
| `demos/` | One component per documented example, such as `ButtonVariants.html`. A doc page renders it and shows its source from the same file. The demo data modules `demo_data.star` and `demo_charts.star` live here too. |
| `content/index.html`, `content/theming.html` | The introduction and theming pages. |
| `content/components/` | One page per kit component: `kebab-name.html`, for example `date-picker.html`. |
| `content/apps/` | One page per demo app (`layout: app`): the layout, the app's component and its View source tabs. Pages stay here because brick derives URLs from `content/`. |
| `data/nav.yaml` | The sidebar's categories, in order, and the apps list that `AppNav` renders. |
| `data/*.yaml` | Fixed data for pages and components, such as `settings.yaml`. |
| `static/demo/` | Images the demos use. |
| `main.go`, `demodb/` | The program: the site (with the kit offered as `kit.Library()`), the Starlark scripting backend and the seeded database. The database is the importable package `demodb` (`demodb.Open`), so other programs that render these pages, such as the formatter's render comparison in `cmd/brick`, open the same rows. `demodb/db.go` opens the database and runs `seedApps`; each app with tables creates and seeds them in its own `demodb/db_<app>.go` (`db_contacts.go`, `db_invoices.go` …). |
| `*_test.go` | The showcase tests (see below). |

## Adding a component page

1. Write the component in the repository's `kit/components/` (read `kit/README.md` → *Authoring rules* first). The site uses `kit/` directly as the embedded library, so there is nothing to copy.
2. Write each example as its own component in `demos/`, named `<Component><Aspect>.html` (for example `SliderBasic.html`). A demo is plain markup that uses the component; it needs no frontmatter.
3. Add `content/components/<kebab-name>.html`, shaped like `button.html`:
   - Frontmatter: `title`, `category` (one of the categories in `data/nav.yaml`), `description`, and `api:`, a list of `{name, type, default, desc}` rows.
   - Body: an `<h1>`, then `<p class="lead">{{ .Params.description }}</p>`.
   - Then one `<Example title="…">` per demo: `<Example title="Basics" block><SliderBasic /><brick:prop name="code"><Source src="~/demos/SliderBasic.html" /></brick:prop></Example>`. `block` lays the preview out as a block instead of a wrapping row.
   - Finally `<h2>Props</h2><PropsTable :rows=".Params.api" />`.
4. Assert the page's key HTML in a showcase test, then run `task kit:catalog` so the kit's README lists it.

`TestEveryComponentHasAPage` fails for a kit component without a page (or one whose category is not in the nav). `TestEveryPageRenders` fails on any render error or engine warning, and on a stateless page that renders different bytes twice.

## The demo apps

Every app page uses `<AppShell>` with `<AppNav current="…" />` as its sidebar, so the apps link to each other. The app's screen is one stateful component (or a few) in `apps/<app>/`, with actions written in Starlark beside it. Each folder's `README.md` lists every file of that app, including the page, the Go seed and the test file that live elsewhere.

| App | Page | What it exercises |
|---|---|---|
| Contacts | `/apps/contacts/` | CRUD over `contacts`: search (`brick:model.debounce`), filters, sort, `Pagination`, row selection with bulk actions, a validated create/edit `Dialog`, `ConfirmDialog`, a detail `Drawer` and toasts. |
| Task board | `/apps/tasks/` | A kanban board over `tasks`, with a `load:` loader. Drag and drop goes through `brick.action` from a client module; each card also has a keyboard "Move to…" menu. There is a table view (`Tabs`) and an add/edit `Dialog`. |
| Dashboard | `/apps/dashboard/` | KPIs, `LineChart`, `BarChart` and `DonutChart` over `orders`. A date range and a paged orders table re-query through actions called with `brick.action`; the numbers come from a `load:` loader (`DashPanels`). |
| Inbox | `/apps/inbox/` | A three-pane mail client over `messages`: folders, search, a reading pane, a compose `Dialog`, `ConfirmDialog` and toasts. |
| Store | `/apps/store/` | A catalog over `products`, with filters, sort and search held in state (`brick:model`) and a cart `Drawer` over `cart`. Checkout is validated with `[state, errors]`. |
| Invoices | `/apps/invoices/` | CRUD over `invoices` and `invoice_lines`: status `Tabs` with counts, stat cards, search and sort, and an editor whose line totals the server recomputes on every change (`brick:change` actions). It uses `Combobox` and `DatePicker`, validates on save, and has a printable document view (`@media print`). |
| Chat | `/apps/chat/` | Channels and direct messages over the chat tables (`demodb/db_chat.go`), with unread counts, a composer, a details `Drawer` and `brick:poll` for new messages. |
| Settings | `/apps/settings/` | Five stateful forms in `Tabs` (profile, account, notifications, appearance, billing), each validating with `[state, errors]` and confirming with a `Toast`. Appearance applies the theme in the browser as you choose it. |
| Sign in | `/apps/auth/` | A stateful sign-in form (`AuthSignInForm`) with `PasswordInput` and validation. |
| Landing page | `/apps/landing/` | A marketing page built from `Hero` and the layout components; no state. |

Each app has a test file, `apps_<name>_test.go`, that loads its page and posts to `/_brick/action` the way the runtime does. It then checks the re-rendered HTML and, for the database apps, the rows the actions wrote. Every test in that file is named `Test<App>…`, so `task ui:test:app APP=contacts` runs one app's tests.

## The database and Starlark

- `demodb/db.go` (`demodb.Open`) opens a private in-memory SQLite database (`file:brickui?mode=memory`, pure Go `modernc.org/sqlite`, one connection) and seeds it. `seed()` runs each app's `demodb/db_<app>.go` in turn (`seedApps`): `contacts`, `tasks`, `messages` (Inbox), `orders` (Dashboard), `products` and `cart` (Store), `invoices` and `invoice_lines`, then the chat, calendar and CMS tables. Invoices reads the seeded contacts, so it runs after Contacts. Every table's closed sets and formats are documented beside its columns.
- Seeding is deterministic: rows are computed from the loop index and a fixed epoch (`seedEpoch`, 2026-01-05), so every run produces the same rows and the pages the same bytes. The apps that need "today" use a fixed date (`TODAY` in `apps/tasks/tasks.star` and `apps/invoices/invoices.star`) for the same reason.
- `main.go` (`newSite`) builds the Starlark backend with `scriptstarlark.New(scriptstarlark.Options{Globals: {"today": …}, DB: db, DBExec: true})`. That grants every script `db.query` (read) and `db.exec` (write) on this database, plus a `today()` built-in, and nothing else: scripts have no files, network or clock of their own. SQL in a script must be a literal string with `?` parameters.
- The engine also gets the goldmark markdown renderer and the bluemonday sanitizer. `brick.yaml` is applied with `LoadConfig`, and `site.New` serves pages, assets and `/_brick/action`, keeping per-visitor state in memory.
- The tests call the same `newSite`, each with a freshly seeded database, so a test sees exactly what a visitor sees.
