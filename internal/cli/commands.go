package cli

import (
	"errors"

	"github.com/spf13/cobra"

	"github.com/CodeMonkey80s/golens/internal/format"
	"github.com/CodeMonkey80s/golens/internal/mcp"
	"github.com/CodeMonkey80s/golens/internal/usecase"
)

// flagOptions reads the persistent global flags into format.Options.
func flagOptions(cmd *cobra.Command) format.Options {
	jsonOut, _ := cmd.Flags().GetBool("json")
	compact, _ := cmd.Flags().GetBool("compact")
	return format.Options{JSON: jsonOut, Compact: compact}
}

// fileOptions reads the traversal flags shared by the structural commands.
func fileOptions(cmd *cobra.Command) usecase.FileOptions {
	skipTestdata, _ := cmd.Flags().GetBool("skip-testdata")
	skipIgnored, _ := cmd.Flags().GetBool("skip-ignored")
	return usecase.FileOptions{SkipTestdata: skipTestdata, SkipIgnored: skipIgnored}
}

func resolveOptions(cmd *cobra.Command) usecase.ResolveOptions {
	opts := fileOptions(cmd)
	return usecase.ResolveOptions(opts)
}

func addTraversalFlags(cmd *cobra.Command) {
	cmd.Flags().Bool("skip-testdata", false, "skip directories named testdata, as the go tool does")
	cmd.Flags().Bool("skip-ignored", false, "skip files and directories whose names begin with _")
}

func newOutlineCmd(env Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "outline <file|dir>",
		Short: "Generate a structural hierarchy map of a file or directory",
		Long: `Generate a structural hierarchy map of a file or directory.

  Depth   1 = declarations only
          2 = adds struct fields and parameters
          default -1 (unlimited); --depth 3 and above add nothing further
  Build   a file's effective build constraint, when it has one
  Lists   every .go file in a directory, including platform variants
  Skip    --skip-testdata / --skip-ignored opt into the build's view

Structural, not semantic: imports, types, functions, methods, vars and consts.

A directory argument is resolved before it is walked, so a symlinked source
tree is listed as the directory of files it is rather than reported as empty.
Paths in the output start from the directory as you named it. Symlinks inside
a tree are not followed.

Build constraints are ignored on purpose -- the question is what the tree
contains, not what this machine compiles. The header therefore reports each
file's constraint so you can tell which edits the current GOOS will compile:

  file: filesystems_windows.go
  package: main
  build: windows

A file that does not parse is left out of the outline and named on stderr,
and the run still exits 4: one broken file in a tree of thousands must not
hide the rest. stdout carries every file that did parse, so a --json payload
is still complete and pipeable. Pointed at a single broken file there is
nothing to salvage and the parse error is the whole answer. To survey a tree
for structural problems, use "golens check", which reports a parse failure as
one diagnostic among many.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			depth, _ := cmd.Flags().GetInt("depth")
			return usecase.OutlineWithFileOptions(env.Engine, args[0], depth, env.Stdout, flagOptions(cmd), fileOptions(cmd))
		},
	}

	cmd.Flags().Int("depth", usecase.DepthUnlimited, "restrict AST hierarchy recursion depth")
	addTraversalFlags(cmd)
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
  Partial a file that will not parse is skipped, not fatal

Safest round trip available: the text get returns is exactly the span bounds
reports, so replacing one with the other cannot corrupt the file.

Ambiguity is common for the platform variants of one method. It exits 2, writes
nothing to stdout, and lists candidates on stderr. Pass a file, not a directory:

  golens get Mount.Stat mounts_linux.go

A method promoted from an embedded field belongs to the type that declares it.
Given "type Derived struct { Base }", "get Derived.Lock" exits 2 and "get Lock"
returns Base.Lock. golens resolves declarations, not the method set.

A directory may hold package p beside an external test package p_test, and
both may declare the same name without either redeclaring it. That exits 2
as well, but it is reported as a name found in two packages rather than as
an ambiguity, because there is no duplicate to fix. Use p.Name to select one
package, or pass the file.

When two directories share a package name, --package-dir narrows the search
to one directory; without it the package-qualified form is ambiguous.

A file that does not parse is skipped, exactly as for outline. The symbol is
resolved from the files that did parse, the skipped ones are named on stderr,
and the run still exits 4 -- the answer was found, but the search that produced
it was incomplete, and exit 0 would claim otherwise. A symbol that resolves
nowhere still exits 2, and the message says how many files could not be
parsed: it may be sitting in one of them. Pointed at a single broken file there
is nothing to salvage and the parse error is the whole answer.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			boundsOnly, _ := cmd.Flags().GetBool("bounds-only")
			target := args[1]
			if dir, _ := cmd.Flags().GetString("package-dir"); dir != "" {
				target = dir
			}
			return usecase.GetWithOptions(env.Engine, args[0], target, !boundsOnly, env.Stdout, flagOptions(cmd), resolveOptions(cmd))
		},
	}

	cmd.Flags().Bool("bounds-only", false, "omit source text; return only line ranges and byte offsets")
	cmd.Flags().String("package-dir", "", "resolve the symbol only in this directory (useful when packages share a name)")
	addTraversalFlags(cmd)
	return cmd
}

func newBoundsCmd(env Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bounds <symbol> <file|dir>",
		Short: "Calculate precise byte offsets and line bounds for targeted edits",
		Long: `Calculate precise byte offsets and line bounds for targeted edits.

  Bytes   half-open: src[start_byte:end_byte] is exactly the declaration
  Lines   inclusive at both ends; trailing newline is outside the span
  Starts  at the declaration's NAME, not its keyword
  Edit    your replacement text must start at the name too
  Miss    symbol not found OR ambiguous -- both exit 2
  Parse   a file that does not parse exits 4 with the error on stderr
  Search  a directory resolves the symbol across its packages, as get does
  Filter  --package-dir limits the search when two packages share a name
  Skip    --skip-testdata / --skip-ignored opt into the build's view

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
			target := args[1]
			if dir, _ := cmd.Flags().GetString("package-dir"); dir != "" {
				target = dir
			}
			return usecase.BoundsWithOptions(env.Engine, args[0], target, env.Stdout, flagOptions(cmd), resolveOptions(cmd))
		},
	}

	cmd.Flags().String("package-dir", "", "resolve the symbol only in this directory (useful when packages share a name)")
	addTraversalFlags(cmd)
	return cmd
}

