package mcp

import (
	"github.com/odvcencio/canopy/internal/deps"
)

func (s *Service) callDeps(args map[string]any) (any, error) {
	target := s.stringArgOrDefault(args, "path", s.defaultRoot)
	cachePath := s.stringArgOrDefault(args, "cache", s.defaultCache)

	idx, err := s.loadOrBuild(cachePath, target)
	if err != nil {
		return nil, err
	}
	idx = applyGeneratedFilter(idx, boolArg(args, "include_generated", false), stringArg(args, "generator"))

	cyclesOnly := boolArg(args, "cycles_only", false)

	report, err := deps.Build(idx, deps.Options{
		Mode:         s.stringArgOrDefault(args, "by", "package"),
		Top:          intArg(args, "top", 10),
		Focus:        stringArg(args, "focus"),
		Depth:        intArg(args, "depth", 1),
		Reverse:      boolArg(args, "reverse", false),
		IncludeEdges: boolArg(args, "edges", false) || cyclesOnly,
	})
	if err != nil {
		return nil, err
	}

	graph := deps.GraphFromEdges(report.Edges)
	cycles := deps.DetectCycles(graph)
	if cycles == nil {
		cycles = []deps.Cycle{}
	}
	report.Cycles = cycles

	if cyclesOnly {
		return map[string]any{
			"cycles": cycles,
		}, nil
	}

	return report, nil
}
