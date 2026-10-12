# AGENTS.md

Instructions for agents working in this repository.

## What this is

golens is a Go library and CLI that answers structural questions about Go source: what is in
this file, where is this symbol, what are its exact byte offsets, is this package valid. It
exists so an agent can read a targeted slice of a file instead of the whole thing.

The consumer is an agent, so output shape is a contract, not a preference. Two properties are
load-bearing and are asserted by tests:

- `file[start_byte:end_byte]` equals the source `get` returned for the same symbol, byte for
  byte.
- Byte ranges are half-open; line ranges are inclusive at both ends; the terminating newline is
  outside the span.

Do not change either without reading `documentation/decisions.md` first.

## State

Feature-complete. There is no next session, and nothing is blocked.

| Field | Value |
|---|---|
| Tests | 336 (incl. subtests), 6 packages with tests, all passing |
| Test count by package | arch 6 · cli 87 · domain 15 · goast 48 · format 23 · usecase 157 |
| Verify with | **`go test -count=1 ./...`** — see the caching trap below |
| Toolchain | go1.27.2; no `golang.org/x` dependencies, only cobra and its two indirects |
| Binary | 5.33 MiB stripped, budget 15 MiB |
| Parse budget | ~22 µs typical, ~273 µs largest file here (budget 5 ms P95) |
| Lint | `golangci-lint run ./...` — 0 issues, includes `revive` `var-naming` |
| Self-check | `golens check ./internal` and `./cmd` both exit 0 |
| Remote | `github.com/CodeMonkey80s/golens`, public; `go install ...@latest` verified working |

Parse dominates three of the four commands, and `check` is about ten times a parse because it runs
several analyses. Reproduce with
`go test -run XXX -bench . ./internal/usecase ./internal/engine/goast`; expect roughly ±20%
run-to-run, so hold to the budget rather than to a single figure.

The binary size is quoted stripped, in MiB, which is the unit CI prints and budgets in.
`go install` produces 8.07 MiB — the same code without `-ldflags="-s -w"` — so do not read the
two figures as a contradiction. Both clear the budget.

**The `v0.1.0` tag predates the version injection.** It was cut while `Version` was still a
hardcoded constant, so `go install …@latest` installs the pre-fix code and reports `0.1.0` from
the source. `main` has the fix; the next tag ships it. Nothing is blocked, and nothing in the
tree states a release number — see the injection trap below.

## Build and test

```console
$ go build ./...             # build
$ go test -count=1 ./...     # full suite — the -count=1 is required, see below
$ go vet ./...
$ gofmt -l .                 # must print nothing
$ golangci-lint run ./...    # includes revive's var-naming
```

There is no `-short` mode, deliberately. The whole suite runs in about a quarter of a second, so a
fast path would buy nothing — and it would let `go test -short ./...` report `ok` while skipping the
tree-walking tests most likely to catch a regression. A green result that means less than it looks
like is the failure mode this repository keeps running into: a subset assertion satisfied by the
empty set, `go test` reporting a stale pass, help text contradicting a test that passes.

**`go test ./...` without `-count=1` can report a stale pass.** Three packages assert facts about
the build graph or the source tree rather than about their own test inputs: `internal/arch` (shells
out to `go list -deps`), plus `selfcheck_test.go` and `TestRepositoryPassesItsOwnCheck` (walk the
tree). Go's cache does not invalidate on those. This hid a real failure during S1. CI enforces it.

`internal/arch` asserts layering: the CLI must not import an engine, and the domain must not
import `go/ast`. It fails the build if those boundaries are crossed, which is deliberate.

**Naming is the linter's job, not golens'.** `golens check` reports structural faults and never
style. This repository follows Go's initialism convention — `userID` and `HTTPHandler`, not
`userId` and `HttpHandler` — and `revive`'s `var-naming` rule enforces it in CI. An initialism
stays lowercase only at the front of an identifier that already begins lowercase, so `urlPath`
is correct. Do not add a naming check to the tool: `decisions.md` D9 records why, including the
4,119 false positives an earlier version produced on the standard library. The full three-attempt
history is in "Not doing, and why" below.

## Layout

| Path | Holds |
|---|---|
| `cmd/golens` | main; composition only |
| `internal/cli` | cobra wiring, exit codes, stream routing |
| `internal/usecase` | command behaviour, the checks |
| `internal/engine/goast` | AST to domain model |
| `internal/domain` | shared types, including `Declaration()` |
| `internal/format` | human and JSON rendering |
| `internal/constructs` | compiled declarations the cross-renderer corpus walk needs; not imported by anything |

