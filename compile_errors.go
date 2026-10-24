// Mapping a template error back to its source file and line: the position helpers and compiled.mapError.

package brick

import (
	"errors"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/ubgo/brick/internal/lex"
	"github.com/ubgo/brick/internal/transpile"
)

// advancePos returns the position after b, starting at p.
func advancePos(p lex.Pos, b []byte) lex.Pos {
	for _, ch := range b {
		if ch == '\n' {
			p.Line++
			p.Col = 1
		} else {
			p.Col++
		}
	}
	return p
}

// lineStarts indexes the byte offset of each line of s.
func lineStarts(s string) []int {
	out := []int{0}
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, i+1)
		}
	}
	return out
}

// tmplErrRE matches html/template and text/template error prefixes:
// "template: NAME:LINE:COL: msg" and "html/template:NAME:LINE:COL: msg".
var tmplErrRE = regexp.MustCompile(`^(?:html/)?template: ?([^:]+):(\d+)(?::(\d+))?: (.*)$`)

// execAtRE removes the generated define name from execution errors.
var execAtRE = regexp.MustCompile(`executing "[^"]*" at `)

// mapError rewrites a template error through the source map (T10), so the
// author sees their file, line and column, never generated source.
func (c *compiled) mapError(err error, code Code) *Error {
	var be *Error
	if errors.As(err, &be) {
		return be
	}
	msg := err.Error()
	m := tmplErrRE.FindStringSubmatch(msg)
	if m == nil {
		return &Error{File: c.path, Code: code, Err: err}
	}
	line, _ := strconv.Atoi(m[2])
	col := 0
	if m[3] != "" {
		col, _ = strconv.Atoi(m[3])
	}
	text := execAtRE.ReplaceAllString(m[4], "at ")
	text = strings.ReplaceAll(text, transpile.FnCallPrefix, "")
	fl, fc := c.sourcePos(line, col)
	return &Error{File: c.path, Line: fl, Col: fc, Code: code, Err: errors.New(text)}
}

// sourcePos maps a generated line and 0-based column to the author's file.
func (c *compiled) sourcePos(line, col int) (int, int) {
	if line < 1 || line > len(c.lines) {
		return 0, 0
	}
	off := c.lines[line-1] + col
	segs := c.out.Segments
	i, _ := slices.BinarySearchFunc(segs, off, func(s transpile.Segment, t int) int {
		switch {
		case t < s.Gen:
			return 1
		case t >= s.Gen+s.Len:
			return -1
		}
		return 0
	})
	if i >= len(segs) {
		i = len(segs) - 1
	}
	if i < 0 {
		return 0, 0
	}
	s := segs[i]
	if !s.Verbatim || off < s.Gen {
		return s.Src.Line, s.Src.Col
	}
	p := advancePos(lex.Pos{Line: s.Src.Line, Col: s.Src.Col}, []byte(c.out.Source[s.Gen:min(off, len(c.out.Source))]))
	return p.Line, p.Col
}

// pathBase is the last element of a slash path.
func pathBase(p string) string {
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[i+1:]
	}
	return p
}

// metaErrLine is the frontmatter line of a parseMeta error: the line of
// its brick: key when the error names one ("brick: temporary: …"), else 1.
func metaErrLine(src []byte, err error) int {
	for _, k := range brickKeys {
		if strings.HasPrefix(err.Error(), keyBrick+": "+k+":") {
			return brickLine(src, k)
		}
	}
	return 1
}
