package brick

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"html"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// The no-JavaScript rewrite touches only brick:submit forms
// inside a stateful root that leave method, action and confirmation to the
// engine, and takes each form's handle from its innermost root.
func TestRewriteForms(t *testing.T) {
	t.Parallel()
	root := func(h, body string) string {
		return `<div data-brick-state="` + h + `" data-brick-token="T` + h + `">` + body + `</div>`
	}
	cases := map[string]struct{ in, want string }{
		"plain": {root("a", `<form brick:submit="save"><input brick:model="x"></form>`),
			root("a", `<form brick:submit="save" method="post"><input brick:model="x" name="brick:model:x"><input type="hidden" name="brick:state" value="a"><input type="hidden" name="brick:token" value="Ta"><input type="hidden" name="brick:action" value="save"><input type="hidden" name="brick:models" value="[[&#34;v&#34;,&#34;x&#34;,&#34;&#34;]]"></form>`)},
		"author action": {root("a", `<form brick:submit="save" action="/login"></form>`), ""},
		"author method": {root("a", `<form brick:submit="save" method="get"></form>`), ""},
		"confirm":       {root("a", `<form brick:submit="wipe" brick:confirm="Sure?"></form>`), ""},
		"prompt":        {root("a", `<form brick:submit="rename" brick:confirm.prompt="New name?"></form>`), ""},
		"client only":   {root("a", `<form brick:submit='$dispatch("x")'></form>`), ""},
		"no root":       {`<form brick:submit="save"></form>`, ""},
		"script text":   {root("a", `<script>"<form brick:submit=\"x\">"</script>`), ""},
		"nested root": {root("a", root("b", `<form brick:submit="go(1, &#34;z&#34;)"></form>`)),
			root("a", root("b", `<form brick:submit="go(1, &#34;z&#34;)" method="post"><input type="hidden" name="brick:state" value="b"><input type="hidden" name="brick:token" value="Tb"><input type="hidden" name="brick:action" value="go(1, &#34;z&#34;)"></form>`))},
		"named model kept": {root("a", `<form brick:submit="s"><input type="radio" brick:model="p" name="p" value="1"><input type="radio" brick:model="p" name="p" value="2"></form>`),
			root("a", `<form brick:submit="s" method="post"><input type="radio" brick:model="p" name="p" value="1"><input type="radio" brick:model="p" name="p" value="2"><input type="hidden" name="brick:state" value="a"><input type="hidden" name="brick:token" value="Ta"><input type="hidden" name="brick:action" value="s"><input type="hidden" name="brick:models" value="[[&#34;r&#34;,&#34;p&#34;,&#34;p&#34;]]"></form>`)},
	}
	for name, c := range cases {
		want := c.want
		if want == "" {
			want = c.in
		}
		got := string(rewriteForms([]Chunk{{Kind: ChunkBytes, Bytes: []byte(c.in)}})[0].Bytes)
		if got != want {
			t.Errorf("%s:\n got  %s\n want %s", name, got, want)
		}
	}
}

// The posted call is parsed as the runtime parses it; client-only calls
// are refused.
func TestParseFormCall(t *testing.T) {
	t.Parallel()
	name, args, err := parseFormCall(`bump(10, "a")`)
	if err != nil || name != "bump" || len(args) != 2 || args[0] != int64(10) && args[0] != 10 {
		t.Errorf("bump: %q %v %v", name, args, err)
	}
	for _, c := range []string{`$dispatch("x")`, `$parent.save`, `$cancelUpload("f")`, `save(nope)`, `<x>`} {
		if _, _, err := parseFormCall(c); err == nil || !strings.Contains(err.Error(), "brick:") {
			t.Errorf("%s: %v", c, err)
		}
	}
}

// No-JavaScript uploads. BV in a comment marks a
// break-verified test.

// nojsUploader is a form with a one-file field (engine-named input) and a
// list field (author-named input); its action sets n, so a test sees
// whether the action ran after the files.
const nojsUploader = `---
state:
  avatar: null
  photos: []
  n: 0
brick:
  uploads:
    avatar: {accept: [image/png], max_size: 100KB}
    photos: {accept: [image/*], max_size: 1KB, max_files: 2}
---
<form brick:submit='$set("n", 1)'><input type="file" brick:upload="avatar"><input type="file" brick:upload="photos" name="pics" multiple><i>{{ with .State.avatar }}{{ .id }}:{{ .name }}:{{ .size }}{{ end }}|{{ range .State.photos }}{{ .id }},{{ end }}|{{ .State.n }}|{{ range $k, $v := .Errors }}{{ $k }}={{ $v }};{{ end }}</i></form>`

