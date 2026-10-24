# Uploads: files into state, bytes into your storage

## What it is and when you want it

A profile form needs an avatar, but the action endpoint takes JSON and scripts never touch the filesystem. Name the state fields that take files under `brick: {uploads:}`, put `brick:upload` on a file input, and give the engine an `UploadSink`, the place the bytes go. Each file streams to `/_brick/upload` as the pseudo-action `$upload`, through your action middleware and the declared limits; the sink stores it; a small ref (`id`, `name`, `type`, `size`, `url`) lands in state. The same form uploads with JavaScript off, and a sink that presigns lets the browser send the bytes straight to storage.

## Before you start

- A stateful component with a form ([Deferred fields](deferred-fields.md)), and for the no-JavaScript path, the POST route from [Forms without JavaScript](without-javascript.md).
- A sink: `upload/disk` (the reference, one directory), `contrib/upload-s3`, or your own `brick.UploadSink`. The example uses `upload/disk` in a temporary directory, and an in-memory presigning store.
- The example: [examples/forms-uploads](../../../examples/forms-uploads/).

## Step 1 — Declare the upload fields and the inputs

`null` is one file (a new upload replaces it); `[]` is several (`max_files`, default 10). `accept` lists media types (`image/*` works) and `max_size` is bytes or `KB`/`MB`/`GB`, 10 MB by default.

The `<brick:client>` block, the drop zone and the status line at the end of the file belong to Step 7.

`theme/components/Profile.html`

<!-- snippet: examples/forms-uploads/theme/components/Profile.html -->
```html
---
state:
  name: ""
  avatar: null
  photos: []
brick:
  uploads:
    avatar: {accept: [image/png, image/jpeg], max_size: 2MB}
    photos: {accept: [image/*], max_files: 2}
---
<brick:script>
def save(state, props, form):
    name = str(form.get("name", "")).strip()
    if not name:
        return [state, {"name": "Enter a name."}]
    return dict(state, name = name)
</brick:script>
<brick:client>
// A drop zone for the photos field. brick.upload(el, field, files) sends
// the files as a brick:upload input would: the same limits, the same
// events, one queue with the component's other actions. It resolves when
// every accepted file is done; a refused file emits upload-error and is
// not sent.
document.addEventListener("dragover", (event) => {
  if (event.target.closest("[data-photo-drop]")) event.preventDefault();
});
document.addEventListener("drop", (event) => {
  const zone = event.target.closest("[data-photo-drop]");
  if (!zone) return;
  event.preventDefault();
  brick.upload(zone, "photos", event.dataTransfer.files).catch((err) => console.error(err));
});

// The runtime's upload events, for a status line of your own.
const status = () => document.querySelector("[data-upload-status]");
brick.on("upload-progress", (d) => {
  status().textContent = d.name + ": " + Math.round((100 * d.loaded) / d.total) + "%";
});
brick.on("upload-done", (d) => { status().textContent = d.name + " uploaded"; });
brick.on("upload-error", (d) => { status().textContent = d.name + ": " + d.message; });
</brick:client>
<form brick:submit="save">
  {{ if .State.name }}<p>Saved: {{ .State.name }}</p>{{ end }}
  <input name="name" value="{{ .State.name }}">
  {{ with .Errors.name }}<p class="error">{{ . }}</p>{{ end }}
  {{ with .State.avatar }}
    <img src="{{ .url }}" alt="{{ .name }}" width="72" height="72">
    <button type="button" brick:click='$removeUpload("avatar", "{{ .id }}")'>Remove</button>
  {{ else }}
    <span>No photo</span>
  {{ end }}
  <input type="file" accept="image/png,image/jpeg" brick:upload="avatar">
  <progress brick:upload-progress="avatar" value="0" max="1"></progress>
  <button type="button" brick:click='$cancelUpload("avatar")'>Cancel</button>
  {{ with .Errors.avatar }}<p class="error">{{ . }}</p>{{ end }}
  <ul>{{ range .State.photos }}<li>{{ .name }} ({{ .size }} bytes)</li>{{ end }}</ul>
  <input type="file" multiple accept="image/*" brick:upload="photos">
  <div data-photo-drop>Drop photos here</div>
  <p data-upload-status></p>
  <button>Save</button>
</form>
```

