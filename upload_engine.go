package brick

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// The upload endpoint and its wire. The browser sends
// one request per file: PUT with the bytes as the body, or, for a sink
// that presigns, POST with no body to ask where to send them. The custom
// Brick-* headers force a CORS preflight, so a cross-site form cannot post
// here; the handle and token authorise the instance as for an action.
const (
	// UploadEndpoint is the upload route relative to the asset base
	// ("/_brick/upload" by default), beside ActionEndpoint; runtime.js
	// derives it from its own script URL the same way.
	UploadEndpoint = "upload"
	// Request headers. Name, Sets and Children are URI-encoded (the file
	// name and JSON may hold any character; a header holds Latin-1).
	uploadFieldHeader    = "Brick-Upload"
	uploadHandleHeader   = "Brick-Handle"
	uploadTokenHeader    = "Brick-Token"
	uploadNameHeader     = "Brick-Name"
	uploadSizeHeader     = "Brick-Size"
	uploadSetsHeader     = "Brick-Sets"
	uploadChildrenHeader = "Brick-Children"
	// uploadMetaPresign marks the presign request in the $upload call's
	// meta argument, for middleware.
	uploadMetaPresign = "presign"
)

// Upload built-ins. $upload is the pseudo-action every
// upload request runs as, so ActionMiddleware (auth, quotas, rate limits)
// sees it like any action; $uploaded confirms a presigned upload;
// $removeUpload drops one file through UploadRemover. $cancelUpload is
// client-only (it aborts the browser's request) and never reaches here.
const (
	actionUpload       = "$upload"
	actionUploaded     = "$uploaded"
	actionRemoveUpload = "$removeUpload"
	actionCancelUpload = "$cancelUpload"
)

// Upload limits when uploads: leaves them out. A field without max_size
// still has one, so no declaration means "unbounded".
const (
	DefaultMaxUploadBytes = 10 << 20
	DefaultMaxUploadFiles = 10
	// sniffBytes is how much of a file http.DetectContentType reads; the
	// only bytes held in memory beyond io.Copy's buffer.
	sniffBytes = 512
	// presignTTL is how long a presigned id stays confirmable; an id
	// older than this is refused as if it were foreign.
	presignTTL = time.Hour
	// maxPendingUploads caps the presigned ids one instance holds, so a
	// client asking again and again cannot grow the sealed instance; the
	// oldest is dropped first.
	maxPendingUploads = 16
)

// Upload refusals, matched by the HTTP adapter to a status. Each aborts
// the writer before the response is sent.
var (
	// ErrUploadTooLarge means the file is over the field's max_size —
	// declared, counted while streaming, or reported by Confirm. 413.
	ErrUploadTooLarge = errors.New("brick: the file is too large")
	// ErrUploadType means the file's sniffed type is not in the field's
	// accept list. 415.
	ErrUploadType = errors.New("brick: this type of file is not accepted")
)

// uploadSpec is one brick: {uploads:} entry: the accepted media types
// ("image/png", "image/*"; empty means any), the size limit per file, and
// how many files the field holds. many is true for a list field.
type uploadSpec struct {
	accept   []string
	maxSize  int64
	maxFiles int
	many     bool
}

// Keys of one uploads: entry.
const (
	uploadKeyAccept   = "accept"
	uploadKeyMaxSize  = "max_size"
	uploadKeyMaxFiles = "max_files"
)

var (
	sizeRE   = regexp.MustCompile(`^(?i)\s*(\d+)\s*(b|kb|mb|gb)?\s*$`)
	acceptRE = regexp.MustCompile(`^[a-z0-9][\w.+-]*/(\*|[a-z0-9][\w.+-]*)$`)
)

// sizeUnits are the max_size suffixes; KB, MB and GB are 1024-based.
var sizeUnits = map[string]int64{"": 1, "b": 1, "kb": 1 << 10, "mb": 1 << 20, "gb": 1 << 30}

