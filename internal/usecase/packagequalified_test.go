package usecase_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/CodeMonkey80s/golens/internal/engine/goast"
	"github.com/CodeMonkey80s/golens/internal/format"
	"github.com/CodeMonkey80s/golens/internal/testfixture"
	"github.com/CodeMonkey80s/golens/internal/usecase"
)

// TestPackageQualifiedQueryResolves covers `golens get domain.Node .`.
//
// Package-qualified lookup now resolves: `p.Func` finds `Func` in package `p`.
// This is the v0.3 feature that replaced the old "package prefix is unsupported" error.
func TestPackageQualifiedQueryResolves(t *testing.T) {
	dir := t.TempDir()
	testfixture.Write(t, dir, "ok.go", "package domain\n\ntype Node struct{}\n\nfunc (n Node) Head() string { return \"\" }\n")

	var out bytes.Buffer
	err := usecase.Get(goast.New(), "domain.Node", dir, true, &out, format.Options{})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !strings.Contains(out.String(), "Node struct") {
		t.Errorf("expected to resolve Node in package domain:\n%s", out.String())
	}

	// The form that resolves for a method within a type is still Type.Method
	// and should still work.
	var out2 bytes.Buffer
	if err := usecase.Get(goast.New(), "Node.Head", dir, true, &out2, format.Options{}); err != nil {
		t.Fatalf("Get Node.Head: %v", err)
	}
}

// TestPackageQualifiedQueryReportsIncompleteSearch covers the case where the package-qualified
// lookup succeeds but some files in the search could not be parsed. The skipped count is
// reported in the error (exit 4), while the symbol is still returned (exit 0 would be wrong
// because the search was incomplete).
func TestPackageQualifiedQueryReportsIncompleteSearch(t *testing.T) {
	dir := t.TempDir()
	testfixture.Write(t, dir, "ok.go", "package domain\n\ntype Node struct{}\n")
	testfixture.Write(t, dir, "broken.go", "package domain\n\nfunc B( {\n")

	var out bytes.Buffer
	err := usecase.Get(goast.New(), "domain.Node", dir, true, &out, format.Options{})

	// The symbol resolves, but the run still exits 4 due to skipped files.
	var skipped usecase.SkippedFilesError
	if errors.As(err, &skipped) {
		if len(skipped.Failures) != 1 {
			t.Errorf("expected 1 skipped file, got %d", len(skipped.Failures))
		}
	} else {
		t.Errorf("expected SkippedFilesError, got %T: %v", err, err)
	}
	if !strings.Contains(out.String(), "Node struct") {
		t.Errorf("expected to resolve Node in package domain:\n%s", out.String())
	}
}

// TestBareAndMethodQueriesAreUnaffected guards the branch the new one sits beside: a query
// that is not package-prefixed must still produce the plain miss, and one that is must not
// fire when the prefix happens to be a package name in another directory.
func TestBareAndMethodQueriesAreUnaffected(t *testing.T) {
	dir := t.TempDir()
	testfixture.Write(t, dir, "a.go", "package domain\n\ntype Node struct{}\n\nfunc (n Node) Head() {}\n")

	t.Run("a bare miss stays the plain not-found", func(t *testing.T) {
		var out bytes.Buffer
		err := usecase.Get(goast.New(), "Nope", dir, true, &out, format.Options{})
		if err.Error() != `symbol "Nope" not found` {
			t.Errorf("err = %q, want the plain not-found message", err.Error())
		}
	})

	t.Run("a dotted miss on a type still resolves", func(t *testing.T) {
		// Node.Head is a method within a type, which is the form that does resolve, so it
		// must not be reclassified as a package qualifier.
		var out bytes.Buffer
		if err := usecase.Get(goast.New(), "Node.Head", dir, true, &out, format.Options{}); err != nil {
			t.Fatalf("Get: %v", err)
		}
	})

	t.Run("a dotted miss on an absent method is not advice-worthy", func(t *testing.T) {
		var out bytes.Buffer
		err := usecase.Get(goast.New(), "Node.Frob", dir, true, &out, format.Options{})
		var pq usecase.NotFoundInPackage
		if errors.As(err, &pq) {
			t.Errorf("Node.Frob was read as a package qualifier: %v", err)
		}
	})

	t.Run("a miss in a real package names the package", func(t *testing.T) {
		var out bytes.Buffer
		err := usecase.Get(goast.New(), "domain.Nope", dir, true, &out, format.Options{})

		var pq usecase.NotFoundInPackage
		if !errors.As(err, &pq) {
			t.Fatalf("err = %T (%v), want NotFoundInPackage", err, err)
		}
		if pq.Package != "domain" || pq.Name != "Nope" {
			t.Errorf("package = %q, name = %q; want domain, Nope", pq.Package, pq.Name)
		}
		if _, ok := err.(interface{ Advice() string }); ok {
			t.Error("NotFoundInPackage offers advice; a missing symbol has no form that resolves")
		}
	})
}