- `<progress brick:upload-progress="avatar">` fills while the file is sent, and the input carries `data-uploading`.
- `$removeUpload("avatar", id)` asks the sink to delete the file and clears the ref; `$cancelUpload("avatar")` aborts the browser's request.
- The rendered root carries the limits as `data-brick-uploads`, so the browser refuses a too-large file before sending it; the server checks again anyway.
- An action reads `state["avatar"]["id"]` like any field. Keeping the file (moving it out of the temporary area, attaching it to a record) is a capability your program grants through `ScriptGlobals`.

`theme/content/profile.html`

<!-- snippet: examples/forms-uploads/theme/content/profile.html -->
```html
<!doctype html>
<html><body>
<h1>Your profile</h1>
<Profile />
{{ assets }}
</body></html>
```

## Step 2 — Give the engine a sink and your own check

`Options.Uploads` takes the sink; `Options.UploadCheck` sees each file's sniffed type and first `brick.UploadCheckBytes` bytes after the `uploads:` limits and before the sink stores anything. Return `brick.RefuseUpload(message)` to refuse with a message the user sees on the field.

The disk sink comes from the `upload/disk` package; the imports of the program:

<!-- snippet: examples/forms-uploads/main.go lines=12-25 -->
```go
import (
	"context"
	"encoding/binary"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/ubgo/brick"
	assetshttp "github.com/ubgo/brick/contrib/assets-http"
	scriptstarlark "github.com/ubgo/brick/contrib/script-starlark"
	"github.com/ubgo/brick/upload/disk"
)
```

`main.go`

