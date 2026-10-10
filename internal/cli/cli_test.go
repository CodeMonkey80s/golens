package cli

import (
	"bytes"
	"encoding/json"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CodeMonkey80s/golens/internal/engine/goast"
	"github.com/CodeMonkey80s/golens/internal/testfixture"
)

const userGo = `package store

import "context"

type UserStore struct {
	mu    sync.Mutex
	cache map[int]*User
}

func (s *UserStore) GetUser(ctx context.Context, id int) (*User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if u, ok := s.cache[id]; ok {
		return u, nil
	}
	return nil, ErrNotFound
}
`

// run executes the command tree against buffers and returns stdout, stderr and the exit
// code. Nothing here touches os.Stdout, so tests can run in parallel.
func run(t *testing.T, args ...string) (stdout, stderr string, code int) {
	t.Helper()

	var out, errOut bytes.Buffer
	root := New(Env{Stdout: &out, Stderr: &errOut, Engine: goast.New()})
	root.SetArgs(args)
	root.SetOut(&out)
	root.SetErr(&errOut)
	code = Execute(root)
	return out.String(), errOut.String(), code
}

func fixture(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

func TestOutlineExitZero(t *testing.T) {
	path := fixture(t, "user.go", userGo)
	stdout, stderr, code := run(t, "outline", path)

	if code != 0 {
		t.Errorf("exit = %d, want 0 (stderr: %s)", code, stderr)
	}
	for _, want := range []string{"package: store", "FUNCTIONS & METHODS", "GetUser"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout missing %q:\n%s", want, stdout)
		}
	}
}

