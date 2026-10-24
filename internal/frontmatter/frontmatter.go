// Package frontmatter splits a brick file into its YAML frontmatter and its
// body, and normalizes the YAML values into brick's canonical type set.
//
// Why it exists: every brick file — page, layout, component — has the same
// shape: optional YAML between `---` lines, then a Go template. A near-miss in
// the delimiter grammar would render the frontmatter as visible page text,
// which is a silent failure, so the grammar is strict and every near-miss is
// either an error or a warning. Values are normalized once here so
// YAML, JSON and TOML data all reach templates with the same Go types (D1).
package frontmatter

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"time"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"
)

// delimiter is the line that opens and closes a frontmatter block.
const delimiter = "---"

// bom is the UTF-8 byte-order mark some Windows editors prepend; it is
// stripped rather than treated as content.
var bom = []byte{0xEF, 0xBB, 0xBF}

// ErrUnclosed reports a file whose opening `---` has no closing `---`.
var ErrUnclosed = errors.New("frontmatter: opening --- has no closing ---")

// ErrInvalidUTF8 reports input that is not valid UTF-8.
var ErrInvalidUTF8 = errors.New("frontmatter: file is not valid UTF-8")

// ErrNotMapping reports frontmatter whose top level is not a key/value map.
var ErrNotMapping = errors.New("frontmatter: must be a map of keys to values")

// PosError carries the 1-based line (and byte offset for encoding errors) of a
// problem in the original file, so callers can report file:line.
type PosError struct {
	Line   int
	Offset int
	Err    error
}

// Error prefixes the cause with its line in the file, so a frontmatter
// mistake points at the line the author wrote.
func (e *PosError) Error() string { return fmt.Sprintf("line %d: %v", e.Line, e.Err) }

// Unwrap exposes the sentinel so callers can match with errors.Is.
func (e *PosError) Unwrap() error { return e.Err }

// Warning is a non-fatal finding, returned as a value so it is never lost
// when the host does not log.
type Warning struct {
	Line    int
	Message string
}

// Doc is a parsed file.
//
// Invariant: Body is the author's bytes, unchanged except that a leading BOM
// and the frontmatter block (plus exactly one newline after the closing
// delimiter) are removed. BodyLine is the 1-based line in the original file
// where Body starts, used by the source map.
type Doc struct {
	// Params holds the frontmatter; empty (never nil) when there is none.
	Params         map[string]any
	HasFrontmatter bool
	Body           []byte
	BodyLine       int
	Warnings       []Warning
}

// Parse splits src into frontmatter and body. It never modifies src.
func Parse(src []byte) (Doc, error) {
	if !utf8.Valid(src) {
		return Doc{}, &PosError{Line: lineOf(src, invalidAt(src)), Offset: invalidAt(src), Err: ErrInvalidUTF8}
	}
	src = bytes.TrimPrefix(src, bom)
	doc := Doc{Params: map[string]any{}, Body: src, BodyLine: 1}

	first, rest, ok := cutLine(src)
	if !ok || !isDelimiter(first) {
		if w, found := leadingBlankThenDelimiter(src); found {
			doc.Warnings = append(doc.Warnings, w)
		}
		return doc, nil
	}

	// Find the closing delimiter line.
	yamlStart := len(src) - len(rest)
	offset := yamlStart
	line := 2
	for {
		l, r, more := cutLine(src[offset:])
		if isDelimiter(l) {
			yamlSrc := src[yamlStart:offset]
			params, err := decode(yamlSrc)
			if err != nil {
				return Doc{}, err
			}
			doc.Params = params
			doc.HasFrontmatter = true
			// Exactly one newline after the closing delimiter is consumed.
			bodyStart := len(src) - len(r)
			doc.Body = src[bodyStart:]
			doc.BodyLine = line + 1
			if !more {
				doc.Body = src[len(src):]
			}
			return doc, nil
		}
		if !more {
			return Doc{}, &PosError{Line: 1, Err: ErrUnclosed}
		}
		offset = len(src) - len(r)
		line++
	}
}

