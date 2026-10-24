# brick/contrib/sanitize-bluemonday

The policy behind brick's `{{ sanitize }}` built-in: [bluemonday](https://github.com/microcosm-cc/bluemonday)'s user-generated-content allowlist, with `rel="nofollow noreferrer"` added to every link.

## What it is and when you want it

Brick escapes every value by default. When a value is HTML on purpose (a user's bio from a form, a comment from a database, a rich-text field), `{{ sanitize .Props.bio }}` lets the safe parts through. The engine ships no sanitizer of its own, so there is no unsafe default; this module is the one the project recommends. Use it whenever a theme calls `sanitize`, or write your own `func(string) string` and pass that instead.

## Install

```sh
go get github.com/ubgo/brick/contrib/sanitize-bluemonday
```

## Use it

```go
eng, err := brick.New(brick.Options{FS: fsys, Sanitizer: sanitizebluemonday.New()})
```

```html
<div class="bio">{{ sanitize .Props.bio }}</div>
```

`New()` takes no arguments and returns a `func(string) string`. The policy is built once and is safe for concurrent use.

## A complete example

`Example` renders a bio with an `onclick`, a `<script>` and a `javascript:` link through `{{ sanitize }}` and prints what survives. `Example_noSanitizer` shows the error a theme gets when `Options.Sanitizer` is unset. Run both with `cd contrib/sanitize-bluemonday && go test -run Example -v .`.

<!-- snippet: contrib/sanitize-bluemonday/example_test.go lines=21-36 -->
```go
func Example() {
	eng, err := brick.New(brick.Options{FS: bioTheme, Sanitizer: sanitizebluemonday.New()})
	if err != nil {
		fmt.Println(err)
		return
	}
	bio := `<p onclick="steal()">Hi, I <em>write</em> <a href="https://example.com">here</a>.</p><script>steal()</script><a href="javascript:steal()">x</a>`
	html, err := eng.RenderComponent(context.Background(), "Bio", map[string]any{"bio": bio}, "")
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(string(html))
	// Output:
	// <div class="bio"><p>Hi, I <em>write</em> <a href="https://example.com" rel="nofollow noreferrer">here</a>.</p>x</div>
}
```

## What it keeps and removes

| Kept | Removed |
|---|---|
| paragraphs, headings, emphasis, lists, tables | `<script>` and `<style>` elements |
| links (gain `rel="nofollow noreferrer"`) and images | event handlers such as `onclick` |
| (nothing else) | `javascript:` URLs, iframes, forms |
| `class`-free semantic markup | `javascript:` URLs, iframes, forms |

## Options

There are none. To change the policy (allow `class`, keep an extra tag), copy `bluemonday.go` (13 lines) into your project, change the policy, and pass the result as `Options.Sanitizer`; the engine only needs a `func(string) string`.

## Pitfalls

- **No sanitizer is an error, not a pass-through.** A theme that calls `sanitize` with `Options.Sanitizer` unset fails to render with `sanitize needs Options.Sanitizer (for example contrib/sanitize-bluemonday); there is no unsafe default`.
- **Sanitize on output, not on input.** The policy can change between releases; keep the stored text as the user wrote it and sanitize when you render.
- **It sanitizes HTML, not Markdown.** Render Markdown first, then sanitize the result if the source is untrusted.

## Related

- [Sanitizing untrusted HTML](../../docs/guides/production/sanitizing.md)
- [Features reference](../../docs/reference/features.md)
- [example_test.go](example_test.go)
