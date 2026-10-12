package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCheckAndOutlineAgreeOnAnIOFailure covers check reporting an unreadable file as exit 3
// while outline reported the same file as exit 4.
//
// The documented codes are 3 for "check found diagnostics" and 4 for "parse or I/O failure", so
// an I/O failure has exactly one right answer and the two commands were giving different ones.
// The exit code is what an agent branches on: 3 says the code has problems and invites an edit,
// 4 says the tool could not do its job and invites chasing a stale link or a permission bit.
func TestCheckAndOutlineAgreeOnAnIOFailure(t *testing.T) {
	if _, err := os.Lstat("/nonexistent-target-for-tests"); err == nil {
		t.Skip("unexpected target exists")
	}
	dir := t.TempDir()
	writeFile(t, dir, "ok.go", "package p\n\nfunc A() {}\n")
	if err := os.Symlink("/nonexistent-target-for-tests", filepath.Join(dir, "dangling.go")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	_, outlineCode := runExitCode(t, "outline", dir)
	_, checkCode := runExitCode(t, "check", dir)

	if checkCode != 4 {
		t.Errorf("check exited %d, want 4 for an I/O failure", checkCode)
	}
	if outlineCode != checkCode {
		t.Errorf("outline exited %d but check exited %d; the two must agree on an I/O failure",
			outlineCode, checkCode)
	}
}

// TestCheckKeepsExitThreeForFindingsAboutCode guards the other direction.
//
// The split is between a file that could not be read and a file that was read and found wanting.
// Collapsing the second into the first would be the worse error: a genuine syntax error or unused
// import reported as exit 4 tells the caller the tool failed when the code is at fault.
func TestCheckKeepsExitThreeForFindingsAboutCode(t *testing.T) {
	tests := []struct {
		name    string
		files   map[string]string
		wantSub string
	}{
		{
			name:    "syntax error",
			files:   map[string]string{"syn.go": "package p\n\nfunc B( {\n"},
			wantSub: "expected ')'",
		},
		{
			name:    "unused import",
			files:   map[string]string{"u.go": "package p\n\nimport \"fmt\"\n\nfunc A() {}\n"},
			wantSub: "imported and not used",
		},
		{
			name: "duplicate declaration",
			files: map[string]string{
				"a.go": "package p\n\nfunc d() {}\n",
				"b.go": "package p\n\nfunc d() {}\n",
			},
			wantSub: "redeclared",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			for name, content := range tc.files {
				writeFile(t, dir, name, content)
			}

			_, stderr, code := run(t, "check", dir)
			if code != 3 {
				t.Errorf("exit = %d, want 3 for a finding about the code\n%s", code, stderr)
			}
			if !strings.Contains(stderr, tc.wantSub) {
				t.Errorf("stderr lost the finding %q:\n%s", tc.wantSub, stderr)
			}
		})
	}
}

// TestCheckExitsFourWhenBothKindsOccur pins the precedence.
//
// Both a syntax error and an unreadable file can be present at once. Exit 4 dominates, because
// the caller must resolve the I/O failure before the tree is fully read — the same reasoning as
// `get` skipping files. Exit 3 would assert every file was readable, and one was not. Both texts
// still reach stderr: suppressing one to justify a code would trade one loss for another.
func TestCheckExitsFourWhenBothKindsOccur(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "ok.go", "package p\n\nfunc A() {}\n")
	writeFile(t, dir, "syn.go", "package p\n\nfunc B( {\n")
	if err := os.Symlink("/nonexistent-target-for-tests", filepath.Join(dir, "dangling.go")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	stdout, stderr, code := run(t, "check", dir)
	if code != 4 {
		t.Errorf("exit = %d, want 4; an unreadable file outranks a syntax error", code)
	}
	if !strings.Contains(stderr, "expected ')'") {
		t.Errorf("the syntax error was suppressed by the exit-4 path:\n%s", stderr)
	}
	if !strings.Contains(stderr, "dangling.go") {
		t.Errorf("the unreadable file was suppressed by the exit-4 path:\n%s", stderr)
	}
	if stdout != "" {
		t.Errorf("the human form must leave stdout empty:\n%s", stdout)
	}
}

// TestCheckJSONCarriesTheUnreadableFile guards against fixing the exit code by dropping the
// finding. A --json consumer is told not to add 2>&1, so a payload that omits the unreadable
// file leaves it with exit 4 and nothing to explain it.
func TestCheckJSONCarriesTheUnreadableFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "ok.go", "package p\n\nfunc A() {}\n")
	if err := os.Symlink("/nonexistent-target-for-tests", filepath.Join(dir, "dangling.go")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	stdout, stderr, code := run(t, "check", dir, "--json")
	if code != 4 {
		t.Errorf("exit = %d, want 4", code)
	}
	if stderr != "" {
		t.Errorf("check --json must leave stderr empty:\n%s", stderr)
	}
	if !strings.Contains(stdout, "dangling.go") {
		t.Errorf("the unreadable file is missing from the payload:\n%s", stdout)
	}
}

// TestUnreadableFileIsNotReportedTwice guards the wording.
//
// Returning the full skip summary after the per-file diagnostic printed the same line twice on
// stderr, which reads as two failures where there was one. The diagnostic carries the cause; the
// error carries the count.
func TestUnreadableFileIsNotReportedTwice(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "ok.go", "package p\n\nfunc A() {}\n")
	if err := os.Symlink("/nonexistent-target-for-tests", filepath.Join(dir, "dangling.go")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	_, stderr, _ := run(t, "check", dir)

	if n := strings.Count(stderr, "no such file or directory"); n != 1 {
		t.Errorf("the unreadable file's cause appears %d times, want 1:\n%s", n, stderr)
	}

	// The diagnostic names the file; the summary must not, or stderr reads as two failures
	// where there was one. The path legitimately appears more than once inside the single
	// diagnostic line -- the file field and the underlying os error both carry it -- so the
	// assertion is about which lines report it, not how often the path is spelled.
	var reporting []string
	for line := range strings.SplitSeq(strings.TrimSpace(stderr), "\n") {
		if strings.Contains(line, "dangling.go") && !strings.HasPrefix(line, "golens:") {
			reporting = append(reporting, line)
		}
	}
	if len(reporting) != 1 {
		t.Errorf("%d non-summary lines report the unreadable file, want 1:\n%s", len(reporting), stderr)
	}

	summary := ""
	for line := range strings.SplitSeq(strings.TrimSpace(stderr), "\n") {
		if strings.HasPrefix(line, "golens:") {
			summary = line
		}
	}
	if strings.Contains(summary, "dangling.go") {
		t.Errorf("the summary re-names a file the diagnostic already reported: %q", summary)
	}
	if summary == "" {
		t.Errorf("the human form should still carry the count:\n%s", stderr)
	}
}

// runExitCode runs the command and discards its output, for tests that only care about the code.
func runExitCode(t *testing.T, args ...string) (stdout string, code int) {
	t.Helper()
	out, _, code := run(t, args...)
	return out, code
}
