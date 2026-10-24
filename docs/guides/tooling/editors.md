# Editors: `brick lsp` and VS Code

## What it is and when you want it

`brick lsp` is a language server for brick `.html` files: an editor starts it and talks the Language Server Protocol over stdin and stdout. It reports what `brick check` reports, completes component tags, props and frontmatter keys, shows a component's contract on hover, jumps to a component's file, finds every call of a component or prop, renames components and props across the site, and formats a document as `brick fmt` does. `editors/vscode` in the brick repository is a thin VS Code client for it that also adds highlighting and as-you-type frontmatter checks. Use it whenever you edit a theme: the editor and `brick check` run the same engine calls, so they never disagree.

## Before you start

- The `brick` CLI on your PATH ([install](../../reference/cli.md#install)). The extension runs `brick lsp` and `brick schema` from that binary.
- A site: the server treats the nearest folder above the open file that holds `brick.yaml` or `site.yaml` as the site, else the workspace folder, so one workspace can hold several sites.
- For VS Code: Node.js and npm, to install the extension's dependencies. The extension is not published to the Marketplace.

## Step 1 — Install the VS Code extension

The extension lives in the brick repository and is installed from source.

```sh
go install github.com/ubgo/brick/cmd/brick@latest
cd editors/vscode
npm install
npx @vscode/vsce package
code --install-extension brick-lsp-0.11.0.vsix
```

Or open `editors/vscode` in VS Code and press F5 to run it in an Extension Development Host. It activates when the workspace contains a `brick.yaml` or `site.yaml` anywhere, and serves `.html` files. Its messages go to the "brick" output channel.

## Step 2 — Point it at your binary (optional)

The extension has one setting. A bare name is looked up on PATH; changing it restarts the server.

```json
{
  "brick.path": "/path/to/go/bin/brick"
}
```

| Setting | Default | Meaning |
|---|---|---|
| `brick.path` | `brick` | The brick binary that runs `brick lsp` (and `brick schema` for the frontmatter check). |

## Step 3 — Use it in another editor

Any LSP client can run the server; it takes no arguments and needs no configuration. Register `brick lsp` as the server command for `.html` files. For example, Neovim's built-in client (0.10 or later; not exercised by the brick tests):

```lua
vim.api.nvim_create_autocmd("FileType", {
  pattern = "html",
  callback = function(args)
    vim.lsp.start({
      name = "brick",
      cmd = { "brick", "lsp" },
      root_dir = vim.fs.root(args.buf, { "brick.yaml", "site.yaml" }),
    })
  end,
})
```

You get the server's features (diagnostics, completion, hover, definition, references, rename, formatting). The TextMate highlighting and the as-you-type frontmatter check are part of the VS Code extension only.

## What each feature does

| Feature | What you get |
|---|---|
| Diagnostics | What `brick check` reports, per file, after each open and save. A burst of saves runs one check (the server waits 300 ms after a save). |
| Completion | Component tags after `<` (`<Card>`, or `<x-card>` once you type `x-`), a component's props inside its tag (type, default, required, allowed values), the values of a `oneof` prop, and frontmatter keys and values inside the `---` block, including every `brick:` setting. |
| Hover | A component's props and slots, a prop's type and doc, a frontmatter key's meaning. |
| Go to definition | A component tag opens the component's file. |
| Find references | Every call of a component across pages, layouts and components, in both tag forms and through `use:` aliases; on a prop, its declaration, its uses in the component and every place it is passed (attributes, `:title=`, `<brick:prop name="…">`). A tag-shaped string inside `{{ }}`, a comment, a raw body or a `<brick:script>` is not a call. |
| Rename (F2) | A component: every tag that calls it in its own form (`<Card>` → `<Tile>`, `<x-card>` → `<x-tile>`), the `use:` paths naming its file, and the file itself with its locale variants, as one undoable edit. A prop: its declaration, every use in the component and its locale variants, `props["title"]` in its script, and every call site. |
| Formatting | The document laid out as [`brick fmt`](fmt.md) writes it, with the site's `fmt:` options from `brick.yaml`. |
| Highlighting (VS Code) | `{{ … }}` actions anywhere, including inside attribute values; component tags and engine tags (`<brick:prop>`); `brick:*` attributes. Works without the server. |
| Frontmatter check (VS Code) | The `---` block is highlighted as YAML and checked as you type against the schema `brick schema` prints: an unknown `brick:` key, a wrong type or value, and a YAML syntax error, with the source "brick frontmatter". |

## Try it

The server answers on stdin and stdout, so you can talk to it from a shell. Send one `initialize` request from a site folder and it replies with its capabilities, then exits 0 when stdin closes:

```console
$ msg='{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"capabilities":{}}}'
$ printf 'Content-Length: %d\r\n\r\n%s' ${#msg} "$msg" | brick lsp
Content-Length: 437

{"jsonrpc":"2.0","id":1,"result":{"capabilities":{"completionProvider":{"triggerCharacters":["<"," ",":","-","{","\""]},"definitionProvider":true,"documentFormattingProvider":true,"hoverProvider":true,"referencesProvider":true,"renameProvider":{"prepareProvider":true},"textDocumentSync":{"change":1,"openClose":true,"save":{"includeText":false}}},"serverInfo":{"name":"brick","version":"v0.0.0-20261013175757-f76fdd502477+dirty"}}}
```

`serverInfo.version` is the same module version `brick version` prints, so an editor reports the binary it really runs. A full session in a site made with `brick new site` and `brick new component Greeting`, with a page holding `<Greeting title="Hi" />`, answers a hover on the tag with:

```json
{
  "contents": {
    "kind": "markdown",
    "value": "**<Greeting>** · `components/Greeting.html`\n\nProps:\n\n- `title` string = Greeting"
  },
  "range": {"start": {"line": 3, "character": 1}, "end": {"line": 3, "character": 9}}
}
```

and opening a page that calls a missing `<Gre />` publishes:

```json
{
  "code": "unknown-tag",
  "message": "unknown component <Gre> — no file in use: or the roots (components) defines it",
  "range": {"start": {"line": 3, "character": 0}, "end": {"line": 3, "character": 7}},
  "severity": 1,
  "source": "brick"
}
```

## Pitfalls

- **Diagnostics follow saves, not keystrokes.** The check renders every page, which takes seconds on a large site. Only the VS Code frontmatter check runs as you type, because it needs just the file.
- **Some errors show twice.** The server publishes what `brick check` reports, and today an error the render also hits is listed a second time with its location repeated in the message (`content/oops.html:4:1: unknown component <Gre> …`). A frontmatter mistake can also appear once from "brick frontmatter" and once from "brick" after a save.
- **Kit components have no file.** A component built into the binary (the kit through `libraries: [brick-kit]`) has nowhere for go to definition to go, and rename refuses it: `<Card> is built into brick (the kit); it has no file in this site to rename`. Hover and completion still work. A kit vendored into `brick_modules/` does open.
- **Rename refuses what it cannot follow.** A clash or a call site it cannot read (`brick:spread`, `.Props` handed on whole) is refused with the reason, for example `cannot rename title on <Card>: 1 site(s) brick cannot follow: …`; a prop from a vendored library is refused with `<X> comes from a library in brick_modules; rename its props in the library`. A prop passed from Go (`RenderComponent(name, map)`) or read in a `.star` module is not seen.
- **Vendored code is not searched.** References and rename skip `brick_modules/` and `node_modules/`. Unsaved editor text wins over the file on disk.
- **The frontmatter check needs `brick schema`.** With a `brick` too old to have it, live checking is off and the "brick" output channel says `brick frontmatter: live checking is off: brick schema: …`.
- **`brick lsp` takes no arguments.** `brick lsp extra` prints `brick lsp: takes no arguments` and exits 2.

## Related

- [`brick lsp` and `brick schema` in the CLI reference](../../reference/cli.md#brick-lsp)
- [Formatting with brick fmt](fmt.md)
- [Testing themes with bricktest](bricktest.md)
- [Store conformance with statetest](statetest.md)
- [Feature index: Editors](../../reference/features.md#editors)
- `editors/vscode/README.md` in the repository, for the extension's tests (`task vscode:test`) and limits
