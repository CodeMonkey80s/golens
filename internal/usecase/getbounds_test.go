package usecase_test

import (
	"bytes"
	"encoding/json"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CodeMonkey80s/golens/internal/domain"
	"github.com/CodeMonkey80s/golens/internal/engine/goast"
	"github.com/CodeMonkey80s/golens/internal/format"
	"github.com/CodeMonkey80s/golens/internal/testfixture"
	"github.com/CodeMonkey80s/golens/internal/usecase"
)

// The S1 hard gate, routed through the CLI rather than the engine.
//
// The engine already proves src[start_byte:end_byte] equals the extracted source. This
// proves no formatting layer mangles the offsets on the way out, which is the risk that only
// appears once get is wired end to end. A consumer reads these numbers from stdout and seeks
// with them, so a discrepancy here corrupts a file rather than merely printing wrong.
func TestByteInvariantThroughGet(t *testing.T) {
	dir := t.TempDir()
	path := testfixture.Write(t, dir, "store.go", testfixture.Store)

	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	// Every symbol class the fixture contains, so the assertion is not limited to one shape.
	symbols := []string{
		"User", "UserStore", "Alias", "NewUserStore",
		"UserStore.GetUser", "UserStore.Len",
	}

	for _, name := range symbols {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer
			opts := format.Options{Json: true, Compact: true}
			if err := usecase.Get(goast.New(), name, path, false, &buf, opts); err != nil {
				t.Fatalf("Get %s: %v", name, err)
			}

			var payload struct {
				Symbol    string `json:"symbol"`
				StartByte int    `json:"start_byte"`
				EndByte   int    `json:"end_byte"`
				StartLine int    `json:"start_line"`
				EndLine   int    `json:"end_line"`
			}
			if err := json.Unmarshal(buf.Bytes(), &payload); err != nil {
				t.Fatalf("Unmarshal: %v\n%s", err, buf.String())
			}

			if payload.Symbol != name {
				t.Errorf("symbol = %q, want %q", payload.Symbol, name)
			}

			// The slice must be valid and must round-trip.
			if payload.StartByte < 0 || payload.EndByte > len(src) || payload.StartByte >= payload.EndByte {
				t.Fatalf("out-of-range bounds [%d,%d) for a %d-byte file", payload.StartByte, payload.EndByte, len(src))
			}
			slice := string(src[payload.StartByte:payload.EndByte])

			// The slice must be the declaration the caller asked for.
			if !strings.Contains(slice, shortName(name)) {
				t.Errorf("slice does not contain %q:\n%q", shortName(name), slice)
			}

			// And it must be syntactically complete: a slice that splits a declaration would
			// still round-trip byte-for-byte while being useless to an agent.
			//
			// A bare type or func declaration is not a whole file, so it is checked as an
			// expression-statement-free fragment: wrapping it in a package clause tests that
			// the braces balance and the syntax is well formed.
			if err := parsesAsFragment(name, slice); err != nil {
				t.Errorf("slice for %s is not syntactically complete: %v\n%s", name, err, slice)
			}

			// Line numbers must agree with the bytes.
			if got := countLines(src[:payload.StartByte]); got+1 != payload.StartLine {
				t.Errorf("StartLine = %d, but %d newlines precede StartByte", payload.StartLine, got)
			}
		})
	}
}

// TestOverwriteRoundTrip is the test that closes the loop on the tool's reason for existing.
//
// Everything else proves the offsets are self-consistent. This proves they are correct for the
// operation an agent performs: take the bounds, overwrite that span with a replacement, and
// land on a file that still parses and contains the replacement.
//
// A byte-offset bug that survived A1 would be invisible here if the offsets merely round-trip;
// this catches the case where they address the right bytes but the wrong span, such as
// including the trailing newline and merging two declarations.
func TestOverwriteRoundTrip(t *testing.T) {
	dir := t.TempDir()
	src := testfixture.Store
	path := testfixture.Write(t, dir, "store.go", src)

	const replacement = "func (s *UserStore) GetUser(ctx context.Context, id int) (*User, error) {\n\treturn nil, ErrNotFound\n}"

	// Read the bounds the way an agent would: parse the JSON payload.
	var buf bytes.Buffer
	opts := format.Options{Json: true, Compact: true}
	if err := usecase.Bounds(goast.New(), "UserStore.GetUser", path, &buf, opts); err != nil {
		t.Fatalf("Bounds: %v", err)
	}

	var payload struct {
		StartByte int `json:"start_byte"`
		EndByte   int `json:"end_byte"`
	}
	if err := json.Unmarshal(buf.Bytes(), &payload); err != nil {
		t.Fatalf("Unmarshal: %v\n%s", err, buf.String())
	}

	// Perform the overwrite with nothing but the reported offsets.
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	edited := make([]byte, 0, len(original))
	edited = append(edited, original[:payload.StartByte]...)
	edited = append(edited, replacement...)
	edited = append(edited, original[payload.EndByte:]...)

	// The result must parse.
	if _, err := parser.ParseFile(token.NewFileSet(), path, edited, parser.SkipObjectResolution); err != nil {
		t.Fatalf("edited file does not parse: %v", err)
	}

	// And it must contain the replacement, not a mangled version of it.
	doc, err := goast.New().ParseSource(path, edited)
	if err != nil {
		t.Fatalf("re-parse: %v", err)
	}

	sym := mustResolveOne(t, doc, "UserStore.GetUser")
	got := sym.Bounds.Source(edited)
	if !strings.Contains(got, "return nil, ErrNotFound") {
		t.Errorf("replacement not present in the new declaration:\n%s", got)
	}
	if strings.Contains(got, "ErrNotFound\n}") && strings.Contains(got, "s.cache") {
		t.Error("old body survived; the span did not cover the whole declaration")
	}

	// Everything after the edited declaration must be untouched, which proves the span did
	// not overrun into the next declaration.
	if len(doc.Lookup("UserStore.Len")) != 1 {
		t.Error("the declaration following the edit was lost; the span overran")
	}

	// The declarations before it must be untouched.
	mustResolveOne(t, doc, "NewUserStore")
}

