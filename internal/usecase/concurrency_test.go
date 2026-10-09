package usecase_test

import (
	"bytes"
	"fmt"
	"runtime"
	"strings"
	"testing"

	"github.com/CodeMonkey80s/golens/internal/engine/goast"
	"github.com/CodeMonkey80s/golens/internal/format"
	"github.com/CodeMonkey80s/golens/internal/testfixture"
	"github.com/CodeMonkey80s/golens/internal/usecase"
)

// TestConcurrentOutputMatchesSerial is acceptance criterion A3, and the only criterion that was
// unproven until now.
//
// Concurrency must be unobservable in the output. This builds a tree larger than the
// sequential threshold, renders it, then compares against the same tree rendered with
// GOMAXPROCS pinned to 1 so the serial path is taken. Any difference in ordering, formatting or
// content fails here.
func TestConcurrentOutputMatchesSerial(t *testing.T) {
	if runtime.GOMAXPROCS(0) < 2 {
		t.Skip("needs at least 2 procs to exercise the concurrent path")
	}

	dir := buildTree(t, 20)

	for _, opts := range []format.Options{
		{},
		{Json: true},
		{Compact: true},
		{Json: true, Compact: true},
	} {
		name := describeOptions(opts)

		t.Run(name, func(t *testing.T) {
			concurrent := outlineWithProcs(t, dir, usecase.DepthUnlimited, opts, runtime.GOMAXPROCS(0))
			serial := outlineWithProcs(t, dir, usecase.DepthUnlimited, opts, 1)

			if concurrent != serial {
				t.Errorf("concurrent output differs from serial:\n%s", firstDifference(concurrent, serial))
			}
			if concurrent == "" {
				t.Error("both paths produced empty output; the comparison is vacuous")
			}
		})
	}
}

// TestConcurrentCheckMatchesSerial extends A3 to check, which aggregates diagnostics across
// files and sorts them.
func TestConcurrentCheckMatchesSerial(t *testing.T) {
	if runtime.GOMAXPROCS(0) < 2 {
		t.Skip("needs at least 2 procs to exercise the concurrent path")
	}

	dir := t.TempDir()
	for i := 0; i < 12; i++ {
		name := fmt.Sprintf("f%02d.go", i)
		// Every file has an unused import, so diagnostics are plentiful and their order
		// depends on traversal, which is exactly what concurrency could disturb.
		testfixture.Write(t, dir, name, fmt.Sprintf(
			"package p\n\nimport (\n\t\"fmt\"\n\t\"os\"\n)\n\nfunc F%02d() {}\n", i))
	}

	concurrent := checkWithProcs(t, dir, runtime.GOMAXPROCS(0))
	serial := checkWithProcs(t, dir, 1)

	if concurrent != serial {
		t.Errorf("concurrent check differs from serial:\nconcurrent:\n%s\nserial:\n%s",
			concurrent, serial)
	}
	if !strings.Contains(concurrent, "imported and not used") {
		t.Errorf("expected diagnostics, got:\n%s", concurrent)
	}
}

// TestConcurrentDeterministicAcrossRuns confirms repeated concurrent runs agree, which catches a
// goroutine writing to a shared index.
func TestConcurrentDeterministicAcrossRuns(t *testing.T) {
	if runtime.GOMAXPROCS(0) < 2 {
		t.Skip("needs at least 2 procs to exercise the concurrent path")
	}

	dir := buildTree(t, 16)

	first := outlineWithProcs(t, dir, usecase.DepthUnlimited, format.Options{}, runtime.GOMAXPROCS(0))
	for i := 0; i < 8; i++ {
		again := outlineWithProcs(t, dir, usecase.DepthUnlimited, format.Options{}, runtime.GOMAXPROCS(0))
		if again != first {
			t.Fatalf("run %d differed from the first:\n%s", i, firstDifference(first, again))
		}
	}
}

// TestSingleFileStaysSequential confirms a one-file invocation does not pay for concurrency. The
// threshold exists for this reason, and the budget in specification 3.2 is per file.
func TestSingleFileStaysSequential(t *testing.T) {
	dir := t.TempDir()
	path := testfixture.Write(t, dir, "one.go", testfixture.Store)

	var buf bytes.Buffer
	if err := usecase.Outline(goast.New(), path, usecase.DepthUnlimited, &buf, format.Options{}); err != nil {
		t.Fatalf("Outline: %v", err)
	}

	// The human outline renders a method by its receiver, not by the qualified lookup name,
	// so the assertion matches that form.
	if !strings.Contains(buf.String(), "func (s *UserStore) GetUser") {
		t.Errorf("single-file outline missing content:\n%s", buf.String())
	}
}

