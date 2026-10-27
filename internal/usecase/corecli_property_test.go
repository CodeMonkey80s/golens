package usecase_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CodeMonkey80s/golens/internal/engine/goast"
	"github.com/CodeMonkey80s/golens/internal/format"
	"github.com/CodeMonkey80s/golens/internal/usecase"
)

type generatedDecl struct {
	symbol string
	source string
	start  int
	end    int
}

// TestGeneratedDeclarationsRoundTrip builds valid Go source while recording the byte span of
// every declaration, then checks the CLI's get/bounds contract against those recorded spans.
// The expectation comes from construction, not from the parser, so this is not a self-check.
func TestGeneratedDeclarationsRoundTrip(t *testing.T) {
	for seed := int64(0); seed < 5; seed++ {
		t.Run(fmt.Sprintf("seed-%d", seed), func(t *testing.T) {
			rng := rand.New(rand.NewSource(seed))
			var b strings.Builder
			b.WriteString("package p\n\n")

			var decls []generatedDecl
			types := 0
			for i := 0; i < 4; i++ {
				if rng.Intn(2) == 0 {
					b.WriteString("// filler\n\n")
				}

				// A struct type, with the span starting at the name.
				name := fmt.Sprintf("T%d_%d", seed, types)
				types++
				b.WriteString("type ")
				start := b.Len()
				fmt.Fprintf(&b, "%s struct {\n\tA int\n\tB string\n}\n", name)
				end := b.Len() - 1 // exclude the newline
				decls = append(decls, generatedDecl{
					symbol: name,
					source: b.String()[start:end],
					start:  start,
					end:    end,
				})
				b.WriteString("\n")

				// A function, with the span starting at the func keyword.
				fn := fmt.Sprintf("F%d_%d", seed, i)
				start = b.Len()
				fmt.Fprintf(&b, "func %s() {}\n", fn)
				end = b.Len() - 1
				decls = append(decls, generatedDecl{
					symbol: fn,
					source: b.String()[start:end],
					start:  start,
					end:    end,
				})
				b.WriteString("\n")
			}

			// One method, to exercise Type.Method resolution.
			methodType := "T0_0"
			methodName := fmt.Sprintf("M%d", seed)
			start := b.Len()
			fmt.Fprintf(&b, "func (t *%s) %s() {}\n", methodType, methodName)
			end := b.Len() - 1
			decls = append(decls, generatedDecl{
				symbol: methodType + "." + methodName,
				source: b.String()[start:end],
				start:  start,
				end:    end,
			})

			src := b.String()
			dir := t.TempDir()
			path := filepath.Join(dir, "generated.go")
			if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
				t.Fatal(err)
			}

			for _, decl := range decls {
				t.Run(decl.symbol, func(t *testing.T) {
					var getBuf bytes.Buffer
					if err := usecase.Get(goast.New(), decl.symbol, path, true, &getBuf, format.Options{JSON: true}); err != nil {
						t.Fatalf("Get: %v", err)
					}
					var got struct {
						StartByte int    `json:"start_byte"`
						EndByte   int    `json:"end_byte"`
						Source    string `json:"source"`
					}
					if err := json.Unmarshal(getBuf.Bytes(), &got); err != nil {
						t.Fatalf("get JSON: %v\n%s", err, getBuf.String())
					}
					if got.StartByte != decl.start || got.EndByte != decl.end {
						t.Errorf("span = [%d,%d), want [%d,%d)", got.StartByte, got.EndByte, decl.start, decl.end)
					}
					if got.Source != decl.source {
						t.Errorf("source = %q, want %q", got.Source, decl.source)
					}

					var boundsBuf bytes.Buffer
					if err := usecase.Bounds(goast.New(), decl.symbol, path, &boundsBuf, format.Options{JSON: true}); err != nil {
						t.Fatalf("Bounds: %v", err)
					}
					var onlyBuf bytes.Buffer
					if err := usecase.Get(goast.New(), decl.symbol, path, false, &onlyBuf, format.Options{JSON: true}); err != nil {
						t.Fatalf("Get bounds-only: %v", err)
					}
					if boundsBuf.String() != onlyBuf.String() {
						t.Errorf("bounds and get --bounds-only differ:\n%s\n%s", boundsBuf.String(), onlyBuf.String())
					}
				})
			}

			var outlineBuf bytes.Buffer
			if err := usecase.Outline(goast.New(), path, usecase.DepthUnlimited, &outlineBuf, format.Options{JSON: true}); err != nil {
				t.Fatalf("Outline: %v", err)
			}
			var files []struct {
				Nodes []struct {
					Name     string `json:"name"`
					Children []struct {
						Name string `json:"name"`
					} `json:"children"`
				} `json:"nodes"`
			}
			if err := json.Unmarshal(outlineBuf.Bytes(), &files); err != nil {
				t.Fatalf("outline JSON: %v\n%s", err, outlineBuf.String())
			}
			names := map[string]bool{}
			for _, file := range files {
				for _, node := range file.Nodes {
					names[node.Name] = true
					for _, child := range node.Children {
						names[child.Name] = true
					}
				}
			}
			for _, decl := range decls {
				name := decl.symbol
				if i := strings.LastIndex(name, "."); i >= 0 {
					name = name[i+1:]
				}
				if !names[name] {
					t.Errorf("outline is missing %q", decl.symbol)
				}
			}
		})
	}
}
