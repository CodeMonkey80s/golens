package goast

import (
	"strings"
	"testing"

	"github.com/CodeMonkey80s/golens/internal/domain"
)

// handChecked is the source used to prove the byte-offset contract.
//
// It is written as a literal rather than a fixture file so the expected offsets can be
// verified by counting characters in this test, independently of the parser.
const handChecked = `package store

import "context"

type User struct {
	Id int
}

type UserStore struct {
	mu    sync.Mutex
	cache map[int]*User
}

func NewUserStore() *UserStore {
	return &UserStore{}
}

func (s *UserStore) GetUser(ctx context.Context, id int) (*User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if u, ok := s.cache[id]; ok {
		return u, nil
	}
	return nil, ErrNotFound
}
`

// TestBoundsAreHalfOpen is the S1 hard gate: it proves the central invariant of the whole
// tool, that file[start_byte:end_byte] is exactly the extracted source, byte for byte.
//
// If this test ever fails, agent edits would corrupt files, so it asserts both directions
// of the contract: the slice equals the source, and the offsets address the real bytes.
func TestBoundsAreHalfOpen(t *testing.T) {
	engine := New()
	doc, err := engine.ParseSource("user.go", []byte(handChecked))
	if err != nil {
		t.Fatalf("ParseSource: %v", err)
	}

	for _, sym := range doc.Symbols {
		sym := sym
		t.Run(sym.QualifiedName, func(t *testing.T) {
			src := sym.Bounds.Source([]byte(handChecked))
			if src == "" {
				t.Fatalf("Bounds.Source returned empty string")
			}

			// The slice must equal the extracted text.
			if direct := handChecked[sym.Bounds.StartByte:sym.Bounds.EndByte]; direct != src {
				t.Errorf("slice mismatch:\n slice: %q\n  got: %q", direct, src)
			}

			// The text must start at start_byte and stop before end_byte.
			if !strings.HasPrefix(handChecked[sym.Bounds.StartByte:], src) {
				t.Errorf("source at start_byte %d does not begin with extracted text", sym.Bounds.StartByte)
			}
			if end := sym.Bounds.StartByte + len(src); end != sym.Bounds.EndByte {
				t.Errorf("EndByte = %d, want %d (start + len)", sym.Bounds.EndByte, end)
			}
		})
	}
}

// TestExtractedSourceMatchesDeclaration pins the exact declaration text for one symbol,
// so a regression that shifts offsets by a character is caught even if the slice still
// round-trips.
func TestExtractedSourceMatchesDeclaration(t *testing.T) {
	engine := New()
	doc, err := engine.ParseSource("user.go", []byte(handChecked))
	if err != nil {
		t.Fatalf("ParseSource: %v", err)
	}

	sym := mustLookup(t, doc, "UserStore.GetUser")

	want := `func (s *UserStore) GetUser(ctx context.Context, id int) (*User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if u, ok := s.cache[id]; ok {
		return u, nil
	}
	return nil, ErrNotFound
}`

	if got := sym.Bounds.Source([]byte(handChecked)); got != want {
		t.Errorf("extracted source mismatch:\n got:\n%s\nwant:\n%s", got, want)
	}
}

// TestTrailingNewlineExcluded verifies the span stops at the closing brace so that an
// agent overwriting the range leaves the file's line structure intact.
func TestTrailingNewlineExcluded(t *testing.T) {
	engine := New()
	doc, err := engine.ParseSource("user.go", []byte(handChecked))
	if err != nil {
		t.Fatalf("ParseSource: %v", err)
	}

	sym := mustLookup(t, doc, "UserStore.GetUser")

	src := sym.Bounds.Source([]byte(handChecked))
	if strings.HasSuffix(src, "\n") {
		t.Errorf("span includes trailing newline: %q", src)
	}
	if !strings.HasSuffix(src, "}") {
		t.Errorf("span should end at closing brace, got %q", src[len(src)-20:])
	}
}

