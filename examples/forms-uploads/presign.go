package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/ubgo/brick"
)

// idBytes is how many random bytes name one stored object: 128 bits, so
// an id cannot be guessed.
const idBytes = 16

// maxStoredBytes caps one PUT to the store. The engine re-checks the
// field's max_size on what Confirm reports, but a real store also refuses
// an oversized body itself, as an S3 presigned PUT does.
const maxStoredBytes = 10 << 20

// object is one stored file: its bytes and the Content-Type the PUT
// carried.
type object struct {
	data  []byte
	ctype string
	name  string
}

// presignStore stands in for object storage (S3, R2, GCS) in this demo:
// Presign hands the browser a URL under prefix to PUT the file to, the
// store's own handler receives it, and Confirm reports what was stored,
// never what the client claimed. A real deployment uses
// contrib/upload-s3; this one keeps everything in memory.
// ds:def id=presignstore-79nd9e2c
type presignStore struct {
	// Presign and Confirm (below) make this a brick.UploadPresigner; Open
	// keeps the proxied path every UploadSink has.
	prefix string
	mu     sync.Mutex
	issued map[string]string // id → the file name Presign was asked for
	stored map[string]object
}

// newPresignStore returns an empty in-memory store; prefix is the URL path its
// presigned upload and download links hang under.
func newPresignStore(prefix string) *presignStore {
	return &presignStore{prefix: prefix, issued: map[string]string{}, stored: map[string]object{}}
}

// Open is the proxied path, which every UploadSink has. The runtime
// presigns whenever the sink offers it, but a form posted with JavaScript
// off streams its files through the engine, so they arrive here.
// ds:def id=presignstore-open-p5uyd7aq
func (s *presignStore) Open(_ context.Context, m brick.UploadMeta) (brick.UploadWriter, error) {
	id, err := newID()
	if err != nil {
		return nil, err
	}
	return &memWriter{s: s, id: id, meta: m}, nil
}

// memWriter buffers one proxied file and stores it on Close.
type memWriter struct {
	s    *presignStore
	id   string
	meta brick.UploadMeta
	buf  bytes.Buffer
}

// Write buffers p in memory; the upload only becomes visible at Close, so a
// half-written file is never served.
func (w *memWriter) Write(p []byte) (int, error) { return w.buf.Write(p) }

// Close stores the file under its id; m.Type is already the sniffed type.
func (w *memWriter) Close() (brick.UploadRef, error) {
	w.s.mu.Lock()
	w.s.stored[w.id] = object{data: w.buf.Bytes(), ctype: w.meta.Type, name: w.meta.Name}
	w.s.mu.Unlock()
	return brick.UploadRef{ID: w.id, Name: w.meta.Name, Type: w.meta.Type, Size: int64(w.buf.Len()), URL: w.s.prefix + w.id}, nil
}

// Abort drops what was buffered; nothing was stored yet.
func (w *memWriter) Abort(error) error {
	w.buf.Reset()
	return nil
}

// newID is a fresh random object id.
func newID() (string, error) {
	b := make([]byte, idBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// Presign issues a fresh random id and the URL to PUT the bytes to.
// ds:def id=presignstore-presign-89gnczqg
func (s *presignStore) Presign(_ context.Context, m brick.UploadMeta) (brick.PresignedUpload, error) {
	id, err := newID()
	if err != nil {
		return brick.PresignedUpload{}, err
	}
	s.mu.Lock()
	s.issued[id] = m.Name
	s.mu.Unlock()
	return brick.PresignedUpload{URL: s.prefix + id, Method: http.MethodPut, Headers: map[string]string{"Content-Type": m.Type}, ID: id}, nil
}

// Confirm reads the stored object for an id this store issued. The size
// is the stored length; the type is the Content-Type the PUT carried,
// which the client chose, so nothing here has sniffed the bytes.
// ds:def id=presignstore-confirm-n7s3whns
func (s *presignStore) Confirm(_ context.Context, id string) (brick.UploadRef, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	o, ok := s.stored[id]
	if !ok {
		return brick.UploadRef{}, errors.New("presignStore: nothing was uploaded under that id")
	}
	return brick.UploadRef{ID: id, Name: o.name, Type: o.ctype, Size: int64(len(o.data)), URL: s.prefix + id}, nil
}

// Remove forgets one file, which is what lets $removeUpload work.
// ds:def id=presignstore-remove-32zmy2vq
func (s *presignStore) Remove(_ context.Context, ref brick.UploadRef) error {
	s.mu.Lock()
	delete(s.stored, ref.ID)
	s.mu.Unlock()
	return nil
}

// ServeHTTP is the storage side: PUT stores an issued id once, GET serves
// it back. Mounted under the prefix with http.StripPrefix.
func (s *presignStore) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/")
	switch r.Method {
	case http.MethodPut:
		s.mu.Lock()
		name, ok := s.issued[id]
		delete(s.issued, id)
		s.mu.Unlock()
		if !ok {
			http.Error(w, "unknown upload id", http.StatusForbidden)
			return
		}
		data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxStoredBytes))
		if err != nil {
			http.Error(w, "too large", http.StatusRequestEntityTooLarge)
			return
		}
		s.mu.Lock()
		s.stored[id] = object{data: data, ctype: r.Header.Get("Content-Type"), name: name}
		s.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	case http.MethodGet:
		s.mu.Lock()
		o, ok := s.stored[id]
		s.mu.Unlock()
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", o.ctype)
		_, _ = io.Copy(w, bytes.NewReader(o.data))
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
