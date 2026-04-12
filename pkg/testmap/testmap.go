// Package testmap maps test functions to implementation functions via structural call graph traversal.
package testmap

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/odvcencio/canopy/pkg/model"
	"github.com/odvcencio/canopy/pkg/xref"
)

// TestRef identifies a test function that covers an implementation symbol.
type TestRef struct {
	Name     string `json:"name"`
	File     string `json:"file"`
	Distance int    `json:"distance"`
}

// TestMapping describes a single implementation symbol and its test coverage.
type TestMapping struct {
	Symbol    string    `json:"symbol"`
	File      string    `json:"file"`
	Kind      string    `json:"kind"`
	StartLine int       `json:"start_line"`
	EndLine   int       `json:"end_line"`
	Tests     []TestRef `json:"tests,omitempty"`
	Coverage  string    `json:"coverage"`
}

// Report contains the full test mapping analysis.
type Report struct {
	Mappings      []TestMapping `json:"mappings"`
	TestedCount   int           `json:"tested_count"`
	UntestedCount int           `json:"untested_count"`
	Coverage      float64       `json:"coverage"`
}

// Options controls test mapping behavior.
type Options struct {
	UntestedOnly bool
	Kind         string // "function", "method", "" for all
	MaxDepth     int    // default 3
}

// Map builds a test-to-implementation mapping from the given index.
func Map(idx *model.Index, opts Options) (*Report, error) {
	if idx == nil {
		return nil, fmt.Errorf("index is nil")
	}

	maxDepth := opts.MaxDepth
	if maxDepth <= 0 {
		maxDepth = 3
	}

	graph, err := xref.Build(idx)
	if err != nil {
		return nil, fmt.Errorf("build xref graph: %w", err)
	}

	// Build file language lookup from index.
	fileLang := map[string]string{}
	for _, file := range idx.Files {
		fileLang[file.Path] = file.Language
	}

	// Partition callable definitions into test vs impl.
	var testDefs []xref.Definition
	var implDefs []xref.Definition
	for _, def := range graph.Definitions {
		if !def.Callable {
			continue
		}
		if opts.Kind != "" {
			switch opts.Kind {
			case "function":
				if def.Kind != "function_definition" {
					continue
				}
			case "method":
				if def.Kind != "method_definition" {
					continue
				}
			}
		}
		lang := fileLang[def.File]
		if IsTestFile(def.File, lang) {
			testDefs = append(testDefs, def)
		} else {
			implDefs = append(implDefs, def)
		}
	}

	// For each test function, walk forward in call graph to find reachable impl functions.
	// Build bidirectional map: implID -> []TestRef with minimum distance.
	type testHit struct {
		name     string
		file     string
		distance int
	}
	implTests := map[string][]testHit{}

	for _, testDef := range testDefs {
		walk := graph.Walk([]string{testDef.ID}, maxDepth, false)

		// BFS to compute distances from this test function.
		distances := bfsDistances(testDef.ID, &graph, walk.Edges, maxDepth)

		for _, node := range walk.Nodes {
			if node.ID == testDef.ID {
				continue
			}
			if !node.Callable {
				continue
			}
			lang := fileLang[node.File]
			if IsTestFile(node.File, lang) {
				continue
			}
			dist, ok := distances[node.ID]
			if !ok {
				continue
			}
			implTests[node.ID] = append(implTests[node.ID], testHit{
				name:     testDef.Name,
				file:     testDef.File,
				distance: dist,
			})
		}
	}

	// Deduplicate test hits per impl: keep minimum distance per (testName, testFile).
	for id, hits := range implTests {
		best := map[string]testHit{}
		for _, h := range hits {
			key := h.file + "\x00" + h.name
			existing, ok := best[key]
			if !ok || h.distance < existing.distance {
				best[key] = h
			}
		}
		deduped := make([]testHit, 0, len(best))
		for _, h := range best {
			deduped = append(deduped, h)
		}
		sort.Slice(deduped, func(i, j int) bool {
			if deduped[i].distance != deduped[j].distance {
				return deduped[i].distance < deduped[j].distance
			}
			if deduped[i].file != deduped[j].file {
				return deduped[i].file < deduped[j].file
			}
			return deduped[i].name < deduped[j].name
		})
		implTests[id] = deduped
	}

	// Build mappings for all impl definitions.
	implSet := map[string]bool{}
	for _, def := range implDefs {
		implSet[def.ID] = true
	}

	mappings := make([]TestMapping, 0, len(implDefs))
	testedCount := 0
	untestedCount := 0

	for _, def := range implDefs {
		hits := implTests[def.ID]
		var coverage string
		var refs []TestRef

		if len(hits) == 0 {
			coverage = "untested"
			untestedCount++
		} else {
			hasDirect := false
			for _, h := range hits {
				if h.distance == 1 {
					hasDirect = true
				}
				refs = append(refs, TestRef{
					Name:     h.name,
					File:     h.file,
					Distance: h.distance,
				})
			}
			if hasDirect {
				coverage = "tested"
			} else {
				coverage = "indirectly_tested"
			}
			testedCount++
		}

		if opts.UntestedOnly && coverage != "untested" {
			continue
		}

		mappings = append(mappings, TestMapping{
			Symbol:    def.Name,
			File:      def.File,
			Kind:      def.Kind,
			StartLine: def.StartLine,
			EndLine:   def.EndLine,
			Tests:     refs,
			Coverage:  coverage,
		})
	}

	// Sort mappings by file + startLine.
	sort.Slice(mappings, func(i, j int) bool {
		if mappings[i].File == mappings[j].File {
			if mappings[i].StartLine == mappings[j].StartLine {
				return mappings[i].Symbol < mappings[j].Symbol
			}
			return mappings[i].StartLine < mappings[j].StartLine
		}
		return mappings[i].File < mappings[j].File
	})

	total := testedCount + untestedCount
	var coveragePct float64
	if total > 0 {
		coveragePct = float64(testedCount) / float64(total)
	}

	return &Report{
		Mappings:      mappings,
		TestedCount:   testedCount,
		UntestedCount: untestedCount,
		Coverage:      coveragePct,
	}, nil
}

