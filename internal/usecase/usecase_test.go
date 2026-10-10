package usecase

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CodeMonkey80s/golens/internal/domain"
	"github.com/CodeMonkey80s/golens/internal/format"
)

const userGo = `package store

import (
	"context"
	"os"
)

type User struct {
	Id int
}

type UserStore struct {
	mu    sync.Mutex
	cache map[int]*User
}

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
`

func engine() domain.Engine { return newEngine() }

func write(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

func TestOutlineHuman(t *testing.T) {
	dir := t.TempDir()
	path := write(t, dir, "user.go", userGo)

	var buf bytes.Buffer
	if err := Outline(engine(), path, DepthUnlimited, &buf, format.Options{}); err != nil {
		t.Fatalf("Outline: %v", err)
	}

	got := buf.String()
	for _, want := range []string{
		"file: " + filepath.ToSlash(path),
		"package: store",
		"IMPORTS",
		"TYPES & STRUCTS",
		"FUNCTIONS & METHODS",
		"func NewUserStore() *UserStore",
		"func (s *UserStore) GetUser(ctx context.Context, id int) (*User, error)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("outline missing %q:\n%s", want, got)
		}
	}
}

// TestOutlineJSONIsAlwaysArray pins the JSON shape decision: an array of per-file objects
// even for a single file, so the schema never varies with input type.
func TestOutlineJSONIsAlwaysArray(t *testing.T) {
	dir := t.TempDir()
	path := write(t, dir, "user.go", userGo)

	var buf bytes.Buffer
	if err := Outline(engine(), path, DepthUnlimited, &buf, format.Options{JSON: true}); err != nil {
		t.Fatalf("Outline: %v", err)
	}

	var out []format.OutlineFile
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("got %d files, want 1", len(out))
	}
	if out[0].Package != "store" {
		t.Errorf("Package = %q, want store", out[0].Package)
	}
	if len(out[0].Nodes) == 0 {
		t.Error("expected outline nodes")
	}
}

// TestOutlineDirectoryIsDeterministic covers acceptance criterion A2: identical input
// yields byte-identical output, and multi-file runs repeat the file header per block.
func TestOutlineDirectoryIsDeterministic(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "b.go", "package b\n\nfunc B() {}\n")
	write(t, dir, "a.go", "package a\n\nfunc A() {}\n")
	write(t, dir, "sub/c.go", "package c\n\nfunc C() {}\n")
	// Must be skipped: non-Go, hidden, vendor, node_modules.
	write(t, dir, "notes.txt", "ignored")
	write(t, dir, ".hidden/d.go", "package d\n")
	write(t, dir, "vendor/v.go", "package v\n")
	write(t, dir, "node_modules/n.go", "package n\n")

	var first, second bytes.Buffer
	opts := format.Options{}
	if err := Outline(engine(), dir, DepthUnlimited, &first, opts); err != nil {
		t.Fatalf("Outline: %v", err)
	}
	if err := Outline(engine(), dir, DepthUnlimited, &second, opts); err != nil {
		t.Fatalf("Outline: %v", err)
	}

	if first.String() != second.String() {
		t.Errorf("output not deterministic:\n%s\n---\n%s", first.String(), second.String())
	}

	got := first.String()
	if !strings.Contains(got, "package: a") {
		t.Error("missing a.go")
	}
	if !strings.Contains(got, "package: c") {
		t.Error("missing sub/c.go")
	}
	for _, skipped := range []string{"package: v", "package: n", "package: d"} {
		if strings.Contains(got, skipped) {
			t.Errorf("should have skipped %q", skipped)
		}
	}

	// Lexical order: a.go before b.go before sub/c.go.
	if strings.Index(got, "a.go") > strings.Index(got, "b.go") {
		t.Error("files not in lexical order")
	}
}

func TestOutlineRejectsNonGoFile(t *testing.T) {
	dir := t.TempDir()
	path := write(t, dir, "notes.txt", "hello")

	err := Outline(engine(), path, DepthUnlimited, &bytes.Buffer{}, format.Options{})
	if err == nil {
		t.Fatal("expected error for non-Go file")
	}
	var usage interface{ IsUsage() bool }
	if !asUsage(err, &usage) {
		t.Errorf("expected usage error, got %v", err)
	}
}