// parseUploads reads brick: {uploads:}: field → {accept, max_size,
// max_files}. Whether each field fits its state is checked by
// checkUploads, which sees state:.
func parseUploads(v any) (map[string]uploadSpec, error) {
	mp, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("brick: uploads: must map state fields to limits (avatar: {accept: [image/png], max_size: 2MB})")
	}
	out := make(map[string]uploadSpec, len(mp))
	for _, field := range sortedKeys(mp) {
		spec := uploadSpec{maxSize: DefaultMaxUploadBytes}
		var lim map[string]any
		switch x := mp[field].(type) {
		case nil:
		case map[string]any:
			lim = x
		default:
			return nil, fmt.Errorf("brick: uploads: %s: the limits are a map ({accept: [image/*], max_size: 2MB, max_files: 1})", field)
		}
		for _, k := range sortedKeys(lim) {
			switch k {
			case uploadKeyAccept:
				list, ok := lim[k].([]any)
				if !ok {
					list = []any{lim[k]}
				}
				for _, a := range list {
					s, ok := a.(string)
					if !ok || !acceptRE.MatchString(strings.ToLower(s)) {
						return nil, fmt.Errorf("brick: uploads: %s: accept: %v is not a media type (image/png, image/*)", field, a)
					}
					spec.accept = append(spec.accept, strings.ToLower(s))
				}
			case uploadKeyMaxSize:
				n, err := parseSize(lim[k])
				if err != nil {
					return nil, fmt.Errorf("brick: uploads: %s: max_size: %w", field, err)
				}
				spec.maxSize = n
			case uploadKeyMaxFiles:
				n, ok := asInt(lim[k])
				if !ok || n < 1 {
					return nil, fmt.Errorf("brick: uploads: %s: max_files is a whole number of at least 1", field)
				}
				spec.maxFiles = n
			default:
				return nil, fmt.Errorf("brick: uploads: %s: unknown key %q (known: %s, %s, %s)", field, k, uploadKeyAccept, uploadKeyMaxSize, uploadKeyMaxFiles)
			}
		}
		out[field] = spec
	}
	return out, nil
}

// parseSize reads a byte count: a whole number, or one with a B, KB, MB
// or GB suffix ("2MB" is 2 << 20).
func parseSize(v any) (int64, error) {
	if n, ok := asInt(v); ok {
		if n < 1 {
			return 0, errors.New("must be at least 1 byte")
		}
		return int64(n), nil
	}
	s, _ := v.(string)
	m := sizeRE.FindStringSubmatch(s)
	if m == nil {
		return 0, fmt.Errorf("%v is not a size (2MB, 500KB, 1048576)", v)
	}
	n, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil || n < 1 {
		return 0, fmt.Errorf("%v is not a size of at least 1 byte", v)
	}
	return n * sizeUnits[strings.ToLower(m[2])], nil
}

// asInt reads a YAML or JSON whole number.
func asInt(v any) (int, bool) {
	switch x := v.(type) {
	case int:
		return x, true
	case int64:
		return int(x), true
	case uint64:
		return int(x), true
	case float64:
		if x == float64(int(x)) {
			return int(x), true
		}
	case json.Number:
		n, err := x.Int64()
		return int(n), err == nil
	}
	return 0, false
}

// checkUploads is the compile check of uploads: (7.1): a sink must be
// configured, every key must be a declared top-level state field holding
// null (one file) or a list (several), and max_files above 1 needs a list.
// A list field without max_files gets DefaultMaxUploadFiles.
func checkUploads(src []byte, m *meta, haveSink bool) (int, error) {
	if len(m.uploads) == 0 {
		return 0, nil
	}
	if m.state == nil {
		return lineOf(src, "", brickUploads), errors.New("brick: uploads: needs state: — an upload lands in a state field")
	}
	if !haveSink {
		return lineOf(src, "", brickUploads), errors.New("brick: uploads: no UploadSink configured (Options.Uploads); set one, such as upload/disk, so files are never silently dropped")
	}
	for _, field := range sortedKeys(m.uploads) {
		spec := m.uploads[field]
		line := lineOf(src, brickUploads, field)
		init, ok := m.state[field]
		if !ok {
			return line, fmt.Errorf("brick: uploads: %s is not a field of state: (declared: %s); declare it as null for one file or [] for several", field, strings.Join(sortedKeys(m.state), ", "))
		}
		switch init.(type) {
		case nil:
			if spec.maxFiles > 1 {
				return line, fmt.Errorf("brick: uploads: %s: max_files is %d, but state.%s is null, which holds one file; declare it as [] for several", field, spec.maxFiles, field)
			}
			spec.maxFiles = 1
		case []any:
			spec.many = true
			if spec.maxFiles == 0 {
				spec.maxFiles = DefaultMaxUploadFiles
			}
		default:
			return line, fmt.Errorf("brick: uploads: state.%s is %T; an upload field is null (one file) or [] (several)", field, init)
		}
		m.uploads[field] = spec
	}
	return 0, nil
}

