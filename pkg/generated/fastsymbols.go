package generated

import (
	"regexp"
	"strings"

	"github.com/odvcencio/gts-suite/pkg/model"
)

// FastExtractThreshold is the file size above which generated files use
// fast regex extraction instead of full tree-sitter parsing.
const FastExtractThreshold = 100 * 1024 // 100KB

// Language-specific patterns for fast symbol extraction.
// These capture top-level declarations that other code might reference.
var fastPatterns = map[string][]*regexp.Regexp{
	"go": {
		regexp.MustCompile(`(?m)^type\s+(\w+)\s+(?:struct|interface)\b`),
		regexp.MustCompile(`(?m)^func\s+(\w+)\s*\(`),
		regexp.MustCompile(`(?m)^func\s+\(\w+\s+\*?(\w+)\)\s+(\w+)\s*\(`),
		regexp.MustCompile(`(?m)^var\s+(\w+)\s+`),
		regexp.MustCompile(`(?m)^const\s+(\w+)\s+`),
	},
	"python": {
		regexp.MustCompile(`(?m)^class\s+(\w+)`),
		regexp.MustCompile(`(?m)^def\s+(\w+)\s*\(`),
	},
	"javascript": {
		regexp.MustCompile(`(?m)^(?:export\s+)?(?:class|function)\s+(\w+)`),
		regexp.MustCompile(`(?m)^(?:export\s+)?(?:const|let|var)\s+(\w+)\s*=`),
	},
	"typescript": {
		regexp.MustCompile(`(?m)^(?:export\s+)?(?:class|function|interface|type|enum)\s+(\w+)`),
		regexp.MustCompile(`(?m)^(?:export\s+)?(?:const|let|var)\s+(\w+)\s*[=:]`),
	},
	"java": {
		regexp.MustCompile(`(?m)^\s*(?:public|private|protected)?\s*(?:static\s+)?(?:class|interface|enum)\s+(\w+)`),
		regexp.MustCompile(`(?m)^\s*(?:public|private|protected)\s+(?:static\s+)?[\w<>\[\]]+\s+(\w+)\s*\(`),
	},
	"rust": {
		regexp.MustCompile(`(?m)^pub\s+(?:struct|enum|trait|fn)\s+(\w+)`),
		regexp.MustCompile(`(?m)^(?:struct|enum|trait|fn)\s+(\w+)`),
	},
	"c": {
		regexp.MustCompile(`(?m)^(?:typedef\s+)?(?:struct|enum|union)\s+(\w+)`),
		regexp.MustCompile(`(?m)^(?:static\s+)?(?:inline\s+)?[\w*]+\s+(\w+)\s*\(`),
	},
}

// FastExtractSymbols does fast regex-based symbol extraction for large generated files.
// Returns a FileSummary with symbols but no references (references from generated code
// are not useful — we only need symbols so the xref graph can resolve references INTO
// generated code from human-written code).
func FastExtractSymbols(path string, source []byte, language string) model.FileSummary {
	summary := model.FileSummary{
		Path:     path,
		Language: language,
	}

	patterns, ok := fastPatterns[language]
	if !ok {
		// No patterns for this language — return empty summary
		// (still tagged as generated, just no symbols)
		return summary
	}

	seen := make(map[string]bool)
	lineOffsets := buildLineOffsets(source)

	for _, re := range patterns {
		matches := re.FindAllSubmatchIndex(source, -1)
		for _, match := range matches {
			// Get the last capture group (the symbol name)
			nameStart := match[len(match)-2]
			nameEnd := match[len(match)-1]
			if nameStart < 0 {
				continue
			}
			name := string(source[nameStart:nameEnd])
			if seen[name] {
				continue
			}
			seen[name] = true

			line := lineFromOffset(lineOffsets, match[0])
			kind := inferKind(re, language)

			summary.Symbols = append(summary.Symbols, model.Symbol{
				File:      path,
				Kind:      kind,
				Name:      name,
				StartLine: line,
				EndLine:   line,
			})
		}
	}

	return summary
}

func buildLineOffsets(source []byte) []int {
	offsets := []int{0}
	for i, b := range source {
		if b == '\n' {
			offsets = append(offsets, i+1)
		}
	}
	return offsets
}

func lineFromOffset(offsets []int, offset int) int {
	lo, hi := 0, len(offsets)
	for lo < hi {
		mid := (lo + hi) / 2
		if offsets[mid] <= offset {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	return lo // 1-based
}

func inferKind(re *regexp.Regexp, language string) string {
	pattern := re.String()
	switch {
	case strings.Contains(pattern, "struct") || strings.Contains(pattern, "class") || strings.Contains(pattern, "interface") || strings.Contains(pattern, "enum"):
		return "type_definition"
	case strings.Contains(pattern, "func") || strings.Contains(pattern, "def") || strings.Contains(pattern, "function") || strings.Contains(pattern, "fn"):
		if strings.Contains(pattern, `\(\w+\s+\*?`) { // method receiver pattern
			return "method_definition"
		}
		return "function_definition"
	case strings.Contains(pattern, "var") || strings.Contains(pattern, "const") || strings.Contains(pattern, "let"):
		return "variable_definition"
	default:
		return "function_definition"
	}
}
