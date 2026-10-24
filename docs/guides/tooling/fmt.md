# Formatting with `brick fmt`

## What it is and when you want it

`brick fmt` lays brick `.html` files out one way, like `gofmt` does for Go: it indents nested elements and component tags, drops trailing spaces, and tidies attribute spacing and frontmatter fences. It never changes `{{ }}` actions, `<pre>`, `<textarea>`, `<script>`, `<style>`, raw bodies or `brick:` bodies, so a page renders the same bytes before and after. Use it when a team edits the same theme, or to keep editor format-on-save and CI in agreement; the language server's formatting request runs the same code.

## Before you start

- The `brick` CLI on your PATH ([install](../../reference/cli.md#install)).
- A site folder. Options live in that site's `brick.yaml` under `fmt:`; without one, `brick fmt` uses its defaults (no wrapping, no attribute reordering).

## Step 1 — See what would change

Start read-only, so nothing is written until you have seen the result. Here is a component written by hand, with uneven spacing and no indentation:

```html
---
props:
  title:   {type: string, default: "Panel"}
---
<section   class="panel"  id="main">
<h2>{{ .Props.title }}</h2>   
<ul>
{{ range .Props.items }}
<li><a href="{{ .url }}" class="link" brick:prefetch>{{ .name }}</a></li>
{{ end }}
</ul>
<pre>
  keep   this
</pre>
</section>
```

`brick fmt -l` lists the files that differ, and `-d` prints a unified diff:

```console
$ brick fmt -l .
components/Panel.html
$ brick fmt -d components/Panel.html
--- components/Panel.html
+++ components/Panel.html (formatted)
@@ -2,14 +2,14 @@
 props:
   title:   {type: string, default: "Panel"}
 ---
-<section   class="panel"  id="main">
-<h2>{{ .Props.title }}</h2>   
-<ul>
-{{ range .Props.items }}
-<li><a href="{{ .url }}" class="link" brick:prefetch>{{ .name }}</a></li>
-{{ end }}
-</ul>
-<pre>
+<section class="panel" id="main">
+  <h2>{{ .Props.title }}</h2>
+  <ul>
+    {{ range .Props.items }}
+    <li><a href="{{ .url }}" class="link" brick:prefetch>{{ .name }}</a></li>
+    {{ end }}
+  </ul>
+  <pre>
   keep   this
 </pre>
 </section>
```

The `<pre>` body keeps its spacing, and the YAML inside the frontmatter is left as written; only the fences are tidied.

## Step 2 — Write the result

`-w` writes each formatted file back in place; with no path it walks the current directory for `.html` files, skipping hidden folders, `brick_modules/` and `node_modules/`.

```console
$ brick fmt -w components/Panel.html
$ brick fmt -l .
$
```

Without `-l`, `-w` or `-d`, `brick fmt` prints each formatted file to standard output, and `brick fmt -` formats standard input, which is what an editor's "format document" command can call.

## Step 3 — Keep the team's options in `brick.yaml`

Put the options in the site's `brick.yaml`, so every run and every editor writes the same layout. Every key is off when absent.

```yaml
fmt:
  lineLength: 60
  attrOrder: ["brick:*", id, class]
  inline: [a]
```

- `lineLength` wraps a markup line longer than this many characters, one attribute per line; `0` never wraps.
- `attrOrder` moves these attributes to the front of each tag, in this order; an entry ending in `*` is a prefix (`brick:*`). Values never change.
- `inline` names elements and component tags whose content keeps the parent's indentation (`a`, `em`, `Icon`), so wrapped prose inside them is not pushed a level deeper.
- `formatters` hands a `brick:client`, `brick:style` or `brick:script` body to a command (Step 4).

The same Panel with that `brick.yaml`:

```console
$ brick fmt components/Panel.html
---
props:
  title:   {type: string, default: "Panel"}
---
<section id="main" class="panel">
  <h2>{{ .Props.title }}</h2>
  <ul>
    {{ range .Props.items }}
    <li><a
      brick:prefetch
      class="link"
      href="{{ .url }}">{{ .name }}</a></li>
    {{ end }}
  </ul>
  <pre>
  keep   this
</pre>
</section>
```

`id` and `class` moved to the front of `<section>`, and the `<a>` line was longer than 60 characters, so it wrapped with `brick:prefetch` first. The nearest `brick.yaml` above each path is the one used. Flags override it for one run: `-line-length`, `-attr-order` and `-inline`.

```console
$ brick fmt -line-length 0 -attr-order id components/Panel.html | sed -n 5,6p
<section id="main" class="panel">
  <h2>{{ .Props.title }}</h2>
```

## Step 4 — Format client code and styles with your own tool

A `formatters` entry runs a command on a body: the body on standard input without its indentation, the result on standard output. It runs in the site's folder, split on spaces and without a shell, with a 30-second limit; a body that holds `{{` is left alone. A real setup uses prettier:

```yaml
fmt:
  formatters:
    brick:client: prettier --stdin-filepath client.js
    brick:style: prettier --stdin-filepath style.css
```

To see the mechanism without installing anything, `tr a-z A-Z` as the style formatter:

```yaml
fmt:
  formatters:
    brick:style: tr a-z A-Z
```

```console
$ cat components/S.html
<brick:style>
  .a { color: red }
</brick:style>
<p>x</p>
$ brick fmt components/S.html
<brick:style>
  .A { COLOR: RED }
</brick:style>
<p>x</p>
```

## Step 5 — Leave a region alone

Wrap a region in `{{/* brick:no-format */}}` and `{{/* brick:format */}}` to keep it exactly as written; a file that starts with the first marker is not formatted at all.

```console
$ printf '<div>\n{{/* brick:no-format */}}\n<p>   keep   </p>\n{{/* brick:format */}}\n<p>x</p>\n</div>\n' | brick fmt -
<div>
  {{/* brick:no-format */}}
<p>   keep   </p>
{{/* brick:format */}}
  <p>x</p>
</div>
```

## Step 6 — Gate formatting in CI from Go

`brick fmt -l` exits 0 even when files differ, so a pipeline has to read what it prints. `examples/tooling-cli` does that in Go, around one helper that runs the binary and keeps its standard error for the failure message:

<!-- ds:block id=run-7ud3w9q6 title="examples/tooling-cli/main.go" -->
**examples/tooling-cli/main.go** · [`examples/tooling-cli/main.go:35-43`](../../../examples/tooling-cli/main.go#L35-L43)

```go
func run(bin string, args ...string) (string, error) {
	cmd := exec.Command(bin, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return stdout.String(), fmt.Errorf("%s %s: %w: %s", bin, strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}
```
<!-- /ds:block hash=08c13a -->

The gate lists the unformatted files, and `brick fmt -d` gives the diff to print beside each:

<!-- ds:block id=unformatted-wr6hqwd5 title="examples/tooling-cli/main.go" -->
**examples/tooling-cli/main.go** · [`examples/tooling-cli/main.go:49-55`](../../../examples/tooling-cli/main.go#L49-L55)

```go
func unformatted(bin, dir string) ([]string, error) {
	out, err := run(bin, "fmt", "-l", dir)
	if err != nil {
		return nil, err
	}
	return strings.Fields(out), nil
}
```
<!-- /ds:block hash=4d5b14 -->

<!-- ds:block id=formatdiff-9ckqaedk title="examples/tooling-cli/main.go" -->
**examples/tooling-cli/main.go** · [`examples/tooling-cli/main.go:60-62`](../../../examples/tooling-cli/main.go#L60-L62)

```go
func formatDiff(bin, file string) (string, error) {
	return run(bin, "fmt", "-d", file)
}
```
<!-- /ds:block hash=81d8a9 -->

Run against the hand-written `Panel.html` of Step 1, with a `brick` built from this repository:

```console
$ go run ./tooling-cli -brick ./brick
brick v0.0.0-20261013175757-f76fdd502477+dirty
not formatted: tooling-cli/theme/components/Panel.html
files need `brick fmt -w`
exit status 1
```

After `brick fmt -w tooling-cli/theme/components/Panel.html` the same command prints `formatted` and exits 0. `go test ./tooling-cli/` builds the command and checks the listing, the diff, the write and a syntax error (which fails the gate with brick's own message, `1:4: unclosed {{`).

## Try it

```console
$ brick new site blog && cd blog
$ brick fmt -d
$ brick fmt -w
```

A fresh site from `brick new site` is already formatted, so `-d` prints nothing; edit a file by hand and run `brick fmt -d` again to see the diff from Step 1. In CI, `test -z "$(brick fmt -l .)"` fails the job when a file is not formatted (`brick fmt -l` itself exits 0 in that case).

## Pitfalls

- **`-l` does not fail on unformatted files.** Exit status is 1 only when a file cannot be read or parsed; check the listed output instead, as in "Try it".
- **Indentation can be left as written.** When the tags inside a `{{ if }}` or `{{ range }}` cannot be followed with certainty, `brick fmt` keeps that file's indentation and changes only spacing. Add `-v` to see why: `components/Odd.html: indentation left as written: line 2: the branches of a {{ }} block open or close different numbers of tags`.
- **A syntax error stops that file.** `printf '<p>{{ .x </p>\n' | brick fmt -` prints `brick fmt: 1:4: unclosed {{` and exits 1; other files in the run are still formatted.
- **A failing formatter leaves the file untouched and fails the run.** With `brick:style: false` the output is `brick fmt: components/S.html: 1:1: <brick:style> formatter: false: exit status 1`, exit 1. An empty command is refused up front: `brick fmt: brick.yaml: fmt.formatters.brick:style: empty command`, exit 1.
- **Attribute order is kept when it is not safe to change.** A tag holding `{{ }}` between attributes, repeating one, or spread over lines keeps its order even with `attrOrder` set.
- **`-line-length` must be 0 or more.** `brick fmt -line-length -2` prints `brick fmt: -line-length must be 0 or more` and exits 2.
- **Vendored code is skipped.** `brick_modules/` and `node_modules/` are someone else's layout; pass a path inside them explicitly if you really want them formatted.

## Related

- [`brick fmt` in the CLI reference](../../reference/cli.md#brick-fmt)
- [Editors: brick lsp and VS Code](editors.md) (format on save uses the same code)
- [Testing themes with bricktest](bricktest.md)
- [Feature index: `brick fmt` and the `fmt:` section](../../reference/features.md#cli)
