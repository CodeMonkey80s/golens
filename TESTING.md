# Manual test plan — golens against a real repository

Eighteen cases to run by hand against a real Go project. Written for golens v0.1 (Go sources
only).

**Cases 4, 6, 8 and 9 probe Go constructs the test fixtures do not cover.** Those are the
places a bug is most likely, which is why they are here rather than only in the automated suite.
They are also the easiest to skip without noticing, so **run the preflight first** — a repository
that lacks the construct cannot fail the case, and a case that ran vacuously looks identical to
one that passed.

## Setup

```console
$ cd ~/Development/area51/golens
$ go build -o /tmp/golens ./cmd/golens
$ cd /path/to/your/repo     # the Go project under test
```

**Ground rule:** any deviation is worth reporting. If something looks wrong, work out whether it
is *wrong* or merely *ugly* — both are useful, but only one is a bug.

## Preflight — does this repository exercise the hard cases?

Cases 4, 6, 8 and 9 probe constructs the fixtures do not cover, so they are the ones most likely
to find something. **They are also the easiest to skip without noticing**, because a repository
that lacks the construct cannot fail the case: no cgo file means nothing to trip over, and a
"pass" is indistinguishable from a pass that examined something.

Run this first, in the repository under test:

```console
$ printf 'build tags : %s\n' "$(grep -rl '//go:build' --include='*.go' . 2>/dev/null | wc -l)"
$ printf 'generics   : %s\n' "$(/tmp/golens outline . --depth 1 2>/dev/null | grep -cE '^  • (type [A-Za-z_][A-Za-z0-9_]*|func [A-Za-z_][A-Za-z0-9_]*)\[[A-Za-z]|^  • func \([^)]*\[[A-Za-z]')"
$ printf 'cgo        : %s\n' "$(grep -rl 'import "C"' --include='*.go' . 2>/dev/null | wc -l)"
$ printf 'go:embed   : %s\n' "$(grep -rl '//go:embed' --include='*.go' . 2>/dev/null | wc -l)"
$ printf 'largest    : %s\n' "$(ls -S $(find . -name '*.go' -not -path '*/testdata/*' -not -path '*/.git/*') | head -1)"
```

The generics probe asks golens rather than grep, because that is the point of the tool: a `grep` for
`^type X[` also matches **test data inside a raw string literal**, so it reports generics in a
repository that declares none. golens reads code, not text. This is not hypothetical — run against
golens itself the grep route reports `1` and the golens route reports `0`, because its one
`type Stack[T any]` is fixture text inside a backtick string.

The pattern matches a generic type, a generic function, and a method on a generic receiver, and
deliberately does **not** match `type X map[K]V`, which is a map and not a type parameter list.
Verified against four repositories that declare none and one that does.

| Case | Needs | A zero or small result means |
|---|---|---|
| 4 | build tags | cannot run here |
| 6 | generics (`func F[T]`) | cannot run here — needs a `go 1.18`+ module |
| 8 | cgo or `go:embed` | cannot run here |
| 9 | a genuinely large file | **check the line count**, not just the name |

A zero is not a failure; it is a gap in your coverage, and it should be recorded as one. Either
find a second repository that has the construct, or say in your report that the case went
unexercised. Recording it as "pass" because the case ran is how a green report covers a hole.

This is not hypothetical. Run against `duf`, the preflight reads:

```
build tags : 12
generics   : 0        <- case 6 cannot run; duf is go 1.15
cgo        : 0        <- case 8 cannot run
go:embed   : 0        <- case 8 cannot run
largest    : ./filesystems_linux.go   (301 lines — too small for case 9)
```

so a duf run exercises two of the four, and the other two have to come from somewhere else.

### Where to find each construct

Recorded from a survey of the repositories available on one machine, since the cases that matter
most are the ones a single target cannot reach:

| Case | duf | Use instead |
|---|---|---|
| 4 — build tags | yes | — |
| 6 — generics | no | **nothing available.** No repository on that machine declares generics; golens's only `type Stack[T any]` is fixture text inside a raw string. Case 6 stays unexercised until a generics-using module is available, and that should be reported rather than assumed |
| 8 — cgo / `go:embed` | no | `GoDiabolicalDecorations` has a `go:embed` file (`tools/gen-font-bitmap/main.go`) |
| 9 — large file | 301 lines, too small | `CubeRL` — `internal/game/game.go` is 1,383 lines / 37 KB |

