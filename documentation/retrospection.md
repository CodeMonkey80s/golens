# Retrospection: Writing Specifications for Agent-Built Tools

golens is the worked example. It is a Go CLI, embedding package and MCP server that answers
structural questions about Go source with byte-accurate spans. It was specified, implemented and
then corrected over roughly three weeks, from v0.1.0 to v0.6.2.

This document is not a release note. It is a guide for the next specification: what to write down,
what to leave out, when to trust the code over the document, and when to change the specification
instead of the implementation.

**Status of the example:** v0.6.2 is stable in practice, pre-1.0 in compatibility terms. The CLI
contract is the mature part; the embedding API and MCP server are younger.

---

## 1. Verdict

The project stayed on course in its core and changed at its edges.

- The four original commands, half-open byte spans, deterministic output, exit codes, JSON schema
  and no-daemon constraint survived every release.
- The Go-only scope replaced the original Tree-sitter aspiration.
- Naming validation was attempted, then deliberately removed.
- Package-qualified lookup, build-aware output, JSON errors, traversal flags, directory `bounds`,
  multi-target `check`, a public API and MCP were added after the first specification.
- The specification diff was roughly +215/−28 lines; most of the growth is precise behaviour
  that the first version left implicit.

The useful distinction is **invariant versus scope**. Invariants should be hard to change because
a test fails. Scope should be easy to change when evidence demands it, with the change recorded.

---

## 2. What the original specification promised

The v0.1 contract was deliberately small:

- Four commands: `outline`, `get`, `bounds`, `check`.
- Go source only, with a Tree-sitter path for other languages described as future work.
- Stateless, on-demand parsing; no daemon, index or database.
- Byte offsets that are half-open, with line ranges inclusive.
- Deterministic output; concurrency must not be observable.
- Exit codes 0–4 and a strict stdout/stderr split.
- JSON with `schema_version: 1` as the first field.
- A parse budget under 5 ms P95 and a binary under 15 MiB.
- `check` as a structural validator, not a compiler or a style linter.

Most of that list is still the specification. The parts that changed are instructive.

---

## 3. How the specification moved

| Phase | Change | Why |
|---|---|---|
| v0.1 | Foundation, four commands, byte contract | The original plan |
| v0.2 | Generics, aliases, multi-name fields, unnamed parameters, interface methods, depth and JSON parity | Real Go constructs exposed omissions that the fixtures did not contain |
| v0.2–v0.3 | Symlinks, partial trees, testdata scope, I/O vs parse exit codes | Real repositories and the standard library behaved differently from the fixtures |
| v0.3 | Package-qualified lookup, `/vN` import names, dot imports | Users and real modules asked forms the spec did not resolve |
| v0.4 | Effective build constraints, `check --tags`, JSON errors | A structural tool cannot pretend the build does not exist |
| v0.5 | Directory `bounds`, multi-target `check`, `--package-dir`, CI breadth, nightly checks | The interface and the evidence both grew |
| v0.6 | `--skip-testdata`, `--skip-ignored`, `buildable_now`, public API, MCP | Build-facing traversal and non-CLI consumers |
| v0.6.2 | Package-level names outrank same-named methods | A repository-wide sweep found a legal Go shape that was unaddressable |

The direction was consistent: **from “what is in this file” to “what would a build see, and what
can an agent safely do with it.”** The project did not become a linter, a language server or a
type checker.

### What was removed

- **Tree-sitter and non-Go languages.** Keeping an engine abstraction for a future language cost
  design attention and bought nothing. Go-only was the honest scope.
- **Naming validation.** It was implemented, produced thousands of false positives on the
  standard library, and was ultimately indistinguishable from `revive`. A structural tool does
  not grade style. See `decisions.md` D9.

Removing a feature is a specification success when the feature cannot meet the higher contract.

---

## 4. Invariants that survived

These are the load-bearing promises. Changes to them need a specification change and a very good
reason.

1. **The byte invariant.** `file[start_byte:end_byte]` is exactly the source `get` returned.
2. **Half-open spans; inclusive lines.** The terminating newline is outside the span.
3. **The span starts at the name where the language requires it.** For a type, var or const the
   keyword is outside the range so grouped declarations stay independently addressable.
4. **Determinism.** Identical input gives byte-identical output, including under concurrency.
5. **Stream separation.** Payloads on stdout, diagnostics on stderr; `check --json` keeps stderr
   empty.
6. **Exit codes are claims.** 2 means correct your input; 3 means fix the code; 4 means fix the
   machine or the file. 4 wins when both 3 and 4 apply.
