package cli

import (
	"strings"
	"testing"
)

// TestCheckTagsFlag pins the flag wiring for the build context. The usecase test covers the
// constraint maths; this covers the invocation a caller actually types.
func TestCheckTagsFlag(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a_integration.go", "//go:build integration\n\npackage p\n\nfunc F() {}\n")
	writeFile(t, dir, "b.go", "package p\n\nfunc F() {}\n")

	_, stderr, code := run(t, "check", dir)
	if code != 0 {
		t.Fatalf("default check exit = %d, want 0 (stderr: %s)", code, stderr)
	}

	_, stderr, code = run(t, "check", dir, "--tags", "integration")
	if code != 3 {
		t.Fatalf("tagged check exit = %d, want 3 (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stderr, "F redeclared") {
		t.Errorf("tagged check did not report the conflict:\n%s", stderr)
	}
}
