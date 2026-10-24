# forms-uploads

A profile form with an avatar (one file) and a photo list (at most two). Each file streams to `/_brick/upload` as the `$upload` pseudo-action into the `upload/disk` sink in a temporary directory, and an `Options.UploadCheck` refuses PNGs wider than 4096 pixels from their first bytes. The same form uploads with JavaScript off (multipart, through `Engine.FormPost`). With `-presign`, an in-memory store ([presign.go](presign.go)) takes the bytes straight from the browser and confirms them with `$uploaded`. A drop zone for the photos uses `brick.upload(el, field, files)` and the `upload-progress`, `upload-done` and `upload-error` events. No real storage account is used.

## Run it

```sh
cd examples
go run ./forms-uploads -addr :8087              # then open http://localhost:8087/profile
go run ./forms-uploads -addr :8087 -presign     # direct-to-storage uploads
```

## Test it

```sh
cd examples
go test ./forms-uploads/ -v
```

```text
--- PASS: TestUploadThroughTheEngine (0.04s)
--- PASS: TestUploadWithoutJavaScript (0.02s)
--- PASS: TestPresignedUpload (0.00s)
--- PASS: TestUploadsNeedASink (0.00s)
--- PASS: TestDropZoneClientServed (0.00s)
ok  	github.com/ubgo/brick/examples/forms-uploads	0.084s
```

Guide: [Uploads: files into state, bytes into your storage](../../docs/guides/forms/uploads.md).
