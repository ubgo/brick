package brick

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// Tests for uploads through an UploadSink. BV in
// a comment marks a break-verified test.

// pngHead is the PNG signature: enough for http.DetectContentType.
var pngHead = []byte("\x89PNG\r\n\x1a\n" + strings.Repeat("\x00", 24))

// memSink is a test UploadSink: it keeps committed files, records every
// Abort, and removes on request.
type memSink struct {
	files   map[string][]byte
	refs    map[string]UploadRef
	aborted []error
	removed []string
	opened  int
	n       int
}

func newMemSink() *memSink {
	return &memSink{files: map[string][]byte{}, refs: map[string]UploadRef{}}
}

type memWriter struct {
	s   *memSink
	m   UploadMeta
	buf bytes.Buffer
}

func (s *memSink) Open(_ context.Context, m UploadMeta) (UploadWriter, error) {
	s.opened++
	return &memWriter{s: s, m: m}, nil
}

func (w *memWriter) Write(p []byte) (int, error) { return w.buf.Write(p) }

func (w *memWriter) Close() (UploadRef, error) {
	w.s.n++
	id := fmt.Sprintf("f%d", w.s.n)
	w.s.files[id] = w.buf.Bytes()
	ref := UploadRef{ID: id, Name: w.m.Name, Type: w.m.Type, Size: int64(w.buf.Len()), URL: "/files/" + id}
	w.s.refs[id] = ref
	return ref, nil
}

func (w *memWriter) Abort(err error) error {
	w.s.aborted = append(w.s.aborted, err)
	return nil
}

// removingSink adds UploadRemover.
type removingSink struct{ *memSink }

func (s removingSink) Remove(_ context.Context, ref UploadRef) error {
	s.removed = append(s.removed, ref.ID)
	delete(s.files, ref.ID)
	return nil
}

// presignSink adds UploadPresigner and UploadRemover: Confirm reports
// stored, the object as the "storage" holds it.
type presignSink struct {
	removingSink
	stored map[string]UploadRef
}

func (s *presignSink) Presign(_ context.Context, m UploadMeta) (PresignedUpload, error) {
	s.n++
	id := fmt.Sprintf("p%d", s.n)
	s.stored[id] = UploadRef{ID: id, Name: m.Name, Type: m.Type, Size: m.Size}
	return PresignedUpload{URL: "https://storage.test/" + id, Method: http.MethodPut, ID: id}, nil
}

func (s *presignSink) Confirm(_ context.Context, id string) (UploadRef, error) {
	ref, ok := s.stored[id]
	if !ok {
		return UploadRef{}, errors.New("no such object")
	}
	return ref, nil
}

const uploaderComponent = `---
state:
  avatar: null
  photos: []
  note: ""
brick:
  uploads:
    avatar: {accept: [image/png, image/jpeg], max_size: 1KB}
    photos: {accept: [image/*], max_size: 1KB, max_files: 2}
  rules:
    avatar: [required]
---
<div><i>{{ with .State.avatar }}{{ .id }}:{{ .name }}:{{ .type }}:{{ .size }}:{{ .url }}{{ end }}|{{ range .State.photos }}{{ .id }},{{ end }}|{{ range $k, $v := .Errors }}{{ $k }}={{ $v }};{{ end }}</i><input type="file" brick:upload="avatar"><progress brick:upload-progress="avatar"></progress><button brick:click='$removeUpload("photos", "f1")'>x</button></div>`

func uploadEngine(t *testing.T, sink UploadSink, opts Options) (*Engine, ActionRequest) {
	t.Helper()
	opts.FS = theme("components/Uploader.html", uploaderComponent, "content/a.html", `<Uploader />`)
	opts.States = NewMemoryStates(0, 0)
	opts.Uploads = sink
	e := newEngine(t, opts)
	page, err := e.Render(context.Background(), "content/a.html", nil)
	if err != nil {
		t.Fatal(err)
	}
	h, tok := handleOf(t, string(page))
	return e, ActionRequest{Handle: h, Token: tok}
}

func file(field, name, typ string, data []byte) UploadFile {
	return UploadFile{Field: field, Name: name, Type: typ, Size: int64(len(data)), Body: bytes.NewReader(data)}
}

// next carries the instance's new handle into the next request.
func next(t *testing.T, res *ActionResult) ActionRequest {
	t.Helper()
	h, tok := handleOf(t, string(res.HTML))
	return ActionRequest{Handle: h, Token: tok}
}

