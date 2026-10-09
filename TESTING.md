# Manual test plan — golens against a real repository

Fifteen cases to run by hand against a real Go project. Written for golens v0.1 (Go sources
only).

**Cases 4, 6, 8 and 9 probe Go constructs the test fixtures do not cover.** Those are the
places a bug is most likely, which is why they are here rather than only in the automated suite.

## Setup

```console
$ cd ~/Development/area51/golens
$ go build -o /tmp/golens ./cmd/golens
$ cd /path/to/your/repo     # the Go project under test
```

**Ground rule:** any deviation is worth reporting. If something looks wrong, work out whether it
is *wrong* or merely *ugly* — both are useful, but only one is a bug.

---

## 1. Smoke test

```console
$ /tmp/golens outline main.go
```

Look for: `file:`, `package:`, and sections for IMPORTS, TYPES & STRUCTS, FUNCTIONS & METHODS.
The counts should roughly match what you see in the file.

Failure: a crash, empty output, or a missing section.

☐

## 2. Cross-validate the outline against grep

```console
$ grep -rhc --include='*.go' '^func ' . | paste -sd+ | bc
$ /tmp/golens outline . | grep -c '• func '
```

Look for: the totals agree.

Note that `grep '^func '` already matches methods, because `func (c ColorRGB) String()` is also
`func` followed by a space. Do **not** add a second `grep '^func ('` count — that double-counts
every method and makes the totals disagree on any repo that has methods, which reads as a
serious traversal bug when it is not.

Expect the totals to differ by a small number. `grep` also matches declarations inside `/* */`
comments, which golens correctly ignores.

Failure: a gap means the traversal missed files, which would be serious.

☐

## 3. Whole-directory outline

```console
$ /tmp/golens outline . | head -60
$ /tmp/golens outline . | grep -c '^file: '
$ find . -name '*.go' | wc -l
```

Look for: one `file:` header per file, in lexical order, none duplicated or skipped. The count
should be less than `find` if the tree has `vendor/`, `node_modules/` or hidden directories.

Failure: a file appears twice, or a file you know about is absent.

☐

## 4. Build-tagged files — untested construct

```console
$ grep -rl '//go:build' --include='*.go' . | head -3
$ /tmp/golens outline <one-of-those-files>
```

Look for: a clean parse. golens ignores build constraints entirely, so a Linux-only file parses
fine on Linux.

Failure: a parse error on a file `go build` accepts.

☐

## 5. `get` an exported function

```console
$ /tmp/golens get <SomeFuncName> <file.go>
```

Look for: the extracted text matches the file character-for-character. Compare the first and
last lines side by side. The last line should be `}` with nothing after it.

Failure: any extra or missing character.

☐

## 6. `get` a method on a generic type — untested construct

```console
$ /tmp/golens get <Type>.<Method> <file.go>      # e.g. Stack.Push
```

Look for: the correct body, and a header showing the receiver. If the repository uses type
parameters (`Stack[T]`), check the offsets carefully — this is the case most likely to break.

Failure: a wrong span, or a parse error involving the receiver.

☐

## 7. Depth 2 on a struct-heavy file

```console
$ /tmp/golens outline <file.go> --depth 1
$ /tmp/golens outline <file.go> --depth 2
```

Look for: depth 2 adds struct fields and function parameters; depth 1 does not. Check a struct
with **embedded fields** (e.g. `sync.Mutex` inline) and **struct tags** (`json:"name"`) — the
awkward cases.

Failure: a field appearing at depth 1, or a field missing at depth 2.

☐

## 8. cgo and `go:embed` files — untested construct

```console
$ grep -rl 'import "C"' --include='*.go' .
$ grep -rl '//go:embed' --include='*.go' .
$ /tmp/golens outline <each>
```

Look for: clean parses. cgo files contain unusual comment blocks and `unsafe`-adjacent syntax.

Failure: parse errors.

☐

## 9. A large generated file — untested at scale

```console
$ ls -S $(find . -name '*.go') | head -1
$ time /tmp/golens outline <biggest-file.go> --depth 1
$ /tmp/golens bounds <SomeType> <biggest-file.go>
```

Look for: completes quickly (budget is <5 ms per file parse) and the offsets are still correct.

Failure: a multi-second parse, or wrong offsets on a large file.

☐

