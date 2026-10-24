package brick

import (
	"bytes"
	"image"
	"image/jpeg"
	"io"
	"strings"
	"testing"
	"testing/fstest"
)

// rawEncoder is a test format: "raw" writes width, height and NRGBA bytes,
// so the test can check the encoder received the resized pixels.
type rawEncoder struct{ format string }

func (r rawEncoder) Format() string { return r.format }

func (r rawEncoder) Encode(w io.Writer, img image.Image) error {
	b := img.Bounds()
	_, err := io.WriteString(w, r.format+":"+strings.Repeat("x", b.Dx())+"|"+strings.Repeat("y", b.Dy()))
	return err
}

func readAsset(t *testing.T, e *Engine, url string) []byte {
	t.Helper()
	f, err := e.Assets().Open(strings.TrimPrefix(url, DefaultAssetBase))
	if err != nil {
		t.Fatalf("open %s: %v", url, err)
	}
	b, _ := io.ReadAll(f)
	return b
}

func TestImageEncoderFormat(t *testing.T) {
	t.Parallel()
	fsys := theme(
		"content/raw.html", `{{ (image "~/static/p.png" 50 "raw").URL }}`,
		"content/both.html", `{{ (image "~/static/p.png" 50 "raw").URL }} {{ (image "~/static/p.png" 50).URL }}`,
		"content/tojpeg.html", `{{ (image "~/static/p.png" 50 "jpeg").URL }}`,
		"content/data.html", "---\nw: 40\nf: raw\n---\n{{ (image \"~/static/p.png\" .Params.w .Params.f).URL }}",
	)
	fsys["static/p.png"] = &fstest.MapFile{Data: pngOf(t, 200, 100)}
	e := newEngine(t, Options{FS: fsys, ImageEncoders: []ImageEncoder{rawEncoder{"raw"}}})

	got := render(t, e, "content/raw.html", nil)
	if !strings.HasPrefix(got, "/_brick/img/p.") || !strings.HasSuffix(got, "-50.raw") {
		t.Fatalf("raw url: %q", got)
	}
	if b := string(readAsset(t, e, got)); b != "raw:"+strings.Repeat("x", 50)+"|"+strings.Repeat("y", 25) {
		t.Fatalf("encoder got the wrong pixels: %q", b)
	}
	// The same file in two formats makes two copies, never one shadowing the other.
	both := strings.Fields(render(t, e, "content/both.html", nil))
	if len(both) != 2 || !strings.HasSuffix(both[0], ".raw") || !strings.HasSuffix(both[1], "-50.png") {
		t.Fatalf("two formats: %q", both)
	}
	// A built-in format can be named too: PNG in, JPEG out.
	j := render(t, e, "content/tojpeg.html", nil)
	if !strings.HasSuffix(j, "-50.jpg") {
		t.Fatalf("jpeg url: %q", j)
	}
	if _, err := jpeg.Decode(bytes.NewReader(readAsset(t, e, j))); err != nil {
		t.Fatalf("jpeg copy: %v", err)
	}
	if got := render(t, e, "content/data.html", nil); !strings.HasSuffix(got, "-40.raw") {
		t.Fatalf("width and format from data: %q", got)
	}
}

// A registered "png" replaces the built-in encoder (precedence, not prohibition).
func TestImageEncoderReplacesBuiltin(t *testing.T) {
	t.Parallel()
	fsys := theme("content/a.html", `{{ (image "~/static/p.png" 10).URL }}`)
	fsys["static/p.png"] = &fstest.MapFile{Data: pngOf(t, 20, 20)}
	e := newEngine(t, Options{FS: fsys, ImageEncoders: []ImageEncoder{rawEncoder{"png"}}})
	got := render(t, e, "content/a.html", nil)
	if b := string(readAsset(t, e, got)); !strings.HasPrefix(b, "png:") || !strings.HasSuffix(got, "-10.png") {
		t.Fatalf("replaced png: %q %q", got, b)
	}
}

func TestImageEncoderErrors(t *testing.T) {
	t.Parallel()
	fsys := theme(
		"content/webp.html", `{{ image "~/static/p.png" 50 "webp" }}`,
		"content/two.html", `{{ image "~/static/p.png" "raw" "png" }}`,
		"content/bad.html", `{{ image "~/static/p.png" true }}`,
		"content/frac.html", "---\nw: 40.5\n---\n{{ image \"~/static/p.png\" .Params.w }}",
	)
	fsys["static/p.png"] = &fstest.MapFile{Data: pngOf(t, 20, 20)}
	e := newEngine(t, Options{FS: fsys, ImageEncoders: []ImageEncoder{rawEncoder{"raw"}}})
	for page, want := range map[string]string{
		"content/webp.html": `image "~/static/p.png": no encoder for format "webp" (have jpeg, png, raw); register one in Options.ImageEncoders, for example contrib/image-webp`,
		"content/two.html":  `image "~/static/p.png": two formats ("raw" and "png"); name one`,
		"content/bad.html":  `image "~/static/p.png": argument true (bool) is neither a width nor a format`,
		"content/frac.html": `image "~/static/p.png": width 40.5 must be a whole number`,
	} {
		if err := renderErr(t, e, page); !strings.Contains(err.Error(), want) {
			t.Errorf("%s:\n got  %v\n want %s", page, err, want)
		}
	}

	for _, tc := range []struct {
		encs []ImageEncoder
		want string
	}{
		{[]ImageEncoder{nil}, "brick: Options.ImageEncoders: nil encoder"},
		{[]ImageEncoder{rawEncoder{"WebP"}}, `brick: Options.ImageEncoders: format "WebP" must be lowercase letters and digits`},
		{[]ImageEncoder{rawEncoder{""}}, `brick: Options.ImageEncoders: format "" must be lowercase letters and digits`},
		{[]ImageEncoder{rawEncoder{"raw"}, rawEncoder{"raw"}}, `brick: Options.ImageEncoders: two encoders for format "raw"`},
	} {
		_, err := New(Options{FS: fsys, ImageEncoders: tc.encs})
		if err == nil || err.Error() != tc.want {
			t.Errorf("New: got %v, want %s", err, tc.want)
		}
	}
}
