package mcp

import (
	"context"

	"m31labs.dev/canopy/internal/contextpack"
	"m31labs.dev/canopy/internal/indexcoverage"
	"m31labs.dev/canopy/pkg/contextbundle"
)

// bundleResponse is the MCP-facing shape of a contextbundle.Result:
// Result.Content is JSON-tagged "-" (it is measured and hashed as raw
// bytes, not re-encoded), so the MCP transport needs its own envelope.
type bundleResponse struct {
	Content      string                 `json:"content"`
	Receipt      contextbundle.Receipt  `json:"receipt"`
	Manifest     contextbundle.Manifest `json:"manifest"`
	ParserHealth indexcoverage.Health   `json:"parser_health"`
}

func (s *Service) callContext(args map[string]any) (any, error) {
	rootPath := s.stringArgOrDefault(args, "root", s.defaultRoot)
	cachePath := s.stringArgOrDefault(args, "cache", s.defaultCache)
	includeGenerated := boolArg(args, "include_generated", false)
	generator := stringArg(args, "generator")
	tokens := intArg(args, "tokens", 800)

	idx, err := s.loadOrBuild(cachePath, rootPath)
	if err != nil {
		return nil, err
	}

	// Additive bundle-mode inputs (spec 10.13): task, mode, selectors, and
	// previous_receipts. Old file/line/semantic/semantic_depth inputs keep
	// mapping to the legacy compatibility request below.
	opts := contextpack.BundleOptions{
		Mode:             contextbundle.TaskKind(stringArg(args, "mode")),
		Task:             stringArg(args, "task"),
		Tokens:           tokens,
		Focus:            parseSelectorsArg(args, "selectors"),
		IncludeGenerated: includeGenerated,
		PreviousReceipts: parseReceiptRefsArg(args, "previous_receipts"),
	}

	if !contextpack.UseLegacy() && opts.WantsBundle() {
		result, err := contextpack.BuildBundle(context.Background(), idx, rootPath, opts)
		if err != nil {
			return nil, err
		}
		parserHealth, err := indexcoverage.BuildHealthForPaths(idx, manifestPaths(result.Manifest), 5)
		if err != nil {
			return nil, err
		}
		return bundleResponse{
			Content:      string(result.Content),
			Receipt:      result.Receipt,
			Manifest:     result.Manifest,
			ParserHealth: parserHealth,
		}, nil
	}

	// Legacy path — unchanged behavior for existing callers.
	filePath, err := requiredStringArg(args, "file")
	if err != nil {
		return nil, err
	}
	line := intArg(args, "line", 1)
	semantic := boolArg(args, "semantic", false)
	semanticDepth := intArg(args, "semantic_depth", 1)

	idx = applyGeneratedFilter(idx, includeGenerated, generator)

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

func manifestPaths(manifest contextbundle.Manifest) []string {
	paths := make([]string, 0, len(manifest.Items))
	for _, item := range manifest.Items {
		paths = append(paths, item.Path)
	}
	return paths
}

func parseSelectorsArg(args map[string]any, key string) []contextbundle.Selector {
	raw, ok := args[key]
	if !ok || raw == nil {
		return nil
	}
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]contextbundle.Selector, 0, len(items))
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		out = append(out, contextbundle.Selector{
			File:      stringArg(m, "file"),
			Symbol:    stringArg(m, "symbol"),
			Kind:      stringArg(m, "kind"),
			EntityID:  stringArg(m, "entity_id"),
			Line:      intArg(m, "line", 0),
			StartLine: intArg(m, "start_line", 0),
			EndLine:   intArg(m, "end_line", 0),
			Required:  boolArg(m, "required", false),
		})
	}
	return out
}

func parseReceiptRefsArg(args map[string]any, key string) []contextbundle.ReceiptRef {
	raw, ok := args[key]
	if !ok || raw == nil {
		return nil
	}
	switch typed := raw.(type) {
	case []any:
		out := make([]contextbundle.ReceiptRef, 0, len(typed))
		for _, item := range typed {
			switch v := item.(type) {
			case string:
				if v != "" {
					out = append(out, contextbundle.ReceiptRef{ID: v})
				}
			case map[string]any:
				id := stringArg(v, "id")
				if id != "" {
					out = append(out, contextbundle.ReceiptRef{ID: id, Kind: stringArg(v, "kind")})
				}
			}
		}
		return out
	default:
		return nil
	}
}
