package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ubgo/brick"
)

// Project is what the server needs from a brick site. The CLI implements it
// with the real engine (site.New, ComponentDocs, Check), so the editor sees
// exactly what `brick docs` and `brick check` see.
type Project interface {
	// Components lists every component the site at root can call.
	Components(ctx context.Context, root string) ([]brick.ComponentDoc, error)
	// Check reports the site's diagnostics, as `brick check` does. Files
	// are relative to root.
	Check(ctx context.Context, root string) ([]brick.Diagnostic, error)
}

// Options configures a Server.
type Options struct {
	Project Project
	// Schema is the frontmatter JSON Schema (brick.FrontmatterSchema),
	// which drives frontmatter completion and hover.
	Schema []byte
	// Debounce is how long the server waits after a save before checking,
	// so a burst of saves runs one check. Zero means DefaultDebounce.
	Debounce time.Duration
	// Format lays out one document the way `brick fmt` does: path is the
	// file's location, so the formatter can find the site's raw
	// components. Nil leaves textDocument/formatting unadvertised.
	Format func(path string, src []byte) ([]byte, error)
	// RawComponents returns, for a site root, the raw-body test for one
	// file's source: which component tags capture their body verbatim
	// (`raw: true` components, the kit's, the file's use: aliases of
	// them). References and rename scan with it, so a tag inside a raw
	// body is not a call. The engine's own raw tags (<brick:script>,
	// <brick:prop raw>, …) are known without it. Nil means no raw
	// components.
	RawComponents func(root string) func(src []byte) RawTest
	// Version is reported in serverInfo.
	Version string
	// Log receives the server's own trouble (a check that failed to run);
	// nil discards it. stdout is the protocol stream, so never pass it.
	Log io.Writer
}

// DefaultDebounce is the wait between a save and its check.
const DefaultDebounce = 300 * time.Millisecond

// RootMarkers are the files that mark a site's root: the server checks the
// nearest ancestor of a document holding one, so one workspace can hold
// several sites (this repository's examples do).
var RootMarkers = []string{"brick.yaml", "site.yaml"}

// LSP constants the server uses.
const (
	syncFull             = 1
	severityError        = 1
	severityWarning      = 2
	completionProperty   = 10
	completionClass      = 7
	completionValue      = 12
	completionKeyword    = 14
	insertPlain          = 1
	insertSnippet        = 2
	markupMarkdown       = "markdown"
	diagnosticSource     = "brick"
	serverName           = "brick"
	methodPublishDiags   = "textDocument/publishDiagnostics"
	methodLogMessage     = "window/logMessage"
	messageTypeError     = 1
	hyphenTagPrefix      = "x-"
	fileScheme           = "file"
	completionTriggerTag = "<"
	resourceRename       = "rename"
)

// Server answers one client over one Conn.
type Server struct {
	opts   Options
	conn   *Conn
	schema map[string]any

	// root is the workspace folder; snippets says the client accepts
	// snippet completions.
	root     string
	snippets bool
	// renameFiles says the client applies RenameFile operations in a
	// WorkspaceEdit, which a component rename needs.
	renameFiles bool
	// docChanges says the client takes documentChanges in a
	// WorkspaceEdit; a prop rename answers the plain changes map without.
	docChanges bool

	mu    sync.Mutex
	docs  map[string]string // open documents by URI
	comps map[string]*compEntry
	// published holds, per site root, the URIs that have diagnostics, so
	// a file whose problems are fixed is cleared.
	published map[string]map[string]bool
	timers    map[string]*time.Timer
	cancels   map[string]context.CancelFunc
	wg        sync.WaitGroup
	shutdown  bool
}

// compEntry is one site's component list, loaded once and replaced after
// a save.
type compEntry struct {
	ready chan struct{}
	docs  []brick.ComponentDoc
	err   error
}

// NewServer returns a server for conn.
func NewServer(conn *Conn, opts Options) (*Server, error) {
	if opts.Project == nil {
		return nil, errors.New("lsp: Options.Project is required")
	}
	if opts.Debounce == 0 {
		opts.Debounce = DefaultDebounce
	}
	if opts.Log == nil {
		opts.Log = io.Discard
	}
	s := &Server{opts: opts, conn: conn, docs: map[string]string{}, comps: map[string]*compEntry{},
		published: map[string]map[string]bool{}, timers: map[string]*time.Timer{}, cancels: map[string]context.CancelFunc{}}
	if len(opts.Schema) > 0 {
		if err := json.Unmarshal(opts.Schema, &s.schema); err != nil {
			return nil, fmt.Errorf("lsp: frontmatter schema: %w", err)
		}
	}
	return s, nil
}

