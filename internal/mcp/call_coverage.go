package mcp

import (
	"fmt"

	"m31labs.dev/canopy/internal/indexcoverage"
)

func (s *Service) callCoverage(args map[string]any) (any, error) {
	target := s.stringArgOrDefault(args, "path", s.defaultRoot)
	cachePath := s.stringArgOrDefault(args, "cache", s.defaultCache)
	limit := intArg(args, "limit", 100)
	if limit <= 0 {
		return nil, fmt.Errorf("limit must be > 0")
	}

	idx, err := s.loadOrBuild(cachePath, target)
	if err != nil {
		return nil, err
	}
	if generator := stringArg(args, "generator"); generator != "" {
		idx = idx.FilterByGenerator(generator)
	}
	return indexcoverage.Build(idx, indexcoverage.Options{
		IncludeClean:     boolArg(args, "include_clean", false),
		IncludeGenerated: boolArg(args, "include_generated", false),
		MaxFiles:         limit,
	})
}
