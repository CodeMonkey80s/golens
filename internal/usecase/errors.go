package usecase

import (
	"fmt"
	"strings"
)

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
	// Skipped counts files in the search that could not be parsed, and Total how many were
	// searched. Both are zero for the ordinary single-file case.
	//
	// The candidate list is incomplete whenever Skipped is non-zero: a file that would not
	// parse may declare the same name again. Without saying so, a caller picking the first
	// candidate to edit would be picking from a list that is known to have a gap in it.
	Skipped int
	Total   int
}

func (e AmbiguousSymbol) Error() string {
	msg := fmt.Sprintf("%s is ambiguous, matches: %v", e.Name, e.Matches)
	if e.Skipped > 0 {
		msg += fmt.Sprintf("; %d of %d files could not be parsed and may declare it too", e.Skipped, e.Total)
	}
	return msg
}

// Advice returns the line to print beneath the candidate list.
//
// The error names the problem but not the remedy, and the place a caller meets it is the error
// itself rather than the help text. Without the remedy the likely response is to retry the same
// command, or to conclude the tool is broken. Duf declares Mount.Stat in four platform files,
// so this is the common case rather than an edge one.
func (e AmbiguousSymbol) Advice() string {
	return "  pass a specific file instead of a directory, or the qualified name, to disambiguate"
}

// IsNotFound marks ambiguity under exit 2, the same code as a missing symbol, since both
// mean "the caller must disambiguate or correct the name".
func (e AmbiguousSymbol) IsNotFound() bool { return true }

// Candidates returns the qualified names that matched. The cli layer prints these beneath
// the error so the caller can correct the symbol instead of guessing.
func (e AmbiguousSymbol) Candidates() []string { return e.Matches }

// NotFoundSymbolWithSkips reports a symbol that resolved nowhere while some of the files
// searched could not be parsed.
//
// It exists because plain NotFoundSymbol is a confident answer and this one is not. The
// declaration may sit in a file that could not be read, and an agent told only "not found"
// would go looking for a symbol to rename or a call site to fix. Saying which files were
// unreadable keeps the answer honest without changing its exit code: the caller still has to
// correct the name or disambiguate, which is what 2 means.
type NotFoundSymbolWithSkips struct {
	Name string
	// Total is how many files the search covered, so the message can say how complete it was.
	Total int
	// Files names those that could not be parsed, in path order.
	Files []ParseError
}

func (e NotFoundSymbolWithSkips) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "symbol %q not found", e.Name)
	if len(e.Files) > 0 {
		fmt.Fprintf(&b, "; %d of %d files could not be parsed and may declare it", len(e.Files), e.Total)
	}
	return b.String()
}

// IsNotFound marks it under exit 2, the same code as a plain miss: the caller must correct
// the name or disambiguate either way.
func (e NotFoundSymbolWithSkips) IsNotFound() bool { return true }

// Unwrap reaches the first parse failure, so a caller wanting the position rather than the
// summary still finds it.
func (e NotFoundSymbolWithSkips) Unwrap() error {
	if len(e.Files) == 0 {
		return nil
	}
	return e.Files[0]
}

// CrossPackageGroup is one package's declarations of a symbol found in a directory that holds
// more than one package.
type CrossPackageGroup struct {
	Package string
	// Files names the declarations, in path order.
	Files []string
}

// CrossPackageSymbol reports a name that resolved in more than one package in the same
// directory.
//
// It exists because AmbiguousSymbol is the wrong answer for this case, and the wrong answer is
// actively misleading. A directory may hold `package p` beside an external test `package p_test`,
// and both may declare `A` without either redeclaring anything — the test package is a separate
// namespace. Calling that "ambiguous" with candidate lines to choose between implies a duplicate
// declaration to go and fix, and there is none. `check` already agrees: packageSymbols keys its
// namespace on directory *and* package name, so the two commands disagreed about whether a
// conflict existed.
//
// The advice names a file rather than a qualified name. A qualified name addresses a method
// within a type ("UserStore.GetUser"); it cannot express a package, and the previous advice
// recommended one that `get p.A` does not resolve.
type CrossPackageSymbol struct {
	Name   string
	Groups []CrossPackageGroup
}

func (e CrossPackageSymbol) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "symbol %q is declared in %d packages, not ambiguous within one:", e.Name, len(e.Groups))
	for _, g := range e.Groups {
		fmt.Fprintf(&b, "\n  package %s: %s", g.Package, strings.Join(g.Files, ", "))
	}
	return b.String()
}

// Advice states the remedy that works. The previous message suggested a qualified name, which
// addresses a method within a type and cannot select a package — a caller who followed it hit
// "symbol not found" and had no reason to connect the two.
func (e CrossPackageSymbol) Advice() string {
	return "  pass the specific file; a qualified name selects a method within a type, not a package"
}

// IsNotFound keeps the exit code at 2. The caller must still choose, and an agent reacting to 2
// corrects its input rather than retrying the same command.
func (e CrossPackageSymbol) IsNotFound() bool { return true }

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
