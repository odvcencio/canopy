package similarity

import (
	"crypto/sha256"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/odvcencio/canopy/pkg/model"
)

// FunctionPrint represents a normalized fingerprint of a function.
type FunctionPrint struct {
	File           string `json:"file"`
	Name           string `json:"name"`
	StartLine      int    `json:"start_line"`
	EndLine        int    `json:"end_line"`
	BodyHash       string `json:"body_hash"`
	normalizedBody string // cached, not serialized
}

// Pair represents a pair of similar functions.
type Pair struct {
	A      FunctionPrint `json:"a"`
	B      FunctionPrint `json:"b"`
	Score  float64       `json:"score"`
	Method string        `json:"method"` // "exact" or "ngram"
}

var (
	hexAddr  = regexp.MustCompile(`0x[0-9a-fA-F]+`)
	localVar = regexp.MustCompile(`(?:var|local)_\d+`)
	wsRun    = regexp.MustCompile(`\s+`)
)

// NormalizeBody strips addresses, renames local vars sequentially, collapses whitespace.
func NormalizeBody(body string) string {
	result := hexAddr.ReplaceAllString(body, "ADDR")
	varCounter := 0
	seen := make(map[string]string)
	result = localVar.ReplaceAllStringFunc(result, func(match string) string {
		if replacement, ok := seen[match]; ok {
			return replacement
		}
		replacement := fmt.Sprintf("v%d", varCounter)
		seen[match] = replacement
		varCounter++
		return replacement
	})
	result = wsRun.ReplaceAllString(strings.TrimSpace(result), " ")
	return result
}

func hashBody(body string) string {
	h := sha256.Sum256([]byte(body))
	return fmt.Sprintf("%x", h)
}

// Ngrams generates token n-grams from a string.
func Ngrams(s string, n int) map[string]bool {
	tokens := strings.Fields(s)
	grams := make(map[string]bool)
	for i := 0; i <= len(tokens)-n; i++ {
		gram := strings.Join(tokens[i:i+n], " ")
		grams[gram] = true
	}
	return grams
}

// Jaccard computes the Jaccard similarity between two sets.
func Jaccard(a, b map[string]bool) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 0
	}
	intersection := 0
	for k := range a {
		if b[k] {
			intersection++
		}
	}
	union := len(a) + len(b) - intersection
	if union == 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}

func readFunctionBody(root, file string, startLine, endLine int) (string, error) {
	path := file
	if root != "" {
		path = root + "/" + file
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	lines := strings.Split(string(data), "\n")
	if startLine < 1 {
		startLine = 1
	}
	if endLine > len(lines) {
		endLine = len(lines)
	}
	return strings.Join(lines[startLine-1:endLine], "\n"), nil
}

func extractPrints(idx *model.Index, root string) []FunctionPrint {
	var prints []FunctionPrint
	for _, f := range idx.Files {
		for _, sym := range f.Symbols {
			if sym.Kind != "function_definition" && sym.Kind != "method_definition" {
				continue
			}
			body, err := readFunctionBody(root, f.Path, sym.StartLine, sym.EndLine)
			if err != nil {
				continue
			}
			normalized := NormalizeBody(body)
			prints = append(prints, FunctionPrint{
				File:           f.Path,
				Name:           sym.Name,
				StartLine:      sym.StartLine,
				EndLine:        sym.EndLine,
				BodyHash:       hashBody(normalized),
				normalizedBody: normalized,
			})
		}
	}
	return prints
}

// Compare finds similar functions between two indexes.
// maxFuncs caps the number of functions from each index (0 = unlimited).
func Compare(a, b *model.Index, aRoot, bRoot string, threshold float64, top int, maxFuncs int) ([]Pair, error) {
	aPrints := extractPrints(a, aRoot)
	bPrints := extractPrints(b, bRoot)

	// Cap function count to bound the O(n²) comparison.
	if maxFuncs > 0 {
		// Sort by body size descending so we keep the largest/most interesting functions.
		sort.Slice(aPrints, func(i, j int) bool {
			return len(aPrints[i].normalizedBody) > len(aPrints[j].normalizedBody)
		})
		sort.Slice(bPrints, func(i, j int) bool {
			return len(bPrints[i].normalizedBody) > len(bPrints[j].normalizedBody)
		})
		if len(aPrints) > maxFuncs {
			aPrints = aPrints[:maxFuncs]
		}
		if len(bPrints) > maxFuncs {
			bPrints = bPrints[:maxFuncs]
		}
	}

	var pairs []Pair

	for _, ap := range aPrints {
		for _, bp := range bPrints {
			if ap.File == bp.File && ap.Name == bp.Name {
				continue
			}
			// Skip pairs at the same location — tree-sitter can emit multiple
			// symbols (function name + return type) for the same function body.
			if ap.File == bp.File && ap.StartLine == bp.StartLine && ap.EndLine == bp.EndLine {
				continue
			}

			// Exact match
			if ap.BodyHash == bp.BodyHash {
				pairs = append(pairs, Pair{A: ap, B: bp, Score: 1.0, Method: "exact"})
				continue
			}

			// Size-ratio short-circuit: if shorter body < 1/3 longer, Jaccard can't reach 0.7
			aNorm := ap.normalizedBody
			bNorm := bp.normalizedBody
			shorter, longer := len(aNorm), len(bNorm)
			if shorter > longer {
				shorter, longer = longer, shorter
			}
			if longer > 0 && float64(shorter)/float64(longer) < 0.33 {
				continue
			}

			aGrams := Ngrams(aNorm, 3)
			bGrams := Ngrams(bNorm, 3)
			score := Jaccard(aGrams, bGrams)
			if score >= threshold {
				pairs = append(pairs, Pair{A: ap, B: bp, Score: score, Method: "ngram"})
			}
		}
	}

	sort.Slice(pairs, func(i, j int) bool {
		return pairs[i].Score > pairs[j].Score
	})

	// Apply top-N cap
	if top > 0 && len(pairs) > top {
		pairs = pairs[:top]
	}

	return pairs, nil
}
