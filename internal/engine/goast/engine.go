// Package goast is the Go language implementation of domain.Engine.
//
// It is the only package in golens permitted to import go/parser, go/ast and go/token.
// Keeping the stdlib AST behind this boundary is what lets v0.2 add a Tree-Sitter engine
// without the domain or usecase layers changing.
package goast

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"

	"github.com/CodeMonkey80s/golens/internal/domain"
)

// Engine parses Go source files.
//
// The zero value is ready to use. Engine is stateless and safe for concurrent use: all
// mutable parse state lives inside Parse.
type Engine struct {
	// ParseComments controls whether comments are attached to the AST. It is false by
	// default because comment parsing costs measurably more time and nothing in the v0.1
	// output surface needs it. See specification 1.1 and the 5ms budget in 3.2.
	ParseComments bool
}

// New returns an Engine configured for v0.1 defaults.
func New() *Engine { return &Engine{} }

// Parse reads path and returns its structural document.
//
// A syntax error is returned as *ParseError so the CLI can distinguish it from a missing
// file without inspecting error strings.
func (e *Engine) Parse(path string) (domain.Document, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return domain.Document{}, fmt.Errorf("read %s: %w", path, err)
	}

	doc, err := e.ParseSource(path, src)
	if err != nil {
		return domain.Document{}, err
	}
	return doc, nil
}

// ParseSource parses in-memory source attributed to path.
//
// This exists so tests can assert the byte-offset contract against literal source without
// writing temporary files, and so a future stdin or buffer-based input needs no new
// engine method.
func (e *Engine) ParseSource(path string, src []byte) (domain.Document, error) {
	// Comments are off by default: parsing them costs measurably more time and nothing in
	// the v0.1 output surface needs them. Object resolution is deliberately left ON --
	// it populates ast.File.Unresolved, which the unused-import check in check depends
	// on. Correctness over a microsecond.
	mode := parser.Mode(0)
	if e.ParseComments {
		mode |= parser.ParseComments
	}

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, src, mode)
	if err != nil {
		return domain.Document{}, newParseError(path, err)
	}

	tf := fset.File(file.Pos())
	if tf == nil {
		return domain.Document{}, fmt.Errorf("parse %s: no file position recorded", path)
	}

	pkgName := ""
	if file.Name != nil {
		pkgName = file.Name.Name
	}

	doc := domain.Document{
		Path:     filepath.ToSlash(path),
		Package:  pkgName,
		Bounds:   fileBounds(tf, len(src)),
		Source:   src,
		Imports:  importPaths(file),
		Comments: e.ParseComments,
	}

	b := &builder{fset: fset, tf: tf}
	doc.Nodes = b.nodes(file)
	doc.Symbols = b.symbols(file)
	return doc, nil
}

// fileBounds returns the bounds of the whole file.
//
// StartByte is 0 rather than the base offset fset assigned to this file, so offsets are
// absolute within the file's own byte slice. That is what makes doc.Source[bounds slices]
// work directly.
func fileBounds(tf *token.File, size int) domain.Bounds {
	return domain.Bounds{
		StartLine: 1,
		EndLine:   tf.LineCount(),
		StartByte: 0,
		EndByte:   size,
	}
}

// nodeBounds converts an AST node position pair into domain.Bounds.
//
// EndLine is derived from the offset one byte before EndByte rather than from the End
// position itself. An AST node's End is one past its last byte, which for a declaration
// ending at column 1 of a line already reports the following line; using EndByte-1 gives
// the line the declaration actually ends on.
func nodeBounds(tf *token.File, start, end token.Pos) domain.Bounds {
	startOffset := offsetOf(tf, start)
	endOffset := offsetOf(tf, end)

	b := domain.Bounds{
		StartByte: startOffset,
		EndByte:   endOffset,
		StartLine: tf.Line(start),
	}
	if endOffset > startOffset {
		b.EndLine = tf.Line(start + token.Pos(endOffset-startOffset) - 1)
	} else {
		b.EndLine = b.StartLine
	}
	return b
}

func offsetOf(tf *token.File, pos token.Pos) int {
	off := tf.Offset(pos)
	if off < 0 {
		return 0
	}
	return off
}

func importPaths(file *ast.File) []string {
	out := make([]string, 0, len(file.Imports))
	for _, imp := range file.Imports {
		if imp.Path == nil {
			continue
		}
		out = append(out, imp.Path.Value)
	}
	return out
}

type builder struct {
	fset *token.FileSet
	tf   *token.File
}

// symbols collects package-level declarations: funcs, methods, types, vars and consts,
// both exported and unexported.
//
// Methods are qualified as Type.Method using the receiver's base type name, matching the
// lookup format in specification 2.2.
func (b *builder) symbols(file *ast.File) []domain.Symbol {
	var out []domain.Symbol

	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			out = append(out, b.funcSymbol(d))
		case *ast.GenDecl:
			out = append(out, b.genSymbols(d)...)
		}
	}
	return out
}