## Traps

**`domain.Node.Receiver` is the receiver as the source spells it**, `"s *UserStore"`, not a bare
type. If you change it, grep for `Receiver` in `internal/usecase` — the naming check that once
consumed it is gone, but the format is still load-bearing for `get` and `bounds`.

**The release number is injected, never declared.** `cli.Version` is a `var` initialised to a
constant string expression, set at link time with `-X`. It was `const Version = "0.1.0"`, so
every build ever produced reported v0.1.0 — including a build made after v0.2.0 shipped, which is
worse than reporting nothing, because a caller checking the version is told a falsehood. Two
things follow, and both are load-bearing. The variable must stay initialised to a **constant
string expression**: the linker documents `-X` as ignoring a variable whose initialiser is a
call, and it does so **without failing the build**, so replacing the literal with a resolver
function breaks releases silently. And the source must stay free of release numbers.
`TestVersionIsInjectedByLdflags` builds the binary with a stamp and reads it back;
`TestVersionDefaultCarriesNoReleaseNumber` fails if a number reappears in the source. Where no
flag was passed, `releaseVersion()` falls back to what `debug.ReadBuildInfo()` recorded — that is
what makes `go install …@v0.1.0` report v0.1.0 without anyone passing anything. **Read what that
fallback actually prints before documenting it:** it is a pseudo-version derived from the nearest
tag, so a clean checkout at `v0.1.0-1-g1cd5ab9` reports `v0.1.1-0.20261012165945-1cd5ab9df783`,
a dirty one appends `+dirty`, and `(devel)` appears only where there is no VCS information at
all. A README sentence claiming a plain `go build` prints `(devel)` was wrong for every clone
that has the tag, and nothing caught it because no test pinned the claim.

**Two syscalls can disagree about the same path.** `Files` asked `os.Stat` whether the target
was a directory — which follows a symlink — then handed the same path to `filepath.WalkDir`,
which does not follow one. The walk received the symlink itself as a non-directory, the
callback treated it as a file, and the run collected nothing. `golens outline link` reported "no
.go files found" and **exit `2`** — the code this tool defines as *"you named something that is
not there"* — for a directory holding real source. Symlinked trees are ordinary, not exotic.
Only the root is resolved now (D26); a test fails if inner symlinks ever start being followed,
because that is loop protection.

**A partial search must not answer like a complete one.** `get` returned on the first parse
error, so one half-edited file made every symbol in the directory unreachable — while `outline`
and `check` both survived it. The asymmetry was invisible because `get` returns one symbol and
"partial output" has no obvious meaning there. It does have meaning: the symbol either resolves
or it does not, and the unreadable files bear only on whether the answer is complete. So `get`
returns the symbol, names the skips, and **still exits `4`** — a caller that branched on 0 was
told the tree was fully readable, and it was not. A miss still exits `2`, and says how many files
could not be parsed, because "not found" from a partial search is not the claim "not found" makes
from a complete one. Ambiguity gets the same caveat: the candidate list may have a gap in it.
An honest exit code that looks like failure on a successful lookup is the correct trade.

**Declaration heads must match the source.** A rendered head that does not is an edit hazard: an
agent copying `type Stack struct` for a real `type Stack[T any] struct` writes code that will not
compile. `TestDeclarationHeadsMatchSource` in `internal/engine/goast` pins the constructs that
previously diverged, including the alias `=` and the receiver variable name.

**A field that changes what a head means must reach every renderer.** `domain.Node.Declaration()`
is the only place a head is assembled, and it reads `Name`, `TypeParams`, `Receiver`, `Detail` and
`IsAlias`. The human outline calls it. `format.convertNodes` does not — it copies fields by hand,
and when it did, `outline --json` emitted an alias and a defined type over the same underlying type
as two byte-identical nodes. `TestDeclarationHeadsMatchSource` passed throughout, because it only
ever looked at the human path. When you add a field that a declaration head depends on, add it to
`format.OutlineNode` too; `TestRenderersAgreeOnTheWholeCorpus` in `internal/format` then checks
every declaration in this repository rather than the constructs a test remembered to name.

