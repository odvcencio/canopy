package mcp

import (
	"github.com/odvcencio/canopy/internal/files"
)

func (s *Service) callFiles(args map[string]any) (any, error) {
	target := s.stringArgOrDefault(args, "path", s.defaultRoot)
	cachePath := s.stringArgOrDefault(args, "cache", s.defaultCache)

	idx, err := s.loadOrBuild(cachePath, target)
	if err != nil {
		return nil, err
	}
	idx = applyGeneratedFilter(idx, boolArg(args, "include_generated", false), stringArg(args, "generator"))
	report, err := files.Build(idx, files.Options{
		Language:   stringArg(args, "language"),
		MinSymbols: intArg(args, "min_symbols", 0),
		SortBy:     s.stringArgOrDefault(args, "sort", "symbols"),
		Top:        intArg(args, "top", 50),
	})
	if err != nil {
		return nil, err
	}
	return report, nil
}