func (b *builder) funcSymbol(fn *ast.FuncDecl) domain.Symbol {
	name := fn.Name.Name
	kind := domain.KindFunc
	if fn.Recv != nil && len(fn.Recv.List) > 0 {
		kind = domain.KindMethod
		if recv := receiverTypeName(fn.Recv.List[0].Type); recv != "" {
			name = recv + "." + name
		}
	}

	return domain.Symbol{
		Name:          fn.Name.Name,
		QualifiedName: name,
		Kind:          kind,
		Bounds:        nodeBounds(b.tf, fn.Pos(), fn.End()),
		Detail:        signature(fn),
		Receiver:      receiverText(fn.Recv),
	}
}

// receiverText renders a method receiver for the declaration head, preserving the
// pointer star and the receiver variable name: "*UserStore", "r *UserStore".
//
// The rendered form always names the variable "s" so output is stable regardless of how
// the source named it; the type is what carries meaning for a consumer.
func receiverText(recv *ast.FieldList) string {
	if recv == nil || len(recv.List) == 0 {
		return ""
	}
	field := recv.List[0]

	var sb strings.Builder
	if _, ok := field.Type.(*ast.StarExpr); ok {
		sb.WriteByte('*')
	}
	if t := receiverTypeName(field.Type); t != "" {
		sb.WriteString(t)
	}
	return sb.String()
}

func (b *builder) genSymbols(gen *ast.GenDecl) []domain.Symbol {
	kind := kindForToken(gen.Tok)
	var out []domain.Symbol

	for _, spec := range gen.Specs {
		switch s := spec.(type) {
		case *ast.TypeSpec:
			detail := ""
			if s.Type != nil {
				detail = typeKeyword(s.Type)
			}
			out = append(out, domain.Symbol{
				Name:          s.Name.Name,
				QualifiedName: s.Name.Name,
				Kind:          kind,
				Bounds:        nodeBounds(b.tf, s.Pos(), s.End()),
				Detail:        detail,
			})
		case *ast.ValueSpec:
			for _, name := range s.Names {
				detail := ""
				if s.Type != nil {
					detail = typeKeyword(s.Type)
				}
				out = append(out, domain.Symbol{
					Name:          name.Name,
					QualifiedName: name.Name,
					Kind:          kind,
					// Bounds span the name, not the whole spec, so a grouped var or const
					// declaration yields independently addressable symbols: editing one
					// must not overwrite its neighbours.
					Bounds: nodeBounds(b.tf, name.Pos(), name.End()),
					Detail: detail,
				})
			}
		}
	}
	return out
}

// nodes builds the outline tree: imports first, then declarations.
func (b *builder) nodes(file *ast.File) []domain.Node {
	var nodes []domain.Node

	if len(file.Imports) > 0 {
		children := make([]domain.Node, 0, len(file.Imports))
		for _, imp := range file.Imports {
			child := domain.Node{Name: importName(imp), Kind: domain.KindImport}
			if imp.Path != nil {
				child.Detail = imp.Path.Value
				child.Bounds = nodeBounds(b.tf, imp.Pos(), imp.End())
			}
			children = append(children, child)
		}
		nodes = append(nodes, domain.Node{
			Name:     "IMPORTS",
			Kind:     domain.KindImport,
			IsGroup:  true,
			Bounds:   groupBounds(children),
			Children: children,
		})
	}

	// Types and funcs are grouped so the human outline can print section headers with an
	// aggregate line range. The grouping is presentational; Symbols remains flat.
	var types, funcs, values []domain.Node

	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			kind := domain.KindFunc
			receiver := ""
			if d.Recv != nil && len(d.Recv.List) > 0 {
				kind = domain.KindMethod
				receiver = receiverText(d.Recv)
			}
			funcs = append(funcs, domain.Node{
				Name:     d.Name.Name,
				Kind:     kind,
				Detail:   signature(d),
				Receiver: receiver,
				Bounds:   nodeBounds(b.tf, d.Pos(), d.End()),
				Children: b.children(d),
			})
		case *ast.GenDecl:
			kind := kindForToken(d.Tok)
			if kind != domain.KindType && kind != domain.KindVar && kind != domain.KindConst {
				continue
			}
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					node := domain.Node{
						Name:   s.Name.Name,
						Kind:   kind,
						Bounds: nodeBounds(b.tf, s.Pos(), s.End()),
					}
					if s.Type != nil {
						node.Detail = typeKeyword(s.Type)
						node.Children = b.typeChildren(s.Type)
					}
					types = append(types, node)
				case *ast.ValueSpec:
					for _, name := range s.Names {
						node := domain.Node{
							Name: name.Name,
							Kind: kind,
							// One spec can bind several names, so bounds span the name
							// itself rather than the whole spec; an agent editing a single
							// constant must not overwrite its neighbours.
							Bounds: nodeBounds(b.tf, name.Pos(), name.End()),
						}
						if s.Type != nil {
							node.Detail = typeKeyword(s.Type)
						}
						values = append(values, node)
					}
				}
			}
		}
	}

	// Group order mirrors the specification's example: types, then funcs and methods.
	// Vars and consts come last because they are the least commonly queried.
	if len(types) > 0 {
		nodes = append(nodes, domain.Node{
			Name:     "TYPES & STRUCTS",
			Kind:     domain.KindType,
			IsGroup:  true,
			Bounds:   groupBounds(types),
			Children: types,
		})
	}
	if len(funcs) > 0 {
		nodes = append(nodes, domain.Node{
			Name:     "FUNCTIONS & METHODS",
			Kind:     domain.KindFunc,
			IsGroup:  true,
			Bounds:   groupBounds(funcs),
			Children: funcs,
		})
	}
	if len(values) > 0 {
		nodes = append(nodes, domain.Node{
			Name:     "VARS & CONSTS",
			Kind:     domain.KindVar,
			IsGroup:  true,
			Bounds:   groupBounds(values),
			Children: values,
		})
	}

	return nodes
}

