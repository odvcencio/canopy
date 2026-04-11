package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/odvcencio/gts-suite/internal/deps"
	"github.com/odvcencio/gts-suite/pkg/index"
	"github.com/odvcencio/gts-suite/pkg/model"
)

type driftEdge struct {
	Type string `json:"type"` // "added" or "removed"
	From string `json:"from"`
	To   string `json:"to"`
}

type driftResult struct {
	Base      string      `json:"base"`
	Head      string      `json:"head"`
	Added     int         `json:"added"`
	Removed   int         `json:"removed"`
	NewCycles int         `json:"new_cycles,omitempty"`
	Details   []driftEdge `json:"details,omitempty"`
}

func newDriftCmd() *cobra.Command {
	var (
		cachePath  string
		noCache    bool
		jsonOutput bool
		base       string
	)

	cmd := &cobra.Command{
		Use:   "drift [path]",
		Short: "Compare dependency graph between two git refs",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := "."
			if len(args) == 1 {
				target = args[0]
			}
			absTarget, err := filepath.Abs(target)
			if err != nil {
				return fmt.Errorf("resolving target path: %w", err)
			}

			// Resolve HEAD ref.
			headRef, err := gitRevParse(absTarget, "HEAD")
			if err != nil {
				return fmt.Errorf("resolving HEAD: %w", err)
			}

			// Resolve base ref.
			baseRef, err := gitRevParse(absTarget, base)
			if err != nil {
				return fmt.Errorf("resolving base ref %q: %w", base, err)
			}

			// 1. Build HEAD index using existing loadOrBuild.
			headIdx, err := loadOrBuild(cmd, cachePath, target, noCache)
			if err != nil {
				return fmt.Errorf("building HEAD index: %w", err)
			}

			// 2. Build base index via temporary worktree.
			baseIdx, err := buildBaseIndex(cmd, absTarget, base)
			if err != nil {
				return fmt.Errorf("building base index: %w", err)
			}

			// 3. Build dep graphs from both indexes.
			headReport, err := deps.Build(headIdx, deps.Options{Mode: "package", IncludeEdges: true})
			if err != nil {
				return fmt.Errorf("building HEAD deps: %w", err)
			}
			baseReport, err := deps.Build(baseIdx, deps.Options{Mode: "package", IncludeEdges: true})
			if err != nil {
				return fmt.Errorf("building base deps: %w", err)
			}

			// 4. Diff internal edges.
			headEdges := internalEdgeSet(headReport.Edges)
			baseEdges := internalEdgeSet(baseReport.Edges)

			var details []driftEdge
			addedCount := 0
			removedCount := 0

			for key, edge := range headEdges {
				if _, ok := baseEdges[key]; !ok {
					addedCount++
					details = append(details, driftEdge{
						Type: "added",
						From: edge.From,
						To:   edge.To,
					})
				}
			}
			for key, edge := range baseEdges {
				if _, ok := headEdges[key]; !ok {
					removedCount++
					details = append(details, driftEdge{
						Type: "removed",
						From: edge.From,
						To:   edge.To,
					})
				}
			}

			// Sort details for deterministic output: added before removed, then by from/to.
			sortDriftEdges(details)

			// 5. Count new cycles: cycles in HEAD that are not in base.
			baseCycles := deps.DetectCycles(deps.GraphFromEdges(baseReport.Edges))
			headCycles := deps.DetectCycles(deps.GraphFromEdges(headReport.Edges))
			newCycles := countNewCycles(headCycles, baseCycles)

			result := driftResult{
				Base:      shortRef(baseRef),
				Head:      shortRef(headRef),
				Added:     addedCount,
				Removed:   removedCount,
				NewCycles: newCycles,
				Details:   details,
			}

			if jsonOutput {
				return emitJSON(result)
			}

			// Text output.
			cyclePart := ""
			if newCycles > 0 {
				cyclePart = fmt.Sprintf("  %d new cycles", newCycles)
			}
			fmt.Printf("drift: %s..%s  +%d imports  -%d imports%s\n",
				base, "HEAD", result.Added, result.Removed, cyclePart)

			for _, d := range details {
				fmt.Printf("  [%s] %s -> %s\n", d.Type, d.From, d.To)
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&cachePath, "cache", "", "load HEAD index from cache instead of parsing")
	cmd.Flags().BoolVar(&noCache, "no-cache", false, "skip auto-discovery of cached index")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "emit JSON output")
	cmd.Flags().StringVar(&base, "base", "origin/main", "git ref to compare against")
	return cmd
}

