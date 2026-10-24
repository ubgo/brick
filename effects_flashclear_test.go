package brick

import (
	"strings"
	"testing"
)

// clear_flash records a flash-clear effect: no level for every flash, or
// one Flash* level; anything else fails the action that asked. The kind name is the runtime's contract.
func TestClearFlash(t *testing.T) {
	t.Parallel()
	eff := &Effects{}
	if err := eff.ClearFlash(""); err != nil {
		t.Fatal(err)
	}
	if err := eff.ClearFlash(FlashWarning); err != nil {
		t.Fatal(err)
	}
	if err := eff.ClearFlash("loud"); err == nil || !strings.Contains(err.Error(), "not one of") {
		t.Fatalf("bad level: %v", err)
	}
	got := eff.List()
	if len(got) != 2 || got[0].Kind != "flash-clear" || got[0].Level != "" || got[1].Level != FlashWarning {
		t.Fatalf("effects = %+v", got)
	}
	var nilEff *Effects
	if err := nilEff.ClearFlash(""); err == nil {
		t.Fatal("clear_flash outside an action succeeded")
	}
}