// TestEndLineDerivedFromEndByteMinusOne covers the rule that a declaration ending at
// column 1 of the following line reports the line it actually ends on, not the next one.
func TestEndLineDerivedFromEndByteMinusOne(t *testing.T) {
	// The closing brace sits on the last content line; the byte after it is the newline.
	src := "package p\n\nfunc f() {\n}\n"

	engine := New()
	doc, err := engine.ParseSource("p.go", []byte(src))
	if err != nil {
		t.Fatalf("ParseSource: %v", err)
	}

	sym := mustLookup(t, doc, "f")

	if sym.Bounds.EndLine != 4 {
		t.Errorf("EndLine = %d, want 4", sym.Bounds.EndLine)
	}
	if sym.Bounds.LineCount() != 2 {
		t.Errorf("LineCount = %d, want 2", sym.Bounds.LineCount())
	}
	if got := src[sym.Bounds.StartByte:sym.Bounds.EndByte]; got != "func f() {\n}" {
		t.Errorf("span = %q, want %q", got, "func f() {\n}")
	}
}

// TestSymbolDiscovery covers the symbol kinds the specification requires: exported and
// unexported funcs, methods, types, vars and consts.
func TestSymbolDiscovery(t *testing.T) {
	src := `package p

type Exported struct{}
type unexported struct{}

var Var1 int
var var2 = 2

const Const1 = 1
const const2 = 2

func Fn() {}
func unexportedFn() {}

type Recv struct{}

func (r Recv) Method()  {}
func (r *Recv) PtrMethod() {}

type Alias = Exported
`

	engine := New()
	doc, err := engine.ParseSource("p.go", []byte(src))
	if err != nil {
		t.Fatalf("ParseSource: %v", err)
	}

	want := []string{
		"Exported", "unexported", "Alias",
		"Var1", "var2", "Const1", "const2",
		"Fn", "unexportedFn", "Recv.Method", "Recv.PtrMethod",
	}
	got := names(doc)

	for _, name := range want {
		if !contains(got, name) {
			t.Errorf("missing symbol %q; have %v", name, got)
		}
	}
}

// TestMethodQualification covers the receiver forms that must all yield the bare type
// name: value, pointer, generic and package-qualified receivers.
func TestMethodQualification(t *testing.T) {
	src := `package p

import "sync"

type T struct{}
type G[T any] struct{}

func (t T) Value()       {}
func (t *T) Pointer()    {}
func (g G[T]) Generic()  {}
func (s sync.Locker) Ext() {}
`

	engine := New()
	doc, err := engine.ParseSource("p.go", []byte(src))
	if err != nil {
		t.Fatalf("ParseSource: %v", err)
	}

	for _, want := range []string{"T.Value", "T.Pointer", "G.Generic", "Locker.Ext"} {
		if !lookupPresent(doc, want) {
			t.Errorf("missing qualified symbol %q; have %v", want, names(doc))
		}
	}
}

// TestLookupAmbiguity covers the exit-2 precondition: a bare method name shared by two
// receivers is ambiguous and must return both candidates.
func TestLookupAmbiguity(t *testing.T) {
	src := `package p

type A struct{}
type B struct{}

func (A) Save() {}
func (B) Save() {}
`

	engine := New()
	doc, err := engine.ParseSource("p.go", []byte(src))
	if err != nil {
		t.Fatalf("ParseSource: %v", err)
	}

	matches := doc.Lookup("Save")
	if len(matches) != 2 {
		t.Fatalf("Lookup(\"Save\") returned %d matches, want 2: %v", len(matches), names(doc))
	}
	if matches[0].QualifiedName == matches[1].QualifiedName {
		t.Errorf("expected distinct qualified names, both %q", matches[0].QualifiedName)
	}
}

// TestFileBounds covers the whole-file span, including an empty file.
func TestFileBounds(t *testing.T) {
	engine := New()

	doc, err := engine.ParseSource("p.go", []byte("package p\n"))
	if err != nil {
		t.Fatalf("ParseSource: %v", err)
	}
	if doc.Bounds.StartByte != 0 || doc.Bounds.EndByte != len("package p\n") {
		t.Errorf("file bounds = %v, want [0,%d)", doc.Bounds, len("package p\n"))
	}
	if doc.Bounds.StartLine != 1 {
		t.Errorf("StartLine = %d, want 1", doc.Bounds.StartLine)
	}
	if doc.Package != "p" {
		t.Errorf("Package = %q, want %q", doc.Package, "p")
	}
}

