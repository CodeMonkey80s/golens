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

// TestPackageQualifiedQueryIsToldWhatFormResolves covers `golens get domain.Node .`.
//
// Go namespaces by package and the path argument already selects one, so prefixing the
// package name is the reasonable guess — but Lookup matches a bare name or a qualified
// method name and nothing else, so the query came back as "symbol not found": the same
// message a genuinely absent symbol gets. A caller cannot tell "you used a form this tool
// does not accept" from "there is no such declaration", and it acts on the second by
// renaming or deleting code.
//
// The fix is the message, not the lookup: package-qualified resolution is a separate
// specification change, and the honest answer today is to name the form that resolves.
func TestPackageQualifiedQueryIsToldWhatFormResolves(t *testing.T) {
	dir := t.TempDir()
	testfixture.Write(t, dir, "ok.go", "package domain\n\ntype Node struct{}\n\nfunc (n Node) Head() string { return \"\" }\n")

	var out bytes.Buffer
	err := usecase.Get(goast.New(), "domain.Node", dir, true, &out, format.Options{})

	var pq usecase.PackageQualifiedSymbol
	if !errors.As(err, &pq) {
		t.Fatalf("err = %v (%T), want PackageQualifiedSymbol", err, err)
	}
	if !strings.Contains(err.Error(), `symbol "domain.Node" not found`) {
		t.Errorf("the miss no longer says what was asked for: %v", err)
	}
	if !strings.Contains(pq.Advice(), `pass "Node"`) {
		t.Errorf("advice does not name the form that resolves: %q", pq.Advice())
	}
	if !strings.Contains(pq.Advice(), "Type.Method") {
		t.Errorf("advice does not name the dotted form that does resolve: %q", pq.Advice())
	}
	if out.Len() != 0 {
		t.Errorf("a miss wrote to stdout:\n%s", out.String())
	}

	// The advice must be reachable through the interface the cli layer uses, or it prints
	// nothing and the fix appears not to have worked — the nesting defect recorded in
	// TestAdviceIsPrintedWithoutACandidateList.
	var advised interface{ Advice() string } = pq
	if advised.Advice() == "" {
		t.Error("PackageQualifiedSymbol exposes no advice through the cli's interface")
	}
}

// TestPackageQualifiedQueryStillReportsAnIncompleteSearch: the query named a package, so
// the message must not read as though every file were parsed.
func TestPackageQualifiedQueryStillReportsAnIncompleteSearch(t *testing.T) {
	dir := t.TempDir()
	testfixture.Write(t, dir, "ok.go", "package domain\n\ntype Node struct{}\n")
	testfixture.Write(t, dir, "broken.go", "package domain\n\nfunc B( {\n")

	var out bytes.Buffer
	err := usecase.Get(goast.New(), "domain.Node", dir, true, &out, format.Options{})

	var pq usecase.PackageQualifiedSymbol
	if !errors.As(err, &pq) {
		t.Fatalf("err = %v (%T), want PackageQualifiedSymbol", err, err)
	}
	if !strings.Contains(err.Error(), "1 of 2 files could not be parsed") {
		t.Errorf("a partial search answered like a complete one: %v", err)
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
		var pq usecase.PackageQualifiedSymbol
		if errors.As(err, &pq) {
			t.Errorf("Node.Frob was read as a package qualifier: %v", err)
		}
	})
}
