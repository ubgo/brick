# The `brick` command

Every `brick` subcommand and every flag, from the CLI's own usage text and flag definitions in `cmd/brick`. The outputs below were copied from real runs against a site made with `brick new site clisite` (kit Card page, plus `Greeting`, `Counter -state` and a `blog/hello` page added with `brick new`). Long outputs are trimmed where marked.

- [Install](#install)
- [Exit codes](#exit-codes)
- [Flags are per command](#flags-are-per-command)
- [brick check](#brick-check)
- [brick routes](#brick-routes)
- [brick build](#brick-build)
- [brick serve](#brick-serve)
- [brick vendor](#brick-vendor)
- [brick docs](#brick-docs)
- [brick new](#brick-new)
- [brick fmt](#brick-fmt)
- [brick keygen](#brick-keygen)
- [brick schema](#brick-schema)
- [brick lsp](#brick-lsp)
- [brick version](#brick-version)
- [Environment: BRICK_SECRET](#environment-brick_secret)
- [What the CLI turns on](#what-the-cli-turns-on)

## Install

```sh
go install github.com/ubgo/brick/cmd/brick@latest
```

In the brick repository, `task cli:install` does the same from the working tree. `brick`, `brick -h`, `brick --help` and `brick help` print the usage text; with no arguments at all it also exits 2.

## Exit codes

| Code | Meaning |
|---|---|
| `0` | Success. Warnings alone never fail a command. |
| `1` | The command ran and reports a failure: an error diagnostic (`check`), a failed export (`build`), a file that already exists (`new`), a file `fmt` cannot read or parse, a bad `BRICK_SECRET`. |
| `2` | A usage mistake: an unknown command, an unknown or misplaced flag, more than one directory, `keygen` or `lsp` given arguments, `brick` with no arguments. |

## Flags are per command

Each command registers only the flags it reads, so a flag that belongs to another command is a usage error instead of being ignored. `brick <command> -h` lists that command's flags and exits 0.

```console
$ brick check -addr :1 clisite
flag provided but not defined: -addr
Usage:
  brick check [-json] [-drafts] [-locales en,fr] [dir]

Report every problem in the site at once; exit 1 when any is an error.
dir defaults to the current directory.

Flags:
  -drafts
    	include draft pages
  -json
    	print JSON
  -locales string
    	comma-separated locales with their own URLs, default first (e.g. en,fr)
$ echo $?
2
```

The site commands (`check`, `routes`, `build`, `serve`, `vendor`, `docs`) take at most one directory, which defaults to the current one; `brick check a b` prints `brick: one directory at most` and exits 2. They read `brick.yaml`, `site.yaml`, `data/` and `i18n/` when present.

| Flag | Commands | Default | Meaning |
|---|---|---|---|
| `-json` | check, routes, build, vendor, docs (and version) | `false` | Print JSON instead of text. |
| `-drafts` | check, routes, build | `false` | Include draft pages. `serve` always includes them. |
| `-locales en,fr` | check, routes, build, serve | `""` | Comma-separated locales with their own URLs, default first. |
| `-base /path` | build, serve | `""` | Host the site under this path; links and assets are rewritten. Must start with a single `/`. |
| `-keep /a/,/b/` | build, serve | `""` | Comma-separated root-absolute prefixes the `-base` rewrite leaves alone, for links to a sibling site on the same host (`site.Options.KeepPaths`). |
| `-o dir` | build | `public` | The output directory. |
| `-addr :8080` | serve | `:8080` | The listen address. |
| `-check` | vendor | `false` | Verify `brick_modules/` against `brick.lock` without fetching. |

## brick check

```text
brick check [-json] [-drafts] [-locales en,fr] [dir]
```

Compiles every file and renders every page, then reports every problem at once, sorted by file and line. Exit 1 when any diagnostic is an error.

```console
$ brick check clisite
ok: 2 pages
```

A page with a mistake, as text and as JSON:

```console
$ brick check brokensite
content/oops.html:4:1: error: unknown component <Nosuch> — no file in use: or the roots (components) defines it
$ echo $?
1
$ brick check -json dup
[
  {
    "file": "content/x.html",
    "line": 1,
    "col": 4,
    "severity": "error",
    "code": "syntax",
    "message": "unclosed {{"
  },
  {
    "file": "content/x.html",
    "line": 1,
    "col": 4,
    "severity": "error",
    "code": "syntax",
    "message": "content/x.html:1:4: unclosed {{"
  }
]
```

Each JSON item has `file`, `line` and `col` (omitted when zero), `severity` (`error` or `warning`), `code` and `message`. Today an error the render also hits is listed twice, the second time with its location repeated in the message; read the first.

Warnings are printed before `ok:` and do not change the exit code; for example a project component that shadows a kit one:

```text
brick_libs/brick-kit/components/Card.html: warning: <Card> from library brick-kit is shadowed by ~/components/Card.html (the project and earlier libraries win; use a prefix to keep both)
ok: 2 pages
```

A page that uses a capability only the site's host program has (`host:` in `brick.yaml`) is reported as a warning, `not rendered by this check: …`, rather than an error.

`host:` is how a site tells `brick check` and `brick lsp` what its own Go program supplies. It lists the script globals the host predeclares and says whether it configures an upload sink; the CLI stands in for them so the site compiles, and a real render is unchanged because `Apply` ignores the section.

```yaml
# brick.yaml
host:
  globals: [stamp]
  uploads: true
```

With a page whose script calls `stamp()`, the check without `host:` fails, and with it only warns that the page was not run here:

```console
$ brick check .
content/index.html:4: error: content/index.html#script:6:21: undefined: stamp
$ cat brick.yaml
host:
  globals: [stamp]
$ brick check .
content/index.html: warning: not rendered by this check: load: stamp_line: content/index.html#script:6:26: stamp (declared in brick.yaml host: globals): only the site's host program supplies this, so it does not run here
ok: 1 pages
```

## brick routes

```text
brick routes [-json] [-drafts] [-locales en,fr] [dir]
```

Lists every URL and the file behind it, then the dynamic pages with their parameters.

```console
$ brick routes clisite
/              content/index.html
/blog/hello/   content/blog/hello.html
/blog/[slug]/  content/blog/[slug].html  (slug)
```

With `-locales`, each locale after the first gets its own prefix:

```console
$ brick routes -locales en,fr
/                content/index.html
/blog/hello/     content/blog/hello.html
/fr/             content/index.html
/fr/blog/hello/  content/blog/hello.html
```

`-json` prints one object per route (trimmed to one):

```json
[
  {
    "URL": "/blog/hello/",
    "Path": "content/blog/hello.html",
    "Locale": "",
    "Markdown": false,
    "Title": "Hello",
    "Section": "blog",
    "Meta": {
      "title": "Hello"
    }
  }
]
```

A page with `draft: true` is listed only with `-drafts`.

## brick build

```text
brick build [-json] [-drafts] [-o dir] [-base /path] [-keep /a/,/b/] [-locales en,fr] [dir]
```

Writes the static site: pages, static files, component assets and, when configured, `sitemap.xml`. When `brick.yaml` has a Tailwind section it rebuilds the CSS first and prints `tailwind: rebuilt <file>` (to stderr with `-json`, so stdout stays parseable).

```console
$ brick build
wrote 2 pages, 0 static files and 3 assets to public
$ brick build -json -o dist
{
  "pages": 2,
  "static": 0,
  "assets": 3,
  "sitemap": false,
  "out": "dist"
}
```

A `brick: {per_request: true}` page is served, not built, and is reported as `skipped <url>: brick: {per_request: true} pages are served, not built` (and in the JSON `skipped` list).

A page with a stateful component needs `BRICK_SECRET`, because its state is sealed into the HTML with the key the action server will use:

```console
$ brick build -o x
brick: site: export /clicks/: site: a page has a stateful component, and exporting it needs Options.StateKeys shared with the action server
brick: set BRICK_SECRET to the key the action server uses (brick keygen prints a new one)
$ echo $?
1
$ BRICK_SECRET=$(brick keygen 2>/dev/null) brick build -o x
wrote 3 pages, 0 static files and 3 assets to x
```

A dynamic page needs `brick: {paths: …}` to be exported; `brick new page blog/[slug]` writes one without it, so a build of a fresh dynamic page fails until you add it:

```text
brick: site: export content/blog/[slug].html: a dynamic page needs brick: {paths: …} to be exported, naming the script function that lists its route values
```

A failed build may already have written some files into the output directory; do not publish it.

`-base` rewrites links and asset URLs for a site hosted under a path (a GitHub Pages project site):

```console
$ brick build -base /docs -o based
$ grep -o 'href="[^"]*"' based/index.html | head -1
href="/docs/_brick/c/Button.1e124ea1.css"
$ brick build -base docs -o b2
brick: site: BasePath "docs" must start with a single /
```

`-keep` lists prefixes that belong to another site on the same host, so the rewrite leaves them alone. From `examples/site-subpath/docs`, which links to `/shop/`:

```console
$ brick build -base /docs -o plain docs
$ grep -o 'href="/[^"]*shop/"' plain/index.html
href="/docs/shop/"
$ brick build -base /docs -keep /shop/ -o kept docs
$ grep -o 'href="/[^"]*shop/"' kept/index.html
href="/shop/"
```

## brick serve

```text
brick serve [-addr :8080] [-base /path] [-keep /a/,/b/] [-locales en,fr] [dir]
```

The dev server: drafts included, live reload on every save, and the component playground at `/_brick/play/<Component>` (for example `/_brick/play/Card?title=Hi`). It stops on Ctrl-C (SIGINT) or SIGTERM and exits 0.

```console
$ brick serve -addr 127.0.0.1:18765
brick: warning: BRICK_SECRET is not set, so state is sealed with a key made for this process: a restart invalidates it (components recover in place, keeping what was typed). Set BRICK_SECRET (brick keygen prints one) to keep state across restarts.
serving . on http://127.0.0.1:18765 (Ctrl-C to stop)
```

An address that starts with `:` is printed as `localhost:<port>`. Without `BRICK_SECRET`, `serve` makes a key for the process; with it, state survives a restart and any server with the same keys answers an action.

## brick vendor

```text
brick vendor [-check] [-json] [dir]
```

Fetches remote `use:` imports and libraries into `brick_modules/` and pins them in `brick.lock`. `-check` verifies `brick_modules/` against `brick.lock` without the network, which is what CI wants.

```console
$ brick vendor
vendored 0 files (0 fetched) into brick_modules/; brick.lock written
$ brick vendor -check
ok: brick_modules/ matches brick.lock
$ brick vendor -json
{
  "files": [],
  "fetched": 0,
  "checked": false
}
```

See [Using someone else's components](../guide.md#using-someone-elses-components).

## brick docs

```text
brick docs [-json] [dir]
```

Loads the site the way `check` and `build` do (brick.yaml and vendored libraries included) and prints every component the site can call: its file, how it renders (stateful, island), and each prop with its type, default, required flag, allowed values, doc and examples, and each slot. A component whose frontmatter does not parse is an error, exit 1.

```console
$ brick docs | grep -A3 '^Counter'
Counter  components/Counter.html  (stateful, island)
  props:
    title  string  default "Counter"

$ brick docs | grep -A6 '^Accordion'
Accordion  brick_libs/brick-kit/components/Accordion.html
  props:
    group  string  default ""
    open  bool  default false
    title  string  required
    variant  string  default "default"  one of "default", "bordered", "separated"
```

`-json` prints an array; one prop looks like `{"name": "size", "type": "string", "default": "md", "hasDefault": true, "oneof": ["sm", "md"]}`. `brick docs -json | jq '.[] | select(.name == "Card")'` gives one component's contract.

## brick new

```text
brick new site <dir>
brick new component <Name> [-state] [-client] [-island] [-lazy] [-dir site]
brick new page <path> [-dir site]
```

Writes a starter project or file. It never overwrites a file, and `new site` refuses a folder that is not empty.

| Flag | Applies to | Default | Meaning |
|---|---|---|---|
| `-state` | component | `false` | Declare state and one action (a stateful component). |
| `-client` | component | `false` | Add client code run by `brick.mount`. |
| `-island` | component | `false` | Render per request (`brick: {island: true}`). |
| `-lazy` | component | `false` | A lazy island, fetched after the page loads (`brick: {lazy: visible}`). |
| `-dir` | component, page | `.` | The site directory. |

```console
$ brick new site clisite
wrote clisite/brick.yaml
wrote clisite/site.yaml
wrote clisite/layouts/default.html
wrote clisite/content/index.html

Next:
  brick serve clisite    http://localhost:8080
  brick check clisite
$ brick new component Counter -state -dir clisite
wrote components/Counter.html
$ brick new page 'blog/[slug]' -dir clisite
wrote content/blog/[slug].html
$ brick new page blog/hello -dir clisite
brick new: clisite/content/blog/hello.html already exists; brick new never overwrites
$ brick new site clisite
brick new site: clisite is not empty; brick new site writes only into a new or empty folder
```

The printed path of a component or page is relative to `-dir`. The generated `Counter.html`:

```html
---
props:
  title: {type: string, default: "Counter"}
state:
  count: 0
---
{{/*
Counter: one sentence on what it is for.

  <Counter title="Clicks" />

Stateful: its state lives on the server and each click runs the
increment action, which returns the new state; the component re-renders
in place.
*/}}
<brick:script>
def increment(state, props):
    return dict(state, count=state["count"] + 1)
</brick:script>
<section class="counter">
  <h2>{{ .Props.title }}</h2>
  <button type="button" brick:click="increment">Clicked {{ .State.count }} times</button>
</section>
```

Page paths: `blog/hello` writes `content/blog/hello.html`, `blog/[slug]` a dynamic route answering `/blog/<slug>/`, `docs/[...path]` a catch-all route.

## brick fmt

```text
brick fmt [-l] [-w] [-d] [-v] [-line-length N] [-attr-order a,b] [-inline a,em] [path ...]
```

Formats brick `.html` files without changing what they render. With no path it formats the current directory; `-` reads standard input. The [fmt guide](../guides/tooling/fmt.md) shows each option on a real file.

| Flag | Default | Meaning |
|---|---|---|
| `-l` | `false` | List the files whose formatting differs, instead of printing them. |
| `-w` | `false` | Write the result back to each file. |
| `-d` | `false` | Print a unified diff instead of the file. |
| `-v` | `false` | With `-l`, `-w` or `-d`: say why a file's indentation was left alone. |
| `-line-length n` | unset | Wrap lines longer than `n` (overrides `fmt.lineLength`; `0` turns wrapping off; below 0 is a usage error). |
| `-attr-order list` | unset | Comma-separated attributes to put first (overrides `fmt.attrOrder`). |
| `-inline list` | unset | Comma-separated elements whose content is not indented (overrides `fmt.inline`). |

```console
$ brick fmt -l .
components/Panel.html
$ brick fmt -line-length -2 x
brick fmt: -line-length must be 0 or more
$ printf '<p>{{ .x </p>\n' | brick fmt -
brick fmt: 1:4: unclosed {{
$ echo $?
1
```

Exit 1 when a file cannot be read or parsed, 0 otherwise (a file that needs formatting is not a failure, even with `-l`).

## brick keygen

```text
brick keygen
```

Prints a new 32-byte state key, base64 (44 characters), on stdout, alone, so it pipes straight into a secret manager. How to use it goes to stderr. It never writes a file, and takes no arguments (`brick keygen: takes no arguments`, exit 2).

```console
$ brick keygen
<44 base64 characters>
Store it in your secret manager, never in code or brick.yaml, and pass it as BRICK_SECRET:
  brick keygen | gopass insert -f <app>/brick-secret
  BRICK_SECRET=$(gopass show -o <app>/brick-secret) brick serve
Rotate: put the new key first and keep the old one second for one TTL: BRICK_SECRET=new,old
$ brick keygen -h
brick keygen prints a new 32-byte state key, base64, for BRICK_SECRET.
```

## brick schema

```text
brick schema [-o file]
```

Prints a JSON Schema (draft 2020-12) for the YAML frontmatter of a page, layout or component: every engine key with its shape and a description. Keys that are not the engine's are the file's params and are allowed. What needs the whole site (a `load:` function that exists, a `rules:` path declared in `state:`) stays `brick check`'s job.

| Flag | Default | Meaning |
|---|---|---|
| `-o file` | stdout | Write the schema to this file instead of stdout. |

```console
$ brick schema -o s.json
wrote s.json
$ brick schema | jq -c '.properties.brick.properties | keys'
["css","init","island","lazy","listen","load_many","lock_state","locked","log","messages","model","on","params","paths","per_request","query","remember","rules","sitemap","slots","store","temporary","updated","uploads","url","writable"]
```

Point an editor's YAML schema setting at the written file (for example `.vscode/brick-frontmatter.schema.json`); the VS Code extension and `brick lsp` read the schema from the binary instead.

## brick lsp

```text
brick lsp
```

The language server, on stdin and stdout. An editor starts it; it is not run by hand, and it takes no arguments. It answers diagnostics, completion, hover, definition, references, rename and formatting. See the [editors guide](../guides/tooling/editors.md) for what each does and a real exchange.

## brick version

```text
brick version [-json]
```

The module version and VCS revision the binary was built from.

```console
$ brick version
brick v0.0.0-20261013175757-f76fdd502477+dirty (rev f76fdd502477, modified, 2026-10-13T17:57:57Z) go1.27.1
$ brick version -json
{
  "version": "v0.0.0-20261013175757-f76fdd502477+dirty",
  "revision": "f76fdd5024773c1f177504009359d9541fbdd3fb",
  "time": "2026-10-13T17:57:57Z",
  "modified": true,
  "go": "go1.27.1"
}
```

From Go, parse the JSON to log which build ran. `examples/tooling-cli` does this in `binaryVersion`:

<!-- ds:block id=binaryversion-v8vhcnpd title="examples/tooling-cli/main.go" -->
**examples/tooling-cli/main.go** · [`examples/tooling-cli/main.go:66-76`](../../examples/tooling-cli/main.go#L66-L76)

```go
func binaryVersion(bin string) (Version, error) {
	out, err := run(bin, "version", "-json")
	if err != nil {
		return Version{}, err
	}
	var v Version
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		return Version{}, fmt.Errorf("brick version -json: %w", err)
	}
	return v, nil
}
```
<!-- /ds:block hash=d097e8 -->

## Environment: BRICK_SECRET

`BRICK_SECRET` holds the state keys: comma-separated base64, newest first. The first key seals, every key opens, so rotation is `BRICK_SECRET=new,old` for one TTL, then `new` alone. It is read by every site command except `vendor`; a value that does not parse fails the command:

```console
$ BRICK_SECRET=notbase64 brick check
brick: BRICK_SECRET: brick: state key 1 is not base64
```

Unset, `serve` makes a key for the process (with the warning above), `build` refuses a page with a stateful component, and `check`, `routes` and `docs` keep state in memory. See [State that survives a restart](../guide.md#state-that-survives-a-restart--brick_secret).

## What the CLI turns on

The CLI opens a site with the goldmark Markdown renderer, the bluemonday sanitizer, the Starlark script runtime and the WebP image encoder. Scripts get no `db` or `http` capability: granting one is a decision for a host's Go code, which declares it in `brick.yaml` under `host:` so `check` can report pages that need it. The kit is built in: `libraries: [brick-kit]` in `brick.yaml` makes every kit component available with no copy and no vendoring.

## Related

- [Formatting with brick fmt](../guides/tooling/fmt.md)
- [Editors: brick lsp and VS Code](../guides/tooling/editors.md)
- [Testing themes with bricktest](../guides/tooling/bricktest.md)
- [Feature index: CLI](features.md#cli)
