package usecase_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/CodeMonkey80s/golens/internal/engine/goast"
	"github.com/CodeMonkey80s/golens/internal/format"
	"github.com/CodeMonkey80s/golens/internal/testfixture"
	"github.com/CodeMonkey80s/golens/internal/usecase"
)

// --- Adversarial probes for edge cases ---

// TestAdversarialInitPackageQualified verifies that package-qualified lookup
// works on init() functions, which are repeatable names.
func TestAdversarialInitPackageQualified(t *testing.T) {
	dir := t.TempDir()
	testfixture.Write(t, dir, "a.go", "package p\n\nfunc init() {}\nfunc init() {}\nfunc Regular() {}\n")

	// Bare name 'init' is ambiguous (two declarations)
	var out bytes.Buffer
	err := usecase.Get(goast.New(), "init", dir, true, &out, format.Options{})
	if err == nil {
		t.Fatal("expected ambiguous error for bare 'init'")
	}

	// Package-qualified lookup should also be ambiguous (two init in same package)
	out.Reset()
	err = usecase.Get(goast.New(), "p.init", dir, true, &out, format.Options{})
	if err == nil {
		t.Fatal("expected ambiguous error for 'p.init' with two inits")
	}
	var amb usecase.AmbiguousSymbol
	if !errors.As(err, &amb) {
		t.Fatalf("err = %v (%T), want AmbiguousSymbol", err, err)
	}
	if len(amb.Candidates()) != 2 {
		t.Errorf("expected 2 candidates for p.init, got %d: %v", len(amb.Candidates()), amb.Candidates())
	}
}

// TestAdversarialBlankIdentifierPackageQualified verifies blank identifier
// package-qualified lookup.
func TestAdversarialBlankIdentifierPackageQualified(t *testing.T) {
	dir := t.TempDir()
	testfixture.Write(t, dir, "a.go", "package p\n\nvar _ = 42\nvar _ = 43\n")

	// Package-qualified blank identifier should be ambiguous
	var out bytes.Buffer
	err := usecase.Get(goast.New(), "p._", dir, true, &out, format.Options{})
	if err == nil {
		t.Fatal("expected ambiguous error for 'p._' with two blank vars")
	}
	var amb usecase.AmbiguousSymbol
	if !errors.As(err, &amb) {
		t.Fatalf("err = %v (%T), want AmbiguousSymbol", err, err)
	}
}

// TestAdversarialVersionedImportV1PackageNamedV1 tests the edge case where
// a path ends in /v1 but the package is genuinely named "v1" (not a version suffix).
// The derived name "v1" IS the real name here, so if used, no diagnostic.
func TestAdversarialVersionedImportV1PackageNamedV1(t *testing.T) {
	// Package v1 genuinely named "v1", used as selector base
	src := `package p

import "example.com/foo/v1"

func F() { _ = v1.Something() }
`
	res := runCheck(t, t.TempDir(), "p.go", src)
	if res.code != 0 {
		t.Errorf("exit = %d, want 0: import 'v1' used as selector base", res.code)
	}
	for _, d := range res.diags {
		if strings.Contains(d, "imported and not used") {
			t.Errorf("unexpected unused-import diagnostic: %q", d)
		}
	}
}

// TestAdversarialVersionedImportV1PackageNamedOther tests that /v1 where the
// package is NOT named "v1" correctly derives the alternate name.
func TestAdversarialVersionedImportV1PackageNamedOther(t *testing.T) {
	// Path ends in /v1 but package declares "package foo" (element before /v1),
	// used as "foo"
	src := `package p

import "example.com/foo/v1"

func F() { _ = foo.Something() }
`
	res := runCheck(t, t.TempDir(), "p.go", src)
	if res.code != 0 {
		t.Errorf("exit = %d, want 0: import used under alternate name 'foo'", res.code)
	}
	for _, d := range res.diags {
		if strings.Contains(d, "imported and not used") {
			t.Errorf("unexpected unused-import diagnostic: %q", d)
		}
	}
}

// TestAdversarialVersionedImportUnused tests that a versioned import that is
// NOT used is still reported.
func TestAdversarialVersionedImportUnused(t *testing.T) {
	src := `package p

import "example.com/foo/v1"

func F() {}
`
	res := runCheck(t, t.TempDir(), "p.go", src)
	if res.code != 3 {
		t.Errorf("exit = %d, want 3: unused import should be reported", res.code)
	}
	found := false
	for _, d := range res.diags {
		if strings.Contains(d, "imported and not used") {
			found = true
		}
	}
	if !found {
		t.Error("expected unused-import diagnostic for versioned import")
	}
}

