package usecase

import "fmt"

// errNotGoFile reports a non-Go file passed where source was expected.
type errNotGoFile string

func (e errNotGoFile) Error() string {
	return fmt.Sprintf("%s is not a Go source file", string(e))
}

// IsUsage marks the error as a usage mistake, so the CLI exits 1 rather than 4.
func (e errNotGoFile) IsUsage() bool { return true }

// errDirectoryNotAccepted reports a directory passed to a command that takes a file only.
type errDirectoryNotAccepted string

func (e errDirectoryNotAccepted) Error() string {
	return fmt.Sprintf("%s is a directory; this command accepts a single file", string(e))
}

// IsUsage marks the error as a usage mistake, so the CLI exits 1.
func (e errDirectoryNotAccepted) IsUsage() bool { return true }

// errNoGoFiles reports a directory containing no Go sources.
type errNoGoFiles string

func (e errNoGoFiles) Error() string {
	return fmt.Sprintf("no .go files found in %s", string(e))
}

// IsNotFound marks the error as a not-found condition, so the CLI exits 2.
func (e errNoGoFiles) IsNotFound() bool { return true }

// AmbiguousSymbol reports a name that resolved to more than one declaration.
//
// Carrying the candidates in the error keeps the exit-2 path honest: the CLI prints them
// rather than silently picking one.
type AmbiguousSymbol struct {
	Name    string
	Matches []string
}

func (e AmbiguousSymbol) Error() string {
	return fmt.Sprintf("%s is ambiguous, matches: %v", e.Name, e.Matches)
}

// IsNotFound marks ambiguity under exit 2, the same code as a missing symbol, since both
// mean "the caller must disambiguate or correct the name".
func (e AmbiguousSymbol) IsNotFound() bool { return true }

// Candidates returns the qualified names that matched. The cli layer prints these beneath
// the error so the caller can correct the symbol instead of guessing.
func (e AmbiguousSymbol) Candidates() []string { return e.Matches }

// NotFoundSymbol reports a name that matched nothing.
type NotFoundSymbol string

func (e NotFoundSymbol) Error() string {
	return fmt.Sprintf("symbol %q not found", string(e))
}

// IsNotFound marks the error for exit 2.
func (e NotFoundSymbol) IsNotFound() bool { return true }

// NotFoundFile reports a path that does not exist or cannot be inspected.
//
// This shares exit 2 with NotFoundSymbol because both mean the caller named something
// that is not there, and an agent reacting to 2 should correct its input rather than
// retry. A file that exists but fails to parse is a different condition and exits 4.
type NotFoundFile string

func (e NotFoundFile) Error() string {
	return fmt.Sprintf("no such file or directory: %s", string(e))
}

// IsNotFound marks the error for exit 2.
func (e NotFoundFile) IsNotFound() bool { return true }
