package mcp

import (
	"github.com/odvcencio/canopy/pkg/xref"
)

func (s *Service) callCallgraph(args map[string]any) (any, error) {
	name, err := requiredStringArg(args, "name")
	if err != nil {
		return nil, err
	}
	regexMode := boolArg(args, "regex", false)
	depth := intArg(args, "depth", 2)
	reverse := boolArg(args, "reverse", false)
	target := s.stringArgOrDefault(args, "path", s.defaultRoot)
	cachePath := s.stringArgOrDefault(args, "cache", s.defaultCache)

	idx, err := s.loadOrBuild(cachePath, target)
	if err != nil {
		return nil, err
	}
	idx = applyGeneratedFilter(idx, boolArg(args, "include_generated", false), stringArg(args, "generator"))

	graph, err := xref.Build(idx)
	if err != nil {
		return nil, err
	}
	roots, err := graph.FindDefinitions(name, regexMode)
	if err != nil {
		return nil, err
	}
	rootIDs := make([]string, 0, len(roots))
	for _, root := range roots {
		rootIDs = append(rootIDs, root.ID)
	}
	walk := graph.Walk(rootIDs, depth, reverse)

	// MaterializedEdges builds full Definition copies for JSON.
	// The MCP framework serializes the return value, so we can't stream here.
	// For very large walks, consider adding a depth/edge cap.
	edges := walk.MaterializedEdges()

	return map[string]any{
		"roots":                 walk.Roots,
		"nodes":                 walk.Nodes,
		"edges":                 edges,
		"depth":                 walk.Depth,
		"reverse":               walk.Reverse,
		"unresolved_call_count": len(graph.Unresolved),
	}, nil
}