Running case 8 against GoDiabolicalDecorations and case 9 against CubeRL: both pass. The embed
file parses cleanly and `go build` agrees; the 37 KB file outlines in under 10 ms against a 5 ms
per-file parse budget, and `Game`'s 455-byte span is byte-exact against `get`.

### k9s — the strongest single target so far

| Construct | duf | GoDiabolicalDecorations | CubeRL | **k9s** |
|---|---|---|---|---|
| build tags | 12 | 0 | 1 | 1 |
| generics | 0 | 0 | 0 | **4** |
| cgo / `go:embed` | 0 | 1 | 0 | **2** |
| large file | 301 lines | — | 1,383 | 826 |
| `.go` files | ~30 | — | — | **524** |

A full 18-case run against `~/Development/temporary/k9s` (go 1.26.8) passed every case. It is the
best target in one respect this survey does not measure: it has **305 method names declared on more
than one type**, so case 12 is exercised for real rather than assumed — `get ActiveContext .` exits
2 and lists five candidates. It has no platform-variant files, so case 13 still needs duf.

Two results worth keeping:

- **Case 11 over 524 files: the two renderings agree exactly** — 9,159 declarations at depth 1 and
  14,139 at depth 2, human bullets against JSON nodes. This is the check that found the `--depth 1`
  defect, and it holds at a scale the fixtures never reached.
- **Case 13: zero diagnostics on 524 files that build clean**, with a positive control confirming
  the check still fires. Zero false positives is the direction that matters.

One file, `internal/tchart/gauge_test.go`, is entirely commented out and yields no `nodes` key in
the JSON payload. Both renderings agree it has no declarations — `omitempty` on `nodes` is correct,
and this is the A12 "file with only comments" class. A comparison script that assumes the key
exists will misreport it; tolerate an absent `nodes`.

---

## 1. Smoke test

```console
$ /tmp/golens outline main.go
```

Look for: `file:`, `package:`, and sections for IMPORTS, TYPES & STRUCTS, FUNCTIONS & METHODS.
The counts should roughly match what you see in the file.

Before anything else, the two calls a caller tries first:

```console
$ /tmp/golens --version
$ /tmp/golens --help | head -5
```

Look for: a version string, and a root help that names all four commands with the exit codes.
`--version` is what an agent reaches for before reading anything, so an empty or error response
here is the first sign of a broken build.

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

Expect the totals to differ by a small number: `grep` also matches declarations inside `/* */`
comments, which golens correctly ignores. **Resolve the difference rather than eyeballing it:**

```console
$ grep -rh --include='*.go' '^func ' . \
    | sed -E 's/^func (\([^)]*\) )?([A-Za-z0-9_]+).*/\2/' | sort > /tmp/grep-funcs.txt
$ /tmp/golens outline . | grep '• func ' \
    | sed -E 's/.*• func (\([^)]*\) )?([A-Za-z0-9_]+).*/\2/' | sort > /tmp/golens-funcs.txt
$ comm -23 /tmp/grep-funcs.txt /tmp/golens-funcs.txt   # grep saw, golens did not
$ comm -13 /tmp/grep-funcs.txt /tmp/golens-funcs.txt   # golens saw, grep did not
```

The first list should hold only functions commented out in the source — grep them and confirm.
The second must be **empty**: anything in it is a declaration golens found that a plain `grep`
missed, which would be a real traversal problem. On `duf` the first list is exactly one entry,
`isLocalFs`, which sits inside a `/* */` block.

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

**Check the ordering with `LC_ALL=C`.** golens sorts by byte value. A shell `sort` under a UTF-8
locale uses collation, which weights punctuation differently and will interleave the names:

```console
$ /tmp/golens outline . | grep '^file: ' | sed 's/^file: //' > /tmp/order.txt
$ LC_ALL=C sort -c /tmp/order.txt && echo "byte order, as specified"
```

On a tree containing both `filesystems.go` and `filesystems_darwin.go`, a locale-aware `sort -c`
reports "disorder" on a perfectly correct listing, because `.` and `_` are not compared by byte.
Without `LC_ALL=C` this case reads as a traversal bug when it is not one.

Failure: a file appears twice, a file you know about is absent, or the order fails the
`LC_ALL=C` check above.

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

