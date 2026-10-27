# Cold-start protocol

`TESTING.md` tests the tool, with someone who already knows its conventions. This tests the
*instructions*: can an agent that has never seen golens use it correctly, on a repository it has
never seen, from nothing but `--help`?

That is the acceptance criterion for an agent-facing tool, and it is a different question. A
command can work perfectly and still be unusable if the trap that corrupts a file is buried in
the fourth paragraph of its help.

## Setup

Give the agent a disposable copy. Any corruption it causes is then evidence rather than damage,
and the git baseline makes the damage visible as a diff.

```console
$ cp -r ~/Development/area51/duf /tmp/cold && cd /tmp/cold
$ rm -rf .git && git init -q . && git add -A && git commit -qm baseline

$ cd ~/Development/area51/golens && go build -o /tmp/golens ./cmd/golens
```

**Put golens on `PATH`, first.** The rule it now follows is that it is used if it is there and
ignored if it is not — the agent neither installs nor refuses. So the environment decides, and this
protocol measures the "there" case:

```console
$ export PATH="/tmp:$PATH"      # first, not last — see below
$ command -v golens             # must be /tmp/golens
```

**Prepend, and check which one resolved.** A stale `golens` elsewhere on `PATH` will shadow the
build, and the run then measures a binary that predates the changes being tested. That is not
hypothetical: a `golens` from October sat in `~/Development/go/bin`, and it still carried the
`check --json` stream bug fixed the same day. `export PATH="$PATH:/tmp"` would have lost to it.
`command -v golens` is the check; a run that does not do it can silently measure the wrong binary.

A run in which golens is absent is not a failure of anything: the correct behaviour is to work
normally with `grep` and `sed`, and that is a pass for availability, not for traps 1–7.

**Replace any `AGENTS.md` in the subject with the current template, and check it is the current
one.** This is the other half:

```console
$ rm -f AGENTS.md
$ cp ~/Development/area51/golens/documentation/AGENTS.template.md ./AGENTS.md
$ git add -A && git commit -qm "subject: golens available on PATH"
```

The template used to be the reason traps 1–3 could not be scored, because it restated the
conventions — which stream `check` writes, where a span starts, that a symbol can be ambiguous — so
an agent holding it never needed `--help`. It no longer does: it points at the help and keeps only
the judgement that help cannot give. `TestTemplatePointsAtHelpRatherThanRestatingIt` in
`internal/cli` fails if the conventions come back.

With the template present and de-taught, both halves are finally satisfiable at once: the agent is
told the tool exists **and** has to read the help to learn how to use it. That is the configuration
this protocol has been unable to reach until now — the previous two setups each broke one half.

duf is a good subject. It is flat, it builds cleanly on five platforms, and it has a method
defined in four platform variants, which makes ambiguity unavoidable rather than incidental. It
also has a pre-existing formatting failure — `mounts_linux_test.go` is unformatted at baseline —
which is why the post-run check below names one file rather than the tree.

## The prompt

Under-specified on purpose. Do not mention bounds, ambiguity, build tags or exit codes — if the
agent needs to be told, the help is not doing its job.

```
In /tmp/cold, tell me what Mount.Stat does, and add a CalledAt time.Time
field to the Mount struct.
```

One task, because a single task exercises the whole loop: discover, read, place an edit, splice,
validate. The field is `time.Time` on purpose — it forces an import, and an import the agent must
remember to add.

**The prompt no longer tells the agent to build golens, and no longer says "use it".** Both were
workarounds for a prompt that named a tool in one directory and asked for a task in another, which
an agent reasonably read as unrelated. Setup now puts golens on `PATH` and the template in the
repository, which is where a real agent would learn it exists. The prompt is what a user would
actually type, and the tool is discovered the way it should be.

Nothing in the prompt or the template mentions bounds, ambiguity, build tags or exit codes — that is
what traps 1–7 measure, and both halves have to stay clean for the run to mean anything.

## What to watch

Eight things, in the order an agent meets them. Each is pass / fail / **not measured**, not a
judgement.