// Serve reads messages until the client sends exit or closes the stream.
// It returns nil after a clean shutdown and exit.
func (s *Server) Serve(ctx context.Context) error {
	defer s.stopChecks()
	for {
		m, err := s.conn.Read()
		if err != nil {
			var re *rpcError
			if errors.As(err, &re) {
				_ = s.conn.Reply(json.RawMessage("null"), nil, re)
				continue
			}
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		if m.Method == "exit" {
			if !s.shutdown {
				return errors.New("lsp: exit before shutdown")
			}
			return nil
		}
		s.handle(ctx, m)
	}
}

// stopChecks cancels pending and running checks and waits for them.
func (s *Server) stopChecks() {
	s.mu.Lock()
	for _, t := range s.timers {
		if t.Stop() {
			s.wg.Done() // the check never runs, so it never calls Done
		}
	}
	for _, c := range s.cancels {
		c()
	}
	s.mu.Unlock()
	s.wg.Wait()
}

// handle dispatches one message; a request always gets a reply.
func (s *Server) handle(ctx context.Context, m *message) {
	isRequest := len(m.ID) > 0
	result, rerr := s.dispatch(ctx, m)
	if !isRequest {
		if rerr != nil {
			_ = s.conn.Notify(methodLogMessage, map[string]any{"type": messageTypeError, "message": rerr.Message})
		}
		return
	}
	_ = s.conn.Reply(m.ID, result, rerr)
}

// dispatch runs one method. Unknown notifications are ignored, as the
// protocol asks; unknown requests answer MethodNotFound.
func (s *Server) dispatch(ctx context.Context, m *message) (any, *rpcError) {
	switch m.Method {
	case "initialize":
		return s.initialize(m.Params)
	case "initialized", "$/cancelRequest", "$/setTrace", "workspace/didChangeConfiguration":
		return nil, nil
	case "shutdown":
		s.shutdown = true
		s.stopChecks()
		return nil, nil
	case "textDocument/didOpen":
		var p struct {
			TextDocument struct {
				URI  string `json:"uri"`
				Text string `json:"text"`
			} `json:"textDocument"`
		}
		if err := json.Unmarshal(m.Params, &p); err != nil {
			return nil, badParams(err)
		}
		s.mu.Lock()
		s.docs[p.TextDocument.URI] = p.TextDocument.Text
		s.mu.Unlock()
		if root, ok := s.siteRoot(p.TextDocument.URI); ok {
			s.loadComponents(ctx, root, false)
			s.scheduleCheck(ctx, root)
		}
		return nil, nil
	case "textDocument/didChange":
		var p struct {
			TextDocument   docID `json:"textDocument"`
			ContentChanges []struct {
				Text string `json:"text"`
			} `json:"contentChanges"`
		}
		if err := json.Unmarshal(m.Params, &p); err != nil {
			return nil, badParams(err)
		}
		if n := len(p.ContentChanges); n > 0 {
			s.mu.Lock()
			s.docs[p.TextDocument.URI] = p.ContentChanges[n-1].Text
			s.mu.Unlock()
		}
		return nil, nil
	case "textDocument/didSave":
		var p struct {
			TextDocument docID `json:"textDocument"`
		}
		if err := json.Unmarshal(m.Params, &p); err != nil {
			return nil, badParams(err)
		}
		if root, ok := s.siteRoot(p.TextDocument.URI); ok {
			s.loadComponents(ctx, root, true)
			s.scheduleCheck(ctx, root)
		}
		return nil, nil
	case "textDocument/didClose":
		var p struct {
			TextDocument docID `json:"textDocument"`
		}
		if err := json.Unmarshal(m.Params, &p); err != nil {
			return nil, badParams(err)
		}
		s.mu.Lock()
		delete(s.docs, p.TextDocument.URI)
		s.mu.Unlock()
		return nil, nil
	case "textDocument/completion":
		return s.withPosition(ctx, m.Params, s.completion)
	case "textDocument/hover":
		return s.withPosition(ctx, m.Params, s.hover)
	case "textDocument/definition":
		return s.withPosition(ctx, m.Params, s.definition)
	case "textDocument/formatting":
		return s.formatting(m.Params)
	case "textDocument/references":
		return s.references(ctx, m.Params)
	case "textDocument/prepareRename":
		return s.prepareRename(ctx, m.Params)
	case "textDocument/rename":
		return s.rename(ctx, m.Params)
	}
	if len(m.ID) == 0 {
		return nil, nil
	}
	return nil, &rpcError{Code: codeMethodNotFound, Message: "brick lsp does not handle " + m.Method}
}

// docID is a TextDocumentIdentifier.
type docID struct {
	URI string `json:"uri"`
}

// badParams is the reply to params that do not decode.
func badParams(err error) *rpcError {
	return &rpcError{Code: codeInvalidParams, Message: err.Error()}
}

// initialize records the workspace root and the client's snippet support,
// and states what the server answers.
func (s *Server) initialize(params json.RawMessage) (any, *rpcError) {
	var p struct {
		RootURI          string  `json:"rootUri"`
		RootPath         *string `json:"rootPath"`
		WorkspaceFolders []struct {
			URI string `json:"uri"`
		} `json:"workspaceFolders"`
		Capabilities struct {
			Workspace struct {
				WorkspaceEdit struct {
					DocumentChanges    bool     `json:"documentChanges"`
					ResourceOperations []string `json:"resourceOperations"`
				} `json:"workspaceEdit"`
			} `json:"workspace"`
			TextDocument struct {
				Completion struct {
					CompletionItem struct {
						SnippetSupport bool `json:"snippetSupport"`
					} `json:"completionItem"`
				} `json:"completion"`
			} `json:"textDocument"`
		} `json:"capabilities"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, badParams(err)
	}
	switch {
	case len(p.WorkspaceFolders) > 0:
		s.root, _ = uriToPath(p.WorkspaceFolders[0].URI)
	case p.RootURI != "":
		s.root, _ = uriToPath(p.RootURI)
	case p.RootPath != nil:
		s.root = *p.RootPath
	}
	s.snippets = p.Capabilities.TextDocument.Completion.CompletionItem.SnippetSupport
	we := p.Capabilities.Workspace.WorkspaceEdit
	s.renameFiles = we.DocumentChanges && slices.Contains(we.ResourceOperations, resourceRename)
	s.docChanges = we.DocumentChanges
	caps := map[string]any{
		"textDocumentSync":   map[string]any{"openClose": true, "change": syncFull, "save": map[string]any{"includeText": false}},
		"completionProvider": map[string]any{"triggerCharacters": []string{completionTriggerTag, " ", ":", "-", "{", "\""}},
		"hoverProvider":      true,
		"definitionProvider": true,
		"referencesProvider": true,
		"renameProvider":     map[string]any{"prepareProvider": true},
	}
	if s.opts.Format != nil {
		caps["documentFormattingProvider"] = true
	}
	return map[string]any{"capabilities": caps, "serverInfo": map[string]any{"name": serverName, "version": s.opts.Version}}, nil
}

// formatting answers textDocument/formatting with one edit replacing the
// whole document, so the editor gets exactly what `brick fmt` writes
// (the formatter re-lays the file as a whole; per-line diffs would only
// re-derive what it already decided). An unchanged document answers no
// edits. A document the formatter cannot read answers no edits too, with
// the reason logged rather than raised: format-on-save would otherwise
// pop an error on every save of a half-typed file, and the diagnostics
// already show the problem.
func (s *Server) formatting(params json.RawMessage) (any, *rpcError) {
	var p struct {
		TextDocument docID `json:"textDocument"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, badParams(err)
	}
	if s.opts.Format == nil {
		return nil, &rpcError{Code: codeMethodNotFound, Message: "brick lsp was started without a formatter"}
	}
	s.mu.Lock()
	text, ok := s.docs[p.TextDocument.URI]
	s.mu.Unlock()
	if !ok {
		return nil, nil
	}
	path, err := uriToPath(p.TextDocument.URI)
	if err != nil {
		return nil, nil
	}
	out, err := s.opts.Format(path, []byte(text))
	if err != nil {
		_ = s.conn.Notify(methodLogMessage, map[string]any{"type": messageTypeError, "message": "brick fmt: " + path + ": " + err.Error()})
		return []any{}, nil
	}
	if string(out) == text {
		return []any{}, nil
	}
	return []any{map[string]any{"range": rangeOf(text, 0, len(text)), "newText": string(out)}}, nil
}

// position is a TextDocumentPositionParams.
type position struct {
	TextDocument docID `json:"textDocument"`
	Position     struct {
		Line      int `json:"line"`
		Character int `json:"character"`
	} `json:"position"`
}

// cursor is a classified position in an open document of a site.
type cursor struct {
	uri    string
	root   string
	text   string
	offset int
	ctx    Context
}

// withPosition decodes a position request and runs fn on its cursor. A
// position in a closed document, or outside any site, answers null.
func (s *Server) withPosition(ctx context.Context, params json.RawMessage, fn func(context.Context, cursor) any) (any, *rpcError) {
	var p position
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, badParams(err)
	}
	s.mu.Lock()
	text, ok := s.docs[p.TextDocument.URI]
	s.mu.Unlock()
	if !ok {
		return nil, nil
	}
	root, ok := s.siteRoot(p.TextDocument.URI)
	if !ok {
		return nil, nil
	}
	off := OffsetOf(text, p.Position.Line, p.Position.Character)
	return fn(ctx, cursor{uri: p.TextDocument.URI, root: root, text: text, offset: off, ctx: Classify(text, off)}), nil
}

