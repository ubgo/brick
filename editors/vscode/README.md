# brick for VS Code

A thin VS Code client for `brick lsp`, the brick language server. Every answer comes from the server, which runs the same engine calls as the CLI, so the editor and `brick check` never disagree.

## What it does

- **Highlighting**: a TextMate injection into VS Code's HTML grammar colours `{{ … }}` actions (keywords, `$variables`, `.Fields`, strings, pipes) anywhere, including inside attribute values; component tags (`<Card>`, `<ui.Card>`, `<x-card>`) and engine tags (`<brick:prop>`); and `brick:*` attributes in any tag. It works without the server.
- **Frontmatter**: the `---` block at the top of a file is highlighted as YAML (VS Code's own YAML grammar), and checked as you type against the schema `brick schema` prints: an unknown `brick:` key, a value of the wrong type or outside its allowed values, and a YAML syntax error are underlined where they are, with the source "brick frontmatter". The schema comes from the same `brick` binary the server runs, so the check, completion and `brick check` agree.
- **Diagnostics**: what `brick check` reports, per file, after each open and save (a burst of saves runs one check).
- **Completion**: component tags after `<` (`<Card>`, or `<x-card>` once you type `x-`), a component's props inside its tag (with type, default, required and allowed values), the values of a `oneof` prop, and frontmatter keys and values inside the `---` block, including every `brick:` setting.
- **Hover**: a component's props and slots, a prop's type and doc, a frontmatter key's meaning.
- **Go to definition**: a component tag opens the component's file.
- **Find all references**: on a component tag (`<Card>`, `<x-card>`, open or close), or anywhere in the component's own file, every call of it across the site's pages, layouts and components, in both tag forms and through `use:` aliases; on a prop (its `props:` key, a `.Props.title` use, or the prop inside a component tag), its declaration, its uses inside the component and every place it is passed (attributes, `:title=` and `<brick:prop name="…">` blocks). A tag-shaped string inside `{{ }}`, a comment, a raw body or a `<brick:script>` is not a call, so it is not listed.
- **Rename symbol** (F2 on a component tag): renames every tag that calls the component, each in its own form (`<Card>` → `<Tile>`, `<x-card>` → `<x-tile>`), rewrites `use:` paths that name its file, and renames the file itself with its locale variants (`Card.fr.html`), all as one undoable edit. A tag reached through a `use:` alias keeps the alias. A kit component (no file), a vendored library's component, a name that is not a component name and a name already taken are refused with the reason.
- **Rename a prop** (F2 on a prop's name in `props:`, on `.Props.title`, on `title=` at a call, or on a `<brick:prop name>` block): renames the declaration, every use in the component and its locale variants, `props["title"]` in its script, and every call site across the site. Refused with the reason when the new name clashes or a call cannot be followed (`brick:spread`, `.Props` handed on whole); a warning lists any use brick could not tell was this prop.

The site is the nearest folder above the open file that holds `brick.yaml` or `site.yaml`, else the workspace folder, so one workspace can hold several sites.

## Install

1. Install the brick CLI so `brick` is on your PATH: `go install github.com/ubgo/brick/cmd/brick@latest` (or `task cli:install` in the brick repository).
2. In this folder, run `npm install`.
3. Either run it from source (open this folder in VS Code and press F5 to start an Extension Development Host), or package and install it: `npx @vscode/vsce package`, then `code --install-extension brick-lsp-0.11.0.vsix`.

The extension is not published to the Marketplace.

## Tests

`task vscode:test` (or `npm test` here) tokenizes brick markup and frontmatter through the grammars and checks the scopes, checks sample frontmatter against the real schema (from `$BRICK_BIN`, or built from this repository with Go), and runs `extension.js` against a stand-in for the VS Code API to check that open documents and edits get the frontmatter diagnostics. What those tests cannot show is how VS Code paints them; that needs the extension running in VS Code.

## Settings

| Setting | Default | Meaning |
|---|---|---|
| `brick.path` | `brick` | The brick binary that runs `brick lsp`. A bare name is looked up on PATH; changing it restarts the server. |

## How it activates

It starts when the workspace contains a `brick.yaml` or `site.yaml` anywhere, and serves `.html` files (VS Code's `html` language). The server's own messages go to the "brick" output channel.

## Limits

- Diagnostics follow saves, not keystrokes: `brick check` renders every page, which takes seconds on a large site. The frontmatter check is the exception: it needs only the file, so it runs as you type.
- A frontmatter mistake that `brick check` also reports shows twice after a save, once from each source.
- The frontmatter check needs a `brick` with the `schema` command (v10 or later); with an older one it is off, and the "brick" output channel says why. Page params (keys outside the engine's) are free-form, so only the engine's keys are checked.
- A component built into the brick binary (the brick kit through `libraries: [brick-kit]`) has no file on disk, so go to definition has nowhere to go; hover and completion still work. A kit vendored into `brick_modules/` does open.
- References and rename read the site's files from disk (open editors' unsaved text wins) and skip `brick_modules/` and `node_modules/`, so calls inside a vendored library are neither listed nor renamed. Rename covers components and props; a prop passed from Go host code (`RenderComponent(name, map)`) or read in a `.star` module is not seen.
- Frontmatter completion reads block and flow YAML (`props:` lines of `name: {type: …}`); it does not follow anchors or multi-line flow mappings.

## Other editors

Any LSP client can run `brick lsp` over stdin and stdout; point it at `.html` files.
