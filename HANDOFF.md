# HANDOFF — golens

**Status: S5 COMPLETE. All five sessions done.** `go test -count=1 ./...` green (136 tests),
`go vet` clean, `gofmt` clean, clean under `-race`.

**All twelve acceptance criteria (A1–A12) are green.**

---

## Current state

| Field | Value |
|---|---|
| Session | S5 complete — harden + ship. Plan finished |
| Branch | `s5-harden-ship` (tip `43ae86d`) |
| Tests | 136 top-level, 7 packages, all passing |
| Test count by package | arch 7 · cli 26 · domain 7 · goast 16 · format 19 · naming 8 · usecase 53 |
| Verify with | **`go test -count=1 ./...`** — see caching note |
| Binary | 3.92 MB (budget 15 MB) |
| Parse budget | ~13 µs typical, 282 µs largest file (budget 5 ms) |
| Self-check | `golens check ./internal`, `./cmd`, and `check . --naming` all exit 0 |
| Remote | **none configured** — nothing has been or can be pushed |

### Branch map

```
main            ddcd80a  S1: foundation, engine, and all four commands
s2-outline      5963ae6  S2: outline golden tests, --compact, fix unlimited depth
s3-get-bounds   554ef6d  S3: byte invariant through get, overwrite round trip
s4-check-naming 675d0dc  S4: check test suite, cross-file conflicts, false positive fixed
                 f5f85c5  docs: S4 retrospective and a current handoff
s5-harden-ship  43ae86d  S5: concurrency (A3), spec conformance, and a README  ← you are here
```

Five branches, none merged. `main` still holds S1 only.

## Landed in S5

| Item | Detail |
|---|---|
| **A3 concurrency** | `parse.go`: goroutine per file, bounded by GOMAXPROCS, positional results. Sequential below 8 files and for single files |
| **A3 proof** | Renders with `GOMAXPROCS=1` and compares byte-for-byte, across all four output modes and tree sizes straddling the threshold. Clean under `-race` |
| **Spec conformance suite** | `internal/cli/spec_test.go` — 12 tests, each citing the specification clause it verifies |
| **README** | Install, four commands with real output, exit codes, byte-offset convention, agent workflow, layout, performance |
| **Import-path check** | Verified reachable, extended to catch escape sequences |

## Bugs found in S5

| Bug | Impact |
|---|---|
| `parseAll` returning errors broke `check` | `check`'s contract is that a parse failure is a *diagnostic*, not an abort. Now returns partial results plus every error |
| `classifyUsage` wrapped every unrecognised error as usage | `outline` on an unparseable file exited 1 instead of 4, telling the caller it had invoked the tool wrongly |
| `get` printed `Lines: lines 28-35` and `1 lines` | Spec says `Lines: 17-25 (9 lines)`. **My own S3 golden test had enshrined the divergence** |

That last one is the most useful result of the session. Four sessions had verified their own
surfaces; the line-by-line spec review is what caught a claim that was tested, passing, and wrong.

## Acceptance criteria — all green

| # | Criterion | Evidence |
|---|---|---|
| A1 | `file[start:end]` == extracted source | Property test across 12 fixture classes, plus an overwrite round trip that re-parses |
| A2 | Two runs → byte-identical stdout | `TestOutlineDirectoryIsDeterministic` |
| A3 | Concurrent == serial output | `TestConcurrentOutputMatchesSerial`, all 4 output modes |
| A4 | JSON parses, `schema_version: 1` first | `TestSpecSchemaVersionIsFirstField`, all 4 commands |
| A5 | `--compact` loses no information | `TestCompactIsLosslessSubset` |
| A6 | `get --bounds-only` ≡ `bounds` | Asserted at format, usecase and CLI level |
| A7 | Repo-wide naming test clean | `TestRepositoryFollowsNamingConvention` |
| A8 | Parse < 5 ms P95 | Benchmark-gated test |
| A9 | Binary < 15 MB | 3.92 MB |
| A10 | No cgo | `TestNoCgoDependency` |
| A11 | `domain`/`usecase` import nothing external | `internal/arch` |
| A12 | 12 fixture classes | `TestFixtureClasses` |

## Caching note

`go test ./...` **without `-count=1` can report a stale pass.** Four packages assert facts about
the build graph or the source tree rather than about their own test inputs: `internal/arch`
(`go list -deps`), `internal/naming` (scans this repo), `selfcheck_test.go` and
`TestRepositoryPassesItsOwnCheck` (walk the tree). Go's cache does not invalidate on those.

This hid a real failure during S1. **Always use `go test -count=1 ./...` here.**

## Frozen interfaces

Unchanged since S1 except `Node.IsGroup` (S2). See `decisions.md` §2 for the canonical copy.

```go
type Bounds struct{ StartLine, EndLine, StartByte, EndByte int }
type Node struct {
    Name, Detail, Receiver string
    Kind     Kind
    Bounds   Bounds
    Children []Node
    IsGroup  bool
}
type Symbol struct{ Name, QualifiedName string; Kind Kind; Bounds Bounds; File, Detail, Receiver string }
type Document struct{ Path, Package string; Bounds Bounds; Nodes []Node; Symbols []Symbol; Imports []string; Source []byte; Comments bool }
type Engine interface{ Parse(path string) (Document, error) }
```

## Remaining work

The plan is finished. These are optional, none blocking.

| Item | Notes |
|---|---|
| **Merge branches to `main`** | Five branches, none merged. `main` holds S1 only |
| `LICENSE` | README says "not yet specified" — deliberately, rather than claiming MIT without a file |
| GoReleaser or CI workflow | Not in scope; a `.github/workflows` with the `-count=1` command would be the minimum |
| Tag / publish | No remote configured |
| `var x = 5` shows no `= 5` in outline | Cosmetic; `KindVar`/`KindConst` `Detail` carries the type only |
| `type X = Y` renders `type Alias User`, losing `=` | Cosmetic |
| `check .` exits 3 on `testdata/store/store.go` | Intentional — the unused import gives the outline golden a stable shape. Worth a comment in the fixture |
| Unused-import false negatives | Accepted: comment-only references and shadowed locals. Never false positives, verified empirically |
| `userIdX` naming false positive | Accepted: resolving it needs an English dictionary, not an approved dependency |

## Read first

1. `README.md` — how to use the tool
2. `documentation/retrospective-s4.md` — **error bias per check**, and why a checker must run on its own codebase
3. `documentation/retrospective-s2.md` — sentinel values; three bugs in green tests
4. `documentation/retrospective-s1.md` — the `go/ast` position model; why `Detail` stays a token
5. `documentation/specification.md` — normative
6. `documentation/decisions.md` — rationale, frozen interfaces, A1–A12
