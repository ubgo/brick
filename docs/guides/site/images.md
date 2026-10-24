# Images: resized copies and WebP

## What it is and when you want it

A 4000-pixel photo sent to a phone wastes seconds, and an `<img>` without a size makes the page jump as it loads. The `image` function reads a picture from `static/`, makes resized copies, serves them under `/_brick/img/` with a content hash in the name (so browsers cache them forever), and gives the tag everything it needs: `.URL`, `.SrcSet`, `.Width`, `.Height`. `brick build` writes the copies out. You want it for every picture that is larger than where it is shown.

## Before you start

- Read [pages and routes](pages-and-routes.md).
- JPEG and PNG are built in, in pure Go. WebP comes from [`contrib/image-webp`](../../../contrib/image-webp/), which the `brick` command registers for you.
- The example is [`examples/site-images`](../../../examples/site-images/), with a 1200×675 `static/diagram.png`.

## Step 1 — Ask for the copies

With no widths you get 480, 960 and 1440 pixels, never wider than the original (so here 480, 960 and 1200). Widths after the path pick others, and a format name after them converts: `"jpeg"`, `"png"`, or `"webp"` once its encoder is registered (Step 2). `content/index.html`:

<!-- snippet: examples/site-images/theme/content/index.html -->
```html
---
title: Images
---
{{- with image "~/static/diagram.png" }}
<img src="{{ .URL }}" srcset="{{ .SrcSet }}" sizes="(max-width: 800px) 100vw, 800px" width="{{ .Width }}" height="{{ .Height }}" alt="How it fits together" loading="lazy">
{{- end }}
{{- with image "~/static/diagram.png" 320 640 "jpeg" }}
<img src="{{ .URL }}" srcset="{{ .SrcSet }}" width="{{ .Width }}" height="{{ .Height }}" alt="The same, as JPEG">
{{- end }}
{{- with image "~/static/diagram.png" 480 960 "webp" }}
<img src="{{ .URL }}" srcset="{{ .SrcSet }}" width="{{ .Width }}" height="{{ .Height }}" alt="The same, as lossless WebP">
{{- end }}
```

`.URL`, `.Width` and `.Height` describe the widest copy; `.SrcSet` lists all of them.

## Step 2 — Register the WebP encoder

JPEG and PNG need nothing. WebP comes from `contrib/image-webp`, registered in `brick.Options.ImageEncoders`; the `brick` command does this for you. From `examples/site-images/main.go`:

<!-- ds:block id=newsite-xkpagvwq title="examples/site-images/main.go" -->
**examples/site-images/main.go** · [`examples/site-images/main.go:29-38`](../../../examples/site-images/main.go#L29-L38)

```go
func newSite(ctx context.Context, dir string) (*site.Site, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	return site.New(ctx, site.Options{
		FS:     root.FS(),
		Engine: brick.Options{ImageEncoders: []brick.ImageEncoder{imagewebp.New()}},
	})
}
```
<!-- /ds:block hash=24a938 -->

The WebP is lossless (every pixel kept, transparency included, no quality setting), which suits screenshots and diagrams; for photographs JPEG is usually smaller. Any type with `Format() string` and `Encode(w io.Writer, img image.Image) error` is an encoder, and its output must be deterministic because file names carry the source's hash.

## Try it

```sh
cd examples/site-images
go run .
curl -s localhost:8096/
```

```html
<!doctype html>
<html>
<head><title>Images</title></head>
<body>
<img src="/_brick/img/diagram.f9b1ebb0-1200.png" srcset="/_brick/img/diagram.f9b1ebb0-480.png 480w, /_brick/img/diagram.f9b1ebb0-960.png 960w, /_brick/img/diagram.f9b1ebb0-1200.png 1200w" sizes="(max-width: 800px) 100vw, 800px" width="1200" height="675" alt="How it fits together" loading="lazy">
<img src="/_brick/img/diagram.f9b1ebb0-640.jpg" srcset="/_brick/img/diagram.f9b1ebb0-320.jpg 320w, /_brick/img/diagram.f9b1ebb0-640.jpg 640w" width="640" height="360" alt="The same, as JPEG">
<img src="/_brick/img/diagram.f9b1ebb0-960.webp" srcset="/_brick/img/diagram.f9b1ebb0-480.webp 480w, /_brick/img/diagram.f9b1ebb0-960.webp 960w" width="960" height="540" alt="The same, as lossless WebP">
</body>
</html>
```

`f9b1ebb0` is the start of the source file's SHA-256, so a copy's name changes only when the picture does. A copy is served with `Cache-Control: public, max-age=31536000, immutable`. `go run . -export public` writes the page, `diagram.png` and the seven copies under `public/_brick/img/` (the `.webp` ones are served as `image/webp`). `go test .` checks the page, the headers, the exported files and the errors below.

## Pitfalls

- **Only `static/` (`Options.ImageDirs`).** The path may come from data (`image .Params.cover`), but a file elsewhere is refused: `image "~/layouts/default.html": image paths must be inside Options.ImageDirs (default static/)`. A missing file fails the render: `~/static/nope.png: file does not exist`.
- **A format needs an encoder.** On a site with no extra encoders registered: `image "~/static/diagram.png": no encoder for format "avif" (have jpeg, png); register one in Options.ImageEncoders, for example contrib/image-webp`. Your own Go program must register WebP itself; only the `brick` command does it for you.
- **Huge images are refused before decoding.** Above `Options.MaxImagePixels` (default 40,000,000 pixels) the image is not decoded, so an upload cannot exhaust memory.
- **Never wider than the original.** Asking for 1440 from a 1200-pixel file gives 1200; `.Width` tells you what you got, so always print it rather than hard-coding a size.
- **Paths start with `~/`.** Like every brick path, `./` and `../` are errors.

## Related

- [Static export](static-export.md) · [Tailwind](tailwind.md) · [Pages and routes](pages-and-routes.md)
- [Feature index](../../reference/features.md) rows: the `image` built-in, image encoders and WebP (`contrib/image-webp`).
- Long-form: [guide › Images](../../guide.md#images).
- Example: [`examples/site-images`](../../../examples/site-images/).
