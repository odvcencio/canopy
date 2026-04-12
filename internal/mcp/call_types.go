package mcp

import (
	"github.com/odvcencio/canopy/pkg/typemetrics"
	"github.com/odvcencio/canopy/pkg/xref"
)

func (s *Service) callTypeMetrics(args map[string]any) (any, error) {
	target := s.stringArgOrDefault(args, "path", s.defaultRoot)
	cachePath := s.stringArgOrDefault(args, "cache", s.defaultCache)
	sortField := s.stringArgOrDefault(args, "sort", "fields")
	top := intArg(args, "top", 0)
	minFields := intArg(args, "min_fields", 0)

	idx, err := s.loadOrBuild(cachePath, target)
	if err != nil {
		return nil, err
	}
	idx = applyGeneratedFilter(idx, boolArg(args, "include_generated", false), stringArg(args, "generator"))

	graph, err := xref.Build(idx)
	if err != nil {
		return nil, err
	}

	report, err := typemetrics.Analyze(idx, idx.Root, graph)
	if err != nil {
		return nil, err
	}

	_ = sortField
	_ = top
	_ = minFields

	return report, nil
}
