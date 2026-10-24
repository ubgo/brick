package brick

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"path"
	"slices"
	"strconv"
	"strings"
	"sync"

	// GIF decoding registers with image.Decode; GIFs are resized to PNG.
	_ "image/gif"
)

// imageDir is where resized images live under AssetBase.
const imageDir = "img"

// jpegQuality is the quality of resized JPEGs. Pinned: output bytes are
// part of the compatibility promise (A14).
const jpegQuality = 85

// Built-in image formats. A copy keeps its source's format (GIF becomes
// PNG) unless the call names one: {{ image "~/static/p.png" 480 "webp" }}.
const (
	imageJPEG = "jpeg"
	imagePNG  = "png"
)

// ImageEncoder adds an output format to {{ image }} beyond the built-in
// JPEG and PNG, and is registered through Options.ImageEncoders (for
// example contrib/image-webp). The root never names an image vendor: a
// format exists only because a host registered it, and asking for one that
// nobody registered is an error naming the format, never a silent
// fallback to JPEG or PNG.
//
// There is no quality argument on purpose: brick has no quality setting
// (JPEG is pinned at 85 so output bytes stay stable), so an encoder that
// has one takes it as its own constructor option. Encode must be
// deterministic — same image, same bytes — because file names carry the
// source's hash and output is part of the same-input-same-bytes promise.
type ImageEncoder interface {
	// Format is the name a template asks for and the file extension:
	// lowercase letters and digits ("webp", "avif"). Registering "jpeg"
	// or "png" replaces the built-in encoder for that format.
	Format() string
	// Encode writes img in the format. It is called concurrently.
	Encode(w io.Writer, img image.Image) error
}

// imageExt is the file extension of a format; only JPEG's differs from
// its name. The extension is what assets-http and the dev server use to
// pick the Content-Type (mime.TypeByExtension), so it must be the
// format's registered one.
func imageExt(format string) string {
	if format == imageJPEG {
		return ".jpg"
	}
	return "." + format
}

// checkImageEncoders validates Options.ImageEncoders at New, so a typo in
// a host's wiring fails at startup rather than on the first page that
// asks for the format.
func checkImageEncoders(encs []ImageEncoder) (map[string]ImageEncoder, error) {
	m := map[string]ImageEncoder{}
	for _, enc := range encs {
		if enc == nil {
			return nil, errors.New("brick: Options.ImageEncoders: nil encoder")
		}
		f := enc.Format()
		if !isImageFormat(f) {
			return nil, fmt.Errorf("brick: Options.ImageEncoders: format %q must be lowercase letters and digits", f)
		}
		if _, dup := m[f]; dup {
			return nil, fmt.Errorf("brick: Options.ImageEncoders: two encoders for format %q", f)
		}
		m[f] = enc
	}
	return m, nil
}

