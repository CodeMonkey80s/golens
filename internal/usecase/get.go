package usecase

import (
	"io"
	"path/filepath"

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

	for _, path := range paths {
		doc, parseErr := engine.Parse(path)
		if parseErr != nil {
			skipped.Files = append(skipped.Files, ParseError{Path: path, Err: parseErr})
			continue
		}
		for _, sym := range doc.Lookup(name) {
			sym.File = path
			matches = append(matches, sym)
			if len(matches) == 1 {
				matchDoc = doc
			}
		}
	}

	switch len(matches) {
	case 0:
		if len(skipped.Files) == len(paths) {
			// Nothing parsed, so nothing could have resolved. Reporting "symbol not found"
			// would be a confident wrong answer; the parse failure is the real one.
			return domain.Symbol{}, domain.Document{}, skipped, skipped.Files[0]
		}
		return domain.Symbol{}, domain.Document{}, skipped, notFoundAmong(name, skipped)
	case 1:
		return matches[0], matchDoc, skipped, nil
	default:
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
