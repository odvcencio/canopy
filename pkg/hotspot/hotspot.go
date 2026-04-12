// Package hotspot detects code hotspots by combining git churn, complexity, and call graph centrality.
package hotspot

import (
	"bufio"
	"fmt"
	"math"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/odvcencio/canopy/pkg/complexity"
	"github.com/odvcencio/canopy/pkg/model"
	"github.com/odvcencio/canopy/pkg/xref"
)

// FunctionHotspot holds the three-dimensional hotspot score for a function.
type FunctionHotspot struct {
	File       string  `json:"file"`
	Name       string  `json:"name"`
	Kind       string  `json:"kind"`
	StartLine  int     `json:"start_line"`
	EndLine    int     `json:"end_line"`
	Churn      float64 `json:"churn"`      // percentile-ranked 0-1
	Complexity float64 `json:"complexity"` // percentile-ranked 0-1
	Centrality float64 `json:"centrality"` // percentile-ranked 0-1
	Score      float64 `json:"score"`      // geometric mean
	Commits    int     `json:"commits"`    // raw commit count
	Authors    int     `json:"authors"`    // raw author count
	Cyclomatic int     `json:"cyclomatic"` // raw cyclomatic
	FanIn      int     `json:"fan_in"`     // raw incoming calls
}

// Report contains the full hotspot analysis result.
type Report struct {
	Functions []FunctionHotspot `json:"functions"`
	Count     int               `json:"count"`
}

// Options controls hotspot analysis.
type Options struct {
	Root  string // git repo root for churn data
	Since string // git log --since period (e.g. "90d", "6m", "1y")
	Top   int    // limit to top N results
}

// FileChurn holds raw git churn data per file.
type FileChurn struct {
	Commits int
	Authors int
}

// Analyze computes hotspot scores for functions in the index.
func Analyze(idx *model.Index, opts Options) (*Report, error) {
	if idx == nil {
		return &Report{}, nil
	}

	root := opts.Root
	if root == "" {
		root = idx.Root
	}

	// Get complexity metrics.
	compReport, err := complexity.Analyze(idx, root, complexity.Options{})
	if err != nil {
		return nil, fmt.Errorf("complexity analysis: %w", err)
	}

	// Build xref graph for centrality and enrich complexity.
	graph, err := xref.Build(idx)
	if err != nil {
		return nil, fmt.Errorf("xref build: %w", err)
	}
	complexity.EnrichWithXref(compReport, graph)

	// Get git churn data.
	rawChurn, err := GitChurn(root, opts.Since)
	if err != nil {
		// If git fails (not a repo, etc.), proceed with zero churn.
		rawChurn = map[string]FileChurn{}
	}
	churnMap := normalizeChurnMap(rawChurn, root, resolveGitRoot(root))

	// Build hotspot entries from complexity data.
	hotspots := make([]FunctionHotspot, 0, len(compReport.Functions))
	for _, fn := range compReport.Functions {
		fileChurn := churnMap[fn.File]
		hotspots = append(hotspots, FunctionHotspot{
			File:       fn.File,
			Name:       fn.Name,
			Kind:       fn.Kind,
			StartLine:  fn.StartLine,
			EndLine:    fn.EndLine,
			Commits:    fileChurn.Commits,
			Authors:    fileChurn.Authors,
			Cyclomatic: fn.Cyclomatic,
			FanIn:      fn.FanIn,
		})
	}

	if len(hotspots) == 0 {
		return &Report{}, nil
	}

	// Percentile-rank each dimension.
	rankChurn(hotspots)
	rankComplexity(hotspots)
	rankCentrality(hotspots)

	// Compute composite score.
	for i := range hotspots {
		h := &hotspots[i]
		h.Score = geometricMean(h.Churn, h.Complexity, h.Centrality)
	}

	// Sort descending by score.
	sort.SliceStable(hotspots, func(i, j int) bool {
		if hotspots[i].Score != hotspots[j].Score {
			return hotspots[i].Score > hotspots[j].Score
		}
		if hotspots[i].File != hotspots[j].File {
			return hotspots[i].File < hotspots[j].File
		}
		return hotspots[i].StartLine < hotspots[j].StartLine
	})

	if opts.Top > 0 && opts.Top < len(hotspots) {
		hotspots = hotspots[:opts.Top]
	}

	return &Report{
		Functions: hotspots,
		Count:     len(hotspots),
	}, nil
}

func normalizeChurnMap(raw map[string]FileChurn, scanRoot string, gitRoot string) map[string]FileChurn {
	if len(raw) == 0 {
		return map[string]FileChurn{}
	}

	absScanRoot := scanRoot
	if abs, err := filepath.Abs(scanRoot); err == nil {
		absScanRoot = abs
	}

	scanPrefix := ""
	if gitRoot != "" {
		if rel, err := filepath.Rel(gitRoot, absScanRoot); err == nil && rel != "." {
			scanPrefix = filepath.ToSlash(filepath.Clean(rel))
		}
	}

	absScanRoot = filepath.ToSlash(filepath.Clean(absScanRoot))
	out := make(map[string]FileChurn, len(raw))
	for path, churn := range raw {
		key := filepath.ToSlash(filepath.Clean(path))
		key = trimChurnPrefix(key, scanPrefix)
		key = trimChurnPrefix(key, absScanRoot)
		key = strings.TrimPrefix(key, "./")
		out[key] = churn
	}
	return out
}

func trimChurnPrefix(path string, prefix string) string {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return path
	}
	prefix = filepath.ToSlash(filepath.Clean(prefix))
	switch {
	case path == prefix:
		return "."
	case strings.HasPrefix(path, prefix+"/"):
		return strings.TrimPrefix(path, prefix+"/")
	default:
		return path
	}
}

