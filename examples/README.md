# brick examples

Every feature, in a project you can run. Each one is checked by a Go test, and the demo site also runs in a real headless Chrome (`task e2e`).

```sh
cd examples
go run . -serve :8080     # the demo site with live reload — open http://localhost:8080
go run . -check           # every problem at once
BRICK_SECRET=<key> go run . -out build   # the static site; its stateful pages need a state key (brick keygen)
go run ./engineonly       # the engine alone in a net/http app — http://localhost:8081/?user=Sara
```

Stateful components (the like button, the clock, the sign-up form, every demo app in `ui/`) seal their state into the page with `BRICK_SECRET`, so an open page keeps working when the server restarts. Without it, serving uses a key made for the process (a restart invalidates state; the page recovers in place) and `-out` refuses the stateful pages. `task demo`, `task demo:build`, `task ui:serve` and `task demo:both` read a dev key from gopass (`personal/brick/dev-state-key`) and fall back the same way.

## Every example

One row per program. Most are the small project a guide builds, with a Go test that runs the real handler and checks the exact output the guide shows. Run one from this folder with `go run ./<name>` and test it with `go test ./<name>/`; each program's own README says what it prints or serves.

| Example | What it shows | Guide |
|---|---|---|
| [components-errors](./components-errors/) | Components chosen from data, a boundary with a fallback, Prod and Dev error pages, `OnError` hooks, and `CompileAll` diagnostics with file and line | [guides/components/errors.md](../docs/guides/components/errors.md) |
| [components-frontmatter](./components-frontmatter/) | Generates the frontmatter reference from `brick.FrontmatterSchema()` | [reference/frontmatter.md](../docs/reference/frontmatter.md) |
| [components-layouts-data](./components-layouts-data/) | Layout chains, `.Ctx` with `provide:` and `inject:`, `.Site` data, `use:` imports, translations and a language variant | [guides/components/layouts-and-data.md](../docs/guides/components/layouts-and-data.md) |
| [components-loaders](./components-loaders/) | Starlark loaders and `load_many:`, `.star` modules, a host global, and loader answers (redirect, status, header) turned into HTTP | [guides/components/loaders.md](../docs/guides/components/loaders.md) |
| [components-props](./components-props/) | Typed props with defaults and `oneof`, object props, the `:post` shorthand, `{{ attrs }}`, `brick:spread`, host functions and prop types | [guides/components/props.md](../docs/guides/components/props.md) |
| [components-slots](./components-slots/) | `{{ children }}`, named blocks, scoped and declared slots, child tags as data, raw bodies | [guides/components/slots.md](../docs/guides/components/slots.md) |
| [components-styles-client](./components-styles-client/) | Scoped and inclusive styles, sibling stylesheets, `StyleTransform`, `<brick:client>` modules and `brick.mount` | [guides/components/styles-and-client.md](../docs/guides/components/styles-and-client.md) |
| [forms-nojs](./forms-nojs/) | A signup form that works with JavaScript off, answered by the host's own router with `Engine.FormPost` | [guides/forms/without-javascript.md](../docs/guides/forms/without-javascript.md) |
| [forms-signup](./forms-signup/) | Deferred fields, list and dotted-path values, `brick:dirty`, loading states, confirm and disable-with | [guides/forms/deferred-fields.md](../docs/guides/forms/deferred-fields.md) |
| [forms-uploads](./forms-uploads/) | Uploads into the `upload/disk` sink, a per-file check, presigned uploads and uploads with JavaScript off | [guides/forms/uploads.md](../docs/guides/forms/uploads.md) |
| [forms-validation-lists](./forms-validation-lists/) | Rules declared once, touched-field errors, wildcard rules on list rows, `.Form` objects and a custom validator | [guides/forms/validation.md](../docs/guides/forms/validation.md) |
| [interactivity-client-commands](./interactivity-client-commands/) | A menu opened with `brick:js` commands and no server, keyed rows, transitions and enter and leave animations | [guides/interactivity/morph-transitions-client-commands.md](../docs/guides/interactivity/morph-transitions-client-commands.md) |
| [interactivity-counter](./interactivity-counter/) | A stateful counter: actions with arguments, `$set` and `$toggle`, `init:` and `updated:` hooks, renderless actions, a Modelable stepper | [guides/interactivity/stateful-components.md](../docs/guides/interactivity/stateful-components.md) |
| [interactivity-events-modifiers](./interactivity-events-modifiers/) | Every event attribute and modifier on one inbox, with the compile errors for typos | [guides/interactivity/events-and-modifiers.md](../docs/guides/interactivity/events-and-modifiers.md) |
| [interactivity-lazy-stream](./interactivity-lazy-stream/) | An island, a lazy island, a streamed island and a streamed action reply on a cached page | [guides/interactivity/islands-lazy-and-streaming.md](../docs/guides/interactivity/islands-lazy-and-streaming.md) |
| [interactivity-navigation](./interactivity-navigation/) | Navigation without reloads: `brick:current`, `brick:prefetch`, `brick:persist`, `brick:scroll` and fragment navigation | [guides/interactivity/navigation.md](../docs/guides/interactivity/navigation.md) |
| [interactivity-url-state](./interactivity-url-state/) | State in the address bar (`query:`), `params:`, `remember:`, `temporary:`, `locked:` and `writable:` | [guides/interactivity/url-and-server-state.md](../docs/guides/interactivity/url-and-server-state.md) |
| [live-client](./live-client/) | The small Go client the `live-*` examples use to post actions and open push streams like the browser runtime does | [guides/live/README.md](../docs/guides/live/README.md) |
| [live-effects-events](./live-effects-events/) | Effects (flash, title, navigate, download, redirect, custom) and events between components | [guides/live/effects.md](../docs/guides/live/effects.md) |
| [live-jobs](./live-jobs/) | A background job that reports back, cancelling it, and the smallest custom queue | [guides/live/background-jobs.md](../docs/guides/live/background-jobs.md) |
| [live-push-presence](./live-push-presence/) | Server push to every open page and a presence list, over real event streams | [guides/live/push-and-presence.md](../docs/guides/live/push-and-presence.md) |
| [live-rows-regions](./live-rows-regions/) | Row streams, streamed text, a load-more region and a native form submit | [guides/live/rows-and-regions.md](../docs/guides/live/rows-and-regions.md) |
| [methodlinks](./methodlinks/) | `brick:method` links sending PATCH, DELETE and POST through `brick.MethodOverride` | [guides/forms/method-links.md](../docs/guides/forms/method-links.md) |
| [production-host-api](./production-host-api/) | The engine with no site layer: `Render`, `RenderComponent`, `RenderChunks` and `Fill`, `ActionHandler`, `RunAction` | [guides/production/host-api.md](../docs/guides/production/host-api.md) |
| [production-middleware](./production-middleware/) | The action middleware chain: recover, log, require a session, rate limit, timeout and an app rule | [guides/production/action-middleware.md](../docs/guides/production/action-middleware.md) |
| [production-sealed-state](./production-sealed-state/) | State sealed into the page with `BRICK_SECRET`, key rotation, session binding and sign-out | [guides/production/sealed-state.md](../docs/guides/production/sealed-state.md) |
| [production-state-stores](./production-state-stores/) | Two stores in one engine: memory and a named store backed by your own `BytesStore` | [guides/production/state-stores.md](../docs/guides/production/state-stores.md) |
| [site-blog](./site-blog/) | A blog with the `site` package: routes from files, `.Site.Pages`, markdown, a collection, a draft, a 404 page, the cache and an export | [guides/site/pages-and-routes.md](../docs/guides/site/pages-and-routes.md) |
| [site-dynamic-routes](./site-dynamic-routes/) | `[slug]` and `[...rest]` pages, `paths:` for the build, and a static page that beats a pattern | [guides/site/dynamic-routes.md](../docs/guides/site/dynamic-routes.md) |
| [site-feeds-sitemap](./site-feeds-sitemap/) | Output pages (`feed.xml.html`, `robots.txt.html`), one feed per tag and the sitemap | [guides/site/feeds-and-sitemap.md](../docs/guides/site/feeds-and-sitemap.md) |
| [site-i18n](./site-i18n/) | One site in three languages: variants, `t`, hreflang links and a language switcher | [guides/site/languages.md](../docs/guides/site/languages.md) |
| [site-images](./site-images/) | The `image` built-in: resized copies, srcset and WebP | [guides/site/images.md](../docs/guides/site/images.md) |
| [site-protected](./site-protected/) | An island that turns visitors away, and a page rendered per visitor | [guides/site/protected-pages.md](../docs/guides/site/protected-pages.md) |
| [site-subpath](./site-subpath/) | Two sites on one server under `/docs/` and `/shop/` with `BasePath`, `KeepPaths` and `site.Mount` | [guides/site/subpath-and-mount.md](../docs/guides/site/subpath-and-mount.md) |
| [site-tailwind](./site-tailwind/) | A stylesheet built by the standalone Tailwind CLI whenever the classes in the project change | [guides/site/tailwind.md](../docs/guides/site/tailwind.md) |
| [tooling-bricktest](./tooling-bricktest/) | Testing a theme with `bricktest.Live`: submit, upload, effects, view log and golden files | [guides/tooling/bricktest.md](../docs/guides/tooling/bricktest.md) |
| [tooling-statetest](./tooling-statetest/) | A whole custom state store proven with the `statetest` conformance suite | [guides/tooling/statetest.md](../docs/guides/tooling/statetest.md) |
| [components-libraries](./components-libraries/) | Three kinds of library in one `brick.yaml`: a project folder with a `prefix`, the embedded kit cut down with `only:`, and a remote library pinned by `brick vendor` | [guides/components/libraries.md](../docs/guides/components/libraries.md) |
| [components-options](./components-options/) | The engine settings that decide how components are found and written: `Options.Roots`, `syntax:`, `passthrough:`, `Options.DataFormats`, `tag` and `endtag`, `dedent:`, `InheritAssets` and render stats | [guides/components/props.md](../docs/guides/components/props.md) |
| [forms-method-csrf](./forms-method-csrf/) | `brick:method` delete links for an app with a CSRF token, added to the hidden form with `brick.methodFormWith` | [guides/forms/method-links.md](../docs/guides/forms/method-links.md) |
| [interactivity-nested](./interactivity-nested/) | A parent holding one stateful row per contact: `reactive:` props, `:key` identity, and reading instances from the host | [guides/interactivity/stateful-components.md](../docs/guides/interactivity/stateful-components.md) |
| [tooling-cli](./tooling-cli/) | The `brick` command as a CI job uses it: `brick version -json` and `brick fmt -l` / `-d` as a formatting gate | [guides/tooling/fmt.md](../docs/guides/tooling/fmt.md) |
| [engineonly](./engineonly/) | The engine in a plain `net/http` app: `RenderChunks` and `Fill`, `contrib/assets-http`, `ActionHandler`, `RenderComponent` | [guides/production/host-api.md](../docs/guides/production/host-api.md) |
| [tailwind](./tailwind/) | The `tailwind:` section of `brick.yaml` with the standalone binary | [guides/site/tailwind.md](../docs/guides/site/tailwind.md) |
| [vendorkit](./vendorkit/) | Vendoring `kit/` into the demo site as if it were fetched remotely | [README.md#remote-components](../README.md#remote-components) |
| [ui](./ui/) | The kit showcase: a page per component and twelve SQLite-backed demo apps | [kit/README.md](../kit/README.md) |

The demo site (`go run . -serve :8080`, folders `site/` and `demosite/`) and the cookbook and guide tests (`cookbook_test.go`, `guide_test.go`) are described in the next section. `genimg/` only writes the demo image.

## Where each feature is shown

| Feature | Where |
|---|---|
| Components, props, `children`, attributes, `brick:spread` | `site/content/index.html` (Card), `site/content/features/data.html` (Primary → Button) |
| Scoped slots (`node` + `.Slot`), `.Children` | `site/components/PostList.html`, `site/components/Tabs.html` |
| Raw bodies | `site/components/CodeBlock.html` |
| `<brick:render>` from data | `site/content/blog/hello.html` |
| Layouts, `.Ctx` with `provide` | `site/layouts/default.html` |
| `use:` data, `.Site` from `site.yaml` and `data/` | `site/content/index.html`, `site/data/` |
| Translation, number and date formatting | `site/i18n/`, `site/content/index.html`, `/blog/` |
| Language variants and per-language URLs | `site/content/about.html` + `about.fr.html` → `/about/`, `/fr/about/` |
| Starlark helpers, loaders, `load()` of data | `site/lib/`, `site/components/RecentPosts.html` |
| A host capability granted to scripts | `now()` in `main.go`, used by `site/lib/clock.star` |
| Boundaries | `site/content/features/data.html` (Broken → fallback) |
| `sanitize` | `site/content/features/data.html` |
| Client code, scoped styles, `{{ assets }}` | `site/components/Counter.html` |
| Client module imports (`brick:Modal`) | `site/components/Opener.html` → `/features/client/` |
| Islands (per-request parts of a cached page) | `site/components/Account.html` in the header — try `?as=Sara` |
| Actions | `site/components/Likes.html` |
| Bindings, validation, loading states | `site/components/Signup.html` |
| Polling | `site/components/Clock.html` → `/features/live/` |
| Navigation, `brick:prefetch`, `brick:persist`, `brick:nav="off"` | `site/layouts/default.html` |
| The site layer: routes, `.Site.Pages`, markdown pages, collections, 404, static files | `site/content/blog/`, `site/content/404.html`, `site/static/` |
| Images | `site/content/features/images.html` |
| Remote imports and `brick_modules/` | `site/content/features/kit.html`, `site/brick.lock` (vendored by `go run ./vendorkit`) |
| The kit (Tabs, Accordion, Carousel) | `/features/kit/` |
| Hooks | the `generator` AfterRender hook in `main.go` |
| The engine without the site layer: `RenderChunks` + `Fill`, `contrib/assets-http`, `ActionHandler`, `RenderComponent` | `engineonly/` |
| Tailwind | `tailwind/` (needs the standalone `tailwindcss` binary) |
| Cookbook recipes | `cookbook_test.go` runs every recipe in `docs/cookbook.md` |
