package brick

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"strings"
	"testing"
	"testing/fstest"
)

func pngOf(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(x), G: uint8(y), B: 100, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestImage(t *testing.T) {
	t.Parallel()
	fsys := theme(
		"content/a.html", `{{ with image "~/static/p.png" 50 100 400 }}<img src="{{ .URL }}" srcset="{{ .SrcSet }}" width="{{ .Width }}" height="{{ .Height }}">{{ end }}`,
		"content/data.html", "---\ncover: ~/static/p.png\n---\n{{ (image .Params.cover 20).Width }}",
		"content/outside.html", `{{ image "~/secret/p.png" }}`,
		"content/notimage.html", `{{ image "~/static/notes.txt" }}`,
		"content/huge.html", `{{ image "~/static/p.png" }}`,
	)
	fsys["static/p.png"] = &fstest.MapFile{Data: pngOf(t, 200, 100)}
	fsys["secret/p.png"] = &fstest.MapFile{Data: pngOf(t, 10, 10)}
	fsys["static/notes.txt"] = &fstest.MapFile{Data: []byte("hi")}
	e := newEngine(t, Options{FS: fsys})
	got := render(t, e, "content/a.html", nil)
	if !strings.Contains(got, `srcset="/_brick/img/p.`) || !strings.Contains(got, `-50.png 50w, `) || !strings.Contains(got, `-200.png 200w"`) || !strings.Contains(got, `width="200" height="100"`) {
		t.Fatalf("img: %s", got)
	}
	r, _ := e.RenderChunks(context.Background(), "content/a.html", nil)
	if !strings.Contains(strings.Join(r.Files, " "), "static/p.png") {
		t.Fatalf("image not a dependency: %v", r.Files)
	}
	f, err := e.Assets().Open(strings.TrimPrefix(strings.Split(strings.Split(got, `src="/_brick/`)[1], `"`)[0], ""))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(f)
	cfg, format, err := image.DecodeConfig(bytes.NewReader(b))
	if err != nil || format != "png" || cfg.Width != 200 {
		t.Fatalf("served copy: %v %s %+v", err, format, cfg)
	}
	if got := render(t, e, "content/data.html", nil); got != "20" {
		t.Fatalf("path from data: %q", got)
	}
	if err := renderErr(t, e, "content/outside.html"); !strings.Contains(err.Error(), "ImageDirs") {
		t.Fatalf("outside: %v", err)
	}
	if err := renderErr(t, e, "content/notimage.html"); !strings.Contains(err.Error(), "not a JPEG") {
		t.Fatalf("not an image: %v", err)
	}
	small := newEngine(t, Options{FS: fsys, MaxImagePixels: 100})
	if err := renderErr(t, small, "content/huge.html"); !strings.Contains(err.Error(), "MaxImagePixels") {
		t.Fatalf("limit: %v", err)
	}
}

func TestImageJPEGAndDownscale(t *testing.T) {
	t.Parallel()
	img := image.NewRGBA(image.Rect(0, 0, 4, 2))
	for y := range 2 {
		for x := range 4 {
			v := uint8(0)
			if x >= 2 {
				v = 200
			}
			img.Set(x, y, color.RGBA{v, v, v, 255})
		}
	}
	d := downscale(img, 2, 1)
	if d.NRGBAAt(0, 0).R != 0 || d.NRGBAAt(1, 0).R != 200 {
		t.Fatalf("area average: %v %v", d.NRGBAAt(0, 0), d.NRGBAAt(1, 0))
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatal(err)
	}
	fsys := theme("content/a.html", `{{ (image "~/static/x.jpg" 2 99).SrcSet }}`)
	fsys["static/x.jpg"] = &fstest.MapFile{Data: buf.Bytes()}
	got := render(t, newEngine(t, Options{FS: fsys}), "content/a.html", nil)
	if !strings.HasSuffix(got, "-2.jpg 2w, /_brick/img/x."+got[strings.Index(got, "x.")+2:strings.Index(got, "x.")+2+hashLen]+"-4.jpg 4w") {
		t.Fatalf("jpeg widths: %s", got)
	}
}
