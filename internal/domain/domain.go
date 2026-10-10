// Package domain defines the language-neutral types used across golens.
//
// This package must not import anything outside the standard library, and must not
// import any other golens package. It is the innermost layer: CLI wiring, engines and
// formatters all depend on it, never the reverse.
package domain

import (
	"fmt"
	"strconv"
)

// Kind classifies a node in a structural outline.
//
// Kinds are deliberately coarse. They are meant to be stable across languages so that a
// consumer can branch on "is this a function or a type" without knowing the engine that
// produced it.
type Kind string

const (
	KindPackage Kind = "package"
	KindImport  Kind = "import"
	KindType    Kind = "type"
	KindFunc    Kind = "func"
	KindMethod  Kind = "method"
	KindVar     Kind = "var"
	KindConst   Kind = "const"
	KindField   Kind = "field"
)

// Bounds is a half-open span within a source file.
//
// Line numbers are 1-based and inclusive at both ends. Byte offsets are 0-based and
// half-open: Src[StartByte:EndByte] is exactly the source text of the span.
//
// The asymmetry is deliberate. Lines mirror compiler diagnostics, where a declaration on
// line 17 through 25 is written "17-25". Bytes mirror Go slicing, so a caller can do
//
//	file[bounds.StartByte:bounds.EndByte]
//
// and get the exact text. EndByte is one past the last byte.
type Bounds struct {
	StartLine int
	EndLine   int
	StartByte int
	EndByte   int
}

// Valid reports whether the bounds describe a usable span: start before end, and
// non-negative throughout.
func (b Bounds) Valid() bool {
	return b.StartByte >= 0 && b.StartLine >= 1 && b.EndLine >= b.StartLine && b.EndByte > b.StartByte
}

// LineCount returns the number of lines the span covers, inclusive at both ends.
// A single-line span reports 1.
func (b Bounds) LineCount() int {
	if b.EndLine < b.StartLine {
		return 0
	}
	return b.EndLine - b.StartLine + 1
}

// ByteLen returns the length of the span in bytes, which is EndByte minus StartByte.
func (b Bounds) ByteLen() int {
	if b.EndByte <= b.StartByte {
		return 0
	}
	return b.EndByte - b.StartByte
}

// LineRange renders the line span for human output. A single-line span renders as
// "line 12"; anything wider renders as "lines 12-25". This matches the two forms used in
// the specification's outline examples.
func (b Bounds) LineRange() string {
	if b.EndLine == b.StartLine {
		return "line " + strconv.Itoa(b.StartLine)
	}
	return "lines " + strconv.Itoa(b.StartLine) + "-" + strconv.Itoa(b.EndLine)
}

func (b Bounds) String() string {
	return fmt.Sprintf("lines %d-%d bytes %d-%d", b.StartLine, b.EndLine, b.StartByte, b.EndByte)
}

// Source extracts the text the bounds describe from src.
//
// This is the single place byte offsets are turned back into text. The half-open
// convention means the terminating newline after a declaration falls outside the span,
// so an agent that overwrites [StartByte:EndByte) leaves the file's line structure
// intact.
func (b Bounds) Source(src []byte) string {
	if b.StartByte < 0 || b.EndByte > len(src) || b.StartByte >= b.EndByte {
		return ""
	}
	return string(src[b.StartByte:b.EndByte])
}

// Node is one entry in a structural outline.
//
// Nodes form a tree via Children. Depth-limited walks use nil Children to mean
// "suppressed", which is distinct from an empty non-nil slice meaning "no children".
type Node struct {
	Name   string
	Kind   Kind
	Detail string
	// Receiver is the method receiver exactly as the source spells it, without the
	// surrounding parentheses: "s *UserStore", "g G[T]". An unnamed receiver is just its
	// type. It is empty for anything that is not a method.
	Receiver string
	// TypeParams is the bracketed type parameter list of a generic declaration, "[T any]",
	// or empty when the declaration has none. It is kept separate from Name so that the
	// symbol name stays the bare identifier for lookup.
	TypeParams string
	// IsAlias marks a type alias, "type A = B", as opposed to a defined type, "type A B".
	// The "=" is the only difference between the two in source, and it changes the meaning:
	// an alias is interchangeable with B, a defined type is not. Rendering an alias as a
	// defined type produces code that compiles and behaves wrongly, so Declaration must
	// reproduce the "=" rather than infer the form from Detail.
	IsAlias  bool
	Bounds   Bounds
	Children []Node

	// IsGroup marks a synthetic container the engine introduced for presentation, such as
	// the "IMPORTS" or "TYPES & STRUCTS" headings. A group has no source text of its own; its
	// bounds are the span of its members. Formatters must not print a group as though it were
	// a declaration, or a declaration that itself has children, such as a struct with fields,
	// would be mistaken for one.
	IsGroup bool
}

