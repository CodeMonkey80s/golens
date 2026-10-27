package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestCheckDifferentialAgainstGoBuild compares the check gate with the compiler on the
// cases both are meant to agree about. It is deliberately bidirectional: a case that builds
// must be clean under check, and a case that fails to build must produce diagnostics.
//
// This cannot cover everything check models (it runs one GOOS per case), but it catches the
// failure that matters most: a clean tree reported as broken, or a broken tree reported clean.
func TestCheckDifferentialAgainstGoBuild(t *testing.T) {
	cases := []struct {
		name      string
		files     map[string]string
		buildTags string
		buildOK   bool
		checkTags string
		wantCheck int
	}{
		{
			name:      "clean",
			files:     map[string]string{"a.go": "package p\n\nfunc F() {}\n"},
			buildOK:   true,
			wantCheck: 0,
		},
		{
			name:      "unused import",
			files:     map[string]string{"a.go": "package p\n\nimport \"fmt\"\n\nfunc F() {}\n"},
			buildOK:   false,
			wantCheck: 3,
		},
		{
			name: "redeclaration",
			files: map[string]string{
				"a.go": "package p\n\nfunc F() {}\n",
				"b.go": "package p\n\nfunc F() {}\n",
			},
			buildOK:   false,
			wantCheck: 3,
		},
		{
			name:      "malformed import path",
			files:     map[string]string{"a.go": "package p\n\nimport \"bad path\"\n\nfunc F() {}\n"},
			buildOK:   false,
			wantCheck: 3,
		},
		{
			name: "platform variants",
			files: map[string]string{
				"a_linux.go":   "package p\n\nfunc F() {}\n",
				"a_windows.go": "package p\n\nfunc F() {}\n",
			},
			buildOK:   true,
			wantCheck: 0,
		},
		{
			name: "ignored file",
			files: map[string]string{
				"a.go":        "package p\n\nfunc F() {}\n",
				"_ignored.go": "package p\n\nimport \"fmt\"\n\nfunc G() {}\n",
			},
			buildOK:   true,
			wantCheck: 0,
		},
		{
			name: "testdata fixture",
			files: map[string]string{
				"a.go":               "package p\n\nfunc F() {}\n",
				"testdata/broken.go": "package p\n\nfunc B( {\n",
			},
			buildOK:   true,
			wantCheck: 0,
		},
		{
			name: "custom tag clean by default",
			files: map[string]string{
				"a_integration.go": "//go:build integration\n\npackage p\n\nfunc F() {}\n",
				"b.go":             "package p\n\nfunc G() {}\n",
			},
			buildOK:   true,
			wantCheck: 0,
		},
		{
			name: "custom tag conflict",
			files: map[string]string{
				"a_integration.go": "//go:build integration\n\npackage p\n\nfunc F() {}\n",
				"b.go":             "package p\n\nfunc F() {}\n",
			},
			buildTags: "integration",
			buildOK:   false,
			checkTags: "integration",
			wantCheck: 3,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := writeCheckModule(t, tc.files)

			buildErr := goBuild(t, dir, tc.buildTags)
			if tc.buildOK && buildErr != nil {
				t.Fatalf("go build: %v", buildErr)
			}
			if !tc.buildOK && buildErr == nil {
				t.Fatal("go build succeeded; the case is not testing what it claims")
			}

			args := []string{"check", dir}
			if tc.checkTags != "" {
				args = append(args, "--tags", tc.checkTags)
			}
			_, stderr, code := run(t, args...)
			if code != tc.wantCheck {
				t.Fatalf("check exit = %d, want %d\n%s", code, tc.wantCheck, stderr)
			}
			if tc.wantCheck == 3 && !strings.Contains(stderr, ":") {
				t.Errorf("diagnostics have no file:line:column prefix:\n%s", stderr)
			}
		})
	}
}

func writeCheckModule(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module checkdiff\n\ngo 1.24\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for name, src := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func goBuild(t *testing.T, dir, tags string) error {
	t.Helper()
	args := []string{"build"}
	if tags != "" {
		args = append(args, "-tags", tags)
	}
	args = append(args, "./...")
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return &buildError{err: err, output: string(out)}
	}
	return nil
}

type buildError struct {
	err    error
	output string
}

func (e *buildError) Error() string { return e.err.Error() + "\n" + e.output }
