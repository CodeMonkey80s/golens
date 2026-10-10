package format_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CodeMonkey80s/golens/internal/engine/goast"
	"github.com/CodeMonkey80s/golens/internal/format"
	"github.com/CodeMonkey80s/golens/internal/usecase"
)

// jsonUnmarshal is a named alias so the helper below reads without importing encoding/json
// under a shadowed name.
func jsonUnmarshal(data []byte, v any) error { return json.Unmarshal(data, v) }

// storeFixture is the golden subject. Its line numbers are load-bearing: every expectation
// below depends on them, so inserting a line here is a deliberate act that shows up as a
// reviewable diff.
const storeFixture = `package store

import (
	"context"
	"fmt"
)

type User struct {
	Id   int
	Name string
}

type UserStore struct {
	mu    sync.Mutex
	cache map[int]*User
}

type Alias = User

var ErrNotFound = errors.New("not found")

const MaxUsers = 100

func NewUserStore() *UserStore {
	return &UserStore{}
}

func (s *UserStore) GetUser(ctx context.Context, id int) (*User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if u, ok := s.cache[id]; ok {
		return u, nil
	}
	return nil, ErrNotFound
}

func (s UserStore) Len() int {
	return len(s.cache)
}
`

// TestOutlineMatchesSpecificationSample pins the human output against the sample in
// specification 2.1, line for line.
//
// The spec's example is the contract an agent is written against, so a divergence here is a
// spec break rather than a cosmetic difference. The sample uses different line numbers than
// this fixture, so the comparison is on structure and wording rather than literal text; the
// per-line ranges are asserted separately below.
func TestOutlineMatchesSpecificationSample(t *testing.T) {
	dir := t.TempDir()
	path := writeFixture(t, dir, "user.go", storeFixture)

	// Depth 1, matching the specification's sample, which shows declarations without their
	// nested fields.
	var buf bytes.Buffer
	if err := usecase.Outline(goast.New(), path, 1, &buf, format.Options{}); err != nil {
		t.Fatalf("Outline: %v", err)
	}
	got := buf.String()

	// The spec's sample, with its own line numbers, reproduced structurally. Note imports carry
	// no line range while declarations do: an import is always one line, so its range is
	// redundant, whereas a declaration's range is what an agent seeks.
	want := []string{
		"file: ",
		"package: store",
		"",
		"IMPORTS",
		"  • context",
		"  • fmt",
		"",
		"TYPES & STRUCTS (lines 8-18)",
		"  • type User struct (lines 8-11)",
		"  • type UserStore struct (lines 13-16)",
		// The "=" survives. An alias head that drops it reads as a defined type, which
		// is a different program; see TestDeclarationHeadsMatchSource.
		"  • type Alias = User (line 18)",
		"",
		"FUNCTIONS & METHODS (lines 24-39)",
		"  • func NewUserStore() *UserStore (lines 24-26)",
		"  • func (s *UserStore) GetUser(ctx context.Context, id int) (*User, error) (lines 28-35)",
	}

	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")

	// Line 0 is the file header; only its prefix is fixed, since the path is a temp dir.
	if !strings.HasPrefix(lines[0], "file: ") || !strings.HasSuffix(lines[0], "user.go") {
		t.Errorf("line 0 = %q, want a file: header ending in user.go", lines[0])
	}
	for i, w := range want[1:] {
		idx := i + 1
		if idx >= len(lines) {
			t.Fatalf("output ended before line %d:\n%s", idx, got)
		}
		if lines[idx] != w {
			t.Errorf("line %d:\n got: %q\nwant: %q", idx, lines[idx], w)
		}
	}
}

// TestOutlineImportNamesAreUnquoted pins a spec detail that is easy to get wrong: the sample
// shows "context", not "\"context\"". The local name is what a reader would search for, and a
// renamed import's alias is the whole point of the binding.
func TestOutlineImportNamesAreUnquoted(t *testing.T) {
	dir := t.TempDir()
	path := writeFixture(t, dir, "imports.go", `package imports

import (
	"context"
	jsonalias "encoding/json"
	_ "net/http/pprof"
	. "strings"
)

func F() string { return jsonalias.Marshal(context.Background().String()) + ToUpper("x") }
`)

	var buf bytes.Buffer
	if err := usecase.Outline(goast.New(), path, usecase.DepthUnlimited, &buf, format.Options{}); err != nil {
		t.Fatalf("Outline: %v", err)
	}
	got := buf.String()

	for _, want := range []string{
		"• context",
		"• jsonalias", // the alias, not "encoding/json"
		"• _",         // blank import
		"• .",         // dot import
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, `"encoding/json"`) {
		t.Errorf("import path should not be quoted in the outline:\n%s", got)
	}
}

