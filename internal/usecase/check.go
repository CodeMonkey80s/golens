package usecase

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/CodeMonkey80s/golens/internal/domain"
	"github.com/CodeMonkey80s/golens/internal/format"
	"github.com/CodeMonkey80s/golens/internal/naming"
)

// Diagnostic is one finding produced by check.
type Diagnostic struct {
	// Severity is "error" for parse failures and duplicate declarations, "warning" for
	// the best-effort unused-import and import-path checks.
	Severity string
	Path     string
	Line     int
	Column   int
	Message  string
}

// String renders the diagnostic in the file:line:col: message form from specification 2.4.
func (d Diagnostic) String() string {
	return fmt.Sprintf("%s:%d:%d: %s", d.Path, d.Line, d.Column, d.Message)
}

// jsonDiagnostic is the wire shape for --json.
type jsonDiagnostic struct {
	Severity string `json:"severity"`
	File     string `json:"file"`
	Line     int    `json:"line"`
	Column   int    `json:"column"`
	Message  string `json:"message"`
}

type checkPayload struct {
	SchemaVersion int              `json:"schema_version"`
	Diagnostics   []jsonDiagnostic `json:"diagnostics"`
}

// HasDiagnostics reports whether any diagnostics were found, so the CLI can exit 3.
type HasDiagnostics struct {
	Count int
}

func (HasDiagnostics) Error() string { return "diagnostics found" }

// IsDiagnostics marks the error for exit 3.
func (HasDiagnostics) IsDiagnostics() bool { return true }

// Check validates the target and writes diagnostics to stderr, or the JSON payload to
// stdout when --json is set.
//
// Diagnostics always go to stderr in the human form so that a --json consumer reading
// stdout receives only the payload. Findings are ordered by path then line so repeated
// runs produce identical output.
func Check(engine domain.Engine, target string, stdout, stderr io.Writer, opts format.Options, namingOnly bool) error {
	paths, err := Files(target)
	if err != nil {
		return err
	}
	if len(paths) == 0 {
		return errNoGoFiles(target)
	}

	var diags []Diagnostic
	for _, p := range paths {
		found, err := checkFile(engine, p, namingOnly)
		if err != nil {
			return err
		}
		diags = append(diags, found...)
	}

	if opts.Json {
		payload := checkPayload{SchemaVersion: format.SchemaVersion}
		for _, d := range diags {
			payload.Diagnostics = append(payload.Diagnostics, jsonDiagnostic{
				Severity: d.Severity,
				File:     d.Path,
				Line:     d.Line,
				Column:   d.Column,
				Message:  d.Message,
			})
		}
		if err := format.WriteJson(stdout, payload, opts.Compact); err != nil {
			return err
		}
	} else {
		for _, d := range diags {
			fmt.Fprintln(stderr, d.String())
		}
	}

	if len(diags) > 0 {
		return HasDiagnostics{Count: len(diags)}
	}
	return nil
}

// checkFile inspects one file for the checks scoped in decision D8.
//
// namingOnly replaces the syntax and analysis checks with the naming-convention check, which
// is what golens check --naming does. The two are separate because a caller validating syntax
// should not receive naming opinions, and a caller enforcing a convention should not be
// blocked by an unrelated parse warning.
func checkFile(engine domain.Engine, path string, namingOnly bool) ([]Diagnostic, error) {
	doc, err := engine.Parse(path)
	if err != nil {
		// A parse failure is itself the finding: check must report it as a diagnostic and
		// continue with other files rather than aborting the run.
		var pe interface{ Diagnostics() []string }
		if errors.As(err, &pe) {
			out := make([]Diagnostic, 0, len(pe.Diagnostics()))
			for _, msg := range pe.Diagnostics() {
				out = append(out, Diagnostic{
					Severity: "error",
					Path:     path,
					Line:     1,
					Column:   1,
					Message:  msg,
				})
			}
			return out, nil
		}
		return nil, err
	}

	if namingOnly {
		return namingViolations(doc), nil
	}

	var diags []Diagnostic
	diags = append(diags, duplicateDeclarations(doc)...)
	diags = append(diags, invalidImportPaths(doc)...)
	diags = append(diags, unusedImports(doc)...)
	return diags, nil
}

