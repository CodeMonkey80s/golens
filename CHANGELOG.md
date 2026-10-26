# Changelog

All notable changes to golens are documented in this file.

---

## v0.3.0 (unreleased)

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