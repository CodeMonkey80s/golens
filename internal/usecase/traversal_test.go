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

// TestOutlineSkipTestdata covers the reason the flag exists: an intentionally broken fixture
// under testdata makes the default outline exit 4, while the build-facing view is clean.
func TestOutlineSkipTestdata(t *testing.T) {
	dir := t.TempDir()
	testfixture.Write(t, dir, "good.go", "package p\n\nfunc Good() {}\n")
	testfixture.Write(t, dir, "testdata/broken.go", "package p\n\nfunc Broken( {\n")

	var out bytes.Buffer
	err := usecase.Outline(goast.New(), dir, usecase.DepthUnlimited, &out, format.Options{})
	var skipped usecase.SkippedFilesError
	if !errors.As(err, &skipped) {
		t.Fatalf("default outline err = %T (%v), want SkippedFilesError", err, err)
	}
	if !strings.Contains(out.String(), "Good") {
		t.Errorf("default outline dropped the parseable file:\n%s", out.String())
	}

	out.Reset()
	if err := usecase.OutlineWithFileOptions(goast.New(), dir, usecase.DepthUnlimited, &out, format.Options{}, usecase.FileOptions{SkipTestdata: true}); err != nil {
		t.Fatalf("outline with --skip-testdata: %v", err)
	}
	if !strings.Contains(out.String(), "Good") {
		t.Errorf("skip-testdata outline dropped the parseable file:\n%s", out.String())
	}
}

// TestResolveSkipTestdata and TestResolveSkipIgnored cover the same policy for symbol lookup.
func TestResolveSkipTestdata(t *testing.T) {
	dir := t.TempDir()
	testfixture.Write(t, dir, "good.go", "package p\n\nfunc Good() {}\n")
	testfixture.Write(t, dir, "testdata/fixture.go", "package p\n\nfunc Fixture() {}\n")

	var out bytes.Buffer
	if err := usecase.Get(goast.New(), "Fixture", dir, true, &out, format.Options{}); err != nil {
		t.Fatalf("default get should resolve a testdata symbol: %v", err)
	}

	out.Reset()
	err := usecase.GetWithOptions(goast.New(), "Fixture", dir, true, &out, format.Options{}, usecase.ResolveOptions{SkipTestdata: true})
	var notFound interface{ IsNotFound() bool }
	if !errors.As(err, &notFound) || !notFound.IsNotFound() {
		t.Fatalf("get with --skip-testdata err = %T (%v), want not found", err, err)
	}
}

func TestResolveSkipIgnored(t *testing.T) {
	dir := t.TempDir()
	testfixture.Write(t, dir, "good.go", "package p\n\nfunc Good() {}\n")
	testfixture.Write(t, dir, "_ignored.go", "package p\n\nfunc Ignored() {}\n")

	var out bytes.Buffer
	if err := usecase.Get(goast.New(), "Ignored", dir, true, &out, format.Options{}); err != nil {
		t.Fatalf("default get should resolve an ignored-name symbol: %v", err)
	}

	out.Reset()
	err := usecase.GetWithOptions(goast.New(), "Ignored", dir, true, &out, format.Options{}, usecase.ResolveOptions{SkipIgnored: true})
	var notFound interface{ IsNotFound() bool }
	if !errors.As(err, &notFound) || !notFound.IsNotFound() {
		t.Fatalf("get with --skip-ignored err = %T (%v), want not found", err, err)
	}
}

// TestCheckSkipsIgnoredFiles covers the other half of the Go ignore rule: a file whose name
// begins with "_" is not built, so a diagnostic about it is not a finding about the source.
func TestCheckSkipsIgnoredFiles(t *testing.T) {
	dir := t.TempDir()
	testfixture.Write(t, dir, "good.go", "package p\n\nfunc Good() {}\n")
	testfixture.Write(t, dir, "_ignored.go", "package p\n\nimport \"fmt\"\n\nfunc Ignored() {}\n")

	var stdout, stderr bytes.Buffer
	if err := usecase.Check(goast.New(), dir, &stdout, &stderr, format.Options{}); err != nil {
		t.Fatalf("check should ignore _-prefixed files: %v\n%s", err, stderr.String())
	}
}
