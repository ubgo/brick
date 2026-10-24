<p align="center"><img src="brand/png/brick-icon-512.png" alt="brick logo: a running-bond brick wall with one amber brick" width="72" height="72"></p>

<h1 align="center">brick</h1>

<p align="center"><strong>Server-rendered HTML components for Go, on <code>html/template</code>. No build step, no new template language, no JavaScript framework.</strong></p>

<div align="center">

[![License](https://img.shields.io/badge/license-Apache--2.0-blue)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.24%2B-00ADD8?logo=go&logoColor=white)](./go.mod)
[![Dependencies](https://img.shields.io/badge/core%20dependencies-1%20(YAML)-brightgreen)](./go.mod)
[![Build step](https://img.shields.io/badge/build%20step-none-brightgreen)](#why-brick)
[![UI kit](https://img.shields.io/badge/UI%20kit-133%20components-4f46e5)](./kit)
[![Demo apps](https://img.shields.io/badge/demo%20apps-12-4f46e5)](./examples/ui)

</div>

brick is a component-based HTML render engine and site layer for Go, built on the standard library's `html/template`. Pages, layouts and components are `.html` files with optional YAML frontmatter, and components are tags (`<Card>` or `<x-card>`) that resolve to files. Every `{{ }}` is Go's own: brick adds a **calling convention**, never a new template language, and `html/template` still does all the escaping. On top of the engine it adds server-side rendering for whole sites (routes, markdown pages, a render cache, live reload, static export), islands, server actions and two-way bindings without a client framework, sandboxed Starlark scripts, and a UI kit of 133 components with twelve demo apps. It is for Go developers who want components, server-side rendering and interactivity with no bundler.

```html
<!-- components/Card.html -->
<div class="card"><h3>{{ .Props.title }}</h3>{{ children }}</div>

<!-- content/about.html -->
---
title: About
---
<h1>{{ .Title }}</h1>
{{ range .Props.posts }}<Card title="{{ .Title }}"><p>{{ .Summary }}</p></Card>{{ end }}
```

```go
eng, err := brick.New(brick.Options{FS: os.DirFS("site")})
html, err := eng.Render(ctx, "content/about.html", map[string]any{"posts": posts})
```

- Output is deterministic and an `*Engine` is safe for concurrent renders.

## Start here

- **New to brick?** [Getting started](docs/getting-started.md) takes ten minutes: install the `brick` command, then a first page, a component and a button that changes something on the server.
- **Learn it in order:** the [learning path](docs/README.md) has twelve steps from first use to a production setup, each with a guide and a runnable example.
- **Facing a decision?** The [choosing guides](docs/choosing/README.md) compare the options: [state store](docs/choosing/state-store.md), [interactivity](docs/choosing/interactivity.md), [forms](docs/choosing/forms.md), [rendering and hosting](docs/choosing/rendering-and-hosting.md) and [where components come from](docs/choosing/component-sources.md).
- **Looking for a feature?** The [feature index](docs/reference/features.md) lists every one with its guide and an example that uses it.

![brick architecture: browser, HTTP handlers, engine and storage, with plugins and operations](docs/assets/architecture.svg)

## Contents

- [Why brick?](#why-brick) · [Install](#install)
- Engine: [Files](#files) · [Components and props](#components-and-props) · [Children, nodes and slots](#children-nodes-and-slots) · [Layouts](#layouts) · [Data and scope](#data-and-scope) · [Context](#context-ctx) · [Translation](#translation) · [Scripts and loaders](#scripts-and-loaders) · [Boundaries and dynamic components](#boundaries-and-dynamic-components)
- Interactivity: [Actions](#actions--server-state-without-writing-an-api) · [Navigation without reloads](#navigation-without-reloads) · [Islands](#islands--per-request-parts-of-a-cached-page) · [Client code, styles and CSP](#client-code-styles-and-csp)
- Reference: [Errors](#errors) · [Checking and testing](#checking-and-testing) · [Security model](#security-model) · [Built-ins](#built-in-functions) · [Options](#options)
- Beyond the engine: [A whole site](#a-whole-site--site-and-the-brick-command) · [Remote components](#remote-components) · [Component libraries](#component-libraries) · [A component kit](#a-component-kit)
- [Documentation](#documentation) · [Example](#example) · [Development](#development) · [FAQ](#faq) · [Status](#status) · [License](#license)

## Why brick?

Go's `html/template` has no components: every reusable piece of markup becomes a copy-pasted `define` block, passing more than one value into it means building maps by hand, and there is no way to hand a block of markup to another template. brick makes a component a file and a tag, keeps `html/template` and its escaping exactly as they are, and adds interactivity from the server, so a Go app gets a component UI with no bundler and no new template language.

- **Components are files and tags.** `<Card>` or `<x-card>`, with props, children, named and scoped slots, and values passed down through context.
- **It is still `html/template`.** Every `{{ }}` is Go's own, so escaping, pipelines and your existing template knowledge carry over unchanged.
- **No build step, no code generation.** Drop a file in `components/` and it is a component; with `brick serve` a saved change reloads the page.
- **Interactivity from the server.** Islands, server actions, server push over SSE, background jobs that report back, events between components, flashes and redirects from actions, two-way bindings and in-place navigation, driven by a small runtime (about 5 KB gzipped), plus client commands (`brick:js.click='toggle("#menu")'`) and enter and exit transitions (`brick:enter`, `brick:leave`) with no JavaScript of your own. Component state lives in a store you choose — sealed into the page by default when a key is set, so open pages survive a restart.
- **A whole site layer.** Routes, markdown pages, collections, a render cache, live reload, static export and hosting under a sub-path, from the `site` package or the `brick` command.
- **Safe by construction.** Scripts are sandboxed Starlark with step and time limits, every file path is confined to the project, and data never chooses which file is read.
- **A UI kit to start from.** 133 component files that are complete HTML without JavaScript and get better with it, shown in twelve demo apps built on SQLite.

## Install

```sh
go get github.com/ubgo/brick@latest
```

The root module needs Go 1.24 and depends only on a YAML parser. Optional adapters are separate modules under [`contrib/`](contrib):

| Module | What it adds |
|---|---|
| `contrib/script-starlark` | Starlark scripts, loaders, and the `db` / `http` capabilities (Go 1.25) |
| `contrib/markdown-goldmark` | the `<Markdown>` component, with optional heading ids and chroma highlighting (Go 1.25) |
| `contrib/sanitize-bluemonday` | the policy behind `{{ sanitize }}` |
| `contrib/assets-http` | an `http.Handler` for component JS and CSS |
| `contrib/upload-s3` | an S3 `UploadSink` with remove and presigned direct-to-bucket uploads (aws-sdk-go-v2) |
| `contrib/states-redis` | a Redis `BytesStore` for `brick.Bytes`: shared state with session revocation and listing (go-redis v9); reports revoked instances to `OnInstanceEnd` (v19) |
| `contrib/states-sql` | `brick.BytesStore` on `database/sql`: one table for Postgres, MySQL or SQLite; revocation, listing and pruning; reports expired and revoked instances to `OnInstanceEnd`, each once across servers (v19); no driver imported |
| `contrib/keys-awskms` | a `KeyRing` whose state keys are data keys encrypted under AWS KMS, decrypted once at start (aws-sdk-go-v2) |
| `contrib/keys-vault` | a `KeyRing` whose state keys are wrapped by Vault's transit engine, unwrapped once at start (vault/api) |
| `contrib/keys-gcpkms` | a `KeyRing` whose state keys are encrypted under Google Cloud KMS, decrypted once at start (cloud.google.com/go/kms; not tested against real GCP) |
| `contrib/actions-otel` | OpenTelemetry spans and metrics for every action and upload, with no user data in attributes (otel v1.47) |
| `contrib/ratelimit-redis` | an `actionmw.Limiter` on Redis: one token bucket shared by every server, atomic in one Lua script on Redis's clock (go-redis v9) |
| `contrib/push-redis` | a `brick.Broadcaster` on Redis pub/sub for server push across servers: one pattern subscription per process, fanned out locally, carrying `RevokeSession` to every server (v19) (go-redis v9) |
| `contrib/image-webp` | lossless WebP output for `{{ image }}` through `Options.ImageEncoders` (nativewebp, pure Go, no cgo); the `brick` CLI registers it for every site |

Standard-library packages in the root module round it out: `actionmw` (action middleware; `actionmw.Slog` honours a component's `brick: {log: false|debug|info|warn|error}` (v20) and always logs failures at Warn), `statetest` (the conformance suite for a custom state store, sealer, codec or key ring), `validate` (the default field validator) and `upload/disk` (the reference upload sink), beside `bricktest` for theme tests.

## Files

Every file — page, layout, component — has the same shape: optional frontmatter, then a Go template body. Nothing is required:

```html
<h1>Hello</h1>
```

is a complete page. The default layout wraps it if `layouts/default.html` exists.

```filetree
site/
  content/about.html        a page — rendered by path: eng.Render(ctx, "content/about.html", props)
  components/Card.html      <Card> or <x-card>; subfolders do not namespace names
  layouts/default.html      applied when a page names no layout
  data/pricing.yaml         .Site.Data.pricing, via LoadSite
  i18n/en.yaml              messages, via LoadMessages
```

Every path a file writes starts at the project root: `~/components/Card.html`. `./` and `../` are errors, and nothing can leave `Options.FS`.

## Components and props

```html
<Card title="Hi" :count="5" :items=".Posts" open label="plan {{ .Params.name }}" />
```

| Form | Value |
|---|---|
| `title="Hi"` | the string `Hi` |
| `:count="5"` | a Go template expression, evaluated where it is written — here the int `5` |
| `open` | `true` |
| `label="plan {{ .name }}"` | a string built from the actions inside |
| `:post` (no value) | the variable `$post`, the same as `:post="$post"` (v22); a compile error when no `$post` is declared there |

Inside the component the props are `.Props`. Declaring them is optional and buys checking:

```yaml
---
props:
  title: string
  count: {type: int, default: 0}
  items: {type: list, required: true}
  variant: {type: string, default: plain, oneof: [plain, featured]}
---
```

Types are `string int float bool list map any`. A literal value that fails its declaration is a compile error at the call site. A `map` or `list` prop can declare its shape with `fields:` / `of:` (v20), and `brick check` warns on a misspelled field read or a literal that does not fit (`prop-shape`), with no render cost. Undeclared attributes are still props, and `{{ attrs }}` writes them onto a tag for you:

```html
<button {{ attrs }}>{{ .Props.label }}</button>
```

To pass them on to an inner component, spread a map: `<Button brick:spread=".Attrs">` makes every entry a prop, and an attribute written on the tag wins.

`children` and `key` are reserved prop names. `key` sets instance identity and is read as `.Key`. Every component also has `.ID`, a deterministic instance id; `Options.EmitIDs` writes it on each single-root component's root as `data-brick="…"`.

**Resolution.** A tag resolves through `use:` in the file first, then the configured roots in order (`components/` by default), then the [component libraries](#component-libraries) in the order they are listed. The earlier root wins, and a shadowed file is a warning. `use:` also aliases: `BlogCard: ~/content/blog/Card.html`. A component defined inside the file with `<brick:component name="Price">…</brick:component>` wins over both. An unknown tag is a compile error, unless it is listed in `Options.Passthrough` or the policy is `UnknownPassthrough`.

## Children, nodes and slots

`{{ children }}` renders the caller's content **at that moment**. If a component never calls it, the children never render — that is what top-down means.

Markup for a named prop goes in a block, which the component renders lazily with `{{ node }}`:

```html
<Card>
  <brick:prop name="header"><Title>{{ .Params.heading }}</Title></brick:prop>
  Unlabeled content is children.
</Card>

<!-- Card.html -->
<header>{{ node "header" }}</header>{{ children }}{{ if hasNode "footer" }}…{{ end }}
```

A component can hand a value to the block — a scoped slot — which the block reads as `.Slot`:

```html
<!-- PostList.html owns the loop -->
<ul>{{ range .Props.posts }}<li>{{ node "item" . }}</li>{{ end }}</ul>

<!-- the page owns the look -->
<PostList :posts=".Params.posts">
  <brick:prop name="item"><a href="{{ .Slot.URL }}">{{ .Slot.Title }}</a></brick:prop>
</PostList>
```

`.Children` lets a component inspect its statically written child components without rendering them, and `{{ child $c }}` renders one:

```html
<!-- Tabs.html -->
{{ range .Children }}<button>{{ .Props.title }}</button>{{ end }}
{{ child (index .Children 0) }}
```

**Verbatim text.** A component with `raw: true` in its frontmatter receives its body untouched as `.Raw`, with no template and no escaping to think about, or reads it from a file with `src="~/examples/main.go"`. `<brick:prop name="sql" raw>…</brick:prop>` does the same for one prop.

Everything Go does still works across components: `{{ range $i, $p := .Posts }}<Card>{{ $p.Title }}</Card>{{ end }}` — the body sees `$p`, `.` and `$` exactly where it was written.

**Declared slots and component docs (v9).** `brick: {slots: {title: {required: true}, footer:}}` lists the blocks a component takes (`children` is the body): a call that leaves out a required block or passes an undeclared one fails at compile with the caller's file and line. Props and slots accept `doc:` and `examples:`, which rendering never reads; `Engine.ComponentDocs()` returns every callable component with its checked props and slots, so a reference page cannot drift from the frontmatter the engine enforces; `brick docs` (or `-json`) prints that list for a site.

## Layouts

A layout is a component that wraps the page as `{{ children }}`. The order is `brick.WithLayout(...)`, then `layout:` in the page, then `layouts/default.html`. `layout: none` or `WithLayout("none")` renders a fragment. A layout may name its own `layout:`, forming a chain. In a layout, `.Params` and `.Title` are the page's.

## Data and scope

| Read | Is |
|---|---|
| `.Params`, `.Title`, `.Date` … | this file's frontmatter (`.Title .Description .Layout .Date .Draft` are promoted) |
| `.Props` | what the caller gave: tag attributes, or the `props` passed to `Render` for a page |
| `.Data.<key>` | this file's `use:` data imports (`.json`, `.yaml`) |
| `.Site` | the one read-only global, from `brick.LoadSite` |
| `.Page` | the page being rendered, from any depth (`.Page.Props`, `.Page.Locale`, `.Page.URL`, `.Page.Translations`: the same page in the other locales, for `hreflang` links) |
| `.Ctx` | values provided by render-tree ancestors |

```go
site, err := brick.LoadSite(fsys, "site.yaml", "data")   // ${VAR} and ${VAR:-default} are interpolated
eng, err := brick.New(brick.Options{FS: fsys, Site: site})
```

`use:` imports `.json` and `.yaml` data; any other format — TOML, for instance — is one parser away with `Options.DataFormats` (or `brick.WithDataFormat` for `LoadSite` and `LoadMessages`):

```go
brick.Options{DataFormats: map[string]brick.DataFormat{".toml": func(b []byte) (any, error) {
    var v map[string]any
    return v, toml.Unmarshal(b, &v)
}}}
```

Missing keys render empty. Set `strictParams: true` in a file (or `Options.StrictParams`) to make them errors, and read optional keys with `index`.

## Context (`.Ctx`)

```html
<!-- layout -->
{{ provide "accent" .Page.Params.accent }}{{ children }}

<!-- any component below -->
<div class="card--{{ .Ctx.accent }}">
```

Frontmatter `provide:` works too, and `brick.Provide(key, v)` seeds the root. A component can declare what it reads: `inject: {accent: {default: gray}, user: {required: true}}`.

## Translation

```go
msgs, err := brick.LoadMessages(fsys, "i18n")                 // i18n/en.yaml, i18n/fr.yaml …
eng, err := brick.New(brick.Options{FS: fsys, Messages: msgs, DefaultLocale: "en"})
html, err := eng.Render(ctx, "content/cart.html", nil, brick.WithLocale("fr"))
```

```html
{{ t "cart.add" }}  {{ t "cart.items" (dict "count" .Props.n) }}  {{ numberFormat 1234.5 }}
```

Plural forms follow CLDR rules, and `dateFormat` writes month and day names in fr, de, es, it, pt and nl. Any file can have a language variant beside it — `about.fr.html`, `default.ar.html`, `Card.pt-BR.html` — chosen exact tag → language → shared file.

## Scripts and loaders

Functions that return values are written in [Starlark](https://github.com/google/starlark-go), a small, sandboxed Python dialect:

```go
sc, err := scriptstarlark.New(scriptstarlark.Options{})
eng, err := brick.New(brick.Options{FS: fsys, Scripting: sc})
```

```html
---
use:
  helpers: ~/lib/helpers.star
---
{{ helpers.slugify .Title }}

<brick:script>
def price(n):
    return "$%d" % n
</brick:script>
{{ price 42 }}
```

A component can fetch its own data with a **loader**; what it returns merges into `.Props`, and a prop written at the call site always wins:

```html
---
use:
  posts: ~/lib/posts.star
load: posts.recent
props:
  limit: {type: int, default: 5}
---
{{ range .Props.items }}<a href="{{ .url }}">{{ .title }}</a>{{ end }}
```

A component used in a `{{ range }}` can batch its loads instead (v9): `brick: {load_many: users.cards}` holds the calls until the page has run, then calls `cards` once per nesting level with a list of every distinct input, and the function returns a list of dicts in the same order. Ten `<UserCard>` rows cost one data call, and the output is what ten single loads would have produced.

Scripts cannot touch files, the network, the clock or randomness. They reach the outside only through capabilities the host grants in Go. `scriptstarlark.Options{DB: readOnlyDB}` adds `db.query`, whose SQL must be fixed text with `?` values (checked from the syntax tree). `HTTPAllow: []string{"https://api.example.com"}` adds `http.get`. Every call has a step limit and a time limit.

## Boundaries and dynamic components

```html
<brick:boundary>
  <RecentPosts />
  <brick:prop name="fallback"><p>Unavailable right now.</p></brick:prop>
</brick:boundary>

{{ range .Params.blocks }}<brick:render :name=".type" :props="omit . `type`" />{{ end }}
```

A boundary discards the partial output of a failing subtree and renders its fallback. The error is still reported in `Result.Errors` and to `OnError` hooks. `<brick:render>` picks a registered component by name from data — a name, never a path.

## Actions — server state without writing an API

```html
---
state:
  likes: 0
---
<brick:script>
def like(state, props):
    return dict(state, likes = state["likes"] + 1)
</brick:script>
<div><button brick:click="like">♥ {{ .State.likes }}</button></div>
```

A click posts `{handle, token, action}` to `/_brick/action`; the server loads the state from its store (`Options.States`), runs the script function, re-renders only that component, and the runtime patches it in place. `brick:submit` and `brick:change` work the same way; arguments are JSON literals (`bump(10)`), and the built-ins `$set`, `$refresh`, `$toggle` and `$reset` need no script.

**Where state lives** is a choice. `brick.NewMemoryStates(0, 0)` keeps it in the process. `brick.NewSealedStates` seals it into the page, encrypted (AES-256-GCM) and bound to the session, so open pages keep working across a restart, a deploy or a second server: `brick keygen` prints a key, and `BRICK_SECRET` (or `site.Options.StateKeys`) passes it in. `brick.Bytes` turns any byte store (Redis, SQL, your own) into a state store, and `brick: {store: name}` picks a named store per component. When state is lost anyway, the runtime recovers the component in place and keeps what the user typed; a 503 or 429 is retried after its `Retry-After` (once by default, `Options.Runtime.Retries`). Every action passes through `Options.ActionMiddleware`; the `actionmw` package has logging, rate limiting, `RequireSession`, timeouts and panic recovery.

**Effects and events.** An action can also ask the browser to do something after the patch: `brick.flash("Saved", "success")` shows a message (the kit `Toaster` turns it into a toast) and `brick.clear_flash()` dismisses the ones on screen (v21), `brick.redirect` / `brick.navigate` / `brick.title` / `brick.download` / `brick.url` do what they say, and `brick.dispatch("contact-deleted", {"ids": [42]})` tells the other components on the page. A component listens with `brick: {on: {contact-deleted: $refresh}}`, a parent hears one child with `<Row brick:on:deleted="reload" />`, and markup dispatches without a request with `brick:click='$dispatch("picked", {"id": 1})'`. Listeners are ordinary actions with their own handle and token, checked when the file is read; delivery stays on the page and stops runaway chains. In Go, `Engine.RunAction` returns the effects, middleware reads or adds them through `brick.EffectsFrom(ctx)`, and `bricktest.Live` runs a page's actions and delivers its events in a test.

Bindings: `<input brick:model="name">` keeps a state field in sync as the user types (debounced; `.lazy` for on-change), `brick:text` / `brick:show` follow it locally with no request, an action returning `[state, errors]` exposes `{{ .Errors.field }}` for validation messages, `brick:loading` shows while a request runs, and `brick:poll="10s"` refreshes a component.

**Forms without JavaScript (v16).** A stateful component's `<form brick:submit="save">` is rendered with `method="post"` and the hidden fields the runtime would send, so with scripts off it posts to its own page: the site runs the action through the same checks (same-origin post, token, session, locked fields, middleware) and answers `303` back to the page, or the page with `422` and the errors. With scripts on nothing changes. `Options.NoFormPosts` turns it off; `Engine.FormPost` serves a host with its own router. A file input with `brick:upload` inside such a form uploads too (v19): the form is sent multipart and each file streams into your `UploadSink` with the same limits as the runtime's upload before the action runs; a refused file answers 422 with the reason in `.Errors.<field>`.

**Forms (v7).** `brick:model.defer` holds a field in the browser and sends it, as `sets`, with the component's next action in the same request, so a ten-field form costs one request. Checkbox groups and `<select multiple>` bind to list fields, and paths reach inside state (`address.city`, `items.0.qty`) without ever adding a key. The attributes, all checked when the file is read:

| Attribute | Modifiers | What it does |
|---|---|---|
| `brick:model="path"` | `.lazy` `.defer` `.debounce.<n>ms` `.throttle.<n>ms` | Binds an input to a state field |
| `brick:text="path"`, `brick:show="path"` | — | Follow a field locally, no request |
| `brick:click`, `brick:submit`, `brick:change` | `.once` `.debounce.<n>ms` `.throttle.<n>ms`; `.optimistic` (click, change); `.lock` (submit); `.outside` (click: a click outside the element, v14); `.details` (click, v22: keys, button, count and position as the last argument) | Run an action |
| `brick:keydown="call"` / `brick:keyup="call"` | a key (`.enter` `.escape` … `a`-`z` `0`-`9`), `.ctrl` `.meta` `.shift` `.alt` for a combo (exact match), `.window`, `.once` `.debounce` `.throttle` `.optimistic`; `.norepeat` (keydown, v22: once per press while the key is held) | Run an action on a key press or release |
| `brick:input="call"` | `.once` `.debounce` `.throttle` `.optimistic` | Run an action on every edit, with the control's value (v14) |
| `brick:focus="call"` / `brick:blur="call"` | `.once` `.debounce` `.throttle` (blur also `.optimistic`); `.window` (v21) | Run an action when the element itself gains or loses focus; blur passes the value (v14). With `.window`, when the browser tab does, with no value |
| `brick:intersect="call"` | `.leave` `.once` `.half` `.full` `.margin.<n>px` `.dwell[.<n>ms]` (v15: only after the element stayed in view, 1000 ms by default, once per stay) | Run an action when the element scrolls into (or out of) view; re-armed after every morph, so infinite scroll needs no code |
| `brick:loading="actions"` | `.delay[.<n>ms]` `.remove` `.except` (v21) | Show (or hide) while those actions run; the trigger gets `data-loading`. A target may be a call (`remove({{ .id }})`, arguments match as a prefix) or a field's change (`$set("q")`) (v21) |
| `brick:disable-with="Saving…"` | — | Disable a button and swap its label while its request runs; with no value, keep the label (v21) |
| `brick:dirty="paths"` | `.remove` | Show while fields differ from the last render; `data-dirty`, `dirty`/`clean` events |
| `brick:confirm="Delete?"` | `.prompt` (v18: ask for text and pass it as the call's last argument; `brick.promptWith(fn)` replaces `window.prompt`) | Ask first; `window.confirm` by default, the kit dialog on a page with a `<ConfirmDialog>`, `brick.confirmWith(fn)` for your own |
| `brick:poll="5s"`, `brick:poll.5s="call"` | `.visible` `.keep-alive` (v15: keep polling while the tab is hidden) | Refresh, or run an action, on a timer |
| `brick:upload="field"` | — | Send a file input's files to an upload field (see Uploads below) |
| `brick:upload-progress="field"` | — | A `<progress>` filled while that field's files are sent |
| `brick:js.<event>='cmd(args); …'` | one event (`click` `dblclick` `input` `change` `submit` `focus` `blur` `keydown` `keyup` `mouseenter` `mouseleave`), `.once`, `.outside` (click), `.window` and key combos (keydown, keyup) | Run DOM commands in the browser with no request, on any page (v18): `show` `hide` `toggle` `addClass` `removeClass` `toggleClass` `setAttr` `removeAttr` `toggleAttr` `focus` `dispatch`; arguments are JSON literals, changes survive the next morph |
| `brick:leave="classes"` | — | Play these classes before the element is removed by a morph or a `rows` delete, or hidden by a JS `hide` or `brick:show` (v21); waits for the transition, at most 2 s, not under reduced motion (v18) |
| `brick:enter="classes"` | — | The classes an element starts from when `brick:show` or a JS `show` reveals it; they come off on the next frame so its transition plays in (v21) |

**The browser runtime (v8 batch A).** `brick:key` keys a row among its siblings, so inserting at the top keeps every other row's element; `brick:ignore` (`.self`) and `brick:replace` tell the morph to leave an element alone or swap it whole; `brick:transition` wraps a component's action morphs in a view transition. `.optimistic` on a literal `$set`/`$toggle` flips `brick:show`/`brick:text` before the server answers and reverts on failure; `brick:submit.lock` makes a form read-only while it is sent. `brick.hook("request" | "response" | "morph" | "morphed" | "ready", fn)` adds headers, cancels, or keeps a widget's node out of a morph, and `brick.watch(el, path, fn)` follows a signal. `brick:ref` names an element for `brick.ref(el, name)`, and `brick:offline` shows while the network is down. `brick.js(el, cmds)` runs the `brick:js` command list from client code, and `brick.find(name)` / `brick.all(name)` look stateful components up by name (v21). A stateful child declares `brick: {model: value}` and a stateful parent binds its own field to it with `<Stepper brick:model="qty">` (v22, Modelable): the parent's value flows down when it renders the child, and the child's changes come back as a `$set`.

**Server state (v8 batch B).** An action can answer without re-rendering: `brick.skip_render()` saves the state and sends back only the new handle, and `brick.reply(value)` is what `brick.action(el, name, args)` resolves with, for autocomplete or "is this name taken". `brick: {init: load_into}` seeds a new instance from its props (an edit form starts from the record), `brick: {updated: tidy}` normalises fields after `$set`, and `$reset("q")` puts a field back to its declared value. `brick: {query: {q: {as: search}, page: {history: push}}}` keeps fields in the URL: a shared link opens filtered, typing rewrites the parameter, and Back restores the filter in place. `brick: {params: on_params}` (v22) runs a hook whenever the URL changes those fields, and a same-page link that changes only them is a patch: no page fetch, only the owning components re-render. `brick: {remember: [columns]}` keeps fields per session through `Options.Remember` (`brick.NewMemoryRemember()` for development). Pages route by pattern too: `content/posts/[slug].html` answers `/posts/hello/` with `.Page.Route.slug`, `[...rest]` and `[[...rest]]` catch the rest, `brick.not_found()` answers with the 404 page, and `brick: {paths:}` lists the values `brick build` exports.

**Lazy islands and streamed replies (v8 batch C).** `<SalesChart brick:lazy="visible" />` renders a slow panel after the page: the page carries a sealed, session-bound ticket and the enclosing `<brick:boundary>`'s `loading` block, and the runtime redeems the ticket at `/_brick/island` when the panel scrolls into view (`load` and `idle` too; `brick: {lazy:}` makes it a component's default). `brick.stream("answer", text)` sends text into a `brick:ref` while an action runs, as NDJSON the runtime applies with `textContent`, and the final render lands as usual. Streamed islands (v20): `brick:lazy="stream"` sends the page at once and streams a slow panel into the same response when it is ready, placed by a tiny inline script (`Options.StreamWorkers` bounds how many render at once); curl, crawlers, client navigation and exports get it rendered in place.

**Rows, form hand-off and recovery (v9).** `brick.rows("log", component="Msg", items=[msg], at="top", limit=50)` adds, updates or removes rows of a `brick:ref` list without keeping them in state (each row is a component render, keyed by `brick:key`), and `brick: {temporary: [rows]}` keeps a field's value for one render without saving it to the state store. `brick.submit("login")` hands a `brick:ref` form to the browser's native submit after the morph, for a login that sets a cookie. `brick.region("feed", "append")` (v22) grows a `brick:ref` + `brick:ignore` element with the same element of the action's render, so a "load more" list written inline keeps its earlier pages only in the browser, and `brick.transition("slide")` (v22) chooses the answer's view transition (`"none"` skips it). `action-error` carries `{action, message, root, id, status}`, and `brick.reloadComponent(d.root)` renders a crashed component fresh from the server. `brick.debug(true)` and `brick.latency(800)` are console switches for local debugging.

**Server push (v17).** A stateful component that should change when something happens elsewhere declares the topics it listens to, `brick: {listen: [orders, "order:{{ .Props.id }}"]}`, and an action publishes one with `brick.publish("orders")` (or Go code with `engine.Publish(ctx, "orders")`) once it has saved its write. The page opens one `EventSource` on `/_brick/events` for all its topics, and every listening component runs a quiet `$refresh`, once per tick however many events arrive; a hidden tab waits until it is shown unless the component says `keep_alive: true`. Subscribing is closed by default: the browser never names a topic, it sends the listen tokens the server sealed into each root (bound to the session, expiring after a day), so it can only hear topics its own page was rendered with, and `PushOptions.AuthorizeTopic` can re-check each one. An event carries only the topic; the component reads its data again through its own render. `Options.Push = brick.NewPush(brick.PushOptions{})` turns it on with an in-memory broadcaster for one process; `contrib/push-redis` carries topics between servers, which must then share a `Sealer`. Limits: 1 000 streams per process (503 beyond), 64 topics per stream, a heartbeat every 25 s. An open stream is trusted no longer than what opened it (v19): it ends when its earliest listen token expires (after refreshing every listener, so the runtime reopens with fresh tokens), when a recheck every `PushOptions.Recheck` (1 minute) finds the session changed or a topic no longer authorized, and at once on `Push.RevokeSession` (or `Engine.RevokeSession`), across servers with `contrib/push-redis`. Topics starting with `brick:` are the engine's own. Presence (v22): `brick: {listen: {presence: "room:{{ .Props.id }}"}}` and `{{ .Presence "room:7" }}` show who is on a topic (`Total`, `Entries` with `ID`, `Data`, `Conns`) and refresh every viewer when someone joins or leaves; `PushOptions.Presence.Identify` decides what an entry shows (an anonymous id by default), and `pushredis.NewPresenceStore` shares the list across servers.

**Background jobs (v18).** An action that would take too long starts a job instead, `id = brick.start_job("report", {"month": m}, topic = "reports")`, and answers at once. The job is a Go function the host registered in `brick.NewJobs(brick.JobsOptions{Funcs: …, Push: push})` (`Options.Jobs`); it runs after the response, only if the action succeeded, writes its result through your own data layer, and publishes its topic when it ends, so every component listening to it refreshes and reads the result. A job never sees the request, the session or the component's state; args are JSON values up to 16 KiB. Every failure reaches `JobsOptions.OnError` before the topic is published. The default queue is an in-process pool (4 workers, 256 waiting, a 10-minute timeout per job; full means `ErrJobQueueFull`, never a stalled action); `JobsOptions.Queue` plugs in a real queue whose worker calls `jobs.Run`. Go code starts one with `eng.StartJob(ctx, name, args, topic)`. `brick.cancel_job(id)` (v21) cancels a job from an action, only one its own component started in the same session; `eng.CancelJob(ctx, id)` cancels any; a cancelled job ends through `OnError` with `ErrJobCancelled` and its topic is published, and a real queue cancels through the optional `JobCanceller`.

**Locked fields (v13).** Every `state:` field can be written by the browser through `$set` and `brick:model`. For a field the server chose, such as the row being edited, list it in `brick: {locked: [editing_id]}`: actions, `init:` and `updated:` still write it, but a client `$set`, `$toggle`, `$reset`, deferred set or `brick:model` on it, or on any path below it, is refused with a 422 naming the field (`brick.ErrLockedField`, an `ErrBadValue`) and the action does not run. `brick check` reports a `brick:model` or literal `$set` naming a locked field, and a locked field that is also in `query:` or `uploads:`.

**Closed by default (v18).** `Options.LockState` (or `lockState: true` in `brick.yaml`) turns the default around: the browser writes no state field unless the component lists it in `brick: {writable: [title]}` (fields in `uploads:` and `query:` stay writable, since the client writes them by declaration). A refused write is the same 422 `ErrLockedField`, and a literal `brick:model` or `$set` on a closed field fails to compile, naming the fix. `brick: {lock_state: false}` keeps one component open; `lock_state: true` or `writable:` alone closes one under an open engine. Off by default.

**Validation (v7).** Declare rules once in frontmatter, `brick: {rules: {email: [required, email]}}`, and `{{ .Errors.email }}` shows the message only after the user has touched the field (a `.lazy` field on blur). `brick.validate()` in the submit action checks and touches every field; the action's own errors win over the rules. Unknown rules and paths fail when the file is read, and `Options.Validator` plugs in your own validator. Rules on list items use `*` (v19): `todos.*.title: [required, {min: 2}]` checks every item, and the error is keyed by the item's path, `{{ index .Errors "todos.2.title" }}`. Client code reads the last render's errors with no request through `brick.errors(el)` (v20): `.has("email")`, `.first()`, `.all()`, `.clear()`. `.Form` (v22) gives each field one value, `{{ with .Form.email }}<input id="{{ .ID }}" brick:model.lazy="{{ .Name }}" value="{{ .Value }}">{{ .Error }}{{ end }}`, and `{{ range .Form.todos.Items }}` walks a list's rows with their paths (`todos.2.title`).

**Uploads (v7).** Declare which state fields take files, `brick: {uploads: {avatar: {accept: [image/png, image/jpeg], max_size: 2MB}}}`, and put `brick:upload="avatar"` on a file input. Each file streams to `/_brick/upload` through your action middleware as `$upload`; the engine counts the bytes, sniffs the type, and hands them to the `UploadSink` you set in `Options.Uploads` (the reference `upload/disk`, or S3 with presigned direct uploads). The ref (`id`, `name`, `type`, `size`, `url`) lands in state, so `.State.avatar.url` is the preview; `<progress brick:upload-progress="avatar">` fills while it is sent, and `$removeUpload` / `$cancelUpload` remove or abort. Scripts never see bytes. `Options.UploadCheck` (v21) is your own check in Go: it sees each file's meta and first 512 bytes before the sink stores anything, and `brick.RefuseUpload("…")` shows its message on the field.


## Navigation without reloads

`navigation: true` in `brick.yaml` (or `Options.Navigation`) makes internal links and forms fetch the next page and patch only what changed — no server change, full-load fallback on any error. `brick:persist` keeps an element across pages, `brick:prefetch` loads a page on hover (after a short pause), keyboard focus, a press, or when visible, `brick:nav="off"` opts a link out and `brick:nav="replace"` replaces the history entry. `brick:current` marks the link to the page being shown (`aria-current="page"`, `data-current`), `<html data-navigating>` marks a slow navigation, `brick.on("navigate", d => d.preventDefault())` cancels one, and back/forward restore the scroll position. `brick:scroll="<name>"` (v20) keeps a sidebar's scroll position across pages and back/forward. When the next page shares the layout, the site answers with only the page (v22, fragment mode), spliced between the layout's marks; a docs navigation on the kit showcase drops by about 80%.

## Islands — per-request parts of a cached page

```html
---
brick:
  island: true
---
{{ with .Ctx.user }}Hi, {{ .Name }}{{ else }}<a href="/login">Sign in</a>{{ end }}
```

A page is rendered once; an island is a hole in it, filled per request: `r, _ := eng.RenderChunks(…)` (cache `r`), then `eng.Fill(ctx, r, brick.Provide("user", u))` per response. `site.Options.Request` does this for every page the site serves. Islands take props only and still see what layouts provided. A page whose whole body is per visitor sets `brick: {per_request: true}` (v22): it skips the page cache, renders with the request's values, is sent `Cache-Control: private, no-cache`, and `brick build` skips it with a warning.

## Client code, styles and CSP

```html
<brick:client>customElements.define("x-counter", class extends HTMLElement { … })</brick:client>
<brick:style scoped>button { font-weight: 600 }</brick:style>
<div class="counter"><x-counter>…</x-counter></div>
```

Each component type's script and stylesheet is emitted once per page, where the layout calls `{{ assets }}`, at content-hashed URLs with `integrity` hashes, after a small runtime (about 5 KB gzipped): `brick.mount("Counter", (el, props) => cleanup)` runs once per element, including elements inserted later, and `brick:mount="visible"` or `"idle"` on the tag defers it. `brick.emit` / `brick.on` pass events between components, and `import { open } from "brick:Modal"` in client code imports another component's module. Serve them with `contrib/assets-http`. Scoped styles use CSS `@scope`; `scoped="inclusive"` (v18) also reaches a nested scoped component's root, to place or size it, but nothing inside it, and `Options.StyleTransform` rewrites the CSS each block ships (a minifier, a fallback). A component's CSS can also live in a sibling `Card.css` (v22), global by default or scoped with `brick: {css: scoped}`, in the same hashed sheet as its inline blocks. A client component's root carries `data-brick-type` and its `client: true` props as JSON.

For a strict CSP, render once and fill a fresh nonce per response:

```go
r, err := eng.RenderChunks(ctx, "content/index.html", props)   // cache r
html, err := r.Bytes(brick.WithNonce(nonce))                    // {{ nonce }} and every emitted tag get it
```

## Errors

Every error is a `*brick.Error` with the author's file, line and column, the component chain, and a stable `Code`. `Options.Mode` defaults to `Prod`, which returns an error and no bytes. `Dev` also returns the page with an inline error block. Compile warnings (shadowed names, unread blocks, `provide` after `children`, …) are logged to `Options.Logger` and returned by `eng.Warnings()` and `Result.Warnings`.

`brick check` also warns about unbalanced HTML (`html-balance`, v9): an element never closed, a close tag with nothing to close, or one that skips an open element, with file and line, following every `{{ if }}` branch and warning only when every path is wrong. In `Dev` mode the root element of each single-root component carries `data-brick-loc="components/Card.html:5"`, so the browser inspector leads to the source; production output is unchanged.

For editors, `brick schema -o .vscode/brick-frontmatter.schema.json` (or `brick.FrontmatterSchema()`) writes a JSON Schema of frontmatter (v10), generated from the engine's own key lists so it cannot drift from what `brick check` accepts. It checks one file's shape; cross-file rules (a `load:` function that exists) stay with `brick check`. For `.html` files, `brick lsp` (v11) is a language server: `brick check`'s diagnostics on save, completion of component tags, props and frontmatter keys, hover docs, go to definition, find references and component rename (v15: tags, `use:` paths and the file), prop rename and prop references (v19: the declaration, every use in the component and every call site, refused with the reason when a site cannot be followed) and formatting (v13: Format Document and format-on-save write exactly what `brick fmt` writes); [`editors/vscode`](editors/vscode) is a VS Code client for it, which also highlights `{{ }}` actions, component tags and `brick:*` attributes, and (v20) highlights the frontmatter block as YAML and checks it as you type against `brick schema`.

## Checking and testing

```go
report, err := eng.CompileAll(ctx, "content")   // every page, component and layout, plus a dry run for escaper errors
for _, d := range report.Diagnostics {
    fmt.Println(d)   // content/pricing.html:12:5: error: unknown component <Crad> …
}
```

```go
eng := bricktest.Engine(t, brick.Options{}, bricktest.Theme("components/Card.html", `<div>{{ children }}</div>`))
out, err := eng.RenderComponent(ctx, "Card", map[string]any{"title": "Hi"}, "<p>body</p>")
bricktest.Golden(t, "card", out)   // go test -update rewrites testdata/card.golden
```

For interactive components, `bricktest.Live` fills a page and holds its stateful components, so a test runs actions and events the way the browser does:

```go
live := bricktest.Live(t, eng, "content/apps/contacts.html")
res := live.Component("ContactsApp").Call("contacts.remove", 42)
bricktest.AssertDispatched(t, res, "contact-deleted", map[string]any{"ids": []any{42}})
bricktest.AssertFlash(t, res, "success", "Deleted Ada Lovelace.")
live.Deliver(res)                                   // runs ContactCount's $refresh
live.Component("ContactCount").Text()               // "63 contacts"
```

`bricktest.NewMemoryUploads()` is an in-memory `UploadSink`, and `LiveRoot.Upload(field, name, type, data)` sends a file through the whole upload pipeline. Since v20, `LiveRoot.Children()` and `Child(name)` reach nested stateful components, `AssertRenderSkipped` / `AssertRendered` check `brick.skip_render`, and `AssertEffect` / `AssertNoEffect` / `AssertEffects` check what an action queued for the client. Since v21, `SubmitWith(form, button, values)` presses a particular submit button, `live.AssertViewHas(t, "Card", "price", 42)` checks the data a component's template saw (`brick.WithViewLog`), `bricktest.Live(t, eng, page, brick.WithLazyInline())` renders lazy islands in place, and `live.WindowFocus()` / `WindowBlur()` fire `.window` focus and blur. Since v22, `ClickWith(selector, bricktest.ClickDetails{…})` clicks with keys and a position for `brick:click.details`.

A test can also drive a component the way the runtime does and assert on its state instead of its HTML (v9): `form.Submit("form#signup", map[string]any{"email": "nope"})`, `form.Click("button.reset")`, and (v13) `form.Change("select#plan", "pro")`, `form.Keydown("input#q", "Enter")`, `live.Keydown("Escape")` for `.window` keys and `list.Intersect("#more")`, each resolving modifiers as the runtime does; then `form.AssertSet(t, "name", "")`, `form.AssertErrors(t, "email")` or `form.AssertNoErrors(t)`; `live.Dump()` writes the page to a file to open in a browser. Outside tests, `Engine.Instance(ctx, handle, brick.WithSession(id))` returns a read-only `InstanceView` of one stateful component.

An engine is a snapshot: it compiles each file once. After editing files, call `eng.Reload(ctx)` and swap the new engine in.

## Security model

- **Every file is trusted code**: pages, layouts, components and scripts are like Go source. Only data is untrusted — render props, frontmatter values, `.Site`, data files, loader results — and `html/template` escapes it.
- `{{ sanitize .bio }}` is the one way data becomes markup, through `Options.Sanitizer`. There is no unsafe default.
- Paths never leave `Options.FS`; pass an `os.Root`-backed FS (`root.FS()`) to also stop symlink escape. A file is never chosen by data: `src=` is literal, and `<brick:render>` takes names only.
- Nonces are validated. `.Env` does not exist; environment variables reach templates only through `${VAR}` in `site.yaml`.

## Built-in functions

Go's own (`and or not len index slice print printf eq ne lt le gt ge …`) plus: `image default coalesce dict list omit pick has keys first last seq toJSON fromJSON upper lower trim trimPrefix trimSuffix replace split join contains hasPrefix hasSuffix truncate slugify pluralize add sub mul div mod max min dateFormat numberFormat t sanitize provide children node hasNode child attrs nonce assets cacheTag tag endtag`. `tag` / `endtag` write an element whose name is data, such as a heading level (`{{ tag (print "h" .Props.level) "class" "title" }}…{{ endtag (print "h" .Props.level) }}`), for elements without URL, script or raw-text content only.

## Options

| Field | Default | |
|---|---|---|
| `FS` | nil | the project root; nil means only `WithSource` pages render |
| `Roots` | `["components"]` | component directories, earlier wins |
| `Libraries` | | component libraries used by name, after `Roots`; see [Component libraries](#component-libraries) |
| `LayoutDir` | `layouts` | |
| `Unknown`, `Passthrough`, `Syntax` | error, none, both | unknown-tag policy, verbatim tags, accepted tag forms |
| `StrictParams` | false | missing map keys become errors |
| `LockState` | false | close every stateful component's state to the client except its `brick: {writable:}` (and upload and query) fields (v18); brick.yaml `lockState` |
| `StyleTransform` | nil | rewrite each `<brick:style>` block's CSS at compile (v18): gets a `brick.StyleBlock`, returns the CSS that ships; must be deterministic |
| `Mode` | `Prod` | `Dev` shows errors inline too |
| `Site`, `Messages`, `DefaultLocale` | | data and translation |
| `Scripting`, `Markdown`, `Sanitizer` | nil | adapters from `contrib/` |
| `Hooks` | | values implementing `BeforeRender`, `AfterRender`, `OnCompile`, `OnError`, `OnInstanceEnd` (v19: told when a stateful instance expires, is evicted or is revoked, by stores that can see it) |
| `Funcs`, `PropTypes` | | Go template functions and extra `props:` types; built-in names are an error from `New` |
| `States`, `StateStores` | nil | the default state store and named ones a component picks with `brick: {store: name}` |
| `Codec`, `Sessions` | JSON v1, none | how byte and sealed stores encode state; who a request belongs to (state is bound to it) |
| `ActionMiddleware`, `ActionRequest`, `ActionErrors` | | middleware around every action (outermost first), request values for actions, a custom error envelope |
| `MaxActionBytes` | 1 MiB | action request size cap (JSON actions and no-JavaScript form posts) |
| `NoFormPosts` | false | turn off forms without JavaScript (v16): no `method="post"` rewrite of `brick:submit` forms, and pages refuse form posts |
| `Runtime` | 250 ms, 1 s, beside the runtime, 1 retry, 5 s | the browser runtime's debounce, poll floor (never below 250 ms), action URL, retries after a 503 or 429, and longest `Retry-After` it waits for; written into the page as a `brick-config` meta only when set |
| `StreamWorkers` | 4 | how many `brick:lazy="stream"` islands of one page render at once (v20) |
| `MaxEffectBytes` | 32 KiB | cap on one action's encoded effects (flashes, events, redirects) |
| `Push` | nil (off) | server push (v17): `brick.NewPush(brick.PushOptions{...})` with a `Broadcaster` (in-memory by default), a token `Sealer`, `MaxConnections`, `MaxTopics`, `TokenTTL`, `Heartbeat`, `AuthorizeTopic`, `Recheck` (v19, 1 minute), `Presence` (v22: `Identify`, `Store`, `TTL`, caps); a component declaring `brick: {listen:}` fails to compile without it |
| `UploadCheck` | nil | a per-file check run before the `Uploads` sink opens (v21): `func(ctx, UploadMeta, head []byte) error`; `brick.RefuseUpload(msg)` refuses with a message on the field |
| `Jobs` | nil (off) | background jobs (v18): `brick.NewJobs(brick.JobsOptions{Funcs, Push, Queue, Workers, QueueSize, Timeout, OnError})`; `brick.start_job` and `Engine.StartJob` fail with `ErrJobsOff` without it |
| `RedirectHosts` | none (same host only) | other hosts that `redirect`, `navigate`, `download` and `url` effects, and a loader's `brick.redirect` (v15), may point at |
| `Rand`, `Clock` | `crypto/rand`, `time.Now` | for deterministic tests |
| `MaxDepth`, `MaxComponents`, `MaxOutputBytes`, `MaxFileBytes` | 64, 100 000, 32 MiB, 8 MiB | zero means the default, never unlimited |
| `EmitIDs` | false | write instance ids on component roots |
| `DataFormats` | | parsers for data extensions beyond JSON and YAML |
| `ImageEncoders` | JPEG and PNG only | extra output formats for `{{ image }}` (v16), such as `contrib/image-webp`; a registered `jpeg` or `png` replaces the built-in |
| `TimeZone` | UTC | used by `dateFormat` |
| `AssetBase` | `/_brick/` | URL prefix for component assets |
| `Logger` | discard | `*slog.Logger` |

`brick.LoadConfig(fsys, "brick.yaml")` reads the same settings from a file and can pin the engine version a theme needs (`brick: ">=0.1, <2"`). Its `host:` section (`host: {globals: [db, now], uploads: true}`) is for the `brick` CLI only: it names the script globals and upload sink your Go program supplies, so `brick check` and `brick lsp` compile the site with stand-ins and report a page that calls one as a `host-only` warning rather than `undefined: now`. `Apply` ignores it.

## A whole site — `site` and the `brick` command

The `site` package turns a project folder into a website on top of the engine: every page gets a URL (`content/blog/hello.md` → `/blog/hello/`, or `brick: {url: /x/}`), `.Site.Pages` lists them for index pages, `.md` files are pages, a section can require frontmatter fields (`content/blog/_collection.yaml`), each page is rendered once and cached until a file it read changes, and the whole site exports as static files.

```sh
go install github.com/ubgo/brick/cmd/brick@latest

brick new site blog      # a working project using the kit; then brick serve blog
brick check              # every problem at once; exit 1 on errors; -json for tools
brick routes             # every URL and the file behind it; -json adds each page's title, date, draft, section and frontmatter (v20)
brick serve              # http://localhost:8080 — drafts, live reload, /_brick/play/Card?title=Hi
brick build -o public    # static export; -base /docs for a sub-path, -locales en,fr adds /fr/… URLs, -keep /a/,/b/ leaves those URL prefixes unrewritten, -json for tools
brick vendor             # fetch remote use: imports into brick_modules/, pin brick.lock; -check for CI, -json for tools
brick keygen             # print a new state key for BRICK_SECRET
brick new component Counter -state   # a starter component (-client, -island, -lazy); brick new page blog/hello or blog/[slug]
brick fmt    [-l|-w|-d] [-line-length N] [-attr-order a,b] [-inline a,em] [path ...]   # format .html files without changing what they render
brick docs   [-json] [dir]              # list every component with its props and slots
brick schema [-o file]                  # print the JSON Schema of frontmatter, for editors
brick lsp                               # language server for editors on stdin/stdout (editors/vscode is a VS Code client)
brick version [-json]                   # the module version and VCS revision this binary was built from
```

```go
s, err := site.New(ctx, site.Options{FS: root.FS(), Engine: brick.Options{Markdown: markdowngoldmark.New()}})
http.Handle("/", s.Handler())          // pages, assets, static/, 404
go s.Watch(ctx, watch.Options{})       // rebuild as files change
rep, err := s.Export(ctx, "public")    // or write the static site
```

`s.Routes()` and `s.Patterns()` (v20) carry each page's `Title`, `Description`, `Date`, `Draft`, `Section` and `Meta` (its frontmatter without `brick:`), plus `Dynamic` for a `[param]` route, so a host builds a menu, a sitemap or an admin list from them alone. A site can set formatter options in brick.yaml (v20): `fmt: {lineLength: 100, attrOrder: ["brick:*", id, class], formatters: {"brick:style": "prettier --stdin-filepath x.css"}}`; `-line-length` and `-attr-order` override them for one run, and everything is off by default. `inline: [a, em, Icon]` (v21, `-inline`) names elements and component tags whose content keeps the parent's indentation.

[`watch`](watch) is the polling, content-hashing file watcher underneath, and [`tailwind`](tailwind) rebuilds a Tailwind stylesheet only when the classes in your files change (`tailwind: {input: styles/app.css, output: static/app.css}` in `brick.yaml`); neither imports brick.

**Feeds, robots.txt and the sitemap (v14).** A page named with an output extension before `.html` renders that exact file: `content/feed.xml.html` is `/feed.xml` (served as `application/xml`) and `content/robots.txt.html` is `/robots.txt`, with no layout unless named and nothing HTML added. Only `xml`, `rss`, `atom` and `txt` are allowed; `data.json.html`, `.js`, `.css` and `.csv` are refused with an error, because html/template escapes for HTML and would break them. An output page can be dynamic (v22): `content/tags/[tag]/feed.xml.html` answers `/tags/go/feed.xml` for each `brick: {paths:}` value, with the parameter in a directory, never in the file name. `sitemap: {}` in `brick.yaml` serves and builds `/sitemap.xml` with absolute URLs from `baseURL`, `hreflang` alternates for translated pages and `<lastmod>` from `lastmod:` or `date:`; drafts, output pages, the 404 page, dynamic pages without `paths:` and pages with `brick: {sitemap: false}` are left out. A markdown page's layout reads `.Page.Headings` (`Level`, `ID`, `Text`) for a table of contents; `markdown: {headingIDs: true, highlight: true}` in `brick.yaml` gives headings ids the table can link to and highlights fenced code (both off by default).

**Protected pages, redirects and error statuses (v15).** A loader can answer instead of returning data: `brick.redirect(url, status=303)` (302, 303, 307 or 308), `brick.error(status, message="")` (any 4xx or 5xx, rendered by `content/error.html` with `.Ctx.error.status` and `.message`) and `brick.header(name, value)` (only `Cache-Control`, `Content-Language`, `Expires`, `Last-Modified`, `Link`, `Vary`, `X-Robots-Tag`). A page's render is cached and shared, so a guard that depends on the visitor goes in an island's loader, which sees your request values: a signed-out visitor gets `303 Location: /login/` with `Cache-Control: private, no-store`, and no answer is ever cached. Redirect targets are paths, or absolute URLs on the request's own host or `brick.Options.RedirectHosts`, so loader data cannot make an open redirect. A boundary never contains an answer. `brick build` writes a redirecting page as a meta-refresh page (never the protected content) and stops on an error status. From Go the answers are `*brick.Redirect` and `*brick.HTTPError` beside `brick.ErrNotFound`.

**Images.** `{{ with image "~/static/team.jpg" }}<img src="{{ .URL }}" srcset="{{ .SrcSet }}" width="{{ .Width }}" height="{{ .Height }}" alt="…">{{ end }}` makes 480/960/1440-pixel copies in pure Go, served from `/_brick/img/` with content-hashed names. A format after the widths changes the output (`image "~/static/d.png" 480 960 "webp"`): JPEG and PNG are built in, `Options.ImageEncoders` adds others, and `contrib/image-webp` (registered by the CLI) writes lossless WebP. A path from data is allowed but confined to `static/` (`Options.ImageDirs`) and to real images.

**Hosting under a path, or several sites on one server.** Pass `-base /docs` to `brick build` or `brick serve` (or set `site.Options{BasePath: "/docs"}`) to host a site under `/docs/`, for example a GitHub Pages project site. The site stays written as if it were at the root: brick rewrites root-absolute links, assets, forms and redirects in the HTML it serves and exports. To serve several sites from one server, give each its own base and call `site.ListenAndServeAll(ctx, ":8080", docs, shop)`, or mount `site.Mount(docs, shop)` in your own server. `task demo:both` serves the kit showcase at `/ui/` and the engine demo at `/engine/` from one server, with no changes to either site.

**Dev conveniences (v9).** With navigation on, live reload fetches the edited page and morphs it in, so scroll position, focus and typed input survive (a changed stylesheet, script or a failed render still reloads fully; `brick.refresh()` does it from code). In Dev, `/_brick/instances` lists the live stateful instances of every store that can list them, without state values, handles or session ids. Pages with a `brick:prefetch` link carry native speculation rules, served and exported, for the links nav.js does not intercept.

**Dev toolbar (v10).** In Dev every page shows a small collapsible "brick" pill in the bottom-left corner with that request's render time, site-cache hit or miss, component count and the five most used components (each linking to its playground), islands and lazy islands, loader and `load_many` calls, and a link to `/_brick/instances`. It is added to each response only, so production output, cached Results and exports are unchanged. A host that serves pages itself reads the same counts from `Result.Stats` (`brick.RenderStats`), and passes `brick.WithStats(&stats)` to `Fill` to add island renders. Since v20 it also lists each lazy island as it lands (component, render time, components, loader calls), read from a Dev-only `Server-Timing: brick-island` entry on the island answer.

## Remote components

```yaml
use:
  Card: github.com/acme/brick-kit/components/Card.html@v1.2.0
```

`brick vendor` fetches remote imports (transitively) into `brick_modules/` and pins every file's sha256 in `brick.lock`; `brick vendor -check` verifies offline for CI. Rendering reads only the vendored copies — no network at render — and inside a kit `~/` means the kit's own root. To make a whole kit available by name, list it as a component library instead.

## Component libraries

A component library is a folder with a `brick-library.yaml` manifest: a UI kit, or your team's shared components. List it once in `brick.yaml` and every page uses its components by name, with no `use:` and no copies:

```yaml
# brick.yaml
libraries:
  - brick-kit                                  # a library the host offers (the brick CLI offers the kit)
  - github.com/acme/charts@v1.4.0              # a library in a repository, fetched by `brick vendor`
  - {from: github.com/acme/ui@v2.1.0, prefix: Acme, only: [DatePicker]}
  - ~/ui                                       # a folder in this project
```

```html
<Hero title="Build pages, not pipelines" />
<AcmeDatePicker name="due" />
```

- **Your project always wins:** `use:` first, then your own component folders, then the libraries in the order listed. Every shadowed name is logged, naming both files.
- **`prefix:`** keeps two libraries' `Button`s apart (`<AcmeButton>`); **`only:`** takes just the named components.
- **Inside a library, `~/` is the library's own root,** so its components import each other wherever it sits and can never read your project's files.
- **`global:`** in the manifest links a component's CSS and JavaScript on every page that uses the library: the kit's design tokens arrive without a `<Theme />` line in the layout.
- **From Go,** pass libraries directly — `brick.Options{Libraries: []brick.Library{kit.Library()}}` — or offer them to `brick.yaml` by name with `cfg.Apply(&opts, kit.Library())`.
- **Shipping your own:** push the folder to a repository and tag it (users write `github.com/you/ui@v1.0.0` and run `brick vendor`), and optionally add a Go file that embeds it and returns a `brick.Library`.

## A component kit

[`kit/`](kit) is a full UI library of more than 130 components:

- **Foundations:** tokens with light and dark themes, icons and typography.
- **Actions:** buttons, toggles and a floating action button.
- **Forms:** fields, a combobox, date and time pickers, multi-select, a rich-text editor and a file dropzone.
- **Data display:** tables and a data table with sort, filter and paging, avatars, stats, a timeline, a tree and a sortable list.
- **Feedback and overlays:** alerts, toasts, dialogs, drawers, menus, popovers and a ⌘K command palette.
- **Navigation and layout:** tabs, a sidebar, an app shell, a menubar, a mega-menu and resizable panels.
- **Charts and mockups:** server-rendered bar, line, donut, sparkline and heatmap charts, and browser, phone, window and terminal mockups.

Every component is complete HTML without JavaScript and gets better with it. The kit is a [component library](#component-libraries), so there is nothing to copy and no theme line in the layout:

```yaml
# brick.yaml, with the brick CLI (which ships the kit)
libraries: [brick-kit]
```

```go
// or from Go: the kit is embedded in the github.com/ubgo/brick/kit package
opts := brick.Options{FS: root.FS(), Libraries: []brick.Library{kit.Library()}}
```

A project that is not on Go can vendor it instead (`libraries: [github.com/ubgo/brick/kit@main]`, then `brick vendor`). The layout needs only `{{ assets }}`; `class="bk-base"` on `<body>` opts in to the kit's base font, colours and focus rings. [`kit/README.md`](kit/README.md) has the catalog and the authoring rules; `kit_lint_test.go` enforces the rules.

The showcase [`examples/ui`](examples/ui) has a page for every component. It also has demo apps built only from the kit, on SQLite through the Starlark `db` capability:

- Contacts: full CRUD
- Invoices: line items and totals computed on the server
- A task board with drag and drop
- A dashboard
- An inbox
- A store with a cart
- Chat
- A calendar
- A blog CMS whose HTML is sanitized
- Settings
- Sign-in
- A landing page

Run it with `task ui:serve` and open http://localhost:8002.

## Documentation

- [`docs/README.md`](docs/README.md): the map. A twelve-step learning path, a "start from your goal" table, and every guide, reference and example in one place.
- [`docs/getting-started.md`](docs/getting-started.md): install, a first page, a component and an action.
- [`docs/choosing/`](docs/choosing/README.md): decision guides for state stores, interactivity, forms, hosting and component sources.
- [`docs/guides/`](docs/guides/components/README.md): step-by-step guides by topic ([components](docs/guides/components/README.md), [site](docs/guides/site/README.md), [interactivity](docs/guides/interactivity/README.md), [forms](docs/guides/forms/README.md), [live updates](docs/guides/live/README.md), [production](docs/guides/production/README.md), [tooling](docs/guides/tooling/README.md)), each built from a runnable example whose test checks the output shown.
- [`docs/reference/`](docs/reference/features.md): the [feature index](docs/reference/features.md), the [`brick` command](docs/reference/cli.md), [frontmatter keys](docs/reference/frontmatter.md), the [attribute reference](docs/reference/attributes.md) and the [browser runtime API](docs/reference/runtime-api.md).
- [`docs/guide.md`](docs/guide.md): every feature on one long page, as the problem, the fix and the details.
- [`docs/cookbook.md`](docs/cookbook.md): real tasks start to finish, with every file and the exact output. Each recipe is executed by [`examples/cookbook_test.go`](examples/cookbook_test.go), and the guide's outputs by [`examples/guide_test.go`](examples/guide_test.go), so a documented output is what the engine produces.
- The package docs (`go doc github.com/ubgo/brick`) and the runnable `Example` functions.

Snippets in the guides are copied from the tested example files and checked by `task docs:snippets`; links and anchors are checked by `task docs:links`.

## Example

[`examples/`](examples) shows every feature in a runnable project — see its README for the table of where each one lives:

```sh
cd examples
go run . -serve :8080        # the demo site: http://localhost:8080, /fr/, /?as=Sara
go run . -check              # every problem at once
BRICK_SECRET=<key> go run . -out build   # the static site; its stateful pages need the key your action server uses
go run ./engineonly          # the engine alone in a net/http app
```

Stateful components seal their state into the page with the keys in `BRICK_SECRET` (comma-separated base64, newest first), so an open page keeps working across a restart and any server with the same keys answers its actions. `brick keygen` prints a new key; keep it in a secret manager, never in code. Without one, `serve` makes a key for the process (a restart then invalidates state, which recovers in place) and an export of a stateful page fails, because it would carry handles no server can open. The repo's `task demo`, `task demo:build` and `task ui:serve` read a dev key from gopass.

## Development

```sh
task js:deps  # once: jsdom for the browser-runtime tests (they skip without it)
task check    # gofmt, vet, staticcheck, cross-builds, -race tests in every module
task e2e      # the demo site in a real headless Chrome: actions, bindings, navigation
task ui:test  # the kit's lint and jsdom tests and the showcase tests
task ui:e2e   # the kit showcase and demo apps in a real headless Chrome
task e2e:both # both examples mounted under /ui/ and /engine/ on one server, in headless Chrome
task test:v21:chrome # the v21 runtime features against runtime/test/v21site, in headless Chrome
task test:v22:chrome # the v22 runtime features (Modelable, server transitions, .norepeat, .details) against the showcase, in headless Chrome
task --list   # everything else
```

## FAQ

**Is brick a new template language?** No. Every `{{ }}` is Go's standard `html/template`, which does all the escaping. brick adds a calling convention on top: component tags, props, slots and blocks.

**Does brick need a build step or code generation?** No. Templates are read and compiled at runtime from an `fs.FS`, so there is nothing to regenerate: a new `.html` file is a component, and under `brick serve` or the site watcher a saved change shows on the next request. `brick build` exists only to export a static site.

**Do I need to write JavaScript?** No. Pages are plain server-rendered HTML. Interactivity (actions, bindings, navigation, islands) comes from a small runtime that `{{ assets }}` links for you, and every kit component works with JavaScript turned off.

**Can I use brick inside an existing `net/http` app?** Yes. Create an engine with `brick.New` and call `Render` from any handler; [`examples/engineonly`](examples/engineonly) is a runnable example. The `site` package is optional.

**Is it safe to render user data?** Data is escaped by `html/template`. Template files, layouts, components and scripts are treated as trusted code, every path is confined to the project root, scripts run in a sandbox with step and time limits, and `{{ sanitize }}` cleans untrusted HTML through a pluggable policy. See the [security model](#security-model).

**Can I host a brick site under a sub-path or as static files?** Yes. `brick build -o public` exports the whole site, and `-base /docs` (or `site.Options.BasePath`) hosts it under a path such as a GitHub Pages project site. Several sites can share one server with `site.Mount`.

## Status

v1 — the render engine — and v2 — the site layer and CLI — are complete. So are Tailwind and images (v2.1), Phase 1 (islands and the mount runtime), Phase 1.5 (navigation) and Phase 2 (actions) and Phase 3 (bindings), v3 remote components, v4 component libraries, v5 sealed, pluggable state, v6 action effects and events between components, v7 forms, validation and uploads, v8 the remaining interactivity gaps (runtime, server state, lazy islands and streamed replies) and v9 the parity-audit gaps (row streams, declared slots, batch loaders, HTML balance checks, a state-level test harness and `brick fmt`). v10 added the Dev toolbar with `Result.Stats`, `brick schema`, showcase demos for every v9 feature and fewer allocations per action. v11 added `brick lsp`, a language server, with a VS Code client in `editors/vscode`. v12 added `host:` in `brick.yaml`, so the CLI can check a site whose Go program supplies script globals and uploads. v13 added `brick new site`, `brick version`, `-json` for `build` and `vendor`, per-command flags and formatting in `brick lsp`; locked state fields (`brick: {locked:}`); `.Page.Translations` and `.Page.URL` for `hreflang` links; HTML highlighting in the VS Code client; and `bricktest` drivers for `brick:change`, `brick:keydown` and `brick:intersect`. v14 added `brick:input`, `brick:focus`, `brick:blur`, `brick:keyup`, key combos (`brick:keydown.ctrl.enter`) and `brick:click.outside`; output pages (`feed.xml.html`, `robots.txt.html`); a built-in, opt-in sitemap; `.Page.Headings` for a table of contents; and heading ids and highlighting in the goldmark adapter. v15 added loader answers (`brick.redirect`, `brick.error` with `content/error.html`, `brick.header`) and protected pages through island loaders; `brick:intersect.dwell` for impressions and `brick:poll.keep-alive`; and find references and component rename in `brick lsp`. v16 added forms that work without JavaScript and image encoders with WebP. v17 added server push over SSE: `brick: {listen:}`, `brick.publish`, signed topic subscriptions and `contrib/push-redis`. v18 built the parked parity gaps: client commands (`brick:js.*`), exit transitions (`brick:leave`), `brick:confirm.prompt`, background jobs (`brick.start_job`), `<brick:style scoped="inclusive">` with `Options.StyleTransform`, and closed-by-default state (`Options.LockState`, `brick: {writable:}`). v19 cut push streams at token expiry, on a recheck and on `RevokeSession`; made file uploads work in forms without JavaScript; added validation rules on list items (`todos.*.title`), the `InstanceEndHook` and prop rename in `brick lsp`. v20 streamed islands into the page response (`brick:lazy="stream"`); added `brick.errors(el)`, `brick:scroll`, Dropzone image previews, struct-typed props (`fields:` / `of:`), route metadata on `Site.Routes()`, per-component action logging (`brick: {log:}`), `bricktest` child and effect helpers, `brick fmt` options (`fmt:` in brick.yaml), live frontmatter checking in the VS Code client and lazy islands in the Dev toolbar. v21 closed the short tail of the parity scan: `brick:focus.window` / `brick:blur.window`, `brick:loading` call, `$set` and `.except` targets, `brick:enter` and transitions on `brick:show`, `brick.js`, `brick.find` / `brick.all`, valueless `brick:disable-with`, job cancel (`brick.cancel_job`), `brick.clear_flash`, `Options.UploadCheck`, `bricktest` `SubmitWith`, `AssertViewHas` and `WithLazyInline`, and `fmt.inline`. v22 built the thirteen items that had waited on the owner: per-component URL callbacks (`brick: {params:}`) with patch links, navigation fragment mode, Modelable components, presence, dynamic output pages, per-request pages, form objects (`.Form`), regions (`brick.region`), server transition control (`brick.transition`), `brick:keydown.norepeat`, `brick:click.details`, sibling component stylesheets and the `:prop` shorthand. Phase 4 — a WebSocket transport with expression-level diffs — stays a maybe; the design already reserves what they need (instance ids, chunked output, reserved `brick:*` names).

## License

Apache-2.0. See [LICENSE](LICENSE). To contribute, read [CONTRIBUTING.md](CONTRIBUTING.md); to report a vulnerability, follow [SECURITY.md](SECURITY.md).
