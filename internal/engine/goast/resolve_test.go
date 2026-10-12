package goast_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CodeMonkey80s/golens/internal/engine/goast"
)

// The engine skipped object resolution to save ~17% of parse time, and the comment justifying
// the opposite was false:
//
//	Object resolution is deliberately left ON -- it populates ast.File.Unresolved,
//	which the unused-import check in check depends on. Correctness over a microsecond.
//
// Nothing read Unresolved; that sentence was the only reference in the repository. The
// unused-import check is collectUsedNames, a lexical scan over the raw source.
//
// A comment is not evidence, and the previous version of this file was not a test either: it
// parsed the corpus through the engine under both modes, which necessarily compared the
// engine with itself and would have passed if the engine had grown a dependency on resolved
// identifiers. These two are the real checks.

// TestEngineDoesNotReadResolvedIdentifiers pins the premise the optimisation rests on, as a
// property of the source rather than a claim in a comment.
//
// The resolver exists to populate Ident.Obj, File.Scope and File.Unresolved. If any engine
// code starts reading one of those, skipping it stops being safe and this fails, naming the
// line — before the change lands rather than after a panic on a file an agent asked about.
func TestEngineDoesNotReadResolvedIdentifiers(t *testing.T) {
	root := repoRoot(t)

	// Identifiers the resolver alone populates. "Scope" and "Obj" are matched as selectors
	// so an unrelated local variable of that name is not a false alarm; the resolver's
	// outputs are only ever read as fields.
	forbidden := []string{".Obj", "Unresolved", ".Scope", "Object:"}

	for _, path := range goFilesUnder(t, root, false) {
		if !strings.HasSuffix(path, "engine.go") &&
			!strings.HasSuffix(path, "print.go") &&
			!strings.HasSuffix(path, "buildconstraint.go") {
			continue
		}

		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}

		for i, line := range strings.Split(string(src), "\n") {
			trimmed := strings.TrimSpace(line)
			// The comment that made the false claim is allowed to name the fields it is
			// explaining away; nothing else may.
			if strings.HasPrefix(trimmed, "//") {
				continue
			}
			for _, bad := range forbidden {
				if strings.Contains(trimmed, bad) {
					t.Errorf("%s:%d reads %q, which only object resolution populates:\n\t%s",
						filepath.Base(path), i+1, bad, trimmed)
				}
			}
		}
	}
}

// TestDocumentsMatchAParseWithResolution cross-checks the engine's output against a parse that
// does resolve identifiers.
//
// For every non-test file in this repository, the import paths and the count of top-level
// declarations the engine reports must match what a resolution-enabled parse of the same source
// says. That is an independent source of truth — the raw AST, parsed the other way — so this
// can fail if the engine quietly depends on resolution, where an engine-versus-engine comparison
// could not.
func TestDocumentsMatchAParseWithResolution(t *testing.T) {
	root := repoRoot(t)
	engine := goast.New()

	checked := 0
	for _, path := range goFilesUnder(t, root, false) {
		src, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		// The reference parse: resolution ON, read directly off the raw AST.
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, src, 0)
		if err != nil {
			continue // unparseable files are covered elsewhere
		}

		doc, err := engine.ParseSource(path, src)
		if err != nil {
			t.Fatalf("%s: engine rejected a file the reference parse accepted: %v",
				filepath.Base(path), err)
		}

		var wantImports []string
		for _, imp := range file.Imports {
			if imp.Path != nil {
				wantImports = append(wantImports, imp.Path.Value)
			}
		}
		if strings.Join(doc.Imports, "|") != strings.Join(wantImports, "|") {
			t.Errorf("%s: Imports = %v, want %v (from a parse with object resolution)",
				filepath.Base(path), doc.Imports, wantImports)
		}

		if got, want := len(doc.Symbols), countSymbols(file); got != want {
			t.Errorf("%s: %d symbols, want %d (from a parse with object resolution)",
				filepath.Base(path), got, want)
		}

		checked++
	}

	if checked == 0 {
		t.Fatal("compared zero files; the cross-check proved nothing")
	}
	t.Logf("cross-checked %d files against a resolution-enabled parse", checked)
}

// countSymbols counts top-level declarations the way the engine does: one per FuncDecl, one per
// TypeSpec, and one per name in a ValueSpec.
func countSymbols(file *ast.File) int {
	n := 0
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			n++
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					n++
				case *ast.ValueSpec:
					n += len(s.Names)
				}
			}
		}
	}
	return n
}

// repoRoot locates the repository root from the test's working directory.
func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	root := filepath.Clean(filepath.Join(wd, "..", "..", ".."))
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("no go.mod at %s: %v", root, err)
	}
	return root
}

// goFilesUnder lists .go files beneath root, skipping test files when tests is false.
func goFilesUnder(t *testing.T, root string, tests bool) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == "vendor" || strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") {
			return nil
		}
		if !tests && strings.HasSuffix(p, "_test.go") {
			return nil
		}
		out = append(out, p)
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	return out
}
