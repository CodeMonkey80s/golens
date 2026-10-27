# golens

Structural AST queries for AI coding agents. Read targeted source instead of whole files, and
edit using byte-accurate bounds.

```
golens outline <file|dir>       structural hierarchy map
golens get <symbol> <file|dir>  exact source of one symbol
golens bounds <symbol> <file|dir> byte and line offsets for an edit
golens check <file|dir>...      fast syntax and namespace validation
golens mcp                      serve the tools over MCP on stdin/stdout
```

Pure Go, no cgo, no daemon, no index. One binary, about 5.4 MiB stripped (`-ldflags="-s -w"`), parses a typical file in tens of
microseconds.

---

## Why

An agent that needs one function has two options: read the whole file and pay for it in context,
or grep and hope. `golens` parses the file properly and answers structurally, then hands back
byte offsets precise enough to edit with.

```console
$ golens outline internal/domain/domain.go --depth 1
file: internal/domain/domain.go
package: domain

IMPORTS
  • fmt
  • strconv

TYPES & STRUCTS (lines 18-324)
  • type Kind string (line 18)
  • type Bounds struct (lines 42-47)
  • type Node struct (lines 103-130)
  • type Symbol struct (lines 189-209)
  • type Document struct (lines 218-245)
  • type Engine interface (lines 292-294)
  • type ErrorExit int (line 300)
  • type LocatedError struct (lines 319-324)

FUNCTIONS & METHODS (lines 51-330)
  • func (b Bounds) Valid() bool (lines 51-53)
  • func (b Bounds) LineCount() int (lines 57-62)
  • func (b Bounds) ByteLen() int (lines 65-70)
  • func (b Bounds) LineRange() string (lines 75-80)
  • func (b Bounds) String() string (lines 82-84)
  • func (b Bounds) Source(src []byte) string (lines 92-97)
  • func (n Node) Declaration() string (lines 139-171)
  • func (n Node) Keyword() string (lines 178-183)
  • func (s Symbol) Declaration() string (lines 213-215)
  • func (d *Document) Symbol(name string) (Symbol, bool) (lines 252-259)
  • func (d *Document) Lookup(name string) []Symbol (lines 267-275)
  • func (d *Document) RootNode() Node (lines 278-284)
  • func (e ErrorExit) Code() int (line 310)
  • func (e LocatedError) String() string (lines 328-330)

VARS & CONSTS (lines 21-307)
  • const KindPackage Kind (line 21)
  • const KindImport Kind (line 22)
  • const KindType Kind (line 23)
  • const KindFunc Kind (line 24)
  • const KindMethod Kind (line 25)
  • const KindVar Kind (line 26)
  • const KindConst Kind (line 27)
  • const KindField Kind (line 28)
  • const ExitOk ErrorExit (line 303)
  • const ExitUsage (line 304)
  • const ExitNotFound (line 305)
  • const ExitDiagnostics (line 306)
  • const ExitParse (line 307)
```

Note `type ErrorExit int` and `const ExitOk ErrorExit` — the type and its typed constant, which is
where a declaration head that dropped half the source would be obvious.

```console
$ golens get Bounds.ByteLen internal/domain/domain.go
File: internal/domain/domain.go
Symbol: Bounds.ByteLen
Lines: 65-70 (6 lines)

func (b Bounds) ByteLen() int {
	if b.EndByte <= b.StartByte {
		return 0
	}
	return b.EndByte - b.StartByte
}
```

```console
$ golens bounds Bounds.ByteLen internal/domain/domain.go
Bounds.ByteLen  lines 65-70  bytes 1996-2106
```

That last line is the point. To edit `ByteLen` you seek to 1996 and write 110 bytes — exactly
the length of the text `get` returned. No fuzzy matching, no re-reading the file to find out
where you ended up.

---

## Install

Build from a checkout:

```console
$ git clone https://github.com/CodeMonkey80s/golens
$ cd golens && go build -o golens ./cmd/golens
```

Or install it directly:

```console
$ go install github.com/CodeMonkey80s/golens/cmd/golens@latest
```

Tagged releases also attach `linux`, `darwin` and `windows` archives for `amd64`/`arm64`, with a
`checksums.txt`, for machines that do not have a Go toolchain.

Requires the Go version in `go.mod` (currently 1.24). Builds with `CGO_ENABLED=0`.

A `go install` binary is larger because it is not stripped; both forms are inside the 15 MiB budget.

