// Package usecase holds the orchestration layer: it wires a domain.Engine to the format
// package and implements the behaviour of each command.
//
// It depends on domain and format but never on cobra, and it never touches go/ast. That
// one-way dependency is what keeps a future Tree-Sitter engine from requiring changes
// here.
package usecase

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/CodeMonkey80s/golens/internal/domain"
	"github.com/CodeMonkey80s/golens/internal/format"
)

// DepthUnlimited means no recursion limit is applied to the outline.
const DepthUnlimited = -1

// SkippedDirs are never descended into during directory traversal.
var skippedDirs = []string{".git", "vendor", "node_modules"}

// Files collects the .go files targeted by an outline invocation.
//
// A file path yields just that file. A directory yields every .go file beneath it, in
// lexical order, with skipped and hidden directories pruned. Sorting is explicit rather
// than relying on ReadDir alone so the ordering guarantee is visible at the call site:
// determinism is what lets a consumer diff or cache the output.
//
// This walk is shared by outline, get and check; checkFiles is the one exception, and its
// comment says why pruning testdata there is not a scope decision made invisibly.
func Files(target string) ([]string, error) {
	return files(target, false)
}

// checkFiles collects the files a check invocation analyses.
//
// The walk is Files' walk minus one directory name: testdata. The go tool ignores it by
// documented rule -- "Directory and file names that begin with '.' or '_' are ignored by the
// go tool, as are directories named 'testdata'" -- so no build ever compiles anything in
// one. check's contract, specification 2.4, is *"reports what go build would reject, without
// building"*, and reporting a fixture no build reaches is a claim about code that is never
// compiled.
//
// The cost of getting this wrong is not cosmetic. On $GOROOT/src it was 1,212 diagnostics,
// every one of them inside a testdata directory, and exit 3 -- which an agent reads as "the
// code has problems, go and fix them". Compiler fixtures are written to be broken, so the
// remedy it points at is to edit source whose exact bytes are the test case.
//
// D22 declined to prune testdata for outline, on the grounds that an agent asking what a
// directory contains wants all of it. That reasoning holds there and does not transfer:
// outline is asked what the tree holds, check is asked what a build would reject, and a
// directory the toolchain skips is inside the first question and outside the second.
//
// A testdata file named explicitly is still checked -- files() returns a file target
// without walking, so "golens check testdata/store.go" keeps working exactly as before.
func checkFiles(target string) ([]string, error) {
	paths, err := files(target, true)
	if err != nil {
		return nil, err
	}
	// A directory holding nothing but testdata is not "a directory with no Go sources":
	// there are .go files in it, and they are all in a directory the go tool skips. Saying
	// "no .go files found" tells a caller it named something that is not there, which is
	// what exit 2 means, when the directory is right there. Name the reason instead.
	if len(paths) == 0 && onlyPrunedFiles(target) {
		return nil, errNoCheckableFiles(target)
	}
	return paths, nil
}

// onlyPrunedFiles reports whether the unpruned walk would have found .go files where the
// pruned one found none.
//
// It runs a second walk, which is only reached when the first collected nothing -- the
// directory is empty, or every file in it sits under testdata.
func onlyPrunedFiles(target string) bool {
	info, err := os.Stat(target)
	if err != nil || !info.IsDir() {
		return false
	}
	found, err := files(target, false)
	return err == nil && len(found) > 0
}