// nojsPart is one multipart part: a text field when typ is "", else a
// file part (file "" is a file input with no file chosen).
type nojsPart struct{ name, file, typ, data string }

// nojsUploadEngine renders the uploader page and returns the engine, the
// page and its hidden fields in document order.
func nojsUploadEngine(t *testing.T, sink UploadSink, opts Options) (*Engine, []nojsPart) {
	t.Helper()
	opts.FS = theme("components/Up.html", nojsUploader, "content/a.html", `<Up />`)
	opts.States = NewMemoryStates(0, 0)
	opts.Uploads = sink
	e := newEngine(t, opts)
	page, err := e.Render(context.Background(), "content/a.html", nil)
	if err != nil {
		t.Fatal(err)
	}
	var hidden []nojsPart
	for _, m := range regexp.MustCompile(`<input type="hidden" name="([^"]+)" value="([^"]*)">`).FindAllStringSubmatch(string(page), -1) {
		hidden = append(hidden, nojsPart{name: m[1], data: html.UnescapeString(m[2])})
	}
	if len(hidden) < 4 || hidden[0].name != formFieldState || hidden[3].name != formFieldUploads {
		t.Fatalf("the upload form does not lead with the instance fields: %s", page)
	}
	if !strings.Contains(string(page), `enctype="multipart/form-data"`) {
		t.Fatalf("no multipart enctype: %s", page)
	}
	return e, hidden
}