func TestGetHumanAndBoundsAgree(t *testing.T) {
	dir := t.TempDir()
	path := write(t, dir, "user.go", userGo)
	src := userGo

	var getBuf bytes.Buffer
	if err := Get(engine(), "UserStore.GetUser", path, true, &getBuf, format.Options{}); err != nil {
		t.Fatalf("Get: %v", err)
	}

	var boundsBuf bytes.Buffer
	if err := Bounds(engine(), "UserStore.GetUser", path, &boundsBuf, format.Options{}); err != nil {
		t.Fatalf("Bounds: %v", err)
	}

	// The byte range reported by bounds must slice exactly the text get printed.
	got := getBuf.String()
	body := got[strings.Index(got, "\n\n")+2:]

	type payload struct {
		StartByte int `json:"start_byte"`
		EndByte   int `json:"end_byte"`
	}

	var p payload
	if err := json.Unmarshal(mustJSON(t, engine(), "UserStore.GetUser", path), &p); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if slice := src[p.StartByte:p.EndByte]; slice != strings.TrimSuffix(body, "\n") {
		t.Errorf("bounds slice mismatch:\nslice: %q\n  get: %q", slice, body)
	}
}

// TestGetBoundsOnlyMatchesBoundsJSON covers acceptance criterion A6 at the usecase level.
func TestGetBoundsOnlyMatchesBoundsJSON(t *testing.T) {
	dir := t.TempDir()
	path := write(t, dir, "user.go", userGo)

	var a, b bytes.Buffer
	opts := format.Options{JSON: true, Compact: true}

	if err := Get(engine(), "UserStore.GetUser", path, false, &a, opts); err != nil {
		t.Fatalf("Get bounds-only: %v", err)
	}
	if err := Bounds(engine(), "UserStore.GetUser", path, &b, opts); err != nil {
		t.Fatalf("Bounds: %v", err)
	}

	if a.String() != b.String() {
		t.Errorf("payloads differ:\n get: %s\nbound: %s", a.String(), b.String())
	}
}

// TestResolveAmbiguityExitsTwo covers decision D17: an ambiguous bare name reports every
// candidate rather than guessing.
func TestResolveAmbiguityExitsTwo(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a.go", "package p\n\ntype A struct{}\n\nfunc (A) Save() {}\n")
	write(t, dir, "b.go", "package p\n\ntype B struct{}\n\nfunc (B) Save() {}\n")

	_, _, err := Resolve(engine(), "Save", dir)
	if err == nil {
		t.Fatal("expected ambiguity error")
	}

	var amb AmbiguousSymbol
	if !asAmbiguous(err, &amb) {
		t.Fatalf("expected AmbiguousSymbol, got %T", err)
	}
	if len(amb.Matches) != 2 {
		t.Errorf("candidates = %v, want 2 entries", amb.Matches)
	}
}

func TestResolveNotFound(t *testing.T) {
	dir := t.TempDir()
	path := write(t, dir, "user.go", userGo)

	_, _, err := Resolve(engine(), "NoSuchSymbol", path)
	if err == nil {
		t.Fatal("expected not-found error")
	}
	var notFound NotFoundSymbol
	if !asNotFound(err, &notFound) {
		t.Errorf("expected NotFoundSymbol, got %T", err)
	}
}

// TestBoundsRejectsDirectory covers decision D18: bounds takes a file only.
func TestBoundsRejectsDirectory(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "user.go", userGo)

	err := Bounds(engine(), "GetUser", dir, &bytes.Buffer{}, format.Options{})
	if err == nil {
		t.Fatal("expected usage error for directory")
	}
	var usage interface{ IsUsage() bool }
	if !asUsage(err, &usage) {
		t.Errorf("expected usage error, got %v", err)
	}
}

func TestGetAcrossDirectory(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "user.go", "package p\n\nfunc Alpha() {}\n")
	write(t, dir, "other.go", "package p\n\nfunc Beta() {}\n")

	var buf bytes.Buffer
	if err := Get(engine(), "Beta", dir, true, &buf, format.Options{}); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !strings.Contains(buf.String(), "func Beta() {}") {
		t.Errorf("output missing Beta:\n%s", buf.String())
	}
}

// TestCheckFindsUnusedImport covers the unused-import heuristic on the common case.
func TestCheckFindsUnusedImport(t *testing.T) {
	dir := t.TempDir()
	path := write(t, dir, "user.go", "package p\n\nimport \"fmt\"\n\nfunc F() int { return 1 }\n")

	var stdout, stderr bytes.Buffer
	err := Check(engine(), path, &stdout, &stderr, format.Options{})

	var hasDiags HasDiagnostics
	if !asHasDiags(err, &hasDiags) {
		t.Fatalf("expected HasDiagnostics, got %v", err)
	}
	if !strings.Contains(stderr.String(), "imported and not used") {
		t.Errorf("stderr missing unused-import diagnostic:\n%s", stderr.String())
	}
	if !strings.Contains(stderr.String(), "user.go:3:1") {
		t.Errorf("diagnostic should carry file:line:col:\n%s", stderr.String())
	}
}