func newCheckCmd(env Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "check <file|dir>...",
		Short: "Validate source syntax and detect unused imports or duplicate declarations",
		Long: `Validate source syntax and detect unused imports or duplicate declarations.

  Output   human form: diagnostics to stderr, stdout empty
          --json:    payload to stdout, stderr empty -- do not add 2>&1
  Capture  golens check . 2>&1    -- stdout alone looks like a clean result
  Exit     3 = ran and found a problem in the code
           4 = a file could not be read, or the run is incomplete
  Tags     --tags integration matches "go build -tags integration"
  Limits   unused-import check is best-effort
  Scope    testdata and _-prefixed names are not walked; name a file to check it
  Multi    several targets are de-duplicated and checked in one sorted run

Exit 3 and exit 4 are different questions. 3 is about the source: fix the
syntax, the import or the redeclaration. 4 is about the machine: a dangling
symlink or a permission bit, not a defect to edit. When both occur, both
are reported and the run exits 4 -- told 3, a caller would believe every
file was readable.

A testdata directory is not checked. The go tool ignores those directories,
and names beginning with "_", so no build compiles anything in one, and a finding about a file no build
reaches is not a finding about the source: they hold compiler fixtures
written to be broken, and "fixing" one means editing the bytes that are the
test. The rule is about the walk, not about you -- name a testdata file
directly and it is checked. outline, get and bounds still traverse them,
because they answer what the tree contains, not what a build would reject.

This is NOT a type checker. It validates syntax, import usage and duplicate
declarations, and nothing else. Exit 0 does not mean the file compiles: an
undefined type, a dropped struct field or a wrong argument count all pass.
Run "go build ./..." to verify compilation.

Duplicate declarations are reported only when both files could be compiled
together, so mutually exclusive platform variants stay silent. Syntax and import
checks still run on files the current platform excludes.

The unused-import check may miss an import used only in a comment or shadowed by
a local. It never reports one that is genuinely in use.

Check reports structural faults only. It does not judge style: no naming
convention, no formatting, no idiom. A linter is the tool for that, and it is
something a caller chooses to run rather than something they inherit by asking
a structural question.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			tags, _ := cmd.Flags().GetStringSlice("tags")
			return usecase.CheckTargetsWithOptions(env.Engine, args, usecase.CheckOptions{Tags: tags}, env.Stdout, env.Stderr, flagOptions(cmd))
		},
	}

	cmd.Flags().StringSlice("tags", nil, "comma-separated build tags, as in `go build -tags`")
	return cmd
}

func newMCPCmd(env Env) *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "Serve outline, get, bounds and check over MCP on stdin/stdout",
		Long: `Serve the structural tools over the Model Context Protocol.

The transport is newline-delimited JSON-RPC 2.0 on stdin/stdout. stdout carries protocol
messages only; tool output is returned inside the tool result. This is a stdio server, not a
daemon: closing stdin ends it.

Tools: outline, get, bounds, check. The argument names mirror the CLI flags: path, depth,
json, compact, symbol, target, bounds_only, package_dir, targets, tags, skip_testdata and
skip_ignored.

Point an MCP client at the command:

  {
    "command": "golens",
    "args": ["mcp"]
  }`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if env.Stdin == nil {
				return errors.New("mcp needs stdin")
			}
			return mcp.Serve(env.Stdin, env.Stdout, env.Engine, releaseVersion())
		},
	}
}
