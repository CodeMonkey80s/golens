package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Spec conformance: each test below cites a claim from specification.md and asserts the
// shipped behaviour matches it.
//
// These exist because every session so far verified only its own surface. A claim can be
// implemented correctly, tested thoroughly, and still drift from the document that defines it.
// The exit-code table and the schema-versioning rule are the highest-risk claims because an
// agent depends on them mechanically.

// TestSpecExitCodeTable verifies the table in specification 2 against every reachable outcome.
func TestSpecExitCodeTable(t *testing.T) {
	dir := t.TempDir()
	write := func(name, src string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}

	good := write("good.go", "package p\n\ntype T struct{ Id int }\n\nfunc (t T) M() {}\n")
	unused := write("unused.go", "package p\n\nimport \"fmt\"\n\nfunc F() {}\n")
	dup := write("dup.go", "package p\n\nfunc F() {}\n\nfunc F() {}\n")

	tests := []struct {
		specCode int
		what     string
		args     []string
	}{
		{0, "outline succeeds", []string{"outline", good}},
		{0, "get succeeds", []string{"get", "T.M", good}},
		{0, "bounds succeeds", []string{"bounds", "T.M", good}},
		{0, "check on a clean file", []string{"check", good}},

		{1, "usage: missing argument", []string{"outline"}},
		{1, "usage: too many arguments", []string{"bounds", "a", "b", "c"}},
		{1, "usage: unknown flag", []string{"outline", good, "--nope"}},

		{2, "not found: unknown symbol", []string{"get", "Missing", good}},
		{2, "not found: missing file", []string{"outline", filepath.Join(dir, "nope.go")}},

		{3, "diagnostics: unused import", []string{"check", unused}},
		{3, "diagnostics: redeclaration", []string{"check", dup}},

		{4, "parse failure: outline on a broken file", []string{"outline", write("broken.go", "package p\n\nfunc f( {\n")}},
	}

	for _, tt := range tests {
		t.Run(tt.what, func(t *testing.T) {
			_, _, code := run(t, tt.args...)
			if code != tt.specCode {
				t.Errorf("exit = %d, spec says %d for: %v", code, tt.specCode, tt.args)
			}
		})
	}
}

