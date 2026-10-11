# golens — Engineering Decision Log (v0.1 MVP)

Status: **Decided.** This document resolves every ambiguity found in `specification.md`.
No normative content was removed; §5 below contains the exact patch text to merge into the spec.

Principle applied to every decision: **best fit for an agent consumer.** A coding agent
parses this output programmatically, so determinism, schema stability and
unambiguous machine conventions outrank human prettiness. Where the spec was silent,
the choice is the one that cannot be misread and cannot silently corrupt a file.

---

## 1. Decision summary

| # | Decision | Rationale in one line |
|---|---|---|
| D1 | Byte offsets are half-open `[start, end)` | Directly usable as a Go slice; composes with `token.File.Offset`. |
| D2 | `end_line` computed from `end_byte - 1`, never from `node.End()` | Avoids an off-by-one when a decl ends at column 1 of the next line. |
| D3 | `get` span is purely AST-derived; no trailing newline | The `\n` stays outside the span, so an overwrite preserves line structure. |
| D4 | Unreachable-code detection is **out of v0.1** | It is a lint category; contradicts §2.4's "AST validator only" positioning. |
| D5 | `--compact` is a lossless subset; `--compact` beats pretty `--json` | Prevents `--compact` from drifting into lossy over time. |
| D6 | `outline` on a dir = recursive, lexical order, one JSON object per file | Deterministic output is what makes agent diffing and caching work. |
| D7 | `--depth 1` = package-level declarations only | Natural reading of "hierarchy depth"; keeps the common case cheap. |
| D8 | `check` stays AST-only: parse errors, unused imports, duplicate decls | Module resolution and type checking break the <5 ms, no-daemon budget. |
| D9 | Naming enforced by an in-repo AST test; initialisms lowercased uniformly | CI cannot be bypassed; exceptions would make the check non-deterministic. |
| D10 | Engine interface is narrow and tree-shaped; `Bounds` in engine coordinates | Keeps the v0.2 Tree-sitter promise cheap to honour now instead of a rewrite later. |
| D11 | Exit codes 0/1/2/3/4; payload → stdout, diagnostics → stderr | Agents branch on exit codes; `--json` must stay pipe-clean. |
| D12 | `bounds` human output is one pinned line | §2.3 never defined it; agents need a stable default. |
| D13 | Single-line decls render `(line 10)`, ranges render `(lines 5-8)` | Matches the two forms already present in the spec. |
| D14 | Every JSON payload carries `"schema_version": 1` | Lets agents detect format drift instead of silently mis-parsing. |
| D15 | Comments are not parsed in v0.1 | Cheaper, matches every spec example, no output change. |
| D16 | `get --bounds-only` and `bounds` share one code path | Eliminates two payload shapes drifting apart. |
| D17 | Ambiguous symbol → exit 2 with candidates; bare method names allowed if unique | Better ergonomics than hard failure, still deterministic. |
| D18 | `bounds` accepts a file only | §2.3 specifies `<file>`; do not widen the contract silently. |
| D19 | Directory inputs parse files concurrently, bounded by GOMAXPROCS | Single-file latency budget is per-file; dirs are where wall-clock hurts agents. |
| D20 | Clean package layout enforced by import direction | §3.1 requires decoupling; layout is what makes it real. |
| D21 | A file's build set is name ∧ tag ∧ cgo, over the 47 real GOOS/GOARCH pairs | 70,854 false positives on the standard library until all three were modelled |
| D22 | `outline` writes every parseable file first, then reports the skips and exits 4 | One broken file must not hide 8,000 others, and exit 0 would be a lie |

---

## 2. Rationale for the non-obvious calls

### D1–D3 · The offset contract

This is the only decision that can corrupt a user's file if wrong, so it is stated as an
invariant rather than a description:

> For every symbol, `file[start_byte:end_byte]` equals the extracted source, byte for byte.

That single property is pinned as a test across all fixtures. Byte ranges are half-open;
line ranges are inclusive. The asymmetry is intentional and documented — lines mirror
compiler diagnostics (`Lines: 17-25 (9 lines)`), bytes mirror Go slicing.