7. **`schema_version: 1` first.** A consumer can detect format drift.
8. **No style, no type checking, no daemon.** These are scope boundaries, not missing features.

A specification should state which items are invariants and which are preferences. If everything
is “important,” nothing is.

---

## 5. What changed, and why

| Area | Initial assumption | Reality | Result |
|---|---|---|---|
| Resolution | Bare name or `Type.Method` | Users wrote `Package.Symbol`; packages collide | Package-qualified lookup; later `--package-dir` |
| Package/type name collision | “A qualified name cannot select a package” | That was a limitation, not a law | `Type.Method` first; package lookup on zero match |
| Build context | Constraints were an `outline` header detail | `get` resolved excluded files; `check` missed tagged conflicts | Effective constraint, `buildable_now`, `check --tags` |
| `bounds` input | File only | `get --bounds-only` accepted directories, so A6 was false for them | Directory `bounds` |
| `check` input | One path | Callers naturally passed two | Variadic, de-duplicated and sorted |
| Errors | Human prose plus exit code | Agents parse output; exit 2 conflated miss and ambiguity | `--json-errors` with `kind` and candidates |
| Traversal | Structural commands walk everything | Testdata fixtures intentionally fail to parse | `--skip-testdata`, `--skip-ignored`; `check` skips both by default |
| Surface | CLI only | Embedding and MCP were natural consumers | Public output-oriented API and MCP server |

Each row started as an unstated assumption. The correction was not “the code drifted”; the
specification was incomplete.

---

## 6. Issue classes encountered

### 6.1 Missing requirements

The first specification did not define:

- What `Package.Symbol` means, or which form wins when a package and a type share a name.
- Whether build constraints affect resolution or only display.
- What an agent should do with a machine error.
- Whether `testdata` and `_`-prefixed files are part of “the tree.”
- Whether a directory is a valid `bounds` target.
- Whether a method name is in package scope when a type has the same bare name.

These are not implementation bugs. They are specification gaps that the implementation had to
invent answers for. Write the precedence rules before the resolver.

### 6.2 Wrong assumptions about Go

The fixtures did not contain many ordinary Go constructs, so several defects worked by omission:

- `type Alias = X` rendered as a defined type.
- `A, B int` rendered only `A`.
- `func F(context.Context, int)` rendered no parameters.
- An interface omitted its method set.
- JSON dropped `is_alias`, `type_params` and `receiver`.
- `--depth 1 --json` returned group headings and no declarations.

Each was a declaration head that either did not compile or described a different program. A
fixture corpus is not evidence until it contains the constructs that break: generics, aliases,
embedded fields, generic receivers, multi-name fields, unnamed parameters, unions and methods.

### 6.3 Green-test traps

Several defects survived a green suite because the test asserted a subset, an empty set, or only
one renderer. Examples:

- A JSON test that checked “every JSON name appears in human output” passed with zero JSON nodes.
- A test that asserted one direction of a renderer comparison missed the other.
- A stale test cache reported `ok` after an import change.
- Help text claimed `check` kept stdout empty while `check --json` wrote there.

Countermeasures:

- Compare **counts** in both directions, not subsets.
- `go test -count=1` is mandatory for packages that inspect the build graph or source tree.
- Pin help claims to tests.
- Treat the corpus as compiled source, not fixture text in a raw string.

### 6.4 Cross-command disagreement

The same input must not get different answers from different commands. It did:

- `check` called mutually exclusive platform files clean; `get` called them ambiguous.
- `check` exited 3 for an unreadable file; `outline` exited 4.
- `get --bounds-only` accepted directories; `bounds` rejected them.
- `check` skipped `testdata`; `outline` and `get` walked it.

Fixing the wording is not enough. Decide which behaviour is true and make both commands implement
it, or make the difference explicit in the specification.

### 6.5 Build and platform reality

A structural tool sees every file; a compiler sees a build set. The gap produced:

- 70,854 false redeclarations on the standard library before filename, tag and cgo constraints
  were all modelled.
- A false negative for `//go:build integration`.
- Platform-excluded symbols that looked live.
- 1,212 diagnostics from intentionally broken compiler fixtures under `testdata`.

Specify the build model: GOOS/GOARCH pairs, filename suffix, `//go:build`, cgo, custom `--tags`,
and what “could co-build” means. If you do not model it, say the tool is text-only.

### 6.6 Resolution edges

- A name declared in `package p` and `package p_test` is not a duplicate.
- A package-qualified miss is not the same as a bare miss.
- A type and a method may share a bare name; the method is not in package scope.
- Two directories may both declare `package util`, so a package-qualified name is still ambiguous.