<!-- ds:block id=uploadsprefix-h7xzu2h7 title="examples/forms-uploads/main.go" -->
**examples/forms-uploads/main.go** · [`examples/forms-uploads/main.go:33`](../../../examples/forms-uploads/main.go#L33-L33)

```go
"/uploads/"
```
<!-- /ds:block hash=7291c6 -->

<!-- ds:block id=maxpngwidth-pthmetbn title="examples/forms-uploads/main.go" -->
**examples/forms-uploads/main.go** · [`examples/forms-uploads/main.go:38`](../../../examples/forms-uploads/main.go#L38-L38)

```go
4096
```
<!-- /ds:block hash=8b926d -->

<!-- ds:block id=pngheader-9davw5f9 title="examples/forms-uploads/main.go" -->
**examples/forms-uploads/main.go** · [`examples/forms-uploads/main.go:43`](../../../examples/forms-uploads/main.go#L43-L43)

```go
"\x89PNG\r\n\x1a\n\x00\x00\x00\x0dIHDR"
```
<!-- /ds:block hash=62c75d -->

<!-- ds:block id=checkpngwidth-nffedgqa title="examples/forms-uploads/main.go" -->
**examples/forms-uploads/main.go** · [`examples/forms-uploads/main.go:51-59`](../../../examples/forms-uploads/main.go#L51-L59)

```go
func checkPNGWidth(_ context.Context, m brick.UploadMeta, head []byte) error {
	if m.Type != "image/png" || len(head) < len(pngHeader)+4 || string(head[:len(pngHeader)]) != pngHeader {
		return nil
	}
	if w := binary.BigEndian.Uint32(head[len(pngHeader):]); w > maxPNGWidth {
		return brick.RefuseUpload(fmt.Sprintf("The image is %d pixels wide; the most is %d.", w, maxPNGWidth))
	}
	return nil
}
```
<!-- /ds:block hash=5717cf -->

<!-- ds:block id=app-7p5jxeq6 title="examples/forms-uploads/main.go" -->
**examples/forms-uploads/main.go** · [`examples/forms-uploads/main.go:63-68`](../../../examples/forms-uploads/main.go#L63-L68)

```go
type app struct {
	eng  *brick.Engine
	sink brick.UploadSink
	// files serves the stored bytes back under uploadsPrefix.
	files http.Handler
}
```
<!-- /ds:block hash=aa8055 -->

<!-- ds:block id=newapp-3sd49eb4 title="examples/forms-uploads/main.go" -->
**examples/forms-uploads/main.go** · [`examples/forms-uploads/main.go:74-105`](../../../examples/forms-uploads/main.go#L74-L105)

```go
func newApp(dir, uploadDir string, presign bool) (*app, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	sc, err := scriptstarlark.New(scriptstarlark.Options{})
	if err != nil {
		return nil, err
	}
	a := &app{}
	if presign {
		ps := newPresignStore(uploadsPrefix)
		a.sink, a.files = ps, ps
	} else {
		d, err := disk.New(disk.Options{Dir: uploadDir, URLPrefix: uploadsPrefix})
		if err != nil {
			return nil, err
		}
		a.sink, a.files = d, d.Handler()
	}
	a.eng, err = brick.New(brick.Options{
		FS:          root.FS(),
		Scripting:   sc,
		States:      brick.NewMemoryStates(0, 0),
		Uploads:     a.sink,
		UploadCheck: checkPNGWidth,
	})
	if err != nil {
		return nil, err
	}
	return a, nil
}
```
<!-- /ds:block hash=da7d01 -->

## Step 3 — Mount the upload endpoint and serve the files

The upload endpoint sits beside the action endpoint; the sink's handler serves the stored bytes at the prefix written into each ref's `url`. The POST route is the one from [Forms without JavaScript](without-javascript.md).

<!-- ds:block id=app-handler-34zqazvz title="examples/forms-uploads/main.go" -->
**examples/forms-uploads/main.go** · [`examples/forms-uploads/main.go:111-143`](../../../examples/forms-uploads/main.go#L111-L143)

```go
func (a *app) handler() http.Handler {
	eng := a.eng
	mux := http.NewServeMux()
	mux.Handle(brick.DefaultAssetBase+brick.ActionEndpoint, eng.ActionHandler())
	mux.Handle(brick.DefaultAssetBase+brick.UploadEndpoint, eng.UploadHandler())
	mux.Handle(brick.DefaultAssetBase, assetshttp.Handler(brick.DefaultAssetBase, func() assetshttp.Source { return eng }))
	mux.Handle(uploadsPrefix, http.StripPrefix(uploadsPrefix, a.files))
	mux.HandleFunc("GET /profile", func(w http.ResponseWriter, r *http.Request) {
		a.write(w, r, http.StatusOK, eng.FormResumeOption(w, r))
	})
	mux.HandleFunc("POST /profile", func(w http.ResponseWriter, r *http.Request) {
		if !eng.IsFormPost(r) {
			http.Error(w, "not a form post", http.StatusBadRequest)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		out, err := eng.FormPost(r)
		if err != nil {
			eng.FormError(w, r, err)
			return
		}
		if !out.Failed && (out.Resume.Handle == "" || eng.SetFormResume(w, r, out.Resume)) {
			http.Redirect(w, r, out.Location, http.StatusSeeOther)
			return
		}
		status := http.StatusOK
		if out.Failed {
			status = http.StatusUnprocessableEntity
		}
		a.write(w, r, status, brick.WithFormResume(out.Resume))
	})
	return mux
}
```
<!-- /ds:block hash=1948a1 -->

<!-- ds:block id=run-grwa57pb title="examples/forms-uploads/main.go" -->
**examples/forms-uploads/main.go** · [`examples/forms-uploads/main.go:177-189`](../../../examples/forms-uploads/main.go#L177-L189)

```go
func run(addr, dir string, presign bool) error {
	tmp, err := os.MkdirTemp("", "brick-uploads-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	a, err := newApp(dir, tmp, presign)
	if err != nil {
		return err
	}
	log.Printf("serving on %s, files in %s", addr, tmp)
	return http.ListenAndServe(addr, a.handler())
}
```
<!-- /ds:block hash=cbd067 -->

The disk sink serves images inline and everything else as a download, and its `Sweep` removes files older than its TTL; it starts no goroutine, so call `Sweep` on your own timer.

## Step 4 — Send a file the way the runtime does

The runtime sends one `PUT /_brick/upload` per file, the instance in `Brick-*` headers, the bytes as the body.

`main_test.go`

<!-- ds:block id=fixture-put-f88ktnau title="examples/forms-uploads/main_test.go" -->
**examples/forms-uploads/main_test.go** · [`examples/forms-uploads/main_test.go:73-81`](../../../examples/forms-uploads/main_test.go#L73-L81)

```go
func (f *fixture) put(field, name string, body []byte) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPut, "/_brick/upload", bytes.NewReader(body))
	r.Header.Set("Brick-Upload", field)
	r.Header.Set("Brick-Handle", f.handle)
	r.Header.Set("Brick-Token", f.token)
	r.Header.Set("Brick-Name", name)
	r.Header.Set("Content-Type", "image/png")
	return f.do(r)
}
```
<!-- /ds:block hash=8fe3a3 -->

<!-- snippet: examples/forms-uploads/main_test.go lines=95-145 -->
```go
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
```

## Step 5 — Upload with JavaScript off

The rewrite adds `enctype="multipart/form-data"` and puts the instance fields first; the browser sends the file with the form, and `FormPost` streams each file part through the same `$upload` path before `save` runs, so the action already sees `.State.avatar` filled. A refused file does not run the action: the page comes back `422` with the reason in `.Errors.avatar`.

<!-- ds:block id=fixture-formpost-e9ssvfr9 title="examples/forms-uploads/main_test.go" -->
**examples/forms-uploads/main_test.go** · [`examples/forms-uploads/main_test.go:151-179`](../../../examples/forms-uploads/main_test.go#L151-L179)

```go
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
```
<!-- /ds:block hash=a664e0 -->

<!-- ds:block id=testuploadwithoutjavascript-jhgkqs79 title="examples/forms-uploads/main_test.go" -->
**examples/forms-uploads/main_test.go** · [`examples/forms-uploads/main_test.go:185-203`](../../../examples/forms-uploads/main_test.go#L185-L203)

```go
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
```
<!-- /ds:block hash=7561c7 -->

## Step 6 — Let the browser upload straight to storage

A sink that also implements `brick.UploadPresigner` (`Presign` and `Confirm`) is used for direct uploads: the runtime asks for a target with `POST /_brick/upload` and `Brick-Size`, PUTs the bytes to the URL it gets, and confirms with `$uploaded(field, id)`. The engine accepts only an id it issued to that instance within the hour, and checks the limits again on what `Confirm` reads from storage. This in-memory store stands in for S3; `contrib/upload-s3` is the real one.

`presign.go`

<!-- ds:block id=presignstore-79nd9e2c title="examples/forms-uploads/presign.go" -->
**examples/forms-uploads/presign.go** · [`examples/forms-uploads/presign.go:40-47`](../../../examples/forms-uploads/presign.go#L40-L47)

```go
type presignStore struct {
	// Presign and Confirm (below) make this a brick.UploadPresigner; Open
	// keeps the proxied path every UploadSink has.
	prefix string
	mu     sync.Mutex
	issued map[string]string // id → the file name Presign was asked for
	stored map[string]object
}
```
<!-- /ds:block hash=5f2e88 -->

<!-- ds:block id=presignstore-open-p5uyd7aq title="examples/forms-uploads/presign.go" -->
**examples/forms-uploads/presign.go** · [`examples/forms-uploads/presign.go:59-65`](../../../examples/forms-uploads/presign.go#L59-L65)

```go
func (s *presignStore) Open(_ context.Context, m brick.UploadMeta) (brick.UploadWriter, error) {
	id, err := newID()
	if err != nil {
		return nil, err
	}
	return &memWriter{s: s, id: id, meta: m}, nil
}
```
<!-- /ds:block hash=95bd26 -->

<!-- ds:block id=presignstore-presign-89gnczqg title="examples/forms-uploads/presign.go" -->
**examples/forms-uploads/presign.go** · [`examples/forms-uploads/presign.go:104-113`](../../../examples/forms-uploads/presign.go#L104-L113)

```go
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
```
<!-- /ds:block hash=53a3de -->

<!-- ds:block id=presignstore-confirm-n7s3whns title="examples/forms-uploads/presign.go" -->
**examples/forms-uploads/presign.go** · [`examples/forms-uploads/presign.go:119-127`](../../../examples/forms-uploads/presign.go#L119-L127)

```go
func (s *presignStore) Confirm(_ context.Context, id string) (brick.UploadRef, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	o, ok := s.stored[id]
	if !ok {
		return brick.UploadRef{}, errors.New("presignStore: nothing was uploaded under that id")
	}
	return brick.UploadRef{ID: id, Name: o.name, Type: o.ctype, Size: int64(len(o.data)), URL: s.prefix + id}, nil
}
```
<!-- /ds:block hash=7942bb -->

<!-- ds:block id=presignstore-remove-32zmy2vq title="examples/forms-uploads/presign.go" -->
**examples/forms-uploads/presign.go** · [`examples/forms-uploads/presign.go:131-136`](../../../examples/forms-uploads/presign.go#L131-L136)

```go
func (s *presignStore) Remove(_ context.Context, ref brick.UploadRef) error {
	s.mu.Lock()
	delete(s.stored, ref.ID)
	s.mu.Unlock()
	return nil
}
```
<!-- /ds:block hash=97baad -->

The whole store, with its storage handler, is in [presign.go](../../../examples/forms-uploads/presign.go). The test walks the three requests:

<!-- snippet: examples/forms-uploads/main_test.go lines=203-258 -->
```go
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
```

## Step 7 — Send files from code with `brick.upload` and hear the events

A drop zone, a paste handler or a camera capture has files but no `<input type="file">`. `brick.upload(el, field, files)` sends them as a `brick:upload` input would: `el` is any element inside the stateful component, `field` an upload field from `uploads:`, `files` a `FileList` or an array of `File`. Each file is its own job in the component's request queue, and the promise resolves once every accepted file is done. A file the declared `accept`, `max_size` or `max_files` refuses is never sent; it emits `upload-error` with `refused: true`.

The runtime reports every upload on `brick.on`, the same events a `brick:upload-progress` element listens to:

| Event | `detail` |
|---|---|
| `upload-progress` | `{field, name, size, loaded, total}` |
| `upload-done` | `{field, name, size}` |
| `upload-error` | `{message, field, name, size, …}` |

`theme/components/Profile.html` (the `<brick:client>` block)

<!-- snippet: examples/forms-uploads/theme/components/Profile.html lines=18-41 -->
```html
<brick:client>
// A drop zone for the photos field. brick.upload(el, field, files) sends
// the files as a brick:upload input would: the same limits, the same
// events, one queue with the component's other actions. It resolves when
// every accepted file is done; a refused file emits upload-error and is
// not sent.
document.addEventListener("dragover", (event) => {
  if (event.target.closest("[data-photo-drop]")) event.preventDefault();
});
document.addEventListener("drop", (event) => {
  const zone = event.target.closest("[data-photo-drop]");
  if (!zone) return;
  event.preventDefault();
  brick.upload(zone, "photos", event.dataTransfer.files).catch((err) => console.error(err));
});

// The runtime's upload events, for a status line of your own.
const status = () => document.querySelector("[data-upload-status]");
brick.on("upload-progress", (d) => {
  status().textContent = d.name + ": " + Math.round((100 * d.loaded) / d.total) + "%";
});
brick.on("upload-done", (d) => { status().textContent = d.name + " uploaded"; });
brick.on("upload-error", (d) => { status().textContent = d.name + ": " + d.message; });
</brick:client>
```

The page carries a `<div data-photo-drop>` for the zone and a `<p data-upload-status>` the events write into. Dropping a file, and the status moving as the bytes go, only a browser does. The test checks what Go owns: the zone and status line are on the page, the form is multipart, and the `Profile` module holding `brick.upload(` and the three listeners is served.

`main_test.go`

<!-- snippet: examples/forms-uploads/main_test.go lines=280-302 -->
```go
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
```

## Try it

```sh
cd examples
go test ./forms-uploads/ -v
```

```text
=== RUN   TestUploadThroughTheEngine
--- PASS: TestUploadThroughTheEngine (0.04s)
=== RUN   TestUploadWithoutJavaScript
--- PASS: TestUploadWithoutJavaScript (0.01s)
=== RUN   TestPresignedUpload
--- PASS: TestPresignedUpload (0.00s)
=== RUN   TestUploadsNeedASink
--- PASS: TestUploadsNeedASink (0.00s)
=== RUN   TestDropZoneClientServed
--- PASS: TestDropZoneClientServed (0.00s)
PASS
ok  	github.com/ubgo/brick/examples/forms-uploads	0.073s
```

What the handler answered for each `PUT /_brick/upload` in a real run of these requests (the id is random):

```text
me.png, an 8×8 PNG         → 200, the root re-rendered: <img src="/uploads/495dc4281624b676f2e53fcf2761296a" alt="me.png" width="72" height="72">
notes.png, really text     → 415 brick: this type of file is not accepted: avatar takes image/png, image/jpeg, not text/plain
wide.png, 5000 px wide     → 422 The image is 5000 pixels wide; the most is 4096.
huge.png, 3 MB             → 413 brick: the file is too large: avatar takes files up to 2097152 bytes
a third photo              → 422 brick: field value refused: photos holds at most 2 files
```

## Pitfalls

- **The type is sniffed from the bytes, never taken from the browser.** A text file renamed `.png` and sent as `image/png` answers `415` (pinned in the test). Text formats such as CSV take the claimed `text/*` type, because they all sniff as `text/plain`.
- **`max_size` is enforced while the bytes stream**, whatever size the client claims, and every refusal aborts the sink's writer, so nothing half-written is kept.
- **`uploads:` needs a sink.** Without `Options.Uploads` the component fails to compile: `components/F.html:5: brick: uploads: no UploadSink configured (Options.Uploads); set one, such as upload/disk, so files are never silently dropped`. `brick:upload` on a field not under `uploads:` fails too: `brick:upload="name": "name" is not an upload field (declare it in brick: {uploads:}; declared: )`.
- **A presigned upload is never sniffed.** The bytes go straight to storage, so `Confirm` reports the stored size but the type is the `Content-Type` the client signed for, and `UploadCheck` runs with `head == nil` (the example's check lets those through). If you need the content checked, use the proxied path or scan the file when your app keeps it.
- **A presigning sink still needs a working `Open`.** The runtime presigns whenever the sink offers it, but a form posted with JavaScript off streams its files through the engine into `Open` (the last case in `TestPresignedUpload`).
- **`$removeUpload` needs `brick.UploadRemover`.** Without a `Remove` method the call is refused rather than leaving the bytes behind while the ref disappears from state.
- **`$set` cannot write an upload field**, so a client cannot point `avatar` at someone else's file.
- **Uploads are temporary until your app keeps them.** The disk sink's `Sweep` (or an S3 lifecycle rule) must remove files that were uploaded and never kept.

## Related

- [Forms without JavaScript](without-javascript.md), [Validation](validation.md) (`avatar: [required]`), [Deferred fields](deferred-fields.md).
- `brick.upload(el, field, files)` and the upload events are in Step 7; the full list is in the [runtime API reference](../../reference/runtime-api.md). The long form: [Uploads — files into state, bytes into your storage](../../guide.md#uploads--files-into-state-bytes-into-your-storage).
- The S3 sink: [contrib/upload-s3](../../../contrib/upload-s3/README.md). Tests without HTTP: `bricktest.NewMemoryUploads()` and `LiveRoot.Upload`.
- Feature index rows: [Forms, validation and uploads](../../reference/features.md#forms-validation-and-uploads).
- Example: [examples/forms-uploads](../../../examples/forms-uploads/).
