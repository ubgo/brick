package main

import (
	"bytes"
	"fmt"
	"html"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"regexp"
	"strings"
	"testing"
)

// avatarStateRE finds the avatar demo's handle and token.
var avatarStateRE = regexp.MustCompile(`data-brick-state="([^"]+)" data-brick-token="([^"]+)"[^>]*data-brick-uploads=`)

// The upload page renders the avatar demo with its
// limits stamped for the runtime, and a PNG sent to the upload endpoint
// lands in state with the disk sink's URL, served back by the uploads
// handler.
func TestUploadPage(t *testing.T) {
	_, h := showcase(t)
	const url = "/components/upload/"
	code, body := get(t, h, url)
	if code != http.StatusOK {
		t.Fatalf("%s: status %d", url, code)
	}
	mustContain(t, url, body, `brick:upload="avatar"`, `brick:upload-progress="avatar"`, `id="au-empty"`)
	m := avatarStateRE.FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("%s: no upload root", url)
	}
	png := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 24)...)
	req := httptest.NewRequest(http.MethodPut, "/_brick/upload", bytes.NewReader(png))
	req.Header.Set("Brick-Upload", "avatar")
	req.Header.Set("Brick-Handle", m[1])
	req.Header.Set("Brick-Token", m[2])
	req.Header.Set("Brick-Name", "me.png")
	req.Header.Set("Content-Type", "image/png")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("upload: %d %s", rec.Code, rec.Body)
	}
	src := regexp.MustCompile(`id="au-preview" src="(/uploads/[0-9a-f]+)"`).FindStringSubmatch(rec.Body.String())
	if src == nil || !strings.Contains(rec.Body.String(), `id="au-name">me.png<`) {
		t.Fatalf("no preview in %s", rec.Body)
	}
	files, err := withUploads(http.NotFoundHandler(), "")
	if err != nil {
		t.Fatal(err)
	}
	rec = httptest.NewRecorder()
	files.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, src[1], nil))
	if rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), png) || rec.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("preview: %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	// A text file claiming to be a PNG is refused by the sniff.
	req = httptest.NewRequest(http.MethodPut, "/_brick/upload", strings.NewReader("not a picture"))
	req.Header.Set("Brick-Upload", "avatar")
	req.Header.Set("Brick-Handle", m[1])
	req.Header.Set("Brick-Token", m[2])
	req.Header.Set("Content-Type", "image/png")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("sniff: %d %s", rec.Code, rec.Body)
	}
}

// njuFormRE is the no-JS upload demo's form as rendered.
var njuFormRE = regexp.MustCompile(`(?s)<form [^>]*id="nju-form"[^>]*>.*?</form>`)

// njuPost posts the no-JS upload demo's form as a browser with scripts off
// sends it: multipart, the rendered fields in document order, then name
// and the file part under the file input's own name.
func njuPost(t *testing.T, h http.Handler, form, name, file, data string) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for _, m := range regexp.MustCompile(`<input type="hidden" name="([^"]+)" value="([^"]*)">`).FindAllStringSubmatch(form, -1) {
		_ = mw.WriteField(m[1], html.UnescapeString(m[2]))
	}
	_ = mw.WriteField("name", name)
	fileName := regexp.MustCompile(`<input id="nju-input" [^>]*name="([^"]+)"`).FindStringSubmatch(form)
	if fileName == nil {
		t.Fatalf("no named file input in %s", form)
	}
	hdr := textproto.MIMEHeader{}
	hdr.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="%s"`, fileName[1], file))
	hdr.Set("Content-Type", "image/png")
	w, err := mw.CreatePart(hdr)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.WriteString(w, data)
	_ = mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/components/upload/", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// The no-JS upload demo works with scripts off: its form is
// multipart, a PNG posted with a name lands in the disk sink and the page
// the 303 leads to shows it, and a text file named .png is refused with a
// 422 that shows the reason by the field.
func TestNoJSUploadDemo(t *testing.T) {
	_, h := showcase(t)
	_, body := get(t, h, "/components/upload/")
	form := njuFormRE.FindString(body)
	if form == "" || !strings.Contains(form, `enctype="multipart/form-data"`) {
		t.Fatalf("no multipart no-JS form: %q", form)
	}
	png := "\x89PNG\r\n\x1a\n" + strings.Repeat("\x00", 24)
	rec := njuPost(t, h, form, "Ada", "me.png", png)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("post: %d %s", rec.Code, rec.Body)
	}
	req := httptest.NewRequest(http.MethodGet, rec.Header().Get("Location"), nil)
	for _, c := range rec.Result().Cookies() {
		req.AddCookie(c)
	}
	after := httptest.NewRecorder()
	h.ServeHTTP(after, req)
	mustContain(t, "after the post", after.Body.String(), `id="nju-preview" src="/uploads/`, `me.png · image/png`, `Saved 1× as <b>Ada</b>`)

	rec = njuPost(t, h, form, "Ada", "fake.png", "not a picture")
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("fake png: %d", rec.Code)
	}
	mustContain(t, "the refusal", rec.Body.String(), `id="nju-err-avatar"`)
}
