package contextbundle

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"m31labs.dev/canopy/pkg/model"
)

// This file turns already-indexed Canopy structure into projection bytes
// without reparsing source (decision #4). map/signature/skeleton content
// comes entirely from model.Symbol fields already captured at index time;
// only body/full/head_tail read source bytes, and they do so by the indexed
// line span rather than by invoking a parser (spec 9.2).

// projectSymbol renders one candidate's content at mode, reading source only
// when the mode needs raw bytes (body/full/head_tail).
func projectSymbol(root string, c *candidateItem, mode ProjectionMode, lineNumbers bool) ([]byte, int64, error) {
	switch mode {
	case ProjectionFull:
		return readWholeFile(root, c.Path, lineNumbers)
	case ProjectionBody:
		return readLineSpan(root, c.Path, c.StartLine, c.EndLine, lineNumbers)
	case ProjectionHeadTail:
		return readHeadTail(root, c.Path, c.StartLine, c.EndLine, lineNumbers)
	case ProjectionSkeleton:
		return []byte(c.Signature), int64(len(c.Signature)), nil
	case ProjectionSignature:
		return []byte(signatureLine(c)), int64(len(c.Signature)), nil
	case ProjectionMap:
		return []byte(signatureLine(c)), int64(len(c.Signature)), nil
	case ProjectionMetadata:
		return []byte(metadataLine(c)), 0, nil
	case ProjectionReceiptOnly:
		return nil, 0, nil
	default:
		return []byte(signatureLine(c)), int64(len(c.Signature)), nil
	}
}

// projectFile renders a whole-file repository-map candidate: every symbol's
// signature, one per line, sorted by start line. This is Canopy's own
// skeleton — LX never reparses these bytes.
func projectFile(idx *model.Index, path string, mode ProjectionMode) ([]byte, int64) {
	var file *model.FileSummary
	for i := range idx.Files {
		if normalizeRelPath(idx.Files[i].Path) == normalizeRelPath(path) {
			file = &idx.Files[i]
			break
		}
	}
	if file == nil {
		return []byte(path), 0
	}
	if mode == ProjectionMetadata || mode == ProjectionReceiptOnly {
		return []byte(fmt.Sprintf("%s (%d symbols)", file.Path, len(file.Symbols))), 0
	}

	symbols := append([]model.Symbol(nil), file.Symbols...)
	sort.Slice(symbols, func(i, j int) bool { return symbols[i].StartLine < symbols[j].StartLine })

	var b strings.Builder
	for _, sym := range symbols {
		label := sym.Signature
		if label == "" {
			label = sym.Name
		}
		fmt.Fprintf(&b, "%d: %s %s\n", sym.StartLine, sym.Kind, label)
	}
	return []byte(b.String()), file.SizeBytes
}

func signatureLine(c *candidateItem) string {
	if c.Signature != "" {
		return c.Signature
	}
	return fmt.Sprintf("%s %s", c.Kind, c.Name)
}

func metadataLine(c *candidateItem) string {
	return fmt.Sprintf("%s:%d-%d %s %s", c.Path, c.StartLine, c.EndLine, c.Kind, c.Name)
}

func readWholeFile(root, relPath string, lineNumbers bool) ([]byte, int64, error) {
	data, err := os.ReadFile(filepath.Join(root, relPath))
	if err != nil {
		return nil, 0, err
	}
	if !lineNumbers {
		return data, int64(len(data)), nil
	}
	return numberLines(data, 1), int64(len(data)), nil
}

func readLineSpan(root, relPath string, start, end int, lineNumbers bool) ([]byte, int64, error) {
	data, err := os.ReadFile(filepath.Join(root, relPath))
	if err != nil {
		return nil, 0, err
	}
	lines := splitSourceLines(data)
	start = clampSpanLine(start, len(lines))
	end = clampSpanLine(end, len(lines))
	if end < start {
		end = start
	}
	slice := lines[start-1 : end]
	joined := strings.Join(slice, "\n")
	if lineNumbers {
		return numberLines([]byte(joined), start), int64(len(data)), nil
	}
	return []byte(joined), int64(len(data)), nil
}

func readHeadTail(root, relPath string, start, end int, lineNumbers bool) ([]byte, int64, error) {
	data, err := os.ReadFile(filepath.Join(root, relPath))
	if err != nil {
		return nil, 0, err
	}
	lines := splitSourceLines(data)
	total := len(lines)
	headN, tailN := 5, 5
	var b strings.Builder
	headEnd := headN
	if headEnd > total {
		headEnd = total
	}
	for i := 0; i < headEnd; i++ {
		writeLine(&b, lines[i], i+1, lineNumbers)
	}
	tailStart := total - tailN
	if tailStart < headEnd {
		tailStart = headEnd
	}
	if tailStart > headEnd {
		fmt.Fprintf(&b, "... (%d lines omitted)\n", tailStart-headEnd)
	}
	for i := tailStart; i < total; i++ {
		writeLine(&b, lines[i], i+1, lineNumbers)
	}
	return []byte(b.String()), int64(len(data)), nil
}

func writeLine(b *strings.Builder, line string, num int, lineNumbers bool) {
	if lineNumbers {
		fmt.Fprintf(b, "%d| %s\n", num, line)
		return
	}
	b.WriteString(line)
	b.WriteByte('\n')
}

func numberLines(data []byte, startAt int) []byte {
	lines := splitSourceLines(data)
	var b strings.Builder
	for i, line := range lines {
		fmt.Fprintf(&b, "%d| %s\n", startAt+i, line)
	}
	return []byte(b.String())
}

func splitSourceLines(data []byte) []string {
	s := string(data)
	s = strings.TrimSuffix(s, "\n")
	if s == "" {
		return []string{""}
	}
	return strings.Split(s, "\n")
}

func clampSpanLine(n, total int) int {
	if total <= 0 {
		return 1
	}
	if n < 1 {
		return 1
	}
	if n > total {
		return total
	}
	return n
}
