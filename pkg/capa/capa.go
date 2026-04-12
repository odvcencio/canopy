package capa

import "github.com/odvcencio/canopy/pkg/model"

// Rule defines a capability detection pattern.
type Rule struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	AttackID    string   `json:"attack_id"`
	Category    string   `json:"category"`
	AnyAPIs     []string `json:"any_apis,omitempty"`
	AllAPIs     []string `json:"all_apis,omitempty"`
	Confidence  string   `json:"confidence"`
}

// Match represents a detected capability.
type Match struct {
	Rule        Rule     `json:"rule"`
	MatchedAPIs []string `json:"matched_apis"`
	Functions   []string `json:"functions"`
	Files       []string `json:"files"`
}

type apiLocation struct {
	file     string
	function string
}

// Detect runs capability detection against an index using the provided rules.
func Detect(idx *model.Index, rules []Rule) []Match {
	apiUsage := make(map[string][]apiLocation)

	for _, f := range idx.Files {
		for _, ref := range f.References {
			if ref.Kind != "reference.call" {
				continue
			}
			// Find enclosing function
			enclosing := ""
			for _, sym := range f.Symbols {
				if sym.StartLine <= ref.StartLine && ref.StartLine <= sym.EndLine {
					enclosing = sym.Name
					break
				}
			}
			if enclosing == "" && len(f.Symbols) > 0 {
				enclosing = f.Symbols[0].Name
			}
			apiUsage[ref.Name] = append(apiUsage[ref.Name], apiLocation{file: f.Path, function: enclosing})
		}
	}

	var matches []Match
	for _, rule := range rules {
		m := matchRule(rule, apiUsage)
		if m != nil {
			matches = append(matches, *m)
		}
	}
	return matches
}

func matchRule(rule Rule, apiUsage map[string][]apiLocation) *Match {
	fileSet := make(map[string]bool)
	funcSet := make(map[string]bool)
	var matched []string

	if len(rule.AllAPIs) > 0 {
		// All APIs must be present
		for _, api := range rule.AllAPIs {
			if _, ok := apiUsage[api]; !ok {
				return nil
			}
		}
		for _, api := range rule.AllAPIs {
			matched = append(matched, api)
			for _, loc := range apiUsage[api] {
				fileSet[loc.file] = true
				funcSet[loc.function] = true
			}
		}
	} else if len(rule.AnyAPIs) > 0 {
		// Any API match
		for _, api := range rule.AnyAPIs {
			if locs, ok := apiUsage[api]; ok {
				matched = append(matched, api)
				for _, loc := range locs {
					fileSet[loc.file] = true
					funcSet[loc.function] = true
				}
			}
		}
		if len(matched) == 0 {
			return nil
		}
	} else {
		return nil
	}

	files := make([]string, 0, len(fileSet))
	for f := range fileSet {
		files = append(files, f)
	}
	funcs := make([]string, 0, len(funcSet))
	for f := range funcSet {
		funcs = append(funcs, f)
	}

	return &Match{
		Rule:        rule,
		MatchedAPIs: matched,
		Functions:   funcs,
		Files:       files,
	}
}
