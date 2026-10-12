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
	"runtime/debug"
	"strings"

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
		Long: `golens provides structural AST queries (outline, get, bounds, check) so agents
can read targeted source instead of whole files, and edit using byte-accurate bounds.

  outline  what is in this file or directory
  get      the exact source of one symbol -- safe to write back verbatim
  bounds   where to splice a replacement
  check    is this valid, and are these imports used

Exit codes:
  0  success
  1  usage error -- wrong flags or arguments
  2  symbol or file not found, or the symbol is ambiguous
  3  check ran successfully and found diagnostics
  4  parse or I/O failure

Streams:
  outline, get and bounds write to stdout.
  check writes to stderr in the human form. Capture both:
  "golens check . 2>&1". Reading only stdout yields empty output and
  looks like a clean result.
  check --json is the exception: the payload goes to stdout and stderr
  stays empty, so it pipes. Do not add 2>&1 to a --json call.

Run "golens <command> --help" before the first use of a command. The conventions
differ between them, and bounds in particular will corrupt a file if guessed.

The outline header reports a file's //go:build constraint when it has one, so
you can tell whether the current GOOS compiles an edit before making it.`,
		// Errors are reported by Execute, which owns the exit code mapping.
		SilenceErrors: true,
		SilenceUsage:  true,
	}

	// Global flags are persistent so they may appear before or after the subcommand.
	root.PersistentFlags().Bool("json", false, "output a machine-readable JSON payload")
	root.PersistentFlags().Bool("compact", false, "minify output to reduce token usage")

	// Cobra renders --version as "golens version <v>" using the command's Name, which is
	// bare, so the template is set to include the binary name.
	root.SetVersionTemplate("golens {{.Version}}\n")
	root.Version = releaseVersion()

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
		fmt.Fprintln(root.ErrOrStderr(), "golens: "+classified.Error()) //nolint:errcheck // diagnostic write; a failure here is not actionable
	}
	if amb, ok := classified.(AmbiguousError); ok {
		for _, candidate := range amb.Candidates() {
			fmt.Fprintln(root.ErrOrStderr(), "  "+candidate) //nolint:errcheck // diagnostic write
		}
		if advised, ok := classified.(interface{ Advice() string }); ok {
			fmt.Fprintln(root.ErrOrStderr(), advised.Advice()) //nolint:errcheck // diagnostic write
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

// Version is the release identifier reported by --version. It is set at link time:
//
//	go build -ldflags "-X github.com/CodeMonkey80s/golens/internal/cli.Version=v0.1.0"
//
// It has to stay a variable initialised to a constant string expression. The linker's -X is
// documented to ignore a variable whose initialiser is a call, so the default below is a
// literal rather than a call to the resolver: swapping it for one would silently break
// injection while the binary still built, which is the shape of failure this repository
// keeps running into.
//
// Nothing in the source states a release number. The tag is the single source of truth, and
// TestVersionIsInjectedByLdflags fails if this stops working.
var Version = develVersion

// develVersion is what an uninstrumented build reports: a plain `go build` from a checkout,
// or `go test`, where no release was ever stamped in.
const develVersion = "(devel)"

// releaseVersion resolves what --version prints.
//
// The link-time value wins when there is one. Otherwise the module version the toolchain
// stamps into the binary is used, so `go install module@v0.1.0` reports v0.1.0 with no flags
// at all — and since that is how most people obtain a published module, hardcoding a number
// here would report a stale release to everyone who installed it.
func releaseVersion() string {
	if Version != develVersion {
		return Version
	}
	info, ok := debug.ReadBuildInfo()
	if !ok || info.Main.Version == "" || info.Main.Version == develVersion {
		return Version
	}
	return info.Main.Version
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
// without this they would fall through to exit 4 and report a parse failure for what is really
// a typo in the invocation.
//
// Only errors that already carry a classification pass through unchanged, and only errors that
// are recognisably cobra's get wrapped. Everything else is a genuine parse or I/O failure and
// must keep exit 4: this function used to wrap *any* unrecognised error as a usage error, which
// turned `outline` on an unparseable file from exit 4 into exit 1 and told a caller it had
// invoked the tool wrongly when the file was at fault.
func classifyUsage(err error) error {
	if err == nil {
		return nil
	}
	var usage interface{ IsUsage() bool }
	if errors.As(err, &usage) {
		return err
	}
	var notFound interface{ IsNotFound() bool }
	if errors.As(err, &notFound) {
		return err
	}
	var diags interface{ IsDiagnostics() bool }
	if errors.As(err, &diags) {
		return err
	}
	if isCobraError(err) {
		return usageError{err}
	}
	return err
}

// isCobraError reports whether err came from cobra's own flag and argument handling.
//
// Cobra's messages are stable enough to recognise, and there is no exported error type to match
// on. The check is deliberately narrow: misclassifying a real failure as a usage error is worse
// than leaving a cobra error to exit 4, because exit 4 at least tells the caller the input
// could not be processed.
func isCobraError(err error) bool {
	msg := err.Error()
	return strings.HasPrefix(msg, "unknown command ") ||
		strings.HasPrefix(msg, "unknown flag: ") ||
		strings.HasPrefix(msg, "unknown shorthand flag: ") ||
		strings.Contains(msg, "accepts ") && strings.Contains(msg, "arg(s)") ||
		strings.Contains(msg, "requires at least ") ||
		strings.Contains(msg, "flag needs an argument")
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