func TestUploadSetsRefAndRerenders(t *testing.T) {
	sink := newMemSink()
	e, req := uploadEngine(t, sink, Options{})
	res, err := e.RunUpload(context.Background(), req, file("avatar", "me.png", "image/png", pngHead))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := body(t, res.HTML), "f1:me.png:image/png:32:/files/f1||"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if res.Version != 1 || !bytes.Equal(sink.files["f1"], pngHead) {
		t.Fatalf("version %d, stored %q", res.Version, sink.files["f1"])
	}
	// A list field appends.
	req = next(t, res)
	res, err = e.RunUpload(context.Background(), req, file("photos", "a.png", "image/png", pngHead))
	if err != nil {
		t.Fatal(err)
	}
	if got := body(t, res.HTML); !strings.Contains(got, "|f2,|") {
		t.Fatalf("photos not appended: %q", got)
	}
}

// The middleware sees $upload with the field and the claimed meta, and
// can refuse it before a byte is stored.
func TestUploadMiddlewareRefuses(t *testing.T) {
	sink := newMemSink()
	var seen *ActionCall
	mw := func(next ActionFunc) ActionFunc {
		return func(ctx context.Context, a *ActionCall) (*ActionResult, error) {
			seen = a
			return nil, ErrForbidden
		}
	}
	e, req := uploadEngine(t, sink, Options{ActionMiddleware: []ActionMiddleware{mw}})
	_, err := e.RunUpload(context.Background(), req, file("avatar", "me.png", "image/png", pngHead))
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("err = %v", err)
	}
	if seen == nil || seen.Action != "$upload" || seen.Args[0] != "avatar" || seen.Args[1].(map[string]any)["name"] != "me.png" {
		t.Fatalf("middleware saw %+v", seen)
	}
	if sink.opened != 0 {
		t.Fatal("the sink was opened despite the refusal")
	}
}

// Over max_size while streaming is 413 and aborts the writer; a declared
// size over the limit is refused before Open. BV: dropping the
// written > maxSize check makes the streaming case succeed.
func TestUploadTooLarge(t *testing.T) {
	sink := newMemSink()
	e, req := uploadEngine(t, sink, Options{})
	big := append(append([]byte{}, pngHead...), make([]byte, 1100)...)
	f := file("avatar", "big.png", "image/png", big)
	f.Size = 10 // the client lies about the size
	_, err := e.RunUpload(context.Background(), req, f)
	if !errors.Is(err, ErrUploadTooLarge) {
		t.Fatalf("err = %v", err)
	}
	if len(sink.aborted) != 1 || len(sink.files) != 0 {
		t.Fatalf("aborted %v, files %d", sink.aborted, len(sink.files))
	}
	_, err = e.RunUpload(context.Background(), req, file("avatar", "big.png", "image/png", big))
	if !errors.Is(err, ErrUploadTooLarge) || sink.opened != 1 {
		t.Fatalf("declared size: err = %v, opened %d", err, sink.opened)
	}
}

// The type is sniffed from the bytes: text claiming to be a PNG is 415.
// BV: trusting the claimed type instead of sniffedType lets it through.
func TestUploadSniffRefuses(t *testing.T) {
	sink := newMemSink()
	e, req := uploadEngine(t, sink, Options{})
	_, err := e.RunUpload(context.Background(), req, file("avatar", "x.png", "image/png", []byte("<script>alert(1)</script>")))
	if !errors.Is(err, ErrUploadType) {
		t.Fatalf("err = %v", err)
	}
	if sink.opened != 0 {
		t.Fatal("a refused type reached the sink")
	}
}

// An undeclared field is refused, and so is $set on an upload field.
// BV: removing the uploadSpecFor check lets "note" take a file.
func TestUploadUndeclaredField(t *testing.T) {
	sink := newMemSink()
	e, req := uploadEngine(t, sink, Options{})
	for _, field := range []string{"note", "nope"} {
		if _, err := e.RunUpload(context.Background(), req, file(field, "a.png", "image/png", pngHead)); !errors.Is(err, ErrBadValue) {
			t.Fatalf("%s: err = %v", field, err)
		}
	}
	if _, err := e.RunAction(context.Background(), ActionRequest{Handle: req.Handle, Token: req.Token, Action: "$set", Args: []any{"avatar", "forged"}}); !errors.Is(err, ErrBadValue) {
		t.Fatalf("$set on an upload field: %v", err)
	}
	if _, err := e.RunAction(context.Background(), ActionRequest{Handle: req.Handle, Token: req.Token, Action: "$refresh", Sets: []FieldSet{{Path: "avatar", Value: nil}}}); !errors.Is(err, ErrBadValue) {
		t.Fatalf("sets on an upload field: %v", err)
	}
	// $upload is never callable over the JSON endpoint.
	if _, err := e.RunAction(context.Background(), ActionRequest{Handle: req.Handle, Token: req.Token, Action: "$upload", Args: []any{"avatar"}}); !errors.Is(err, ErrUnknownAction) {
		t.Fatalf("$upload as an action: %v", err)
	}
}