// matchAccept reports whether a media type (no parameters) is in accept;
// an empty list accepts everything.
func matchAccept(accept []string, typ string) bool {
	if len(accept) == 0 {
		return true
	}
	major, _, _ := strings.Cut(typ, "/")
	for _, a := range accept {
		if a == typ || a == "*/*" || (strings.HasSuffix(a, "/*") && strings.TrimSuffix(a, "/*") == major) {
			return true
		}
	}
	return false
}

// mediaBase strips parameters: "text/plain; charset=utf-8" → "text/plain".
func mediaBase(t string) string {
	base, _, err := mime.ParseMediaType(t)
	if err != nil {
		base, _, _ = strings.Cut(t, ";")
	}
	return strings.ToLower(strings.TrimSpace(base))
}

// sniffedType decides a proxied file's type from its first bytes, never
// from the client's claim — except that sniffing cannot tell text formats
// apart (CSV, Markdown and JSON all sniff as text/plain), so a text/plain
// sniff takes a claimed textual type. A claim of HTML or XML is not taken:
// those sniff as themselves when they are, and a host serving a
// "text/html" upload back would be serving script.
func sniffedType(head []byte, claimed string) string {
	sniffed := mediaBase(http.DetectContentType(head))
	c := mediaBase(claimed)
	if sniffed == "text/plain" && c != "" && (strings.HasPrefix(c, "text/") || c == "application/json") && c != "text/html" && c != "text/xml" {
		return c
	}
	return sniffed
}

// uploadBody is the file an upload request carries, set by
// UploadHandler and read only by the core step: the field and bytes come
// from the request, never from ActionCall.Args, which are informational
// for middleware.
type uploadBody struct {
	field   string
	name    string
	ctype   string
	size    int64
	presign bool
	body    io.Reader
}

type uploadBodyKey struct{}

// refToState is the dict an UploadRef becomes in state (D14): id, name,
// type, size and url always, meta when the sink set any.
func refToState(ref UploadRef) map[string]any {
	m := map[string]any{"id": ref.ID, "name": ref.Name, "type": ref.Type, "size": ref.Size, "url": ref.URL}
	if len(ref.Meta) > 0 {
		meta := make(map[string]any, len(ref.Meta))
		for k, v := range ref.Meta {
			meta[k] = v
		}
		m["meta"] = meta
	}
	return m
}

// refFromState reads a state dict back into an UploadRef, for Remove.
func refFromState(m map[string]any) UploadRef {
	str := func(k string) string { s, _ := m[k].(string); return s }
	ref := UploadRef{ID: str("id"), Name: str("name"), Type: str("type"), URL: str("url")}
	if n, ok := asInt(m["size"]); ok {
		ref.Size = int64(n)
	}
	if meta, ok := m["meta"].(map[string]any); ok {
		ref.Meta = map[string]string{}
		for k, v := range meta {
			if s, ok := v.(string); ok {
				ref.Meta[k] = s
			}
		}
	}
	return ref
}

// refuseUploadSets refuses $set or a deferred set on an upload field: the
// field changes only through an upload, so a client cannot write a forged
// ref (or a string where scripts expect a dict).
func refuseUploadSets(m *meta, sets []FieldSet) error {
	for _, s := range sets {
		first, _, _ := strings.Cut(s.Path, ".")
		if _, ok := m.uploads[first]; ok {
			return fmt.Errorf("%w: %s is an upload field; it changes only through uploads", ErrBadValue, first)
		}
	}
	return nil
}

