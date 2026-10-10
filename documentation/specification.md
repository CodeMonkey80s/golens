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

**Directory input.** The path is traversed recursively; `.git`, `vendor` and hidden directories are skipped, and only `.go` files are considered. Files are emitted in lexical path order, and each block repeats the `file:` header. Output is deterministic: identical input always yields byte-identical output.

**JSON shape.** Always an array of per-file objects, including for a single file, so the schema never varies with input type.

**`--depth`.** Counts container nesting. Depth 1 emits package-level declarations only — types, funcs, methods, vars and consts — with children suppressed. Depth 2 additionally emits struct fields and method sets. The default is unlimited.

**Line ranges.** A declaration spanning several lines renders as `(lines 5-8)`; a single-line declaration renders as `(line 10)`.

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

- Fatal parsing errors (unclosed brackets, invalid tokens), reported in `file:line:col:` form.
- Unused imports, detected by AST analysis of selector base names. Best effort: a local name that is shadowed or referenced only in a comment may yield a false positive.
- Duplicate top-level declarations within a package.
- Import path syntax errors (empty, whitespace, or invalid characters).

**Out of scope.** Module-level import resolution and type-level conflict detection both require `go/types` and a module graph, which conflict with the stateless < 5ms design.

**Output**

```text
user.go:5:1: expected 'package', found 'func'
user.go:12: imported and not used: "fmt"
```

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

## 4. Phase 1 Implementation Plan

- **CLI Scaffolding**: Initialize cobra layout with outline, get, bounds, and check.
- **Parser Engine Core**: Implement the Go language target utilizing go/parser and go/ast.
- **AST Traversal**: Construct Visitor functions to map package namespaces and extract structural nodes.
- **Formatter Injection**: Build the standard human-readable text and minified JSON encoders.
- **E2E Testing**: Write parallelized go test suites executing CLI commands against mock .go source files.
