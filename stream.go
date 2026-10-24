package brick

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"unicode/utf8"
)

// Streaming action output. An action that produces text over
// seconds (a chat reply, a completion) writes it to a brick:ref while it
// runs; the final render still lands as usual. Chunks are text only (D18):
// the runtime sets them with textContent, so streamed text never needs
// escaping or sanitising, and markup is the final render's job.
const (
	// StreamContentType is the streamed action answer: one JSON line per
	// chunk, then one line holding the normal envelope or an error. The
	// runtime lists it in Accept; an answer stays a plain envelope unless
	// the action streams.
	StreamContentType = "application/vnd.brick.stream+ndjson"
	// accelBufferingHeader asks a buffering proxy (nginx) to pass each
	// chunk on as it is written.
	accelBufferingHeader = "X-Accel-Buffering"
)

// ErrStreamClosed is returned by a Write after the action has answered.
var ErrStreamClosed = errors.New("brick: the action already answered; its stream is closed")

// ActionStream receives text an action streams before it returns
// . The HTTP transport writes NDJSON; Engine.Action callers
// and bricktest pass their own with WithActionStream. A middleware that
// wants to audit, filter or drop chunks wraps the stream before calling
// next — after next, the text has already been sent.
type ActionStream interface {
	// Write sends text to the element brick:ref="ref" in the component's
	// root: appended, or replacing its text when replace is true.
	Write(ref, text string, replace bool) error
}

type streamKey struct{}

// WithActionStream sets the stream StreamFrom returns.
func WithActionStream(ctx context.Context, s ActionStream) context.Context {
	return context.WithValue(ctx, streamKey{}, s)
}

// StreamFrom returns the action's stream. It never returns nil: outside a
// streaming transport it is a no-op, so an action behaves the same in a
// test, a non-streaming host or a client that did not ask for a stream.
func StreamFrom(ctx context.Context) ActionStream {
	if s, ok := ctx.Value(streamKey{}).(ActionStream); ok && s != nil {
		return s
	}
	return noStream{}
}

// noStream drops every chunk.
type noStream struct{}

func (noStream) Write(string, string, bool) error { return nil }

// streamChunk is one NDJSON chunk line's payload.
type streamChunk struct {
	Ref     string `json:"ref"`
	Text    string `json:"text"`
	Replace bool   `json:"replace,omitempty"`
}

// streamError is the last line of a stream whose action failed after it
// had started streaming: the status the plain answer would have had.
type streamError struct {
	Status  int    `json:"status"`
	Message string `json:"message"`
}

// ndjsonStream is the HTTP transport's ActionStream. The first Write
// commits the response as NDJSON; until then the answer can still be a
// plain envelope or an error status.
type ndjsonStream struct {
	mu        sync.Mutex
	w         http.ResponseWriter
	rc        *http.ResponseController
	committed bool
	closed    bool
}

func newNDJSONStream(w http.ResponseWriter) *ndjsonStream {
	return &ndjsonStream{w: w, rc: http.NewResponseController(w)}
}

// Write sends one chunk line and flushes it. A ref must be a name, and
// text valid UTF-8, so a line can never be anything but one chunk.
func (s *ndjsonStream) Write(ref, text string, replace bool) error {
	if ref == "" || strings.ContainsAny(ref, " \t\n\"'<>") {
		return errors.New("brick: stream: ref must be a brick:ref name")
	}
	if !utf8.ValidString(text) {
		return errors.New("brick: stream: text must be valid UTF-8")
	}
	return s.line(map[string]any{"stream": streamChunk{Ref: ref, Text: text, Replace: replace}}, false)
}

// line writes one JSON line, committing the response first. last closes
// the stream.
func (s *ndjsonStream) line(v any, last bool) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrStreamClosed
	}
	if !s.committed {
		h := s.w.Header()
		h.Set("Content-Type", StreamContentType)
		h.Set("Cache-Control", "no-store")
		h.Set(accelBufferingHeader, "no")
		h.Add("Vary", "Accept")
		s.w.WriteHeader(http.StatusOK)
		s.committed = true
	}
	s.closed = last
	if _, err := s.w.Write(append(b, '\n')); err != nil {
		return err
	}
	// A writer that cannot flush still gets every line, at the end.
	if err := s.rc.Flush(); err != nil && !errors.Is(err, http.ErrNotSupported) {
		return err
	}
	return nil
}

// started reports whether any chunk was sent; it closes the stream for
// further chunks either way, since the action has returned.
func (s *ndjsonStream) started() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.committed {
		s.closed = true
	}
	return s.committed
}

// acceptsStream reports whether the client listed StreamContentType.
func acceptsStream(accept string) bool {
	for _, part := range strings.Split(accept, ",") {
		mt, _, _ := strings.Cut(part, ";")
		if strings.TrimSpace(mt) == StreamContentType {
			return true
		}
	}
	return false
}

// finishStream writes a streamed action's last line: the envelope the
// plain answer would have carried, or, after a failure, its status and
// message as the error mapping (or Options.ActionErrors) would send them.
func (e *Engine) finishStream(s *ndjsonStream, r *http.Request, res *ActionResult, err error) {
	if err == nil {
		// The last line: a client that went away has no one left to tell.
		_ = s.line(e.envelopeOf(res), true)
		return
	}
	rec := &recorder{header: http.Header{}}
	if e.opts.ActionErrors != nil {
		e.opts.ActionErrors(rec, r, err)
	} else {
		e.writeActionError(rec, r, err)
	}
	if rec.status == 0 {
		rec.status = http.StatusOK
	}
	_ = s.line(map[string]any{"v": effectsEnvelopeVersion, "error": streamError{Status: rec.status, Message: strings.TrimSpace(rec.body.String())}}, true)
}

// recorder captures an error answer so a stream can carry it as its last
// line, after the status line has already gone out.
type recorder struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (r *recorder) Header() http.Header         { return r.header }
func (r *recorder) Write(p []byte) (int, error) { return r.body.Write(p) }
func (r *recorder) WriteHeader(code int) {
	if r.status == 0 {
		r.status = code
	}
}