### Version

`--version` reports the version the **tag** decides. Nothing in the source states a release
number — it was `const Version = "0.1.0"` once, which meant every build ever produced reported
v0.1.0, including one built after v0.2.0 shipped. Tags from v0.2.7 onward carry the fix.

For a release build, stamp it at link time:

```console
$ go build -ldflags "-X github.com/CodeMonkey80s/golens/internal/cli.Version=$(git describe --tags)" -o golens ./cmd/golens
```

With no flags the binary still knows: the toolchain records the module version, so
`go install …@v0.1.0` reports `v0.1.0` by itself. A build from a checkout reports what Go
recorded for that commit — a pseudo-version like `v0.1.1-0.20261012165945-1cd5ab9df783`
when the tree is clean, with `+dirty` appended when it is not, and `(devel)` only where
there is no VCS information at all (a source archive, or `-buildvcs=false`).

### Making an agent use it

golens is built for agents, so the interesting question is how one finds it. There is no plugin and
nothing to register — **copy `documentation/AGENTS.template.md` into your repository's root as
`AGENTS.md`.** It is short, it points at `--help` rather than restating conventions, and it teaches an
agent to use golens when it is on `PATH` and to work normally when it is not.

**That works only if your agent actually loads `AGENTS.md`, and not all of them do.** Most coding
agents read it once, at session start, from the directory they were launched in. If yours does:

```console
$ golens outline . --depth 1     # ILLUSTRATIVE: run in your repository, not this one
```

If it does not, say so in your instructions or your prompt:

```
Read AGENTS.md in this directory first.
```

**Launch the agent in the repository you want it to read.** A session started elsewhere and later
`cd`'d in will not pick the file up, and the agent will go to `grep` without ever hearing of golens.
This is measured, not assumed: across six cold-start runs, one found golens unprompted from the
template and two needed it named in the prompt, with the same file in the same place. See
`documentation/COLDSTART.md` for the runs.

---

## Commands

### `outline <file|dir>`

Structural map of a file or directory: imports, types, funcs and methods, vars and consts.

Struct fields are listed one per name, so `A, B int` is two entries rather than one. An
interface lists its method set — for an interface that *is* the content of the type. A type
union like `interface{ ~int | ~string }` is a constraint rather than a method set, so it lists
nothing.

| Flag | Effect |
|---|---|
| `--depth <int>` | Recursion limit. `1` shows declarations only, `2` adds fields and parameters. Default unlimited |
| `--skip-testdata`, `--skip-ignored` | Omit directories the go tool ignores, at the cost of a source-only view |
| `--json` | Array of per-file objects |
| `--compact` | One record per line, no decoration |

A directory is walked recursively. `.git`, `vendor`, `node_modules` and hidden directories are
skipped, and only `.go` files are read. Files are emitted in lexical order, so output is
deterministic.

Every file is listed whatever its constraints. When a file is not built everywhere, the header
reports its **effective constraint** — the conjunction of the `//go:build` expression, the
filename suffix (`_windows`, `_amd64`, `_linux_amd64`) and `cgo`:

```text
file: filesystems_windows.go
package: main
build: windows
```

A file with no constraint has no `build:` line. The line is a warning, not a filter: the file is
still in the outline because the question is what the tree contains.

A directory argument is resolved before it is walked, so a symlinked source tree is listed as the
directory of files it is rather than reported as empty. Paths in the output start from the
directory as you named it. Symlinks *inside* a tree are not followed.

`--json` always returns an array, even for a single file, so the schema never changes shape with
the input.

### `get <symbol> <file|dir>`

The exact source of one declaration.

| Flag | Effect |
|---|---|
| `--bounds-only` | Omit the source, return only ranges and offsets |
| `--package-dir` | Resolve only in this directory; useful when two packages share a name |
| `--skip-testdata`, `--skip-ignored` | Omit ignored directories; useful when fixtures do not parse |
| `--json`, `--compact` | As above |

Symbols may be a bare name (`Check`) or qualified for methods (`UserStore.GetUser`). Both
exported and unexported identifiers resolve. Passing a directory searches the whole package.

A bare method name is accepted when it is unique. When two receivers share a name the command
exits `2` and lists the candidates rather than guessing.

Package-qualified lookup (`Package.Symbol`) resolves `Symbol` in the named package. This is
distinct from `Type.Method`, which selects a method within a type. The package name must match a
package parsed in the search. If the first segment matches both a package and a type,
`Type.Method` is tried first; package-qualified lookup applies only when it finds nothing.

