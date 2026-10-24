package brick_test

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/ubgo/brick"
	"github.com/ubgo/brick/bricktest"
)

// brick: {log:} reaches every action middleware on ActionCall.Log (G18):
// unset and true are nil (the logger's default), false is Off, a level
// word is that level.
func TestActionLogReachesMiddleware(t *testing.T) {
	var seen []*brick.ActionLog
	spy := func(next brick.ActionFunc) brick.ActionFunc {
		return func(ctx context.Context, a *brick.ActionCall) (*brick.ActionResult, error) {
			seen = append(seen, a.Log)
			return next(ctx, a)
		}
	}
	comp := func(log string) string {
		return "---\nstate:\n  n: 0\nbrick:\n  log: " + log + "\n---\n<b>{{ .State.n }}</b>"
	}
	eng := bricktest.Engine(t, brick.Options{States: brick.NewMemoryStates(0, 0), ActionMiddleware: []brick.ActionMiddleware{spy}}, bricktest.Theme(
		"components/Plain.html", "---\nstate:\n  n: 0\n---\n<b>{{ .State.n }}</b>",
		"components/On.html", comp("true"),
		"components/Quiet.html", comp("false"),
		"components/Chatty.html", comp("debug"),
		"content/a.html", `<main><Plain /><On /><Quiet /><Chatty /></main>`,
	))
	live := bricktest.Live(t, eng, "content/a.html")
	for _, name := range []string{"Plain", "On", "Quiet", "Chatty"} {
		live.Component(name).Set("n", 1)
	}
	if len(seen) != 4 || seen[0] != nil || seen[1] != nil {
		t.Fatalf("unset/true: %+v", seen)
	}
	if !seen[2].Off {
		t.Fatalf("log: false: %+v", seen[2])
	}
	if seen[3].Off || seen[3].Level != slog.LevelDebug {
		t.Fatalf("log: debug: %+v", seen[3])
	}
}

// A value outside true, false and the level words is a compile error that
// lists the accepted words.
func TestActionLogRejectsUnknown(t *testing.T) {
	for _, v := range []string{"verbose", "1", "[info]"} {
		eng, err := brick.New(brick.Options{FS: bricktest.Theme(
			"components/C.html", "---\nstate:\n  n: 0\nbrick:\n  log: "+v+"\n---\n<b></b>",
			"content/a.html", "<C />",
		), States: brick.NewMemoryStates(0, 0)})
		if err == nil {
			_, err = eng.Render(context.Background(), "content/a.html", nil)
		}
		if err == nil || !strings.Contains(err.Error(), "debug, info, warn, error") {
			t.Errorf("log: %s: %v", v, err)
		}
	}
}
