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
func Files(target string) ([]string, error) {
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

	var found []string
	err = filepath.WalkDir(target, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// A directory that cannot be read should not abort the whole traversal; the
			// files already collected are still valid output.
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}

		if d.IsDir() {
			if path != target && skipDir(d.Name()) {
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
	return found, nil
}

func skipDir(name string) bool {
	if strings.HasPrefix(name, ".") {
		return true
	}
	for _, skip := range skippedDirs {
		if name == skip {
			return true
		}
	}
	return false
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
