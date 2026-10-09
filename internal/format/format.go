// Package format renders domain values as human-readable text or JSON.
//
// Compact is a formatting flag, never a second code path: every encoder renders from the
// same data and differs only in decoration. See specification 2.
package format

import (
	"encoding/json"
	"io"
	"strconv"
	"strings"

	"github.com/CodeMonkey80s/golens/internal/domain"
)

// SchemaVersion is stamped as the first field of every JSON payload so a consumer can
// detect format drift rather than silently mis-parsing.
const SchemaVersion = 1

// Options controls decoration. Compact removes decoration without removing information.
type Options struct {
	Json    bool
	Compact bool
}

// BoundsPayload is the wire shape for a span. It is shared by get --bounds-only and
// bounds, so the two commands cannot drift apart.
type BoundsPayload struct {
	SchemaVersion int    `json:"schema_version"`
	Symbol        string `json:"symbol"`
	File          string `json:"file"`
	StartLine     int    `json:"start_line"`
	EndLine       int    `json:"end_line"`
	StartByte     int    `json:"start_byte"`
	EndByte       int    `json:"end_byte"`
}

// GetPayload extends BoundsPayload with the extracted source.
//
// It deliberately adds no field beyond Source. Acceptance criterion A6 requires
// get --bounds-only to emit a payload identical to bounds, so any extra field here would
// break that guarantee; the line count is derivable from start_line and end_line and is
// reported in the human form instead.
type GetPayload struct {
	SchemaVersion int    `json:"schema_version"`
	Symbol        string `json:"symbol"`
	File          string `json:"file"`
	StartLine     int    `json:"start_line"`
	EndLine       int    `json:"end_line"`
	StartByte     int    `json:"start_byte"`
	EndByte       int    `json:"end_byte"`
	Source        string `json:"source,omitempty"`
}

// OutlineNode is one node in the JSON outline. Detail is omitted when empty to keep
// payloads small.
type OutlineNode struct {
	Name      string        `json:"name"`
	Kind      domain.Kind   `json:"kind"`
	Detail    string        `json:"detail,omitempty"`
	StartLine int           `json:"start_line"`
	EndLine   int           `json:"end_line"`
	Children  []OutlineNode `json:"children,omitempty"`
}

// OutlineFile is the per-file JSON object. The outline payload is always an array of
// these, even for a single file, so the schema does not vary with input type.
type OutlineFile struct {
	SchemaVersion int           `json:"schema_version"`
	File          string        `json:"file"`
	Package       string        `json:"package"`
	Imports       []string      `json:"imports,omitempty"`
	Nodes         []OutlineNode `json:"nodes,omitempty"`
}

// WriteJson encodes v to w, indented unless compact is set.
func WriteJson(w io.Writer, v any, compact bool) error {
	enc := json.NewEncoder(w)
	if !compact {
		enc.SetIndent("", "  ")
	}
	return enc.Encode(v)
}

// Bounds builds the shared bounds payload.
func Bounds(sym domain.Symbol, file string) BoundsPayload {
	return BoundsPayload{
		SchemaVersion: SchemaVersion,
		Symbol:        sym.QualifiedName,
		File:          file,
		StartLine:     sym.Bounds.StartLine,
		EndLine:       sym.Bounds.EndLine,
		StartByte:     sym.Bounds.StartByte,
		EndByte:       sym.Bounds.EndByte,
	}
}

// Get builds the get payload. source is omitted when includeSource is false, which is how
// --bounds-only produces a payload identical to bounds.
func Get(sym domain.Symbol, file, source string, includeSource bool) GetPayload {
	b := Bounds(sym, file)
	p := GetPayload{
		SchemaVersion: b.SchemaVersion,
		Symbol:        b.Symbol,
		File:          b.File,
		StartLine:     b.StartLine,
		EndLine:       b.EndLine,
		StartByte:     b.StartByte,
		EndByte:       b.EndByte,
	}
	if includeSource {
		p.Source = source
	}
	return p
}

// WriteBoundsHuman prints the single-line form required by specification 2.3.
func WriteBoundsHuman(w io.Writer, sym domain.Symbol, file string, compact bool) error {
	if compact {
		_, err := io.WriteString(w, strconv.Quote(sym.QualifiedName)+" "+lineRange(sym.Bounds)+" "+byteRange(sym.Bounds)+"\n")
		return err
	}

	_, err := io.WriteString(w,
		sym.QualifiedName+"  "+
			lineRange(sym.Bounds)+"  "+
			byteRange(sym.Bounds)+"\n")
	return err
}

