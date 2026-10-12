# SPEC.md: golens (v0.1 MVP)

golens is a high-performance, stateless CLI tool written in Go that provides AI coding agents and developers with structural AST queries (outline, get, bounds, check). It replaces full-file reads with targeted, token-efficient source extraction and byte-accurate editing bounds.

## 1. System Architecture & Design Goals

### 1.1 Core Principles

- **Stateless & On-Demand**: Zero background daemons, zero persistent indexing, and zero database overhead. Each invocation parses target files in-memory in under 5ms.
- **Pure Go (CGO_ENABLED=0)**: Built strictly using standard library packages (go/parser, go/ast, go/token) for fast cross-platform compilation without Cgo or external C libraries.
- **Token-Optimized for Agents**: Output schemas are designed for minimal prompt prefill overhead, providing exact line ranges and byte offsets for programmatic edits.

### 1.2 Target Engine Scope

- **Language Support (v0.1 MVP)**: Go source code (.go).
- **Parser Architecture**: Modular Engine interface to allow seamless addition of Tree-Sitter grammars for non-Go languages in future releases (v0.2+).

## 2. CLI Command Specifications

All commands accept two global flags:

- `--json`: Outputs machine-readable JSON payload.
- `--compact`: Minifies output formatting to minimize context token usage.

**Exit codes.** `0` success · `1` usage error · `2` symbol or file not found or ambiguous · `3` `check` found diagnostics · `4` parse or I/O failure. Payloads are written to stdout, diagnostics to stderr. When `--json` and `--compact` are combined, `--compact` takes precedence and emits non-indented JSON.

**Schema versioning.** Every JSON payload includes `"schema_version": 1` as its first field.

**Version reporting.** `golens --version` prints the release identifier on one line, prefixed by
the binary name, and exits `0`. The value is **not** stated in the source: it is stamped at link
time with `-ldflags "-X …/internal/cli.Version=<v>"`, and when nothing is stamped the binary
reports the module version the Go toolchain recorded — the tag for `go install module@v0.1.0`, or
a pseudo-version derived from the nearest tag for a build from a checkout. The release tag is
therefore the single source of a release number, and no build can report a version that has
drifted from its tag.

**`--compact` semantics.** Compact output is a lossless subset of the human output: the same information with decoration removed. It is a formatting flag, not a second code path.

### 2.1 `golens outline`

Generates a structural hierarchy map of a file or directory.

**Usage**

```bash
golens outline <file|dir> [flags]
```

**Flags**

- `--depth <int>`: Restricts AST hierarchy recursion depth (default: unlimited).
- `--json`: Returns structured JSON array.

**Directory input.** The path is traversed recursively; `.git`, `vendor`, `node_modules` and
hidden directories are skipped, and only `.go` files are considered. Files are emitted in lexical
path order, and each block repeats the `file:` header. Output is deterministic: identical input
always yields byte-identical output.

**JSON shape.** Always an array of per-file objects, including for a single file, so the schema never varies with input type.

**Node fields carry the whole declaration head.** Each node carries `name`, `kind`, `detail`, `start_line`, `end_line` and `children`, plus three fields that are not decoration:

| Field | Present on | Value |
|---|---|---|
| `type_params` | generic types and functions | the bracketed list verbatim, `"[T any]"` |
| `receiver` | methods | the receiver as the source spells it, `"s *UserStore"` |
| `is_alias` | type aliases | `true` for `type A = B`, absent for `type A B` |

`name` and `detail` alone cannot express any of the three, so a consumer that reconstructs a declaration head from a node writes a different program than the source describes: `type Stack struct` for `type Stack[T any] struct` does not compile, and `type Alias Stack[int]` for `type Alias = Stack[int]` compiles as a defined type, which is the more dangerous of the two. An alias and a defined type over the same underlying type are otherwise byte-identical nodes, so `is_alias` is the only thing distinguishing them. Each field is omitted when empty or false, so a file with no generics, no methods and no aliases pays nothing.

