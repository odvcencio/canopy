// Package impact computes the blast radius of changed symbols by walking the reverse call graph.
package impact

import (
	"fmt"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/odvcencio/gts-suite/pkg/model"
	"github.com/odvcencio/gts-suite/pkg/xref"
)

// AffectedSymbol represents a symbol that would be impacted by a change.
type AffectedSymbol struct {
	Name      string  `json:"name"`
	File      string  `json:"file"`
	Kind      string  `json:"kind"`
	StartLine int     `json:"start_line"`
	EndLine   int     `json:"end_line"`
	Distance  int     `json:"distance"`
	Risk      float64 `json:"risk"`
}

// Result contains the full impact analysis output.
type Result struct {
	Changed       []string         `json:"changed"`
	Affected      []AffectedSymbol `json:"affected"`
	AffectedFiles []string         `json:"affected_files"`
	TotalAffected int              `json:"total_affected"`
}

// Options configures the impact analysis.
type Options struct {
	Changed  []string // explicit symbol names
	DiffRef  string   // git diff ref e.g. "HEAD~1"
	Root     string   // repo root for git operations
	MaxDepth int      // max reverse walk depth (default 10)
}

// Analyze computes the blast radius of changed symbols using reverse call graph traversal.
func Analyze(idx *model.Index, opts Options) (*Result, error) {
	if idx == nil {
		return nil, fmt.Errorf("index is nil")
	}

	maxDepth := opts.MaxDepth
	if maxDepth <= 0 {
		maxDepth = 10
	}

	graph, err := xref.Build(idx)
	if err != nil {
		return nil, fmt.Errorf("build xref graph: %w", err)
	}

	// Resolve changed definitions.
	changedDefs, changedNames, err := resolveChanged(graph, idx, opts)
	if err != nil {
		return nil, err
	}

	if len(changedDefs) == 0 {
		return &Result{
			Changed:       changedNames,
			Affected:      []AffectedSymbol{},
			AffectedFiles: []string{},
			TotalAffected: 0,
		}, nil
	}

	// Collect root IDs and build a set for exclusion.
	rootIDs := make([]string, 0, len(changedDefs))
	rootSet := map[string]bool{}
	for _, def := range changedDefs {
		rootIDs = append(rootIDs, def.ID)
		rootSet[def.ID] = true
	}

	// Walk reverse call graph to find all transitive callers.
	walk := graph.Walk(rootIDs, maxDepth, true)

	// BFS to compute distances from changed symbols.
	distances := bfsDistances(&graph, rootIDs, maxDepth)

	// Build affected list excluding the changed symbols themselves.
	affected := make([]AffectedSymbol, 0, len(walk.Nodes))
	fileSet := map[string]bool{}
	for _, node := range walk.Nodes {
		if rootSet[node.ID] {
			continue
		}
		dist, ok := distances[node.ID]
		if !ok {
			continue
		}
		affected = append(affected, AffectedSymbol{
			Name:      node.Name,
			File:      node.File,
			Kind:      node.Kind,
			StartLine: node.StartLine,
			EndLine:   node.EndLine,
			Distance:  dist,
			Risk:      1.0 / float64(dist+1),
		})
		fileSet[node.File] = true
	}

	sort.Slice(affected, func(i, j int) bool {
		if affected[i].Distance != affected[j].Distance {
			return affected[i].Distance < affected[j].Distance
		}
		if affected[i].File != affected[j].File {
			return affected[i].File < affected[j].File
		}
		return affected[i].StartLine < affected[j].StartLine
	})

	affectedFiles := make([]string, 0, len(fileSet))
	for file := range fileSet {
		affectedFiles = append(affectedFiles, file)
	}
	sort.Strings(affectedFiles)

	return &Result{
		Changed:       changedNames,
		Affected:      affected,
		AffectedFiles: affectedFiles,
		TotalAffected: len(affected),
	}, nil
}

