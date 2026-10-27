# golens — Project History

Consolidated from `retrospective-s1.md`, `retrospective-s2.md`, `retrospective-s4.md`. The implementation plan is in `implementation-plan.md`.

---

## Session Summary

| Session | Focus | Duration | Tests Added | Key Insight |
|---------|-------|----------|-------------|-------------|
| S1 | Engine, AST → domain, `outline`/`get`/`bounds` core | ~6h | 72 | Half-open byte offsets; `node.End()` is past the last byte |
| S2 | `outline` depth, `--compact`, JSON vs human agreement | ~1.5h | +13 | Default flag values matter; one `visible()` helper for three renderers |
| S3 | Byte invariants, overwrite round-trip, `Declaration()` | ~4h | +30 | A field that changes a head's meaning must reach every renderer |
| S4 | `check` — unused imports, duplicate declarations | ~1.5h | +19 | Error bias is per-check: over-report absence, under-report presence |
| S5 | Cross-package resolution, symlinks, `testdata`, I/O vs parse exit codes | ~3h | +25 | Two commands must not disagree about the same input; exit code is a claim |

Total: ~16h, 159 tests (now 379 with subtests)

---

## The Defects That Shipped — And Why Tests Didn't Catch Them

This is the most important part of the history. Every defect below was **in green code** when found.

| Defect | Session | Why Tests Missed It | What Changed |
|--------|---------|---------------------|--------------|
| Generic type params dropped from heads | S3 | Corpus had no generics | `internal/constructs` + corpus walk test |
| Receiver variable invented (`(s ...)`) | S3 | Corpus had no methods with named receivers | Corpus test catches missing `receiver` |
| `type Alias = X` rendered `type Alias X` | S3 | Corpus had no aliases | `is_alias` field + corpus test |
| `outline --json` dropped `is_alias` | S3 | JSON path not tested against source | `TestJSONDeclarationHeadsMatchSource` |
| `outline --json --depth 1` returned no declarations | S2 | Test asserted subset (empty set satisfies) | Compare **counts** in both directions |
| `/vN` suffixes bound as package name | S4 | No platform-variant fixtures | Real GOOS×GOARCH matrix + filename constraints |
| Redeclarations across exclusive build tags | S4 | 24 false positives on `duf` | Build set = name ∧ tag ∧ cgo over 47 pairs |
| Unused imports reported when local name not derivable | S1 | No dash-import fixtures | Silence when `!isIdentifier(decl.name)` |
| Symlinked directory reported "no .go files" | S5 | No symlink fixtures | Resolve root before walk |
| `check` and `outline` disagreed on I/O failure exit code | S5 | No I/O failure test | `ParseError.Located()` discriminator |
| `get` aborted on first parse error | S5 | Single-file test only | Skip, return symbol, exit 4, name skips |
| Package-prefixed query answered "not found" | S5 | No dotted-query test | `CrossPackageSymbol` + `PackageQualifiedSymbol` |
| Multi-name struct field (`A, B int`) dropped `B` | S3 | Corpus had no multi-name fields | One node per name + bounds per name |
| Unnamed function params rendered nothing | S3 | Reused `fieldNodes` which skipped nameless | Unified renderer for fields and params |
| `get`/`check` disagreed on platform variants; `get` hid filename constraints | v0.4 | No test compared resolution with the build set | `decorateDocument`, `build`/`buildable_now`, `check --tags` annotations |
| Fuzz filenames exceeded the filesystem limit; hidden names produced a legitimate no-files error | v0.6 | Filenames were fixed at `a.go`/`b.go` | Sanitized, capped filenames; no-payload errors accepted |

**Pattern**: The corpus didn't contain the construct. The test asserted a subset. The defect worked by **omission** — the output was silently wrong rather than visibly broken.

---

## Key Lessons by Category

### 1. Byte Offsets Are the Contract (S1)
> **`node.End()` is one *past* the last byte.** Not the last byte.

```go
startOffset := tf.Offset(node.Pos())      // first byte of the node
endOffset   := tf.Offset(node.End())      // one past the last byte
EndLine     := tf.Line(tf.Pos(endOffset - 1))   // line the last byte sits on
```

This single fact determines whether an agent's edit corrupts a file. Pinned as `TestBoundsAreHalfOpen` — an invariant test, not a snapshot.

