package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestTraversalFlags covers the CLI wiring for --skip-testdata and --skip-ignored.
func TestTraversalFlags(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "good.go", "package p\n\nfunc Good() {}\n")
	if err := os.MkdirAll(filepath.Join(dir, "testdata"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "testdata"), "broken.go", "package p\n\nfunc Broken( {\n")
	writeFile(t, dir, "_ignored.go", "package p\n\nfunc Ignored() {}\n")

	_, stderr, code := run(t, "outline", dir)
	if code != 4 {
		t.Fatalf("default outline exit = %d, want 4 (stderr: %s)", code, stderr)
	}

	stdout, stderr, code := run(t, "outline", dir, "--skip-testdata")
	if code != 0 {
		t.Fatalf("outline --skip-testdata exit = %d, want 0 (stderr: %s)", code, stderr)
	}
	if strings.Contains(stdout, "Broken") {
		t.Errorf("outline --skip-testdata still listed the fixture:\n%s", stdout)
	}

	_, stderr, code = run(t, "get", "Ignored", dir, "--skip-ignored")
	if code != 2 {
		t.Fatalf("get --skip-ignored exit = %d, want 2 (stderr: %s)", code, stderr)
	}

	stdout, stderr, code = run(t, "get", "Ignored", dir, "--skip-testdata")
	if code != 0 {
		t.Fatalf("default get exit = %d, want 0 (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stdout, "func Ignored()") {
		t.Errorf("default get did not resolve the ignored-name symbol:\n%s", stdout)
	}
}
