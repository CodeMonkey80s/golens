package usecase_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/CodeMonkey80s/golens/internal/engine/goast"
	"github.com/CodeMonkey80s/golens/internal/format"
	"github.com/CodeMonkey80s/golens/internal/testfixture"
	"github.com/CodeMonkey80s/golens/internal/usecase"
)

// checkResult is what a caller gets back from check, so the tests below read the outcome
// rather than poking at the diagnostics slice.
type checkResult struct {
	diags []string
	code  int
}

// runCheck invokes check against a single file and collects the stderr diagnostics.
func runCheck(t *testing.T, dir, name, src string) checkResult {
	t.Helper()

	path := testfixture.Write(t, dir, name, src)

	var stdout, stderr bytes.Buffer
	err := usecase.Check(goast.New(), path, &stdout, &stderr, format.Options{})

	var out checkResult
	out.diags = strings.Split(strings.TrimRight(stderr.String(), "\n"), "\n")
	if len(out.diags) == 1 && out.diags[0] == "" {
		out.diags = nil
	}

	if err != nil {
		var hasDiags usecase.HasDiagnostics
		if !asDiagnostics(err, &hasDiags) {
			t.Fatalf("Check returned an unexpected error: %v", err)
		}
		out.code = 3
	} else {
		out.code = 0
	}
	return out
}

func (r checkResult) contains(sub string) bool {
	for _, d := range r.diags {
		if strings.Contains(d, sub) {
			return true
		}
	}
	return false
}

// TestCheckOutputMatchesSpecificationFormat pins the diagnostic shape from specification 2.4:
// file:line:col: message.
func TestCheckOutputMatchesSpecificationFormat(t *testing.T) {
	res := runCheck(t, t.TempDir(), "unused.go", "package p\n\nimport \"fmt\"\n\nfunc F() {}\n")

	if res.code != 3 {
		t.Errorf("exit = %d, want 3 when diagnostics exist", res.code)
	}
	if len(res.diags) != 1 {
		t.Fatalf("got %d diagnostics, want 1: %v", len(res.diags), res.diags)
	}

	// The message must start with the file path, then line:col.
	want := "unused.go:3:1: imported and not used: \"fmt\""
	if !strings.HasSuffix(res.diags[0], want) {
		t.Errorf("diagnostic = %q\nwant suffix %q", res.diags[0], want)
	}
}

// TestCheckExitCodes covers the exit-code contract for check: 0 when clean, 3 when diagnostics
// exist. Exit 3 rather than 1 or 4 because the check succeeded mechanically and found problems,
// which is a different outcome from a usage mistake or a parse failure.
func TestCheckExitCodes(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want int
	}{
		{"clean file", "package p\n\nimport \"fmt\"\n\nfunc F() { _ = fmt.Sprint(1) }\n", 0},
		{"unused import", "package p\n\nimport \"fmt\"\n\nfunc F() {}\n", 3},
		{"duplicate declaration", "package p\n\nfunc F() {}\n\nfunc F() {}\n", 3},
		{"syntax error", "package p\n\nfunc f( {\n", 3},
		{"no imports, no declarations", "package p\n", 0},
		{"comments only", "// nothing\npackage p\n", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := runCheck(t, t.TempDir(), "p.go", tt.src)
			if res.code != tt.want {
				t.Errorf("exit = %d, want %d (diagnostics: %v)", res.code, tt.want, res.diags)
			}
		})
	}
}