**`--depth`.** Counts container nesting. Depth 1 emits package-level declarations only — types, funcs, methods, vars and consts — with children suppressed. Depth 2 additionally emits struct fields and the method set of an interface. The hierarchy is two levels deep in practice, so depth 3 and above produce output identical to depth 2 and to the unlimited default; a value of 0 is treated as 1.

**An interface's method set is its content.** An interface is listed with its methods beneath it, each with its own span, so a caller can see what a type requires without opening the file. An embedded interface is listed by name, because it is a method of the enclosing one. A **type union is not a method** — `interface{ ~int | ~string }` is a constraint and contributes no entries, since its terms are type requirements rather than declarations.

**Interface methods are spelled as the source spells them.** A method renders as `Close() error`, with the parameter list attached directly to the name and no `func` keyword, because `func` is a syntax error inside an interface body. This differs from a struct field's `Name Type` spacing, and the distinction is deliberate: a declaration head that compiles is the point of the whole exercise.

**Line ranges.** A declaration spanning several lines renders as `(lines 5-8)`; a single-line declaration renders as `(line 10)`.

**Build constraints.** Every `.go` file is parsed whatever its constraints, because the question is
what the tree contains rather than what one machine compiles. A file carrying a `//go:build`
expression therefore reports it on a third header line, and an untagged file has no such line. A
consumer editing a platform-specific file should check that line before assuming the current
`GOOS` will compile the change.

**Human Output Format (`golens outline user.go`)**

```text
file: user.go
package: store

IMPORTS
  • context

TYPES & STRUCTS (lines 5-10)
  • type User struct (lines 5-8)
  • type UserStore struct (lines 10)

FUNCTIONS & METHODS (lines 12-25)
  • func NewUserStore() *UserStore (lines 12-15)
  • func (s *UserStore) GetUser(ctx context.Context, id int) (*User, error) (lines 17-25)
```

For a file with a build constraint:

```text
file: filesystems_windows.go
package: main
build: windows

IMPORTS
  • registry

FUNCTIONS & METHODS (lines 16-56)
```

**Unparseable files.** A file that does not parse is omitted from the outline, named on stderr,
and the run still exits `4`. Every other file is outlined first, so one broken file cannot hide
the rest of a tree — and because stdout is still a complete JSON payload when `--json` is used, a
machine consumer is never handed a truncated document. Pointed at a single broken file there is
nothing partial to deliver, so the parse error is reported alone.

The summary line states the outcome rather than the cause: **N of M files could not be read and
are missing from the output**. The list holds both files that do not parse and files that cannot
be opened, and an unreadable file is not a parse failure — reporting it as one sends a caller to
repair the syntax of a file it has not been able to read. Each entry carries its own cause, so
the two are distinguishable: a syntax error names line and column, an I/O failure names the
underlying error.

**Directory arguments follow symlinks.** A directory given on the command line is resolved before
it is walked, so a symlinked source tree is outlined as the directory of files it is rather than
reported as empty. Paths in the output start from the directory as named, not from its resolved
target, so a caller can open what it was shown. Symlinks *within* a tree are not followed.

**Every struct field is listed, one entry per name.** A field written with several names,
`A, B int`, declares two fields and produces two entries — in the human, JSON and compact
renderings alike. Each entry carries the span of its own name, so the two are independently
addressable rather than sharing the bounds of the whole line.

### 2.2 `golens get`

Extracts the exact source code content for a specific AST symbol.

**Usage**

```bash
golens get <symbol> <file|dir> [flags]
```

**Selection Rules**

- `<symbol>` matches any exported or unexported package-level identifier (func, type, method receiver, var, const).
- Format for methods: `Type.Method` (e.g., `UserStore.GetUser`).
- If `<dir>` is provided, golens resolves the symbol across the package namespace.

**Flags**

- `--bounds-only`: Excludes source code text; returns only line ranges and byte offsets.
- `--json`: Returns a structured JSON object.

**Offsets are half-open.** `start_byte` is the first byte of the symbol and `end_byte` is one past its last byte, so `file[start_byte:end_byte]` is exactly the extracted source. Line numbers are inclusive at both ends, matching the `(9 lines)` count in the example below. The span is derived purely from the AST node, so the terminating newline is excluded and an overwrite of `[start_byte, end_byte)` preserves line structure. When `--bounds-only` is combined with `--json`, the payload is identical to `golens bounds`.

