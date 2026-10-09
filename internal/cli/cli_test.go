package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CodeMonkey80s/golens/internal/domain"
	"github.com/CodeMonkey80s/golens/internal/engine/goast"
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

var _ = domain.ExitOk

// TestCheckNamingFlag exercises golens check --naming, the CLI surface for the
// camelCaseId convention from specification 3.1.
//
// The flag replaces the syntax checks rather than adding to them: a caller asking about
// naming should not also receive unused-import warnings, and vice versa.
func TestCheckNamingFlag(t *testing.T) {
	dir := t.TempDir()
	// HTTPHandler is banned: the initialism is all-caps. HttpHandler would be allowed,
	// since a single leading capital is ordinary camelCase.
	bad := filepath.Join(dir, "bad.go")
	if err := os.WriteFile(bad, []byte("package p\n\nfunc HTTPHandler() {}\n"), 0o644); err != nil {
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
	if !strings.Contains(stderr, "HTTPHandler") {
		t.Errorf("stderr missing the offending identifier:\n%s", stderr)
	}

	// The same file is clean under the default syntax checks.
	_, stderr, code = run(t, "check", bad)
	if code != 0 {
		t.Errorf("default check should not report naming, exit = %d (stderr: %s)", code, stderr)
	}
}
