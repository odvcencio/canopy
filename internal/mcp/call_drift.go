package mcp

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"

	"github.com/odvcencio/canopy/internal/deps"
	"github.com/odvcencio/canopy/pkg/index"
)

type driftEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
}

func (s *Service) callDrift(args map[string]any) (any, error) {
	target := s.stringArgOrDefault(args, "path", s.defaultRoot)
	cachePath := s.stringArgOrDefault(args, "cache", s.defaultCache)
	base := s.stringArgOrDefault(args, "base", "origin/main")

	absTarget, err := filepath.Abs(target)
	if err != nil {
		return nil, fmt.Errorf("resolving target path: %w", err)
	}

	// Build HEAD index.
	headIdx, err := s.loadOrBuild(cachePath, target)
	if err != nil {
		return nil, fmt.Errorf("building HEAD index: %w", err)
	}

	headReport, err := deps.Build(headIdx, deps.Options{Mode: "package", IncludeEdges: true})
	if err != nil {
		return nil, fmt.Errorf("building HEAD dependency graph: %w", err)
	}

	// Create git worktree for base ref.
	worktreeDir, err := os.MkdirTemp("", "canopy-drift-*")
	if err != nil {
		return nil, fmt.Errorf("creating temp dir for worktree: %w", err)
	}
	defer os.RemoveAll(worktreeDir)

	addCmd := exec.Command("git", "-C", absTarget, "worktree", "add", "--detach", worktreeDir, base)
	if out, err := addCmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("git worktree add %s: %s: %w", base, string(out), err)
	}
	defer exec.Command("git", "-C", absTarget, "worktree", "remove", "--force", worktreeDir).Run()

	// Build base index from the worktree.
	baseBuilder, err := index.NewBuilderWithWorkspaceIgnores(worktreeDir)
	if err != nil {
		return nil, fmt.Errorf("creating base index builder: %w", err)
	}
	baseIdx, err := baseBuilder.BuildPath(worktreeDir)
	if err != nil {
		return nil, fmt.Errorf("building base index: %w", err)
	}

	baseReport, err := deps.Build(baseIdx, deps.Options{Mode: "package", IncludeEdges: true})
	if err != nil {
		return nil, fmt.Errorf("building base dependency graph: %w", err)
	}

	// Diff internal edges.
	baseEdges := internalEdgeSet(baseReport.Edges)
	headEdges := internalEdgeSet(headReport.Edges)

	var added, removed []driftEdge
	for key, edge := range headEdges {
		if _, exists := baseEdges[key]; !exists {
			added = append(added, edge)
		}
	}
	for key, edge := range baseEdges {
		if _, exists := headEdges[key]; !exists {
			removed = append(removed, edge)
		}
	}

	sort.Slice(added, func(i, j int) bool {
		if added[i].From != added[j].From {
			return added[i].From < added[j].From
		}
		return added[i].To < added[j].To
	})
	sort.Slice(removed, func(i, j int) bool {
		if removed[i].From != removed[j].From {
			return removed[i].From < removed[j].From
		}
		return removed[i].To < removed[j].To
	})

	// Detect new cycles.
	baseCycles := deps.DetectCycles(deps.GraphFromEdges(baseReport.Edges))
	headCycles := deps.DetectCycles(deps.GraphFromEdges(headReport.Edges))

	baseCycleKeys := make(map[string]bool, len(baseCycles))
	for _, c := range baseCycles {
		baseCycleKeys[cycleKey(c)] = true
	}
	var newCycles []deps.Cycle
	for _, c := range headCycles {
		if !baseCycleKeys[cycleKey(c)] {
			newCycles = append(newCycles, c)
		}
	}

	return map[string]any{
		"base":       base,
		"head":       "HEAD",
		"added":      len(added),
		"removed":    len(removed),
		"new_cycles": len(newCycles),
		"details": map[string]any{
			"added_edges":   added,
			"removed_edges": removed,
			"new_cycles":    newCycles,
		},
	}, nil
}

func internalEdgeSet(edges []deps.Edge) map[string]driftEdge {
	result := make(map[string]driftEdge, len(edges))
	for _, e := range edges {
		if !e.Internal {
			continue
		}
		key := e.From + " -> " + e.To
		result[key] = driftEdge{From: e.From, To: e.To}
	}
	return result
}

func cycleKey(c deps.Cycle) string {
	key := ""
	for i, p := range c.Path {
		if i > 0 {
			key += " -> "
		}
		key += p
	}
	return key
}
