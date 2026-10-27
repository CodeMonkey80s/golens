package usecase_test

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CodeMonkey80s/golens/internal/engine/goast"
	"github.com/CodeMonkey80s/golens/internal/format"
	"github.com/CodeMonkey80s/golens/internal/usecase"
)

// TestEveryRepositorySymbolRoundTrips walks this repository and asks the public commands about
// every package-level symbol in every file. It is not a fixture: it exercises the resolver,
// span extraction and A6 equivalence at the scale of the codebase, including generics, aliases,
// grouped declarations and methods.
func TestEveryRepositorySymbolRoundTrips(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", ".."))
	engine := goast.New()

	var checked int
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".go") {
			return nil
		}

		doc, err := engine.Parse(path)
		if err != nil {
			t.Fatalf("Parse %s: %v", path, err)
		}
		for _, sym := range doc.Symbols {
			if sym.Name == "_" || sym.Name == "init" {
				continue
			}
			checked++

			var getBuf bytes.Buffer
			if err := usecase.Get(engine, sym.QualifiedName, path, true, &getBuf, format.Options{JSON: true}); err != nil {
				t.Fatalf("Get %s in %s: %v", sym.QualifiedName, path, err)
			}
			var got struct {
				StartByte int    `json:"start_byte"`
				EndByte   int    `json:"end_byte"`
				Source    string `json:"source"`
			}
			if err := json.Unmarshal(getBuf.Bytes(), &got); err != nil {
				t.Fatalf("Get JSON %s: %v\n%s", sym.QualifiedName, err, getBuf.String())
			}
			if got.StartByte < 0 || got.EndByte > len(doc.Source) || got.StartByte >= got.EndByte {
				t.Fatalf("%s: invalid span [%d,%d) for %d bytes", sym.QualifiedName, got.StartByte, got.EndByte, len(doc.Source))
			}
			if string(doc.Source[got.StartByte:got.EndByte]) != got.Source {
				t.Fatalf("%s: source does not equal src[start:end]", sym.QualifiedName)
			}

			var boundsBuf, onlyBuf bytes.Buffer
			if err := usecase.Bounds(engine, sym.QualifiedName, path, &boundsBuf, format.Options{JSON: true}); err != nil {
				t.Fatalf("Bounds %s: %v", sym.QualifiedName, err)
			}
			if err := usecase.Get(engine, sym.QualifiedName, path, false, &onlyBuf, format.Options{JSON: true}); err != nil {
				t.Fatalf("Get --bounds-only %s: %v", sym.QualifiedName, err)
			}
			if boundsBuf.String() != onlyBuf.String() {
				t.Fatalf("%s: bounds and get --bounds-only disagree", sym.QualifiedName)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if checked == 0 {
		t.Fatal("no symbols checked; the walk is vacuous")
	}
}
