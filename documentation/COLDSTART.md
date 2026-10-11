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

**Remove any `AGENTS.md` from the subject before the run, and check.** This is not optional and it
is not a hypothetical:

```console
$ rm -f AGENTS.md
$ git status --porcelain     # must be empty
```

`cp -r` copies untracked files, and a repository that has previously been used with golens may
carry one. A copy of `AGENTS.template.md` states, in one sentence, that byte offsets start at the
declaration's *name*, that `check` writes to stderr, and that a symbol can be ambiguous — which is
traps 3, 4 and 1 exactly. An agent handed that file cannot be scored on whether it read `--help`,
because the answer is already in its instructions. A run contaminated this way still produces
useful evidence for traps 5–7 (which are read off the artifact on disk), but 1–3 are **unscorable**,
not "pass". Say so in the report rather than recording a result you cannot trust.

duf is a good subject. It is flat, it builds cleanly on five platforms, and it has a method
defined in four platform variants, which makes ambiguity unavoidable rather than incidental. It
also has a pre-existing formatting failure — `mounts_linux_test.go` is unformatted at baseline —
which is why the post-run check below names one file rather than the tree.

## The prompt

Under-specified on purpose. Do not mention bounds, ambiguity, build tags or exit codes — if the
agent needs to be told, the help is not doing its job.

```
Build golens from ~/Development/area51/golens into /tmp/golens. Then, in
/tmp/cold, tell me what Mount.Stat does, and add a CalledAt time.Time field
to the Mount struct.
```

One task, because a single task exercises the whole loop: build, discover, read, place an edit,
splice, validate. The field is `time.Time` on purpose — it forces an import, and an import the
agent must remember to add.

## What to watch

Eight things, in the order an agent meets them. Each is pass / fail / **unscorable**, not a
judgement. Unscorable means the run cannot distinguish "the agent worked it out" from "the agent
was told" — see the subject-purity check in Setup.

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

Record which of the six the agent missed and what it said when it missed it. A wrong tool
message is worth as much as a wrong edit: it means an agent will retry the same failing command
rather than adapt. If trap 1 fails, the help is not being read, which usually means the first
line does not say enough about why to read it.
