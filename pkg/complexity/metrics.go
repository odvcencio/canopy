package complexity

import (
	"math"
	"strings"

	"github.com/odvcencio/gotreesitter"
)

// LOCMetrics holds line-of-code counts for a code region.
//
//   - SLOC:  total physical lines in the region (code, comments, and blanks).
//   - PLOC:  physical lines containing at least one code token.
//   - LLOC:  logical lines — a count of statement-like nodes.
//   - CLOC:  physical lines containing at least one comment.
//   - Blank: lines with neither code nor comment (SLOC - PLOC - comment-only lines).
//
// A line carrying both code and a trailing comment counts in PLOC and CLOC,
// so SLOC = PLOC + CLOC + Blank does not hold in general.
type LOCMetrics struct {
	SLOC  int `json:"sloc"`
	PLOC  int `json:"ploc"`
	LLOC  int `json:"lloc"`
	CLOC  int `json:"cloc"`
	Blank int `json:"blank"`
}

// HalsteadMetrics holds Halstead complexity measures derived from operator and
// operand counts. n1/n2 are the distinct operator/operand counts, N1/N2 the
// total occurrences.
type HalsteadMetrics struct {
	DistinctOperators int     `json:"distinct_operators"` // n1
	DistinctOperands  int     `json:"distinct_operands"`  // n2
	TotalOperators    int     `json:"total_operators"`    // N1
	TotalOperands     int     `json:"total_operands"`     // N2
	Vocabulary        int     `json:"vocabulary"`         // n1 + n2
	Length            int     `json:"length"`             // N1 + N2
	Volume            float64 `json:"volume"`
	Difficulty        float64 `json:"difficulty"`
	Effort            float64 `json:"effort"`
	Time              float64 `json:"time"`
	Bugs              float64 `json:"bugs"`
}

// MaintainabilityMetrics holds the three common Maintainability Index variants.
// Higher is more maintainable.
type MaintainabilityMetrics struct {
	Original     float64 `json:"original"`
	SEI          float64 `json:"sei"`
	VisualStudio float64 `json:"visual_studio"`
}

// halsteadFromCounts computes Halstead measures from the distinct/total
// operator and operand counts. Volume is 0 when the vocabulary is <= 1, and
// difficulty (hence effort/time/bugs) is 0 when there are no operands, so
// degenerate regions never yield NaN or Inf.
func halsteadFromCounts(distinctOperators, distinctOperands, totalOperators, totalOperands int) HalsteadMetrics {
	h := HalsteadMetrics{
		DistinctOperators: distinctOperators,
		DistinctOperands:  distinctOperands,
		TotalOperators:    totalOperators,
		TotalOperands:     totalOperands,
		Vocabulary:        distinctOperators + distinctOperands,
		Length:            totalOperators + totalOperands,
	}

	vocab := float64(h.Vocabulary)
	if vocab > 1 {
		h.Volume = float64(h.Length) * math.Log2(vocab)
	}
	if distinctOperands > 0 {
		h.Difficulty = float64(distinctOperators) / 2.0 * float64(totalOperands) / float64(distinctOperands)
	}
	h.Effort = h.Difficulty * h.Volume
	h.Time = h.Effort / 18.0 // Stroud number
	if h.Effort > 0 {
		h.Bugs = math.Pow(h.Effort, 2.0/3.0) / 3000.0
	}
	return h
}

// maintainability computes the three Maintainability Index variants from
// Halstead volume, cyclomatic complexity, source lines of code, and the comment
// percentage (in [0, 100]). Volume and SLOC are floored at 1 before the
// logarithms so empty or trivial regions produce finite values instead of the
// -Inf that ln(0)/log2(0) would yield.
func maintainability(volume float64, cyclomatic, sloc int, commentPct float64) MaintainabilityMetrics {
	v := math.Max(volume, 1.0)
	s := math.Max(float64(sloc), 1.0)
	cc := float64(cyclomatic)

	original := 171.0 - 5.2*math.Log(v) - 0.23*cc - 16.2*math.Log(s)
	sei := 171.0 - 5.2*math.Log2(v) - 0.23*cc - 16.2*math.Log2(s) +
		50.0*math.Sin(math.Sqrt(2.4*commentPct))
	vs := original * 100.0 / 171.0
	if vs < 0 {
		vs = 0
	}

	return MaintainabilityMetrics{Original: original, SEI: sei, VisualStudio: vs}
}