**Do the splice programmatically at least once, in binary.** An editor applies its own encoding and
cannot demonstrate the failure below; a script can. This is the only step in this document that
exercises the offsets the way an agent actually uses them:

```console
$ python3 - <<'EOF'
import json, subprocess
b = json.loads(subprocess.run(["/tmp/golens","bounds","SYMBOL","/tmp/test.go","--json"],
                              capture_output=True, text=True).stdout)
raw = open("/tmp/test.go","rb").read()                    # bytes, not str
raw = raw[:b["start_byte"]] + b"NEW" + raw[b["end_byte"]:]
open("/tmp/test.go","wb").write(raw)
EOF
```

Look for: the file still parses, and the replacement landed inside the declaration.

**Case 16 does the same splice on a file with a non-ASCII identifier, and then does it wrongly.**
Do that one too — the failure is silent, and it does not reproduce on every layout, so passing
here proves less than it looks like it does.

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

`get --bounds-only` is the form a scripted edit wants, and it must be indistinguishable from
`bounds`. Check the payloads are byte-identical rather than merely similar:

```console
$ /tmp/golens get --bounds-only <Symbol> <file.go> --json > /tmp/a.json
$ /tmp/golens bounds <Symbol> <file.go> --json > /tmp/b.json
$ diff /tmp/a.json /tmp/b.json && echo "IDENTICAL payloads"
```

Without `--bounds-only`, `get` adds a `source` field; with it, nothing else should change. A
difference in the offsets means the two commands disagree about the span, which is the failure
that matters — a caller switching between them must not get a different edit location.

**Then check the JSON outline carries the whole declaration head.** This is the case that a
human-only review misses: the human form and the JSON form render the same data by different
code, so each can be correct alone while they disagree. Find a file with a type alias and a
generic type, and compare:

```console
$ /tmp/golens outline <file.go> | grep 'type '
$ /tmp/golens outline <file.go> --json --compact | jq -c '.[] | .nodes[].children[] | select(.kind=="type")'
```

Every `type` in the human output must be reconstructible from the JSON node. An alias and a
defined type over the same underlying type differ **only** in `is_alias`, so if two nodes have
the same `name` and `detail` and neither carries `is_alias`, one of them is an alias the payload
cannot express. `type_params` must appear on generic types and functions; `receiver` must appear
on methods, spelled as the source spells it.

Failure: invalid JSON, stderr text leaking into stdout, or a declaration in the human output that
the JSON node cannot reproduce.

**Then compare the two renderings node for node**, which is what catches a payload that is merely
*plausible* rather than correct:

```console
$ /tmp/golens outline . --depth 1 | grep -c '^  • '                      # human declarations
$ /tmp/golens outline . --depth 1 --json --compact | jq '[..|objects|select(has("kind"))]|length'
```

The two counts must be equal at every depth. A subset check is not enough — a payload that emits
nothing satisfies "every JSON name appears in the human output" trivially, which is how
`--depth 1 --json` shipped group headings with no declarations under them for its whole life.

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

**Check that each diagnostic points at a real line, not line 1.** The position is half the
diagnostic; a reader who follows it lands on the line named. Make a file whose error is nowhere
near the top:

```console
$ mkdir -p /tmp/pos && cat > /tmp/pos/deep.go <<'EOF'
package p

import "fmt"

type T struct {
	A int
}

func F() {
	fmt.Println("ok")
	x := 1 +
}
EOF
$ /tmp/golens check /tmp/pos/deep.go
```

Look for: a line and column past line 5, and the same position `go build` gives. For the file above
both report `12:1` — the offending `}` on line 12, not the incomplete expression on line 11, because
that is where the parser discovers it. A diagnostic reading `deep.go:1:1:` is a defect even when the
message text contains the right position, because the documented `file:line:col:` format is what a
caller parses. Same for an unused import on line 3 of a longer file — if it reports line 1, the
position is being discarded somewhere.

### Cross-check per platform, not just once

`go build` compiles one file per build-tag combination. `golens check` parses **every** `.go`
file in the tree, whatever its constraints. On a repository with platform files that is a
different file set, so a clean `go build` does not clear the diagnostics. Build each platform:

```console
$ for os in linux darwin windows freebsd openbsd; do
      if out=$(GOOS=$os go build ./... 2>&1); then
          echo "GOOS=$os: clean"
      else
          echo "GOOS=$os:"; echo "$out" | head -3
      fi
  done
```

