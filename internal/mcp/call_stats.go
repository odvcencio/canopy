package mcp

import (
	"fmt"

	"github.com/odvcencio/canopy/internal/stats"
)

func (s *Service) callStats(args map[string]any) (any, error) {
	target := s.stringArgOrDefault(args, "path", s.defaultRoot)
	cachePath := s.stringArgOrDefault(args, "cache", s.defaultCache)
	top := intArg(args, "top", 10)
	if top <= 0 {
		return nil, fmt.Errorf("top must be > 0")
	}

	idx, err := s.loadOrBuild(cachePath, target)
	if err != nil {
		return nil, err
	}
	idx = applyGeneratedFilter(idx, boolArg(args, "include_generated", false), stringArg(args, "generator"))
	report, err := stats.Build(idx, stats.Options{
		TopFiles: top,
	})
	if err != nil {
		return nil, err
	}
	return report, nil
}
