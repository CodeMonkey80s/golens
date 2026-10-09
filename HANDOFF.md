# HANDOFF — golens

**Status: S1–S4 COMPLETE.** All four commands work end to end. `go test -count=1 ./...` green (114 tests), `go vet` clean, `gofmt` clean.

**S5 — harden + ship — is the only session remaining.**

---

## Current state

| Field | Value |
|---|---|
| Session | S4 complete — check + naming. S5 not started |
| Branch | `s4-check-naming` (tip `675d0dc`). `main` holds S1 at `ddcd80a` |
| Test suite | 114 top-level tests, 7 packages, all passing |
| Test count by package | arch 7 · cli 15 · domain 7 · goast 16 · format 19 · naming 8 · usecase 38 |
| Verify with | **`go test -count=1 ./...`** — see the caching note below |
| Blockers | none |
| Binary | 3.90 MB (budget 15 MB) — A9 met |
| Parse budget | P95 12.7 µs fixture / 282 µs largest repo file (budget 5 ms) — A8 met |
| Self-check | `golens check ./internal ./cmd` exits 0; `check .` exits 3 only on the intentionally imperfect `testdata/` fixture |

### Branch map

```
main            ddcd80a  S1: foundation, engine, and all four commands
s2-outline      5963ae6  S2: outline golden tests, --compact, fix unlimited depth
s3-get-bounds   554ef6d  S3: byte invariant through get, overwrite round trip
s4-check-naming 675d0dc  S4: check test suite, cross-file conflicts, false positive fixed  ← you are here
```

Not yet merged to `main`. S5 should either branch from `s4-check-naming` or `main` should be
fast-forwarded first — the latter is cleaner, and merging to a known-good `main` between phases
is worth the two minutes.

## Landed

### Packages

| Path | Contents |
|---|---|
| `cmd/golens/main.go` | Composes engine + cli, exits with the returned code |
| `internal/domain/` | `Bounds`, `Node`, `Symbol`, `Document`, `Engine`, `Kind`, `ErrorExit` |
| `internal/engine/goast/` | Only package importing `go/parser`, `go/ast`, `go/token`. `Engine`, `ParseError` |
| `internal/format/` | Human, JSON, compact renderers |
| `internal/usecase/` | `Outline`, `Get`, `Bounds`, `Resolve`, `Check`, `Files`, error classification |
| `internal/cli/` | Cobra wiring, exit-code mapping. Imports no engine |
| `internal/naming/` | `camelCaseId` checker + initialism dictionary |
| `internal/testfixture/` | Shared Go sources for the twelve fixture classes |
| `internal/arch/` | Structural tests enforcing package boundaries |

### Commands working

```
golens outline <file|dir>          --depth N --json --compact
golens get <symbol> <file|dir>     --bounds-only --json --compact
golens bounds <symbol> <file>      --json --compact
golens check <file|dir>            --naming --json --compact
```

### Acceptance criteria

| # | Criterion | Status |
|---|---|---|
| A1 | `file[start:end]` == extracted source, byte for byte | **Proven** at engine, `get`, and CLI level, incl. the overwrite round trip |
| A2 | Two runs → byte-identical stdout | **Proven** |
| A3 | Directory output independent of concurrency | **NOT PROVEN** — no concurrent path exists. S5 |
| A4 | Every JSON payload parses, carries `schema_version: 1` | **Proven** |
| A5 | `--compact` loses no information | **Proven** |
| A6 | `get --bounds-only` ≡ `bounds` payload | **Proven** at format and usecase level |
| A7 | Repo-wide naming test, zero violations | **Proven** |
| A8 | Parse < 5 ms P95 | **Proven** — benchmark-gated |
| A9 | Binary < 15 MB | **Proven** — 3.90 MB |
| A10 | No cgo | **Proven** |
| A11 | `domain`/`usecase` import nothing external | **Proven** |
| A12 | 12 fixture classes | **Proven** |

**A3 is the only open criterion.**

## Caching note — read before trusting a green run

`go test ./...` **without `-count=1` can report a stale pass.** Go keys the cache on test inputs,
but `internal/arch` asserts facts about the *build graph* via `go list -deps`, `internal/naming`
scans the repo's own files, and `selfcheck_test.go` walks the whole tree. None of those results
depend on the test file, so editing an import does not invalidate them.

This already hid one real failure during S1. **Always use `go test -count=1 ./...` here.**

## Frozen interfaces (do not change without updating `decisions.md` §2)

```go
type Bounds struct{ StartLine, EndLine, StartByte, EndByte int }
func (b Bounds) Valid() bool
func (b Bounds) LineCount() int
func (b Bounds) ByteLen() int
func (b Bounds) LineRange() string
func (b Bounds) Source(src []byte) string

type Node struct {
    Name, Detail, Receiver string
    Kind     Kind
    Bounds   Bounds
    Children []Node
    IsGroup  bool   // synthetic presentation container, not a declaration
}
func (n Node) Declaration() string
func (n Node) Keyword() string

type Symbol struct{ Name, QualifiedName string; Kind Kind; Bounds Bounds; File, Detail, Receiver string }
type Document struct{ Path, Package string; Bounds Bounds; Nodes []Node; Symbols []Symbol; Imports []string; Source []byte; Comments bool }
func (d *Document) Symbol(name string) (Symbol, bool)
func (d *Document) Lookup(name string) []Symbol

type Engine interface{ Parse(path string) (Document, error) }

type ErrorExit int
const (ExitOk ErrorExit = iota; ExitUsage; ExitNotFound; ExitDiagnostics; ExitParse)
```

