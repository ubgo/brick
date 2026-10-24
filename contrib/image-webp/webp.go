// Package imagewebp adds WebP output to brick's {{ image }} built-in,
// through nativewebp, a pure-Go encoder (no cgo).
//
//	eng, err := brick.New(brick.Options{ImageEncoders: []brick.ImageEncoder{imagewebp.New()}})
//
// and in a template:
//
//	{{ with image "~/static/team.jpg" 480 960 "webp" }}<img src="{{ .URL }}" srcset="{{ .SrcSet }}">{{ end }}
//
// The output is LOSSLESS WebP (VP8L) only: nativewebp has no lossy
// encoder. Lossless WebP is usually smaller than PNG, but for photographs
// it is usually larger than brick's JPEG at quality 85, so ask for "webp"
// for screenshots, illustrations and images with transparency, and keep
// photos as JPEG.
//
// Importing this package also registers a WebP decoder with the standard
// image package (nativewebp does that in its init), so {{ image }} can
// then read .webp source files as well.
package imagewebp

import (
	"image"
	"io"

	"github.com/HugoSmits86/nativewebp"
	"github.com/ubgo/brick"
)

// Format is the name a template asks for and the file extension. The dev
// server and contrib/assets-http serve ".webp" as image/webp.
const Format = "webp"

// Compression is nativewebp's encoder effort, 0 (fastest) to 6 (smallest
// files). It changes file size and encoding time, never pixels.
type Compression = nativewebp.CompressionLevel

// The compression levels nativewebp names.
const (
	BestSpeed          = nativewebp.BestSpeed
	DefaultCompression = nativewebp.DefaultCompression
	BestCompression    = nativewebp.BestCompression
)

// Option configures the encoder.
type Option func(*encoder)

// WithCompression sets the encoder effort (default DefaultCompression).
// It is the one knob: there is no quality setting, because the output is
// lossless.
func WithCompression(c Compression) Option {
	return func(e *encoder) { e.opts.CompressionLevel = c }
}

type encoder struct{ opts nativewebp.Options }

// New returns a brick.ImageEncoder for the "webp" format. It is safe for
// concurrent use and deterministic: the same image always encodes to the
// same bytes, which brick's hashed file names rely on.
func New(opts ...Option) brick.ImageEncoder {
	e := &encoder{opts: nativewebp.Options{CompressionLevel: DefaultCompression}}
	for _, o := range opts {
		o(e)
	}
	return e
}

func (e *encoder) Format() string { return Format }

// Encode writes img as lossless WebP. Every pixel survives exactly,
// including alpha; there is no quality to lose.
func (e *encoder) Encode(w io.Writer, img image.Image) error {
	opts := e.opts
	ew := &errWriter{w: w}
	if err := nativewebp.Encode(ew, img, &opts); err != nil {
		return err
	}
	return ew.err
}

// errWriter keeps the first write error: nativewebp does not check the
// results of its writes, and a failed write must not pass as an image.
type errWriter struct {
	w   io.Writer
	err error
}

func (ew *errWriter) Write(p []byte) (int, error) {
	if ew.err != nil {
		return 0, ew.err
	}
	n, err := ew.w.Write(p)
	ew.err = err
	return n, err
}
