# Component libraries

## What it is and when you want it

A component library is a folder of components that a site uses by name, with no `use:` line in each page. It can sit inside your project, ship inside a Go package (the UI kit does), or live in a Git repository that `brick vendor` copies into your project. Reach for one when the same set of components serves more than one site, or when you want the kit's buttons and dialogs without writing them. For one component from one place, a single `use:` import is lighter (see [Components and props](props.md)).

## Before you start

- You have a site and a Go program that builds the engine from its `brick.yaml`. The [example program](../../../examples/components-libraries/) is one; it uses only a scratch folder and no network.
- A library is a folder with a `brick-library.yaml` at its root. Inside a library `~/` means the library's own root, so its components import each other wherever the folder sits and can never read your project's files.
- Which source suits you is decided in [Choosing where components come from](../../choosing/component-sources.md).

## Step 1 — Write a library folder

The manifest names the library, says which folder holds the components (default `components`), and may list `global:` components whose stylesheets every user of the library gets.

<!-- snippet: examples/components-libraries/theme/ui/brick-library.yaml -->
```yaml
name: team-ui
description: The team's own small components.
components: components
```

A component in it is an ordinary component file.

<!-- snippet: examples/components-libraries/theme/ui/components/Pill.html -->
```html
---
props:
  label: {type: string, required: true}
---
<span class="pill">{{ .Props.label }}</span>
```

## Step 2 — List the libraries in brick.yaml

`libraries:` is read in order: the project's own `components/` folder always wins a name, then the first library that has it, and the engine logs every name it shadowed. Each entry is a plain string (`from` alone) or a map with `from`, `prefix` and `only`. `from` is one of three things: a `~/` folder in the project, a remote library `host/owner/repo@version`, or a bare name of a library the Go program offers.

<!-- snippet: examples/components-libraries/theme/brick.yaml -->
```yaml
# Three kinds of component library, in precedence order (earlier wins a
# name, and the project's own components/ folder beats all of them).
libraries:
  # A folder inside the project. prefix keeps its tags apart from others.
  - {from: "~/ui", prefix: Team}
  # A library the Go program offers by name: here the embedded UI kit, cut
  # down to the two components this site uses.
  - {from: brick-kit, only: [Badge, Button]}
  # A remote library: `brick vendor` copies it into brick_modules/ and pins
  # it in brick.lock; the engine only ever reads the copy.
  - {from: github.com/acme/ui@v1.2.0, prefix: Acme}
```

- `prefix: Team` registers `Pill.html` as `<TeamPill>`, so two libraries' components can coexist.
- `only: [Badge, Button]` limits the library to those components, named as the library names them (before any prefix).

## Step 3 — Apply the config in Go

`LoadConfig` reads `brick.yaml` (an unknown key is an error, so a typo never does nothing) and `Config.Apply` copies its settings into `Options`. The libraries a bare name may pick are passed to `Apply`; nothing else is ever looked up, so a config cannot reach a library the program did not hand over.