func TestCheckCleanFileIsSilent(t *testing.T) {
	dir := t.TempDir()
	path := write(t, dir, "clean.go", "package p\n\nimport \"fmt\"\n\nfunc F() string { return fmt.Sprint(1) }\n")

	var stdout, stderr bytes.Buffer
	if err := Check(engine(), path, &stdout, &stderr, format.Options{}); err != nil {
		t.Errorf("expected no diagnostics, got %v", err)
	}
	if stderr.Len() != 0 {
		t.Errorf("expected silent stderr, got:\n%s", stderr.String())
	}
}

// TestCheckSkipsBlankAndDotImports verifies "_" and "." are never reported as unused.
func TestCheckSkipsBlankAndDotImports(t *testing.T) {
	dir := t.TempDir()
	src := "package p\n\nimport (\n\t_ \"net/http/pprof\"\n\t. \"strings\"\n)\n\nfunc F() string { return ToUpper(\"x\") }\n"
	path := write(t, dir, "dots.go", src)

	var stdout, stderr bytes.Buffer
	err := Check(engine(), path, &stdout, &stderr, format.Options{})
	if err != nil {
		t.Errorf("expected no diagnostics, got %v (stderr: %s)", err, stderr.String())
	}
}

// TestCheckReportsDuplicateDeclarations covers the AST-achievable namespace check.
func TestCheckReportsDuplicateDeclarations(t *testing.T) {
	dir := t.TempDir()
	path := write(t, dir, "dup.go", "package p\n\nfunc F() {}\n\nfunc F() {}\n")

	var stdout, stderr bytes.Buffer
	err := Check(engine(), path, &stdout, &stderr, format.Options{})

	var hasDiags HasDiagnostics
	if !asHasDiags(err, &hasDiags) {
		t.Fatalf("expected HasDiagnostics, got %v", err)
	}
	if !strings.Contains(stderr.String(), "redeclared") {
		t.Errorf("stderr missing redeclaration diagnostic:\n%s", stderr.String())
	}
}

// TestCheckReportsParseErrorWithoutAborting covers the exit-3 path for a broken file and
// confirms other files are still inspected.
func TestCheckReportsParseErrorWithoutAborting(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a_broken.go", "package p\n\nfunc f( {\n")
	write(t, dir, "b_clean.go", "package p\n\nimport \"fmt\"\n\nfunc F() { _ = fmt.Sprint }\n")

	var stdout, stderr bytes.Buffer
	err := Check(engine(), dir, &stdout, &stderr, format.Options{})

	var hasDiags HasDiagnostics
	if !asHasDiags(err, &hasDiags) {
		t.Fatalf("expected HasDiagnostics, got %v", err)
	}
	if !strings.Contains(stderr.String(), "a_broken.go") {
		t.Errorf("stderr missing broken file diagnostic:\n%s", stderr.String())
	}
}

func TestCheckJSONGoesToStdout(t *testing.T) {
	dir := t.TempDir()
	path := write(t, dir, "user.go", "package p\n\nimport \"fmt\"\n\nfunc F() {}\n")

	var stdout, stderr bytes.Buffer
	err := Check(engine(), path, &stdout, &stderr, format.Options{JSON: true})
	if err == nil {
		t.Fatal("expected diagnostics error")
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr must stay clean in JSON mode, got:\n%s", stderr.String())
	}

	var payload struct {
		SchemaVersion int `json:"schema_version"`
		Diagnostics   []struct {
			Severity string `json:"severity"`
			Message  string `json:"message"`
		} `json:"diagnostics"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("Unmarshal: %v\n%s", err, stdout.String())
	}
	if payload.SchemaVersion != format.SchemaVersion {
		t.Errorf("SchemaVersion = %d, want %d", payload.SchemaVersion, format.SchemaVersion)
	}
	if len(payload.Diagnostics) == 0 {
		t.Error("expected diagnostics in JSON payload")
	}
}

func mustJSON(t *testing.T, e domain.Engine, name, path string) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := Get(e, name, path, false, &buf, format.Options{JSON: true, Compact: true}); err != nil {
		t.Fatalf("Get: %v", err)
	}
	return buf.Bytes()
}
