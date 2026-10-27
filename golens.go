// Package golens exposes the CLI's structural queries to Go programs.
//
// The command line remains the primary interface. This package is a thin, supported
// embedding layer over the same engine and usecase code, for tools that want outline, get,
// bounds or check without spawning a process. It renders the same human or JSON output as
// the CLI; it does not expose the internal AST types.
package golens

import (
	"errors"
	"io"

	"github.com/CodeMonkey80s/golens/internal/engine/goast"
	"github.com/CodeMonkey80s/golens/internal/format"
	"github.com/CodeMonkey80s/golens/internal/usecase"
)

// DepthUnlimited asks Outline for the whole tree.
const DepthUnlimited = usecase.DepthUnlimited

// Options controls embedding calls.
type Options struct {
	// JSON emits the machine-readable payload instead of human text.
	JSON bool
	// Compact removes decoration without removing information.
	Compact bool
	// Depth limits Outline recursion. Zero means unlimited, matching the CLI default.
	Depth int
	// BoundsOnly makes Get emit only offsets, like `get --bounds-only`.
	BoundsOnly bool
	// PackageDir narrows Get and Bounds to one directory when package names collide.
	PackageDir string
	// Tags are custom build tags for Check, matching `go build -tags`.
	Tags []string
	// SkipTestdata and SkipIgnored opt into the build's view for the structural commands.
	SkipTestdata bool
	SkipIgnored  bool
}

// ExitCode is the process exit code the CLI would return for the same operation.
type ExitCode int

const (
	ExitOK          ExitCode = 0
	ExitUsage       ExitCode = 1
	ExitNotFound    ExitCode = 2
	ExitDiagnostics ExitCode = 3
	ExitParse       ExitCode = 4
)

func formatOptions(opts Options) format.Options {
	return format.Options{JSON: opts.JSON, Compact: opts.Compact}
}

func resolveOptions(opts Options) usecase.ResolveOptions {
	return usecase.ResolveOptions{SkipTestdata: opts.SkipTestdata, SkipIgnored: opts.SkipIgnored}
}

func targetOrPackageDir(target string, opts Options) string {
	if opts.PackageDir != "" {
		return opts.PackageDir
	}
	return target
}

// Outline writes the structural outline of a file or directory.
func Outline(target string, w io.Writer, opts Options) error {
	depth := opts.Depth
	if depth == 0 {
		depth = DepthUnlimited
	}
	return usecase.OutlineWithFileOptions(goast.New(), target, depth, w, formatOptions(opts), usecase.FileOptions{
		SkipTestdata: opts.SkipTestdata,
		SkipIgnored:  opts.SkipIgnored,
	})
}

// Get writes the exact source of one symbol, or only its bounds with BoundsOnly.
func Get(symbol, target string, w io.Writer, opts Options) error {
	return usecase.GetWithOptions(goast.New(), symbol, targetOrPackageDir(target, opts), !opts.BoundsOnly, w, formatOptions(opts), resolveOptions(opts))
}

// Bounds writes the byte and line span of one symbol.
func Bounds(symbol, target string, w io.Writer, opts Options) error {
	return usecase.BoundsWithOptions(goast.New(), symbol, targetOrPackageDir(target, opts), w, formatOptions(opts), resolveOptions(opts))
}

// Check validates one or more files or directories.
//
// Diagnostics go to stderr in the human form, or into the JSON payload on stdout when
// opts.JSON is set, exactly as for the CLI. The returned error is still classified by
// ExitCodeFor.
func Check(targets []string, stdout, stderr io.Writer, opts Options) error {
	if len(targets) == 0 {
		return noTargetsError{}
	}
	return usecase.CheckTargetsWithOptions(goast.New(), targets, usecase.CheckOptions{Tags: opts.Tags}, stdout, stderr, formatOptions(opts))
}

type noTargetsError struct{}

func (noTargetsError) Error() string { return "golens: check needs at least one target" }
func (noTargetsError) IsUsage() bool { return true }

// ExitCodeFor maps an error from this package to the CLI's documented exit code.
func ExitCodeFor(err error) ExitCode {
	if err == nil {
		return ExitOK
	}

	var usage interface{ IsUsage() bool }
	if errors.As(err, &usage) && usage.IsUsage() {
		return ExitUsage
	}
	var notFound interface{ IsNotFound() bool }
	if errors.As(err, &notFound) && notFound.IsNotFound() {
		return ExitNotFound
	}
	var diagnostics interface{ IsDiagnostics() bool }
	if errors.As(err, &diagnostics) && diagnostics.IsDiagnostics() {
		return ExitDiagnostics
	}
	return ExitParse
}
