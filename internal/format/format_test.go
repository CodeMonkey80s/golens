package format

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/CodeMonkey80s/golens/internal/domain"
)

func sampleDoc() domain.Document {
	return domain.Document{
		Path:    "user.go",
		Package: "store",
		Imports: []string{`"context"`},
		Nodes: []domain.Node{
			{
				Name: "IMPORTS",
				Kind: domain.KindImport,
				Children: []domain.Node{
					{Name: "context", Kind: domain.KindImport, Detail: `"context"`, Bounds: domain.Bounds{StartLine: 3, EndLine: 3}},
				},
			},
			{
				Name:   "TYPES & STRUCTS",
				Kind:   domain.KindType,
				Bounds: domain.Bounds{StartLine: 5, EndLine: 10},
				Children: []domain.Node{
					{
						Name: "User", Kind: domain.KindType, Detail: "struct",
						Bounds: domain.Bounds{StartLine: 5, EndLine: 8},
						Children: []domain.Node{
							{Name: "Id", Kind: domain.KindField, Detail: "int", Bounds: domain.Bounds{StartLine: 6, EndLine: 6}},
						},
					},
					{Name: "UserStore", Kind: domain.KindType, Detail: "struct", Bounds: domain.Bounds{StartLine: 10, EndLine: 10}},
				},
			},
			{
				Name:   "FUNCTIONS & METHODS",
				Kind:   domain.KindFunc,
				Bounds: domain.Bounds{StartLine: 12, EndLine: 25},
				Children: []domain.Node{
					{
						Name: "NewUserStore", Kind: domain.KindFunc, Detail: "() *UserStore",
						Bounds: domain.Bounds{StartLine: 12, EndLine: 15},
					},
					{
						Name: "GetUser", Kind: domain.KindMethod, Receiver: "s *UserStore",
						Detail: "(ctx context.Context, id int) (*User, error)",
						Bounds: domain.Bounds{StartLine: 17, EndLine: 25},
					},
				},
			},
		},
	}
}

func sampleSym() domain.Symbol {
	return domain.Symbol{
		Name:          "GetUser",
		QualifiedName: "UserStore.GetUser",
		Kind:          domain.KindMethod,
		Receiver:      "s *UserStore",
		Bounds:        domain.Bounds{StartLine: 17, EndLine: 25, StartByte: 430, EndByte: 612},
	}
}

// TestWriteBoundsHuman covers the single-line form pinned in specification 2.3.
func TestWriteBoundsHuman(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteBoundsHuman(&buf, sampleSym(), "user.go", false); err != nil {
		t.Fatalf("WriteBoundsHuman: %v", err)
	}

	want := "UserStore.GetUser  lines 17-25  bytes 430-612\n"
	if buf.String() != want {
		t.Errorf("got:\n%q\nwant:\n%q", buf.String(), want)
	}
}

// TestWriteGetHuman covers the header block and source, matching specification 2.2.
func TestWriteGetHuman(t *testing.T) {
	src := "func (s *UserStore) GetUser() {\n\treturn nil\n}"

	var buf bytes.Buffer
	if err := WriteGetHuman(&buf, sampleSym(), "user.go", src, false); err != nil {
		t.Fatalf("WriteGetHuman: %v", err)
	}

	got := buf.String()
	for _, want := range []string{
		"File: user.go",
		"Symbol: UserStore.GetUser",
		"Lines: 17-25 (9 lines)",
		"func (s *UserStore) GetUser() {",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q:\n%s", want, got)
		}
	}
	if !strings.HasSuffix(got, "}\n") {
		t.Errorf("output should end with closing brace + newline:\n%q", got)
	}
}

// TestSchemaVersionIsFirstField covers the schema-versioning contract: an agent parses
// the first key to detect format drift.
func TestSchemaVersionIsFirstField(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteJson(&buf, Bounds(sampleSym(), "user.go"), false); err != nil {
		t.Fatalf("WriteJson: %v", err)
	}

	out := buf.String()
	firstKey := out[strings.Index(out, `"`)+1:]
	if !strings.HasPrefix(firstKey, "schema_version") {
		t.Errorf("schema_version must be the first field, got:\n%s", out)
	}

	var payload BoundsPayload
	if err := json.Unmarshal(buf.Bytes(), &payload); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if payload.SchemaVersion != SchemaVersion {
		t.Errorf("SchemaVersion = %d, want %d", payload.SchemaVersion, SchemaVersion)
	}
}

// TestCompactJsonIsValidAndSmaller verifies --compact stays parseable while dropping
// indentation. Compact must never change the data, only its presentation.
func TestCompactJsonIsValidAndSmaller(t *testing.T) {
	var pretty, compact bytes.Buffer
	if err := WriteJson(&pretty, Bounds(sampleSym(), "user.go"), false); err != nil {
		t.Fatalf("WriteJson pretty: %v", err)
	}
	if err := WriteJson(&compact, Bounds(sampleSym(), "user.go"), true); err != nil {
		t.Fatalf("WriteJson compact: %v", err)
	}

	if !json.Valid(compact.Bytes()) {
		t.Errorf("compact JSON is not valid:\n%s", compact.String())
	}
	if strings.Contains(compact.String(), "\n  ") {
		t.Errorf("compact JSON should not be indented:\n%s", compact.String())
	}

	var a, b BoundsPayload
	if err := json.Unmarshal(pretty.Bytes(), &a); err != nil {
		t.Fatalf("Unmarshal pretty: %v", err)
	}
	if err := json.Unmarshal(compact.Bytes(), &b); err != nil {
		t.Fatalf("Unmarshal compact: %v", err)
	}
	if a != b {
		t.Errorf("compact lost information:\n pretty: %+v\ncompact: %+v", a, b)
	}
}

