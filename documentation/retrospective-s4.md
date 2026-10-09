# S4 Retrospective — `check`, and two checks that needed opposite error biases

Written at the end of S4. This session produced the most instructive failure in the project so
far: a check that worked, passed its own tests, and was **actively harmful** on real code.

---

## 1. The headline: the tool flagged three bugs in itself, and all three were false positives

After wiring cross-file duplicate detection, running `golens check ./` on its own source
reported:

```
internal/engine/goast/fixtures_test.go:321:1: names redeclared in this package
internal/engine/goast/helpers_test.go:15:1: mustLookup redeclared in this package
internal/engine/goast/helpers_test.go:36:1: lookupPresent redeclared in this package
```

None were real. The files were:

| File | Package |
|---|---|
| `engine_test.go` | `package goast` |
| `fixtures_test.go` | `package goast_test` |

Same directory, different packages. The conflict map was keyed on directory alone.

**A Go namespace is one package in one directory, and a directory may legitimately hold two:**
the package under test and its external test package. Key on `dir + "\x00" + packageName`.

---

## 2. The lesson that generalises: error bias is per-check, not per-tool

This is the most portable thing learned across four sessions.

| Check | A false positive causes | Correct bias |
|---|---|---|
| Unused import | Agent deletes a **live** import → build breaks | **Over-report.** A miss costs nothing; `go build` catches it |
| Redeclaration | Agent deletes a **working declaration** | **Under-report.** The declaration is already the author's own code |

Both are "diagnostic about a declaration". Both are `error`-ish severity. They need opposite
biases, and I reasoned about the first carefully in S1 then shipped the second with the
opposite bias by default in S4.

### Why under-reporting is right for redeclaration

The asymmetry is about **who wrote the offending code**:

- An unused import is *absence*. Something is missing, and the only evidence is that no use
  appears. Reporting "this looks unused" is cheap to reverse — the agent re-adds it if the build
  complains.
- A redeclaration is *presence*. The code is there, written deliberately. Reporting "this
  conflicts" invites deleting working code to satisfy a checker.

**False positives are cheap when the thing is missing, expensive when the thing exists.** That
single sentence decides bias for any new check.

### The check that forced the lesson

```go
// Wrong: every directory is one namespace.
p.byDir[dir][name] = append(...)

// Right: one package in one directory.
ns := dir + "\x00" + doc.Package
p.byNamespace[ns][name] = append(...)
```

Caught only because the tool was run on the tool. `TestRepositoryPassesItsOwnCheck` now runs it
in CI, so this direction cannot regress silently.

---

## 3. Two bugs, both in duplicate detection

### Bug A: the check was per-file

`duplicateDeclarations(doc)` saw one `Document` at a time, so two files of one package declaring
the same name — *exactly* the conflict it exists to find — went unreported. S1's tests only
ever used single-file fixtures, so this was invisible.

Replaced with `packageSymbols`: accumulate across files, report the second and later sites,
naming the first so a reader can find it.

### Bug B: double-reporting

With both the per-file and package passes live, one conflict produced two diagnostics:

```
G redeclared in this block (first declared at line 7)
G redeclared in this package (first declared at /tmp/.../p.go:7)
```

Two passes over the same fact. The package pass is strictly more informed — it sees
cross-file conflicts too — so the per-file pass was deleted, not merged.

**Generalisation: when you replace a check with a broader one, delete the narrower one.** An
overlapping pair produces duplicate diagnostics, which erodes trust faster than a missing one.

---

## 4. S4 cost against plan, and why the estimate was wrong

| | Planned | Actual |
|---|---|---|
| Effort | 9–12h | ~1.5h |
| Tests added | — | 19 (95 → 114) |

The estimate was high because I assumed the unused-import heuristic would need real work. It
did not — S1 built it, and an empirical probe confirmed the bias held:

| Probe | Result |
|---|---|
| Comment-only reference | Missed (false negative) — safe |
| Shadowed local variable | Missed (false negative) — safe |
| Aliased import used | Correct |
| Two imports sharing a prefix (`net/http`, `net/http/httptest`) | Correct |
| All-11-imports file, every one used | No false positives |

**The heuristic was right for the documented reason.** S1's design work paid out; S4 only had to
prove it.

---

## 5. Test-expectation errors: four now, and a firm pattern

The recurring failure across all four sessions:

| Session | Wrong expectation | What I did |
|---|---|---|
| S1 | Hand-counted byte offsets (13/25 vs actual 11/22) | Verified against source |
| S2 | Fixture line numbers (29 vs 28) | Verified against source |
| S3 | Fixture layout assumptions | Rewrote the fixture |
| S4 | `a.go` + `a_test.go` both `package p` with same helper | Rewrote the fixture |

The S4 case is the interesting one: I wrote a fixture asserting that `p` and `p_test` can both
declare `helper` — then added a third file declaring `package p` with `helper` *again*, which is
a genuine conflict. The code was right three times.

**A fixture that fails a check about namespaces must itself be a valid namespace.** Test data
gets the same scrutiny as the code under test.

---

## 6. Practices that paid off

| Practice | Result |
|---|---|
| Run the tool on its own source | Found the false positive no fixture would have |
| Probe the heuristic empirically before trusting the design | Confirmed bias, found nothing to fix |
| Delete the superseded check | Killed the double-report |
| Test matrices rather than examples | 13 unused-import cases, 9 namespace cases; the gaps showed up as missing rows |
| Assert exit codes per scenario | `check` has three distinct outcomes (0, 3, and usage errors) and each was untested |

---

## 7. Open threads for S5

| Thread | Notes |
|---|---|
| A3 unverified | Concurrent directory parse not implemented; the one unproven criterion |
| `check ./` exits 3 on `testdata/store/store.go` | Intentional — the fixture has an unused import so the outline golden has a stable shape. Worth a comment in the fixture, or move it out of the check path |
| `invalidImportPaths` still untested directly | The code path exists; no test asserts empty-path or whitespace detection. Go's own parser rejects both before we see them, so the check may be unreachable — verify and either test or delete |
| `KindVar`/`KindConst` `Detail` omits the value | `var x = 5` shows no `= 5`. Cosmetic |
| `type X = Y` alias renders `type Alias User`, losing the `=` | Cosmetic |
| README | The tool has no documentation a user could read |
| Final line-by-line review against the spec | Not yet done; every session has checked only its own surface |

---

## 8. If you remember one thing

**A checker must be run on its own codebase before it can be trusted.** `check` passed 19 tests
covering every scenario I thought to write, and then produced three false positives the moment it
saw a directory containing both `foo` and `foo_test`.

Test fixtures live in tidy, hand-made directories. Real repositories contain the awkward cases
nobody designs a fixture for: external test packages, build-tagged files, generated code,
subdirectories. **The codebase is the fixture you did not choose, and it is the only one that
runs in CI.**

Pair that with the bias rule from §2: decide which way to err *per check*, before writing it,
based on whether the offending thing is an absence or a presence.