// namingViolations reports identifiers in doc that break the camelCaseId convention.
//
// The check runs over the outline's node names rather than re-parsing: every declared name
// the engine surfaced is already there, so this needs no second parse.
func namingViolations(doc domain.Document) []Diagnostic {
	var ids []string
	collect := func(n domain.Node) {
		if n.Kind == domain.KindImport || n.Kind == domain.KindPackage {
			return
		}
		if n.Name != "" && n.Name != "_" {
			ids = append(ids, n.Name)
		}
		if n.Receiver != "" {
			ids = append(ids, strings.TrimPrefix(n.Receiver, "*"))
		}
	}

	var walk func(nodes []domain.Node)
	walk = func(nodes []domain.Node) {
		for _, n := range nodes {
			collect(n)
			walk(n.Children)
		}
	}
	walk(doc.Nodes)

	var diags []Diagnostic
	for _, v := range naming.Check(ids) {
		diags = append(diags, Diagnostic{
			Severity: "warning",
			Path:     doc.Path,
			Line:     1,
			Column:   1,
			Message:  v.Identifier + ": " + v.Reason,
		})
	}
	return diags
}

// duplicateDeclarations reports names declared more than once at package level.
//
// This is the AST-achievable part of "package namespace conflicts": a redeclared
// identifier is a genuine namespace error. Type-level conflicts would need go/types and
// are out of scope per decision D8.
func duplicateDeclarations(doc domain.Document) []Diagnostic {
	seen := make(map[string]domain.Bounds, len(doc.Symbols))
	var diags []Diagnostic

	for _, sym := range doc.Symbols {
		// Grouped var/const specs legitimately span several names in one declaration,
		// so a repeat is only a conflict when the name itself repeats.
		if prev, ok := seen[sym.QualifiedName]; ok && prev.StartByte != sym.Bounds.StartByte {
			diags = append(diags, Diagnostic{
				Severity: "error",
				Path:     doc.Path,
				Line:     sym.Bounds.StartLine,
				Column:   1,
				Message:  fmt.Sprintf("%s redeclared in this block (first declared at line %d)", sym.QualifiedName, prev.StartLine),
			})
			continue
		}
		seen[sym.QualifiedName] = sym.Bounds
	}
	return diags
}

// invalidImportPaths reports import paths that cannot be valid.
//
// Module-level resolution needs go list and a module graph, so only syntactic validity
// is checked: a non-empty, quoted path with no spaces or control characters.
func invalidImportPaths(doc domain.Document) []Diagnostic {
	var diags []Diagnostic

	for _, imp := range doc.Imports {
		raw := strings.Trim(imp, `"`)
		switch {
		case raw == "":
			diags = append(diags, warning(doc.Path, 1, "empty import path"))
		case strings.ContainsAny(raw, " \t\n"):
			diags = append(diags, warning(doc.Path, 1, fmt.Sprintf("invalid import path %q: contains whitespace", raw)))
		}
	}
	return diags
}

// unusedImports reports imports whose local name is never referenced in the file.
//
// The method is selector-base-name analysis: an import binds its local name into the
// file scope, and every qualified use of that name appears as the X of a SelectorExpr.
// This is deliberately best effort, as stated in the specification: a local name that is
// shadowed by a local variable, or referenced only inside a string, can yield a false
// positive. A false negative is not possible for a genuinely unused import.
//
// Blank and dot imports are skipped: "_" is a deliberate discard and "." contributes no
// local name to test.
func unusedImports(doc domain.Document) []Diagnostic {
	// Import declarations themselves must be excluded from the scan, otherwise every
	// import's own local name appears in the source and nothing is ever reported.
	declarations := importDeclarations(doc)

	used := collectUsedNames(doc.Source, declarations)

	var diags []Diagnostic
	for _, decl := range declarations {
		// Blank and dot imports bind no local name to test: "_" is a deliberate discard and
		// "." contributes the imported names directly.
		if decl.name == "_" || decl.name == "." || decl.name == "" {
			continue
		}
		if !used[decl.name] {
			diags = append(diags, Diagnostic{
				Severity: "warning",
				Path:     doc.Path,
				Line:     decl.startLine,
				Column:   1,
				Message:  fmt.Sprintf("imported and not used: %s", decl.detail),
			})
		}
	}
	return diags
}