**The span starts at the name, not the keyword.** For a func the span starts at `func`; for a type, var or const it starts at the declared name, leaving the `type` / `var` / `const` keyword outside the range. Grouped declarations make this unavoidable — `const ( A = 1; B = 2 )` is one clause, so a clause-wide span would give `A` and `B` identical offsets and an edit to one would overwrite the other. A consumer writing replacement text must therefore begin at the name; writing back the text `golens get` returned for the same symbol always satisfies this.

**Symbol ambiguity.** A bare method name is accepted when it is unique in the package. Any ambiguity exits `2` and lists the candidates on stderr rather than guessing.

**Packages are distinct namespaces.** A directory may hold `package p` beside an external test `package p_test`, and both may declare the same name without either redeclaring anything. When a bare name resolves in more than one package, the run exits `2` and reports the packages and their files — **it does not call this ambiguous**, because there is no duplicate declaration to fix. A qualified name (`Type.Method`) selects a method within a type and cannot select a package, so the stated remedy is a file.

**Unparseable files in the search.** A file that does not parse is skipped rather than fatal, exactly as for `outline`. The symbol is resolved from the files that did parse, and the skipped files are named on stderr with the run still exiting `4` — the answer was found, but the search that produced it was incomplete, and exit `0` would claim otherwise. When the symbol resolves nowhere, the run still exits `2`, and the message says how many files could not be parsed: "not found" from a partial search is not the same answer as "not found" from a complete one, and the declaration may sit in a file that would not parse. Pointed at a single broken file there is nothing to salvage, so the parse error alone is the answer.

**Human Output Format (`golens get UserStore.GetUser user.go`)**

```text
File: user.go
Symbol: UserStore.GetUser
Lines: 17-25 (9 lines)

func (s *UserStore) GetUser(ctx context.Context, id int) (*User, error) {
    s.mu.Lock()
    defer s.mu.Unlock()
    if u, ok := s.cache[id]; ok {
        return u, nil
    }
    return nil, ErrNotFound
}
```

### 2.3 `golens bounds`

Calculates precise byte offsets and line bounds for targeted programmatic edits. Used by agents to construct exact seek and overwrite commands without fuzzy searching.

**Usage**

```bash
golens bounds <symbol> <file> [flags]
```

**Flags**

- `--json`: (Default behavior implicitly) Returns byte offsets.

**Human output.** `golens bounds <symbol> <file>` prints a single line: `UserStore.GetUser  lines 17-25  bytes 430-612`.

**Input.** Accepts a file only. Passing a directory is a usage error (exit `1`).

**Offsets are half-open.** Byte offsets follow the same half-open convention as `golens get`: `[start_byte, end_byte)`. `end_line` is derived from `end_byte - 1` rather than from the AST end position, so a declaration ending at column 1 of the following line reports the correct line.

**JSON Output Format (`golens bounds UserStore.GetUser user.go --json`)**

```json
{
  "schema_version": 1,
  "symbol": "UserStore.GetUser",
  "file": "user.go",
  "start_line": 17,
  "end_line": 25,
  "start_byte": 430,
  "end_byte": 612
}
```

### 2.4 `golens check`

Validates source file syntax and detects unused imports and duplicate declarations. Executes significantly faster than golangci-lint by acting solely as a lightweight AST validator. Unreachable-code detection is out of scope for v0.1.

**Usage**

```bash
golens check <file|dir> [flags]
```

**Checks Performed**

- Fatal parsing errors (unclosed brackets, invalid tokens), reported in `file:line:col:` form with
  the line and column the parser reported. A parse failure is one diagnostic among many: every
  other file in the tree is still checked.
- Unused imports, detected by AST analysis of selector base names. Best effort: a local name that
  is shadowed or referenced only in a comment may yield a false positive, never a false negative on
  an import that is genuinely unused. When the local name cannot be derived — a path whose last
  element is not a valid identifier — the check stays silent rather than accusing a live import.