// nojsPost builds a multipart form post of parts, in order.
func nojsPost(t *testing.T, site string, parts []nojsPart) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for _, p := range parts {
		if p.typ == "" {
			_ = mw.WriteField(p.name, p.data)
			continue
		}
		h := textproto.MIMEHeader{}
		h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="%s"`, p.name, p.file))
		h.Set("Content-Type", p.typ)
		w, err := mw.CreatePart(h)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.WriteString(w, p.data)
	}
	_ = mw.Close()
	r := httptest.NewRequest(http.MethodPost, "/a/", &buf)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	r.Header.Set("Sec-Fetch-Site", site)
	return r
}

// nojsShow renders the page as the redirect or the 422 shows it.
func nojsShow(t *testing.T, e *Engine, out *FormPostResult) string {
	t.Helper()
	page, err := e.Render(context.Background(), "content/a.html", nil, WithFormResume(out.Resume))
	if err != nil {
		t.Fatal(err)
	}
	return body(t, page)
}

var nojsPNG = string(pngHead)

// Files stream into the sink as the JS path's uploads, then the action
// runs with the instance they left; a file larger than MaxActionBytes is
// fine (its bytes count against max_size only), an empty file input is
// skipped, and the 303 target is the page. BV: not counting upload bytes
// as such refuses the large file as an oversized form.
func TestFormPostUploads(t *testing.T) {
	t.Parallel()
	sink := newMemSink()
	e, hidden := nojsUploadEngine(t, sink, Options{MaxActionBytes: 600})
	// Over MaxActionBytes plus formReadSlack, under max_size: upload bytes
	// never count against MaxActionBytes.
	big := nojsPNG + strings.Repeat("x", 80<<10)
	parts := append(slices.Clone(hidden),
		nojsPart{name: formUploadPrefix + "avatar", file: "me.png", typ: "image/png", data: big},
		nojsPart{name: "pics", file: "a.png", typ: "image/png", data: nojsPNG},
		nojsPart{name: "pics", file: "b.png", typ: "image/png", data: nojsPNG},
		nojsPart{name: "pics", file: "", typ: "application/octet-stream"},
	)
	r := nojsPost(t, "same-origin", parts)
	if !e.IsFormPost(r) {
		t.Fatal("not a form post")
	}
	out, err := e.FormPost(r)
	if err != nil {
		t.Fatal(err)
	}
	if out.Failed || out.Location != "/a/" || out.Resume.Handle == "" {
		t.Fatalf("out = %+v", out)
	}
	if got, want := nojsShow(t, e, out), "f1:me.png:81952|f2,f3,|1|"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if sink.opened != 3 || len(sink.aborted) != 0 {
		t.Fatalf("opened %d, aborted %v", sink.opened, sink.aborted)
	}
}

// A refused file stops the post before the action: the 422 shows the
// instance as the earlier files left it, with the refusal as the field's
// error, and the writer is aborted. BV: dropping the written > maxSize
// check stores the large file; dropping the accept check stores the text.
func TestFormPostUploadRefused(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		file nojsPart
		want string
	}{
		"too large":  {nojsPart{name: formUploadPrefix + "avatar", file: "big.png", typ: "image/png", data: nojsPNG + strings.Repeat("x", 101<<10)}, "the file is too large"},
		"wrong type": {nojsPart{name: formUploadPrefix + "avatar", file: "fake.png", typ: "image/png", data: "plain text, not a PNG"}, "this type of file is not accepted"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			sink := newMemSink()
			e, hidden := nojsUploadEngine(t, sink, Options{})
			parts := append(slices.Clone(hidden),
				nojsPart{name: "pics", file: "a.png", typ: "image/png", data: nojsPNG},
				c.file,
				nojsPart{name: "pics", file: "b.png", typ: "image/png", data: nojsPNG},
			)
			out, err := e.FormPost(nojsPost(t, "same-origin", parts))
			if err != nil {
				t.Fatal(err)
			}
			if !out.Failed || out.Resume.ID == "" {
				t.Fatalf("out = %+v", out)
			}
			got := nojsShow(t, e, out)
			if !strings.HasPrefix(got, "|f1,|0|avatar="+c.want) {
				t.Fatalf("got %q", got)
			}
			if len(sink.files) != 1 {
				t.Fatalf("stored %d files", len(sink.files))
			}
		})
	}
	// The first file refused: the 422 still shows that instance.
	sink := newMemSink()
	e, hidden := nojsUploadEngine(t, sink, Options{})
	out, err := e.FormPost(nojsPost(t, "same-origin", append(slices.Clone(hidden), nojsPart{name: formUploadPrefix + "avatar", file: "x.gif", typ: "image/gif", data: "GIF89a" + strings.Repeat("\x00", 20)})))
	if err != nil || !out.Failed || out.Resume.ID == "" {
		t.Fatalf("out %+v, err %v", out, err)
	}
	if got := nojsShow(t, e, out); !strings.HasPrefix(got, "||0|avatar=this type of file is not accepted") {
		t.Fatalf("got %q", got)
	}
}

// A one-file field takes one file per post: a second part is refused
// rather than streamed into the sink and replaced.
func TestFormPostUploadOneFile(t *testing.T) {
	t.Parallel()
	sink := newMemSink()
	e, hidden := nojsUploadEngine(t, sink, Options{})
	f := nojsPart{name: formUploadPrefix + "avatar", file: "a.png", typ: "image/png", data: nojsPNG}
	out, err := e.FormPost(nojsPost(t, "same-origin", append(slices.Clone(hidden), f, f)))
	if err != nil || !out.Failed {
		t.Fatalf("out %+v, err %v", out, err)
	}
	if got := nojsShow(t, e, out); !strings.HasPrefix(got, "f1:a.png:32||0|avatar=field value refused: avatar holds one file") || sink.opened != 1 {
		t.Fatalf("got %q, opened %d", got, sink.opened)
	}
}

// The CSRF check holds on the multipart path: a cross-site or same-site
// post is refused before a byte reaches the sink, and so is a file part
// that comes before the instance fields. Text over MaxActionBytes is
// still 413. BV: making formOriginOK accept everything stores the file.
func TestFormPostUploadGuards(t *testing.T) {
	t.Parallel()
	f := nojsPart{name: formUploadPrefix + "avatar", file: "a.png", typ: "image/png", data: nojsPNG}
	for _, site := range []string{"cross-site", "same-site", "none"} {
		sink := newMemSink()
		e, hidden := nojsUploadEngine(t, sink, Options{})
		r := nojsPost(t, site, append(slices.Clone(hidden), f))
		if !e.IsFormPost(r) {
			t.Fatalf("%s: not a form post", site)
		}
		if _, err := e.FormPost(r); !errors.Is(err, ErrForbidden) || sink.opened != 0 {
			t.Fatalf("%s: err %v, opened %d", site, err, sink.opened)
		}
	}
	sink := newMemSink()
	e, hidden := nojsUploadEngine(t, sink, Options{})
	if _, err := e.FormPost(nojsPost(t, "same-origin", append([]nojsPart{f}, hidden...))); ActionStatus(err) != http.StatusBadRequest || sink.opened != 0 {
		t.Fatalf("file first: err %v, opened %d", err, sink.opened)
	}
	e, hidden = nojsUploadEngine(t, sink, Options{MaxActionBytes: 600})
	parts := append(slices.Clone(hidden), nojsPart{name: "note", data: strings.Repeat("n", 70<<10)}, f)
	if _, err := e.FormPost(nojsPost(t, "same-origin", parts)); ActionStatus(err) != http.StatusRequestEntityTooLarge || sink.opened != 0 {
		t.Fatalf("big text: err %v, opened %d", err, sink.opened)
	}
}

// An upload form gets the multipart enctype and leads with the instance
// fields and the upload map; an author's own non-multipart enctype, and a
// form that closes in another chunk, stay as v16 wrote them.
func TestRewriteUploadForms(t *testing.T) {
	t.Parallel()
	root := `<div data-brick-state="a" data-brick-token="Ta">`
	lead := `<input type="hidden" name="brick:state" value="a"><input type="hidden" name="brick:token" value="Ta"><input type="hidden" name="brick:action" value="s">`
	cases := map[string]struct{ in, want string }{
		"upload": {root + `<form brick:submit="s"><input type="file" brick:upload="avatar"><input type="file" brick:upload="photos" name="pics"></form></div>`,
			root + `<form brick:submit="s" method="post" enctype="multipart/form-data">` + lead + `<input type="hidden" name="brick:uploads" value="{&#34;brick:upload:avatar&#34;:&#34;avatar&#34;,&#34;pics&#34;:&#34;photos&#34;}"><input type="file" brick:upload="avatar" name="brick:upload:avatar"><input type="file" brick:upload="photos" name="pics"></form></div>`},
		"author multipart": {root + `<form brick:submit="s" enctype="multipart/form-data"><input type="file" brick:upload="avatar"></form></div>`,
			root + `<form brick:submit="s" enctype="multipart/form-data" method="post">` + lead + `<input type="hidden" name="brick:uploads" value="{&#34;brick:upload:avatar&#34;:&#34;avatar&#34;}"><input type="file" brick:upload="avatar" name="brick:upload:avatar"></form></div>`},
		"author urlencoded": {root + `<form brick:submit="s" enctype="application/x-www-form-urlencoded"><input type="file" brick:upload="avatar"></form></div>`,
			root + `<form brick:submit="s" enctype="application/x-www-form-urlencoded" method="post"><input type="file" brick:upload="avatar">` + lead + `</form></div>`},
		"disabled": {root + `<form brick:submit="s"><input type="file" brick:upload="avatar" disabled></form></div>`,
			root + `<form brick:submit="s" method="post"><input type="file" brick:upload="avatar" disabled>` + lead + `</form></div>`},
	}
	for name, c := range cases {
		if got := string(rewriteForms([]Chunk{{Kind: ChunkBytes, Bytes: []byte(c.in)}})[0].Bytes); got != c.want {
			t.Errorf("%s:\n got  %s\n want %s", name, got, c.want)
		}
	}
	split := rewriteForms([]Chunk{{Kind: ChunkBytes, Bytes: []byte(root + `<form brick:submit="s">`)}, {Kind: ChunkNonce}, {Kind: ChunkBytes, Bytes: []byte(`<input type="file" brick:upload="avatar"></form></div>`)}})
	if got, want := string(split[0].Bytes)+"|"+string(split[2].Bytes), root+`<form brick:submit="s" method="post">|<input type="file" brick:upload="avatar" name="brick:upload:avatar">`+lead+`</form></div>`; got != want {
		t.Errorf("split:\n got  %s\n want %s", got, want)
	}
}