The span comes straight from the AST (`Offset(node.Pos())` → `Offset(node.End())`) with no
adjustment. `node.End()` sits just past the closing `}`, so the terminating newline is
naturally excluded and an agent overwriting the range leaves line structure intact. No
special-casing, no trimming.

### D4 · Unreachable code

§2.4's prose promises it; §2.4's *Checks Performed* list omits it. The list wins, and the
prose is corrected. Reason: unreachable-code detection is opinionated, a classic lint
category, and shipping it would contradict the command's stated purpose of being a
lightweight AST validator. Deferred to v0.2 with the multi-language engine.

### D5 · `--compact`

Definition is by rule, not by example: *same information, decoration removed.* The
implementation is a single flag threaded into the formatters, never a second code path.
Precedence is pinned because §2 leaves it open: `--compact` wins over pretty-printed
`--json`. For `check`, human and compact output are already identical (one diagnostic per
line), so `--compact` is a no-op there — stated rather than left to chance.

### D6–D7 · outline

Directory traversal is lexical via `filepath.WalkDir`, skipping `.git`, `vendor` and hidden
directories. Determinism is the whole point: an agent that sees the same file twice must
receive byte-identical output. `outline --json` always returns an **array**, even for a
single file, so the schema never changes shape based on input. Multi-file human output
repeats the `file:` header per block.

`--depth` counts container nesting: depth 1 is package-level declarations (types, funcs,
methods, vars, consts) with children suppressed; depth 2 adds struct fields and method
sets. It is not every AST level — a generic AST depth would be meaningless to a caller.

### D8 · check scope

The spec's three checks are re-scoped by what an AST alone can honestly support:

| Check | v0.1 behaviour | Mechanism |
|---|---|---|
| Fatal parse errors | Full support | `scanner.ErrorList` from `parser` |
| Unused imports | Full support (AST, best effort) | Selector-base-name analysis against declared import local names; `_` and `.` imports handled explicitly |
| Package namespace conflicts | Narrowed to **duplicate top-level declarations** | Pure AST, genuine namespace error. Confined to files that could co-build — see D21 |
| "Import resolution issues" | Narrowed to **import path syntax** (empty, whitespace, invalid chars) | Module resolution needs `go list` — subprocess, module graph, blows the budget |
| Type-level conflicts | Out of scope | Inherently requires `go/types` |

Known limitation, documented rather than hidden: the unused-import check can report a
false positive when an import's local name is shadowed or appears only in a comment. This
is acceptable for a fast checker and is stated as best-effort in the spec.

### D9 · Naming is not golens' business

**golens does not enforce naming, on any codebase including its own.** The `internal/naming`
package and the `golens check --naming` flag are gone. This repository follows Go's initialism
convention, and `revive`'s `var-naming` rule enforces it in CI.

The reasoning has three layers, and the first is the one that matters.

**A structural query should not return style judgements.** `golens` answers what is in this file,
where is this symbol, what are its exact byte offsets, does it parse. "Should this identifier be
`ID` or `Id`" is a different kind of claim, and returning it unasked makes the caller filter
before they can trust the output. Worse, the failure is not symmetric: a naming false positive
tells an agent to rewrite an identifier that was correct, and code that compiled yesterday stops
doing so. The tool's own bias toward silence argues the same way -- better to under-report than
to have an agent act on a wrong claim.

**The convention already has a home, and it is a better one.** `revive` ships `var-naming`,
which is the rule golint popularised and staticcheck carries as ST1003. Enforcing it means
opting into a linter, which is the right shape for an opt-in judgement: a caller who wants
naming checked runs a linter. Measured on identical inputs, `revive` and the deleted golens rule
reported the same violations, and `revive` caught more.

**The three attempts are the evidence.** This went wrong twice before landing, and the sequence
is the useful part.

1. `camelCaseId`, a local dialect where every initialism is lowercased. Implemented faithfully;
   **4,119 diagnostics on the Go standard library**, including `isASCII` and `ensureEOF`, and 11
   on ordinary idiomatic Go. Every one wrong. A convention check that reports correct code gets
   switched off, which is worse than having none.
2. Go's convention, implemented in-tree. Correct -- 0 on idiomatic Go, 850 on the standard
   library, all genuine -- and by then indistinguishable from `revive`. Keeping a second
   implementation of a rule the ecosystem already ships is a maintenance liability with no
   user-visible benefit.
