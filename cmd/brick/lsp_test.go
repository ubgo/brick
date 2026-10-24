package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/textproto"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// lspSession drives `brick lsp` over pipes with real framing, against a
// real site, the way an editor does.
type lspSession struct {
	t   *testing.T
	in  io.WriteCloser
	out *bufio.Reader
	id  int
}

func (s *lspSession) send(method string, params any, request bool) int {
	s.t.Helper()
	m := map[string]any{"jsonrpc": "2.0", "method": method, "params": params}
	if request {
		s.id++
		m["id"] = s.id
	}
	b, _ := json.Marshal(m)
	if _, err := fmt.Fprintf(s.in, "Content-Length: %d\r\n\r\n%s", len(b), b); err != nil {
		s.t.Fatal(err)
	}
	return s.id
}

// next reads one message.
func (s *lspSession) next() map[string]any {
	s.t.Helper()
	h, err := textproto.NewReader(s.out).ReadMIMEHeader()
	if err != nil {
		s.t.Fatal(err)
	}
	n, _ := strconv.Atoi(h.Get("Content-Length"))
	body := make([]byte, n)
	if _, err := io.ReadFull(s.out, body); err != nil {
		s.t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		s.t.Fatal(err)
	}
	return m
}

// until reads messages until one matches.
func (s *lspSession) until(match func(map[string]any) bool) map[string]any {
	s.t.Helper()
	for {
		if m := s.next(); match(m) {
			return m
		}
	}
}

func (s *lspSession) call(method string, params any) any {
	s.t.Helper()
	id := s.send(method, params, true)
	return s.until(func(m map[string]any) bool { return m["id"] == float64(id) })["result"]
}