// GitChurn parses git log output to compute per-file churn metrics.
func GitChurn(root, since string) (map[string]FileChurn, error) {
	if root == "" {
		root = "."
	}

	// Resolve the actual git repo root — the scan path may be a subdirectory.
	gitRoot := resolveGitRoot(root)
	if gitRoot == "" {
		gitRoot = root
	}

	args := []string{"-C", gitRoot, "log", "--numstat", "--format=%aN"}
	if since != "" {
		args = append(args, "--since="+normalizeSince(since))
	}
	// Scope to the scan path within the repo.
	args = append(args, "--", root)

	cmd := exec.Command("git", args...)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git log: %w", err)
	}

	return ParseGitLog(string(out)), nil
}

// resolveGitRoot finds the git repository root from a given path.
func resolveGitRoot(path string) string {
	cmd := exec.Command("git", "-C", path, "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// ParseGitLog extracts per-file commit counts and author counts from git log --numstat output.
func ParseGitLog(output string) map[string]FileChurn {
	result := map[string]FileChurn{}
	fileAuthors := map[string]map[string]bool{}

	scanner := bufio.NewScanner(strings.NewReader(output))
	var currentAuthor string
	// Track which files we've already counted for the current commit.
	commitFiles := map[string]bool{}

	for scanner.Scan() {
		line := scanner.Text()

		// Empty line separates commits.
		if line == "" {
			commitFiles = map[string]bool{}
			continue
		}

		// Lines starting with a digit are numstat lines: added\tremoved\tfile
		if len(line) > 0 && (line[0] >= '0' && line[0] <= '9' || line[0] == '-') {
			parts := strings.SplitN(line, "\t", 3)
			if len(parts) < 3 {
				continue
			}
			file := parts[2]
			if file == "" {
				continue
			}

			if !commitFiles[file] {
				commitFiles[file] = true
				churn := result[file]
				churn.Commits++
				result[file] = churn

				if currentAuthor != "" {
					if fileAuthors[file] == nil {
						fileAuthors[file] = map[string]bool{}
					}
					fileAuthors[file][currentAuthor] = true
				}
			}
		} else {
			// Author name line.
			currentAuthor = strings.TrimSpace(line)
		}
	}

	for file, authors := range fileAuthors {
		churn := result[file]
		churn.Authors = len(authors)
		result[file] = churn
	}

	return result
}

// normalizeSince converts shorthand like "90d" to git-compatible "--since" values.
func normalizeSince(since string) string {
	since = strings.TrimSpace(since)
	if since == "" {
		return "6 months ago"
	}

	// Already a full date or phrase.
	if strings.Contains(since, " ") || strings.Contains(since, "-") {
		return since
	}

	// Parse NNunit shorthand.
	n := len(since)
	if n < 2 {
		return since
	}
	unit := since[n-1:]
	numStr := since[:n-1]
	num, err := strconv.Atoi(numStr)
	if err != nil {
		return since
	}

	switch strings.ToLower(unit) {
	case "d":
		return fmt.Sprintf("%d days ago", num)
	case "w":
		return fmt.Sprintf("%d weeks ago", num)
	case "m":
		return fmt.Sprintf("%d months ago", num)
	case "y":
		return fmt.Sprintf("%d years ago", num)
	default:
		return since
	}
}

// geometricMean computes the geometric mean of three values.
// Uses a small epsilon to avoid zeroing out the score when one dimension is 0.
func geometricMean(a, b, c float64) float64 {
	const epsilon = 0.01
	a = math.Max(a, epsilon)
	b = math.Max(b, epsilon)
	c = math.Max(c, epsilon)
	return math.Pow(a*b*c, 1.0/3.0)
}

// rankChurn assigns percentile ranks (0-1) based on commits + authors.
func rankChurn(hotspots []FunctionHotspot) {
	values := make([]float64, len(hotspots))
	for i, h := range hotspots {
		values[i] = float64(h.Commits) + float64(h.Authors)*0.5
	}
	ranks := percentileRank(values)
	for i := range hotspots {
		hotspots[i].Churn = ranks[i]
	}
}

// rankComplexity assigns percentile ranks based on cyclomatic complexity.
func rankComplexity(hotspots []FunctionHotspot) {
	values := make([]float64, len(hotspots))
	for i, h := range hotspots {
		values[i] = float64(h.Cyclomatic)
	}
	ranks := percentileRank(values)
	for i := range hotspots {
		hotspots[i].Complexity = ranks[i]
	}
}

// rankCentrality assigns percentile ranks based on fan-in count.
func rankCentrality(hotspots []FunctionHotspot) {
	values := make([]float64, len(hotspots))
	for i, h := range hotspots {
		values[i] = float64(h.FanIn)
	}
	ranks := percentileRank(values)
	for i := range hotspots {
		hotspots[i].Centrality = ranks[i]
	}
}

// percentileRank converts raw values to percentile ranks (0-1).
// Uses the "percentage of values less than or equal" method.
func percentileRank(values []float64) []float64 {
	n := len(values)
	if n == 0 {
		return nil
	}
	if n == 1 {
		return []float64{1.0}
	}

	// Sort a copy to get rank positions.
	sorted := make([]float64, n)
	copy(sorted, values)
	sort.Float64s(sorted)

	ranks := make([]float64, n)
	for i, v := range values {
		// Count how many values are strictly less than this one.
		pos := sort.SearchFloat64s(sorted, v)
		ranks[i] = float64(pos) / float64(n-1)
	}
	return ranks
}