The `if` is load-bearing. Writing `go build ./... 2>&1 | head -3` followed by `echo $?` reads
**`head`'s** exit status, which is always 0, so the loop reports every platform as clean no
matter what the compiler said. Capture into a variable so `$?` belongs to the build.

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

## 14. `check` stays out of style

`golens` reports structural faults and nothing else. This case proves the boundary holds, which
matters because a style check that returns unasked is the kind of thing an agent acts on.

Write a file that is valid Go and badly named:

```console
$ cat > /tmp/styled.go <<'EOF'
package p

type HttpHandler struct{}
type DbUrl struct{}

func UserId() {}
EOF
$ /tmp/golens check /tmp/styled.go 2>&1; echo "exit=$?"
```

Look for: **no output, exit 0.** Every identifier there violates Go's initialism convention and
golens must say nothing about it.

```console
$ /tmp/golens check /tmp/styled.go --naming; echo "exit=$?"
```

Look for: `unknown flag: --naming`, exit 1. The flag is gone rather than merely ignored.

Confirm the structural checks still fire on the same file — this is the other half, because a tool
that has given up on style should not have given up on parsing:

```console
$ cat > /tmp/broken.go <<'EOF'
package p

import "fmt"

func F( {
EOF
$ /tmp/golens check /tmp/broken.go 2>&1; echo "exit=$?"
```

Look for: a parse diagnostic on stderr and exit 3.

Failure: any style diagnostic from `check`, or a structural diagnostic going missing.

Note the `2>&1` throughout: `check` writes diagnostics to stderr, so a pipe without it silently
sees nothing and the raw lines scroll past unfiltered.

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

## 16. Offsets are bytes, not characters

Case 10 splices correctly. This one deliberately splices **wrongly**, because the failure it
produces is the one that destroys files: no exception, no warning, and a parse error pointing
somewhere other than the cause.

`start_byte` and `end_byte` count bytes. Indexing a *decoded string* with those numbers counts
characters, and the two numbering schemes diverge as soon as the file contains any multi-byte
character — including one in a comment or a string literal, which is the common case and has
nothing to do with naming.

```console
$ mkdir -p /tmp/enc
$ cat > /tmp/enc/e.go <<'EOF'
package enc

import "strings"

// Box drawing: ╭ ╮ ╯
func first() string { return strings.Join([]string{"╭", "╮", "╯"}, "") }
func second() {}
EOF
$ printf 'module enc\n\ngo 1.27\n' > /tmp/enc/go.mod
$ python3 -c "raw=open('/tmp/enc/e.go','rb').read(); print(len(raw),'bytes',len(raw.decode()),'chars')"
$ /tmp/golens bounds second /tmp/enc/e.go --json
```

155 bytes, 143 characters — six multi-byte characters, so twelve bytes of drift. Now splice
`second` the way case 10 does, **in binary**:

```console
$ python3 - <<'EOF'
import json, subprocess
b = json.loads(subprocess.run(["/tmp/golens","bounds","second","/tmp/enc/e.go","--json"],
                              capture_output=True, text=True).stdout)
raw = open("/tmp/enc/e.go", "rb").read()
open("/tmp/enc/e.go","wb").write(raw[:b["start_byte"]] + b"func Renamed() {}" + raw[b["end_byte"]:])
EOF
$ (cd /tmp/enc && go build ./... && echo intact)
```

Look for: `intact`.

Now the same splice through a decoded string, using the **same numbers**:

```console
$ cat > /tmp/enc/e.go <<'EOF'
package enc

import "strings"

// Box drawing: ╭ ╮ ╯
func first() string { return strings.Join([]string{"╭", "╮", "╯"}, "") }
func second() {}
EOF
$ python3 - <<'EOF'
import json, subprocess
b = json.loads(subprocess.run(["/tmp/golens","bounds","second","/tmp/enc/e.go","--json"],
                              capture_output=True, text=True).stdout)
text = open("/tmp/enc/e.go", encoding="utf-8").read()      # str, not bytes
open("/tmp/enc/e.go","w",encoding="utf-8").write(
    text[:b["start_byte"]] + "func Renamed() {}" + text[b["end_byte"]:])
EOF
$ tail -1 /tmp/enc/e.go
$ (cd /tmp/enc && go build ./...)
```

