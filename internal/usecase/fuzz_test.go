package usecase_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/CodeMonkey80s/golens/internal/engine/goast"
	"github.com/CodeMonkey80s/golens/internal/format"
	"github.com/CodeMonkey80s/golens/internal/usecase"
)

// writeSource places src in a fresh file inside dir and returns its path.
// The fuzz targets reuse one directory per process because each worker runs one
// input at a time; allocating a t.TempDir per execution would spend the whole
// budget on filesystem churn.
func writeSource(t *testing.T, dir, src string) string {
	t.Helper()
	path := filepath.Join(dir, "fuzz.go")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func fuzzDir(f *testing.F) string {
	dir, err := os.MkdirTemp("", "golens-fuzz-")
	if err != nil {
		f.Fatal(err)
	}
	f.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// FuzzOutline checks that arbitrary bytes on disk either produce a parse error
// or a JSON payload with the documented shape. A panic here is a bug in the
// engine, not in the input.
func FuzzOutline(f *testing.F) {
	f.Add("package p\n\nfunc F() {}\n")
	f.Add("package p\n\ntype T struct{ A int }\n\nfunc (t T) M() {}\n")
	f.Add("")
	f.Add("package p\n\nfunc F( {\n")

	engine := goast.New()
	dir := fuzzDir(f)

	f.Fuzz(func(t *testing.T, src string) {
		path := writeSource(t, dir, src)

		var buf bytes.Buffer
		if err := usecase.Outline(engine, path, usecase.DepthUnlimited, &buf, format.Options{JSON: true}); err != nil {
			return
		}

		var files []struct {
			SchemaVersion int               `json:"schema_version"`
			Nodes         []json.RawMessage `json:"nodes"`
		}
		if err := json.Unmarshal(buf.Bytes(), &files); err != nil {
			t.Fatalf("outline output is not valid JSON: %v\n%s", err, buf.String())
		}
		if len(files) != 1 || files[0].SchemaVersion != 1 {
			t.Fatalf("outline JSON must be one file with schema_version 1: %s", buf.String())
		}
	})
}

// FuzzGet checks that a symbol lookup never panics and that a successful answer
// satisfies the byte invariant: src[start_byte:end_byte] is exactly the returned
// source.
func FuzzGet(f *testing.F) {
	f.Add("package p\n\nfunc F() {}\n", "F")
	f.Add("package p\n\ntype T struct{}\n\nfunc (t T) M() {}\n", "T.M")
	f.Add("package p\n\nvar X = 1\n", "X")
	f.Add("package p\n\nimport . \"fmt\"\n\nfunc F() { Println(1) }\n", "F")
	f.Add("not go at all\n", "F")

	engine := goast.New()
	dir := fuzzDir(f)

	f.Fuzz(func(t *testing.T, src, symbol string) {
		path := writeSource(t, dir, src)

		var buf bytes.Buffer
		if err := usecase.Get(engine, symbol, path, true, &buf, format.Options{JSON: true}); err != nil {
			if buf.Len() != 0 {
				t.Fatalf("get wrote a payload on error: %s", buf.String())
			}
			return
		}

		var got struct {
			SchemaVersion int    `json:"schema_version"`
			StartByte     int    `json:"start_byte"`
			EndByte       int    `json:"end_byte"`
			Source        string `json:"source"`
		}
		if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
			t.Fatalf("get output is not valid JSON: %v\n%s", err, buf.String())
		}
		if got.SchemaVersion != 1 {
			t.Fatalf("schema_version = %d, want 1", got.SchemaVersion)
		}
		if got.StartByte < 0 || got.EndByte < got.StartByte || got.EndByte > len(src) {
			t.Fatalf("span [%d,%d) is outside a %d-byte source", got.StartByte, got.EndByte, len(src))
		}
		if src[got.StartByte:got.EndByte] != got.Source {
			t.Fatalf("src[%d:%d] != source\nspan:   %q\nsource: %q",
				got.StartByte, got.EndByte, src[got.StartByte:got.EndByte], got.Source)
		}
	})
}

// FuzzBounds checks that bounds never panics and never returns a span outside
// the file it was given.
func FuzzBounds(f *testing.F) {
	f.Add("package p\n\nfunc F() {}\n", "F")
	f.Add("package p\n\ntype T struct{}\n\nfunc (t T) M() {}\n", "T.M")
	f.Add("package p\n\nvar X = 1\n", "X")
	f.Add("not go at all\n", "F")

	engine := goast.New()
	dir := fuzzDir(f)

	f.Fuzz(func(t *testing.T, src, symbol string) {
		path := writeSource(t, dir, src)

		var buf bytes.Buffer
		if err := usecase.Bounds(engine, symbol, path, &buf, format.Options{JSON: true}); err != nil {
			if buf.Len() != 0 {
				t.Fatalf("bounds wrote a payload on error: %s", buf.String())
			}
			return
		}

		var got struct {
			SchemaVersion int `json:"schema_version"`
			StartByte     int `json:"start_byte"`
			EndByte       int `json:"end_byte"`
		}
		if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
			t.Fatalf("bounds output is not valid JSON: %v\n%s", err, buf.String())
		}
		if got.SchemaVersion != 1 {
			t.Fatalf("schema_version = %d, want 1", got.SchemaVersion)
		}
		if got.StartByte < 0 || got.EndByte < got.StartByte || got.EndByte > len(src) {
			t.Fatalf("span [%d,%d) is outside a %d-byte source", got.StartByte, got.EndByte, len(src))
		}
	})
}

