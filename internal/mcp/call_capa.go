package mcp

import (
	"sort"
	"strings"

	"github.com/odvcencio/canopy/pkg/capa"
)

func (s *Service) callCapa(args map[string]any) (any, error) {
	target := s.stringArgOrDefault(args, "path", s.defaultRoot)
	cachePath := s.stringArgOrDefault(args, "cache", s.defaultCache)
	category := strings.ToLower(strings.TrimSpace(stringArg(args, "category")))
	minConfidence := strings.ToLower(strings.TrimSpace(stringArg(args, "min_confidence")))

	idx, err := s.loadOrBuild(cachePath, target)
	if err != nil {
		return nil, err
	}
	idx = applyGeneratedFilter(idx, boolArg(args, "include_generated", false), stringArg(args, "generator"))

	rules := capa.BuiltinRules()

	if category != "" {
		var filtered []capa.Rule
		for _, r := range rules {
			if r.Category == category {
				filtered = append(filtered, r)
			}
		}
		rules = filtered
	}

	if minConfidence != "" {
		minLevel := confidenceLevel(minConfidence)
		var filtered []capa.Rule
		for _, r := range rules {
			if confidenceLevel(r.Confidence) >= minLevel {
				filtered = append(filtered, r)
			}
		}
		rules = filtered
	}

	matches := capa.Detect(idx, rules)

	sort.Slice(matches, func(i, j int) bool {
		return matches[i].Rule.Name < matches[j].Rule.Name
	})

	type capaResult struct {
		Name        string   `json:"name"`
		AttackID    string   `json:"attack_id"`
		Category    string   `json:"category"`
		Confidence  string   `json:"confidence"`
		Description string   `json:"description"`
		MatchedAPIs []string `json:"matched_apis"`
		Files       []string `json:"files"`
		Functions   []string `json:"functions"`
	}

	results := make([]capaResult, 0, len(matches))
	for _, m := range matches {
		results = append(results, capaResult{
			Name:        m.Rule.Name,
			AttackID:    m.Rule.AttackID,
			Category:    m.Rule.Category,
			Confidence:  m.Rule.Confidence,
			Description: m.Rule.Description,
			MatchedAPIs: m.MatchedAPIs,
			Files:       m.Files,
			Functions:   m.Functions,
		})
	}

	return map[string]any{
		"count":   len(results),
		"matches": results,
	}, nil
}

func confidenceLevel(c string) int {
	switch c {
	case "high":
		return 3
	case "medium":
		return 2
	case "low":
		return 1
	default:
		return 0
	}
}
