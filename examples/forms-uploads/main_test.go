package main

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/ubgo/brick"
)

// instanceRE reads the root's handle and token, as runtime.js does before
// it sends a file.
var instanceRE = regexp.MustCompile(`data-brick-state="([^"]+)" data-brick-token="([0-9a-f]+)"`)

// avatarRE reads the stored file's URL out of the re-rendered root.
var avatarRE = regexp.MustCompile(`<img src="(/uploads/[0-9a-f]+)" alt="([^"]+)"`)

// pngOf is a real w×h PNG, so the sniffed type and the IHDR width are
// what a browser would send.
func pngOf(t *testing.T, w, h int) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewGray(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// fixture is a running app and the instance its first render created.
type fixture struct {
	t      *testing.T
	h      http.Handler
	handle string
	token  string
}

func newFixture(t *testing.T, presign bool) *fixture {
	t.Helper()
	a, err := newApp("theme", t.TempDir(), presign)
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{t: t, h: a.handler()}
	page := f.do(httptest.NewRequest(http.MethodGet, "/profile", nil)).Body.String()
	m := instanceRE.FindStringSubmatch(page)
	if m == nil {
		t.Fatalf("no stateful root:\n%s", page)
	}
	f.handle, f.token = m[1], m[2]
	return f
}

func (f *fixture) do(r *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	f.h.ServeHTTP(rec, r)
	return rec
}

// put sends one file the way runtime.js does: PUT to /_brick/upload with
// the instance in Brick-* headers and the bytes as the body.
// ds:def id=fixture-put-f88ktnau
func (f *fixture) put(field, name string, body []byte) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPut, "/_brick/upload", bytes.NewReader(body))
	r.Header.Set("Brick-Upload", field)
	r.Header.Set("Brick-Handle", f.handle)
	r.Header.Set("Brick-Token", f.token)
	r.Header.Set("Brick-Name", name)
	r.Header.Set("Content-Type", "image/png")
	return f.do(r)
}

// action sends one JSON action.
func (f *fixture) action(name string, args ...any) *httptest.ResponseRecorder {
	b, err := json.Marshal(map[string]any{"handle": f.handle, "token": f.token, "action": name, "args": args})
	if err != nil {
		f.t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, "/_brick/action", bytes.NewReader(b))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Brick-Action", "1")
	return f.do(r)
}

// A file streams into the disk sink and its ref lands in state; the sink
// serves it back; every refusal names its reason; $removeUpload deletes
// the bytes as well as the ref.
func TestUploadThroughTheEngine(t *testing.T) {
	f := newFixture(t, false)
	me := pngOf(t, 8, 8)
	rec := f.put("avatar", "me.png", me)
	m := avatarRE.FindStringSubmatch(rec.Body.String())
	if rec.Code != http.StatusOK || m == nil || m[2] != "me.png" {
		t.Fatalf("upload: %d\n%s", rec.Code, rec.Body)
	}
	served := f.do(httptest.NewRequest(http.MethodGet, m[1], nil))
	if served.Code != http.StatusOK || !bytes.Equal(served.Body.Bytes(), me) || served.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("served: %d %q", served.Code, served.Header().Get("Content-Type"))
	}

	for _, c := range []struct {
		field, name string
		body        []byte
		status      int
		msg         string
	}{
		{"avatar", "notes.png", []byte("hello, this is text"), http.StatusUnsupportedMediaType, "brick: this type of file is not accepted: avatar takes image/png, image/jpeg, not text/plain"},
		{"avatar", "wide.png", pngOf(t, 5000, 1), http.StatusUnprocessableEntity, "The image is 5000 pixels wide; the most is 4096."},
		{"avatar", "huge.png", append(pngOf(t, 8, 8), make([]byte, 3<<20)...), http.StatusRequestEntityTooLarge, "brick: the file is too large: avatar takes files up to 2097152 bytes"},
	} {
		rec := f.put(c.field, c.name, c.body)
		if rec.Code != c.status || strings.TrimSpace(rec.Body.String()) != c.msg {
			t.Fatalf("%s: %d %q", c.name, rec.Code, rec.Body)
		}
	}

	for _, name := range []string{"a.png", "b.png"} {
		if rec := f.put("photos", name, pngOf(t, 2, 2)); rec.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", name, rec.Code, rec.Body)
		}
	}
	rec = f.put("photos", "c.png", pngOf(t, 2, 2))
	if rec.Code != http.StatusUnprocessableEntity || strings.TrimSpace(rec.Body.String()) != "brick: field value refused: photos holds at most 2 files" {
		t.Fatalf("third photo: %d %q", rec.Code, rec.Body)
	}

	id := strings.TrimPrefix(m[1], uploadsPrefix)
	rec = f.action("$removeUpload", "avatar", id)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "<span>No photo</span>") || !strings.Contains(rec.Body.String(), "<ul><li>a.png (76 bytes)</li><li>b.png (76 bytes)</li></ul>") {
		t.Fatalf("remove: %d\n%s", rec.Code, rec.Body)
	}
	if gone := f.do(httptest.NewRequest(http.MethodGet, m[1], nil)); gone.Code != http.StatusNotFound {
		t.Fatalf("removed file still served: %d", gone.Code)
	}
}

