package reachability

import (
	"testing"

	"github.com/odvcencio/canopy/pkg/capa"
	"github.com/odvcencio/canopy/pkg/model"
	"github.com/odvcencio/canopy/pkg/xref"
)

// buildTestIndex creates a minimal model.Index with the given files.
func buildTestIndex(files []model.FileSummary) *model.Index {
	return &model.Index{
		Version: "test",
		Root:    "/project",
		Files:   files,
	}
}

func TestAnalyze_NilIndex(t *testing.T) {
	_, err := Analyze(nil, "pkg/handler", Options{})
	if err == nil {
		t.Fatal("expected error for nil index")
	}
}

func TestAnalyze_EmptyPackage(t *testing.T) {
	idx := buildTestIndex(nil)
	_, err := Analyze(idx, "", Options{})
	if err == nil {
		t.Fatal("expected error for empty package")
	}
}

func TestAnalyze_NoRoots(t *testing.T) {
	idx := buildTestIndex([]model.FileSummary{
		{
			Path: "other/main.go",
			Symbols: []model.Symbol{
				{Kind: "function_definition", Name: "main", StartLine: 1, EndLine: 10},
			},
		},
	})
	result, err := Analyze(idx, "pkg/handler", Options{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Findings) != 0 {
		t.Fatalf("expected 0 findings, got %d", len(result.Findings))
	}
}

func TestAnalyze_DirectCapabilityCall(t *testing.T) {
	// handler.Process -> calls Command (process execution capability)
	idx := buildTestIndex([]model.FileSummary{
		{
			Path: "pkg/handler/handler.go",
			Symbols: []model.Symbol{
				{Kind: "function_definition", Name: "Process", StartLine: 1, EndLine: 10},
			},
			References: []model.Reference{
				{Kind: "reference.call", Name: "Command", StartLine: 3, StartColumn: 5, EndLine: 3, EndColumn: 12},
			},
		},
		{
			Path: "pkg/exec/exec.go",
			Symbols: []model.Symbol{
				{Kind: "function_definition", Name: "Command", StartLine: 1, EndLine: 5},
			},
		},
	})

	result, err := Analyze(idx, "pkg/handler", Options{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Package != "pkg/handler" {
		t.Fatalf("expected package %q, got %q", "pkg/handler", result.Package)
	}

	found := false
	for _, f := range result.Findings {
		if f.Category == "process_execution" {
			found = true
			if len(f.ReachPath) < 2 {
				t.Fatalf("expected at least 2 hops in reach path, got %d", len(f.ReachPath))
			}
			if f.ReachPath[0].Function != "Process" {
				t.Fatalf("expected root function Process, got %s", f.ReachPath[0].Function)
			}
			if f.ReachPath[len(f.ReachPath)-1].Function != "Command" {
				t.Fatalf("expected terminal function Command, got %s", f.ReachPath[len(f.ReachPath)-1].Function)
			}
		}
	}
	if !found {
		t.Fatal("expected process_execution finding")
	}
}

func TestAnalyze_TransitiveReach(t *testing.T) {
	// handler.Fetch -> helper.DoRequest -> callee Get (network)
	idx := buildTestIndex([]model.FileSummary{
		{
			Path: "pkg/handler/handler.go",
			Symbols: []model.Symbol{
				{Kind: "function_definition", Name: "Fetch", StartLine: 1, EndLine: 10},
			},
			References: []model.Reference{
				{Kind: "reference.call", Name: "DoRequest", StartLine: 3, StartColumn: 5, EndLine: 3, EndColumn: 14},
			},
		},
		{
			Path: "pkg/helper/helper.go",
			Symbols: []model.Symbol{
				{Kind: "function_definition", Name: "DoRequest", StartLine: 1, EndLine: 10},
			},
			References: []model.Reference{
				{Kind: "reference.call", Name: "Get", StartLine: 5, StartColumn: 5, EndLine: 5, EndColumn: 8},
			},
		},
		{
			Path: "pkg/netclient/client.go",
			Symbols: []model.Symbol{
				{Kind: "function_definition", Name: "Get", StartLine: 1, EndLine: 5},
			},
		},
	})

	result, err := Analyze(idx, "pkg/handler", Options{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	found := false
	for _, f := range result.Findings {
		if f.Category == "network_access" {
			found = true
			if len(f.ReachPath) < 3 {
				t.Fatalf("expected at least 3 hops (Fetch->DoRequest->Get), got %d", len(f.ReachPath))
			}
		}
	}
	if !found {
		t.Fatal("expected network_access finding from transitive call")
	}
}

func TestAnalyze_CategoryFilter(t *testing.T) {
	idx := buildTestIndex([]model.FileSummary{
		{
			Path: "pkg/handler/handler.go",
			Symbols: []model.Symbol{
				{Kind: "function_definition", Name: "Process", StartLine: 1, EndLine: 20},
			},
			References: []model.Reference{
				{Kind: "reference.call", Name: "Command", StartLine: 3, StartColumn: 5, EndLine: 3, EndColumn: 12},
				{Kind: "reference.call", Name: "Get", StartLine: 8, StartColumn: 5, EndLine: 8, EndColumn: 8},
			},
		},
		{
			Path: "pkg/exec/exec.go",
			Symbols: []model.Symbol{
				{Kind: "function_definition", Name: "Command", StartLine: 1, EndLine: 5},
			},
		},
		{
			Path: "pkg/net/net.go",
			Symbols: []model.Symbol{
				{Kind: "function_definition", Name: "Get", StartLine: 1, EndLine: 5},
			},
		},
	})

	// Filter to only process_execution
	result, err := Analyze(idx, "pkg/handler", Options{Category: "process_execution"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, f := range result.Findings {
		if f.Category != "process_execution" {
			t.Fatalf("expected only process_execution findings, got %s", f.Category)
		}
	}
	if len(result.Findings) == 0 {
		t.Fatal("expected at least one process_execution finding")
	}
}

func TestAnalyze_AttackIDFilter(t *testing.T) {
	idx := buildTestIndex([]model.FileSummary{
		{
			Path: "pkg/handler/handler.go",
			Symbols: []model.Symbol{
				{Kind: "function_definition", Name: "Process", StartLine: 1, EndLine: 20},
			},
			References: []model.Reference{
				{Kind: "reference.call", Name: "Command", StartLine: 3, StartColumn: 5, EndLine: 3, EndColumn: 12},
			},
		},
		{
			Path: "pkg/exec/exec.go",
			Symbols: []model.Symbol{
				{Kind: "function_definition", Name: "Command", StartLine: 1, EndLine: 5},
			},
		},
	})

	result, err := Analyze(idx, "pkg/handler", Options{AttackID: "T1059"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, f := range result.Findings {
		if f.AttackID != "T1059" {
			t.Fatalf("expected only T1059 findings, got %s", f.AttackID)
		}
	}
	if len(result.Findings) == 0 {
		t.Fatal("expected at least one T1059 finding")
	}
}

func TestAnalyze_DepthLimit(t *testing.T) {
	// Chain: A -> B -> C -> Command
	// With depth=2 we should NOT reach Command (A at depth 0, B at depth 1, C at depth 2 -- C's edges not explored)
	idx := buildTestIndex([]model.FileSummary{
		{
			Path: "pkg/a/a.go",
			Symbols: []model.Symbol{
				{Kind: "function_definition", Name: "A", StartLine: 1, EndLine: 5},
			},
			References: []model.Reference{
				{Kind: "reference.call", Name: "B", StartLine: 2, StartColumn: 1, EndLine: 2, EndColumn: 2},
			},
		},
		{
			Path: "pkg/b/b.go",
			Symbols: []model.Symbol{
				{Kind: "function_definition", Name: "B", StartLine: 1, EndLine: 5},
			},
			References: []model.Reference{
				{Kind: "reference.call", Name: "C", StartLine: 2, StartColumn: 1, EndLine: 2, EndColumn: 2},
			},
		},
		{
			Path: "pkg/c/c.go",
			Symbols: []model.Symbol{
				{Kind: "function_definition", Name: "C", StartLine: 1, EndLine: 5},
			},
			References: []model.Reference{
				{Kind: "reference.call", Name: "Command", StartLine: 2, StartColumn: 1, EndLine: 2, EndColumn: 8},
			},
		},
		{
			Path: "pkg/exec/exec.go",
			Symbols: []model.Symbol{
				{Kind: "function_definition", Name: "Command", StartLine: 1, EndLine: 5},
			},
		},
	})

	// Depth 2: path is [A (depth 0)] -> B (depth 1) -> C (depth 2) -> Command (depth 3, exceeds limit)
	result, err := Analyze(idx, "pkg/a", Options{Depth: 2})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// With depth 2, paths longer than 2 hops from root should be truncated.
	// The path would be A->B->C->Command = 4 elements (path len > maxDepth).
	// Our BFS checks len(path) > maxDepth, so at depth=2, path [A] is len 1, [A,B] is len 2, [A,B,C] is len 3 > 2, stop.
	for _, f := range result.Findings {
		if f.Category == "process_execution" {
			t.Fatal("should not reach Command with depth=2")
		}
	}

	// Depth 4: should find it
	result, err = Analyze(idx, "pkg/a", Options{Depth: 4})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	found := false
	for _, f := range result.Findings {
		if f.Category == "process_execution" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected process_execution finding with depth=4")
	}
}

func TestPackageRoots(t *testing.T) {
	g := &xref.Graph{
		Definitions: []xref.Definition{
			{ID: "1", File: "pkg/handler/handler.go", Package: "pkg/handler", Kind: "function_definition", Name: "Process", Callable: true},
			{ID: "2", File: "pkg/other/other.go", Package: "pkg/other", Kind: "function_definition", Name: "Other", Callable: true},
			{ID: "3", File: "pkg/handler/util.go", Package: "pkg/handler", Kind: "type_definition", Name: "Config", Callable: false},
		},
	}

	roots := packageRoots(g, "pkg/handler")
	if len(roots) != 1 {
		t.Fatalf("expected 1 root, got %d", len(roots))
	}
	if roots[0].Name != "Process" {
		t.Fatalf("expected Process root, got %s", roots[0].Name)
	}
}

func TestBuildAPILookup(t *testing.T) {
	rules := []capa.Rule{
		{
			Name: "test", Category: "test",
			AnyAPIs: []string{"foo", "bar"},
		},
		{
			Name: "test2", Category: "test2",
			AllAPIs: []string{"baz"},
		},
	}

	lookup := buildAPILookup(rules)
	if _, ok := lookup["foo"]; !ok {
		t.Fatal("expected foo in lookup")
	}
	if _, ok := lookup["bar"]; !ok {
		t.Fatal("expected bar in lookup")
	}
	if _, ok := lookup["baz"]; !ok {
		t.Fatal("expected baz in lookup")
	}
	if _, ok := lookup["missing"]; ok {
		t.Fatal("unexpected missing in lookup")
	}
}

func TestSupplyChainRules(t *testing.T) {
	rules := supplyChainRules()
	if len(rules) == 0 {
		t.Fatal("expected at least one supply chain rule")
	}

	categories := map[string]bool{}
	for _, r := range rules {
		categories[r.Category] = true
		if r.Name == "" {
			t.Fatal("rule has empty name")
		}
		if len(r.AnyAPIs) == 0 && len(r.AllAPIs) == 0 {
			t.Fatalf("rule %q has no APIs", r.Name)
		}
	}

	for _, expected := range []string{"process_execution", "network_access", "file_access"} {
		if !categories[expected] {
			t.Fatalf("expected category %q in supply chain rules", expected)
		}
	}
}
