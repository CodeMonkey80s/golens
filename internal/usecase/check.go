package usecase

import (
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/CodeMonkey80s/golens/internal/domain"
	"github.com/CodeMonkey80s/golens/internal/format"
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
//
// Check reports structural faults only: parse failures, malformed import paths, unused
// imports and duplicate declarations. It does not judge style. A caller who wants naming
// or any other convention enforced runs a linter, which is the tool for a judgement the
// caller must opt into rather than one they inherit by asking a structural question.
func Check(engine domain.Engine, target string, stdout, stderr io.Writer, opts format.Options) error {
	paths, err := checkFiles(target)
	if err != nil {
		return err
	}
	if len(paths) == 0 {
		return errNoGoFiles(target)
	}

	var pkg packageSymbols

	// A parse failure is a finding, not an abort: check reports diagnostics and keeps going, so
	// one unparseable file must not stop the others from being checked. parseAll therefore
	// returns partial results alongside every error, letting this loop analyse what parsed and
	// report what did not, in a single pass.
	docs, parseErrs := parseAll(engine, paths)

	var diags []Diagnostic
	for _, doc := range docs {
		diags = append(diags, analyse(doc, &pkg)...)
	}

	// Parse failures and I/O failures are both reported, but they are not the same kind of
	// news and they do not earn the same exit code.
	//
	// A syntax error is a finding about the code: the caller wants to fix it, and exit 3 is
	// the documented code for "check ran and found something". A file that cannot be opened
	// is a finding about the machine — a dangling symlink, a permission bit — and exit 4 is
	// the documented code for "parse or I/O failure". Reporting both as diagnostics made
	// check exit 3 where outline exited 4 on the very same file, so the two commands
	// disagreed about what had happened.
	//
	// ParseError.Located separates them: the engine attaches a position to a syntax error
	// and to nothing else, and an I/O diagnostic has no position to report.
	var unreadable []ParseError
	for _, pe := range parseErrs {
		// A zero ParseError means the parse succeeded; only real errors are reported.
		if pe.Err == nil {
			continue
		}
		if pe.Located() {
			diags = append(diags, pe.Diagnostics()...)
			continue
		}
		unreadable = append(unreadable, pe)
		// The text still goes out as a diagnostic. It is how the caller learns which file
		// could not be opened and why, and suppressing it to justify an exit code would
		// trade one loss of information for another.
		diags = append(diags, pe.Diagnostics()...)
	}

	diags = append(diags, pkg.duplicates()...)

	// emit writes the findings and reports HasDiagnostics when there were any. Its error is
	// deliberately not returned directly: it would short-circuit the exit-4 condition below,
	// which is exactly the mistake an earlier draft of this made — the branch was written,
	// unreachable, and the test looked like it passed because emit never got that far.
	emitErr := emit(diags, stdout, stderr, opts)

	// Exit 4 dominates exit 3. Both may hold, and the caller must still resolve the I/O
	// failure before the tree is fully read — the same reasoning as `get` skipping files
	// (D25). Reporting 3 would assert that every file was readable, and one was not.
	//
	// The per-file text has already gone out as a diagnostic above, so this error carries
	// only the count. Naming the files again printed the same line twice on stderr, which
	// reads as two failures where there was one.
	if len(unreadable) > 0 {
		return UnreadableFilesError{Count: len(unreadable), Total: len(paths), Quiet: opts.JSON}
	}
	return emitErr
}

// analyse runs the per-document checks. Package-level symbols are handed to pkg, which owns
// cross-file conflicts.
func analyse(doc domain.Document, pkg *packageSymbols) []Diagnostic {
	var diags []Diagnostic
	diags = append(diags, invalidImportPaths(doc)...)
	diags = append(diags, unusedImports(doc)...)

	pkg.add(filepath.ToSlash(filepath.Dir(doc.Path)), doc)
	return diags
}

// emit writes diagnostics in the requested form and reports whether any were found.
//
// The JSON payload always carries an array, empty rather than null when nothing was found:
// a consumer that iterates payload["diagnostics"] or takes its length breaks on null, and
// the shape of a payload must not depend on whether the run had anything to say. An empty
// slice is what makes "[]" the wire form for a clean tree.
func emit(diags []Diagnostic, stdout, stderr io.Writer, opts format.Options) error {
	if opts.JSON {
		payload := checkPayload{
			SchemaVersion: format.SchemaVersion,
			Diagnostics:   make([]jsonDiagnostic, 0, len(diags)),
		}
		for _, d := range diags {
			payload.Diagnostics = append(payload.Diagnostics, jsonDiagnostic{
				Severity: d.Severity,
				File:     d.Path,
				Line:     d.Line,
				Column:   d.Column,
				Message:  d.Message,
			})
		}
		if err := format.WriteJSON(stdout, payload, opts.Compact); err != nil {
			return err
		}
	} else {
		for _, d := range diags {
			fmt.Fprintln(stderr, d.String()) //nolint:errcheck // diagnostic write; a failure here is not actionable
		}
	}

	if len(diags) > 0 {
		return HasDiagnostics{Count: len(diags)}
	}
	return nil
}

// packageSymbols accumulates package-level declarations across files, keyed by namespace.
//
// The key is directory *and* package name, not directory alone. A Go namespace is one package
// in one directory, and a directory may legitimately hold two: an external test package
// (foo_test) alongside the package under test (foo). Those two share a directory but not a
// namespace, so a helper may be declared in both without conflict. Keying on the directory alone
// reported those as redeclarations.
//
// Two directories may also both declare "package p" without conflicting, which is why the
// directory is part of the key too.
type packageSymbols struct {
	byNamespace map[string]map[string][]symbolSite
	// builtIn caches, per namespace and file path, the build configurations that include
	// the file. Two declarations only conflict if their files can be compiled together.
	builtIn map[string]map[string][]bool
}

// symbolSite records one declaration of a name.
type symbolSite struct {
	path string
	line int
}

// add records doc's package-level symbols under its namespace.
func (p *packageSymbols) add(dir string, doc domain.Document) {
	ns := dir + "\x00" + doc.Package

	if p.byNamespace == nil {
		p.byNamespace = make(map[string]map[string][]symbolSite)
		p.builtIn = make(map[string]map[string][]bool)
	}
	if p.byNamespace[ns] == nil {
		p.byNamespace[ns] = make(map[string][]symbolSite, len(doc.Symbols))
		p.builtIn[ns] = make(map[string][]bool)
	}
	if _, seen := p.builtIn[ns][doc.Path]; !seen {
		p.builtIn[ns][doc.Path] = constraintVector(fileConstraint(doc.Path, doc.BuildConstraint, requiresCgo(doc)))
	}

	for _, sym := range doc.Symbols {
		// init and the blank identifier may be declared any number of times in a package,
		// and Go says so explicitly: "Multiple init functions may be declared in a package,
		// even in the same file." Treating either as a name that must be unique produced
		// 166 false positives in runtime alone, where every platform file carries its own
		// init and its own "var _ = ..." placeholder.
		if isRepeatableName(sym.Name) {
			continue
		}
		name := sym.QualifiedName
		p.byNamespace[ns][name] = append(p.byNamespace[ns][name], symbolSite{
			path: doc.Path,
			line: sym.Bounds.StartLine,
		})
	}
}

// isRepeatableName reports whether Go permits a package to declare this name more than once.
//
// Only init and the blank identifier qualify. Neither is a name a caller can resolve, so
// neither can be redeclared in any sense that matters.
func isRepeatableName(name string) bool {
	return name == "init" || name == "_"
}

// requiresCgo reports whether a file imports "C", which makes it a cgo file.
//
// The go tool lists such files under CgoFiles rather than GoFiles and drops them entirely when
// cgo is disabled, so a file importing "C" is not built in a cgo-less configuration even when
// nothing in its name or its build tags says so.
func requiresCgo(doc domain.Document) bool {
	for _, imp := range doc.Imports {
		if strings.Trim(imp, `"`) == "C" {
			return true
		}
	}
	return false
}

// duplicates reports names declared more than once within one directory.
//
// Only the second and later sites are reported, so a pair of conflicting declarations produces
// one diagnostic rather than two, and the message names the earlier site so the reader can find
// it. Results are sorted by path then line, because map iteration order is random and an agent
// may diff or cache the output.
//
// A pair is reported only when both files could be compiled into the same binary. Files
// guarded by mutually exclusive build constraints never are, so the platform variants of a
// declaration are not conflicts and stay silent. The earlier site named in the message is the
// first one that can actually co-build with this site, not merely the previous line of input.
func (p *packageSymbols) duplicates() []Diagnostic {
	var diags []Diagnostic

	for ns, names := range p.byNamespace {
		for name, sites := range names {
			for i := 1; i < len(sites); i++ {
				first, ok := p.firstConflict(ns, sites, i)
				if !ok {
					continue
				}
				diags = append(diags, Diagnostic{
					Severity: "error",
					Path:     sites[i].path,
					Line:     sites[i].line,
					Column:   1,
					Message: fmt.Sprintf("%s redeclared in this package (first declared at %s:%d)",
						name, sites[first].path, sites[first].line),
				})
			}
		}
	}

	sort.Slice(diags, func(i, j int) bool {
		if diags[i].Path != diags[j].Path {
			return diags[i].Path < diags[j].Path
		}
		return diags[i].Line < diags[j].Line
	})
	return diags
}

// firstConflict returns the index of the earliest earlier site that could be compiled into
// the same binary as sites[i], and whether such a site exists.
//
// Scanning from the start rather than testing only sites[i-1] matters once constraints are
// honoured. Sites arrive in file-walk order, so the previous entry may be a mutually
// exclusive platform variant that shares no configuration with this one, while an untagged
// file earlier in the walk is the real conflict.
func (p *packageSymbols) firstConflict(ns string, sites []symbolSite, i int) (int, bool) {
	here := p.builtIn[ns][sites[i].path]
	for j := range i {
		if couldCoBuild(here, p.builtIn[ns][sites[j].path]) {
			return j, true
		}
	}
	return 0, false
}

// duplicateDeclarations was the per-file half of the namespace check and is now subsumed by
// packageSymbols.duplicates, which sees conflicts across files as well as within one. Reporting
// both would double-report every conflict.

// invalidImportPaths reports import paths that cannot be valid.
//
// Module-level resolution needs go list and a module graph, so only syntactic validity
// is checked: a non-empty, quoted path with no spaces or control characters.
func invalidImportPaths(doc domain.Document) []Diagnostic {
	var diags []Diagnostic

	// The declarations are walked rather than doc.Imports because they carry the line each import
	// was written on. Reading the strings alone left every diagnostic pinned to line 1, which
	// sends a reader to the package clause instead of the bad import.
	for _, decl := range importDeclarations(doc) {
		raw := strings.Trim(decl.detail, `"`)
		switch {
		case raw == "":
			diags = append(diags, warning(doc.Path, decl.startLine, "empty import path"))
		case strings.ContainsAny(raw, " \t\n"):
			diags = append(diags, warning(doc.Path, decl.startLine,
				fmt.Sprintf("invalid import path %q: contains whitespace", raw)))
		case containsEscape(decl.detail):
			// A literal tab in a path is written "\t" in the source, so after decoding the
			// path text no longer contains a tab. Checking the quoted literal is what catches
			// a path the module system would reject for an embedded control character.
			diags = append(diags, warning(doc.Path, decl.startLine,
				fmt.Sprintf("invalid import path %s: contains an escape sequence", decl.detail)))
		}
	}
	return diags
}

// containsEscape reports whether a quoted import literal holds a backslash escape.
//
// Go interprets \t, \n, \\ and friends inside an interpreted string literal, so a path containing
// a real control character arrives here already decoded and looks clean. The module system
// rejects such paths, so the check inspects the literal rather than the decoded text.
func containsEscape(literal string) bool {
	return strings.Contains(literal, `\`)
}

// goKeywords is the set of Go keywords. They are excluded from orphan-identifier
// detection because they are never dot-imported names.
var goKeywords = map[string]bool{
	"break":       true,
	"case":        true,
	"chan":        true,
	"const":       true,
	"continue":    true,
	"default":     true,
	"defer":       true,
	"else":        true,
	"fallthrough": true,
	"for":         true,
	"func":        true,
	"go":          true,
	"goto":        true,
	"if":          true,
	"import":      true,
	"interface":   true,
	"map":         true,
	"package":     true,
	"range":       true,
	"return":      true,
	"select":      true,
	"struct":      true,
	"switch":      true,
	"type":        true,
	"var":         true,
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
// unusedImports reports imports whose local name is never referenced in the file.
//
// The method is selector-base-name analysis: an import binds its local name into the
// file scope, and every qualified use of that name appears as the X of a SelectorExpr.
// This is deliberately best effort, as stated in the specification: a local name that is
// shadowed by a local variable, or referenced only inside a string, can yield a false
// positive. A false negative is not possible for a genuinely unused import.
//
// Blank imports are skipped: "_" is a deliberate discard.
// Dot imports (".") contribute all exported names directly, so we cannot know which local
// names they bind without type resolution (out of scope). To avoid false negatives on
// genuinely unused dot imports while avoiding false positives on working code, we report
// a dot import as unused only when the source contains no identifiers that are not
// declared locally or bound by named imports -- i.e., there is no evidence any
// dot-imported name is in use.
func unusedImports(doc domain.Document) []Diagnostic {
	// Import declarations themselves must be excluded from the scan, otherwise every
	// import's own local name appears in the source and nothing is ever reported.
	declarations := importDeclarations(doc)

	used := collectUsedNames(doc.Source, declarations)

	var diags []Diagnostic
	for _, decl := range declarations {
		// Blank imports are skipped: "_" is a deliberate discard.
		if decl.name == "_" {
			continue
		}
		// Dot imports (".") bring all exported names directly. Without type resolution we
		// cannot know which names they bind, so we only report them as unused when there
		// is no evidence any identifier comes from them: if every identifier in the source
		// is declared locally or bound by a named import, the dot import is unused.
		if decl.name == "." {
			if !hasOrphanIdentifiers(doc, declarations) {
				diags = append(diags, Diagnostic{
					Severity: "warning",
					Path:     doc.Path,
					Line:     decl.startLine,
					Column:   1,
					Message:  fmt.Sprintf("imported and not used: %s", decl.detail),
				})
			}
			continue
		}
		// `import "C"` is cgo's marker, not a package import. The name C never appears in
		// the source as a selector base, because cgo rewrites the file before the type
		// checker sees it, so treating it as an ordinary import reports every cgo file in a
		// tree as carrying an unused import. Five such diagnostics on the standard library.
		if decl.name == "C" && strings.Trim(decl.detail, "\"") == "C" {
			continue
		}
		// The local name is derived from the last path element, which is not always the name
		// the package declares. "github.com/mattn/go-runewidth" declares "package runewidth",
		// so the derived name is "go-runewidth" -- not a Go identifier, and therefore never
		// present in the source no matter how the import is used.
		//
		// A major-version suffix is the other exception. "github.com/containerd/containerd/
		// api/services/tasks/v1" declares "package tasks", so the local name is "tasks",
		// not the path's last element "v1". When the derived name doesn't appear in source,
		// try the element before a version-like suffix before accusing the import.
		//
		// Resolving the real name needs module resolution, which is out of scope, so the
		// check cannot always decide. Silence is the only safe answer: accusing an import
		// that is in use would have an agent delete working code.
		if !isIdentifier(decl.name) {
			continue
		}
		if used[decl.name] {
			continue
		}
		// Try alternative names for versioned import paths: if "v1" doesn't appear but
		// "tasks" (the element before "/v1") does, the import is in use under its real name.
		accuse := true
		for _, alt := range alternativeImportNames(decl.detail) {
			if used[alt] {
				accuse = false
				break
			}
		}
		if accuse {
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

// hasOrphanIdentifiers reports whether the source contains identifiers that are not
// declared locally and not bound by named imports. Such identifiers could come from dot
// imports, so we cannot accuse dot imports of being unused.
func hasOrphanIdentifiers(doc domain.Document, declarations []importDecl) bool {
	// Collect all locally declared names.
	declared := make(map[string]bool)
	for _, sym := range doc.Symbols {
		declared[sym.Name] = true
	}
	// Collect named import local names.
	for _, decl := range declarations {
		if decl.name != "_" && decl.name != "." && decl.name != "" && isIdentifier(decl.name) {
			declared[decl.name] = true
		}
	}
	// The package name itself is always in scope.
	declared[doc.Package] = true
	// Go keywords are never dot-imported names, so exclude them from orphan detection.
	for kw := range goKeywords {
		declared[kw] = true
	}

	// Scan the source for every identifier. Any identifier not declared locally or bound
	// by a named import could come from a dot import. Import declaration byte ranges are
	// skipped because the import path itself is not an identifier in the file scope.
	for i := 0; i < len(doc.Source); i++ {
		if !isIdentStart(doc.Source[i]) || !atWordStart(doc.Source, i) {
			continue
		}
		if withinAny(declarations, i) {
			// Skip past the import declaration.
			for _, decl := range declarations {
				if i >= decl.startByte && i < decl.endByte {
					i = decl.endByte - 1
					break
				}
			}
			continue
		}
		end := identEnd(doc.Source, i)
		if !declared[string(doc.Source[i:end])] {
			return true
		}
		i = end - 1
	}
	return false
}

// alternativeImportNames returns possible local names for an import path when the
// primary derived name doesn't appear in source. For a path ending in "/vN" (N >= 1),
// it also returns the element before the suffix, since Go modules often declare a
// package named after that element rather than the version suffix.
//
// For example, "github.com/containerd/containerd/api/services/tasks/v1" declares
// "package tasks", so both "v1" (derived) and "tasks" (real) are candidates.
func alternativeImportNames(path string) []string {
	path = strings.Trim(path, "\"")
	elements := strings.Split(path, "/")
	if len(elements) < 2 {
		return nil
	}
	last := elements[len(elements)-1]
	if !isVersionElement(last) {
		return nil
	}
	return []string{elements[len(elements)-2]}
}

// isVersionElement reports whether s looks like a Go module version suffix "vN"
// (N >= 1). v1 is treated as a version element here because module paths commonly
// end in /v1 even though the package declares a different name.
func isVersionElement(s string) bool {
	if len(s) < 2 || s[0] != 'v' {
		return false
	}
	for _, c := range s[1:] {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
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