**A missing field is worse than a wrong line number.** `typeChildren` emitted one node per
`ast.Field`, but `A, B int` is one `ast.Field` holding two names. `B` was absent from the human
outline, the JSON outline and the compact outline alike — not mis-rendered, *absent* — so the
field list an agent reads before editing a struct simply disagreed with the struct. Parameters
were already handled per name by `fieldNodes`, which is why the tell was the asymmetry between
the two: the same source construct worked in a signature and not in a struct. This is the third
defect in this repository to work by **omission** (after the alias `=` and the dropped
`is_alias`), and the third to ship because the corpus did not contain the construct. That is why
`internal/constructs` now declares a multi-name field, and why
`TestCorpusContainsTheConstructsThatBroke` counts `multiNameField` alongside the rest — otherwise
the cross-renderer walk passes at 1,300 declarations while proving nothing about it.

**An invariant nobody enumerates is an invariant nobody checks.** The alias `=` broke twice — once
in `Declaration`, once in the JSON path — because the tests named the constructs they remembered.
`TestRenderersAgreeOnTheWholeCorpus` walks every non-test `.go` file here and compares the two
renderings at three depths, in both directions, asserting that a JSON head reassembled from its
serialised fields appears in the human output. Its corpus is the repository itself, which is why
`internal/constructs` exists: it declares generics, aliases, an embedded field and a generic
receiver as **real compiled code**, so the walk has something to walk. Fixture text in a backtick
literal would not do — `go/ast` does not read test data as code, which is correct and also why the
corpus had to be extended rather than the test pointed at the fixtures.
`TestCorpusContainsTheConstructsThatBroke` fails if a construct is ever removed from it.

**Compare renderings in both directions, and by count.** A subset assertion is satisfied by the
empty set, which is how `outline --json --depth 1` shipped returning four group headings and none
of the declarations while its test passed.

**Help text must not state a behaviour no test pins.** `check`'s help said "stdout stays empty"
while `TestStdoutStaysCleanOnError` proved `check --json` writes a payload there. The suite was
green and the documentation was wrong, because nothing connected a claim about behaviour to the
test verifying it. `TestCheckHelpStatesTheStreamContract` makes that connection; add the same kind
of test whenever help states something a test could check.

**Verify a recorded console output against the binary.** Every output block in the README that names
a file in this repository is re-run by `TestReadmeExamplesAreReproducible`, which builds the binary
and diffs it. Two blocks had drifted before it existed — a stale outline example and two disagreeing
binary sizes — and hand-checking found those while failing to keep them current. Blocks referring to
the specification's fictional `user.go` are not checkable and say so;
`TestReadmeMarksItsUnverifiedExamples` fails on any block that is neither verified nor marked. The
scope is deliberately narrow: output is machine-checkable, rationale is not, and prose must not be
generated.

**An outline head is not the byte span.** `get` and `bounds` report a name-scoped span, but the
outline head is the whole declaration. For `var counter int` the span covers `counter` alone, so
pasting the head into it yields `var var counter int int`. That is a consequence of the
name-scoped span the spec mandates, not a bug — edit with the text `get` returned, which is what
the `bounds` help text says.

**Group nodes are transparent; they do not consume a depth level.** `compactNodes` and
`convertNodes` must agree on this or `--depth` means different things per output format. It went
wrong once: `convertNodes` counted a group as a level, so `outline --json --depth 1` returned the
four headings and none of the declarations. When you change depth handling, compare the declaration
**counts** of the two renderings at every depth; they must be equal.

**`check` deliberately under-reports.** A false positive on an import that is in use would have
an agent delete working code, so the check stays silent whenever it cannot determine the local
name. When adding a check, bias toward silence and say so in the comment.

## Acceptance criteria

Eighteen, all green. Evidence is a test, not a claim. `documentation/decisions.md` §4 states the
criteria; this is which test pins each.