func isImageFormat(f string) bool {
	if f == "" {
		return false
	}
	for _, r := range f {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}

// imageFormats lists the formats a call may name, for error messages.
func (e *Engine) imageFormats() []string {
	fs := []string{imageJPEG, imagePNG}
	for f := range e.imageEncoders {
		if !slices.Contains(fs, f) {
			fs = append(fs, f)
		}
	}
	slices.Sort(fs)
	return fs
}

// imageArgs splits the arguments after the path: integers are widths and
// at most one string names the output format. Widths from JSON data
// arrive as float64, so whole floats count as integers.
func imageArgs(raw string, args []any) (widths []int, format string, err error) {
	for _, a := range args {
		switch v := a.(type) {
		case int:
			widths = append(widths, v)
		case int64:
			widths = append(widths, int(v))
		case float64:
			if v != float64(int(v)) {
				return nil, "", fmt.Errorf("image %q: width %v must be a whole number", raw, v)
			}
			widths = append(widths, int(v))
		case string:
			if format != "" {
				return nil, "", fmt.Errorf("image %q: two formats (%q and %q); name one", raw, format, v)
			}
			format = v
		default:
			return nil, "", fmt.Errorf("image %q: argument %v (%T) is neither a width nor a format", raw, a, a)
		}
	}
	return widths, format, nil
}

// Image is what {{ image }} returns: the pieces of an <img> tag.
type Image struct {
	// URL is the largest resized copy, for src.
	URL string
	// SrcSet lists every width, for srcset: "/_brick/img/p.ab12-480.jpg 480w, …".
	SrcSet string
	// Width and Height are the largest copy's size, for the width and
	// height attributes that let the browser reserve space.
	Width, Height int
	// Sources are the individual copies, smallest first.
	Sources []ImageSource
}

// ImageSource is one resized copy.
type ImageSource struct {
	URL           string
	Width, Height int
}

// imageEntry resizes one (file, widths) request once per snapshot.
type imageEntry struct {
	once sync.Once
	img  Image
	err  error
}

// errImageDir explains the confinement rule for {{ image }} paths.
var errImageDir = errors.New("image paths must be inside Options.ImageDirs (default static/)")

// image is {{ image "~/static/p.jpg" [widths…] ["format"] }}. The path may
// come from data, so it is confined to ImageDirs and must decode as an
// image: that is the whole set of files it can reach.
func (st *renderState) image(s *Scope, raw string, args ...any) (Image, error) {
	widths, format, err := imageArgs(raw, args)
	if err != nil {
		return Image{}, err
	}
	if format != "" && format != imageJPEG && format != imagePNG && st.e.imageEncoders[format] == nil {
		return Image{}, fmt.Errorf("image %q: no encoder for format %q (have %s); register one in Options.ImageEncoders, for example contrib/image-webp", raw, format, strings.Join(st.e.imageFormats(), ", "))
	}
	if st.dry {
		return Image{}, nil
	}
	p, err := resolvePath(raw)
	if err != nil {
		return Image{}, err
	}
	if !slices.ContainsFunc(st.e.opts.ImageDirs, func(d string) bool {
		d = fsDir(d)
		return strings.HasPrefix(p, d+"/")
	}) {
		return Image{}, fmt.Errorf("image %q: %w", raw, errImageDir)
	}
	if len(widths) == 0 {
		widths = st.e.opts.ImageWidths
	}
	for _, w := range widths {
		if w <= 0 {
			return Image{}, fmt.Errorf("image %q: width %d must be positive", raw, w)
		}
	}
	st.files[p] = true
	key := p + "|" + fmt.Sprint(widths) + "|" + format
	st.e.mu.Lock()
	if st.e.images == nil {
		st.e.images = map[string]*imageEntry{}
	}
	ent, ok := st.e.images[key]
	if !ok {
		ent = &imageEntry{}
		st.e.images[key] = ent
	}
	st.e.mu.Unlock()
	ent.once.Do(func() { ent.img, ent.err = st.e.resizeImage(p, widths, format) })
	return ent.img, ent.err
}

// resizeImage makes the copies of one image and registers them as assets.
// An empty format keeps the source's (GIF becomes PNG).
func (e *Engine) resizeImage(p string, widths []int, format string) (Image, error) {
	src, err := e.readFile(p)
	if err != nil {
		return Image{}, err
	}
	cfg, srcFormat, err := image.DecodeConfig(bytes.NewReader(src))
	if err != nil {
		return Image{}, fmt.Errorf("image %s: not a JPEG, PNG or GIF: %w", displayPath(p), err)
	}
	if cfg.Width*cfg.Height > e.opts.MaxImagePixels {
		return Image{}, fmt.Errorf("image %s is %dx%d, over MaxImagePixels (%d)", displayPath(p), cfg.Width, cfg.Height, e.opts.MaxImagePixels)
	}
	img, _, err := image.Decode(bytes.NewReader(src))
	if err != nil {
		return Image{}, fmt.Errorf("image %s: %w", displayPath(p), err)
	}
	if format == "" {
		format = imagePNG
		if srcFormat == imageJPEG {
			format = imageJPEG
		}
	}
	ext := imageExt(format)
	enc := e.imageEncoders[format]
	sum := sha256.Sum256(src)
	hash := hex.EncodeToString(sum[:])[:hashLen]
	stem := strings.TrimSuffix(path.Base(p), path.Ext(p))
	// Widths never exceed the original; if every width does, the original
	// size is the one copy.
	var ws []int
	for _, w := range widths {
		ws = append(ws, min(w, cfg.Width))
	}
	slices.Sort(ws)
	ws = slices.Compact(ws)
	var out Image
	var set []string
	for _, w := range ws {
		h := max(1, (cfg.Height*w+cfg.Width/2)/cfg.Width)
		resized := downscale(img, w, h)
		var buf bytes.Buffer
		switch {
		case enc != nil:
			err = enc.Encode(&buf, resized)
		case format == imageJPEG:
			err = jpeg.Encode(&buf, resized, &jpeg.Options{Quality: jpegQuality})
		default:
			err = png.Encode(&buf, resized)
		}
		if err != nil {
			return Image{}, fmt.Errorf("image %s: %w", displayPath(p), err)
		}
		name := path.Join(imageDir, stem+"."+hash+"-"+strconv.Itoa(w)+ext)
		e.mu.Lock()
		if e.assetFiles == nil {
			e.assetFiles = map[string]assetFile{}
		}
		e.assetFiles[name] = assetFile{name: name, data: buf.Bytes()}
		e.mu.Unlock()
		url := e.opts.AssetBase + name
		out.Sources = append(out.Sources, ImageSource{URL: url, Width: w, Height: h})
		set = append(set, url+" "+strconv.Itoa(w)+"w")
		out.URL, out.Width, out.Height = url, w, h
	}
	out.SrcSet = strings.Join(set, ", ")
	return out, nil
}

// downscale resizes by area averaging: each destination pixel is the mean
// of the source pixels it covers, which is sharp enough for photos and has
// no ringing. Upscaling is never asked for.
func downscale(src image.Image, w, h int) *image.NRGBA {
	b := src.Bounds()
	sw, sh := b.Dx(), b.Dy()
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		y0 := b.Min.Y + y*sh/h
		y1 := max(b.Min.Y+(y+1)*sh/h, y0+1)
		for x := range w {
			x0 := b.Min.X + x*sw/w
			x1 := max(b.Min.X+(x+1)*sw/w, x0+1)
			var r, g, bl, a, n uint64
			for sy := y0; sy < y1; sy++ {
				for sx := x0; sx < x1; sx++ {
					c := color.NRGBAModel.Convert(src.At(sx, sy)).(color.NRGBA)
					r += uint64(c.R)
					g += uint64(c.G)
					bl += uint64(c.B)
					a += uint64(c.A)
					n++
				}
			}
			dst.SetNRGBA(x, y, color.NRGBA{R: uint8(r / n), G: uint8(g / n), B: uint8(bl / n), A: uint8(a / n)})
		}
	}
	return dst
}
