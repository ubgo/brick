// Reading a no-JS form post body: the multipart and urlencoded readers, file parts, and the upload fields an action receives.

package brick

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
)

// formUploads streams the file parts of a multipart form post. Each part runs as the $upload pseudo-action, exactly as the
// runtime's upload request does — ActionMiddleware, the handle, token and
// session, the field's uploads: limits, the sniffed type, the size counted
// while streaming, the sink, the rules — and the instance's new handle and
// token carry into the next part and then the form's action, which
// FormPost runs as before. Text parts after a file are read as they come.
//
// A refused file (too large, wrong type, no room) stops the post before
// the action runs: the answer is the page with 422 and the refusal as the
// field's error, showing the instance as the earlier parts left it. It
// returns nil, nil when every part was stored and the action should run.
func (e *Engine) formUploads(r *http.Request, rest *formRest) (*FormPostResult, error) {
	form := r.PostForm
	handle, token := form.Get(formFieldState), form.Get(formFieldToken)
	if handle == "" || token == "" || form.Get(formFieldAction) == "" {
		rest.drain()
		return nil, errFormBody
	}
	page := r.URL.RequestURI()
	var last *Instance
	// one holds the one-file fields a part already filled: a second part
	// for one in the same post is refused, so a post cannot stream file
	// after file into the sink.
	one := map[string]bool{}
	for part := rest.part; part != nil; {
		field := uploadFieldOf(form, part.FormName())
		var err error
		var p *pending
		if one[field] {
			err = fmt.Errorf("%w: %s holds one file", ErrBadValue, field)
		} else {
			ub := &uploadBody{field: field, name: part.FileName(), ctype: part.Header.Get("Content-Type"), body: uploadPart{p: part, body: rest.body}}
			_, p, err = e.uploadWith(r.Context(), r, ActionRequest{Handle: handle, Token: token}, ub, renderOptions{})
		}
		switch {
		case errors.Is(err, ErrGone):
			rest.drain()
			return &FormPostResult{Location: page}, nil
		case errors.Is(err, ErrUploadTooLarge), errors.Is(err, ErrUploadType), errors.Is(err, ErrBadValue):
			rest.drain()
			out := &FormPostResult{Location: page, Failed: true}
			id := ""
			if last != nil {
				id = last.ID
			} else {
				id = e.formInstanceID(r, handle, token)
			}
			if id != "" {
				out.Resume = FormResume{ID: id, Handle: handle, Token: token, Errors: map[string]any{field: strings.TrimPrefix(err.Error(), "brick: ")}}
			}
			return out, nil
		case err != nil:
			rest.drain()
			return nil, err
		}
		last = p.inst
		handle, token = p.inst.Store+"."+p.inst.Handle, p.inst.Token
		if spec, ok := p.c.meta.uploads[field]; ok && !spec.many {
			one[field] = true
		}
		// What the sink did not read of the part (none, unless it stopped
		// early) is skipped by NextPart.
		part, err = rest.readParts(form)
		if err != nil {
			rest.drain()
			var mb *http.MaxBytesError
			if errors.As(err, &mb) {
				return nil, &ActionError{Status: http.StatusRequestEntityTooLarge, Message: "brick: the form is larger than Options.MaxActionBytes"}
			}
			return nil, errFormBody
		}
	}
	form.Set(formFieldState, handle)
	form.Set(formFieldToken, token)
	return nil, nil
}

// parseFormBody reads the body once. A urlencoded body is read whole, at
// most MaxActionBytes. A multipart body is read part by part: text parts land in r.PostForm, a file part that is not an upload is
// discarded, and both count against MaxActionBytes; at the first file part
// bound for an upload field it stops, leaving that part unread in the
// formRest that replaces r.Body, so the file streams later, once, into the
// sink. A second call returns the first call's error.
func (e *Engine) parseFormBody(r *http.Request) error {
	if r.PostForm != nil {
		if rest, ok := r.Body.(*formRest); ok {
			return rest.err
		}
		return nil
	}
	mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if mt == formEnctype {
		r.PostForm = url.Values{}
		limit := int64(e.opts.MaxActionBytes)
		body := &formBody{r: r.Body, limit: limit + formReadSlack}
		orig := r.Body
		r.Body = io.NopCloser(body)
		mr, err := r.MultipartReader()
		rest := &formRest{ReadCloser: orig, body: body, mr: mr, limit: limit}
		r.Body = rest
		if err != nil {
			rest.err = err
			return err
		}
		rest.part, rest.err = rest.readParts(r.PostForm)
		return rest.err
	}
	r.Body = http.MaxBytesReader(nil, r.Body, int64(e.opts.MaxActionBytes))
	if err := r.ParseForm(); err != nil {
		r.PostForm = map[string][]string{}
		return err
	}
	return nil
}

