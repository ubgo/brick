package brick

import (
	"context"
	"errors"
	"io"
)

// UploadSink stores uploaded bytes. The host
// implements it — a disk directory, S3, a virus-scanning proxy — and plugs
// it in through Options.Uploads; brick never chooses where bytes go, and
// scripts only ever see the UploadRef. Orphans are the sink's job: a file
// uploaded and never committed by the app must expire on its own (a TTL
// sweep, an S3 lifecycle rule on the temp prefix).
//
// Optional upgrades are detected by type assertion, so a sink that lacks
// one is never forced to fake it: UploadRemover ($removeUpload) and
// UploadPresigner (direct-to-storage uploads).
type UploadSink interface {
	// Open starts one file. The engine has already authorised the
	// instance and run the ActionMiddleware chain; m.Size is the client's
	// claim, enforced while the bytes stream.
	Open(ctx context.Context, m UploadMeta) (UploadWriter, error)
}

// UploadWriter receives one file's bytes. Exactly one of Close and Abort
// is called: Close commits and returns the ref; Abort discards what was
// written (limit exceeded, type refused, client gone, a later step failed)
// and is given the reason.
type UploadWriter interface {
	io.Writer
	Close() (UploadRef, error)
	Abort(err error) error
}

// UploadMeta describes one incoming file. Every string except Component,
// Field and Session is the client's claim: Name is a display name, never a
// path, and Type is checked against the sniffed bytes before Open.
type UploadMeta struct {
	// Component is the tag name, Field the declared uploads: key.
	Component string
	Field     string
	// Name is the file name the browser reported; Type its content type
	// after the sniff check.
	Name string
	Type string
	// Session is the hashed session id the instance is bound to, "" when
	// none, so a sink can partition temp files per user.
	Session string
	// Size is as declared by the client, 0 when unknown (a file part of a
	// no-JavaScript form post carries no size); the engine enforces the
	// declared max_size while streaming, whatever this says.
	Size int64
}

// UploadCheck is a host's own per-file check, set as
// Options.UploadCheck: a virus-signature lookup, a magic-number or image
// dimension test, a per-user quota. It runs after the declared accept,
// max_size and max_files checks and before UploadSink.Open, so a refused
// file is never stored. m.Type is the sniffed type. head is the file's
// first UploadCheckBytes bytes (fewer for a smaller file); it is nil for a
// presign request, where brick never sees the bytes, so a content check
// on direct-to-storage uploads belongs in the sink's Confirm.
//
// Return RefuseUpload(message) to refuse with a message shown on the
// field; any other error fails the upload as a server error (reported,
// and not shown in Prod).
type UploadCheck func(ctx context.Context, m UploadMeta, head []byte) error

// UploadCheckBytes is how many leading bytes an UploadCheck sees: the
// bytes brick already holds to sniff the type, so the check costs no
// extra buffering, and enough for any file signature.
const UploadCheckBytes = sniffBytes

// ErrUploadRefused is an UploadCheck's refusal. It also matches
// ErrBadValue, so the action answers 422 with the message (safe in Prod)
// and a no-JavaScript form shows it on the field.
var ErrUploadRefused = errors.New("brick: the file was refused")

// uploadRefusal is the error RefuseUpload makes.
type uploadRefusal struct{ msg string }

func (u *uploadRefusal) Error() string { return u.msg }

// Is matches both sentinels, so every existing ErrBadValue path (status,
// Prod message, no-JS field error) handles a refusal unchanged.
func (u *uploadRefusal) Is(target error) bool {
	return target == ErrUploadRefused || target == ErrBadValue
}

// RefuseUpload is the error an UploadCheck returns to refuse a file with
// message, which the user sees on the field: write it for them ("the
// image must be square"), never with a path or an internal detail.
func RefuseUpload(message string) error {
	if message == "" {
		message = ErrUploadRefused.Error()
	}
	return &uploadRefusal{msg: message}
}

// UploadRef is what lands in state: small, JSON-shaped and opaque to
// brick (D14). Scripts see it as a dict (id, name, type, size, url, meta);
// turning it into a permanent file is a host capability (ScriptGlobals).
type UploadRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
	Size int64  `json:"size"`
	// URL is an optional preview URL the host serves (the disk sink's
	// handler, a signed S3 GET).
	URL string `json:"url,omitempty"`
	// Meta is the sink's own fields; brick carries them untouched.
	Meta map[string]string `json:"meta,omitempty"`
}

// UploadRemover is the optional upgrade behind the built-in
// $removeUpload(field, id): the sink forgets one uploaded file. Without
// it, $removeUpload is refused rather than leaving the bytes behind while
// the ref disappears from state.
type UploadRemover interface {
	Remove(ctx context.Context, ref UploadRef) error
}

// UploadPresigner is the optional upgrade for direct-to-storage uploads
// : the browser PUTs the bytes straight to storage and
// brick never proxies them. The engine accepts a confirmation only for an
// id it issued to the same instance, and re-checks the declared limits on
// what Confirm reports, so a client cannot claim someone else's object or
// a file larger than allowed.
type UploadPresigner interface {
	// Presign returns where and how the browser uploads one file.
	Presign(ctx context.Context, m UploadMeta) (PresignedUpload, error)
	// Confirm reads the stored object for an id Presign issued and returns
	// its ref with the real size and type, from storage, never from the
	// client.
	Confirm(ctx context.Context, id string) (UploadRef, error)
}

// PresignedUpload is one presigned target: the browser sends Method to
// URL with Headers and the file as the body, then confirms ID.
type PresignedUpload struct {
	URL     string            `json:"url"`
	Method  string            `json:"method"`
	Headers map[string]string `json:"headers,omitempty"`
	// ID names the object for Confirm; the engine records it in the
	// instance as a pending upload, so only this instance may confirm it.
	ID string `json:"id"`
}
