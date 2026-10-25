package usecase

import (
	"io"
	"path/filepath"
	"sort"
	"strings"

	"github.com/CodeMonkey80s/golens/internal/domain"
	"github.com/CodeMonkey80s/golens/internal/format"
)

// Resolve finds a single symbol by name across the files of a package or directory.
//
// Lookup rules, per specification 2.2 and decision D17:
//
//   - An exact qualified name ("UserStore.GetUser") resolves to that method.
//   - A bare name resolves when exactly one declaration matches.
//   - Zero matches is a NotFoundSymbol, exit 2.
//   - More than one match is an AmbiguousSymbol carrying the candidates, also exit 2, so
//     the CLI lists them instead of guessing.
//
// A bare name matching declarations in more than one *package* is reported differently from a
// real conflict within one package. A directory may legitimately hold `package p` beside an
// external test `package p_test`, and both may declare `A` without either redeclaring anything.
// check already keys its namespace on directory and package name for exactly this reason
// (packageSymbols), so the two commands disagreeing about whether a conflict exists is the
// defect. See CrossPackageSymbol.
//
// Resolution walks files in lexical order, so which candidate is reported first in an
// error message is deterministic.
//
// A file that does not parse is skipped rather than fatal, and returned to the caller as
// the last result. outline does the same and for the same reason (D22): a half-edited file
// must not hide the files beside it. When target is a single file the skip is the whole
// answer and the caller returns it alone, which is what `get` on one broken file has always
// done.
//
// Skipping cannot silently hide a symbol. The caller reports the skips alongside the result
// and exits 4, and when nothing resolved at all the not-found answer names them too — the
// symbol may be in one of the files that were skipped, and saying so is the difference
// between "this does not exist" and "this could not be read".
func Resolve(engine domain.Engine, name, target string) (domain.Symbol, domain.Document, Skipped, error) {
	paths, err := Files(target)
	if err != nil {
		return domain.Symbol{}, domain.Document{}, Skipped{}, err
	}
	if len(paths) == 0 {
		return domain.Symbol{}, domain.Document{}, Skipped{}, errNoGoFiles(target)
	}

	var matches []domain.Symbol
	var matchDoc domain.Document
	var skipped Skipped
	skipped.Total = len(paths)
	// pkgNames records every namespace the search parsed, so a query that opens with a
	// package name can be told the form is not accepted instead of being answered with a
	// bare "not found". It is populated only from files that parsed, so the error names a
	// package that was really searched rather than one the query invented.
	pkgNames := make(map[string]bool)
	// packages counts the distinct namespaces the matches fall in, so a name declared once
	// in `p` and once in `p_test` can be told apart from one declared twice in `p`.
	packages := make(map[string][]int)

	for _, path := range paths {
		doc, parseErr := engine.Parse(path)
		if parseErr != nil {
			skipped.Files = append(skipped.Files, ParseError{Path: path, Err: parseErr})
			continue
		}
		pkgNames[doc.Package] = true
		for _, sym := range doc.Lookup(name) {
			sym.File = path
			matches = append(matches, sym)
			if len(matches) == 1 {
				matchDoc = doc
			}
			if _, seen := packages[doc.Package]; !seen {
				packages[doc.Package] = nil
			}
			packages[doc.Package] = append(packages[doc.Package], len(matches)-1)
		}
	}

	switch len(matches) {
	case 0:
		if len(skipped.Files) == len(paths) {
			// Nothing parsed, so nothing could have resolved. Reporting "symbol not found"
			// would be a confident wrong answer; the parse failure is the real one.
			return domain.Symbol{}, domain.Document{}, skipped, skipped.Files[0]
		}
		// If the query looks like p.Func and p is a package we parsed, try package-qualified lookup.
		if sym, doc, pkgSkipped, err := tryPackageQualified(engine, name, pkgNames, paths, skipped); err == nil && sym.Name != "" {
			return sym, doc, pkgSkipped, nil
		} else if err != nil {
			return domain.Symbol{}, domain.Document{}, pkgSkipped, err
		}
		if pq, ok := packageQualified(name, pkgNames, skipped); ok {
			return domain.Symbol{}, domain.Document{}, skipped, pq
		}
		return domain.Symbol{}, domain.Document{}, skipped, notFoundAmong(name, skipped)
	case 1:
		return matches[0], matchDoc, skipped, nil
	default:
		if len(packages) > 1 {
			return domain.Symbol{}, domain.Document{}, skipped, crossPackage(name, matches, packages)
		}
		names := make([]string, 0, len(matches))
		for _, m := range matches {
			names = append(names, filepath.ToSlash(m.File)+": "+m.QualifiedName)
		}
		return domain.Symbol{}, domain.Document{}, skipped, AmbiguousSymbol{
			Name:    name,
			Matches: names,
			Skipped: len(skipped.Files),
			Total:   len(paths),
		}
	}
}

