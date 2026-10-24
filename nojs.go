package brick

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"strings"

	"github.com/ubgo/brick/internal/weburl"
)

// Forms without JavaScript.
//
// Why it exists: a <form brick:submit="save"> does nothing useful while the
// runtime is not running (scripts off, a script that failed to load, a
// crawler, a slow first paint): the browser submits it as a GET to the same
// page and the action never runs. With this fallback the same markup posts
// as an ordinary HTML form, the action runs through the same pipeline as a
// JSON action, and the visitor lands on the page re-rendered with the new
// state (POST-redirect-GET), or with the action's errors and the typed
// values (422).
//
// How the pieces fit:
//   - rewriteForms rewrites every brick:submit form inside a stateful root
//     in the rendered HTML: method="post" (no action attribute, so it posts
//     to the page's own URL), hidden fields carrying exactly what the
//     runtime sends (the handle, the token and the action call), and a name
//     on every brick:model control so the browser sends it at all. The
//     runtime drops every field whose name starts with "brick:" from the
//     dict it passes, so the JS path is unchanged.
//   - Engine.FormPost runs such a post: the CSRF check, the body, the
//     fields mapped as the runtime maps them (named fields as the last
//     argument, brick:model controls as sets), then Engine.action, so the
//     token, session, locked fields, middleware and rules all apply.
//   - Files: a form holding a brick:upload file input also
//     gets enctype="multipart/form-data" and its instance fields first,
//     and FormPost streams each file part through the $upload pseudo-
//     action (formUploads) before the form's action runs, so a file meets
//     the same middleware, limits, sniffing and sink as a runtime upload.
//   - The answer: 303 back to the page with a short-lived cookie naming the
//     new handle (FormResume), or the page itself with 422 when the action
//     returned errors (the site layer renders it). The next page render
//     resumes that instance instead of making a fresh one.
//
// Invariants:
//   - Nothing new is trusted: the hidden fields are the same untrusted
//     handle, token and action name the JSON path receives, checked by the
//     same code. A model field's kind (checkbox, list, value) only shapes
//     the value; the type gate of $set decides whether it lands.
//   - A form post is cross-site capable, so FormPost refuses one whose
//     Sec-Fetch-Site is not same-origin, or, without that header, whose
//     Origin is not this host (formOriginOK). The token is the second lock,
//     as on the JSON path.
//   - The page is the redirect target: the request's own URL, never a
//     Referer or a field, so there is nothing to make an open redirect of.
//     An action's redirect effect may replace it, already checked by the
//     effects' open-redirect lock.
//   - An author's choice wins: a form that writes its own action or method
//     is left alone (a classic POST to a host route, the submit effect), and
//     so is one with brick:confirm or brick:confirm.prompt (without the
//     runtime nobody can be asked, and a destructive action must not run
//     unasked) or a client-only
//     call ($parent.fn, $dispatch, $cancelUpload).

// The reserved form fields. Every name starts with "brick:", which the
// runtime filters out of the fields it passes to an action.
const (
	formFieldState  = "brick:state"
	formFieldToken  = "brick:token"
	formFieldAction = "brick:action"
	// formFieldModels lists the form's brick:model controls as a JSON array
	// of [kind, path, name]: name is "" when the engine named the control
	// itself (formModelPrefix + path).
	formFieldModels = "brick:models"
	formModelPrefix = "brick:model:"
	// formFieldUploads maps the form's upload parts to their fields as a
	// JSON object {part name: field}; formUploadPrefix names a file input
	// the engine named itself (formUploadPrefix + field). Both travel in
	// the hidden block at the START of an upload form: the
	// browser sends parts in tree order, and the server must know the
	// instance before a file part arrives, so it can stream it straight
	// into the sink instead of spooling it.
	formFieldUploads = "brick:uploads"
	formUploadPrefix = "brick:upload:"
	formEnctype      = "multipart/form-data"
	// formPrefix is what every reserved field starts with.
	formPrefix = "brick:"
)

// Model kinds in formFieldModels: how a control's posted values become one
// field value, mirroring the runtime's valueOf and fieldsOf.
const (
	modelValue = "v" // the last posted value; absent leaves the field alone
	modelCheck = "c" // a checkbox bound to a bool: present is true, absent false
	modelList  = "a" // a checkbox group or <select multiple>: every value, absent is []
	modelRadio = "r" // a radio group: the checked value; none leaves the field alone
)

