package cli

import (
	"github.com/spf13/cobra"

	"github.com/CodeMonkey80s/golens/internal/format"
	"github.com/CodeMonkey80s/golens/internal/usecase"
)

// flagOptions reads the persistent global flags into format.Options.
func flagOptions(cmd *cobra.Command) format.Options {
	jsonOut, _ := cmd.Flags().GetBool("json")
	compact, _ := cmd.Flags().GetBool("compact")
	return format.Options{Json: jsonOut, Compact: compact}
}

func newOutlineCmd(env Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "outline <file|dir>",
		Short: "Generate a structural hierarchy map of a file or directory",
		Long: `Generate a structural hierarchy map of a file or directory.

The outline is structural, not semantic: imports, types, functions, methods,
vars and consts. Struct fields and parameters appear from depth 2 downward, and
--depth defaults to unlimited, so a plain run shows everything.

Build constraints are ignored. Every .go file in a directory is parsed and
listed, including platform variants, because the question is what the tree
contains rather than what this machine would compile. A directory holding
filesystems_linux.go and filesystems_darwin.go lists both.

That also means the output does not show a file's build constraint. Read the
//go:build line in the file itself before editing it: a change to a variant that
the current GOOS excludes will not be compiled, so it will appear to do nothing.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			depth, _ := cmd.Flags().GetInt("depth")
			return usecase.Outline(env.Engine, args[0], depth, env.Stdout, flagOptions(cmd))
		},
	}

	cmd.Flags().Int("depth", usecase.DepthUnlimited, "restrict AST hierarchy recursion depth")
	return cmd
}

func newGetCmd(env Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <symbol> <file|dir>",
		Short: "Extract the exact source of a specific AST symbol",
		Long: `Extract the exact source of a specific AST symbol.

The result is the file's own bytes for that declaration, so it is safe to write
back verbatim. That is the most reliable round trip available: the text get
returns is exactly the span bounds reports, so replacing one with the other
cannot corrupt the file.

A method is addressed as Type.Method.

A symbol may be ambiguous when several files declare it, which is common for the
platform variants of one method. Ambiguity exits 2, writes nothing to stdout and
lists the candidates on stderr. Pass a specific file rather than a directory to
disambiguate:

  golens get Mount.Stat mounts_linux.go`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			boundsOnly, _ := cmd.Flags().GetBool("bounds-only")
			return usecase.Get(env.Engine, args[0], args[1], !boundsOnly, env.Stdout, flagOptions(cmd))
		},
	}

	cmd.Flags().Bool("bounds-only", false, "omit source text; return only line ranges and byte offsets")
	return cmd
}

func newBoundsCmd(env Env) *cobra.Command {
	return &cobra.Command{
		Use:   "bounds <symbol> <file>",
		Short: "Calculate precise byte offsets and line bounds for targeted edits",
		Long: `Calculate precise byte offsets and line bounds for targeted edits.

Byte ranges are half-open: src[start_byte:end_byte] is exactly the declaration.
Line ranges are inclusive at both ends. The terminating newline falls outside the
span, so an overwrite leaves the file's line structure intact.

The span starts at the declaration's name, not its keyword. For a func that is
"func"; for a type, var or const, the keyword is outside the range:

  type User struct{ ... }     bounds start at "User", not at "type"
  func NewUser() *User { ... } bounds start at "func"

Replacement text must therefore begin at the name as well. Writing a fresh
"type User struct{ ... }" into that span yields "type type User struct{ ... }".

Grouped declarations are why it works this way. In "const ( A = 1; B = 2 )" each
name needs its own bounds, so that editing one does not overwrite the other.

The safest round trip is to write back the text "golens get" returned for the
same symbol.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return usecase.Bounds(env.Engine, args[0], args[1], env.Stdout, flagOptions(cmd))
		},
	}
}

func newCheckCmd(env Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "check <file|dir>",
		Short: "Validate source syntax and detect unused imports or duplicate declarations",
		Long: `Validate source syntax and detect unused imports or duplicate declarations.

Diagnostics go to stderr and stdout stays empty, so capture both streams:

  golens check . 2>&1

Exit 3 means the check ran and found problems. Exit 0 means it found none.

--naming reports camelCaseId naming-convention violations instead of the syntax
checks. SCREAMING_SNAKE_CASE constants are exempt, since camelCaseId cannot
apply to them.

Two limits worth knowing before acting on the output. Build constraints are
ignored, so the syntax and import checks run on files this platform would
exclude; duplicate declarations are only reported when both files could be
compiled together. And the unused-import check is best-effort: it may miss an
import used only in a comment or shadowed by a local, but it never reports an
import that is genuinely in use.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// --naming is off by default: the check is AST-level validation, and a caller
			// asking for a syntax check should not get naming opinions unless requested.
			namingOnly, _ := cmd.Flags().GetBool("naming")
			return usecase.Check(env.Engine, args[0], env.Stdout, env.Stderr, flagOptions(cmd), namingOnly)
		},
	}

	cmd.Flags().Bool("naming", false, "report camelCaseId naming-convention violations instead of syntax checks")
	return cmd
}
