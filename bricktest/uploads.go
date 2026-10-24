package bricktest

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/ubgo/brick"
)

// MemoryUploads is an in-memory brick.UploadSink and UploadRemover for
// theme tests: it keeps committed files by id, and
// records every Abort so a test can check that a refused upload left
// nothing behind. Ids are "u1", "u2", … in order; URLs are
// "/uploads/<id>". Safe for concurrent use.
type MemoryUploads struct {
	mu      sync.Mutex
	files   map[string][]byte
	refs    map[string]brick.UploadRef
	aborted []error
	removed []string
	n       int
}

var (
	_ brick.UploadSink    = (*MemoryUploads)(nil)
	_ brick.UploadRemover = (*MemoryUploads)(nil)
)

// memoryUploadURLPrefix is the URL MemoryUploads gives each ref.
const memoryUploadURLPrefix = "/uploads/"

// NewMemoryUploads returns an empty sink.
func NewMemoryUploads() *MemoryUploads {
	return &MemoryUploads{files: map[string][]byte{}, refs: map[string]brick.UploadRef{}}
}

// Open starts one file.
func (m *MemoryUploads) Open(_ context.Context, meta brick.UploadMeta) (brick.UploadWriter, error) {
	return &memoryWriter{m: m, meta: meta}, nil
}

// Remove forgets a file.
func (m *MemoryUploads) Remove(_ context.Context, ref brick.UploadRef) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.removed = append(m.removed, ref.ID)
	delete(m.files, ref.ID)
	delete(m.refs, ref.ID)
	return nil
}

// File returns a committed file's bytes and ref.
func (m *MemoryUploads) File(id string) ([]byte, brick.UploadRef, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.files[id]
	return bytes.Clone(b), m.refs[id], ok
}

// Len is how many committed files the sink holds.
func (m *MemoryUploads) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.files)
}

// Aborted are the reasons of every aborted file, in order.
func (m *MemoryUploads) Aborted() []error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]error(nil), m.aborted...)
}

// Removed are the ids Remove was called with, in order.
func (m *MemoryUploads) Removed() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.removed...)
}

type memoryWriter struct {
	m    *MemoryUploads
	meta brick.UploadMeta
	buf  bytes.Buffer
}

func (w *memoryWriter) Write(p []byte) (int, error) { return w.buf.Write(p) }

func (w *memoryWriter) Close() (brick.UploadRef, error) {
	w.m.mu.Lock()
	defer w.m.mu.Unlock()
	w.m.n++
	id := fmt.Sprintf("u%d", w.m.n)
	ref := brick.UploadRef{ID: id, Name: w.meta.Name, Type: w.meta.Type, Size: int64(w.buf.Len()), URL: memoryUploadURLPrefix + id}
	w.m.files[id] = bytes.Clone(w.buf.Bytes())
	w.m.refs[id] = ref
	return ref, nil
}

func (w *memoryWriter) Abort(err error) error {
	w.m.mu.Lock()
	defer w.m.mu.Unlock()
	w.m.aborted = append(w.m.aborted, err)
	return nil
}

// Upload sends one file to field through the engine's whole upload
// pipeline (the $upload middleware, limits, sniffing, the sink), as the
// runtime's brick:upload does, and replaces the held HTML. typ is the
// type the browser would claim. The test fails on an error; TryUpload
// returns it.
func (r *LiveRoot) Upload(field, name, typ string, data []byte) *brick.ActionResult {
	r.live.t.Helper()
	res, err := r.TryUpload(field, name, typ, data)
	if err != nil {
		r.live.t.Fatalf("bricktest: upload %s on %s: %v", field, r.id, err)
	}
	return res
}

// TryUpload is Upload returning the error instead of failing.
func (r *LiveRoot) TryUpload(field, name, typ string, data []byte) (*brick.ActionResult, error) {
	el, ok := r.element()
	if !ok {
		return nil, fmt.Errorf("bricktest: component %s is no longer on the page", r.id)
	}
	res, err := r.live.eng.RunUpload(context.Background(), brick.ActionRequest{Handle: el.attrs["data-brick-state"], Token: el.attrs["data-brick-token"]},
		brick.UploadFile{Field: field, Name: name, Type: typ, Size: int64(len(data)), Body: bytes.NewReader(data)}, r.live.opts...)
	if err != nil {
		return nil, err
	}
	r.live.html = r.live.html[:el.start] + strings.TrimSpace(string(res.HTML)) + r.live.html[el.end:]
	r.live.source[res] = r.id
	return res, nil
}
