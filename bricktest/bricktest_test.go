package bricktest

import (
	"context"
	"testing"

	"github.com/ubgo/brick"
)

func TestGoldenCard(t *testing.T) {
	t.Parallel()
	eng := Engine(t, brick.Options{}, Theme(
		"components/Card.html", "---\nprops:\n  title: string\n---\n<div class=\"card\"><h3>{{ .Props.title }}</h3>{{ children }}</div>",
	))
	out, err := eng.RenderComponent(context.Background(), "Card", map[string]any{"title": "Hi <you>"}, "<p>body</p>")
	if err != nil {
		t.Fatal(err)
	}
	Golden(t, "card", out)
}
