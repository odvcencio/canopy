package mcp

import (
	"fmt"

	"github.com/odvcencio/canopy/pkg/similarity"
)

func (s *Service) callSimilarity(args map[string]any) (any, error) {
	pathA := s.stringArgOrDefault(args, "path_a", s.defaultRoot)
	pathB := s.stringArgOrDefault(args, "path_b", "")
	cacheA := s.stringArgOrDefault(args, "cache_a", s.defaultCache)
	cacheB := stringArg(args, "cache_b")
	threshold := floatArg(args, "threshold", 0.7)

	includeGenerated := boolArg(args, "include_generated", false)
	generator := stringArg(args, "generator")

	idxA, err := s.loadOrBuild(cacheA, pathA)
	if err != nil {
		return nil, err
	}
	idxA = applyGeneratedFilter(idxA, includeGenerated, generator)

	idxB := idxA
	rootB := pathA
	if pathB != "" || cacheB != "" {
		idxB, err = s.loadOrBuild(cacheB, pathB)
		if err != nil {
			return nil, err
		}
		idxB = applyGeneratedFilter(idxB, includeGenerated, generator)
		rootB = pathB
	}

	top := intArg(args, "top", 100)
	maxFuncs := intArg(args, "max_funcs", 2000)
	pairs, err := similarity.Compare(idxA, idxB, pathA, rootB, threshold, top, maxFuncs)
	if err != nil {
		return nil, err
	}

	type pairResult struct {
		FileA     string  `json:"file_a"`
		NameA     string  `json:"name_a"`
		LinesA    string  `json:"lines_a"`
		FileB     string  `json:"file_b"`
		NameB     string  `json:"name_b"`
		LinesB    string  `json:"lines_b"`
		Score     float64 `json:"score"`
		Method    string  `json:"method"`
	}

	results := make([]pairResult, 0, len(pairs))
	for _, p := range pairs {
		results = append(results, pairResult{
			FileA:  p.A.File,
			NameA:  p.A.Name,
			LinesA: formatLines(p.A.StartLine, p.A.EndLine),
			FileB:  p.B.File,
			NameB:  p.B.Name,
			LinesB: formatLines(p.B.StartLine, p.B.EndLine),
			Score:  p.Score,
			Method: p.Method,
		})
	}

	return map[string]any{
		"threshold": threshold,
		"count":     len(results),
		"pairs":     results,
	}, nil
}

func formatLines(start, end int) string {
	if start == end {
		return fmt.Sprintf("%d", start)
	}
	return fmt.Sprintf("%d-%d", start, end)
}