| # | Criterion | Evidence |
|---|---|---|
| A1 | `file[start:end]` == extracted source | `TestByteInvariantThroughGet` across 12 fixture classes, `TestOverwriteRoundTrip`, `TestOverwritePreservesLineStructure` |
| A2 | Two runs → byte-identical stdout | `TestOutlineDirectoryIsDeterministic`, `TestConcurrentDeterministicAcrossRuns` |
| A3 | Concurrent == serial output | `TestConcurrentOutputMatchesSerial`, all four output modes, tree sizes straddling the threshold |
| A4 | JSON parses, `schema_version: 1` first | `TestSpecSchemaVersionIsFirstField`, all four commands |
| A5 | `--compact` loses no information | `TestCompactIsLosslessSubset` |
| A6 | `get --bounds-only` ≡ `bounds` | `TestSpecBoundsOnlyMatchesBounds`, plus format and usecase level |
| A7 | golens never reports style | `TestCheckDoesNotJudgeStyle`, `TestCheckAcrossFilesReportsNothingStructural` |
| A8 | Parse < 5 ms P95 | `TestParseP95UnderBudget` |
| A9 | Binary < 15 MiB | 5.33 MiB, gated in CI |
| A10 | No cgo | `TestNoCgoDependency` |
| A11 | `domain`/`usecase` import nothing external | `internal/arch` |
| A12 | 12 fixture classes | `TestFixtureClasses` |
| A13 | Platform variants silent; untagged pair exits 3 | `TestPlatformVariantsDoNotConflict` (5 pairs), `TestCoBuildableFilesStillConflict` (**positive control**), `TestImplicitFilenameConstraints`, `TestFilenameAndTagConstraintsCombine`, `TestCgoFilesAreExcludedWhenCgoIsOff` |
| A14 | Partial tree: parseable stdout, skipped named, exit 4 | `TestOutlineSkipsUnparseableFilesWithoutHidingTheRest`, `TestOutlineReportsSkippedFilesExitFour`, `TestOutlineJSONStaysParseableWhenAFileIsBroken`, `TestOutlineSingleBrokenFileKeepsTheOriginalMessage` |
| A15 | No release number in the source; the tag is the only source | `TestVersionIsInjectedByLdflags` (builds with a stamp, runs the binary), `TestVersionDefaultCarriesNoReleaseNumber`, `TestReleaseVersionPrefersTheLinkerValue`, `TestReleaseVersionFallsBackToBuildMetadata` |
| A16 | `get` survives a broken neighbour: returns the symbol, exits 4, names the skips; a miss still exits 2 and says the search was incomplete | `TestGetSkipsUnparseableFilesRatherThanAborting`, `TestGetSkipsUnparseableFiles`, `TestNotFoundNamesSkipsOnlyWhenItMust` |
| A17 | A symlinked directory is walked; paths reported under the name given; inner symlinks still not followed | `TestSymlinkedDirectoryIsWalked`, `TestSymlinkedDirectoryIsNotEmpty` |
| A18 | Every struct field appears in the outline, one node per name, each with bounds selecting its own name | `TestMultiNameStructFieldsAreNotDropped`, `TestMultiNameFieldsGetTheirOwnBounds`, `TestEmbeddedFieldIsStillOneNode` |

A13's positive control is the part that matters: a rule that suppressed every duplicate would
otherwise pass the five variant cases. `internal/cli/spec_test.go` adds 11 more, each citing the
specification clause it verifies.

## Frozen interfaces

Unchanged since S1 except `Node.IsGroup` (S2), and `TypeParams` plus `IsAlias` on `Node`/`Symbol`.
`documentation/decisions.md` §2 has the canonical copy with rationale.

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

`domain.LocatedError` carries a parse error's file, line and column. It lives in `domain` rather
than `engine/goast` because both layers need it and `internal/arch` forbids `usecase` importing
the engine. It exists because the engine used to return pre-formatted `"file:line:col: message"`
strings, which left nowhere to put the position — `check` reported every parse error at `1:1`, and
an agent following the documented format went to line 1 whatever the real position was.

## Not doing, and why

| Item | Why not |
|---|---|
| **`var x = 5` shows no `= 5`** | Correct, not a gap. The outline head is *not* the byte span: the span is name-scoped, so pasting `var x = 5` into a span holding `x` writes `var x = 5 = 5`, which does not parse. `var counter int` pastes the same way. Adding `= 5` makes the head *more* divergent from the span while making nothing paste-safe |
| **`check .` exits 3 on `testdata/store/store.go`** | Intentional — the unused import gives the outline golden a stable shape |
| Unused-import false negatives | Accepted: comment-only references and shadowed locals. Never false positives, verified against three repositories. A miss is caught by `go build`; a false positive deletes working code |
| Naming outside a module | `revive` cannot run without a `go.mod`. Filling that gap with golens would make a structural tool the one that grades code — `decisions.md` D9 |
| `internal/naming`, `check --naming` | Removed. Three attempts, then deletion: `camelCaseId` produced 4,119 false positives on the standard library; the correct in-tree rule was indistinguishable from `revive`; so it was removed rather than kept as a second implementation. Two tests pin the boundary so it cannot return through a new flag |

## Defects the plan shipped, and what each cost

Kept because each one is a bug class rather than an instance. `git log` has the commits; the point
here is the shape of the failure.

