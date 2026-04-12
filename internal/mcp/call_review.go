package mcp

import (
	"fmt"
	"os/exec"
	"sort"
	"strings"

	"github.com/odvcencio/canopy/internal/deps"
	"github.com/odvcencio/canopy/pkg/boundaries"
	"github.com/odvcencio/canopy/pkg/capa"
	"github.com/odvcencio/canopy/pkg/complexity"
	"github.com/odvcencio/canopy/pkg/impact"
	"github.com/odvcencio/canopy/pkg/xref"
)

type reviewComplexityDelta struct {
	File       string `json:"file"`
	Name       string `json:"name"`
	Cyclomatic int    `json:"cyclomatic"`
	Cognitive  int    `json:"cognitive"`
	Lines      int    `json:"lines"`
}

type reviewReport struct {
	Base            string                 `json:"base"`
	ChangedFiles    int                    `json:"changed_files"`
	Files           []string               `json:"files"`
	ComplexityDelta []reviewComplexityDelta `json:"complexity_delta,omitempty"`
	BoundaryIssues  []boundaries.Violation  `json:"boundary_issues,omitempty"`
	NewCapabilities []reviewCapaMatch       `json:"new_capabilities,omitempty"`
	BlastRadius     int                     `json:"blast_radius"`
}

type reviewCapaMatch struct {
	Name       string `json:"name"`
	Category   string `json:"category"`
	Confidence string `json:"confidence"`
	AttackID   string `json:"attack_id,omitempty"`
}

func (s *Service) callReview(args map[string]any) (any, error) {
	base, err := requiredStringArg(args, "base")
	if err != nil {
		return nil, err
	}

	target := s.stringArgOrDefault(args, "path", s.defaultRoot)
	cachePath := s.stringArgOrDefault(args, "cache", s.defaultCache)

	// Get changed files via git diff.
	changed, err := gitDiffNameOnly(target, base)
	if err != nil {
		return nil, err
	}
	if len(changed) == 0 {
		return reviewReport{Base: base, ChangedFiles: 0, Files: []string{}}, nil
	}

	changedSet := make(map[string]bool, len(changed))
	for _, f := range changed {
		changedSet[f] = true
	}

	idx, err := s.loadOrBuild(cachePath, target)
	if err != nil {
		return nil, err
	}
	idx = applyGeneratedFilter(idx, boolArg(args, "include_generated", false), stringArg(args, "generator"))

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

	// 2. Boundary violations for changed files.
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
			// Filter to violations involving changed packages.
			for _, v := range violations {
				for _, f := range changed {
					pkg := fileToPackage(f)
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
	// Build sub-index with only changed files.
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

	// 4. Blast radius via impact analysis.
	impactResult, impactErr := impact.Analyze(idx, impact.Options{
		DiffRef:  base,
		Root:     target,
		MaxDepth: 5,
	})
	if impactErr == nil && impactResult != nil {
		report.BlastRadius = impactResult.TotalAffected
	}

	return report, nil
}

func gitDiffNameOnly(repoDir, base string) ([]string, error) {
	cmd := exec.Command("git", "-C", repoDir, "diff", "--name-only", base)
	out, err := cmd.Output()
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

func fileToPackage(path string) string {
	parts := strings.Split(path, "/")
	if len(parts) <= 1 {
		return "."
	}
	return strings.Join(parts[:len(parts)-1], "/")
}