// uploadCount is how many files a field holds now.
func uploadCount(inst *Instance, field string) int {
	switch x := inst.State[field].(type) {
	case []any:
		return len(x)
	case map[string]any:
		return 1
	}
	return 0
}

// placeRef writes a ref into its field: set for one file, appended for a
// list. The count was checked before any byte was stored.
func placeRef(inst *Instance, field string, spec uploadSpec, ref UploadRef) {
	st, _ := deepCopy(inst.State).(map[string]any)
	if spec.many {
		list, _ := st[field].([]any)
		st[field] = append(list, refToState(ref))
	} else {
		st[field] = refToState(ref)
	}
	inst.State = st
}

// checkUploadCount refuses a file a list field has no room for. A
// one-file field replaces its file, so it always has room.
func checkUploadCount(inst *Instance, field string, spec uploadSpec) error {
	if spec.many && uploadCount(inst, field) >= spec.maxFiles {
		return fmt.Errorf("%w: %s holds at most %d files", ErrBadValue, field, spec.maxFiles)
	}
	return nil
}

// uploadSpecFor returns the declared limits of a field, or ErrBadValue
// when it takes no files.
func uploadSpecFor(c *compiled, field string) (uploadSpec, error) {
	spec, ok := c.meta.uploads[field]
	if !ok {
		return uploadSpec{}, fmt.Errorf("%w: %q is not an upload field of this component (brick: {uploads:})", ErrBadValue, field)
	}
	return spec, nil
}

// receiveUpload is the core of $upload (7.3 step 3): the limits from
// uploads:, the size counted while streaming, the type sniffed, then the
// bytes streamed into the sink. Every failure after Open aborts the
// writer, so a refused or interrupted file never lingers as committed.
// For a presign request it asks the sink for a target instead and records
// the id as pending on the instance (7.4).
func (e *Engine) receiveUpload(ctx context.Context, inst *Instance, c *compiled, ub *uploadBody) (*UploadRef, *PresignedUpload, error) {
	spec, err := uploadSpecFor(c, ub.field)
	if err != nil {
		return nil, nil, err
	}
	if e.opts.Uploads == nil {
		return nil, nil, fmt.Errorf("%w: no UploadSink configured", ErrUnknownAction)
	}
	if err := checkUploadCount(inst, ub.field, spec); err != nil {
		return nil, nil, err
	}
	if ub.size > spec.maxSize {
		return nil, nil, fmt.Errorf("%w: %s takes files up to %d bytes", ErrUploadTooLarge, ub.field, spec.maxSize)
	}
	meta := UploadMeta{Component: inst.Name, Field: ub.field, Name: ub.name, Type: mediaBase(ub.ctype), Session: inst.Session, Size: ub.size}
	if ub.presign {
		pre, err := e.presignUpload(ctx, inst, spec, meta)
		return nil, pre, err
	}
	head := make([]byte, sniffBytes)
	n, err := io.ReadFull(ub.body, head)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return nil, nil, fmt.Errorf("reading upload %s: %w", ub.field, err)
	}
	head = head[:n]
	meta.Type = sniffedType(head, ub.ctype)
	if !matchAccept(spec.accept, meta.Type) {
		return nil, nil, fmt.Errorf("%w: %s takes %s, not %s", ErrUploadType, ub.field, strings.Join(spec.accept, ", "), meta.Type)
	}
	if err := e.checkUpload(ctx, meta, head); err != nil {
		return nil, nil, err
	}
	w, err := e.opts.Uploads.Open(ctx, meta)
	if err != nil {
		return nil, nil, err
	}
	// One byte past the limit is enough to know it is over; the rest is
	// never read.
	written, err := io.Copy(w, io.LimitReader(io.MultiReader(bytes.NewReader(head), ub.body), spec.maxSize+1))
	if err == nil && written > spec.maxSize {
		err = fmt.Errorf("%w: %s takes files up to %d bytes", ErrUploadTooLarge, ub.field, spec.maxSize)
	}
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		_ = w.Abort(err)
		return nil, nil, err
	}
	ref, err := w.Close()
	if err != nil {
		return nil, nil, err
	}
	if ref.Size == 0 {
		ref.Size = written
	}
	if ref.Type == "" {
		ref.Type = meta.Type
	}
	if ref.Name == "" {
		ref.Name = meta.Name
	}
	return &ref, nil, nil
}

