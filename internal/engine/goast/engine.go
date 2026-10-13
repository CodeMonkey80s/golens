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
		// The path is not added here. os.ReadFile's error already names it -- "open
		// /abs/path: no such file or directory" -- and ParseError prepends the path a caller
		// asked about, so wrapping it as well printed the same path three times in one
		// diagnostic. The caller already knows which file it asked to read.
		return domain.Document{}, err
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
	// the v0.1 output surface needs them.
	//
	// Object resolution is skipped, and it used to be left ON for a stated reason that was
	// false. The comment claimed it "populates ast.File.Unresolved, which the unused-import
	// check in check depends on". Nothing reads Unresolved: the only reference to it in the
	// repository was that comment. The unused-import check is collectUsedNames, a lexical
	// scan over doc.Source that never touches the AST — see its comment for why it is
	// lexical rather than AST-based.
	//
	// So the pass was being paid for and never used. The resolver's Visit is ~17% of parse
	// time on a profile, and skipping it is measurably better than that on a real 20 KB file:
	// 192 µs -> 136 µs and 4060 -> 3487 allocs per parse. It populates Ident.Obj,
	// File.Scope and File.Unresolved; the builder reads Decls, Imports, Name, Recv, Params,
	// Results, TypeParams, Specs, Fields, Assign and Type, none of which the resolver
	// produces. TestSkipObjectResolutionProducesIdenticalDocuments pins that equivalence
	// rather than trusting this comment, which is what the last version of it deserved.
	//
	// It is also strictly safer: the resolver is the stage that panics on some malformed
	// input, and golens must never panic on a file an agent asked about.
	mode := parser.SkipObjectResolution
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
		// BuildConstraint is surfaced so a caller editing a file can tell whether the
		// current platform compiles it. golens parses every file regardless, so nothing
		// else in the document hints at the constraint.
		BuildConstraint: buildConstraintText(src),
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
		TypeParams:    typeParamsText(declTypeParams(fn)),
		Detail:        signature(fn),
		Receiver:      receiverText(fn.Recv),
	}
}

// declTypeParams returns a function declaration's own type parameter list.
//
// A FuncDecl carries its FuncType on the Type field, which may be nil for a declaration that did
// not parse completely; the guard keeps a partial parse from panicking the way a direct
// d.Type.TypeParams would.
func declTypeParams(fn *ast.FuncDecl) *ast.FieldList {
	if fn == nil || fn.Type == nil {
		return nil
	}
	return fn.Type.TypeParams
}