<!-- ds:block id=newengine-fz42jpaf title="examples/components-libraries/main.go" -->
**examples/components-libraries/main.go** · [`examples/components-libraries/main.go:105-119`](../../../examples/components-libraries/main.go#L105-L119)

```go
func newEngine(work string) (*brick.Engine, error) {
	root, err := os.OpenRoot(work)
	if err != nil {
		return nil, err
	}
	cfg, err := brick.LoadConfig(root.FS(), configFile)
	if err != nil {
		return nil, err
	}
	opts := brick.Options{FS: root.FS()}
	if err := cfg.Apply(&opts, kit.Library()); err != nil {
		return nil, err
	}
	return brick.New(opts)
}
```
<!-- /ds:block hash=f7fad1 -->

The same options written in Go, with no `brick.yaml`, are a `brick.Library` value: `Dir` for a folder in `Options.FS`, or `FS` for an embedded one, plus `Prefix` and `Only`. The example's `TestLibraryInGoAndKitTheme` runs exactly this.

```go
lib := kit.Library()
lib.Prefix = "K"
lib.Only = []string{"Badge"}
eng, err := brick.New(brick.Options{FS: fsys, Libraries: []brick.Library{lib}})
```

## Step 4 — Vendor a remote library

A remote library is fetched once, at install time, never while rendering. `brick vendor` reads every remote entry of `libraries:` and every remote `use:` in the project, copies the files into `brick_modules/`, and pins each file's hash in `brick.lock`. Commit both. `brick vendor -check` verifies them offline, which makes it a CI gate (see the [CLI reference](../../reference/cli.md#brick-vendor)).

The command does this in Go with `remote.Vendor`, and its two functions are the escape hatch for a mirror or a local source. The example serves a pretend `github.com/acme/ui@v1.2.0` from an embedded folder:

<!-- ds:block id=fetch-6mus9qqw title="examples/components-libraries/main.go" -->
**examples/components-libraries/main.go** · [`examples/components-libraries/main.go:64-70`](../../../examples/components-libraries/main.go#L64-L70)

```go
func fetch(_ context.Context, r brick.Remote) ([]byte, error) {
	base, err := source(r)
	if err != nil {
		return nil, err
	}
	return remoteSrc.ReadFile(path.Join(base, r.Path))
}
```
<!-- /ds:block hash=0ffce7 -->

<!-- ds:block id=list-t2zdw492 title="examples/components-libraries/main.go" -->
**examples/components-libraries/main.go** · [`examples/components-libraries/main.go:75-89`](../../../examples/components-libraries/main.go#L75-L89)

```go
func list(_ context.Context, dir brick.Remote) ([]string, error) {
	base, err := source(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	err = fs.WalkDir(remoteSrc, path.Join(base, dir.Path), func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		out = append(out, strings.TrimPrefix(p, base+"/"))
		return nil
	})
	return out, err
}
```
<!-- /ds:block hash=45ff67 -->

<!-- ds:block id=setup-rjv29uqg title="examples/components-libraries/main.go" -->
**examples/components-libraries/main.go** · [`examples/components-libraries/main.go:94-99`](../../../examples/components-libraries/main.go#L94-L99)

```go
func setup(ctx context.Context, work string) (remote.Report, error) {
	if err := os.CopyFS(work, os.DirFS(themeDir)); err != nil {
		return remote.Report{}, err
	}
	return remote.Vendor(ctx, remote.Options{Dir: work, Fetch: fetch, List: list})
}
```
<!-- /ds:block hash=4325bc -->

One remote component, not a whole library, is a `use:` with the full specifier. The vendor step also brings the nearest library manifest above it, so the component's `~/` imports resolve from its library root.

<!-- snippet: examples/components-libraries/theme/content/remote-use.html -->
```html
---
use:
  Chip: github.com/acme/ui/components/Chip.html@v1.2.0
---
<Chip>Beta</Chip>
```

## Step 5 — Use the components and the kit's theme

A page names the components with no import. `{{ assets }}` links each used component's stylesheet, and the kit's manifest lists `Theme` under `global:`, so its design tokens come with every page that uses the kit.

<!-- snippet: examples/components-libraries/theme/content/index.html -->
```html
{{ assets }}
<TeamPill label="Team" />
<Badge tone="success">Active</Badge>
<Button variant="outline">Save</Button>
<AcmeTag>New</AcmeTag>
```

To re-theme the kit, override its `--bk-*` custom properties in your own stylesheet; no component is edited. `data-theme="dark"` or `"light"` on `<html>` forces a mode (the default follows the system), and `data-accent` picks the primary colour from indigo (the default), blue, emerald, rose, amber, violet or zinc. The token list is in `kit/components/Theme.html`.

<!-- snippet: examples/components-libraries/theme/static/theme.css -->
```css
/* Re-theme the kit by overriding its tokens; no component is edited. */
:root { --bk-primary: #0f766e; --bk-radius: .25rem; }
```

## Step 6 — Ship a library as a Go package

A library that travels with a Go program is an `embed.FS` of its folder, mounted read-only at `brick_libs/<Name>/` in the engine's filesystem. This is the whole of the kit's `kit` package (see also [Choosing where components come from](../../choosing/component-sources.md)):

<!-- snippet: kit/kit.go lines=19-34 -->
```go
// Name is the kit's library name: its mount (brick_libs/brick-kit/) and the
// bare name brick.yaml picks it by. It matches the manifest's name.
const Name = "brick-kit"

// files is the library folder as shipped: the manifest, the components and
// the data they read. Nothing else (README, Go files) is part of it, so the
// engine sees exactly what a vendored copy would.
//
//go:embed brick-library.yaml components data
var files embed.FS

// Library is the kit as an embedded library. Each call returns a fresh
// value, so a caller setting Prefix or Only never changes another's.
func Library() brick.Library {
	return brick.Library{Name: Name, FS: FS()}
}
```

`Library()` returns a fresh value each call, so a caller setting `Prefix` or `Only` never changes another's. The name must match `^[a-z0-9][a-z0-9._-]*$` (it becomes a path segment) and `brick_libs` is reserved, so a project cannot have a folder of that name.

## Try it

```sh
cd examples/components-libraries
go run .
go test .
```

The program copies `theme/` into a scratch folder, vendors, and renders both pages from the vendored copy. The `go test` line ends with `ok  	github.com/ubgo/brick/examples/components-libraries`.

<!-- snippet: examples/components-libraries/testdata/output.txt -->
```text
== vendored 3 files (3 fetched)
github.com/acme/ui/brick-library.yaml@v1.2.0
github.com/acme/ui/components/Chip.html@v1.2.0
github.com/acme/ui/components/Tag.html@v1.2.0

== content/index.html
<brick-assets><link rel="stylesheet" href="/_brick/c/Badge.b7608c46.css" integrity="sha384-t0WimmJFz6B+5ayAXY9SlQTKTJiVX0LmS93vmWaxwMCbxTpae6w4Xs84ftsNTEUt"><link rel="stylesheet" href="/_brick/c/Button.1e124ea1.css" integrity="sha384-MTMXpICj3aepq9Q51xzzvhzKNlWfxezPdGlbmxuxc77lw/qnI1wAxFOoVk9Qe9CI"><link rel="stylesheet" href="/_brick/c/Theme.d4122c3e.css" integrity="sha384-89QIQ5GHjeETghIcpBaAeDeRU9plF/A2zzvF/9BAf8twQxK+4lm1FcxBI84uM18R"></brick-assets>
<span class="pill">Team</span>
<span class="bk-badge bk-badge--success bk-badge--soft" >Active</span>
<button type="button" class="bk-btn bk-btn--outline" >Save</button>
<em class="acme-tag">New</em>


== content/remote-use.html
<b class="acme-chip">Beta</b>

```

## Pitfalls

- **A bare name must be offered.** `from: brick-kit` in a program that did not pass the kit to `Config.Apply` fails with `brick.yaml: libraries: "brick-kit" is not a ~/ folder, a remote library (host/owner/repo@version), or a library this program offers (none)`.
- **The project wins, quietly.** A `components/Badge.html` of your own hides the kit's `<Badge>` without an error; the engine logs what it shadowed. Give the library a `prefix` to keep both.
- **`only:` hides the rest.** With `only: [Badge, Button]` a page that writes `<Alert>` fails with `unknown component <Alert>`. The names are the library's own, before `prefix`.
- **A remote library is read from the vendored copy.** If `brick_modules/` is missing, run `brick vendor`; if it differs from `brick.lock`, `brick vendor -check` fails with ``remote: brick_modules does not match brick.lock; run `brick vendor` ``.
- **A custom `Fetch` should return a not-found error for a missing file.** `Vendor` probes for a library manifest above a single imported file and needs `fs.ErrNotExist` (or `remote.ErrNotFound`) to tell "no manifest here" from a failed download.
- **Libraries do not see your project's files.** Inside a library `~/` is the library root, so a library component that imports `~/data/x.yaml` reads the library's own `data/`.

## Related

- [Components and props](props.md): `use:` imports, the two tag spellings and finding components in more than one folder.
- [Styles and client code](styles-and-client.md): `{{ assets }}` and how each component's CSS is linked.
- [Choosing where components come from](../../choosing/component-sources.md) and the [CLI reference](../../reference/cli.md#brick-vendor).
- [Feature index](../../reference/features.md): the "Libraries, remote and kit" rows.
- Example: [examples/components-libraries](../../../examples/components-libraries/).
