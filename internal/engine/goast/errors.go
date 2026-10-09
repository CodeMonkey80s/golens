package goast

import (
	"go/scanner"
	"go/token"
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

func (e *ParseError) Error() string {
	if len(e.Errors) == 0 {
		return "parse " + e.Path + ": syntax error"
	}
	return e.Errors[0].Error()
}

// Diagnostics returns one "file:line:col: message" string per error, in the form the
// specification's check output uses.
func (e *ParseError) Diagnostics() []string {
	out := make([]string, 0, len(e.Errors))
	for _, err := range e.Errors {
		pos := err.Pos
		if pos.Filename == "" {
			pos.Filename = e.Path
		}
		out = append(out, pos.String()+": "+err.Msg)
	}
	return out
}