A directory may hold `package p` beside an external test `package p_test`, and both may declare
the same name without either redeclaring anything. That is reported separately — as a name found
in two packages, not as an ambiguity — and the remedy is `p.Name` or a specific file.

The output reports the declaration's **effective build constraint** when it has one, from the
filename, the `//go:build` line, or cgo. A symbol in a Windows-only file therefore says
`Build: windows (not buildable on this platform)` even when you are on Linux, and JSON carries
`build` plus `buildable_now`. An ambiguous candidate list marks each file with its constraint:
`/src/a_linux.go: F  [build: linux]` beside `/src/a_windows.go: F  [build: windows]`. These paths
are illustrative, not a file in this repository.

`get` resolves every file in the tree whatever its constraints — the question is what the tree
contains, not what this machine compiles. The build line is the warning that the edit will not be
compiled here.

A file that will not parse is skipped rather than fatal, as with `outline`. The symbol is resolved
from the files that did parse, the skipped ones are named on stderr, and the run **still exits
`4`** — the answer was found, but the search that produced it was incomplete, and exit `0` would
claim otherwise. A symbol that resolves nowhere still exits `2`, and says how many files could not
be parsed, because it may be sitting in one of them.

### `bounds <symbol> <file|dir>`

Byte and line offsets for an edit. A directory resolves the symbol through the same search as `get`; the JSON payload is identical to `get --bounds-only --json`. The `--package-dir` flag narrows the search when two packages share a name. `--skip-testdata` and `--skip-ignored` apply to the search too.

### `check <file|dir>...`

Fast validation — no type checker, no module resolution.

| Flag | Effect |
|---|---|
| `--json`, `--compact` | As above |
| `--tags` | Custom build tags, as in `go build -tags`; a file guarded by one participates in duplicate detection |

`check` accepts several files or directories in one run; they are de-duplicated and sorted, so
overlapping targets do not repeat a diagnostic. `check` reports structural faults only. It does not judge style: no naming convention, no
formatting, no idiom. Those belong to a linter, which is something a caller opts into rather
than something they inherit by asking a structural question.

Checks performed:

- Fatal parse errors, in `file:line:col: message` form
- Unused imports, including dot imports (`import . "pkg"`)
- Duplicate top-level declarations, within one file or across a package
- Malformed import paths

```console
$ golens check user.go
user.go:4:1: imported and not used: "fmt"
user.go:5:1: imported and not used: "os"
$ echo $?
3
```

Every diagnostic carries `file:line:column`. The column is the parser's position for a syntax error
and `1` for the checks that report a declaration rather than a position.

**Exit 3 means the code has a problem; exit 4 means the tool could not read something.** A syntax
error, unused import or duplicate declaration exits `3`. A file that could not be opened — a
dangling symlink, a permission bit — exits `4`, the same code `outline` gives for the same file.
When both happen in one run, both texts are reported and the run exits `4`, because a caller told
`3` would believe every file was readable.

**Out of scope by design:** unreachable code, type-level conflicts, and module import resolution.
The last two need `go/types` and a module graph, which cannot coexist with the stateless,
sub-millisecond design. See `documentation/specification.md` §2.4.

The unused-import check is lexical and deliberately biased. It can miss an import whose name
appears only in a comment or is shadowed by a local variable. It will not report an import that
is actually in use — that error would break a build, while a miss is caught by `go build` anyway.

---

## Byte offsets

**Byte ranges are half-open. Line ranges are inclusive at both ends.**

```go
src[start_byte:end_byte]   // exactly the declaration
```

`start_byte` is its first byte; `end_byte` is one past its last. The terminating newline falls
*outside* the span, so overwriting `[start, end)` leaves the file's line structure intact.

### The span starts at the name, not the keyword

For a **function** the span starts at `func`. For a **type, var or const** it starts at the
*name*, so the `type` / `var` / `const` keyword is outside the range:

```go
type User struct{ ... }   // start_byte points at 'U' in User, not at 'type'
func NewUser() *User {}    // start_byte points at 'func'
```

This is deliberate. `const ( A = 1; B = 2 )` is a single declaration clause, and spanning the
whole clause would give `A` and `B` identical bounds — editing one would overwrite the other.
Starting at the name is what keeps every name in a grouped declaration independently
addressable.