Look for:

```
func second(func Renamed() {}

# enc
./e.go:7:18: syntax error: unexpected name Renamed, expected (
```

Note *where* the damage lands. `second`'s own span is pure ASCII, so nothing is wrong with it.
The slice simply starts twelve bytes too late, cutting through `func second(` and leaving its
head behind. The overrun from the earlier multi-byte characters is spent before the span even
begins.

That is the whole hazard in one run. Python raised nothing, the write succeeded, and the error
names line 7 column 18 — nowhere near the decision that caused it.

**Do not file the second run as a golens defect.** The offsets were correct; the consumer applied
them to the wrong unit. Counting bytes is the fix.

**On some files the mistake is invisible, which is worse.** Take the last declaration in a file
whose multi-byte characters come earlier, and the slice starts late and ends past the end. When
that declaration is the last thing in the file there is nothing left to damage, and the only
difference between a correct and an incorrect splice may be the file's trailing newline — a
difference no check in this plan looks for. Verified against `duf`: splicing
`style.go`'s only function wrongly produced a file that parses, compiles, and differs from the
correct result by one byte.

So a clean run of the wrong approach proves nothing about the approach. Run the splice
programmatically on a file you know is affected.

The same trap with a different offset: Python's `utf-8-sig` decode *strips* a BOM, shifting every
index by three more.

☐

## 17. Implicit filename constraints

Go applies two independent constraints to a file: the `//go:build` line, and the file's **name**.
`syscall/env_windows.go` has no `//go:build` line at all — it is Windows-only purely because of
the `_windows` suffix. A checker that reads only the comment will treat such a file as built
everywhere and collide it with every other declaration of the same name.

This is the highest-consequence case in the plan, because the failure is a false positive on
code that compiles. Make the two files:

```console
$ mkdir -p /tmp/impl && printf 'module impl\n\ngo 1.27\n' > /tmp/impl/go.mod
$ printf 'package p\n\nfunc F() {}\n' > /tmp/impl/a_windows.go
$ printf 'package p\n\nfunc F() {}\n' > /tmp/impl/a_linux.go
$ head -1 /tmp/impl/a_windows.go          # no //go:build: the name is the constraint
$ /tmp/golens check /tmp/impl; echo "exit=$?"
$ (cd /tmp/impl && go build ./... && echo "go build: clean")
```

Look for: **`exit=0` and no diagnostics.** `go build` accepts both files; golens must too.

Then the same pair *with* explicit constraints, which is the case the earlier build-tag fix
covered and which must keep working:

```console
$ printf '//go:build windows\n\npackage p\n\nfunc F() {}\n' > /tmp/impl/a_windows.go
$ printf '//go:build linux\n\npackage p\n\nfunc F() {}\n' > /tmp/impl/a_linux.go
$ /tmp/golens check /tmp/impl; echo "exit=$?"
```

Look for: `exit=0`.

And the positive control, two genuinely co-buildable files:

```console
$ printf 'package p\n\nfunc F() {}\n' > /tmp/impl/a_windows.go
$ printf 'package p\n\nfunc F() {}\n' > /tmp/impl/a_linux.go
$ mv /tmp/impl/a_windows.go /tmp/impl/one.go && mv /tmp/impl/a_linux.go /tmp/impl/two.go
$ /tmp/golens check /tmp/impl; echo "exit=$?"
```

Look for: **exit 3**, naming `F`. Two untagged files really do collide.

On a real tree this case has far more reach than anything else here. Measured against the Go
standard library:

| Package | `golens check` | `go build` |
|---|---|---|
| `slices`, `maps`, `cmp` | 0 | clean |
| `mime`, `net/url`, `crypto/tls`, `runtime/pprof` | 0 | clean |
| `net`, `os`, `internal/poll` | 0 | clean |
| `runtime` | 0 | clean |
| `syscall` | 0 | clean |

Before the fix those same packages reported 2, 10, 321, 5,147 and 61,287 redeclarations
respectively — 70,854 across the tree, every one of them a false positive on code that compiles.
If a single package in a small repository reports redeclarations that `go build` calls clean,
this is the cause.

The fix has three parts, and each is separately load-bearing. Probe them separately, because a
pass here can be vacuous:

```console
$ /tmp/golens check $GOROOT/src 2>&1 | grep -v '/testdata/' | wc -l   # 0
```