// tryPackageQualified attempts package-qualified lookup when the normal resolution finds nothing.
// It is called only in the zero-match branch, so it never overrides a successful Type.Method resolution.
// Returns (symbol, document, skipped, nil) when found; (zero, zero, skipped, NotFound) when not found;
// (zero, zero, skipped, AmbiguousSymbol) when ambiguous in the named package.
func tryPackageQualified(engine domain.Engine, name string, pkgNames map[string]bool, paths []string, skipped Skipped) (domain.Symbol, domain.Document, Skipped, error) {
	pkg, rest, found := strings.Cut(name, ".")
	if !found || rest == "" || !pkgNames[pkg] {
		return domain.Symbol{}, domain.Document{}, skipped, nil
	}

	// The first segment is a known package name. Try to resolve the rest as a symbol in that package.
	var matches []domain.Symbol
	var matchDoc domain.Document
	var pkgSkipped Skipped
	pkgSkipped.Total = len(paths)

	for _, path := range paths {
		doc, parseErr := engine.Parse(path)
		if parseErr != nil {
			pkgSkipped.Files = append(pkgSkipped.Files, ParseError{Path: path, Err: parseErr})
			continue
		}
		if doc.Package != pkg {
			continue
		}
		for _, sym := range doc.Lookup(rest) {
			sym.File = path
			matches = append(matches, sym)
			if len(matches) == 1 {
				matchDoc = doc
			}
		}
	}

	switch len(matches) {
	case 0:
		return domain.Symbol{}, domain.Document{}, skipped, nil
	case 1:
		return matches[0], matchDoc, pkgSkipped, nil
	default:
		// Multiple matches in the same package — genuine ambiguity.
		names := make([]string, 0, len(matches))
		for _, m := range matches {
			names = append(names, filepath.ToSlash(m.File)+": "+m.QualifiedName)
		}
		return domain.Symbol{}, domain.Document{}, pkgSkipped, AmbiguousSymbol{
			Name:    rest,
			Matches: names,
			Skipped: len(skipped.Files),
			Total:   skipped.Total,
		}
	}
}

// crossPackage builds the error for a name that matched in more than one package.
//
// It is deliberately not AmbiguousSymbol. Nothing is ambiguous about `A` in a directory holding
// `package p` and `package p_test`: neither redeclares anything, and check already says so by
// keying its namespace on directory and package. Reporting a conflict that does not exist sends
// the caller looking for a duplicate to fix, and there isn't one.
//
// The remedy is a file, not a qualified name, because a qualified name addresses a method within
// a type ("UserStore.GetUser") and cannot express a package. The advice says so, which the
// previous message did not: it recommended the qualified name, and `get p.A` does not resolve.
func crossPackage(name string, matches []domain.Symbol, packages map[string][]int) error {
	grouped := make([]CrossPackageGroup, 0, len(packages))
	for _, pkg := range sortedKeys(packages) {
		var paths []string
		for _, idx := range packages[pkg] {
			paths = append(paths, filepath.ToSlash(matches[idx].File))
		}
		grouped = append(grouped, CrossPackageGroup{Package: pkg, Files: paths})
	}
	return CrossPackageSymbol{Name: name, Groups: grouped}
}

