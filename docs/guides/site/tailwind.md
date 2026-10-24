# Tailwind

## What it is and when you want it

brick runs the standalone Tailwind CLI for you: name the input and output CSS in `brick.yaml` and `brick build` and `brick serve` rebuild the stylesheet, but only when the set of classes used in your files (or the input CSS) changed, so editing text reloads instantly. From Go, the `tailwind` package does the same with `tailwind.Build`, and `site.Options.OnChange` wires it to the dev server's watcher. You want it when you style with Tailwind and do not want a Node toolchain beside a Go program.

## Before you start

- Install the standalone binary as `tailwindcss` on `PATH` ([Tailwind's standalone CLI](https://tailwindcss.com/blog/standalone-cli)), or point `binary:` at it. brick does not download it.
- Read [pages and routes](pages-and-routes.md) and [dev server](dev-server.md).
- The example is [`examples/site-tailwind`](../../../examples/site-tailwind/). The older [`examples/tailwind`](../../../examples/tailwind/) is the same idea for the `brick` command alone.

## Step 1 — Name the input and output

`brick.yaml` (read by the `brick` command):

<!-- snippet: examples/site-tailwind/theme/brick.yaml -->
```yaml
tailwind:
  input: styles/app.css
  output: static/app.css
```

`styles/app.css` is Tailwind v4's one-line input:

<!-- snippet: examples/site-tailwind/theme/styles/app.css -->
```css
@import "tailwindcss";
```

The output goes in `static/`, so the site serves it at `/app.css`. The layout links it:

<!-- snippet: examples/site-tailwind/theme/layouts/default.html -->
```html
<!doctype html>
<html>
<head><title>{{ .Title }}</title><link rel="stylesheet" href="/app.css"></head>
<body class="bg-slate-50 text-slate-800">{{ children }}</body>
</html>
```

## Step 2 — Build from Go and rebuild on change

`tailwind.Options` mirrors the YAML. `newSite` passes an `OnChange` hook that the watcher calls before applying each batch of changes. From `examples/site-tailwind/main.go`:

<!-- ds:block id=css-du27ku5d title="examples/site-tailwind/main.go" -->
**examples/site-tailwind/main.go** · [`examples/site-tailwind/main.go:30-32`](../../../examples/site-tailwind/main.go#L30-L32)

```go
func css(dir, binary string) tailwind.Options {
	return tailwind.Options{Dir: dir, Input: "styles/app.css", Output: "static/app.css", Binary: binary}
}
```
<!-- /ds:block hash=2f72d4 -->

<!-- ds:block id=newsite-t9dxbc8c title="examples/site-tailwind/main.go" -->
**examples/site-tailwind/main.go** · [`examples/site-tailwind/main.go:38-56`](../../../examples/site-tailwind/main.go#L38-L56)

```go
func newSite(ctx context.Context, dir string, tw tailwind.Options) (*site.Site, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	return site.New(ctx, site.Options{
		FS:  root.FS(),
		Dev: true,
		OnChange: func(ctx context.Context, changes []watch.Change) error {
			for _, c := range changes {
				if strings.HasSuffix(c.Path, ".html") || strings.HasSuffix(c.Path, ".md") || c.Path == tw.Input {
					_, err := tailwind.Build(ctx, tw)
					return err
				}
			}
			return nil
		},
	})
}
```
<!-- /ds:block hash=07f14b -->

`main` calls `tailwind.Build(ctx, tw)` once before serving, so the first page already has its stylesheet.

## Try it

```sh
cd examples/site-tailwind
go run .                     # or: brick serve theme
```

With the binary installed, it logs `tailwind: rebuilt=true static/app.css` and serves `http://localhost:8097/`. The output's first line is a stamp, `/* brick-tailwind: <hash> */`: a hash of every class token in your `.html` and `.md` files plus the input CSS. Edit the text of `content/index.html` and the binary does not run; add a class and it does.

Without the binary, both the example and `brick build theme` stop with:

```text
tailwind: the tailwindcss binary was not found on PATH; install the standalone CLI (https://tailwindcss.com/blog/standalone-cli) or set Binary
```

`go test .` runs without Tailwind installed: it uses a stand-in shell script as the binary to check that the first build runs it, a text edit does not, a new class does, and the watcher's `OnChange` rebuilds while the dev server runs; `TestRealTailwind` runs the real binary when it is on `PATH` and is skipped, with that reason, when it is not.

## Pitfalls

- **brick never fetches Tailwind.** No binary is an error, not a silent unstyled site; install it in CI too.
- **Only `class="…"` attributes decide whether to rebuild.** The stamp hashes every whitespace-separated token inside a `class` attribute of `.html` and `.md` files. Tailwind itself still scans your sources when it runs, but a class added only in a script or a data file does not change the stamp, so it does not trigger a rebuild; touch a class attribute or delete `static/app.css` to force one.
- **The stamp lives in the output.** Deleting `static/app.css`, or editing its first line, forces a rebuild; commit it or not, but do not hand-edit it.
- **`binary:` is relative to the project.** `binary: ./bin/tailwindcss` means inside the site folder; a bare name is looked up on `PATH`.
- **A failed rebuild in the dev server is logged, not fatal.** `OnChange` errors are logged and the batch is still applied, so the page reloads with the previous stylesheet; read the server log when a new class does not show up.

## Related

- [Dev server](dev-server.md) · [Static export](static-export.md) · [Images](images.md)
- [Feature index](../../reference/features.md) rows: Tailwind (`tailwind`), `site.Options.OnChange`.
- Long-form: [guide › A whole site](../../guide.md#a-whole-site--urls-serving-building).
- Examples: [`examples/site-tailwind`](../../../examples/site-tailwind/), [`examples/tailwind`](../../../examples/tailwind/).