func TestUploadMaxFiles(t *testing.T) {
	sink := newMemSink()
	e, req := uploadEngine(t, sink, Options{})
	for i := range 2 {
		res, err := e.RunUpload(context.Background(), req, file("photos", "a.png", "image/png", pngHead))
		if err != nil {
			t.Fatalf("%d: %v", i, err)
		}
		req = next(t, res)
	}
	if _, err := e.RunUpload(context.Background(), req, file("photos", "a.png", "image/png", pngHead)); !errors.Is(err, ErrBadValue) {
		t.Fatalf("third file: %v", err)
	}
	if sink.opened != 2 {
		t.Fatalf("opened %d", sink.opened)
	}
}

// An upload touches its field, so its rules render.
func TestUploadTouchesAndValidates(t *testing.T) {
	sink := removingSink{newMemSink()}
	e, req := uploadEngine(t, sink, Options{})
	res, err := e.RunUpload(context.Background(), req, file("avatar", "me.png", "image/png", pngHead))
	if err != nil {
		t.Fatal(err)
	}
	res, err = e.RunAction(context.Background(), ActionRequest{Handle: next(t, res).Handle, Token: next(t, res).Token, Action: "$removeUpload", Args: []any{"avatar", "f1"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := body(t, res.HTML); got != "||avatar=This field is required.;" {
		t.Fatalf("got %q", got)
	}
	if len(sink.removed) != 1 || sink.removed[0] != "f1" {
		t.Fatalf("removed %v", sink.removed)
	}
}

func TestRemoveUploadNeedsRemover(t *testing.T) {
	sink := newMemSink()
	e, req := uploadEngine(t, sink, Options{})
	res, err := e.RunUpload(context.Background(), req, file("photos", "a.png", "image/png", pngHead))
	if err != nil {
		t.Fatal(err)
	}
	n := next(t, res)
	_, err = e.RunAction(context.Background(), ActionRequest{Handle: n.Handle, Token: n.Token, Action: "$removeUpload", Args: []any{"photos", "f1"}})
	if !errors.Is(err, ErrUnknownAction) {
		t.Fatalf("err = %v", err)
	}
}

// A failed re-render after the sink committed removes the file.
func TestUploadRenderFailureRemoves(t *testing.T) {
	sink := removingSink{newMemSink()}
	fail := errors.New("boom")
	e, req := uploadEngine(t, sink, Options{ActionRequest: func(*http.Request) []RenderOption { return []RenderOption{func(o *renderOptions) { o.err = fail }} }})
	r := httptest.NewRequest(http.MethodPut, "/_brick/upload", bytes.NewReader(pngHead))
	_, err := e.upload(context.Background(), r, req, &uploadBody{field: "avatar", name: "a.png", ctype: "image/png", size: int64(len(pngHead)), body: bytes.NewReader(pngHead)}, renderOptions{})
	if !errors.Is(err, fail) {
		t.Fatalf("err = %v", err)
	}
	if len(sink.removed) != 1 {
		t.Fatalf("committed file not removed: %v", sink.removed)
	}
}

func presignEngine(t *testing.T, clock *time.Time) (*Engine, ActionRequest, *presignSink) {
	t.Helper()
	sink := &presignSink{removingSink: removingSink{newMemSink()}, stored: map[string]UploadRef{}}
	e, req := uploadEngine(t, sink, Options{Clock: func() time.Time { return *clock }})
	return e, req, sink
}

func TestPresignThenUploaded(t *testing.T) {
	now := time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC)
	e, req, sink := presignEngine(t, &now)
	res, err := e.RunUpload(context.Background(), req, UploadFile{Field: "avatar", Name: "me.png", Type: "image/png", Size: 100, Presign: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Presigned == nil || res.Presigned.ID != "p1" || sink.opened != 0 {
		t.Fatalf("presigned %+v", res.Presigned)
	}
	n := next(t, res)
	res, err = e.RunAction(context.Background(), ActionRequest{Handle: n.Handle, Token: n.Token, Action: "$uploaded", Args: []any{"avatar", "p1"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := body(t, res.HTML); !strings.HasPrefix(got, "p1:me.png:image/png:100:") {
		t.Fatalf("got %q", got)
	}
	// The id is spent: confirming it again is refused.
	n = next(t, res)
	if _, err := e.RunAction(context.Background(), ActionRequest{Handle: n.Handle, Token: n.Token, Action: "$uploaded", Args: []any{"avatar", "p1"}}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("second confirm: %v", err)
	}
}

// Only an id issued to this instance, for this field and in time, is
// accepted. BV: skipping the Pending lookup lets the foreign id through.
func TestUploadedRefusesForeignAndExpired(t *testing.T) {
	now := time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC)
	e, req, sink := presignEngine(t, &now)
	sink.stored["other"] = UploadRef{ID: "other", Type: "image/png", Size: 10}
	if _, err := e.RunAction(context.Background(), ActionRequest{Handle: req.Handle, Token: req.Token, Action: "$uploaded", Args: []any{"avatar", "other"}}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("foreign id: %v", err)
	}
	res, err := e.RunUpload(context.Background(), req, UploadFile{Field: "avatar", Name: "me.png", Type: "image/png", Size: 100, Presign: true})
	if err != nil {
		t.Fatal(err)
	}
	n := next(t, res)
	if _, err := e.RunAction(context.Background(), ActionRequest{Handle: n.Handle, Token: n.Token, Action: "$uploaded", Args: []any{"photos", "p1"}}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("id for another field: %v", err)
	}
	now = now.Add(presignTTL + time.Second)
	if _, err := e.RunAction(context.Background(), ActionRequest{Handle: n.Handle, Token: n.Token, Action: "$uploaded", Args: []any{"avatar", "p1"}}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("expired id: %v", err)
	}
}

// Confirm's real size is checked again, and the refused object removed.
func TestUploadedRechecksLimits(t *testing.T) {
	now := time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC)
	e, req, sink := presignEngine(t, &now)
	res, err := e.RunUpload(context.Background(), req, UploadFile{Field: "avatar", Name: "me.png", Type: "image/png", Size: 100, Presign: true})
	if err != nil {
		t.Fatal(err)
	}
	sink.stored["p1"] = UploadRef{ID: "p1", Type: "image/png", Size: 5000}
	n := next(t, res)
	if _, err := e.RunAction(context.Background(), ActionRequest{Handle: n.Handle, Token: n.Token, Action: "$uploaded", Args: []any{"avatar", "p1"}}); !errors.Is(err, ErrUploadTooLarge) {
		t.Fatalf("err = %v", err)
	}
	if len(sink.removed) != 1 {
		t.Fatalf("refused object kept: %v", sink.removed)
	}
}

func TestPendingSurvivesCodec(t *testing.T) {
	at := time.Date(2026, 10, 11, 1, 2, 3, 0, time.UTC)
	inst := &Instance{Token: "t", Path: "p", Name: "n", State: map[string]any{}, Pending: []PendingUpload{{ID: "a", Field: "avatar", Issued: at}}}
	b, err := EncodeInstance(inst)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeInstance(b)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Pending) != 1 || got.Pending[0].ID != "a" || !got.Pending[0].Issued.Equal(at) {
		t.Fatalf("pending = %+v", got.Pending)
	}
}

// The HTTP handler maps the refusals to 413 and 415, and answers a
// success with the envelope.
func TestUploadHandlerStatuses(t *testing.T) {
	sink := newMemSink()
	e, req := uploadEngine(t, sink, Options{})
	put := func(field, ctype string, data []byte) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPut, "/_brick/upload", bytes.NewReader(data))
		r.Header.Set("Brick-Upload", field)
		r.Header.Set("Brick-Handle", req.Handle)
		r.Header.Set("Brick-Token", req.Token)
		r.Header.Set("Brick-Name", url.PathEscape("mé.png"))
		r.Header.Set("Content-Type", ctype)
		r.Header.Set("Accept", EffectsContentType)
		w := httptest.NewRecorder()
		e.UploadHandler().ServeHTTP(w, r)
		return w
	}
	if w := put("avatar", "image/png", make([]byte, 2000)); w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("too large: %d %s", w.Code, w.Body)
	}
	if w := put("avatar", "image/png", []byte("hello")); w.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("wrong type: %d %s", w.Code, w.Body)
	}
	if w := put("note", "image/png", pngHead); w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("undeclared: %d %s", w.Code, w.Body)
	}
	w := put("avatar", "image/png", pngHead)
	if w.Code != http.StatusOK {
		t.Fatalf("ok: %d %s", w.Code, w.Body)
	}
	var env actionEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil || env.Version != 1 || !strings.Contains(env.HTML, ":mé.png:") {
		t.Fatalf("envelope %v %+v", err, env)
	}
	r := httptest.NewRequest(http.MethodGet, "/_brick/upload", nil)
	w = httptest.NewRecorder()
	e.UploadHandler().ServeHTTP(w, r)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET: %d", w.Code)
	}
}