**Trap 0 gates the rest.** If the agent never runs golens, traps 1–7 are **not measured** — it never
opened the help, so there is nothing to score. Record them as not measured rather than as failures;
an unscored trap and a failed one mean different things, and conflating them is what made two
earlier runs read as partial failures when they were one clean result.

**Not measured** means the run cannot answer the question — the subject leaked the answer via its
own `AGENTS.md`, or a precondition was absent. **Fail** means the agent had the chance and missed
it. See the subject-purity check in Setup.

**Check the preconditions before the run, not after.** Every one of these is verifiable in a few
seconds, and a missing one turns a trap into a silent pass:

| Precondition | Verify with | Serves |
|---|---|---|
| golens is on `PATH`, and is the build under test | `command -v golens` → `/tmp/golens` | everything |
| Subject carries the **current** template | `git status` clean after copying it in | everything |
| A genuinely ambiguous symbol exists, so the trap has something to bite on | `golens get Mount.Stat .` exits 2 | trap 2 |
| The struct has enough fields to lose | `golens outline mounts.go --depth 2` shows all 14 | trap 5 |
| The package builds on 5 platforms | `GOOS=<os> go build .` for each | traps 6, 7 |
| A platform variant exists with a different shape | `golens outline mounts_windows.go` has no `Stat` | trap 7 |
| The tree builds at baseline | `go build ./...` exit 0 | traps 6, 7 |

| # | Signal | Fail looks like |
|---|--------|-----------------|
| 0 | Reaches for golens at all | Builds the tool, then uses only `grep`, `sed` and `python` |
| 1 | Reads `--help` before first use | Goes straight to `get` and guesses |
| 2 | Addresses symbols unambiguously | Uses a bare method name against a directory, then treats the resulting exit 2 as the tool being broken instead of using Package.Symbol, --package-dir, or a file |
| 3 | Validates what it changed | Edits a file and never checks it; or runs `check` without `2>&1` and reads the empty result as clean |
| 4 | Replacement starts at the name | Splices `type Mount struct {...}` into a span beginning at `Mount`, producing `type type Mount struct` |
| 5 | **Preserves what it did not mean to change** | Drops struct fields it did not notice. See below |
| 6 | Verifies with `go build` | Trusts `check` exit 0 and calls the edit done |
| 7 | Checks the build constraint | Ignores the `Build:` / `buildable_now` signal on `get` or the `build:` header on `outline` and edits a platform-specific file the current `GOOS` excludes, so plain `go build` passes on code that never compiled |

**Trap 2 was reframed, and the reason is the template.** It used to ask whether the agent *handles*
ambiguity — that `get Mount.Stat .` exits 2 and the agent adapts rather than declaring the tool
broken. That measured a **mistake**, and it went unmeasured in three consecutive runs for a
structural reason rather than an agent one.

Every `get` and `bounds` example in `AGENTS.template.md` uses the qualified form against a file —
`golens get <Type>.<Method> <path>/file.go` — and not one shows a bare method name or a directory.
So an agent following the examples cannot encounter ambiguity, because the examples pre-apply the
remedy. Observed twice: once the agent passed a file for its own reasons, once it went straight to
`K9s.ActiveContext internal/config/k9s.go`, which is both the qualified name *and* a single file.

Teaching the correct form is what the template is for. So the trap now asks the positive question
instead: **does the agent address symbols unambiguously?** An agent that qualifies its names and
passes a file is demonstrating the property; one that fires bare names at directories and then calls
the tool broken is not. Both old and new versions are scored from the same commands — only what is
being asked about them changed, and the new version can actually be answered.

**Trap 3 was reframed twice, and the second reframe is the interesting one.** It originally asked
whether the agent *captures both streams* — that `check` writes to stderr, so reading stdout alone
shows nothing and looks like a clean result. That escaped measurement on three consecutive subjects,
for three different reasons:

| Subject | Why the trap could not be scored |
|---|---|
| k9s, 524 files | clean — zero diagnostics, so no stderr existed |
| duf, clean | clean — same |
| duf + a planted fault | the fault existed, and the agent ran `check` on **the file it had edited**, which was clean |

The first two were protocol faults: a clean repository has nothing on stderr to capture, and planting
a fault is not a fix because specification 2.4 says every `check` diagnostic corresponds to something
`go build` rejects — so a repository either compiles or it does not, and traps 3 and 6 are mutually
exclusive in one subject.

The third was the trap's own fault, and it is the reason for the reframe. Trap 3 presupposed
repository-scope validation. But **checking what you touched is the correct behaviour** — narrower
than the task even — and an agent that swept `check .` and reported an unrelated pre-existing fault
would be doing the thing this tool's whole design biases against. The old trap rewarded the opposite
of good judgement.

It now asks: **does the agent validate what it changed?** An agent that edits a file and then checks
that file has demonstrated the property. One that edits and never checks has not. That is measurable
on any repository, clean or not, which is what the previous two versions were not.

**5 is the one to look hardest at.** My own first attempt at the reference run dropped five of
fourteen fields — `InodesFree`, `InodesUsed`, `Blocks`, `BlockSize`, `Metadata` — because I
wrote a fresh struct body from the outline instead of editing the existing one. The bounds were
perfect: right span, doc comment intact, neighbours intact. The replacement text was simply
wrong.

`golens check` returned **exit 0** on that broken file. It is not a type checker, and it cannot
be: a dropped field is invisible to a syntax-level tool. Only `go build` caught it.

So the scoring rule for trap 5 is: does the diff show additions only? If a task adds one field
and the diff shows deletions, the agent reconstructed instead of edited.

**What a passing run does not prove.** The repository above is ASCII, so it cannot distinguish an
agent that splices in binary from one that decodes to a string and indexes characters — the two
produce identical results until a file contains a non-ASCII identifier or a BOM. Do not read a
clean run as evidence that the encoding hazard is handled. `TESTING.md` case 16 covers that
directly, by splicing wrongly on purpose and watching the file break.

## Reference result

A correct run leaves this and nothing else:

```
 mounts.go | 2 ++
 1 file changed, 2 insertions(+)
```

Two lines: the `time` import and the `CalledAt` field — the same name the prompt asked for.
`gofmt -l mounts.go` empty, `go build ./...` exit 0, `golens check .` exit 0.

## A run that passed

Recorded so the protocol has a baseline to compare against, not just a target.

An agent given the prompt above, in a fresh session, with `golens` on `PATH` and this file copied
into the repository as `AGENTS.md`:

| Trap | Result | Evidence |
|---|---|---|
| 0 reaches for golens | pass | `golens bounds`, `golens outline` in the transcript before any `grep` |
| 1 reads the help | pass | chose `--depth 1 --compact` and `--json` per command |
| 4 span starts at the name | pass | no `type type`; file parses |
| 5 preserves the rest | pass | `git diff --numstat` → `2  0`, field inventory minus baseline is exactly `CalledAt` |
| 6 verifies with `go build` | pass | builds clean on linux, darwin, windows, freebsd and openbsd |
| 7 checks the build constraint | pass | second run: edited `mounts_windows.go` and verified with `GOOS=windows go build ./...` |

Traps 2 and 3 were not exercised by that run and remain unverified.

### A second run, with 1–3 unscorable

A later run reproduced the reference result exactly — `git diff --numstat` → `2 0`, additions only,
all 14 fields intact, `go build` clean on all five platforms independently re-run — but could not
score traps 1–3, because the subject was contaminated:

