package domain

import "testing"

// TestBoundsHalfOpenSlice is the domain-level guarantee the whole tool rests on:
// src[StartByte:EndByte] yields exactly the span's text.
func TestBoundsHalfOpenSlice(t *testing.T) {
	// "package p\n" is 10 bytes, the blank line is 1, so "func f() {}" starts at byte 11
	// and occupies 11 bytes. Offsets here are counted by hand on purpose: the point of
	// this test is that a hand-count and a byte slice agree.
	src := []byte("package p\n\nfunc f() {}\n")
	b := Bounds{StartLine: 3, EndLine: 3, StartByte: 11, EndByte: 22}

	if got := string(src[b.StartByte:b.EndByte]); got != "func f() {}" {
		t.Errorf("slice = %q, want %q", got, "func f() {}")
	}
	if got := b.Source(src); got != "func f() {}" {
		t.Errorf("Source = %q, want %q", got, "func f() {}")
	}
	if b.ByteLen() != 11 {
		t.Errorf("ByteLen = %d, want 11", b.ByteLen())
	}
}

// TestBoundsLineRange covers the two rendering forms from specification 2.1.
func TestBoundsLineRange(t *testing.T) {
	tests := []struct {
		name string
		in   Bounds
		want string
	}{
		{"single line", Bounds{StartLine: 10, EndLine: 10}, "line 10"},
		{"range", Bounds{StartLine: 5, EndLine: 8}, "lines 5-8"},
		{"two lines", Bounds{StartLine: 12, EndLine: 13}, "lines 12-13"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.in.LineRange(); got != tt.want {
				t.Errorf("LineRange = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestBoundsLineCount covers inclusive-at-both-ends counting, which is why a single-line
// span reports 1 rather than 0.
func TestBoundsLineCount(t *testing.T) {
	tests := []struct {
		in   Bounds
		want int
	}{
		{Bounds{StartLine: 17, EndLine: 25}, 9},
		{Bounds{StartLine: 10, EndLine: 10}, 1},
		{Bounds{StartLine: 1, EndLine: 1}, 1},
	}
	for _, tt := range tests {
		if got := tt.in.LineCount(); got != tt.want {
			t.Errorf("%v.LineCount = %d, want %d", tt.in, got, tt.want)
		}
	}
}

// TestBoundsOutOfRangeSourceIsEmpty verifies Source degrades safely rather than panicking
// when asked for a span the buffer cannot satisfy.
func TestBoundsOutOfRangeSourceIsEmpty(t *testing.T) {
	src := []byte("short")

	for _, b := range []Bounds{
		{StartByte: 10, EndByte: 20}, // entirely past the end
		{StartByte: 4, EndByte: 99},  // end past the end
		{StartByte: -1, EndByte: 3},  // negative start
		{StartByte: 3, EndByte: 3},   // empty span
	} {
		if got := b.Source(src); got != "" {
			t.Errorf("%v.Source = %q, want empty", b, got)
		}
	}
}

// TestBoundsValid covers the guard used to reject unusable spans.
func TestBoundsValid(t *testing.T) {
	tests := []struct {
		name string
		in   Bounds
		want bool
	}{
		{"typical", Bounds{StartLine: 1, EndLine: 5, StartByte: 0, EndByte: 40}, true},
		{"zero width", Bounds{StartLine: 1, EndLine: 1, StartByte: 5, EndByte: 5}, false},
		{"negative byte", Bounds{StartLine: 1, EndLine: 1, StartByte: -1, EndByte: 5}, false},
		{"line zero", Bounds{StartLine: 0, EndLine: 1, StartByte: 0, EndByte: 5}, false},
		{"inverted lines", Bounds{StartLine: 9, EndLine: 2, StartByte: 0, EndByte: 5}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.in.Valid(); got != tt.want {
				t.Errorf("Valid = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestLookupAmbiguity covers the precondition for exit code 2: a name resolving to more
// than one symbol must return every candidate so the CLI can list them.
func TestLookupAmbiguity(t *testing.T) {
	doc := Document{Symbols: []Symbol{
		{Name: "Save", QualifiedName: "A.Save", Kind: KindMethod},
		{Name: "Save", QualifiedName: "B.Save", Kind: KindMethod},
		{Name: "Get", QualifiedName: "Get", Kind: KindFunc},
	}}

	if got := doc.Lookup("Save"); len(got) != 2 {
		t.Errorf("Lookup(Save) = %d results, want 2", len(got))
	}
	if got := doc.Lookup("Get"); len(got) != 1 {
		t.Errorf("Lookup(Get) = %d results, want 1", len(got))
	}
	if got := doc.Lookup("Missing"); len(got) != 0 {
		t.Errorf("Lookup(Missing) = %d results, want 0", len(got))
	}

	// A qualified name resolves to exactly its own symbol.
	if got := doc.Lookup("A.Save"); len(got) != 1 || got[0].QualifiedName != "A.Save" {
		t.Errorf("Lookup(A.Save) = %v, want exactly A.Save", got)
	}
}

// TestErrorExitCodes pins the documented exit code contract.
func TestErrorExitCodes(t *testing.T) {
	tests := []struct {
		in   ErrorExit
		want int
	}{
		{ExitOk, 0},
		{ExitUsage, 1},
		{ExitNotFound, 2},
		{ExitDiagnostics, 3},
		{ExitParse, 4},
	}
	for _, tt := range tests {
		if got := tt.in.Code(); got != tt.want {
			t.Errorf("%v.Code = %d, want %d", tt.in, got, tt.want)
		}
	}
}
