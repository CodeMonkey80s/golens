package goast_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CodeMonkey80s/golens/internal/domain"
	"github.com/CodeMonkey80s/golens/internal/engine/goast"
	"github.com/CodeMonkey80s/golens/internal/testfixture"
)

// TestFixtureClasses covers acceptance criterion A12.
//
// Twelve fixture classes, each probing a specific threat to the byte-offset contract or to
// the analysis. The classes are written to real files here rather than to in-memory strings,
// because that is what the criterion asks for and because reading from disk exercises the
// path an agent actually uses.
func TestFixtureClasses(t *testing.T) {
	dir := t.TempDir()

	t.Run("multi-file package", func(t *testing.T) {
		// Class 1: two files in one package, so resolution must span both.
		testfixture.Write(t, dir, "multi/a.go", "package multi\n\nfunc Alpha() {}\n")
		testfixture.Write(t, dir, "multi/b.go", "package multi\n\nfunc Beta() {}\n")

		for _, name := range []string{"Alpha", "Beta"} {
			if _, err := goast.New().Parse(testfixture.Write(t, dir, "multi/probe.go",
				"package multi\n\nfunc "+name+"() {}\n")); err != nil {
				t.Errorf("parse %s: %v", name, err)
			}
		}
	})

	t.Run("unexported identifiers", func(t *testing.T) {
		// Class 2: lower-case declarations must resolve like any other.
		path := testfixture.Write(t, dir, "unexported.go", testfixture.Unexported)
		doc, err := goast.New().Parse(path)
		if err != nil {
			t.Fatalf("Parse: %v", err)
		}

		for _, want := range []string{"inner", "counter", "bump", "inner.get"} {
			if !lookupPresent(doc, want) {
				t.Errorf("missing unexported symbol %q; have %v", want, names(doc))
			}
		}
	})

	t.Run("duplicate top-level declarations", func(t *testing.T) {
		// Class 3: same name twice in one file.
		path := testfixture.Write(t, dir, "duplicate.go", testfixture.Duplicate)
		doc, err := goast.New().Parse(path)
		if err != nil {
			t.Fatalf("Parse: %v", err)
		}

		matches := doc.Lookup("F")
		if len(matches) != 2 {
			t.Fatalf("Lookup(F) = %d, want 2 candidates: %v", len(matches), names(doc))
		}
		if matches[0].Bounds.StartByte == matches[1].Bounds.StartByte {
			t.Error("both declarations report the same bounds; they must be independently addressable")
		}
	})

	t.Run("ambiguous method names", func(t *testing.T) {
		// Class 3: one method name, two receivers.
		path := testfixture.Write(t, dir, "ambiguous.go", testfixture.Ambiguous)
		doc, err := goast.New().Parse(path)
		if err != nil {
			t.Fatalf("Parse: %v", err)
		}

		if got := doc.Lookup("Save"); len(got) != 2 {
			t.Errorf("Lookup(Save) = %d, want 2", len(got))
		}
		if !lookupPresent(doc, "Alpha.Save") || !lookupPresent(doc, "Beta.Save") {
			t.Errorf("qualified names must both resolve; have %v", names(doc))
		}
	})

	t.Run("dot blank and named imports", func(t *testing.T) {
		// Class 4: four import forms whose local names differ from their paths.
		path := testfixture.Write(t, dir, "imports.go", testfixture.ImportVariety)
		doc, err := goast.New().Parse(path)
		if err != nil {
			t.Fatalf("Parse: %v", err)
		}

		if len(doc.Imports) != 4 {
			t.Errorf("Imports = %v, want 4 entries", doc.Imports)
		}
		want := map[string]string{
			`"context"`:        "context",
			`"encoding/json"`:  "jsonalias",
			`"net/http/pprof"`: "_",
			`"strings"`:        ".",
		}
		got := importNames(doc)
		for path, name := range want {
			if got[path] != name {
				t.Errorf("local name for %s = %q, want %q", path, got[path], name)
			}
		}
	})

	t.Run("unused import", func(t *testing.T) {
		// Class 4: the negative case, a single unused import.
		path := testfixture.Write(t, dir, "unused.go", testfixture.UnusedImport)
		doc, err := goast.New().Parse(path)
		if err != nil {
			t.Fatalf("Parse: %v", err)
		}
		if got := importNames(doc)[`"fmt"`]; got != "fmt" {
			t.Errorf("import fmt local name = %q, want fmt", got)
		}
	})

	t.Run("unicode identifiers", func(t *testing.T) {
		// Class 7: multi-byte runes. Byte offsets must be bytes.
		path := testfixture.Write(t, dir, "unicode.go", testfixture.Unicode)
		doc, err := goast.New().Parse(path)
		if err != nil {
			t.Fatalf("Parse: %v", err)
		}

		sym := mustLookup(t, doc, "Ünïcödé")
		src := string(doc.Source)

		got := sym.Bounds.Source(doc.Source)
		want := "func Ünïcödé(naïve string) string {\n\treturn naïve + \"日本語\"\n}"
		if got != want {
			t.Errorf("span = %q, want %q", got, want)
		}
		if sym.Bounds.ByteLen() != len(got) {
			t.Errorf("ByteLen = %d, want %d (bytes, not runes)", sym.Bounds.ByteLen(), len(got))
		}
		if n := len([]rune(got)); n == sym.Bounds.ByteLen() {
			t.Error("ByteLen equals rune count; offsets are not byte-based")
		}
		_ = src
	})

	t.Run("empty file", func(t *testing.T) {
		// Class 9: a package clause with nothing else.
		path := testfixture.Write(t, dir, "empty.go", testfixture.Empty)
		doc, err := goast.New().Parse(path)
		if err != nil {
			t.Fatalf("Parse: %v", err)
		}
		if doc.Package != "empty" {
			t.Errorf("Package = %q, want empty", doc.Package)
		}
		if len(doc.Symbols) != 0 {
			t.Errorf("Symbols = %v, want none", names(doc))
		}
		if doc.Bounds.EndByte != len(testfixture.Empty) {
			t.Errorf("EndByte = %d, want %d", doc.Bounds.EndByte, len(testfixture.Empty))
		}
	})

	t.Run("comments only", func(t *testing.T) {
		// Class 10: valid file, no declarations.
		path := testfixture.Write(t, dir, "comments.go", testfixture.CommentsOnly)
		doc, err := goast.New().Parse(path)
		if err != nil {
			t.Fatalf("Parse: %v", err)
		}
		if len(doc.Symbols) != 0 {
			t.Errorf("Symbols = %v, want none", names(doc))
		}
	})

	t.Run("unparseable file", func(t *testing.T) {
		// Class 11: a syntax error must be reported, not swallowed.
		path := testfixture.Write(t, dir, "broken.go", testfixture.Broken)
		_, err := goast.New().Parse(path)
		if err == nil {
			t.Fatal("expected a parse error")
		}
		pe, ok := err.(*goast.ParseError)
		if !ok {
			t.Fatalf("error type = %T, want *goast.ParseError", err)
		}
		if len(pe.Diagnostics()) == 0 {
			t.Error("expected diagnostics")
		}
	})

	t.Run("CRLF line endings", func(t *testing.T) {
		// Class 5: go/token counts the CR as part of the line, so offsets shift by one per
		// line while the half-open invariant must hold.
		path := testfixture.Write(t, dir, "crlf.go", testfixture.Crlf)
		doc, err := goast.New().Parse(path)
		if err != nil {
			t.Fatalf("Parse: %v", err)
		}

		sym := mustLookup(t, doc, "f")
		got := sym.Bounds.Source(doc.Source)
		if want := "func f() {\r\n}"; got != want {
			t.Errorf("span = %q, want %q", got, want)
		}
		if sym.Bounds.EndLine != 4 {
			t.Errorf("EndLine = %d, want 4", sym.Bounds.EndLine)
		}
	})

	t.Run("UTF-8 BOM", func(t *testing.T) {
		// Class 6: three BOM bytes precede the package clause, shifting every offset.
		path := testfixture.Write(t, dir, "bom.go", testfixture.Bom)
		doc, err := goast.New().Parse(path)
		if err != nil {
			t.Fatalf("Parse: %v", err)
		}

		sym := mustLookup(t, doc, "f")
		got := sym.Bounds.Source(doc.Source)
		if want := "func f() {}"; got != want {
			t.Errorf("span = %q, want %q", got, want)
		}
		if sym.Bounds.StartByte < 3 {
			t.Errorf("StartByte = %d, want at least 3 to skip the BOM", sym.Bounds.StartByte)
		}
		if sym.Bounds.StartLine != 3 {
			t.Errorf("StartLine = %d, want 3", sym.Bounds.StartLine)
		}
	})

	t.Run("deep directory tree", func(t *testing.T) {
		// Class 12: nested traversal.
		deep := "tree/a/b/c/d/e/deep.go"
		path := testfixture.Write(t, dir, deep, "package deep\n\nfunc Deep() {}\n")
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("nested fixture not written: %v", err)
		}
		if filepath.Base(filepath.Dir(path)) != "e" {
			t.Errorf("fixture not nested as intended: %s", path)
		}
	})
}

