package lint

import (
	"bytes"
	"strings"
)

// Suppression represents an inline lint suppression comment found in source code.
type Suppression struct {
	Metric string // metric name or "*" for all
	Line   int    // 1-based line number of the comment
	File   bool   // true for file-level suppression (//canopy:lint-ignore-file)
}

// ParseSuppressions scans source code for inline suppression comments and
// returns all found suppressions.
//
// Supported formats:
//
//	//canopy:lint-ignore cyclomatic — intentionally complex
//	//canopy:lint-ignore-file — generated code
//
// The comment marker must appear at the start of the trimmed line (possibly
// preceded by whitespace). Everything after the metric name is treated as an
// optional human-readable reason and is discarded.
func ParseSuppressions(source []byte) []Suppression {
	if len(source) == 0 {
		return nil
	}

	var result []Suppression
	lineNo := 0

	for _, rawLine := range bytes.Split(source, []byte("\n")) {
		lineNo++
		line := strings.TrimSpace(string(rawLine))

		// File-level suppression: //canopy:lint-ignore-file
		if strings.HasPrefix(line, "//canopy:lint-ignore-file") {
			result = append(result, Suppression{
				Metric: "*",
				Line:   lineNo,
				File:   true,
			})
			continue
		}

		// Line/function-level suppression: //canopy:lint-ignore <metric>
		if strings.HasPrefix(line, "//canopy:lint-ignore") {
			rest := strings.TrimPrefix(line, "//canopy:lint-ignore")
			rest = strings.TrimSpace(rest)

			// Strip optional reason after em-dash, double-dash, or #
			metric := extractMetric(rest)
			if metric == "" {
				metric = "*"
			}

			result = append(result, Suppression{
				Metric: strings.ToLower(metric),
				Line:   lineNo,
				File:   false,
			})
			continue
		}
	}

	return result
}

// extractMetric pulls the first word out of the rest-of-line after the
// directive prefix, stopping at reason separators (—, --, #) or end of string.
func extractMetric(rest string) string {
	if rest == "" {
		return ""
	}

	// Remove reason separators first.
	for _, sep := range []string{" — ", " -- ", " # "} {
		if idx := strings.Index(rest, sep); idx >= 0 {
			rest = rest[:idx]
		}
	}

	// Also handle em-dash without spaces.
	if idx := strings.Index(rest, "—"); idx >= 0 {
		rest = rest[:idx]
	}

	rest = strings.TrimSpace(rest)

	// Take only the first word (the metric name).
	if idx := strings.IndexByte(rest, ' '); idx >= 0 {
		rest = rest[:idx]
	}

	return rest
}

// IsSuppressed reports whether a lint violation at the given startLine for the
// given metric is suppressed by any of the provided suppressions.
//
// A file-level suppression suppresses all violations in the file regardless of
// line number. A line-level suppression suppresses a violation if the
// suppression comment appears on the line immediately before startLine.
func IsSuppressed(suppressions []Suppression, startLine int, metric string) bool {
	metric = strings.ToLower(metric)

	for _, s := range suppressions {
		// File-level suppression covers everything.
		if s.File {
			if s.Metric == "*" || s.Metric == metric {
				return true
			}
			continue
		}

		// Line-level: the comment must be on the line immediately before the target.
		if s.Line+1 != startLine {
			continue
		}

		if s.Metric == "*" || s.Metric == metric {
			return true
		}
	}

	return false
}
