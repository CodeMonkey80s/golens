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

duf is a good subject. It is flat, it builds cleanly on five platforms, and it has a method
defined in four platform variants, which makes ambiguity unavoidable rather than incidental.

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

Six things, in the order an agent meets them. Each is a pass/fail, not a judgement.

| # | Signal | Fail looks like |
|---|--------|-----------------|
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

## Reference result

A correct run leaves this and nothing else:

```
 mounts.go | 2 ++
 1 file changed, 2 insertions(+)
```

Two lines: the `time` import and the `CreatedAt` field. `gofmt -l` empty, `go build ./...` exit 0,
`golens check .` exit 0.

## A run that passed

Recorded so the protocol has a baseline to compare against, not just a target.

An agent given the prompt above, in a fresh session, with `golens` on `PATH` and this file copied
into the repository as `AGENTS.md`:

| Trap | Result | Evidence |
|---|---|---|
| 0 reaches for golens | pass | `golens bounds`, `golens outline` in the transcript before any `grep` |
| 1 reads the help | pass | chose `--depth 1 --compact` and `--json` per command |
| 4 span starts at the name | pass | no `type type`; file parses |
| 5 preserves the rest | pass | `git diff --numstat` → `2  0`, field inventory minus baseline is exactly `CreatedAt` |
| 6 verifies with `go build` | pass | builds clean on linux, darwin, windows, freebsd and openbsd |
| 7 checks the build constraint | pass | second run: edited `mounts_windows.go` and verified with `GOOS=windows go build ./...` |

Traps 2 and 3 were not exercised by that run and remain unverified.

Worth recording from the same session: an earlier agent, given the identical task and a binary at
`/tmp/golens` with no `AGENTS.md` in the repository, built the tool and then used only `grep`,
`sed` and `python`. It produced a correct diff and never mentioned golens. Discoverability was
the whole difference — the tool was not at fault, and no amount of help text would have reached
an agent that had no signal it existed.

## After the run

```console
$ git diff --stat     # additions only
$ gofmt -l .          # empty
$ go build ./...      # exit 0
```

Then throw the sandbox away. Do not let the agent's work persist into a repository you care
about — the point is to watch it fail safely.

## Reporting

Record which of the six the agent missed and what it said when it missed it. A wrong tool
message is worth as much as a wrong edit: it means an agent will retry the same failing command
rather than adapt. If trap 1 fails, the help is not being read, which usually means the first
line does not say enough about why to read it.
