# HANDOFF — golens

**Status: S1 COMPLETE.** All four commands work end to end. `go test ./...` green, `go vet` clean, `gofmt` clean.

S2–S5 are not started.

---

## Current state

| Field | Value |
|---|---|
| Session | S1 complete — Foundation |
| Test suite | 72 top-level tests, 7 packages, all passing |
| Test count by package | arch 7 · cli 10 · domain 7 · goast 14 · format 10 · naming 8 · usecase 16 |
| Verify with | **`go test -count=1 ./...`** — see the caching note below |
| Code size | 25 files, ~4,800 lines incl. tests |
| Blockers | none |
| Binary | 3.89 MB (budget 15 MB) — acceptance criterion A9 met |
| Parse budget | P95 12.7 µs on fixture, 282 µs on largest repo file (budget 5 ms) — A8 met |

## Landed

### Packages

| Path | Contents |
|---|---|
| `cmd/golens/main.go` | Composes engine + cli, exits with the returned code. No logic. |
| `internal/domain/` | `Bounds`, `Node`, `Symbol`, `Document`, `Engine`, `Kind`, `ErrorExit`. Stdlib only, imports nothing in-project. |
| `internal/engine/goast/` | The only package importing `go/parser`, `go/ast`, `go/token`, `go/scanner`. Implements `Engine`, `ParseError`. |
| `internal/format/` | Human, JSON and compact renderers. `WriteJson`, `Bounds`, `Get`, `Outline`. |
| `internal/usecase/` | `Outline`, `Get`, `Bounds`, `Resolve`, `Check`, `Files`. Error classification types. |
| `internal/cli/` | Cobra wiring, global flags, exit-code mapping. Does not import an engine. |
| `internal/naming/` | `camelCaseId` convention checker + initialism dictionary. |
| `internal/arch/` | Structural tests enforcing the package boundaries. |

### Commands working

```
golens outline <file|dir>   [--depth N] [--json] [--compact]
golens get <symbol> <file|dir>  [--bounds-only] [--json] [--compact]
golens bounds <symbol> <file>   [--json] [--compact]
golens check <file|dir>         [--naming] [--json] [--compact]
```

### Acceptance criteria status

| # | Criterion | Status |
|---|---|---|
| A1 | `file[start:end]` == extracted source, byte for byte | **Proven** — `TestBoundsAreHalfOpen`, the S1 hard gate |
| A2 | Two runs → byte-identical stdout | **Proven** — `TestOutlineDirectoryIsDeterministic` |
| A3 | Directory output independent of concurrency | N/A yet — no concurrent parse path exists |
| A4 | Every JSON payload parses, carries `schema_version: 1` | **Proven** |
| A5 | `--compact` loses no information | **Proven** — `TestCompactJsonIsValidAndSmaller` |
| A6 | `get --bounds-only` ≡ `bounds` payload | **Proven** at format and usecase level |
| A7 | Repo-wide naming test, zero violations | **Proven** — `TestRepositoryFollowsNamingConvention` |
| A8 | Parse < 5 ms P95 | **Proven** — benchmark-gated test |
| A9 | Binary < 15 MB | **Proven** — 3.89 MB |
| A10 | No cgo | **Proven** — `TestNoCgoDependency` |
| A11 | `domain`/`usecase` import nothing external | **Proven** — `internal/arch` |
| A12 | 12 fixture classes | **Partial** — see gaps below |

## Caching note — read before trusting a green run

`go test ./...` **without `-count=1` can report a stale pass.** Go keys the cache on test
inputs, but `internal/arch` asserts facts about the *source tree* via `go list -deps`, and
`internal/naming` scans the repository's own files. Neither is a test input, so editing a
source file to add an import does not invalidate their cached results.

This already bit once during S1: wiring `check --naming` introduced a real
`usecase → naming` boundary violation that stayed hidden behind a cached `ok` for arch.

**Always use `go test -count=1 ./...` in this repo.** If a package's behaviour depends on
files outside the test file, that is a signal to bypass the cache rather than trust it.

## Frozen interfaces (S1 hard gate — satisfied)

These are the signatures S2–S5 build against. Do not change them without updating
`decisions.md` §2 first.