// siteRoot is the site a document belongs to: its nearest ancestor holding
// a RootMarker, else the workspace folder. ok is false for a document
// that is not a local file.
func (s *Server) siteRoot(uri string) (string, bool) {
	p, err := uriToPath(uri)
	if err != nil {
		return "", false
	}
	for dir := filepath.Dir(p); ; dir = filepath.Dir(dir) {
		for _, m := range RootMarkers {
			if _, err := os.Stat(filepath.Join(dir, m)); err == nil {
				return dir, true
			}
		}
		if dir == s.root || filepath.Dir(dir) == dir {
			break
		}
	}
	if s.root == "" {
		return filepath.Dir(p), true
	}
	return s.root, true
}

// loadComponents loads a site's component list in the background. reload
// replaces a loaded list (after a save); requests keep the old one until
// the new one is ready.
func (s *Server) loadComponents(ctx context.Context, root string, reload bool) {
	s.mu.Lock()
	if _, ok := s.comps[root]; ok && !reload {
		s.mu.Unlock()
		return
	}
	e := &compEntry{ready: make(chan struct{})}
	if _, ok := s.comps[root]; !ok {
		s.comps[root] = e
	}
	s.mu.Unlock()
	s.wg.Go(func() {
		e.docs, e.err = s.opts.Project.Components(ctx, root)
		if e.err != nil {
			fmt.Fprintf(s.opts.Log, "brick lsp: components of %s: %v\n", root, e.err)
		}
		close(e.ready)
		s.mu.Lock()
		// A failed reload (a component mid-edit that does not parse) keeps
		// the last good list, so completion survives a broken save.
		if cur := s.comps[root]; e.err == nil || cur == e {
			s.comps[root] = e
		}
		s.mu.Unlock()
	})
}

