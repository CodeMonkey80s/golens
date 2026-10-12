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

// TestPackageQualifiedQueryAdviceReachesStderr is the user-visible half of the
// package-qualified miss: `golens get p.Node .` answered "symbol not found", the same message
// an absent symbol gets, so a caller had no way to learn that the dotted form is not a lookup
// here. It is still exit 2 — the query must be corrected — but it now says which form resolves,
// and that line has to reach stderr to be of any use.
func TestPackageQualifiedQueryAdviceReachesStderr(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.go", "package domain\n\ntype Node struct{}\n")

	stdout, stderr, code := run(t, "get", "domain.Node", dir)
	if code != 2 {
		t.Errorf("exit = %d, want 2: the caller must correct the query", code)
	}
	if stdout != "" {
		t.Errorf("stdout should stay empty:\n%s", stdout)
	}
	if !strings.Contains(stderr, `symbol "domain.Node" not found`) {
		t.Errorf("stderr does not report the query:\n%s", stderr)
	}
	if !strings.Contains(stderr, `pass "Node"`) {
		t.Errorf("stderr carries no advice naming the form that resolves:\n%s", stderr)
	}

	// bounds shares the resolver, so it must not disagree about the same query.
	if _, bstderr, bcode := run(t, "bounds", "domain.Node", filepath.Join(dir, "a.go")); bcode != 2 || !strings.Contains(bstderr, `pass "Node"`) {
		t.Errorf("bounds disagrees with get: exit %d\n%s", bcode, bstderr)
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