```go
type Bounds struct{ StartLine, EndLine, StartByte, EndByte int }
func (b Bounds) Valid() bool
func (b Bounds) LineCount() int
func (b Bounds) ByteLen() int
func (b Bounds) LineRange() string
func (b Bounds) Source(src []byte) string

type Node struct{ Name string; Kind Kind; Detail, Receiver string; Bounds Bounds; Children []Node }
func (n Node) Declaration() string
func (n Node) Keyword() string

type Symbol struct{ Name, QualifiedName string; Kind Kind; Bounds Bounds; File, Detail, Receiver string }
func (s Symbol) Declaration() string

type Document struct{ Path, Package string; Bounds Bounds; Nodes []Node; Symbols []Symbol; Imports []string; Source []byte; Comments bool }
func (d *Document) Symbol(name string) (Symbol, bool)
func (d *Document) Lookup(name string) []Symbol

type Engine interface{ Parse(path string) (Document, error) }

type ErrorExit int
const (ExitOk ErrorExit = iota; ExitUsage; ExitNotFound; ExitDiagnostics; ExitParse)
```

## Decisions taken during S1 beyond the written plan

| Decision | Reason |
|---|---|
| **A missing file exits 2, not 4** | Both mean "you named something absent"; 4 is reserved for a file that exists but will not parse. Keeps the "correct your input" signal uniform. |
| **Const/var bounds span the name, not the spec** | `const ( A = 1; B = 2 )` yields two independently addressable symbols. Editing one must not clobber its neighbour. |
| **Outline groups vars/consts separately** | A third `VARS & CONSTS` section; the spec only names two, so grouping them under `TYPES & STRUCTS` was wrong and misleading. |
| **Struct `Detail` is the keyword, not the body** | `type User struct`, matching the spec's example. Expanding the struct made single-token output unusable under a token budget. |
| **Methods render with the `func` keyword** | `func (s *UserStore) GetUser(...)`, as the spec's example shows. `Kind` still distinguishes them for querying. |
| **`GetPayload` has no `lines` field** | It would break A6. Line count is derivable from start/end line and appears in the human form. |
| **Engine composition happens in `cmd/`** | `cli` importing an engine would pull `go/ast` into cobra. Caught by `internal/arch`. |
| **`check --naming` replaces, not augments** | A caller asking about naming should not get unused-import warnings, and vice versa. |
| **`usecase` may depend on `naming`** | `naming` is a leaf like `format` — pure functions over identifiers, no project dependencies — so the arrow points inward. Listed explicitly in the arch allowlist rather than as a prefix rule, so a fourth sibling package still forces a decision. `TestNamingIsALeaf` guards the assumption. |
| **Naming rule: ≥2 capitals at a word boundary, in the dictionary** | Derived from the spec's own examples. `userId` allowed, `userID` banned, `HttpHandler` allowed, `HTTPHandler` banned. `userIdX` is a documented false positive — resolving it needs an English dictionary, which is not an approved dependency. |

## Gaps and known limits

| Gap | Impact | Where to pick up |
|---|---|---|
| Fixtures 5, 6, 8, 9, 10, 12 not yet as files | CRLF, BOM, empty file, comments-only, unicode-in-dir, deep tree are covered by inline-source tests but not by real files on disk | S3 — build `testdata/` per criterion A12 |
| No concurrent directory parse | A3 unverifiable; large trees are serial today | S5, decision D19 |
| `check` unused-import is lexical | False positive if a name is shadowed or appears in a string. Bias is deliberate: a false positive would have an agent delete a live import. Documented as best-effort in the spec. | Accept as-is |
| `--compact` human output is minimal but unspecified by the spec | S1 emits quoted symbol + line range | S5 — confirm against a real agent |
| No `check` import-path whitespace diagnostic test | Code path exists, untested | S4 |
| `KindVar`/`KindConst` nodes carry `Detail` from the type only, not the value | `var x = 5` shows no `= 5` | Cosmetic; decide in S2 |

## Next

**S2 — `outline`** (task 5, est. 5–6h). Already largely implemented; what remains:

- [ ] Confirm human output matches the spec's `golens outline user.go` sample byte-for-byte
- [ ] Verify `--depth 1` vs `--depth 2` on real files, not just the sample document
- [ ] Decide the `--compact` outline shape (currently unaddressed)
- [ ] Multi-file output: confirm repeated `file:` header per block matches the spec
- [ ] Golden-file tests under `testdata/`

S2 exit criteria: spec sample matched byte-for-byte; determinism test passes (already
passing — keep it green).

## Open questions

None blocking. Two cosmetic items above need a decision but neither blocks S2.

## Read first

1. `documentation/specification.md` — normative
2. `documentation/decisions.md` — rationale, frozen interfaces, A1–A12
3. `documentation/implementation-plan.md` — sequencing
4. `documentation/retrospective-s1.md` — **the non-obvious semantics** S1 established: the
   `go/ast` position model, why `Detail` must stay a token, why `-count=1` is mandatory, and
   the reasoning errors that cost time. Read this before touching engine or format code.