// locFromLineSets assembles the LOC metrics from the total physical line count,
// the set of lines that contain code, the set of lines that contain a comment,
// and a precomputed logical-line count. Line numbering only needs to be
// internally consistent across the sets.
func locFromLineSets(totalLines int, codeLines, commentLines map[int]bool, logicalLines int) LOCMetrics {
	commentOnly := 0
	for line := range commentLines {
		if !codeLines[line] {
			commentOnly++
		}
	}

	blank := totalLines - len(codeLines) - commentOnly
	if blank < 0 {
		blank = 0
	}

	return LOCMetrics{
		SLOC:  totalLines,
		PLOC:  len(codeLines),
		LLOC:  logicalLines,
		CLOC:  len(commentLines),
		Blank: blank,
	}
}

// commentPercent returns the share of a region's lines that carry a comment, as
// a percentage in [0, 100] — the comment-density input to the Maintainability
// Index (SEI variant).
func commentPercent(loc LOCMetrics) float64 {
	if loc.SLOC <= 0 {
		return 0
	}
	pct := float64(loc.CLOC) / float64(loc.SLOC) * 100.0
	if pct > 100 {
		pct = 100
	}
	return pct
}

// isCommentNode reports whether a node type denotes a comment in any grammar
// (line_comment, block_comment, comment, doc_comment, ...).
func isCommentNode(nodeType string) bool {
	return strings.Contains(nodeType, "comment")
}

// isLogicalLineNode reports whether a node type denotes a statement, used as a
// language-agnostic approximation of logical lines of code. It is a heuristic:
// tree-sitter grammars name statements with a "_statement" or "_declaration"
// suffix (e.g. return_statement, short_var_declaration, expression_statement).
func isLogicalLineNode(nodeType string) bool {
	return strings.HasSuffix(nodeType, "_statement") ||
		strings.HasSuffix(nodeType, "_declaration")
}

// analyzeSourceMetrics walks a parsed region once and returns its LOC and
// Halstead metrics. Operator/operand classification is language-agnostic: a
// leaf token that is a named node (identifier, literal) is an operand; a leaf
// that is anonymous (punctuation or a keyword) is an operator. Comment subtrees
// are skipped for Halstead but recorded for CLOC.
func analyzeSourceMetrics(root *gotreesitter.Node, lang *gotreesitter.Language, source []byte) (LOCMetrics, HalsteadMetrics) {
	operators := map[string]int{}
	operands := map[string]int{}
	codeLines := map[int]bool{}
	commentLines := map[int]bool{}
	logical := 0

	var walk func(node *gotreesitter.Node)
	walk = func(node *gotreesitter.Node) {
		if node == nil {
			return
		}
		nodeType := node.Type(lang)

		if isCommentNode(nodeType) {
			for row := int(node.StartPoint().Row); row <= int(node.EndPoint().Row); row++ {
				commentLines[row] = true
			}
			return // do not descend: comment tokens are neither operators nor operands
		}

		if isLogicalLineNode(nodeType) {
			logical++
		}

		if node.ChildCount() == 0 {
			if strings.TrimSpace(node.Text(source)) != "" {
				if node.IsNamed() {
					operands[node.Text(source)]++
				} else {
					operators[nodeType]++
				}
				for row := int(node.StartPoint().Row); row <= int(node.EndPoint().Row); row++ {
					codeLines[row] = true
				}
			}
		}

		for _, child := range node.Children() {
			walk(child)
		}
	}
	walk(root)

	loc := locFromLineSets(countLines(source), codeLines, commentLines, logical)

	totalOperators := 0
	for _, c := range operators {
		totalOperators += c
	}
	totalOperands := 0
	for _, c := range operands {
		totalOperands += c
	}
	hal := halsteadFromCounts(len(operators), len(operands), totalOperators, totalOperands)

	return loc, hal
}

// countLines returns the number of physical lines in src. A trailing newline
// does not add an extra empty line.
func countLines(src []byte) int {
	if len(src) == 0 {
		return 0
	}
	n := 0
	for _, b := range src {
		if b == '\n' {
			n++
		}
	}
	if src[len(src)-1] != '\n' {
		n++
	}
	return n
}
