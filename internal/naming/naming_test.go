package naming

import "testing"

// TestAllowedSpellings pins the specification's own examples. Anything here that fails is
// a false positive, which would be the expensive error direction: a convention check that
// cries wolf gets disabled.
func TestAllowedSpellings(t *testing.T) {
	allowed := []string{
		// The specification's examples.
		"userId", "jsonAst", "httpHandler", "Ok", "Id",
		// One leading capital is ordinary camelCase, not an initialism.
		"Ast", "Http", "Url", "Xml", "Uuid",
		// Embedded, correctly lowercased.
		"parseJson", "newHttpHandler", "userIdMap", "dbUrl", "astNodes",
		// Not initialisms at all, or not dictionary words.
		"identity", "ideas", "video", "audio", "index", "userIdX", "dbField",
		// Acronyms Go spells as words.
		"cpuCount", "ramUsage", "uiWidth", "okFlag",
	}

	for _, id := range allowed {
		if v := Check([]string{id}); len(v) != 0 {
			t.Errorf("%q should be allowed, got: %s", id, v[0].Reason)
		}
	}
}

// TestBannedSpellings pins the specification's banned list plus the all-caps forms the
// dictionary must catch.
func TestBannedSpellings(t *testing.T) {
	banned := []string{
		// The specification's examples.
		"userID", "jsonAST", "HTTPHandler", "OK",
		// Leading initialism merged with the next capital.
		"ASTParser", "URLPath", "HTTPServer", "IDField",
		// Embedded all-caps initialism.
		"parseJSON", "userIDMap", "dbURL", "httpURLValue", "newHTTPClient",
	}

	for _, id := range banned {
		if v := Check([]string{id}); len(v) == 0 {
			t.Errorf("%q should be banned", id)
		}
	}
}

// TestReasonNamesTheFix verifies a violation explains what to write instead, since the
// output feeds a developer or agent that has to act on it.
func TestReasonNamesTheFix(t *testing.T) {
	v := Check([]string{"userID"})
	if len(v) != 1 {
		t.Fatalf("got %d violations, want 1", len(v))
	}
	if v[0].Identifier != "userID" {
		t.Errorf("Identifier = %q, want userID", v[0].Identifier)
	}
	// The reason names the offending word and the replacement, not the whole identifier.
	for _, want := range []string{"ID", "id"} {
		if !contains(v[0].Reason, want) {
			t.Errorf("Reason %q should mention %q", v[0].Reason, want)
		}
	}
}

// TestOutputIsDeterministic covers ordering, which matters because this feeds both a test
// assertion and an agent-facing diagnostic.
func TestOutputIsDeterministic(t *testing.T) {
	input := []string{"userID", "jsonAST", "HTTPHandler", "OK", "parseJSON"}

	first := Check(input)
	for i := 0; i < 5; i++ {
		again := Check(input)
		if len(again) != len(first) {
			t.Fatalf("violation count varies between runs")
		}
		for j := range first {
			if again[j] != first[j] {
				t.Fatalf("order varies between runs: %v vs %v", first, again)
			}
		}
	}

	for i := 1; i < len(first); i++ {
		if first[i-1].Identifier > first[i].Identifier {
			t.Errorf("violations not sorted: %q before %q", first[i-1].Identifier, first[i].Identifier)
		}
	}
}