// TestUnicodeOffsetsAreByteBased verifies offsets are bytes, not runes. A source with
// multi-byte identifiers must still satisfy the half-open invariant.
func TestUnicodeOffsetsAreByteBased(t *testing.T) {
	src := "package p\n\nfunc Ünïcödé() {}\n"

	engine := New()
	doc, err := engine.ParseSource("p.go", []byte(src))
	if err != nil {
		t.Fatalf("ParseSource: %v", err)
	}

	sym := mustLookup(t, doc, "Ünïcödé")

	got := sym.Bounds.Source([]byte(src))
	if got != "func Ünïcödé() {}" {
		t.Errorf("span = %q, want %q", got, "func Ünïcödé() {}")
	}
	// The function name is 9 runes but 13 bytes, so a rune-based implementation would
	// report EndByte-StartByte == len("func () {}") + 9.
	if n := sym.Bounds.ByteLen(); n != len(got) {
		t.Errorf("ByteLen = %d, want %d", n, len(got))
	}
}

// TestCrlfSource proves line counting and byte offsets stay consistent when lines end
// with CRLF. go/token treats the CR as part of the line, so offsets shift by one per
// line while the half-open slice invariant must still hold.
func TestCrlfSource(t *testing.T) {
	src := "package p\r\n\r\nfunc f() {\r\n}\r\n"

	engine := New()
	doc, err := engine.ParseSource("p.go", []byte(src))
	if err != nil {
		t.Fatalf("ParseSource: %v", err)
	}

	sym := mustLookup(t, doc, "f")
	if got := src[sym.Bounds.StartByte:sym.Bounds.EndByte]; got != "func f() {\r\n}" {
		t.Errorf("span = %q, want %q", got, "func f() {\r\n}")
	}
}

// TestParseErrorDiagnostics covers the exit-4 path and the file:line:col: format.
func TestParseErrorDiagnostics(t *testing.T) {
	engine := New()
	_, err := engine.ParseSource("broken.go", []byte("package p\n\nfunc f( {\n"))
	if err == nil {
		t.Fatal("expected parse error")
	}

	pe, ok := err.(*ParseError)
	if !ok {
		t.Fatalf("error type = %T, want *ParseError", err)
	}
	if len(pe.Errors) == 0 {
		t.Fatal("expected at least one scanner error")
	}

	diags := pe.Diagnostics()
	if len(diags) == 0 {
		t.Fatal("expected diagnostics")
	}
	if !strings.Contains(diags[0], "broken.go:") {
		t.Errorf("diagnostic missing filename: %q", diags[0])
	}
}

// TestImportExtraction covers dot, blank and named imports, whose local binding differs
// from the last path element.
func TestImportExtraction(t *testing.T) {
	src := `package p

import (
	"context"
	"encoding/json"
	jsonalias "encoding/json"
	_ "net/http/pprof"
	. "strings"
)
`

	engine := New()
	doc, err := engine.ParseSource("p.go", []byte(src))
	if err != nil {
		t.Fatalf("ParseSource: %v", err)
	}

	// Five import specs, four distinct paths: jsonalias "encoding/json" repeats a path
	// under a different local name, so both must appear.
	want := []string{
		`"context"`,
		`"encoding/json"`,
		`"encoding/json"`,
		`"net/http/pprof"`,
		`"strings"`,
	}
	if len(doc.Imports) != len(want) {
		t.Fatalf("Imports = %v, want %d entries %v", doc.Imports, len(want), want)
	}
	for i, imp := range doc.Imports {
		if imp != want[i] {
			t.Errorf("Imports[%d] = %q, want %q", i, imp, want[i])
		}
	}

	// Each outline import node carries the local binding name, not the path. Blank and
	// dot imports bind to "_" and "."; a named import binds to its explicit alias, so the
	// two "encoding/json" specs report different names.
	imports := map[string][]string{}
	for _, node := range doc.Nodes {
		if node.Kind != domain.KindImport {
			continue
		}
		for _, child := range node.Children {
			imports[child.Detail] = append(imports[child.Detail], child.Name)
		}
	}

	if names := imports[`"encoding/json"`]; len(names) != 2 || names[0] != "json" || names[1] != "jsonalias" {
		t.Errorf("encoding/json local names = %v, want [json jsonalias]", names)
	}
	if name := firstName(imports[`"context"`]); name != "context" {
		t.Errorf("context local name = %q, want context", name)
	}
	if name := firstName(imports[`"net/http/pprof"`]); name != "_" {
		t.Errorf("blank import local name = %q, want _", name)
	}
	if name := firstName(imports[`"strings"`]); name != "." {
		t.Errorf("dot import local name = %q, want .", name)
	}
}