3. Removal. Naming is `revive`'s; golens is not a linter.

The one capability genuinely lost: `revive` cannot run outside a module (`directory prefix .
does not contain main module`), so for a loose file handed to an agent outside any repository,
golens was the only thing that could answer the naming question. That is a real gap, and it is
the wrong tool to fill it -- a tool whose subject is structural facts about code should not
become the one that grades it.

`TestCheckDoesNotJudgeStyle` and `TestCheckAcrossFilesReportsNothingStructural` pin the
boundary, so naming cannot quietly return through a new flag.


### D10 · Engine interface

```go
type Bounds struct{ StartLine, EndLine, StartByte, EndByte int }
type Node struct{ Name, Kind, Detail string; Bounds Bounds; Children []Node }
type Document struct{ Path, Package string; Bounds Bounds; Nodes []Node }
type Engine interface{ Parse(path string) (Document, error) }
```

Deliberately narrow and tree-shaped, because that is the shape a Tree-sitter grammar can
also produce. Any wider interface leaks Go specifics (`*ast.Node`, `token.FileSet`) into
the generic layer and would have to be rewritten for v0.2. Byte and line semantics are the
engine's own concern; the Go engine converts from `token.FileSet` internally.

### D11 · Exit codes

| Code | Meaning |
|---|---|
| 0 | Success |
| 1 | Usage error (bad flags, missing args) |
| 2 | Symbol or file not found / ambiguous |
| 3 | `check` found diagnostics |
| 4 | Parse or I/O failure |

Payloads go to stdout, diagnostics to stderr. This is what lets `golens get X f.go --json |
jq` work without stderr noise corrupting the stream.

### D15 · Comments

Parsed out by default. Every spec example shows declaration-only output, so this changes
nothing observable, and it buys headroom against the 5 ms P95 budget. Doc-comment
awareness for symbol selection (`--with-docs`) is a v0.2 item alongside multi-language
support.

### D16–D17 · Symbol resolution

`get --bounds-only` emits the **identical** payload to `bounds` — one serialiser, one
struct, no drift. Resolution accepts `Type.Method` and, for convenience, a bare method name
when it is unique in the package. Any ambiguity, including duplicate top-level
declarations, exits 2 with the candidate list on stderr rather than silently picking one.
Nested and field access (`Type.Field`, package-qualified `pkg.Func`) are out of v0.1 scope.

### D19–D20 · Concurrency and layout

Single-file invocation stays strictly sequential — that path carries the P95 budget and
should not pay scheduling overhead. Directory traversal fans out per-file parses bounded
by `GOMAXPROCS`, then reassembles in lexical order so concurrency never affects output.

```
cmd/golens/main.go         thin: parse flags, call usecase, render, exit
internal/domain/          Symbol, Bounds, Node, Document, Engine — zero external deps
internal/engine/goast/    the only package importing go/parser, go/ast, go/token
internal/usecase/         outline, get, bounds, check orchestration
internal/cli/             cobra wiring only
internal/format/          human, json, compact encoders
```

Import direction is one-way (`cli → usecase → domain`, `engine/goast → domain`), so the
Clean Architecture requirement in §3.1 is structurally enforced rather than a code-review
convention.

### D21 · A file's build set is name ∧ tag ∧ cgo, over a real GOOS × GOARCH matrix

`check` only reports a redeclaration when both files could reach the same binary. Getting that
right requires three things the first implementation missed, and missing any one of them made the
check unusable rather than merely imprecise: on the standard library, one missing piece produced
70,854 false positives across 8,088 files.

1. **The filename is a build constraint.** `syscall/env_windows.go` carries no `//go:build` line
   at all; it is Windows-only purely by suffix. Reading only the comment treats it as built
   everywhere and collides it with every declaration of the same name — 61,287 of them in
   `syscall`. The rule is `go/build`'s, including the parts that look wrong: split from the *first*
   underscore, so `os/zero_copy_linux.go` is linux-only and `runtime/os_only_solaris.go` is
   solaris-only; test the last two components as GOOS+GOARCH *before* the last one alone, because
   testing the arch alone makes `zerrors_darwin_amd64.go` match every OS.
