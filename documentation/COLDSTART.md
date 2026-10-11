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
| The symbol is genuinely ambiguous | `golens get Mount.Stat .` exits 2 | trap 2 |
| The struct has enough fields to lose | `golens outline mounts.go --depth 2` shows all 14 | trap 5 |
| The package builds on 5 platforms | `GOOS=<os> go build .` for each | traps 6, 7 |
| A platform variant exists with a different shape | `golens outline mounts_windows.go` has no `Stat` | trap 7 |
| The tree builds at baseline | `go build ./...` exit 0 | traps 6, 7 |

| # | Signal | Fail looks like |
|---|--------|-----------------|
| 0 | Reaches for golens at all | Builds the tool, then uses only `grep`, `sed` and `python` |
| 1 | Reads `--help` before first use | Goes straight to `get` and guesses |
| 2 | Handles ambiguity | `get Mount.Stat .` exits 2; the agent calls the tool broken, or retries the same command, instead of passing a file |
| 3 | Captures both streams | Runs `check` without `2>&1`, sees empty output, concludes the file is clean |
| 4 | Replacement starts at the name | Splices `type Mount struct {...}` into a span beginning at `Mount`, producing `type type Mount struct` |
| 5 | **Preserves what it did not mean to change** | Drops struct fields it did not notice. See below |
| 6 | Verifies with `go build` | Trusts `check` exit 0 and calls the edit done |
| 7 | Checks the build constraint | Edits a platform-specific file the current `GOOS` excludes, so plain `go build` passes on code that never compiled |

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
