package usecase_test

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"github.com/CodeMonkey80s/golens/internal/engine/goast"
	"github.com/CodeMonkey80s/golens/internal/format"
	"github.com/CodeMonkey80s/golens/internal/usecase"
)

// FuzzOutline fuzzes outline with arbitrary Go source, ensuring it never
// panics and always produces valid output (or a clean error).
func FuzzOutline(f *testing.F) {
	engine := goast.New()

	f.Fuzz(func(t *testing.T, src []byte) {
		dir := t.TempDir()
		path := dir + "/fuzz.go"
		if err := os.WriteFile(path, src, 0o644); err != nil {
			t.Fatal(err)
		}

		var buf bytes.Buffer
		err := usecase.Outline(engine, path, usecase.DepthUnlimited, &buf, format.Options{})
		// Outline must never panic. It may return an error for unparseable input,
		// but the output on stdout must still be valid (or empty).
		if err != nil {
			return
		}

		if buf.Len() == 0 {
			t.Errorf("outline produced empty output for parseable input:\n%s", string(src))
		}
	})
}

// FuzzGet fuzzes get with arbitrary Go source and symbol names, ensuring it
// never panics.
func FuzzGet(f *testing.F) {
	engine := goast.New()

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) == 0 {
			return
		}
		mid := len(data) / 2
		if mid == 0 {
			mid = 1
		}
		src := data[:mid]
		var sb bytes.Buffer
		for _, b := range data[mid:] {
			if b < 32 || b > 126 {
				sb.WriteByte('_')
			} else {
				sb.WriteByte(b)
			}
		}
		symbolName := sb.String()
		if symbolName == "" {
			symbolName = "F"
		}

		dir := t.TempDir()
		path := dir + "/fuzz.go"
		if err := os.WriteFile(path, src, 0o644); err != nil {
			t.Fatal(err)
		}

		var buf bytes.Buffer
		err := usecase.Get(engine, symbolName, path, true, &buf, format.Options{})
		if err != nil {
			if buf.Len() != 0 {
				t.Errorf("get produced stdout on error:\n%s", buf.String())
			}
			return
		}

		if buf.Len() == 0 {
			t.Errorf("get produced empty output for found symbol %q:\n%s", symbolName, string(src))
		}
	})
}

// FuzzBounds fuzzes bounds with arbitrary Go source and symbol names, ensuring
// it never panics and byte offsets are valid.
func FuzzBounds(f *testing.F) {
	engine := goast.New()

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) == 0 {
			return
		}
		mid := len(data) / 2
		if mid == 0 {
			mid = 1
		}
		src := data[:mid]
		var sb bytes.Buffer
		for _, b := range data[mid:] {
			if b < 32 || b > 126 {
				sb.WriteByte('_')
			} else {
				sb.WriteByte(b)
			}
		}
		symbolName := sb.String()
		if symbolName == "" {
			symbolName = "F"
		}

		dir := t.TempDir()
		path := dir + "/fuzz.go"
		if err := os.WriteFile(path, src, 0o644); err != nil {
			t.Fatal(err)
		}

		var buf bytes.Buffer
		err := usecase.Bounds(engine, symbolName, path, &buf, format.Options{JSON: true})
		if err != nil {
			return
		}

		var result struct {
			StartByte int `json:"start_byte"`
			EndByte   int `json:"end_byte"`
		}
		if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
			t.Fatalf("bounds output not valid JSON: %v\n%s", err, buf.String())
		}

		if result.StartByte < 0 || result.EndByte < 0 {
			t.Errorf("negative bounds: start=%d end=%d", result.StartByte, result.EndByte)
		}
		if result.StartByte > result.EndByte {
			t.Errorf("start > end: start=%d end=%d", result.StartByte, result.EndByte)
		}
		if result.EndByte > len(src) {
			t.Errorf("end=%d beyond source length=%d", result.EndByte, len(src))
		}
	})
}
