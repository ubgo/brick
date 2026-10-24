package main

import (
	"context"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/ubgo/brick/site"
)

// indexPage is the exact page the images guide shows. The 8 hex digits are
// the start of diagram.png's SHA-256, so the names change only with the file.
const indexPage = `<!doctype html>
<html>
<head><title>Images</title></head>
<body>
<img src="/_brick/img/diagram.f9b1ebb0-1200.png" srcset="/_brick/img/diagram.f9b1ebb0-480.png 480w, /_brick/img/diagram.f9b1ebb0-960.png 960w, /_brick/img/diagram.f9b1ebb0-1200.png 1200w" sizes="(max-width: 800px) 100vw, 800px" width="1200" height="675" alt="How it fits together" loading="lazy">
<img src="/_brick/img/diagram.f9b1ebb0-640.jpg" srcset="/_brick/img/diagram.f9b1ebb0-320.jpg 320w, /_brick/img/diagram.f9b1ebb0-640.jpg 640w" width="640" height="360" alt="The same, as JPEG">
<img src="/_brick/img/diagram.f9b1ebb0-960.webp" srcset="/_brick/img/diagram.f9b1ebb0-480.webp 480w, /_brick/img/diagram.f9b1ebb0-960.webp 960w" width="960" height="540" alt="The same, as lossless WebP">
</body>
</html>
`

func serve(h http.Handler, url string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url, nil))
	return rec
}

func TestImages(t *testing.T) {
	s, err := newSite(context.Background(), themeDir)
	if err != nil {
		t.Fatal(err)
	}
	if rec := serve(s.Handler(), "/"); rec.Code != http.StatusOK || rec.Body.String() != indexPage {
		t.Fatalf("/: %d\n%s", rec.Code, rec.Body)
	}
	rec := serve(s.Handler(), "/_brick/img/diagram.f9b1ebb0-480.png")
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/png" || rec.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" {
		t.Fatalf("copy: %d %v", rec.Code, rec.Header())
	}
	if rec := serve(s.Handler(), "/_brick/img/diagram.f9b1ebb0-960.webp"); rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/webp" {
		t.Fatalf("webp copy: %d %v", rec.Code, rec.Header())
	}
}

// Export writes every resized copy under _brick/img/.
func TestExportImages(t *testing.T) {
	ctx := context.Background()
	s, err := newSite(ctx, themeDir)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if _, err := s.Export(ctx, dir); err != nil {
		t.Fatal(err)
	}
	var files []string
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && d.Name() != ".brick-export" {
			rel, _ := filepath.Rel(dir, p)
			files = append(files, filepath.ToSlash(rel))
		}
		return err
	})
	want := []string{"_brick/img/diagram.f9b1ebb0-1200.png", "_brick/img/diagram.f9b1ebb0-320.jpg", "_brick/img/diagram.f9b1ebb0-480.png", "_brick/img/diagram.f9b1ebb0-480.webp", "_brick/img/diagram.f9b1ebb0-640.jpg", "_brick/img/diagram.f9b1ebb0-960.png", "_brick/img/diagram.f9b1ebb0-960.webp", "diagram.png", "index.html"}
	if !slices.Equal(files, want) {
		t.Fatalf("files:\n%s", strings.Join(files, "\n"))
	}
}

// The errors a reader hits, with the text the guide quotes.
func TestImageErrors(t *testing.T) {
	png, err := os.ReadFile(filepath.Join(themeDir, "static/diagram.png"))
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct{ page, want string }{
		{`{{ with image "~/static/diagram.png" 480 "avif" }}{{ .URL }}{{ end }}`, `image "~/static/diagram.png": no encoder for format "avif" (have jpeg, png); register one in Options.ImageEncoders, for example contrib/image-webp`},
		{`{{ with image "~/layouts/default.html" }}{{ .URL }}{{ end }}`, `image "~/layouts/default.html": image paths must be inside Options.ImageDirs (default static/)`},
		{`{{ with image .Params.cover }}{{ .URL }}{{ end }}`, `~/static/nope.png: file does not exist`},
	}
	for _, c := range cases {
		m := fstest.MapFS{
			"static/diagram.png":   {Data: png},
			"layouts/default.html": {Data: []byte("{{ children }}")},
			"content/index.html":   {Data: []byte("---\ncover: ~/static/nope.png\n---\n" + c.page)},
		}
		s, err := site.New(context.Background(), site.Options{FS: m})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.Render(context.Background(), "/"); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v", c.page, err)
		}
	}
}
