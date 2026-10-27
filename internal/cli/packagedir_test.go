package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPackageDirDisambiguatesSameNamedPackages covers the case a package-qualified query
// cannot solve on its own: two directories both declaring `package util`.
func TestPackageDirDisambiguatesSameNamedPackages(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "a")
	second := filepath.Join(root, "b")
	for _, dir := range []string{first, second} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, first, "a.go", "package util\n\nfunc X() {}\n")
	writeFile(t, second, "b.go", "package util\n\nfunc X() {}\n")

	_, stderr, code := run(t, "get", "util.X", root)
	if code != 2 {
		t.Fatalf("without --package-dir exit = %d, want 2 (stderr: %s)", code, stderr)
	}

	stdout, stderr, code := run(t, "get", "util.X", root, "--package-dir", second)
	if code != 0 {
		t.Fatalf("with --package-dir exit = %d, want 0 (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stdout, "func X()") || !strings.Contains(stdout, second) {
		t.Errorf("resolved the wrong package or no source:\n%s", stdout)
	}

	stdout, stderr, code = run(t, "bounds", "util.X", root, "--package-dir", second, "--json")
	if code != 0 {
		t.Fatalf("bounds with --package-dir exit = %d, want 0 (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stdout, "start_byte") || !strings.Contains(stdout, filepath.ToSlash(second)) {
		t.Errorf("bounds resolved the wrong package:\n%s", stdout)
	}
}