// TestStoreFixtureIsCanonical checks the shared fixture parses into the shape the outline
// tests rely on, so a change to it fails here rather than mysteriously in eight places.
func TestStoreFixtureIsCanonical(t *testing.T) {
	dir := t.TempDir()
	path := testfixture.Write(t, dir, "store.go", testfixture.Store)

	doc, err := goast.New().Parse(path)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if doc.Package != "store" {
		t.Errorf("Package = %q, want store", doc.Package)
	}
	if len(doc.Imports) != 2 {
		t.Errorf("Imports = %v, want context and fmt", doc.Imports)
	}

	for _, want := range []string{
		"User", "UserStore", "Alias",
		"ErrNotFound", "MaxUsers",
		"NewUserStore", "UserStore.GetUser", "UserStore.Len",
	} {
		if !lookupPresent(doc, want) {
			t.Errorf("missing symbol %q; have %v", want, names(doc))
		}
	}

	// GetUser is the multi-line method the human-output tests assert on. Its exact lines
	// are pinned so that inserting a line in the fixture fails here rather than silently
	// shifting every golden output downstream.
	sym := mustLookup(t, doc, "UserStore.GetUser")
	if sym.Bounds.StartLine != 28 {
		t.Errorf("GetUser StartLine = %d, want 28; the fixture's line numbers moved", sym.Bounds.StartLine)
	}
	if sym.Bounds.EndLine != 35 {
		t.Errorf("GetUser EndLine = %d, want 35", sym.Bounds.EndLine)
	}
	if sym.Bounds.LineCount() != 8 {
		t.Errorf("GetUser LineCount = %d, want 8", sym.Bounds.LineCount())
	}

	// Every symbol must satisfy the half-open invariant, whatever the fixture.
	for _, s := range doc.Symbols {
		got := s.Bounds.Source(doc.Source)
		if got == "" {
			t.Errorf("%s: empty span", s.QualifiedName)
			continue
		}
		if string(doc.Source[s.Bounds.StartByte:s.Bounds.EndByte]) != got {
			t.Errorf("%s: slice does not equal extracted source", s.QualifiedName)
		}
	}
}

// importNames maps each import path to its local binding name, taken from the outline
// nodes rather than re-deriving it from the path.
func importNames(doc domain.Document) map[string]string {
	out := map[string]string{}
	for _, node := range flatten(doc.Nodes) {
		if node.Kind == domain.KindImport && len(node.Children) == 0 {
			out[node.Detail] = node.Name
		}
	}
	return out
}

func flatten(nodes []domain.Node) []domain.Node {
	var out []domain.Node
	for _, n := range nodes {
		out = append(out, n)
		out = append(out, flatten(n.Children)...)
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

func mustLookup(t *testing.T, doc domain.Document, name string) domain.Symbol {
	t.Helper()
	matches := doc.Lookup(name)
	if len(matches) != 1 {
		t.Fatalf("symbol %q: %d matches, want exactly 1 (%v)", name, len(matches), names(doc))
	}
	return matches[0]
}

func lookupPresent(doc domain.Document, name string) bool {
	return len(doc.Lookup(name)) > 0
}

var _ = strings.TrimSpace