// decode parses the YAML block and normalizes every value (D1).
func decode(yamlSrc []byte) (map[string]any, error) {
	var node yaml.Node
	if err := yaml.Unmarshal(yamlSrc, &node); err != nil {
		return nil, shiftYAMLError(err)
	}
	if node.Kind == 0 || len(node.Content) == 0 {
		return map[string]any{}, nil
	}
	root := node.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil, &PosError{Line: root.Line + 1, Err: ErrNotMapping}
	}
	var raw any
	if err := root.Decode(&raw); err != nil {
		return nil, shiftYAMLError(err)
	}
	norm, ok := Normalize(raw).(map[string]any)
	if !ok {
		return nil, &PosError{Line: root.Line + 1, Err: ErrNotMapping}
	}
	return norm, nil
}

// Normalize converts decoded YAML/JSON/TOML values into brick's canonical type
// set: maps become map[string]any, integers int64, other numbers
// float64, dates time.Time; lists and scalars otherwise keep their value.
// It is exported so every data loader shares one conversion.
func Normalize(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			out[k] = Normalize(val)
		}
		return out
	case map[any]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			out[fmt.Sprint(k)] = Normalize(val)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, val := range x {
			out[i] = Normalize(val)
		}
		return out
	case int:
		return int64(x)
	case int32:
		return int64(x)
	case uint64:
		if x <= 1<<63-1 {
			return int64(x)
		}
		return float64(x)
	case float32:
		return float64(x)
	case time.Time:
		return x
	default:
		return v
	}
}

// shiftYAMLError moves YAML line numbers from the block to the file: the
// block starts on line 2 (after the opening delimiter).
func shiftYAMLError(err error) error {
	msg := err.Error()
	// Decode errors (duplicate keys, bad types) arrive as a *yaml.TypeError
	// whose messages each start "line N: ..."; report the first one.
	var te *yaml.TypeError
	if errors.As(err, &te) && len(te.Errors) > 0 {
		msg = "yaml: " + te.Errors[0]
	}
	// yaml.v3 messages look like "yaml: line 3: mapping values ...".
	const prefix = "yaml: line "
	if i := bytes.Index([]byte(msg), []byte(prefix)); i >= 0 {
		rest := msg[i+len(prefix):]
		j := 0
		for j < len(rest) && rest[j] >= '0' && rest[j] <= '9' {
			j++
		}
		if n, convErr := strconv.Atoi(rest[:j]); convErr == nil {
			return &PosError{Line: n + 1, Err: fmt.Errorf("frontmatter: %s", rest[j:])}
		}
	}
	return fmt.Errorf("frontmatter: %w", err)
}

// isDelimiter reports whether a line (without its newline) is `---` with
// optional trailing spaces, tabs or a carriage return.
func isDelimiter(line []byte) bool {
	line = bytes.TrimRight(line, " \t\r")
	return string(line) == delimiter
}

// cutLine returns the first line (without "\n"), the rest after the newline,
// and whether a newline was found.
func cutLine(b []byte) (line, rest []byte, found bool) {
	return bytes.Cut(b, []byte("\n"))
}

// leadingBlankThenDelimiter detects the near-miss of blank lines before an
// opening delimiter, which would render the frontmatter as page text.
func leadingBlankThenDelimiter(src []byte) (Warning, bool) {
	line := 1
	for {
		l, rest, more := cutLine(src)
		if len(bytes.TrimSpace(l)) != 0 {
			if isDelimiter(l) && line > 1 {
				return Warning{Line: line, Message: "blank line before --- — this frontmatter is rendered as page text"}, true
			}
			return Warning{}, false
		}
		if !more {
			return Warning{}, false
		}
		src = rest
		line++
	}
}

func invalidAt(b []byte) int {
	for i := 0; i < len(b); {
		r, size := utf8.DecodeRune(b[i:])
		if r == utf8.RuneError && size == 1 {
			return i
		}
		i += size
	}
	return len(b)
}

func lineOf(b []byte, offset int) int {
	return bytes.Count(b[:offset], []byte("\n")) + 1
}
