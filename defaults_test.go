package brick

import "testing"

// TestDefaultsReturnFreshSlices pins why these are functions: mutating a
// returned slice must not change the default that the next call (and so
// the next engine) sees.
func TestDefaultsReturnFreshSlices(t *testing.T) {
	roots := DefaultRoots()
	roots[0] = "mutated"
	if got := DefaultRoots()[0]; got != "components" {
		t.Errorf("DefaultRoots()[0] = %q after mutation, want components", got)
	}
	dirs := DefaultImageDirs()
	dirs[0] = "mutated"
	if got := DefaultImageDirs()[0]; got != "static" {
		t.Errorf("DefaultImageDirs()[0] = %q after mutation, want static", got)
	}
	widths := DefaultImageWidths()
	widths[0] = -1
	if got := DefaultImageWidths()[0]; got != 480 {
		t.Errorf("DefaultImageWidths()[0] = %d after mutation, want 480", got)
	}
}