## 10. The overwrite round trip — most important test

Work on a **copy**, not the real repository.

```console
$ cp <file.go> /tmp/test.go
$ /tmp/golens bounds <Symbol> /tmp/test.go --json --compact
```

Note `start_byte` and `end_byte`. Replace exactly those bytes with your own text, using any
editor or a script. Then verify:

```console
$ gofmt -l /tmp/test.go && go vet /tmp/test.go && /tmp/golens outline /tmp/test.go | head -20
```

**The span starts at the declaration's name, not its keyword.** For a type, var or const,
`start_byte` points at the identifier, so the `type` / `var` / `const` keyword lies *outside*
the range. Replacement text must start at the name as well:

```go
// bounds ColorRGB → the span is "ColorRGB struct {\n\t...\n}"
// WRONG: write "type ColorRGB struct {...}"  → "type type ColorRGB struct {..."
// RIGHT: write "ColorRGB struct {...}", or write back exactly what `get` returned
```

Grouped declarations are the reason: `const ( A = 1; B = 2 )` is one clause, so spanning the
clause would give `A` and `B` identical bounds and editing one would clobber the other. For a
**func** the span does start at `func`, so this caveat does not apply.

Also note `gofmt -l` will flag a struct whose fields you replaced with different alignment,
even though the file is correct Go. Check `gofmt -d` and read the diff before calling it a
failure.

Look for: `test.go` still formats, vets, and its neighbouring declarations are intact. Repeat on
a **one-line declaration** as well. Note that a valueless `var x` in a group is spanned at the
name alone, so the replacement is a name, not a whole clause.

Failure: anything unparseable, a swallowed neighbour, or mangled braces. This is the failure mode
that would actually corrupt someone's code.

☐

## 11. JSON is pipeable

```console
$ /tmp/golens bounds <Symbol> <file.go> --json | jq .start_byte
$ /tmp/golens outline . --json | jq 'length'
$ /tmp/golens get <Symbol> <file.go> --json --compact | jq -r .source | head -3
```

Look for: clean JSON, `schema_version: 1` as the first field, and `jq 'length'` returning your
file count.

Failure: invalid JSON, or stderr text leaking into stdout.

☐

## 12. Ambiguous symbol

Pick a method name that exists on two or more types.

```console
$ /tmp/golens get <MethodName> <dir>; echo "exit=$?"
```

Look for: **exit 2**, both candidates listed on stderr, and stdout empty.

Failure: if it silently picks one, that is the wrong behaviour.

☐

## 13. Audit every `check` diagnostic

```console
$ /tmp/golens check . ; echo "exit=$?"
$ /tmp/golens check . 2>&1 | wc -l
$ go build ./... 2>&1 | head
```

Read **every** diagnostic and judge it, then cross-check against the real compiler.

Look for: each diagnostic is a genuine issue, and nothing real is missing.

### Cross-check per platform, not just once

`go build` compiles one file per build-tag combination. `golens check` parses **every** `.go`
file in the tree, whatever its constraints. On a repository with platform files that is a
different file set, so a clean `go build` does not clear the diagnostics. Build each platform:

```console
$ for os in linux darwin windows freebsd openbsd; do
      GOOS=$os go build ./... 2>&1 | head -3
      echo "GOOS=$os exit=$?"
  done
```

Now group the diagnostics by kind and check each against *some* platform's verdict.

### Two false-positive classes to expect

**Redeclarations across mutually exclusive build tags.** If `isFuseFs` appears in
`filesystems_linux.go`, `filesystems_darwin.go` and `filesystems_windows.go`, no build compiles
two of them, so the compiler never reports a redeclaration. golens sees one package containing
all three and does.

**Unused imports whose path element is not a valid identifier.** golens derives an import's
local name from the last path element, and cannot resolve the real package name — module
resolution is out of scope. For `github.com/mattn/go-runewidth` that yields `go-runewidth`,
which is not a Go identifier and never appears in the source, so a genuinely used import is
reported as unused:

```console
$ cat /tmp/dash.go <<'EOF'
package dash

import "github.com/mattn/go-runewidth"

func W(r rune) int { return runewidth.RuneWidth(r) }
EOF
$ /tmp/golens check /tmp/dash.go     # reports an import that is used on the last line
```

