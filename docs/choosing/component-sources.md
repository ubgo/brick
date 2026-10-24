# Choosing where components come from

A component is a file, and a file can come from four places: your project, a single remote file, a component library, or the UI kit (which is a library). They differ in how they are pinned, whether they can read your project's files, and how name clashes are settled. The step-by-step build is the [component libraries guide](../guides/components/libraries.md); this page helps you pick, and the snippets below are tested site configuration.

## The decision

| Source | Declared in | Pinned by | Name clashes | Pick it when |
|---|---|---|---|---|
| Your project | a `components/` folder | your own repository | your files win over every library; only a `use:` import ranks above them | the component is yours and used by this site only |
| One remote file | `use:` in a page or component, for example `Card: github.com/acme/brick-kit/components/Card.html@v1.2.0` | `brick.lock`, with a sha256 per file, written by `brick vendor` | `use:` wins over everything, then your components, then libraries | you want one component from someone else and nothing more |
| A library folder in your project | `libraries: [~/ui]` in `brick.yaml` | your repository | the project wins, then libraries in the order listed | several sites in a monorepo share components |
| A library in a repository | `libraries: [github.com/acme/charts@v1.4.0]` | `brick vendor` and `brick.lock` | as above; `prefix:` keeps two `Button`s apart, `only:` takes some | a team shares a component set across projects |
| The UI kit | `libraries: [brick-kit]` with the `brick` command, `kit.Library()` from Go | the CLI version, or `libraries: [github.com/ubgo/brick/kit@main]` vendored | as above | you want 130+ finished components (tables, dialogs, charts, forms) and a design system to start from |

Rendering reads only the vendored copies (`brick_modules/`), so there is no network at render time. `brick vendor -check` verifies the lock offline, which suits CI.

## If X then Y

- **A new project that needs buttons, dialogs and a table today:** the kit. `brick new site blog` already lists `libraries: [brick-kit]` in `brick.yaml`, and the layout needs only `{{ assets }}`.
- **You use Go and want no network at all:** `brick.Options{Libraries: []brick.Library{kit.Library()}}`. The kit is embedded in the `github.com/ubgo/brick/kit` package.
- **The kit's `Button` and your `Button` collide:** keep yours as is (the project wins; the engine logs what it shadowed), or give the library a prefix so both stay available, as the demo site does with `prefix: Kit` and uses `<KitTabs>`.
- **You publish your own set:** push a folder with a `brick-library.yaml` manifest to a repository and tag it. Inside a library `~/` means the library's own root, so its components import each other wherever it sits and can never read your project's files.
- **One component, from one place:** `use:`. Do not vendor a whole library for a single file.

From Go, the kit is an embedded package, and the same call offers it to a `brick.yaml` that lists `brick-kit`:

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

<!-- snippet: examples/site/brick.yaml -->
```yaml
defaultLocale: en
navigation: true
# The brick kit as a remote library: `task
# examples:vendor` vendors the whole kit/ folder into brick_modules/ and
# pins it in brick.lock. prefix keeps the kit's tags apart from this site's
# own components (it has a Button, a Card and Tabs): <KitTabs>, <KitButton>.
libraries:
  - {from: github.com/ubgo/brick/kit@main, prefix: Kit}
# What the Go host (examples/demosite) supplies, so `brick check` and
# `brick lsp` can compile the site without it.
host:
  globals: [now]
```

## Pitfalls

- **Shadowing is a warning, not an error.** Starting a site that defines `components/Card.html` beside the kit's `Card` logs `<Card> from library brick-kit is shadowed by ~/components/Card.html` and uses yours. That is the precedence rule at work; add a `prefix:` if you want both.
- **A library cannot reach into your project.** `~/` inside a library is the library's root. Pass data in as props or through `.Ctx`.
- **Remote components are trusted code.** Pin versions and review `brick.lock` changes like dependency updates. Every file is code; only data is untrusted.
- **`brick.yaml` is read by the `brick` command.** A Go host passes `Libraries` itself, or loads the config and calls `Apply`.

## Guides and examples

| Source | Reference | Example |
|---|---|---|
| Project components | [Components and props](../guides/components/props.md) | [components-props](../../examples/components-props/) |
| Remote files and `brick vendor` | [Component libraries](../guides/components/libraries.md), [CLI reference](../reference/cli.md) | [vendorkit](../../examples/vendorkit/) |
| Libraries | [Component libraries](../guides/components/libraries.md) | [components-libraries](../../examples/components-libraries/), [site](../../examples/site/) |
| The kit | [kit/README.md](../../kit/README.md) | [ui](../../examples/ui/) |

Back to [choosing guides](README.md).
