# brick/contrib/upload-s3

A `brick.UploadSink` that stores uploads in S3 or any S3-compatible server (MinIO, RustFS, R2). It also implements `UploadRemover` (`$removeUpload`) and `UploadPresigner`, so large files go from the browser straight to the bucket and brick never proxies their bytes.

```go
cfg, _ := config.LoadDefaultConfig(ctx)               // your credentials, your way
sink, err := uploads3.New(s3.NewFromConfig(cfg), uploads3.Options{
    Bucket:    "my-app-uploads",
    PublicURL: func(key string) string { return "https://cdn.example.com/" + key },
})
eng, err := brick.New(brick.Options{Uploads: sink /* … */})
```

The host builds the `*s3.Client`; this module never loads credentials.

| Option | Default | Meaning |
|---|---|---|
| `Bucket` | required | where every upload goes |
| `TempPrefix` | `brick-uploads/tmp/` | key prefix for uncommitted uploads |
| `PresignExpiry` | 15 minutes | how long a presigned PUT URL works |
| `PublicURL` | none | turns an object key into `UploadRef.URL` |

**Proxied uploads** stream into the bucket holding at most one 5 MiB part in memory: a smaller file is one `PutObject`, a larger one a multipart upload that is aborted if the engine aborts the writer.

**Presigned uploads.** `Presign` signs a PUT for a new random key under the temp prefix, with the content type and the file name (as `x-amz-meta-name`) signed, plus the length when the client declared one, so storage refuses any other length. The browser sends the returned `Headers`. `Confirm` then reads the object with `HeadObject` and reports the stored size and type, never the client's. Ids are 32 lowercase hex characters, checked before any key is built, so a forged id cannot reach an object outside the prefix. The bucket needs a CORS rule allowing `PUT` from your site with those headers.

**Your own per-file check.** Set `brick.Options.UploadCheck` beside `Uploads: sink` to check each file (a signature, a quota) before it reaches the bucket: return `brick.RefuseUpload("message")` and the field shows the message, while the sink never sees the file. It is an option rather than a wrapper around this sink, so `$removeUpload` and presigning keep working. Presigned bytes go straight to the bucket, so for those the check sees only the name, type and size the client claimed.

**Orphans.** Every object stays under `TempPrefix` until your app commits it (copies it to a permanent key through a host capability). This module never deletes an uncommitted upload on its own. Add two lifecycle rules on the temp prefix: one that expires objects (for example after one day) and one with `AbortIncompleteMultipartUpload`, so abandoned proxied uploads are cleaned up too.

## A complete example

The `Example` function runs without a network: the endpoint is a name that is never contacted, because presigning signs locally. It builds the client, the sink and the engine, then asks the sink for a presigned PUT. `ExampleNew_nilClient` shows the error a nil client gets. Run both with `cd contrib/upload-s3 && go test -run Example -v .`.

<!-- snippet: contrib/upload-s3/example_test.go lines=26-59 -->
```go
func Example() {
	client := s3.New(s3.Options{
		Region:       "us-east-1",
		BaseEndpoint: aws.String("http://s3.invalid"),
		UsePathStyle: true,
		Credentials:  credentials.NewStaticCredentialsProvider("example-key", "example-secret", ""),
	})
	sink, err := uploads3.New(client, uploads3.Options{
		Bucket:    "my-app-uploads",
		PublicURL: func(key string) string { return "https://cdn.example.com/" + key },
	})
	if err != nil {
		fmt.Println(err)
		return
	}

	if _, err := brick.New(brick.Options{FS: uploadTheme, Uploads: sink}); err != nil {
		fmt.Println(err)
		return
	}

	up, err := sink.Presign(context.Background(), brick.UploadMeta{Name: "photo.png", Type: "image/png", Size: 2048})
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(up.Method)
	fmt.Println(strings.HasPrefix(up.URL, "http://s3.invalid/my-app-uploads/"+uploads3.DefaultTempPrefix))
	fmt.Println(up.Headers["Content-Type"])
	// Output:
	// PUT
	// true
	// image/png
}
```

Pitfalls: the bucket needs a CORS rule for presigned `PUT`; `New` takes a `*s3.Client`, not a config, so you choose credentials and endpoint; and `PublicURL` only shapes `UploadRef.URL`, it never makes an object public.

## Tests

`go test ./...` needs no network. `task test:s3` runs the integration test against the shared local RustFS (`BRICK_S3_TEST=1`; override `BRICK_S3_ENDPOINT`, `BRICK_S3_BUCKET`, `BRICK_S3_ACCESS_KEY`, `BRICK_S3_SECRET_KEY`, `BRICK_S3_REGION`). It writes under a fresh per-run prefix in the `brick-upload-test` bucket and deletes only that prefix.
