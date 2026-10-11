package format_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CodeMonkey80s/golens/internal/domain"
	"github.com/CodeMonkey80s/golens/internal/engine/goast"
	"github.com/CodeMonkey80s/golens/internal/format"
)

// TestRenderersAgreeOnTheWholeCorpus is the enumerable form of the cross-renderer invariant.
//
// The three tests it replaces each named the constructs they remembered to check: a generic type,
// a generic function, a generic receiver, an alias. Each found a real defect, and each would have
// missed any construct its author did not think of -- which is why the alias came back a second
// time through the JSON renderer after the human one had been fixed.
//
// This one enumerates instead. It walks every Go file in this repository, and for each declaration
// the parser finds asserts that:
//
//   - the human outline renders a bullet for it,
//   - the JSON outline emits a node for it,
//   - the two counts agree, in both directions, at every depth.
//
// The corpus is the repository's own source. That is the S4 lesson applied to itself: the codebase
// is the fixture nobody chose, and the only one that runs in CI. It contains generics, aliases,
// embedded fields, methods on generic receivers and grouped declarations, so the constructs the
// hand-picked tests covered are all present -- and so is whatever this repository gains next.
//
// The cost is that this walks the tree, which takes about 80 ms. That is cheap enough that there
// is no -short escape hatch: a mode that skipped it would report ok while skipping the tests most
// likely to catch a regression, and a green result that means less than it looks like is the one
// failure mode this whole exercise exists to end.
func TestRenderersAgreeOnTheWholeCorpus(t *testing.T) {
	root := repoRoot(t)
	files := goFilesUnder(t, root)
	if len(files) < 10 {
		t.Fatalf("only found %d .go files under %s; the corpus is too small to mean anything", len(files), root)
	}

	engine := goast.New()

	var checked, declarations int
	for _, path := range files {
		doc, err := engine.Parse(path)
		if err != nil {
			// A file golens cannot parse cannot be outlined by it either, so it is not a
			// disagreement between renderers. Skip rather than fail.
			continue
		}

		for _, depth := range []int{1, 2, format.UnlimitedDepth} {
			human, jsonHeads := renderBoth(t, engine, doc, depth)
			checked++

			humanBullets := humanDeclarationBullets(human)

			// Enumerable, and in both directions. A one-directional check is satisfied by
			// the empty set, which is exactly how --depth 1 shipped returning nothing.
			if len(humanBullets) != len(jsonHeads) {
				t.Errorf("%s at depth %d: %d human bullets, %d JSON nodes\nhuman:\n%s",
					rel(t, root, path), depth, len(humanBullets), len(jsonHeads), human)
				continue
			}
			if len(jsonHeads) == 0 && declarationsInFile(doc, depth) > 0 {
				t.Errorf("%s at depth %d: renderings agree on zero declarations, but the file has %d",
					rel(t, root, path), depth, declarationsInFile(doc, depth))
			}

			// Every JSON head must be reconstructible into the human text. This is the check
			// that catches a field one renderer has and the other does not: an alias with no
			// "=", a generic with no [T any], a method with no receiver.
			for _, head := range jsonHeads {
				if !strings.Contains(human, head) {
					t.Errorf("%s at depth %d: JSON head %q is not in the human output:\n%s",
						rel(t, root, path), depth, head, human)
				}
			}

			declarations += len(jsonHeads)
		}
	}

	// A corpus walk that silently matched nothing is the failure mode this whole exercise exists
	// to prevent, so it is asserted rather than assumed.
	if checked == 0 || declarations == 0 {
		t.Fatalf("the corpus walk verified nothing: %d renders, %d declarations. "+
			"A green run here must mean something was actually compared.", checked, declarations)
	}
	t.Logf("verified %d renders across %d files, %d declarations", checked, len(files), declarations)
}

// renderBoth produces the human and JSON outlines for one document at one depth.
func renderBoth(t *testing.T, engine domain.Engine, doc domain.Document, depth int) (string, []string) {
	t.Helper()

	var human bytes.Buffer
	if err := format.WriteOutlineHuman(&human, doc, depth, false); err != nil {
		t.Fatalf("WriteOutlineHuman: %v", err)
	}

	// The payload is always an array, even for one file, so the schema does not vary with
	// input type. Writing a bare object here would test a shape the tool never emits.
	var buf bytes.Buffer
	if err := format.WriteJSON(&buf, []format.OutlineFile{format.Outline(doc, depth)}, false); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}

	return human.String(), jsonDeclarationHeads(t, buf.Bytes())
}

// jsonDeclarationHeads returns the fully reassembled declaration head for every node in a payload.
//
// This is the invariant stated once: the JSON node carries Name, TypeParams, Receiver, Detail and
// IsAlias, and putting them back together the way Node.Declaration does must yield text that
// appears in the human outline. If a field is added to one renderer and not the other, the head
// assembled here stops matching and the test fails -- for every construct in the corpus, not for
// the ones somebody remembered.
func jsonDeclarationHeads(t *testing.T, data []byte) []string {
	t.Helper()

	var files []format.OutlineFile
	if err := json.Unmarshal(data, &files); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	var heads []string
	var walk func(nodes []format.OutlineNode)
	walk = func(nodes []format.OutlineNode) {
		for _, n := range nodes {
			// Group nodes are not declarations; they are a presentation of the hierarchy.
			if n.Kind != domain.KindImport {
				heads = append(heads, nodeDeclaration(n))
			}
			walk(n.Children)
		}
	}
	for _, f := range files {
		walk(f.Nodes)
	}
	return heads
}

