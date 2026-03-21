package mcp

import (
	"github.com/odvcencio/gts-suite/pkg/xref"
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

	return map[string]any{
		"roots":                 walk.Roots,
		"nodes":                 walk.Nodes,
		"edges":                 walk.MaterializedEdges(),
		"depth":                 walk.Depth,
		"reverse":               walk.Reverse,
		"unresolved_call_count": len(graph.Unresolved),
	}, nil
}
