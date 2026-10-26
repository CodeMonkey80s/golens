# Migration Guide: v0.2.x → v0.3.0

This document describes the changes in golens v0.3.0 and what you need to do when upgrading.

## New Feature: Package-Qualified Lookup

### What changed

`golens get <symbol> <file|dir>` now supports **package-qualified lookup** in addition to bare names and `Type.Method` qualified names:

```
golens get Package.Symbol <directory>
```

This resolves `Symbol` in the package named `Package`, within the searched directory tree.

### Why

Previously, `golens get domain.Node .` answered `"domain.Node" not found` — the same message a genuinely absent symbol gets. Callers could not tell "you used a form this tool does not accept" from "there is no such declaration."

### What changed for callers

| Query form | v0.2.x | v0.3.0 |
|------------|--------|--------|
| `get Name <dir>` | Resolves bare name across all packages | Same behavior |
| `get Type.Method <dir>` | Resolves method on type | Same behavior |
| `get Package.Symbol <dir>` | "symbol not found" | Resolves `Symbol` in `Package` |
| `get p.A <dir>` where `p` and `p_test` both exist | Cross-package (exit 2) | Resolves `A` in `package p` |

### What did NOT change

- `Type.Method` still selects a method within a type, not a package
- A bare name resolving in more than one package is still reported as cross-package (exit 2), not as package-qualified ambiguity
- If the first segment matches both a package and a type, package-qualified lookup takes precedence

---

## Change: Unused-Import Detection for Dot Imports

### What changed

Dot imports (`import . "pkg"`) are now reported as unused when there is no evidence any dot-imported name is in use.

### Before v0.3.0

Dot imports were silently skipped — never reported as unused, even when genuinely unused. This was a false negative violating the spec's "never a false negative on an import that is genuinely unused" guarantee.

### v0.3.0

golens scans the source for identifiers that are not declared locally or bound by named imports. If no such "orphan" identifiers exist, a dot import is reported as unused:

```go
package p

import . "fmt"

func F() {}
// → imported and not used: "fmt"
```

```go
package p

import . "fmt"

func F() { Println("x") }
// → no diagnostic (Println comes from fmt)
```

### Why

Without type resolution (out of scope), golens cannot know which names a dot import binds. The heuristic is: if every identifier in the source is declared locally or bound by a named import, the dot import is unused. If any identifier could plausibly come from a dot import, it is silently kept to avoid false positives.

---

## No Breaking Changes

v0.3.0 is a **minor release** — no commands, flags, or exit codes changed in a breaking way. The only behavioral changes are:

1. `get Package.Symbol` now resolves (previously answered "not found")
2. Unused dot imports are now reported (previously silently skipped)

Both changes fix false negatives; they do not introduce false positives on working code.

---

## What to Verify After Upgrading

If you integrate golens into CI or scripts:

1. **Check your codebase compiles with `golens check`** — any newly-reported unused dot imports should be reviewed
2. **Check `get Package.Symbol` queries** — if you previously worked around the lack of package-qualified lookup, you can now use the direct form
3. **Verify `--version` output** — the version is stamped at link time; see the version section below

---

## Version Reporting

`golens --version` reports the release identifier, stamped at link time:

```bash
go build -ldflags "-X github.com/CodeMonkey80s/golens/internal/cli.Version=$(git describe --tags)" -o golens ./cmd/golens
```

The variable must stay initialised to a constant string expression. The linker silently ignores `-X` on a variable whose initialiser is a call, and does so without failing the build. The source must stay free of release numbers — the tag is the single source of truth.