// components returns a site's components, waiting for the first load.
func (s *Server) components(ctx context.Context, root string) []brick.ComponentDoc {
	s.loadComponents(ctx, root, false)
	s.mu.Lock()
	e := s.comps[root]
	s.mu.Unlock()
	select {
	case <-e.ready:
	case <-ctx.Done():
		return nil
	}
	return e.docs
}

// findComponent resolves a written tag the way the engine does: `Card` by
// name, `x-post-list` by its hyphen fold.
func findComponent(list []brick.ComponentDoc, tag string) (brick.ComponentDoc, bool) {
	if rest, ok := strings.CutPrefix(tag, hyphenTagPrefix); ok {
		key := foldHyphen(rest)
		for _, c := range list {
			if foldHyphen(c.Name) == key {
				return c, true
			}
		}
		return brick.ComponentDoc{}, false
	}
	for _, c := range list {
		if c.Name == tag {
			return c, true
		}
	}
	return brick.ComponentDoc{}, false
}

// foldHyphen is the engine's hyphen fold: `post-list` and `PostList` both
// fold to `postlist`.
func foldHyphen(s string) string { return strings.ToLower(strings.ReplaceAll(s, "-", "")) }

// HyphenName is a component's `x-` tag: `PostList` → `x-post-list`. Any
// hyphenation folds to the same component; this one breaks before each
// capital that starts a word.
func HyphenName(name string) string {
	var b strings.Builder
	b.WriteString(hyphenTagPrefix)
	for i := 0; i < len(name); i++ {
		c := name[i]
		if isUpper(c) {
			prevLower := i > 0 && !isUpper(name[i-1])
			nextLower := i > 0 && i+1 < len(name) && isUpper(name[i-1]) && !isUpper(name[i+1])
			if prevLower || nextLower {
				b.WriteByte('-')
			}
			c += 'a' - 'A'
		}
		b.WriteByte(c)
	}
	return b.String()
}

