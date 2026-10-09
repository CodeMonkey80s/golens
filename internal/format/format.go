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
	sb.WriteString("Lines: " + lineRange(sym.Bounds) + " (" + strconv.Itoa(sym.Bounds.LineCount()) + " lines)\n")
	sb.WriteString("\n")
	sb.WriteString(source)
	if !strings.HasSuffix(source, "\n") {
		sb.WriteString("\n")
	}

	_, err := io.WriteString(w, sb.String())
	return err
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
func convertNodes(nodes []domain.Node, depth int) []OutlineNode {
	if depth == 0 || len(nodes) == 0 {
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
			Children:  convertNodes(n.Children, depth-1),
		})
	}
	return out
}

// Outline converts a document for JSON output. depth limits recursion; a negative depth
// means unlimited.
func Outline(doc domain.Document, depth int) OutlineFile {
	return OutlineFile{
		SchemaVersion: SchemaVersion,
		File:          doc.Path,
		Package:       doc.Package,
		Imports:       doc.Imports,
		Nodes:         convertNodes(doc.Nodes, depth),
	}
}

// bullet is the marker used by the human outline. It matches the specification's examples.
const bullet = "  • "

// WriteOutlineHuman prints the human outline for one document.
//
// depth limits recursion the same way the JSON form does, so the two stay consistent:
// depth 1 shows group headers and declarations, depth 2 adds fields and parameters.
func WriteOutlineHuman(w io.Writer, doc domain.Document, depth int) error {
	var sb strings.Builder

	sb.WriteString("file: " + doc.Path + "\n")
	sb.WriteString("package: " + doc.Package + "\n")

	if err := writeSection(&sb, doc.Nodes, depth); err != nil {
		return err
	}

	_, err := io.WriteString(w, sb.String())
	return err
}

func writeSection(sb *strings.Builder, nodes []domain.Node, depth int) error {
	for _, node := range nodes {
		if len(node.Children) == 0 {
			continue
		}

		sb.WriteString("\n")
		if header := sectionHeader(node); header != "" {
			sb.WriteString(header + "\n")
		}

		if err := writeNodes(sb, node.Children, depth-1); err != nil {
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

func writeNodes(sb *strings.Builder, nodes []domain.Node, depth int) error {
	for _, n := range nodes {
		sb.WriteString(bullet)
		sb.WriteString(entry(n))
		sb.WriteString(" (" + n.Bounds.LineRange() + ")\n")

		if depth > 0 {
			if err := writeNodes(sb, n.Children, depth-1); err != nil {
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
		if n.Detail != "" {
			return n.Detail
		}
		return n.Name
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
