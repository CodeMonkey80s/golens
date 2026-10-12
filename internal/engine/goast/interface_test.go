package goast_test

import (
	"strings"
	"testing"

	"github.com/CodeMonkey80s/golens/internal/domain"
	"github.com/CodeMonkey80s/golens/internal/engine/goast"
)

// TestInterfaceMethodSetIsListed covers the gap where an interface rendered as a bare
// "type I interface".
//
// For a struct, the fields are the content of the type. For an interface, the method set is
// the entire content, so reporting nothing told a caller that a type whose whole purpose is
// its method list was empty. The specification says depth 2 emits "struct fields and method
// sets", which is what a Go programmer reads it to mean.
func TestInterfaceMethodSetIsListed(t *testing.T) {
	src := `package p

type User struct{ ID int }

type Store interface {
	Get(id int) (*User, error)
	Close() error
}
`

	doc, err := goast.New().ParseSource("i.go", []byte(src))
	if err != nil {
		t.Fatalf("ParseSource: %v", err)
	}

	store := findNode(doc.Nodes, "Store")
	if store == nil {
		t.Fatal("no node for Store")
	}

	var names []string
	for _, c := range store.Children {
		names = append(names, c.Name)
	}
	if strings.Join(names, ",") != "Get,Close" {
		t.Errorf("methods = %v, want [Get Close]; an interface's whole content was omitted", names)
	}

	// The Detail is the parameter and result list, and it must not carry the "func"
	// keyword. Printing an *ast.FuncType directly does, which renders text that is a syntax
	// error inside an interface body -- so a caller pasting the outline back gets code that
	// does not compile.
	get := store.Children[0]
	if !strings.HasPrefix(get.Detail, "(") {
		t.Errorf("Get detail = %q, want it to start with the parameter list", get.Detail)
	}
	if strings.Contains(get.Detail, "func") {
		t.Errorf("Get detail = %q, want no %q keyword: it is a syntax error in an interface", get.Detail, "func")
	}
}

// TestEmbeddedInterfaceIsListedAndUnionsAreNot separates the two non-method entries that can
// appear in an interface body, which look alike in the AST but mean opposite things.
func TestEmbeddedInterfaceIsListedAndUnionsAreNot(t *testing.T) {
	src := `package p

type Reader interface{ Read() error }

type Both interface {
	Reader
	Open() error
}

type Cmp interface{ ~int | ~string }

type Plain interface{ int | string }

type Empty interface{}
`

	doc, err := goast.New().ParseSource("ei.go", []byte(src))
	if err != nil {
		t.Fatalf("ParseSource: %v", err)
	}

	t.Run("an embedded interface is a method of the enclosing one", func(t *testing.T) {
		both := findNode(doc.Nodes, "Both")
		if both == nil {
			t.Fatal("no node for Both")
		}
		var names []string
		for _, c := range both.Children {
			names = append(names, c.Name)
		}
		if strings.Join(names, ",") != "Reader,Open" {
			t.Errorf("members = %v, want [Reader Open]", names)
		}
	})

	// A union is a single *ast.BinaryExpr with token.OR, not one entry per element -- the
	// first thing to get wrong, since it looks like a list. Rendering it as a member would
	// put "~int | ~string" in the outline as though the interface declared it.
	t.Run("a type union is not a method", func(t *testing.T) {
		for _, name := range []string{"Cmp", "Plain", "Empty"} {
			n := findNode(doc.Nodes, name)
			if n == nil {
				t.Fatalf("no node for %s", name)
			}
			if len(n.Children) != 0 {
				var got []string
				for _, c := range n.Children {
					got = append(got, c.Name)
				}
				t.Errorf("%s lists %v; a constraint has no method set", name, got)
			}
		}
	})

	t.Run("an interface with methods still lists them", func(t *testing.T) {
		r := findNode(doc.Nodes, "Reader")
		if r == nil || len(r.Children) != 1 || r.Children[0].Name != "Read" {
			t.Errorf("Reader members wrong: %+v", r)
		}
	})
}

// TestInterfaceMethodsHaveTheirOwnBounds applies the byte invariant at member level, the same
// way TestMultiNameFieldsGetTheirOwnBounds does for struct fields.
func TestInterfaceMethodsHaveTheirOwnBounds(t *testing.T) {
	src := `package p

type I interface {
	Get(id int) error
	Close() error
}
`

	doc, err := goast.New().ParseSource("i.go", []byte(src))
	if err != nil {
		t.Fatalf("ParseSource: %v", err)
	}

	i := findNode(doc.Nodes, "I")
	if i == nil || len(i.Children) != 2 {
		t.Fatalf("methods = %d, want 2", len(i.Children))
	}

	for _, c := range i.Children {
		if got := c.Bounds.Source(doc.Source); got != c.Name {
			t.Errorf("method %s has bounds selecting %q, want %q", c.Name, got, c.Name)
		}
	}
	if i.Children[0].Bounds.StartByte >= i.Children[1].Bounds.StartByte {
		t.Error("methods are not in source order")
	}
}

// TestNonStructTypeMembersStayEmpty guards the branches the interface change could have
// opened by accident: a named map, slice, chan or func type has no members to list.
func TestNonStructTypeMembersStayEmpty(t *testing.T) {
	src := `package p

type M map[string]int
type S []byte
type C chan int
type F func(a int) error
type P *int
`

	doc, err := goast.New().ParseSource("named.go", []byte(src))
	if err != nil {
		t.Fatalf("ParseSource: %v", err)
	}

	for _, name := range []string{"M", "S", "C", "F", "P"} {
		n := findNode(doc.Nodes, name)
		if n == nil {
			t.Fatalf("no node for %s", name)
		}
		if len(n.Children) != 0 {
			t.Errorf("%s lists %d members; a named non-struct type has none to list", name, len(n.Children))
		}
	}
}

// findNode locates a node by name anywhere in the tree.
func findNode(nodes []domain.Node, name string) *domain.Node {
	for i := range nodes {
		if nodes[i].Name == name {
			return &nodes[i]
		}
		if child := findNode(nodes[i].Children, name); child != nil {
			return child
		}
	}
	return nil
}