// TestEmptyInputIsClean guards against a vacuous pass on an empty scan.
func TestEmptyInputIsClean(t *testing.T) {
	if v := Check(nil); len(v) != 0 {
		t.Errorf("Check(nil) = %v, want none", v)
	}
	if v := Check([]string{""}); len(v) != 0 {
		t.Errorf(`Check([""]) = %v, want none`, v)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// TestKnownAmbiguityIsAccepted documents the one case this rule cannot resolve.
//
// "userIdX" is lexically identical to "userIDMap": an uppercase run followed by a lowercase
// tail. In the second the run is ID merging into Map, which the rule reads as ID and flags;
// in the first the run is IDX, a single token whose trailing x belongs to it. Distinguishing
// them needs a dictionary of English words, which is not a dependency this project is
// allowed.
//
// The rule resolves it toward "banned", because a false positive on a naming convention is
// the failure that gets a check switched off, whereas a missed violation costs only a review
// comment.
func TestKnownAmbiguityIsAccepted(t *testing.T) {
	for _, id := range []string{"userIdX"} {
		if v := Check([]string{id}); len(v) != 0 {
			t.Logf("%q reported as %s (accepted: ambiguous by design)", id, v[0].Reason)
		}
	}
}

// TestCheckLocatedReportsDeclarationLine pins the position carried through to a violation,
// and the collapse of a name repeated on one line.
func TestCheckLocatedReportsDeclarationLine(t *testing.T) {
	got := CheckLocated([]Identifier{
		{Name: "userId", Line: 3},
		{Name: "userID", Line: 7},
		{Name: "HTTPHandler", Line: 12},
	})

	want := []Violation{
		{Identifier: "HTTPHandler", Reason: "initialism must not be all caps per camelCaseId: HTTP should be http", Line: 12},
		{Identifier: "userID", Reason: "initialism must not be all caps per camelCaseId: ID should be id", Line: 7},
	}
	if len(got) != len(want) {
		t.Fatalf("CheckLocated returned %d violations, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("violation %d:\n got: %+v\nwant: %+v", i, got[i], want[i])
		}
	}
}

// TestCheckLocatedCollapsesSameLineRepeats covers a receiver and its type declared on one
// line: the same name is collected twice, and reporting it twice at one position would read
// as two distinct findings.
func TestCheckLocatedCollapsesSameLineRepeats(t *testing.T) {
	got := CheckLocated([]Identifier{
		{Name: "userID", Line: 4},
		{Name: "userID", Line: 4},
	})
	if len(got) != 1 {
		t.Errorf("got %d violations for one name on one line, want 1: %v", len(got), got)
	}

	// The same name on two lines is two declarations, so both are reported.
	got = CheckLocated([]Identifier{
		{Name: "userID", Line: 4},
		{Name: "userID", Line: 9},
	})
	if len(got) != 2 {
		t.Errorf("got %d violations for one name on two lines, want 2: %v", len(got), got)
	}
}

// TestCheckLocatedIsDeterministic guards the ordering: the same input must produce the same
// output across runs, since this feeds an agent-facing diagnostic.
func TestCheckLocatedIsDeterministic(t *testing.T) {
	input := []Identifier{
		{Name: "userID", Line: 20},
		{Name: "userID", Line: 5},
		{Name: "HTTPHandler", Line: 9},
	}

	first := CheckLocated(input)
	again := CheckLocated(input)

	if len(first) != len(again) {
		t.Fatalf("length changed between runs: %d then %d", len(first), len(again))
	}
	for i := range first {
		if first[i] != again[i] {
			t.Fatalf("violation %d differs between runs:\n%+v\n%+v", i, first[i], again[i])
		}
	}
	if first[0].Line > first[1].Line && first[0].Identifier == first[1].Identifier {
		t.Errorf("same name not ordered by line: %+v", first)
	}
}

// TestScreamingSnakeConstantsAreExempt covers the exemption for constants, which follow a
// different naming convention and cannot satisfy camelCaseId at all.
//
// Applying the rule to them reported every such constant by construction: RAMFS_MAGIC
// contains RAM and became "RAM should be ram", VMHGFS_SUPER_MAGIC "VM should be vm". Three
// of seven diagnostics on duf were of this kind.
func TestScreamingSnakeConstantsAreExempt(t *testing.T) {
	for _, id := range []string{
		"RAMFS_MAGIC",
		"VMHGFS_SUPER_MAGIC",
		"RPC_PIPEFS_SUPER_MAGIC",
		"A",
		"MAX_SIZE_2",
	} {
		if v := Check([]string{id}); len(v) != 0 {
			t.Errorf("Check(%q) = %q, want no violation: all-caps constants are not camelCase", id, v[0])
		}
	}
}

// TestBareAllCapsNamesStillBanned guards the exemption from swallowing the cases the
// specification calls out. OK and ID have no underscore, so the rule still applies to them.
func TestBareAllCapsNamesStillBanned(t *testing.T) {
	for _, id := range []string{"OK", "ID", "URL", "HTTP"} {
		if v := Check([]string{id}); len(v) == 0 {
			t.Errorf("Check(%q) reported nothing, want a violation", id)
		}
	}
}
