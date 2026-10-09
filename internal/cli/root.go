// Package cli wires cobra commands to the usecase layer.
//
// This package owns flag parsing, stdout/stderr routing and exit codes, and nothing else.
// It contains no AST logic and no formatting logic.
package cli

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/CodeMonkey80s/golens/internal/domain"
)

// Env carries the process streams and engine, so the command tree is testable without
// touching global state.
type Env struct {
	Stdout io.Writer
	Stderr io.Writer
	Engine domain.Engine
}

// New builds the root command with every subcommand attached.
func New(env Env) *cobra.Command {
	root := &cobra.Command{
		Use:   "golens",
		Short: "Structural AST queries for AI coding agents",
		Long: "golens provides structural AST queries (outline, get, bounds, check) so agents\n" +
			"can read targeted source instead of whole files, and edit using byte-accurate bounds.",
		// Errors are reported by Execute, which owns the exit code mapping.
		SilenceErrors: true,
		SilenceUsage:  true,
	}

	// Global flags are persistent so they may appear before or after the subcommand.
	root.PersistentFlags().Bool("json", false, "output a machine-readable JSON payload")
	root.PersistentFlags().Bool("compact", false, "minify output to reduce token usage")

	root.AddCommand(
		newOutlineCmd(env),
		newGetCmd(env),
		newBoundsCmd(env),
		newCheckCmd(env),
	)
	return root
}

// Execute runs the root command and returns the process exit code.
//
// The code is derived from the error rather than chosen at each call site: exit 1 for a
// usage mistake, 2 when a symbol or file is missing or ambiguous, 4 for a parse or I/O
// failure. Errors are written to stderr so that --json output on stdout stays pipe-clean.
func Execute(root *cobra.Command) int {
	err := root.Execute()
	if err == nil {
		return int(domain.ExitOk)
	}

	classified := classifyUsage(err)
	code := exitCodeFor(classified)

	// Exit 3 is a successful check that found problems, so the message goes to stderr and
	// the caller reads the diagnostics that were already written there. Every other code
	// means the command did not do its job and the message explains why.
	if code != int(domain.ExitDiagnostics) {
		fmt.Fprintln(root.ErrOrStderr(), "golens: "+classified.Error())
	}
	if amb, ok := classified.(AmbiguousError); ok {
		for _, candidate := range amb.Candidates() {
			fmt.Fprintln(root.ErrOrStderr(), "  "+candidate)
		}
	}

	return code
}

// AmbiguousError exposes the candidate list of an ambiguous-symbol error without the cli
// package importing the concrete usecase type.
type AmbiguousError interface {
	error
	Candidates() []string
}

// exitCodeFor maps an error to the documented exit code.
//
// Classification is by behaviour rather than concrete type, so the usecase layer owns the
// meaning and this package never imports its error types. Three shapes are recognised:
//
//   - IsUsage: a bad invocation, including cobra's own argument-count errors.
//   - IsNotFound: a symbol or file that is missing or ambiguous.
//   - IsDiagnostics: check succeeded mechanically and found problems, which is exit 3.
//
// Anything else is a parse or I/O failure.
func exitCodeFor(err error) int {
	var usage interface{ IsUsage() bool }
	if errors.As(err, &usage) && usage.IsUsage() {
		return int(domain.ExitUsage)
	}

	var notFound interface{ IsNotFound() bool }
	if errors.As(err, &notFound) && notFound.IsNotFound() {
		return int(domain.ExitNotFound)
	}

	var diags interface{ IsDiagnostics() bool }
	if errors.As(err, &diags) && diags.IsDiagnostics() {
		return int(domain.ExitDiagnostics)
	}

	return int(domain.ExitParse)
}

// classifyUsage tags cobra's own errors as usage mistakes.
//
// Cobra reports a wrong argument count and an unknown flag through plain error values, so
// without this they would fall through to exit 4 and report a parse failure for what is
// really a typo in the invocation.
func classifyUsage(err error) error {
	if err == nil {
		return nil
	}
	if _, ok := err.(interface{ IsUsage() bool }); ok {
		return err
	}
	if _, ok := err.(interface{ IsNotFound() bool }); ok {
		return err
	}
	if _, ok := err.(interface{ IsDiagnostics() bool }); ok {
		return err
	}
	return usageError{err}
}

// usageError wraps an error so it maps to exit 1.
type usageError struct{ err error }

func (u usageError) Error() string { return u.err.Error() }
func (u usageError) Unwrap() error { return u.err }
func (u usageError) IsUsage() bool { return true }

// DefaultEnv returns the process streams wired to engine.
//
// The engine is a parameter rather than constructed here: this package must not import an
// engine implementation, or cobra would pull go/ast into the CLI layer and break the
// boundary asserted by internal/arch. Composition happens in cmd/golens.
func DefaultEnv(engine domain.Engine) Env {
	return Env{
		Stdout: os.Stdout,
		Stderr: os.Stderr,
		Engine: engine,
	}
}
