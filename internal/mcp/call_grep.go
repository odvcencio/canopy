package mcp

import (
	"sort"

	"github.com/odvcencio/canopy/pkg/query"
)

func (s *Service) callGrep(args map[string]any) (any, error) {
	selectorRaw, err := requiredStringArg(args, "selector")
	if err != nil {
		return nil, err
	}
	target := s.stringArgOrDefault(args, "path", s.defaultRoot)
	cachePath := s.stringArgOrDefault(args, "cache", s.defaultCache)

	idx, err := s.loadOrBuild(cachePath, target)
	if err != nil {
		return nil, err
	}
	idx = applyGeneratedFilter(idx, boolArg(args, "include_generated", false), stringArg(args, "generator"))

	selector, err := query.ParseSelector(selectorRaw)
	if err != nil {
		return nil, err
	}

	type grepMatch struct {
		File      string `json:"file"`
		Kind      string `json:"kind"`
		Name      string `json:"name"`
		Signature string `json:"signature,omitempty"`
		StartLine int    `json:"start_line"`
		EndLine   int    `json:"end_line"`
	}

	matches := make([]grepMatch, 0, idx.SymbolCount())
	for _, file := range idx.Files {
		for _, symbol := range file.Symbols {
			if !selector.Match(symbol) {
				continue
			}
			matches = append(matches, grepMatch{
				File:      file.Path,
				Kind:      symbol.Kind,
				Name:      symbol.Name,
				Signature: symbol.Signature,
				StartLine: symbol.StartLine,
				EndLine:   symbol.EndLine,
			})
		}
	}

	sort.Slice(matches, func(i, j int) bool {
		if matches[i].File == matches[j].File {
			if matches[i].StartLine == matches[j].StartLine {
				return matches[i].Name < matches[j].Name
			}
			return matches[i].StartLine < matches[j].StartLine
		}
		return matches[i].File < matches[j].File
	})

	return map[string]any{
		"matches": matches,
		"count":   len(matches),
	}, nil
}