// TestOverwritePreservesLineStructure covers the newline question from decision D3: the span
// stops at the closing brace, so an overwrite leaves the file's line structure intact.
func TestOverwritePreservesLineStructure(t *testing.T) {
	dir := t.TempDir()
	src := testfixture.Store
	path := testfixture.Write(t, dir, "store.go", src)

	before, err := goast.New().Parse(path)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	beforeLines := len(strings.Split(string(before.Source), "\n"))

	var buf bytes.Buffer
	opts := format.Options{Json: true, Compact: true}
	if err := usecase.Bounds(goast.New(), "UserStore.GetUser", path, &buf, opts); err != nil {
		t.Fatalf("Bounds: %v", err)
	}

	var payload struct {
		StartByte int `json:"start_byte"`
		EndByte   int `json:"end_byte"`
	}
	if err := json.Unmarshal(buf.Bytes(), &payload); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	// Replacing the span with a single-line body changes exactly the lines it covered.
	const short = "func (s *UserStore) GetUser() {}"
	edited := string(src[:payload.StartByte]) + short + string(src[payload.EndByte:])

	after, err := goast.New().ParseSource(path, []byte(edited))
	if err != nil {
		t.Fatalf("re-parse: %v", err)
	}
	afterLines := len(strings.Split(string(after.Source), "\n"))

	sym := mustResolveOne(t, before, "UserStore.GetUser")
	removed := sym.Bounds.LineCount() - 1 // the replacement is one line

	if got := beforeLines - removed; got != afterLines {
		t.Errorf("line count after edit = %d, want %d (removed %d, replaced with 1)", afterLines, got, removed)
	}

	// The following declaration must still be where it was relative to the end.
	mustResolveOne(t, after, "UserStore.Len")
}

// TestGetHumanMatchesSpecificationSample pins the human get output against the sample in
// specification 2.2: header block, blank line, then the declaration.
func TestGetHumanMatchesSpecificationSample(t *testing.T) {
	dir := t.TempDir()
	path := testfixture.Write(t, dir, "store.go", testfixture.Store)

	var buf bytes.Buffer
	if err := usecase.Get(goast.New(), "UserStore.GetUser", path, true, &buf, format.Options{}); err != nil {
		t.Fatalf("Get: %v", err)
	}

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	want := []string{
		"File: " + filepath.ToSlash(path),
		"Symbol: UserStore.GetUser",
		"Lines: 28-35 (8 lines)",
		"",
		"func (s *UserStore) GetUser(ctx context.Context, id int) (*User, error) {",
	}

	for i, w := range want {
		if i >= len(lines) {
			t.Fatalf("output ended before line %d:\n%s", i, buf.String())
		}
		if lines[i] != w {
			t.Errorf("line %d:\n got: %q\nwant: %q", i, lines[i], w)
		}
	}

	// The block must end with the closing brace, with no trailing blank line.
	if !strings.HasSuffix(buf.String(), "}\n") {
		t.Errorf("output should end with the closing brace:\n%q", buf.String())
	}

	// A one-line declaration must not read "1 lines". The specification's sample shows
	// "Lines: 17-25 (9 lines)" -- a bare range after the label, pluralised correctly.
	single := filepath.Join(dir, "one.go")
	if err := os.WriteFile(single, []byte("package p\n\ntype T struct{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var oneBuf bytes.Buffer
	if err := usecase.Get(goast.New(), "T", single, true, &oneBuf, format.Options{}); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !strings.Contains(oneBuf.String(), "(1 line)") {
		t.Errorf("a one-line declaration should read \"(1 line)\":\n%s", oneBuf.String())
	}
	if strings.Contains(oneBuf.String(), "(1 lines)") {
		t.Errorf("plural noun used for a single line:\n%s", oneBuf.String())
	}
	if strings.Contains(oneBuf.String(), "Lines: lines ") {
		t.Errorf("the label already says Lines; the range should be bare:\n%s", oneBuf.String())
	}
}

// TestBoundsHumanMatchesSpecificationSample pins the single-line form from specification 2.3.
func TestBoundsHumanMatchesSpecificationSample(t *testing.T) {
	dir := t.TempDir()
	path := testfixture.Write(t, dir, "store.go", testfixture.Store)

	var buf bytes.Buffer
	if err := usecase.Bounds(goast.New(), "UserStore.GetUser", path, &buf, format.Options{}); err != nil {
		t.Fatalf("Bounds: %v", err)
	}

	got := strings.TrimRight(buf.String(), "\n")
	if strings.Count(got, "\n") != 0 {
		t.Errorf("bounds human output must be a single line:\n%q", got)
	}
	for _, want := range []string{"UserStore.GetUser", "lines 28-35", "bytes "} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q:\n%s", want, got)
		}
	}
}

