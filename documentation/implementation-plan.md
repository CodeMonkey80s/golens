# golens — Implementation Plan

Status: **All five sessions landed.** S1–S5 are complete; the plan is kept as a record of the
sequence and its rationale, not as work to do. Two rows below have since been overtaken by
events and are annotated where they diverge from what shipped. `AGENTS.md` at the repo root
records where the project actually is.

Companion documents — this file answers *when and in what order*; they answer *what and why*:

| Document | Owns | Stability |
|---|---|---|
| `specification.md` | **WHAT** to build — normative | Stable; changes only on design change |
| `decisions.md` | **WHY** — 23 decisions, frozen interfaces, acceptance criteria A1–A14 | Stable after S1 |
| `implementation-plan.md` | **WHEN** — sequencing, estimates, handoffs | Perishable; rows retire as work lands |
| `AGENTS.md` (repo root) | **WHERE** — current state, traps, open questions | Rewritten as the project moves |

> This file is deliberately kept out of `specification.md`. The spec is the agent-facing
> ingest artifact and must stay free of estimates and session logistics.

---

## 1. Scheduling principle

Split by **milestone, not by task**. Thirteen task-sized sessions would each pay a
re-orientation cost that outweighs the benefit; five milestone sessions each end on a
verifiable exit criterion.

Two rules govern the split:

| Rule | Reason |
|---|---|
| Byte-offset math is proven in **S1**, not S3 | It is the only logic in this spec that can silently corrupt a user's file, and the least verifiable by inspection. Prove it while attention is highest; S3 becomes plumbing instead of debugging. |
| Traversal → resolution → byte-invariant tests stay in **one** causal chain | Splitting mid-chain means the next session debugs the previous session's work. |

---

## 2. Session plan

| Session | Scope | Tasks | Est. | Exit criteria (all verifiable) |
|---|---|---|---|---|
| **S1** Foundation | Layout, domain types, Go engine, minimal formatters. **Prove offset math.** | 1, 2, 3 | 9–12h | Engine parses a file → correct `Bounds`. Byte invariant proven on a hand-checked fixture. Interface signatures frozen into `decisions.md` |
| **S2** `outline` | Human + JSON, `--depth`, dir recursion, lexical order, range rendering | 5 | 5–6h | Matches the spec's `golens outline user.go` sample byte-for-byte. Determinism test passes |
| **S3** `get` + `bounds` | Resolver, ambiguity handling, dir resolution, `--bounds-only` | 4, 6, 7 | 12–17h | Property test green across **all** fixture classes. `get --bounds-only` ≡ `bounds` |
| **S4** `check` + naming | Parse errors, unused imports, duplicate decls, path syntax, `--naming` | 8, 10 | 9–12h | Passes the 12-class fixture suite incl. CRLF, BOM, unicode idents, dot/blank imports. **Shipped narrower:** `--naming` and `internal/naming` were removed before release — naming is `revive`'s job, and `decisions.md` D9 records why |
| **S5** Harden + ship | `--compact`, `schema_version`, P95 gate, binary size, `CGO_ENABLED=0`, README | 9, 12, 13 | 6–9h | A1–A14 all green. Full line-by-line review against `specification.md` |
| | **Total** | | **41–56h** | |

Task numbers refer to the original 13-task breakdown preserved in `decisions.md` context.

**On S5's exit criterion.** The line-by-line review was not part of S5. S5 checked its own
surface, as every session before it had, and the review was deferred until the sessions were
done — at which point reading the whole specification against the whole implementation found
that `outline --json` dropped `type_params`, `receiver` and `is_alias`, the same three fields
the human form renders. The alias case is the one that matters: an alias and a defined type
over the same underlying type were byte-identical JSON nodes describing different programs.
See `decisions.md` D22, and "Defects the plan shipped" in `AGENTS.md`.

The lesson is the S4 one repeating: a reviewer who reads only the code they changed will not
find the divergence between two renderers of the same data, because each looks correct alone.

Running the plan afterwards found a second defect the review missed. `outline --json --depth 1`
returned the four group headings and **none of the declarations** — the human form at the same
depth listed nine from `slices/iter.go`. `convertNodes` counted a group node as a level of the
hierarchy, so depth 1 was consumed before any declaration was reached, and `--depth` was useless to
a machine consumer: depth 2 was the only depth that returned anything. `compactNodes` already
documented the correct rule ("group nodes are transparent ... they do not consume a level") two
functions away, and `TestJSONDepthMatchesHumanDepth` passed because it only asserted JSON ⊆ human.
A subset assertion is satisfied by the empty set.

### Effort envelope

| Scenario | Effort | Elapsed |
|---|---|---|
| Senior Go dev, familiar with `go/ast` | 55–70h | ~1.5–2 weeks |
| AI-assisted, working with you in the loop | 41–56h | ~4–6 sessions |

