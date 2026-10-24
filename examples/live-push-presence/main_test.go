package main

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/ubgo/brick"
	"github.com/ubgo/brick/bricktest"
)

// The whole flow over a real server with real event streams;
// testdata/transcript.txt is the output docs/guides/live/push-and-presence.md
// shows under "Try it".
func TestTranscript(t *testing.T) {
	a, err := newApp("theme")
	if err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	if err := demo(&out, a.handler()); err != nil {
		t.Fatalf("%v\nso far:\n%s", err, out.String())
	}
	want, err := os.ReadFile("testdata/transcript.txt")
	if err != nil {
		t.Fatal(err)
	}
	if out.String() != string(want) {
		t.Fatalf("transcript:\n%s\nwant:\n%s", out.String(), want)
	}
}

// The pitfalls the guide lists, each with the error a user sees.
func TestPitfalls(t *testing.T) {
	ctx := context.Background()
	push, err := brick.NewPush(brick.PushOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Run("listen without Options.Push", func(t *testing.T) {
		eng := bricktest.Engine(t, brick.Options{States: brick.NewMemoryStates(0, 0)}, bricktest.Theme(
			"components/L.html", "---\nstate: {}\nbrick: {listen: orders}\n---\n<p>x</p>",
			"content/index.html", `<L />`,
		))
		_, err := eng.Render(ctx, "content/index.html", nil)
		mustMention(t, err, "server push is off (Options.Push)")
	})
	t.Run("listen without state", func(t *testing.T) {
		eng := bricktest.Engine(t, brick.Options{Push: push, States: brick.NewMemoryStates(0, 0)}, bricktest.Theme(
			"components/L.html", "---\nbrick: {listen: orders}\n---\n<p>x</p>",
			"content/index.html", `<L />`,
		))
		_, err := eng.Render(ctx, "content/index.html", nil)
		mustMention(t, err, "needs state: — a pushed topic refreshes the component, which is an action")
	})
	t.Run("engine topic", func(t *testing.T) {
		eng := bricktest.Engine(t, brick.Options{Push: push}, bricktest.Theme("content/index.html", `<p>x</p>`))
		mustMention(t, eng.Publish(ctx, "brick:revoke:x"), `topics starting with "brick:" are the engine's own`)
	})
	t.Run("bad topic", func(t *testing.T) {
		eng := bricktest.Engine(t, brick.Options{Push: push}, bricktest.Theme("content/index.html", `<p>x</p>`))
		mustMention(t, eng.Publish(ctx, "order 7"), "is not a topic (1-200 characters from A-Z a-z 0-9 _ . : / @ -)")
	})
}

func mustMention(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("err = %v, want it to mention %q", err, want)
	}
}