// FuzzCheck fuzzes the heuristic-heavy half of the tool: unused imports, dot imports,
// duplicate declarations and build constraints across two files. Those analyses are where
// false positives and panics have historically come from, and they were not covered by the
// single-file outline/get/bounds targets.
func FuzzCheck(f *testing.F) {
	f.Add("package p\n\nfunc F() {}\n", "package p\n\nfunc G() {}\n", "")
	f.Add("package p\n\nimport \"fmt\"\n\nfunc F() {}\n", "", "")
	f.Add("package p\n\nimport . \"fmt\"\n\nfunc F() { Println(1) }\n", "", "")
	f.Add("//go:build integration\n\npackage p\n\nfunc F() {}\n", "package p\n\nfunc F() {}\n", "integration")
	f.Add("package p\n\nvar A, B = 1, 2\n", "package p\n\ntype T struct{ A int }\n", "")

	engine := goast.New()
	dir := fuzzDir(f)

	f.Fuzz(func(t *testing.T, srcA, srcB, tags string) {
		if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte(srcA), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "b.go"), []byte(srcB), 0o644); err != nil {
			t.Fatal(err)
		}

		var stdout, stderr bytes.Buffer
		err := usecase.CheckWithOptions(engine, dir, usecase.CheckOptions{Tags: []string{tags}}, &stdout, &stderr, format.Options{JSON: true})

		var payload struct {
			SchemaVersion int               `json:"schema_version"`
			Diagnostics   []json.RawMessage `json:"diagnostics"`
		}
		if uerr := json.Unmarshal(stdout.Bytes(), &payload); uerr != nil {
			t.Fatalf("check JSON is invalid: %v\n%s", uerr, stdout.String())
		}
		if payload.SchemaVersion != 1 {
			t.Fatalf("schema_version = %d, want 1", payload.SchemaVersion)
		}
		if stderr.Len() != 0 {
			t.Fatalf("check --json wrote to stderr: %s", stderr.String())
		}

		if err == nil {
			if len(payload.Diagnostics) != 0 {
				t.Fatalf("check returned no error but reported %d diagnostics", len(payload.Diagnostics))
			}
			return
		}

		var found usecase.HasDiagnostics
		if !errors.As(err, &found) {
			t.Fatalf("check error = %T (%v), want HasDiagnostics", err, err)
		}
		if found.Count != len(payload.Diagnostics) {
			t.Fatalf("HasDiagnostics.Count = %d, payload has %d", found.Count, len(payload.Diagnostics))
		}
	})
}

// FuzzResolve fuzzes symbol resolution across a directory: two files, optional build
// constraints, and an arbitrary query. It checks that a successful payload still satisfies
// the byte invariant against the file it names.
func FuzzResolve(f *testing.F) {
	f.Add("package p\n\nfunc F() {}\n", "package p\n\nfunc G() {}\n", "F")
	f.Add("package p\n\ntype T struct{}\n\nfunc (T) M() {}\n", "package p\n", "T.M")
	f.Add("package p\n\nfunc F() {}\n", "package p\n\nfunc F() {}\n", "F")
	f.Add("package p\n\nfunc A() {}\n", "package p\n\nfunc B( {\n", "A")
	f.Add("//go:build windows\n\npackage p\n\nfunc Win() {}\n", "package p\n", "Win")

	engine := goast.New()
	dir := fuzzDir(f)

	f.Fuzz(func(t *testing.T, srcA, srcB, symbol string) {
		if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte(srcA), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "b.go"), []byte(srcB), 0o644); err != nil {
			t.Fatal(err)
		}

		var buf bytes.Buffer
		err := usecase.Get(engine, symbol, dir, true, &buf, format.Options{JSON: true})
		if buf.Len() == 0 {
			if err == nil {
				t.Fatalf("get returned no error and no payload for %q", symbol)
			}
			return
		}

		var got struct {
			SchemaVersion int    `json:"schema_version"`
			File          string `json:"file"`
			StartByte     int    `json:"start_byte"`
			EndByte       int    `json:"end_byte"`
			Source        string `json:"source"`
		}
		if uerr := json.Unmarshal(buf.Bytes(), &got); uerr != nil {
			t.Fatalf("get JSON is invalid: %v\n%s", uerr, buf.String())
		}
		if got.SchemaVersion != 1 {
			t.Fatalf("schema_version = %d, want 1", got.SchemaVersion)
		}
		data, rerr := os.ReadFile(got.File)
		if rerr != nil {
			t.Fatalf("get named %q, which cannot be read: %v", got.File, rerr)
		}
		if got.StartByte < 0 || got.EndByte < got.StartByte || got.EndByte > len(data) {
			t.Fatalf("span [%d,%d) is outside %q (%d bytes)", got.StartByte, got.EndByte, got.File, len(data))
		}
		if string(data[got.StartByte:got.EndByte]) != got.Source {
			t.Fatalf("file[%d:%d] != source in %q", got.StartByte, got.EndByte, got.File)
		}
	})
}
