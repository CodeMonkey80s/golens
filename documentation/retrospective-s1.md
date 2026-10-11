# S1 Retrospective — what building the engine actually taught

Written at the end of S1 for the sessions that follow. This is not a status report; `AGENTS.md`
covers that. It records the things that were **not** obvious from the specification, and the
reasoning errors that cost time, so they are not repeated.

---

## 1. The go/ast position model is the whole game

The single most important fact learned, and the one the specification could not state:

> **`node.End()` is one *past* the last byte of the node.** It is not the last byte.

Everything about byte-accurate editing falls out of this.

| Mistake available | What actually happens |
|---|---|
| `EndLine = fset.Position(node.End()).Line` | A decl ending at column 1 of line N+1 reports line N+1 |
| `src[Start:End-1]` | Drops the closing `}` |
| `EndByte = offset(node.End())` treated as inclusive | Off by one on every edit |

The correct derivation, and it is now the only one in the codebase:

```go
startOffset := tf.Offset(node.Pos())      // first byte of the node
endOffset   := tf.Offset(node.End())      // one past the last byte
EndLine     := tf.Line(tf.Pos(endOffset - 1))   // the line the last *byte* sits on
```

`Bounds.Source` then reduces to `string(src[StartByte:EndByte])` with no adjustment, because
the half-open convention already excludes the trailing newline. An agent overwriting the range
leaves line structure intact — correct behaviour obtained for free rather than by special-casing.

**Kept as a property test** (`TestBoundsAreHalfOpen`), not a snapshot. It asserts
`file[start:end] == extracted source` for every symbol. A snapshot would have frozen today's
bug; an invariant catches any future one.

### Corollary: bounds should span the *name*, not the spec

`const ( A = 1; B = 2 )` is one `ValueSpec` with two names. Using `spec.Pos()/spec.End()`
gives both symbols identical bounds spanning `A = 1; B = 2`. An agent editing `A` would clobber
`B`. The fix is `name.Pos()/name.End()` — smaller, and independently addressable.

Generalisation worth keeping: **when a declaration binds several names, each name needs its own
bounds.** Found by reading real outline output, not by a test failing.

---

## 2. Presentation-only fields must stay presentation-only

Two mistakes, both from putting display concerns into the domain:

| Mistake | Symptom | Fix |
|---|---|---|
| `TypeSpec.Detail = exprString(s.Type)` | Outline printed the entire struct body inline: `type User struct{ Id int }` | `typeKeyword()` — renders `struct`, `map[K]V`, `[]T`, `*T` |
| `FieldList` rendered like an expression | Signatures lost parameter names: `GetUser(context.Context, int)` instead of `GetUser(ctx context.Context, id int)` | Separate `fieldListString` for params, `resultsString` for results (Go omits parens for one unnamed result) |

The rule: **`Detail` is a token, not a dump.** This tool exists to cut token cost; expanding a
struct body into every outline line inverts that. It also forced a 120-rune truncation, measured
in runes not bytes, so a multi-byte identifier is never split mid-character.

---

## 3. Go's camelCase hides the word boundary from a parser

The `camelCaseId` rule looked trivial from the spec's examples. It took four attempts to get
right, and the reasoning went somewhere unexpected:

| Attempt | Rule | Why it failed |
|---|---|---|
| 1 | Any dictionary initialism must be lowercase | Flagged `userId`, `Id`, `Ok` — all explicitly **allowed** |
| 2 | Flag ≥2 consecutive capitals | Missed `HTTPHandler`, `ASTParser` — the run merges with the next capital (`HTTPH`) |
| 3 | Longest-prefix match on the run | Flagged `userIdX` (`IDX` contains `ID`) |
| 4 | ≥2 capitals at a word boundary, dictionary lookup, prefix search only when a capital follows | **Correct** |

The rule that actually works, and it is short:

> Two or more capitals at a word boundary, where the resulting word is in the dictionary.
> A prefix counts only if strictly shorter than the run, or the run has no lowercase tail.

That distinguishes all four spec examples correctly: `userId` ✓ allowed, `userID` ✓ banned,
`HttpHandler` ✓ allowed, `HTTPHandler` ✓ banned.

**The lesson is about the shape of the ambiguity.** `userIdX` and `userIDMap` are lexically
identical — an uppercase run plus a lowercase tail. One is a single token, the other is an
initialism merging into a word. Resolving it needs an English dictionary, which is *not* on the
approved dependency list. So the rule resolves toward "banned", because a false positive on a
convention check gets the check switched off, while a miss costs one review comment.

**Errors fail asymmetrically. Decide which way to err before writing the check, not after it
misbehaves.** The same reasoning drove the unused-import heuristic: over-approximate usage,
because a false positive tells an agent to delete a live import and break the build, while a
miss is caught by `go build` anyway.

---

## 4. Two packages assert things the test cache cannot see

This cost a real bug and is the most practically important lesson here.

`go test ./...` reported `ok` for `internal/arch` **after** I introduced a genuine boundary
violation (`usecase → naming`). The violation was real; the test would have caught it; it did
not run.

Cause: Go keys its cache on test inputs. `internal/arch` shells out to `go list -deps` and
`internal/naming` scans the repository's own files. Neither result depends on the test file,
so **editing a source file to add an import does not invalidate their cached results.**

| Symptom | Reality |
|---|---|
| Green run, real violation | Cache, not correctness |

Two rules adopted:

1. **`go test -count=1 ./...` is mandatory in this repo.** Recorded under "Build and test" in
   `AGENTS.md`.
2. A test whose result depends on files outside the test file should arguably bypass the cache
   on its own. Worth doing for `internal/arch` if it ever proves fiddly.

Generalisation: a boundary test that shells out is testing the *build graph*, not the package.
It is more valuable than a normal test precisely because it catches what compilation cannot —
which also means it deserves its own trigger.

---

## 5. Wiring is where architecture leaks

Three violations were only discoverable at the wiring layer, all now covered by `internal/arch`:

| Leak | Consequence | Fix |
|---|---|---|
| `cli` imported an engine to default it | Pulled `go/ast`, `go/parser`, `go/token` into the cobra layer | Compose in `cmd/`: `cli.DefaultEnv(goast.New())` |
| Cobra's arg errors are plain values | Wrong arg count exited 4 (parse failure) instead of 1 (usage) | `classifyUsage()` wraps anything without a marker interface |
| A missing file returned a raw `*fs.PathError` | Exited 4; spec puts "file not found" under 2 | `NotFoundFile` marker type |

The exit-code mapping uses **behavioural marker interfaces** (`IsUsage`, `IsNotFound`,
`IsDiagnostics`) rather than concrete types, so `cli` never imports `usecase`'s error types.
Adding a fifth exit code means adding one interface method, not editing a type switch.

`check` returning diagnostics as an `error` felt wrong at first and is right: exit 3 is an
outcome, not a malfunction, and routing it through the error channel keeps `main` at three
lines.

---

## 6. Test expectations were wrong more often than the code was

Worth recording honestly, because the instinct is to blame the code.

| Case | Verdict |
|---|---|
| Hand-counted byte offsets in a test (13/25 vs actual 11/22) | **Test wrong.** Fixed by computing offsets with a script, then writing the expectation |
| Expected 4 imports, wrote 5 specs incl. `jsonalias "encoding/json"` | **Test wrong.** The engine correctly emitted the duplicate path with a different local name |
| Expected `bounds`/`get` payloads to match with a `lines` field | **Design wrong.** `lines` breaks A6. Dropped the field — spec fixed, not code |
| Missing-file exit code expectation (4 vs 2) | **Test wrong**, and the code was also wrong; both fixed |
| `check --naming` reported nothing for `HttpHandler` | **Code right, test wrong.** `HttpHandler` is legal under the rule; `HTTPHandler` is not |

**A test that fails on first run is a hypothesis, not a verdict.** Two of the five "failures"
above were the test being wrong about behaviour that was already correct. Reading the failure
before reaching for the code saved real time.

---

## 7. Things that looked like bugs and were not

| Observation | Reality |
|---|---|
| `var x = 5` shows no value in the outline | `ValueSpec.Values` not rendered. Cosmetic; deliberate, pending a decision |
| Duplicate `"encoding/json"` in imports | Correct — `json` and `jsonalias` both bind that path |
| `const KindPackage` appeared under `TYPES & STRUCTS` | Grouping bug, not an engine bug. Vars/consts now get their own `VARS & CONSTS` section |
| `parser.SkipObjectResolution` seemed free | It is faster but kills `File.Unresolved`, which the unused-import check needs. Kept object resolution **on** deliberately |
| Outline showed `func NewUserStore() *UserStore` | Correct once parameter names were restored — earlier run dropped them |

---

## 8. Practices that paid off

| Practice | Result |
|---|---|
| Spec example as a test assertion | The `• func (s *UserStore) GetUser(ctx context.Context, id int) (*User, error)` line drove the whole signature-rendering design. Testing against the spec's literal text is how the spec stays honest |
| Boundaries as tests, not conventions | Caught 3 leaks in `internal/arch`. A written rule in a doc catches nothing |
| `t.TempDir()` + `run(args...)` helper | CLI tests exercise the real command tree and real parser with no globals. Parallel-safe |
| stderr stays clean in `--json` mode | Asserted explicitly (`TestStdoutStaysCleanOnError`). It is the property that makes output pipeable |
| Benchmarks as gates, not reports | `TestParseP95UnderBudget` fails the build on a regression that changes no output — otherwise silent |
| `strings.Builder` in the formatter | Zero-alloc string building; measured via `b.ReportAllocs()` |

---

## 9. Open threads for later sessions

| Thread | Belongs in |
|---|---|
| `--compact` human output is unspecified by the spec; S1 emits quoted symbol + line range | S5, needs a real agent to validate |
| `userIdX` false positive needs an English dictionary to resolve | Won't fix; documented as by-design bias |
| Unused-import check is lexical, so shadowed or string-only names can false-positive | Accepted; bias documented in the spec |
| Fixtures 5, 6, 8, 9, 10, 12 (CRLF, BOM, empty, comments-only, unicode dirs, deep tree) exist only as inline source strings, not `testdata/` files | S3 |
| No concurrent directory parse yet, so A3 is unverifiable | S5, decision D19 |
| `KindVar`/`KindConst` carry type but not value | Cosmetic; decide in S2 |

---

## 10. If you remember one thing

**The specification is precise about interfaces and silent about semantics.** Every hard bug in
S1 came from a gap the spec could not have stated: what `End()` means, whether a grouped
`const` is one symbol or several, what `EndLine` means when a decl ends at column 1.

Those are exactly the places a reader cannot infer intent. Write them down as invariants with
tests attached, or the next session will make the same reasonable wrong assumption.