// The resume cookie. On HTTPS it takes the __Host- prefix, which a sibling
// subdomain cannot set, so a planted cookie cannot hand a visitor someone
// else's instance there; on plain HTTP the session binding is the lock.
const (
	formCookie       = "brick-form"
	formCookieSecure = "__Host-brick-form"
	// formCookieMaxAge is long enough for the redirect's GET and nothing
	// else; the cookie is cleared by the first page that reads it.
	formCookieMaxAge = 60
	// maxFormCookie keeps the cookie under the 4096 bytes every browser
	// stores, with room for its name and attributes. A sealed handle can
	// be larger; the site layer then answers with the page itself.
	maxFormCookie = 3800
)

// Bounds of a multipart form post. File bytes bound for an
// upload field are limited by that field's max_size while they stream;
// everything else the body holds is limited by Options.MaxActionBytes.
const (
	// formReadSlack is how far the raw read may run ahead of what the
	// parts handed out: multipart reads through a 4 KiB buffer, so file
	// bytes still in it are not yet counted as file bytes.
	formReadSlack = 64 << 10
	// formDrainBytes is how much of the rest of a refused post is read
	// and discarded, so the browser, still sending, receives the 422 page
	// instead of a reset connection. Nothing is kept; a larger rest is
	// cut off and the connection closed.
	formDrainBytes = 64 << 20
)

// clientOnlyCalls are brick:submit calls only the runtime can perform: a
// form using one is not rewritten, and FormPost refuses one.
var clientOnlyCalls = []string{actionDispatch, "$cancelUpload", "$parent."}

// FormPostResult is what a no-JavaScript form post asks the host to answer.
type FormPostResult struct {
	// Location is where the visitor goes: the posting page (the request's
	// own URL, with any query-synced field changes applied), or an
	// action's redirect or navigate target, already checked by the
	// open-redirect lock.
	Location string
	// Resume is the instance to show there. Empty when the instance was
	// gone (the page renders fresh, as a JSON action recovers).
	Resume FormResume
	// Failed is set when the action returned errors: answer with the page
	// rendered with Resume and status 422 rather than redirecting, so the
	// errors and the typed values show (the site layer does).
	Failed bool
	// Effect is set when Location is an action's redirect or navigate
	// target rather than the posting page: a site under a base path
	// prefixes it, as it does a loader's redirect.
	Effect bool
}

// FormError answers a failed form post: Options.ActionErrors when set,
// else the action endpoint's status and plain message (no file paths in
// Prod), as for a JSON action.
func (e *Engine) FormError(w http.ResponseWriter, r *http.Request, err error) {
	if e.opts.ActionErrors != nil {
		e.opts.ActionErrors(w, r, err)
		return
	}
	e.writeActionError(w, r, err)
}

// IsFormPost reports whether r is a no-JavaScript brick:submit post: a POST
// with a form body (urlencoded or multipart) carrying the brick:action
// field. It reads the body (a urlencoded one whole, a multipart one up to
// its first file part bound for an upload field, so no file byte is read
// before FormPost's CSRF check; see parseFormBody), so call it before
// anything else reads r.Body. A host with its own router sends such
// requests to the page's URL to FormPost.
func (e *Engine) IsFormPost(r *http.Request) bool {
	if r.Method != http.MethodPost || e.opts.NoFormPosts {
		return false
	}
	mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if mt != "application/x-www-form-urlencoded" && mt != "multipart/form-data" {
		return false
	}
	if err := e.parseFormBody(r); err != nil {
		// A body FormPost cannot read is still a form post: it answers 400.
		return true
	}
	_, ok := r.PostForm[formFieldAction]
	if rest, isRest := r.Body.(*formRest); isRest && rest.part != nil {
		// An upload part came first: FormPost answers 400 when the
		// instance fields did not precede it.
		return true
	}
	return ok
}

