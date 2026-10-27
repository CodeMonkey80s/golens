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

// safeGoFilename turns arbitrary fuzz input into a single, safe .go filename.
func safeGoFilename(name string) string {
	name = filepath.Base(name)
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	name = b.String()
	if name == "" || name == "." || name == ".." {
		name = "fuzz"
	}
	name = strings.TrimSuffix(name, ".go")
	if len(name) > 80 {
		name = name[:80]
	}
	return name + ".go"
}

// resetFuzzDir removes files from the previous input. The filename is fuzzed, so unlike the
// single-file targets these directories would otherwise accumulate one file per execution and
// each input would be analysed alongside every earlier one.
func resetFuzzDir(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if err := os.RemoveAll(filepath.Join(dir, entry.Name())); err != nil {
			t.Fatal(err)
		}
	}
}

// FuzzCheck fuzzes the heuristic-heavy half of the tool: unused imports, dot imports,
// duplicate declarations and build constraints across two files. Those analyses are where
// false positives and panics have historically come from, and they were not covered by the
// single-file outline/get/bounds targets.
func FuzzCheck(f *testing.F) {
	f.Add("package p\n\nfunc F() {}\n", "package p\n\nfunc G() {}\n", "", "a.go", "b.go")
	f.Add("package p\n\nimport \"fmt\"\n\nfunc F() {}\n", "", "", "a.go", "b.go")
	f.Add("package p\n\nimport . \"fmt\"\n\nfunc F() { Println(1) }\n", "", "", "a.go", "b.go")
	f.Add("//go:build integration\n\npackage p\n\nfunc F() {}\n", "package p\n\nfunc F() {}\n", "integration", "a_integration.go", "b.go")
	f.Add("package p\n\nvar A, B = 1, 2\n", "package p\n\ntype T struct{ A int }\n", "", "a.go", "b.go")
	f.Add("package p\n\nfunc F() {}\n", "package p\n\nfunc F() {}\n", "", "a_windows.go", "a_linux.go")

	engine := goast.New()
	dir := fuzzDir(f)

	f.Fuzz(func(t *testing.T, srcA, srcB, tags, rawNameA, rawNameB string) {
		resetFuzzDir(t, dir)
		nameA := safeGoFilename(rawNameA)
		nameB := safeGoFilename(rawNameB)
		if nameA == nameB {
			nameB = strings.TrimSuffix(nameB, ".go") + "_b.go"
		}
		if err := os.WriteFile(filepath.Join(dir, nameA), []byte(srcA), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, nameB), []byte(srcB), 0o644); err != nil {
			t.Fatal(err)
		}

		var stdout, stderr bytes.Buffer
		err := usecase.CheckWithOptions(engine, dir, usecase.CheckOptions{Tags: []string{tags}}, &stdout, &stderr, format.Options{JSON: true})
		if stdout.Len() == 0 {
			// A directory of only hidden files is a legitimate no-go-files error; there is no
			// payload to validate.
			if err == nil {
				t.Fatal("check returned no error and no payload")
			}
			return
		}

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
	f.Add("package p\n\nfunc F() {}\n", "package p\n\nfunc G() {}\n", "F", "a.go", "b.go")
	f.Add("package p\n\ntype T struct{}\n\nfunc (T) M() {}\n", "package p\n", "T.M", "a.go", "b.go")
	f.Add("package p\n\nfunc F() {}\n", "package p\n\nfunc F() {}\n", "F", "a.go", "b.go")
	f.Add("package p\n\nfunc A() {}\n", "package p\n\nfunc B( {\n", "A", "a.go", "b.go")
	f.Add("//go:build windows\n\npackage p\n\nfunc Win() {}\n", "package p\n", "Win", "a_windows.go", "b.go")
	f.Add("package p\n\nfunc F() {}\n", "package p\n\nfunc F() {}\n", "F", "a_linux.go", "a_windows.go")

	engine := goast.New()
	dir := fuzzDir(f)

	f.Fuzz(func(t *testing.T, srcA, srcB, symbol, rawNameA, rawNameB string) {
		resetFuzzDir(t, dir)
		nameA := safeGoFilename(rawNameA)
		nameB := safeGoFilename(rawNameB)
		if nameA == nameB {
			nameB = strings.TrimSuffix(nameB, ".go") + "_b.go"
		}
		if err := os.WriteFile(filepath.Join(dir, nameA), []byte(srcA), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, nameB), []byte(srcB), 0o644); err != nil {
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