func firstName(names []string) string {
	if len(names) == 0 {
		return ""
	}
	return names[0]
}

// TestNestedNodesAtDepthTwo covers the struct field expansion required by --depth 2.
func TestNestedNodesAtDepthTwo(t *testing.T) {
	src := `package p

type Config struct {
	Name    string
	Timeout int
}

func F(a int, b string) {}
`

	engine := New()
	doc, err := engine.ParseSource("p.go", []byte(src))
	if err != nil {
		t.Fatalf("ParseSource: %v", err)
	}

	fields := findChildren(doc.Nodes, "Config")
	if len(fields) != 2 {
		t.Fatalf("Config has %d children, want 2: %v", len(fields), fieldNames(fields))
	}
	if fields[0].Name != "Name" || fields[1].Name != "Timeout" {
		t.Errorf("fields = %v, want [Name Timeout]", fieldNames(fields))
	}

	params := findChildren(doc.Nodes, "F")
	if len(params) != 2 {
		t.Fatalf("F has %d children, want 2: %v", len(params), fieldNames(params))
	}
}

// TestDeclarationHeadsMatchSource pins the rendered declaration head for the three
// constructs where it previously diverged from the source: generic type parameters,
// receiver spelling, and embedded struct fields.
//
// Each of these produced text that does not compile when copied into an edit --
// "type Stack struct" for "type Stack[T any] struct", "func (s Locker) Ext()" for
// "func (s sync.Locker) Ext()", and "sync.Mutex sync.Mutex" for an embedded field.
func TestDeclarationHeadsMatchSource(t *testing.T) {
	src := `package p

import "sync"

type Stack[T any] struct {
	sync.Mutex
	items []T
}

func (s *Stack[T]) Push(v T) {}
func (s Stack[T]) Len() int   { return len(s.items) }
func (l sync.Locker) Lock()   {}
func (s Stack[T]) Pop() (T, bool) { var z T; return z, false }
`

	engine := New()
	doc, err := engine.ParseSource("p.go", []byte(src))
	if err != nil {
		t.Fatalf("ParseSource: %v", err)
	}

	wantHeads := map[string]string{
		"Stack.Push":  "func (s *Stack[T]) Push(v T)",
		"Stack.Len":   "func (s Stack[T]) Len() int",
		"Locker.Lock": "func (l sync.Locker) Lock()",
		"Stack.Pop":   "func (s Stack[T]) Pop() (T, bool)",
	}
	for qualified, want := range wantHeads {
		sym, ok := doc.Symbol(qualified)
		if !ok {
			t.Errorf("symbol %q not found; have %v", qualified, names(doc))
			continue
		}
		if got := sym.Declaration(); got != want {
			t.Errorf("%s declaration head:\n got: %s\nwant: %s", qualified, got, want)
		}
	}

	typeSym, ok := doc.Symbol("Stack")
	if !ok {
		t.Fatalf("symbol \"Stack\" not found")
	}
	if got, want := typeSym.Declaration(), "type Stack[T any] struct"; got != want {
		t.Errorf("Stack declaration head:\n got: %s\nwant: %s", got, want)
	}

	// The embedded field is the whole declaration: its name, with no type repeated.
	fields := findChildren(doc.Nodes, "Stack")
	var embedded *domain.Node
	for i, f := range fields {
		if f.Kind == domain.KindField && f.Name == "sync.Mutex" {
			embedded = &fields[i]
			break
		}
	}
	if embedded == nil {
		t.Fatalf("no struct field found under Stack")
	}
	if embedded.Name != "sync.Mutex" || embedded.Detail != "" {
		t.Errorf("embedded field: Name = %q, Detail = %q; want Name \"sync.Mutex\", Detail \"\"",
			embedded.Name, embedded.Detail)
	}
}

func findChildren(nodes []domain.Node, name string) []domain.Node {
	for _, node := range nodes {
		if node.Name == name {
			return node.Children
		}
		for _, child := range node.Children {
			if child.Name == name {
				return child.Children
			}
		}
	}
	return nil
}

func fieldNames(nodes []domain.Node) []string {
	out := make([]string, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, n.Name)
	}
	return out
}

func names(doc domain.Document) []string {
	out := make([]string, 0, len(doc.Symbols))
	for _, s := range doc.Symbols {
		out = append(out, s.QualifiedName)
	}
	return out
}

func contains(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}
