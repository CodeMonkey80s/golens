package usecase_test

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/CodeMonkey80s/golens/internal/engine/goast"
	"github.com/CodeMonkey80s/golens/internal/format"
	"github.com/CodeMonkey80s/golens/internal/testfixture"
	"github.com/CodeMonkey80s/golens/internal/usecase"
)

// TestExternalTestPackageIsNotAConflict guards a false positive this check produced on the
// project's own source.
//
// A Go namespace is one package in one directory, and a directory may legitimately hold two: an
// external test package (foo_test) beside the package under test (foo). A helper declared in
// both is not a redeclaration. Keying the conflict map on directory alone reported them, which
// is the worse direction for this tool -- a namespace error is an "error" severity diagnostic,
// and an agent reacting to one might delete a working declaration.
func TestExternalTestPackageIsNotAConflict(t *testing.T) {
	dir := t.TempDir()
	// The layout that matters: package p declares a helper, and the external test package
	// p_test declares one of the same name. Sharing a directory is not sharing a namespace, so
	// this must not be reported.
	testfixture.Write(t, dir, "a.go", "package p\n\nfunc helper() int { return 1 }\n")
	testfixture.Write(t, dir, "external_test.go", "package p_test\n\nfunc helper() int { return 2 }\n")

	var stdout, stderr bytes.Buffer
	err := usecase.Check(goast.New(), dir, &stdout, &stderr, format.Options{})
	if err != nil {
		t.Errorf("a helper in both p and p_test must not be a conflict:\n%s", stderr.String())
	}
}

// TestDistinctNamespacesDoNotConflict covers the other half of the rule: two directories may both
// declare the same package name and the same symbol without conflicting.
func TestDistinctNamespacesDoNotConflict(t *testing.T) {
	dir := t.TempDir()
	testfixture.Write(t, dir, "one/p.go", "package p\n\nfunc Shared() {}\n")
	testfixture.Write(t, dir, "two/p.go", "package p\n\nfunc Shared() {}\n")

	var stdout, stderr bytes.Buffer
	if err := usecase.Check(goast.New(), dir, &stdout, &stderr, format.Options{}); err != nil {
		t.Errorf("the same package name in two directories is legal:\n%s", stderr.String())
	}
}

// TestSameNamespaceInSameDirectoryConflicts is the positive case for the rule above, so the two
// cannot both pass by accident.
func TestSameNamespaceInSameDirectoryConflicts(t *testing.T) {
	dir := t.TempDir()
	testfixture.Write(t, dir, "a.go", "package p\n\nfunc Shared() {}\n")
	testfixture.Write(t, dir, "b.go", "package p\n\nfunc Shared() {}\n")

	var stdout, stderr bytes.Buffer
	if err := usecase.Check(goast.New(), dir, &stdout, &stderr, format.Options{}); err == nil {
		t.Error("two files in one package declaring the same name must conflict")
	}
}

// TestRepositoryPassesItsOwnCheck runs the tool against its own source tree.
//
// A checker that cannot be run on its own project is a checker nobody will trust. This also
// covers the awkward real-world case: a directory holding package foo, package foo_test, and
// several test files with helpers spread between them.
func TestRepositoryPassesItsOwnCheck(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	// The test runs in internal/usecase; the module root is two levels up. testdata/ is
	// excluded because its fixtures are deliberately invalid: store.go has an unused import so
	// that the outline golden output has a specific shape.
	root = root + "/../.."

	var stdout, stderr bytes.Buffer
	err = usecase.Check(goast.New(), root, &stdout, &stderr, format.Options{})
	if err != nil {
		for _, line := range strings.Split(strings.TrimRight(stderr.String(), "\n"), "\n") {
			if strings.Contains(line, "/testdata/") {
				continue // fixtures are intentionally imperfect
			}
			t.Errorf("golens reports on its own source: %s", line)
		}
	}
}
