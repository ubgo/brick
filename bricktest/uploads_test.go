package bricktest

import (
	"errors"
	"strings"
	"testing"

	"github.com/ubgo/brick"
)

func TestMemoryUploads(t *testing.T) {
	sink := NewMemoryUploads()
	eng := Engine(t, brick.Options{States: brick.NewMemoryStates(0, 0), Uploads: sink}, Theme(
		"components/Avatar.html", "---\nstate:\n  avatar: null\nbrick:\n  uploads:\n    avatar: {accept: [image/png], max_size: 64}\n---\n<div>{{ with .State.avatar }}<img src=\"{{ .url }}\" alt=\"{{ .name }}\">{{ end }}</div>",
		"content/a.html", "<Avatar />",
	))
	live := Live(t, eng, "content/a.html")
	png := []byte("\x89PNG\r\n\x1a\n0000")
	live.Component("Avatar").Upload("avatar", "me.png", "image/png", png)
	if html := live.Component("Avatar").HTML(); !strings.Contains(html, `<img src="/uploads/u1" alt="me.png">`) {
		t.Fatalf("html = %s", html)
	}
	if b, ref, ok := sink.File("u1"); !ok || string(b) != string(png) || ref.Type != "image/png" {
		t.Fatalf("file = %q %+v %v", b, ref, ok)
	}
	_, err := live.Component("Avatar").TryUpload("avatar", "big.png", "image/png", append(png, make([]byte, 100)...))
	// Refused on its declared size, before the sink was opened.
	if !errors.Is(err, brick.ErrUploadTooLarge) || sink.Len() != 1 {
		t.Fatalf("over size: %v, files %d", err, sink.Len())
	}
}
