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

  Depth   1 = declarations only
          2 = adds struct fields and parameters
          default -1 (unlimited); --depth 3 and above add nothing further
  Lists   every .go file in a directory, including platform variants
  Hides   a file's build constraint -- the output does not show it

Structural, not semantic: imports, types, functions, methods, vars and consts.

Build constraints are ignored on purpose -- the question is what the tree
contains, not what this machine compiles. The cost is that the output cannot
tell you which files apply to the current platform. Read the //go:build line in
the file before editing it: a change to an excluded variant is never compiled,
so it will appear to have done nothing.`,
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

  Safe    the result is the file's own bytes -- write it back verbatim
  Method  address it as Type.Method
  Miss    symbol not found OR ambiguous -- both exit 2

Safest round trip available: the text get returns is exactly the span bounds
reports, so replacing one with the other cannot corrupt the file.

Ambiguity is common for the platform variants of one method. It exits 2, writes
nothing to stdout, and lists candidates on stderr. Pass a file, not a directory:

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

  Bytes   half-open: src[start_byte:end_byte] is exactly the declaration
  Lines   inclusive at both ends; trailing newline is outside the span
  Starts  at the declaration's NAME, not its keyword
  Edit    your replacement text must start at the name too
  Miss    symbol not found OR ambiguous -- both exit 2

  type User struct { ... }      bounds start at "User",  "type" is outside
  func NewUser() *User { ... }  bounds start at "func"

Writing a fresh "type User struct { ... }" into that span produces
"type type User struct { ... }".

The span is name-scoped so that grouped declarations stay independently
addressable: in "const ( A = 1; B = 2 )" each name needs its own bounds, or
editing one would overwrite the other. That is deliberate -- do not work around it.

To edit safely: take the text "golens get" returned for the same symbol and write
it back.`,
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

  Output   stderr only; stdout stays empty
  Capture  golens check . 2>&1    -- stdout alone looks like a clean result
  Exit     3 = ran and found diagnostics, 0 = found none
  Limits   build constraints ignored; unused-import check is best-effort

Duplicate declarations are reported only when both files could be compiled
together, so mutually exclusive platform variants stay silent. Syntax and import
checks still run on files the current platform excludes.

The unused-import check may miss an import used only in a comment or shadowed by
a local. It never reports one that is genuinely in use.

--naming reports camelCaseId violations instead of the syntax checks.
SCREAMING_SNAKE_CASE constants are exempt; the convention cannot apply to them.`,
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
