# S2 Retrospective — outline, and the bugs that hiding tests let through

Written at the end of S2. S3's retrospective should read this first: the three bugs below were
all in code that was **already green** when S2 started.

---

## 1. The headline: three bugs, zero failing tests

S1 finished with 72 passing tests. S2 opened with 72 passing tests and **three real bugs** in
`outline`, all in paths that a passing suite appeared to cover.

| Bug | Why it survived |
|---|---|
| `--depth` unset produced **byte-identical output to `--depth 1`** | `TestOutlineDepthLimitsRecursion` compared depth 1 vs 2 and passed. It never ran the default |
| Compact output **dropped every declaration with children** | Leaf declarations rendered fine, so any test using a func with no params passed |
| Compact output **emitted every record twice** | Only visible at unlimited depth on a multi-declaration file |

The pattern is worth internalising: **a green test suite is evidence about the tests you
wrote, not about the code.** Every one of these lived in a gap between what a test asserted
and what it meant to assert.

### Bug 1 in detail: negative depth is a lie

```go
if depth > 0 {                       // WRONG for the unlimited default of -1
    recurse()
}
```

`depth` is `-1` by default (unlimited). `-1 > 0` is false at every level, so "unlimited" was
silently "depth 1". The code looked correct because `depth > 0` reads as an obvious guard.

The fix is a named helper, because the bug is in the *expression*, not the logic:

```go
func visible(depth, level int) bool {
    if depth < 0 {
        return true
    }
    return level < depth
}
```

**Generalisation: a sentinel value that flows through arithmetic will eventually be compared
directly.** `-1` meaning "unlimited" is a magic value that keeps its meaning only as long as
nobody forgets it. Every function on the path now asks `visible()`, so the sentinel is
interpreted in exactly one place.

### Bug 2 in detail: "has children" is not a type test

Compact flattening was keyed on `len(n.Children) == 0`:

```go
if len(n.Children) == 0 {
    print(n)              // a leaf: correct
} else {
    flatten(n.Children)   // WRONG: a struct with fields is not a container
}
```

That silently deleted every `type X struct {...}` and every method with parameters — the two
most common declaration shapes in Go.

The fix required a domain change, which is the real lesson:

```go
type Node struct {
    ...
    IsGroup bool   // a synthetic presentation container, not a declaration
}
```

**A formatter asking "is this a group?" is a design smell.** The engine knows which nodes it
synthesised; the formatter was inferring it from shape and getting it wrong. Now `IsGroup` is
set once where groups are created, and every consumer agrees by construction.

Same family as the S1 `Detail` lesson: **structural questions belong to the layer that owns the
structure.** The formatter renders; it does not classify.

### Bug 3 in detail: the first compact implementation recursed twice

```go
compactNodes(sb, n.Children, depth-1)   // flatten
if depth > 0 {
    compactNodes(sb, n.Children, depth-1)   // then again — bug
}
```

Caught by an assertion that counts duplicate lines, not by reading the output. Duplicate-line
detection is cheap and catches a whole bug class; worth reaching for whenever output is
line-oriented.

---

## 2. Golden tests beat eyeballing output

`TestOutlineMatchesSpecificationSample` pins the human output against the spec's sample **line
for line**. Two spec divergences surfaced immediately, neither of which looked wrong:

| Implementation had | Spec says | Why |
|---|---|---|
| `• "context"` | `• context` | The **local name** is what a reader searches for, and a rename's alias is the whole point of the binding |
| `• context (line 4)` | `• context` | An import is always one line, so its range is redundant. A declaration's range is what an agent seeks — *that* is the product |

Neither was a bug I'd have found by reading my own code. Both came from transcribing the
spec's sample literally.

**Write the spec's example into a test.** It costs one fixture and turns prose into a contract.

### Where the golden literals live

They are in `golden_test.go` as string literals, not as `testdata/golden/*.txt`. Deliberate:

| | Literals | Checked-in golden files |
|---|---|---|
| Failure diff | Shows expected vs actual inline | Needs a regeneration flag to be usable |
| Accidental update | Hard — you edit code and a test | Easy — you run `-update` and bless it |
| Review | The expectation is visible in the diff | A changed `.txt` is opaque |

`testdata/store/store.go` **is** committed, as the shared subject. Only the expectations are
literals. Open question for later: if more output surfaces appear, real golden files with a
regeneration flag may become worth the ceremony.

---

## 3. Test fixture line numbers are load-bearing

