# site-images

The `image` built-in. `content/index.html` asks for resized copies of `static/diagram.png` (1200×675) at the default widths and as a JPEG at 320 and 640 pixels; the site serves them under `/_brick/img/` with a content hash in the name and an immutable cache header, and the export writes them out.

```sh
go run .                    # http://localhost:8096/
go run . -export public     # the page and every resized copy
go test .                   # the exact page, the headers, the exported files and the error messages
```

Guide: [images: resized copies and WebP](../../docs/guides/site/images.md).

## What the tests prove

- TestImages: the resized copies, `srcset` and the JPEG and WebP conversions through the handler.
- TestExportImages: every copy is written under `_brick/img/`.
- TestImageErrors: the error texts the guide quotes.

A passing run from `examples/`:

```text
$ go test ./site-images/
ok  	github.com/ubgo/brick/examples/site-images	0.759s
```
