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
		Args:  cobra.ExactArgs(1),
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
		Args:  cobra.ExactArgs(2),
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
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return usecase.Bounds(env.Engine, args[0], args[1], env.Stdout, flagOptions(cmd))
		},
	}
}

func newCheckCmd(env Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "check <file|dir>",
		Short: "Validate source syntax and detect unused imports or duplicate declarations",
		Args:  cobra.ExactArgs(1),
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