`Node.IsGroup` was added in S2. A formatter must not classify groups by "has children" — that
silently drops every struct with fields.

## Decisions taken during implementation

Recorded in `decisions.md` §2 plus:

| Decision | Session | Reason |
|---|---|---|
| Missing file exits **2**, not 4 | S1 | Both mean "you named something absent"; 4 is for exists-but-won't-parse |
| Const/var bounds span the **name**, not the spec | S1 | So `const (A=1; B=2)` gives two independently editable symbols |
| Vars/consts get their own `VARS & CONSTS` group | S1 | Folding them into `TYPES & STRUCTS` was misleading |
| Struct `Detail` is the keyword | S1 | `type User struct`, matching the spec. Expanding the body defeats the token budget |
| Methods render with the `func` keyword | S1 | `func (s *T) M()`, as the spec shows |
| `GetPayload` has no `lines` field | S1 | Would break A6 |
| Engine composed in `cmd/`, not `cli` | S1 | `cli` importing an engine pulled `go/ast` into cobra |
| `check --naming` replaces, not augments | S1 | Naming and syntax are separate concerns |
| Naming rule: ≥2 capitals at a word boundary, in the dictionary | S1 | Derived from the spec's own examples |
| `usecase` may depend on `naming` | S1 | Leaf package like `format`; `TestNamingIsALeaf` guards it |
| `visible()` helper for the depth sentinel | S2 | `depth > level` made unlimited silently equal depth 1 |
| `Node.IsGroup` rather than shape inference | S2 | "Has children" is not a type test |
| Namespace key is `dir + package`, not dir | S4 | A directory may hold both `foo` and `foo_test` |
| Per-file duplicate check **deleted** | S4 | The package pass subsumes it; both fired, double-reporting |

## Gaps and known limits

| Gap | Impact | Pick up in |
|---|---|---|
| **A3: no concurrent directory parse** | The one unproven criterion | S5 |
| No README | A user cannot read the tool without reading the spec | **S5** |
| No line-by-line review against the full spec | Each session checked only its own surface. Something outside all four sessions may be missing | **S5** |
| `invalidImportPaths` untested, possibly unreachable | Go's parser rejects empty and whitespace paths first. Verify, then test or delete | S5 |
| `check .` exits 3 on `testdata/store/store.go` | Intentional — the unused import gives the outline golden a stable shape | Note in the fixture, or move it out of the check path |
| Unused-import check is lexical | False negatives on shadowed or comment-only references. Never false positives — verified empirically in S4 | Accepted, documented in the spec |
| `userIdX` naming false positive | Needs an English dictionary to resolve; not an approved dependency | Accepted, documented |
| `var x = 5` shows no `= 5` in outline | Cosmetic; `KindVar`/`KindConst` `Detail` carries the type only | S5 if time |
| `type X = Y` renders `type Alias User`, losing `=` | Cosmetic | S5 if time |

## Next

**S5 — harden + ship.** Tasks 9, 12, 13. Estimated 2–4h; mostly verification rather than
discovery, which is why it should be the shortest remaining session.

- [ ] **A3**: concurrent directory parse bounded by GOMAXPROCS, reassembled in lexical order so output is identical to the serial path. Add a test asserting serial == concurrent output
- [ ] **README**: install, the four commands with real output examples, the exit-code table, and the byte-offset convention explained
- [ ] **Line-by-line review** of `specification.md` against the implementation — every command, flag, and documented behaviour. This is the one thing four sessions have collectively not done
- [ ] Decide `invalidImportPaths`: test it or delete it
- [ ] Final `gofmt -l`, `go vet`, `go test -count=1 ./...`, `CGO_ENABLED=0` build, binary size, benchmark
- [ ] Merge to `main`, tag if desired

### S5 exit criteria

- [ ] A3 proven: concurrent output byte-identical to serial
- [ ] README exists and a newcomer could use the tool from it
- [ ] Every spec claim verified against the implementation, discrepancies either fixed or documented
- [ ] All A1–A12 green
- [ ] `go test -count=1 ./...` green, `vet` and `gofmt` clean

## Open questions

None blocking. Two cosmetic items above need a decision but neither blocks S5.

## Read first

1. `documentation/retrospective-s4.md` — **the bias lesson**: two checks needing opposite error directions, and why running a checker on its own codebase is mandatory
2. `documentation/retrospective-s2.md` — sentinel values, and three bugs that sat in green tests
3. `documentation/retrospective-s1.md` — the `go/ast` position model, why `Detail` stays a token
4. `documentation/specification.md` — normative
5. `documentation/decisions.md` — rationale, frozen interfaces, A1–A12
6. `documentation/implementation-plan.md` — sequencing