// checkUpload runs Options.UploadCheck. A refusal keeps the
// host's message, which the field shows; any other error is the host's
// failure and is wrapped as one. head is a copy, so a check that keeps it
// cannot change the bytes the sink receives.
func (e *Engine) checkUpload(ctx context.Context, meta UploadMeta, head []byte) error {
	if e.opts.UploadCheck == nil {
		return nil
	}
	err := e.opts.UploadCheck(ctx, meta, bytes.Clone(head))
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrUploadRefused) {
		// Unwrapped: the host's message is what the user reads, and the
		// refusal already matches ErrBadValue.
		return err
	}
	return fmt.Errorf("brick: upload check for %s: %w", meta.Field, err)
}

// presignUpload asks a presigning sink for a target and records its id on
// the instance. The type and size checked here are the client's claims;
// Confirm's real values are checked again by $uploaded.
func (e *Engine) presignUpload(ctx context.Context, inst *Instance, spec uploadSpec, meta UploadMeta) (*PresignedUpload, error) {
	ps, ok := e.opts.Uploads.(UploadPresigner)
	if !ok {
		return nil, fmt.Errorf("%w: the upload sink does not presign; send the file itself", ErrUnknownAction)
	}
	if !matchAccept(spec.accept, meta.Type) {
		return nil, fmt.Errorf("%w: %s takes %s, not %s", ErrUploadType, meta.Field, strings.Join(spec.accept, ", "), meta.Type)
	}
	// The check sees only the claims here: the bytes go straight to
	// storage.
	if err := e.checkUpload(ctx, meta, nil); err != nil {
		return nil, err
	}
	pre, err := ps.Presign(ctx, meta)
	if err != nil {
		return nil, err
	}
	if pre.ID == "" {
		return nil, errors.New("brick: UploadPresigner.Presign returned no ID")
	}
	inst.Pending = append(inst.Pending, PendingUpload{ID: pre.ID, Field: meta.Field, Issued: e.opts.Clock()})
	if over := len(inst.Pending) - maxPendingUploads; over > 0 {
		inst.Pending = slices.Delete(inst.Pending, 0, over)
	}
	return &pre, nil
}

// confirmUpload is the core of $uploaded(field, id) (7.4): only an id this
// instance was issued for that field, not yet expired, is accepted; the
// sink's Confirm reads the real size and type from storage, and the limits
// are checked again on them. A refused file is removed when the sink can.
func (e *Engine) confirmUpload(ctx context.Context, inst *Instance, c *compiled, args []any) (string, *UploadRef, error) {
	field, id, err := fieldAndID(actionUploaded, args)
	if err != nil {
		return "", nil, err
	}
	spec, err := uploadSpecFor(c, field)
	if err != nil {
		return "", nil, err
	}
	ps, ok := e.opts.Uploads.(UploadPresigner)
	if !ok {
		return "", nil, fmt.Errorf("%w: the upload sink does not presign", ErrUnknownAction)
	}
	i := slices.IndexFunc(inst.Pending, func(p PendingUpload) bool { return p.ID == id && p.Field == field })
	if i < 0 {
		return "", nil, fmt.Errorf("%w: upload %q was not issued to this component", ErrForbidden, id)
	}
	issued := inst.Pending[i].Issued
	inst.Pending = slices.Delete(inst.Pending, i, i+1)
	if e.opts.Clock().Sub(issued) > presignTTL {
		return "", nil, fmt.Errorf("%w: upload %q expired", ErrForbidden, id)
	}
	if err := checkUploadCount(inst, field, spec); err != nil {
		return "", nil, err
	}
	ref, err := ps.Confirm(ctx, id)
	if err != nil {
		return "", nil, err
	}
	refuse := func(err error) (string, *UploadRef, error) {
		if rm, ok := e.opts.Uploads.(UploadRemover); ok {
			_ = rm.Remove(ctx, ref)
		}
		return "", nil, err
	}
	if ref.Size > spec.maxSize {
		return refuse(fmt.Errorf("%w: %s takes files up to %d bytes", ErrUploadTooLarge, field, spec.maxSize))
	}
	if !matchAccept(spec.accept, mediaBase(ref.Type)) {
		return refuse(fmt.Errorf("%w: %s takes %s, not %s", ErrUploadType, field, strings.Join(spec.accept, ", "), ref.Type))
	}
	return field, &ref, nil
}

