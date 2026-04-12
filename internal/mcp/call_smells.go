package mcp

import (
	"strings"

	"github.com/odvcencio/canopy/pkg/complexity"
	"github.com/odvcencio/canopy/pkg/coupling"
	"github.com/odvcencio/canopy/pkg/smells"
	"github.com/odvcencio/canopy/pkg/typemetrics"
	"github.com/odvcencio/canopy/pkg/xref"
)

func (s *Service) callSmells(args map[string]any) (any, error) {
	target := s.stringArgOrDefault(args, "path", s.defaultRoot)
	cachePath := s.stringArgOrDefault(args, "cache", s.defaultCache)
	idFilter := stringArg(args, "id")
	severity := stringArg(args, "severity")
	top := intArg(args, "top", 0)

	idx, err := s.loadOrBuild(cachePath, target)
	if err != nil {
		return nil, err
	}
	idx = applyGeneratedFilter(idx, boolArg(args, "include_generated", false), stringArg(args, "generator"))

	graph, err := xref.Build(idx)
	if err != nil {
		return nil, err
	}

	compReport, compErr := complexity.Analyze(idx, idx.Root, complexity.Options{})
	if compErr == nil {
		complexity.EnrichWithXref(compReport, graph)
	}

	couplingReport, couplingErr := coupling.Analyze(idx, graph)

	typeReport, typeErr := typemetrics.Analyze(idx, idx.Root, graph)

	input := smells.Input{
		Index:     idx,
		XrefGraph: graph,
	}
	if compErr == nil {
		input.Complexity = compReport
	}
	if couplingErr == nil {
		input.Coupling = couplingReport
	}
	if typeErr == nil {
		input.Types = typeReport
	}

	report := smells.Detect(input)

	// Apply --id filter.
	if idFilter != "" {
		ids := map[string]bool{}
		for _, id := range strings.Split(idFilter, ",") {
			id = strings.TrimSpace(id)
			if id != "" {
				ids[id] = true
			}
		}
		filtered := report.Smells[:0]
		for _, sm := range report.Smells {
			if ids[sm.ID] {
				filtered = append(filtered, sm)
			}
		}
		report.Smells = filtered
		report.Summary = smells.RecomputeSummary(report.Smells)
	}

	// Apply severity filter.
	if severity != "" {
		sev := strings.ToLower(strings.TrimSpace(severity))
		filtered := report.Smells[:0]
		for _, sm := range report.Smells {
			if sm.Severity == sev {
				filtered = append(filtered, sm)
			}
		}
		report.Smells = filtered
		report.Summary = smells.RecomputeSummary(report.Smells)
	}

	// Apply top limit.
	if top > 0 && len(report.Smells) > top {
		report.Smells = report.Smells[:top]
		report.Summary = smells.RecomputeSummary(report.Smells)
	}

	return report, nil
}
