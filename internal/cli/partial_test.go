package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSymlinkedDirectoryIsNotEmpty is the user-visible half of the symlink defect.
//
// The usecase test proves Files walks a symlinked directory. This proves what the command
// prints and, more importantly, what it returns: the bug exited 2, which this tool defines
// as "you named something that is not there", so an agent was told a directory full of
// source did not exist.
func TestSymlinkedDirectoryIsNotEmpty(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "real")
	if err := os.Mkdir(real, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(real, "c.go"), []byte("package real\n\nfunc C() {}\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	link := filepath.Join(root, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	stdout, _, code := run(t, "outline", link)
	if code != 0 {
		t.Errorf("outline of a symlinked directory exited %d, want 0\n%s", code, stdout)
	}
	if !strings.Contains(stdout, "func C()") {
		t.Errorf("outline found nothing through the symlink:\n%s", stdout)
	}

	// The reported path must start from what the caller named. A resolved path belongs to
	// a tree the caller never asked about, so the offsets in the output would not line up
	// with the directory they are standing in.
	if !strings.Contains(stdout, filepath.Join(link, "c.go")) {
		t.Errorf("outline reported a resolved path rather than the one named:\n%s", stdout)
	}

	stdout, _, code = run(t, "get", "C", link)
	if code != 0 || !strings.Contains(stdout, "func C() {}") {
		t.Errorf("get through a symlink exited %d:\n%s", code, stdout)
	}
}

// TestGetSkipsUnparseableFiles pins the exit codes, which the usecase test cannot see: the
// layer that matters is the one an agent branches on.
func TestGetSkipsUnparseableFiles(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	write("ok.go", "package p\n\nfunc A() {}\n")
	write("broken.go", "package p\n\nfunc B( {\n")

	t.Run("a found symbol is written and the run still exits 4", func(t *testing.T) {
		stdout, _, code := run(t, "get", "A", dir)
		if !strings.Contains(stdout, "func A() {}") {
			t.Errorf("the readable file was discarded:\n%s", stdout)
		}
		// Exit 0 would tell the caller the directory was fully readable. It was not, and
		// the answer may be incomplete in a way the caller cannot see from stdout alone.
		if code != 4 {
			t.Errorf("exit = %d, want 4 for an incomplete search", code)
		}
	})

	t.Run("a miss still exits 2 and leaves stdout empty", func(t *testing.T) {
		stdout, stderr, code := run(t, "get", "Nope", dir)
		if code != 2 {
			t.Errorf("exit = %d, want 2", code)
		}
		if stdout != "" {
			t.Errorf("a miss wrote to stdout:\n%s", stdout)
		}
		// The miss must say the search was incomplete, or it reads as a confident "this
		// symbol does not exist" when it may be sitting in the file that would not parse.
		if !strings.Contains(stderr, "could not be parsed") {
			t.Errorf("stderr does not mention the unreadable file:\n%s", stderr)
		}
	})

	t.Run("a single broken file is still just the parse error", func(t *testing.T) {
		_, stderr, code := run(t, "get", "A", filepath.Join(dir, "broken.go"))
		if code != 4 {
			t.Errorf("exit = %d, want 4", code)
		}
		if strings.Contains(stderr, "1 of 1 files") {
			t.Errorf("a single file produced a summary instead of the diagnostic:\n%s", stderr)
		}
	})

	t.Run("bounds on a readable file is unaffected", func(t *testing.T) {
		stdout, _, code := run(t, "bounds", "A", filepath.Join(dir, "ok.go"))
		if code != 0 || !strings.Contains(stdout, "bytes") {
			t.Errorf("bounds exited %d:\n%s", code, stdout)
		}
	})
}
