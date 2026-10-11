# HANDOFF — golens

**Status: feature-complete. The session plan is done and 12 commits of unplanned hardening landed
after it.** Every gate below was re-run after the spec review; all are green.

There is no next session to hand off to. This document records where the project actually is, so the
next agent starts from fact rather than from the plan.

## Current state

| Field | Value |
|---|---|
| Branch | `main` only, working tree clean |
| Remote | **none configured** — nothing has been or can be pushed |
| Tests | 298 (incl. subtests), 6 packages, all passing |
| Test count by package | arch 6 · cli 67 · domain 15 · goast 45 · format 21 · usecase 144 |
| Licence | MIT, `LICENSE` at repo root |
| Toolchain | go1.27.2, `golang.org/x` dependencies: none. Only cobra and its two indirects |
| Verify with | **`go test -count=1 ./...`** — see caching note below |
| Binary | 5.30 MB stripped, 8.05 MB plain (budget 15 MB) |
| Parse budget | 14.6 µs typical, 304 µs largest file (budget 5 ms P95) |
| Lint | `golangci-lint run ./...` — 0 issues, v2.14.0. Includes `revive` `var-naming` |
| Self-check | `golens check ./internal` and `./cmd` both exit 0 |

### Verification actually run

```
go test -count=1 ./...          # pass
go test -race -count=1 ./...    # pass
go vet ./...                    # clean
gofmt -l .                      # empty
golangci-lint run ./...         # 0 issues
./golens check ./internal       # exit 0
./golens check ./cmd            # exit 0
```

## History

Twenty-three commits on `main`, linear. The first eleven are the five-session plan and its merge; the
last twelve are post-plan hardening.

```
cbec81d  README: update binary size to 5.4 MB stripped
881414f  ci: add golangci-lint with a minimal, agent-shaped linter set
ec84025  usecase: benchmark all four commands end to end
b89e0f3  cli: show the build constraint, advise disambiguation, add --version
2b53ceb  check help: say it is not a type checker; add the cold-start protocol
974f912  cli: lead each command's help with a labelled fact block
d011d65  cli: put the per-command conventions in --help, add AGENTS.md
16cbee9  check: drop three false-positive classes found against duf
ea925e7  TESTING: cover the check false positives found against duf
72c791a  Match declaration heads to the source, and locate naming violations
939d6fb  Resolve /vN module suffixes in import local names
da7ea2e  ci: GitHub Actions workflow
02334de  docs: record the merge into main
7e7089a  Add acknowledgements section
91d5498  Add MIT LICENSE
0b136d3  docs: final handoff, all sessions complete
43ae86d  S5: concurrency (A3), spec conformance, and a README
f5f85c5  docs: S4 retrospective and a current handoff
675d0dc  S4: check test suite, cross-file conflicts, and a false positive fixed
554ef6d  S3: byte invariant through get, and the overwrite round trip
5963ae6  S2: outline golden tests, --compact, and fix unlimited depth
2b1889f  S2: add shared fixtures and the twelve A12 classes
ddcd80a  S1: foundation, engine, and all four commands
```

The five-session plan in `documentation/implementation-plan.md` is S1–S5 and all five landed. The
commit messages are the real record; each names the bugs it fixed, which is more useful here than any
summary.

## What landed after the plan

Twelve commits that no document in `documentation/` accounts for. All of them came from running the
tool against real repositories — `duf` and `GoDiabolicalDecorations` — rather than from fixtures.

### Defects found and fixed

| Commit | Defect | Impact |
|---|---|---|
| `72c791a` | Generic type parameters dropped from rendered heads | `type Stack[T any] struct` rendered as `type Stack struct`; an agent copying it writes code that will not compile |
| `72c791a` | Receiver variable name invented | Every method rendered `(s ...)` regardless of source — all 61 in the test repo |
| `72c791a` | Embedded fields rendered their type twice | `sync.Mutex sync.Mutex` |
| `72c791a` | Naming diagnostics reported every violation at line 1 | Location was fabricated |
| `939d6fb` | `/vN` module suffixes bound as the package name | `math/rand/v2` bound as `v2`; the unused-import check would call a live import dead |
| `16cbee9` | Redeclarations across mutually exclusive build constraints | **24 false positives on duf**, a repo that compiles cleanly on five platforms. Now evaluates constraints over a GOOS × GOARCH × cgo matrix |
| `16cbee9` | Unused imports whose local name cannot be derived | `github.com/mattn/go-runewidth` → `go-runewidth`, not an identifier; check now stays silent rather than guessing |
| `16cbee9` | camelCaseId applied to `SCREAMING_SNAKE_CASE` | Flagged every all-caps constant by construction. Superseded entirely by the initialism rule, which treats underscores as preserved boundaries |
| `b89e0f3` | `outline` never showed a file's build constraint | An agent could edit `filesystems_windows.go` on linux and conclude the tool lied |
| spec review | `outline --json` dropped `type_params`, `receiver` and `is_alias` | An alias and a defined type over the same underlying type were byte-identical JSON nodes describing different programs; `type Alias Stack[int]` compiles as a defined type |

