// Resuming a no-JS form after a post: the resume cookie, the Engine options that turn it on, and restoring the component instance a form belongs to.

package brick

import (
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
)

// SetFormResume writes the resume cookie for the page the visitor is sent
// to. It reports false, writing nothing, when the resume does not fit in a
// cookie (a large sealed handle, long error messages): the host then
// answers with the page itself (WithFormResume) instead of redirecting.
func (e *Engine) SetFormResume(w http.ResponseWriter, r *http.Request, fr FormResume) bool {
	b, err := json.Marshal(fr)
	if err != nil {
		return false
	}
	v := base64.RawURLEncoding.EncodeToString(b)
	if len(v) > maxFormCookie {
		return false
	}
	http.SetCookie(w, &http.Cookie{Name: formCookieName(r), Value: v, Path: "/", MaxAge: formCookieMaxAge, HttpOnly: true, Secure: requestIsHTTPS(r), SameSite: http.SameSiteLaxMode})
	return true
}

// FormResumeOption reads and clears the resume cookie a form post left: the
// render option for the page it redirected to (nil-safe: with no cookie it
// changes nothing). The cookie is untrusted; see FormResume.
func (e *Engine) FormResumeOption(w http.ResponseWriter, r *http.Request) RenderOption {
	name := formCookieName(r)
	c, err := r.Cookie(name)
	if err != nil {
		return func(*renderOptions) {}
	}
	http.SetCookie(w, &http.Cookie{Name: name, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: requestIsHTTPS(r), SameSite: http.SameSiteLaxMode})
	var fr FormResume
	b, err := base64.RawURLEncoding.DecodeString(c.Value)
	if err != nil || len(b) > maxFormCookie || json.Unmarshal(b, &fr) != nil {
		return func(*renderOptions) {}
	}
	return WithFormResume(fr)
}

// resumeFormInstance is the instance a form post left for the component
// with this id, or nil: it must load with the render's session, carry the
// token, and be this file on this page. Anything else renders fresh, as a
// lost instance does.
func (st *renderState) resumeFormInstance(callee *compiled, id string) (*Instance, map[string]any) {
	fr := st.formResume
	if fr == nil || fr.ID != id {
		return nil, nil
	}
	st.formResume = nil
	name, handle, ok := strings.Cut(fr.Handle, ".")
	store := st.e.stores[name]
	if !ok || store == nil {
		return nil, nil
	}
	hash := hashSession(st.session)
	inst, ok, err := store.Get(WithStateScope(st.ctx, StateScope{Store: name, Session: hash}), handle)
	if err != nil || !ok {
		return nil, nil
	}
	if subtle.ConstantTimeCompare([]byte(inst.Session), []byte(hash)) != 1 || subtle.ConstantTimeCompare([]byte(inst.Token), []byte(fr.Token)) != 1 {
		return nil, nil
	}
	if inst.ID != id || inst.Path != callee.path || (st.page != nil && inst.PagePath != st.page.Path) {
		return nil, nil
	}
	inst.Handle, inst.Store = handle, name
	st.e.migrateState(inst, callee)
	return inst, fr.Errors
}

// formCookieName is the resume cookie's name for this request.
func formCookieName(r *http.Request) string {
	if requestIsHTTPS(r) {
		return formCookieSecure
	}
	return formCookie
}

// FormResume names the instance a page shows after a no-JavaScript form
// post: its data-brick id, its handle and token as the root carries them,
// and the errors the action returned (which, unlike rule errors, are not
// stored with the instance). It is untrusted when it comes back in a
// cookie: the render loads the handle with the same session and token
// checks as an action, and uses it only for the component with that id,
// file and page.
type FormResume struct {
	ID     string         `json:"i"`
	Handle string         `json:"h"`
	Token  string         `json:"t"`
	Errors map[string]any `json:"e,omitempty"`
}

// WithFormResume renders the page with the instance a form post left,
// instead of a fresh one for the component with that id. The
// site layer passes it for a 422 answer, and FormResumeOption for the GET
// after a redirect.
func WithFormResume(fr FormResume) RenderOption {
	return func(o *renderOptions) {
		if fr.ID != "" && fr.Handle != "" {
			o.formResume = &fr
		}
	}
}
