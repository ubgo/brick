package brick

import (
	"errors"
	"fmt"
	"strings"
)

// Code is a stable identifier for a kind of problem. Codes are
// part of the compatibility promise: tools match on them, so a code is never
// renamed or reused.
type Code string

// Error and warning codes.
const (
	CodeSyntax        Code = "syntax"         // scanner, tree or template syntax
	CodeFrontmatter   Code = "frontmatter"    // frontmatter grammar or YAML
	CodeUnknownTag    Code = "unknown-tag"    // a tag that resolves to nothing
	CodeReserved      Code = "reserved"       // a reserved name was used
	CodeImport        Code = "import"         // a use:, src= or layout path problem
	CodePath          Code = "path"           // a path not starting with ~/ or leaving the root
	CodeProp          Code = "prop"           // a prop failed type, oneof or required
	CodeLayout        Code = "layout"         // missing layout or a layout cycle
	CodeRuntime       Code = "runtime"        // an execution error
	CodeLimit         Code = "limit"          // a limit was exceeded (E4)
	CodeCanceled      Code = "canceled"       // the render's context ended (E2)
	CodePanic         Code = "panic"          // a recovered panic in a hook or function
	CodeScript        Code = "script"         // a script failed to load or run
	CodeHook          Code = "hook"           // a hook returned an error
	CodeInject        Code = "inject"         // a required inject: key had no provider
	CodeRender        Code = "render"         // <brick:render> could not resolve its name
	CodeShadow        Code = "shadow"         // a name hidden by precedence (warning)
	CodeUnusedNode    Code = "unused-node"    // a block prop the component never reads
	CodeChildrenIf    Code = "children-if"    // a .Children entry inside {{ if }}
	CodeFalseString   Code = "false-string"   // an undeclared ="false" or ="0" (C6)
	CodeHTMLTag       Code = "html-tag"       // an uppercase HTML tag passed through (S7)
	CodeLayoutIgnored Code = "layout-ignored" // layout: in a component file
	CodeEngineKey     Code = "engine-key"     // an engine key in the wrong kind of file
	CodeProvide       Code = "provide"        // provide overriding frontmatter provide:
	CodeI18n          Code = "i18n"           // a missing translation key
	CodeZgotmplZ      Code = "zgotmplz"       // html/template replaced an unsafe URL (X6)
	CodeBoundary      Code = "boundary"       // a boundary warning (loading node)
	CodeAssets        Code = "assets"         // client code with no {{ assets }}
	CodeSlot          Code = "slot"           // .Slot read where it is always empty
	CodeUnkeyed       Code = "unkeyed"        // range rows with no brick:key in a stateful file
	CodeHTMLBalance   Code = "html-balance"   // an unclosed or stray HTML tag (warning)
	CodeHostOnly      Code = "host-only"      // a check skipped a page that uses a host-only stub (warning)
	CodeShape         Code = "prop-shape"     // a read or a value that does not fit a prop's fields:/of: shape (warning, G15)
)

// Severity says whether a Diagnostic fails the build.
type Severity int

// Severities.
const (
	SeverityError Severity = iota
	SeverityWarning
)

// String is "error" or "warning", as printed in a Diagnostic.
func (s Severity) String() string {
	if s == SeverityWarning {
		return "warning"
	}
	return wordError
}

// Error is every error brick reports: where it happened, what kind
// it is, and the component chain that led there.
//
// Invariant: File is a path relative to Options.FS (X2), never absolute, so
// an error is safe to show and stable across machines.
type Error struct {
	File      string
	Line      int
	Col       int
	Component string
	// Chain is the component path from the page down, outermost first.
	Chain    []string
	Code     Code
	Severity Severity
	Err      error
}

// Error prints file:line:col, the component, the message and the chain.
func (e *Error) Error() string {
	var b strings.Builder
	if e.File != "" {
		b.WriteString(e.File)
		if e.Line > 0 {
			fmt.Fprintf(&b, ":%d", e.Line)
			if e.Col > 0 {
				fmt.Fprintf(&b, ":%d", e.Col)
			}
		}
		b.WriteString(": ")
	}
	if e.Component != "" {
		fmt.Fprintf(&b, "<%s>: ", e.Component)
	}
	if e.Err != nil {
		b.WriteString(e.Err.Error())
	}
	if len(e.Chain) > 1 {
		b.WriteString(" (in " + strings.Join(e.Chain, " → ") + ")")
	}
	return b.String()
}

// Unwrap exposes the cause, so errors.Is(err, context.Canceled) works.
func (e *Error) Unwrap() error { return e.Err }

// ErrorList is several errors, reported together (CompileAll, boundaries).
type ErrorList []*Error

// Error prints one error per line.
func (l ErrorList) Error() string {
	parts := make([]string, len(l))
	for i, e := range l {
		parts[i] = e.Error()
	}
	return strings.Join(parts, "\n")
}

// Unwrap lets errors.Is and errors.As see every error in the list.
func (l ErrorList) Unwrap() []error {
	out := make([]error, len(l))
	for i, e := range l {
		out[i] = e
	}
	return out
}

// Diagnostic is one finding from compiling or rendering: the same fields as
// Error, so one formatter serves both.
type Diagnostic struct {
	File     string
	Line     int
	Col      int
	Severity Severity
	Code     Code
	Message  string
}

// String prints "file:line:col: severity: message", the format of go vet.
func (d Diagnostic) String() string {
	loc := d.File
	if d.Line > 0 {
		loc += fmt.Sprintf(":%d", d.Line)
		if d.Col > 0 {
			loc += fmt.Sprintf(":%d", d.Col)
		}
	}
	return fmt.Sprintf("%s: %s: %s", loc, d.Severity, d.Message)
}

// asError returns err as *Error, wrapping it with code when it is not one.
func asError(err error, code Code, file string) *Error {
	var be *Error
	if errors.As(err, &be) {
		return be
	}
	return &Error{File: file, Code: code, Err: err}
}