// TestExitCodes pins the documented contract: 0 success, 1 usage, 2 not found or
// ambiguous, 3 diagnostics, 4 parse failure.
func TestExitCodes(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "user.go")
	if err := os.WriteFile(good, []byte(userGo), 0o644); err != nil {
		t.Fatal(err)
	}
	broken := filepath.Join(dir, "broken.go")
	if err := os.WriteFile(broken, []byte("package p\n\nfunc f( {\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	unused := filepath.Join(dir, "unused.go")
	if err := os.WriteFile(unused, []byte("package p\n\nimport \"fmt\"\n\nfunc F() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(dir, "nope.go")

	tests := []struct {
		name string
		args []string
		want int
	}{
		{"outline ok", []string{"outline", good}, 0},
		{"get ok", []string{"get", "GetUser", good}, 0},
		{"bounds ok", []string{"bounds", "GetUser", good}, 0},
		{"check clean", []string{"check", good}, 0},
		{"missing arg", []string{"outline"}, 1},
		{"too many args", []string{"bounds", "a", "b", "c"}, 1},
		{"unknown symbol", []string{"get", "NoSuchThing", good}, 2},
		// A path that does not exist is a not-found condition (2), the same as a missing
		// symbol: both mean the caller named something absent. Exit 4 is reserved for a
		// file that exists but cannot be parsed.
		{"missing file", []string{"outline", missing}, 2},
		{"bounds on directory", []string{"bounds", "GetUser", dir}, 1},
		{"check finds diagnostics", []string{"check", unused}, 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, code := run(t, tt.args...)
			if code != tt.want {
				t.Errorf("exit = %d, want %d", code, tt.want)
			}
		})
	}
}

// TestCheckBrokenFileExitsThree confirms a parse failure is a diagnostic (exit 3) rather
// than a hard failure (exit 4), since check's job is to report it.
func TestCheckBrokenFileExitsThree(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "broken.go")
	if err := os.WriteFile(path, []byte("package p\n\nfunc f( {\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, stderr, code := run(t, "check", path)
	if code != 3 {
		t.Errorf("exit = %d, want 3", code)
	}
	if !strings.Contains(stderr, "broken.go") {
		t.Errorf("stderr missing file name:\n%s", stderr)
	}
}

// TestStdoutStaysCleanOnError is the property that makes --json pipeable: diagnostics and
// errors go to stderr so stdout carries only the payload.
func TestStdoutStaysCleanOnError(t *testing.T) {
	path := fixture(t, "unused.go", "package p\n\nimport \"fmt\"\n\nfunc F() {}\n")

	stdout, stderr, code := run(t, "check", path, "--json")
	if code != 3 {
		t.Errorf("exit = %d, want 3", code)
	}
	if stderr != "" {
		t.Errorf("stderr should be clean in JSON mode:\n%s", stderr)
	}

	var payload struct {
		SchemaVersion int `json:"schema_version"`
		Diagnostics   []struct {
			Message string `json:"message"`
		} `json:"diagnostics"`
	}
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("stdout is not clean JSON: %v\n%s", err, stdout)
	}
	if len(payload.Diagnostics) == 0 {
		t.Error("expected diagnostics")
	}
}

// TestErrorMessageOnStderr confirms failures never pollute stdout.
func TestErrorMessageOnStderr(t *testing.T) {
	path := fixture(t, "user.go", userGo)

	stdout, stderr, code := run(t, "get", "NoSuchThing", path)
	if code != 2 {
		t.Errorf("exit = %d, want 2", code)
	}
	if stdout != "" {
		t.Errorf("stdout should be empty on error, got:\n%s", stdout)
	}
	if !strings.Contains(stderr, "NoSuchThing") {
		t.Errorf("stderr should name the missing symbol:\n%s", stderr)
	}
}

// TestAmbiguousSymbolListsCandidates covers decision D17 at the CLI boundary: exit 2 with
// every candidate named, so the caller can correct the symbol rather than guess.
func TestAmbiguousSymbolListsCandidates(t *testing.T) {
	dir := t.TempDir()
	for name, content := range map[string]string{
		"a.go": "package p\n\ntype A struct{}\n\nfunc (A) Save() {}\n",
		"b.go": "package p\n\ntype B struct{}\n\nfunc (B) Save() {}\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	stdout, stderr, code := run(t, "get", "Save", dir)
	if code != 2 {
		t.Errorf("exit = %d, want 2", code)
	}
	if stdout != "" {
		t.Errorf("stdout should be empty, got:\n%s", stdout)
	}
	if !strings.Contains(stderr, "ambiguous") {
		t.Errorf("stderr should report ambiguity:\n%s", stderr)
	}
	for _, want := range []string{"A.Save", "B.Save"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr missing candidate %s:\n%s", want, stderr)
		}
	}
}

// TestGlobalFlagsAcceptedBeforeSubcommand confirms the persistent flags work in either
// position, which agents rely on when composing commands.
func TestGlobalFlagsAcceptedBeforeSubcommand(t *testing.T) {
	path := fixture(t, "user.go", userGo)

	for _, args := range [][]string{
		{"get", "GetUser", path, "--json"},
		{"--json", "get", "GetUser", path},
	} {
		stdout, stderr, code := run(t, args...)
		if code != 0 {
			t.Errorf("%v: exit = %d (stderr: %s)", args, code, stderr)
		}
		if !json.Valid([]byte(stdout)) {
			t.Errorf("%v: stdout is not JSON:\n%s", args, stdout)
		}
	}
}

// TestCompactOverridesIndent covers decision D5: --compact wins over pretty --json.
func TestCompactOverridesIndent(t *testing.T) {
	path := fixture(t, "user.go", userGo)

	stdout, _, code := run(t, "bounds", "GetUser", path, "--json", "--compact")
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if strings.Contains(stdout, "\n  ") {
		t.Errorf("--compact should not indent:\n%s", stdout)
	}
	if !json.Valid([]byte(stdout)) {
		t.Errorf("compact output is not valid JSON:\n%s", stdout)
	}
}

// TestHelpIsAvailable confirms the command tree is discoverable.
func TestHelpIsAvailable(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"outline", "--help"}} {
		stdout, _, code := run(t, args...)
		if code != 0 {
			t.Errorf("%v: exit = %d", args, code)
		}
		if !strings.Contains(stdout, "golens") {
			t.Errorf("%v: help should mention the binary name:\n%s", args, stdout)
		}
	}
}