// TestBoundsExcludesTrailingNewline pins decision D3 at the payload level: the span stops at
// the closing brace, so the byte after EndByte is the newline rather than part of the span.
func TestBoundsExcludesTrailingNewline(t *testing.T) {
	dir := t.TempDir()
	src := testfixture.Store
	path := testfixture.Write(t, dir, "store.go", src)

	var buf bytes.Buffer
	if err := usecase.Bounds(goast.New(), "UserStore.GetUser", path, &buf, format.Options{Json: true}); err != nil {
		t.Fatalf("Bounds: %v", err)
	}

	var payload struct {
		StartByte int `json:"start_byte"`
		EndByte   int `json:"end_byte"`
	}
	if err := json.Unmarshal(buf.Bytes(), &payload); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if src[payload.EndByte] != '\n' {
		t.Errorf("byte at EndByte is %q, want a newline; the span should stop at the closing brace", src[payload.EndByte])
	}
	if src[payload.EndByte-1] != '}' {
		t.Errorf("byte before EndByte is %q, want the closing brace", src[payload.EndByte-1])
	}
}

// TestCompactGetAndBounds pins the compact forms, which are the least specified surface in the
// tool and therefore the most likely to drift.
func TestCompactGetAndBounds(t *testing.T) {
	dir := t.TempDir()
	path := testfixture.Write(t, dir, "store.go", testfixture.Store)
	opts := format.Options{Compact: true}

	var boundsBuf, getBuf bytes.Buffer
	if err := usecase.Bounds(goast.New(), "UserStore.GetUser", path, &boundsBuf, opts); err != nil {
		t.Fatalf("Bounds: %v", err)
	}
	if err := usecase.Get(goast.New(), "UserStore.GetUser", path, true, &getBuf, opts); err != nil {
		t.Fatalf("Get: %v", err)
	}

	// Compact bounds keeps every field, drop-for-drop on one line.
	bounds := strings.TrimRight(boundsBuf.String(), "\n")
	if strings.Count(bounds, "\n") != 0 {
		t.Errorf("compact bounds must be one line:\n%q", bounds)
	}
	for _, want := range []string{"UserStore.GetUser", "28-35", "bytes"} {
		if !strings.Contains(bounds, want) {
			t.Errorf("compact bounds missing %q:\n%s", want, bounds)
		}
	}

	// Compact get keeps the symbol and the source.
	if !strings.Contains(getBuf.String(), "UserStore.GetUser") {
		t.Errorf("compact get missing symbol:\n%s", getBuf.String())
	}
	if !strings.Contains(getBuf.String(), "ErrNotFound") {
		t.Errorf("compact get missing source:\n%s", getBuf.String())
	}
}

