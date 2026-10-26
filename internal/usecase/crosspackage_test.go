package usecase_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/CodeMonkey80s/golens/internal/engine/goast"
	"github.com/CodeMonkey80s/golens/internal/format"
	"github.com/CodeMonkey80s/golens/internal/testfixture"
	"github.com/CodeMonkey80s/golens/internal/usecase"
)

// TestCrossPackageNameIsNotReportedAsAmbiguous covers a directory holding two packages.
//
// `package p` beside an external test `package p_test` is legal Go, and both may declare the
// same name without either redeclaring anything. Reporting that as "ambiguous, choose one"
// implies a duplicate declaration to go and fix, and there is none — check already says so,
// because packageSymbols keys its namespace on directory *and* package name. The two commands
// disagreeing about whether a conflict exists was the defect.
func TestCrossPackageNameIsNotReportedAsAmbiguous(t *testing.T) {
	dir := t.TempDir()
	testfixture.Write(t, dir, "a.go", "package p\n\nfunc A() {}\n")
	testfixture.Write(t, dir, "b_test.go", "package p_test\n\nfunc A() {}\n")

	var out bytes.Buffer
	err := usecase.Get(goast.New(), "A", dir, true, &out, format.Options{})

	var cross usecase.CrossPackageSymbol
	if !errors.As(err, &cross) {
		t.Fatalf("err = %v (%T), want CrossPackageSymbol, not AmbiguousSymbol", err, err)
	}

	var ambiguous usecase.AmbiguousSymbol
	if errors.As(err, &ambiguous) {
		t.Error("the error also satisfies AmbiguousSymbol; that type claims a conflict that does not exist")
	}

	if len(cross.Groups) != 2 {
		t.Fatalf("groups = %d, want 2", len(cross.Groups))
	}
	// Groups are sorted by package name so the message is stable across runs.
	if cross.Groups[0].Package != "p" || cross.Groups[1].Package != "p_test" {
		t.Errorf("groups = %q, %q; want p then p_test in order", cross.Groups[0].Package, cross.Groups[1].Package)
	}

	if out.Len() != 0 {
		t.Errorf("stdout should stay empty on a resolution failure:\n%s", out.String())
	}

	if !cross.IsNotFound() {
		t.Error("IsNotFound() = false, want true: the caller must still choose and exit 2")
	}
}

// TestCrossPackageAdviceRecommendsPackageQualifiedLookup guards the remedy. The old advice
// named a file only, and the older one recommended a qualified name before package-qualified
// lookup existed. Since v0.3, `p.A` and `p_test.A` resolve, so the advice names that form and
// keeps a file as the unambiguous fallback.
func TestCrossPackageAdviceRecommendsPackageQualifiedLookup(t *testing.T) {
	dir := t.TempDir()
	testfixture.Write(t, dir, "a.go", "package p\n\nfunc A() {}\n")
	testfixture.Write(t, dir, "b_test.go", "package p_test\n\nfunc A() {}\n")

	var out bytes.Buffer
	err := usecase.Get(goast.New(), "A", dir, true, &out, format.Options{})

	var cross usecase.CrossPackageSymbol
	if !errors.As(err, &cross) {
		t.Fatalf("err = %v (%T), want CrossPackageSymbol", err, err)
	}

	advice, ok := err.(interface{ Advice() string })
	if !ok {
		t.Fatal("CrossPackageSymbol offers no Advice()")
	}
	if got := advice.Advice(); !strings.Contains(got, "p.A") {
		t.Errorf("Advice() = %q, want the package-qualified form p.A", got)
	}

	// The form the advice names must actually work, which is the property that failed before.
	for _, name := range []string{"p.A", "p_test.A"} {
		var probe bytes.Buffer
		if perr := usecase.Get(goast.New(), name, dir, true, &probe, format.Options{}); perr != nil {
			t.Errorf("%q should now resolve with package-qualified lookup: %v", name, perr)
		}
		if !strings.Contains(probe.String(), "func A()") {
			t.Errorf("%q resolved but output unexpected:\n%s", name, probe.String())
		}
	}
}

// TestSamePackageAmbiguityIsUnchanged guards the other direction: a genuine duplicate within
// one package must still report as ambiguous, with candidate lines to choose between.
//
// Without this the cross-package change could swallow real conflicts, which is the worse error
// by far -- a redeclaration that ships is a build failure, a redeclaration that is missed is not
// caught by golens but is caught by `go build`.
func TestSamePackageAmbiguityIsUnchanged(t *testing.T) {
	dir := t.TempDir()
	testfixture.Write(t, dir, "a.go", "package p\n\nfunc dup() {}\n")
	testfixture.Write(t, dir, "b.go", "package p\n\nfunc dup() {}\n")

	var out bytes.Buffer
	err := usecase.Get(goast.New(), "dup", dir, true, &out, format.Options{})

	var amb usecase.AmbiguousSymbol
	if !errors.As(err, &amb) {
		t.Fatalf("err = %v (%T), want AmbiguousSymbol", err, err)
	}
	if len(amb.Candidates()) != 2 {
		t.Errorf("candidates = %v, want 2", amb.Candidates())
	}
}

// TestSameNameInOnePackageStillResolvesWhenUniqueElsewhere confirms the common case is
// untouched: a helper that exists in only one package of a two-package directory resolves
// normally rather than reporting a cross-package split.
func TestSameNameInOnePackageStillResolvesWhenUniqueElsewhere(t *testing.T) {
	dir := t.TempDir()
	testfixture.Write(t, dir, "a.go", "package p\n\nfunc A() {}\n")
	testfixture.Write(t, dir, "b_test.go", "package p_test\n\nfunc helper() {}\n")

	var out bytes.Buffer
	if err := usecase.Get(goast.New(), "helper", dir, true, &out, format.Options{}); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !strings.Contains(out.String(), "func helper()") {
		t.Errorf("output:\n%s", out.String())
	}
}
