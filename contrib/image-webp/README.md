# brick/contrib/image-webp

WebP output for brick's `{{ image }}` built-in, through [nativewebp](https://github.com/HugoSmits86/nativewebp), a pure-Go encoder (no cgo).

```go
eng, err := brick.New(brick.Options{FS: fsys, ImageEncoders: []brick.ImageEncoder{imagewebp.New()}})
```

```html
{{ with image "~/static/diagram.png" 480 960 "webp" }}
<img src="{{ .URL }}" srcset="{{ .SrcSet }}" width="{{ .Width }}" height="{{ .Height }}" alt="How it fits together">
{{ end }}
```

```html
<img src="/_brick/img/diagram.3f9c2a1b-960.webp" srcset="/_brick/img/diagram.3f9c2a1b-480.webp 480w, /_brick/img/diagram.3f9c2a1b-960.webp 960w" width="960" height="540" alt="How it fits together">
```

**Lossless only.** nativewebp writes lossless WebP (VP8L) and has no lossy encoder, so every pixel, alpha included, survives exactly and there is no quality setting. Lossless WebP is usually smaller than PNG, but for photographs it is usually larger than brick's JPEG (quality 85): ask for `"webp"` for screenshots, diagrams, illustrations and images with transparency, and leave photos as JPEG.

- The one option is the encoder effort: `imagewebp.New(imagewebp.WithCompression(imagewebp.BestCompression))`, 0 (`BestSpeed`) to 6 (`BestCompression`), default 4. It changes file size and encoding time, never pixels.
- Output is deterministic (same image, same bytes), which brick's hashed file names rely on.
- Files end in `.webp`; `contrib/assets-http`, the `brick serve` dev server and `brick build` serve or write them as `image/webp`.
- Importing the package also registers a WebP decoder with Go's `image` package (nativewebp does it in its `init`), so `{{ image }}` can read `.webp` source files too.
- Without this encoder, a call that names `"webp"` fails with `no encoder for format "webp" (have jpeg, png); register one in Options.ImageEncoders, for example contrib/image-webp`. There is no silent fallback to PNG or JPEG.
- The `brick` CLI registers this encoder for every site; nothing to configure in `brick.yaml`.