Each needs a precedence rule and an error shape that tells the caller what to do next.

### 6.7 Release and documentation process

- The version was once a source constant, so every build reported `0.1.0`.
- A stale local binary produced a false containerd failure.
- `CHANGELOG.md` said “unreleased” after tags existed.
- `documentation/CI.md` documented an invocation the CLI rejected.
- The spec, README and migration guide all claimed package-qualified lookup took precedence over
  `Type.Method`; the code did the opposite.
- README sizes and counts drifted from the build.

The lesson is not “write less documentation.” It is **test the machine-checkable claims and remove
the rest**: schema fields, README examples, help text, release commands. Prose rationale can age;
an output example cannot.

### 6.8 Agent-facing reality

- An agent that never loaded `AGENTS.md` did not discover golens at all.
- A template that restated the help conventions made the cold-start test vacuous.
- The add-a-field task did not exercise `bounds` or a platform edit, so two traps were unmeasured.
- Fuzz seeds split one byte slice in half, making most inputs unparseable Go.
- Fuzzing filenames found a harness bug (unbounded names, hidden names), not a product bug.

Agent instructions are part of the specification surface. Test discoverability separately from
the tool. Record unexercised cases as gaps, never as passes.

---

## 7. What to specify before writing code

Use this as a checklist. Every item should be normative, observable, or explicitly non-normative.

### Contract

- One-sentence purpose and the consumer.
- The single source of truth for versions and schemas.
- Exit codes, including precedence when several apply.
- stdout/stderr behaviour per command and per output mode.
- Error taxonomy: not found, ambiguous, usage, parse, I/O, conflict.
- Whether errors have a machine-readable form.

### Data and spans

- Byte vs character semantics; half-open or closed.
- What the span starts at for each declaration kind.
- Trailing newline policy.
- Encoding assumptions: UTF-8, BOM, CRLF, non-ASCII identifiers.
- Line numbering: 1-based, inclusive.
- JSON schema with required/optional fields and omission rules.

### Resolution

- Bare names, methods, package-qualified names, files vs directories.
- Precedence when forms collide.
- Ambiguity definition and candidate listing.
- Cross-package names vs real duplicates.
- Remedies that actually resolve the problem.
- What happens with unparseable neighbours.

### Build and traversal

- GOOS/GOARCH model; filename suffixes; `//go:build`; `cgo`; custom tags.
- Which files are “in the tree” vs “in a build.”
- Hidden, `testdata`, `_`, vendor, symlinks and ignored directories.
- Whether structural commands and `check` use the same traversal.

### Behaviour

- Determinism and concurrency guarantees.
- Performance budget and how it is measured.
- Output stability and schema versioning policy.
- Streaming, partial output and exit-code interaction.
- Resource limits, if any.

### Scope

- Explicit non-goals: type checking, style, module resolution, daemon, language server.
- What happens to unsupported Go constructs.
- Compatibility policy before and after 1.0.

### Evidence

- Acceptance criteria mapped to named tests.
- Real repositories used as targets.
- Manual/cold-start protocol and its environment assumptions.
- What counts as a false positive vs an accepted limitation.

---

## 8. Specification or code: which is wrong?

Use this order.

1. **Check the observable reality.** Does `go build`, the Go language, the protocol, or a real
   repository contradict the claim? Reality wins.
2. **Check the contract’s level.** If the code violates a stated invariant, fix the code. If the
   spec never stated the behaviour, fix the spec.
3. **Check the consequence.** A false positive that deletes working code is worse than a false
   negative that leaves dead code. A silent wrong span is worse than a loud error.
4. **Check the scope.** Is this a bug in the agreed scope, or a request to expand it? Expanding
   scope needs a new acceptance criterion, not a patch hidden in a bug fix.
5. **Record the decision.** Add the evidence and the rule to the decision log before changing the
   implementation.

A useful test: if a user cannot tell whether the tool is wrong or the document is wrong, the
specification has failed.

---

## 9. When to correct the specification

Correct it early when:

- Two valid interpretations produce different output.
- A requirement cannot be measured or tested.
- A term is used with two meanings.
- An error has no remedy, or the remedy does not work.
- A format is described in prose but not in a schema.

Correct it during implementation when:

- The language or platform behaves differently from the assumption.
- Satisfying the requirement would violate a higher contract.
- A real repository exposes a false positive or a missing form.

Correct it during agent testing when:

- The agent cannot discover the tool.
- The agent reads the help and still chooses the unsafe operation.
- The agent follows an error's advice and remains stuck.

Correct it at release when:

- A documented command does not run.
- A version, count or size claim has drifted.
- A changelog or release note claims work that is not in the tag.

Do **not** change the specification merely because a test is inconvenient, or because one
repository has an unusual layout. Add a fixture and decide explicitly.

---

## 10. What to avoid

- **Vague adjectives.** “Fast,” “robust,” “agent-friendly” without a budget or a test.
- **Undefined precedence.** If two forms can match, say which wins.
- **Multiple sources of truth.** Version, schema and conventions live in one place.
- **Examples without a schema.** A sample JSON object does not define required versus optional.
- **Subset assertions.** They pass on the empty set.
- **Tests that assert one direction.** Compare renderings by count, both ways.
- **Silent fallbacks.** Guessing between candidates is worse than reporting ambiguity.
- **Error advice that does not work.** “Pass the bare name” after the bare name was already
  known to be missing is worse than no advice.
- **Style or type checking in a structural tool.** It changes the product's identity.
- **Overfitting to one repository.** Use several, including the standard library.
- **Documentation as proof.** A green suite and a confident README can both be wrong.
- **Feature expansion without acceptance criteria.** Every new surface needs an invariant, a test
  and a failure mode.

---

## 11. Agent-specific guidance

An agent is a consumer with no shared context. That changes the specification.

- **Help text is part of the contract.** Test the claims it makes. If help says stdout stays
  empty, a test should prove it.
- **Discoverability is a separate requirement.** An agent may not load `AGENTS.md`; it may not
  read the README. Provide a machine-level signal and test the cold start.
- **Prefer machine-readable output.** Stable JSON, explicit error kinds, candidates and skip
  counts. Prose is for humans; agents need fields.
- **Make remedies executable.** An error should name a form that resolves, not merely what failed.
- **Protect the edit loop.** `get` → `bounds` → binary splice → `check`/`go build`. Every step
  should be safe to retry and should fail loudly.
- **Do not require hidden conventions.** If the agent must know a convention that is not in
  `--help`, the help is incomplete.
- **Test with a fresh agent.** A developer who knows the tool cannot measure discoverability.
- **Record unexercised traps.** A cold-start run that never uses `bounds` has not measured the
  span trap.

---

## 12. Worked example: the precedence contradiction

This is the clearest case of a specification problem that looked like a code problem.

- The code tried `Type.Method` first and package-qualified lookup only on zero matches.
- The specification, README and migration guide all said package-qualified lookup took precedence
  when a name was both a package and a type.
- Every test passed, because no test pinned the documentation claim.
- The fix was not to change the resolver. The fix was to make the documentation match the code,
  add `TestAdversarialTypeMethodBeatsPackageQualified`, and state the rule once in the spec.

A second case: a missing package-qualified symbol returned an error advising the caller to “pass
the bare name.” The bare name was also missing. The advice was stale from before package-qualified
lookup existed; a test asserted only that advice was non-empty.

**Rule:** every error's remedy must be tested, not merely printed.

---

## 13. Evidence that mattered

- Unit and integration tests, including acceptance criteria A1–A25.
- Race detector, shuffle, `-count=1`, and a mandatory self-check.
- Fuzzing: single-file and two-file targets, filename fuzzing, committed seed corpus.
- Real repositories: duf, k9s, containerd and the Go standard library.
- k9s: 530 files, 3,928 functions, depth counts 9,245 / 14,974, `check` clean.
- Standard library: 7,134 parsed files, 45 intentionally unparseable fixtures, `check` exit 0.
- Nightly `go build std`, stdlib check, k9s build/check and longer fuzz campaigns.
- Release workflow: archives, checksums, changelog-derived notes, provenance attestation.
- Manual test plan with preflight, explicit unexercised cases, and run logs.
- Cold-start runs that distinguish “agent did not load instructions” from “tool failed.”

Track **false positives and span corruption**, not total test count. A suite of green tests that
never exercises the dangerous operation is evidence of nothing.

---

## 14. Maintenance policy

Once the core contract is stable, stop adding features.

- Fix reported correctness, security, toolchain and CI issues.
- Keep the Go floor, dependencies and release workflow current.
- Re-run real-world and cold-start checks after help or output changes.
- Tag fixes; do not turn a patch release into a scope expansion.
- Reopen development only for evidence: a false positive, a span bug, non-determinism, a broken
  supported platform, or a real consumer regression.

Pre-1.0 can be stable in practice. A 1.0 tag is a compatibility promise, not a quality award.

---

## Appendix A: One-page specification checklist