func TestUploadCompileErrors(t *testing.T) {
	comp := func(fm, body string) string {
		return "---\nstate:\n  avatar: null\n  photos: []\n  note: \"\"\n" + fm + "---\n<div>" + body + "</div>"
	}
	sink := newMemSink()
	cases := []struct {
		name, src string
		sink      UploadSink
		want      string
	}{
		{"no sink", comp("brick:\n  uploads:\n    avatar: {}\n", ""), nil, "no UploadSink configured"},
		{"undeclared", comp("brick:\n  uploads:\n    missing: {}\n", ""), sink, "missing is not a field"},
		{"scalar field", comp("brick:\n  uploads:\n    note: {}\n", ""), sink, "an upload field is null"},
		{"max_files on one", comp("brick:\n  uploads:\n    avatar: {max_files: 3}\n", ""), sink, "max_files is 3"},
		{"bad size", comp("brick:\n  uploads:\n    avatar: {max_size: lots}\n", ""), sink, "is not a size"},
		{"unknown key", comp("brick:\n  uploads:\n    avatar: {maxsize: 2MB}\n", ""), sink, `unknown key "maxsize"`},
		{"bad accept", comp("brick:\n  uploads:\n    avatar: {accept: [png]}\n", ""), sink, "not a media type"},
		{"attr undeclared", comp("brick:\n  uploads:\n    avatar: {}\n", `<input type="file" brick:upload="photos">`), sink, `"photos" is not an upload field`},
		{"remove undeclared", comp("brick:\n  uploads:\n    avatar: {}\n", `<button brick:click='$removeUpload("note", "x")'>x</button>`), sink, `"note" is not an upload field`},
		{"cancel args", comp("brick:\n  uploads:\n    avatar: {}\n", `<button brick:click='$cancelUpload()'>x</button>`), sink, "takes an upload field"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := newEngineErr(Options{FS: theme("components/U.html", c.src, "content/a.html", `<U />`), States: NewMemoryStates(0, 0), Uploads: c.sink}, "content/a.html")
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want %q", err, c.want)
			}
		})
	}
}

