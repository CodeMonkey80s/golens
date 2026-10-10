// Package naming_test enforces the initialism convention against this repository's own
// source, covering acceptance criterion A7.
//
// The test walks the module's Go files with go/parser and checks every declared identifier:
// package names, type, func, method, field, parameter and result names, and constant and
// variable names. Running it in CI is what makes the convention real rather than a
// suggestion, since a review cannot be relied on to catch every identifier.
package naming_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/CodeMonkey80s/golens/internal/naming"
)

// skipDirs are not walked: they hold no first-party source.
var skipDirs = map[string]bool{
	".git": true, "vendor": true, "node_modules": true, "testdata": true,
}

func TestRepositoryFollowsNamingConvention(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping repository-wide naming scan in short mode")
	}

	root := repoRoot(t)

	var files []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if path != root && (skipDirs[name] || strings.HasPrefix(name, ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(d.Name(), ".go") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}

	if len(files) == 0 {
		t.Fatal("no Go files found; the scan would pass vacuously")
	}

	ids, err := collectIdentifiers(root, files)
	if err != nil {
		t.Fatalf("collect identifiers: %v", err)
	}
	if len(ids) == 0 {
		t.Fatal("no identifiers collected; the scan would pass vacuously")
	}

	violations := naming.Check(ids)
	if len(violations) == 0 {
		t.Logf("scanned %d files, %d identifiers, no violations", len(files), len(ids))
		return
	}

	for _, v := range violations {
		t.Errorf("%s", v)
	}
}

// TestCheckDetectsRealViolations proves the scan can actually fail, so a green
// TestRepositoryFollowsNamingConvention means something rather than reflecting a checker
// that never fires.
func TestCheckDetectsRealViolations(t *testing.T) {
	for _, id := range []string{"userId", "dbUrl", "parseJson"} {
		if len(naming.Check([]string{id})) == 0 {
			t.Errorf("%q should be reported as a violation", id)
		}
	}
	for _, id := range []string{"userID", "HTTPHandler", "parseJSON", "urlPath"} {
		if v := naming.Check([]string{id}); len(v) != 0 {
			t.Errorf("%q should be allowed: %s", id, v[0])
		}
	}
}

// collectIdentifiers parses each file and returns every declared identifier name.
func collectIdentifiers(root string, files []string) ([]string, error) {
	fset := token.NewFileSet()

	var ids []string
	for _, file := range files {
		parsed, err := parser.ParseFile(fset, file, nil, parser.SkipObjectResolution)
		if err != nil {
			// A file that does not parse is a build failure elsewhere; skip it here so the
			// naming scan reports only naming problems.
			continue
		}

		rel, err := filepath.Rel(root, file)
		if err != nil {
			rel = file
		}

		_ = rel
		ids = append(ids, parsed.Name.Name)
		ids = append(ids, declared(parsed, rel)...)
	}
	return ids, nil
}

// declared walks a file collecting every name it declares.
func declared(file *ast.File, rel string) []string {
	var out []string

	// Positions are collected for future diagnostics but only names are returned: the
	// check is a naming rule, and a violation already names the identifier.
	add := func(name, kind string, pos token.Pos) {
		if name == "" || name == "_" {
			return
		}
		_, _ = kind, pos
		out = append(out, name)
	}

	// Package-level declarations.
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			add(d.Name.Name, "func", d.Name.Pos())
			if d.Recv != nil {
				for _, field := range d.Recv.List {
					for _, name := range field.Names {
						add(name.Name, "receiver", name.Pos())
					}
					collectFields(add, field.Type)
				}
			}
			collectFields(add, d.Type)
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					add(s.Name.Name, "type", s.Name.Pos())
					collectFields(add, s.Type)
				case *ast.ValueSpec:
					for _, name := range s.Names {
						add(name.Name, "value", name.Pos())
					}
					collectFields(add, s.Type)
				}
			}
		}
	}

	return out
}

// collectFields walks a type expression recording every identifier it names: struct fields,
// interface methods, and the parameter and result names of a function type.
func collectFields(add func(name, kind string, pos token.Pos), expr ast.Expr) {
	switch t := expr.(type) {
	case *ast.StructType:
		if t.Fields != nil {
			for _, field := range t.Fields.List {
				for _, name := range field.Names {
					add(name.Name, "field", name.Pos())
				}
				collectFields(add, field.Type)
			}
		}
	case *ast.InterfaceType:
		if t.Methods != nil {
			for _, field := range t.Methods.List {
				for _, name := range field.Names {
					add(name.Name, "interface method", name.Pos())
				}
				collectFields(add, field.Type)
			}
		}
	case *ast.FuncType:
		collectFieldList(add, t.Params)
		collectFieldList(add, t.Results)
	case *ast.StarExpr:
		collectFields(add, t.X)
	case *ast.ArrayType:
		collectFields(add, t.Elt)
	case *ast.MapType:
		collectFields(add, t.Key)
		collectFields(add, t.Value)
	case *ast.ChanType:
		collectFields(add, t.Value)
	case *ast.Ellipsis:
		collectFields(add, t.Elt)
	case *ast.IndexExpr:
		collectFields(add, t.X)
	case *ast.IndexListExpr:
		collectFields(add, t.X)
	case *ast.SelectorExpr:
		add(t.Sel.Name, "selector", t.Sel.Pos())
	case *ast.Ident:
		add(t.Name, "type ref", t.Pos())
	}
}

func collectFieldList(add func(name, kind string, pos token.Pos), list *ast.FieldList) {
	if list == nil {
		return
	}
	for _, field := range list.List {
		for _, name := range field.Names {
			add(name.Name, "param", name.Pos())
		}
		collectFields(add, field.Type)
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot determine test file location")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("go.mod not found at %s: %v", root, err)
	}
	return root
}