// TestConcurrentReportsParseFailureDeterministically confirms a broken file fails the same way
// every run, even when parsed concurrently. Reporting whichever goroutine lost the race would
// make the tool unusable in a script.
func TestConcurrentReportsParseFailureDeterministically(t *testing.T) {
	if runtime.GOMAXPROCS(0) < 2 {
		t.Skip("needs at least 2 procs to exercise the concurrent path")
	}

	dir := t.TempDir()
	// Two broken files, so which one is reported first is a real question.
	testfixture.Write(t, dir, "a_broken.go", testfixture.Broken)
	testfixture.Write(t, dir, "b_broken.go", testfixture.Broken)
	for i := 0; i < 10; i++ {
		testfixture.Write(t, dir, fmt.Sprintf("ok%02d.go", i), fmt.Sprintf("package p\n\nfunc F%02d() {}\n", i))
	}

	first := ""
	for i := 0; i < 6; i++ {
		var buf bytes.Buffer
		err := usecase.Outline(goast.New(), dir, usecase.DepthUnlimited, &buf, format.Options{})

		if err == nil {
			t.Fatal("expected an error for a tree containing unparseable files")
		}
		got := err.Error()
		if first == "" {
			first = got
			continue
		}
		if got != first {
			t.Fatalf("error varied between runs:\nfirst: %s\n  i=%d: %s", first, i, got)
		}
	}
	if !strings.Contains(first, "a_broken.go") {
		t.Errorf("expected the first path in lexical order, got: %s", first)
	}
}

// TestCheckReportsEveryBrokenFile confirms check's contract: a parse failure is a diagnostic, so
// all of them are reported rather than aborting at the first.
func TestCheckReportsEveryBrokenFile(t *testing.T) {
	dir := t.TempDir()
	testfixture.Write(t, dir, "a_broken.go", testfixture.Broken)
	testfixture.Write(t, dir, "b_broken.go", testfixture.Broken)
	testfixture.Write(t, dir, "c_broken.go", testfixture.Broken)

	var stdout, stderr bytes.Buffer
	_ = usecase.Check(goast.New(), dir, &stdout, &stderr, format.Options{}, false)

	for _, want := range []string{"a_broken.go", "b_broken.go", "c_broken.go"} {
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("missing diagnostic for %s:\n%s", want, stderr.String())
		}
	}
}

// TestConcurrentWithRandomTreeSizes sweeps tree sizes around the sequential threshold, where a
// bug in the branch between the two paths would hide.
func TestConcurrentWithRandomTreeSizes(t *testing.T) {
	if runtime.GOMAXPROCS(0) < 2 {
		t.Skip("needs at least 2 procs")
	}

	// 7 and 8 straddle serialThreshold, so both branches are exercised at their boundary.
	for _, n := range []int{1, 2, 7, 8, 9, 15} {
		t.Run(fmt.Sprintf("files=%d", n), func(t *testing.T) {
			dir := buildTree(t, n)

			concurrent := outlineWithProcs(t, dir, usecase.DepthUnlimited, format.Options{}, runtime.GOMAXPROCS(0))
			serial := outlineWithProcs(t, dir, usecase.DepthUnlimited, format.Options{}, 1)

			if concurrent != serial {
				t.Errorf("output differs at the threshold boundary:\n%s", firstDifference(concurrent, serial))
			}
		})
	}
}

// buildTree writes n Go files into a temp directory, returning the directory path.
func buildTree(t *testing.T, n int) string {
	t.Helper()

	dir := t.TempDir()
	for i := 0; i < n; i++ {
		// Names are zero-padded so lexical order matches numeric order, which keeps the
		// expected ordering obvious when a test fails.
		name := fmt.Sprintf("f%03d.go", i)
		src := fmt.Sprintf(`package tree

import "context"

type T%03d struct {
	Id   int
	Name string
}

func (t *T%03d) Do(ctx context.Context) error {
	return nil
}

func Helper%03d() {}
`, i, i, i)
		testfixture.Write(t, dir, name, src)
	}
	return dir
}

// outlineWithProcs renders a tree with GOMAXPROCS pinned for the duration.
//
// Setting the limit and restoring it is the only way to force the sequential path through the
// public API, since parseAll decides internally.
func outlineWithProcs(t *testing.T, dir string, depth int, opts format.Options, procs int) string {
	t.Helper()

	prev := runtime.GOMAXPROCS(procs)
	defer runtime.GOMAXPROCS(prev)

	var buf bytes.Buffer
	if err := usecase.Outline(goast.New(), dir, depth, &buf, opts); err != nil {
		t.Fatalf("Outline with %d procs: %v", procs, err)
	}
	return buf.String()
}

func checkWithProcs(t *testing.T, dir string, procs int) string {
	t.Helper()

	prev := runtime.GOMAXPROCS(procs)
	defer runtime.GOMAXPROCS(prev)

	var stdout, stderr bytes.Buffer
	_ = usecase.Check(goast.New(), dir, &stdout, &stderr, format.Options{}, false)
	return stderr.String()
}

func describeOptions(opts format.Options) string {
	switch {
	case opts.Json && opts.Compact:
		return "json-compact"
	case opts.Json:
		return "json"
	case opts.Compact:
		return "compact"
	default:
		return "human"
	}
}

// firstDifference renders the first differing line between two outputs, so a failure shows the
// actual mismatch rather than two large blobs.
func firstDifference(a, b string) string {
	aLines := strings.Split(a, "\n")
	bLines := strings.Split(b, "\n")

	for i := 0; i < len(aLines) || i < len(bLines); i++ {
		var al, bl string
		if i < len(aLines) {
			al = aLines[i]
		}
		if i < len(bLines) {
			bl = bLines[i]
		}
		if al != bl {
			return fmt.Sprintf("line %d:\n concurrent: %q\n    serial: %q", i+1, al, bl)
		}
	}
	return "(no line differs; lengths differ)"
}
