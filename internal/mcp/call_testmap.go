package mcp

import (
	"strings"

	"github.com/odvcencio/canopy/pkg/testmap"
)

func (s *Service) callTestmap(args map[string]any) (any, error) {
	target := s.stringArgOrDefault(args, "path", s.defaultRoot)
	cachePath := s.stringArgOrDefault(args, "cache", s.defaultCache)
	untestedOnly := boolArg(args, "untested_only", false)
	kind := strings.ToLower(strings.TrimSpace(stringArg(args, "kind")))

	idx, err := s.loadOrBuild(cachePath, target)
	if err != nil {
		return nil, err
	}
	idx = applyGeneratedFilter(idx, boolArg(args, "include_generated", false), stringArg(args, "generator"))

	report, err := testmap.Map(idx, testmap.Options{
		UntestedOnly: untestedOnly,
		Kind:         kind,
	})
	if err != nil {
		return nil, err
	}

	return report, nil
}
