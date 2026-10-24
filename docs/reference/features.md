# Feature index

Every brick feature, grouped by area, with the page that explains it and an example program or file that uses it. A dash means there is no such page or example yet. Packages are import paths under `github.com/ubgo/brick` (`runtime` is the browser runtime, `cmd/brick` the CLI).

## Pages and components

| Feature | Package | Guide | Example |
|---|---|---|---|
| Pages are plain .html files with optional frontmatter | `brick` | [Try it](../guides/site/languages.md#try-it) | [examples/site/content/index.html](../../examples/site/content/index.html) |
| Component tags in two spellings (`<Card>`, `<x-card>`) | `brick` | [Step 1 — Write a page with frontmatter](../guides/components/props.md#step-1--write-a-page-with-frontmatter) | [examples/components-options/theme/content/hyphen.html](../../examples/components-options/theme/content/hyphen.html) |
| Component roots and lookup (`Options.Roots`) | `brick` | [Step 5 — Find components in more than one folder](../guides/components/props.md#step-5--find-components-in-more-than-one-folder) | [examples/components-options/main.go](../../examples/components-options/main.go) |
| Typed props (`type`, `default`, `required`, `oneof`) | `brick` | [Step 8 — Document a component in its own file](../guides/components/props.md#step-8--document-a-component-in-its-own-file) | [examples/components-props/theme/components/Card.html](../../examples/components-props/theme/components/Card.html) |
| Expression attributes (`:count="5"`) | `brick` | [Components and props](../../README.md#components-and-props) | [examples/production-host-api/theme/content/list.html](../../examples/production-host-api/theme/content/list.html) |
| `:prop` shorthand (`<Card :post />`) | `brick` | [Step 1 — Write a page with frontmatter](../guides/components/props.md#step-1--write-a-page-with-frontmatter) | [examples/components-props/theme/content/index.html](../../examples/components-props/theme/content/index.html) |
| Undeclared attributes and `{{ attrs }}` | `brick` | [Step 3 — Place attributes the component did not declare](../guides/components/props.md#step-3--place-attributes-the-component-did-not-declare) | [examples/components-props/theme/components/Button.html](../../examples/components-props/theme/components/Button.html) |
| `brick:spread` (pass a dict as props) | `brick` | [Step 3 — Place attributes the component did not declare](../guides/components/props.md#step-3--place-attributes-the-component-did-not-declare) | [examples/components-props/theme/components/Primary.html](../../examples/components-props/theme/components/Primary.html) |
| Struct-typed props (`fields:`, `of:`) | `brick` | [Step 2 — Declare the component's props](../guides/components/props.md#step-2--declare-the-components-props) | [examples/components-props/theme/components/Card.html](../../examples/components-props/theme/components/Card.html) |
| `strictProps` / `strictParams` | `brick` | [Step 9 — Refuse attributes a component did not declare](../guides/components/props.md#step-9--refuse-attributes-a-component-did-not-declare) | [examples/components-frontmatter/main.go](../../examples/components-frontmatter/main.go) |
| `passthrough:` (tags written as plain HTML) | `brick` | [Step 6 — Choose how a file's tags are read](../guides/components/props.md#step-6--choose-how-a-files-tags-are-read) | [examples/components-options/theme/content/widget.html](../../examples/components-options/theme/content/widget.html) |
| Dynamic components (`<brick:render>`) | `brick` | [Step 1 — Choose a component from data](../guides/components/errors.md#step-1--choose-a-component-from-data) | [examples/components-errors/theme/content/index.html](../../examples/components-errors/theme/content/index.html) |
| `tag` / `endtag` built-ins (element name from data) | `brick` | [Step 7 — Write an element whose name is data](../guides/components/props.md#step-7--write-an-element-whose-name-is-data) | [examples/components-options/theme/components/Heading.html](../../examples/components-options/theme/components/Heading.html) |
| Component docs (`doc:`, `examples:`, `Engine.ComponentDocs`) | `brick` | [Step 8 — Document a component in its own file](../guides/components/props.md#step-8--document-a-component-in-its-own-file) | [examples/components-options/theme/components/Card.html](../../examples/components-options/theme/components/Card.html) |
| Per-file template syntax (`syntax:`) | `brick` | [Step 6 — Choose how a file's tags are read](../guides/components/props.md#step-6--choose-how-a-files-tags-are-read) | [examples/components-options/theme/content/hyphen-bad.html](../../examples/components-options/theme/content/hyphen-bad.html) |

## Blocks, children and slots

| Feature | Package | Guide | Example |
|---|---|---|---|
| `{{ children }}` | `brick` | [Step 2 — Wrap pages in a layout](../guides/components/layouts-and-data.md#step-2--wrap-pages-in-a-layout) | [examples/components-libraries/remote-src/acme/ui/components/Chip.html](../../examples/components-libraries/remote-src/acme/ui/components/Chip.html) |
| Named blocks (`<brick:prop>`, `{{ node }}`, `{{ hasNode }}`) | `brick` | [Step 8 — Document a component in its own file](../guides/components/props.md#step-8--document-a-component-in-its-own-file) | [examples/components-slots/theme/components/Card.html](../../examples/components-slots/theme/components/Card.html) |
| Scoped slots (`.Slot`) | `brick` | [Step 3 — Declare which blocks a component takes](../guides/components/slots.md#step-3--declare-which-blocks-a-component-takes) | [examples/components-errors/theme/content/index.html](../../examples/components-errors/theme/content/index.html) |
| Inspecting child tags (`.Children`, `{{ child }}`) | `brick` | [Step 4 — Read child tags as data](../guides/components/slots.md#step-4--read-child-tags-as-data) | [examples/components-slots/theme/components/Tabs.html](../../examples/components-slots/theme/components/Tabs.html) |
| Declared slots (`brick: {slots:}`) | `brick` | [Step 8 — Document a component in its own file](../guides/components/props.md#step-8--document-a-component-in-its-own-file) | [examples/components-slots/theme/components/Panel.html](../../examples/components-slots/theme/components/Panel.html) |
| Raw bodies (`raw: true`) and `src=` code files | `brick` | [Step 6 — Keep the indentation of a raw body](../guides/components/slots.md#step-6--keep-the-indentation-of-a-raw-body) | [examples/components-options/theme/components/Dedented.html](../../examples/components-options/theme/components/Dedented.html) |
| `dedent:` | `brick` | [Step 6 — Keep the indentation of a raw body](../guides/components/slots.md#step-6--keep-the-indentation-of-a-raw-body) | [examples/components-options/theme/components/Pre.html](../../examples/components-options/theme/components/Pre.html) |

## Layouts, context and data

| Feature | Package | Guide | Example |
|---|---|---|---|
| Layouts (`layout:`, `Options.LayoutDir`) | `brick` | [Step 2 — Wrap pages in a layout](../guides/components/layouts-and-data.md#step-2--wrap-pages-in-a-layout) | [examples/components-layouts-data/theme/content/about.html](../../examples/components-layouts-data/theme/content/about.html) |
| `.Ctx` with `provide:` / `inject:` | `brick` | [Step 3 — Provide a value to everything beneath](../guides/components/layouts-and-data.md#step-3--provide-a-value-to-everything-beneath) | [examples/components-layouts-data/theme/components/Card.html](../../examples/components-layouts-data/theme/components/Card.html) |
| `.Site` and `LoadSite` (site.yaml, data/) | `brick` | [Step 1 — Load the site config, data and messages in Go](../guides/components/layouts-and-data.md#step-1--load-the-site-config-data-and-messages-in-go) | [examples/components-layouts-data/theme/content/index.html](../../examples/components-layouts-data/theme/content/index.html) |
| `.Params` (frontmatter values) | `brick` | [Step 5 — Answer with a redirect, a status or a header](../guides/components/loaders.md#step-5--answer-with-a-redirect-a-status-or-a-header) | [examples/components-loaders/theme/content/team.html](../../examples/components-loaders/theme/content/team.html) |
| Data formats (`Options.DataFormats`) | `brick` | [Step 5 — Find components in more than one folder](../guides/components/props.md#step-5--find-components-in-more-than-one-folder) | [examples/components-options/main.go](../../examples/components-options/main.go) |
| `use:` imports of components and data | `brick` | [Step 4 — Check a whole folder before it ships](../guides/components/errors.md#step-4--check-a-whole-folder-before-it-ships) | [examples/components-options/theme/content/spin.html](../../examples/components-options/theme/content/spin.html) |
| Translations, plurals and localized dates | `brick` | [Translations](../guide.md#translations) | [examples/site-i18n/theme/content/index.html](../../examples/site-i18n/theme/content/index.html) |
| Language variants (`about.fr.html`) and `.Page.Translations` | `site` | [Step 3 — Link the languages in the layout](../guides/site/languages.md#step-3--link-the-languages-in-the-layout) | [examples/site-i18n/theme/layouts/default.html](../../examples/site-i18n/theme/layouts/default.html) |

## Scripts and loaders

| Feature | Package | Guide | Example |
|---|---|---|---|
| Scripting seam (`Options.Scripting`) | `brick` | [Step 1 — Give the engine a script language](../guides/components/loaders.md#step-1--give-the-engine-a-script-language) | [examples/components-errors/main.go](../../examples/components-errors/main.go) |
| Inline `<brick:script>` functions | `brick` | [Step 3 — Let a component load its own data](../guides/components/loaders.md#step-3--let-a-component-load-its-own-data) | [examples/components-errors/theme/components/RecentPosts.html](../../examples/components-errors/theme/components/RecentPosts.html) |
| `.star` modules and `load()` | `contrib/script-starlark` | [Step 5 — Answer with a redirect, a status or a header](../guides/components/loaders.md#step-5--answer-with-a-redirect-a-status-or-a-header) | [examples/components-loaders/theme/lib/posts.star](../../examples/components-loaders/theme/lib/posts.star) |
| Component loaders (`brick: {load:}`) | `brick` | [Step 3 — Let a component load its own data](../guides/components/loaders.md#step-3--let-a-component-load-its-own-data) | [examples/components-loaders/theme/content/post.html](../../examples/components-loaders/theme/content/post.html) |
| Batch loaders (`load_many:`) | `brick` | [Step 4 — Batch a loader used in a loop](../guides/components/loaders.md#step-4--batch-a-loader-used-in-a-loop) | [examples/components-loaders/theme/components/UserCard.html](../../examples/components-loaders/theme/components/UserCard.html) |
| Host capabilities for scripts (`ScriptGlobals`, db, http) | `contrib/script-starlark` | [Step 1 — Give the engine a script language](../guides/components/loaders.md#step-1--give-the-engine-a-script-language) | [examples/components-loaders/main.go](../../examples/components-loaders/main.go) |
| Step and time limits for scripts | `contrib/script-starlark` | [Step 4 — Cap action bodies and shape their errors](../guides/production/limits-determinism.md#step-4--cap-action-bodies-and-shape-their-errors) | [examples/production-host-api/main.go](../../examples/production-host-api/main.go) |
| Loader answers: `brick.redirect`, `brick.error`, `brick.header`, `not_found` | `brick` | [Step 2 — Write a module that imports a module and a data file](../guides/components/loaders.md#step-2--write-a-module-that-imports-a-module-and-a-data-file) | [examples/components-loaders/theme/lib/posts.star](../../examples/components-loaders/theme/lib/posts.star) |
| Your own template functions (`Options.Funcs`) | `brick` | [Step 4 — Add your own function and prop type from Go](../guides/components/props.md#step-4--add-your-own-function-and-prop-type-from-go) | [examples/components-props/main.go](../../examples/components-props/main.go) |
| Your own prop types (`Options.PropTypes`) | `brick` | [Step 4 — Add your own function and prop type from Go](../guides/components/props.md#step-4--add-your-own-function-and-prop-type-from-go) | [examples/components-props/main.go](../../examples/components-props/main.go) |

## Errors and diagnostics

| Feature | Package | Guide | Example |
|---|---|---|---|
| Boundaries with fallbacks (`<brick:boundary>`) | `brick` | [Step 1 — Choose a component from data](../guides/components/errors.md#step-1--choose-a-component-from-data) | [examples/components-errors/theme/content/index.html](../../examples/components-errors/theme/content/index.html) |
| Diagnostics with file and line (`brick check`, `ErrorList`) | `brick` | [Step 4 — Check a whole folder before it ships](../guides/components/errors.md#step-4--check-a-whole-folder-before-it-ships) | [examples/forms-validation-lists/main.go](../../examples/forms-validation-lists/main.go) |
| Unbalanced HTML warning (`html-balance`) | `brick` | [Step 6 — Read the unbalanced-HTML warning and the source location](../guides/components/errors.md#step-6--read-the-unbalanced-html-warning-and-the-source-location) | [examples/components-options/main.go](../../examples/components-options/main.go) |
| Dev source locations (`data-brick-loc`) | `brick` | [Try it](../guides/components/errors.md#try-it) | [examples/components-options/main.go](../../examples/components-options/main.go) |
| Context-aware Dev error pages (`Mode`) | `brick` | [Step 3 — Hear every error in Go](../guides/components/errors.md#step-3--hear-every-error-in-go) | [examples/components-errors/main.go](../../examples/components-errors/main.go) |
| Hooks (`Options.Hooks`: compile, before/after render, error) | `brick` | [Step 3 — Hear every error in Go](../guides/components/errors.md#step-3--hear-every-error-in-go) | [examples/components-errors/main.go](../../examples/components-errors/main.go) |
| Render limits (`MaxDepth`, `MaxComponents`, `MaxOutputBytes`) | `brick` | [Step 2 — Lower the render limits for data you do not control](../guides/production/limits-determinism.md#step-2--lower-the-render-limits-for-data-you-do-not-control) | [examples/production-host-api/main.go](../../examples/production-host-api/main.go) |

## Styles and client code

| Feature | Package | Guide | Example |
|---|---|---|---|
| `<brick:style>` global and scoped | `brick` | [Step 1 — Scope a component's CSS](../guides/components/styles-and-client.md#step-1--scope-a-components-css) | [examples/components-options/theme/components/Badge.html](../../examples/components-options/theme/components/Badge.html) |
| `scoped="inclusive"` | `brick` | [Step 2 — Let a parent reach a child's root](../guides/components/styles-and-client.md#step-2--let-a-parent-reach-a-childs-root) | [examples/components-styles-client/theme/components/Grid.html](../../examples/components-styles-client/theme/components/Grid.html) |
| Sibling stylesheets (`Card.css`, `brick: {css:}`) | `brick` | [Step 3 — Keep the CSS in its own file](../guides/components/styles-and-client.md#step-3--keep-the-css-in-its-own-file) | [examples/components-styles-client/theme/components/Badge.html](../../examples/components-styles-client/theme/components/Badge.html) |
| `Options.StyleTransform` | `brick` | [Step 4 — Transform every stylesheet in Go](../guides/components/styles-and-client.md#step-4--transform-every-stylesheet-in-go) | [examples/components-styles-client/main.go](../../examples/components-styles-client/main.go) |
| `<brick:client>` modules with SRI | `brick` | [Step 6 — Import one component's code from another](../guides/components/styles-and-client.md#step-6--import-one-components-code-from-another) | [examples/components-styles-client/theme/components/Modal.html](../../examples/components-styles-client/theme/components/Modal.html) |
| Client module imports (`brick:Modal`) | `brick` | [Step 6 — Import one component's code from another](../guides/components/styles-and-client.md#step-6--import-one-components-code-from-another) | [examples/components-styles-client/theme/components/Opener.html](../../examples/components-styles-client/theme/components/Opener.html) |
| `{{ assets }}`, `AssetBase` and CSP nonces | `brick` | [Step 5 — Use the components and the kit's theme](../guides/components/libraries.md#step-5--use-the-components-and-the-kits-theme) | [examples/components-options/theme/content/island.html](../../examples/components-options/theme/content/island.html) |
| `brick.mount(type, fn)` | `runtime` | [Try it](../guides/components/styles-and-client.md#try-it) | [examples/components-styles-client/theme/components/Opener.html](../../examples/components-styles-client/theme/components/Opener.html) |
| Client props (`client: true`) | `brick` | [Step 5 — Mount browser code on a component](../guides/components/styles-and-client.md#step-5--mount-browser-code-on-a-component) | [examples/components-styles-client/theme/components/Accordion.html](../../examples/components-styles-client/theme/components/Accordion.html) |
| `InheritAssets` (assets of nested islands) | `brick` | [Step 7 — Keep the old engine's assets after a rebuild](../guides/components/styles-and-client.md#step-7--keep-the-old-engines-assets-after-a-rebuild) | [examples/components-options/main.go](../../examples/components-options/main.go) |

## Islands, lazy and streaming

| Feature | Package | Guide | Example |
|---|---|---|---|
| Islands (`brick: {island: true}`) | `brick` | [Step 3 — Mark the island, the lazy island and the streamed one](../guides/interactivity/islands-lazy-and-streaming.md#step-3--mark-the-island-the-lazy-island-and-the-streamed-one) | [examples/interactivity-lazy-stream/theme/components/Account.html](../../examples/interactivity-lazy-stream/theme/components/Account.html) |
| `Engine.Fill` / `RenderChunks` for per-request parts | `brick` | [Step 3 — Cache the page, fill the island per request](../guides/production/host-api.md#step-3--cache-the-page-fill-the-island-per-request) | [examples/production-host-api/main.go](../../examples/production-host-api/main.go) |
| Lazy islands (`brick:lazy`, `visible`, `idle`) | `brick` | [Step 3 — Mark the island, the lazy island and the streamed one](../guides/interactivity/islands-lazy-and-streaming.md#step-3--mark-the-island-the-lazy-island-and-the-streamed-one) | [examples/interactivity-lazy-stream/theme/content/index.html](../../examples/interactivity-lazy-stream/theme/content/index.html) |
| Island tickets (`Options.IslandTickets`) | `brick` | [Step 5 — Seal the tickets so they survive a restart](../guides/interactivity/islands-lazy-and-streaming.md#step-5--seal-the-tickets-so-they-survive-a-restart) | [examples/interactivity-lazy-stream/main.go](../../examples/interactivity-lazy-stream/main.go) |
| Streamed page render (`brick:lazy="stream"`, `FillStream`) | `brick` | [Step 3 — Mark the island, the lazy island and the streamed one](../guides/interactivity/islands-lazy-and-streaming.md#step-3--mark-the-island-the-lazy-island-and-the-streamed-one) | [examples/interactivity-lazy-stream/theme/content/index.html](../../examples/interactivity-lazy-stream/theme/content/index.html) |
| Lazy islands in tests (`WithLazyInline`) | `brick` | [Step 6 — Read lazy islands in a test](../guides/interactivity/islands-lazy-and-streaming.md#step-6--read-lazy-islands-in-a-test) | — |

## Navigation

| Feature | Package | Guide | Example |
|---|---|---|---|
| Navigation without reloads (`navigation: true`) | `runtime` | [Step 1 — Turn navigation on](../guides/interactivity/navigation.md#step-1--turn-navigation-on) | [examples/interactivity-navigation/theme/brick.yaml](../../examples/interactivity-navigation/theme/brick.yaml) |
| `brick:prefetch` and speculation rules | `runtime` | [Step 2 — Mark up the layout](../guides/interactivity/navigation.md#step-2--mark-up-the-layout) | [examples/interactivity-navigation/theme/layouts/default.html](../../examples/interactivity-navigation/theme/layouts/default.html) |
| `brick:persist` | `runtime` | [Step 2 — Mark up the layout](../guides/interactivity/navigation.md#step-2--mark-up-the-layout) | [examples/interactivity-navigation/theme/layouts/default.html](../../examples/interactivity-navigation/theme/layouts/default.html) |
| `brick:nav="off"` / `brick:nav="replace"` | `runtime` | [Step 2 — Mark up the layout](../guides/interactivity/navigation.md#step-2--mark-up-the-layout) | [examples/interactivity-navigation/theme/layouts/default.html](../../examples/interactivity-navigation/theme/layouts/default.html) |
| `brick:current` (active link) | `runtime` | [Step 2 — Mark up the layout](../guides/interactivity/navigation.md#step-2--mark-up-the-layout) | [examples/interactivity-navigation/theme/layouts/default.html](../../examples/interactivity-navigation/theme/layouts/default.html) |
| `brick:scroll` (keep a scroll position) | `runtime` | [Step 2 — Mark up the layout](../guides/interactivity/navigation.md#step-2--mark-up-the-layout) | [examples/interactivity-navigation/theme/layouts/default.html](../../examples/interactivity-navigation/theme/layouts/default.html) |
| `brick.navigate(url)` and the `navigate` event | `runtime` | [Step 3 — Listen and cancel from code](../guides/interactivity/navigation.md#step-3--listen-and-cancel-from-code) | [examples/live-effects-events/theme/components/Inbox.html](../../examples/live-effects-events/theme/components/Inbox.html) |
| Fragment navigation (`Brick-Nav`, page marks) | `site` | [Try it](../guides/interactivity/navigation.md#try-it) | — |
| Live reload that keeps your place (`brick.refresh`) | `site` | [Step 3 — Try a component on its own](../guides/site/dev-server.md#step-3--try-a-component-on-its-own) | [examples/site-blog/theme/static/refresh.js](../../examples/site-blog/theme/static/refresh.js) |
| Method links (`brick:method`, `brick.MethodOverride`) | `brick` | [Step 1 — Put the verb on the link](../guides/forms/method-links.md#step-1--put-the-verb-on-the-link) | [examples/forms-method-csrf/theme/components/PostRow.html](../../examples/forms-method-csrf/theme/components/PostRow.html) |

## Stateful components and actions

| Feature | Package | Guide | Example |
|---|---|---|---|
| Stateful components (`state:`) | `brick` | [Actions — a button that changes server state](../guide.md#actions--a-button-that-changes-server-state) | [examples/production-state-stores/theme/components/Search.html](../../examples/production-state-stores/theme/components/Search.html) |
| Actions (`brick:click`, Starlark action functions) | `brick` | [Step 3 — Declare state and write the actions](../guides/interactivity/stateful-components.md#step-3--declare-state-and-write-the-actions) | [examples/interactivity-counter/theme/components/Cart.html](../../examples/interactivity-counter/theme/components/Cart.html) |
| Built-in calls `$set`, `$toggle`, `$refresh`, `$reset` | `brick` | [Step 3 — Declare state and write the actions](../guides/interactivity/stateful-components.md#step-3--declare-state-and-write-the-actions) | [examples/interactivity-counter/theme/components/Cart.html](../../examples/interactivity-counter/theme/components/Cart.html) |
| Returning errors (`[state, errors]`) | `brick` | [Step 1 — Declare the rules of a signup form](../guides/forms/validation.md#step-1--declare-the-rules-of-a-signup-form) | [examples/forms-validation-lists/theme/components/Signup.html](../../examples/forms-validation-lists/theme/components/Signup.html) |
| `init:` and `updated:` hooks | `brick` | [Step 3 — Declare state and write the actions](../guides/interactivity/stateful-components.md#step-3--declare-state-and-write-the-actions) | [examples/interactivity-counter/theme/components/Counter.html](../../examples/interactivity-counter/theme/components/Counter.html) |
| Renderless actions and replies (`skip_render`, `reply`) | `brick` | [Step 3 — Declare state and write the actions](../guides/interactivity/stateful-components.md#step-3--declare-state-and-write-the-actions) | [examples/interactivity-counter/theme/components/Counter.html](../../examples/interactivity-counter/theme/components/Counter.html) |
| Streamed action output (`brick.stream`) | `brick` | [Step 4 — Stream text from an action](../guides/interactivity/islands-lazy-and-streaming.md#step-4--stream-text-from-an-action) | [examples/interactivity-lazy-stream/theme/components/Ask.html](../../examples/interactivity-lazy-stream/theme/components/Ask.html) |
| Nested stateful components and `reactive:` props | `brick` | [Step 6 — Keep nested rows and let them follow the parent](../guides/interactivity/stateful-components.md#step-6--keep-nested-rows-and-let-them-follow-the-parent) | [examples/interactivity-nested/theme/components/ContactRow.html](../../examples/interactivity-nested/theme/components/ContactRow.html) |
| Modelable components (`brick: {model:}`) | `brick` | [Step 5 — Nest a component that binds to its parent (Modelable)](../guides/interactivity/stateful-components.md#step-5--nest-a-component-that-binds-to-its-parent-modelable) | [examples/interactivity-counter/theme/components/Stepper.html](../../examples/interactivity-counter/theme/components/Stepper.html) |
| Locked fields (`locked:`) | `brick` | [Step 4 — Decide who may write each field](../guides/interactivity/url-and-server-state.md#step-4--decide-who-may-write-each-field) | [examples/interactivity-url-state/theme/components/RowEditor.html](../../examples/interactivity-url-state/theme/components/RowEditor.html) |
| Closed-by-default state (`LockState`, `writable:`) | `brick` | [Step 4 — Decide who may write each field](../guides/interactivity/url-and-server-state.md#step-4--decide-who-may-write-each-field) | [examples/interactivity-url-state/theme/components/Profile.html](../../examples/interactivity-url-state/theme/components/Profile.html) |
| Temporary fields (`temporary:`) | `brick` | [Step 3 — Put fields in the URL and respond when it changes](../guides/interactivity/url-and-server-state.md#step-3--put-fields-in-the-url-and-respond-when-it-changes) | [examples/interactivity-url-state/theme/components/Catalog.html](../../examples/interactivity-url-state/theme/components/Catalog.html) |
| Remembered fields (`remember:`, `Rememberer`) | `brick` | [Step 3 — Put fields in the URL and respond when it changes](../guides/interactivity/url-and-server-state.md#step-3--put-fields-in-the-url-and-respond-when-it-changes) | [examples/interactivity-url-state/theme/components/Catalog.html](../../examples/interactivity-url-state/theme/components/Catalog.html) |
| URL-synced fields (`query:`) | `brick` | [Step 3 — Put fields in the URL and respond when it changes](../guides/interactivity/url-and-server-state.md#step-3--put-fields-in-the-url-and-respond-when-it-changes) | [examples/interactivity-url-state/theme/components/Catalog.html](../../examples/interactivity-url-state/theme/components/Catalog.html) |
| URL callbacks (`brick: {params:}`, `$params`) | `brick` | [Step 3 — Put fields in the URL and respond when it changes](../guides/interactivity/url-and-server-state.md#step-3--put-fields-in-the-url-and-respond-when-it-changes) | [examples/interactivity-url-state/theme/components/Catalog.html](../../examples/interactivity-url-state/theme/components/Catalog.html) |
| Per-component action logging (`log:`) | `brick` | [Step 5 — Tune what the action log says](../guides/interactivity/events-and-modifiers.md#step-5--tune-what-the-action-log-says) | [examples/interactivity-events-modifiers/theme/components/Ticker.html](../../examples/interactivity-events-modifiers/theme/components/Ticker.html) |
| State in a named store (`store:`, `Options.StateStores`) | `brick` | [Step 1 — Pick a store per component](../guides/production/state-stores.md#step-1--pick-a-store-per-component) | [examples/production-state-stores/theme/components/Cart.html](../../examples/production-state-stores/theme/components/Cart.html) |
| Instance end hook (`InstanceEndHook`) | `brick` | [Step 7 — Look at a live instance and hear when one ends](../guides/interactivity/stateful-components.md#step-7--look-at-a-live-instance-and-hear-when-one-ends) | [examples/interactivity-nested/main.go](../../examples/interactivity-nested/main.go) |
| Instance inspector (`Engine.Instance`, `/_brick/instances`) | `brick` | [Step 4 — Read the toolbar](../guides/site/dev-server.md#step-4--read-the-toolbar) | [examples/interactivity-nested/main.go](../../examples/interactivity-nested/main.go) |

## Bindings and events

| Feature | Package | Guide | Example |
|---|---|---|---|
| `brick:model` (`.lazy`, `.debounce`, `.throttle`) | `runtime` | [Pitfalls](../guides/interactivity/events-and-modifiers.md#pitfalls) | [examples/interactivity-counter/theme/components/Cart.html](../../examples/interactivity-counter/theme/components/Cart.html) |
| Deferred fields (`brick:model.defer`) | `runtime` | [Step 1 — Write the form with deferred fields](../guides/forms/deferred-fields.md#step-1--write-the-form-with-deferred-fields) | [examples/forms-signup/theme/components/Signup.html](../../examples/forms-signup/theme/components/Signup.html) |
| Lists and nested paths (`items.0.qty`) | `brick` | [Step 5 — Bind list rows and choose how to ask before you act](../guides/forms/deferred-fields.md#step-5--bind-list-rows-and-choose-how-to-ask-before-you-act) | [examples/forms-signup/main_test.go](../../examples/forms-signup/main_test.go) |
| `brick:text` and `brick:show` | `runtime` | [Step 1 — Key the rows and protect the widgets](../guides/interactivity/morph-transitions-client-commands.md#step-1--key-the-rows-and-protect-the-widgets) | [examples/interactivity-counter/theme/components/Counter.html](../../examples/interactivity-counter/theme/components/Counter.html) |
| `brick:submit` (with `.lock`) | `runtime` | [Step 1 — Write the form as usual](../guides/forms/without-javascript.md#step-1--write-the-form-as-usual) | [examples/forms-nojs/theme/components/Signup.html](../../examples/forms-nojs/theme/components/Signup.html) |
| `brick:change`, `brick:input`, `brick:focus`, `brick:blur` | `runtime` | [Step 2 — Put the attributes on the elements](../guides/interactivity/events-and-modifiers.md#step-2--put-the-attributes-on-the-elements) | [examples/interactivity-events-modifiers/theme/components/Inbox.html](../../examples/interactivity-events-modifiers/theme/components/Inbox.html) |
| Window focus and blur (`.window`) | `runtime` | [Step 4 — Respond to the window, a held key, a long look and the connection](../guides/interactivity/events-and-modifiers.md#step-4--respond-to-the-window-a-held-key-a-long-look-and-the-connection) | [examples/interactivity-events-modifiers/theme/components/Panel.html](../../examples/interactivity-events-modifiers/theme/components/Panel.html) |
| `brick:keydown` / `brick:keyup` and key combos | `runtime` | [Step 2 — Put the attributes on the elements](../guides/interactivity/events-and-modifiers.md#step-2--put-the-attributes-on-the-elements) | [examples/interactivity-events-modifiers/theme/components/Panel.html](../../examples/interactivity-events-modifiers/theme/components/Panel.html) |
| `.norepeat` on held keys | `runtime` | [Step 4 — Respond to the window, a held key, a long look and the connection](../guides/interactivity/events-and-modifiers.md#step-4--respond-to-the-window-a-held-key-a-long-look-and-the-connection) | [examples/interactivity-events-modifiers/theme/components/Panel.html](../../examples/interactivity-events-modifiers/theme/components/Panel.html) |
| Event modifiers `.once`, `.debounce`, `.throttle` | `runtime` | [Pitfalls](../guides/interactivity/events-and-modifiers.md#pitfalls) | [examples/interactivity-url-state/theme/components/Catalog.html](../../examples/interactivity-url-state/theme/components/Catalog.html) |
| `brick:click.outside` | `runtime` | [Step 2 — Put the attributes on the elements](../guides/interactivity/events-and-modifiers.md#step-2--put-the-attributes-on-the-elements) | [examples/interactivity-client-commands/theme/components/Menu.html](../../examples/interactivity-client-commands/theme/components/Menu.html) |
| `brick:click.details` | `runtime` | [Step 2 — Put the attributes on the elements](../guides/interactivity/events-and-modifiers.md#step-2--put-the-attributes-on-the-elements) | [examples/interactivity-events-modifiers/theme/components/Inbox.html](../../examples/interactivity-events-modifiers/theme/components/Inbox.html) |
| Optimistic updates (`.optimistic`) | `runtime` | [Pitfalls](../guides/interactivity/events-and-modifiers.md#pitfalls) | [examples/ui/demos/RuntimeLike.html](../../examples/ui/demos/RuntimeLike.html) |
| `brick:poll` (`.visible`, `.keep-alive`) | `runtime` | [Pitfalls](../guides/interactivity/events-and-modifiers.md#pitfalls) | [examples/interactivity-events-modifiers/theme/components/Ticker.html](../../examples/interactivity-events-modifiers/theme/components/Ticker.html) |
| `brick:intersect` (`.leave`, `.dwell`, `.margin`) | `runtime` | [Step 4 — Respond to the window, a held key, a long look and the connection](../guides/interactivity/events-and-modifiers.md#step-4--respond-to-the-window-a-held-key-a-long-look-and-the-connection) | [examples/interactivity-events-modifiers/theme/components/Panel.html](../../examples/interactivity-events-modifiers/theme/components/Panel.html) |
| `brick:loading` targets (`.delay`, `.remove`, `.except`) | `runtime` | [Step 1 — Write the form with deferred fields](../guides/forms/deferred-fields.md#step-1--write-the-form-with-deferred-fields) | [examples/forms-signup/theme/components/Signup.html](../../examples/forms-signup/theme/components/Signup.html) |
| `brick:disable-with` | `runtime` | [Step 1 — Write the form with deferred fields](../guides/forms/deferred-fields.md#step-1--write-the-form-with-deferred-fields) | [examples/forms-signup/theme/components/Signup.html](../../examples/forms-signup/theme/components/Signup.html) |
| `brick:dirty` | `runtime` | [Step 1 — Write the form with deferred fields](../guides/forms/deferred-fields.md#step-1--write-the-form-with-deferred-fields) | [examples/forms-signup/theme/components/Signup.html](../../examples/forms-signup/theme/components/Signup.html) |
| `brick:confirm` and `brick:confirm.prompt` | `runtime` | [Step 5 — Bind list rows and choose how to ask before you act](../guides/forms/deferred-fields.md#step-5--bind-list-rows-and-choose-how-to-ask-before-you-act) | [examples/forms-method-csrf/theme/components/PostRow.html](../../examples/forms-method-csrf/theme/components/PostRow.html) |
| `brick:offline` | `runtime` | [Step 4 — Respond to the window, a held key, a long look and the connection](../guides/interactivity/events-and-modifiers.md#step-4--respond-to-the-window-a-held-key-a-long-look-and-the-connection) | [examples/interactivity-events-modifiers/theme/components/Panel.html](../../examples/interactivity-events-modifiers/theme/components/Panel.html) |
| `brick:ref` and `brick.ref` | `runtime` | [Try it](../guides/live/rows-and-regions.md#try-it) | [examples/live-rows-regions/theme/components/Login.html](../../examples/live-rows-regions/theme/components/Login.html) |
| Morph controls (`brick:key`, `brick:ignore`, `brick:replace`, `brick:keep`) | `runtime` | [Step 1 — Key the rows and protect the widgets](../guides/interactivity/morph-transitions-client-commands.md#step-1--key-the-rows-and-protect-the-widgets) | [examples/interactivity-client-commands/theme/components/Todos.html](../../examples/interactivity-client-commands/theme/components/Todos.html) |
| View transitions (`brick:transition`, `brick.transition`) | `runtime` | [Step 1 — Key the rows and protect the widgets](../guides/interactivity/morph-transitions-client-commands.md#step-1--key-the-rows-and-protect-the-widgets) | [examples/interactivity-client-commands/theme/components/Todos.html](../../examples/interactivity-client-commands/theme/components/Todos.html) |
| Enter and leave transitions (`brick:enter`, `brick:leave`) | `runtime` | [Step 1 — Key the rows and protect the widgets](../guides/interactivity/morph-transitions-client-commands.md#step-1--key-the-rows-and-protect-the-widgets) | [examples/interactivity-client-commands/theme/components/Menu.html](../../examples/interactivity-client-commands/theme/components/Menu.html) |
| Client commands (`brick:js.<event>`) | `runtime` | [Step 3 — Open a menu with client commands](../guides/interactivity/morph-transitions-client-commands.md#step-3--open-a-menu-with-client-commands) | [examples/interactivity-client-commands/theme/components/Menu.html](../../examples/interactivity-client-commands/theme/components/Menu.html) |

## Forms, validation and uploads

| Feature | Package | Guide | Example |
|---|---|---|---|
| Forms without JavaScript (progressive `brick:submit`) | `brick` | [Step 1 — Write the form as usual](../guides/forms/without-javascript.md#step-1--write-the-form-as-usual) | [examples/forms-nojs/theme/components/Signup.html](../../examples/forms-nojs/theme/components/Signup.html) |
| Form posts in your own router (`Engine.FormPost`) | `brick` | [Step 2 — Answer the post with `Engine.FormPost`](../guides/forms/without-javascript.md#step-2--answer-the-post-with-engineformpost) | [examples/forms-nojs/main.go](../../examples/forms-nojs/main.go) |
| Form objects (`.Form`) | `brick` | [Step 2 — Answer the post with `Engine.FormPost`](../guides/forms/without-javascript.md#step-2--answer-the-post-with-engineformpost) | [examples/forms-validation-lists/theme/components/Todos.html](../../examples/forms-validation-lists/theme/components/Todos.html) |
| Declared rules (`rules:`, `messages:`) | `brick` | [Try it](../guides/forms/validation.md#try-it) | [examples/forms-validation-lists/theme/components/Signup.html](../../examples/forms-validation-lists/theme/components/Signup.html) |
| Wildcard rules on list items (`todos.*.title`) | `brick` | [Step 2 — Check every row of a list with `*` and render it with `.Form`](../guides/forms/validation.md#step-2--check-every-row-of-a-list-with--and-render-it-with-form) | [examples/forms-validation-lists/theme/components/Todos.html](../../examples/forms-validation-lists/theme/components/Todos.html) |
| `brick.validate` / `brick.untouch` and touched fields | `brick` | [Step 1 — Declare the rules of a signup form](../guides/forms/validation.md#step-1--declare-the-rules-of-a-signup-form) | [examples/forms-validation-lists/theme/components/Signup.html](../../examples/forms-validation-lists/theme/components/Signup.html) |
| Custom validator (`Options.Validator`) | `validate` | [Step 3 — Add your own rule with `Options.Validator`](../guides/forms/validation.md#step-3--add-your-own-rule-with-optionsvalidator) | [examples/forms-validation-lists/main.go](../../examples/forms-validation-lists/main.go) |
| Uploads (`uploads:`, `brick:upload`, `UploadSink`) | `brick` | [Step 1 — Declare the upload fields and the inputs](../guides/forms/uploads.md#step-1--declare-the-upload-fields-and-the-inputs) | [examples/forms-uploads/theme/components/Profile.html](../../examples/forms-uploads/theme/components/Profile.html) |
| Upload progress (`brick:upload-progress`) | `runtime` | [Step 1 — Declare the upload fields and the inputs](../guides/forms/uploads.md#step-1--declare-the-upload-fields-and-the-inputs) | [examples/forms-uploads/theme/components/Profile.html](../../examples/forms-uploads/theme/components/Profile.html) |
| Presigned uploads (`UploadPresigner`) | `brick` | [Step 6 — Let the browser upload straight to storage](../guides/forms/uploads.md#step-6--let-the-browser-upload-straight-to-storage) | [examples/forms-uploads/presign.go](../../examples/forms-uploads/presign.go) |
| Per-file upload check (`Options.UploadCheck`) | `brick` | [Step 2 — Give the engine a sink and your own check](../guides/forms/uploads.md#step-2--give-the-engine-a-sink-and-your-own-check) | [examples/forms-uploads/main.go](../../examples/forms-uploads/main.go) |
| Disk upload sink (`upload/disk`) | `upload/disk` | [Step 2 — Give the engine a sink and your own check](../guides/forms/uploads.md#step-2--give-the-engine-a-sink-and-your-own-check) | [examples/forms-uploads/main.go](../../examples/forms-uploads/main.go) |
| Uploads without JavaScript | `brick` | [Step 5 — Upload with JavaScript off](../guides/forms/uploads.md#step-5--upload-with-javascript-off) | [examples/forms-uploads/main.go](../../examples/forms-uploads/main.go) |

## Effects and events

| Feature | Package | Guide | Example |
|---|---|---|---|
| Effects: `flash`, `redirect`, `navigate`, `title`, `download`, `url` | `brick` | [Step 2 — Record effects in the action](../guides/live/effects.md#step-2--record-effects-in-the-action) | [examples/live-rows-regions/theme/components/Login.html](../../examples/live-rows-regions/theme/components/Login.html) |
| `brick.clear_flash` | `brick` | [Step 2 — Record effects in the action](../guides/live/effects.md#step-2--record-effects-in-the-action) | [examples/live-effects-events/theme/components/Inbox.html](../../examples/live-effects-events/theme/components/Inbox.html) |
| Custom effects from Go (`EffectsFrom`, `x-*` kinds) | `brick` | [Step 4 — Record an effect from Go with `EffectsFrom`](../guides/live/effects.md#step-4--record-an-effect-from-go-with-effectsfrom) | [examples/live-effects-events/main.go](../../examples/live-effects-events/main.go) |
| Events between components (`brick: {on:}`, `brick:on:`) | `brick` | [Step 3 — Let a child tell its parent, from an attribute](../guides/live/events.md#step-3--let-a-child-tell-its-parent-from-an-attribute) | [examples/live-effects-events/theme/components/Thread.html](../../examples/live-effects-events/theme/components/Thread.html) |
| `brick.dispatch` / `$dispatch` / `$parent` | `brick` | [Step 4 — Leave browser-only calls inert](../guides/forms/without-javascript.md#step-4--leave-browser-only-calls-inert) | [examples/forms-nojs/theme/components/Signup.html](../../examples/forms-nojs/theme/components/Signup.html) |
| Row streams (`brick.rows`) | `brick` | [Step 1 — A list fed row by row](../guides/live/rows-and-regions.md#step-1--a-list-fed-row-by-row) | [examples/live-rows-regions/theme/components/Chat.html](../../examples/live-rows-regions/theme/components/Chat.html) |
| Regions (`brick.region`) | `brick` | [Step 3 — A "load more" list that keeps its earlier pages](../guides/live/rows-and-regions.md#step-3--a-load-more-list-that-keeps-its-earlier-pages) | [examples/live-rows-regions/theme/components/Feed.html](../../examples/live-rows-regions/theme/components/Feed.html) |
| Native submit hand-off (`brick.submit`) | `brick` | [Step 4 — Hand a form to the browser's own submit](../guides/live/rows-and-regions.md#step-4--hand-a-form-to-the-browsers-own-submit) | [examples/live-rows-regions/theme/components/Login.html](../../examples/live-rows-regions/theme/components/Login.html) |
| Effect size and redirect hosts (`MaxEffectBytes`, `RedirectHosts`) | `brick` | [Step 5 — Limit where redirects may go and how big effects may be](../guides/live/effects.md#step-5--limit-where-redirects-may-go-and-how-big-effects-may-be) | [examples/live-effects-events/main.go](../../examples/live-effects-events/main.go) |

## Push, presence and jobs

| Feature | Package | Guide | Example |
|---|---|---|---|
| Server push (`listen:`, `brick.publish`, `Options.Push`) | `brick` | [Step 1 — Listen to a topic](../guides/live/push-and-presence.md#step-1--listen-to-a-topic) | [examples/live-push-presence/theme/components/Room.html](../../examples/live-push-presence/theme/components/Room.html) |
| `Engine.Publish` from Go | `brick` | [Step 3 — Publish from Go code too](../guides/live/push-and-presence.md#step-3--publish-from-go-code-too) | [examples/live-push-presence/main.go](../../examples/live-push-presence/main.go) |
| Push options and topic authorization (`AuthorizeTopic`) | `brick` | [Step 2 — Turn push on and decide who may listen](../guides/live/push-and-presence.md#step-2--turn-push-on-and-decide-who-may-listen) | [examples/live-push-presence/demo.go](../../examples/live-push-presence/demo.go) |
| Presence (`listen: {presence:}`, `.Presence`) | `brick` | [Step 2 — Turn push on and decide who may listen](../guides/live/push-and-presence.md#step-2--turn-push-on-and-decide-who-may-listen) | [examples/live-push-presence/theme/components/Room.html](../../examples/live-push-presence/theme/components/Room.html) |
| Background jobs (`brick.start_job`, `Options.Jobs`) | `brick` | [Step 1 — Start the job from an action](../guides/live/background-jobs.md#step-1--start-the-job-from-an-action) | [examples/live-jobs/theme/components/Report.html](../../examples/live-jobs/theme/components/Report.html) |
| Cancel a job (`brick.cancel_job`, `Engine.CancelJob`) | `brick` | [Step 1 — Start the job from an action](../guides/live/background-jobs.md#step-1--start-the-job-from-an-action) | [examples/live-jobs/theme/components/Report.html](../../examples/live-jobs/theme/components/Report.html) |
| Custom job queue (`JobQueue`, `JobCanceller`) | `brick` | [Step 2 — Register the job and record its outcome](../guides/live/background-jobs.md#step-2--register-the-job-and-record-its-outcome) | [examples/live-jobs/queue.go](../../examples/live-jobs/queue.go) |

## Browser runtime API

| Feature | Package | Guide | Example |
|---|---|---|---|
| `brick.hook` (request, response, morph, morphed, ready) | `runtime` | [Step 2 — Respond to runtime events](../guides/live/browser-runtime.md#step-2--respond-to-runtime-events) | [examples/live-effects-events/theme/components/Inbox.html](../../examples/live-effects-events/theme/components/Inbox.html) |
| `brick.on` events (`action-error`, `action-retry`, …) | `runtime` | [Step 2 — Respond to runtime events](../guides/live/browser-runtime.md#step-2--respond-to-runtime-events) | [examples/live-effects-events/theme/components/Inbox.html](../../examples/live-effects-events/theme/components/Inbox.html) |
| `brick.watch` (follow a signal) | `runtime` | [Step 7 — Find components and follow their signals](../guides/live/browser-runtime.md#step-7--find-components-and-follow-their-signals) | [examples/live-effects-events/theme/components/Badge.html](../../examples/live-effects-events/theme/components/Badge.html) |
| `brick.errors(el)` | `runtime` | [Step 5 — Read the errors from browser code with `brick.errors`](../guides/forms/validation.md#step-5--read-the-errors-from-browser-code-with-brickerrors) | [examples/forms-validation-lists/theme/components/Todos.html](../../examples/forms-validation-lists/theme/components/Todos.html) |
| `brick.js(el, commands)` | `runtime` | [Step 3 — Open a menu with client commands](../guides/interactivity/morph-transitions-client-commands.md#step-3--open-a-menu-with-client-commands) | [examples/interactivity-client-commands/theme/components/Menu.html](../../examples/interactivity-client-commands/theme/components/Menu.html) |
| `brick.find` / `brick.all` | `runtime` | [Step 7 — Find components and follow their signals](../guides/live/browser-runtime.md#step-7--find-components-and-follow-their-signals) | [examples/live-effects-events/theme/components/Badge.html](../../examples/live-effects-events/theme/components/Badge.html) |
| `brick.action` (call from code) | `runtime` | [Step 4 — Call a renderless action from code](../guides/interactivity/stateful-components.md#step-4--call-a-renderless-action-from-code) | [examples/ui/apps/dashboard/DashBoard.html](../../examples/ui/apps/dashboard/DashBoard.html) |
| `brick.effect(kind, fn)` | `runtime` | [Step 2 — Respond to runtime events](../guides/live/browser-runtime.md#step-2--respond-to-runtime-events) | [examples/live-effects-events/theme/components/Inbox.html](../../examples/live-effects-events/theme/components/Inbox.html) |
| `brick.confirmWith` / `brick.promptWith` | `runtime` | [Step 5 — Bind list rows and choose how to ask before you act](../guides/forms/deferred-fields.md#step-5--bind-list-rows-and-choose-how-to-ask-before-you-act) | [examples/forms-signup/theme/components/Cart.html](../../examples/forms-signup/theme/components/Cart.html) |
| `brick.reloadComponent` | `runtime` | [Step 2 — Respond to runtime events](../guides/live/browser-runtime.md#step-2--respond-to-runtime-events) | [examples/live-effects-events/theme/components/Inbox.html](../../examples/live-effects-events/theme/components/Inbox.html) |
| `brick.upload(el, field, files)` | `runtime` | [Step 7 — Send files from code with `brick.upload` and hear the events](../guides/forms/uploads.md#step-7--send-files-from-code-with-brickupload-and-hear-the-events) | [examples/forms-uploads/theme/components/Profile.html](../../examples/forms-uploads/theme/components/Profile.html) |
| `brick.debug` / `brick.latency` | `runtime` | [Step 6 — Debug from the console](../guides/live/browser-runtime.md#step-6--debug-from-the-console) | — |
| `brick.methodFormWith` | `runtime` | [Step 4 — Add your CSRF token with `brick.methodFormWith`](../guides/forms/method-links.md#step-4--add-your-csrf-token-with-brickmethodformwith) | [examples/forms-method-csrf/theme/components/PostRow.html](../../examples/forms-method-csrf/theme/components/PostRow.html) |
| Retry on 503/429 and `Options.Runtime` (`<meta name="brick-config">`) | `brick` | [Step 5 — Tune retries and other runtime settings from Go](../guides/live/browser-runtime.md#step-5--tune-retries-and-other-runtime-settings-from-go) | [examples/live-effects-events/main.go](../../examples/live-effects-events/main.go) |

## State storage and security

| Feature | Package | Guide | Example |
|---|---|---|---|
| Sealed state (`NewSealedStates`, `BRICK_SECRET`) | `brick` | [Try it](../guides/production/sealed-state.md#try-it) | [examples/production-sealed-state/main.go](../../examples/production-sealed-state/main.go) |
| Key rings (`StaticKeys`, `KeysFromEnv`, `brick keygen`) | `brick` | [Step 2 — Read the key ring and seal with it](../guides/production/sealed-state.md#step-2--read-the-key-ring-and-seal-with-it) | [examples/production-sealed-state/main.go](../../examples/production-sealed-state/main.go) |
| Pluggable stores (`BytesStore`, `brick.Bytes`, `Codec`, `Sealer`) | `brick` | [Step 2 — Write a `BytesStore`](../guides/production/state-stores.md#step-2--write-a-bytesstore) | [examples/production-state-stores/main.go](../../examples/production-state-stores/main.go) |
| Memory store (`MemoryStates`) and pruning | `brick` | [Step 3 — Mount the engine](../guides/forms/deferred-fields.md#step-3--mount-the-engine) | [examples/forms-signup/main.go](../../examples/forms-signup/main.go) |
| Session binding and sign-out (`Sessions`, `RevokeSession`) | `brick` | [Step 3 — End a session's state on sign-out](../guides/production/sessions.md#step-3--end-a-sessions-state-on-sign-out) | [examples/production-sealed-state/main.go](../../examples/production-sealed-state/main.go) |
| Action middleware (`ActionMiddleware`, `actionmw`) | `actionmw` | [Step 2 — Build the chain](../guides/production/action-middleware.md#step-2--build-the-chain) | [examples/production-middleware/main.go](../../examples/production-middleware/main.go) |
| Rate limits (`actionmw.RateLimit`) | `actionmw` | [Step 2 — Build the chain](../guides/production/action-middleware.md#step-2--build-the-chain) | [examples/production-middleware/main.go](../../examples/production-middleware/main.go) |
| Request values in actions (`Options.ActionRequest`) | `brick` | [Step 5 — Call an action from Go](../guides/production/host-api.md#step-5--call-an-action-from-go) | [examples/production-middleware/main.go](../../examples/production-middleware/main.go) |
| Action error envelope and size cap (`ActionErrors`, `MaxActionBytes`) | `brick` | [Step 2 — Lower the render limits for data you do not control](../guides/production/limits-determinism.md#step-2--lower-the-render-limits-for-data-you-do-not-control) | [examples/production-middleware/main.go](../../examples/production-middleware/main.go) |
| Sanitizing untrusted HTML (`sanitize`, `Options.Sanitizer`) | `brick` | [Use it](../../contrib/sanitize-bluemonday/README.md#use-it) | [contrib/sanitize-bluemonday/example_test.go](../../contrib/sanitize-bluemonday/example_test.go) |

## Site layer

| Feature | Package | Guide | Example |
|---|---|---|---|
| Routes from files and `.Site.Pages` | `site` | [Step 4 — List the posts from `.Site.Pages`](../guides/site/pages-and-routes.md#step-4--list-the-posts-from-sitepages) | [examples/site-blog/theme/content/blog/index.html](../../examples/site-blog/theme/content/blog/index.html) |
| Markdown pages | `site` | [Step 1 — Register the backend with heading ids](../guides/site/markdown-and-headings.md#step-1--register-the-backend-with-heading-ids) | [examples/site-blog/theme/content/blog/tide-tables.md](../../examples/site-blog/theme/content/blog/tide-tables.md) |
| Content collections (required fields, `Schema`) | `site` | [Step 2 — Declare the component's props](../guides/components/props.md#step-2--declare-the-components-props) | [examples/site-blog/theme/content/blog/_collection.yaml](../../examples/site-blog/theme/content/blog/_collection.yaml) |
| Drafts | `site` | [Step 2 — Mark a post as a draft](../guides/site/collections-and-drafts.md#step-2--mark-a-post-as-a-draft) | [examples/site-blog/theme/content/blog/half-written.html](../../examples/site-blog/theme/content/blog/half-written.html) |
| Dynamic routes (`[slug]`, `[...rest]`, `paths:`) | `site` | [Step 2 — List the values and load one](../guides/site/dynamic-routes.md#step-2--list-the-values-and-load-one) | [examples/site-dynamic-routes/theme/content/docs/\[...path\].html](../../examples/site-dynamic-routes/theme/content/docs/%5B...path%5D.html) |
| Per-request pages (`per_request:`) | `site` | [Step 3 — Render a whole page per visitor](../guides/site/protected-pages.md#step-3--render-a-whole-page-per-visitor) | [examples/site-protected/theme/content/dashboard.html](../../examples/site-protected/theme/content/dashboard.html) |
| Protected pages, redirects and status codes | `site` | [Protect a page, redirect, and answer with a status](../guide.md#protect-a-page-redirect-and-answer-with-a-status) | [examples/site-dynamic-routes/theme/scripts/posts.star](../../examples/site-dynamic-routes/theme/scripts/posts.star) |
| `404.html` and `error.html` | `site` | [Step 1 — Lay out the files](../guides/site/pages-and-routes.md#step-1--lay-out-the-files) | [examples/site-blog/theme/content/404.html](../../examples/site-blog/theme/content/404.html) |
| Output pages (`feed.xml.html`, `robots.txt.html`) | `site` | [Step 4 — One feed per tag](../guides/site/feeds-and-sitemap.md#step-4--one-feed-per-tag) | [examples/site-feeds-sitemap/main.go](../../examples/site-feeds-sitemap/main.go) |
| Sitemap (`sitemap:`) | `site` | [The sitemap](../guide.md#the-sitemap) | [examples/site-feeds-sitemap/theme/brick.yaml](../../examples/site-feeds-sitemap/theme/brick.yaml) |
| Page headings and tables of contents (`.Page.Headings`) | `site` | [Step 3 — Print the headings in the layout](../guides/site/markdown-and-headings.md#step-3--print-the-headings-in-the-layout) | [examples/site-blog/theme/layouts/doc.html](../../examples/site-blog/theme/layouts/doc.html) |
| Route metadata (`Site.Routes`, `Route.Meta`) | `site` | [Step 6 — Build a menu from route metadata](../guides/site/pages-and-routes.md#step-6--build-a-menu-from-route-metadata) | [examples/site-i18n/main_test.go](../../examples/site-i18n/main_test.go) |
| Render cache invalidated by read files | `site` | [Step 1 — Tag the pages that read outside data](../guides/site/render-cache.md#step-1--tag-the-pages-that-read-outside-data) | [examples/site-blog/theme/content/blog/index.html](../../examples/site-blog/theme/content/blog/index.html) |
| Static export (`Site.Export`, `brick build`) | `site` | [brick build](cli.md#brick-build) | [examples/site-blog/README.md](../../examples/site-blog/README.md) |
| Dev server, live reload and playground | `site` | [Step 3 — Try a component on its own](../guides/site/dev-server.md#step-3--try-a-component-on-its-own) | [examples/site-tailwind/main.go](../../examples/site-tailwind/main.go) |
| Dev toolbar and render stats (`WithStats`) | `site` | [Step 5 — Read the same numbers from Go](../guides/site/dev-server.md#step-5--read-the-same-numbers-from-go) | [examples/components-options/main.go](../../examples/components-options/main.go) |
| Hosting under a path (`BasePath`, `site.Mount`) | `site` | [Step 2 — Give each site its base, and keep the sibling's links](../guides/site/subpath-and-mount.md#step-2--give-each-site-its-base-and-keep-the-siblings-links) | [examples/site-subpath/main.go](../../examples/site-subpath/main.go) |
| File watcher (`watch`) | `watch` | [Step 2 — Drop pages when something changes](../guides/site/render-cache.md#step-2--drop-pages-when-something-changes) | [examples/site-blog/ops.go](../../examples/site-blog/ops.go) |
| Images (`{{ image }}`, srcset, formats) | `brick` | [Step 1 — Ask for the copies](../guides/site/images.md#step-1--ask-for-the-copies) | [examples/site-images/main.go](../../examples/site-images/main.go) |
| Tailwind (`tailwind`) | `tailwind` | [Step 1 — Name the input and output](../guides/site/tailwind.md#step-1--name-the-input-and-output) | [examples/site-tailwind/theme/styles/app.css](../../examples/site-tailwind/theme/styles/app.css) |

## Libraries, remote and kit

| Feature | Package | Guide | Example |
|---|---|---|---|
| Component libraries (`libraries:`, `brick-library.yaml`) | `brick` | [If X then Y](../choosing/component-sources.md#if-x-then-y) | [examples/components-libraries/theme/brick.yaml](../../examples/components-libraries/theme/brick.yaml) |
| Library prefixes and subsets (`prefix:`, `only:`) | `brick` | [If X then Y](../choosing/component-sources.md#if-x-then-y) | [examples/site/brick.yaml](../../examples/site/brick.yaml) |
| Shipping a library as a Go package (`brick.Library`, `Config.Apply`) | `brick` | [Step 3 — Apply the config in Go](../guides/components/libraries.md#step-3--apply-the-config-in-go) | [kit/kit.go](../../kit/kit.go) |
| Remote imports (`use: github.com/…@v1`), `brick vendor`, `brick.lock` | `remote` | [brick vendor](cli.md#brick-vendor) | [examples/components-libraries/theme/brick.yaml](../../examples/components-libraries/theme/brick.yaml) |
| The kit (`brick-kit`, 133 components) | `kit` | [If X then Y](../choosing/component-sources.md#if-x-then-y) | [examples/components-libraries/theme/brick.yaml](../../examples/components-libraries/theme/brick.yaml) |
| Kit theming (`--bk-*` tokens, accents, dark mode) | `kit` | [Step 5 — Use the components and the kit's theme](../guides/components/libraries.md#step-5--use-the-components-and-the-kits-theme) | [examples/components-libraries/theme/static/theme.css](../../examples/components-libraries/theme/static/theme.css) |

## CLI

| Feature | Package | Guide | Example |
|---|---|---|---|
| `brick check` | `cmd/brick` | [brick check](cli.md#brick-check) | [examples/components-errors/main.go](../../examples/components-errors/main.go) |
| `brick routes` | `cmd/brick` | [brick routes](cli.md#brick-routes) | [examples/site-blog/README.md](../../examples/site-blog/README.md) |
| `brick build` | `cmd/brick` | [brick build](cli.md#brick-build) | [examples/site-dynamic-routes/theme/scripts/posts.star](../../examples/site-dynamic-routes/theme/scripts/posts.star) |
| `brick serve` | `cmd/brick` | [brick serve](cli.md#brick-serve) | [examples/site-blog/README.md](../../examples/site-blog/README.md) |
| `brick vendor` | `cmd/brick` | [brick vendor](cli.md#brick-vendor) | [examples/components-libraries/theme/brick.yaml](../../examples/components-libraries/theme/brick.yaml) |
| `brick docs` | `cmd/brick` | [brick docs](cli.md#brick-docs) | [examples/components-options/main.go](../../examples/components-options/main.go) |
| `brick new site / component / page` | `cmd/brick` | [brick new](cli.md#brick-new) | [examples/ui/demos/TerminalMockupBasic.html](../../examples/ui/demos/TerminalMockupBasic.html) |
| `brick fmt` and the `fmt:` section | `cmd/brick` | [Step 1 — See what would change](../guides/tooling/fmt.md#step-1--see-what-would-change) | [examples/tooling-cli/main.go](../../examples/tooling-cli/main.go) |
| `brick keygen` | `cmd/brick` | [Try it](../guides/production/sealed-state.md#try-it) | [examples/production-sealed-state/main.go](../../examples/production-sealed-state/main.go) |
| `brick schema` (frontmatter JSON Schema) | `cmd/brick` | [brick schema](cli.md#brick-schema) | [examples/components-frontmatter/main.go](../../examples/components-frontmatter/main.go) |
| `brick version` | `cmd/brick` | [brick version](cli.md#brick-version) | [examples/tooling-cli/main.go](../../examples/tooling-cli/main.go) |
| `host:` in brick.yaml (checking a site whose host supplies capabilities) | `cmd/brick` | [brick check](cli.md#brick-check) | [examples/site/brick.yaml](../../examples/site/brick.yaml) |

## Editors

| Feature | Package | Guide | Example |
|---|---|---|---|
| `brick lsp` (diagnostics, completion, hover, definition, references, rename, formatting) | `cmd/brick` | [Try it](../guides/tooling/editors.md#try-it) | [examples/ui/README.md](../../examples/ui/README.md) |
| VS Code extension (`editors/vscode`) | `editors/vscode` | [Step 1 — Install the VS Code extension](../guides/tooling/editors.md#step-1--install-the-vs-code-extension) | [editors/vscode/README.md](../../editors/vscode/README.md) |

## Testing

| Feature | Package | Guide | Example |
|---|---|---|---|
| `bricktest` theme tests and golden files | `bricktest` | [Step 2 — Render the page once with `Live`](../guides/tooling/bricktest.md#step-2--render-the-page-once-with-live) | [examples/tooling-bricktest/main_test.go](../../examples/tooling-bricktest/main_test.go) |
| `bricktest.Live`: call actions, deliver events, assert effects | `bricktest` | [Step 5 — Tune what the action log says](../guides/interactivity/events-and-modifiers.md#step-5--tune-what-the-action-log-says) | [examples/interactivity-counter/main_test.go](../../examples/interactivity-counter/main_test.go) |
| Drivers: `Click`, `Submit`, `Change`, `Keydown`, `Intersect`, … | `bricktest` | [Step 3 — Drive it from a test](../guides/interactivity/events-and-modifiers.md#step-3--drive-it-from-a-test) | [examples/interactivity-counter/main_test.go](../../examples/interactivity-counter/main_test.go) |
| State assertions (`AssertSet`, `AssertErrors`, `View`) | `bricktest` | [Step 3 — Let a child tell its parent, from an attribute](../guides/live/events.md#step-3--let-a-child-tell-its-parent-from-an-attribute) | [examples/live-effects-events/main_test.go](../../examples/live-effects-events/main_test.go) |
| View log (`WithViewLog`, `AssertViewHas`) | `bricktest` | [Step 6 — Read the view log](../guides/tooling/bricktest.md#step-6--read-the-view-log) | [examples/tooling-bricktest/main_test.go](../../examples/tooling-bricktest/main_test.go) |
| Upload tests (`MemoryUploads`, `LiveRoot.Upload`) | `bricktest` | [Step 2 — Render the page once with `Live`](../guides/tooling/bricktest.md#step-2--render-the-page-once-with-live) | [examples/tooling-bricktest/main_test.go](../../examples/tooling-bricktest/main_test.go) |
| Store conformance suite (`statetest`) | `statetest` | [Step 2 — Run the suite](../guides/tooling/statetest.md#step-2--run-the-suite) | [examples/tooling-statetest/main_test.go](../../examples/tooling-statetest/main_test.go) |

## Go host API

| Feature | Package | Guide | Example |
|---|---|---|---|
| `brick.New` and `Engine.Render` | `brick` | [Step 1 — Build one engine](../guides/production/host-api.md#step-1--build-one-engine) | [examples/production-sealed-state/main.go](../../examples/production-sealed-state/main.go) |
| `RenderComponent` (one component from Go) | `brick` | [Step 2 — Render a page, or one component](../guides/production/host-api.md#step-2--render-a-page-or-one-component) | [examples/production-host-api/main.go](../../examples/production-host-api/main.go) |
| `ActionHandler` / `IslandHandler` / `UploadHandler` / `EventsHandler` | `brick` | [Step 3 — Mount the engine](../guides/forms/deferred-fields.md#step-3--mount-the-engine) | [examples/forms-signup/main.go](../../examples/forms-signup/main.go) |
| `Engine.RunAction` / `RunUpload` without HTTP | `brick` | [Step 5 — Call an action from Go](../guides/production/host-api.md#step-5--call-an-action-from-go) | [examples/production-host-api/main.go](../../examples/production-host-api/main.go) |
| `Options.FS` and path confinement (`~/`) | `brick` | [Step 1 — Ask for the copies](../guides/site/images.md#step-1--ask-for-the-copies) | [examples/engineonly/main.go](../../examples/engineonly/main.go) |
| Determinism and concurrency (same input, same bytes) | `brick` | [Step 3 — Rely on same input, same bytes](../guides/production/limits-determinism.md#step-3--rely-on-same-input-same-bytes) | [examples/production-host-api/main.go](../../examples/production-host-api/main.go) |

## Contrib modules

| Feature | Package | Guide | Example |
|---|---|---|---|
| `contrib/script-starlark` | `contrib/script-starlark` | [Step 3 — Hear every error in Go](../guides/components/errors.md#step-3--hear-every-error-in-go) | [examples/components-errors/main.go](../../examples/components-errors/main.go) |
| `contrib/markdown-goldmark` (heading ids, highlighting) | `contrib/markdown-goldmark` | [Step 3 — Choose when drafts show](../guides/site/collections-and-drafts.md#step-3--choose-when-drafts-show) | [examples/site-blog/main.go](../../examples/site-blog/main.go) |
| `contrib/sanitize-bluemonday` | `contrib/sanitize-bluemonday` | [Use it](../../contrib/sanitize-bluemonday/README.md#use-it) | [contrib/sanitize-bluemonday/example_test.go](../../contrib/sanitize-bluemonday/example_test.go) |
| `contrib/assets-http` | `contrib/assets-http` | [Step 3 — Mount the engine](../guides/forms/deferred-fields.md#step-3--mount-the-engine) | [examples/forms-signup/main.go](../../examples/forms-signup/main.go) |
| `contrib/image-webp` | `contrib/image-webp` | [Step 2 — Register the WebP encoder](../guides/site/images.md#step-2--register-the-webp-encoder) | [examples/site-images/main.go](../../examples/site-images/main.go) |
| `contrib/states-redis` | `contrib/states-redis` | [Redis and SQL](../guides/production/state-stores.md#redis-and-sql) | [contrib/states-redis/example_test.go](../../contrib/states-redis/example_test.go) |
| `contrib/states-sql` | `contrib/states-sql` | [Redis and SQL](../guides/production/state-stores.md#redis-and-sql) | [contrib/states-sql/example_test.go](../../contrib/states-sql/example_test.go) |
| `contrib/push-redis` | `contrib/push-redis` | [Step 5 — Run more than one server](../guides/live/push-and-presence.md#step-5--run-more-than-one-server) | [contrib/push-redis/example_test.go](../../contrib/push-redis/example_test.go) |
| `contrib/ratelimit-redis` | `contrib/ratelimit-redis` | [Rate limits across servers](../guides/production/action-middleware.md#rate-limits-across-servers) | [contrib/ratelimit-redis/example_test.go](../../contrib/ratelimit-redis/example_test.go) |
| `contrib/actions-otel` | `contrib/actions-otel` | [Traces and metrics](../guides/production/action-middleware.md#traces-and-metrics) | [contrib/actions-otel/example_test.go](../../contrib/actions-otel/example_test.go) |
| `contrib/upload-s3` | `contrib/upload-s3` | [A complete example](../../contrib/upload-s3/README.md#a-complete-example) | [contrib/upload-s3/example_test.go](../../contrib/upload-s3/example_test.go) |
| `contrib/keys-awskms` | `contrib/keys-awskms` | [Keys from a KMS](../guides/production/sealed-state.md#keys-from-a-kms) | [contrib/keys-awskms/example_test.go](../../contrib/keys-awskms/example_test.go) |
| `contrib/keys-vault` | `contrib/keys-vault` | [Keys from a KMS](../guides/production/sealed-state.md#keys-from-a-kms) | [contrib/keys-vault/example_test.go](../../contrib/keys-vault/example_test.go) |
| `contrib/keys-gcpkms` | `contrib/keys-gcpkms` | [Keys from a KMS](../guides/production/sealed-state.md#keys-from-a-kms) | [contrib/keys-gcpkms/example_test.go](../../contrib/keys-gcpkms/example_test.go) |