// importDecl is one import spec: the local name it binds plus where it was written.
type importDecl struct {
	name      string
	detail    string
	startByte int
	endByte   int
	startLine int
}

// importDeclarations collects the leaf import specs from the outline.
//
// The outline nests imports under an "IMPORTS" group node that shares the Kind but has
// children, so only leaves are real declarations.
func importDeclarations(doc domain.Document) []importDecl {
	var out []importDecl
	for _, node := range flattenNodes(doc.Nodes) {
		if node.Kind != domain.KindImport || len(node.Children) > 0 {
			continue
		}
		out = append(out, importDecl{
			name:      node.Name,
			detail:    node.Detail,
			startByte: node.Bounds.StartByte,
			endByte:   node.Bounds.EndByte,
			startLine: node.Bounds.StartLine,
		})
	}
	return out
}

// collectUsedNames gathers every identifier that could reference a package-level import,
// ignoring the byte ranges occupied by the import declarations themselves.
//
// The scan is lexical over the source rather than AST-based, because the engine exposes
// outline nodes rather than the underlying ast.File. Two passes run over the code outside
// the import block:
//
//  1. Selector bases: "json.Marshal" yields "json". Precise, and covers the normal case.
//  2. Any other lowercase word, which catches a dot-import's names and a local alias used
//     without qualification.
//
// The bias is deliberate. A false positive tells an agent to delete an import that is in
// use, which breaks the build; a false negative merely misses an unused import, which the
// Go compiler reports anyway. Over-approximating is the safe direction.
func collectUsedNames(src []byte, declarations []importDecl) map[string]bool {
	used := make(map[string]bool)

	for i := 0; i < len(src); i++ {
		if !isIdentStart(src[i]) || !atWordStart(src, i) {
			continue
		}
		end := identEnd(src, i)
		word := string(src[i:end])

		// Skip the import declaration's own text, or every import would look used.
		if !withinAny(declarations, i) {
			if isImportLike(word) {
				used[word] = true
			}

			// A selector base is recorded regardless of case, since a renamed import may
			// bind an upper-case name and a qualified use is unambiguous evidence.
			j := end
			for j < len(src) && (src[j] == ' ' || src[j] == '\t') {
				j++
			}
			if j < len(src) && src[j] == '.' && !withinAny(declarations, j) {
				used[word] = true
			}
		}

		i = end - 1
	}

	return used
}

// withinAny reports whether offset falls inside one of the declaration spans.
func withinAny(declarations []importDecl, offset int) bool {
	for _, d := range declarations {
		if offset >= d.startByte && offset < d.endByte {
			return true
		}
	}
	return false
}

// isImportLike reports whether a word could plausibly be an import's local name. Only
// lowercase words qualify, because a renamed import binding is lowercase by convention
// while type and variable names are not.
func isImportLike(word string) bool {
	if word == "" {
		return false
	}
	r, _ := utf8.DecodeRuneInString(word)
	return unicode.IsLower(r)
}

func flattenNodes(nodes []domain.Node) []domain.Node {
	var out []domain.Node
	for _, n := range nodes {
		out = append(out, n)
		out = append(out, flattenNodes(n.Children)...)
	}
	return out
}

func warning(path string, line int, msg string) Diagnostic {
	return Diagnostic{Severity: "warning", Path: path, Line: line, Column: 1, Message: msg}
}