// removeUpload is the core of $removeUpload(field, id): the sink must be
// able to forget the file (UploadRemover), so the bytes never outlive the
// ref; then the ref leaves state.
func (e *Engine) removeUpload(ctx context.Context, inst *Instance, c *compiled, args []any) (string, error) {
	field, id, err := fieldAndID(actionRemoveUpload, args)
	if err != nil {
		return "", err
	}
	spec, err := uploadSpecFor(c, field)
	if err != nil {
		return "", err
	}
	rm, ok := e.opts.Uploads.(UploadRemover)
	if !ok {
		return "", fmt.Errorf("%w: the upload sink cannot remove files (it does not implement UploadRemover)", ErrUnknownAction)
	}
	st, _ := deepCopy(inst.State).(map[string]any)
	var found map[string]any
	if spec.many {
		list, _ := st[field].([]any)
		for i, x := range list {
			if m, ok := x.(map[string]any); ok && m["id"] == id {
				found = m
				st[field] = slices.Delete(slices.Clone(list), i, i+1)
				break
			}
		}
	} else if m, ok := st[field].(map[string]any); ok && m["id"] == id {
		found = m
		st[field] = nil
	}
	if found == nil {
		return "", fmt.Errorf("%w: %s holds no file %q", ErrBadValue, field, id)
	}
	if err := rm.Remove(ctx, refFromState(found)); err != nil {
		return "", err
	}
	inst.State = st
	return field, nil
}

// fieldAndID reads the (field, id) arguments of $uploaded and
// $removeUpload.
func fieldAndID(name string, args []any) (string, string, error) {
	if len(args) != 2 {
		return "", "", fmt.Errorf("%w: %s takes a field and a file id", ErrUnknownAction, name)
	}
	field, ok1 := args[0].(string)
	id, ok2 := args[1].(string)
	if !ok1 || !ok2 {
		return "", "", fmt.Errorf("%w: %s takes a field and a file id, both strings", ErrUnknownAction, name)
	}
	return field, id, nil
}

// PendingUpload is a presigned id the engine issued to one instance
// (7.4), kept in the instance — sealed with it — until $uploaded confirms
// it, a newer one pushes it out, or presignTTL passes.
type PendingUpload struct {
	ID     string
	Field  string
	Issued time.Time
}

// UploadHandler serves upload requests: PUT with the
// file as the body, or POST with no body to presign. Mount it at
// AssetBase + UploadEndpoint (the site layer does). It runs as the
// pseudo-action $upload through Options.ActionMiddleware, and answers
// with the action envelope; errors map as for actions, plus 413 and 415.
func (e *Engine) UploadHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		presign := r.Method == http.MethodPost
		if r.Method != http.MethodPut && !presign {
			w.Header().Set("Allow", http.MethodPut+", "+http.MethodPost)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		field := r.Header.Get(uploadFieldHeader)
		if field == "" {
			http.Error(w, "an upload carries the Brick-Upload header", http.StatusBadRequest)
			return
		}
		// A name that does not unescape is dropped, as an absent one is: it is
		// the browser's label for the file; the built-in sinks store by ID.
		name, _ := url.PathUnescape(r.Header.Get(uploadNameHeader))
		ub := &uploadBody{field: field, name: name, ctype: r.Header.Get("Content-Type"), size: r.ContentLength, presign: presign, body: r.Body}
		if presign {
			n, err := strconv.ParseInt(r.Header.Get(uploadSizeHeader), 10, 64)
			if err != nil || n < 0 {
				http.Error(w, "a presign request carries the file size in Brick-Size", http.StatusBadRequest)
				return
			}
			ub.size = n
		}
		req := ActionRequest{Handle: r.Header.Get(uploadHandleHeader), Token: r.Header.Get(uploadTokenHeader), Action: actionUpload}
		if !decodeHeaderJSON(r.Header.Get(uploadSetsHeader), &req.Sets) || !decodeHeaderJSON(r.Header.Get(uploadChildrenHeader), &req.Children) {
			http.Error(w, "bad upload request", http.StatusBadRequest)
			return
		}
		res, err := e.upload(r.Context(), r, req, ub, renderOptions{})
		if err == nil {
			e.writeActionResult(w, r, res)
			return
		}
		if errors.Is(err, context.Canceled) {
			return
		}
		if e.opts.ActionErrors != nil {
			e.opts.ActionErrors(w, r, err)
			return
		}
		e.writeActionError(w, r, err)
	})
}