2. **The matrix is real, not a cross product.** `go tool dist list` gives 47 GOOS/GOARCH pairs; a
   cross product gives 210. An invented pair makes real constraints look satisfied — under a
   fictional `freebsd/mipsle`, both `//go:build mips || mipsle` and `//go:build freebsd` hold. A
   constraint is only ever as true as the configurations it is evaluated over.
   Name *recognition* stays deliberately wider than the matrix: a file named for a platform Go has
   dropped is constrained to a configuration that does not exist, so it is never built, so it is
   suppressed rather than accused.
3. **`import "C"` means the file requires cgo.** The go tool lists such files under `CgoFiles` and
   drops them when cgo is off, whatever the name and tags say. The stdlib's own
   `crypto/internal/sysrand/internal/seccomp` is this exact case.

`init` and `_` are also exempt from the uniqueness rule, because Go says explicitly that a package
may declare either any number of times. Treating `init` as an ordinary name accounted for 166 of
the false positives in `runtime` alone.

Result: **0 diagnostics** on the standard library outside `testdata` directories, which hold
compiler fixtures written to be broken. Every remaining class was a false positive on code that
compiles, and the bias throughout is toward silence — a tag the tool cannot parse evaluates false.

### D22 · A partial outline is worth more than a clean failure

`outline` used to abort on the first unparseable file and write nothing. That is right for a
single broken file, where the parse error is the whole answer, and badly wrong for a tree. The
standard library contains unparseable files in `testdata`; asking for an outline of `$GOROOT/src`
returned no output at all, which is the answer an agent most often wants.

So the outline is written first and the failure reported afterwards: unparseable files are omitted,
stdout carries everything else — still a complete, parseable JSON payload under `--json` — stderr
names what is missing and how complete the answer is, and the exit code stays **4**.

Exit 4 is the load-bearing part. The alternative, exiting 0, would let an agent believe it had
seen the whole tree. A caller that branches on `$?` learns the outline is partial; a caller that
only reads stdout still gets every file that could be read. Those two facts are not in conflict,
and collapsing them would lose one.

The alternative worth rejecting: skipping `testdata/`. An agent asking what a directory contains
wants all of it, and silently narrowing the traversal would be a scope decision made invisibly.

### D23 · Every renderer of a declaration head carries the whole head

`Declaration()` in `domain` is the single place that assembles a Go declaration head, and it
consumes four fields: `Name`, `TypeParams`, `Receiver` and `Detail`, plus `IsAlias`. The human
outline goes through it. The JSON outline did not — `convertNodes` copied `Name`, `Kind` and
`Detail` by hand and dropped the rest.

The result was that `outline --json` could not distinguish a type alias from a defined type:

```json
{"name": "Alias",   "kind": "type", "detail": "Stack[int]"}
{"name": "Defined", "kind": "type", "detail": "Stack[int]"}
```

Two identical nodes, two different programs. An agent reconstructing a declaration from either
writes `type Alias Stack[int]`, which **compiles** — as a defined type, no longer interchangeable
with `Stack`. This is the same defect the alias fix addressed in `Declaration()`, arriving through
the other renderer. Generic type parameters and method receivers were dropped the same way, and
those do not compile when copied.

**The rule: a field that changes the meaning of a rendered declaration belongs in every renderer
that renders one.** A fix applied to one output format is not a fix to the data. Adding a field
to the JSON node is therefore not a schema convenience — it is the only way that payload can
describe the program it was generated from. The three fields are `omitempty`, so a file with no
generics, no methods and no aliases pays nothing for them.

What generalises: this survived two fixes to the same bug. The first fix taught `Declaration()`
about `IsAlias`; the second found that the JSON path had never been taught. A test that pins one
renderer's output pins *that* renderer. `TestJSONDeclarationHeadsMatchSource` now asserts the
JSON node against the same source constructs `TestDeclarationHeadsMatchSource` pins for the human
form, and compares the alias against its defined twin — the property is that the two differ, which
neither test can see from one renderer alone.

---

## 3. Resolved ambiguities at a glance