// Declaration renders the node as a Go declaration head, which is what the human outline
// prints after the bullet.
//
// For a method the receiver is restored verbatim, so a node built from Kind Method with
// Name "GetUser" and Receiver "s *UserStore" renders as "func (s *UserStore) GetUser(...)".
// Receiver carries the source spelling, type parameters included, so a generic receiver
// renders as "func (g G[T]) Generic()" rather than dropping the "[T]".
func (n Node) Declaration() string {
	keyword := n.Keyword()

	switch n.Kind {
	case KindFunc, KindMethod:
		// Detail is the parenthesised parameter and result text, so it attaches directly
		// to the name: "func NewUserStore() *UserStore".
		if n.Kind == KindMethod && n.Receiver != "" {
			return keyword + " (" + n.Receiver + ") " + n.Name + n.Detail
		}
		return keyword + " " + n.Name + n.Detail

	default:
		// TypeParams attaches to the name with no separating space, as in the source:
		// "type Stack[T any] struct". Detail is the underlying type or literal, separated
		// by a space.
		head := keyword + " " + n.Name + n.TypeParams
		// An alias keeps its "=", spaced as the source spells it: "type Alias = X".
		// Without it the head describes a defined type, which is a different program:
		// the name stops being interchangeable with X.
		if n.IsAlias {
			head += " ="
		}
		if n.Detail != "" {
			return head + " " + n.Detail
		}
		return head
	}
}

// Keyword returns the source keyword for the node's kind.
//
// A method renders with the "func" keyword, matching Go source and the specification's
// outline example, even though Kind distinguishes methods for query purposes. Exported
// because the CLI walks the outline tree and needs the same rendering the domain uses.
func (n Node) Keyword() string {
	if n.Kind == KindMethod {
		return "func"
	}
	return string(n.Kind)
}

// Symbol is a package-level declaration, addressable by name.
//
// QualifiedName is the lookup key: "GetUser" for a package-level function,
// "UserStore.GetUser" for a method. Name is always the bare identifier.
type Symbol struct {
	Name          string
	QualifiedName string
	Kind          Kind
	Bounds        Bounds
	File          string
	Detail        string
	Receiver      string
	TypeParams    string
	// IsAlias marks a type alias rather than a defined type. See Node.IsAlias.
	IsAlias bool
}

// Declaration renders the symbol as a Go declaration head, using the same form as
// Node.Declaration.
func (s Symbol) Declaration() string {
	return Node{Name: s.Name, Kind: s.Kind, Detail: s.Detail, Receiver: s.Receiver, TypeParams: s.TypeParams, IsAlias: s.IsAlias}.Declaration()
}

// Document is the result of parsing one source file.
type Document struct {
	Path    string
	Package string
	Bounds  Bounds
	Nodes   []Node
	Symbols []Symbol
	Imports []string
	Source  []byte
	// BuildConstraint is the file's //go:build expression, normalised from either source
	// syntax, or empty when the file carries no constraint and is always built.
	//
	// It is recorded because golens parses every file in a tree whatever its constraints,
	// which is right for a structural query and misleading for an edit. A caller changing a
	// file needs to know whether the current platform compiles it.
	BuildConstraint string
	Comments        bool
}

// Symbol looks up a package-level declaration by bare or qualified name.
//
// Matching is case-sensitive and exact. When a bare method name is given and several
// methods share it, the caller must disambiguate using the qualified form; Symbol itself
// reports only the first match in document order. Use Lookup to detect that case.
func (d *Document) Symbol(name string) (Symbol, bool) {
	for _, s := range d.Symbols {
		if s.QualifiedName == name || s.Name == name {
			return s, true
		}
	}
	return Symbol{}, false
}

// Lookup resolves a symbol name and reports every match.
//
// An empty result means the symbol does not exist. More than one result means the name
// is ambiguous and the caller must exit with code 2 and show the candidates rather than
// guessing. Note that a bare method name legitimately matches one symbol per receiver
// type, so ambiguity is expected there.
func (d *Document) Lookup(name string) []Symbol {
	var out []Symbol
	for _, s := range d.Symbols {
		if s.QualifiedName == name || s.Name == name {
			out = append(out, s)
		}
	}
	return out
}

// Node is the root node of the document's outline, carrying the file and package name.
func (d *Document) RootNode() Node {
	return Node{
		Name:   d.Package,
		Kind:   KindPackage,
		Bounds: d.Bounds,
	}
}

// Engine parses a source file into a Document.
//
// The interface is deliberately narrow and tree-shaped so that a Tree-Sitter backed
// implementation can satisfy it in v0.2 without the generic layer changing. Engines own
// all language-specific concerns: which declarations count as symbols, how byte offsets
// are computed, and how names are qualified.
type Engine interface {
	Parse(path string) (Document, error)
}

// ErrorExit classifies a failure by the exit code the CLI should return.
//
// Exit codes are part of the public contract: 0 success, 1 usage error, 2 symbol or file
// not found, 3 check found diagnostics, 4 parse or I/O failure.
type ErrorExit int

const (
	ExitOk ErrorExit = iota
	ExitUsage
	ExitNotFound
	ExitDiagnostics
	ExitParse
)

func (e ErrorExit) Code() int { return int(e) }
