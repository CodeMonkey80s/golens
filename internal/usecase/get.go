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
func Resolve(engine domain.Engine, name, target string) (domain.Symbol, domain.Document, error) {
	paths, err := Files(target)
	if err != nil {
		return domain.Symbol{}, domain.Document{}, err
	}
	if len(paths) == 0 {
		return domain.Symbol{}, domain.Document{}, errNoGoFiles(target)
	}

	var matches []domain.Symbol
	var matchDoc domain.Document

	for _, path := range paths {
		doc, err := engine.Parse(path)
		if err != nil {
			return domain.Symbol{}, domain.Document{}, err
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
		return domain.Symbol{}, domain.Document{}, NotFoundSymbol(name)
	case 1:
		return matches[0], matchDoc, nil
	default:
		names := make([]string, 0, len(matches))
		for _, m := range matches {
			names = append(names, filepath.ToSlash(m.File)+": "+m.QualifiedName)
		}
		return domain.Symbol{}, domain.Document{}, AmbiguousSymbol{Name: name, Matches: names}
	}
}

// Get extracts the source of a single symbol.
//
// includeSource false corresponds to --bounds-only and emits the identical payload to
// Bounds, per acceptance criterion A6.
func Get(engine domain.Engine, name, target string, includeSource bool, w io.Writer, opts format.Options) error {
	sym, doc, err := Resolve(engine, name, target)
	if err != nil {
		return err
	}

	source := ""
	if includeSource {
		source = sym.Bounds.Source(doc.Source)
	}

	if opts.Json {
		return format.WriteJson(w, format.Get(sym, filepath.ToSlash(sym.File), source, includeSource), opts.Compact)
	}
	return format.WriteGetHuman(w, sym, filepath.ToSlash(sym.File), source, opts.Compact)
}

// Bounds reports the byte and line span of a single symbol without its source.
//
// The target must be a file: passing a directory is a usage error, per decision D18.
func Bounds(engine domain.Engine, name, target string, w io.Writer, opts format.Options) error {
	info, err := statFile(target)
	if err != nil {
		return err
	}

	sym, _, err := Resolve(engine, name, info)
	if err != nil {
		return err
	}

	if opts.Json {
		return format.WriteJson(w, format.Bounds(sym, filepath.ToSlash(target)), opts.Compact)
	}
	return format.WriteBoundsHuman(w, sym, filepath.ToSlash(target), opts.Compact)
}