The three `16cbee9` classes were found by testing against a real repository, and every one is a false
positive — the direction AGENTS.md says is the expensive one, because an agent that believes a
diagnostic may delete working code. The narrowed rule still catches genuine violations; TESTING.md
case 13 carries a positive control.

### Additions

| Commit | Item |
|---|---|
| `da7ea2e` | `.github/workflows/ci.yml` — `verify` and `quick` jobs, on push and PR to `main` |
| `881414f` | `.golangci.yml` — errcheck, staticcheck, unused, ineffassign, plus auto-fixable formatters. Style linters deliberately off |
| `ec84025` | End-to-end benchmarks for all four commands |
| `d011d65` | Per-command `Long` help carrying exit codes and the stream contract; `AGENTS.md` at root; `documentation/AGENTS.template.md` |
| `974f912` | Each command's help now opens with a labelled fact block; destructive facts moved from paragraph four to line five |
| `b89e0f3` | `--version`; ambiguity errors carry the remedy |
| `2b53ceb` | `check` help states plainly that exit 0 does not mean the file compiles; `documentation/COLDSTART.md` |
| `d011d65` | Dropped `go/build` — it imports `go/ast`, which `internal/arch` forbids outside `engine/goast`. `runtime.Version` derives the release tags instead |

`d011d65` is the load-bearing one for future agents: help text cannot drift from the binary, so the
conventions live in the command definitions rather than in a file an agent may never read.

### Performance

Measured on this machine, largest non-test file. Parse dominates three of the four commands.

| Operation | ns/op | allocs/op |
|---|---|---|
| Parse, hand-written fixture | 14,626 | 373 |
| Parse, largest real file | 303,719 | 6,242 |
| `outline` human | 334,167 | 6,278 |
| `outline` json | 358,972 | 6,154 |
| `outline` compact | 329,493 | 6,301 |
| `outline` directory (25 files) | 1,687,859 | 39,768 |
| `get` | 288,652 | 4,732 |
| `bounds` | 275,134 | 4,733 |
| `check` | 2,794,502 | 53,575 |

`check` is ten times a parse because it runs several analyses. Reproduce with
`go test -run XXX -bench . ./internal/usecase ./internal/engine/goast`.

## Alias fix — the last misfiled defect

`outline` rendered `type Alias = X` as `type Alias X`. Every other instance of this bug class
produced code that **failed to compile**; this one compiled and meant something else:

```go
type Alias = X        // an alias: interchangeable with X
type Copied X         // what golens printed: a new defined type
```

Copying the rendered head gave `cannot use X{} (value of struct type X) as Copied value` and
`a.Describe undefined`. A silently wrong declaration is worse than a loud one, so this was the
highest-severity defect in the project — and the S5 handoff had filed it as cosmetic, which is
probably why it survived.

The cause is that `Detail` cannot distinguish the two forms: both are `Name` plus an underlying
type. `ast.TypeSpec.Assign` is set only for an alias, so `Node.IsAlias` and `Symbol.IsAlias` carry
it and `Declaration` reproduces the `=`. `TypeParams` set the precedent of adding a field for a
distinction the AST has and the model dropped.

Two golden expectations had recorded the bug (`TestOutlineMatchesSpecificationSample`,
`TestCompactIsLosslessSubset`) and were updated. Neither asserts against the specification — the
spec's sample contains no alias — so both were pinning observed output rather than required output.
`TestDeclarationHeadsMatchSource` now covers `Alias` and `Defined` side by side, since the
interesting property is that two declarations with the same underlying type differ.

Verified the test fails without the fix rather than assuming it: removing the `IsAlias` branch in
`Declaration` reproduces `got: type Alias Stack[int]`.

## Acceptance criteria

All twelve green. Evidence is a test, not a claim.

