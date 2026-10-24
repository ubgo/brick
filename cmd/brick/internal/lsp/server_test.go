package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ubgo/brick"
)

// fakeProject answers with fixed components and whatever diagnostics the
// test sets, counting checks.
type fakeProject struct {
	mu     sync.Mutex
	diags  []brick.Diagnostic
	checks atomic.Int32
}

func (f *fakeProject) Components(context.Context, string) ([]brick.ComponentDoc, error) {
	return []brick.ComponentDoc{{Name: "PostList", Path: "components/PostList.html", Props: []brick.PropDoc{
		{Name: "posts", Type: "list", Required: true, Doc: "The posts."},
		{Name: "variant", Type: "string", Default: "plain", HasDefault: true, OneOf: []any{"plain", "featured"}},
	}}}, nil
}

func (f *fakeProject) Check(context.Context, string) ([]brick.Diagnostic, error) {
	f.checks.Add(1)
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.diags, nil
}

// client is the editor's end of a server over pipes.
type client struct {
	t     *testing.T
	conn  *Conn
	in    io.WriteCloser
	msgs  chan *message
	id    int
	done  chan error
	notes []*message
}

func startServer(t *testing.T, p Project) *client {
	t.Helper()
	return startServerWith(t, Options{Project: p})
}

// startServerWith starts a server with opts, filling in the schema and a
// short debounce.
func startServerWith(t *testing.T, opts Options) *client {
	t.Helper()
	cr, sw := io.Pipe() // server → client
	sr, cw := io.Pipe() // client → server
	opts.Schema, opts.Debounce = brick.FrontmatterSchema(), 20*time.Millisecond
	srv, err := NewServer(NewConn(sr, sw), opts)
	if err != nil {
		t.Fatal(err)
	}
	c := &client{t: t, conn: NewConn(cr, cw), in: cw, msgs: make(chan *message, 64), done: make(chan error, 1)}
	go func() { c.done <- srv.Serve(context.Background()) }()
	go func() {
		for {
			m, err := c.conn.Read()
			if err != nil {
				close(c.msgs)
				return
			}
			c.msgs <- m
		}
	}()
	t.Cleanup(func() { _ = cw.Close(); _ = sw.Close() })
	return c
}

// call sends a request and returns its result, keeping notifications.
func (c *client) call(method string, params any, result any) {
	c.t.Helper()
	c.id++
	b, _ := json.Marshal(params)
	id, _ := json.Marshal(c.id)
	if err := c.conn.write(&message{ID: id, Method: method, Params: b}); err != nil {
		c.t.Fatal(err)
	}
	for m := range c.msgs {
		if string(m.ID) != string(id) {
			c.notes = append(c.notes, m)
			continue
		}
		if m.Error != nil {
			c.t.Fatalf("%s: %v", method, m.Error)
		}
		if result != nil {
			if err := json.Unmarshal(m.Result, result); err != nil {
				c.t.Fatal(err)
			}
		}
		return
	}
	c.t.Fatalf("%s: stream closed", method)
}

func (c *client) notify(method string, params any) {
	c.t.Helper()
	if err := c.conn.Notify(method, params); err != nil {
		c.t.Fatal(err)
	}
}

// diagnostics waits for the next publishDiagnostics for uri.
func (c *client) diagnostics(uri string) []map[string]any {
	c.t.Helper()
	timeout := time.After(5 * time.Second)
	for {
		for i, m := range c.notes {
			var p struct {
				URI         string           `json:"uri"`
				Diagnostics []map[string]any `json:"diagnostics"`
			}
			if m.Method == methodPublishDiags && json.Unmarshal(m.Params, &p) == nil && p.URI == uri {
				c.notes = append(c.notes[:i], c.notes[i+1:]...)
				return p.Diagnostics
			}
		}
		select {
		case m := <-c.msgs:
			c.notes = append(c.notes, m)
		case <-timeout:
			c.t.Fatalf("no diagnostics for %s", uri)
		}
	}
}

// pos is the position delta bytes after marker in text.
func pos(uri, text, marker string, delta int) map[string]any {
	off := strings.Index(text, marker) + delta
	l, ch := PositionOf(text, off)
	return map[string]any{"textDocument": map[string]string{"uri": uri}, "position": map[string]int{"line": l, "character": ch}}
}