Hold **15–20% contingency** on top. Writing tests is fast; getting the byte-for-byte
invariant to hold across 12 fixture classes is where iteration actually costs time, and
it cannot be predicted until the code runs. Better to under-promise than miss a date.

---

## 3. Do not split these

| Item | Sessions | Reason |
|---|---|---|
| Namespace traversal → resolver → byte-invariant tests | S1 → S3 | One causal chain; splitting mid-chain hands the next session a debugging session |
| Unused-import analysis + naming checker | S4 | Shared AST-walk helpers; splitting guarantees a duplicated walker |
| Formatter scaffolding | S1 → S5 | Formatters are needed by S2, so they start in S1 and are *consolidated* in S5 |

---

## 4. API drift is the real risk

Not forgetting — **drift**. A fresh session that half-remembers the engine interface will
invent a slightly different one, and by S4 there are three incompatible shapes and a
rewrite.

Mitigation: **the end of S1 is a hard gate.** Concrete Go signatures are written into
`decisions.md` as frozen fact before S2 begins. Everything after S1 is implementation
against a written interface, not interpretation.

```go
type Bounds struct{ StartLine, EndLine, StartByte, EndByte int }
type Node struct{ Name, Kind, Detail string; Bounds Bounds; Children []Node }
type Document struct{ Path, Package string; Bounds Bounds; Nodes []Node }
type Engine interface{ Parse(path string) (Document, error) }
```

---

## 5. Handoff protocol

Each session ends with three things, **in this order**:

| # | Action | Role |
|---|---|---|
| 1 | `go test -count=1 ./...` green | **The real handoff contract** — not a summary |
| 2 | `AGENTS.md` updated at repo root | Durable state: landed, traps, open questions |
| 3 | Chat summary | Disposable. The file is not |

`AGENTS.md` is rewritten rather than appended to, so it cannot grow into a stale log.

**If a session ends with failing tests, that is a genuine blocker.** Stop and report it
rather than papering over it — a red suite passed to the next session is how a two-hour
problem becomes a two-day one.

---

## 6. Task → session mapping

| Task | Description | Session |
|---|---|---|
| 1 | Scaffold: layout, cobra wiring, 4 commands, globals, exit codes, stdout/stderr split | S1 |
| 2 | Domain types: `Bounds`, `Node`, `Document`, `Engine`, `Symbol` | S1 |
| 3 | Go engine: parse, `FileSet` → `Bounds` conversion, imports | S1 |
| 4 | Namespace traversal: funcs, methods w/ receiver, types, vars, consts, dot/blank imports | S3 |
| 5 | `outline`: human + JSON, `--depth`, dir recursion, lexical order | S2 |
| 6 | `get`: resolver, ambiguity, dir resolution, `--bounds-only` | S3 |
| 7 | `bounds`: file-only validation, human line, `end_line` derivation | S3 |
| 8 | `check`: parse errors, unused imports, dup decls, path syntax, exit 3 | S4 |
| 9 | Formatters: human / JSON / compact, `schema_version` first | S1 → S5 |
| 10 | Naming checker: initialism dictionary + convention logic | S4 |
| 11 | Tests: 12 fixture classes, byte-invariant property test, e2e, benchmarks | S3 → S5 |
| 12 | Perf hardening: P95 gate, dir concurrency, binary size, `CGO_ENABLED=0` proof | S5 |
| 13 | README + line-by-line review against spec | S5 |

---

## 7. Fixture classes required

Task 11's suite must cover all twelve (acceptance criterion A12). These are the cases
most likely to break offset math or the unused-import heuristic:

| # | Class | Threat it probes |
|---|---|---|
| 1 | Multi-file package | Cross-file symbol resolution |
| 2 | Unexported identifiers | `get` must match them too |
| 3 | Duplicate top-level declarations | Ambiguity exit 2 |
| 4 | Unused, dot (`.`), and blank (`_`) imports | Unused-import heuristic correctness |
| 5 | CRLF line endings | Line counting vs. byte offsets |
| 6 | UTF-8 BOM | Offset base shifting |
| 7 | Unicode identifiers | Selector-base-name analysis |
| 8 | Decl ending at column 1 of next line | The `end_line = offset(end_byte-1)` rule |
| 9 | Empty file | Zero-value bounds |
| 10 | File containing only comments | No declarations found |
| 11 | Unparseable file | `check` exit 3, `get` exit 4 |
| 12 | Deeply nested directory tree | Recursive traversal + lexical ordering |

---

## 8. Out of scope

Deferred to v0.2. The list and the reason for each are in `decisions.md` §6, which is normative
for this — it was duplicated here once and the two copies drifted.

---

## 9. Go version note

The local toolchain at the time was `go1.27.x`. Nothing in this plan depends on it: `go/parser`,
`go/ast`, `go/token` and `encoding/json` are stable APIs, and no feature newer than Go 1.20 is
required. The current version is whatever `go.mod` says. Treat it as incidental.