| # | Criterion | Evidence |
|---|---|---|
| A1 | `file[start:end]` == extracted source | `TestByteInvariantThroughGet` across 12 fixture classes, plus `TestOverwriteRoundTrip` and `TestOverwritePreservesLineStructure` |
| A2 | Two runs → byte-identical stdout | `TestOutlineDirectoryIsDeterministic`, `TestConcurrentDeterministicAcrossRuns` |
| A3 | Concurrent == serial output | `TestConcurrentOutputMatchesSerial`, all four output modes, random tree sizes straddling the threshold |
| A4 | JSON parses, `schema_version: 1` first | `TestSpecSchemaVersionIsFirstField`, all four commands |
| A5 | `--compact` loses no information | `TestCompactIsLosslessSubset` |
| A6 | `get --bounds-only` ≡ `bounds` | `TestSpecBoundsOnlyMatchesBounds`, plus format and usecase level |
| A7 | golens never reports style | `TestCheckDoesNotJudgeStyle`, `TestCheckAcrossFilesReportsNothingStructural` |
| A8 | Parse < 5 ms P95 | `TestParseP95UnderBudget` |
| A9 | Binary < 15 MB | 5.30 MB |
| A10 | No cgo | `TestNoCgoDependency` |
| A11 | `domain`/`usecase` import nothing external | `internal/arch` |
| A12 | 12 fixture classes | `TestFixtureClasses` |

`internal/cli/spec_test.go` adds 11 more, each citing the specification clause it verifies.

## Caching note

`go test ./...` **without `-count=1` can report a stale pass.** Three packages assert facts about
the build graph or the source tree rather than about their own test inputs: `internal/arch`
(`go list -deps`), and `selfcheck_test.go` and `TestRepositoryPassesItsOwnCheck` (walk the tree).
Go's cache does not invalidate on those.

This hid a real failure during S1. **Always use `go test -count=1 ./...` here.** CI enforces it, and
`go test -short ./...` skips the tests that walk the repository.

## Frozen interfaces

Unchanged since S1 except `Node.IsGroup` (S2), and `TypeParams` plus `IsAlias` on `Node`/`Symbol`
(`72c791a` and the alias fix below). See `decisions.md` §2 for the canonical copy.

```go
type Bounds struct{ StartLine, EndLine, StartByte, EndByte int }
type Node struct {
    Name, Detail, Receiver, TypeParams string
    Kind     Kind
    IsAlias  bool
    Bounds   Bounds
    Children []Node
    IsGroup  bool
}
type Symbol struct{ Name, QualifiedName string; Kind Kind; Bounds Bounds; File, Detail, Receiver, TypeParams string; IsAlias bool }
type Document struct{ Path, Package string; Bounds Bounds; Nodes []Node; Symbols []Symbol; Imports []string; Source []byte; Comments bool }
type Engine interface{ Parse(path string) (Document, error) }
```

`TypeParams` carries the bracketed list verbatim, `"[T any]"`, kept separate from `Name` so the
symbol name stays the bare identifier for lookup. `Receiver` is the receiver as the source spells
it, `"s *UserStore"`. `IsAlias` distinguishes `type A = B` from `type A B`, which `Detail` alone
cannot. All three are load-bearing — see the traps in `AGENTS.md`.

`domain.LocatedError` carries a parse error's file, line and column. It lives in `domain` rather
than `engine/goast` because both layers need it and `internal/arch` forbids `usecase` importing
the engine. It exists because the engine used to return pre-formatted `"file:line:col: message"`
strings, which left nowhere to put the position — `check` reported every parse error at `1:1`, and
an agent following the documented format went to line 1 whatever the real position was.

## Naming is not golens' business

`golens check --naming` and the `internal/naming` package are **gone**. Naming is `revive`'s job,
enforced on this repository's own source in CI via `.golangci.yml`.

The reasoning, in one paragraph: `golens` answers structural questions — what is in this file,
where is this symbol, what are its exact byte offsets, does it parse. "Should this be `ID` or
`Id`" is a different kind of claim, and returning it unasked mixes a judgement into output the
caller asked for. The failure is also asymmetric: a naming false positive tells an agent to
rewrite an identifier that was correct, so code that compiled yesterday stops doing so.

It took three attempts to see that, and the sequence is the useful record:

1. `camelCaseId` — a local dialect lowercasing every initialism. Implemented faithfully, and it
   produced **4,119 false positives on the Go standard library**, including `isASCII` and
   `ensureEOF`.
2. Go's own convention, reimplemented in-tree. Correct, and by then indistinguishable from
   `revive`, which already ships it.
3. Removal.