// TestGetBoundsOnlyMatchesBounds covers acceptance criterion A6: get --bounds-only and
// bounds must emit identical payloads, so the two shapes cannot drift apart.
func TestGetBoundsOnlyMatchesBounds(t *testing.T) {
	sym := sampleSym()
	boundsPayload := Bounds(sym, "user.go")
	getPayload := Get(sym, "user.go", "ignored", false)

	var bufA, bufB bytes.Buffer
	if err := WriteJson(&bufA, boundsPayload, true); err != nil {
		t.Fatalf("WriteJson: %v", err)
	}
	if err := WriteJson(&bufB, getPayload, true); err != nil {
		t.Fatalf("WriteJson: %v", err)
	}

	if bufA.String() != bufB.String() {
		t.Errorf("payloads differ:\nbounds: %s\n  get: %s", bufA.String(), bufB.String())
	}
}

// TestGetWithSourceIncludesSource confirms Source appears only when requested.
func TestGetWithSourceIncludesSource(t *testing.T) {
	withSrc := Get(sampleSym(), "user.go", "func f() {}", true)
	if withSrc.Source != "func f() {}" {
		t.Errorf("Source = %q, want the extracted text", withSrc.Source)
	}

	withoutSrc := Get(sampleSym(), "user.go", "func f() {}", false)
	if withoutSrc.Source != "" {
		t.Errorf("Source should be empty when excluded, got %q", withoutSrc.Source)
	}
}

// TestOutlineDepthLimitsRecursion covers --depth: depth 1 shows declarations only, and
// the group headers keep their aggregate line range.
func TestOutlineDepthLimitsRecursion(t *testing.T) {
	doc := sampleDoc()

	var shallow, deep bytes.Buffer
	if err := WriteOutlineHuman(&shallow, doc, 1, false); err != nil {
		t.Fatalf("WriteOutlineHuman depth 1: %v", err)
	}
	if err := WriteOutlineHuman(&deep, doc, 2, false); err != nil {
		t.Fatalf("WriteOutlineHuman depth 2: %v", err)
	}

	if strings.Contains(shallow.String(), "Id int") {
		t.Errorf("depth 1 should not include struct fields:\n%s", shallow.String())
	}
	if !strings.Contains(deep.String(), "Id int") {
		t.Errorf("depth 2 should include struct fields:\n%s", deep.String())
	}
}

// TestOutlineHumanFormat pins the header lines, group headings and bullet style from the
// specification's outline example.
func TestOutlineHumanFormat(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteOutlineHuman(&buf, sampleDoc(), 1, false); err != nil {
		t.Fatalf("WriteOutlineHuman: %v", err)
	}

	got := buf.String()
	for _, want := range []string{
		"file: user.go",
		"package: store",
		"IMPORTS",
		"TYPES & STRUCTS (lines 5-10)",
		"FUNCTIONS & METHODS (lines 12-25)",
		"• type User struct (lines 5-8)",
		"• func NewUserStore() *UserStore (lines 12-15)",
		"• func (s *UserStore) GetUser(ctx context.Context, id int) (*User, error) (lines 17-25)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q:\n%s", want, got)
		}
	}
}

// TestOutlineSingleLineRange covers the (line 10) versus (lines 5-8) distinction.
func TestOutlineSingleLineRange(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteOutlineHuman(&buf, sampleDoc(), 1, false); err != nil {
		t.Fatalf("WriteOutlineHuman: %v", err)
	}

	got := buf.String()
	if !strings.Contains(got, "UserStore struct (line 10)") {
		t.Errorf("single-line declaration should render (line 10):\n%s", got)
	}
	if !strings.Contains(got, "User struct (lines 5-8)") {
		t.Errorf("multi-line declaration should render (lines 5-8):\n%s", got)
	}
}

// TestOutlineJsonIsArrayShaped confirms the JSON outline is a per-file object with the
// documented field names and depth limiting applied.
func TestOutlineJsonShape(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteJson(&buf, Outline(sampleDoc(), 1), false); err != nil {
		t.Fatalf("WriteJson: %v", err)
	}

	var out OutlineFile
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if out.SchemaVersion != SchemaVersion {
		t.Errorf("SchemaVersion = %d, want %d", out.SchemaVersion, SchemaVersion)
	}
	if out.File != "user.go" || out.Package != "store" {
		t.Errorf("file/package = %q/%q, want user.go/store", out.File, out.Package)
	}
	if len(out.Imports) != 1 {
		t.Errorf("imports = %v, want one entry", out.Imports)
	}
	if len(out.Nodes) != 3 {
		t.Fatalf("nodes = %d, want 3 groups", len(out.Nodes))
	}

	// Walk to the TYPES group and confirm depth 1 suppressed the field child.
	for _, group := range out.Nodes {
		if group.Name != "TYPES & STRUCTS" {
			continue
		}
		for _, decl := range group.Children {
			if decl.Name == "User" && len(decl.Children) != 0 {
				t.Errorf("depth 1 should suppress User's fields, got %d children", len(decl.Children))
			}
		}
	}
}