// TestSpecAmbiguousSymbolExitsTwo covers the ambiguity clause in specification 2.2: exit 2 with
// the candidates listed on stderr rather than a guess.
func TestSpecAmbiguousSymbolExitsTwo(t *testing.T) {
	dir := t.TempDir()
	for name, src := range map[string]string{
		"a.go": "package p\n\ntype A struct{}\n\nfunc (A) Save() {}\n",
		"b.go": "package p\n\ntype B struct{}\n\nfunc (B) Save() {}\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	stdout, stderr, code := run(t, "get", "Save", dir)
	if code != 2 {
		t.Errorf("exit = %d, spec says 2 for ambiguity", code)
	}
	if stdout != "" {
		t.Errorf("stdout must be empty on failure, got:\n%s", stdout)
	}
	for _, candidate := range []string{"A.Save", "B.Save"} {
		if !strings.Contains(stderr, candidate) {
			t.Errorf("spec says the candidates are listed; %s missing from:\n%s", candidate, stderr)
		}
	}

	// A bare method name is accepted when unique, per the same clause.
	unique := filepath.Join(dir, "c.go")
	if err := os.WriteFile(unique, []byte("package p\n\ntype C struct{}\n\nfunc (C) Only() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, code := run(t, "get", "Only", dir); code != 0 {
		t.Errorf("a unique bare method name should resolve, exit = %d", code)
	}
}

// TestSpecSchemaVersionIsFirstField verifies the schema-versioning clause in specification 2 for
// every JSON-producing command. An agent reads this field to detect format drift.
func TestSpecSchemaVersionIsFirstField(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "p.go")
	if err := os.WriteFile(path, []byte("package p\n\nimport \"fmt\"\n\ntype T struct{ Id int }\n\nfunc (t T) M() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, args := range [][]string{
		{"outline", path, "--json"},
		{"get", "T.M", path, "--json"},
		{"bounds", "T.M", path, "--json"},
		{"check", path, "--json"},
	} {
		t.Run(args[0], func(t *testing.T) {
			stdout, stderr, code := run(t, args...)

			// check exits 3 because the file has an unused import; its payload is still
			// required by the spec.
			if code != 0 && (args[0] != "check" || code != 3) {
				t.Fatalf("exit = %d (stderr: %s)", code, stderr)
			}
			if stderr != "" {
				t.Errorf("stderr must stay clean in JSON mode:\n%s", stderr)
			}

			if first := firstKey(stdout); first != "schema_version" {
				t.Errorf("first field = %q, spec says schema_version must come first:\n%s", first, stdout)
			}
		})
	}
}

// TestSpecCompactOverridesJSON verifies the precedence clause: "--compact takes precedence and
// emits non-indented JSON".
func TestSpecCompactOverridesJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "p.go")
	if err := os.WriteFile(path, []byte("package p\n\ntype T struct{ Id int }\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	pretty, _, _ := run(t, "bounds", "T", path, "--json")
	compact, _, _ := run(t, "bounds", "T", path, "--json", "--compact")

	if !strings.Contains(pretty, "\n  ") {
		t.Errorf("--json alone should be indented:\n%s", pretty)
	}
	if strings.Contains(compact, "\n  ") {
		t.Errorf("spec says --compact wins and emits non-indented JSON:\n%s", compact)
	}
	if !json.Valid([]byte(compact)) {
		t.Errorf("compact JSON must remain valid:\n%s", compact)
	}

	// Same data, different presentation.
	var a, b map[string]any
	if err := json.Unmarshal([]byte(pretty), &a); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(compact), &b); err != nil {
		t.Fatal(err)
	}
	for k, v := range a {
		if b[k] != v {
			t.Errorf("compact lost %s: %v vs %v", k, b[k], v)
		}
	}
}

// TestSpecBoundsOnlyMatchesBounds verifies the clause in specification 2.2: with
// --bounds-only and --json the payload is identical to golens bounds.
func TestSpecBoundsOnlyMatchesBounds(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "p.go")
	if err := os.WriteFile(path, []byte("package p\n\nfunc (t T) M() int { return 1 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	viaGet, _, code := run(t, "get", "T.M", path, "--bounds-only", "--json")
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	viaBounds, _, code := run(t, "bounds", "T.M", path, "--json")
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}

	if viaGet != viaBounds {
		t.Errorf("spec says the payloads are identical.\nget:\n%s\nbounds:\n%s", viaGet, viaBounds)
	}
}

// TestSpecBoundsAcceptsDirectory verifies the input clause in specification 2.3: a directory
// resolves the symbol through the same search as get, and the JSON payloads stay identical.
func TestSpecBoundsAcceptsDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "p.go"), []byte("package p\n\nfunc F() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, code := run(t, "bounds", "F", dir)
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stdout, "bytes") {
		t.Errorf("bounds output does not carry a byte range:\n%s", stdout)
	}

	getJSON, _, getCode := run(t, "get", "F", dir, "--bounds-only", "--json")
	boundsJSON, _, boundsCode := run(t, "bounds", "F", dir, "--json")
	if getCode != 0 || boundsCode != 0 {
		t.Fatalf("exit codes: get=%d bounds=%d", getCode, boundsCode)
	}
	if getJSON != boundsJSON {
		t.Errorf("A6 says the payloads are identical.\nget:\n%s\nbounds:\n%s", getJSON, boundsJSON)
	}
}

// TestSpecDirectoryTraversalSkips verifies the traversal clause in specification 2.1: .git,
// vendor and hidden directories are skipped, and only .go files are considered.
func TestSpecDirectoryTraversalSkips(t *testing.T) {
	dir := t.TempDir()
	write := func(rel, src string) {
		path := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	write("keep.go", "package keep\n\nfunc Kept() {}\n")
	write("notes.txt", "ignored")
	write("README.md", "ignored")
	write("vendor/v.go", "package v\n\nfunc Vendored() {}\n")
	write(".git/g.go", "package g\n\nfunc Git() {}\n")
	write(".hidden/h.go", "package h\n\nfunc Hidden() {}\n")
	write("nested/n.go", "package n\n\nfunc Nested() {}\n")

	stdout, _, code := run(t, "outline", dir)
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}

	for _, want := range []string{"Kept", "Nested"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("missing %s:\n%s", want, stdout)
		}
	}
	for _, unwanted := range []string{"Vendored", "Git", "Hidden", "ignored"} {
		if strings.Contains(stdout, unwanted) {
			t.Errorf("%s should have been skipped:\n%s", unwanted, stdout)
		}
	}
}

