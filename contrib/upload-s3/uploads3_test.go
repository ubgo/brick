package uploads3

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/ubgo/brick"
)

// fakeS3 is an in-memory objectAPI: enough of S3 to check what the sink
// sends, with no network.
type fakeS3 struct {
	mu        sync.Mutex
	objects   map[string]fakeObject
	multipart map[string][][]byte
	aborted   int
	nextID    int
}

type fakeObject struct {
	body []byte
	typ  string
	meta map[string]string
}

func newFake() *fakeS3 {
	return &fakeS3{objects: map[string]fakeObject{}, multipart: map[string][][]byte{}}
}

func (f *fakeS3) PutObject(_ context.Context, in *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	b, _ := io.ReadAll(in.Body)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.objects[*in.Key] = fakeObject{b, aws.ToString(in.ContentType), in.Metadata}
	return &s3.PutObjectOutput{}, nil
}

func (f *fakeS3) CreateMultipartUpload(_ context.Context, in *s3.CreateMultipartUploadInput, _ ...func(*s3.Options)) (*s3.CreateMultipartUploadOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	id := string(rune('a' + f.nextID))
	f.multipart[id] = nil
	f.objects["pending:"+id] = fakeObject{typ: aws.ToString(in.ContentType), meta: in.Metadata}
	return &s3.CreateMultipartUploadOutput{UploadId: aws.String(id)}, nil
}

func (f *fakeS3) UploadPart(_ context.Context, in *s3.UploadPartInput, _ ...func(*s3.Options)) (*s3.UploadPartOutput, error) {
	b, _ := io.ReadAll(in.Body)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.multipart[*in.UploadId] = append(f.multipart[*in.UploadId], b)
	return &s3.UploadPartOutput{ETag: aws.String("etag")}, nil
}

func (f *fakeS3) CompleteMultipartUpload(_ context.Context, in *s3.CompleteMultipartUploadInput, _ ...func(*s3.Options)) (*s3.CompleteMultipartUploadOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	o := f.objects["pending:"+*in.UploadId]
	delete(f.objects, "pending:"+*in.UploadId)
	o.body = bytes.Join(f.multipart[*in.UploadId], nil)
	delete(f.multipart, *in.UploadId)
	f.objects[*in.Key] = o
	return &s3.CompleteMultipartUploadOutput{}, nil
}

func (f *fakeS3) AbortMultipartUpload(_ context.Context, in *s3.AbortMultipartUploadInput, _ ...func(*s3.Options)) (*s3.AbortMultipartUploadOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.aborted++
	delete(f.multipart, *in.UploadId)
	delete(f.objects, "pending:"+*in.UploadId)
	return &s3.AbortMultipartUploadOutput{}, nil
}

func (f *fakeS3) HeadObject(_ context.Context, in *s3.HeadObjectInput, _ ...func(*s3.Options)) (*s3.HeadObjectOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	o, ok := f.objects[*in.Key]
	if !ok {
		return nil, errors.New("not found")
	}
	return &s3.HeadObjectOutput{
		ContentLength: aws.Int64(int64(len(o.body))), ContentType: aws.String(o.typ), Metadata: o.meta,
	}, nil
}

func (f *fakeS3) DeleteObject(_ context.Context, in *s3.DeleteObjectInput, _ ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.objects, *in.Key)
	return &s3.DeleteObjectOutput{}, nil
}

type fakePresigner struct{ in *s3.PutObjectInput }

func (p *fakePresigner) PresignPutObject(_ context.Context, in *s3.PutObjectInput, _ ...func(*s3.PresignOptions)) (*v4.PresignedHTTPRequest, error) {
	p.in = in
	return &v4.PresignedHTTPRequest{
		URL: "https://s3.test/" + *in.Key, Method: http.MethodPut,
		SignedHeader: http.Header{
			"Host": {"s3.test"}, "Content-Length": {"3"},
			"Content-Type": {*in.ContentType}, "X-Amz-Meta-Name": {in.Metadata[metaName]},
		},
	}, nil
}

