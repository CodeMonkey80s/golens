package cli_test

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// versionSymbol is the linker -X target for the release identifier. It has to be the full
// import path of the variable, not the package name, so a wrong path fails the build rather
// than silently leaving the default in place.
const versionSymbol = "github.com/CodeMonkey80s/golens/internal/cli.Version"

// TestVersionIsInjectedByLdflags pins the release mechanism: the tag decides the number and
// -ldflags carries it into the binary.
//
// It is a subprocess test because that is the only way to observe what the linker did. An
// in-process assertion would keep passing if the injection silently stopped working, which is
// the exact failure a constant in the source would have produced — and which the previous
// version of this code did have, reporting 0.1.0 to every build forever.
func TestVersionIsInjectedByLdflags(t *testing.T) {
	root := repoRoot(t)
	const injected = "v9.9.9-injected"

	bin := buildBinaryWith(t, root, "-X "+versionSymbol+"="+injected)
	got := strings.TrimSpace(runBinary(t, bin, root, "--version"))

	if want := "golens " + injected; got != want {
		t.Errorf("--version = %q, want %q\n\nThe -X symbol is wrong, or Version is no longer a variable\ninitialised to a constant string expression. The linker ignores -X on anything else,\nand it does so without failing the build.", got, want)
	}
}

// buildBinaryWith compiles the CLI with the given extra -ldflags value, or with none when it
// is empty.
func buildBinaryWith(t *testing.T, root, ldflags string) string {
	t.Helper()

	bin := filepath.Join(t.TempDir(), "golens")
	args := []string{"build", "-o", bin}
	if ldflags != "" {
		args = append(args, "-ldflags", ldflags)
	}
	args = append(args, "./cmd/golens")

	cmd := exec.Command("go", args...)
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build %v: %v\n%s", args, err, out)
	}
	return bin
}
