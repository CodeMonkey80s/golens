# CI Configuration

golens runs CI on every push and pull request to `main`. This document describes the jobs,
the gates that matter, and how to reproduce a run locally.

CI installs the Go version in `go.mod` (currently the 1.24 floor), so the floor is tested rather
than assumed. Release archives are built by the separate tag-triggered workflow described below.

---

## Jobs

### `verify` (main job)

Runs on `ubuntu-latest`. Steps, in order:

| Step | What it checks |
|------|----------------|
| **Test** | `go test -count=1 ./...` — full suite, including the fuzz seed corpus |
| **Test under the race detector** | `go test -race -count=1 ./...` |
| **Vet** | `go vet ./...` |
| **Fuzz smoke** | 10 seconds each on `FuzzOutline`, `FuzzGet`, `FuzzBounds`, `FuzzCheck`, `FuzzResolve` |
| **Check formatting** | `gofmt -l .` must print nothing |
| **Lint** | `golangci-lint` with bug-detection and formatter rules |
| **Build and check the size budget** | Build with `CGO_ENABLED=0`, verify binary &lt; 15 MiB and no cgo dependency |
| **golens on its own source** | `./golens check ./internal` and `./golens check ./cmd` both exit 0 |
| **golens on containerd** | `./golens check /tmp/containerd` exits 0, at a pinned commit |
| **Upload the binary** | Uploads the built binary as an artifact |

### `quick` (lighter job)

Runs on `ubuntu-latest`: `go test -count=1 ./...` and `go vet ./...`. It exists so a
contributor gets a fast answer without waiting for the race detector, fuzzing and size checks.

---

## Key Gates

### `-count=1` is mandatory

`go test -count=1 ./...` is required, not optional. Three packages assert facts about the build
graph or source tree rather than their own test inputs:

- `internal/arch` shells out to `go list -deps`
- `selfcheck_test.go` and `TestRepositoryPassesItsOwnCheck` walk the tree

Go keys its test cache on test inputs, so editing an import does not invalidate them, and a stale
pass is possible. This has already hidden one real failure. CI enforces `-count=1`.

### Binary budget

The release binary must be under 15 MiB (stripped with `-ldflags="-s -w"`). CI builds the binary,
checks its size, and verifies no cgo dependency via `go list -deps | grep runtime/cgo`.

### Self-check

`./golens check ./internal` and `./golens check ./cmd` must each exit 0. A checker that cannot be
run on its own codebase is not trustworthy — this is how the namespace check's false positive on
external test packages was caught.

`check` accepts exactly one path, which is why CI runs two commands rather than one. Passing
both paths in one invocation exits 1 with `accepts 1 arg(s), received 2`.

### Real-world validation

`./golens check /tmp/containerd` must exit 0. This validates golens against a real third-party
module, catching regressions that unit tests cannot: platform filename constraints, cross-package
resolution, unused-import heuristics on real import graphs, and build-tag interactions.

The containerd commit is **pinned** in `ci.yml`. Cloning the default branch would turn every
upstream change into a failure in this repository's PRs, with no local change to blame. Bump the
commit in a dedicated change when you want newer coverage, and run the check locally first.

### Fuzz smoke

The targets live in `internal/usecase/fuzz_test.go`. `go test` runs their seed corpus; CI adds a
10-second campaign per target to catch panics and broken byte invariants. `FuzzCheck` and
`FuzzResolve` exercise two-file directories, custom build tags and the JSON paths, which the
single-file outline/get/bounds targets do not. This is a smoke test, not a coverage goal — run
longer locally after touching the engine, the checks or the formatters.

### Release

`.github/workflows/release.yml` runs on a `v*` tag. It builds linux, darwin and windows archives
for amd64 and arm64 with `CGO_ENABLED=0`, stamps the version with `-X`, writes `checksums.txt`, and
attaches the assets to the GitHub release (creating it with generated notes if absent). The job
uses the workflow token, so no personal credentials are needed.

---

## Adding a New Check

1. Add the step to the `verify` job in `.github/workflows/ci.yml`
2. If it runs the binary, build it first in the same step
3. If it needs an external repository, pin the commit and fetch it shallowly
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

# Fuzz smoke (10 seconds each)
go test -run '^$' -fuzz FuzzOutline -fuzztime 10s ./internal/usecase
go test -run '^$' -fuzz FuzzGet     -fuzztime 10s ./internal/usecase
go test -run '^$' -fuzz FuzzBounds  -fuzztime 10s ./internal/usecase
go test -run '^$' -fuzz FuzzCheck   -fuzztime 10s ./internal/usecase
go test -run '^$' -fuzz FuzzResolve -fuzztime 10s ./internal/usecase

# Formatting
gofmt -l .

# Lint
golangci-lint run ./...

# Binary budget
export CGO_ENABLED=0
go build -ldflags="-s -w" -o golens ./cmd/golens
# Verify binary < 15 MiB and no cgo dependency

# Self-check (one path per invocation)
./golens check ./internal
./golens check ./cmd
```