// FormPost runs a no-JavaScript brick:submit post and reports
// where the visitor goes and what they see there. Errors are the action's
// (ActionStatus maps them), plus ErrForbidden for a cross-site post; an
// instance that is gone is not an error: Location is the page and Resume
// is empty, so the page renders fresh.
//
// The fields map as the runtime maps them: the call's written arguments,
// then the form's named fields as one dict (the last value of a repeated
// name, as Object.fromEntries(new FormData(form)) keeps it) when there are
// any; every brick:model control in the form as a set, applied first
// through the $set gate, as the JS path's $set and deferred sets are.
func (e *Engine) FormPost(r *http.Request) (*FormPostResult, error) {
	if e.opts.NoFormPosts {
		return nil, &ActionError{Status: http.StatusForbidden, Message: "brick: form posts are off (Options.NoFormPosts)"}
	}
	if !formOriginOK(r) {
		return nil, fmt.Errorf("%w: a form post must come from this site's own pages (Sec-Fetch-Site or Origin)", ErrForbidden)
	}
	if err := e.parseFormBody(r); err != nil {
		var mb *http.MaxBytesError
		if errors.As(err, &mb) {
			return nil, &ActionError{Status: http.StatusRequestEntityTooLarge, Message: "brick: the form is larger than Options.MaxActionBytes"}
		}
		return nil, errFormBody
	}
	if rest, ok := r.Body.(*formRest); ok && rest.part != nil {
		if out, err := e.formUploads(r, rest); out != nil || err != nil {
			return out, err
		}
	}
	req, err := formRequest(r.PostForm)
	if err != nil {
		return nil, err
	}
	res, p, err := e.actionWith(r.Context(), r, req, renderOptions{})
	out := &FormPostResult{Location: r.URL.RequestURI()}
	if errors.Is(err, ErrGone) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	out.Location, out.Effect = formLocation(r, res.Effects)
	inst := p.inst
	out.Resume = FormResume{ID: inst.ID, Handle: inst.Store + "." + inst.Handle, Token: inst.Token, Errors: p.formErrors}
	out.Failed = len(p.formErrors) > 0
	return out, nil
}

// formInstanceID is the data-brick id of the instance a handle names, read
// with the same session and token checks as an action, or "": the 422 page
// of a post whose first file was refused shows that instance with the
// error, not a fresh one.
func (e *Engine) formInstanceID(r *http.Request, handle, token string) string {
	name, h, ok := strings.Cut(handle, ".")
	store := e.stores[name]
	if !ok || store == nil {
		return ""
	}
	var session string
	if e.opts.Sessions != nil {
		s, err := e.opts.Sessions.Session(r)
		if err != nil {
			return ""
		}
		session = s
	}
	hash := hashSession(session)
	inst, ok, err := store.Get(WithStateScope(r.Context(), StateScope{Store: name, Session: hash}), h)
	if err != nil || !ok || subtle.ConstantTimeCompare([]byte(inst.Session), []byte(hash)) != 1 || subtle.ConstantTimeCompare([]byte(inst.Token), []byte(token)) != 1 {
		return ""
	}
	return inst.ID
}

// formRequest builds the action request from a form body.
func formRequest(form url.Values) (ActionRequest, error) {
	handle, token, call := form.Get(formFieldState), form.Get(formFieldToken), form.Get(formFieldAction)
	if handle == "" || token == "" || call == "" {
		return ActionRequest{}, errFormBody
	}
	name, args, err := parseFormCall(call)
	if err != nil {
		return ActionRequest{}, err
	}
	sets, err := formSets(form)
	if err != nil {
		return ActionRequest{}, err
	}
	fields := map[string]any{}
	for k, vs := range form {
		if strings.HasPrefix(k, formPrefix) || len(vs) == 0 {
			continue
		}
		fields[k] = vs[len(vs)-1]
	}
	if len(fields) > 0 {
		args = append(args, fields)
	}
	return ActionRequest{Handle: handle, Token: token, Action: name, Args: args, Sets: sets}, nil
}

// parseFormCall reads the brick:submit value as the runtime's parseCall
// does: a name, then JSON literal arguments.
func parseFormCall(call string) (string, []any, error) {
	m := actionCallRE.FindStringSubmatch(call)
	if m == nil {
		return "", nil, fmt.Errorf("%w: %q is not an action call", ErrUnknownAction, call)
	}
	if isClientOnlyCall(m[1]) {
		return "", nil, fmt.Errorf("%w: %s runs in the browser only", ErrUnknownAction, m[1])
	}
	var args []any
	if a := strings.TrimSpace(m[2]); a != "" {
		dec := json.NewDecoder(strings.NewReader("[" + a + "]"))
		dec.UseNumber()
		if err := dec.Decode(&args); err != nil {
			return "", nil, fmt.Errorf("%w: %q: arguments must be JSON literals", ErrUnknownAction, call)
		}
		for i, v := range args {
			args[i] = normalizeJSON(v)
		}
	}
	return m[1], args, nil
}