// TestUnusedImportDetection covers the detection matrix for the check that matters most.
func TestUnusedImportDetection(t *testing.T) {
	tests := []struct {
		name       string
		src        string
		wantUnused []string
	}{
		{
			name:       "single unused",
			src:        "package p\n\nimport \"fmt\"\n\nfunc F() {}\n",
			wantUnused: []string{`"fmt"`},
		},
		{
			name: "used as selector base",
			src:  "package p\n\nimport \"fmt\"\n\nfunc F() { fmt.Println() }\n",
		},
		{
			name: "one of two used",
			src:  "package p\n\nimport (\n\t\"fmt\"\n\t\"os\"\n)\n\nfunc F() { fmt.Println(os.Args) }\n",
		},
		{
			name:       "both unused",
			src:        "package p\n\nimport (\n\t\"fmt\"\n\t\"os\"\n)\n\nfunc F() {}\n",
			wantUnused: []string{`"fmt"`, `"os"`},
		},
		{
			name: "aliased and used",
			src:  "package p\n\nimport str \"strings\"\n\nfunc F() { str.ToUpper(\"x\") }\n",
		},
		{
			name:       "aliased and unused",
			src:        "package p\n\nimport str \"strings\"\n\nfunc F() {}\n",
			wantUnused: []string{`"strings"`},
		},
		{
			name: "blank import never reported",
			src:  "package p\n\nimport _ \"net/http/pprof\"\n\nfunc F() {}\n",
		},
		{
			name: "dot import never reported",
			src:  "package p\n\nimport . \"strings\"\n\nfunc F() { ToUpper(\"x\") }\n",
		},
		{
			name: "named import used",
			src:  "package p\n\nimport j \"encoding/json\"\n\nfunc F() { j.Marshal(nil) }\n",
		},
		{
			// Documented false negative: the local name appears in a comment, so the lexical
			// scan counts it as used. The bias is deliberate -- see the retrospective.
			name: "comment-only reference is a false negative",
			src:  "package p\n\nimport \"fmt\"\n\nfunc F() {\n\t// fmt.Println in a comment\n\tprintln(1)\n}\n",
		},
		{
			// Documented false negative: a local variable shadows the package name.
			name: "shadowed local is a false negative",
			src:  "package p\n\nimport \"os\"\n\nfunc F() {\n\tos := 1\n\t_ = os\n}\n",
		},
		{
			name: "type alias referencing the import",
			src:  "package p\n\nimport \"context\"\n\ntype Ctx = context.Context\n\nfunc F() {}\n",
		},
		{
			name: "import used only in a signature",
			src:  "package p\n\nimport \"io\"\n\nfunc F(w io.Writer) {}\n",
		},
		{
			name: "two imports sharing a prefix",
			src:  "package p\n\nimport (\n\t\"net/http\"\n\t\"net/http/httptest\"\n)\n\nfunc F() { _ = http.MethodGet; _ = httptest.NewServer }\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := runCheck(t, t.TempDir(), "p.go", tt.src)

			for _, want := range tt.wantUnused {
				if !res.contains("imported and not used: " + want) {
					t.Errorf("missing unused-import diagnostic for %s; got %v", want, res.diags)
				}
			}

			if len(tt.wantUnused) == 0 {
				for _, d := range res.diags {
					if strings.Contains(d, "imported and not used") {
						t.Errorf("unexpected unused-import diagnostic: %q", d)
					}
				}
			}
		})
	}
}

// TestUnusedImportNeverFalsePositive is the safety property the whole bias rests on.
//
// A false positive tells an agent to delete an import that is in use, which breaks the build. A
// false negative merely misses an unused import, which the Go compiler reports anyway. So this
// asserts the direction of the error rather than counting diagnostics.
func TestUnusedImportNeverFalsePositive(t *testing.T) {
	dir := t.TempDir()

	// Every import in this file is genuinely used, in a variety of positions.
	src := `package p

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type S struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func Use(ctx context.Context, w io.Writer, s []int) string {
	var e json.Encoder
	_ = e
	var d time.Duration
	_ = d
	sort.Ints(s)
	return fmt.Sprintf("%s%d%s%s%t",
		strings.ToUpper(os.Args[0]), strconv.Itoa(len(s)), ctx.Err() != nil, w != nil, s != nil)
}
`
	path := testfixture.Write(t, dir, "allused.go", src)

	var stdout, stderr bytes.Buffer
	if err := usecase.Check(goast.New(), path, &stdout, &stderr, format.Options{}); err != nil {
		t.Errorf("check reported diagnostics on a file where every import is used:\n%s", stderr.String())
	}
}

