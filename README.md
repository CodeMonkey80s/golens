# golens

Structural AST queries for AI coding agents. Read targeted source instead of whole files, and
edit using byte-accurate bounds.

```
golens outline <file|dir>       structural hierarchy map
golens get <symbol> <file|dir>  exact source of one symbol
golens bounds <symbol> <file>   byte and line offsets for an edit
golens check <file|dir>         fast syntax and namespace validation
```

Pure Go, no cgo, no daemon, no index. One binary, 3.9 MB, parses a file in well under a
millisecond.

---

## Why

An agent that needs one function has two options: read the whole file and pay for it in context,
or grep and hope. `golens` parses the file properly and answers structurally, then hands back
byte offsets precise enough to edit with.

```console
$ golens outline internal/naming/naming.go --depth 1
file: internal/naming/naming.go
package: naming

IMPORTS
  • sort
  • strings

TYPES & STRUCTS (lines 40-43)
  • type Violation struct (lines 40-43)

FUNCTIONS & METHODS (lines 45-174)
  • func (s Violation) String() string (line 45)
  • func Check(identifiers []string) []Violation (lines 59-72)
  • func violates(ident string) (string, bool) (lines 80-90)
```

```console
$ golens get Violation.String internal/naming/naming.go
File: internal/naming/naming.go
Symbol: Violation.String
Lines: 45 (1 line)

func (v Violation) String() string { return v.Identifier + ": " + v.Reason }
```

```console
$ golens bounds Initialisms internal/naming/naming.go
Initialisms  line 22  bytes 916-927
```

That last line is the point. To edit `Initialisms` you seek to 916 and write 11 bytes. No fuzzy
matching, no re-reading the file to find out where you ended up.

---

## Install

```console
$ go install github.com/CodeMonkey80s/golens/cmd/golens@latest
```

Or build from a checkout:

```console
$ git clone https://github.com/CodeMonkey80s/golens
$ cd golens && go build -o golens ./cmd/golens
```

Requires Go 1.20 or newer. Builds with `CGO_ENABLED=0`.

---

## Commands

### `outline <file|dir>`

Structural map of a file or directory: imports, types, funcs and methods, vars and consts.

| Flag | Effect |
|---|---|
| `--depth <int>` | Recursion limit. `1` shows declarations only, `2` adds fields and parameters. Default unlimited |
| `--json` | Array of per-file objects |
| `--compact` | One record per line, no decoration |

A directory is walked recursively. `.git`, `vendor`, `node_modules` and hidden directories are
skipped, and only `.go` files are read. Files are emitted in lexical order, so output is
deterministic.

`--json` always returns an array, even for a single file, so the schema never changes shape with
the input.

### `get <symbol> <file|dir>`

The exact source of one declaration.

| Flag | Effect |
|---|---|
| `--bounds-only` | Omit the source, return only ranges and offsets |
| `--json`, `--compact` | As above |

Symbols may be a bare name (`Check`) or qualified for methods (`UserStore.GetUser`). Both
exported and unexported identifiers resolve. Passing a directory searches the whole package.

A bare method name is accepted when it is unique. When two receivers share a name the command
exits `2` and lists the candidates rather than guessing.

### `bounds <symbol> <file>`

Byte and line offsets for an edit. Accepts a single file only; a directory is a usage error.

### `check <file|dir>`

Fast validation — no type checker, no module resolution.

| Flag | Effect |
|---|---|
| `--naming` | Report `camelCaseId` violations *instead of* the syntax checks |
| `--json`, `--compact` | As above |

Checks performed:

- Fatal parse errors, in `file:line:col: message` form
- Unused imports
- Duplicate top-level declarations, within one file or across a package
- Malformed import paths

```console
$ golens check ./internal
internal/domain/domain.go:5:1: imported and not used: "unused"
$ echo $?
3
```

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

# 5. Seek to 430, write 182 bytes, done.
```

Read with `outline`, extract with `get`, edit with `bounds`, validate with `check`.

---

## Development

```console
$ go test -count=1 ./...
$ go vet ./...
$ go build -o golens ./cmd/golens
```

**Use `-count=1`.** Four packages assert facts about the build graph or the source tree rather
than about their own test inputs — `internal/arch` shells out to `go list -deps`,
`internal/naming` scans this repository, and `selfcheck_test.go` and
`TestRepositoryPassesItsOwnCheck` walk the tree. Go's test cache does not invalidate on those,
so a stale `ok` is possible. This has hidden a real failure once.

`golens check ./internal` and `./cmd`, and `golens check . --naming`, all pass on this
repository. The tool checks itself in CI, which is how a false positive in its namespace check
was caught.

CI runs on every push and pull request: tests, the race detector, `go vet`, a `gofmt` gate, the
15 MB binary budget, the no-cgo check, and `golens` on its own source.

### Layout

```
cmd/golens/            entry point; composes the engine
internal/domain/       Bounds, Node, Symbol, Document, Engine — stdlib only
internal/engine/goast/ the only package importing go/parser, go/ast, go/token
internal/usecase/      command orchestration and error classification
internal/cli/          cobra wiring and exit-code mapping; imports no engine
internal/format/       human, JSON and compact renderers
internal/naming/       camelCaseId convention checker
internal/arch/         structural tests enforcing the boundaries above
internal/testfixture/  shared fixtures
```

Import direction is one-way: `cli → usecase → domain`, with `engine/goast` and `naming`
depending only on `domain`. `internal/arch` enforces this as a test rather than as a convention,
so a violation fails the build.

### Performance

| Budget | Measured |
|---|---|
| Read + parse, single file | ~13 µs typical, 282 µs on the largest file here (budget 5 ms) |
| Binary size | 3.9 MB (budget 15 MB) |

Directory parsing is concurrent above eight files and sequential below, so the single-file path
never pays for goroutines. Concurrency is unobservable: output is byte-identical either way,
which is tested by rendering with `GOMAXPROCS` pinned to 1 and comparing.

### Further reading

| Document | Contents |
|---|---|
| `documentation/specification.md` | The normative spec |
| `documentation/decisions.md` | Design decisions, frozen interfaces, acceptance criteria |
| `documentation/implementation-plan.md` | Session plan and sequencing |
| `documentation/retrospective-s1.md` | The `go/ast` position model; why tests lie |
| `documentation/retrospective-s2.md` | Sentinel values; three bugs in green tests |
| `documentation/retrospective-s4.md` | Error bias per check; running a checker on itself |

---

## Licence

MIT — see [LICENSE](LICENSE). Copyright is held by the maintainer.

## Acknowledgements

Written with assistance from **Space Bunny Free** (via [OpenCode](https://opencode.ai)).

Scope, design decisions, naming, module path and review were directed by the maintainer; the
per-session retrospectives in `documentation/` record what was learned along the way.