| Was ambiguous | Now |
|---|---|
| `end_byte` inclusive? | Half-open; spec states `[start, end)` explicitly |
| Line counting | Inclusive both ends; `end_line` derived from `end_byte - 1` |
| Trailing newline in `get` | Excluded; span is exactly the AST node |
| `--compact` shape | Rule-based, lossless, wins over pretty JSON |
| `--json` + `--compact` | Compact wins |
| `outline` on a directory | Recursive, lexical, array of per-file objects |
| `outline` with an unparseable file | Skips it, writes the rest, names it on stderr, exits 4 — see D22 |
| Which files can co-build | Name ∧ tag ∧ cgo, over real GOOS/GOARCH pairs — see D21 |
| `--depth` counting | Container nesting; 1 = package-level decls |
| Unreachable code | Removed from §2.4 prose, deferred to v0.2 |
| Import resolution | Path syntax only |
| Namespace conflicts | Duplicate top-level decls only |
| Exit codes | 0/1/2/3/4, documented |
| `bounds` human output | Pinned one-liner |
| Schema versioning | `schema_version: 1` on every payload |
| Ambiguous symbols | Exit 2 + candidate list |
| Comment handling | Not parsed in v0.1 |

---

## 4. Acceptance criteria derived from the decisions

| # | Criterion |
|---|---|
| A1 | For all fixtures and all symbols: `file[start_byte:end_byte]` == extracted source, byte for byte. |
| A2 | Running any command twice on unchanged input yields byte-identical stdout. |
| A3 | Directory output is identical whether files are processed serially or concurrently. |
| A4 | Every JSON payload parses into a struct and carries `schema_version: 1`. |
| A5 | `--compact` output contains no information absent from the full human output. |
| A6 | `get --bounds-only --json` and `bounds --json` produce identical payloads. |
| A7 | golens never reports a style violation. A file whose only irregularity is naming produces no diagnostics and exit 0. |
| A8 | Single-file read+parse is < 5 ms at P95 over a representative corpus (benchmark-gated). |
| A9 | Release binary < 15 MB. |
| A10 | `go build` with `CGO_ENABLED=0` produces no cgo dependencies. |
| A11 | `domain` and `usecase` packages import nothing outside the standard library. |
| A12 | Fixtures include: unicode identifiers, CRLF files, BOM, unparseable file, empty file, file with only comments, duplicate declarations, unused + dot + blank imports. |
| A13 | A directory containing both a `_linux.go` and a `_windows.go` that declare the same name produces no diagnostics and exit 0, and two untagged files that declare it produce exit 3. |
| A14 | `outline` over a directory containing an unparseable file writes every parseable file to stdout, names the skipped files on stderr, and exits 4. Under `--json` the stdout payload parses. |

---

## 5. Spec patch — merge into `specification.md`

### 5.1 Add to §2, immediately after the global-flags list

> **Exit codes.** `0` success · `1` usage error · `2` symbol or file not found or ambiguous ·
> `3` `check` found diagnostics · `4` parse or I/O failure. Payloads are written to stdout,
> diagnostics to stderr. When `--json` and `--compact` are combined, `--compact` takes
> precedence and emits non-indented JSON.
>
> **Schema versioning.** Every JSON payload includes `"schema_version": 1` as its first field.
>
> **`--compact` semantics.** Compact output is a lossless subset of the human output: the
> same information with decoration removed. It is a formatting flag, not a second code path.

### 5.2 §2.1 `golens outline`

> **Directory input.** The path is traversed recursively; `.git`, `vendor` and hidden
> directories are skipped, and only `.go` files are considered. Files are emitted in lexical
> path order, and each block repeats the `file:` header. Output is deterministic: identical
> input always yields byte-identical output.
>
> **JSON shape.** Always an array of per-file objects, including for a single file, so the
> schema never varies with input type.
>
> **`--depth`.** Counts container nesting. Depth 1 (the default is unlimited) emits
> package-level declarations only — types, funcs, methods, vars and consts — with children
> suppressed. Depth 2 additionally emits struct fields and method sets.
>
> **Line ranges.** A declaration spanning several lines renders as `(lines 5-8)`; a
> single-line declaration renders as `(line 10)`.

### 5.3 §2.2 `golens get`

> **Offsets are half-open.** `start_byte` is the first byte of the symbol and `end_byte` is
> one past its last byte, so `file[start_byte:end_byte]` is exactly the extracted source.
> Line numbers are inclusive at both ends, matching the `(9 lines)` count above. When
> `--bounds-only` is combined with `--json`, the payload is identical to `golens bounds`.
>
> **Symbol ambiguity.** A bare method name is accepted when it is unique in the package.
> Any ambiguity exits `2` and lists the candidates on stderr rather than guessing.

