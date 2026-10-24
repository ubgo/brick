package imagewebp

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/ubgo/brick"
	"golang.org/x/image/webp"
)

// photo is a gradient with transparency, so a lossy or alpha-dropping
// encoder could not round-trip it.
func photo(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(x * 3), G: uint8(y * 5), B: uint8(x ^ y), A: uint8(128 + x%128)})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func site(t *testing.T, encs ...brick.ImageEncoder) *brick.Engine {
	t.Helper()
	fsys := fstest.MapFS{
		"content/index.html": {Data: []byte(`{{ with image "~/static/p.png" 60 120 "webp" }}<img src="{{ .URL }}" srcset="{{ .SrcSet }}" width="{{ .Width }}" height="{{ .Height }}">{{ end }}` + "\n" + `{{ (image "~/static/p.png" 60).URL }}`)},
		"static/p.png":       {Data: photo(t, 160, 80)},
	}
	e, err := brick.New(brick.Options{FS: fsys, ImageEncoders: encs})
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func asset(t *testing.T, e *brick.Engine, url string) []byte {
	t.Helper()
	f, err := e.Assets().Open(strings.TrimPrefix(url, brick.DefaultAssetBase))
	if err != nil {
		t.Fatalf("open %s: %v", url, err)
	}
	b, err := io.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func renderPage(t *testing.T, e *brick.Engine) string {
	t.Helper()
	out, err := e.Render(context.Background(), "content/index.html", nil)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

// The webp copy decodes, with golang.org/x/image/webp, to exactly the
// pixels of the PNG copy of the same width: lossless, alpha included.
func TestWebPRoundTrip(t *testing.T) {
	t.Parallel()
	e := site(t, New())
	got := renderPage(t, e)
	lines := strings.Split(got, "\n")
	if !strings.Contains(lines[0], `-60.webp 60w, /_brick/img/p.`) || !strings.Contains(lines[0], `-120.webp 120w"`) || !strings.Contains(lines[0], `width="120" height="60"`) {
		t.Fatalf("img tag: %s", got)
	}
	webpURL := strings.Split(strings.Split(lines[0], `srcset="`)[1], " ")[0]
	pngURL := lines[1]
	if !strings.HasSuffix(webpURL, "-60.webp") || !strings.HasSuffix(pngURL, "-60.png") {
		t.Fatalf("urls: %q %q", webpURL, pngURL)
	}
	wb := asset(t, e, webpURL)
	if !bytes.HasPrefix(wb, []byte("RIFF")) || string(wb[8:12]) != "WEBP" {
		t.Fatalf("not a webp file: % x", wb[:16])
	}
	w, err := webp.Decode(bytes.NewReader(wb))
	if err != nil {
		t.Fatal(err)
	}
	p, err := png.Decode(bytes.NewReader(asset(t, e, pngURL)))
	if err != nil {
		t.Fatal(err)
	}
	if w.Bounds() != p.Bounds() {
		t.Fatalf("bounds %v vs %v", w.Bounds(), p.Bounds())
	}
	for y := p.Bounds().Min.Y; y < p.Bounds().Max.Y; y++ {
		for x := p.Bounds().Min.X; x < p.Bounds().Max.X; x++ {
			a := color.NRGBAModel.Convert(w.At(x, y))
			b := color.NRGBAModel.Convert(p.At(x, y))
			if a != b {
				t.Fatalf("pixel (%d,%d): webp %v, png %v", x, y, a, b)
			}
		}
	}
}

// Same input, same bytes: two engines produce identical webp files.
func TestWebPDeterministic(t *testing.T) {
	t.Parallel()
	a, b := site(t, New()), site(t, New())
	ua := strings.Split(strings.Split(renderPage(t, a), `src="`)[1], `"`)[0]
	ub := strings.Split(strings.Split(renderPage(t, b), `src="`)[1], `"`)[0]
	if ua != ub || !bytes.Equal(asset(t, a, ua), asset(t, b, ub)) {
		t.Fatal("webp output differs between runs")
	}
}

func TestWebPWithoutEncoder(t *testing.T) {
	t.Parallel()
	e := site(t)
	_, err := e.Render(context.Background(), "content/index.html", nil)
	want := `image "~/static/p.png": no encoder for format "webp" (have jpeg, png); register one in Options.ImageEncoders, for example contrib/image-webp`
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("got %v\nwant %s", err, want)
	}
}

func TestWebPContentType(t *testing.T) {
	t.Parallel()
	// contrib/assets-http and the site dev server both pick the type with
	// mime.TypeByExtension; Go's built-in table must know ".webp".
	if got := mime.TypeByExtension("." + Format); got != "image/webp" {
		t.Fatalf("Content-Type for .webp: %q", got)
	}
}

func TestWebPCompressionAndWriteErrors(t *testing.T) {
	t.Parallel()
	img := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	var fast, small bytes.Buffer
	if err := New(WithCompression(BestSpeed)).Encode(&fast, img); err != nil {
		t.Fatal(err)
	}
	if err := New(WithCompression(BestCompression)).Encode(&small, img); err != nil {
		t.Fatal(err)
	}
	for _, b := range [][]byte{fast.Bytes(), small.Bytes()} {
		if _, err := webp.Decode(bytes.NewReader(b)); err != nil {
			t.Fatal(err)
		}
	}
	boom := errors.New("disk full")
	if err := New().Encode(failWriter{boom}, img); !errors.Is(err, boom) {
		t.Fatalf("write error lost: %v", err)
	}
}

type failWriter struct{ err error }

func (f failWriter) Write([]byte) (int, error) { return 0, f.err }