// formPost is a multipart post of the rewritten form, fields in page
// order (the instance fields first, as the rewrite places them), with an
// optional avatar file.
// ds:def id=fixture-formpost-e9ssvfr9
func (f *fixture) formPost(name string, avatar []byte) *httptest.ResponseRecorder {
	// The rewritten form declares enctype="multipart/form-data", so a
	// browser with JavaScript off sends the file parts in this shape.
	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	for _, kv := range [][2]string{
		{"brick:state", f.handle}, {"brick:token", f.token}, {"brick:action", "save"},
		{"brick:uploads", `{"brick:upload:avatar":"avatar","brick:upload:photos":"photos"}`},
		{"name", name},
	} {
		if err := w.WriteField(kv[0], kv[1]); err != nil {
			f.t.Fatal(err)
		}
	}
	if avatar != nil {
		part, err := w.CreateFormFile("brick:upload:avatar", "me.png")
		if err != nil {
			f.t.Fatal(err)
		}
		_, _ = part.Write(avatar)
	}
	if err := w.Close(); err != nil {
		f.t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, "/profile", &b)
	r.Header.Set("Content-Type", w.FormDataContentType())
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	return f.do(r)
}

// With scripts off the form posts multipart: the file goes through the
// same $upload path before save runs, so a refused file never runs the
// action and comes back as a 422 page with the reason on the field.
// ds:def id=testuploadwithoutjavascript-jhgkqs79
func TestUploadWithoutJavaScript(t *testing.T) {
	f := newFixture(t, false)
	rec := f.formPost("Ada", pngOf(t, 5000, 1))
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), `<p class="error">The image is 5000 pixels wide; the most is 4096.</p>`) || strings.Contains(rec.Body.String(), "Saved: Ada") {
		t.Fatalf("wide avatar: %d\n%s", rec.Code, rec.Body)
	}
	rec = f.formPost("Ada", pngOf(t, 8, 8))
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/profile" {
		t.Fatalf("good post: %d %s", rec.Code, rec.Body)
	}
	r := httptest.NewRequest(http.MethodGet, "/profile", nil)
	for _, c := range rec.Result().Cookies() {
		r.AddCookie(c)
	}
	page := f.do(r).Body.String()
	if !strings.Contains(page, "<p>Saved: Ada</p>") || avatarRE.FindStringSubmatch(page) == nil {
		t.Fatalf("after the redirect:\n%s", page)
	}
}

