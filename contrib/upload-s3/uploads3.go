// Package uploads3 is a brick.UploadSink that stores uploads in S3 or any
// S3-compatible server (MinIO, RustFS, R2). It streams proxied uploads
// into the bucket without holding a whole file in memory, and implements
// the optional UploadRemover and UploadPresigner upgrades so large files
// can go from the browser straight to storage.
//
// The host builds the *s3.Client (credentials, region, endpoint, path
// style); this package never loads credentials itself, so it has no
// opinion about where they come from.
//
// Orphans: every object is written under Options.TempPrefix and stays
// there until the app commits it (copies it to a permanent key through a
// host capability). A file uploaded and never saved is never deleted by
// this package, by design: configure an S3 lifecycle rule that expires
// objects under the temp prefix (for example after one day), and an
// AbortIncompleteMultipartUpload rule for the same prefix, so abandoned
// proxied uploads that died between parts are cleaned up too.
package uploads3

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/ubgo/brick"
)

const (
	// DefaultTempPrefix is where uploads land before the app commits them.
	// One prefix for everything uncommitted is what makes a single
	// lifecycle rule enough to expire orphans.
	DefaultTempPrefix = "brick-uploads/tmp/"

	// DefaultPresignExpiry bounds how long a presigned PUT URL works. Long
	// enough for a large file on a slow link to start, short enough that a
	// leaked URL is useless soon after; the engine's pending-upload record
	// expires with the instance anyway.
	DefaultPresignExpiry = 15 * time.Minute

	// partSize is the buffer for proxied uploads: S3's minimum multipart
	// part size (every part but the last must be at least 5 MiB). A file
	// smaller than one part is sent with a single PutObject, so memory per
	// upload never exceeds one part.
	partSize = 5 << 20

	// idBytes is the random part of an upload id: 128 bits, so ids cannot
	// be guessed, rendered as 32 hex characters.
	idBytes = 16

	// metaName is the user-metadata key that carries the display name, so
	// Confirm can return it without trusting the client a second time.
	metaName = "name"

	// Ref meta keys, so the host can find the object behind a ref.
	refMetaBucket = "bucket"
	refMetaKey    = "key"
)

// Options configures a Sink. Only Bucket is required.
type Options struct {
	// Bucket receives every upload.
	Bucket string
	// TempPrefix is prepended to every object key; DefaultTempPrefix when
	// empty. Point the bucket's expiry lifecycle rule at it.
	TempPrefix string
	// PresignExpiry is how long a presigned PUT URL stays valid;
	// DefaultPresignExpiry when zero.
	PresignExpiry time.Duration
	// PublicURL, when set, turns an object key into UploadRef.URL (a CDN
	// URL, or a handler that signs a GET). Without it refs carry no URL.
	PublicURL func(key string) string
}