func testSink(t *testing.T, opts Options) (*Sink, *fakeS3, *fakePresigner) {
	t.Helper()
	f, p := newFake(), &fakePresigner{}
	if opts.Bucket == "" {
		opts.Bucket = "b"
	}
	s, err := newSink(f, p, opts)
	if err != nil {
		t.Fatal(err)
	}
	return s, f, p
}

func TestDefaults(t *testing.T) {
	s, _, _ := testSink(t, Options{})
	if s.opts.TempPrefix != DefaultTempPrefix || s.opts.PresignExpiry != DefaultPresignExpiry {
		t.Fatalf("defaults not applied: %+v", s.opts)
	}
	s, _, _ = testSink(t, Options{TempPrefix: "up/", PresignExpiry: time.Minute})
	if s.opts.TempPrefix != "up/" || s.opts.PresignExpiry != time.Minute {
		t.Fatalf("explicit options overridden: %+v", s.opts)
	}
	if _, err := newSink(newFake(), &fakePresigner{}, Options{}); err == nil {
		t.Fatal("missing bucket accepted")
	}
	if _, err := New(nil, Options{Bucket: "b"}); err == nil {
		t.Fatal("nil client accepted")
	}
}

func TestIDShape(t *testing.T) {
	seen := map[string]bool{}
	for range 100 {
		id, err := newID()
		if err != nil {
			t.Fatal(err)
		}
		if !validID(id) || seen[id] {
			t.Fatalf("bad or repeated id %q", id)
		}
		seen[id] = true
	}
	for _, bad := range []string{"", "../etc/passwd", strings.Repeat("g", 32), strings.Repeat("A", 32), strings.Repeat("a", 31), strings.Repeat("a", 33)} {
		if validID(bad) {
			t.Fatalf("validID(%q) = true", bad)
		}
	}
}

func upload(t *testing.T, s *Sink, m brick.UploadMeta, body []byte) brick.UploadRef {
	t.Helper()
	w, err := s.Open(context.Background(), m)
	if err != nil {
		t.Fatal(err)
	}
	// Small writes, so a part boundary falls inside a Write.
	for r := bytes.NewReader(body); ; {
		chunk := make([]byte, 7)
		n, err := r.Read(chunk)
		if n > 0 {
			if _, werr := w.Write(chunk[:n]); werr != nil {
				t.Fatal(werr)
			}
		}
		if err == io.EOF {
			break
		}
	}
	ref, err := w.Close()
	if err != nil {
		t.Fatal(err)
	}
	return ref
}

func TestProxiedUpload(t *testing.T) {
	for _, tc := range []struct {
		name string
		size int
	}{{"single put", 10}, {"multipart exact", 32}, {"multipart tail", 45}} {
		t.Run(tc.name, func(t *testing.T) {
			s, f, _ := testSink(t, Options{PublicURL: func(k string) string { return "https://cdn/" + k }})
			s.partSize = 16
			body := bytes.Repeat([]byte("x"), tc.size)
			m := brick.UploadMeta{Name: "my photo é.png", Type: "image/png"}
			ref := upload(t, s, m, body)
			if !validID(ref.ID) || ref.Size != int64(tc.size) || ref.Name != m.Name || ref.Type != "image/png" {
				t.Fatalf("ref = %+v", ref)
			}
			key := DefaultTempPrefix + ref.ID
			if ref.URL != "https://cdn/"+key || ref.Meta[refMetaKey] != key || ref.Meta[refMetaBucket] != "b" {
				t.Fatalf("ref url/meta = %+v", ref)
			}
			if got := f.objects[key].body; !bytes.Equal(got, body) {
				t.Fatalf("stored %d bytes, want %d", len(got), len(body))
			}
			c, err := s.Confirm(context.Background(), ref.ID)
			if err != nil || c.Name != m.Name || c.Size != ref.Size {
				t.Fatalf("confirm = %+v, %v", c, err)
			}
		})
	}
}