// receiverText renders a method receiver for the declaration head, without the
// surrounding parentheses: "s *UserStore", "g G[T]", "s sync.Locker".
//
// The source spelling is preserved deliberately. An earlier version invented the variable
// name "s" and reduced the type to its base name, which rendered "func (s Locker) Ext()"
// for a "func (s sync.Locker) Ext()" declaration. A consumer copying a declaration head
// into an edit needs text that compiles, so the receiver is reproduced as written,
// including the variable name, the package qualifier and any type parameters.
func receiverText(recv *ast.FieldList) string {
	if recv == nil || len(recv.List) == 0 {
		return ""
	}
	field := recv.List[0]

	typ := exprString(field.Type)
	if typ == "" {
		return ""
	}
	// An unnamed receiver -- "func (T) M()" -- is just its type.
	if len(field.Names) == 0 {
		return typ
	}
	return field.Names[0].Name + " " + typ
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
				TypeParams:    typeParamsText(s.TypeParams),
				// Assign is set only for "type A = B"; token.NoPos otherwise. See
				// domain.Node.IsAlias for why the distinction has to be carried.
				IsAlias: s.Assign.IsValid(),
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
				Name: d.Name.Name,
				Kind: kind,
				// A generic function declares its own type parameters, "Sort[S ~[]E,
				// E cmp.Ordered]". Without them the rendered head reads "func Sort(x S)"
				// and names two undefined identifiers.
				TypeParams: typeParamsText(declTypeParams(d)),
				Detail:     signature(d),
				Receiver:   receiver,
				Bounds:     nodeBounds(b.tf, d.Pos(), d.End()),
				Children:   b.children(d),
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
						Name:       s.Name.Name,
						Kind:       kind,
						TypeParams: typeParamsText(s.TypeParams),
						IsAlias:    s.Assign.IsValid(),
						Bounds:     nodeBounds(b.tf, s.Pos(), s.End()),
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

// typeChildren exposes a type's members at depth 2, per specification 2.1: the fields of a
// struct, and the method set of an interface.
//
// The interface case was added because "method sets" in the specification means what a Go
// programmer expects it to mean, and for an interface the method set is the entire content of
// the type. An interface rendering as a bare "type I interface" told a caller that a type
// whose whole purpose is its method list had nothing in it.
//
// One node is emitted per member NAME, not per ast.Field. "A, B int" is a single ast.Field
// holding two names, and taking only the first dropped B from the outline entirely — it was
// absent from the human form, the JSON form and the compact form alike, so a caller reading
// the field list before editing a struct was told it had fewer fields than it has. Parameters
// were already handled per name by fieldNodes, which is why the bug showed up in structs and
// not in signatures.
func (b *builder) typeChildren(expr ast.Expr) []domain.Node {
	switch t := expr.(type) {
	case *ast.StructType:
		return b.fieldNodes(t.Fields)
	case *ast.InterfaceType:
		return b.interfaceMethods(t.Methods)
	default:
		// A named map, slice, chan or func type has no members worth listing, and a union
		// constraint's elements are type terms rather than declarations.
		return nil
	}
}

// fieldNodes emits one node per member of an ast.FieldList: the fields of a struct, or the
// parameters of a function. Both are *ast.FieldLists and both are members a caller edits, so
// they render the same way.
//
// One node per NAME, not per ast.Field, because "A, B int" is one field holding two names.
// Each name carries its own span, so two members written on one line are independently
// addressable rather than sharing the bounds of the whole field -- the addressability problem
// a grouped const has, and the reason D27 gives each name its own position.
//
// A member with no name is rendered as its own type expression with no Detail: "mu" for an
// embedded struct field, "context.Context" for an unnamed parameter. Repeating it as Detail
// would render "sync.Mutex sync.Mutex", which is not valid Go.
//
// This is also why the function cannot loop over field.Names alone. Doing so drops every
// nameless member, and that is a defect by omission twice over: struct fields were fixed here
// (structFields handled the embedded case) while parameters were not, so
// "func F(context.Context, int)" rendered a function with no parameters at all -- a legal and
// common signature. The two cases were separate functions for that reason, and the separation
// is what let one be fixed and the other forgotten. Their preconditions are now identical, so
// they are one function again.
func (b *builder) fieldNodes(list *ast.FieldList) []domain.Node {
	if list == nil {
		return nil
	}

	out := make([]domain.Node, 0, len(list.List))
	for _, field := range list.List {
		detail := exprString(field.Type)

		// An embedded struct field or an unnamed parameter: the type expression is the whole
		// declaration, so it is the name and there is no separate type to show.
		if len(field.Names) == 0 {
			if detail == "" {
				continue
			}
			out = append(out, domain.Node{
				Name:   detail,
				Kind:   domain.KindField,
				Bounds: nodeBounds(b.tf, field.Pos(), field.End()),
			})
			continue
		}

		// Each name gets its own span, so the members stay independently addressable and two
		// members written on one line are not reported with the same bounds.
		for _, name := range field.Names {
			out = append(out, domain.Node{
				Name:   name.Name,
				Kind:   domain.KindField,
				Detail: detail,
				Bounds: nodeBounds(b.tf, name.Pos(), name.End()),
			})
		}
	}
	return out
}

// interfaceMethods emits one node per method spec in an interface's method list.
//
// A constraint interface -- `interface{ ~int | ~string }` -- has no methods and yields no
// nodes. Its elements arrive here as tilde unary expressions, which are type terms rather
// than methods, and rendering "~int | ~string" as a child of the interface would put a
// non-declaration in the outline. An embedded interface is a method of one, so it is listed
// by its type expression.
func (b *builder) interfaceMethods(methods *ast.FieldList) []domain.Node {
	if methods == nil || len(methods.List) == 0 {
		return nil
	}

	out := make([]domain.Node, 0, len(methods.List))
	for _, spec := range methods.List {
		fn, ok := spec.Type.(*ast.FuncType)
		if !ok {
			// Not a method: either an embedded interface, whose type expression is the
			// whole declaration, or a type-union element, which is not a declaration at all.
			if len(spec.Names) == 0 && !isUnionElement(spec.Type) {
				if name := exprString(spec.Type); name != "" {
					out = append(out, domain.Node{
						Name:   name,
						Kind:   domain.KindField,
						Bounds: nodeBounds(b.tf, spec.Pos(), spec.End()),
					})
				}
			}
			continue
		}

		// The Detail is the parameter and result list WITHOUT the "func" keyword. Printing
		// the *ast.FuncType directly yields "func(id int) (*User, error)", which is what a
		// function declaration looks like and is a syntax error inside an interface body --
		// so a caller pasting the outline back would not compile. An interface method is
		// spelled "Get(id int) (*User, error)".
		detail := funcSignature(fn)

		for _, name := range spec.Names {
			out = append(out, domain.Node{
				Name:   name.Name,
				Kind:   domain.KindField,
				Detail: detail,
				Bounds: nodeBounds(b.tf, name.Pos(), name.End()),
			})
		}
	}
	return out
}

// isUnionElement reports whether expr is a type-union rather than an embedded interface.
//
// `interface{ ~int | ~string }` and `interface{ int | string }` both parse to a single
// BinaryExpr with token.OR in the method list -- not one entry per element, which is the
// first thing to get wrong here. An embedded interface is an Ident or a SelectorExpr.
//
// Rendering the union as a member would put "~int | ~string" in the outline as though the
// interface declared it, and `interface{ A | B }` is a constraint, not a method set.
func isUnionElement(expr ast.Expr) bool {
	binary, ok := expr.(*ast.BinaryExpr)
	return ok && binary.Op == token.OR
}

// children exposes function parameters at depth 2. Parameters have no further
// structure worth showing, so this never recurses.
func (b *builder) children(fn *ast.FuncDecl) []domain.Node {
	if fn.Type == nil || fn.Type.Params == nil {
		return nil
	}
	return b.fieldNodes(fn.Type.Params)
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

// typeParamsText renders a type parameter list in the bracketed source form, "[T any]",
// or the empty string when the declaration is not generic.
//
// fieldListString produces the parenthesised form used for function parameters; a type
// parameter list is bracketed instead, so the parentheses are swapped rather than
// re-derived. Dropping the list would render "type Stack struct" for a "type Stack[T any]
// struct", and a consumer copying that head into an edit would produce code that does not
// compile.
func typeParamsText(tp *ast.FieldList) string {
	if tp == nil || len(tp.List) == 0 {
		return ""
	}
	params := fieldListString(tp)
	if params == "" {
		return ""
	}
	return "[" + strings.TrimSuffix(strings.TrimPrefix(params, "("), ")") + "]"
}

// receiverTypeName returns the base type name of a method receiver, ignoring pointers,
// generics and package qualifiers: *UserStore, store.User, UserStore[T] all yield
// "UserStore".
//
// This is for qualification only -- it builds the "UserStore.GetUser" lookup key. The
// declaration head uses receiverText, which keeps the full source spelling.
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

// importName returns the local name an import binds in the file scope.
//
// An explicit alias wins. Otherwise the name is the package's own name, which for an
// unversioned path is the last element: "context" for "context", "json" for "encoding/json".
//
// A major-version suffix is the exception. Go's convention is that "math/rand/v2" declares
// "package rand", so the local name is "rand" and not the path's last element "v2". This is not
// an obscure case: /v2 became common with Go 1.21, and getting it wrong both displays the wrong
// name in an outline and makes the unused-import check report a live import as dead.
func importName(imp *ast.ImportSpec) string {
	if imp.Name != nil {
		return imp.Name.Name
	}
	if imp.Path == nil {
		return ""
	}
	return packageNameFromPath(unquote(imp.Path.Value))
}

// packageNameFromPath derives a package's local name from its import path.
//
// It walks back from the end, stopping at the first element that is not a major-version suffix.
// Only /vN with N >= 2 counts: v1 has no suffix in a module path, so "example.com/v1" is a
// package genuinely named "v1".
func packageNameFromPath(path string) string {
	if path == "" {
		return ""
	}

	elements := strings.Split(path, "/")
	name := elements[len(elements)-1]

	// Walk back over version suffixes: /v3/v2 all reduce to the first non-version element.
	for len(elements) > 1 && isMajorVersionSuffix(name) {
		elements = elements[:len(elements)-1]
		name = elements[len(elements)-1]
	}
	return name
}

// isMajorVersionSuffix reports whether element is a major-version suffix such as "v2".
func isMajorVersionSuffix(element string) bool {
	if len(element) < 2 || element[0] != 'v' {
		return false
	}
	// The digits must be 2 or greater, since v1 is never a suffix.
	for i := 1; i < len(element); i++ {
		if element[i] < '0' || element[i] > '9' {
			return false
		}
	}
	return element != "v1"
}

func unquote(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	return s
}