// resolveChanged resolves changed symbol names or diff refs into xref definitions.
func resolveChanged(graph xref.Graph, idx *model.Index, opts Options) ([]xref.Definition, []string, error) {
	var allDefs []xref.Definition
	var names []string

	// From explicit symbol names.
	for _, name := range opts.Changed {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		names = append(names, name)
		defs, err := graph.FindDefinitions(name, false)
		if err != nil {
			return nil, nil, fmt.Errorf("find definitions for %q: %w", name, err)
		}
		allDefs = append(allDefs, defs...)
	}

	// From git diff.
	if strings.TrimSpace(opts.DiffRef) != "" {
		root := strings.TrimSpace(opts.Root)
		if root == "" {
			root = idx.Root
		}
		diffSymbols, err := symbolsFromDiff(idx, root, opts.DiffRef)
		if err != nil {
			return nil, nil, fmt.Errorf("symbols from diff: %w", err)
		}
		for _, name := range diffSymbols {
			names = append(names, name)
			defs, err := graph.FindDefinitions(name, false)
			if err != nil {
				return nil, nil, fmt.Errorf("find definitions for %q: %w", name, err)
			}
			allDefs = append(allDefs, defs...)
		}
	}

	// Deduplicate definitions.
	seen := map[string]bool{}
	unique := make([]xref.Definition, 0, len(allDefs))
	for _, def := range allDefs {
		if seen[def.ID] {
			continue
		}
		seen[def.ID] = true
		unique = append(unique, def)
	}

	// Deduplicate names.
	seenNames := map[string]bool{}
	uniqueNames := make([]string, 0, len(names))
	for _, n := range names {
		if seenNames[n] {
			continue
		}
		seenNames[n] = true
		uniqueNames = append(uniqueNames, n)
	}

	return unique, uniqueNames, nil
}

// bfsDistances computes the shortest distance from any root to each reachable node
// via reverse edges (incoming edges = callers).
func bfsDistances(graph *xref.Graph, rootIDs []string, maxDepth int) map[string]int {
	distances := map[string]int{}
	type item struct {
		id   string
		dist int
	}
	queue := make([]item, 0, len(rootIDs))
	for _, id := range rootIDs {
		distances[id] = 0
		queue = append(queue, item{id: id, dist: 0})
	}

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		if current.dist >= maxDepth {
			continue
		}

		for _, edge := range graph.IncomingEdges(current.id) {
			callerID := graph.EdgeCaller(edge).ID
			if _, visited := distances[callerID]; visited {
				continue
			}
			nextDist := current.dist + 1
			distances[callerID] = nextDist
			queue = append(queue, item{id: callerID, dist: nextDist})
		}
	}

	return distances
}

// symbolsFromDiff runs git diff and matches changed lines to index symbols.
func symbolsFromDiff(idx *model.Index, root, diffRef string) ([]string, error) {
	diffText, err := runGitDiff(root, diffRef)
	if err != nil {
		return nil, err
	}
	return MatchDiffToSymbols(idx, diffText), nil
}

// runGitDiff executes git diff and returns the output.
func runGitDiff(root, diffRef string) (string, error) {
	cmd := exec.Command("git", "-C", root, "diff", "--unified=0", diffRef)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git diff: %w", err)
	}
	return string(out), nil
}

var diffHunkRegex = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+(\d+)(?:,(\d+))? @@`)

// MatchDiffToSymbols parses unified diff text and matches changed line ranges to symbols
// in the index, returning the names of affected symbols.
func MatchDiffToSymbols(idx *model.Index, diffText string) []string {
	type fileRange struct {
		file      string
		startLine int
		endLine   int
	}

	var ranges []fileRange
	var currentFile string

	for _, line := range strings.Split(diffText, "\n") {
		if strings.HasPrefix(line, "+++ b/") {
			currentFile = strings.TrimPrefix(line, "+++ b/")
			continue
		}
		if currentFile == "" {
			continue
		}
		matches := diffHunkRegex.FindStringSubmatch(line)
		if matches == nil {
			continue
		}
		startLine, err := strconv.Atoi(matches[1])
		if err != nil {
			continue
		}
		count := 1
		if matches[2] != "" {
			count, err = strconv.Atoi(matches[2])
			if err != nil {
				count = 1
			}
		}
		if count == 0 {
			count = 1
		}
		endLine := startLine + count - 1
		ranges = append(ranges, fileRange{
			file:      currentFile,
			startLine: startLine,
			endLine:   endLine,
		})
	}

	// Match ranges against symbols.
	seen := map[string]bool{}
	var names []string

	for _, file := range idx.Files {
		for _, r := range ranges {
			if r.file != file.Path {
				continue
			}
			for _, symbol := range file.Symbols {
				if !isCallableKind(symbol.Kind) {
					continue
				}
				if r.startLine <= symbol.EndLine && r.endLine >= symbol.StartLine {
					if !seen[symbol.Name] {
						seen[symbol.Name] = true
						names = append(names, symbol.Name)
					}
				}
			}
		}
	}

	return names
}

func isCallableKind(kind string) bool {
	switch kind {
	case "function_definition", "method_definition":
		return true
	default:
		return false
	}
}
