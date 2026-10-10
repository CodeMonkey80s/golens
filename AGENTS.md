# AGENTS.md

Instructions for agents working in this repository.

## What this is

golens is a Go library and CLI that answers structural questions about Go source: what is in
this file, where is this symbol, what are its exact byte offsets, is this package valid. It
exists so an agent can read a targeted slice of a file instead of the whole thing.

The consumer is an agent, so output shape is a contract, not a preference. Two properties are
load-bearing and are asserted by tests:

- `file[start_byte:end_byte]` equals the source `get` returned for the same symbol, byte for
  byte.
- Byte ranges are half-open; line ranges are inclusive at both ends; the terminating newline is
  outside the span.

Do not change either without reading `documentation/decisions.md` first.

## Build and test

```console
$ go build ./...          # build
$ go test ./...           # full suite
$ go test -short ./...    # skips the tests that walk this repository
$ go vet ./...
$ gofmt -l .              # must print nothing
$ golangci-lint run ./... # includes revive's var-naming
```

`internal/arch` asserts layering: the CLI must not import an engine, and the domain must not
import `go/ast`. It fails the build if those boundaries are crossed, which is deliberate.

**Naming is the linter's job, not golens'.** `golens check` reports structural faults and never
style. This repository follows Go's initialism convention — `userID` and `HTTPHandler`, not
`userId` and `HttpHandler` — and `revive`'s `var-naming` rule enforces it in CI. An initialism
stays lowercase only at the front of an identifier that already begins lowercase, so `urlPath`
is correct. Do not add a naming check to the tool: `decisions.md` D9 records why, including the
4,119 false positives an earlier version produced on the standard library.

## Layout

| Path | Holds |
|---|---|
| `cmd/golens` | main; composition only |
| `internal/cli` | cobra wiring, exit codes, stream routing |
| `internal/usecase` | command behaviour, the checks |
| `internal/engine/goast` | AST to domain model |
| `internal/domain` | shared types, including `Declaration()` |
| `internal/format` | human and JSON rendering |

## Traps

**`domain.Node.Receiver` is the receiver as the source spells it**, `"s *UserStore"`, not a bare
type. If you change it, grep for `Receiver` in `internal/usecase` — the naming check that once
consumed it is gone, but the format is still load-bearing for `get` and `bounds`.

**Declaration heads must match the source.** A rendered head that does not is an edit hazard: an
agent copying `type Stack struct` for a real `type Stack[T any] struct` writes code that will not
compile. `TestDeclarationHeadsMatchSource` in `internal/engine/goast` pins the constructs that
previously diverged, including the alias `=` and the receiver variable name.

**`check` deliberately under-reports.** A false positive on an import that is in use would have
an agent delete working code, so the check stays silent whenever it cannot determine the local
name. When adding a check, bias toward silence and say so in the comment.

## Testing this tool

`TESTING.md` is an eighteen-case manual plan to run against a real repository, not a fixture. It
found the declaration-head divergences, the unused-import false positive and the
build-constraint false positives, none of which any unit test had caught. Cases 10 and 16 corrupt
a file without raising anything, which is why they lead the short list — and case 16 fails only on
some layouts, so one clean run is not evidence the approach is safe.

**Run its preflight first.** Cases 4, 6, 8 and 9 probe constructs the fixtures do not cover, so
a repository lacking the construct cannot fail them — the case runs, passes, and tests nothing.
The preflight prints which constructs the target actually has. A zero is not a failure, it is a
gap in coverage: find another repository or record the case as unexercised. Never record a
vacuous case as a pass.

Run the plan against a real Go project after any change to the engine or the checks, and treat
every deviation as worth reporting — distinguish *wrong* from merely *ugly*, and only one of
those is a bug.

## Agent-facing docs

`README.md` and `documentation/specification.md` are read by agents using this tool.
`golens --help` and `golens <command> --help` carry the per-command conventions: byte-offset
semantics, exit codes, and which stream each command writes to. Keep those in the help text
rather than duplicating them here — help cannot drift from the binary.

`documentation/AGENTS.template.md` is a starting point for a repository that wants its own
agents to use golens. Copy it into the target repo's root and edit.

`documentation/COLDSTART.md` tests the instructions rather than the tool: it gives an agent with
no prior context a real repository and a two-part task, then scores six things. Run it after
changing any help text. It is the only check that would catch a trap moving back down the help
output.
