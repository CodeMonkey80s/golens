package usecase

// This file holds the lexical helpers used by the unused-import check. They scan raw
// bytes rather than an AST because the engine in S1 exposes nodes, not the underlying
// ast.File; see unusedImports for why that trade was made.

// isIdentStart reports whether b can begin a Go identifier.
//
// Bytes at or above utf8.RuneSelf are treated as identifier bytes unconditionally: a
// multi-byte rune cannot be confused with punctuation, and over-approximating here only
// ever adds a word to the used set, which is the safe direction for an import check.
func isIdentStart(b byte) bool {
	return b == '_' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || b >= utf8Self
}

// isIdentPart reports whether b can continue a Go identifier.
func isIdentPart(b byte) bool { return isIdentStart(b) || (b >= '0' && b <= '9') }

const utf8Self = 0x80

// isIdentifier reports whether s is shaped like a Go identifier.
//
// It decides whether a derived import name could ever appear in source. "go-runewidth" has
// a dash and cannot; "runewidth" can. Bytes at or above utf8Self are accepted for the same
// reason isIdentStart accepts them: a multi-byte rune cannot be the punctuation that
// disqualifies a name.
func isIdentifier(s string) bool {
	if s == "" || !isIdentStart(s[0]) {
		return false
	}
	for i := 1; i < len(s); i++ {
		if !isIdentPart(s[i]) {
			return false
		}
	}
	return true
}

// atWordStart reports whether index i begins a word rather than sitting inside one. It
// exists because the selector scan needs to avoid re-reading the tail of an identifier.
func atWordStart(src []byte, i int) bool {
	return i == 0 || !isIdentPart(src[i-1])
}

// identEnd returns the index one past the identifier beginning at i.
func identEnd(src []byte, i int) int {
	j := i
	for j < len(src) && isIdentPart(src[j]) {
		j++
	}
	return j
}

// words returns every identifier-like token in src, in order.
