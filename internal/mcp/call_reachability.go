package mcp

import (
	"github.com/odvcencio/canopy/internal/reachability"
)

func (s *Service) callReachability(args map[string]any) (any, error) {
	pkg := stringArg(args, "package")
	target := s.stringArgOrDefault(args, "path", s.defaultRoot)
	cachePath := s.stringArgOrDefault(args, "cache", s.defaultCache)
	category := stringArg(args, "category")
	attackID := stringArg(args, "attack_id")
	depth := intArg(args, "depth", 10)

	idx, err := s.loadOrBuild(cachePath, target)
	if err != nil {
		return nil, err
	}
	idx = applyGeneratedFilter(idx, boolArg(args, "include_generated", false), stringArg(args, "generator"))

	opts := reachability.Options{
		Category: category,
		AttackID: attackID,
		Depth:    depth,
	}

	result, err := reachability.Analyze(idx, pkg, opts)
	if err != nil {
		return nil, err
	}

	return result, nil
}