// TestAdversarialTypeAliasAndDefinedSameDetail verifies that an alias and a
// defined type over the same underlying type differ only in is_alias.
func TestAdversarialTypeAliasAndDefinedSameDetail(t *testing.T) {
	dir := t.TempDir()
	testfixture.Write(t, dir, "a.go", "package p\n\ntype Alias = int\ntype Defined int\n")

	var out bytes.Buffer
	err := usecase.Outline(goast.New(), dir, usecase.DepthUnlimited, &out, format.Options{JSON: true})
	if err != nil {
		t.Fatalf("Outline: %v", err)
	}

	var payload []map[string]any
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("not valid JSON: %v", err)
	}

	nodes := payload[0]["nodes"].([]any)
	var aliasNode, definedNode map[string]any
	for _, n := range nodes {
		node := n.(map[string]any)
		if node["name"] == "Alias" {
			aliasNode = node
		}
		if node["name"] == "Defined" {
			definedNode = node
		}
	}

	if aliasNode == nil {
		t.Fatal("missing Alias node")
	}
	if definedNode == nil {
		t.Fatal("missing Defined node")
	}

	// Same detail, different is_alias
	if aliasNode["detail"] != definedNode["detail"] {
		t.Errorf("detail mismatch: alias=%v defined=%v", aliasNode["detail"], definedNode["detail"])
	}
	if aliasNode["is_alias"] != true {
		t.Errorf("Alias is_alias = %v, want true", aliasNode["is_alias"])
	}
	if _, ok := definedNode["is_alias"]; ok {
		t.Errorf("Defined should not have is_alias field")
	}
}

// TestAdversarialGenericReceiver tests a method with type parameters on the receiver.
func TestAdversarialGenericReceiver(t *testing.T) {
	dir := t.TempDir()
	testfixture.Write(t, dir, "a.go", "package p\n\ntype Stack[T any] struct{}\n\nfunc (s *Stack[T]) Push(v T) {}\n")

	var out bytes.Buffer
	err := usecase.Outline(goast.New(), dir, 2, &out, format.Options{})
	if err != nil {
		t.Fatalf("Outline: %v", err)
	}
	outStr := out.String()
	if !strings.Contains(outStr, "Push") {
		t.Error("missing Push method")
	}
	if !strings.Contains(outStr, "s *Stack[T]") {
		t.Error("missing generic receiver parameter")
	}
}

// TestAdversarialGroupedConstMultiName verifies grouped const has separate names.
func TestAdversarialGroupedConstMultiName(t *testing.T) {
	dir := t.TempDir()
	testfixture.Write(t, dir, "a.go", "package p\n\nconst (\n\tA = 1\n\tB = 2\n)\n")

	var out bytes.Buffer
	err := usecase.Outline(goast.New(), dir, usecase.DepthUnlimited, &out, format.Options{})
	if err != nil {
		t.Fatalf("Outline: %v", err)
	}
	outStr := out.String()
	if !strings.Contains(outStr, "A") {
		t.Error("missing const A")
	}
	if !strings.Contains(outStr, "B") {
		t.Error("missing const B")
	}
}

// TestAdversarialEmptyFile verifies outline on a file with only package clause.
func TestAdversarialEmptyFile(t *testing.T) {
	dir := t.TempDir()
	testfixture.Write(t, dir, "empty.go", "package p\n")

	var out bytes.Buffer
	err := usecase.Outline(goast.New(), dir, usecase.DepthUnlimited, &out, format.Options{})
	if err != nil {
		t.Fatalf("Outline: %v", err)
	}
}

// TestAdversarialFileWithOnlyImports verifies outline on import-only file.
func TestAdversarialFileWithOnlyImports(t *testing.T) {
	dir := t.TempDir()
	testfixture.Write(t, dir, "imports.go", "package p\n\nimport \"fmt\"\n")

	var out bytes.Buffer
	err := usecase.Outline(goast.New(), dir, usecase.DepthUnlimited, &out, format.Options{})
	if err != nil {
		t.Fatalf("Outline: %v", err)
	}
}

// TestAdversarialBoundsGroupedConstPerName verifies that grouped const names
// have independent bounds.
func TestAdversarialBoundsGroupedConstPerName(t *testing.T) {
	dir := t.TempDir()
	testfixture.Write(t, dir, "a.go", "package p\n\nconst (\n\tA = 1\n\tB = 2\n)\n")

	path := dir + "/a.go"
	var out1, out2 bytes.Buffer
	err := usecase.Bounds(goast.New(), "A", path, &out1, format.Options{JSON: true})
	if err != nil {
		t.Fatalf("bounds A: %v", err)
	}
	err = usecase.Bounds(goast.New(), "B", path, &out2, format.Options{JSON: true})
	if err != nil {
		t.Fatalf("bounds B: %v", err)
	}

	var b1, b2 map[string]any
	if err := json.Unmarshal(out1.Bytes(), &b1); err != nil {
		t.Fatalf("bounds A not valid JSON: %v", err)
	}
	if err := json.Unmarshal(out2.Bytes(), &b2); err != nil {
		t.Fatalf("bounds B not valid JSON: %v", err)
	}

	if b1["start_byte"] == b2["start_byte"] {
		t.Error("A and B have identical start_byte; should differ")
	}
	if b1["end_byte"] == b2["end_byte"] {
		t.Error("A and B have identical end_byte; should differ")
	}
}

