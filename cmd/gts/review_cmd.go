package main

import (
	"fmt"
	"os/exec"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/odvcencio/gts-suite/internal/deps"
	"github.com/odvcencio/gts-suite/pkg/boundaries"
	"github.com/odvcencio/gts-suite/pkg/capa"
	"github.com/odvcencio/gts-suite/pkg/complexity"
	"github.com/odvcencio/gts-suite/pkg/impact"
	"github.com/odvcencio/gts-suite/pkg/xref"
)

type reviewComplexityDelta struct {
	File       string `json:"file"`
	Name       string `json:"name"`
	Cyclomatic int    `json:"cyclomatic"`
	Cognitive  int    `json:"cognitive"`
	Lines      int    `json:"lines"`
}

type reviewCapaMatch struct {
	Name       string `json:"name"`
	Category   string `json:"category"`
	Confidence string `json:"confidence"`
	AttackID   string `json:"attack_id,omitempty"`
}

type reviewReport struct {
	Base            string                  `json:"base"`
	ChangedFiles    int                     `json:"changed_files"`
	Files           []string                `json:"files"`
	ComplexityDelta []reviewComplexityDelta  `json:"complexity_delta,omitempty"`
	BoundaryIssues  []boundaries.Violation  `json:"boundary_issues,omitempty"`
	NewCapabilities []reviewCapaMatch       `json:"new_capabilities,omitempty"`
	BlastRadius     int                     `json:"blast_radius"`
}

func newReviewCmd() *cobra.Command {
	var (
		cachePath  string
		noCache    bool
		base       string
		jsonOutput bool
	)

	cmd := &cobra.Command{
		Use:   "review [path]",
		Short: "Aggregate review report for changed files vs a base ref",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if base == "" {
				return fmt.Errorf("--base is required")
			}

			target := "."
			if len(args) == 1 {
				target = args[0]
			}

			// Get changed files via git diff.
			changed, err := reviewChangedFiles(target, base)
			if err != nil {
				return err
			}
			if len(changed) == 0 {
				report := reviewReport{Base: base, ChangedFiles: 0, Files: []string{}}
				if jsonOutput {
					return emitJSON(report)
				}
				fmt.Printf("review: base=%s changed_files=0\n", base)
				return nil
			}

			changedSet := make(map[string]bool, len(changed))
			for _, f := range changed {
				changedSet[f] = true
			}

			idx, err := loadOrBuild(cmd, cachePath, target, noCache)
			if err != nil {
				return err
			}
			idx = applyGeneratedFilter(cmd, idx)

			report := reviewReport{
				Base:         base,
				ChangedFiles: len(changed),
				Files:        changed,
			}

			// 1. Complexity for changed files.
			compReport, compErr := complexity.Analyze(idx, idx.Root, complexity.Options{})
			if compErr == nil && compReport != nil {
				graph, xrefErr := xref.Build(idx)
				if xrefErr == nil {
					complexity.EnrichWithXref(compReport, graph)
				}
				for _, fn := range compReport.Functions {
					if !changedSet[fn.File] {
						continue
					}
					report.ComplexityDelta = append(report.ComplexityDelta, reviewComplexityDelta{
						File:       fn.File,
						Name:       fn.Name,
						Cyclomatic: fn.Cyclomatic,
						Cognitive:  fn.Cognitive,
						Lines:      fn.Lines,
					})
				}
			}

			// 2. Boundary violations.
			cfg, cfgErr := boundaries.LoadConfig(target)
			if cfgErr == nil && cfg != nil && len(cfg.Rules) > 0 {
				depReport, depErr := deps.Build(idx, deps.Options{Mode: "package", IncludeEdges: true})
				if depErr == nil {
					importEdges := make([]boundaries.ImportEdge, 0, len(depReport.Edges))
					for _, edge := range depReport.Edges {
						if edge.Internal {
							importEdges = append(importEdges, boundaries.ImportEdge{From: edge.From, To: edge.To})
						}
					}
					violations := boundaries.Evaluate(cfg, importEdges)
					for _, v := range violations {
						for _, f := range changed {
							pkg := reviewFileToPackage(f)
							if pkg == v.From || pkg == v.To {
								report.BoundaryIssues = append(report.BoundaryIssues, v)
								break
							}
						}
					}
				}
			}

			// 3. Capabilities in changed files.
			rules := capa.BuiltinRules()
			changedIdx := *idx
			changedIdx.Files = nil
			for _, f := range idx.Files {
				if changedSet[f.Path] {
					changedIdx.Files = append(changedIdx.Files, f)
				}
			}
			matches := capa.Detect(&changedIdx, rules)
			for _, m := range matches {
				report.NewCapabilities = append(report.NewCapabilities, reviewCapaMatch{
					Name:       m.Rule.Name,
					Category:   m.Rule.Category,
					Confidence: m.Rule.Confidence,
					AttackID:   m.Rule.AttackID,
				})
			}

			// 4. Blast radius.
			impactResult, impactErr := impact.Analyze(idx, impact.Options{
				DiffRef:  base,
				Root:     target,
				MaxDepth: 5,
			})
			if impactErr == nil && impactResult != nil {
				report.BlastRadius = impactResult.TotalAffected
			}

			if jsonOutput {
				return emitJSON(report)
			}

			// Text output.
			fmt.Printf("review: base=%s changed_files=%d blast_radius=%d\n", report.Base, report.ChangedFiles, report.BlastRadius)
			if len(report.ComplexityDelta) > 0 {
				fmt.Println("\ncomplexity in changed files:")
				for _, cd := range report.ComplexityDelta {
					fmt.Printf("  %s %s cyc=%d cog=%d lines=%d\n", cd.File, cd.Name, cd.Cyclomatic, cd.Cognitive, cd.Lines)
				}
			}
			if len(report.BoundaryIssues) > 0 {
				fmt.Printf("\nboundary violations: %d\n", len(report.BoundaryIssues))
				for _, v := range report.BoundaryIssues {
					fmt.Printf("  %s\n", v.Message)
				}
			}
			if len(report.NewCapabilities) > 0 {
				fmt.Printf("\ncapabilities detected: %d\n", len(report.NewCapabilities))
				for _, c := range report.NewCapabilities {
					fmt.Printf("  %s (%s, %s)\n", c.Name, c.Category, c.Confidence)
				}
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&cachePath, "cache", "", "load index from cache instead of parsing")
	cmd.Flags().BoolVar(&noCache, "no-cache", false, "skip auto-discovery of cached index")
	cmd.Flags().StringVar(&base, "base", "", "git ref to diff against (required)")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "emit JSON output")
	return cmd
}

func reviewChangedFiles(repoDir, base string) ([]string, error) {
	gitCmd := exec.Command("git", "-C", repoDir, "diff", "--name-only", base)
	out, err := gitCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git diff --name-only %s: %w", base, err)
	}
	var files []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			files = append(files, line)
		}
	}
	sort.Strings(files)
	return files, nil
}

func reviewFileToPackage(path string) string {
	parts := strings.Split(path, "/")
	if len(parts) <= 1 {
		return "."
	}
	return strings.Join(parts[:len(parts)-1], "/")
}