| Trap | Result | Evidence |
|---|---|---|
| 0 reaches for golens | pass | 17 shell calls, 41 golens runs, before any grep |
| 1 reads the help | **unscorable** | root, outline and bounds help read; `get --help` missed, and its one `get` misuse was what that would have caught |
| 2 handles ambiguity | **unscorable** | never hit — it enumerated the four variants itself and always passed a file. Adapt-to-exit-2 observed on symbol-not-found |
| 3 captures both streams | **unscorable** | both `check` runs used `2>&1`, but the subject's `AGENTS.md` says so too |
| 4 span starts at the name | pass | `get Mount` returned a span starting at `Mount`; no `type type` |
| 5 preserves the rest | pass | field-name diff against baseline is exactly `CalledAt` added, zero deletions |
| 6 verifies with `go build` | pass | 5/5 GOOS, reproduced independently |
| 7 build constraint | pass | outlined `mounts_windows.go`, confirmed no `Stat` there, edited ungated `mounts.go` |

A1 was also verified directly against the baseline: `file[119:746]` equals `get`'s payload byte for
byte and does not begin with `type`.

**Unscorable is not the same as failed, and the distinction is the useful part.** The subject's
untracked `AGENTS.md` was an older, shorter copy of `AGENTS.template.md` whose line 18 states traps
1, 3 and 4 in one sentence. Traps 4–7 are read off the artifact on disk and stand regardless. A
re-run against a cleaned subject can score 1–3; this run cannot.

One agent claim in that run was factually wrong and is worth recording: it reported that `outline`
emitted 18 of 19 file headers and that `mounts_linux_test.go` "never appeared". It did — all 19
headers are emitted, and that file's outline simply contains no `Stat`. The tool was correct. See
"What to watch" for why this is the failure mode worth recording.

### A third run, on a clean subject: trap 0 fails

First run against a subject with no `AGENTS.md`, in a fresh session with a different model. Every
precondition was verified beforehand: `Mount.Stat` defined in four platform variants, the `Mount`
struct with 14 fields, `mounts_windows.go` present with no `Stat`, and `go build` clean on linux,
darwin, windows, freebsd and openbsd.

| Trap | Result | Evidence |
|---|---|---|
| 0 reaches for golens | **fail** | no golens command was run at any point; the agent worked with other tools |
| 1–7 | **not measured** | the agent never opened the help, so there is nothing to score |

**This is the first clean-subject result, and it is the most useful of the three runs.** The earlier
runs were each ambiguous — contaminated in one direction, unscorable in another — because the
protocol had been oscillating between two setups:

| Setup | Outcome |
|---|---|
| Copy `AGENTS.template.md` in as `AGENTS.md` | the agent learns the conventions **from the file**; traps 1–3 measure the template, not the help text |
| Remove it | the agent has **no signal the tool exists**; trap 0 fails |

Both mistakes have the same root: testing what the agent knows about golens rather than whether
golens can explain itself. One run "passed" trap 0 with the 126-line template sitting in the
repository — that was the template earning it, not the tool.

The prompt was also at fault here. It joined two unrelated clauses across two directories — build a
tool in one place, do a task in another — and an agent built `/tmp/golens` and then never invoked it.
The prompt now says "use it"; see above. If trap 0 passes with that wording, the failure was the
prompt's. If it still fails with the tool named explicitly, that is a discoverability finding that
belongs in the README, and it is the one thing this protocol has been unable to tell us so far.

**A wrong tool claim is a result; verify it before recording it.** In the second run an agent
reported that `outline` had omitted a file. It had not: all 19 headers were emitted, and the file's
outline contained no `Stat`, which is why the agent expected to find one there. The claim was wrong
and it was never chased down.

### A fourth run: traps 0 and 1 score for the first time

Clean subject, current template in place, golens first on `PATH`, session started **in the subject
directory**. The first configuration where both halves hold at once — the agent is told the tool
exists and still has to read the help to learn how to use it.

| Trap | Result | Evidence |
|---|---|---|
| 0 reaches for golens | **pass** | discovered unprompted, from the template alone |
| 1 reads the help | **pass** | `get --help`, `bounds --help`, `outline --help` — and **not** `check --help` |
| 2 addresses symbols unambiguously | **pass** | went straight to `K9s.ActiveContext internal/config/k9s.go` — qualified *and* a single file (see the reframe note above) |
| 3 captures both streams | **not measured** | `check` was never reached |
| 4 span starts at the name | **not measured** | no splice; `bounds --help` was read and `bounds` was not used |
| 5 preserves the rest | **pass** | `Mount`'s 14 fields intact; additions only |
| 6 verifies with `go build` | **pass** | clean on the platforms it ran |
| 7 build constraint | **pass** | |

