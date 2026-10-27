package usecase_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/CodeMonkey80s/golens/internal/engine/goast"
	"github.com/CodeMonkey80s/golens/internal/format"
	"github.com/CodeMonkey80s/golens/internal/testfixture"
	"github.com/CodeMonkey80s/golens/internal/usecase"
)

// TestPackageLevelNameWinsOverMethod covers a legal Go shape that used to be unresolvable:
// a type and a method on another type sharing one bare name. `get Symbol` could only answer
// "ambiguous" even though the method is not in package scope.
func TestPackageLevelNameWinsOverMethod(t *testing.T) {
	dir := t.TempDir()
	path := testfixture.Write(t, dir, "a.go", `package p

type Symbol struct{}

type Document struct{}

func (d Document) Symbol() {}
`)

	var out bytes.Buffer
	if err := usecase.Get(goast.New(), "Symbol", path, true, &out, format.Options{}); err != nil {
		t.Fatalf("Get Symbol: %v", err)
	}
	if !strings.Contains(out.String(), "Symbol struct") || strings.Contains(out.String(), "func (d Document)") {
		t.Errorf("bare Symbol did not resolve to the package-level type:\n%s", out.String())
	}

	out.Reset()
	if err := usecase.Get(goast.New(), "Document.Symbol", path, true, &out, format.Options{}); err != nil {
		t.Fatalf("Get Document.Symbol: %v", err)
	}
	if !strings.Contains(out.String(), "func (d Document) Symbol()") {
		t.Errorf("qualified name did not resolve to the method:\n%s", out.String())
	}

	// The package-qualified form follows the same rule.
	out.Reset()
	if err := usecase.Get(goast.New(), "p.Symbol", dir, true, &out, format.Options{}); err != nil {
		t.Fatalf("Get p.Symbol: %v", err)
	}
	if !strings.Contains(out.String(), "Symbol struct") {
		t.Errorf("p.Symbol did not resolve to the type:\n%s", out.String())
	}
}
