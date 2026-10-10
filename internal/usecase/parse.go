package usecase

import (
	"errors"
	"runtime"
	"sync"

	"github.com/CodeMonkey80s/golens/internal/domain"
)

// Concurrency policy for directory parsing.
//
// A single-file invocation stays strictly sequential: that path carries the < 5ms P95 budget
// from specification 3.2, and paying goroutine setup for one parse would make the common case
// slower to satisfy a budget it already meets.
//
// Directory traversal is where wall clock actually costs a caller, so files are parsed
// concurrently and reassembled in lexical order. Concurrency must never be observable in the
// output, which is what acceptance criterion A3 asserts: identical input yields byte-identical
// output whether parsed serially or in parallel.
//
// Below serialThreshold files are parsed sequentially regardless, because the goroutine and
// channel overhead exceeds the parse cost for small inputs.
const serialThreshold = 8

// ParseError pairs a path with the error that path produced.
//
// The engine's ParseError already names the file in its message, so this exists to carry the
// association structurally, which is what lets a caller attribute diagnostics to files without
// parsing strings.
type ParseError struct {
	Path string
	Err  error
}

// Error names the file once.
//
// A parse failure already names its file inside the engine's message, so prepending e.Path would
// print it twice -- "/a.go: /a.go:3:9: ...". Anything else, an I/O failure say, carries no file
// of its own, so the path is the only identifying information and is added.
func (e ParseError) Error() string {
	var pe interface{ LocatedErrors() []domain.LocatedError }
	if errors.As(e.Err, &pe) {
		return e.Err.Error()
	}
	return e.Path + ": " + e.Err.Error()
}

func (e ParseError) Unwrap() error { return e.Err }

// Diagnostics returns one Diagnostic per parse error, each carrying the line and column the
// parser reported. A failure that is not a parse error -- an I/O failure, say -- becomes a single
// diagnostic at 1:1, since there is no position to report.
func (e ParseError) Diagnostics() []Diagnostic {
	var pe interface{ LocatedErrors() []domain.LocatedError }
	if !errors.As(e.Err, &pe) {
		return []Diagnostic{{Severity: "error", Path: e.Path, Line: 1, Column: 1, Message: e.Err.Error()}}
	}

	located := pe.LocatedErrors()
	out := make([]Diagnostic, 0, len(located))
	for _, l := range located {
		// A located error names its own file, which is more precise than the path this
		// ParseError was constructed with, so it wins when present.
		path := l.Path
		if path == "" {
			path = e.Path
		}
		out = append(out, Diagnostic{
			Severity: "error",
			Path:     path,
			Line:     l.Line,
			Column:   l.Column,
			Message:  l.Message,
		})
	}
	return out
}

// parseAll parses every path and returns the documents alongside every parse error.
//
// Results are positional: docs[i] corresponds to paths[i]. A path that failed to parse has a
// zero Document at its index, so callers distinguish success from failure by Path being empty
// rather than by consulting the error slice.
//
// Errors are returned rather than aborting because the two callers want opposite behaviour:
// check treats a parse failure as a diagnostic to report, while outline treats it as a failure
// to propagate. Returning both lets each decide without a second parse.
func parseAll(engine domain.Engine, paths []string) ([]domain.Document, []ParseError) {
	docs := make([]domain.Document, len(paths))
	errs := make([]ParseError, len(paths))

	if len(paths) < serialThreshold || runtime.GOMAXPROCS(0) < 2 {
		for i, path := range paths {
			docs[i], errs[i] = parseOne(engine, path)
		}
		return docs, errs
	}
	return parseConcurrent(engine, paths, docs, errs)
}

// parseOne parses a single path, wrapping any error with that path.
func parseOne(engine domain.Engine, path string) (domain.Document, ParseError) {
	doc, err := engine.Parse(path)
	if err != nil {
		return domain.Document{}, ParseError{Path: path, Err: err}
	}
	return doc, ParseError{}
}

// parseConcurrent fans out one goroutine per file, bounded by GOMAXPROCS.
//
// Each worker writes only to its own index, so results need no synchronisation; the WaitGroup
// supplies the happens-before edge that makes them safe to read. The token channel caps parses
// in flight without a semaphore type.
func parseConcurrent(engine domain.Engine, paths []string, docs []domain.Document, errs []ParseError) ([]domain.Document, []ParseError) {
	limit := runtime.GOMAXPROCS(0)
	if limit > len(paths) {
		limit = len(paths)
	}

	var wg sync.WaitGroup
	tokens := make(chan struct{}, limit)

	for i, path := range paths {
		wg.Add(1)
		go func(idx int, p string) {
			defer wg.Done()

			tokens <- struct{}{}
			doc, err := engine.Parse(p)
			<-tokens

			if err != nil {
				errs[idx] = ParseError{Path: p, Err: err}
				return
			}
			docs[idx] = doc
		}(i, path)
	}

	wg.Wait()
	return docs, errs
}
