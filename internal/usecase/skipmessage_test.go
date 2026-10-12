package usecase

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// TestSkippedFilesMessageSeparators covers a formatting defect in SkippedFilesError that only
// appeared past the enumeration limit.
//
// Entries were written one at a time, each prefixed with its own separator, and the truncation
// marker was appended after the loop. Past five failures that produced "...found '{';: next" —
// a semicolon welded straight to a colon — and a comma where the fifth entry should have ended,
// so the marker was glued to the last filename with nothing between them.
//
// It survived because the defect needs MORE THAN maxListedFailures failures, and every test that
// touched this message named one or two. A boundary nobody crosses is a boundary nobody checks.
func TestSkippedFilesMessageSeparators(t *testing.T) {
	failures := func(n int) []ParseError {
		out := make([]ParseError, 0, n)
		for i := 1; i <= n; i++ {
			out = append(out, ParseError{
				Path: fmt.Sprintf("brk%d.go", i),
				Err:  errors.New("3:10: expected ')', found '{'"),
			})
		}
		return out
	}

	tests := []struct {
		name         string
		failures     int
		wantListed   int
		wantExtra    bool
		wantFirstSep bool
	}{
		{"none", 0, 0, false, false},
		{"one", 1, 1, false, false},
		{"several", 3, 3, false, false},
		{"exactly at the limit", maxListedFailures, maxListedFailures, false, true},
		{"one past the limit", maxListedFailures + 1, maxListedFailures, true, true},
		{"well past the limit", maxListedFailures + 9, maxListedFailures, true, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			msg := SkippedFilesError{Failures: failures(tc.failures), Total: tc.failures}.Error()

			// The defect itself: a separator written for the next entry, then the truncation
			// marker appended without one, giving "...found '{';: next" and "found '{', and N more".
			if strings.Contains(msg, "; : ") {
				t.Errorf("message contains a semicolon welded to a colon: %s", msg)
			}

			summary, entries, found := strings.Cut(msg, ": ")
			if !found {
				if tc.wantListed != 0 {
					t.Fatalf("no entry list in %q", msg)
				}
				return // zero failures legitimately has nothing to list.
			}
			if !strings.HasSuffix(summary, "are missing from the output") {
				t.Errorf("summary is not the expected lead-in: %s", msg)
			}

			parts := strings.Split(entries, "; ")
			wantParts := tc.wantListed
			if tc.wantExtra {
				wantParts++
			}
			if len(parts) != wantParts {
				t.Errorf("got %d entries, want %d:\n%s", len(parts), wantParts, msg)
			}

			if tc.wantExtra {
				marker := fmt.Sprintf("and %d more", tc.failures-maxListedFailures)
				if parts[len(parts)-1] != marker {
					t.Errorf("truncation marker = %q, want %q:\n%s", parts[len(parts)-1], marker, msg)
				}
				// The marker is one of the "; "-joined parts, which is exactly what the old
				// formatting got wrong by appending it after the loop instead.
				if !strings.Contains(msg, "; "+marker) {
					t.Errorf("truncation marker is not separated like the other entries: %s", msg)
				}
			}
		})
	}
}

// TestSkippedFilesMessageKeepsTheExactCount pins the part that must not change while fixing the
// formatting: the summary count is always exact even though the enumeration is truncated, so a
// caller can tell how complete the answer is without parsing the list.
func TestSkippedFilesMessageKeepsTheExactCount(t *testing.T) {
	failures := make([]ParseError, 0, 40)
	for i := 1; i <= 40; i++ {
		failures = append(failures, ParseError{
			Path: fmt.Sprintf("brk%d.go", i),
			Err:  errors.New("3:10: expected ')', found '{'"),
		})
	}

	msg := SkippedFilesError{Failures: failures, Total: 40}.Error()

	if !strings.HasPrefix(msg, "40 of 40 files could not be read and are missing from the output") {
		t.Errorf("summary lost the exact count: %s", msg)
	}
	if !strings.Contains(msg, "and 35 more") {
		t.Errorf("truncation marker wrong for 40 failures and a limit of %d: %s", maxListedFailures, msg)
	}
	// The five named are the first five in path order, not an arbitrary five.
	if !strings.Contains(msg, "brk1.go") || !strings.Contains(msg, "brk5.go") {
		t.Errorf("expected the first %d paths to be named: %s", maxListedFailures, msg)
	}
	if strings.Contains(msg, "brk6.go") {
		t.Errorf("named a file past the limit: %s", msg)
	}
}

// TestSkippedFilesSummaryDoesNotClaimAParseFailure pins the wording.
//
// The failures list mixes two causes: a file that does not parse and a file that cannot be
// opened. The summary said "could not be parsed" for both, which told an agent to go and fix
// the syntax of a file it cannot read — it would have to hit the permission error a second
// time before revising. ParseError.Diagnostics already separates them, reporting an I/O
// failure at 1:1 because it has no position; the summary flattened that away.
func TestSkippedFilesSummaryDoesNotClaimAParseFailure(t *testing.T) {
	syntax := SkippedFilesError{
		Failures: []ParseError{{Path: "bad.go", Err: errors.New("3:9: expected ')', found '{'")}},
		Total:    1,
	}
	unreadable := SkippedFilesError{
		Failures: []ParseError{{Path: "secret.go", Err: errors.New("read secret.go: open secret.go: permission denied")}},
		Total:    1,
	}

	for _, tc := range []struct {
		name string
		err  SkippedFilesError
	}{
		{"a syntax error", syntax},
		{"an unreadable file", unreadable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			msg := tc.err.Error()
			if strings.Contains(msg, "could not be parsed") {
				t.Errorf("summary claims the file was parsed and rejected:\n%s", msg)
			}
			// Whatever the cause, the summary must name the outcome: the file is absent
			// from what was produced.
			if !strings.Contains(msg, "missing from the output") {
				t.Errorf("summary does not say what the consequence is:\n%s", msg)
			}
		})
	}

	// The two must still be distinguishable to the caller, which is what ParseError
	// guarantees by carrying either a position or a bare I/O message.
	if !strings.Contains(unreadable.Error(), "permission denied") {
		t.Errorf("the I/O cause is no longer visible: %s", unreadable.Error())
	}
	if !strings.Contains(syntax.Error(), "3:9") {
		t.Errorf("the parse position is no longer visible: %s", syntax.Error())
	}
}

// TestSkippedFilesMessageIsDeterministic guards the property the whole type rests on: the same
// failures render identically every time, because a caller may diff or cache the message.
func TestSkippedFilesMessageIsDeterministic(t *testing.T) {
	failures := make([]ParseError, 0, 12)
	for i := 1; i <= 12; i++ {
		failures = append(failures, ParseError{
			Path: fmt.Sprintf("brk%d.go", i),
			Err:  errors.New("boom"),
		})
	}
	err := SkippedFilesError{Failures: failures, Total: 12}

	first := err.Error()
	for range 5 {
		if got := err.Error(); got != first {
			t.Fatalf("message changed between calls:\n%s\n%s", first, got)
		}
	}
}