I miscounted `GetUser`'s start line (expected 29, actual 28) when writing the first version of
`TestStoreFixtureIsCanonical`. The instinct was to "fix" the test. That would have been wrong:
the expectation was the error, and matching code to a bad expectation would have shifted every
downstream golden silently.

Verified against the source instead, then pinned the exact lines in the test with a comment
explaining why they matter.

**When a fixture feeds a golden test, its layout is part of the contract.** An inserted blank
line should fail loudly. `testfixture` now says so in its package doc.

### Same trap, different disguise, later in S2

Three separate times in this project a test failed because *I* had miscounted lines rather
than because the code was wrong. Once in S1, once here, and once more while writing the S3
byte-invariant test.

It keeps happening because counting lines by eye is unreliable and the failure always *looks*
like a code bug. **When a line-number assertion fails, verify against the source before touching
either side.**

---

## 4. `--compact` needed a decision, not just an implementation

Left undefined in the spec and in the plan as "needs your input". Rather than block, I picked:

```
internal/domain/domain.go	domain
line 18	type Kind string
lines 42-47	type Bounds struct
line 43	StartLine int
```

Tab-separated `line-range → declaration`. No headings, no bullets, no blank lines.

The rule that made it defensible is **lossless subset** (A5): every declaration and every line
range from the full form survives. That is a property, testable without pinning bytes:

```go
for _, fact := range []string{"type User struct", "func NewUserStore() *UserStore", ...} {
    if !strings.Contains(compact, fact) { t.Errorf(...) }
}
```

`TestCompactIsLosslessSubset` also asserts decoration is *gone*, since a subset that keeps the
decoration has gained nothing.

---

## 5. Depth semantics had to be pinned in one place

Three renderers now interpret depth: human, JSON, compact. Each independently got it wrong in
a different way before `visible()` existed:

| Renderer | Bug |
|---|---|
| Human | `depth > level` made unlimited equal depth 1 |
| Compact | Depth was ignored entirely — one fix flattened, another duplicated |
| JSON | Was correct only because `depth == 0` happened to be the right early exit |

**One rule, one function, three callers.** When a parameter crosses a layer boundary, the
interpretation belongs in a shared helper. Three implementations of "what does depth 2 mean"
will disagree.

`Node.IsGroup` came from the same principle: one classification, computed once.

---

## 6. What S2 cost against plan

| | Planned | Actual |
|---|---|---|
| Effort | 5–6h | ~1.5h |
| Tests added | — | 13 (72 → 85) |
| Bugs found | — | 3 |
| Acceptance criteria closed | A12 | A12, A5 (via CLI) |

S2 was fast **because S1 built the formatters first**, not because outline is easy. The plan
assumed S2 would write outline; it had to prove it. That is why the bugs surfaced at all.

**The speed came from tests, not from the code.** Sessions where the work was already written
were the sessions where tests found the most.

---

## 7. Practices worth keeping

| Practice | Result |
|---|---|
| Assert against the spec's literal sample | Two real divergences, neither visible by reading |
| Test the *default* flag value, not just explicit ones | Found the unlimited-depth bug |
| Duplicate-line assertion on line-oriented output | Found the double-emission bug |
| `IsGroup` rather than shape inference | Fixed a bug class, not an instance |
| `visible()` helper for a sentinel parameter | One interpretation, three renderers |
| Pin fixture line numbers | Layout drift fails loudly |
| Verify line assertions against source | Caught 3 bad test expectations across 3 sessions |

---

## 8. Open threads for S3+

| Thread | Belongs in |
|---|---|
| `KindVar`/`KindConst` `Detail` carries the type but not the value — `var x = 5` shows nothing | S3, cosmetic |
| Compact human output for `get`/`bounds` still informal — S3 pins it | S3 |
| `type X = Y` alias renders `type Alias User`, losing the `=` | S3, cosmetic |
| Real `testdata/golden/*.txt` files with a regeneration flag | Only if output surfaces multiply |
| No concurrent directory parse, so A3 unverified | S5 |

---

## 9. If you remember one thing

**Every bug in this section sat in code the test suite had already approved.** Not one was
found by reading the implementation carefully — all three came from writing an assertion that
meant something slightly different from what the existing ones meant.

The tests I wrote in S1 were correct about what they checked. They just checked narrower things
than the tool does. **Coverage is not the same as coverage of the space**, and a green suite
means the questions were answerable, not that the answers are right.

Write the test for what the tool *promises*, not for what the code *does*.
