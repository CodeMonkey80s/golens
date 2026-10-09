package goast

import (
	"testing"

	"github.com/CodeMonkey80s/golens/internal/domain"
)

// mustLookup resolves a single symbol and fails the test if the name is absent or
// ambiguous.
//
// Tests use this instead of Document.Lookup directly because the production callers must
// handle ambiguity with exit code 2, while a test that wants one specific symbol can
// assert uniqueness up front and read much more clearly.
func mustLookup(t *testing.T, doc domain.Document, name string) domain.Symbol {
	t.Helper()

	matches := doc.Lookup(name)
	switch len(matches) {
	case 0:
		t.Fatalf("symbol %q not found; have %v", name, names(doc))
		return domain.Symbol{}
	case 1:
		return matches[0]
	default:
		found := make([]string, 0, len(matches))
		for _, m := range matches {
			found = append(found, m.QualifiedName)
		}
		t.Fatalf("symbol %q is ambiguous, matches %v", name, found)
		return domain.Symbol{}
	}
}

// lookupPresent reports whether name resolves to at least one symbol.
func lookupPresent(doc domain.Document, name string) bool {
	return len(doc.Lookup(name)) > 0
}