// TestDuplicateDeclarationDetection covers the AST-achievable namespace check.
func TestDuplicateDeclarationDetection(t *testing.T) {
	tests := []struct {
		name     string
		src      string
		wantDiag bool
	}{
		{"duplicate func", "package p\n\nfunc F() {}\n\nfunc F() {}\n", true},
		{"duplicate type", "package p\n\ntype T struct{}\n\ntype T struct{}\n", true},
		{"duplicate var", "package p\n\nvar A = 1\n\nvar A = 2\n", true},
		{"duplicate const", "package p\n\nconst A = 1\n\nconst A = 2\n", true},
		{"method with same name on different types", "package p\n\ntype A struct{}\n\ntype B struct{}\n\nfunc (A) S() {}\nfunc (B) S() {}\n", false},
		{"grouped const with distinct names", "package p\n\nconst (\n\tA = 1\n\tB = 2\n)\n", false},
		{"grouped var with distinct names", "package p\n\nvar (\n\tA = 1\n\tB = 2\n)\n", false},
		{"method and func sharing a name", "package p\n\ntype A struct{}\n\nfunc S() {}\nfunc (A) S() {}\n", false},
		{"type and func sharing a name", "package p\n\ntype A struct{}\n\nfunc A() {}\n", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := runCheck(t, t.TempDir(), "p.go", tt.src)
			got := res.contains("redeclared")
			if got != tt.wantDiag {
				t.Errorf("redeclared diagnostic = %v, want %v (diagnostics: %v)", got, tt.wantDiag, res.diags)
			}
		})
	}
}

// TestDuplicateAcrossFiles covers the multi-file case: a name declared in two files of one
// package is a genuine conflict.
func TestDuplicateAcrossFiles(t *testing.T) {
	dir := t.TempDir()
	testfixture.Write(t, dir, "a.go", "package p\n\nfunc Shared() {}\n")
	testfixture.Write(t, dir, "b.go", "package p\n\nfunc Shared() {}\n")

	var stdout, stderr bytes.Buffer
	err := usecase.Check(goast.New(), dir, &stdout, &stderr, format.Options{})

	if err == nil {
		t.Error("expected diagnostics for a name declared in two files")
	}
	if !strings.Contains(stderr.String(), "Shared") {
		t.Errorf("stderr missing the conflicting name:\n%s", stderr.String())
	}
}

// TestCheckContinuesAfterParseError confirms a broken file does not abort the run: check's job is
// to report problems, and it should report all of them in one pass.
func TestCheckContinuesAfterParseError(t *testing.T) {
	dir := t.TempDir()
	testfixture.Write(t, dir, "a_broken.go", "package p\n\nfunc f( {\n")
	testfixture.Write(t, dir, "b_unused.go", "package p\n\nimport \"fmt\"\n\nfunc F() {}\n")
	testfixture.Write(t, dir, "c_dup.go", "package p\n\nfunc G() {}\n\nfunc G() {}\n")

	var stdout, stderr bytes.Buffer
	if err := usecase.Check(goast.New(), dir, &stdout, &stderr, format.Options{}); err == nil {
		t.Fatal("expected diagnostics")
	}

	got := stderr.String()
	for _, want := range []string{"a_broken.go", "b_unused.go", "c_dup.go"} {
		if !strings.Contains(got, want) {
			t.Errorf("run stopped before reporting %s:\n%s", want, got)
		}
	}
}

