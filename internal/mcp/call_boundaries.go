package mcp

import (
	"fmt"

	"github.com/odvcencio/gts-suite/internal/deps"
	"github.com/odvcencio/gts-suite/pkg/boundaries"
)

func (s *Service) callBoundaries(args map[string]any) (any, error) {
	target := s.stringArgOrDefault(args, "path", s.defaultRoot)
	cachePath := s.stringArgOrDefault(args, "cache", s.defaultCache)

	cfg, err := boundaries.LoadConfig(target)
	if err != nil {
		return nil, fmt.Errorf("loading .gtsboundaries: %w", err)
	}
	if cfg == nil || len(cfg.Rules) == 0 {
		return map[string]any{"status": "SKIP", "violations": 0, "message": "no .gtsboundaries found"}, nil
	}

	idx, err := s.loadOrBuild(cachePath, target)
	if err != nil {
		return nil, err
	}
	idx = applyGeneratedFilter(idx, boolArg(args, "include_generated", false), stringArg(args, "generator"))

	report, err := deps.Build(idx, deps.Options{Mode: "package", IncludeEdges: true})
	if err != nil {
		return nil, fmt.Errorf("building dependency graph: %w", err)
	}

	importEdges := make([]boundaries.ImportEdge, 0, len(report.Edges))
	for _, edge := range report.Edges {
		if edge.Internal {
			importEdges = append(importEdges, boundaries.ImportEdge{From: edge.From, To: edge.To})
		}
	}

	violations := boundaries.Evaluate(cfg, importEdges)

	status := "PASS"
	if len(violations) > 0 {
		status = "FAIL"
	}
	return map[string]any{
		"status":     status,
		"violations": len(violations),
		"details":    violations,
	}, nil
}
