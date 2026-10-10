# Using golens

*Template. Copy into the root of a Go repository as `AGENTS.md`, or merge the sections you
want into the file you already have. Replace `<path>` with your repository root.*

golens reads and edits Go source structurally, so an agent can work on one declaration without
reading a whole file. Prefer it over `grep`, `sed` and reading whole files: it answers the
question directly and returns exact byte offsets for an edit.

```console
$ golens --help                          # once per session
$ golens <command> --help                # before first use of that command
```

If `golens` is not on `PATH`, it is not installed here — stop and say so rather than
substituting `grep`. The per-command help carries the conventions that differ between commands
and will corrupt a file if guessed: byte offsets are half-open and start at a declaration's
*name* rather than its keyword, `check` writes to stderr, and a symbol can be ambiguous.

## The loop

**1. Find the symbol.**

```console
$ golens outline <path> --depth 1
$ golens outline <path> --depth 2      # adds struct fields and parameters
```

`--depth` defaults to unlimited, so omit it when you want everything.

**2. Read it.**

```console
$ golens get <Type>.<Method> <path>/file.go
```

The output is the file's own bytes, so it is safe to write back verbatim.

**3. Place the edit.**

```console
$ golens bounds <Type>.<Method> <path>/file.go --json
# -> {"schema_version":1,...,"start_byte":430,"end_byte":612}
```

Replace exactly `[start_byte, end_byte)`. The span starts at the declaration's name, so for a
type or var your replacement text must not repeat the `type` / `var` / `const` keyword. Writing
back what `get` returned always satisfies this.

**Open the file in binary.** `start_byte` and `end_byte` count *bytes*, not characters. Splicing a
decoded string with those numbers is a different operation and silently corrupts any file
containing a non-ASCII identifier or a BOM:

```python
raw  = open(p, "rb").read()          # correct: raw[430:612]
text = open(p, encoding="utf-8").read()   # wrong: text[430:612] indexes characters
```

`utf-8-sig` is worse — it strips the BOM, shifting every offset by a further three. The failure
raises no exception; the span runs long, the tail of the declaration is dropped, and the parse error
that follows points at the name rather than at the cause. Use `raw[start:end]`, `read_bytes()`, or
`dd bs=1 skip=S count=E-S`.

**4. Validate.**

```console
$ golens check <path>/file.go 2>&1
$ gofmt -l <path>/file.go && go vet <path>/...
$ golens outline <path>/file.go | head -20
```

Confirm the file still parses, still formats, and that its neighbours are intact.

## Things that will waste your time if you do not know them

**`check` writes to stderr.** `golens check .` piped or redirected on stdout alone returns
nothing, which looks exactly like a clean result. Always `2>&1`.

**`check` is not a type checker.** It validates syntax, import usage and duplicate declarations.
Exit 0 does not mean the file compiles — an undefined type or a struct field you accidentally
dropped both pass. `go build ./...` is the only thing that settles it.

**A symbol can be ambiguous.** Platform variants of one method commonly appear in four files.
Ambiguity exits 2, prints nothing to stdout, and lists the candidates on stderr. Pass a
specific file instead of a directory to resolve it.

**Build constraints are ignored.** Every `.go` file is parsed, including files this platform
would not compile, because the question is what the tree contains. `outline` therefore reports
each file's constraint in its header, and you should check it before editing:

```console
$ golens outline <path>
file: filesystems_windows.go
package: main
build: windows
```

A change to a file the current `GOOS` excludes is never compiled, so it will appear to have done
nothing. Verify with `GOOS=<platform> go build ./...`, not plain `go build`.

**Edit the declaration, do not rewrite it.** A `bounds` span replaces one whole declaration. If
you retype a struct body from an outline instead of editing the existing text, fields you did
not notice are silently dropped — and nothing here will tell you. Re-read the original, change
only what the task requires, and confirm the diff shows additions rather than deletions.

**Exit codes.** `0` success · `1` wrong flags or arguments · `2` symbol or file not found, or
ambiguous · `3` check found diagnostics · `4` parse or I/O failure.

**Token budget.** Add `--compact` to any command whose output you intend to read in full. It
removes decoration, never information. `--json` gives a machine-readable payload.

**Never let a diagnostic stand alone.** `check`'s unused-import detection is best-effort and
may miss cases, but it is built never to report an import that is in use. Cross-check anything
surprising against `go build` before acting on it.

**`--naming` is Go's convention, not a local one.** It requires `ID`, `URL`, `JSON` and `HTTP` —
the rule golint popularised and staticcheck carries as ST1003 — so `userId` and `dbUrl` are
reported while `userID` and `HTTPHandler` are correct. It is off by default because most
repositories already satisfy it; turn it on to catch drift.