// buildBaseIndex creates a temporary git worktree for the given ref, builds an
// index from it, and cleans up the worktree.
func buildBaseIndex(cmd *cobra.Command, repoDir, ref string) (*model.Index, error) {
	tmpDir, err := os.MkdirTemp("", "gts-drift-*")
	if err != nil {
		return nil, fmt.Errorf("creating temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	worktreePath := filepath.Join(tmpDir, "worktree")

	// Create a detached worktree at the base ref.
	addCmd := exec.Command("git", "-C", repoDir, "worktree", "add", "--detach", worktreePath, ref)
	addCmd.Stderr = os.Stderr
	if err := addCmd.Run(); err != nil {
		return nil, fmt.Errorf("git worktree add: %w", err)
	}
	defer func() {
		rmCmd := exec.Command("git", "-C", repoDir, "worktree", "remove", "--force", worktreePath)
		rmCmd.Stderr = os.Stderr
		_ = rmCmd.Run()
	}()

	// Apply CLI --exclude patterns to the base index too, so drift reports
	// are comparing apples to apples.
	builder, err := index.NewBuilderWithWorkspaceIgnoresAndExtras(worktreePath, cmdExcludes(cmd))
	if err != nil {
		return nil, fmt.Errorf("creating builder for base: %w", err)
	}
	idx, err := builder.BuildPath(worktreePath)
	if err != nil {
		return nil, fmt.Errorf("building base index: %w", err)
	}
	return idx, nil
}

// gitRevParse resolves a git ref to its full SHA.
func gitRevParse(repoDir, ref string) (string, error) {
	cmd := exec.Command("git", "-C", repoDir, "rev-parse", ref)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// shortRef returns the first 10 characters of a SHA, or the full string if shorter.
func shortRef(sha string) string {
	if len(sha) > 10 {
		return sha[:10]
	}
	return sha
}

// internalEdgeSet builds a lookup map of internal edges keyed by "from->to".
func internalEdgeSet(edges []deps.Edge) map[string]deps.Edge {
	set := make(map[string]deps.Edge)
	for _, e := range edges {
		if e.Internal {
			set[e.From+"->"+e.To] = e
		}
	}
	return set
}

// countNewCycles returns the number of cycles in head that are not present in base.
func countNewCycles(headCycles, baseCycles []deps.Cycle) int {
	baseSet := make(map[string]bool, len(baseCycles))
	for _, c := range baseCycles {
		baseSet[strings.Join(c.Path, " -> ")] = true
	}
	count := 0
	for _, c := range headCycles {
		if !baseSet[strings.Join(c.Path, " -> ")] {
			count++
		}
	}
	return count
}

// sortDriftEdges sorts details: added before removed, then alphabetically by from, then to.
func sortDriftEdges(edges []driftEdge) {
	typeOrder := map[string]int{"added": 0, "removed": 1}
	for i := 0; i < len(edges); i++ {
		for j := i + 1; j < len(edges); j++ {
			if driftEdgeLess(edges[j], edges[i], typeOrder) {
				edges[i], edges[j] = edges[j], edges[i]
			}
		}
	}
}

func driftEdgeLess(a, b driftEdge, typeOrder map[string]int) bool {
	if typeOrder[a.Type] != typeOrder[b.Type] {
		return typeOrder[a.Type] < typeOrder[b.Type]
	}
	if a.From != b.From {
		return a.From < b.From
	}
	return a.To < b.To
}
