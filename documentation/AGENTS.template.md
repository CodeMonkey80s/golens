# Using golens

*Template. Copy into the root of a Go repository as `AGENTS.md`, or merge the sections you
want into the file you already have. Replace `<path>` with your repository root.*

golens reads and edits Go source structurally, so an agent can work on one declaration without
reading a whole file.

Build it once from a local checkout and leave the binary somewhere stable:

```console
$ git clone https://github.com/CodeMonkey80s/golens    # or use an existing checkout
$ cd golens && go build -o /tmp/golens ./cmd/golens
```

Substitute the path to your checkout; the examples below assume the binary is at
`/tmp/golens`.

Run `golens --help`, then `golens <command> --help`, before first use. The per-command help
carries the conventions that differ between commands and will corrupt a file if guessed: byte
offsets are half-open and start at a declaration's *name* rather than its keyword, `check`
writes to stderr, and a symbol can be ambiguous.

## The loop

**1. Find the symbol.**

```console
$ /tmp/golens outline <path> --depth 1
$ /tmp/golens outline <path> --depth 2      # adds struct fields and parameters
```

`--depth` defaults to unlimited, so omit it when you want everything.

**2. Read it.**

```console
$ /tmp/golens get <Type>.<Method> <path>/file.go
```

The output is the file's own bytes, so it is safe to write back verbatim.

**3. Place the edit.**

```console
$ /tmp/golens bounds <Type>.<Method> <path>/file.go --json
# -> {"schema_version":1,...,"start_byte":430,"end_byte":612}
```

Replace exactly `[start_byte, end_byte)`. The span starts at the declaration's name, so for a
type or var your replacement text must not repeat the `type` / `var` / `const` keyword. Writing
back what `get` returned always satisfies this.

**4. Validate.**

```console
$ /tmp/golens check <path>/file.go 2>&1
$ gofmt -l <path>/file.go && go vet <path>/...
$ /tmp/golens outline <path>/file.go | head -20
```

Confirm the file still parses, still formats, and that its neighbours are intact.

## Things that will waste your time if you do not know them

**`check` writes to stderr.** `golens check .` piped or redirected on stdout alone returns
nothing, which looks exactly like a clean result. Always `2>&1`.

**A symbol can be ambiguous.** Platform variants of one method commonly appear in four files.
Ambiguity exits 2, prints nothing to stdout, and lists the candidates on stderr. Pass a
specific file instead of a directory to resolve it.

**Build constraints are ignored.** Every `.go` file is parsed, including files this platform
would not compile, because the question is what the tree contains. The consequence for you:
`outline` lists all the platform variants, and the output does not show a file's `//go:build`
line. Read that line in the file before editing — a change to an excluded variant will not be
compiled and will appear to have done nothing.

**Exit codes.** `0` success · `1` wrong flags or arguments · `2` symbol or file not found, or
ambiguous · `3` check found diagnostics · `4` parse or I/O failure.

**Token budget.** Add `--compact` to any command whose output you intend to read in full. It
removes decoration, never information. `--json` gives a machine-readable payload.

**Never let a diagnostic stand alone.** `check`'s unused-import detection is best-effort and
may miss cases, but it is built never to report an import that is in use. Cross-check anything
surprising against `go build` before acting on it.
