package main

import (
	"net/http"
	"strings"
	"testing"
)

// The validated-form page renders its demo with
// lazy bindings and no errors before anything was touched: declared rules
// show nothing until a field is sent or the submit validates.
func TestValidatedFormPage(t *testing.T) {
	_, h := showcase(t)
	const url = "/components/validated-form/"
	code, body := get(t, h, url)
	if code != http.StatusOK {
		t.Fatalf("%s: status %d", url, code)
	}
	mustContain(t, url, body,
		`brick:model.lazy="email"`,
		`brick:submit="signup"`,
		`Touched: nothing yet`,
	)
	if strings.Contains(body, "This field is required.") {
		t.Fatalf("%s: an untouched form shows a rule error", url)
	}
}
