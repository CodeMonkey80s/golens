# CI Configuration

golens runs CI on every push and pull request to `main`. This document describes the CI jobs, gates, and how to add new checks.

---

## Jobs

### `verify` (main job)

Runs on `ubuntu-latest`. Steps:

| Step | What it checks |
|------|----------------|
| **Test** | `go test -count=1 ./...` — full test suite |
| **Test under the race detector** | `go test -race -count=1 ./...` — data race detection |
| **Vet** | `go vet ./...` |
| **Check formatting** | `gofmt -l .` must print nothing |
| **Lint** | `golangci-lint run` with bug-detection and formatter rules |
| **Build and check the size budget** | Build with `CGO_ENABLED=0`, verify binary < 15 MiB, no cgo dependency |
| **golens on its own source** | `golens check ./internal ./cmd` must exit 0 |
| **golens on containerd** | `golens check /tmp/containerd` must exit 0 (real-world validation) |
| **Upload the binary** | Uploads the built binary as an artifact |

### `quick` (lighter job)

Runs on `ubuntu-latest`. Steps:

| Step | What it checks |
|------|----------------|
| **Test** | `go test -count=1 ./...` |
| **Vet** | `go vet ./...` |

---

## Key Gates

### `-count=1` is mandatory

`go test -count=1 ./...` is required, not optional. Three packages assert facts about the build graph or source tree rather than their own test inputs:

- `internal/arch` shells out to `go list -deps`
- `selfcheck_test.go` and `TestRepositoryPassesItsOwnCheck` walk the tree

Go keys its test cache on test inputs, so editing an import does not invalidate them, and a stale pass is possible. This has already hidden one real failure. CI enforces `-count=1`.

### Binary budget

The release binary must be under 15 MiB (stripped with `-ldflags="-s -w"`). CI builds the binary, checks its size, and verifies no cgo dependency via `go list -deps | grep runtime/cgo`.

### Self-check

`golens check ./internal ./cmd` must exit 0. A checker that cannot be run on its own codebase is not trustworthy — this is how the namespace check's false positive on external test packages was caught.

### Real-world validation

`golens check /tmp/containerd` must exit 0. This validates golens against a real third-party module (containerd), catching regressions that unit tests cannot: platform filename constraints, cross-package resolution, unused-import heuristics on real import graphs, and build-tag interactions.

---

## Adding a New Check

1. Add the step to the `verify` job in `.github/workflows/ci.yml`
2. If it runs the binary, build it first in the same step
3. If it needs external repositories (like containerd), use a shallow clone to save time
4. Document the check here

---

## Local Reproduction

```bash
# Full test suite (as CI does)
go test -count=1 ./...

# Race detector
go test -race -count=1 ./...

# Vet
go vet ./...

# Formatting
gofmt -l .

# Lint
golangci-lint run ./...

# Binary budget
export CGO_ENABLED=0
go build -ldflags="-s -w" -o golens ./cmd/golens
# Verify binary < 15 MiB and no cgo dependency

# Self-check
./golens check ./internal ./cmd
```