The rule is that golens must **never falsely accuse an import that is in use**. When it cannot
determine the name, silence is correct and a diagnostic is not. Affected paths are common:
`mattn/go-isatty`, `mattn/go-colorable`, anything with a dash or a digit in the final element.

### Positive control

A check that reports nothing proves nothing. Confirm it still catches a real problem:

```console
$ cp <file.go> /tmp/ctl.go
$ # add an import that is genuinely never referenced
$ /tmp/golens check /tmp/ctl.go ; echo "exit=$?"   # want 3, naming the import
```

The unused-import check is documented as best-effort. It should **miss** some cases — an import
whose name appears only in a comment, or is shadowed by a local variable. A miss is acceptable.
A false accusation is not: it would have an agent delete working code.

Failure: any diagnostic that no platform's compiler would report, or a false positive on a used
import.

☐

## 14. Naming check against your own style

```console
$ /tmp/golens check . --naming 2>&1 | head -20
$ /tmp/golens check . --naming 2>&1 | wc -l
```

Look for: every reported identifier really is an all-caps initialism — `userID`, `HTTPHandler`.

Expect **false positives** in two situations:

- A run of capitals that is genuinely one word, such as `userIdX`. Note those separately; this
  is a known and accepted limitation.
- **Any identifier with no lowercase letters at all.** `camelCaseId` cannot be satisfied by an
  identifier that is entirely upper-case, so every SCREAMING_SNAKE_CASE constant containing a
  dictionary word is flagged by construction — `RAMFS_MAGIC` reports "RAM should be ram",
  `VMHGFS_SUPER_MAGIC` reports "VM should be vm". Those are not camelCase names, so the rule
  does not apply to them and reporting them is a defect, not a limitation.

```console
$ /tmp/golens check . --naming 2>&1 | grep -oP '(?<=: )[A-Z_0-9]+(?=: initialism)' | sort -u
```

Note the `2>&1`: `check` writes diagnostics to stderr, so a pipe without it silently sees
nothing and the raw lines scroll past unfiltered.

Failure: a plain camelCase name reported as a violation, or an all-caps identifier reported at
all.

☐

## 15. Determinism and exit codes

```console
$ /tmp/golens outline . > /tmp/a.txt
$ /tmp/golens outline . > /tmp/b.txt
$ diff /tmp/a.txt /tmp/b.txt && echo IDENTICAL

$ /tmp/golens check . 2>/tmp/c.txt
$ /tmp/golens check . 2>/tmp/d.txt
$ diff /tmp/c.txt /tmp/d.txt && echo IDENTICAL
```

Then the exit codes:

```console
$ /tmp/golens outline nope.go    ; echo $?   # want 2
$ /tmp/golens get Nope main.go   ; echo $?   # want 2
$ /tmp/golens outline            ; echo $?   # want 1
$ /tmp/golens outline /tmp/bad.go ; echo $? # want 4
$ /tmp/golens check .            ; echo $?   # want 0 or 3
```

Look for: identical output across runs, and each code as annotated.

Failure: non-deterministic output, or a wrong exit code.

☐

---

## If time is short

Run **10, 13, 6, 9** in that order.

| Case | Why it ranks here |
|---|---|
| 10 | Offsets that round-trip but address the wrong span silently corrupt files |
| 13 | A false unused-import accusation makes an agent delete working code |
| 6 | Generic receivers with type parameters — no fixture covers this |
| 9 | Large files, where an allocation or complexity problem would surface |

---

## Reporting a failure

Three things make a report actionable:

1. **The exact command**, copy-pasted.
2. **What you expected and what happened**, with the relevant output.
3. **The Go version, and whether the file builds with `go build`** — that last one separates a
   tool bug from a genuinely broken input file.

If a case passes, a one-word note is enough. The pattern of what passes on your repository is
worth knowing too.

---

## Reference

| Behaviour | Value |
|---|---|
| Byte ranges | Half-open: `src[start_byte:end_byte]` is exactly the declaration |
| Line ranges | Inclusive at both ends |
| Exit codes | 0 success · 1 usage · 2 not found or ambiguous · 3 diagnostics · 4 parse or I/O |
| JSON | `"schema_version": 1` first; payload on stdout, diagnostics on stderr |
| `--compact` | Removes decoration, never information |
| Out of scope | Unreachable code, type-level conflicts, module import resolution, non-Go languages |
