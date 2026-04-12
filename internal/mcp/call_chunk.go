package mcp

import (
	"fmt"
	"strings"

	"github.com/odvcencio/canopy/internal/chunk"
)

func (s *Service) callChunk(args map[string]any) (any, error) {
	target := s.stringArgOrDefault(args, "path", s.defaultRoot)
	cachePath := s.stringArgOrDefault(args, "cache", s.defaultCache)
	tokens := intArg(args, "tokens", 800)
	if tokens <= 0 {
		return nil, fmt.Errorf("tokens must be > 0")
	}

	idx, err := s.loadOrBuild(cachePath, target)
	if err != nil {
		return nil, err
	}
	idx = applyGeneratedFilter(idx, boolArg(args, "include_generated", false), stringArg(args, "generator"))
	filterPath := ""
	if strings.TrimSpace(cachePath) != "" && strings.TrimSpace(target) != "" {
		filterPath = target
	}
	report, err := chunk.Build(idx, chunk.Options{
		TokenBudget: tokens,
		FilterPath:  filterPath,
	})
	if err != nil {
		return nil, err
	}
	return report, nil
}
