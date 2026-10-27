# Changelog

All notable changes to golens are documented in this file.

---

## v0.6.1 (2026-10-27)

### Fixed

- Root `--help` now names `mcp` and describes the effective build constraint; it still described only the four query commands and the `//go:build` line.

### Changed

- `TESTING.md` and `COLDSTART.md` carry the 2026-10-27 run logs, including unexercised cases, the k9s OpenBSD dependency caveat, and the trap 4/7 gaps.
- Test-plan corrections: k9s has no generic receiver, embedded field or cgo fixture; case 10 now copies the repository rather than a detached file; case 12 uses a same-package ambiguity; case 13 treats non-identifier imports and platform variants as negative tests.

---

## v0.6.0 (2026-10-27)

### Added

- `--skip-testdata` and `--skip-ignored` on `outline`, `get` and `bounds`, so a source tree with intentionally broken fixtures can be viewed the way a build sees it.
- `buildable_now` on `get`, `bounds` and `outline` JSON, plus a human marker, so a platform-excluded declaration is explicit rather than inferred from its path.
- Public embedding package at the module root: `Outline`, `Get`, `Bounds`, `Check`, `Options` and `ExitCodeFor`.
- `golens mcp`: a Model Context Protocol stdio server exposing all four tools.
- Committed fuzz seed corpus for `FuzzCheck` and `FuzzResolve`, including filename constraints.

### Changed

- Release workflow now takes notes from `CHANGELOG.md` and attaches a GitHub build-provenance attestation.
- `--json-errors` distinguishes `parse` from `io` failures instead of collapsing both into `parse_or_io`.
- Effective constraint display de-duplicates terms (`linux && linux && amd64` is now `linux && amd64`).

### Fixed

- Fuzzing filenames exposed a harness bug: unbounded names exceeded the filesystem limit and hidden names produced a legitimate no-files error. The filename is now sanitized and length-capped.

---

## v0.5.0 (2026-10-27)

### Added

- `bounds` accepts a directory, resolving through the same search as `get`; A6's JSON equivalence now holds for directory targets.
- `check` accepts several files or directories, de-duplicating and sorting the combined file set.
- `--package-dir` on `get` and `bounds` resolves a symbol in one directory when two packages share a name.
- CI tests current stable Go, runs the suite on macOS, cross-builds five targets, and a nightly workflow validates the standard library and runs longer fuzz campaigns.

### Changed

- Documentation drift is now tested: every JSON tag on a payload struct must appear in `documentation/json-schema.md`, and volatile test counts/binary sizes were removed from `AGENTS.md` and softened in the README.

---

## v0.4.0 (2026-10-27)

### Added

- **Build constraints are reported everywhere.** `outline` and `get` report the effective constraint (filename + `//go:build` + cgo), not just the comment; `get` and `bounds` JSON carry `build`; ambiguity candidates carry `[build: ...]` markers.
- **`check --tags`** models a tagged build, matching `go build -tags integration`.
- **`--json-errors`** writes a machine-readable error envelope to stderr (`kind`, `message`, `exit_code`, `candidates`, `skipped`/`total`) without disturbing a partial stdout payload.
- **`FuzzCheck` and `FuzzResolve`** exercise two-file directories, custom tags, duplicate detection and the multi-file JSON paths.
- **Tag-triggered release workflow** builds linux/darwin/windows amd64/arm64 archives and `checksums.txt`.

### Fixed

- `get` and `check` disagreed about platform variants: `check` called the tree clean while `get` reported an ambiguity across mutually exclusive files.
- A symbol declared only in a platform-excluded file resolved with no indication that the current platform does not compile it.
- `get domain.Nope` advised the caller to pass `Nope`, which is also missing. Missing symbols now return `NotFoundInPackage` with no advice, and cross-package advice names the working `p.A` form.
- Unused test-only API `errors.AsType` kept the Go floor at 1.26; the floor is now 1.24.

### Changed

- Minimum Go version is 1.24 (tested in CI), down from 1.27.2.
- Bumped `github.com/spf13/pflag` from v1.0.9 to v1.0.10.
- README examples, specification, JSON schema, CI docs and `AGENTS.md` updated for the new fields and flags.

---

## v0.3.2 (2026-10-26)

### Fixed

- The spec, README and migration guide claimed package-qualified lookup overrides `Type.Method`; the code tries `Type.Method` first, and the documents now match, pinned by `TestAdversarialTypeMethodBeatsPackageQualified`
- `documentation/CI.md` claimed `golens check ./internal ./cmd` was valid; `check` accepts exactly one path, so the docs now show two invocations
- Root `AGENTS.md` had been replaced with the target-repository template, losing the repository's own build, layout, trap and acceptance-criteria guidance; restored and brought up to date
- The containerd CI target cloned the default branch, so unrelated upstream changes could fail this repository's PRs; it is now pinned to a commit

### Changed

- Fuzz targets take source and symbol as separate arguments instead of splitting one byte slice in half, which made almost every input unparseable Go
- Fuzz seed corpus added: valid files, methods, vars, dot imports and malformed input now run on every `go test`
- Fuzz assertions now check the byte invariant (`src[start:end] == source`), `schema_version`, and span bounds
- CI runs a 10-second fuzz campaign per target

### Added