// objectAPI is the part of *s3.Client the sink uses, so tests can run
// without a network.
type objectAPI interface {
	PutObject(context.Context, *s3.PutObjectInput, ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	CreateMultipartUpload(context.Context, *s3.CreateMultipartUploadInput, ...func(*s3.Options)) (*s3.CreateMultipartUploadOutput, error)
	UploadPart(context.Context, *s3.UploadPartInput, ...func(*s3.Options)) (*s3.UploadPartOutput, error)
	CompleteMultipartUpload(context.Context, *s3.CompleteMultipartUploadInput, ...func(*s3.Options)) (*s3.CompleteMultipartUploadOutput, error)
	AbortMultipartUpload(context.Context, *s3.AbortMultipartUploadInput, ...func(*s3.Options)) (*s3.AbortMultipartUploadOutput, error)
	HeadObject(context.Context, *s3.HeadObjectInput, ...func(*s3.Options)) (*s3.HeadObjectOutput, error)
	DeleteObject(context.Context, *s3.DeleteObjectInput, ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
}

// putPresigner is the part of *s3.PresignClient the sink uses.
type putPresigner interface {
	PresignPutObject(context.Context, *s3.PutObjectInput, ...func(*s3.PresignOptions)) (*v4.PresignedHTTPRequest, error)
}

// Sink stores uploads in one bucket. It is safe for concurrent use.
type Sink struct {
	api      objectAPI
	presign  putPresigner
	opts     Options
	partSize int
}

var (
	_ brick.UploadSink      = (*Sink)(nil)
	_ brick.UploadRemover   = (*Sink)(nil)
	_ brick.UploadPresigner = (*Sink)(nil)
)

// ErrBadID is returned for an id this sink could not have issued. Ids
// are checked before any key is built, so a forged id can never reach an
// object outside the temp prefix.
var ErrBadID = errors.New("uploads3: malformed upload id")

// New returns a sink writing to opts.Bucket through client.
func New(client *s3.Client, opts Options) (*Sink, error) {
	if client == nil {
		return nil, errors.New("uploads3: nil *s3.Client")
	}
	return newSink(client, s3.NewPresignClient(client), opts)
}

func newSink(api objectAPI, p putPresigner, opts Options) (*Sink, error) {
	if opts.Bucket == "" {
		return nil, errors.New("uploads3: Options.Bucket is required")
	}
	if opts.TempPrefix == "" {
		opts.TempPrefix = DefaultTempPrefix
	}
	if opts.PresignExpiry <= 0 {
		opts.PresignExpiry = DefaultPresignExpiry
	}
	return &Sink{api: api, presign: p, opts: opts, partSize: partSize}, nil
}

// Key is the object key for an upload id.
func (s *Sink) Key(id string) string { return s.opts.TempPrefix + id }

func newID() (string, error) {
	b := make([]byte, idBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func validID(id string) bool {
	if len(id) != 2*idBytes {
		return false
	}
	_, err := hex.DecodeString(id)
	return err == nil && strings.ToLower(id) == id
}

// ref builds the UploadRef for a stored object.
func (s *Sink) ref(id, name, typ string, size int64) brick.UploadRef {
	key := s.Key(id)
	r := brick.UploadRef{
		ID: id, Name: name, Type: typ, Size: size,
		Meta: map[string]string{refMetaBucket: s.opts.Bucket, refMetaKey: key},
	}
	if s.opts.PublicURL != nil {
		r.URL = s.opts.PublicURL(key)
	}
	return r
}

// Open starts a proxied upload.
func (s *Sink) Open(ctx context.Context, m brick.UploadMeta) (brick.UploadWriter, error) {
	id, err := newID()
	if err != nil {
		return nil, err
	}
	return &writer{s: s, ctx: ctx, id: id, meta: m, buf: make([]byte, 0, s.partSize)}, nil
}

// writer buffers at most one part. Until the first part fills it holds
// the whole file and Close sends one PutObject; after that it is a
// multipart upload, aborted on Abort or on any failure.
type writer struct {
	s        *Sink
	ctx      context.Context
	id       string
	meta     brick.UploadMeta
	buf      []byte
	size     int64
	uploadID *string
	parts    []types.CompletedPart
	done     bool
}

func (w *writer) Write(p []byte) (int, error) {
	if w.done {
		return 0, errors.New("uploads3: write after close")
	}
	n := len(p)
	for len(p) > 0 {
		room := w.s.partSize - len(w.buf)
		take := min(room, len(p))
		w.buf = append(w.buf, p[:take]...)
		p = p[take:]
		if len(w.buf) == w.s.partSize {
			if err := w.flush(); err != nil {
				return n - len(p), err
			}
		}
	}
	w.size += int64(n)
	return n, nil
}

func (w *writer) metadata() map[string]string {
	return map[string]string{metaName: url.PathEscape(w.meta.Name)}
}

// flush sends the buffer as the next multipart part.
func (w *writer) flush() error {
	key := aws.String(w.s.Key(w.id))
	if w.uploadID == nil {
		out, err := w.s.api.CreateMultipartUpload(w.ctx, &s3.CreateMultipartUploadInput{
			Bucket: aws.String(w.s.opts.Bucket), Key: key,
			ContentType: aws.String(w.meta.Type), Metadata: w.metadata(),
		})
		if err != nil {
			return err
		}
		w.uploadID = out.UploadId
	}
	num := int32(len(w.parts) + 1)
	out, err := w.s.api.UploadPart(w.ctx, &s3.UploadPartInput{
		Bucket: aws.String(w.s.opts.Bucket), Key: key, UploadId: w.uploadID,
		PartNumber: aws.Int32(num), Body: bytes.NewReader(w.buf),
		ContentLength: aws.Int64(int64(len(w.buf))),
	})
	if err != nil {
		return err
	}
	w.parts = append(w.parts, types.CompletedPart{ETag: out.ETag, PartNumber: aws.Int32(num)})
	w.buf = w.buf[:0]
	return nil
}

func (w *writer) Close() (brick.UploadRef, error) {
	if w.done {
		return brick.UploadRef{}, errors.New("uploads3: closed twice")
	}
	w.done = true
	key := aws.String(w.s.Key(w.id))
	if w.uploadID == nil {
		_, err := w.s.api.PutObject(w.ctx, &s3.PutObjectInput{
			Bucket: aws.String(w.s.opts.Bucket), Key: key,
			Body: bytes.NewReader(w.buf), ContentLength: aws.Int64(int64(len(w.buf))),
			ContentType: aws.String(w.meta.Type), Metadata: w.metadata(),
		})
		if err != nil {
			return brick.UploadRef{}, err
		}
		return w.s.ref(w.id, w.meta.Name, w.meta.Type, w.size), nil
	}
	if len(w.buf) > 0 {
		if err := w.flush(); err != nil {
			return brick.UploadRef{}, errors.Join(err, w.abortMultipart())
		}
	}
	_, err := w.s.api.CompleteMultipartUpload(w.ctx, &s3.CompleteMultipartUploadInput{
		Bucket: aws.String(w.s.opts.Bucket), Key: key, UploadId: w.uploadID,
		MultipartUpload: &types.CompletedMultipartUpload{Parts: w.parts},
	})
	if err != nil {
		return brick.UploadRef{}, errors.Join(err, w.abortMultipart())
	}
	return w.s.ref(w.id, w.meta.Name, w.meta.Type, w.size), nil
}

// Abort discards the upload. Before the first part nothing reached
// storage, so only a started multipart upload needs aborting.
func (w *writer) Abort(error) error {
	if w.done {
		return nil
	}
	w.done = true
	w.buf = nil
	if w.uploadID == nil {
		return nil
	}
	return w.abortMultipart()
}

// abortTimeout bounds the abort of a failed multipart upload. The abort
// runs on its own context (the request's may be gone), so it needs its own
// limit; a failed abort is not retried, because the
// AbortIncompleteMultipartUpload lifecycle rule the package doc asks for
// removes the incomplete upload anyway.
const abortTimeout = 30 * time.Second

func (w *writer) abortMultipart() error {
	// The request context may already be cancelled (the client went away),
	// so the abort gets its own; the lifecycle rule is the backstop.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(w.ctx), abortTimeout)
	defer cancel()
	_, err := w.s.api.AbortMultipartUpload(ctx, &s3.AbortMultipartUploadInput{
		Bucket: aws.String(w.s.opts.Bucket), Key: aws.String(w.s.Key(w.id)), UploadId: w.uploadID,
	})
	return err
}

// Remove deletes the object behind ref. The key is rebuilt from the id,
// never taken from ref.Meta, so a ref cannot point Remove elsewhere.
func (s *Sink) Remove(ctx context.Context, ref brick.UploadRef) error {
	if !validID(ref.ID) {
		return ErrBadID
	}
	_, err := s.api.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.opts.Bucket), Key: aws.String(s.Key(ref.ID)),
	})
	return err
}

// unsentHeaders are signed by the presigner but set by the browser
// itself (XHR forbids setting them), so they are left out of Headers.
var unsentHeaders = map[string]bool{"host": true, "content-length": true}

// Presign returns a PUT URL for one new object under the temp prefix.
// The content type and the name metadata are signed, so the browser must
// send exactly the headers returned; when the client declared a size, the
// length is signed too and storage refuses any other body length.
func (s *Sink) Presign(ctx context.Context, m brick.UploadMeta) (brick.PresignedUpload, error) {
	id, err := newID()
	if err != nil {
		return brick.PresignedUpload{}, err
	}
	in := &s3.PutObjectInput{
		Bucket: aws.String(s.opts.Bucket), Key: aws.String(s.Key(id)),
		ContentType: aws.String(m.Type),
		Metadata:    map[string]string{metaName: url.PathEscape(m.Name)},
	}
	if m.Size > 0 {
		in.ContentLength = aws.Int64(m.Size)
	}
	req, err := s.presign.PresignPutObject(ctx, in, s3.WithPresignExpires(s.opts.PresignExpiry))
	if err != nil {
		return brick.PresignedUpload{}, err
	}
	headers := map[string]string{}
	for k, v := range req.SignedHeader {
		if unsentHeaders[strings.ToLower(k)] || len(v) == 0 {
			continue
		}
		headers[k] = v[0]
	}
	method := req.Method
	if method == "" {
		method = http.MethodPut
	}
	return brick.PresignedUpload{URL: req.URL, Method: method, Headers: headers, ID: id}, nil
}

// Confirm reads the stored object for id and returns its ref. Size and
// type come from storage (HeadObject), never from the client: the type is
// the one Presign signed, and the size is what was actually stored.
func (s *Sink) Confirm(ctx context.Context, id string) (brick.UploadRef, error) {
	if !validID(id) {
		return brick.UploadRef{}, ErrBadID
	}
	out, err := s.api.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.opts.Bucket), Key: aws.String(s.Key(id)),
	})
	if err != nil {
		return brick.UploadRef{}, fmt.Errorf("uploads3: confirm %s: %w", id, err)
	}
	name := out.Metadata[metaName]
	if n, err := url.PathUnescape(name); err == nil {
		name = n
	}
	return s.ref(id, name, aws.ToString(out.ContentType), aws.ToInt64(out.ContentLength)), nil
}