// bfsDistances computes shortest distances from a source node through the given edges.
func bfsDistances(sourceID string, graph *xref.Graph, edges []xref.Edge, maxDepth int) map[string]int {
	// Build adjacency list (forward: caller -> callee).
	adj := map[string][]string{}
	for _, e := range edges {
		adj[graph.EdgeCaller(e).ID] = append(adj[graph.EdgeCaller(e).ID], graph.EdgeCallee(e).ID)
	}

	dist := map[string]int{sourceID: 0}
	queue := []string{sourceID}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		currentDist := dist[current]
		if currentDist >= maxDepth {
			continue
		}
		for _, next := range adj[current] {
			if _, visited := dist[next]; visited {
				continue
			}
			dist[next] = currentDist + 1
			queue = append(queue, next)
		}
	}
	return dist
}

// IsTestFile determines whether a file is a test file based on its path and language.
func IsTestFile(path, language string) bool {
	path = filepath.ToSlash(filepath.Clean(path))
	base := filepath.Base(path)
	lower := strings.ToLower(base)
	lowerPath := strings.ToLower(path)
	lang := strings.ToLower(strings.TrimSpace(language))

	switch lang {
	case "go":
		return strings.HasSuffix(lower, "_test.go")

	case "python":
		if strings.HasPrefix(lower, "test_") && strings.HasSuffix(lower, ".py") {
			return true
		}
		if strings.HasSuffix(lower, "_test.py") {
			return true
		}
		if pathContainsDir(lowerPath, "tests") {
			return true
		}
		return false

	case "javascript", "typescript", "tsx", "jsx":
		for _, ext := range []string{".test.js", ".test.ts", ".test.tsx", ".test.jsx", ".spec.js", ".spec.ts", ".spec.tsx", ".spec.jsx"} {
			if strings.HasSuffix(lower, ext) {
				return true
			}
		}
		if pathContainsDir(lowerPath, "__tests__") {
			return true
		}
		return false

	case "java", "kotlin":
		if strings.HasSuffix(lower, "test.java") || strings.HasSuffix(lower, "tests.java") {
			return true
		}
		if strings.HasSuffix(lower, "test.kt") {
			return true
		}
		if pathContainsDir(lowerPath, "src/test") {
			return true
		}
		return false

	case "rust":
		if pathContainsDir(lowerPath, "tests") {
			return true
		}
		return false

	case "ruby":
		if strings.HasSuffix(lower, "_spec.rb") || strings.HasSuffix(lower, "_test.rb") {
			return true
		}
		return false

	default:
		// Fallback: use heuristics across languages.
		return isTestFileFallback(path)
	}
}

// isTestFileFallback applies language-agnostic test file heuristics.
func isTestFileFallback(path string) bool {
	lower := strings.ToLower(filepath.ToSlash(path))
	base := strings.ToLower(filepath.Base(path))

	// Go convention.
	if strings.HasSuffix(base, "_test.go") {
		return true
	}
	// Python conventions.
	if strings.HasPrefix(base, "test_") || strings.HasSuffix(base, "_test.py") {
		return true
	}
	// JS/TS conventions.
	for _, ext := range []string{".test.js", ".test.ts", ".test.tsx", ".test.jsx", ".spec.js", ".spec.ts", ".spec.tsx", ".spec.jsx"} {
		if strings.HasSuffix(base, ext) {
			return true
		}
	}
	// Java/Kotlin conventions.
	if strings.HasSuffix(base, "test.java") || strings.HasSuffix(base, "tests.java") || strings.HasSuffix(base, "test.kt") {
		return true
	}
	// Ruby conventions.
	if strings.HasSuffix(base, "_spec.rb") || strings.HasSuffix(base, "_test.rb") {
		return true
	}

	// Path-based heuristics.
	if pathContainsDir(lower, "test") || pathContainsDir(lower, "tests") || pathContainsDir(lower, "__tests__") {
		return true
	}
	if strings.Contains(base, "test") {
		return true
	}
	return false
}

// pathContainsDir checks if the slash-normalized path contains a directory component.
func pathContainsDir(path, dir string) bool {
	path = strings.ToLower(filepath.ToSlash(path))
	dir = strings.ToLower(dir)
	// Check for /dir/ anywhere in path.
	if strings.Contains(path, "/"+dir+"/") {
		return true
	}
	// Check if path starts with dir/.
	if strings.HasPrefix(path, dir+"/") {
		return true
	}
	return false
}