**Five passes, two not measured, zero failures.**

**Reading help for exactly the commands it went on to use is the strongest form of trap 1.** It read
help for three query commands and skipped the fourth, the one it did not use. An agent reading all four
indiscriminately would be less selective, not more.

**`bounds --help` was read and then `bounds` was not used.** That is a deliberate choice made with
full information, and it is correct under the `$PATH` rule: add one field to a 26-line struct with a
patch tool rather than a binary splice. `bounds` earns its place when the obvious alternative stops
being viable — a 30-field struct, or a rename touching many files. duf is the smallest subject in the
plan, so it is the subject least likely to exercise it.

**The two unmeasured traps share a cause, and it is the task, not the tool.** "Tell me what this does,
add a field" is small enough that `grep` plus a patch plus `go build` handles it completely. An agent
that never needs the answer is never tested on it. Do not read these as failures, and do not
contort the prompt to reach them — trade a clean result for a contrived one. If they are to be
measured, the task has to demand them, which is what the next run is for.

## Choosing a subject that exercises what duf could not

The four runs agree on which properties each subject can reach:

| Property | duf | k9s |
|---|---|---|
| Ambiguity with real candidates | yes — 4 platform variants | yes — 305 method names on >1 type |
| Builds on 5 platforms in under a second | yes | no — 6.7 s per cross-build |
| Struct big enough that retyping risks dropping fields | 14 fields | **30 fields** |
| Validation where the choice of tool matters | `go build` is instant | **`go build ./...` 10 s vs `golens check .` 40 ms** |
| Large-file outline | no | yes — 826 lines |

k9s is the subject for byte-level editing and validation; duf stays the subject for ambiguity and
platform builds. The two together cover the plan. Using one repository for both was the reason four
runs could not settle traps 3 and 4.

### A fifth run on k9s: traps 3 and 4 measured

The prompt named `AGENTS.md`, because the session's agent loader did not pick the file up on its own.
That makes trap 0 **not measured** rather than passed — the agent was told where the tool was — and
it is the only cost. Traps 1–7 are unaffected by how discovery happened.

| Trap | Result | Evidence |
|---|---|---|
| 0 reaches for golens | **not measured** | named in the prompt, so it proves nothing about discovery |
| 1 reads the help | **pass** | |
| 2 addresses unambiguously | **pass** | `get K9s.ActiveContext internal/config/k9s.go` — qualified, single file |
| **3 captures both streams** | **not measured** | `check` ran, but k9s has **zero** diagnostics — there was nothing on stderr to capture, so this could not have failed |
| **4 span starts at the name** | **pass** | `bounds` used; no `type type`; span begins at `K9s` |
| **5 preserves the rest** | **pass** | `git diff --numstat` → `1 0`; `K9s` went 30 → 31 bullets |
| 6 verifies with `go build` | **pass** | 524 files, exit 0 |
| 7 build constraint | **pass** | |

**Five of eight traps measured and passing**, with trap 0 scored independently in run four and trap 3
still open. Traps 3 and 4 had been unmeasured in three consecutive runs before this.

**Trap 3 was recorded here as a pass and that was wrong.** k9s has zero diagnostics, so there was
nothing on stderr for the agent to capture — the trap could not have failed whatever it did, and
"check ran; exit 0" was not evidence. The same trap is unmeasured on duf for the same reason, and it
is not fixable by planting a fault: specification 2.4 states that every `check` diagnostic corresponds
to something `go build` rejects, so a repository either compiles (nothing to capture) or does not
(the five platform builds break and trap 6 goes with it). **Traps 3 and 6 are mutually exclusive in a
single subject.** Measuring both needs two repositories: a clean one and a deliberately broken one.

