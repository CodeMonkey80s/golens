package golens_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CodeMonkey80s/golens"
	"github.com/CodeMonkey80s/golens/internal/usecase"
)

func fixtureFile(t *testing.T, src string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "p.go")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestEmbeddingOutlineGetBounds(t *testing.T) {
	path := fixtureFile(t, "package p\n\ntype T struct{ A int }\n\nfunc F() {}\n")

	var outline bytes.Buffer
	if err := golens.Outline(path, &outline, golens.Options{JSON: true, Depth: 1}); err != nil {
		t.Fatalf("Outline: %v", err)
	}
	var files []map[string]any
	if err := json.Unmarshal(outline.Bytes(), &files); err != nil {
		t.Fatalf("outline JSON: %v\n%s", err, outline.String())
	}
	if len(files) != 1 {
		t.Fatalf("outline files = %d, want 1", len(files))
	}

	var got bytes.Buffer
	if err := golens.Get("F", path, &got, golens.Options{}); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !strings.Contains(got.String(), "func F()") {
		t.Errorf("get output:\n%s", got.String())
	}

	var bounds bytes.Buffer
	if err := golens.Bounds("F", path, &bounds, golens.Options{JSON: true}); err != nil {
		t.Fatalf("Bounds: %v", err)
	}
	var payload struct {
		StartByte int `json:"start_byte"`
		EndByte   int `json:"end_byte"`
	}
	if err := json.Unmarshal(bounds.Bytes(), &payload); err != nil {
		t.Fatalf("bounds JSON: %v\n%s", err, bounds.String())
	}
	if payload.StartByte >= payload.EndByte {
		t.Errorf("empty span [%d,%d)", payload.StartByte, payload.EndByte)
	}
}

func TestEmbeddingCheckAndExitCodes(t *testing.T) {
	clean := fixtureFile(t, "package p\n\nfunc F() {}\n")
	unused := fixtureFile(t, "package p\n\nimport \"fmt\"\n\nfunc F() {}\n")

	var stdout, stderr bytes.Buffer
	if err := golens.Check([]string{clean}, &stdout, &stderr, golens.Options{}); err != nil {
		t.Fatalf("clean check: %v\n%s", err, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	err := golens.Check([]string{unused}, &stdout, &stderr, golens.Options{})
	var diagnostics usecase.HasDiagnostics
	if !errors.As(err, &diagnostics) {
		t.Fatalf("unused check err = %T (%v), want HasDiagnostics", err, err)
	}
	if code := golens.ExitCodeFor(err); code != golens.ExitDiagnostics {
		t.Errorf("ExitCodeFor(diagnostics) = %d, want %d", code, golens.ExitDiagnostics)
	}

	err = golens.Get("Missing", clean, &stdout, golens.Options{})
	if code := golens.ExitCodeFor(err); code != golens.ExitNotFound {
		t.Errorf("ExitCodeFor(not found) = %d, want %d", code, golens.ExitNotFound)
	}

	err = golens.Check(nil, &stdout, &stderr, golens.Options{})
	if code := golens.ExitCodeFor(err); code != golens.ExitUsage {
		t.Errorf("ExitCodeFor(no targets) = %d, want %d", code, golens.ExitUsage)
	}
}