| Defect | Why it matters |
|---|---|
| Generic type parameters dropped from heads | `type Stack[T any] struct` rendered `type Stack struct`. An agent copying it writes code that will not compile |
| Receiver variable invented | Every method rendered `(s ...)` regardless of source — all 61 in the test repo |
| `type Alias = X` rendered `type Alias X` | **The worst one.** Every sibling defect produced code that failed to compile; this compiled and meant something else — the alias stops being interchangeable with `X`, and the name stops being a synonym for it. A silently wrong declaration is worse than a loud one |
| `/vN` suffixes bound as the package name | `math/rand/v2` bound as `v2`, so the unused-import check would call a live import dead |
| Redeclarations across exclusive build constraints | **24 false positives on duf**, which compiles cleanly on five platforms |
| `outline --json` dropped `is_alias` | The alias defect again, through the other renderer. See D23 |
| `outline --json --depth 1` returned no declarations | `--depth` was useless to a machine consumer; depth 2 was the only depth that returned anything |

The last two arrived after the project was called complete, and both were found by **running the
tool and reading the spec** — not by reading the code, and not by the unit tests. `outline --json`
had two renderers of one document that disagreed about both a field and a depth rule, and the test
that should have caught it asserted only one direction. A subset assertion is satisfied by the empty
set.

## Testing this tool

`TESTING.md` is an eighteen-case manual plan to run against a real repository, not a fixture. It
found the declaration-head divergences, the unused-import false positive and the
build-constraint false positives, none of which any unit test had caught. Cases 10 and 16 corrupt
a file without raising anything, which is why they lead the short list — and case 16 fails only on
some layouts, so one clean run is not evidence the approach is safe.

**Run its preflight first.** Cases 4, 6, 8 and 9 probe constructs the fixtures do not cover, so
a repository lacking the construct cannot fail them — the case runs, passes, and tests nothing.
The preflight prints which constructs the target actually has. A zero is not a failure, it is a
gap in coverage: find another repository or record the case as unexercised. Never record a
vacuous case as a pass.

Run the plan against a real Go project after any change to the engine, the checks or the formatters,
and treat every deviation as worth reporting — distinguish *wrong* from merely *ugly*, and only one
of those is a bug. **The standard library is a target**: `stdlib/slices` declares 86 generics and
covers case 6, which no repository in `~/Development` does.

## Where things are

`golens --help` and `golens <command> --help` carry the per-command conventions: byte-offset
semantics, exit codes, and which stream each command writes to. **Keep conventions in the help
text rather than duplicating them in a document — help cannot drift from the binary.** A change to
any command's help should be followed by `documentation/COLDSTART.md`.

| Document | Read it for |
|---|---|
| `documentation/specification.md` | **Normative.** Change this first, then the code |
| `documentation/decisions.md` | Why each call was made; frozen interfaces; A1–A18 stated |
| `documentation/AGENTS.template.md` | Copy into a target repo's root so its agents inherit the traps |
| `TESTING.md` | The 18-case manual plan — run it after any engine, check or formatter change |
| `documentation/COLDSTART.md` | Tests the instructions rather than the tool. Run after changing help text |
| `documentation/implementation-plan.md` | The five sessions, now historical |
| `documentation/retrospective-s1/s2/s4.md` | Where the design came from, including three bugs in green tests |
| `README.md` | How to use the tool. Read by agents using it, not by agents working on it |

The retrospectives and the plan are history. They are kept because each records a *class* of
mistake rather than an instance, and because `AGENTS.md` alone would not tell you why the obvious
change is the wrong one.

## Commit message convention

Use a lightweight `area: brief description` format. Prefix every commit with one of:

| Prefix | When to use |
|--------|-------------|
| `feat:` | New user-visible feature or capability |
| `fix:` | Bug fix |
| `refactor:` | Code restructuring without behavior change |
| `test:` | Test additions, fixes, or infrastructure |
| `docs:` | Documentation only |
| `ci:` | CI/CD configuration |
| `cli:` | CLI-specific changes (flags, help, wiring) |
| `engine:` | Engine/internal AST/domain changes |

**Rules:**
- Subject line: imperative mood, lowercase after prefix, no trailing period
- Body (optional): explain *why* and *what changed*, not *how*
- One logical change per commit; split if `and` appears in subject

Examples:
```
feat: render type parameters on generic functions
fix: report real positions in parse diagnostics, align spec
test: add preflight so hard cases cannot pass vacuously
refactor: remove naming check, golens describes code not grades
cli: show build constraint, advise disambiguation, add --version
docs: update binary size to 5.4 MB stripped
ci: add golangci-lint with minimal agent-shaped linter set
```