func isUpper(c byte) bool { return c >= 'A' && c <= 'Z' }

// rangeOf is an LSP range between two byte offsets of text.
func rangeOf(text string, start, end int) map[string]any {
	sl, sc := PositionOf(text, start)
	el, ec := PositionOf(text, end)
	return map[string]any{"start": map[string]int{"line": sl, "character": sc}, "end": map[string]int{"line": el, "character": ec}}
}

// markdown is MarkupContent.
func markdown(s string) map[string]any { return map[string]any{"kind": markupMarkdown, "value": s} }

// completion lists what can be typed at the cursor.
func (s *Server) completion(ctx context.Context, c cursor) any {
	items := []map[string]any{}
	wordEnd := c.ctx.WordStart + len(c.ctx.Word)
	edit := func(newText string) map[string]any {
		return map[string]any{"range": rangeOf(c.text, c.ctx.WordStart, wordEnd), "newText": newText}
	}
	switch c.ctx.Kind {
	case KindTagName:
		hyphen := strings.HasPrefix(c.ctx.Prefix, hyphenTagPrefix) || (c.ctx.Prefix != "" && !isUpper(c.ctx.Prefix[0]))
		for _, comp := range s.components(ctx, c.root) {
			label := comp.Name
			if hyphen {
				label = HyphenName(comp.Name)
			}
			items = append(items, map[string]any{
				"label": label, "kind": completionClass, "detail": comp.Path,
				"documentation": markdown(componentMarkdown(comp)), "textEdit": edit(label),
			})
		}
	case KindAttrName:
		comp, ok := findComponent(s.components(ctx, c.root), c.ctx.Tag)
		if !ok {
			break
		}
		for _, p := range comp.Props {
			if slices.Contains(c.ctx.Written, p.Name) && p.Name != c.ctx.Word {
				continue
			}
			item := map[string]any{"label": p.Name, "kind": completionProperty, "detail": propSummary(p),
				"documentation": markdown(propMarkdown(p)), "insertTextFormat": insertPlain, "textEdit": edit(p.Name)}
			if s.snippets && p.Type != "bool" {
				item["insertTextFormat"] = insertSnippet
				item["textEdit"] = edit(p.Name + `="` + valueSnippet(p) + `"`)
			}
			items = append(items, item)
		}
	case KindAttrValue:
		comp, ok := findComponent(s.components(ctx, c.root), c.ctx.Tag)
		if !ok {
			break
		}
		for _, p := range comp.Props {
			if p.Name == c.ctx.Attr {
				for _, v := range p.OneOf {
					items = append(items, map[string]any{"label": fmt.Sprint(v), "kind": completionValue})
				}
			}
		}
	case KindFrontmatterKey:
		node := schemaAt(s.schema, c.ctx.Path)
		for _, key := range sortedKeys(schemaProperties(s.schema, node)) {
			sub := resolve(s.schema, schemaProperties(s.schema, node)[key])
			items = append(items, map[string]any{"label": key, "kind": completionProperty,
				"documentation": markdown(description(sub)), "textEdit": edit(key)})
		}
	case KindFrontmatterValue:
		for _, v := range schemaValues(s.schema, schemaAt(s.schema, c.ctx.Path)) {
			items = append(items, map[string]any{"label": v, "kind": completionKeyword, "textEdit": edit(v)})
		}
	}
	return map[string]any{"isIncomplete": false, "items": items}
}