- README covers package-qualified lookup and dot-import detection

---

## v0.3.1 (2026-10-26)

### Added

- Documentation set: `documentation/CI.md`, `documentation/json-schema.md` (every JSON payload field by field), `documentation/migration-v0.3.md`, `CONTRIBUTING.md`, `CHANGELOG.md`
- Fuzz targets `FuzzOutline`, `FuzzGet`, `FuzzBounds` in `internal/usecase`
- Dot-import test cases: used, unused, and import-only-local-declarations
- containerd added to CI as a real-world validation target
- Spec clarifications: package-qualified lookup precedence (§2.2), dot-import heuristic (§2.4), anonymous functions excluded (§2.1), version-injection fragility (§1.2)

### Changed

- `AGENTS.md`/template: target-repository guidance kept in `documentation/AGENTS.template.md`

---

## v0.3.0 (2026-10-25)

### Added

- **Package-qualified lookup**: `golens get Package.Symbol <dir>` resolves `Symbol` in the named package, distinct from `Type.Method` which selects a method within a type
- **Unused dot-import detection**: `import . "pkg"` is reported as unused when there is no evidence any dot-imported name is in use (heuristic: no orphan identifiers in source)
- **Alternative import names for versioned paths**: `github.com/containerd/containerd/api/services/tasks/v1` derives `tasks` (not `v1`) as the local name when `v1` does not appear in source

### Fixed

- Unused-import false positive on versioned import paths (`.../tasks/v1` was reported as unused when `tasks` was used)
- Unused dot-import false negative (dot imports were previously never reported)

### Changed

- `golens get Package.Symbol` now resolves instead of answering "symbol not found"

---

## v0.2.9

### Added

- `documentation/history.md` — consolidated project history from retrospectives
- `comparison.html` — golens vs gopls capability comparison

### Changed

- `documentation/specification.md` §1.2 — Go-only language support (removed Tree-Sitter aspiration)
- `documentation/decisions.md` §5 — clarified as historical record

### Fixed

- None

---

## v0.2.8

### Fixed

- Version injection: `Version` is a `var` initialised to a constant string expression, stamped at link time. The source no longer states a release number.
- `check` exits 4 (not 3) for unreadable files, matching `outline`
- `check --json` keeps unreadable files in the payload with stderr empty
- `get` skips unparseable files rather than aborting, returning the symbol and naming skips
- Package-prefixed queries (`get domain.Node .`) are told the resolving form instead of "not found"
- Symlinked directory arguments are resolved before walking; symlinks inside trees are still not followed

---

## v0.2.7

### Added

- `TestVersionIsInjectedByLdflags` — subprocess test that builds with `-X` and runs the binary
- `TestVersionDefaultCarriesNoReleaseNumber` — source stays free of release numbers

### Changed

- `Version` is a `var` (not `const`) initialised to a constant string expression, enabling link-time injection

---

## v0.2.6

### Fixed

- `check` and `outline` agree on exit code 4 for unreadable files
- Advice printed independently of candidate list (not nested inside candidate loop)
- `get` returns symbol from parseable files even when neighbours are broken, exiting 4 naming the skips

---

## v0.2.5

### Added

- `internal/constructs` — compiled declarations for the cross-renderer corpus walk (generics, aliases, embedded fields, generic receivers, multi-name fields, unnamed parameters)
- `TestCorpusContainsTheConstructsThatBroke` — fails if a construct is removed

### Fixed

- Multi-name struct fields (`A, B int`) produce two entries, each with its own bounds
- Unnamed function parameters (`func F(context.Context, int)`) appear as one node per name
- `internal/constructs` now declares multi-name fields and unnamed parameters

---

## v0.2.4

### Fixed

- `outline --json` includes `is_alias`, `type_params`, and `receiver` fields (was dropping them, making aliases and defined types byte-identical)
- `outline --json --depth 1` returns declarations (was returning only group headings)
- Interface methods render without `func` keyword (`Close() error`, not `func Close() error`)
- Type unions (`interface{ ~int | ~string }`) contribute no entries
- Embedded interfaces listed by name

---

## v0.2.3

### Fixed

- `check` keys namespace on directory *and* package name, not directory alone (fixes false positives on `package p` beside `package p_test`)
- Build-constraint matrix: 47 real GOOS/GOARCH pairs (not 210 cross-product), filename suffixes, cgo exclusion
- Fixes 70,854 false positives on the standard library

---

## v0.2.2

### Fixed

- `--depth` default (`-1` = unlimited) no longer collapses to depth 1 (`visible()` helper)
- Compact output no longer drops declarations with children
- Compact output no longer emits records twice

---

## v0.2.1

### Added

- `Node.IsGroup` field — group nodes are transparent, do not consume a depth level

---

## v0.2.0

### Added

- `--depth` flag on `outline`
- `--compact` flag on all commands
- JSON output on all commands
- `bounds` human output (one pinned line)
- `get --bounds-only` flag

### Changed

- Engine interface narrowed to `Parse(path) (Document, error)` for Tree-sitter readiness
- `Namespace` keyed on directory *and* package name

---

## v0.1.0

Initial release. outline, get, bounds, check commands. Half-open byte offsets. Name-scoped spans. Exit codes 0/1/2/3/4. JSON with `schema_version: 1`.