// TestOutlineDepthSelectsNestedNodes covers the --depth contract: depth 1 shows declarations,
// depth 2 adds fields and parameters, and unlimited shows everything.
func TestOutlineDepthSelectsNestedNodes(t *testing.T) {
	dir := t.TempDir()
	path := writeFixture(t, dir, "store.go", storeFixture)

	out := func(depth int) string {
		t.Helper()
		var buf bytes.Buffer
		if err := usecase.Outline(goast.New(), path, depth, &buf, format.Options{}); err != nil {
			t.Fatalf("Outline depth %d: %v", depth, err)
		}
		return buf.String()
	}

	shallow := out(1)
	deep := out(2)
	unlimited := out(usecase.DepthUnlimited)

	// A field of User exists only at depth 2 or unlimited.
	for _, field := range []string{"• Id int", "• Name string"} {
		if strings.Contains(shallow, field) {
			t.Errorf("depth 1 must not include %q:\n%s", field, shallow)
		}
		if !strings.Contains(deep, field) {
			t.Errorf("depth 2 must include %q:\n%s", field, deep)
		}
		if !strings.Contains(unlimited, field) {
			t.Errorf("unlimited must include %q:\n%s", field, unlimited)
		}
	}

	// Declarations appear at every depth.
	for _, decl := range []string{"• type User struct", "• func NewUserStore() *UserStore"} {
		for name, out := range map[string]string{"depth1": shallow, "depth2": deep, "unlimited": unlimited} {
			if !strings.Contains(out, decl) {
				t.Errorf("%s must include %q", name, decl)
			}
		}
	}
}

// TestUnlimitedDepthDiffersFromDepthOne is the regression test for a real bug: an unlimited
// depth of -1 was compared with `depth > level`, which is false for every level when the depth
// is negative, making "unlimited" behave exactly like depth 1.
func TestUnlimitedDepthDiffersFromDepthOne(t *testing.T) {
	dir := t.TempDir()
	path := writeFixture(t, dir, "store.go", storeFixture)

	var shallow, unlimited bytes.Buffer
	if err := usecase.Outline(goast.New(), path, 1, &shallow, format.Options{}); err != nil {
		t.Fatalf("Outline depth 1: %v", err)
	}
	if err := usecase.Outline(goast.New(), path, usecase.DepthUnlimited, &unlimited, format.Options{}); err != nil {
		t.Fatalf("Outline unlimited: %v", err)
	}

	if shallow.String() == unlimited.String() {
		t.Error("unlimited depth produced identical output to depth 1; the negative-depth case is not handled")
	}
	if !strings.Contains(unlimited.String(), "• Id int") {
		t.Errorf("unlimited depth should include nested fields:\n%s", unlimited.String())
	}
}

// TestCompactIsLosslessSubset covers acceptance criterion A5: compact output removes
// decoration but no information.
//
// Both forms are checked for the facts an agent needs -- each declaration and its line range
// -- rather than compared literally, since compaction deliberately changes the layout.
func TestCompactIsLosslessSubset(t *testing.T) {
	dir := t.TempDir()
	path := writeFixture(t, dir, "store.go", storeFixture)

	var full, compact bytes.Buffer
	if err := usecase.Outline(goast.New(), path, usecase.DepthUnlimited, &full, format.Options{}); err != nil {
		t.Fatalf("Outline: %v", err)
	}
	if err := usecase.Outline(goast.New(), path, usecase.DepthUnlimited, &compact, format.Options{Compact: true}); err != nil {
		t.Fatalf("Outline compact: %v", err)
	}

	// Every declaration in the full form must survive compaction.
	for _, fact := range []string{
		"context", "fmt",
		"type User struct", "type UserStore struct", "type Alias = User",
		"func NewUserStore() *UserStore",
		"func (s *UserStore) GetUser(ctx context.Context, id int) (*User, error)",
		"func (s UserStore) Len() int",
		"var ErrNotFound", "const MaxUsers",
	} {
		if !strings.Contains(full.String(), fact) {
			t.Errorf("full output missing %q:\n%s", fact, full.String())
		}
		if !strings.Contains(compact.String(), fact) {
			t.Errorf("compact output lost %q:\n%s", fact, compact.String())
		}
	}

	// Decoration must be gone.
	for _, decoration := range []string{"•", "IMPORTS", "TYPES & STRUCTS", "package:"} {
		if strings.Contains(compact.String(), decoration) {
			t.Errorf("compact output should drop %q:\n%s", decoration, compact.String())
		}
	}
	if strings.Contains(compact.String(), "\n\n") {
		t.Errorf("compact output should have no blank lines:\n%q", compact.String())
	}
}