// nodeDeclaration assembles a JSON node back into a declaration head.
//
// It deliberately mirrors domain.Node.Declaration rather than calling it: the point is to check
// that the *serialised* fields carry what the human renderer used, and a shared helper would
// hide a field that never made it into the payload.
func nodeDeclaration(n format.OutlineNode) string {
	// A field renders as "name detail" with no keyword, matching format.entry.
	if n.Kind == domain.KindField {
		if n.Detail != "" {
			return n.Name + " " + n.Detail
		}
		return n.Name
	}

	keyword := string(n.Kind)
	if n.Kind == domain.KindMethod {
		keyword = "func"
	}

	if n.Kind == domain.KindFunc || n.Kind == domain.KindMethod {
		if n.Receiver != "" {
			return keyword + " (" + n.Receiver + ") " + n.Name + n.Detail
		}
		return keyword + " " + n.Name + n.TypeParams + n.Detail
	}

	head := keyword + " " + n.Name + n.TypeParams
	if n.IsAlias {
		head += " ="
	}
	if n.Detail != "" {
		return head + " " + n.Detail
	}
	return head
}

// declarationsInFile counts the declarations the outline should render at a depth, used only to
// assert the walk matched something.
func declarationsInFile(doc domain.Document, depth int) int {
	n := 0
	var walk func(nodes []domain.Node, level int)
	walk = func(nodes []domain.Node, level int) {
		for _, node := range nodes {
			if node.IsGroup {
				walk(node.Children, level)
				continue
			}
			if depth >= 0 && level >= depth {
				continue
			}
			n++
			walk(node.Children, level+1)
		}
	}
	walk(doc.Nodes, 0)
	return n
}

func repoRoot(t *testing.T) string {
	t.Helper()

	// The test runs in internal/format, so the module root is two levels up.
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	root := filepath.Clean(filepath.Join(wd, "..", ".."))
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("no go.mod at %s: %v", root, err)
	}
	return root
}

func goFilesUnder(t *testing.T, root string) []string {
	t.Helper()

	var files []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // an unreadable directory is not this test's business
		}
		if d.IsDir() {
			name := d.Name()
			if path != root && (name == ".git" || name == "vendor" || name == "testdata" || name == "node_modules") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}
	if len(files) == 0 {
		t.Fatalf("no .go files under %s", root)
	}
	return files
}

func rel(t *testing.T, root, path string) string {
	t.Helper()
	r, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return r
}

// TestCorpusContainsTheConstructsThatBroke guards the corpus itself.
//
// An enumerable invariant is only as good as what it enumerates. If a future refactor removes the
// last generic type or the last alias from the non-test source, this walk would still pass while
// covering nothing -- the exact vacuous-pass failure TESTING.md's preflight exists to catch, in a
// different place.
func TestCorpusContainsTheConstructsThatBroke(t *testing.T) {
	root := repoRoot(t)
	fset := token.NewFileSet()

	var genericType, genericFunc, genericReceiver, alias, embedded, method int
	for _, path := range goFilesUnder(t, root) {
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch node := n.(type) {
			case *ast.FuncDecl:
				if node.Recv != nil && recvHasTypeParams(node.Recv) {
					genericReceiver++
				}
				if node.Type != nil && node.Type.TypeParams != nil && len(node.Type.TypeParams.List) > 0 {
					genericFunc++
				}
			case *ast.GenDecl:
				for _, spec := range node.Specs {
					s, ok := spec.(*ast.TypeSpec)
					if !ok {
						continue
					}
					if s.TypeParams != nil && len(s.TypeParams.List) > 0 {
						genericType++
					}
					if s.Assign.IsValid() {
						alias++
					}
					// Embedded fields live on the struct type, not in GenDecl.Specs: a
					// field with no name is embedded.
					if st, ok := s.Type.(*ast.StructType); ok && st.Fields != nil {
						for _, f := range st.Fields.List {
							if len(f.Names) == 0 {
								embedded++
							}
						}
					}
				}
			}
			return true
		})
	}
	if m := countMethods(t, root, fset); m > method {
		method = m
	}

	for _, c := range []struct {
		name string
		got  int
	}{
		{"generic types", genericType},
		{"generic functions", genericFunc},
		{"methods on generic receivers", genericReceiver},
		{"type aliases", alias},
		{"embedded fields", embedded},
		{"methods", method},
	} {
		if c.got == 0 {
			t.Errorf("the corpus contains no %s; the cross-renderer walk would pass while covering nothing", c.name)
		} else {
			t.Logf("corpus: %d %s", c.got, c.name)
		}
	}
}

// recvHasTypeParams reports whether a receiver is instantiated with type parameters.
//
// A pointer receiver is "*ast.StarExpr wrapping the index", so the pointer has to be unwrapped
// before the index is visible — otherwise every method on a generic type reads as non-generic and
// the corpus guard reports a gap that is not there.
func recvHasTypeParams(recv *ast.FieldList) bool {
	if recv == nil {
		return false
	}
	for _, f := range recv.List {
		if exprHasTypeParams(f.Type) {
			return true
		}
	}
	return false
}

func exprHasTypeParams(expr ast.Expr) bool {
	switch e := expr.(type) {
	case *ast.IndexExpr, *ast.IndexListExpr:
		return true
	case *ast.StarExpr:
		return exprHasTypeParams(e.X)
	case *ast.ParenExpr:
		return exprHasTypeParams(e.X)
	}
	return false
}

func countMethods(t *testing.T, root string, fset *token.FileSet) int {
	t.Helper()

	var n int
	for _, path := range goFilesUnder(t, root) {
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			continue
		}
		for _, d := range f.Decls {
			if fn, ok := d.(*ast.FuncDecl); ok && fn.Recv != nil {
				n++
			}
		}
	}
	return n
}

var _ = fmt.Sprintf
