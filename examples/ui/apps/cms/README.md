# CMS

A blog admin over SQLite — posts list with filters and bulk publishing, a rich-text editor with autosave, scheduling and a sanitized preview, and a public blog it publishes to.

Open it at http://localhost:8002/apps/cms/ (`task ui:serve`). The public blog it publishes to is at `/apps/cms/blog/`.

## Files

| File | What it holds |
|---|---|
| `CmsApp.html` | The Blog CMS admin — a posts list (status filter with counts, search, sort, pages, selection with bulk publish / unpublish / delete, a row menu) and a post editor (title and address, the rich-text Editor with a Preview of the sanitized draft, excerpt, status and schedule, author, tags and cover), with autosave, publish confirmation and toasts. |
| `CmsBlogHeader.html` | The public blog's top bar — brand, navigation and theme toggle — shared by the blog index and the post page. |
| `CmsBlogList.html` | The public blog's live posts (published, or scheduled for a date that has come), newest first, as a card grid. |
| `CmsCover.html` | A post's cover — a gradient in the post's tone with its title set large — shared by the CMS admin list, the editor preview and the public blog. |
| `CmsPost.html` | One public post, chosen by the ?slug= in the URL. |
| `cms.star` | The queries and actions, in Starlark (loaded with `use:` as `~/apps/cms/cms.star`). |
| `../../content/apps/cms.html` | The page: `layout: app`, `AppShell` with the shared `AppNav`, this app's component, and the View source tabs. It stays in `content/` because brick derives URLs from there. The blog pages are `../../content/apps/cms/blog/`. |
| `../../demodb/db_cms.go` | The app's tables and their deterministic seed. It is Go, so it lives in the importable `demodb` package; `seed()` in `demodb/db.go` runs it. |
| `../../apps_cms_test.go` | The app's tests: they load the page and post to `/_brick/action` the way the runtime does. |

## Test

```sh
task ui:test:app APP=cms   # only this app's tests
task ui:test                # every showcase test
```
