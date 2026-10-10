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
	return format.Options{JSON: jsonOut, Compact: compact}
}

func newOutlineCmd(env Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "outline <file|dir>",
		Short: "Generate a structural hierarchy map of a file or directory",
		Long: `Generate a structural hierarchy map of a file or directory.

  Depth   1 = declarations only
          2 = adds struct fields and parameters
          default -1 (unlimited); --depth 3 and above add nothing further
  Build   a file's //go:build expression, when it has one
  Lists   every .go file in a directory, including platform variants

Structural, not semantic: imports, types, functions, methods, vars and consts.

Build constraints are ignored on purpose -- the question is what the tree
contains, not what this machine compiles. The header therefore reports each
file's constraint so you can tell which edits the current GOOS will compile:

  file: filesystems_windows.go
  package: main
  build: windows

A file that does not parse aborts the traversal and exits 4, even if it is one
of thousands: an outline entry without its source is not useful. To survey a
tree that may contain broken files, use "golens check", which reports the
failure as one diagnostic among many and still covers every other file.`,
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

  golens get Mount.Stat mounts_linux.go

A method promoted from an embedded field belongs to the type that declares it.
Given "type Derived struct { Base }", "get Derived.Lock" exits 2 and "get Lock"
returns Base.Lock. golens resolves declarations, not the method set.`,
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

start_byte and end_byte count BYTES, not characters. Open the file in binary
and splice by those numbers -- open(p,"rb").read()[s:e], Path.read_bytes(),
dd bs=1 skip=s count=(e-s). Decoding the file to a string first and indexing
that gives CHARACTER offsets, which are a different numbering whenever the
file contains a non-ASCII identifier or a BOM, and Python's utf-8-sig decode
drops the BOM and shifts every offset by three more.

The failure is silent. A file differing by one byte per non-ASCII character
gives a span that runs past the declaration, and a string splice drops the
rest of it:

  type Cafe struct{ Name string }   26 bytes, 26 characters -- same
  type Café struct{ Name string }   27 bytes, 26 characters -- differ

  slicing the decoded text at start_byte:end_byte yields
    "type RENAMED\n"
  and the file no longer parses, with the error pointing at the name rather
  than at the cause.

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

This is NOT a type checker. It validates syntax, import usage and duplicate
declarations, and nothing else. Exit 0 does not mean the file compiles: an
undefined type, a dropped struct field or a wrong argument count all pass.
Run "go build ./..." to verify compilation.

Duplicate declarations are reported only when both files could be compiled
together, so mutually exclusive platform variants stay silent. Syntax and import
checks still run on files the current platform excludes.

The unused-import check may miss an import used only in a comment or shadowed by
a local. It never reports one that is genuinely in use.

--naming reports Go initialism violations instead of the syntax checks: an
initialism spelled inconsistently within one identifier. Go writes ID, URL,
JSON and HTTP; userId and dbUrl are reported.

  userID, HTTPHandler, parseJSON   allowed
  userId, dbUrl, HttpHandler       reported, with the fix named

An initialism stays lowercase only at the front of an identifier that already
begins lowercase, so urlPath and jsonBody are correct. Underscores are word
boundaries rather than noise, so usecase_test and RAMFS_MAGIC are left alone.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// --naming is off by default: the check is AST-level validation, and a caller
			// asking for a syntax check should not get naming opinions unless requested.
			namingOnly, _ := cmd.Flags().GetBool("naming")
			return usecase.Check(env.Engine, args[0], env.Stdout, env.Stderr, flagOptions(cmd), namingOnly)
		},
	}

	cmd.Flags().Bool("naming", false, "report Go initialism naming-convention violations instead of syntax checks")
	return cmd
}
