package mcp

import (
	"fmt"
	"sort"

	"github.com/odvcencio/canopy/internal/lint"
)

func (s *Service) callLint(args map[string]any) (any, error) {
	target := s.stringArgOrDefault(args, "path", s.defaultRoot)
	cachePath := s.stringArgOrDefault(args, "cache", s.defaultCache)
	rawRules := stringSliceArg(args, "rule")
	rawPatterns := stringSliceArg(args, "pattern")
	if len(rawRules) == 0 && len(rawPatterns) == 0 {
		return nil, fmt.Errorf("at least one rule or pattern is required")
	}

	idx, err := s.loadOrBuild(cachePath, target)
	if err != nil {
		return nil, err
	}
	idx = applyGeneratedFilter(idx, boolArg(args, "include_generated", false), stringArg(args, "generator"))

	rules := make([]lint.Rule, 0, len(rawRules))
	for _, rawRule := range rawRules {
		rule, parseErr := lint.ParseRule(rawRule)
		if parseErr != nil {
			return nil, fmt.Errorf("parse rule %q: %w", rawRule, parseErr)
		}
		rules = append(rules, rule)
	}

	patterns := make([]lint.QueryPattern, 0, len(rawPatterns))
	for _, rawPattern := range rawPatterns {
		pattern, loadErr := lint.LoadQueryPattern(rawPattern)
		if loadErr != nil {
			return nil, fmt.Errorf("load pattern %q: %w", rawPattern, loadErr)
		}
		patterns = append(patterns, pattern)
	}

	violations := lint.Evaluate(idx, rules)
	patternViolations, err := lint.EvaluatePatterns(idx, patterns)
	if err != nil {
		return nil, err
	}
	violations = append(violations, patternViolations...)
	sort.Slice(violations, func(i, j int) bool {
		if violations[i].File == violations[j].File {
			if violations[i].StartLine == violations[j].StartLine {
				if violations[i].RuleID == violations[j].RuleID {
					return violations[i].Name < violations[j].Name
				}
				return violations[i].RuleID < violations[j].RuleID
			}
			return violations[i].StartLine < violations[j].StartLine
		}
		return violations[i].File < violations[j].File
	})

	return map[string]any{
		"rules":      rules,
		"patterns":   patterns,
		"violations": violations,
		"count":      len(violations),
	}, nil
}