// TestCheckJSONPayload pins the JSON shape: schema_version, a diagnostics array, and severity
// per entry.
func TestCheckJSONPayload(t *testing.T) {
	dir := t.TempDir()
	testfixture.Write(t, dir, "p.go", "package p\n\nimport \"fmt\"\n\nfunc F() {}\n\nfunc G() {}\n\nfunc G() {}\n")

	var stdout, stderr bytes.Buffer
	_ = usecase.Check(goast.New(), dir, &stdout, &stderr, format.Options{JSON: true})

	if stderr.Len() != 0 {
		t.Errorf("stderr must stay clean in JSON mode:\n%s", stderr.String())
	}

	var payload struct {
		SchemaVersion int `json:"schema_version"`
		Diagnostics   []struct {
			Severity string `json:"severity"`
			File     string `json:"file"`
			Line     int    `json:"line"`
			Column   int    `json:"column"`
			Message  string `json:"message"`
		} `json:"diagnostics"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\n%s", err, stdout.String())
	}

	if payload.SchemaVersion != format.SchemaVersion {
		t.Errorf("schema_version = %d, want %d", payload.SchemaVersion, format.SchemaVersion)
	}
	if len(payload.Diagnostics) != 2 {
		t.Fatalf("got %d diagnostics, want 2: %+v", len(payload.Diagnostics), payload.Diagnostics)
	}

	// The duplicate is an error; the unused import is a warning.
	severities := map[string]bool{}
	for _, d := range payload.Diagnostics {
		severities[d.Severity] = true
		if d.File == "" || d.Line == 0 || d.Column == 0 {
			t.Errorf("diagnostic missing position: %+v", d)
		}
	}
	if !severities["error"] || !severities["warning"] {
		t.Errorf("expected both severities, got %v", severities)
	}
}

// TestCheckEmptyArrayNotNull pins the wire form of a clean run.
//
// The diagnostics field is nil when there is nothing to report, and encoding it produced
// "diagnostics": null. A consumer that iterates the field or takes its length breaks on
// null -- and it breaks on exactly the runs that are supposed to be the easy ones, a clean
// tree under --json. The payload's shape must not depend on whether the run had anything to
// say, so a clean tree emits [].
func TestCheckEmptyArrayNotNull(t *testing.T) {
	dir := t.TempDir()
	testfixture.Write(t, dir, "clean.go", "package p\n\nimport \"fmt\"\n\nfunc F() { fmt.Println() }\n")

	var stdout, stderr bytes.Buffer
	if err := usecase.Check(goast.New(), dir, &stdout, &stderr, format.Options{JSON: true}); err != nil {
		t.Fatalf("Check: %v", err)
	}

	if stderr.Len() != 0 {
		t.Errorf("stderr must stay clean in JSON mode:\n%s", stderr.String())
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(stdout.Bytes(), &raw); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\n%s", err, stdout.String())
	}

	field, ok := raw["diagnostics"]
	if !ok {
		t.Fatalf("payload has no diagnostics field:\n%s", stdout.String())
	}
	if string(field) != "[]" {
		t.Errorf("diagnostics = %s, want [] -- null is not an array a consumer can iterate", field)
	}
}

// TestCheckDeterminism confirms repeated runs produce identical diagnostics, since an agent may
// diff or cache them.
func TestCheckDeterministicOutput(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.go", "b.go", "c.go"} {
		testfixture.Write(t, dir, name, "package p\n\nimport (\n\t\"fmt\"\n\t\"os\"\n)\n\nfunc F() {}\n")
	}

	var first, second bytes.Buffer
	var discard bytes.Buffer
	_ = usecase.Check(goast.New(), dir, &first, &discard, format.Options{})
	_ = usecase.Check(goast.New(), dir, &second, &discard, format.Options{})

	if first.String() != second.String() {
		t.Errorf("diagnostics not deterministic:\n%s\n---\n%s", first.String(), second.String())
	}
}

// TestCheckRejectsNonGoTarget covers the input validation: a non-Go file is a usage error, not a
// check failure.
func TestCheckRejectsNonGoTarget(t *testing.T) {
	dir := t.TempDir()
	path := testfixture.Write(t, dir, "notes.txt", "hello")

	var stdout, stderr bytes.Buffer
	err := usecase.Check(goast.New(), path, &stdout, &stderr, format.Options{})
	if err == nil {
		t.Fatal("expected an error for a non-Go file")
	}
	var usage interface{ IsUsage() bool }
	if !errors.As(err, &usage) {
		t.Errorf("expected a usage error, got %v", err)
	}
}

// TestCheckAcrossFixtureClasses runs check over the awkward fixtures to confirm it neither
// crashes nor produces spurious diagnostics.
func TestCheckAcrossFixtureClasses(t *testing.T) {
	tests := []struct {
		name string
		src  string
	}{
		{"crlf", testfixture.Crlf},
		{"bom", testfixture.Bom},
		{"unicode", testfixture.Unicode},
		{"empty", testfixture.Empty},
		{"comments only", testfixture.CommentsOnly},
		{"dot and blank imports", testfixture.ImportVariety},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := testfixture.Write(t, dir, "p.go", tt.src)

			var stdout, stderr bytes.Buffer
			err := usecase.Check(goast.New(), path, &stdout, &stderr, format.Options{})
			if err != nil {
				t.Errorf("unexpected diagnostics: %v\n%s", err, stderr.String())
			}
		})
	}
}

// TestCheckHandlesUnparseableFixture confirms the broken fixture reports rather than panics.
func TestCheckHandlesUnparseableFixture(t *testing.T) {
	dir := t.TempDir()
	path := testfixture.Write(t, dir, "broken.go", testfixture.Broken)

	var stdout, stderr bytes.Buffer
	err := usecase.Check(goast.New(), path, &stdout, &stderr, format.Options{})
	if err == nil {
		t.Fatal("expected diagnostics for an unparseable file")
	}
	if !strings.Contains(stderr.String(), "broken.go") {
		t.Errorf("stderr missing the file name:\n%s", stderr.String())
	}
}

// TestCheckSkipsVendorAndHidden confirms directory traversal honours the skip rules.
func TestCheckSkipsVendorAndHidden(t *testing.T) {
	dir := t.TempDir()
	testfixture.Write(t, dir, "clean.go", "package clean\n\nfunc F() {}\n")
	testfixture.Write(t, dir, "vendor/v.go", "package v\n\nimport \"fmt\"\n\nfunc F() {}\n")
	testfixture.Write(t, dir, ".hidden/h.go", "package h\n\nimport \"fmt\"\n\nfunc F() {}\n")
	testfixture.Write(t, dir, "node_modules/n.go", "package n\n\nimport \"fmt\"\n\nfunc F() {}\n")

	var stdout, stderr bytes.Buffer
	if err := usecase.Check(goast.New(), dir, &stdout, &stderr, format.Options{}); err != nil {
		t.Errorf("skipped directories were reported: %v\n%s", err, stderr.String())
	}
	if strings.Contains(stderr.String(), "fmt") {
		t.Errorf("a skipped directory produced a diagnostic:\n%s", stderr.String())
	}
}

func asDiagnostics(err error, target *usecase.HasDiagnostics) bool {
	return errors.As(err, target)
}

// runCheckDir invokes check against a whole directory and collects the stderr diagnostics.
func runCheckDir(t *testing.T, dir string) checkResult {
	t.Helper()

	var stdout, stderr bytes.Buffer
	err := usecase.Check(goast.New(), dir, &stdout, &stderr, format.Options{})

	var out checkResult
	out.diags = strings.Split(strings.TrimRight(stderr.String(), "\n"), "\n")
	if len(out.diags) == 1 && out.diags[0] == "" {
		out.diags = nil
	}
	if err != nil {
		var hasDiags usecase.HasDiagnostics
		if !asDiagnostics(err, &hasDiags) {
			t.Fatalf("Check returned an unexpected error: %v", err)
		}
		out.code = 3
	}
	return out
}

// TestDuplicateDeclarationRespectsBuildConstraints covers the distinction that separates a
// real conflict from a platform variant.
//
// A repository that declares isFuseFs in filesystems_linux.go, filesystems_darwin.go and
// filesystems_windows.go compiles one of them, never two. Reporting those as redeclarations
// is noise, and on duf it was 23 diagnostics against a repository the Go compiler accepts
// cleanly on every platform. The converse matters just as much: an untagged file and a tagged
// one really can collide, and that must still be reported or the check has been silenced
// rather than fixed.
func TestDuplicateDeclarationRespectsBuildConstraints(t *testing.T) {
	const body = "func isFuseFs() bool { return true }\n"

	tests := []struct {
		name    string
		files   map[string]string
		want    bool
		comment string
	}{
		{
			name: "mutually exclusive platforms",
			files: map[string]string{
				"a_linux.go":   "//go:build linux\n\npackage p\n\n" + body,
				"a_darwin.go":  "//go:build darwin\n\npackage p\n\n" + body,
				"a_windows.go": "//go:build windows\n\npackage p\n\n" + body,
			},
			want:    false,
			comment: "no build compiles two of these",
		},
		{
			name: "legacy plus-build syntax",
			files: map[string]string{
				"a_linux.go":  "// +build linux\n\npackage p\n\n" + body,
				"a_darwin.go": "// +build darwin\n\npackage p\n\n" + body,
			},
			want:    false,
			comment: "the pre-1.17 spelling must be honoured too",
		},
		{
			name: "overlapping tags really do conflict",
			files: map[string]string{
				"a.go":       "package p\n\n" + body,
				"b_linux.go": "//go:build linux\n\npackage p\n\n" + body,
			},
			want:    true,
			comment: "an untagged file and a linux one are compiled together",
		},
		{
			name: "two files sharing a tag really do conflict",
			files: map[string]string{
				"a_linux.go": "//go:build linux\n\npackage p\n\n" + body,
				"b_linux.go": "//go:build linux\n\npackage p\n\n" + body,
			},
			want:    true,
			comment: "both are built on linux",
		},
		{
			name: "disjoint platforms and a shared one",
			files: map[string]string{
				"a.go":        "//go:build linux || darwin\n\npackage p\n\n" + body,
				"b_darwin.go": "//go:build darwin\n\npackage p\n\n" + body,
			},
			want:    true,
			comment: "darwin satisfies both",
		},
		{
			// The _linux suffix narrows the tag rather than merely annotating it: a file
			// named a_linux.go is never built on darwin, whatever its tags say. Reading only
			// the comment made this pair look co-buildable on darwin.
			name: "a filename suffix narrows a broader tag",
			files: map[string]string{
				"a_linux.go":  "//go:build linux || darwin\n\npackage p\n\n" + body,
				"b_darwin.go": "//go:build darwin\n\npackage p\n\n" + body,
			},
			want:    false,
			comment: "a_linux.go is linux-only by name, so darwin cannot satisfy both",
		},
		{
			name: "two files differing only by filename suffix",
			files: map[string]string{
				"a_linux.go":  "package p\n\n" + body,
				"b_darwin.go": "package p\n\n" + body,
			},
			want:    false,
			comment: "neither carries a build tag and both are constrained by name alone",
		},
		{
			name: "two untagged files",
			files: map[string]string{
				"a.go": "package p\n\n" + body,
				"b.go": "package p\n\n" + body,
			},
			want:    true,
			comment: "always compiled together",
		},
		{
			name: "cgo and non-cgo variants",
			files: map[string]string{
				"a_cgo.go":   "//go:build cgo\n\npackage p\n\n" + body,
				"b_nocgo.go": "//go:build !cgo\n\npackage p\n\n" + body,
			},
			want:    false,
			comment: "cgo is a build tag and the two are exclusive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			for name, src := range tt.files {
				testfixture.Write(t, dir, name, src)
			}

			res := runCheckDir(t, dir)
			got := res.contains("redeclared")
			if got != tt.want {
				t.Errorf("%s: redeclared = %v, want %v (diagnostics: %v)", tt.comment, got, tt.want, res.diags)
			}
		})
	}
}

// TestUnusedImportStaysSilentWhenNameIsUnknowable covers the import whose local name cannot be
// derived from its path.
//
// golens takes the local name from the last path element and does not resolve modules, so
// "github.com/mattn/go-runewidth" yields "go-runewidth" -- not an identifier, and absent from
// the source however the import is used. Reporting it as unused would tell an agent to delete
// a live import, which is the one outcome the check must never produce.
func TestUnusedImportStaysSilentWhenNameIsUnknowable(t *testing.T) {
	tests := []struct {
		name     string
		src      string
		wantDiag bool
		comment  string
	}{
		{
			name:     "used import behind a dashed path",
			src:      "package p\n\nimport \"github.com/mattn/go-runewidth\"\n\nfunc W(r rune) int { return runewidth.RuneWidth(r) }\n",
			wantDiag: false,
			comment:  "the real package name is runewidth, which golens cannot know",
		},
		{
			name:     "genuinely unused dashed import",
			src:      "package p\n\nimport \"github.com/mattn/go-runewidth\"\n\nfunc F() {}\n",
			wantDiag: false,
			comment:  "still unknowable, so still silent rather than wrong",
		},
		{
			name:     "ordinary unused import is still reported",
			src:      "package p\n\nimport \"strings\"\n\nfunc F() {}\n",
			wantDiag: true,
			comment:  "a name that can be derived is still checked",
		},
		{
			name:     "ordinary used import is still silent",
			src:      "package p\n\nimport \"strings\"\n\nfunc F() string { return strings.TrimSpace(\" \") }\n",
			wantDiag: false,
			comment:  "no false positive on a resolvable name",
		},
		{
			name:     "alias makes the name derivable",
			src:      "package p\n\nimport rw \"github.com/mattn/go-runewidth\"\n\nfunc F() {}\n",
			wantDiag: true,
			comment:  "an explicit alias is a real identifier, so the check applies",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := runCheck(t, t.TempDir(), "p.go", tt.src)
			got := res.contains("imported and not used")
			if got != tt.wantDiag {
				t.Errorf("%s: unused-import diagnostic = %v, want %v (diagnostics: %v)",
					tt.comment, got, tt.wantDiag, res.diags)
			}
		})
	}
}