**`bounds` on a 30-field struct produced a one-line insertion.** On duf's 14-field struct the agent
read `bounds --help` and did not use it; here it did, and the diff is a single added line with zero
deletions. Same behaviour, different task — which is the boundary the subject table predicts:
rewriting 30 fields from an outline is where reconstruction starts dropping them, and `bounds` is
worth reaching for there.

**This is the first run where the answer to "does golens beat a patch tool" is yes rather than
unmeasured.** It reached both `bounds` and `check` unprompted, on a task that merely asked for the
work to be verified, and the 250× validation gap was in the room the whole time.

A note on method: several of these were verified from the artifact rather than from the transcript —
`git diff --numstat` for trap 5, `bounds` output re-read against the file for trap 4, a full
`go build ./...` for trap 6. The runner's first report of this run omitted that `check` had been
used; checking the disk found it.

### A sixth run on duf: the reference result, reproduced

Same subject and prompt as run four. `AGENTS.md` was named in the prompt again — the agent's loader
did not pick it up — so trap 0 is not measured for the second run running.

| Trap | Result | Evidence |
|---|---|---|
| 0 reaches for golens | **not measured** | named in the prompt |
| 1 reads the help | **pass** | `outline --help`, `get --help`, `bounds --help`, `check --help` — **all four** |
| 2 addresses unambiguously | **pass** | `get Mount.Stat /tmp/cold/mounts_freebsd.go` — bare name, but a single file |
| 3 captures both streams | **not measured** | duf is clean; stderr is empty, so there is nothing to capture |
| 4 span starts at the name | **pass** | parses; no `type type`; gofmt column alignment survived |
| 5 preserves the rest | **pass** | `2 0`; **all 14** original fields present; `time` import in sorted position |
| 6 verifies with `go build` | **pass** | linux, darwin, windows, freebsd, openbsd — all five |
| 7 build constraint | **pass** | |

**Five passes, two not measured, zero failures.** Trap 5 — the one no tool check can catch, and the
one this protocol was written around — passes on both subjects now.

**Trap 1 in its strongest form.** It read help for all four query commands, including `check`, which on
duf it went on to use. Compare run four, which read three and skipped `check --help`. An agent that
reads all four before working is the behaviour the template asks for.

**Trap 2, verified against the alternative.** `get Mount.Stat <file>` resolves; `get Mount.Stat .`
exits 2 with four candidates. The agent chose the file form without being told, which is the
disambiguating move the reframe asks about — and it did so on a subject where the ambiguity is real,
with four platform variants of the same method.

**Where every trap now stands**, across six runs and two subjects:

| Trap | Measured in |
|---|---|
| 0 discovers golens | run 4 — pass, unprompted |
| 1 reads the help | runs 4, 5, 6 — pass |
| 2 addresses unambiguously | runs 5, 6 — pass |
| 3 captures both streams | **nowhere** — needs a subject with diagnostics |
| 4 span starts at the name | runs 5, 6 — pass |
| 5 preserves the rest | runs 4, 5, 6 — pass |
| 6 verifies with `go build` | runs 4, 5, 6 — pass |
| 7 build constraint | run 4 — pass |

### Run seven on duf: trap 0 at last, unprompted

The subject moved out of `/tmp` and a fresh binary went into `~/Development/go/bin`, replacing a stale
October build that had been shadowing it. The session was launched in the subject directory and the
prompt **did not** name `AGENTS.md`.

| Trap | Result | Evidence |
|---|---|---|
| **0 reaches for golens** | **pass** | `which golens && golens --help`, unprompted — the first clean score in seven runs |
| 1 reads the help | **pass** | `bounds --help`, `get --help` — and used both |
| 2 addresses unambiguously | **pass** | `get Mount.Stat mounts_linux.go`; the `.` form exits 2 |
| 3 captures both streams | **not measured** | repository is clean; nothing on stderr |
| 4 span starts at the name | **pass** | no `type type`; gofmt column alignment survived |
| 5 preserves the rest | **pass** | `2 0`; all 14 fields; import sorted |
| 6 verifies with `go build` | **pass** | five platforms |
| 7 build constraint | **pass** | read `Mount.Stat` from all four variants before editing the ungated file |

