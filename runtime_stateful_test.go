package brick

import (
	"context"
	"strings"
	"testing"
)

// statefulNoClient is a counter with state and a brick:click but no client
// module of its own: the only thing that wires its button is the runtime.
const statefulNoClient = "---\nstate:\n  count: 0\n---\n<div><span>{{ .State.count }}</span><button brick:click=\"increment\">+</button></div>"

// runtimeTag is the start of the runtime's script URL in {{ assets }}.
const runtimeTag = "/_brick/c/" + runtimeName + "."

// A page whose only interactive part is a stateful component links the
// runtime, wherever the component sits: at the page root, inside a plain
// component, inside {{ range }}, and in an island filled later.
func TestRuntimeLinkedForStatefulComponent(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, page string }{
		{"root", `<Counter />{{ assets }}`},
		{"nested in a plain component", `<Box />{{ assets }}`},
		{"inside range", `{{ range (list 1 2) }}<Counter />{{ end }}{{ assets }}`},
		{"inside a plain island", `<Isl />{{ assets }}`},
		// brick:render picks the stateful component at fill time, so no
		// static walk of the page can see it (the runtime must still link).
		{"chosen by brick:render in a plain island", `<Pick />{{ assets }}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			e := newEngine(t, Options{States: NewMemoryStates(0, 0), Scripting: counterScripting(), FS: theme(
				"components/Counter.html", statefulNoClient,
				"components/Box.html", "<section><Counter /></section>",
				"components/Isl.html", "---\nbrick:\n  island: true\n---\n<aside><Counter /></aside>",
				"components/Pick.html", "---\nbrick:\n  island: true\n---\n<aside><brick:render :name=\"`Counter`\" /></aside>",
				"content/a.html", c.page,
			)})
			ctx := context.Background()
			out, err := e.Render(ctx, "content/a.html", nil)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(out), "data-brick-state=") {
				t.Fatalf("no stateful root rendered:\n%s", out)
			}
			if strings.Count(string(out), runtimeTag) != 1 {
				t.Errorf("want the runtime linked once:\n%s", out)
			}
			// RenderChunks then Fill: the cached page's assets already carry
			// the runtime, so a later fill keeps it.
			r, err := e.RenderChunks(ctx, "content/a.html", nil)
			if err != nil {
				t.Fatal(err)
			}
			filled, err := e.Fill(ctx, r)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Count(string(filled), runtimeTag) != 1 {
				t.Errorf("Fill: want the runtime linked once:\n%s", filled)
			}
		})
	}
}

// A page with nothing interactive still ships zero JavaScript: no stateful
// component, no client code and no navigation means no runtime.
func TestRuntimeNotLinkedForStaticPage(t *testing.T) {
	t.Parallel()
	e := newEngine(t, Options{States: NewMemoryStates(0, 0), Scripting: counterScripting(), FS: theme(
		"components/Plain.html", "<p>{{ .Props.x }}</p>",
		"content/a.html", `<Plain x="1" />{{ assets }}`,
	)})
	out, err := e.Render(context.Background(), "content/a.html", nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), runtimeTag) || strings.Contains(string(out), "<script") {
		t.Errorf("a static page linked script:\n%s", out)
	}
}
