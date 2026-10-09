// Package naming enforces the camelCaseId identifier convention from specification 3.1.
//
// The convention deliberately overrides Go's usual initialism handling: jsonAst rather
// than jsonAST, httpHandler rather than HTTPHandler, Ok rather than OK. The rule
// lowercases an initialism everywhere it appears, including at the start of an identifier,
// and applies uniformly to standalone identifiers.
//
// The check is exposed as golens check --naming and is also what the repository's own
// naming test calls, so the convention is enforced in CI rather than by review.
package naming

import (
	"sort"
	"strings"
)

// Initialisms is the fixed dictionary the check is defined against.
//
// A fixed list is what makes the check deterministic: without it, "is this an initialism"
// becomes a judgement call and the result drifts. The list is lowercased, since the rule
// lowercases whatever it matches.
var Initialisms = []string{
	"acl", "api", "ascii", "ast", "cpu", "css", "db", "dns", "eof", "gpu",
	"graphql", "grpc", "guid", "html", "http", "https", "id", "ip", "json",
	"lhs", "ok", "qps", "ram", "rhs", "rpc", "sla", "smtp", "sql", "ssh",
	"ssl", "tcp", "tls", "ttl", "udp", "ui", "uid", "uri", "url", "utf8",
	"uuid", "vm", "xml", "xsrf",
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
type Violation struct {
	Identifier string
	Reason     string
}

func (v Violation) String() string { return v.Identifier + ": " + v.Reason }

// Check reports every identifier that breaks the camelCaseId convention.
//
// The rule, stated precisely so callers do not have to infer it:
//
//   - Split the identifier into runs of uppercase letters. Each maximal run of two or more
//     uppercase letters is an initialism and must be lowercased.
//   - A single uppercase letter followed by another uppercase letter ("ID", "URL") is the
//     start of an initialism, so "userID" is banned and "userId" required.
//   - A leading run of uppercase letters is an initialism too: "HTTPHandler" must become
//     "httpHandler".
//   - Everything else about Go's naming is left alone. Underscores, leading capitals and
//     all-caps words that are not initialisms are not this check's business.
func Check(identifiers []string) []Violation {
	var out []Violation

	for _, id := range identifiers {
		if reason, bad := violates(id); bad {
			out = append(out, Violation{Identifier: id, Reason: reason})
		}
	}

	// Sorting makes the output deterministic, which matters because this feeds a test
	// assertion and an agent-facing diagnostic.
	sort.Slice(out, func(i, j int) bool { return out[i].Identifier < out[j].Identifier })
	return out
}

// violates reports whether ident breaks the convention, and why.
//
// The rule is a single pass over word boundaries. Go identifiers are camelCase, so a word
// boundary is either the start of the identifier or the position after a lowercase letter,
// digit or underscore. At each boundary the uppercase run is matched against the dictionary
// case-sensitively; a match means the initialism was not lowercased.
func violates(ident string) (string, bool) {
	if ident == "" {
		return "", false
	}
	for _, start := range wordStarts(ident) {
		if reason, bad := matchInitialism(ident, start); bad {
			return reason, true
		}
	}
	return "", false
}

// wordStarts returns the positions in ident that could begin a word: the start of the
// identifier, and every position following a lowercase letter, digit or underscore.
//
// Lowercase rather than uppercase is the boundary signal because in camelCase a word starts
// with a lowercase letter: "userID" has a boundary at the "I", "userId" does not.
func wordStarts(ident string) []int {
	starts := []int{0}
	for i := 1; i < len(ident); i++ {
		if isLower(ident[i-1]) || isDigit(ident[i-1]) || ident[i-1] == '_' {
			starts = append(starts, i)
		}
	}
	return starts
}

// leadingRun returns the length of the uppercase run at the start of s.
func leadingRun(s string) int {
	n := 0
	for n < len(s) && isUpper(s[n]) {
		n++
	}
	return n
}

// matchInitialism reports whether a dictionary initialism begins at start written in all
// capitals, which is what the convention bans.
//
// A word only counts when it opens with two or more capitals. Fewer is ordinary camelCase,
// which the specification allows: it lists userId, Ok and Id as allowed and userID, OK,
// jsonAST and HTTPHandler as banned, so the line is exactly all-caps versus one capital.
//
// Go's camelCase merges an initialism into the following capital, so a prefix search is
// needed at both word positions:
//
//   - Leading: HTTPHandler has the run HTTPH, and HTTP is the entry.
//   - Embedded: userIDMap has the run IDM, and ID is the entry.
//
// A prefix is accepted only when it is strictly shorter than the run, or when the run has no
// lowercase tail. That keeps URLs legal -- its run is URL with a plural tail, so the word is
// URLs, not URL -- while still flagging IDField, where F starts a new capitalised word.
func matchInitialism(ident string, start int) (string, bool) {
	rest := ident[start:]

	run := leadingRun(rest)
	if run < 2 {
		return "", false
	}

	tailEnd := run
	for tailEnd < len(rest) && isLower(rest[tailEnd]) {
		tailEnd++
	}

	// The run extended into its lowercase tail is one candidate: this is how jsonAST and
	// parseJSON are caught, and how URLs is left alone.
	if word := rest[:tailEnd]; tailEnd > run || initialismSet[strings.ToLower(word)] {
		if lower := strings.ToLower(word); initialismSet[lower] && word != lower {
			return violation(word, lower), true
		}
	}

	// Prefixes of the run, longest first.
	for n := run; n >= 2; n-- {
		if tailEnd > run && n == run {
			continue // the whole run followed by a tail is the plural form, not an initialism
		}
		word := rest[:n]
		lower := strings.ToLower(word)
		if initialismSet[lower] && word != lower {
			return violation(word, lower), true
		}
	}

	return "", false
}

func violation(word, lower string) string {
	return "initialism must not be all caps per camelCaseId: " + word + " should be " + lower
}

func isUpper(b byte) bool { return b >= 'A' && b <= 'Z' }
func isLower(b byte) bool { return b >= 'a' && b <= 'z' }
func isDigit(b byte) bool { return b >= '0' && b <= '9' }