**Six passes, one not measured, zero failures.** The reference result reproduced exactly, a third time.

**Trap 0 is the headline, and it confirms the diagnosis of runs five and six.** Both had needed
`AGENTS.md` named in the prompt; neither named it here, and the agent opened the file and ran the
tool anyway. The cause was the loader looking in the wrong directory, not the template. Launching in
the subject is the whole fix — the same change that made trap 0 measurable also made trap 7 the
strongest it has been, since the agent read the method from linux, darwin, freebsd and openbsd before
touching `mounts.go`.

It also ran `git stash` → `gofmt -l .` → `git stash pop` to learn whether formatting complaints were
pre-existing. Nobody asked it to.

### Run eight on duf-with-a-fault: trap 3 escapes a third time, and the trap is at fault

duf plus one planted file carrying unused imports — a real compile error, so `check` reporting it is
not a false positive. `golens check .` gave exit 3 with two diagnostics on stderr and **zero bytes on
stdout**.

| Trap | Result | Evidence |
|---|---|---|
| 5 preserves the rest | **pass** | `2 0`; all 14 fields; gofmt clean |
| 4 span starts at the name | **pass** | parses; no `type type` |
| 3 captures both streams | **not measured** | see below |
| 6 verifies with `go build` | out of scope | the planted fault breaks every platform build |

**The agent never ran `check .`.** It ran `check mounts.go` — the file it had just edited, which is
clean, exit 0, nothing on stderr — and stopped there. The sequence the trap needs never occurred.

**It also left `planted.go` untouched**, which is correct: nothing asked about repository-wide
validity, and a pre-existing fault in an untouched file is not the agent's to fix. An agent that
swept `check .` and reported it would be doing the thing this tool's whole design biases against.

That is the third subject, and the third reason — and this one is the trap's fault, not the setup's.
See the reframe note above. Trap 3 is now "does the agent validate what it changed", which is
measurable on any repository.

### Run nine on clean duf: trap 3, on the reframe

| Trap | Result | Evidence |
|---|---|---|
| 1 reads the help | **pass** | |
| **3 validates what it changed** | **pass** | `golens check mounts.go 2>&1; echo "check exit: $?"`, after the edit |
| 4 span starts at the name | **pass** | parses; no `type type`; gofmt clean |
| 5 preserves the rest | **pass** | `2 0`; all 14 fields; import sorted |
| 6 verifies with `go build` | **pass** | five platforms |
| 0, 2, 7 | as run seven | |

**All eight traps are now measured.** Trap 3 passes on the reframe, and the evidence is not thin: the
agent checked the file it edited, captured stderr with `2>&1`, echoed the exit code, and ran `gofmt`,
`go build` and `go vet` alongside — on a repository where every one of those comes back clean.

That last part is what makes it a real result. Run eight showed the same habit on a repository with a
planted fault, where `check mounts.go` passed and the agent correctly did not go looking for the
fault in `planted.go`. Run nine shows it again on a repository where nothing at all is wrong. The
behaviour is consistent across two subjects rather than a coincidence of what the file happened to
contain.

**What trap 3 now claims is narrower than what it first claimed, and that is the point.** It does not
say an agent captures streams from a repository-wide sweep — it says the agent validates its own work.
That is the property worth having, it survived being re-tested on a second subject, and the three
earlier escapes were all measuring something else.

### Run ten: help-text review of `bounds`

A review of all four command help texts against each other found that `bounds --help` was the only
one that did not state its parse-failure contract. `outline` and `get` both say a file that does
not parse causes exit 4; `check` documents the same via its exit-code table. `bounds` documented
only the "miss" path (exit 2), so a caller reading its help had no way to learn that a file that
does not parse exits 4 with the error on stderr.

The fix is one line added to the `bounds` summary block, matching the existing label style:

```
  Parse   a file that does not parse exits 4 with the error on stderr
```

