package mcp

import (
	"github.com/odvcencio/canopy/pkg/impact"
)

func (s *Service) callImpact(args map[string]any) (any, error) {
	target := s.stringArgOrDefault(args, "path", s.defaultRoot)
	cachePath := s.stringArgOrDefault(args, "cache", s.defaultCache)
	diffRef := stringArg(args, "diff_ref")
	maxDepth := intArg(args, "max_depth", 10)
	changed := stringSliceArg(args, "changed")

	idx, err := s.loadOrBuild(cachePath, target)
	if err != nil {
		return nil, err
	}
	idx = applyGeneratedFilter(idx, boolArg(args, "include_generated", false), stringArg(args, "generator"))

	opts := impact.Options{
		Changed:  changed,
		DiffRef:  diffRef,
		Root:     target,
		MaxDepth: maxDepth,
	}

	result, err := impact.Analyze(idx, opts)
	if err != nil {
		return nil, err
	}

	return result, nil
}
