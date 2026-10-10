package naming

import "testing"

// TestAllowedSpellings pins the spellings Go's own convention requires. Anything here that
// fails is a false positive, which would be the expensive error direction: a convention
// check that cries wolf gets disabled.
func TestAllowedSpellings(t *testing.T) {
	allowed := []string{
		// The convention's core: an initialism is capitals everywhere but the front.
		"userID", "URL", "ID", "HTTPHandler", "parseJSON", "newHTTPClient",
		"userIDMap", "dbURL", "IDField", "UTF8Count",
		// Lowercase at the front of an identifier that starts lowercase. This is the
		// branch that distinguishes the rule from a blunt all-caps check, and it is
		// why jsonAst and httpHandler are correct here even though AST and HTTP
		// appear in capitals nowhere.
		"urlPath", "jsonBody", "httpClient", "idField", "jsonAst", "httpHandler",
		// Ordinary camelCase with no initialism in it.
		"astNodes", "IsValid", "identity", "ideas", "video", "audio", "index",
		"cpuCount", "ramUsage", "uiWidth",
		// Shapes this check must not touch. Underscores are word boundaries here, not
		// something to strip, so a test package keeps its name and a constant block
		// keeps its underscores.
		"usecase_test", "arch_test", "RAMFS_MAGIC", "MAX_SIZE_2",
		// Lowercase dictionary words that are not initialisms.
		"json", "ast", "id", "valid", "least",
	}

	for _, id := range allowed {
		if v := Check([]string{id}); len(v) != 0 {
			t.Errorf("%q should be allowed, got: %s", id, v[0].Reason)
		}
	}
}

// TestBannedSpellings pins the mixed-case forms the dictionary must catch. These are what
// the rule exists to report: an initialism spelled inconsistently within one identifier.
func TestBannedSpellings(t *testing.T) {
	banned := []string{
		"userId", "Url", "Uuid", "Xml", "Http", "Json", "Id",
		"parseJson", "userIdMap", "dbUrl", "camelCaseId",
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
	v := Check([]string{"userId"})
	if len(v) != 1 {
		t.Fatalf("got %d violations, want 1", len(v))
	}
	if v[0].Identifier != "userId" {
		t.Errorf("Identifier = %q, want userId", v[0].Identifier)
	}
	for _, want := range []string{"userId", "userID"} {
		if !contains(v[0].Reason, want) {
			t.Errorf("Reason %q should mention %q", v[0].Reason, want)
		}
	}
}

// TestOutputIsDeterministic covers ordering, which matters because this feeds both a test
// assertion and an agent-facing diagnostic.
func TestOutputIsDeterministic(t *testing.T) {
	input := []string{"userId", "httpHandler", "parseJson", "dbUrl", "Json"}

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

// TestLeadingLowercaseIsNotFlagged pins the branch that makes this rule usable rather than
// merely strict. urlPath is correct Go, and a rule that reported it would be switched off.
func TestLeadingLowercaseIsNotFlagged(t *testing.T) {
	for _, id := range []string{"urlPath", "jsonBody", "httpClient", "id", "ipAddr"} {
		if v := Check([]string{id}); len(v) != 0 {
			t.Errorf("%q should be allowed: %s", id, v[0].Reason)
		}
	}
}

// TestUnderscoresArePreserved covers the deliberate divergence from golint, which strips
// underscores outright and so would turn usecase_test into usecasetest and RAMFS_MAGIC
// into RAMFSMAGIC. Both are names this check must leave exactly as they are.
func TestUnderscoresArePreserved(t *testing.T) {
	for _, id := range []string{
		"usecase_test",
		"arch_test",
		"format_test",
		"RAMFS_MAGIC",
		"MAX_SIZE_2",
		"VMHGFS_SUPER_MAGIC",
	} {
		fixed, bad := violates(id)
		if bad {
			t.Errorf("violates(%q) rewrote it to %q; underscores are boundaries, not noise", id, fixed)
		}
	}
}

// TestAllCapsConstantsAreUntouched covers the shape Go uses for constant blocks. These are
// correct as written and the rule has nothing to say about them.
func TestAllCapsConstantsAreUntouched(t *testing.T) {
	for _, id := range []string{
		"RAMFS_MAGIC",
		"VMHGFS_SUPER_MAGIC",
		"RPC_PIPEFS_SUPER_MAGIC",
		"A",
		"MAX_SIZE_2",
	} {
		if v := Check([]string{id}); len(v) != 0 {
			t.Errorf("Check(%q) = %q, want no violation", id, v[0])
		}
	}
}

// TestCheckLocatedReportsDeclarationLine pins the position carried through to a violation,
// and the collapse of a name repeated on one line.
func TestCheckLocatedReportsDeclarationLine(t *testing.T) {
	got := CheckLocated([]Identifier{
		{Name: "userID", Line: 3},
		{Name: "userId", Line: 7},
		{Name: "dbUrl", Line: 12},
	})

	want := []Violation{
		{Identifier: "dbUrl", Reason: "initialism must be consistently cased per Go convention: dbUrl should be dbURL", Line: 12},
		{Identifier: "userId", Reason: "initialism must be consistently cased per Go convention: userId should be userID", Line: 7},
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
		{Name: "userId", Line: 4},
		{Name: "userId", Line: 4},
	})
	if len(got) != 1 {
		t.Errorf("got %d violations for one name on one line, want 1: %v", len(got), got)
	}

	// The same name on two lines is two declarations, so both are reported.
	got = CheckLocated([]Identifier{
		{Name: "userId", Line: 4},
		{Name: "userId", Line: 9},
	})
	if len(got) != 2 {
		t.Errorf("got %d violations for one name on two lines, want 2: %v", len(got), got)
	}
}

// TestCheckLocatedIsDeterministic guards the ordering: the same input must produce the same
// output across runs, since this feeds an agent-facing diagnostic.
func TestCheckLocatedIsDeterministic(t *testing.T) {
	input := []Identifier{
		{Name: "userId", Line: 20},
		{Name: "userId", Line: 5},
		{Name: "dbUrl", Line: 9},
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

// TestGoStdlibSpellingsAreClean is the check that decided this rule's direction. These are
// names the Go project itself uses, and every one of them is correct Go.
func TestGoStdlibSpellingsAreClean(t *testing.T) {
	for _, id := range []string{
		"isASCII", "ensureEOF", "toASCII",
		"APIResponse", "HTTPServer", "UserID", "GetUserByID",
		"URL", "DefaultAPIURL", "MaxHTTPRequests",
	} {
		if v := Check([]string{id}); len(v) != 0 {
			t.Errorf("%q is ordinary Go and must not be reported: %s", id, v[0].Reason)
		}
	}
}
