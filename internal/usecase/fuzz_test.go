package usecase_test

import (
	"bytes"
	"encoding/json"
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
