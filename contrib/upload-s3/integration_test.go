package uploads3

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"os"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/ubgo/brick"
)

// The integration test runs against a real S3-compatible server only
// when BRICK_S3_TEST=1 (task test:s3), so the default gate needs no
// server. Defaults match the shared local RustFS.
const (
	integrationEnv    = "BRICK_S3_TEST"
	integrationBucket = "brick-upload-test"
	defaultEndpoint   = "http://localhost:7010"
	defaultKey        = "rustfsadmin"
)

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func integrationClient(t *testing.T) *s3.Client {
	t.Helper()
	if os.Getenv(integrationEnv) != "1" {
		t.Skipf("set %s=1 to run against a real S3 server", integrationEnv)
	}
	return s3.New(s3.Options{
		BaseEndpoint: aws.String(envOr("BRICK_S3_ENDPOINT", defaultEndpoint)),
		Region:       envOr("BRICK_S3_REGION", "us-east-1"),
		UsePathStyle: true,
		Credentials: credentials.NewStaticCredentialsProvider(
			envOr("BRICK_S3_ACCESS_KEY", defaultKey), envOr("BRICK_S3_SECRET_KEY", defaultKey), ""),
	})
}

// ensureBucket creates the test bucket unless it already exists.
func ensureBucket(t *testing.T, c *s3.Client, bucket string) {
	t.Helper()
	ctx := context.Background()
	if _, err := c.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(bucket)}); err == nil {
		return
	}
	_, err := c.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(bucket)})
	var owned *types.BucketAlreadyOwnedByYou
	var exists *types.BucketAlreadyExists
	if err != nil && !errors.As(err, &owned) && !errors.As(err, &exists) {
		t.Fatalf("create bucket %s: %v", bucket, err)
	}
}

// emptyPrefix deletes every object under prefix. Cleanup is scoped to
// the run's own prefix, never the whole bucket, so pointing the test at
// a shared bucket cannot delete anyone else's objects.
func emptyPrefix(t *testing.T, c *s3.Client, bucket, prefix string) {
	t.Helper()
	ctx := context.Background()
	p := s3.NewListObjectsV2Paginator(c, &s3.ListObjectsV2Input{Bucket: aws.String(bucket), Prefix: aws.String(prefix)})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		for _, o := range page.Contents {
			if _, err := c.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(bucket), Key: o.Key}); err != nil {
				t.Fatalf("empty: %v", err)
			}
		}
	}
}

func TestIntegration(t *testing.T) {
	c := integrationClient(t)
	bucket := envOr("BRICK_S3_BUCKET", integrationBucket)
	run, err := newID()
	if err != nil {
		t.Fatal(err)
	}
	prefix := "brick-upload-test/" + run + "/"
	t.Logf("bucket %s, prefix %s", bucket, prefix)
	ensureBucket(t, c, bucket)
	t.Cleanup(func() { emptyPrefix(t, c, bucket, prefix) })
	s, err := New(c, Options{Bucket: bucket, TempPrefix: prefix})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	t.Run("proxied", func(t *testing.T) {
		// One small file (single PutObject) and one over a part (multipart).
		for _, size := range []int{1000, partSize + 1234} {
			body := bytes.Repeat([]byte("p"), size)
			w, err := s.Open(ctx, brick.UploadMeta{Name: "ä.txt", Type: "text/plain"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := w.Write(body); err != nil {
				t.Fatal(err)
			}
			ref, err := w.Close()
			if err != nil {
				t.Fatal(err)
			}
			got, err := s.Confirm(ctx, ref.ID)
			if err != nil {
				t.Fatal(err)
			}
			if got.Size != int64(size) || got.Type != "text/plain" || got.Name != "ä.txt" {
				t.Fatalf("size %d: confirm = %+v", size, got)
			}
		}
	})

	t.Run("presigned", func(t *testing.T) {
		body := bytes.Repeat([]byte("q"), 4321)
		// No size is declared; Confirm must report what was stored.
		pu, err := s.Presign(ctx, brick.UploadMeta{Name: "big file.bin", Type: "application/octet-stream"})
		if err != nil {
			t.Fatal(err)
		}
		req, err := http.NewRequest(pu.Method, pu.URL, bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		for k, v := range pu.Headers {
			req.Header.Set(k, v)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("PUT to presigned URL: %s", resp.Status)
		}
		ref, err := s.Confirm(ctx, pu.ID)
		if err != nil {
			t.Fatal(err)
		}
		if ref.Size != int64(len(body)) || ref.Type != "application/octet-stream" || ref.Name != "big file.bin" {
			t.Fatalf("confirm = %+v", ref)
		}

		if err := s.Remove(ctx, ref); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Confirm(ctx, pu.ID); err == nil {
			t.Fatal("object still there after Remove")
		}
	})

	t.Run("presigned length is signed", func(t *testing.T) {
		pu, err := s.Presign(ctx, brick.UploadMeta{Type: "text/plain", Size: 10})
		if err != nil {
			t.Fatal(err)
		}
		req, _ := http.NewRequest(pu.Method, pu.URL, bytes.NewReader(bytes.Repeat([]byte("z"), 11)))
		for k, v := range pu.Headers {
			req.Header.Set(k, v)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			t.Fatal("storage accepted a body longer than the signed length")
		}
	})
}