// files is the shared walk. pruneTestdata adds testdata to the pruned names, which is what
// separates check's file set from outline's; the flag exists so the two walks cannot drift
// apart in any other respect.
func files(target string, pruneTestdata bool) ([]string, error) {
	info, err := os.Stat(target)
	if err != nil {
		// A missing or unreadable path is "not found" under the documented contract, not
		// an I/O failure, so it exits 2 rather than 4.
		return nil, NotFoundFile(target)
	}

	if !info.IsDir() {
		if !strings.HasSuffix(target, ".go") {
			return nil, errNotGoFile(target)
		}
		return []string{target}, nil
	}

	// A symlinked directory has to be resolved before it is walked.
	//
	// os.Stat above follows the link and reports a directory, but WalkDir lstats its root:
	// the callback receives the symlink itself as a non-directory, the name does not end in
	// .go, and the walk collects nothing. The run then reports "no .go files found" and
	// exits 2 — the "you named something that is not there" code — for a directory holding
	// hundreds of files. Symlinked source trees are ordinary (worktrees, $GOPATH/src, editor
	// setups), so this is not an exotic layout; a tool for agents must not tell one it is
	// looking at an empty directory when it is not.
	//
	// Only the root is resolved. Symlinks *within* the tree are still not followed, which
	// is loop protection and stays as it is.
	root := target
	if resolved, err := filepath.EvalSymlinks(target); err == nil {
		root = resolved
	}

	var found []string
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// A directory that cannot be read should not abort the whole traversal; the
			// files already collected are still valid output.
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}

		if d.IsDir() {
			if path != root && skipDir(d.Name(), pruneTestdata) {
				return fs.SkipDir
			}
			return nil
		}

		if strings.HasSuffix(d.Name(), ".go") && !strings.HasPrefix(d.Name(), ".") {
			found = append(found, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Strings(found)

	// Paths are reported under the directory the caller named, not the resolved one, so a
	// caller that reads a path out of the output can open it from the tree it asked about.
	if root != target {
		for i, path := range found {
			found[i] = filepath.Join(target, strings.TrimPrefix(path, root))
		}
	}
	return found, nil
}

// skipDir reports whether a directory is pruned from a walk. pruneTestdata adds the one
// name the go tool ignores that is not already pruned here.
func skipDir(name string, pruneTestdata bool) bool {
	if strings.HasPrefix(name, ".") {
		return true
	}
	if pruneTestdata && name == "testdata" {
		return true
	}
	return slices.Contains(skippedDirs, name)
}

// statFile validates that target is a single Go file, returning it unchanged.
//
// bounds accepts a file only; a directory is a usage error rather than a silent
// cross-file search, which keeps its contract narrower than get's.
func statFile(target string) (string, error) {
	info, err := os.Stat(target)
	if err != nil {
		return "", NotFoundFile(target)
	}
	if info.IsDir() {
		return "", errDirectoryNotAccepted(target)
	}
	if !strings.HasSuffix(target, ".go") {
		return "", errNotGoFile(target)
	}
	return target, nil
}

// Outline renders the outline of every file in target.
func Outline(engine domain.Engine, target string, depth int, w io.Writer, opts format.Options) error {
	paths, err := Files(target)
	if err != nil {
		return err
	}
	if len(paths) == 0 {
		return errNoGoFiles(target)
	}

	// Parsing is concurrent for directories and sequential for a single file; see parse.go for
	// why. Either way the documents come back in path order, so the output below is identical
	// either way.
	docs, errs := parseAll(engine, paths)

	// A file that does not parse contributes nothing to the outline, but it must not hide every
	// other file in the tree. The failure is still reported, and still exits 4; it is just
	// reported after the output rather than instead of it.
	parsed := make([]domain.Document, 0, len(docs))
	var skipped []ParseError
	for i, doc := range docs {
		if errs[i].Err != nil {
			skipped = append(skipped, errs[i])
			continue
		}
		parsed = append(parsed, doc)
	}

	if opts.JSON {
		payloads := make([]format.OutlineFile, 0, len(parsed))
		for _, doc := range parsed {
			payloads = append(payloads, format.Outline(doc, normalizeDepth(depth)))
		}
		if err := format.WriteJSON(w, payloads, opts.Compact); err != nil {
			return err
		}
	} else {
		for _, doc := range parsed {
			if err := format.WriteOutlineHuman(w, doc, normalizeDepth(depth), opts.Compact); err != nil {
				return err
			}
		}
	}

	if len(skipped) > 0 {
		// A single file has nothing partial to salvage, so the original message stands: the
		// failure is the whole answer and "1 of 1 files" says less than the diagnostic does.
		if len(paths) == 1 {
			return skipped[0]
		}
		return SkippedFilesError{Failures: skipped, Total: len(paths)}
	}
	return nil
}

// normalizeDepth converts the unlimited sentinel to -1 and clamps a zero depth to 1, so
// callers never have to special-case the boundary between "no limit" and "top level".
func normalizeDepth(depth int) int {
	if depth == DepthUnlimited {
		return -1
	}
	if depth < 1 {
		return 1
	}
	return depth
}
