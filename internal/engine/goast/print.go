package goast

import (
	"go/ast"
	"go/printer"
	"go/token"
	"strings"
	"unicode/utf8"
)

// maxDetailRunes caps generated detail strings.
//
// The outline and get output are consumed by agents under a token budget, and a very long
// generic instantiation or struct literal in a Detail field is noise. Truncating keeps
// the shape of the declaration visible without letting one node dominate the payload.
const maxDetailRunes = 120

// exprString renders an AST expression as compact source text.
//
// printer.Fprint writes to a strings.Builder without inserting newlines for the
// expressions used here, so single-line results are the norm. A failure is not possible
// for well-formed ASTs, but is tolerated by returning the empty string rather than
// panicking: a missing Detail never invalidates the bounds around it.
func exprString(expr ast.Expr) string {
	if expr == nil {
		return ""
	}

	var sb strings.Builder
	cfg := printer.Config{Mode: printer.RawFormat}
	if err := cfg.Fprint(&sb, token.NewFileSet(), expr); err != nil {
		return ""
	}
	return truncate(sb.String())
}

// signature renders a function's parameter list followed by its results, matching the
// shape shown in the specification's outline example:
//
//	(ctx context.Context, id int) (*User, error)
func signature(fn *ast.FuncDecl) string {
	if fn == nil || fn.Type == nil {
		return ""
	}

	var sb strings.Builder
	sb.WriteString(fieldListString(fn.Type.Params))
	if results := resultsString(fn.Type.Results); results != "" {
		sb.WriteString(" ")
		sb.WriteString(results)
	}
	return truncate(sb.String())
}

// funcSignature renders a function type's parameter and result lists without the leading
// "func" keyword: "(ctx context.Context, id int) (*User, error)".
//
// The keyword is omitted because this renders interface methods, where the declaration is
// "Get(ctx context.Context, id int) (*User, error)" and a "func" keyword is a syntax error.
// Printing an *ast.FuncType directly would include it.
func funcSignature(fn *ast.FuncType) string {
	if fn == nil {
		return ""
	}

	var sb strings.Builder
	sb.WriteString(fieldListString(fn.Params))
	if results := resultsString(fn.Results); results != "" {
		sb.WriteString(" ")
		sb.WriteString(results)
	}
	return truncate(sb.String())
}

// fieldListString renders a parameter list as "(ctx context.Context, id int)".
//
// A FieldList is not an ast.Expr, so it cannot go through exprString. Parameter names are
// included because the specification's outline example shows them, which is also what lets
// a reader identify a parameter without opening the file.
func fieldListString(list *ast.FieldList) string {
	if list == nil {
		return "()"
	}

	var sb strings.Builder
	sb.WriteByte('(')
	for i, field := range list.List {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(namesWithTypes(field))
	}
	sb.WriteByte(')')
	return truncate(sb.String())
}

// resultsString renders a result list in Go's own convention: a single unnamed result has
// no parentheses, anything else is parenthesised. A function returning nothing renders "".
func resultsString(list *ast.FieldList) string {
	if list == nil || len(list.List) == 0 {
		return ""
	}

	if len(list.List) == 1 && len(list.List[0].Names) == 0 {
		return exprString(list.List[0].Type)
	}

	var sb strings.Builder
	sb.WriteByte('(')
	for i, field := range list.List {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(namesWithTypes(field))
	}
	sb.WriteByte(')')
	return truncate(sb.String())
}

// namesWithTypes renders one field as "name type", or just the type when the field is
// unnamed, which is the common case for results.
func namesWithTypes(field *ast.Field) string {
	if len(field.Names) == 0 {
		return exprString(field.Type)
	}

	typ := exprString(field.Type)
	parts := make([]string, 0, len(field.Names))
	for _, n := range field.Names {
		parts = append(parts, n.Name+" "+typ)
	}
	return strings.Join(parts, ", ")
}

// typeKeyword returns the short rendering of a type for a Detail field.
//
// The specification's outline example writes "type User struct" rather than expanding the
// whole struct body, so a struct renders as "struct" and other composite literals as their
// keyword. Anything else falls back to the source text.
func typeKeyword(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.StructType:
		return "struct"
	case *ast.InterfaceType:
		return "interface"
	case *ast.FuncType:
		return "func"
	case *ast.MapType:
		return "map[" + exprString(t.Key) + "]" + exprString(t.Value)
	case *ast.ChanType:
		if t.Dir == ast.RECV {
			return "<-chan " + exprString(t.Value)
		}
		return "chan " + exprString(t.Value)
	case *ast.StarExpr:
		return "*" + exprString(t.X)
	case *ast.ArrayType:
		if t.Len == nil {
			return "[]" + exprString(t.Elt)
		}
		return "[" + exprString(t.Len) + "]" + exprString(t.Elt)
	case *ast.Ellipsis:
		return "..." + exprString(t.Elt)
	case *ast.IndexExpr, *ast.IndexListExpr:
		return exprString(expr)
	default:
		return exprString(expr)
	}
}

// truncate shortens s to maxDetailRunes, appending an ellipsis marker when it cuts.
// Truncation happens on a rune boundary so multi-byte identifiers are never split.
func truncate(s string) string {
	if s == "" || utf8.RuneCountInString(s) <= maxDetailRunes {
		return s
	}

	count := 0
	for i := range s {
		if count == maxDetailRunes {
			return s[:i] + "..."
		}
		count++
	}
	return s
}
