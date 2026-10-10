package usecase_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/CodeMonkey80s/golens/internal/engine/goast"
	"github.com/CodeMonkey80s/golens/internal/format"
	"github.com/CodeMonkey80s/golens/internal/testfixture"
	"github.com/CodeMonkey80s/golens/internal/usecase"
)

// TestInvalidImportPathDetection covers the import-path syntax check from specification 2.4.
//
// This check is easy to assume unreachable: go/parser might be thought to reject a malformed path
// before the document is built. It does not -- `import ""` and `import "foo bar"` both parse
// successfully, and go/parser reports them only when the module graph is resolved, which this
// tool deliberately avoids. So the check does real work, and was previously untested.
func TestInvalidImportPathDetection(t *testing.T) {
	tests := []struct {
		name      string
		src       string
		wantDiag  string
		wantClean bool
	}{
		{
			name:     "empty path",
			src:      "package p\n\nimport \"\"\n\nfunc F() {}\n",
			wantDiag: "empty import path",
		},
		{
			name:     "path with a space",
			src:      "package p\n\nimport \"foo bar\"\n\nfunc F() {}\n",
			wantDiag: "contains whitespace",
		},
		{
			// A real tab is written \t in the source and decoded before this check sees it, so
			// the message names the escape rather than whitespace.
			name:     "path with an escaped tab",
			src:      "package p\n\nimport \"foo\\tbar\"\n\nfunc F() {}\n",
			wantDiag: "contains an escape sequence",
		},
		{
			// A newline cannot appear unescaped inside a raw import literal without breaking
			// the syntax, so this case is expressed through a string containing a newline.
			name:     "empty path among valid ones",
			src:      "package p\n\nimport (\n\t\"\"\n\t\"fmt\"\n)\n\nfunc F() { fmt.Println() }\n",
			wantDiag: "empty import path",
		},
		{
			name:      "ordinary path is clean",
			src:       "package p\n\nimport \"fmt\"\n\nfunc F() { fmt.Println() }\n",
			wantClean: true,
		},
		{
			name:      "path with a slash and digits is clean",
			src:       "package p\n\nimport \"net/http\"\n\nfunc F() { _ = http.MethodGet }\n",
			wantClean: true,
		},
		{
			// Dots and dashes are legal in a module path, so a check that rejected them would
			// produce false positives on ordinary code.
			name:      "path with dots and dashes is clean",
			src:       "package p\n\nimport \"github.com/go-foo/bar.baz\"\n\nfunc F() { _ = bar.Baz }\n",
			wantClean: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := runCheck(t, t.TempDir(), "p.go", tt.src)

			if tt.wantClean {
				for _, d := range res.diags {
					if strings.Contains(d, "import path") {
						t.Errorf("unexpected import-path diagnostic: %q", d)
					}
				}
				return
			}
			if !res.contains(tt.wantDiag) {
				t.Errorf("missing %q; diagnostics: %v", tt.wantDiag, res.diags)
			}
		})
	}
}

// TestInvalidImportPathReportsOnlyOnce guards against a diagnostic storm: one bad path produces
// one import-path diagnostic, not one per analysis that walks the import list.
func TestInvalidImportPathReportsOnlyOnce(t *testing.T) {
	res := runCheck(t, t.TempDir(), "p.go", "package p\n\nimport \"\"\n\nfunc F() {}\n")

	count := 0
	for _, d := range res.diags {
		if strings.Contains(d, "empty import path") {
			count++
		}
	}
	if count != 1 {
		t.Errorf("empty-path diagnostic appeared %d times, want 1: %v", count, res.diags)
	}
}

// TestOutlineRendersEmptyImportPath confirms the outline does not crash on the same input, since
// the local name of an empty path is also empty.
func TestOutlineRendersEmptyImportPath(t *testing.T) {
	dir := t.TempDir()
	path := testfixture.Write(t, dir, "p.go", "package p\n\nimport \"\"\n\nfunc F() {}\n")

	var buf bytes.Buffer
	if err := usecase.Outline(goast.New(), path, usecase.DepthUnlimited, &buf, format.Options{}); err != nil {
		t.Fatalf("Outline: %v", err)
	}
	if !strings.Contains(buf.String(), "IMPORTS") {
		t.Errorf("expected an import section:\n%s", buf.String())
	}
}

// TestCheckOnWholeRepositoryWithBadImports confirms a malformed import does not stop the run.
func TestCheckOnWholeRepositoryWithBadImports(t *testing.T) {
	dir := t.TempDir()
	testfixture.Write(t, dir, "a_bad.go", "package p\n\nimport \"\"\n\nfunc A() {}\n")
	testfixture.Write(t, dir, "b_unused.go", "package p\n\nimport \"fmt\"\n\nfunc B() {}\n")
	testfixture.Write(t, dir, "c_clean.go", "package p\n\nimport \"os\"\n\nfunc C() { _ = os.Args }\n")

	var stdout, stderr bytes.Buffer
	err := usecase.Check(goast.New(), dir, &stdout, &stderr, format.Options{})

	if err == nil {
		t.Error("expected diagnostics")
	}
	got := stderr.String()
	if !strings.Contains(got, "a_bad.go") {
		t.Errorf("the malformed import was not reported:\n%s", got)
	}
	if !strings.Contains(got, "b_unused.go") {
		t.Errorf("the run stopped before the next file:\n%s", got)
	}
	if strings.Contains(got, "c_clean.go") {
		t.Errorf("a clean file was reported:\n%s", got)
	}
}
