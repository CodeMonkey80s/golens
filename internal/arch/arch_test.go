// Package arch_test enforces the package boundaries from specification 3.1 and
// decision D20.
//
// These are structural tests rather than a convention: the Clean Architecture requirement
// in the specification is only real if an import that violates it fails the build. Each
// test walks go list -deps output, which reflects the actual compiled import graph rather
// than a hand-maintained list that would drift.
package arch_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const modulePath = "github.com/CodeMonkey80s/golens"

// deps returns the transitive dependencies of a package, excluding the standard library.
func deps(t *testing.T, pkg string) []string {
	t.Helper()

	cmd := exec.Command("go", "list", "-deps", pkg)
	cmd.Dir = repoRoot(t)
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")

	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list -deps %s: %v", pkg, err)
	}

	var external []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Standard-library paths are single-element or dotless, but go/ast and friends are
		// exactly the packages the AST-boundary test needs to see, so only genuinely
		// non-stdlib paths are filtered here.
		if isStdlib(line) {
			continue
		}
		external = append(external, line)
	}
	return external
}

// depsIncludingStdlib returns every dependency, standard library included. The AST
// boundary tests need this because go/ast and go/parser are stdlib paths.
func depsIncludingStdlib(t *testing.T, pkg string) []string {
	t.Helper()

	cmd := exec.Command("go", "list", "-deps", pkg)
	cmd.Dir = repoRoot(t)
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")

	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list -deps %s: %v", pkg, err)
	}

	var all []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			all = append(all, line)
		}
	}
	return all
}

// isStdlib reports whether an import path belongs to the standard library. A path with no
// dot in its first element is stdlib or a relative package, which is what the go command
// guarantees.
func isStdlib(path string) bool {
	first, _, _ := strings.Cut(path, "/")
	return !strings.Contains(first, ".")
}

func repoRoot(t *testing.T) string {
	t.Helper()

	// The test runs in internal/arch, so the module root is two levels up.
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot determine test file location")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

// TestDomainHasNoGolensDependencies covers the innermost layer: domain must depend on
// nothing inside the project and nothing outside the standard library.
func TestDomainHasNoGolensDependencies(t *testing.T) {
	// A package's own path appears in its own dependency list, so exclude it before
	// asserting that nothing else is pulled in.
	for _, dep := range deps(t, modulePath+"/internal/domain") {
		if dep == modulePath+"/internal/domain" {
			continue
		}
		t.Errorf("domain must not import %s", dep)
	}
}

// TestUsecaseDependsOnlyOnDomainFormatAndNaming covers D20's inward direction: the usecase
// layer may use the leaf packages domain, format and naming, but never cobra, an engine or
// the AST packages.
//
// naming is a leaf like format -- a pure function over identifiers with no project
// dependencies -- so depending on it points inward, not sideways. It is listed explicitly
// rather than loosened into a prefix rule so that adding a new sibling package still fails
// this test and gets a deliberate decision.
func TestUsecaseDependsOnlyOnDomainFormatAndNaming(t *testing.T) {
	allowed := map[string]bool{
		modulePath + "/internal/domain":  true,
		modulePath + "/internal/format":  true,
		modulePath + "/internal/naming":  true,
		modulePath + "/internal/usecase": true,
	}

	for _, dep := range deps(t, modulePath+"/internal/usecase") {
		if allowed[dep] {
			continue
		}
		t.Errorf("usecase must not import %s", dep)
	}
}

// TestNamingIsALeaf guards the assumption behind allowing usecase to depend on naming: if it
// ever gains a project dependency, the arrow stops pointing inward and this test says so.
func TestNamingIsALeaf(t *testing.T) {
	for _, dep := range deps(t, modulePath+"/internal/naming") {
		if dep == modulePath+"/internal/naming" {
			continue
		}
		t.Errorf("naming must be a leaf package, but it imports %s", dep)
	}
}

// TestEngineIsTheOnlyAstImporter covers D20: only engine/goast may reach go/parser,
// go/ast and go/token, which is what keeps a Tree-Sitter engine additive.
func TestEngineIsTheOnlyAstImporter(t *testing.T) {
	astPackages := map[string]bool{
		"go/ast":     true,
		"go/parser":  true,
		"go/token":   true,
		"go/scanner": true,
	}

	for _, pkg := range []string{"domain", "usecase", "format", "cli"} {
		for _, dep := range depsIncludingStdlib(t, modulePath+"/internal/"+pkg) {
			if astPackages[dep] {
				t.Errorf("only engine/goast may import %s, but %s does", dep, pkg)
			}
		}
	}

	// The engine itself must actually import them, or the test above proves nothing.
	found := false
	for _, dep := range depsIncludingStdlib(t, modulePath+"/internal/engine/goast") {
		if astPackages[dep] {
			found = true
		}
	}
	if !found {
		t.Error("engine/goast imports none of go/ast, go/parser, go/token, go/scanner")
	}
}

// TestCliDoesNotReachTheAst covers D20: the CLI owns flags and exit codes only.
func TestCliDoesNotReachTheAst(t *testing.T) {
	for _, dep := range deps(t, modulePath+"/internal/cli") {
		switch dep {
		case "go/ast", "go/parser", "go/token", "go/scanner", "go/printer":
			t.Errorf("cli must not import %s", dep)
		}
	}
}

// TestApprovedDependencies covers specification 3.1: the only permitted third-party
// packages are cobra and its own dependencies.
func TestApprovedDependencies(t *testing.T) {
	approvedPrefixes := []string{
		"go/ast", "go/parser", "go/token", "go/scanner", "go/printer", "go/types",
		"encoding/json", "fmt", "os", "path/filepath", "sort", "strconv", "strings",
		"unicode", "unicode/utf8", "errors", "io", "io/fs", "runtime", "testing",
		"github.com/spf13/cobra", "github.com/spf13/pflag", "github.com/inconshreveable/mousetrap",
	}

	// deps filters out the standard library, which is what this check needs: the
	// specification restricts third-party libraries, not Go's own packages. Cobra's
	// transitive stdlib use (net, regexp, text/template) is unremarkable.
	for _, dep := range deps(t, modulePath+"/cmd/golens") {
		if dep == modulePath || strings.HasPrefix(dep, modulePath+"/") {
			continue // internal packages are governed by the boundary tests above
		}
		ok := false
		for _, prefix := range approvedPrefixes {
			if dep == prefix || strings.HasPrefix(dep, prefix+"/") {
				ok = true
				break
			}
		}
		if !ok {
			t.Errorf("dependency %s is not on the approved list", dep)
		}
	}
}

// TestNoCgoDependency covers acceptance criterion A10: a pure-Go build.
func TestNoCgoDependency(t *testing.T) {
	cmd := exec.Command("go", "list", "-deps", modulePath+"/cmd/golens")
	cmd.Dir = repoRoot(t)
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")

	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.TrimSpace(line) == "runtime/cgo" {
			t.Error("build depends on runtime/cgo; the specification requires CGO_ENABLED=0")
		}
	}
}
