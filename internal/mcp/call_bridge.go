package mcp

import (
	"fmt"

	"github.com/odvcencio/canopy/internal/bridge"
)

func (s *Service) callBridge(args map[string]any) (any, error) {
	target := s.stringArgOrDefault(args, "path", s.defaultRoot)
	cachePath := s.stringArgOrDefault(args, "cache", s.defaultCache)
	top := intArg(args, "top", 20)
	depth := intArg(args, "depth", 1)
	if top <= 0 {
		return nil, fmt.Errorf("top must be > 0")
	}
	if depth <= 0 {
		return nil, fmt.Errorf("depth must be > 0")
	}

	idx, err := s.loadOrBuild(cachePath, target)
	if err != nil {
		return nil, err
	}
	idx = applyGeneratedFilter(idx, boolArg(args, "include_generated", false), stringArg(args, "generator"))
	report, err := bridge.Build(idx, bridge.Options{
		Top:     top,
		Focus:   stringArg(args, "focus"),
		Depth:   depth,
		Reverse: boolArg(args, "reverse", false),
	})
	if err != nil {
		return nil, err
	}
	return report, nil
}