- [ ] Purpose and consumer stated.
- [ ] Invariants separated from preferences.
- [ ] Commands, inputs and precedence defined.
- [ ] Exit codes and stream routing defined, including precedence.
- [ ] Error taxonomy and remedies defined.
- [ ] Byte/line/encoding semantics defined with examples.
- [ ] JSON schema has required/optional/omitted fields.
- [ ] Resolution rules cover bare, qualified, package and cross-package cases.
- [ ] Build model defines GOOS/GOARCH, tags, cgo and filenames.
- [ ] Traversal names what is skipped and why.
- [ ] Determinism, concurrency and performance are testable.
- [ ] Scope and non-goals are explicit.
- [ ] Every normative claim maps to a test, a manual check, or a “non-normative” label.
- [ ] Real-world and cold-start evidence is planned.

## Appendix B: Red flags during implementation

- “The test passes” but the assertion only checks a subset.
- A help sentence that no test mentions.
- Two commands that answer the same input differently.
- An error whose advice cannot be followed.
- A new field on one renderer but not the other.
- A fixture corpus that lacks the construct that just broke.
- A specification sentence with “should” and no test.
- A version, size or count copied into prose by hand.
- A platform claim tested on one platform.
- A green run that used a stale binary.

## Appendix C: Good and bad clauses

**Bad:** “Fast enough for agents.”
**Good:** “Single-file parse under 5 ms P95 on the CI runner; `TestParseP95UnderBudget` enforces it.”

**Bad:** “Handles ambiguous symbols.”
**Good:** “A bare name with more than one peer exits 2 and lists candidates; `Type.Method` wins over
a package when both match; package-qualified lookup applies only after a zero match.”

**Bad:** “JSON output.”
**Good:** “`schema_version` is the first field; `build` and `buildable_now` are omitted when the
file is always built; `get --bounds-only` is byte-identical to `bounds`.”

**Bad:** “Checks the code.”
**Good:** “Reports parse errors, unused imports, malformed import paths and redeclarations under the
modelled build; it is not a type checker and duplicate struct fields are out of scope.”

## Appendix D: Issue ledger

| Issue | Class | Lesson |
|---|---|---|
| Alias `=` rendered as a defined type | Wrong assumption | Compile the declaration head; omission is silent |
| Multi-name fields/params omitted | Wrong assumption | One node per name; corpus must contain the construct |
| JSON dropped `is_alias` | Renderer drift | Every head field reaches every renderer |
| `--depth 1 --json` empty | Subset test | Compare counts in both directions |
| Cross-package name called ambiguous | Missing precedence | Name the namespace rule before resolving |
| Symlinked directory empty | Environment assumption | Test symlinks; resolve only the root |
| `get` aborted on broken neighbour | Error scope | Partial answer + exit 4, not no answer |
| 3 vs 4 for unreadable file | Exit-code contract | Separation of code finding and machine failure |
| 70,854 stdlib redeclarations | Build model | Filename + tag + cgo, over real pairs |
| `check` missed custom tags | Build model | `--tags` or document the default build only |
| `get`/`check` platform disagreement | Cross-command | One build model, one answer |
| Type/method same bare name unaddressable | Missing precedence | Package-level names outrank methods |
| Version hardcoded `0.1.0` | Release process | Tag is the only source; test the linker path |
| Stale binary false alarm | Environment | Check `command -v` and version before a run |
| Docs said package lookup won | Untested prose | Pin documentation claims to tests |
| Fuzz filenames too long/hidden | Harness assumption | Sanitize, cap, and accept legitimate no-file errors |
| Cold start never used golens | Discoverability | Test `AGENTS.md` loading separately |

## Appendix E: Acceptance-criterion template

```
A<N> — <one sentence, observable outcome>
Given: <input and environment>
When:  <command or API call>
Then:  <exact output, exit code, stream and invariant>
Proof: <named test, real-repo run, or manual protocol>
Limits: <what this does not claim>
```

## Appendix F: Suggested section order

1. Purpose and consumer.
2. Invariants.
3. Commands and inputs.
4. Output schemas.
5. Exit codes and streams.
6. Resolution and precedence.
7. Build model and traversal.
8. Errors and remedies.
9. Determinism and performance.
10. Scope and non-goals.
11. Compatibility and versioning.
12. Acceptance criteria.
13. Manual/cold-start evidence.
14. Open questions and rejected alternatives.

---

## Final rule

A specification for an agent is not a description of what the author hopes the tool does. It is a
set of observable claims that a fresh consumer can act on, a test suite can falsify, and a
maintainer can change deliberately when reality disagrees.

Write the invariant, the example and the failure mode. If you cannot state all three, the
requirement is not ready.
