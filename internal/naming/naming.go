// Package naming enforces Go's own initialism convention: a known initialism is written
// consistently within an identifier -- ID, URL, JSON, HTTP -- rather than mixed-case as Id
// or Url.
//
// The rule is the one golint popularised and staticcheck now carries as ST1003, so a
// repository that runs this check agrees with the rest of the Go tooling instead of with a
// local dialect. Implementing it here rather than depending on either keeps golens free of
// external dependencies, which internal/arch requires.
//
// Two positions are treated differently, because that is what Go actually does. At the start
// of an unexported identifier an initialism stays lowercase -- urlPath, jsonBody, httpClient
// -- since Go identifiers begin lowercase by convention. Anywhere else it is all capitals:
// userID, parseJSON, newHTTPClient, ID, URL.
//
// The check is exposed as golens check --naming and is also what the repository's own
// naming test calls, so the convention is enforced in CI rather than by review.
package naming

import (
	"sort"
	"strings"
	"unicode"
)

// Initialisms is the fixed dictionary the check is defined against.
//
// A fixed list is what makes the check deterministic: without it, "is this an initialism"
// becomes a judgement call and the result drifts. The list is the one golint shipped and
// staticcheck inherited; it is keyed in upper case, since that is the correct spelling.
var Initialisms = []string{
	"ACL", "API", "ASCII", "CPU", "CSS", "DNS", "EOF", "GUID", "HTML", "HTTP", "HTTPS",
	"ID", "IP", "JSON", "LHS", "QPS", "RAM", "RHS", "RPC", "SLA", "SMTP", "SQL", "SSH",
	"TCP", "TLS", "TTL", "UDP", "UI", "UID", "URI", "URL", "UTF8", "UUID", "VM", "XML",
	"XSRF", "XSS",
}

// initialismSet is the lookup form of Initialisms.
var initialismSet = func() map[string]bool {
	set := make(map[string]bool, len(Initialisms))
	for _, w := range Initialisms {
		set[w] = true
	}
	return set
}()

// Violation describes one identifier that breaks the convention.
//
// Line is the declaration line when the caller supplied one through Identifier, and zero
// otherwise. A zero Line means "position unknown", not "the first line".
type Violation struct {
	Identifier string
	Reason     string
	Line       int
}

func (v Violation) String() string { return v.Identifier + ": " + v.Reason }

// Identifier is an identifier together with the line it was declared on.
//
// A caller that already knows where a name came from -- the outline carries bounds for
// every node -- should prefer CheckLocated, because a diagnostic that names only the file
// leaves a reader to search for the identifier.
type Identifier struct {
	Name string
	Line int
}

// Check reports every identifier that breaks the initialism convention, with no position.
//
// The rule, stated precisely so callers do not have to infer it:
//
//   - Split the identifier at each lowercase-to-uppercase transition, and at underscores.
//     Underscores are boundaries but are preserved, so usecase_test and RAMFS_MAGIC are
//     left alone rather than rewritten.
//   - For each word, if its upper-case form is in the dictionary, that is the spelling Go
//     wants: ID, not Id; URL, not Url.
//   - A word at the start of an identifier that already begins lowercase keeps its
//     lowercase form, so urlPath and jsonBody are correct and userID is required.
//   - Everything else is out of scope. This is not a general style linter: it says nothing
//     about underscores in ordinary names, capitalisation of non-initialisms, or length.
func Check(identifiers []string) []Violation {
	located := make([]Identifier, 0, len(identifiers))
	for _, id := range identifiers {
		located = append(located, Identifier{Name: id})
	}
	return CheckLocated(located)
}

// CheckLocated is Check with a declaration line for each identifier, carried through to
// the violation so the caller can report where the name is.
//
// Repeats of the same name on the same line collapse to one violation. An identifier can
// legitimately appear more than once on a line -- a receiver and its type on a single
// declaration -- and without the collapse the same problem would be reported twice at the
// same position, which reads as two distinct findings.
func CheckLocated(identifiers []Identifier) []Violation {
	var out []Violation

	seen := make(map[violationKey]bool, len(identifiers))
	for _, id := range identifiers {
		fixed, bad := violates(id.Name)
		if !bad {
			continue
		}
		key := violationKey{line: id.Line, name: id.Name}
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, Violation{
			Identifier: id.Name,
			Reason:     "initialism must be consistently cased per Go convention: " + id.Name + " should be " + fixed,
			Line:       id.Line,
		})
	}

	// Sorting makes the output deterministic, which matters because this feeds a test
	// assertion and an agent-facing diagnostic. The line breaks ties so that two
	// declarations of the same name come out in source order.
	sort.Slice(out, func(i, j int) bool {
		if out[i].Identifier != out[j].Identifier {
			return out[i].Identifier < out[j].Identifier
		}
		return out[i].Line < out[j].Line
	})
	return out
}

// violationKey identifies a reported violation for deduplication.
type violationKey struct {
	line int
	name string
}

// violates returns the corrected spelling of ident and whether it needed correcting.
//
// The walk splits the identifier into words and rewrites any word that names an
// initialism into its dictionary spelling. A word is only rewritten when doing so changes
// the identifier, which is what makes the result a diff rather than a normalisation.
func violates(ident string) (string, bool) {
	if ident == "" {
		return "", false
	}

	runes := []rune(ident)
	firstUpper := len(runes) > 0 && unicode.IsUpper(runes[0])

	for _, word := range words(runes) {
		if !initialismSet[strings.ToUpper(string(runes[word.start:word.end]))] {
			continue
		}
		// An initialism opening an identifier that already starts lowercase stays
		// lowercase: urlPath, jsonBody, httpClient. Everywhere else it is capitals.
		if word.start == 0 && !firstUpper {
			continue
		}
		want := strings.ToUpper(string(runes[word.start:word.end]))
		copy(runes[word.start:word.end], []rune(want))
	}

	fixed := string(runes)
	return fixed, fixed != ident
}

// span is one word within an identifier.
type span struct{ start, end int }

// words returns the spans of the identifier's words: bounded by the start and end of the
// name, by each lowercase-to-uppercase transition, and by underscores.
//
// An underscore is a boundary and is kept in the preceding word, so the span it ends
// carries the separator rather than dropping it. That is deliberate. golint strips
// underscores entirely, which turns usecase_test into usecasetest and RAMFS_MAGIC into
// RAMFSMAGIC -- both wrong, and both names this check must leave alone. Keeping the
// separator means only the initialism itself is ever rewritten.
func words(runes []rune) []span {
	var out []span
	start := 0
	for i := 0; i < len(runes); i++ {
		if runes[i] != '_' && !isBoundary(runes, i) {
			continue
		}
		end := i
		if runes[i] == '_' {
			end = i + 1
		}
		out = append(out, span{start, end})
		start = end
	}
	if start < len(runes) {
		out = append(out, span{start, len(runes)})
	}
	return out
}

// isBoundary reports whether a new word starts at index i, which is the case when the
// preceding rune is lowercase or a digit and this one is uppercase: the "rI" in userID.
//
// A digit does not start a word by itself, so utf8 in utf8Count stays one word rather than
// splitting into "utf" and "8".
func isBoundary(runes []rune, i int) bool {
	if i == 0 {
		return false
	}
	return unicode.IsLower(runes[i-1]) && unicode.IsUpper(runes[i])
}