**The practical consequence:** replacement text must start at the name too. Writing back what
`get` returned is always safe, because `get` returns exactly the span. Writing a fresh
declaration is not:

```go
// bounds ColorRGB  →  start_byte at 'C' of ColorRGB
// WRONG: replaces the span with "type ColorRGB struct {...}" → "type type ColorRGB struct {...}"
// RIGHT: replaces the span with "ColorRGB struct {...}"
```

`end_line` is derived from `end_byte - 1`, not from the AST end position. A declaration ending at
column 1 of the next line reports the line it actually ends on.

This invariant is tested as a property, not a snapshot: for every symbol in every fixture,
`file[start_byte:end_byte]` equals the extracted source byte for byte. A round-trip test then
performs a real overwrite and re-parses the result.

---

## Exit codes

Agents should branch on these.

| Code | Meaning |
|---|---|
| `0` | Success |
| `1` | Usage error — wrong flags or arguments |
| `2` | Symbol or file not found, or ambiguous |
| `3` | `check` found diagnostics |
| `4` | Parse or I/O failure |

A missing file is `2`, not `4`: both mean "you named something that is not there". `4` is
reserved for a file that exists but cannot be processed.

---

## JSON

Every payload carries `"schema_version": 1` as its first field, so a consumer can detect format
drift instead of silently mis-parsing.

Payloads go to **stdout**; diagnostics and errors go to **stderr**. In `--json` mode stdout
carries only the payload, so this works:

```console
$ golens bounds GetUser user.go --json | jq -r .start_byte
430
```

`--compact` wins over pretty-printing and emits non-indented JSON. It removes decoration, never
information.

### Machine-readable errors

`--json-errors` writes failures to **stderr** as a JSON object instead of prose:

```console
$ golens get Nope . --json-errors   # ILLUSTRATIVE
{
  "schema_version": 1,
  "error": {
    "kind": "not_found",
    "message": "symbol \"Nope\" not found",
    "exit_code": 2
  }
}
```

`kind` is `not_found`, `ambiguous`, `usage`, `parse`, `io` or `parse_or_io`; ambiguous failures also carry
`candidates`, and partial searches carry `skipped`/`total`. The error stays on stderr because a
partial `outline` may already have written a valid array to stdout.

---

## Agent workflow

```console
# 1. What is in this file?
$ golens outline user.go --depth 1

# 2. Show me the one function I need.
$ golens get UserStore.GetUser user.go

# 3. Is the file even valid?
$ golens check user.go

# 4. Where do I write my replacement?
$ golens bounds UserStore.GetUser user.go --json
# -> {"schema_version":1,...,"start_byte":430,"end_byte":612}

# 5. Seek to 430, write 182 bytes — end_byte minus start_byte, which is
#    exactly the length of the text `get` returned in step 2.
```

The offsets above are from the specification's sample file. Run the commands against your own file
and use the numbers it gives you; the property that matters is that `end_byte - start_byte` equals
the length of what `get` printed.

Read with `outline`, extract with `get`, edit with `bounds`, validate with `check`.

---

## Embedding

The root package is a small supported API over the same engine:

```go
import (
    "bytes"
    "github.com/CodeMonkey80s/golens"
)

var out bytes.Buffer
err := golens.Get("UserStore.GetUser", "user.go", &out, golens.Options{})
code := golens.ExitCodeFor(err) // 0, 1, 2, 3 or 4
```

`Outline`, `Get`, `Bounds` and `Check` write the same human or JSON output as the CLI;
`Options` carries `JSON`, `Compact`, `Depth`, `BoundsOnly`, `PackageDir`, `Tags`, and the two
traversal flags. It is deliberately output-oriented: the internal AST types are not public.

## MCP

