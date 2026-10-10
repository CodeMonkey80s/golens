package usecase_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CodeMonkey80s/golens/internal/engine/goast"
	"github.com/CodeMonkey80s/golens/internal/format"
	"github.com/CodeMonkey80s/golens/internal/testfixture"
	"github.com/CodeMonkey80s/golens/internal/usecase"
)

// broken is a file the parser rejects, with a position so the diagnostic is worth asserting on.
const broken = "package p\n\nfunc f( {\n"

// TestOutlineSkipsUnparseableFilesWithoutHidingTheRest covers the failure that made outline
// useless on a real repository.
//
// outline aborted on the first file that failed to parse and wrote nothing at all. The Go
// standard library has exactly one unparseable file out of 8,088 --
// cmd/compile/internal/syntax/testdata/issue20789.go -- so asking for an outline of $GOROOT/src
// produced no output and said nothing about the other 8,087 files. The remaining files are now
// outlined first, and the failure is reported afterwards.
func TestOutlineSkipsUnparseableFilesWithoutHidingTheRest(t *testing.T) {
	dir := t.TempDir()
	testfixture.Write(t, dir, "a_broken.go", broken)
	testfixture.Write(t, dir, "b_good.go", "package p\n\nfunc B() {}\n")
	testfixture.Write(t, dir, "c_good.go", "package p\n\nfunc C() {}\n")

	var out bytes.Buffer
	err := usecase.Outline(goast.New(), dir, usecase.DepthUnlimited, &out, format.Options{})

	if err == nil {
		t.Fatal("a parse failure must still be reported")
	}
	got := out.String()
	for _, want := range []string{"func B()", "func C()"} {
		if !strings.Contains(got, want) {
			t.Errorf("outline omitted %s; a broken file must not hide the rest:\n%s", want, got)
		}
	}
	if strings.Contains(got, "func f(") {
		t.Errorf("outline included the unparseable file:\n%s", got)
	}
}

// TestOutlineReportsSkippedFilesExitFour pins the exit path. The parse failure is real, so the
// run still fails -- but it fails after delivering the data, not instead of it.
func TestOutlineReportsSkippedFilesExitFour(t *testing.T) {
	dir := t.TempDir()
	testfixture.Write(t, dir, "a_broken.go", broken)
	testfixture.Write(t, dir, "b_good.go", "package p\n\nfunc B() {}\n")

	var out bytes.Buffer
	err := usecase.Outline(goast.New(), dir, usecase.DepthUnlimited, &out, format.Options{})
	if err == nil {
		t.Fatal("expected an error so the caller learns the outline is incomplete")
	}

	var skipped interface{ Unwrap() error }
	if !errors.As(err, &skipped) {
		t.Fatalf("error does not unwrap to the underlying parse failure: %v", err)
	}
	if got := err.Error(); !strings.Contains(got, "a_broken.go") {
		t.Errorf("error does not name the skipped file: %v", got)
	}
}

// TestOutlineJSONStaysParseableWhenAFileIsBroken is the contract a machine consumer depends on.
//
// stdout carries JSON and stderr carries the failure, so a skipped file must not truncate or
// corrupt the payload. An agent running `golens outline . --json | jq` gets every file that
// parsed; an agent checking $? learns the outline is partial.
func TestOutlineJSONStaysParseableWhenAFileIsBroken(t *testing.T) {
	dir := t.TempDir()
	testfixture.Write(t, dir, "a_broken.go", broken)
	testfixture.Write(t, dir, "b_good.go", "package p\n\nfunc B() {}\n")

	var out bytes.Buffer
	opts := format.Options{JSON: true, Compact: true}
	if err := usecase.Outline(goast.New(), dir, usecase.DepthUnlimited, &out, opts); err == nil {
		t.Fatal("expected an error")
	}

	var payloads []format.OutlineFile
	if err := json.Unmarshal(out.Bytes(), &payloads); err != nil {
		t.Fatalf("stdout is not valid JSON when a file is skipped: %v\n%s", err, out.String())
	}
	if len(payloads) != 1 {
		t.Fatalf("payloads = %d, want 1 (only the parseable file)", len(payloads))
	}
	if !strings.HasSuffix(payloads[0].File, "b_good.go") {
		t.Errorf("outlined %q, want the parseable file", payloads[0].File)
	}
}

// TestOutlineSingleBrokenFileKeepsTheOriginalMessage guards the case where there is nothing
// partial to deliver. Reporting "1 of 1 files could not be parsed" adds noise around a
// diagnostic that already says everything.
func TestOutlineSingleBrokenFileKeepsTheOriginalMessage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a_broken.go")
	testfixture.Write(t, dir, "a_broken.go", broken)

	var out bytes.Buffer
	err := usecase.Outline(goast.New(), path, usecase.DepthUnlimited, &out, format.Options{})
	if err == nil {
		t.Fatal("expected an error")
	}
	if out.Len() != 0 {
		t.Errorf("a single unparseable file should produce no output, got:\n%s", out.String())
	}
	if got := err.Error(); strings.Contains(got, "of 1 files") {
		t.Errorf("single-file message should be the bare diagnostic, got: %v", got)
	}
	if !strings.Contains(err.Error(), "a_broken.go") {
		t.Errorf("message lost the file name: %v", err)
	}
}

// TestOutlineSkippedFileListIsBounded checks the message stays readable when a tree is broadly
// broken. The count stays exact; only the enumeration is truncated.
func TestOutlineSkippedFileListIsBounded(t *testing.T) {
	dir := t.TempDir()
	const brokenFiles = 9
	for i := range brokenFiles {
		name := filepath.Join(dir, string(rune('a'+i))+"_broken.go")
		if err := os.WriteFile(name, []byte(broken), 0o600); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
	}

	var out bytes.Buffer
	err := usecase.Outline(goast.New(), dir, usecase.DepthUnlimited, &out, format.Options{})
	if err == nil {
		t.Fatal("expected an error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "9 of 9 files") {
		t.Errorf("summary count should stay exact, got: %v", msg)
	}
	if !strings.Contains(msg, "and 4 more") {
		t.Errorf("message should truncate the enumeration, got: %v", msg)
	}
}