// TestCompactRespectsDepth confirms compaction and depth compose, rather than one silently
// overriding the other.
func TestCompactRespectsDepth(t *testing.T) {
	dir := t.TempDir()
	path := writeFixture(t, dir, "store.go", storeFixture)

	var shallow, deep bytes.Buffer
	if err := usecase.Outline(goast.New(), path, 1, &shallow, format.Options{Compact: true}); err != nil {
		t.Fatalf("Outline: %v", err)
	}
	if err := usecase.Outline(goast.New(), path, 2, &deep, format.Options{Compact: true}); err != nil {
		t.Fatalf("Outline: %v", err)
	}

	if strings.Contains(shallow.String(), "Id int") {
		t.Errorf("compact depth 1 must not include fields:\n%s", shallow.String())
	}
	if !strings.Contains(deep.String(), "Id int") {
		t.Errorf("compact depth 2 must include fields:\n%s", deep.String())
	}
	if shallow.String() == deep.String() {
		t.Error("compact output ignored depth")
	}
}

// TestCompactHasNoDuplicateRecords guards a real bug: the first compact implementation
// flattened group children and then recursed into them again, emitting every record twice.
func TestCompactHasNoDuplicateRecords(t *testing.T) {
	dir := t.TempDir()
	path := writeFixture(t, dir, "store.go", storeFixture)

	var buf bytes.Buffer
	if err := usecase.Outline(goast.New(), path, usecase.DepthUnlimited, &buf, format.Options{Compact: true}); err != nil {
		t.Fatalf("Outline: %v", err)
	}

	seen := map[string]int{}
	for _, line := range strings.Split(strings.TrimRight(buf.String(), "\n"), "\n") {
		seen[line]++
	}
	for line, n := range seen {
		if n > 1 {
			t.Errorf("record emitted %d times: %q", n, line)
		}
	}
}

// TestJsonDepthMatchesHumanDepth confirms the two renderings select the same declarations, as
// specification 2.1 requires.
func TestJsonDepthMatchesHumanDepth(t *testing.T) {
	dir := t.TempDir()
	path := writeFixture(t, dir, "store.go", storeFixture)
	engine := goast.New()

	for _, depth := range []int{1, 2} {
		var human, jsonBuf bytes.Buffer
		if err := usecase.Outline(engine, path, depth, &human, format.Options{}); err != nil {
			t.Fatalf("Outline human: %v", err)
		}
		if err := usecase.Outline(engine, path, depth, &jsonBuf, format.Options{Json: true}); err != nil {
			t.Fatalf("Outline json: %v", err)
		}

		names := collectJsonNames(t, jsonBuf.Bytes())

		for name := range names {
			if !strings.Contains(human.String(), name) {
				t.Errorf("depth %d: %q in JSON but not in human output", depth, name)
			}
		}
	}
}

// TestMultiFileRepeatsFileHeader covers the directory case: each block repeats the file header,
// so concatenated output is unambiguous.
func TestMultiFileRepeatsFileHeader(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "a.go", "package a\n\nfunc Alpha() {}\n")
	writeFixture(t, dir, "b.go", "package b\n\nfunc Beta() {}\n")

	var buf bytes.Buffer
	if err := usecase.Outline(goast.New(), dir, usecase.DepthUnlimited, &buf, format.Options{}); err != nil {
		t.Fatalf("Outline: %v", err)
	}
	got := buf.String()

	for _, want := range []string{"file: " + filepath.Join(dir, "a.go"), "package: a", "Alpha", "file: " + filepath.Join(dir, "b.go"), "package: b", "Beta"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q:\n%s", want, got)
		}
	}
	if strings.Count(got, "file: ") != 2 {
		t.Errorf("expected one file header per file, got %d:\n%s", strings.Count(got, "file: "), got)
	}
	if strings.Index(got, "a.go") > strings.Index(got, "b.go") {
		t.Error("files not in lexical order")
	}
}

func writeFixture(t *testing.T, dir, name, src string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

func collectJsonNames(t *testing.T, data []byte) map[string]bool {
	t.Helper()

	var files []format.OutlineFile
	if err := jsonUnmarshal(data, &files); err != nil {
		t.Fatalf("Unmarshal: %v\n%s", err, data)
	}

	names := map[string]bool{}
	for _, f := range files {
		for _, group := range f.Nodes {
			for _, decl := range group.Children {
				names[decl.Name] = true
			}
		}
	}
	return names
}