// TestAdversarialPackageQualifiedAmbiguousSamePackage verifies ambiguity
// within a single package via package-qualified lookup.
func TestAdversarialPackageQualifiedAmbiguousSamePackage(t *testing.T) {
	dir := t.TempDir()
	testfixture.Write(t, dir, "a.go", "package p\n\ntype A struct{}\nfunc (A) Save() {}\ntype B struct{}\nfunc (B) Save() {}\n")

	var out bytes.Buffer
	err := usecase.Get(goast.New(), "p.Save", dir, true, &out, format.Options{})
	if err == nil {
		t.Fatal("expected ambiguous error for p.Save")
	}
	var amb usecase.AmbiguousSymbol
	if !errors.As(err, &amb) {
		t.Fatalf("err = %v (%T), want AmbiguousSymbol", err, err)
	}
}

// TestAdversarialPackageNameMatchesTypeName verifies package-qualified lookup
// when package name matches a type name.
func TestAdversarialPackageNameMatchesTypeName(t *testing.T) {
	dir := t.TempDir()
	testfixture.Write(t, dir, "a.go", "package p\n\ntype p struct{}\nfunc (p) Method() {}\n")

	var out bytes.Buffer
	err := usecase.Get(goast.New(), "p.p", dir, true, &out, format.Options{})
	if err != nil {
		t.Fatalf("get p.p: %v", err)
	}
	if !strings.Contains(out.String(), "p struct") {
		t.Errorf("expected to resolve type p:\n%s", out.String())
	}
}

// TestAdversarialNestedBuildConstraintFilename tests _linux_amd64 implicit constraint.
// On linux/amd64, both files are built (linux_amd64 matches current platform,
// untagged is always built), so they conflict. On other platforms they wouldn't.
func TestAdversarialNestedBuildConstraintFilename(t *testing.T) {
	dir := t.TempDir()
	testfixture.Write(t, dir, "a_linux_amd64.go", "package p\n\nfunc F() {}\n")
	testfixture.Write(t, dir, "b.go", "package p\n\nfunc F() {}\n")

	var out bytes.Buffer
	err := usecase.Check(goast.New(), dir, &out, &out, format.Options{})
	if err == nil {
		t.Fatal("expected conflict error: linux_amd64 file and untagged file co-build on this platform")
	}
	if !strings.Contains(out.String(), "redeclared") {
		t.Errorf("expected redeclaration diagnostic:\n%s", out.String())
	}
}

// TestAdversarialUnparseableFileInMiddle verifies partial outline output.
func TestAdversarialUnparseableFileInMiddle(t *testing.T) {
	dir := t.TempDir()
	testfixture.Write(t, dir, "a_good.go", "package p\n\nfunc A() {}\n")
	testfixture.Write(t, dir, "b_broken.go", "package p\n\nfunc f( {\n")
	testfixture.Write(t, dir, "c_good.go", "package p\n\nfunc C() {}\n")

	var out, errOut bytes.Buffer
	err := usecase.Outline(goast.New(), dir, usecase.DepthUnlimited, &out, format.Options{})
	_ = errOut
	if err == nil {
		t.Fatal("expected error for outline with broken file")
	}
	outStr := out.String()
	if !strings.Contains(outStr, "A") {
		t.Error("missing good file A before broken file")
	}
	if !strings.Contains(outStr, "C") {
		t.Error("missing good file C after broken file")
	}
}

// TestAdversarialJSONOutlineEmptyDirectory verifies JSON outline on empty dir.
func TestAdversarialJSONOutlineEmptyDirectory(t *testing.T) {
	dir := t.TempDir()
	var out bytes.Buffer
	err := usecase.Outline(goast.New(), dir, usecase.DepthUnlimited, &out, format.Options{JSON: true})
	if err == nil {
		t.Fatal("expected error for empty directory")
	}
}

// TestAdversarialNonASCIICommentByteOffsets verifies byte offsets work
// with multi-byte characters in comments before the declaration.
func TestAdversarialNonASCIICommentByteOffsets(t *testing.T) {
	dir := t.TempDir()
	src := "package p\n\n// Box drawing: \xe2\x95\xad \xe2\x95\xae \xe2\x95\xaf\nfunc first() string { return \"x\" }\nfunc second() {}\n"
	testfixture.Write(t, dir, "enc.go", src)

	path := dir + "/enc.go"
	var out bytes.Buffer
	err := usecase.Bounds(goast.New(), "second", path, &out, format.Options{JSON: true})
	if err != nil {
		t.Fatalf("bounds: %v", err)
	}

	var b map[string]any
	if err := json.Unmarshal(out.Bytes(), &b); err != nil {
		t.Fatalf("bounds not valid JSON: %v", err)
	}
	startByte := int(b["start_byte"].(float64))
	endByte := int(b["end_byte"].(float64))

	raw := []byte(src)
	span := raw[startByte:endByte]
	if string(span) != "func second() {}" {
		t.Errorf("span = %q, want %q", span, "func second() {}")
	}
}
