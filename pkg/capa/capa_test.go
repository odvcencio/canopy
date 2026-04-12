package capa

import (
	"testing"

	"github.com/odvcencio/canopy/pkg/model"
)

func TestDetectEmpty(t *testing.T) {
	idx := &model.Index{}
	matches := Detect(idx, BuiltinRules())
	if len(matches) != 0 {
		t.Fatalf("expected 0 matches on empty index, got %d", len(matches))
	}
}

func TestDetectAnyAPIMatch(t *testing.T) {
	idx := &model.Index{
		Files: []model.FileSummary{
			{
				Path: "malware.c", Language: "c",
				Symbols: []model.Symbol{
					{Kind: "function_definition", Name: "inject", StartLine: 1, EndLine: 10},
				},
				References: []model.Reference{
					{Kind: "reference.call", Name: "VirtualAllocEx", StartLine: 3},
				},
			},
		},
	}
	rules := []Rule{
		{Name: "test", AttackID: "T1055", Category: "injection",
			AnyAPIs: []string{"VirtualAllocEx", "WriteProcessMemory"}, Confidence: "high"},
	}
	matches := Detect(idx, rules)
	if len(matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matches))
	}
	if matches[0].MatchedAPIs[0] != "VirtualAllocEx" {
		t.Fatalf("expected VirtualAllocEx matched, got %v", matches[0].MatchedAPIs)
	}
}

func TestDetectAllAPIsRequirement(t *testing.T) {
	// Only 2 of 3 required APIs present — should NOT match
	idx := &model.Index{
		Files: []model.FileSummary{
			{
				Path: "malware.c", Language: "c",
				Symbols: []model.Symbol{
					{Kind: "function_definition", Name: "inject", StartLine: 1, EndLine: 10},
				},
				References: []model.Reference{
					{Kind: "reference.call", Name: "VirtualAllocEx", StartLine: 3},
					{Kind: "reference.call", Name: "WriteProcessMemory", StartLine: 5},
				},
			},
		},
	}
	rules := []Rule{
		{Name: "full_injection", AttackID: "T1055", Category: "process_injection",
			AllAPIs: []string{"VirtualAllocEx", "WriteProcessMemory", "CreateRemoteThread"}, Confidence: "high"},
	}
	matches := Detect(idx, rules)
	if len(matches) != 0 {
		t.Fatalf("expected 0 matches (missing CreateRemoteThread), got %d", len(matches))
	}

	// Now add the third API
	idx.Files[0].References = append(idx.Files[0].References,
		model.Reference{Kind: "reference.call", Name: "CreateRemoteThread", StartLine: 7})
	matches = Detect(idx, rules)
	if len(matches) != 1 {
		t.Fatalf("expected 1 match with all 3 APIs, got %d", len(matches))
	}
}

func TestBuiltinRulesValid(t *testing.T) {
	rules := BuiltinRules()
	if len(rules) == 0 {
		t.Fatal("expected at least one builtin rule")
	}
	for _, r := range rules {
		if r.Name == "" {
			t.Fatal("rule has empty name")
		}
		if r.Category == "" {
			t.Fatal("rule has empty category")
		}
		if r.AttackID == "" {
			t.Fatal("rule has empty attack ID")
		}
		if len(r.AnyAPIs) == 0 && len(r.AllAPIs) == 0 {
			t.Fatalf("rule %q has no APIs defined", r.Name)
		}
	}
}
