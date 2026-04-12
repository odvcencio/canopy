package mcp

import (
	"sort"

	"github.com/odvcencio/canopy/pkg/model"
)

func (s *Service) callMap(args map[string]any) (any, error) {
	target := s.stringArgOrDefault(args, "path", s.defaultRoot)
	cachePath := s.stringArgOrDefault(args, "cache", s.defaultCache)

	idx, err := s.loadOrBuild(cachePath, target)
	if err != nil {
		return nil, err
	}
	idx = applyGeneratedFilter(idx, boolArg(args, "include_generated", false), stringArg(args, "generator"))

	type mapFileSummary struct {
		Path           string            `json:"path"`
		Language       string            `json:"language"`
		Imports        []string          `json:"imports,omitempty"`
		Symbols        []model.Symbol    `json:"symbols,omitempty"`
		References     []model.Reference `json:"references,omitempty"`
		SymbolCount    int               `json:"symbol_count"`
		ReferenceCount int               `json:"reference_count"`
	}

	files := make([]mapFileSummary, 0, len(idx.Files))
	for _, file := range idx.Files {
		files = append(files, mapFileSummary{
			Path:           file.Path,
			Language:       file.Language,
			Imports:        append([]string(nil), file.Imports...),
			Symbols:        append([]model.Symbol(nil), file.Symbols...),
			References:     append([]model.Reference(nil), file.References...),
			SymbolCount:    len(file.Symbols),
			ReferenceCount: len(file.References),
		})
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].Path < files[j].Path
	})

	return map[string]any{
		"root":            idx.Root,
		"generated_at":    idx.GeneratedAt,
		"file_count":      len(files),
		"symbol_count":    idx.SymbolCount(),
		"reference_count": idx.ReferenceCount(),
		"files":           files,
		"errors":          idx.Errors,
	}, nil
}