type completion struct {
	Items []struct {
		Label            string `json:"label"`
		InsertTextFormat int    `json:"insertTextFormat"`
		TextEdit         struct {
			NewText string `json:"newText"`
		} `json:"textEdit"`
	} `json:"items"`
}

func (c completion) labels() string {
	var out []string
	for _, it := range c.Items {
		out = append(out, it.Label)
	}
	return strings.Join(out, ",")
}

func TestServerRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "brick.yaml"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "content", "index.html")
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	text := "---\nbrick: {isl}\n---\n<x-post-list posts=\"{{ .x }}\" variant=\"\" >\n<Po"
	if err := os.WriteFile(file, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	uri := pathToURI(file)
	fp := &fakeProject{diags: []brick.Diagnostic{{File: "content/index.html", Line: 4, Col: 2, Severity: brick.SeverityWarning, Code: "unknown-prop", Message: "a warning"}}}
	c := startServer(t, fp)

	c.call("initialize", map[string]any{"rootUri": pathToURI(dir), "capabilities": map[string]any{"textDocument": map[string]any{"completion": map[string]any{"completionItem": map[string]any{"snippetSupport": true}}}}}, nil)
	c.notify("initialized", map[string]any{})
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": uri, "languageId": "html", "version": 1, "text": text}})

	// Diagnostics: brick's 1-based line:col becomes LSP's 0-based range.
	d := c.diagnostics(uri)
	if len(d) != 1 || d[0]["severity"] != float64(severityWarning) || d[0]["code"] != "unknown-prop" {
		t.Fatalf("diagnostics: %v", d)
	}
	if r := d[0]["range"].(map[string]any)["start"].(map[string]any); r["line"] != float64(3) || r["character"] != float64(1) {
		t.Fatalf("diagnostic start: %v", r)
	}

	// Completion: a tag, a hyphen tag, props (minus the written ones), a
	// oneof value, and a frontmatter key.
	var cl completion
	c.call("textDocument/completion", pos(uri, text, "<Po", 3), &cl)
	if cl.labels() != "PostList" {
		t.Fatalf("tag completion: %s", cl.labels())
	}
	c.call("textDocument/completion", pos(uri, text, "x-post", 6), &cl)
	if cl.labels() != "x-post-list" {
		t.Fatalf("hyphen tag completion: %s", cl.labels())
	}
	c.call("textDocument/completion", pos(uri, text, `"" >`, 3), &cl)
	if cl.labels() != "" {
		t.Fatalf("written props offered again: %s", cl.labels())
	}
	c.call("textDocument/completion", pos(uri, text, `variant=""`, 9), &cl)
	if cl.labels() != "plain,featured" {
		t.Fatalf("value completion: %s", cl.labels())
	}
	c.call("textDocument/completion", pos(uri, text, "isl}", 3), &cl)
	if !strings.Contains(cl.labels(), "island") || strings.Contains(cl.labels(), "layout") {
		t.Fatalf("brick: completion: %s", cl.labels())
	}

	// Hover on a hyphen tag and a prop.
	var h struct {
		Contents struct {
			Value string `json:"value"`
		} `json:"contents"`
	}
	c.call("textDocument/hover", pos(uri, text, "post-list", 2), &h)
	if !strings.Contains(h.Contents.Value, "**<PostList>**") || !strings.Contains(h.Contents.Value, "`posts` list, required — The posts.") {
		t.Fatalf("hover tag: %q", h.Contents.Value)
	}
	c.call("textDocument/hover", pos(uri, text, "posts=", 1), &h)
	if !strings.Contains(h.Contents.Value, "The posts.") {
		t.Fatalf("hover prop: %q", h.Contents.Value)
	}

	// Definition: only a component whose file exists answers.
	var loc map[string]any
	c.call("textDocument/definition", pos(uri, text, "post-list", 2), &loc)
	if loc != nil {
		t.Fatalf("definition of a missing file: %v", loc)
	}
	comp := filepath.Join(dir, "components", "PostList.html")
	if err := os.MkdirAll(filepath.Dir(comp), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(comp, []byte("<ul></ul>"), 0o644); err != nil {
		t.Fatal(err)
	}
	c.call("textDocument/definition", pos(uri, text, "post-list", 2), &loc)
	if loc["uri"] != pathToURI(comp) {
		t.Fatalf("definition: %v", loc)
	}

	// A burst of saves runs one check, and a fixed file is cleared.
	fp.mu.Lock()
	fp.diags = nil
	fp.mu.Unlock()
	before := fp.checks.Load()
	for range 5 {
		c.notify("textDocument/didSave", map[string]any{"textDocument": map[string]string{"uri": uri}})
	}
	if d := c.diagnostics(uri); len(d) != 0 {
		t.Fatalf("not cleared: %v", d)
	}
	if n := fp.checks.Load() - before; n != 1 {
		t.Fatalf("%d checks for a burst of saves, want 1", n)
	}

	c.call("shutdown", nil, nil)
	c.notify("exit", nil)
	if err := <-c.done; err != nil {
		t.Fatalf("serve: %v", err)
	}
}

func TestServerUnknownRequest(t *testing.T) {
	c := startServer(t, &fakeProject{})
	c.id++
	id, _ := json.Marshal(c.id)
	if err := c.conn.write(&message{ID: id, Method: "textDocument/codeLens", Params: json.RawMessage("{}")}); err != nil {
		t.Fatal(err)
	}
	m := <-c.msgs
	if m.Error == nil || m.Error.Code != codeMethodNotFound {
		t.Fatalf("got %+v", m)
	}
	// exit without shutdown is an error exit, as the protocol says.
	c.notify("exit", nil)
	if err := <-c.done; err == nil {
		t.Fatal("exit before shutdown returned nil")
	}
}

// textDocument/formatting answers one edit spanning the whole open
// document with what Options.Format returns; an unchanged document and one
// the formatter refuses answer no edits; without a formatter the method is
// neither advertised nor answered.
func TestServerFormatting(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "content", "index.html")
	uri := pathToURI(file)
	var gotPath string
	format := func(path string, src []byte) ([]byte, error) {
		gotPath = path
		if strings.Contains(string(src), "broken") {
			return nil, errors.New("1:1: cannot read")
		}
		return []byte(strings.ReplaceAll(string(src), "  ", "")), nil
	}
	c := startServerWith(t, Options{Project: &fakeProject{}, Format: format})
	var init map[string]any
	c.call("initialize", map[string]any{"rootUri": pathToURI(dir), "capabilities": map[string]any{}}, &init)
	if caps := init["capabilities"].(map[string]any); caps["documentFormattingProvider"] != true {
		t.Fatalf("formatting not advertised: %v", caps)
	}
	text := "<p>\n  é  x</p>\n<b>"
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": uri, "languageId": "html", "version": 1, "text": text}})
	params := map[string]any{"textDocument": map[string]any{"uri": uri}, "options": map[string]any{"tabSize": 2, "insertSpaces": true}}
	var edits []struct {
		Range struct {
			Start, End struct{ Line, Character int }
		}
		NewText string
	}
	c.call("textDocument/formatting", params, &edits)
	if len(edits) != 1 || edits[0].NewText != "<p>\néx</p>\n<b>" || gotPath != file {
		t.Fatalf("edits %+v, path %q", edits, gotPath)
	}
	if r := edits[0].Range; r.Start.Line != 0 || r.Start.Character != 0 || r.End.Line != 2 || r.End.Character != 3 {
		t.Fatalf("range %+v, want the whole document", r)
	}
	for _, body := range []string{"<p>done</p>", "broken"} {
		c.notify("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": uri, "version": 2}, "contentChanges": []map[string]any{{"text": body}}})
		edits = nil
		c.call("textDocument/formatting", params, &edits)
		if len(edits) != 0 {
			t.Fatalf("%q: edits %+v, want none", body, edits)
		}
	}

	plain := startServer(t, &fakeProject{})
	init = nil
	plain.call("initialize", map[string]any{"rootUri": pathToURI(dir), "capabilities": map[string]any{}}, &init)
	if _, ok := init["capabilities"].(map[string]any)["documentFormattingProvider"]; ok {
		t.Fatal("formatting advertised without a formatter")
	}
}