// WriteGetHuman prints the header block followed by the source, matching specification 2.2.
func WriteGetHuman(w io.Writer, sym domain.Symbol, file, source string, compact bool) error {
	if compact {
		_, err := io.WriteString(w, strconv.Quote(sym.QualifiedName)+" "+lineRange(sym.Bounds)+"\n"+source+"\n")
		return err
	}

	var sb strings.Builder
	sb.WriteString("File: " + file + "\n")
	sb.WriteString("Symbol: " + sym.QualifiedName + "\n")
	// The specification's sample reads "Lines: 17-25 (9 lines)" -- a bare range, since the
	// label already says "Lines". lineCountPhrase also handles the singular, so a one-line
	// declaration does not read "1 lines".
	sb.WriteString("Lines: " + spanRange(sym.Bounds) + " (" + lineCountPhrase(sym.Bounds.LineCount()) + ")\n")
	sb.WriteString("\n")
	sb.WriteString(source)
	if !strings.HasSuffix(source, "\n") {
		sb.WriteString("\n")
	}

	_, err := io.WriteString(w, sb.String())
	return err
}

// compactEntry renders one node for the compact outline, where the line range is a separate
// leading field and therefore not repeated inside the text.
func compactEntry(n domain.Node) string {
	if n.Kind == domain.KindField {
		if n.Detail != "" {
			return n.Name + " " + n.Detail
		}
		return n.Name
	}
	return entry(n)
}

// unquotePath strips the surrounding quotes from a Go import path literal.
func unquotePath(path string) string {
	if len(path) >= 2 && path[0] == '"' && path[len(path)-1] == '"' {
		return path[1 : len(path)-1]
	}
	return path
}

// spanRange renders just the numbers, "17-25" or "12" for a single line.
//
// Used after a label that already names the span, where repeating "lines" would stutter.
func spanRange(b domain.Bounds) string {
	if b.EndLine == b.StartLine {
		return strconv.Itoa(b.StartLine)
	}
	return strconv.Itoa(b.StartLine) + "-" + strconv.Itoa(b.EndLine)
}

// lineCountPhrase renders a count with a correctly pluralised noun.
func lineCountPhrase(n int) string {
	if n == 1 {
		return "1 line"
	}
	return strconv.Itoa(n) + " lines"
}

func lineRange(b domain.Bounds) string {
	if b.EndLine == b.StartLine {
		return "line " + strconv.Itoa(b.StartLine)
	}
	return "lines " + strconv.Itoa(b.StartLine) + "-" + strconv.Itoa(b.EndLine)
}

func byteRange(b domain.Bounds) string {
	return "bytes " + strconv.Itoa(b.StartByte) + "-" + strconv.Itoa(b.EndByte)
}

// convertNodes maps domain nodes onto their JSON shape.
// convertNodes maps domain nodes onto their JSON shape, recursing while depth allows.
//
// level and depth work as in the human form, so the two outputs select the same declarations.
func convertNodes(nodes []domain.Node, level, depth int) []OutlineNode {
	if len(nodes) == 0 || !visible(depth, level) {
		return nil
	}

	out := make([]OutlineNode, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, OutlineNode{
			Name:      n.Name,
			Kind:      n.Kind,
			Detail:    n.Detail,
			StartLine: n.Bounds.StartLine,
			EndLine:   n.Bounds.EndLine,
			Children:  convertNodes(n.Children, level+1, depth),
		})
	}
	return out
}

// Outline converts a document for JSON output. depth limits recursion; a negative depth means
// unlimited.
func Outline(doc domain.Document, depth int) OutlineFile {
	return OutlineFile{
		SchemaVersion: SchemaVersion,
		File:          doc.Path,
		Package:       doc.Package,
		Imports:       doc.Imports,
		Nodes:         convertNodes(doc.Nodes, 0, depth),
	}
}

// bullet is the marker used by the human outline. It matches the specification's examples.
const bullet = "  • "

// UnlimitedDepth disables the recursion limit on the outline.
const UnlimitedDepth = -1

// visible reports whether a node at the given level should be printed.
//
// Level 0 is the declarations under a group heading, level 1 their fields and parameters. A
// negative depth means unlimited, which is why this is a helper rather than an inline
// comparison: `depth > level` is false for every level when depth is negative, which silently
// makes "unlimited" behave like depth 1.
func visible(depth, level int) bool {
	if depth < 0 {
		return true
	}
	return level < depth
}