// valueSnippet is the cursor stop inside a new prop's quotes: a choice
// list for a oneof prop, else an empty stop.
func valueSnippet(p brick.PropDoc) string {
	if len(p.OneOf) == 0 {
		return "$1"
	}
	vals := make([]string, len(p.OneOf))
	for i, v := range p.OneOf {
		vals[i] = strings.NewReplacer(`\`, `\\`, `|`, `\|`, `,`, `\,`, `}`, `\}`, `$`, `\$`).Replace(fmt.Sprint(v))
	}
	return "${1|" + strings.Join(vals, ",") + "|}"
}

// hover documents the component, prop or frontmatter key under the cursor.
func (s *Server) hover(ctx context.Context, c cursor) any {
	var text string
	switch c.ctx.Kind {
	case KindTagName:
		if comp, ok := findComponent(s.components(ctx, c.root), c.ctx.Word); ok {
			text = componentMarkdown(comp)
		}
	case KindAttrName:
		if comp, ok := findComponent(s.components(ctx, c.root), c.ctx.Tag); ok {
			for _, p := range comp.Props {
				if p.Name == c.ctx.Word {
					text = propMarkdown(p)
				}
			}
		}
	case KindFrontmatterKey:
		if c.ctx.Word != "" {
			node := schemaProperties(s.schema, schemaAt(s.schema, c.ctx.Path))[c.ctx.Word]
			if node != nil {
				text = "`" + c.ctx.Word + "` — " + description(resolve(s.schema, node))
			}
		}
	}
	if text == "" {
		return nil
	}
	return map[string]any{"contents": markdown(text), "range": rangeOf(c.text, c.ctx.WordStart, c.ctx.WordStart+len(c.ctx.Word))}
}

// definition answers a component tag with the component's file. A
// component built into the binary (the brick kit) has no file to open.
func (s *Server) definition(ctx context.Context, c cursor) any {
	if c.ctx.Kind != KindTagName {
		return nil
	}
	comp, ok := findComponent(s.components(ctx, c.root), c.ctx.Word)
	if !ok {
		return nil
	}
	p := filepath.Join(c.root, filepath.FromSlash(comp.Path))
	if _, err := os.Stat(p); err != nil {
		return nil
	}
	return map[string]any{"uri": pathToURI(p), "range": rangeOf("", 0, 0)}
}

// componentMarkdown is a component's hover and completion documentation.
func componentMarkdown(c brick.ComponentDoc) string {
	var b strings.Builder
	fmt.Fprintf(&b, "**<%s>** · `%s`", c.Name, c.Path)
	if c.Stateful {
		b.WriteString(" · stateful")
	} else if c.Island {
		b.WriteString(" · island")
	}
	if len(c.Props) > 0 {
		b.WriteString("\n\nProps:\n")
		for _, p := range c.Props {
			fmt.Fprintf(&b, "\n- `%s` %s", p.Name, propSummary(p))
			if p.Doc != "" {
				b.WriteString(" — " + p.Doc)
			}
		}
	}
	if len(c.Slots) > 0 {
		b.WriteString("\n\nSlots:\n")
		for _, sl := range c.Slots {
			fmt.Fprintf(&b, "\n- `%s`", sl.Name)
			if sl.Required {
				b.WriteString(" required")
			}
			if sl.Doc != "" {
				b.WriteString(" — " + sl.Doc)
			}
		}
	}
	return b.String()
}

// propSummary is a prop's type, default, requiredness and allowed values
// on one line: `string = plain, one of plain|featured`.
func propSummary(p brick.PropDoc) string {
	s := p.Type
	if p.HasDefault {
		if p.Default == "" {
			s += ` = ""` // an empty default, which %v would print as nothing
		} else {
			s += fmt.Sprintf(" = %v", p.Default)
		}
	}
	if p.Required {
		s += ", required"
	}
	if len(p.OneOf) > 0 {
		vals := make([]string, len(p.OneOf))
		for i, v := range p.OneOf {
			vals[i] = fmt.Sprint(v)
		}
		s += ", one of " + strings.Join(vals, "|")
	}
	return s
}

// propMarkdown is a prop's hover documentation.
func propMarkdown(p brick.PropDoc) string {
	s := fmt.Sprintf("**%s** `%s`", p.Name, propSummary(p))
	if p.Doc != "" {
		s += "\n\n" + p.Doc
	}
	if len(p.Examples) > 0 {
		s += "\n\nExamples: `" + strings.Join(p.Examples, "`, `") + "`"
	}
	return s
}

// scheduleCheck runs a site's check after the debounce, cancelling one
// that is pending or running: only the newest save's result is published.
func (s *Server) scheduleCheck(ctx context.Context, root string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.shutdown {
		return
	}
	if t := s.timers[root]; t != nil && t.Stop() {
		s.wg.Done()
	}
	if c := s.cancels[root]; c != nil {
		c()
	}
	cctx, cancel := context.WithCancel(ctx)
	s.cancels[root] = cancel
	s.wg.Add(1)
	s.timers[root] = time.AfterFunc(s.opts.Debounce, func() {
		defer s.wg.Done()
		s.runCheck(cctx, root)
	})
}

// runCheck checks a site and publishes its diagnostics per file, clearing
// files that no longer have any.
func (s *Server) runCheck(ctx context.Context, root string) {
	if ctx.Err() != nil {
		return
	}
	diags, err := s.opts.Project.Check(ctx, root)
	if ctx.Err() != nil {
		return
	}
	if err != nil {
		// The site does not open (brick.yaml broken): report it on the
		// marker file, the one place the author can fix it.
		diags = []brick.Diagnostic{{File: rootFile(root), Severity: brick.SeverityError, Message: err.Error()}}
	}
	byURI := map[string][]map[string]any{}
	for _, d := range diags {
		file := d.File
		if file == "" {
			file = rootFile(root)
		}
		p := filepath.Join(root, filepath.FromSlash(file))
		uri := pathToURI(p)
		byURI[uri] = append(byURI[uri], s.diagnostic(uri, p, d))
	}
	s.mu.Lock()
	prev := s.published[root]
	now := map[string]bool{}
	for uri := range byURI {
		now[uri] = true
	}
	s.published[root] = now
	s.mu.Unlock()
	for uri := range prev {
		if !now[uri] {
			byURI[uri] = []map[string]any{}
		}
	}
	for _, uri := range sortedKeys(byURI) {
		_ = s.conn.Notify(methodPublishDiags, map[string]any{"uri": uri, "diagnostics": byURI[uri]})
	}
}

// rootFile is the file a site-wide error is reported on: the first root
// marker that exists.
func rootFile(root string) string {
	for _, m := range RootMarkers {
		if _, err := os.Stat(filepath.Join(root, m)); err == nil {
			return m
		}
	}
	return RootMarkers[0]
}

// diagnostic turns a brick diagnostic into an LSP one, spanning from its
// column to the end of the line (the whole line when it has no column).
func (s *Server) diagnostic(uri, path string, d brick.Diagnostic) map[string]any {
	s.mu.Lock()
	text, open := s.docs[uri]
	s.mu.Unlock()
	if !open {
		b, _ := os.ReadFile(path)
		text = string(b)
	}
	start := 0
	if d.Line > 1 {
		start = OffsetOf(text, d.Line-1, 0)
	}
	end := strings.IndexByte(text[start:], '\n')
	if end < 0 {
		end = len(text)
	} else {
		end += start
	}
	end = start + len(strings.TrimRight(text[start:end], "\r \t"))
	if d.Col > 1 {
		start = min(start+d.Col-1, end)
	}
	if start == end {
		end = min(end+1, len(text))
	}
	sev := severityError
	if d.Severity == brick.SeverityWarning {
		sev = severityWarning
	}
	out := map[string]any{"range": rangeOf(text, start, end), "severity": sev, "source": diagnosticSource, "message": d.Message}
	if d.Code != "" {
		out["code"] = string(d.Code)
	}
	return out
}

// sortedKeys returns a map's keys in order, for stable output.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// uriToPath turns a file:// URI into a local path.
func uriToPath(uri string) (string, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return "", err
	}
	if u.Scheme != fileScheme {
		return "", fmt.Errorf("lsp: not a file URI: %s", uri)
	}
	return filepath.FromSlash(u.Path), nil
}

// pathToURI turns a local path into a file:// URI.
func pathToURI(p string) string {
	return (&url.URL{Scheme: fileScheme, Path: filepath.ToSlash(p)}).String()
}