func TestAbort(t *testing.T) {
	s, f, _ := testSink(t, Options{})
	s.partSize = 4
	w, _ := s.Open(context.Background(), brick.UploadMeta{Type: "text/plain"})
	if _, err := w.Write([]byte("0123456789")); err != nil {
		t.Fatal(err)
	}
	if err := w.Abort(errors.New("too big")); err != nil {
		t.Fatal(err)
	}
	if f.aborted != 1 || len(f.objects) != 0 {
		t.Fatalf("aborted=%d objects=%d", f.aborted, len(f.objects))
	}
	// Abort before the first part leaves nothing to clean up.
	w, _ = s.Open(context.Background(), brick.UploadMeta{})
	_, _ = w.Write([]byte("ab"))
	if err := w.Abort(nil); err != nil || f.aborted != 1 || len(f.objects) != 0 {
		t.Fatalf("small abort: err=%v aborted=%d objects=%d", err, f.aborted, len(f.objects))
	}
}

func TestPresign(t *testing.T) {
	s, _, p := testSink(t, Options{})
	pu, err := s.Presign(context.Background(), brick.UploadMeta{Name: "a b.pdf", Type: "application/pdf", Size: 3})
	if err != nil {
		t.Fatal(err)
	}
	if !validID(pu.ID) || *p.in.Key != DefaultTempPrefix+pu.ID || pu.Method != http.MethodPut {
		t.Fatalf("presigned = %+v key %s", pu, *p.in.Key)
	}
	if aws.ToInt64(p.in.ContentLength) != 3 || *p.in.ContentType != "application/pdf" {
		t.Fatalf("signed input = %+v", p.in)
	}
	if _, ok := pu.Headers["Host"]; ok {
		t.Fatal("Host returned as a header to send")
	}
	if _, ok := pu.Headers["Content-Length"]; ok {
		t.Fatal("Content-Length returned as a header to send")
	}
	if pu.Headers["Content-Type"] != "application/pdf" || pu.Headers["X-Amz-Meta-Name"] != "a%20b.pdf" {
		t.Fatalf("headers = %v", pu.Headers)
	}
	// An undeclared size is not signed.
	if _, err := s.Presign(context.Background(), brick.UploadMeta{Type: "text/plain"}); err != nil || p.in.ContentLength != nil {
		t.Fatalf("unsized presign signed a length: %v %v", err, p.in.ContentLength)
	}
}

func TestConfirmReadsStorage(t *testing.T) {
	s, f, _ := testSink(t, Options{})
	id, _ := newID()
	f.objects[s.Key(id)] = fakeObject{body: make([]byte, 1234), typ: "image/jpeg", meta: map[string]string{metaName: "x%2Fy.jpg"}}
	ref, err := s.Confirm(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if ref.Size != 1234 || ref.Type != "image/jpeg" || ref.Name != "x/y.jpg" {
		t.Fatalf("ref = %+v", ref)
	}
	if _, err := s.Confirm(context.Background(), "../secret"); !errors.Is(err, ErrBadID) {
		t.Fatalf("forged id: %v", err)
	}
	missing, _ := newID()
	if _, err := s.Confirm(context.Background(), missing); err == nil {
		t.Fatal("missing object confirmed")
	}
}

func TestRemove(t *testing.T) {
	s, f, _ := testSink(t, Options{})
	ref := upload(t, s, brick.UploadMeta{Type: "text/plain"}, []byte("hi"))
	// A ref whose meta points elsewhere still deletes only its own key.
	ref.Meta[refMetaKey] = "other"
	f.objects["other"] = fakeObject{}
	if err := s.Remove(context.Background(), ref); err != nil {
		t.Fatal(err)
	}
	if _, ok := f.objects[s.Key(ref.ID)]; ok {
		t.Fatal("object not removed")
	}
	if _, ok := f.objects["other"]; !ok {
		t.Fatal("Remove followed ref.Meta")
	}
	if err := s.Remove(context.Background(), brick.UploadRef{ID: "x"}); !errors.Is(err, ErrBadID) {
		t.Fatalf("bad id: %v", err)
	}
}
