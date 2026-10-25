package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCrossPackageNameIsReportedAsSuchAtTheCLI is the user-visible half of the cross-package
// defect: a name declared once in `package p` and once in `package p_test` is not a redeclaration,
// and `check` already exits 0 on the same directory.
func TestCrossPackageNameIsReportedAsSuchAtTheCLI(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.go", "package p\n\nfunc A() {}\n")
	writeFile(t, dir, "b_test.go", "package p_test\n\nfunc A() {}\n")

	// check already understood that these are separate namespaces.
	if _, stderr, code := run(t, "check", dir); code != 0 {
		t.Errorf("check exited %d; two packages in one directory are legal Go\n%s", code, stderr)
	}

	stdout, stderr, code := run(t, "get", "A", dir)
	if code != 2 {
		t.Errorf("exit = %d, want 2: the caller must still choose", code)
	}
	if stdout != "" {
		t.Errorf("stdout should stay empty:\n%s", stdout)
	}
	if !strings.Contains(stderr, "package p") || !strings.Contains(stderr, "package p_test") {
		t.Errorf("stderr does not name both packages:\n%s", stderr)
	}
	if strings.Contains(stderr, "is ambiguous") {
		t.Errorf("stderr claims an ambiguity within one package, which is not what happened:\n%s", stderr)
	}

	// The advice has to be printed, not just present on the error: it lived inside the
	// candidate loop in Execute, so an error with no candidate list never showed its remedy.
	// Here there is no candidate list at all, which is precisely the case that dropped it.
	if !strings.Contains(stderr, "pass the specific file") {
		t.Errorf("stderr carries no advice, so the caller has no remedy:\n%s", stderr)
	}
}

// TestPackageQualifiedQueryResolvesAtCLI is the user-visible half of the package-qualified
// lookup feature: `golens get p.Func` now resolves Func in package p.
func TestPackageQualifiedQueryResolvesAtCLI(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.go", "package domain\n\ntype Node struct{}\n")

	// Package-qualified lookup now resolves.
	stdout, stderr, code := run(t, "get", "domain.Node", dir)
	if code != 0 {
		t.Errorf("exit = %d, want 0: package-qualified lookup should resolve", code)
	}
	if stderr != "" {
		t.Errorf("stderr should be empty on success:\n%s", stderr)
	}
	if !strings.Contains(stdout, "Node struct") {
		t.Errorf("stdout should contain the resolved symbol:\n%s", stdout)
	}

	// bounds shares the resolver, so it must agree.
	stdout2, stderr2, code2 := run(t, "bounds", "domain.Node", filepath.Join(dir, "a.go"))
	if code2 != 0 {
		t.Errorf("bounds exit = %d, want 0", code2)
	}
	if stderr2 != "" {
		t.Errorf("bounds stderr should be empty:\n%s", stderr2)
	}
	if !strings.Contains(stdout2, "Node") {
		t.Errorf("bounds stdout should contain the symbol:\n%s", stdout2)
	}
}

// TestAdviceIsPrintedWithoutACandidateList isolates the Execute change, which was its own bug
// independent of what triggered it.
func TestAdviceIsPrintedWithoutACandidateList(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.go", "package p\n\nfunc A() {}\n")
	writeFile(t, dir, "b_test.go", "package p_test\n\nfunc A() {}\n")

	_, stderr, _ := run(t, "get", "A", dir)

	advised := 0
	for line := range strings.SplitSeq(stderr, "\n") {
		if strings.Contains(line, "pass ") {
			advised++
		}
	}
	if advised != 1 {
		t.Errorf("advice printed %d times, want 1:\n%s", advised, stderr)
	}
}

// TestSamePackageAmbiguityStillListsCandidatesAndAdvises guards the other branch: an error that
// does carry candidates must still print both.
func TestSamePackageAmbiguityStillListsCandidatesAndAdvises(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.go", "package p\n\nfunc dup() {}\n")
	writeFile(t, dir, "b.go", "package p\n\nfunc dup() {}\n")

	_, stderr, code := run(t, "get", "dup", dir)
	if code != 2 {
		t.Errorf("exit = %d, want 2", code)
	}
	if !strings.Contains(stderr, "a.go: dup") || !strings.Contains(stderr, "b.go: dup") {
		t.Errorf("candidates missing:\n%s", stderr)
	}
	if !strings.Contains(stderr, "qualified name") {
		t.Errorf("advice missing for a genuine ambiguity, where a qualified name does apply:\n%s", stderr)
	}
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}