func TestParseSize(t *testing.T) {
	for in, want := range map[any]int64{"2MB": 2 << 20, "500kb": 500 << 10, "1 GB": 1 << 30, 1024: 1024, "7": 7, "3b": 3} {
		got, err := parseSize(in)
		if err != nil || got != want {
			t.Errorf("parseSize(%v) = %d, %v; want %d", in, got, err, want)
		}
	}
	for _, in := range []any{"", "0", "2TB", -1, "1.5MB"} {
		if _, err := parseSize(in); err == nil {
			t.Errorf("parseSize(%v) passed", in)
		}
	}
}

func TestSniffedType(t *testing.T) {
	cases := []struct{ head, claim, want string }{
		{string(pngHead), "image/jpeg", "image/png"},
		{"a,b\n1,2\n", "text/csv", "text/csv"},
		{"<html><body>x", "text/plain", "text/html; charset=utf-8"},
		{"plain words", "text/html", "text/plain"},
		{"{\"a\":1}", "application/json", "application/json"},
	}
	for _, c := range cases {
		if got := sniffedType([]byte(c.head), c.claim); got != mediaBase(c.want) {
			t.Errorf("sniffedType(%q, %q) = %q, want %q", c.head, c.claim, got, mediaBase(c.want))
		}
	}
	if !matchAccept([]string{"image/*"}, "image/webp") || matchAccept([]string{"image/png"}, "image/gif") || !matchAccept(nil, "x/y") {
		t.Error("matchAccept")
	}
}
