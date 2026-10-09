package usecase

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/CodeMonkey80s/golens/internal/engine/goast"
	"github.com/CodeMonkey80s/golens/internal/format"
)

// This file benchmarks the four commands end to end.
//
// The engine benchmarks in engine/goast measure parse alone, which is the cheapest part of every
// command. What an agent actually pays for is the whole path: parse, build the domain model,
// render. A regression in rendering is invisible to those benchmarks and would show up only as
// slower sessions, so each command is measured here across the shapes it is given.
//
// Two inputs are used. The largest Go file in this repository is a realistic shape and is the
// input the 5 ms parse budget in specification 3.2 was written against. The whole package
// directory is the other, because that is what an agent asks when it does not yet know which
// file holds the symbol.

// benchDoc parses the largest Go file in this package's directory tree once per run, so the
// benchmarks measure the command rather than the read.
//
// The parse is shared through a sync.Once: every benchmark needs the same document, and parsing
// it per benchmark would dominate the numbers.
var benchDoc = sync.OnceValues(func() (string, []byte) {
	paths := largestGoFiles()
	if len(paths) == 0 {
		return "", nil
	}
	source, err := os.ReadFile(paths[0])
	if err != nil {
		return "", nil
	}
	return paths[0], source
})

// largestGoFiles lists the Go files in this package's directory, largest first.
//
// It walks only this package rather than the whole module, so the input is stable and the
// benchmark does not drift as unrelated packages grow.
func largestGoFiles() []string {
	entries, err := os.ReadDir(".")
	if err != nil {
		return nil
	}

	type entry struct {
		path string
		size int64
	}
	var found []entry
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".go" {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		found = append(found, entry{filepath.Join(".", e.Name()), info.Size()})
	}
	sort.Slice(found, func(i, j int) bool { return found[i].size > found[j].size })

	paths := make([]string, 0, len(found))
	for _, e := range found {
		paths = append(paths, e.path)
	}
	return paths
}

// BenchmarkOutlineHuman measures the human outline for one file, which is the form an agent
// reads.
func BenchmarkOutlineHuman(b *testing.B) {
	engine := goast.New()
	path, _ := benchDoc()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var out bytes.Buffer
		if err := Outline(engine, path, DepthUnlimited, &out, format.Options{}); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkOutlineJson measures the JSON outline, which is the form a machine consumer parses.
func BenchmarkOutlineJson(b *testing.B) {
	engine := goast.New()
	path, _ := benchDoc()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var out bytes.Buffer
		if err := Outline(engine, path, DepthUnlimited, &out, format.Options{Json: true}); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkOutlineCompact measures the compact outline, which trades structure for token count.
func BenchmarkOutlineCompact(b *testing.B) {
	engine := goast.New()
	path, _ := benchDoc()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var out bytes.Buffer
		if err := Outline(engine, path, DepthUnlimited, &out, format.Options{Compact: true}); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkOutlineDirectory measures a whole-directory outline, which is the path an agent takes
// when it does not yet know which file holds the symbol.
func BenchmarkOutlineDirectory(b *testing.B) {
	engine := goast.New()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var out bytes.Buffer
		if err := Outline(engine, ".", DepthUnlimited, &out, format.Options{}); err != nil {
			b.Fatal(err)
		}
	}
}

// benchDeclFile and benchSymbol name a real declaration to extract.
//
// The largest Go file in this package is a test file, whose only top-level declarations are
// test functions, so get and bounds are measured against the largest non-test file instead.
// Deriving a symbol from the parsed document would cost more than the benchmarks measure.
var benchDeclFile = sync.OnceValue(func() string {
	for _, path := range largestGoFiles() {
		// Test files declare test functions rather than the code under test.
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		return path
	}
	return "."
})

// BenchmarkGet measures extraction of one symbol, which is the cheapest read an agent makes.
func BenchmarkGet(b *testing.B) {
	engine := goast.New()
	path := benchDeclFile()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var out bytes.Buffer
		if err := Get(engine, "Check", path, true, &out, format.Options{}); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkBounds measures the bounds lookup, which is the step that precedes every edit.
func BenchmarkBounds(b *testing.B) {
	engine := goast.New()
	path := benchDeclFile()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var out bytes.Buffer
		if err := Bounds(engine, "Check", path, &out, format.Options{}); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkCheck measures the syntax and import checks over a directory, which is the validation
// step after an edit.
func BenchmarkCheck(b *testing.B) {
	engine := goast.New()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var stdout, stderr bytes.Buffer
		_ = Check(engine, ".", &stdout, &stderr, format.Options{}, false)
	}
}

// BenchmarkCheckNaming measures the naming check, which is the other mode of check.
func BenchmarkCheckNaming(b *testing.B) {
	engine := goast.New()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var stdout, stderr bytes.Buffer
		_ = Check(engine, ".", &stdout, &stderr, format.Options{}, true)
	}
}