- Duplicate top-level declarations within a package, **reported only when both files could be
  compiled into the same binary.** A file's build set is the conjunction of three independent
  constraints: the `//go:build` expression, the constraint Go infers from the *file name*
  (`_windows`, `_amd64`, `_linux_amd64`), and whether the file imports `"C"` — a cgo file is
  excluded outright when cgo is disabled, whatever its name and tags say. Each resulting
  expression is evaluated over a GOOS × GOARCH × cgo matrix, and two declarations conflict only
  when their build sets intersect.

  The matrix enumerates the GOOS/GOARCH pairs the toolchain can actually build (`go tool dist
  list`, 47 of them), not their cross product of 210. An invented pair makes real constraints look
  satisfied: under a fictional `freebsd/mipsle`, `//go:build mips || mipsle` and `//go:build freebsd`
  both hold. **The 47 pairs are a compiled-in snapshot, not a query** — golens is stateless and
  answers in microseconds, so running the toolchain per check is out of budget. A pair added by a
  later Go release is therefore not modelled, which can only *suppress* a diagnostic, never invent
  one. Name recognition is deliberately wider than the matrix, so a file named for a retired
  platform is constrained to a configuration that does not exist and is therefore never built —
  suppressed rather than accused.

  Platform variants of one function are therefore silent, which is what keeps the check usable on
  any repository that targets more than one OS; a build tag the tool cannot recognise evaluates
  false, so it assumes the narrowest reading rather than claiming a conflict it cannot demonstrate.
  `init` and `_` are exempt: Go permits a package to declare either any number of times.
- Import path syntax errors (empty, whitespace, or invalid characters), reported at the line the
  import is written on.

**`check` reports what `go build` would reject, without building.** Every diagnostic above
corresponds to a compiler error: an unused import, a redeclaration, a malformed import path, a
syntax error. That is the whole scope, and it is why the tool needs neither `go/types` nor a module
graph. Anything `go build` accepts is out of scope, which is why naming, formatting and idiom are
not checked here — see §3.1.

**Out of scope.** Module-level import resolution and type-level conflict detection both require `go/types` and a module graph, which conflict with the stateless < 5ms design.

**Output**

Every diagnostic carries `file:line:column: message`, including the ones that are not syntax errors:

```text
user.go:5:1: expected 'package', found 'func'
user.go:12:1: imported and not used: "fmt"
```

The column is the position the parser reported for a parse error, and `1` for the checks that
report a declaration rather than a position — an unused import is attributed to the line its spec
is written on, not to the package clause. `--json` carries the same facts as
`severity`, `file`, `line`, `column` and `message`.

## 3. Project Constraints & Guidelines

### 3.1 Codebase Standards

- **No style enforcement by the tool**: `golens` reports structural facts about a codebase and never judges style. There is no naming check, no formatting check, no idiom check. A command asked "is this file valid" or "what is in this file" must not answer "and also your identifiers are spelled wrong": that is a judgement the caller did not request, and an agent that acts on it wrongly rewrites working code. Style enforcement belongs to a linter, which a caller opts into explicitly.
- **This repository's own naming**: golens' own source follows Go's initialism convention — the rule golint popularised and staticcheck carries as ST1003. `ID`, `URL`, `JSON` and `HTTP` are written consistently within an identifier (`userID`, `HTTPHandler`), and an initialism stays lowercase only at the front of an identifier that already begins lowercase (`urlPath`, `jsonBody`). Enforced by `revive`'s `var-naming` rule in CI, not by golens itself. The distinction is the point: golens' source follows a convention, and golens does not impose one.
- **Package Layout**: One-way import direction `cli → usecase → domain`, with `engine/goast` depending only on `domain`. Only `engine/goast` may import `go/parser`, `go/ast` and `go/token`.
- **Clean Architecture**: Domain entities (Symbols, Bounds, Nodes) must be decoupled from delivery mechanisms (CLI cobra commands, JSON formatters).
- **Dependency Limitations**: Limit third-party libraries. Approved packages:
    - `spf13/cobra` (CLI routing)
    - Standard library (go/*, encoding/json, fmt, os, path/filepath)

### 3.2 Performance Budgets

- File read + AST parse must execute in < 5ms per file (P95).
- Binary size must remain under 15MB.

