package cli

import (
	"strings"
	"testing"
)

// TestCheckMultiplePaths covers the user-visible variadic form.
func TestCheckMultiplePaths(t *testing.T) {
	clean := t.TempDir()
	writeFile(t, clean, "a.go", "package p\n\nfunc A() {}\n")

	unused := t.TempDir()
	writeFile(t, unused, "b.go", "package p\n\nimport \"fmt\"\n\nfunc B() {}\n")

	_, stderr, code := run(t, "check", clean, unused)
	if code != 3 {
		t.Fatalf("exit = %d, want 3 (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stderr, "imported and not used") {
		t.Errorf("diagnostics do not mention the unused import:\n%s", stderr)
	}

	_, stderr, code = run(t, "check", clean)
	if code != 0 {
		t.Fatalf("clean exit = %d, want 0 (stderr: %s)", code, stderr)
	}
}
