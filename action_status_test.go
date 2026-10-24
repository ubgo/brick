package brick

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// ActionStatus is the status the default envelope answers with, for every
// error class, so middleware reporting it agrees with the client.
func TestActionStatusMatchesEnvelope(t *testing.T) {
	t.Parallel()
	e := newEngine(t, Options{})
	for err, want := range map[error]int{
		&ActionError{Status: http.StatusTooManyRequests}: http.StatusTooManyRequests,
		&ActionError{Status: 200}:                        http.StatusForbidden,
		fmt.Errorf("x: %w", ErrStoreUnavailable):         http.StatusServiceUnavailable,
		ErrGone:                                          http.StatusGone,
		ErrForbidden:                                     http.StatusForbidden,
		ErrUnknownAction:                                 http.StatusBadRequest,
		ErrUploadTooLarge:                                http.StatusRequestEntityTooLarge,
		ErrUploadType:                                    http.StatusUnsupportedMediaType,
		ErrBadValue:                                      http.StatusUnprocessableEntity,
		errors.New("script failed"):                      http.StatusUnprocessableEntity,
	} {
		if got := ActionStatus(err); got != want {
			t.Errorf("ActionStatus(%v) = %d, want %d", err, got, want)
		}
		w := httptest.NewRecorder()
		e.writeActionError(w, httptest.NewRequest(http.MethodPost, "/_brick/action", nil), err)
		if w.Code != want {
			t.Errorf("envelope for %v = %d, want %d", err, w.Code, want)
		}
	}
	if ActionStatus(nil) != http.StatusOK {
		t.Error("nil is not 200")
	}
}