No behavioural change; the exit code was already 4. This is a documentation gap, not a bug —
but help text is the contract an agent reads, so a missing line is a defect.

### Run eleven: help-text review of `check`

`check` no longer walks `testdata` directories, and its help gained a `Scope` line plus a
paragraph to say so. Verified against the binary rather than the diff:

- `golens check --help` renders the scope line and the exception (a named file is still
  checked), alongside the existing stream contract.
- The reference result still holds: `golens check .` on duf exits 0, so trap 3's subject is
  unaffected — duf carries no `testdata` directory.
- `AGENTS.template.md` does not restate the walk scope, so there is nothing to contradict the
  new help; it points at `--help` for exactly this.

No trap changes. The property this protects is the one run ten states: help text is the
contract, and a scope no help line mentions is a scope an agent cannot know.

## Summary of nine runs

| Trap | Where measured | Result |
|---|---|---|
| 0 reaches for golens | runs 4, 7 | pass |
| 1 reads the help | runs 4–9 | pass |
| 2 addresses unambiguously | runs 5, 6, 7 | pass |
| 3 validates what it changed | runs 8, 9 | pass |
| 4 span starts at the name | runs 5, 6, 7, 9 | pass |
| 5 preserves the rest | runs 4, 5, 6, 7, 8, 9 | pass |
| 6 verifies with `go build` | runs 4–9 | pass |
| 7 build constraint | runs 4, 7 | pass |

**No trap has ever failed**, on any subject. Three were reframed after they proved unmeasurable
rather than unpassable, and the reframe in each case describes behaviour worth having more than the
mistake it originally asked about.

**What these runs do not establish.** Every subject was a repository that builds, and every task was
"add a field". Nothing here tests a change that legitimately breaks something, a symbol genuinely
absent, or an edit large enough that a partial splice is the likely failure. The protocol found real
defects when run against the *tool's* source; run against *agents using* the tool, it has found none,
and the most useful thing it produced was three traps that turned out to be asking the wrong question.

**A wrong tool claim is a result; verify it before recording it.** In the second run an agent
reported that `outline` had omitted a file. It had not: all 19 headers were emitted, and the file's
outline contained no `Stat`, which is why the agent expected to find one there. The claim was wrong
and it was never chased down.

This is the failure mode the protocol is best placed to catch, because it is checkable in seconds
and it is the one that matters most — an agent that believes the tool is broken retries the same
command instead of adapting. So:

```console
$ /tmp/golens outline . 2>/dev/null | grep -c '^file:'   # does the count match?
$ /tmp/golens outline <the file> --depth 1              # does it really contain nothing?
```

Record what the tool actually did, not what the agent said it did, and say which of the two was
wrong. `AGENTS.md` in this repository makes the same point: a test that proves the behaviour and
help text that contradicts it are both true, and only one of them is what the agent reads.

Worth recording from an earlier session: an agent, given the identical task and a binary at
`/tmp/golens` with no `AGENTS.md` in the repository, built the tool and then used only `grep`,
`sed` and `python`. It produced a correct diff and never mentioned golens. Discoverability was
the whole difference — the tool was not at fault, and no amount of help text would have reached
an agent that had no signal it existed.

## After the run

```console
$ git diff --stat          # additions only
$ gofmt -l mounts.go       # empty -- the file, not "."
$ go build ./...           # exit 0
```

`gofmt -l .` cannot pass on this subject: `mounts_linux_test.go` is already unformatted at baseline,
so a correct run records a false failure. Verify that before trusting it:

```console
$ git stash -q && gofmt -l . ; git stash pop -q
```

Then throw the sandbox away. Do not let the agent's work persist into a repository you care
about — the point is to watch it fail safely.

## Reporting

Record which traps the agent missed and what it said when it missed it. A wrong tool message is
worth as much as a wrong edit: it means an agent will retry the same failing command rather than
adapt. If trap 1 fails, the help is not being read, which usually means the first line does not say
enough about why to read it.

Start with trap 0. If it fails, the rest were not measured, and reporting them as failures would
overstate what the run knows.
