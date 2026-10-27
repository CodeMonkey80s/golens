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

// errNoGoFiles reports a directory containing no Go sources.
type errNoGoFiles string

func (e errNoGoFiles) Error() string {
	return fmt.Sprintf("no .go files found in %s", string(e))
}

// IsNotFound marks the error as a not-found condition, so the CLI exits 2.
func (e errNoGoFiles) IsNotFound() bool { return true }

// errNoCheckableFiles reports a directory whose Go sources all sit in testdata directories,
// which check does not read.
//
// It exists so the message names the actual reason. errNoGoFiles's "no .go files found"
// asserts the directory holds no Go source, and here it holds plenty -- none of it is
// compiled by any build, which is why check skips it. Same exit code, true statement.
type errNoCheckableFiles string

func (e errNoCheckableFiles) Error() string {
	return fmt.Sprintf("no checkable .go files in %s; testdata directories are not checked", string(e))
}

// IsNotFound keeps exit 2: the caller named a path with nothing checkable under it.
func (e errNoCheckableFiles) IsNotFound() bool { return true }

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
//
// Candidates carrying "[build: ...]" are files the named constraint excludes; the marker is
// what turns a bare list of paths into a decision about which one the current platform builds.
func (e AmbiguousSymbol) Advice() string {
	return "  pass a specific file instead of a directory, or the qualified name, to disambiguate; [build: ...] marks a file's build constraint"
}

// IsNotFound marks ambiguity under exit 2, the same code as a missing symbol, since both
// mean "the caller must disambiguate or correct the name".
func (e AmbiguousSymbol) IsNotFound() bool { return true }

// Skips reports how much of the search could not be parsed, so a machine consumer can tell a
// complete answer from a partial one.
func (e AmbiguousSymbol) Skips() (int, int) { return e.Skipped, e.Total }

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

// Skips reports how much of the search could not be parsed.
func (e NotFoundSymbolWithSkips) Skips() (int, int) { return len(e.Files), e.Total }

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
// The advice names the package-qualified form now that it resolves. It also still offers a
// file, because a file is unambiguous even when the bare name is not.
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

// Advice states the remedy that works. Since package-qualified lookup landed, a query of the
// form Package.Symbol selects one of the packages directly; passing a file remains valid too.
func (e CrossPackageSymbol) Advice() string {
	example := e.Name
	if len(e.Groups) > 0 {
		example = e.Groups[0].Package + "." + e.Name
	}
	return fmt.Sprintf("  use %s to select one package, or pass the specific file", example)
}

// IsNotFound keeps the exit code at 2. The caller must still choose, and an agent reacting to 2
// corrects its input rather than retrying the same command.
func (e CrossPackageSymbol) IsNotFound() bool { return true }

// NotFoundInPackage reports a package-qualified query whose package was searched but whose
// symbol is absent. Package-qualified lookup is real since v0.3, so a miss in a known package
// is an ordinary miss with a narrower answer: the package exists, the declaration does not.
//
// It replaces PackageQualifiedSymbol, whose advice ("pass the bare name") was written before
// package-qualified lookup worked and became actively wrong: following it retried a name that
// was already known to be missing.
//
// Skipped and Total travel with it because the answer is partial whenever files could not be
// parsed, and a partial search must not read like a complete one.
type NotFoundInPackage struct {
	// Name is the part after the package qualifier, as written.
	Name string
	// Package is the package the query's first segment named.
	Package string
	// Skipped counts files in the search that could not be parsed, and Total how many were
	// searched.
	Skipped int
	Total   int
}

func (e NotFoundInPackage) Error() string {
	msg := fmt.Sprintf("symbol %q not found in package %q", e.Name, e.Package)
	if e.Skipped > 0 {
		msg += fmt.Sprintf("; %d of %d files could not be parsed and may declare it", e.Skipped, e.Total)
	}
	return msg
}

// IsNotFound keeps the exit code at 2, the same as any other miss: the caller has to correct
// the query, which is what 2 means.
func (e NotFoundInPackage) IsNotFound() bool { return true }

// Skips reports how much of the search could not be parsed.
func (e NotFoundInPackage) Skips() (int, int) { return e.Skipped, e.Total }

// UnreadableFilesError reports files that could not be opened at all, as distinct from files
// that were opened and did not parse.
//
// It carries no per-file listing. The diagnostic for each unreadable file has already been
// written — to stderr in the human form, and into the diagnostics array under --json, so a
// machine consumer is not left with exit 4 and a payload that silently omits the file. Naming
// them a second time here printed the same line twice on stderr, which reads as two failures
// where there was one.
//
// Its job is the exit code. 3 means "check ran and found something about the code"; a file that
// could not be opened is not that, and the documented code for it is 4. outline and check were
// disagreeing about the same file before this type existed.
type UnreadableFilesError struct {
	// Count is how many files could not be opened.
	Count int
	// Total is how many files the run covered.
	Total int
	// Quiet asks the CLI not to write this to stderr.
	//
	// Set when the output format has already said everything. Under --json the per-file
	// diagnostic is in the payload and the exit code is 4, and the documented stream contract
	// is that stderr stays empty — the caller is told not to add 2>&1. In the human form the
	// summary is worth keeping: it is the difference between one unreadable file and a tree
	// with several.
	Quiet bool
}

func (e UnreadableFilesError) Error() string {
	return fmt.Sprintf("%d of %d files could not be read", e.Count, e.Total)
}

// Silent reports whether the CLI should keep this off stderr. See Quiet.
func (e UnreadableFilesError) Silent() bool { return e.Quiet }

// Skips reports how many files could not be read, so a JSON error consumer sees the same
// completeness information as the human summary.
func (e UnreadableFilesError) Skips() (int, int) { return e.Count, e.Total }

// Category marks this as an I/O failure rather than a syntax failure.
func (e UnreadableFilesError) Category() string { return "io" }

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
