package mcp

import (
	"fmt"

	"github.com/odvcencio/canopy/pkg/complexity"
	"github.com/odvcencio/canopy/pkg/model"
	"github.com/odvcencio/canopy/pkg/risk"
	"github.com/odvcencio/canopy/pkg/testmap"
	"github.com/odvcencio/canopy/pkg/xref"
)

func (s *Service) callRisk(args map[string]any) (any, error) {
	target := s.stringArgOrDefault(args, "path", s.defaultRoot)
	cachePath := s.stringArgOrDefault(args, "cache", s.defaultCache)
	top := intArg(args, "top", 20)
	minRisk := floatArg(args, "min_risk", 0)
	since := s.stringArgOrDefault(args, "since", "90d")
	byPackage := boolArg(args, "by_package", false)

	idx, err := s.loadOrBuild(cachePath, target)
	if err != nil {
		return nil, err
	}
	idx = applyGeneratedFilter(idx, boolArg(args, "include_generated", false), stringArg(args, "generator"))

	// Build xref graph.
	graph, err := xref.Build(idx)
	if err != nil {
		return nil, fmt.Errorf("xref build: %w", err)
	}

	// Run complexity analysis and enrich with xref.
	compReport, err := complexity.Analyze(idx, idx.Root, complexity.Options{})
	if err != nil {
		return nil, fmt.Errorf("complexity analysis: %w", err)
	}
	complexity.EnrichWithXref(compReport, graph)

	// Build test coverage map.
	testMapLookup := mcpBuildTestMapLookup(idx)

	// Run risk analysis.
	report, err := risk.Analyze(risk.Input{
		Index:      idx,
		Root:       target,
		Complexity: compReport,
		XrefGraph:  graph,
		TestMap:    testMapLookup,
		Since:      since,
	})
	if err != nil {
		return nil, fmt.Errorf("risk analysis: %w", err)
	}

	// Apply --min-risk filter.
	if minRisk > 0 {
		filtered := report.Functions[:0]
		for _, fn := range report.Functions {
			if fn.Risk >= minRisk {
				filtered = append(filtered, fn)
			}
		}
		report.Functions = filtered
	}

	// Apply --top limit.
	if top > 0 {
		if len(report.Functions) > top {
			report.Functions = report.Functions[:top]
		}
		if len(report.Packages) > top {
			report.Packages = report.Packages[:top]
		}
	}

	if byPackage {
		return struct {
			Packages []risk.PackageRisk `json:"packages"`
			Summary  risk.RiskSummary   `json:"summary"`
		}{
			Packages: report.Packages,
			Summary:  report.Summary,
		}, nil
	}

	return report, nil
}

// mcpBuildTestMapLookup constructs a test coverage lookup map from testmap analysis.
// Returns nil if testmap analysis fails (untested signal defaults to 1.0 for all functions).
func mcpBuildTestMapLookup(idx *model.Index) map[string]bool {
	tmReport, err := testmap.Map(idx, testmap.Options{})
	if err != nil || tmReport == nil {
		return nil
	}
	lookup := make(map[string]bool, len(tmReport.Mappings))
	for _, m := range tmReport.Mappings {
		if m.Coverage == "tested" || m.Coverage == "indirectly_tested" {
			key := fmt.Sprintf("%s\x00%s\x00%d", m.File, m.Symbol, m.StartLine)
			lookup[key] = true
		}
	}
	return lookup
}