`golens mcp` serves `outline`, `get`, `bounds` and `check` at
[Model Context Protocol](https://modelcontextprotocol.io) over stdin/stdout. Point a client at
the command and pass `mcp` as its argument; the server returns the same payloads as the CLI in a
`text` content block and sets `isError` when the command fails.

The transport is newline-delimited JSON-RPC 2.0. stdout carries protocol messages only; the
server exits when stdin closes.

---

## Development

```console
$ go test -count=1 ./...
$ go vet ./...
$ go build -o golens ./cmd/golens
```

**Use `-count=1`.** Three packages assert facts about the build graph or the source tree rather
than about their own test inputs — `internal/arch` shells out to `go list -deps`, and
`selfcheck_test.go` and `TestRepositoryPassesItsOwnCheck` walk the tree. Go's test cache does not
invalidate on those, so a stale `ok` is possible. This has hidden a real failure once.

A release build adds the version stamp and strips the binary:

```console
$ go build -ldflags "-s -w -X github.com/CodeMonkey80s/golens/internal/cli.Version=$(git describe --tags)" -o golens ./cmd/golens
```

Cut one by tagging the commit and pushing the tag. Nothing in the source changes, so there is
nothing to remember to bump. The tag triggers the release workflow: archives, checksums, notes
from `CHANGELOG.md`, and a build-provenance attestation.

`golens check ./internal ./cmd` exits 0 on this repository. The tool checks itself in CI,
which is how a false positive in its namespace check was caught.

`golangci-lint` runs in CI too. Among the rules it enforces is `revive`'s `var-naming`, which
holds this repository to Go's initialism convention (`userID`, `HTTPHandler`, not `userId`,
`HttpHandler`). Naming is the linter's job, not the tool's: `golens` describes code, it does not
grade it.

CI runs on every push and pull request: tests on the Go floor and current stable, the race
detector, `go vet`, a `gofmt` gate, the 15 MB binary budget, the no-cgo check, `golens` on its own
source, macOS tests, and cross-builds for linux/darwin/windows. A nightly job checks the standard
library and runs longer fuzz campaigns.

### Layout

```
golens.go              public embedding API (output-oriented)
cmd/golens/            entry point; composes the engine
internal/domain/       Bounds, Node, Symbol, Document, Engine — stdlib only
internal/engine/goast/ the only package importing go/parser, go/ast, go/token
internal/usecase/      command orchestration and error classification
internal/cli/          cobra wiring and exit-code mapping; imports no engine
internal/mcp/          MCP stdio server over the same usecase calls
internal/format/       human, JSON and compact renderers
internal/arch/         structural tests enforcing the boundaries above
internal/constructs/   compiled declarations for the corpus walk
internal/testfixture/  shared fixtures
```

Import direction is one-way: `cli → usecase → domain`, with `engine/goast` depending only on
`domain`. The root embedding package and `internal/mcp` sit above usecase and do not add engine
dependencies to the CLI. `internal/arch` enforces the boundaries as a test rather than as a
convention, so a violation fails the build.

### Performance

The **budgets** are the contract and are asserted in CI; the measured column is one machine and
will differ on yours.

| Budget | Measured here |
|---|---|
| Read + parse, single file — 5 ms P95 | measured by the benchmark below |
| Binary — 15 MiB | about 5.4 MiB stripped (`-ldflags="-s -w"`) |

Reproduce with `go test -run XXX -bench Parse -benchtime 500x ./internal/engine/goast`. Expect
run-to-run variation of roughly ±20%, so treat a single figure as indicative and the budget as the
thing to hold to.

Directory parsing is concurrent above eight files and sequential below, so the single-file path
never pays for goroutines. Concurrency is unobservable: output is byte-identical either way,
which is tested by rendering with `GOMAXPROCS` pinned to 1 and comparing.

### Further reading

If you are wiring golens into a repository you work on, these two matter most:

| Document | Contents |
|---|---|
| `documentation/AGENTS.template.md` | Copy into your repo's root as `AGENTS.md` — the traps that cost this project real bugs |
| `TESTING.md` | An 18-case manual plan; run it against a real repository before trusting the tool |

The rest:

| Document | Contents |
|---|---|
| `documentation/specification.md` | The normative spec |
| `documentation/decisions.md` | Design decisions, frozen interfaces, acceptance criteria |
| `documentation/COLDSTART.md` | A scored cold-start exercise that tests the instructions rather than the tool |
| `AGENTS.md` | Where the project stands, the traps, and what is deliberately not done |
| `documentation/implementation-plan.md` | Session plan and sequencing |
| `documentation/retrospective-s1.md` | The `go/ast` position model; why tests lie |
| `documentation/retrospective-s2.md` | Sentinel values; three bugs in green tests |
| `documentation/retrospective-s4.md` | Error bias per check; running a checker on itself |

---

## Licence

MIT — see [LICENSE](LICENSE). Copyright is held by the maintainer.

## Acknowledgements

Written with assistance from **multiple LLMs** (via [OpenCode](https://opencode.ai)).

Scope, design decisions, naming, module path and review were directed by the maintainer; the
per-session retrospectives in `documentation/` record what was learned along the way.