// UploadFile is one file for RunUpload, as an upload request carries it.
// Name, Type and Size are the client's claims; Body is nil for a presign
// request (Presign true), which asks where to send the bytes instead.
type UploadFile struct {
	Field   string
	Name    string
	Type    string
	Size    int64
	Body    io.Reader
	Presign bool
}

// RunUpload runs one upload as UploadHandler does — the $upload
// pseudo-action through the middleware chain, the limits, the sink, the
// re-render — for hosts with their own transport and for tests. req names
// the instance (Handle, Token) and may carry Sets and Children; its Action
// and Args are set here.
func (e *Engine) RunUpload(ctx context.Context, req ActionRequest, f UploadFile, opts ...RenderOption) (*ActionResult, error) {
	var ro renderOptions
	for _, o := range opts {
		o(&ro)
	}
	if ro.err != nil {
		return nil, ro.err
	}
	body := f.Body
	if body == nil {
		body = http.NoBody
	}
	return e.upload(ctx, nil, req, &uploadBody{field: f.Field, name: f.Name, ctype: f.Type, size: f.Size, presign: f.Presign, body: body}, ro)
}

// upload runs one upload request as the action $upload: the middleware
// sees the field and the claimed name, type and size as Args; the core
// reads the bytes from the context.
func (e *Engine) upload(ctx context.Context, r *http.Request, req ActionRequest, ub *uploadBody, ro renderOptions) (*ActionResult, error) {
	res, _, err := e.uploadWith(ctx, r, req, ub, ro)
	return res, err
}

// uploadWith is upload, also returning the verified call, whose instance
// carries the handle and token the save made: a no-JavaScript form post
// chains its file parts and then its action through them.
func (e *Engine) uploadWith(ctx context.Context, r *http.Request, req ActionRequest, ub *uploadBody, ro renderOptions) (*ActionResult, *pending, error) {
	req.Action = actionUpload
	req.Args = []any{ub.field, map[string]any{"name": ub.name, "type": mediaBase(ub.ctype), "size": ub.size, uploadMetaPresign: ub.presign}}
	return e.actionWith(context.WithValue(ctx, uploadBodyKey{}, ub), r, req, ro)
}

// decodeHeaderJSON reads a URI-encoded JSON header into v; empty is fine.
func decodeHeaderJSON(h string, v any) bool {
	if h == "" {
		return true
	}
	s, err := url.PathUnescape(h)
	if err != nil {
		return false
	}
	return json.Unmarshal([]byte(s), v) == nil
}

// uploadsAttr is the JSON of data-brick-uploads: field → {accept,
// max_size, max_files, presign}, keys sorted by encoding/json.
func uploadsAttr(ups map[string]uploadSpec, sink UploadSink) (string, error) {
	type wire struct {
		Accept   []string `json:"accept,omitempty"`
		MaxSize  int64    `json:"max_size"`
		MaxFiles int      `json:"max_files"`
		Many     bool     `json:"many,omitempty"`
		Presign  bool     `json:"presign,omitempty"`
	}
	_, presign := sink.(UploadPresigner)
	out := make(map[string]wire, len(ups))
	for f, u := range ups {
		out[f] = wire{Accept: u.accept, MaxSize: u.maxSize, MaxFiles: u.maxFiles, Many: u.many, Presign: presign}
	}
	b, err := json.Marshal(out)
	return string(b), err
}
