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

// TestGetReportsEffectiveBuildConstraint covers the safety gap: a symbol in a Windows-only
// file used to resolve on Linux with no indication that the platform excludes it.
func TestGetReportsEffectiveBuildConstraint(t *testing.T) {
	dir := t.TempDir()
	testfixture.Write(t, dir, "a_windows.go", "package p\n\nfunc Win() {}\n")

	var human bytes.Buffer
	if err := usecase.Get(goast.New(), "Win", dir, true, &human, format.Options{}); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !strings.Contains(human.String(), "Build: windows") {
		t.Errorf("get output does not report the build constraint:\n%s", human.String())
	}

	var js bytes.Buffer
	if err := usecase.Get(goast.New(), "Win", dir, true, &js, format.Options{JSON: true}); err != nil {
		t.Fatalf("Get JSON: %v", err)
	}
	var payload struct {
		Build string `json:"build"`
	}
	if err := json.Unmarshal(js.Bytes(), &payload); err != nil {
		t.Fatalf("Unmarshal get: %v\n%s", err, js.String())
	}
	if payload.Build != "windows" {
		t.Errorf("get JSON build = %q, want windows", payload.Build)
	}

	var outline bytes.Buffer
	if err := usecase.Outline(goast.New(), dir, usecase.DepthUnlimited, &outline, format.Options{JSON: true}); err != nil {
		t.Fatalf("Outline: %v", err)
	}
	var files []struct {
		Build string `json:"build"`
	}
	if err := json.Unmarshal(outline.Bytes(), &files); err != nil {
		t.Fatalf("Unmarshal outline: %v\n%s", err, outline.String())
	}
	if len(files) != 1 || files[0].Build != "windows" {
		t.Errorf("outline JSON builds = %+v, want one file with windows", files)
	}
}

// TestAmbiguousCandidatesCarryBuildConstraint makes the platform variants of one declaration
// distinguishable without opening every file. The candidates were previously just paths.
func TestAmbiguousCandidatesCarryBuildConstraint(t *testing.T) {
	dir := t.TempDir()
	testfixture.Write(t, dir, "a_linux.go", "package p\n\nfunc F() {}\n")
	testfixture.Write(t, dir, "a_windows.go", "package p\n\nfunc F() {}\n")

	var out bytes.Buffer
	err := usecase.Get(goast.New(), "F", dir, true, &out, format.Options{})

	var ambiguous usecase.AmbiguousSymbol
	if !errors.As(err, &ambiguous) {
		t.Fatalf("err = %T (%v), want AmbiguousSymbol", err, err)
	}
	candidates := strings.Join(ambiguous.Candidates(), "\n")
	for _, want := range []string{"[build: linux]", "[build: windows]"} {
		if !strings.Contains(candidates, want) {
			t.Errorf("candidates do not contain %q:\n%s", want, candidates)
		}
	}
}

// TestCheckCustomTags covers the false negative that made check disagree with a tagged build.
// The same two files are clean under the default tags and a redeclaration under integration.
func TestCheckCustomTags(t *testing.T) {
	dir := t.TempDir()
	testfixture.Write(t, dir, "a_integration.go", "//go:build integration\n\npackage p\n\nfunc F() {}\n")
	testfixture.Write(t, dir, "b.go", "package p\n\nfunc F() {}\n")

	var stdout, stderr bytes.Buffer
	if err := usecase.Check(goast.New(), dir, &stdout, &stderr, format.Options{}); err != nil {
		t.Fatalf("default check should be clean: %v\n%s", err, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	err := usecase.CheckWithOptions(goast.New(), dir, usecase.CheckOptions{Tags: []string{"integration"}}, &stdout, &stderr, format.Options{})
	var diagnostics usecase.HasDiagnostics
	if !errors.As(err, &diagnostics) {
		t.Fatalf("tagged check err = %T (%v), want HasDiagnostics", err, err)
	}
	if !strings.Contains(stderr.String(), "F redeclared") {
		t.Errorf("tagged check did not report the conflict:\n%s", stderr.String())
	}

	// The cache is keyed on tags; a plain check after a tagged one must not inherit them.
	stdout.Reset()
	stderr.Reset()
	if err := usecase.Check(goast.New(), dir, &stdout, &stderr, format.Options{}); err != nil {
		t.Fatalf("default check after tagged check should still be clean: %v\n%s", err, stderr.String())
	}
}
