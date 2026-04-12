package mcp

import (
	"github.com/odvcencio/canopy/internal/contextpack"
)

func (s *Service) callContext(args map[string]any) (any, error) {
	filePath, err := requiredStringArg(args, "file")
	if err != nil {
		return nil, err
	}

	rootPath := s.stringArgOrDefault(args, "root", s.defaultRoot)
	cachePath := s.stringArgOrDefault(args, "cache", s.defaultCache)
	line := intArg(args, "line", 1)
	tokens := intArg(args, "tokens", 800)
	semantic := boolArg(args, "semantic", false)
	semanticDepth := intArg(args, "semantic_depth", 1)

	idx, err := s.loadOrBuild(cachePath, rootPath)
	if err != nil {
		return nil, err
	}
	idx = applyGeneratedFilter(idx, boolArg(args, "include_generated", false), stringArg(args, "generator"))

	report, err := contextpack.Build(idx, contextpack.Options{
		FilePath:      filePath,
		Line:          line,
		TokenBudget:   tokens,
		Semantic:      semantic,
		SemanticDepth: semanticDepth,
	})
	if err != nil {
		return nil, err
	}
	return report, nil
}
