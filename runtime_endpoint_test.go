package brick

import (
	"regexp"
	"strings"
	"testing"

	"github.com/ubgo/brick/runtime"
)

// TestRuntimeActionEndpoint pins the one route name the Go side and the
// browser runtime must agree on: ActionHandler is mounted at
// AssetBase + ActionEndpoint, and runtime.js derives the URL it posts to
// from its own script src. If either side renames the route, every action
// silently 404s in the browser while Go tests stay green.
func TestRuntimeActionEndpoint(t *testing.T) {
	js := string(runtime.JS)
	fn := regexp.MustCompile(`(?s)const actionURL = \(\) => \{.*?\n  \};`).FindString(js)
	if fn == "" {
		t.Fatal("runtime.js: actionURL function not found")
	}
	if want := `, "` + ActionEndpoint + `")`; !strings.Contains(fn, want) {
		t.Errorf("actionURL does not replace the script name with %q:\n%s", ActionEndpoint, fn)
	}
	if want := `"` + DefaultAssetBase + ActionEndpoint + `"`; !strings.Contains(fn, want) {
		t.Errorf("actionURL fallback is not %s:\n%s", want, fn)
	}
}