func TestLSP(t *testing.T) {
	dir := project(t,
		"brick.yaml", "roots: [components]\n",
		"components/Card.html", "---\nprops:\n  title: {type: string, doc: The heading.}\n---\n<b>{{ .Props.title }}</b>",
		"content/index.html", "<Card title=\"home\" />\n<Crad />\n",
	)
	page := filepath.Join(dir, "content", "index.html")
	uri := "file://" + filepath.ToSlash(page)
	text := "<Card title=\"home\" />\n<Crad />\n"

	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	var stderr bytes.Buffer
	done := make(chan int, 1)
	go func() { done <- lspCmd(context.Background(), nil, inR, outW, &stderr) }()
	s := &lspSession{t: t, in: inW, out: bufio.NewReader(outR)}

	init := s.call("initialize", map[string]any{"rootUri": "file://" + filepath.ToSlash(dir), "capabilities": map[string]any{}})
	if !strings.Contains(fmt.Sprint(init), "hoverProvider:true") {
		t.Fatalf("initialize: %v", init)
	}
	// serverInfo's version is brick version's, from the build info.
	if want := "version:" + buildVersion().Version; !strings.Contains(fmt.Sprint(init), want) {
		t.Fatalf("serverInfo: %v, want %s", init, want)
	}
	s.send("initialized", map[string]any{}, false)
	s.send("textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": uri, "languageId": "html", "version": 1, "text": text}}, false)

	// brick check's own diagnostic, on the line brick check names.
	diag := s.until(func(m map[string]any) bool { return m["method"] == "textDocument/publishDiagnostics" })
	got := fmt.Sprint(diag["params"])
	if !strings.Contains(got, "unknown component <Crad>") || !strings.Contains(got, "start:map[character:0 line:1]") {
		t.Fatalf("diagnostics: %s", got)
	}

	at := func(line, char int) map[string]any {
		return map[string]any{"textDocument": map[string]any{"uri": uri}, "position": map[string]any{"line": line, "character": char}}
	}
	if got := fmt.Sprint(s.call("textDocument/completion", at(0, 3))); !strings.Contains(got, "label:Card") {
		t.Fatalf("tag completion: %s", got)
	}
	if got := fmt.Sprint(s.call("textDocument/hover", at(0, 7))); !strings.Contains(got, "The heading.") {
		t.Fatalf("prop hover: %s", got)
	}
	want := "file://" + filepath.ToSlash(filepath.Join(dir, "components", "Card.html"))
	if got := fmt.Sprint(s.call("textDocument/definition", at(0, 2))); !strings.Contains(got, want) {
		t.Fatalf("definition: %s", got)
	}

	// Formatting is brick fmt's layout, as one whole-document edit; the
	// kit's raw <CodeBlock> body stays as written, as brick fmt keeps it.
	if !strings.Contains(fmt.Sprint(init), "documentFormattingProvider:true") {
		t.Fatalf("formatting not advertised: %v", init)
	}
	fmtURI := "file://" + filepath.ToSlash(filepath.Join(dir, "content", "fmt.html"))
	messy := "<div>\n<p>x</p>\n<CodeBlock>\n<b>keep</b>\n</CodeBlock>\n</div>\n"
	s.send("textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": fmtURI, "languageId": "html", "version": 1, "text": messy}}, false)
	edits := fmt.Sprint(s.call("textDocument/formatting", map[string]any{"textDocument": map[string]any{"uri": fmtURI}, "options": map[string]any{"tabSize": 2, "insertSpaces": true}}))
	wantText := "<div>\n  <p>x</p>\n  <CodeBlock>\n<b>keep</b>\n</CodeBlock>\n</div>\n"
	if !strings.Contains(edits, "newText:"+wantText) || !strings.Contains(edits, "end:map[character:0 line:6]") {
		t.Fatalf("formatting: %s", edits)
	}

	s.call("shutdown", nil)
	s.send("exit", nil, false)
	select {
	case code := <-done:
		if code != exitOK {
			t.Fatalf("exit code %d, stderr %s", code, stderr.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("brick lsp did not exit")
	}
}

// References and rename against a real site: the real engine's component
// list and the CLI's raw components (the kit's <CodeBlock> is raw), so a
// <Card> inside its body or a {{ }} string is not a call.
func TestLSPReferencesRename(t *testing.T) {
	page := "<Card title=\"a\" />\n<x-card title=\"b\"></x-card>\n{{ \"<Card>\" }}\n<CodeBlock><Card /></CodeBlock>\n"
	dir := project(t,
		"brick.yaml", "roots: [components]\n",
		"components/Card.html", "---\nprops:\n  title: {type: string}\n---\n<b>{{ .Props.title }}</b>",
		"components/Box.html", "<div><Card title=\"box\" /></div>",
		"content/index.html", page,
	)
	uri := "file://" + filepath.ToSlash(filepath.Join(dir, "content", "index.html"))

	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	var stderr bytes.Buffer
	done := make(chan int, 1)
	go func() { done <- lspCmd(context.Background(), nil, inR, outW, &stderr) }()
	s := &lspSession{t: t, in: inW, out: bufio.NewReader(outR)}
	caps := map[string]any{"workspace": map[string]any{"workspaceEdit": map[string]any{"documentChanges": true, "resourceOperations": []string{"rename"}}}}
	s.call("initialize", map[string]any{"rootUri": "file://" + filepath.ToSlash(dir), "capabilities": caps})
	s.send("initialized", map[string]any{}, false)
	s.send("textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": uri, "languageId": "html", "version": 1, "text": page}}, false)

	at := map[string]any{"textDocument": map[string]any{"uri": uri}, "position": map[string]any{"line": 0, "character": 2}, "context": map[string]any{"includeDeclaration": false}}
	refs, _ := s.call("textDocument/references", at).([]any)
	// <Card>, <x-card> and </x-card> in the page, and the one in Box.
	if len(refs) != 4 || !strings.Contains(fmt.Sprint(refs), "Box.html") {
		t.Fatalf("references (%d): %v", len(refs), refs)
	}

	at["newName"] = "Tile"
	edit := fmt.Sprint(s.call("textDocument/rename", at))
	for _, want := range []string{"newText:Tile", "newText:x-tile", "kind:rename", "components/Tile.html"} {
		if !strings.Contains(edit, want) {
			t.Fatalf("rename edit lacks %q: %s", want, edit)
		}
	}
	if n := strings.Count(edit, "newText:"); n != 4 {
		t.Fatalf("rename: %d text edits, want 4: %s", n, edit)
	}

	s.call("shutdown", nil)
	s.send("exit", nil, false)
	select {
	case code := <-done:
		if code != exitOK {
			t.Fatalf("exit code %d, stderr %s", code, stderr.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("brick lsp did not exit")
	}
}

// TestLSPPropRename renames a prop over the real `brick lsp` stream from a
// .Props use inside the component: the declaration, the component's uses
// and both call-site forms change; a call through brick:spread then
// refuses the rename with its location instead of a partial edit.
func TestLSPPropRename(t *testing.T) {
	card := "---\nprops:\n  title: {type: string}\n---\n<b>{{ .Props.title }}</b>{{ range .Props.items }}{{ .Props.title }}{{ end }}"
	dir := project(t,
		"brick.yaml", "roots: [components]\n",
		"components/Card.html", card,
		"content/index.html", "<Card title=\"a\" />\n<x-card :title=\"`b`\"></x-card>\n",
	)
	uri := "file://" + filepath.ToSlash(filepath.Join(dir, "components", "Card.html"))

	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	var stderr bytes.Buffer
	done := make(chan int, 1)
	go func() { done <- lspCmd(context.Background(), nil, inR, outW, &stderr) }()
	s := &lspSession{t: t, in: inW, out: bufio.NewReader(outR)}
	caps := map[string]any{"workspace": map[string]any{"workspaceEdit": map[string]any{"documentChanges": true}}}
	s.call("initialize", map[string]any{"rootUri": "file://" + filepath.ToSlash(dir), "capabilities": caps})
	s.send("initialized", map[string]any{}, false)
	s.send("textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": uri, "languageId": "html", "version": 1, "text": card}}, false)

	// The cursor on `title` in the first {{ .Props.title }} (line 4).
	at := map[string]any{"textDocument": map[string]any{"uri": uri}, "position": map[string]any{"line": 4, "character": 14}, "context": map[string]any{"includeDeclaration": true}}
	refs, _ := s.call("textDocument/references", at).([]any)
	// The declaration, the root use, and the two call sites; not the one
	// under range, where dot is an item.
	if len(refs) != 4 {
		t.Fatalf("prop references (%d): %v", len(refs), refs)
	}

	at["newName"] = "heading"
	id := s.send("textDocument/rename", at, true)
	var shown string
	reply := s.until(func(m map[string]any) bool {
		if m["method"] == "window/showMessage" {
			shown = fmt.Sprint(m["params"])
		}
		return m["id"] == float64(id)
	})
	edit := fmt.Sprint(reply["result"])
	if n := strings.Count(edit, "newText:heading"); n != 4 {
		t.Fatalf("rename: %d edits, want 4: %s", n, edit)
	}
	if !strings.Contains(shown, "skipped 1") {
		t.Fatalf("the use under range was not reported: %q", shown)
	}

	if err := os.WriteFile(filepath.Join(dir, "content", "spread.html"), []byte("<Card brick:spread=\".Params.p\" />\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	id = s.send("textDocument/rename", at, true)
	refused := s.until(func(m map[string]any) bool { return m["id"] == float64(id) })
	if msg := fmt.Sprint(refused["error"]); refused["result"] != nil || !strings.Contains(msg, "content/spread.html:1:7 passes props through brick:spread") {
		t.Fatalf("spread rename: result %v error %s", refused["result"], msg)
	}

	s.call("shutdown", nil)
	s.send("exit", nil, false)
	select {
	case code := <-done:
		if code != exitOK {
			t.Fatalf("exit code %d, stderr %s", code, stderr.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("brick lsp did not exit")
	}
}