// A presigning sink: the browser asks where to send the file (POST with
// Brick-Size), PUTs the bytes straight to the store, then confirms the id
// with $uploaded. The engine accepts only an id it issued to this
// instance.
func TestPresignedUpload(t *testing.T) {
	f := newFixture(t, true)
	me := pngOf(t, 8, 8)
	r := httptest.NewRequest(http.MethodPost, "/_brick/upload", nil)
	r.Header.Set("Brick-Upload", "avatar")
	r.Header.Set("Brick-Handle", f.handle)
	r.Header.Set("Brick-Token", f.token)
	r.Header.Set("Brick-Name", "me.png")
	r.Header.Set("Brick-Size", strconv.Itoa(len(me)))
	r.Header.Set("Content-Type", "image/png")
	rec := f.do(r)
	var env struct {
		Upload struct {
			URL     string            `json:"url"`
			Method  string            `json:"method"`
			Headers map[string]string `json:"headers"`
			ID      string            `json:"id"`
		} `json:"upload"`
	}
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &env) != nil || env.Upload.Method != http.MethodPut || env.Upload.URL != uploadsPrefix+env.Upload.ID {
		t.Fatalf("presign: %d %s", rec.Code, rec.Body)
	}

	if rec := f.action("$uploaded", "avatar", "0123456789abcdef0123456789abcdef"); rec.Code != http.StatusForbidden {
		t.Fatalf("a foreign id: %d %s", rec.Code, rec.Body)
	}

	store := httptest.NewRequest(env.Upload.Method, env.Upload.URL, bytes.NewReader(me))
	for k, v := range env.Upload.Headers {
		store.Header.Set(k, v)
	}
	if rec := f.do(store); rec.Code != http.StatusOK {
		t.Fatalf("store PUT: %d %s", rec.Code, rec.Body)
	}
	rec = f.action("$uploaded", "avatar", env.Upload.ID)
	m := avatarRE.FindStringSubmatch(rec.Body.String())
	if rec.Code != http.StatusOK || m == nil || m[1] != env.Upload.URL || m[2] != "me.png" {
		t.Fatalf("confirm: %d\n%s", rec.Code, rec.Body)
	}
	served := f.do(httptest.NewRequest(http.MethodGet, m[1], nil))
	if got, _ := io.ReadAll(served.Body); !bytes.Equal(got, me) {
		t.Fatal("the store does not serve what was PUT")
	}

	// With scripts off there is no browser to PUT anything: the form's
	// file streams through the engine into the sink's Open instead.
	nojs := newFixture(t, true)
	rec = nojs.formPost("Ada", me)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("no-JS post to a presigning sink: %d %s", rec.Code, rec.Body)
	}
}

// A component that declares uploads: on an engine with no sink fails to
// compile, so a file is never accepted and silently dropped.
func TestUploadsNeedASink(t *testing.T) {
	fsys := fstest.MapFS{
		"content/index.html": {Data: []byte(`<F />`)},
		"components/F.html":  {Data: []byte("---\nstate:\n  avatar: null\nbrick:\n  uploads:\n    avatar: {max_size: 2MB}\n---\n<p></p>\n")},
	}
	eng, err := brick.New(brick.Options{FS: fsys, States: brick.NewMemoryStates(0, 0)})
	if err != nil {
		t.Fatal(err)
	}
	_, err = eng.Render(context.Background(), "content/index.html", nil)
	const want = "components/F.html:5: brick: uploads: no UploadSink configured (Options.Uploads); set one, such as upload/disk, so files are never silently dropped"
	if err == nil || err.Error() != want {
		t.Fatalf("got %v", err)
	}
}

// brick.upload and the upload events are browser code: Go can check that
// the page carries the drop zone and that the Profile module holding the
// calls is served. Dropping a file, and the status line moving as bytes
// go, only a browser shows.
func TestDropZoneClientServed(t *testing.T) {
	f := newFixture(t, false)
	page := f.do(httptest.NewRequest(http.MethodGet, "/profile", nil)).Body.String()
	for _, want := range []string{`<div data-photo-drop>Drop photos here</div>`, `<p data-upload-status></p>`, `enctype="multipart/form-data"`} {
		if !strings.Contains(page, want) {
			t.Fatalf("page lacks %s:\n%s", want, page)
		}
	}
	m := regexp.MustCompile(`src="(/_brick/c/Profile\.[0-9a-f]+\.js)"`).FindStringSubmatch(page)
	if m == nil {
		t.Fatalf("no Profile module on the page:\n%s", page)
	}
	rec := f.do(httptest.NewRequest(http.MethodGet, m[1], nil))
	for _, want := range []string{`brick.upload(zone, "photos"`, `brick.on("upload-progress"`, `brick.on("upload-done"`, `brick.on("upload-error"`} {
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), want) {
			t.Fatalf("module lacks %s (%d):\n%s", want, rec.Code, rec.Body)
		}
	}
}