Verified rather than assumed: `revive`'s `var-naming` reports the same violations golens did on
identical inputs and catches more, and passes on this source with zero issues. Two tests pin the
boundary so naming cannot return through a new flag: `TestCheckDoesNotJudgeStyle` and
`TestCheckAcrossFilesReportsNothingStructural`.

Net effect: −730 lines of source and tests, one acceptance criterion retargeted, one linter
enabled. A7 now reads "golens never reports a style violation" rather than "the repo passes a
naming test".

## Remaining work

Nothing blocking. The first item is the only real one.

The `type X = Y` alias bug that used to be listed here is **fixed** — see "Alias fix" below. It was
misfiled as cosmetic; it was the most dangerous defect the plan ever shipped.

| Item | Notes |
|---|---|
| **Tag and publish** | No remote configured, so `go install ...@latest` cannot work. README leads with a checkout and says so. This is the only thing standing between the code and use |
| `var x = 5` shows no `= 5` in outline | Cosmetic, and unlike the alias case it is not an edit hazard: the byte span covers only the name, and writing the name back is what the specification requires. **Investigated against the spec review and confirmed correct** — see "Var and const values" below |
| `check .` exits 3 on `testdata/store/store.go` | Intentional — the unused import gives the outline golden a stable shape |
| Unused-import false negatives | Accepted: comment-only references and shadowed locals. Never false positives, verified empirically against three repositories |
| Naming outside a module | Accepted loss. `revive` cannot run without a `go.mod`, so a loose file handed to an agent outside any repository gets no naming check. Filling that gap with golens would make a structural tool the one that grades code; `decisions.md` D9 records the trade |

## Var and const values — investigated, no change

`var x = 5` renders as `var x` with no `= 5`, which reads like a gap. The spec review checked
whether it is one. It is not, and closing it would introduce a defect.

The outline's declaration head is **not** the byte span `get` and `bounds` report. The span is
name-scoped, so for `var ( x = 5 )` it covers `x` alone. An agent that pastes the outline head
into that span writes `var x = 5` into a span that already holds `x`, producing:

```go
var (
	var x = 5 = 5
)
```

which does not parse. Pasting `var x` — the current output — writes back exactly what was there.

This is not specific to the value case. `var counter int` pastes the same way, into `counter`:

```go
var var counter int int
```

so the head is already not paste-safe for vars and consts generally, value or not. That is the
correct consequence of the name-scoped span the specification mandates at §2.2, and it is why the
help text says to edit with the text `get` returned rather than with the outline.

Adding `= 5` would make the outline *more* divergent from the span it describes while making
nothing paste-safe. The right fix, if one is wanted, is not in the outline: it is a way to read
the initializer, which today means reading the file.

## Read first

1. `AGENTS.md` — the invariants, the layering rule, and the traps that bit twice
2. `README.md` — how to use the tool
3. `documentation/specification.md` — normative
4. `documentation/decisions.md` — rationale, frozen interfaces, A1–A12
5. `documentation/COLDSTART.md` — tests the instructions, not the tool
6. `documentation/retrospective-s4.md` — **error bias per check**, and why a checker must run on its own codebase
7. `documentation/retrospective-s2.md` — sentinel values; three bugs in green tests
8. `documentation/retrospective-s1.md` — the `go/ast` position model; why `Detail` stays a token
9. `TESTING.md` — eighteen manual cases; **run them against a real repository after any engine or check change**

## Spec review — done, and what it changed

`retrospective-s4.md` flagged "final line-by-line review against the spec" as never done, every
session having checked only its own surface. It is now done: every clause of `specification.md`
was checked against the built binary, not against the code, so a divergence between what the spec
promises and what the tool does could not hide behind a passing test.

Most of it held. `outline`'s header, group layout, depth semantics and `(line N)` / `(lines N-M)`
forms match the sample exactly. All four payloads put `schema_version` first. `get --bounds-only
--json` is byte-identical to `bounds --json`. The byte invariant holds by slicing the file.
`schema_version`, the exit-code table, `bounds` rejecting a directory, ambiguity listing
candidates on stderr, the `init`/`_` exemption, external test packages as distinct namespaces,
and mutually exclusive platform variants all behave as specified. One true positive was confirmed
against `go vet` rather than trusted.

One defect came out of it, and it was not cosmetic: `outline --json` dropped `type_params`,
`receiver` and `is_alias`. See `decisions.md` D23. The generalisable finding is in the AGENTS.md
traps — a field that changes what a declaration head *means* has to reach every renderer, and a
test on one renderer cannot see the other's divergence.