# Using golens

*Template. Copy into the root of a Go repository as `AGENTS.md`, or merge the sections you
want into the file you already have. Replace `<path>` with your repository root.*

*Note for the person copying this: it only works if your agent loads `AGENTS.md`. Most coding agents
read it once at session start from the directory they were launched in, so launch the agent in the
repository you want it to read. If yours does not load it at all, name it in your instructions.*

golens reads and edits Go source structurally, so an agent can work on one declaration without
reading a whole file.

**Use it if it is on `PATH`.** If it is, prefer it over `grep`, `sed` and reading whole files: it
answers the question directly and returns exact byte offsets for an edit. **If it is not on `PATH`,
work normally** — this repository does not require it, and `grep` and `sed` are perfectly good tools.
Do not try to install it, build it, or fetch it. Its presence is the user's decision; your job is to
use what is here.

```console
$ golens --help                          # once per session
$ golens <command> --help                # before first use of that command
```

**Read that help before using it.** The per-command conventions differ between commands and will
corrupt a file if guessed. Everything this document would otherwise have to tell you about them is
in `--help`, and that is the only place it is kept current — so an agent that reads the help cannot
be working from stale notes.

*If you are an agent and you did not read this file yourself — it was loaded for you, or you were
told to open it — it is still worth opening. It is short, and it is the only thing here that tells
you how to use the tool safely.*

## The loop

**1. Find the symbol.**

```console
$ golens outline <path> --depth 1
$ golens outline <path> --depth 2      # adds struct fields and parameters
```

`--depth` defaults to unlimited, so omit it when you want everything. `--skip-testdata` and
`--skip-ignored` opt into the view a build sees when fixtures do not parse.

**2. Read it.**

```console
$ golens get <Type>.<Method> <path>/file.go
$ golens get Package.Symbol <dir>     # when package and type names collide
```

The source block is the file's own bytes, so it is safe to write back verbatim. When the file is
not built everywhere, `get` also prints a `Build:` line with `buildable_now` in JSON; read it
before editing code for another platform.

**3. Place the edit.**

```console
$ golens bounds <Type>.<Method> <path>/file.go --json
# -> {"schema_version":1,...,"start_byte":430,"end_byte":612}
```

Replace exactly `[start_byte, end_byte)` — and take the span from the output rather than computing
it. Writing back what `get` returned always satisfies whatever rule the span follows.

**Open the file in binary.** The offsets count *bytes*, not characters. Splicing a decoded string
with those numbers is a different operation and silently corrupts any file containing a non-ASCII
identifier or a BOM:

```python
raw  = open(p, "rb").read()          # correct: raw[430:612]
text = open(p, encoding="utf-8").read()   # wrong: text[430:612] indexes characters
```

`utf-8-sig` is worse — it strips the BOM, shifting every offset by a further three. The failure
raises no exception; the span runs long, the tail of the declaration is dropped, and the parse error
that follows points at the name rather than at the cause. Use `raw[start:end]`, `read_bytes()`, or
`dd bs=1 skip=S count=E-S`.

**4. Validate.**

```console
$ golens check <path>/file.go
$ gofmt -l <path>/file.go && go vet <path>/...
$ golens outline <path>/file.go | head -20
```

Confirm the file still parses, still formats, and that its neighbours are intact. Which stream
`check` writes to is in its `--help`; it matters here, because reading only one of them can look
exactly like a clean result.

## Judgement `--help` cannot give you

The help states conventions: which stream a command writes, what its exit codes mean, where its
spans start. Those are facts about the tool, and they change when the tool changes, so they live in
one place and you read them there.

What follows is **judgement** — when to reach for the tool at all, and what a result does and does
not prove. That does not change with a release, so it is here.

**`check` is not a type checker.** It validates syntax, import usage and duplicate declarations.
Exit 0 does not mean the file compiles — an undefined type or a struct field you accidentally
dropped both pass. `go build ./...` is the only thing that settles it.

**A diagnostic is a claim, not a verdict.** `check` is deliberately biased toward silence: a false
positive tells you to delete something that works. Cross-check anything surprising against
`go build` before acting on it.

**Edit the declaration, do not rewrite it.** A `bounds` span replaces one whole declaration. If
you retype a struct body from an outline instead of editing the existing text, fields you did
not notice are silently dropped — and nothing in the tool will tell you. Re-read the original,
change only what the task requires, and confirm the diff shows additions rather than deletions.
This is the failure mode most likely to survive every check the tool performs.

**`check` does not judge style.** No naming convention, no formatting, no idiom. If you want those
enforced, run `golangci-lint run`; `revive`'s `var-naming` covers Go's initialism convention
(`userID`, `HTTPHandler`), and `gofmt -l` covers formatting. The separation is deliberate: golens
describes code, it does not grade it, so its output never mixes a structural fact with an opinion you
did not ask for.

**Token budget.** Add `--compact` to any command whose output you intend to read in full. It
removes decoration, never information. `--json` gives a machine-readable payload.