// TestResolveAcrossFixtureClasses confirms the resolver works on the awkward fixture shapes:
// grouped declarations, CRLF, unicode identifiers, and a file with no declarations.
func TestResolveAcrossFixtureClasses(t *testing.T) {
	dir := t.TempDir()

	t.Run("grouped const and var", func(t *testing.T) {
		path := testfixture.Write(t, dir, "grouped.go", "package grouped\n\nconst (\n\tAlpha = 1\n\tBeta  = 2\n)\n\nvar (\n\tGamma = 3\n\tDelta = 4\n)\n")
		for _, name := range []string{"Alpha", "Beta", "Gamma", "Delta"} {
			if _, _, err := usecase.Resolve(goast.New(), name, path); err != nil {
				t.Errorf("Resolve %s: %v", name, err)
			}
		}
	})

	t.Run("unicode identifier", func(t *testing.T) {
		path := testfixture.Write(t, dir, "unicode.go", testfixture.Unicode)
		sym, doc, err := usecase.Resolve(goast.New(), "Ünïcödé", path)
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if !strings.Contains(sym.Bounds.Source(doc.Source), "日本語") {
			t.Errorf("unicode body not extracted:\n%q", sym.Bounds.Source(doc.Source))
		}
	})

	t.Run("CRLF source", func(t *testing.T) {
		path := testfixture.Write(t, dir, "crlf.go", testfixture.Crlf)
		sym, doc, err := usecase.Resolve(goast.New(), "f", path)
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if got := sym.Bounds.Source(doc.Source); got != "func f() {\r\n}" {
			t.Errorf("span = %q, want CRLF preserved", got)
		}
	})

	t.Run("file with no declarations", func(t *testing.T) {
		path := testfixture.Write(t, dir, "comments.go", testfixture.CommentsOnly)
		if _, _, err := usecase.Resolve(goast.New(), "anything", path); err == nil {
			t.Error("expected not-found for a file with no declarations")
		}
	})

	t.Run("bare method name is ambiguous", func(t *testing.T) {
		path := testfixture.Write(t, dir, "ambiguous.go", testfixture.Ambiguous)
		_, _, err := usecase.Resolve(goast.New(), "Save", path)
		if err == nil {
			t.Fatal("expected ambiguity")
		}
		// The qualified form must resolve.
		if _, _, err := usecase.Resolve(goast.New(), "Alpha.Save", path); err != nil {
			t.Errorf("qualified name should resolve: %v", err)
		}
	})
}

// TestGetUnknownSymbolLeavesStdoutClean confirms a failed lookup writes nothing to stdout, so a
// caller piping to jq never receives a partial document.
func TestGetUnknownSymbolLeavesStdoutClean(t *testing.T) {
	dir := t.TempDir()
	path := testfixture.Write(t, dir, "store.go", testfixture.Store)

	var buf bytes.Buffer
	err := usecase.Get(goast.New(), "NoSuchSymbol", path, true, &buf, format.Options{})
	if err == nil {
		t.Fatal("expected not-found error")
	}
	if buf.Len() != 0 {
		t.Errorf("stdout should stay empty on error, got:\n%s", buf.String())
	}
}

// parsesAsFragment checks that a declaration slice is syntactically complete.
//
// A type or function declaration cannot be parsed as a file on its own, so it is wrapped in a
// package clause. A slice that cut a declaration short -- an unbalanced brace, a missing
// closing paren -- fails here even though it would round-trip byte-for-byte.
func parsesAsFragment(name, slice string) error {
	// A type declaration's bounds span the TypeSpec, not the "type" keyword, because the
	// engine locates the name so that a grouped declaration yields addressable symbols. The
	// same holds for an alias. Only a func span starts with a keyword.
	//
	// So the fragment is wrapped in whichever keyword its declaration needs, and a slice that
	// cut a declaration short still fails to parse.
	wrapped := "package p\n\n" + declarationPrefix(name, slice) + slice + "\n"
	_, err := parser.ParseFile(token.NewFileSet(), name, wrapped, parser.SkipObjectResolution)
	return err
}

// declarationPrefix returns the keyword a declaration slice needs before it can be parsed.
//
// The engine's bounds for a type or alias begin at the name, so "User struct {...}" needs
// "type " in front. A method slice likewise begins at the name after the receiver, so it needs
// "func (x T) ". A package-level func already carries its keyword.
func declarationPrefix(name, slice string) string {
	switch {
	case strings.HasPrefix(slice, "func "), strings.HasPrefix(slice, "var "), strings.HasPrefix(slice, "const "):
		return ""
	case strings.Contains(name, "."):
		// A method: the receiver is not part of the span, so it must be restored.
		receiver := strings.SplitN(name, ".", 2)[0]
		return "func (recv *" + receiver + ") "
	default:
		return "type "
	}
}

// mustResolveOne resolves exactly one symbol, failing the test otherwise. Document.Lookup
// returns a slice so a caller can detect ambiguity; here ambiguity would itself be the bug.
func mustResolveOne(t *testing.T, doc domain.Document, name string) domain.Symbol {
	t.Helper()
	matches := doc.Lookup(name)
	if len(matches) != 1 {
		t.Fatalf("Lookup(%q) returned %d matches, want exactly 1", name, len(matches))
	}
	return matches[0]
}

func shortName(qualified string) string {
	if i := strings.LastIndex(qualified, "."); i >= 0 {
		return qualified[i+1:]
	}
	return qualified
}

func countLines(b []byte) int {
	n := 0
	for _, c := range b {
		if c == '\n' {
			n++
		}
	}
	return n
}
