package goast

import (
	"go/scanner"
	"go/token"

	"github.com/CodeMonkey80s/golens/internal/domain"
)

// ParseError reports a syntax error in a source file.
//
// It exists so the CLI can map parse failures to exit code 4 and print
// file:line:col: message diagnostics without inspecting error strings. A scanner.ErrorList
// may contain several entries; all of them are preserved.
type ParseError struct {
	Path   string
	Errors scanner.ErrorList
}

func newParseError(path string, err error) *ParseError {
	pe := &ParseError{Path: path}
	if list, ok := err.(scanner.ErrorList); ok {
		pe.Errors = list
		return pe
	}
	// parser.ParseFile reports a non-ErrorList error only for degenerate input such as a
	// nil file set; wrap it so the message is not lost.
	pe.Errors = scanner.ErrorList{&scanner.Error{
		Pos: token.Position{Filename: path, Line: 1, Column: 1},
		Msg: err.Error(),
	}}
	return pe
}

// LocatedError is one parse error with its position already resolved.
//
// It lives in domain rather than here because two layers need it: the engine produces it and
// usecase consumes it, and internal/arch forbids usecase importing goast. Returning positioned
// data rather than a formatted "file:line:col: message" string is what lets a consumer carry the
// line and column instead of discarding them -- and discarding them is what once made check
// report every parse error at line 1, column 1.
type LocatedError = domain.LocatedError

func (e *ParseError) Error() string {
	if len(e.Errors) == 0 {
		return "parse " + e.Path + ": syntax error"
	}
	// The scanner's message already names the file, so it is returned on its own. Wrapping it
	// with the path again produced "/a.go: /a.go:3:9: ..." at the call site.
	return e.Errors[0].Error()
}

// LocatedErrors returns one entry per parse error, each carrying the file, line and column the
// parser reported. An error without a filename inherits this ParseError's path.
//
// This replaces the previous string-returning Diagnostics. Multiple errors are preserved, since
// a scanner.ErrorList may hold several and a caller reporting them as diagnostics wants them all.
func (e *ParseError) LocatedErrors() []LocatedError {
	out := make([]LocatedError, 0, len(e.Errors))
	for _, err := range e.Errors {
		located := LocatedError{Path: err.Pos.Filename, Line: err.Pos.Line, Column: err.Pos.Column, Message: err.Msg}
		if located.Path == "" {
			located.Path = e.Path
		}
		if located.Line == 0 {
			located.Line = 1
		}
		if located.Column == 0 {
			located.Column = 1
		}
		out = append(out, located)
	}
	return out
}
