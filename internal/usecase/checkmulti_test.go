package usecase_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/CodeMonkey80s/golens/internal/engine/goast"
	"github.com/CodeMonkey80s/golens/internal/format"
	"github.com/CodeMonkey80s/golens/internal/testfixture"
	"github.com/CodeMonkey80s/golens/internal/usecase"
)

// TestCheckMultipleTargets covers the variadic form: one clean directory and one with an
// unused import produce a single run whose exit code is still "diagnostics found".
func TestCheckMultipleTargets(t *testing.T) {
	clean := t.TempDir()
	testfixture.Write(t, clean, "a.go", "package p\n\nfunc A() {}\n")

	unused := t.TempDir()
	testfixture.Write(t, unused, "b.go", "package p\n\nimport \"fmt\"\n\nfunc B() {}\n")

	var stdout, stderr bytes.Buffer
	err := usecase.CheckTargets(goast.New(), []string{clean, unused}, &stdout, &stderr, format.Options{})
	var found usecase.HasDiagnostics
	if !errors.As(err, &found) {
		t.Fatalf("err = %T (%v), want HasDiagnostics", err, err)
	}
	if !strings.Contains(stderr.String(), "imported and not used") {
		t.Errorf("diagnostics do not mention the unused import:\n%s", stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	if err := usecase.CheckTargets(goast.New(), []string{clean, t.TempDir()}, &stdout, &stderr, format.Options{}); err != nil {
		t.Fatalf("two clean targets should pass: %v\n%s", err, stderr.String())
	}
}

// TestCheckTargetsDeduplicates guards overlapping targets: naming a file and its directory
// must not report the same diagnostic twice.
func TestCheckTargetsDeduplicates(t *testing.T) {
	dir := t.TempDir()
	file := testfixture.Write(t, dir, "a.go", "package p\n\nimport \"fmt\"\n\nfunc A() {}\n")

	var stdout, stderr bytes.Buffer
	err := usecase.CheckTargets(goast.New(), []string{dir, file, dir}, &stdout, &stderr, format.Options{})
	if err == nil {
		t.Fatal("expected diagnostics")
	}
	if got := strings.Count(stderr.String(), "imported and not used"); got != 1 {
		t.Errorf("diagnostic reported %d times, want 1:\n%s", got, stderr.String())
	}
}

// TestCheckTargetsMissingPath keeps the fail-fast rule visible: a missing target is exit 2,
// and the other targets are not silently reported as clean.
func TestCheckTargetsMissingPath(t *testing.T) {
	dir := t.TempDir()
	testfixture.Write(t, dir, "a.go", "package p\n\nfunc A() {}\n")

	var stdout, stderr bytes.Buffer
	err := usecase.CheckTargets(goast.New(), []string{dir, dir + "/missing"}, &stdout, &stderr, format.Options{})
	if err == nil {
		t.Fatal("expected a not-found error")
	}
	var notFound interface{ IsNotFound() bool }
	if !errors.As(err, &notFound) || !notFound.IsNotFound() {
		t.Fatalf("err = %T (%v), want a not-found error", err, err)
	}
}