// TestCheckNamingFlag exercises golens check --naming, the CLI surface for the
// initialism convention from specification 3.1.
//
// The flag replaces the syntax checks rather than adding to them: a caller asking about
// naming should not also receive unused-import warnings, and vice versa.
func TestCheckNamingFlag(t *testing.T) {
	dir := t.TempDir()
	// HttpHandler is banned: the initialism is mixed-case. HTTPHandler is what Go
	// wants, since the initialism opens an exported identifier.
	bad := filepath.Join(dir, "bad.go")
	if err := os.WriteFile(bad, []byte("package p\n\nfunc HttpHandler() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Without the flag, an unused import is what gets reported.
	withImport := filepath.Join(dir, "unused.go")
	if err := os.WriteFile(withImport, []byte("package p\n\nimport \"fmt\"\n\nfunc F() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, stderr, code := run(t, "check", withImport, "--naming")
	if code != 0 {
		t.Errorf("naming check should ignore unused imports, exit = %d (stderr: %s)", code, stderr)
	}
	if stderr != "" {
		t.Errorf("expected no naming diagnostics, got:\n%s", stderr)
	}

	_, stderr, code = run(t, "check", bad, "--naming")
	if code != 3 {
		t.Errorf("exit = %d, want 3 for naming violations (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stderr, "HttpHandler should be HTTPHandler") {
		t.Errorf("stderr missing the offending identifier and its fix:\n%s", stderr)
	}

	// The same file is clean under the default syntax checks.
	_, stderr, code = run(t, "check", bad)
	if code != 0 {
		t.Errorf("default check should not report naming, exit = %d (stderr: %s)", code, stderr)
	}
}

// TestOutlineCompactFlag exercises the CLI surface of --compact on outline, and pins the
// lossless-subset property from the outside: an agent reading compact output must still find
// every declaration and its line range.
func TestOutlineCompactFlag(t *testing.T) {
	path := fixture(t, "user.go", userGo)

	full, _, code := run(t, "outline", path)
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	compact, _, code := run(t, "outline", path, "--compact")
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}

	// Every declaration in the full form survives compaction.
	for _, fact := range []string{"GetUser", "UserStore", "context", "package: store"} {
		if !strings.Contains(full, fact) {
			t.Errorf("full output missing %q:\n%s", fact, full)
		}
	}

	// Decoration is dropped.
	for _, decoration := range []string{"•", "IMPORTS", "TYPES & STRUCTS"} {
		if strings.Contains(compact, decoration) {
			t.Errorf("compact output should drop %q:\n%s", decoration, compact)
		}
	}

	// The first line carries path and package, tab separated.
	first := strings.SplitN(compact, "\n", 2)[0]
	if !strings.Contains(first, "user.go") || !strings.Contains(first, "store") {
		t.Errorf("compact first line = %q, want path and package", first)
	}
}

// TestOutlineDepthFlag exercises --depth through the CLI, including the unlimited default.
func TestOutlineDepthFlag(t *testing.T) {
	path := fixture(t, "user.go", userGo)

	deep, _, code := run(t, "outline", path)
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	shallow, _, code := run(t, "outline", path, "--depth", "1")
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}

	// Default is unlimited, so nested members appear: the struct's fields and the method's
	// parameters. Note the method's own signature still contains "ctx context.Context" at any
	// depth, so the parameter *entry* is matched by its leading bullet instead.
	for _, nested := range []string{"• mu sync.Mutex", "• ctx context.Context"} {
		if !strings.Contains(deep, nested) {
			t.Errorf("default depth should include %q:\n%s", nested, deep)
		}
		if strings.Contains(shallow, nested) {
			t.Errorf("--depth 1 must not include %q:\n%s", nested, shallow)
		}
	}

	// Unlimited must not collapse to depth 1.
	if deep == shallow {
		t.Error("default (unlimited) depth produced identical output to --depth 1")
	}
}

// TestOverwriteRoundTripThroughCli is the end-to-end version of the tool's central claim: an
// agent reads offsets from golens bounds, overwrites that span, and lands on a file that still
// compiles structurally.
//
// Everything else in the suite checks that get and bounds agree with each other. This checks
// that they agree with the file on disk, which is the only thing that matters to a caller.
func TestOverwriteRoundTripThroughCli(t *testing.T) {
	path := fixture(t, "user.go", userGo)

	var buf bytes.Buffer
	root := New(Env{Stdout: &buf, Stderr: &bytes.Buffer{}, Engine: goast.New()})
	root.SetArgs([]string{"bounds", "UserStore.GetUser", path, "--json", "--compact"})
	root.SetOut(&buf)
	if code := Execute(root); code != 0 {
		t.Fatalf("bounds exit = %d: %s", code, buf.String())
	}

	var payload struct {
		StartByte int `json:"start_byte"`
		EndByte   int `json:"end_byte"`
	}
	if err := json.Unmarshal(buf.Bytes(), &payload); err != nil {
		t.Fatalf("Unmarshal: %v\n%s", err, buf.String())
	}

	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	const replacement = "func (s *UserStore) GetUser() error { return nil }"
	edited := string(original[:payload.StartByte]) + replacement + string(original[payload.EndByte:])

	if _, err := parser.ParseFile(token.NewFileSet(), path, edited, parser.SkipObjectResolution); err != nil {
		t.Fatalf("edited file does not parse: %v", err)
	}

	// Everything outside the edited span must survive untouched. The fixture declares one type
	// (UserStore), one method (GetUser) and an import, so checking the surrounding text and the
	// declaration count catches an overrunning span without depending on a fixture layout.
	if !strings.Contains(edited, "type UserStore struct") {
		t.Error("the declaration before the edit was lost; the span overran backwards")
	}
	if !strings.Contains(edited, `import "context"`) {
		t.Error("the import block was lost")
	}

	// Exactly one func must remain: the replacement. The fixture declares one method.
	if got := strings.Count(edited, "func "); got != 1 {
		t.Errorf("func count = %d, want 1 (the replacement); the span did not cover the original", got)
	}
	if strings.Contains(edited, "s.cache") || strings.Contains(edited, "s.mu.Lock") {
		t.Error("the original method body survived the overwrite")
	}
}

// TestCheckFlagsAcrossCli covers the checks reachable from the command line, including the
// cross-file namespace conflict that a per-file pass cannot see.
func TestCheckFlagsAcrossCli(t *testing.T) {
	dir := t.TempDir()
	write := func(name, src string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}

	t.Run("cross-file duplicate", func(t *testing.T) {
		sub := filepath.Join(dir, "pkg")
		if err := os.MkdirAll(sub, 0o755); err != nil {
			t.Fatal(err)
		}
		write("pkg/a.go", "package p\n\nfunc Shared() {}\n")
		write("pkg/b.go", "package p\n\nfunc Shared() {}\n")

		_, stderr, code := run(t, "check", sub)
		if code != 3 {
			t.Errorf("exit = %d, want 3", code)
		}
		if !strings.Contains(stderr, "Shared") {
			t.Errorf("stderr missing the conflicting name:\n%s", stderr)
		}
		// Exactly one diagnostic: a conflict must not be reported twice.
		if got := strings.Count(strings.TrimSpace(stderr), "\n"); got != 0 {
			t.Errorf("expected 1 diagnostic, got %d:\n%s", got+1, stderr)
		}
	})

	t.Run("unused import", func(t *testing.T) {
		path := write("u.go", "package p\n\nimport \"fmt\"\n\nfunc F() {}\n")
		_, stderr, code := run(t, "check", path)
		if code != 3 {
			t.Errorf("exit = %d, want 3", code)
		}
		if !strings.Contains(stderr, "imported and not used") {
			t.Errorf("stderr missing unused-import diagnostic:\n%s", stderr)
		}
	})

	t.Run("used import is silent", func(t *testing.T) {
		path := write("c.go", "package p\n\nimport \"fmt\"\n\nfunc F() { fmt.Println() }\n")
		_, stderr, code := run(t, "check", path)
		if code != 0 {
			t.Errorf("exit = %d, want 0 (stderr: %s)", code, stderr)
		}
	})

	t.Run("method names may repeat across types", func(t *testing.T) {
		path := write("m.go", "package p\n\ntype A struct{}\n\ntype B struct{}\n\nfunc (A) S() {}\nfunc (B) S() {}\n")
		_, stderr, code := run(t, "check", path)
		if code != 0 {
			t.Errorf("exit = %d, want 0: methods on different types do not conflict (stderr: %s)", code, stderr)
		}
	})
}

// TestCheckNamingFlagAcrossFiles confirms the naming check works over a whole directory, which is
// how it would run in CI.
func TestCheckNamingFlagAcrossFiles(t *testing.T) {
	dir := t.TempDir()
	for name, src := range map[string]string{
		"clean.go": "package p\n\nfunc userID() int { return 1 }\n",
		"bad.go":   "package p\n\nfunc HttpHandler() {}\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	_, stderr, code := run(t, "check", dir, "--naming")
	if code != 3 {
		t.Errorf("exit = %d, want 3 (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stderr, "HttpHandler") {
		t.Errorf("stderr missing the offending identifier:\n%s", stderr)
	}
	if strings.Contains(stderr, "userID") {
		t.Errorf("a compliant identifier was reported:\n%s", stderr)
	}
}

// TestAmbiguousErrorAdvisesDisambiguation covers the remedy line printed under the candidate
// list.
//
// The error names the problem and the help documents the fix, but a caller meets the error
// first. Without the advice the likely responses are to retry the same command or to conclude
// the tool is broken, and duf declares Mount.Stat in four platform files, so this is the
// ordinary case rather than an edge one.
func TestAmbiguousErrorAdvisesDisambiguation(t *testing.T) {
	dir := t.TempDir()
	testfixture.Write(t, dir, "a_linux.go", "//go:build linux\n\npackage p\n\ntype T struct{}\n\nfunc (T) M() {}\n")
	testfixture.Write(t, dir, "a_darwin.go", "//go:build darwin\n\npackage p\n\ntype T struct{}\n\nfunc (T) M() {}\n")

	var stdout, stderr bytes.Buffer
	cmd := New(Env{Stdout: &stdout, Stderr: &stderr, Engine: goast.New()})
	// Execute reports through cobra's error writer, which defaults to os.Stderr rather than
	// Env.Stderr, so it is pointed at the buffer explicitly.
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"get", "T.M", dir})

	if code := Execute(cmd); code != 2 {
		t.Fatalf("exit = %d, want 2 for an ambiguous symbol", code)
	}
	if !strings.Contains(stderr.String(), "pass a specific file") {
		t.Errorf("stderr lacks the disambiguation advice:\n%s", stderr.String())
	}
}

// TestVersionFlag covers --version, which most callers try before anything else.
//
// Cobra writes the version to the command's own output rather than the Env streams, so the
// test sets it explicitly; without that the assertion passes vacuously on an empty buffer.
func TestVersionFlag(t *testing.T) {
	var out bytes.Buffer
	cmd := New(Env{Stdout: &out, Stderr: &out, Engine: goast.New()})
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--version"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("--version: %v", err)
	}
	if !strings.Contains(out.String(), Version) {
		t.Errorf("--version printed %q, want it to contain %q", out.String(), Version)
	}
}
