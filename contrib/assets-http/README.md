# brick/contrib/assets-http

Serves the JavaScript and CSS that components ship with `<brick:client>` and `<brick:style>` over plain `net/http`. A page's `{{ assets }}` tags point at content-hashed URLs under the engine's asset base; this handler answers those URLs from the engine, so a host that does not use the `site` layer still gets working components.

## What it is and when you want it

The engine builds each component's module and stylesheet in memory and knows their URLs, but it does not listen on a port. If you embed brick in your own server (the `site` layer already mounts this for you), mount this handler at the asset base and the browser can fetch the files. Reach for it whenever a page renders a `<script type="module" src="/_brick/c/…">` tag and nothing serves it.

## Install

```sh
go get github.com/ubgo/brick/contrib/assets-http
```

The module needs Go 1.24 and depends only on `github.com/ubgo/brick`.

## A complete example

The `Example` function below is run by `go test`, so the output in its comment is what the code prints. It renders a page with one `<brick:client>` component, finds the hashed module URL in the page, and fetches it through the handler.

<!-- snippet: contrib/assets-http/example_test.go lines=29-67 -->
```go
func Example() {
	var current atomic.Pointer[brick.Engine]
	eng, err := brick.New(brick.Options{FS: assetTheme})
	if err != nil {
		fmt.Println(err)
		return
	}
	current.Store(eng)

	mux := http.NewServeMux()
	mux.Handle(brick.DefaultAssetBase, assetshttp.Handler(brick.DefaultAssetBase,
		func() assetshttp.Source { return current.Load() }))

	page, err := eng.Render(context.Background(), "content/index.html", nil)
	if err != nil {
		fmt.Println(err)
		return
	}
	m := moduleURL.FindStringSubmatch(string(page))
	if m == nil {
		fmt.Println("no module tag in the page")
		return
	}

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, m[1], nil))
	fmt.Println(rec.Code)
	fmt.Println(rec.Header().Get("Cache-Control"))
	fmt.Println(rec.Header().Get("X-Content-Type-Options"))

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, m[1], nil))
	fmt.Println(rec.Code)
	// Output:
	// 200
	// public, max-age=31536000, immutable
	// nosniff
	// 405
}
```

Run it with `cd contrib/assets-http && go test -run Example -v .`; it prints `--- PASS: Example`.

## Options

`Handler(prefix string, current func() Source) http.Handler` has two arguments and no option struct.

| Argument | Meaning |
|---|---|
| `prefix` | The URL prefix to strip, normally `brick.DefaultAssetBase` (`/_brick/`) or the `Options.AssetBase` you set. A request outside it is a 404. |
| `current` | Returns the `Source` to read from (anything with `Assets() fs.FS`; `*brick.Engine` qualifies). A function, so a host that swaps engines on reload always serves the newest snapshot. |

Every response carries `Cache-Control: public, max-age=31536000, immutable` and `X-Content-Type-Options: nosniff`. Only `GET` and `HEAD` are answered; anything else is `405` with `Allow: GET, HEAD`.

## Pitfalls

- **Assets exist only after compile.** An engine registers a component's assets when it compiles the component, so a request for a file no page has rendered yet is a 404. Call `eng.CompileAll` at startup to serve every asset before the first render.
- **Match the prefix.** If you set `Options.AssetBase` to a CDN path or a sub-path, pass the same value to `Handler`, or the page links to URLs this handler strips wrongly.
- **Do not cache the engine.** Pass a function that reads your current engine, not a captured pointer, or a reload keeps serving the old snapshot.
- **Hashed URLs are forever.** The year-long immutable header is safe because a changed file gets a new URL; never rewrite bytes at an old URL.

## Related

- The `site` layer mounts this handler for you: see [the site guides](../../docs/guides/site/README.md) and [sub-path hosting](../../docs/guides/site/subpath-and-mount.md).
- Component CSS and browser code: [styles and client code](../../docs/guides/components/styles-and-client.md).
- Host wiring without the site layer: [the host API guide](../../docs/guides/production/host-api.md).