func sortedKeys(m map[string][]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Skipped records the files a search could not parse, alongside the size of that search.
//
// Total travels with the failures because the message needs it: "1 of 2 files" says how
// complete an answer is and "1 file" does not. It was lost once by carrying the failures
// alone and reconstructing the total from their length, which made every partial result
// claim the whole search had failed.
type Skipped struct {
	// Files names those that could not be parsed, in path order.
	Files []ParseError
	// Total is how many files the search covered.
	Total int
}

// asError reports the skipped files as the exit-4 condition, or nil when there were none.
//
// Every file failing is handled inside Resolve, which returns the first parse error
// directly. Reaching here means the search did produce an answer, so the answer is written
// and the incompleteness is reported alongside it.
func (s Skipped) asError() error {
	if len(s.Files) == 0 {
		return nil
	}
	return SkippedFilesError{Failures: s.Files, Total: s.Total}
}

// notFoundAmong reports a symbol that resolved nowhere, saying so only when files were
// skipped that might have declared it.
//
// With every file parsed, "not found" is the whole truth and adding a caveat would be
// noise. With some skipped it is not the truth: the declaration may sit in a file that
// could not be read, and an agent told only "not found" would rename or delete a symbol
// that exists.
// packageQualified reports a query that opened with the name of a package the search parsed.
//
// `domain.Node` and `main.main` both look like the way to name a symbol in its package, and
// outline prints its candidates as `file: Type.Method`, so the dotted form reads as
// supported. It is not: Lookup matches a bare name or a qualified method name and nothing
// else. Letting that read as "the symbol does not exist" is the failure — that is the answer
// a caller acts on by renaming or deleting code — so say which segment was a package and
// which form resolves instead.
//
// Only wired into the zero-match branch, so it can never override a symbol that did resolve,
// and it needs at least two segments: `Type.Method` is a legal query and its first segment
// is a type name that will not match a package name anyway.
func packageQualified(name string, pkgs map[string]bool, skipped Skipped) (PackageQualifiedSymbol, bool) {
	prefix, rest, found := strings.Cut(name, ".")
	if !found || rest == "" || !pkgs[prefix] {
		return PackageQualifiedSymbol{}, false
	}
	return PackageQualifiedSymbol{
		Name:    name,
		Package: prefix,
		Symbol:  rest,
		Skipped: len(skipped.Files),
		Total:   skipped.Total,
	}, true
}

func notFoundAmong(name string, skipped Skipped) error {
	if len(skipped.Files) == 0 {
		return NotFoundSymbol(name)
	}
	return NotFoundSymbolWithSkips{
		Name:  name,
		Total: skipped.Total,
		Files: skipped.Files,
	}
}

// Get extracts the source of a single symbol.
//
// includeSource false corresponds to --bounds-only and emits the identical payload to
// Bounds, per acceptance criterion A6.
func Get(engine domain.Engine, name, target string, includeSource bool, w io.Writer, opts format.Options) error {
	sym, doc, skipped, err := Resolve(engine, name, target)
	if err != nil {
		return err
	}

	source := ""
	if includeSource {
		source = sym.Bounds.Source(doc.Source)
	}

	var writeErr error
	if opts.JSON {
		writeErr = format.WriteJSON(w, format.Get(sym, filepath.ToSlash(sym.File), source, includeSource), opts.Compact)
	} else {
		writeErr = format.WriteGetHuman(w, sym, filepath.ToSlash(sym.File), source, opts.Compact)
	}
	if writeErr != nil {
		return writeErr
	}

	// The answer was found and written. The files that were skipped do not make it wrong,
	// but they do make it incomplete, so the run still exits 4 and says which ones: a
	// caller that sees exit 0 has been told the directory was fully readable, and it was
	// not.
	return skipped.asError()
}

// Bounds reports the byte and line span of a single symbol without its source.
//
// The target must be a file: passing a directory is a usage error, per decision D18. That
// narrows the skipping above to the single-file case, where a parse failure leaves nothing
// to report a span for and is returned as-is by Resolve.
func Bounds(engine domain.Engine, name, target string, w io.Writer, opts format.Options) error {
	info, err := statFile(target)
	if err != nil {
		return err
	}

	sym, _, skipped, err := Resolve(engine, name, info)
	if err != nil {
		return err
	}

	var writeErr error
	if opts.JSON {
		writeErr = format.WriteJSON(w, format.Bounds(sym, filepath.ToSlash(target)), opts.Compact)
	} else {
		writeErr = format.WriteBoundsHuman(w, sym, filepath.ToSlash(target), opts.Compact)
	}
	if writeErr != nil {
		return writeErr
	}
	return skipped.asError()
}