// TestSpecOutlineJSONIsAlwaysArray verifies the JSON-shape clause in specification 2.1: always an
// array of per-file objects, including for a single file.
func TestSpecOutlineJSONIsAlwaysArray(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "p.go")
	if err := os.WriteFile(path, []byte("package p\n\nfunc F() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, target := range []string{path, dir} {
		stdout, _, code := run(t, "outline", target, "--json")
		if code != 0 {
			t.Fatalf("exit = %d", code)
		}

		var files []map[string]any
		if err := json.Unmarshal([]byte(stdout), &files); err != nil {
			t.Fatalf("target %s: outline JSON is not an array: %v\n%s", target, err, stdout)
		}
		if len(files) != 1 {
			t.Errorf("target %s: got %d files, want 1", target, len(files))
		}
	}
}

// TestSpecCheckScope verifies the out-of-scope clause in specification 2.4: type-level conflicts
// and module import resolution are excluded, so a file that would need go/types must not be
// reported as conflicting.
func TestSpecCheckScope(t *testing.T) {
	dir := t.TempDir()
	// Same method name on different receiver types is legal Go and must stay silent.
	path := filepath.Join(dir, "ok.go")
	src := "package p\n\ntype A struct{}\n\ntype B struct{}\n\nfunc (A) S() {}\nfunc (B) S() {}\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	_, stderr, code := run(t, "check", path)
	if code != 0 {
		t.Errorf("exit = %d, want 0; this construct is legal Go:\n%s", code, stderr)
	}
}

// TestSpecJSONIsPipeable is the practical form of the stdout/stderr clause: a caller running
// golens check --json | jq must receive valid JSON on stdout no matter the outcome.
func TestSpecJSONIsPipeable(t *testing.T) {
	dir := t.TempDir()
	cases := map[string]string{
		"clean.go":        "package p\n\nfunc F() {}\n",
		"unused.go":       "package p\n\nimport \"fmt\"\n\nfunc F() {}\n",
		"broken.go":       "package p\n\nfunc f( {\n",
		"redeclared.go":   "package p\n\nfunc F() {}\n\nfunc F() {}\n",
		"mixed_unused.go": "package p\n\nimport (\n\t\"fmt\"\n\t\"os\"\n)\n\nfunc F() {}\n",
	}

	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(dir, name)
			if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
				t.Fatal(err)
			}

			stdout, stderr, _ := run(t, "check", path, "--json")

			if stderr != "" {
				t.Errorf("spec says diagnostics go to stdout as JSON in --json mode; stderr had:\n%s", stderr)
			}
			var payload struct {
				SchemaVersion int `json:"schema_version"`
				Diagnostics   []struct {
					Message string `json:"message"`
				} `json:"diagnostics"`
			}
			if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
				t.Fatalf("stdout is not valid JSON, so `golens check --json | jq` breaks: %v\n%s", err, stdout)
			}
		})
	}
}

// TestSpecHelpMentionsUsage confirms each command documents itself, which the spec's usage
// blocks imply a caller can rely on.
func TestSpecHelpMentionsUsage(t *testing.T) {
	for _, cmd := range []string{"outline", "get", "bounds", "check"} {
		stdout, _, code := run(t, cmd, "--help")
		if code != 0 {
			t.Errorf("%s --help exit = %d", cmd, code)
		}
		if !strings.Contains(stdout, "Usage:") {
			t.Errorf("%s --help has no usage line:\n%s", cmd, stdout)
		}
		if !strings.Contains(stdout, "golens "+cmd) {
			t.Errorf("%s --help does not show its invocation:\n%s", cmd, stdout)
		}
	}
}

// firstKey returns the first JSON key in an object payload.
func firstKey(payload string) string {
	trimmed := strings.TrimLeft(payload, " \t\r\n")
	if !strings.HasPrefix(trimmed, "{") {
		// An array payload: use the first key of its first element.
		start := strings.Index(trimmed, "{")
		if start < 0 {
			return ""
		}
		trimmed = trimmed[start:]
	}
	start := strings.Index(trimmed, `"`)
	if start < 0 {
		return ""
	}
	rest := trimmed[start+1:]
	before, _, ok := strings.Cut(rest, `"`)
	if !ok {
		return ""
	}
	return before
}
