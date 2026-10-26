# Contributing to golens

golens is a Go library and CLI that answers structural questions about Go source:
what is in this file, where is this symbol, what are its exact byte offsets, is this
package valid. It exists so an AI coding agent can read a targeted slice of a file
instead of the whole thing.

---

## Before You Contribute

1. Read `AGENTS.md` — it explains the tool's purpose, conventions, and traps
2. Read `documentation/specification.md` — it is the normative contract
3. Read `documentation/decisions.md` — it explains why each call was made
4. Run `go test -count=1 ./...` and confirm everything passes

---

## Making Changes

### 1. Fork and Clone

```bash
git clone https://github.com/CodeMonkey80s/golens.git
cd golens
```

### 2. Create a Branch

```bash
git checkout -b my-change
```

### 3. Make Your Change

Follow these rules:

- **One logical change per commit** — split if "and" appears in the subject
- **Commit message format**: `area: brief description` (imperative mood, lowercase after prefix, no trailing period)
- **Prefixes**: `feat:`, `fix:`, `refactor:`, `test:`, `docs:`, `ci:`, `cli:`, `engine:`
- **Body** (optional): explain *why* and *what changed*, not *how*

Example:
```
fix: report real positions in parse diagnostics, align spec
```

### 4. Run the Full Test Suite

```bash
go test -count=1 ./...
go vet ./...
gofmt -l .
golangci-lint run ./...
```

`-count=1` is mandatory — Go's test cache does not invalidate on packages that assert facts about the build graph or source tree, and a stale pass has hidden a real failure before.

### 5. Run golens on Its Own Source

```bash
./golens check ./internal ./cmd
```

Both must exit 0. A checker that cannot be run on its own codebase is not trustworthy.

### 6. Commit

```bash
git add <changed files>
git commit -m "area: brief description"
```

### 7. Push and Open a PR

```bash
git push origin my-change
```

Then open a pull request on GitHub.

---

## Documentation

If your change affects user-facing behavior, update these files:

| Change | Document |
|--------|----------|
| New command, flag, or exit code | `documentation/specification.md` |
| Why a decision was made | `documentation/decisions.md` |
| How to use the tool | `README.md` |
| How to use the tool as an agent | `documentation/AGENTS.template.md` (root `AGENTS.md` is synced from this) |
| How to run CI | `documentation/CI.md` |
| Breaking changes | `documentation/migration-vX.Y.md` |
| What changed | `CHANGELOG.md` |

If your change affects behavior, add or update tests:

| Change | Test |
|--------|------|
| New command behavior | `internal/cli/*_test.go` |
| New check behavior | `internal/usecase/*_test.go` |
| New AST/domain behavior | `internal/engine/goast/*_test.go` |
| New rendering behavior | `internal/format/*_test.go` |
| Byte-offset invariant | `TestBoundsAreHalfOpen` or similar |

---

## The Traps

This project has a history of bugs that shipped in green code. Read `AGENTS.md` and
`documentation/decisions.md` before changing anything. Key traps:

- **Byte offsets are half-open** — `file[start:end]` must equal the extracted source byte for byte
- **Span starts at the name, not the keyword** — for type/var/const, the keyword is outside the range
- **`check` never reports style** — naming, formatting, and idiom are out of scope
- **`check` stays out of `testdata`** — the go tool ignores it, so no build reaches it
- **Unreachable-code detection is out of scope** — it is a lint category
- **Exit codes are claims** — exit 3 means "fix the code", exit 4 means "fix the machine"
- **`--compact` is lossless** — it removes decoration, never information

---

## Release Process

1. Make your changes on `main`
2. Tag the commit: `git tag vX.Y.Z`
3. Push the tag: `git push origin vX.Y.Z`
4. The version is injected at link time — nothing in the source states a release number

The version is reported by `golens --version` and is stamped at link time with
`-ldflags "-X …/internal/cli.Version=<v>"`. The variable must stay initialised to a
constant string expression; the linker silently ignores a call initialiser.