package usecase_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CodeMonkey80s/golens/internal/engine/goast"
	"github.com/CodeMonkey80s/golens/internal/format"
	"github.com/CodeMonkey80s/golens/internal/testfixture"
	"github.com/CodeMonkey80s/golens/internal/usecase"
)

// TestSymlinkedDirectoryIsWalked covers the failure where a symlinked directory reported
// "no .go files found".
//
// The cause was two syscalls disagreeing: os.Stat follows the link and says "directory", so
// Files proceeded to walk, and WalkDir lstats its root and reports the symlink itself as a
// non-directory. The callback treated it as a file, the name did not end in .go, and the
// walk collected nothing. The run then exited 2 -- the code this tool defines as "you named
// something that is not there" -- for a directory holding real source.
//
// Symlinked source trees are ordinary, so this is a layout a caller will actually use rather
// than a curiosity.
func TestSymlinkedDirectoryIsWalked(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "real")
	if err := os.Mkdir(real, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	testfixture.Write(t, real, "c.go", "package real\n\nfunc C() {}\n")

	link := filepath.Join(root, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	t.Run("outline finds the files", func(t *testing.T) {
		var out bytes.Buffer
		if err := usecase.Outline(goast.New(), link, usecase.DepthUnlimited, &out, format.Options{}); err != nil {
			t.Fatalf("Outline through a symlink: %v", err)
		}
		if !strings.Contains(out.String(), "func C()") {
			t.Errorf("outline through a symlink found nothing:\n%s", out.String())
		}
	})

	t.Run("paths are reported under the path the caller named", func(t *testing.T) {
		var out bytes.Buffer
		if err := usecase.Outline(goast.New(), link, usecase.DepthUnlimited, &out, format.Options{}); err != nil {
			t.Fatalf("Outline: %v", err)
		}
		// The resolved path is real/c.go, but the caller asked about link, so that is the
		// path it gets back and can open.
		if want := "file: " + filepath.Join(link, "c.go"); !strings.Contains(out.String(), want) {
			t.Errorf("outline reported %q, want it to start from the named path %q", out.String(), want)
		}
	})

	t.Run("get resolves through the link", func(t *testing.T) {
		var out bytes.Buffer
		if err := usecase.Get(goast.New(), "C", link, true, &out, format.Options{}); err != nil {
			t.Fatalf("Get through a symlink: %v", err)
		}
		if !strings.Contains(out.String(), "func C() {}") {
			t.Errorf("get through a symlink returned:\n%s", out.String())
		}
	})

	t.Run("a symlink to a file still works", func(t *testing.T) {
		fileLink := filepath.Join(root, "link.go")
		if err := os.Symlink(filepath.Join(real, "c.go"), fileLink); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		var out bytes.Buffer
		if err := usecase.Outline(goast.New(), fileLink, usecase.DepthUnlimited, &out, format.Options{}); err != nil {
			t.Fatalf("Outline through a file symlink: %v", err)
		}
		if !strings.Contains(out.String(), "func C()") {
			t.Errorf("outline through a file symlink found nothing:\n%s", out.String())
		}
	})

	t.Run("symlinks inside the tree are still not followed", func(t *testing.T) {
		// Only the root is resolved. Following links mid-walk risks cycles, and the fix
		// must not quietly change that.
		inner := filepath.Join(root, "outer")
		if err := os.Mkdir(inner, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		testfixture.Write(t, inner, "d.go", "package outer\n\nfunc D() {}\n")
		if err := os.Symlink(inner, filepath.Join(inner, "loop")); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}

		var out bytes.Buffer
		if err := usecase.Outline(goast.New(), inner, usecase.DepthUnlimited, &out, format.Options{}); err != nil {
			t.Fatalf("Outline: %v", err)
		}
		if strings.Count(out.String(), "func D()") != 1 {
			t.Errorf("a symlinked directory was followed, so the walk is no longer cycle-safe:\n%s", out.String())
		}
	})
}

// TestGetSkipsUnparseableFilesRatherThanAborting covers the second defect: get discarded
// every result because one file in the directory would not parse.
//
// outline and check both keep going in this situation, by decision D22 and acceptance
// criterion A14. get did not, so an agent working in a tree containing a half-written file
// lost the ability to read any symbol from the files beside it -- which is the moment it
// most wants to read them.
func TestGetSkipsUnparseableFilesRatherThanAborting(t *testing.T) {
	dir := t.TempDir()
	testfixture.Write(t, dir, "ok.go", "package p\n\nfunc A() {}\n")
	testfixture.Write(t, dir, "broken.go", "package p\n\nfunc B( {\n")

	t.Run("the symbol is returned and the skip is reported", func(t *testing.T) {
		var out bytes.Buffer
		err := usecase.Get(goast.New(), "A", dir, true, &out, format.Options{})

		if !strings.Contains(out.String(), "func A() {}") {
			t.Errorf("the symbol in the readable file was discarded:\n%s", out.String())
		}

		var skipped usecase.SkippedFilesError
		if !errors.As(err, &skipped) {
			t.Fatalf("err = %v (%T), want SkippedFilesError so the run still reports exit 4", err, err)
		}
		if len(skipped.Failures) != 1 || skipped.Total != 2 {
			t.Errorf("skipped %d of %d, want 1 of 2", len(skipped.Failures), skipped.Total)
		}
	})

	t.Run("a miss names the files that might declare it", func(t *testing.T) {
		// "not found" from a partial search is a confident answer that may be wrong. The
		// declaration could be sitting in the file that would not parse, and an agent told
		// only "not found" would go and rename or delete a symbol that exists.
		var out bytes.Buffer
		err := usecase.Get(goast.New(), "Nope", dir, true, &out, format.Options{})

		var miss usecase.NotFoundSymbolWithSkips
		if !errors.As(err, &miss) {
			t.Fatalf("err = %v (%T), want NotFoundSymbolWithSkips", err, err)
		}
		if !strings.Contains(err.Error(), "could not be parsed") {
			t.Errorf("the miss does not mention the skipped file: %v", err)
		}
		if out.Len() != 0 {
			t.Errorf("a miss wrote to stdout:\n%s", out.String())
		}
	})

	t.Run("ambiguity says the candidate list may be incomplete", func(t *testing.T) {
		testfixture.Write(t, dir, "other.go", "package p\n\nfunc A() {}\n")

		var out bytes.Buffer
		err := usecase.Get(goast.New(), "A", dir, true, &out, format.Options{})

		var amb usecase.AmbiguousSymbol
		if !errors.As(err, &amb) {
			t.Fatalf("err = %v (%T), want AmbiguousSymbol", err, err)
		}
		if amb.Skipped == 0 {
			t.Error("ambiguity reported 0 skipped files, but broken.go could not be parsed")
		}
		if !strings.Contains(err.Error(), "could not be parsed") {
			t.Errorf("the ambiguity does not say the list may be incomplete: %v", err)
		}
	})

	t.Run("a single broken file is still just the parse error", func(t *testing.T) {
		// Nothing partial to salvage, and "1 of 1 files" says less than the diagnostic.
		var out bytes.Buffer
		err := usecase.Get(goast.New(), "A", filepath.Join(dir, "broken.go"), true, &out, format.Options{})
		if err == nil {
			t.Fatal("expected the parse error")
		}
		var skipped usecase.SkippedFilesError
		if errors.As(err, &skipped) {
			t.Errorf("a single file produced a summary instead of the diagnostic: %v", err)
		}
		if !strings.Contains(err.Error(), "broken.go") {
			t.Errorf("the parse error does not name the file: %v", err)
		}
	})

	t.Run("bounds on one readable file is unaffected", func(t *testing.T) {
		var out bytes.Buffer
		if err := usecase.Bounds(goast.New(), "A", filepath.Join(dir, "ok.go"), &out, format.Options{}); err != nil {
			t.Fatalf("Bounds: %v", err)
		}
	})
}

// TestNotFoundNamesSkipsOnlyWhenItMust guards the opposite direction: with every file
// parsed, "not found" is the whole truth, and adding a caveat would be noise the caller has
// to read past.
func TestNotFoundNamesSkipsOnlyWhenItMust(t *testing.T) {
	dir := t.TempDir()
	testfixture.Write(t, dir, "ok.go", "package p\n\nfunc A() {}\n")

	var out bytes.Buffer
	err := usecase.Get(goast.New(), "Nope", dir, true, &out, format.Options{})

	var miss usecase.NotFoundSymbolWithSkips
	if errors.As(err, &miss) {
		t.Errorf("a clean search reported skipped files: %v", err)
	}
	if err.Error() != `symbol "Nope" not found` {
		t.Errorf("err = %q, want the plain not-found message", err.Error())
	}
}