### 2. Test the Promise, Not the Code (S1, S2, S3, S4)
- S1: Hand-counted byte offsets wrong (13/25 vs 11/22) → verify against source
- S2: Fixture line numbers wrong (29 vs 28) → verify against source
- S3: Fixture layout assumptions → rewrite fixture
- S4: Test fixture itself had invalid namespace (`p` + `p_test` + `p` again)

**Rule**: When a line/byte assertion fails, verify against the source before touching either side.

### 3. A Green Suite Proves the Tests Pass, Not the Code (S2)
Three bugs in passing tests:
1. `depth > 0` made unlimited (`-1`) equal depth 1 → `visible(depth, level)` helper
2. `len(Children) == 0` deleted structs with fields → `Node.IsGroup` classification
3. Compact recursed twice → duplicate-line assertion caught it

**Generalisation**: A sentinel value flowing through arithmetic will eventually be compared directly. Magic values need one interpreter.

### 4. One Renderer, One Classification, One Interpretation (S2, S3)
| Problem | Fix |
|---------|-----|
| Three renderers interpreted `--depth` differently | One `visible()` function |
| Formatters inferred "is this a group?" from shape | `IsGroup` set once at creation |
| `Declaration()` assembled heads; JSON path copied fields by hand | Every renderer reads the same fields |

### 5. Error Bias Is Per-Check, Not Per-Tool (S4)
| Check | False Positive Causes | Correct Bias |
|-------|----------------------|--------------|
| Unused import | Agent deletes **live** import → build breaks | **Over-report** (miss is safe, `go build` catches) |
| Redeclaration | Agent deletes **working** declaration | **Under-report** (code exists, was written deliberately) |

**False positives are cheap when the thing is missing, expensive when the thing exists.** This single sentence decides bias for any new check.

### 6. Two Commands Must Not Disagree About the Same Input (S5)
- `check` exited 3 for unreadable file; `outline` exited 4
- `check` saw cross-package as conflict; `get` called it "ambiguous"
- Exit 3 = "fix the code", Exit 4 = "fix the machine"
- **Exit code is a claim about what kind of problem the caller has**

### 7. Omission Defects Ship Because the Corpus Lacks the Construct (S3)
Three omission defects in a row:
1. Alias `=` dropped
2. `is_alias` dropped from JSON
3. Multi-name field dropped `B`

All survived because `internal/constructs` didn't declare them. Now it does, and `TestCorpusContainsTheConstructsThatBroke` counts them — removing one fails a test.

---

## What Made the Difference

| Practice | First Used | Result |
|----------|------------|--------|
| Assert against spec's literal sample | S2 | Two real divergences, neither visible by reading |
| Test the **default** flag value, not just explicit | S2 | Found unlimited-depth bug |
| Duplicate-line assertion on line-oriented output | S2 | Found double-emission bug |
| `IsGroup` rather than shape inference | S2 | Fixed a bug class, not an instance |
| `visible()` helper for sentinel parameter | S2 | One interpretation, three renderers |
| Run tool on its own source | S4 | Found false positive no fixture would have |
| Probe heuristic empirically before trusting design | S4 | Confirmed bias, found nothing to fix |
| Delete superseded check, don't merge | S4 | Killed double-report |
| Corpus walk test against real repo files | S3 | Catches what fixtures don't contain |
| Compare renderings **by count in both directions** | S3 | Subset assertions satisfied by empty set |

---

## Open Threads (From Retrospectives)

| Thread | Origin | Status |
|--------|--------|--------|
| `KindVar`/`KindConst` `Detail` carries type but not value (`var x = 5` shows no `= 5`) | S2, S4 | Cosmetic, spec says outline head ≠ byte span |
| `type X = Y` alias renders `type Alias User`, losing `=` | S2, S4 | Cosmetic |
| Real `testdata/golden/*.txt` with regeneration flag | S2 | Only if output surfaces multiply |
| Package-qualified lookup (`golens get p.Func`) | S4, S5 | v0.3, needs spec change |
| Unreachable code detection | S1, S4 | v0.2 with multi-language engine |
| Doc-comment awareness (`--with-docs`) | S1 | v0.2 |
| Tree-sitter engine for non-Go languages | Spec §1.2, D10 | Engine interface ready |

---

## The Three Retrospectives — Original Files (Preserved)

- `retrospective-s1.md` — The AST position model, camelCase naming, cache traps
- `retrospective-s2.md` — Three bugs in green tests, golden tests, depth semantics
- `retrospective-s4.md` — `check`, opposite error biases, self-check false positives

These are kept because each records a **class of mistake** rather than an instance, and `AGENTS.md` alone would not tell you why the obvious change is the wrong one.