- **Filename suffixes.** `_windows`, `_amd64`, `_linux_amd64`. The name is read from the first
  underscore onward: `os/zero_copy_linux.go` is linux-only and `runtime/os_only_solaris.go` is
  solaris-only, despite the package name prefix.
- **A real GOOS × GOARCH matrix.** `go tool dist list` gives 47 pairs; a cross product gives 210.
  An invented pair such as `freebsd/mipsle` makes `//go:build mips || mipsle` and
  `//go:build freebsd` both hold, which is a false positive.
- **cgo.** A file importing `"C"` is excluded when cgo is disabled, whatever its name and tags
  say. `crypto/internal/sysrand/internal/seccomp` is the stdlib's own instance of this.

**Preflight.** This case only exercises those paths if the target actually has platform variants
and cgo files. Check before recording it:

```console
$ find $GOROOT/src -name '*_linux.go' | wc -l      # non-zero: filename suffixes matter
$ grep -rl 'import "C"' $GOROOT/src --include='*.go' | wc -l
```

A zero is not a pass — it is a gap in coverage. Find another repository or record the case as
unexercised.

☐

## 18. A tree containing one unparseable file

A file that does not parse is **left out of the outline, named on stderr, and the run still exits
4** — but every other file is outlined first. `check` behaves the same way for the same reason: a
parse failure is one finding among many, never a reason to stop looking.

Ordering matters here. stdout carries everything that parsed, stderr carries the failure. A caller
that checks `$?` learns the answer is partial; a caller that only reads stdout still gets every
file that could be read. The reverse — exit 0, silent — would be a lie.

The standard library is the natural subject: it holds 8,088 `.go` files, and its `testdata`
trees contain compiler fixtures written to be unparseable.

```console
$ GOROOT=$(go env GOROOT)
$ find $GOROOT/src -name '*.go' | wc -l                          # 8088
$ /tmp/golens outline $GOROOT/src | grep -c '^file:'             # thousands, not zero
$ /tmp/golens outline $GOROOT/src > /dev/null; echo "exit=$?"    # 4
$ /tmp/golens check    $GOROOT/src > /dev/null; echo "exit=$?"    # 3
```

Look for: **`outline` exits 4 with a populated stdout**, and stderr naming the skipped files and
how complete the answer is:

```text
golens: 45 of 7134 files could not be parsed and are missing from the outline: cmd/compile/...
```

The count is exact; the enumeration is capped at five entries followed by "and N more".

Then the machine-consumer case, which is where a truncated payload would actually bite:

```console
$ /tmp/golens outline $GOROOT/src --json --compact 2>/dev/null | python3 -c 'import json,sys; print(len(json.load(sys.stdin)))'
```

Look for: **a count, not a JSON parse error.** stdout must remain valid JSON when files are
skipped, or `golens outline . --json | jq` breaks on exactly the repositories where the agent most
needs it.

And the degenerate case, one broken file with nothing else to salvage — the message should be the
bare diagnostic, not "1 of 1 files could not be parsed":

```console
$ printf 'package p\n\nfunc f( {\n' > /tmp/one_broken.go
$ /tmp/golens outline /tmp/one_broken.go; echo "exit=$?"
```

Look for: `exit=4` and a message that is the parse error itself.

Note that `golens` does **not** skip `testdata` directories, which is what puts the broken fixtures
in scope. That is deliberate — an agent asking what a directory contains wants all of it — but it
means the skipped-file count on a repository like the standard library will never be zero.

☐

---

## If time is short

Run **10, 17, 16, 13, 6, 9** in that order.

| Case | Why it ranks here |
|---|---|
| 10 | Offsets that round-trip but address the wrong span silently corrupt files |
| 17 | Implicit filename constraints. 70,854 false positives on the standard library; every cross-platform package is affected |
| 16 | Byte offsets applied to characters. Triggers on ordinary multi-byte content, and on some files the mistake is completely invisible |
| 18 | A partial outline with a non-zero exit. Silent truncation corrupts an agent's map of the repository |
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
| JSON outline node | Carries `type_params`, `receiver` and `is_alias`; an alias is distinguishable from a defined type |
| `--compact` | Removes decoration, never information |
| Out of scope | Unreachable code, type-level conflicts, module import resolution, non-Go languages |