func isClientOnlyCall(name string) bool {
	for _, c := range clientOnlyCalls {
		if name == c || (strings.HasSuffix(c, ".") && strings.HasPrefix(name, c)) {
			return true
		}
	}
	return false
}

// formSets turns the form's brick:model controls into field sets, in the
// order the form lists them.
func formSets(form url.Values) ([]FieldSet, error) {
	raw := form.Get(formFieldModels)
	if raw == "" {
		return nil, nil
	}
	var models [][3]string
	if err := json.Unmarshal([]byte(raw), &models); err != nil {
		return nil, fmt.Errorf("%w: %s is not a list of [kind, path, name]", ErrUnknownAction, formFieldModels)
	}
	var sets []FieldSet
	for _, m := range models {
		kind, path, name := m[0], m[1], m[2]
		if name == "" {
			name = formModelPrefix + path
		}
		vs, present := form[name]
		switch kind {
		case modelValue, modelRadio:
			if present && len(vs) > 0 {
				sets = append(sets, FieldSet{Path: path, Value: vs[len(vs)-1]})
			}
		case modelCheck:
			sets = append(sets, FieldSet{Path: path, Value: present})
		case modelList:
			list := make([]any, 0, len(vs))
			for _, v := range vs {
				list = append(list, v)
			}
			sets = append(sets, FieldSet{Path: path, Value: list})
		default:
			return nil, fmt.Errorf("%w: %s: unknown kind %q", ErrUnknownAction, formFieldModels, kind)
		}
	}
	return sets, nil
}

// formOriginOK is the CSRF check of a form post. A browser that sends
// Sec-Fetch-Site (every current one) must say same-origin: same-site would
// let a sibling subdomain post. Without it, Origin must name this host.
// With neither the post is refused: it is not from a page of this site.
func formOriginOK(r *http.Request) bool {
	if s := r.Header.Get("Sec-Fetch-Site"); s != "" {
		return s == "same-origin"
	}
	o := r.Header.Get("Origin")
	if o == "" || o == "null" {
		return false
	}
	u, err := url.Parse(o)
	if err != nil || !weburl.Is(u) {
		return false
	}
	return strings.EqualFold(u.Host, r.Host)
}

// formLocation is where the visitor goes after a successful post: an
// action's redirect or navigate target (terminal, already checked by the
// open-redirect lock), else the page that posted — the request's own URL —
// with any query-synced field changes applied, as the runtime applies the
// query effect to the address bar.
func formLocation(r *http.Request, effects []Effect) (string, bool) {
	u := *r.URL
	for _, ef := range effects {
		switch ef.Kind {
		case EffectRedirect, EffectNavigate:
			return ef.URL, true
		case EffectQuery:
			q := u.Query()
			if set, ok := ef.Data["set"].(map[string]any); ok {
				for k, v := range set {
					q.Del(k)
					switch x := v.(type) {
					case []any:
						for _, s := range x {
							q.Add(k, fmt.Sprint(s))
						}
					default:
						q.Set(k, fmt.Sprint(x))
					}
				}
			}
			if del, ok := ef.Data["del"].([]any); ok {
				for _, k := range del {
					q.Del(fmt.Sprint(k))
				}
			}
			u.RawQuery = q.Encode()
		}
	}
	return u.RequestURI(), false
}

// requestIsHTTPS reports a TLS request, directly or behind a proxy that
// says so. A forged X-Forwarded-Proto only picks a cookie name the browser
// then refuses to store over HTTP: the resume is lost, nothing more.
func requestIsHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	p, _, _ := strings.Cut(r.Header.Get("X-Forwarded-Proto"), ",")
	return strings.EqualFold(strings.TrimSpace(p), weburl.SchemeHTTPS)
}

// ---- the HTML rewrite -------------------------------------------------

// formsEnabled reports whether this render rewrites forms: not in a static
// export (nothing serves the post) and not with Options.NoFormPosts.
func (e *Engine) formsEnabled(ro renderOptions) bool {
	return !e.opts.NoFormPosts && !ro.staticState
}