// typeChildren exposes struct fields at depth 2, per specification 2.1.
func (b *builder) typeChildren(expr ast.Expr) []domain.Node {
	st, ok := expr.(*ast.StructType)
	if !ok || st.Fields == nil || st.Fields.NumFields() == 0 {
		return nil
	}

	out := make([]domain.Node, 0, st.Fields.NumFields())
	for _, field := range st.Fields.List {
		node := domain.Node{
			Kind:   domain.KindField,
			Bounds: nodeBounds(b.tf, field.Pos(), field.End()),
		}
		if len(field.Names) == 0 {
			// Embedded field: the type expression is the name.
			node.Name = exprString(field.Type)
			node.Detail = exprString(field.Type)
		} else {
			node.Name = field.Names[0].Name
			node.Detail = exprString(field.Type)
		}
		out = append(out, node)
	}
	return out
}

// children exposes function parameters at depth 2. Parameters have no further
// structure worth showing, so this never recurses.
func (b *builder) children(fn *ast.FuncDecl) []domain.Node {
	if fn.Type == nil || fn.Type.Params == nil {
		return nil
	}
	return b.fieldNodes(fn.Type.Params)
}

func (b *builder) fieldNodes(list *ast.FieldList) []domain.Node {
	if list == nil {
		return nil
	}
	out := make([]domain.Node, 0, len(list.List))
	for _, field := range list.List {
		for _, name := range field.Names {
			out = append(out, domain.Node{
				Name:   name.Name,
				Kind:   domain.KindField,
				Detail: exprString(field.Type),
				Bounds: nodeBounds(b.tf, field.Pos(), field.End()),
			})
		}
	}
	return out
}

// groupBounds spans the first to last member of a group, which is what the human outline
// prints as a section header range.
func groupBounds(members []domain.Node) domain.Bounds {
	if len(members) == 0 {
		return domain.Bounds{}
	}
	return domain.Bounds{
		StartLine: members[0].Bounds.StartLine,
		EndLine:   members[len(members)-1].Bounds.EndLine,
		StartByte: members[0].Bounds.StartByte,
		EndByte:   members[len(members)-1].Bounds.EndByte,
	}
}

func kindForToken(tok token.Token) domain.Kind {
	switch tok {
	case token.TYPE:
		return domain.KindType
	case token.VAR:
		return domain.KindVar
	case token.CONST:
		return domain.KindConst
	default:
		return domain.KindFunc
	}
}

// receiverTypeName returns the base type name of a method receiver, ignoring pointers,
// generics and package qualifiers: *UserStore, store.User, UserStore[T] all yield
// "UserStore".
func receiverTypeName(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.StarExpr:
		return receiverTypeName(t.X)
	case *ast.Ident:
		return t.Name
	case *ast.IndexExpr:
		return receiverTypeName(t.X)
	case *ast.IndexListExpr:
		return receiverTypeName(t.X)
	case *ast.SelectorExpr:
		return t.Sel.Name
	default:
		return ""
	}
}

func importName(imp *ast.ImportSpec) string {
	if imp.Name != nil {
		return imp.Name.Name
	}
	if imp.Path == nil {
		return ""
	}
	// Default local name is the last path element, which is what the parser's object
	// resolution would bind. "context" for "context", "json" for "encoding/json".
	path := unquote(imp.Path.Value)
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' {
			return path[i+1:]
		}
	}
	return path
}

func unquote(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	return s
}
