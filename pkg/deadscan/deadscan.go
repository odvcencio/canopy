// Package deadscan double-checks zero-incoming dead-code candidates against
// the source text. The call graph only sees call-shaped references, so a
// callable used exclusively as a value — passed as a callback, stored in a
// dispatch table, bound to a variable — has zero incoming edges while being
// live. A word-boundary identifier scan of the candidate's own directory
// catches those uses cheaply; any hit means "not provably dead".
package deadscan

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"m31labs.dev/canopy/pkg/model"
)

// Candidate identifies a zero-incoming definition to double-check.
type Candidate struct {
	File      string
	Name      string
	StartLine int
	EndLine   int
}

var identifierPattern = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*`)

// MentionCounts returns, per candidate, the number of identifier mentions of
// the candidate's name in non-test indexed files within the candidate's own
// directory, excluding mentions inside the candidate's definition span (its
// signature and any self-recursion). The directory bound keeps the scan
// cheap and matches the visibility of unexported symbols, which dominate
// real dead-code candidates.
func MentionCounts(root string, files []model.FileSummary, candidates []Candidate) []int {
	counts := make([]int, len(candidates))
	if len(candidates) == 0 {
		return counts
	}

	// dir -> name -> candidate indices
	byDir := map[string]map[string][]int{}
	for i, cand := range candidates {
		dir := filepath.ToSlash(filepath.Dir(cand.File))
		names := byDir[dir]
		if names == nil {
			names = map[string][]int{}
			byDir[dir] = names
		}
		names[cand.Name] = append(names[cand.Name], i)
	}

	for _, file := range files {
		if isTestFile(file.Path) {
			continue
		}
		names := byDir[filepath.ToSlash(filepath.Dir(file.Path))]
		if len(names) == 0 {
			continue
		}
		scanFile(root, file.Path, names, candidates, counts)
	}
	return counts
}

func scanFile(root, relPath string, names map[string][]int, candidates []Candidate, counts []int) {
	f, err := os.Open(filepath.Join(root, filepath.FromSlash(relPath)))
	if err != nil {
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	line := 0
	for scanner.Scan() {
		line++
		text := scanner.Text()
		if !containsAnyName(text, names) {
			continue
		}
		for _, token := range identifierPattern.FindAllString(text, -1) {
			for _, candIdx := range names[token] {
				cand := candidates[candIdx]
				if cand.File == relPath && line >= cand.StartLine && line <= cand.EndLine {
					continue
				}
				counts[candIdx]++
			}
		}
	}
}

// containsAnyName is a cheap pre-filter so most lines skip tokenization.
func containsAnyName(line string, names map[string][]int) bool {
	for name := range names {
		if strings.Contains(line, name) {
			return true
		}
	}
	return false
}

func isTestFile(path string) bool {
	base := filepath.Base(path)
	return strings.Contains(base, "_test.") ||
		strings.Contains(base, ".test.") ||
		strings.Contains(base, ".spec.")
}
