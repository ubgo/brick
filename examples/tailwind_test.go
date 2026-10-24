package main

import (
	"os"
	"strings"
	"testing"

	"github.com/ubgo/brick/tailwind"
)

// The Tailwind example's class candidates are what brick hashes to decide
// whether to run the binary.
func TestTailwindExample(t *testing.T) {
	got, err := tailwind.Candidates(os.DirFS("tailwind"), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []string{"bg-slate-50", "text-3xl", "font-bold", "px-8"} {
		if !strings.Contains(" "+strings.Join(got, " ")+" ", " "+c+" ") {
			t.Errorf("missing candidate %q in %v", c, got)
		}
	}
}
