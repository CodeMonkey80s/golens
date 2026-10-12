package goast_test

import (
	"strings"
	"testing"

	"github.com/CodeMonkey80s/golens/internal/engine/goast"
)

// TestMultiNameStructFieldsAreNotDropped covers the case where "A, B int" is a single
// ast.Field holding two names, and emitting one node for it left B out of the outline
// entirely.
//
// Every render was wrong in the same way — human, JSON and compact alike — so the defect was
// not a formatting mistake but missing data, and the field list a caller reads before
// editing a struct silently disagreed with the struct. Nothing in this repository used the
// form until internal/constructs gained it, which is how 333 tests passed while the field
// count was wrong.
//
// It is also the third instance of the same class: the alias "=" and the dropped is_alias
// both worked by omission, and both shipped because the corpus happened not to contain the
// construct.
func TestMultiNameStructFieldsAreNotDropped(t *testing.T) {
	src := `package p

type S struct {
	A, B int
	C    string
	mu
}

type mu struct{ held bool }
`

	doc, err := goast.New().ParseSource("multi.go", []byte(src))
	if err != nil {
		t.Fatalf("ParseSource: %v", err)
	}

	s := findNode(doc.Nodes, "S")
	if s == nil {
		t.Fatal("no node for S")
	}

	var names []string
	for _, child := range s.Children {
		names = append(names, child.Name)
	}

	want := []string{"A", "B", "C", "mu"}
	if len(names) != len(want) {
		t.Fatalf("fields = %v, want %v -- %d field(s) missing from the outline", names, want, len(want)-len(names))
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("fields = %v, want %v (order and identity both matter)", names, want)
		}
	}
}

// TestMultiNameFieldsGetTheirOwnBounds covers the second half of the same defect.
//
// With one node for the whole ast.Field, A and B would have shared the span "A, B int".
// Two fields with identical bounds is the same addressability problem a grouped const has,
// and it is why each name carries its own position here.
func TestMultiNameFieldsGetTheirOwnBounds(t *testing.T) {
	src := `package p

type S struct {
	A, B int
}
`

	doc, err := goast.New().ParseSource("multi.go", []byte(src))
	if err != nil {
		t.Fatalf("ParseSource: %v", err)
	}

	s := findNode(doc.Nodes, "S")
	if s == nil {
		t.Fatal("no node for S")
	}
	if len(s.Children) != 2 {
		t.Fatalf("fields = %d, want 2", len(s.Children))
	}

	a, b := s.Children[0], s.Children[1]
	if a.Bounds.StartByte == b.Bounds.StartByte {
		t.Errorf("A and B share start_byte %d; editing one would overwrite the other",
			a.Bounds.StartByte)
	}
	if a.Bounds.StartByte >= b.Bounds.StartByte {
		t.Errorf("bounds are not in source order: A at %d, B at %d", a.Bounds.StartByte, b.Bounds.StartByte)
	}

	// Each span must select exactly its own name from the real source, which is the byte
	// invariant applied at field level rather than declared.
	for _, c := range s.Children {
		got := c.Bounds.Source(doc.Source)
		if got != c.Name {
			t.Errorf("field %s has bounds selecting %q, want %q", c.Name, got, c.Name)
		}
	}
}

// TestEmbeddedFieldIsStillOneNode guards the branch the rewrite touched.
//
// An embedded field has no names, so it is not a multi-name case; it is its own declaration
// and must render once, as the type expression.
func TestEmbeddedFieldIsStillOneNode(t *testing.T) {
	src := `package p

type S struct {
	mu
	A, B int
}
`

	doc, err := goast.New().ParseSource("embed.go", []byte(src))
	if err != nil {
		t.Fatalf("ParseSource: %v", err)
	}

	s := findNode(doc.Nodes, "S")
	if s == nil {
		t.Fatal("no node for S")
	}

	var names []string
	for _, c := range s.Children {
		names = append(names, c.Name)
	}
	if strings.Join(names, ",") != "mu,A,B" {
		t.Errorf("fields = %v, want [mu A B]", names)
	}

	// The embedded field has no Detail: repeating it would render "mu mu", which is not
	// valid Go.
	if mu := s.Children[0]; mu.Detail != "" {
		t.Errorf("embedded field Detail = %q, want empty", mu.Detail)
	}
}