### 5.4 §2.3 `golens bounds`

> **Human output.** `golens bounds <symbol> <file>` prints a single line:
> `UserStore.GetUser  lines 17-25  bytes 430-612`.
>
> **Input.** Accepts a file only. Passing a directory is a usage error (`1`).

### 5.5 §2.4 `golens check`

Replace the introduction's "or unreachable code" with:

> Validates source file syntax and detects unused imports and duplicate declarations.
> Executes significantly faster than golangci-lint by acting solely as a lightweight AST
> validator. Unreachable-code detection is out of scope for v0.1.

Replace **Checks Performed** with:

> - Fatal parsing errors (unclosed brackets, invalid tokens), reported in `file:line:col:` form with
>   the line and column the parser reported. A parse failure is one diagnostic among many: every
>   other file in the tree is still checked.
> - Unused imports, detected by AST analysis of selector base names. Best effort: a local name that
>   is shadowed or referenced only in a comment may yield a false positive, never a false negative on
>   an import that is genuinely unused. When the local name cannot be derived — a path whose last
>   element is not a valid identifier — the check stays silent rather than accusing a live import.
> - Duplicate top-level declarations within a package, **reported only when both files could be
>   compiled into the same binary.** Each file's `//go:build` expression is evaluated over a
>   GOOS × GOARCH × cgo matrix, and two declarations conflict only when their resulting build sets
>   intersect. Platform variants of one function are therefore silent, which is what keeps the check
>   usable on any repository that targets more than one OS; a build tag the tool cannot recognise
>   evaluates false, so it assumes the narrowest reading rather than claiming a conflict it cannot
>   demonstrate.
> - Import path syntax errors (empty, whitespace, or invalid characters), reported at the line the
>   import is written on.
>
> **`check` reports what `go build` would reject, without building.** Every diagnostic above
> corresponds to a compiler error: an unused import, a redeclaration, a malformed import path, a
> syntax error. That is the whole scope, and it is why the tool needs neither `go/types` nor a module
> graph. Anything `go build` accepts is out of scope, which is why naming, formatting and idiom are
> not checked here.
>
> **Out of scope.** Module-level import resolution and type-level conflict detection both
> require `go/types` and a module graph, which conflict with the stateless < 5 ms design.

### 5.6 §3.1 Codebase Standards

> **No style enforcement by the tool**: `golens` reports structural facts about a codebase and
> never judges style — no naming check, no formatting check, no idiom check. A command asked
> "is this file valid" or "what is in this file" must not answer "and also your identifiers are
> spelled wrong". Style enforcement belongs to a linter, which a caller opts into explicitly.
> See D9.
>
> **This repository's own naming**: golens' own source follows Go's initialism convention, the
> rule golint popularised and staticcheck carries as ST1003. An initialism is written
> consistently within an identifier — `ID`, `URL`, `JSON`, `HTTP` — so `userID` and
> `HTTPHandler` are correct; it stays lowercase only at the front of an identifier that already
> begins lowercase, so `urlPath` and `jsonBody` are correct. Enforced by `revive`'s `var-naming`
> rule in CI, not by golens itself. The distinction is the point: golens' source follows a
> convention, and golens does not impose one.
>
> **Package Layout.** One-way import direction `cli → usecase → domain`, with
> `engine/goast` and `naming` depending only on `domain`. Only `engine/goast` may import
> `go/parser`, `go/ast` and `go/token`.

---

## 6. Deferred to v0.2 (explicitly out of MVP)

| Item | Reason |
|---|---|
| Multi-language via Tree-Sitter | Needs the v0.1 `Engine` interface to be genuinely language-neutral first |
| Unreachable-code detection | Lint category, contradicts v0.1 positioning |
| Type-level conflict detection | Requires `go/types` |
| Module-level import resolution | Requires module graph; breaks the stateless budget |
| Doc-comment-aware symbol selection (`--with-docs`) | Deferred with comment parsing |
| Field and nested-type access (`Type.Field`) | Not in the v0.1 selection rules |
