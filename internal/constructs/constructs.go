// Package constructs exists so the cross-renderer corpus walk has something to walk.
//
// internal/format/agreement_test.go asserts that the human outline and the JSON outline describe
// the same declarations, by enumerating every Go file in this repository rather than by naming the
// constructs it remembered to check. An enumerable invariant is only as good as its corpus: when
// golens's own source declared no generics, the walk passed at 1,301 declarations while proving
// nothing about the field that had already broken once.
//
// So the constructs live here as real, compiled declarations rather than as fixture text inside a
// string. A fixture in a backtick literal is invisible to go/ast -- golens does not read its own
// test data as code, and neither does this walk, which is the correct behaviour and also the
// reason the corpus had to be extended rather than the test pointed at the fixtures.
//
// Nothing here is used. It compiles, it documents itself, and it is deliberately the most awkward
// Go the repository otherwise avoids: generics in every position the AST can put them, an alias
// beside a defined type with the same underlying type, an embedded field, a method on a generic
// receiver, and a grouped declaration whose members must stay independently addressable.
//
// If this file is ever deleted, TestCorpusContainsTheConstructsThatBroke fails and says so.
package constructs

// Stack is a generic type: the type parameters must survive rendering, because
// "type Stack struct" does not compile.
type Stack[T any] struct {
	// mu is embedded and renders as its own name, not as "mu sync.Mutex".
	mu
	items []T
}

// mu is embedded in Stack. It stands in for sync.Mutex, which cannot be embedded here because
// every method below takes the struct by value and copying a mutex is a vet error.
type mu struct{ held bool }

func (m *mu) acquire() { m.held = true }

// Pair is two parameters, because one is not a representative sample of the bracket handling.
type Pair[K comparable, V any] struct {
	Key K
	Val V
}

// Multi names several fields in one ast.Field: "A, B int" is two fields the parser hands
// over as one node, and taking only the first name dropped B from every outline. Nothing
// else in this repository used the form, which is exactly why 333 tests passed while the
// field list lied.
type Multi struct {
	A, B, C int
	Named   string
}

// Reader has a method set, which for an interface is the entire content of the type. It used
// to render as a bare "type Reader interface" with nothing under it.
type Reader interface {
	Read(p []byte) (int, error)
	Close() error
}

// ReadBoth embeds an interface alongside a method, so both shapes appear in one body.
//
// The embedded name IS a member of the enclosing interface. Comparable's tilde union is a
// type constraint and is NOT, and the two are indistinguishable by shape in the AST — which
// is why both live here rather than only the easy case.
type ReadBoth interface {
	Reader
	Open() error
}

// Comparable is a constraint, not a method set. Its union arrives as a single BinaryExpr with
// token.OR rather than one entry per element, which is the first thing to get wrong when
// reading this file.
type Comparable interface{ ~int | ~string }

// Alias and Defined share an underlying type and must not render identically. An alias is
// interchangeable with its target; a defined type is not, so dropping the "=" compiles and
// means something else.
type (
	Alias   = Stack[int]
	Defined Stack[int]
)

// Map is generic with two constraints.
func Map[K comparable, V any](m map[K]V, keys []K) map[K]V { return m }

// Sorted is a generic function with a constrained parameter.
func Sorted[E cmpOrdered](seq []E) []E { return seq }

// cmpOrdered is a local constraint so the file needs no import beyond sync.
type cmpOrdered interface {
	~int | ~string
}

// Push is a method on a generic receiver. The receiver renders verbatim, type parameters
// included, or "func (s *Stack) Push(v T)" describes a type that does not exist.
//
// The embedded mu is used so the embedding is real rather than decorative: an embedded field with
// no reference to it is not a field the outline can be asked to render correctly.
func (s *Stack[T]) Push(v T) {
	s.acquire()
	s.items = append(s.items, v)
}

// Len is a method on a pointer receiver of a generic type.
func (s *Stack[T]) Len() int { return len(s.items) }

// Count reads through an alias, so the file exercises an alias as a use as well as a
// declaration.
func Count(a *Alias) int { return a.Len() }

// Grouped is a grouped declaration. Each name needs its own bounds, or editing one
// member would overwrite the others, so the outline must render all three.
const (
	GroupedA = 1
	GroupedB = 2
	GroupedC = 3
)

// Vars likewise.
var (
	VarA = "a"
	VarB = "b"
)

// Untyped has no type or value, so Detail is empty and the head is just the name.
const Untyped = iota

// Value is an initialised var: "var Value = 1" is the whole declaration.
var Value = 1
