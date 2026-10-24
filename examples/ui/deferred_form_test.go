package main

import (
	"net/http"
	"testing"
)

// The deferred-form page renders its demo with the
// markup the runtime reads: deferred bindings, the list-valued path the
// engine stamps for the checkbox group, the disable-with label and the
// confirm message.
func TestDeferredFormPage(t *testing.T) {
	_, h := showcase(t)
	const url = "/components/deferred-form/"
	code, body := get(t, h, url)
	if code != http.StatusOK {
		t.Fatalf("%s: status %d", url, code)
	}
	mustContain(t, url, body,
		`brick:model.defer="name"`,
		`brick:model.defer="tags" value="rust"`,
		`data-brick-arrays="tags"`,
		`brick:disable-with="Saving…"`,
		`brick:confirm="Clear the form and everything saved?"`,
		`brick:loading.delay="save"`,
	)
}