// WriteOutlineHuman prints the human outline for one document.
//
// depth limits recursion the same way the JSON form does, so the two stay consistent: depth 1
// shows group headings and declarations, depth 2 adds fields and parameters, and a negative
// depth shows everything.
//
// With compact set the output drops decoration but keeps every fact: no group headings, no
// blank lines, no bullet glyphs, and one record per line as
//
//	<line-range>	<declaration>
//
// That satisfies the lossless-subset rule in specification 2: an agent parsing compact output
// recovers the same lines, spans and declarations as from the full form.
func WriteOutlineHuman(w io.Writer, doc domain.Document, depth int, compact bool) error {
	if compact {
		return writeOutlineCompact(w, doc, depth)
	}

	var sb strings.Builder

	sb.WriteString("file: " + doc.Path + "\n")
	sb.WriteString("package: " + doc.Package + "\n")

	if err := writeSection(&sb, doc.Nodes, depth); err != nil {
		return err
	}

	_, err := io.WriteString(w, sb.String())
	return err
}

// writeOutlineCompact emits one declaration per line with no headings or decoration.
//
// Group nodes are flattened rather than printed: a heading is decoration, and its children
// carry every fact the heading would otherwise convey. level and depth work exactly as in the
// full form, so the two select the same declarations.
func writeOutlineCompact(w io.Writer, doc domain.Document, depth int) error {
	var sb strings.Builder

	sb.WriteString(doc.Path + "\t" + doc.Package + "\n")
	compactNodes(&sb, doc.Nodes, depth)

	_, err := io.WriteString(w, sb.String())
	return err
}

// compactNodes emits one line per declaration, recursing while depth allows.
//
// Group nodes are transparent: they contribute no line of their own and do not consume a
// level, so a group heading cannot silently swallow a depth level. That is why this does not
// simply call visible with its own index -- a file with only group nodes would report no
// declarations at any depth.
func compactNodes(sb *strings.Builder, nodes []domain.Node, depth int) {
	for _, n := range nodes {
		if n.IsGroup {
			compactNodes(sb, n.Children, depth)
			continue
		}

		sb.WriteString(n.Bounds.LineRange() + "\t" + compactEntry(n) + "\n")

		if len(n.Children) > 0 && visible(depth, 1) {
			compactNodes(sb, n.Children, depth-1)
		}
	}
}

// writeSection prints each group's heading followed by its members.
//
// Groups are always shown: a heading is what makes the full form readable, and hiding one at
// depth 1 would leave declarations with no context. depth governs the members' children.
func writeSection(sb *strings.Builder, nodes []domain.Node, depth int) error {
	for _, node := range nodes {
		if len(node.Children) == 0 {
			continue
		}

		sb.WriteString("\n")
		if header := sectionHeader(node); header != "" {
			sb.WriteString(header + "\n")
		}

		if err := writeNodes(sb, node.Children, 0, depth); err != nil {
			return err
		}
	}
	return nil
}

// sectionHeader renders the group heading with its aggregate line range, skipping the
// range for the import group whose members are individually ranged already.
func sectionHeader(node domain.Node) string {
	if node.Kind == domain.KindImport {
		return node.Name
	}
	if node.Bounds.LineCount() > 0 {
		return node.Name + " (" + node.Bounds.LineRange() + ")"
	}
	return node.Name
}

// writeNodes prints each node as a bullet entry, recursing into children while depth allows.
//
// level is this node's own nesting level, so depth 1 prints declarations (level 0) and stops
// before their fields (level 1).
func writeNodes(sb *strings.Builder, nodes []domain.Node, level, depth int) error {
	for _, n := range nodes {
		sb.WriteString(bullet)
		sb.WriteString(entry(n))
		// An import's line range is omitted, matching the specification's sample: an import is
		// always a single line, so the range carries no information, while a declaration's
		// range is what an agent seeks and overwrites with.
		if n.Kind != domain.KindImport {
			sb.WriteString(" (" + n.Bounds.LineRange() + ")")
		}
		sb.WriteString("\n")

		if len(n.Children) > 0 && visible(depth, level+1) {
			if err := writeNodes(sb, n.Children, level+1, depth); err != nil {
				return err
			}
		}
	}
	return nil
}

// entry renders one node as "kind name detail", omitting empty parts.
func entry(n domain.Node) string {
	switch n.Kind {
	case domain.KindImport:
		// The local name, not the quoted path: the specification's outline example shows
		// "context", and the local name is what a reader would search the file for. A renamed
		// import still shows its alias, which is the whole point of the binding.
		if n.Name != "" {
			return n.Name
		}
		return unquotePath(n.Detail)
	case domain.KindField:
		if n.Detail != "" {
			return n.Name + " " + n.Detail
		}
		return n.Name
	default:
		// Types and funcs render as declaration heads: "func (s *T) Name(args)",
		// "type Name struct". Detail for these kinds is already parameter/result text
		// with no separating space, so Declaration must not insert one.
		if n.Kind == domain.KindType || n.Kind == domain.KindFunc ||
			n.Kind == domain.KindMethod || n.Kind == domain.KindVar || n.Kind == domain.KindConst {
			return n.Declaration()
		}
		return string(n.Kind) + " " + n.Name
	}
}
