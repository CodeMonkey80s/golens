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
		// The directory holds a broken file, so the outline is incomplete and the run still
		// fails. What it must not do is hide the parseable files.
		{"outline dir with a broken file", []string{"outline", dir}, 4},
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

// TestOutlineDeliversParseableFilesBeforeFailing is the end-to-end form of the contract an
// agent depends on: stdout carries everything that could be read, stderr names what could not,
// and the exit code says the outline is partial.
//
// outline used to abort before writing anything. The Go standard library contains exactly one
// unparseable file out of 8,088, so an outline of $GOROOT/src returned no output at all -- the
// most common answer a caller asks for was the one that failed.
func TestOutlineDeliversParseableFilesBeforeFailing(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a_broken.go"), []byte("package p\n\nfunc f( {\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b_good.go"), []byte("package p\n\nfunc Good() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, code := run(t, "outline", dir)

	if code != 4 {
		t.Errorf("exit = %d, want 4: a parse failure is real and must stay visible", code)
	}
	if !strings.Contains(stdout, "Good") {
		t.Errorf("stdout must carry the parseable files even on exit 4:\n%s", stdout)
	}
	if strings.Contains(stdout, "func f(") {
		t.Errorf("stdout must not carry the unparseable file:\n%s", stdout)
	}
	if !strings.Contains(stderr, "a_broken.go") {
		t.Errorf("stderr must name the skipped file:\n%s", stderr)
	}
}

// TestOutlineJSONOnBrokenTreeIsPipeClean covers the machine-consumer case: stdout has to stay
// valid JSON so `golens outline . --json | jq` works, with the failure on stderr.
func TestOutlineJSONOnBrokenTreeIsPipeClean(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a_broken.go"), []byte("package p\n\nfunc f( {\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b_good.go"), []byte("package p\n\nfunc Good() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout, _, code := run(t, "outline", dir, "--json", "--compact")
	if code != 4 {
		t.Errorf("exit = %d, want 4", code)
	}

	var payloads []struct {
		File          string `json:"file"`
		SchemaVersion int    `json:"schema_version"`
	}
	if err := json.Unmarshal([]byte(stdout), &payloads); err != nil {
		t.Fatalf("stdout is not valid JSON when a file is skipped: %v\n%s", err, stdout)
	}
	if len(payloads) != 1 {
		t.Fatalf("payloads = %d, want 1", len(payloads))
	}
	if !strings.HasSuffix(payloads[0].File, "b_good.go") {
		t.Errorf("outlined %q, want b_good.go", payloads[0].File)
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

// TestCheckHelpStatesTheStreamContract pins the help text to the behaviour.
//
// The help used to say "stdout stays empty" for check, which is true of the human form and false
// of --json, where the payload goes to stdout and stderr is empty. TestStdoutStaysCleanOnError
// below already proved the behaviour, so the two were contradicting each other: the suite was
// green and the documentation was wrong. Nothing connected a claim about behaviour to the test
// that verifies it, which is how that survives a code review.
//
// This test makes the connection. It asserts both halves of the contract the help must state --
// the human form's stderr and the JSON form's stdout -- and fails if the help is silent about
// either, so help cannot drift away from behaviour that a test already pins.
func TestCheckHelpStatesTheStreamContract(t *testing.T) {
	root := New(DefaultEnv(goast.New()))

	rootHelp := root.Long
	checkCmd, _, err := root.Find([]string{"check"})
	if err != nil {
		t.Fatalf("Find check: %v", err)
	}
	checkHelp := checkCmd.Long

	// The root help must not claim stdout is unconditionally empty, because --json contradicts
	// that. An unqualified claim is the bug; the fix is to scope it.
	for _, forbidden := range []string{
		"nothing to stdout",
		"stderr only",
	} {
		if strings.Contains(rootHelp, forbidden) {
			t.Errorf("root help contains %q, but check --json writes to stdout:\n%s", forbidden, rootHelp)
		}
	}

	// Both must name the JSON exception, or an agent piping --json has no way to know stdout
	// carries the payload.
	for name, help := range map[string]string{"root": rootHelp, "check": checkCmd.Long} {
		if !strings.Contains(help, "--json") {
			t.Errorf("%s help never mentions --json, so it cannot state the stream contract:\n%s", name, help)
		}
		if !strings.Contains(help, "stdout") || !strings.Contains(help, "stderr") {
			t.Errorf("%s help must name both streams to state the contract:\n%s", name, help)
		}
	}
	if !strings.Contains(checkHelp, "2>&1") {
		t.Errorf("check help must keep the human-form capture advice:\n%s", checkHelp)
	}
}

// TestCheckStreamContractHolds confirms the help describes what the code does, in both modes.
// The assertions mirror TestCheckHelpStatesTheStreamContract: if this fails, the help is right
// and the code is wrong.
func TestCheckStreamContractHolds(t *testing.T) {
	path := fixture(t, "unused.go", "package p\n\nimport \"fmt\"\n\nfunc F() {}\n")

	stdout, stderr, code := run(t, "check", path)
	if code != 3 {
		t.Fatalf("human form: exit = %d, want 3", code)
	}
	if stdout != "" {
		t.Errorf("human form: stdout should be empty, got:\n%s", stdout)
	}
	if !strings.Contains(stderr, "imported and not used") {
		t.Errorf("human form: stderr should carry the diagnostic, got:\n%s", stderr)
	}

	stdout, stderr, code = run(t, "check", path, "--json")
	if code != 3 {
		t.Fatalf("--json: exit = %d, want 3", code)
	}
	if stderr != "" {
		t.Errorf("--json: stderr should be empty so stdout pipes, got:\n%s", stderr)
	}
	var payload struct {
		SchemaVersion int `json:"schema_version"`
	}
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("--json: stdout is not a parseable payload: %v\n%s", err, stdout)
	}
	if payload.SchemaVersion != 1 {
		t.Errorf("--json: schema_version = %d, want 1", payload.SchemaVersion)
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

// TestCheckDoesNotJudgeStyle pins the decision that check reports structural faults only.
//
// golens used to ship a --naming flag. It was removed because a structural query should not
// return style judgements the caller did not ask for: an agent that ran check to find out
// whether a file was valid also received an opinion about its identifiers, and the cost of
// acting on that wrongly is rewriting working code. Naming is enforced by golangci-lint's
// revive instead, where opting in is explicit.
//
// This test fails if naming ever comes back through any route, including a new flag.
func TestCheckDoesNotJudgeStyle(t *testing.T) {
	dir := t.TempDir()
	// Every identifier here violates Go's initialism convention. The file is otherwise
	// valid Go and fully used, so a structural check has nothing to report about it.
	styled := filepath.Join(dir, "styled.go")
	src := "package p\n\ntype HttpHandler struct{}\ntype DbUrl struct{}\n\nfunc UserId() {}\n"
	if err := os.WriteFile(styled, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	// check exits 0 and says nothing: the file parses, nothing is unused, nothing is
	// duplicated.
	_, stderr, code := run(t, "check", styled)
	if code != 0 {
		t.Errorf("check reported on a valid file, exit = %d (stderr: %s)", code, stderr)
	}
	if stderr != "" {
		t.Errorf("check must not judge style, but said:\n%s", stderr)
	}

	// The flag is gone rather than merely ignored.
	_, _, code = run(t, "check", styled, "--naming")
	if code == 0 {
		t.Error("--naming still accepted; style enforcement does not belong in this tool")
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

// TestCheckAcrossFilesReportsNothingStructural confirms that a directory whose only
// irregularity is naming produces no diagnostics. It is the directory-scale companion to
// TestCheckDoesNotJudgeStyle: the flag it once exercised walked whole trees, so a
// regression that reintroduced style checking would show up here first.
func TestCheckAcrossFilesReportsNothingStructural(t *testing.T) {
	dir := t.TempDir()
	for name, src := range map[string]string{
		"a_clean.go":  "package p\n\nfunc userID() int { return 1 }\n",
		"b_styled.go": "package p\n\nfunc HttpHandler() {}\nfunc DbUrl() {}\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	_, stderr, code := run(t, "check", dir)
	if code != 0 {
		t.Errorf("exit = %d, want 0: nothing structural is wrong (stderr: %s)", code, stderr)
	}
	if stderr != "" {
		t.Errorf("check must not judge style, but said:\n%s", stderr)
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