// formBody is the raw multipart body. It refuses to read on once more
// than limit bytes went to anything but upload parts (n - uploaded), so a
// huge text field, a discarded file or a header bomb is cut off like an
// oversized urlencoded body, while an upload's bytes are bounded by its
// field's max_size instead.
type formBody struct {
	r           io.Reader
	n, uploaded int64
	limit       int64
}

// Read refuses once the non-upload bytes pass limit, as MaxBytesReader
// would, so the caller answers 413.
func (b *formBody) Read(p []byte) (int, error) {
	if b.n-b.uploaded > b.limit {
		return 0, &http.MaxBytesError{Limit: b.limit - formReadSlack}
	}
	n, err := b.r.Read(p)
	b.n += int64(n)
	return n, err
}

// formRest is a multipart form post read up to its first upload part. It
// replaces r.Body, so FormPost finds it by type, which no client can
// forge; Close closes the original body.
type formRest struct {
	io.ReadCloser
	body  *formBody
	mr    *multipart.Reader
	part  *multipart.Part
	limit int64
	err   error
}

// readParts reads parts into form until the body ends (nil part) or a
// file part bound for an upload field arrives, which it returns unread.
func (rest *formRest) readParts(form url.Values) (*multipart.Part, error) {
	for {
		p, err := rest.mr.NextPart()
		if errors.Is(err, io.EOF) {
			return nil, nil
		}
		if err != nil {
			return nil, rest.bodyErr(err)
		}
		name := p.FormName()
		if p.FileName() != "" {
			if uploadFieldOf(form, name) != "" {
				return p, nil
			}
			// Not an upload: discarded, as v16 did, but counted.
			if _, err := io.Copy(io.Discard, p); err != nil {
				return nil, rest.bodyErr(err)
			}
			continue
		}
		b, err := io.ReadAll(p)
		if err != nil {
			return nil, rest.bodyErr(err)
		}
		if isFilePart(p) && uploadFieldOf(form, name) != "" {
			// An upload input with no file chosen: nothing to send.
			continue
		}
		if name != "" {
			form.Add(name, string(b))
		}
	}
}

// isFilePart reports a part from a file input, even one with no file
// chosen, which a browser sends with an empty filename.
func isFilePart(p *multipart.Part) bool {
	_, params, err := mime.ParseMediaType(p.Header.Get("Content-Disposition"))
	_, ok := params["filename"]
	return err == nil && ok
}

// bodyErr keeps a size refusal recognisable under multipart's wrapping.
func (rest *formRest) bodyErr(err error) error {
	if rest.body.n-rest.body.uploaded > rest.body.limit {
		return &http.MaxBytesError{Limit: rest.limit}
	}
	return err
}

// uploadPart is a file part's bytes as an upload's body.
type uploadPart struct {
	p    *multipart.Part
	body *formBody
}

// Read counts what it reads as upload bytes, so a file never uses up
// MaxActionBytes; its own field's max_size bounds it instead.
func (u uploadPart) Read(b []byte) (int, error) {
	n, err := u.p.Read(b)
	u.body.uploaded += int64(n)
	return n, err
}

// drain reads and discards the rest of a post the engine stopped reading,
// up to formDrainBytes, so the browser gets the answer.
func (rest *formRest) drain() {
	_, _ = io.Copy(io.Discard, io.LimitReader(rest.ReadCloser, formDrainBytes))
}

// uploadFieldOf is the upload field a file part is bound for: the field
// in an engine-made name, or the one the form's brick:uploads maps an
// authored name to; "" for any other part. It only routes bytes: whether
// the field takes files, and which, is the component's uploads: decision.
func uploadFieldOf(form url.Values, name string) string {
	if f, ok := strings.CutPrefix(name, formUploadPrefix); ok {
		return f
	}
	raw := form.Get(formFieldUploads)
	if raw == "" {
		return ""
	}
	var m map[string]string
	if json.Unmarshal([]byte(raw), &m) != nil {
		return ""
	}
	return m[name]
}

// errFormBody is a form post FormPost cannot read.
var errFormBody = fmt.Errorf("%w: a form post needs the brick:state, brick:token and brick:action fields", ErrUnknownAction)
