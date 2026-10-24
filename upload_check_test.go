package brick

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// An UploadCheck sees the sniffed meta and the first bytes after the
// declared checks and before the sink opens; RefuseUpload refuses with the
// host's message as a 422 field error (ErrBadValue), and the sink never
// sees the file.
func TestUploadCheckRefusesBeforeSink(t *testing.T) {
	t.Parallel()
	sink := newMemSink()
	var seen []UploadMeta
	var heads [][]byte
	check := func(_ context.Context, m UploadMeta, head []byte) error {
		seen = append(seen, m)
		heads = append(heads, head)
		if m.Name == "evil.png" {
			return RefuseUpload("this image is not allowed")
		}
		if m.Name == "broken.png" {
			return errors.New("scanner offline")
		}
		return nil
	}
	e, req := uploadEngine(t, sink, Options{UploadCheck: check})
	ctx := context.Background()
	data := append(append([]byte{}, pngHead...), bytes.Repeat([]byte("x"), 600)...)

	_, err := e.RunUpload(ctx, req, file("avatar", "evil.png", "image/png", data))
	if !errors.Is(err, ErrUploadRefused) || !errors.Is(err, ErrBadValue) || err.Error() != "this image is not allowed" {
		t.Fatalf("refusal = %v", err)
	}
	if ActionStatus(err) != http.StatusUnprocessableEntity {
		t.Fatalf("status %d", ActionStatus(err))
	}
	if sink.opened != 0 {
		t.Fatal("a refused file reached the sink")
	}
	if len(heads) != 1 || len(heads[0]) != UploadCheckBytes || !bytes.HasPrefix(heads[0], pngHead) || seen[0].Type != "image/png" || seen[0].Field != "avatar" || seen[0].Component != "Uploader" {
		t.Fatalf("the check saw %+v and %d bytes", seen, len(heads[0]))
	}

	// A host failure is not a refusal: not a field error.
	_, err = e.RunUpload(ctx, req, file("avatar", "broken.png", "image/png", data))
	if err == nil || errors.Is(err, ErrBadValue) || !strings.Contains(err.Error(), "scanner offline") {
		t.Fatalf("host failure = %v", err)
	}

	// A file the declared accept list refuses never reaches the check.
	n := len(seen)
	if _, err := e.RunUpload(ctx, req, file("avatar", "a.txt", "text/plain", []byte("hello"))); !errors.Is(err, ErrUploadType) {
		t.Fatalf("type: %v", err)
	}
	if len(seen) != n {
		t.Fatal("the check ran before the accept list")
	}

	res, err := e.RunUpload(ctx, req, file("avatar", "ok.png", "image/png", pngHead))
	if err != nil {
		t.Fatal(err)
	}
	if sink.opened != 1 || !strings.Contains(string(res.HTML), ":ok.png:") {
		t.Fatalf("an accepted file was not stored: %s", res.HTML)
	}
	// The check got a copy: changing it cannot change the stored bytes.
	heads[len(heads)-1][0] = 'X'
	if f := sink.files["f1"]; !bytes.HasPrefix(f, pngHead) {
		t.Fatal("the check's head aliases the stored bytes")
	}
}

// Over HTTP the refusal is a 422 whose body is the host's message, safe in
// Prod.
func TestUploadCheckHTTP(t *testing.T) {
	t.Parallel()
	refuse := func(context.Context, UploadMeta, []byte) error { return RefuseUpload("no thanks") }
	sink := newMemSink()
	e, req := uploadEngine(t, sink, Options{UploadCheck: refuse, Mode: Prod})
	r := httptest.NewRequest(http.MethodPut, "/_brick/upload", bytes.NewReader(pngHead))
	r.Header.Set(uploadFieldHeader, "avatar")
	r.Header.Set(uploadHandleHeader, req.Handle)
	r.Header.Set(uploadTokenHeader, req.Token)
	r.Header.Set("Content-Type", "image/png")
	w := httptest.NewRecorder()
	e.UploadHandler().ServeHTTP(w, r)
	if w.Code != http.StatusUnprocessableEntity || strings.TrimSpace(w.Body.String()) != "no thanks" || sink.opened != 0 {
		t.Fatalf("%d %q opened=%d", w.Code, w.Body.String(), sink.opened)
	}
}

// A presign request runs the check on the claims with no bytes, before the
// sink presigns anything.
func TestUploadCheckPresign(t *testing.T) {
	t.Parallel()
	sink := &presignSink{removingSink: removingSink{newMemSink()}, stored: map[string]UploadRef{}}
	var head []byte
	called := false
	check := func(_ context.Context, m UploadMeta, h []byte) error {
		called, head = true, h
		if m.Name == "no.png" {
			return RefuseUpload("refused")
		}
		return nil
	}
	e, req := uploadEngine(t, sink, Options{UploadCheck: check})
	_, err := e.RunUpload(context.Background(), req, UploadFile{Field: "avatar", Name: "no.png", Type: "image/png", Size: 10, Presign: true})
	if !errors.Is(err, ErrUploadRefused) || !called || head != nil || len(sink.stored) != 0 {
		t.Fatalf("err=%v called=%v head=%v stored=%d", err, called, head, len(sink.stored))
	}
	if _, err := e.RunUpload(context.Background(), req, UploadFile{Field: "avatar", Name: "yes.png", Type: "image/png", Size: 10, Presign: true}); err != nil || len(sink.stored) != 1 {
		t.Fatalf("accepted presign: %v, stored=%d", err, len(sink.stored))
	}
}

// RefuseUpload with no message still reads as a refusal.
func TestRefuseUploadEmpty(t *testing.T) {
	t.Parallel()
	if err := RefuseUpload(""); err.Error() != ErrUploadRefused.Error() || !errors.Is(err, ErrBadValue) {
		t.Fatalf("%v", err)
	}
}
