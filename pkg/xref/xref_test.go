package xref

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/odvcencio/gts-suite/pkg/model"
)

func TestBuildAndWalk(t *testing.T) {
	idx := &model.Index{
		Root: "/tmp/repo",
		Files: []model.FileSummary{
			{
				Path: "a.go",
				Symbols: []model.Symbol{
					{
						File:      "a.go",
						Kind:      "function_definition",
						Name:      "A",
						StartLine: 1,
						EndLine:   1,
					},
					{
						File:      "a.go",
						Kind:      "function_definition",
						Name:      "B",
						StartLine: 3,
						EndLine:   5,
					},
				},
				References: []model.Reference{
					{
						File:        "a.go",
						Kind:        "reference.call",
						Name:        "A",
						StartLine:   4,
						EndLine:     4,
						StartColumn: 2,
						EndColumn:   3,
					},
				},
			},
			{
				Path: "c.go",
				Symbols: []model.Symbol{
					{
						File:      "c.go",
						Kind:      "function_definition",
						Name:      "C",
						StartLine: 1,
						EndLine:   3,
					},
				},
				References: []model.Reference{
					{
						File:        "c.go",
						Kind:        "reference.call",
						Name:        "B",
						StartLine:   2,
						EndLine:     2,
						StartColumn: 2,
						EndColumn:   3,
					},
				},
			},
		},
	}

	graph, err := Build(idx)
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	if len(graph.Definitions) != 3 {
		t.Fatalf("expected 3 definitions, got %d", len(graph.Definitions))
	}
	if len(graph.Edges) != 2 {
		t.Fatalf("expected 2 edges, got %d", len(graph.Edges))
	}

	matches, err := graph.FindDefinitions("A", false)
	if err != nil {
		t.Fatalf("FindDefinitions returned error: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected 1 root for A, got %d", len(matches))
	}

	walk := graph.Walk([]string{matches[0].ID}, 2, true)
	if len(walk.Edges) != 2 {
		t.Fatalf("expected reverse walk to include 2 edges, got %d", len(walk.Edges))
	}
	if len(walk.Nodes) != 3 {
		t.Fatalf("expected reverse walk to include 3 nodes, got %d", len(walk.Nodes))
	}
	if graph.IncomingCount(matches[0].ID) != 1 {
		t.Fatalf("expected A incoming count to be 1, got %d", graph.IncomingCount(matches[0].ID))
	}
}

func TestBuildAmbiguousGlobalCall(t *testing.T) {
	idx := &model.Index{
		Root: "/tmp/repo",
		Files: []model.FileSummary{
			{
				Path: "x/a.go",
				Symbols: []model.Symbol{
					{
						File:      "x/a.go",
						Kind:      "function_definition",
						Name:      "Foo",
						StartLine: 1,
						EndLine:   1,
					},
				},
			},
			{
				Path: "y/b.go",
				Symbols: []model.Symbol{
					{
						File:      "y/b.go",
						Kind:      "function_definition",
						Name:      "Foo",
						StartLine: 1,
						EndLine:   1,
					},
				},
			},
			{
				Path: "z/c.go",
				Symbols: []model.Symbol{
					{
						File:      "z/c.go",
						Kind:      "function_definition",
						Name:      "Caller",
						StartLine: 1,
						EndLine:   3,
					},
				},
				References: []model.Reference{
					{
						File:        "z/c.go",
						Kind:        "reference.call",
						Name:        "Foo",
						StartLine:   2,
						EndLine:     2,
						StartColumn: 2,
						EndColumn:   5,
					},
				},
			},
		},
	}

	graph, err := Build(idx)
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	if len(graph.Edges) != 0 {
		t.Fatalf("expected 0 resolved edges, got %d", len(graph.Edges))
	}
	if len(graph.Unresolved) != 1 {
		t.Fatalf("expected 1 unresolved call, got %d", len(graph.Unresolved))
	}
	if graph.Unresolved[0].Reason != "ambiguous_global" {
		t.Fatalf("unexpected unresolved reason %q", graph.Unresolved[0].Reason)
	}
	if graph.Unresolved[0].CandidateCount != 2 {
		t.Fatalf("expected candidate count 2, got %d", graph.Unresolved[0].CandidateCount)
	}
}

func TestBuildImportAwareResolutionPrefersImportedPackage(t *testing.T) {
	idx := &model.Index{
		Root: "/tmp/repo",
		Files: []model.FileSummary{
			{
				Path: "alpha/a.go",
				Symbols: []model.Symbol{
					{File: "alpha/a.go", Kind: "function_definition", Name: "Foo", StartLine: 1, EndLine: 1},
				},
			},
			{
				Path: "beta/b.go",
				Symbols: []model.Symbol{
					{File: "beta/b.go", Kind: "function_definition", Name: "Foo", StartLine: 1, EndLine: 1},
				},
			},
			{
				Path:    "app/main.go",
				Imports: []string{"alpha"},
				Symbols: []model.Symbol{
					{File: "app/main.go", Kind: "function_definition", Name: "Caller", StartLine: 1, EndLine: 3},
				},
				References: []model.Reference{
					{File: "app/main.go", Kind: "reference.call", Name: "Foo", StartLine: 2, EndLine: 2, StartColumn: 2, EndColumn: 5},
				},
			},
		},
	}

	graph, err := Build(idx)
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	if len(graph.Edges) != 1 {
		t.Fatalf("expected 1 resolved edge, got %d", len(graph.Edges))
	}
	edge := graph.Edges[0]
	if edge.Resolution != "import" {
		t.Fatalf("expected resolution import, got %q", edge.Resolution)
	}
	if graph.EdgeCallee(edge).Package != "alpha" {
		t.Fatalf("expected callee package alpha, got %q", graph.EdgeCallee(edge).Package)
	}
}

func TestBuildImportAwareResolutionDetectsImportAmbiguity(t *testing.T) {
	idx := &model.Index{
		Root: "/tmp/repo",
		Files: []model.FileSummary{
			{
				Path: "alpha/a.go",
				Symbols: []model.Symbol{
					{File: "alpha/a.go", Kind: "function_definition", Name: "Foo", StartLine: 1, EndLine: 1},
				},
			},
			{
				Path: "beta/b.go",
				Symbols: []model.Symbol{
					{File: "beta/b.go", Kind: "function_definition", Name: "Foo", StartLine: 1, EndLine: 1},
				},
			},
			{
				Path:    "app/main.go",
				Imports: []string{"alpha", "beta"},
				Symbols: []model.Symbol{
					{File: "app/main.go", Kind: "function_definition", Name: "Caller", StartLine: 1, EndLine: 3},
				},
				References: []model.Reference{
					{File: "app/main.go", Kind: "reference.call", Name: "Foo", StartLine: 2, EndLine: 2, StartColumn: 2, EndColumn: 5},
				},
			},
		},
	}

	graph, err := Build(idx)
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	if len(graph.Edges) != 0 {
		t.Fatalf("expected 0 resolved edges, got %d", len(graph.Edges))
	}
	if len(graph.Unresolved) != 1 {
		t.Fatalf("expected 1 unresolved call, got %d", len(graph.Unresolved))
	}
	if graph.Unresolved[0].Reason != "ambiguous_import" {
		t.Fatalf("unexpected unresolved reason %q", graph.Unresolved[0].Reason)
	}
	if graph.Unresolved[0].CandidateCount != 2 {
		t.Fatalf("expected candidate count 2, got %d", graph.Unresolved[0].CandidateCount)
	}
}

func TestBuildImportAwareResolutionMatchesModuleQualifiedImport(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/repo\n"), 0o644); err != nil {
		t.Fatalf("WriteFile go.mod failed: %v", err)
	}

	idx := &model.Index{
		Root: root,
		Files: []model.FileSummary{
			{
				Path: "pkg/alpha/a.go",
				Symbols: []model.Symbol{
					{File: "pkg/alpha/a.go", Kind: "function_definition", Name: "Foo", StartLine: 1, EndLine: 1},
				},
			},
			{
				Path: "vendor/alpha/a.go",
				Symbols: []model.Symbol{
					{File: "vendor/alpha/a.go", Kind: "function_definition", Name: "Foo", StartLine: 1, EndLine: 1},
				},
			},
			{
				Path:    "app/main.go",
				Imports: []string{"example.com/repo/pkg/alpha"},
				Symbols: []model.Symbol{
					{File: "app/main.go", Kind: "function_definition", Name: "Caller", StartLine: 1, EndLine: 3},
				},
				References: []model.Reference{
					{File: "app/main.go", Kind: "reference.call", Name: "Foo", StartLine: 2, EndLine: 2, StartColumn: 2, EndColumn: 5},
				},
			},
		},
	}

	graph, err := Build(idx)
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	if len(graph.Edges) != 1 {
		t.Fatalf("expected 1 resolved edge, got %d", len(graph.Edges))
	}
	if len(graph.Unresolved) != 0 {
		t.Fatalf("expected 0 unresolved calls, got %d", len(graph.Unresolved))
	}

	edge := graph.Edges[0]
	if edge.Resolution != "import" {
		t.Fatalf("expected resolution import, got %q", edge.Resolution)
	}
	if graph.EdgeCallee(edge).Package != "pkg/alpha" {
		t.Fatalf("expected callee package pkg/alpha, got %q", graph.EdgeCallee(edge).Package)
	}
}

func TestBuildImportAwareResolutionAvoidsExternalImportTokenFalsePositive(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/repo\n"), 0o644); err != nil {
		t.Fatalf("WriteFile go.mod failed: %v", err)
	}

	idx := &model.Index{
		Root: root,
		Files: []model.FileSummary{
			{
				Path: "internal/http/a.go",
				Symbols: []model.Symbol{
					{File: "internal/http/a.go", Kind: "function_definition", Name: "Foo", StartLine: 1, EndLine: 1},
				},
			},
			{
				Path: "core/worker/b.go",
				Symbols: []model.Symbol{
					{File: "core/worker/b.go", Kind: "function_definition", Name: "Foo", StartLine: 1, EndLine: 1},
				},
			},
			{
				Path:    "app/main.go",
				Imports: []string{"net/http"},
				Symbols: []model.Symbol{
					{File: "app/main.go", Kind: "function_definition", Name: "Caller", StartLine: 1, EndLine: 3},
				},
				References: []model.Reference{
					{File: "app/main.go", Kind: "reference.call", Name: "Foo", StartLine: 2, EndLine: 2, StartColumn: 2, EndColumn: 5},
				},
			},
		},
	}

	graph, err := Build(idx)
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	if len(graph.Edges) != 0 {
		t.Fatalf("expected 0 resolved edges, got %d", len(graph.Edges))
	}
	if len(graph.Unresolved) != 1 {
		t.Fatalf("expected 1 unresolved call, got %d", len(graph.Unresolved))
	}
	if graph.Unresolved[0].Reason != "ambiguous_global" {
		t.Fatalf("unexpected unresolved reason %q", graph.Unresolved[0].Reason)
	}
	if graph.Unresolved[0].CandidateCount != 2 {
		t.Fatalf("expected candidate count 2, got %d", graph.Unresolved[0].CandidateCount)
	}
}

func TestBuildEmptyIndex(t *testing.T) {
	idx := &model.Index{
		Root:  "/tmp/empty",
		Files: []model.FileSummary{},
	}

	graph, err := Build(idx)
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	if len(graph.Definitions) != 0 {
		t.Fatalf("expected 0 definitions, got %d", len(graph.Definitions))
	}
	if len(graph.Edges) != 0 {
		t.Fatalf("expected 0 edges, got %d", len(graph.Edges))
	}
	if len(graph.Unresolved) != 0 {
		t.Fatalf("expected 0 unresolved, got %d", len(graph.Unresolved))
	}
}

func TestBuildSingleFile(t *testing.T) {
	idx := &model.Index{
		Root: "/tmp/single",
		Files: []model.FileSummary{
			{
				Path: "pkg/main.go",
				Symbols: []model.Symbol{
					{
						File:      "pkg/main.go",
						Kind:      "function_definition",
						Name:      "Main",
						Signature: "func Main()",
						StartLine: 1,
						EndLine:   5,
					},
					{
						File:      "pkg/main.go",
						Kind:      "function_definition",
						Name:      "Helper",
						Signature: "func Helper()",
						StartLine: 7,
						EndLine:   10,
					},
					{
						File:      "pkg/main.go",
						Kind:      "class_definition",
						Name:      "Config",
						StartLine: 12,
						EndLine:   15,
					},
				},
			},
		},
	}

	graph, err := Build(idx)
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	if len(graph.Definitions) != 3 {
		t.Fatalf("expected 3 definitions, got %d", len(graph.Definitions))
	}

	// Verify callable vs non-callable
	callableCount := 0
	for _, def := range graph.Definitions {
		if def.Callable {
			callableCount++
		}
	}
	if callableCount != 2 {
		t.Fatalf("expected 2 callable definitions, got %d", callableCount)
	}

	// Verify package derived from path
	for _, def := range graph.Definitions {
		if def.Package != "pkg" {
			t.Fatalf("expected package 'pkg', got %q", def.Package)
		}
	}
}

func TestBuildCallResolution(t *testing.T) {
	idx := &model.Index{
		Root: "/tmp/callres",
		Files: []model.FileSummary{
			{
				Path: "app.go",
				Symbols: []model.Symbol{
					{
						File:      "app.go",
						Kind:      "function_definition",
						Name:      "Alpha",
						StartLine: 1,
						EndLine:   5,
					},
					{
						File:      "app.go",
						Kind:      "function_definition",
						Name:      "Beta",
						StartLine: 7,
						EndLine:   10,
					},
				},
				References: []model.Reference{
					{
						File:        "app.go",
						Kind:        "reference.call",
						Name:        "Beta",
						StartLine:   3,
						EndLine:     3,
						StartColumn: 2,
						EndColumn:   6,
					},
				},
			},
		},
	}

	graph, err := Build(idx)
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	if len(graph.Edges) != 1 {
		t.Fatalf("expected 1 edge, got %d", len(graph.Edges))
	}

	edge := graph.Edges[0]
	if graph.EdgeCaller(edge).Name != "Alpha" {
		t.Fatalf("expected caller Alpha, got %q", graph.EdgeCaller(edge).Name)
	}
	if graph.EdgeCallee(edge).Name != "Beta" {
		t.Fatalf("expected callee Beta, got %q", graph.EdgeCallee(edge).Name)
	}
	if edge.Count != 1 {
		t.Fatalf("expected edge count 1, got %d", edge.Count)
	}
	if edge.Resolution != "file" {
		t.Fatalf("expected resolution 'file', got %q", edge.Resolution)
	}
	if len(edge.Samples) != 1 {
		t.Fatalf("expected 1 sample, got %d", len(edge.Samples))
	}
	if edge.Samples[0].StartLine != 3 {
		t.Fatalf("expected sample start line 3, got %d", edge.Samples[0].StartLine)
	}
}

func TestBuildCrossFileResolution(t *testing.T) {
	idx := &model.Index{
		Root: "/tmp/crossfile",
		Files: []model.FileSummary{
			{
				Path: "src/caller.go",
				Symbols: []model.Symbol{
					{
						File:      "src/caller.go",
						Kind:      "function_definition",
						Name:      "Invoke",
						StartLine: 1,
						EndLine:   5,
					},
				},
				References: []model.Reference{
					{
						File:        "src/caller.go",
						Kind:        "reference.call",
						Name:        "Target",
						StartLine:   3,
						EndLine:     3,
						StartColumn: 4,
						EndColumn:   10,
					},
				},
			},
			{
				Path: "lib/target.go",
				Symbols: []model.Symbol{
					{
						File:      "lib/target.go",
						Kind:      "function_definition",
						Name:      "Target",
						StartLine: 1,
						EndLine:   3,
					},
				},
			},
		},
	}

	graph, err := Build(idx)
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	if len(graph.Edges) != 1 {
		t.Fatalf("expected 1 edge, got %d", len(graph.Edges))
	}

	edge := graph.Edges[0]
	if graph.EdgeCaller(edge).Name != "Invoke" {
		t.Fatalf("expected caller Invoke, got %q", graph.EdgeCaller(edge).Name)
	}
	if graph.EdgeCallee(edge).Name != "Target" {
		t.Fatalf("expected callee Target, got %q", graph.EdgeCallee(edge).Name)
	}
	// Cross-file, different package -- resolved at global level
	if edge.Resolution != "global" {
		t.Fatalf("expected resolution 'global', got %q", edge.Resolution)
	}
	// Verify the files are correct
	if graph.EdgeCaller(edge).File != "src/caller.go" {
		t.Fatalf("expected caller file 'src/caller.go', got %q", graph.EdgeCaller(edge).File)
	}
	if graph.EdgeCallee(edge).File != "lib/target.go" {
		t.Fatalf("expected callee file 'lib/target.go', got %q", graph.EdgeCallee(edge).File)
	}
}

func TestFindDefinitionsByName(t *testing.T) {
	idx := &model.Index{
		Root: "/tmp/findname",
		Files: []model.FileSummary{
			{
				Path: "a.go",
				Symbols: []model.Symbol{
					{File: "a.go", Kind: "function_definition", Name: "Foo", StartLine: 1, EndLine: 3},
					{File: "a.go", Kind: "function_definition", Name: "Bar", StartLine: 5, EndLine: 7},
					{File: "a.go", Kind: "class_definition", Name: "Baz", StartLine: 9, EndLine: 12},
				},
			},
		},
	}

	graph, err := Build(idx)
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	// Exact match finds one
	matches, err := graph.FindDefinitions("Foo", false)
	if err != nil {
		t.Fatalf("FindDefinitions returned error: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected 1 match for 'Foo', got %d", len(matches))
	}
	if matches[0].Name != "Foo" {
		t.Fatalf("expected match name 'Foo', got %q", matches[0].Name)
	}

	// Non-callable definitions should not appear (Baz is class_definition)
	matches, err = graph.FindDefinitions("Baz", false)
	if err != nil {
		t.Fatalf("FindDefinitions returned error: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("expected 0 matches for non-callable 'Baz', got %d", len(matches))
	}

	// Non-existent name
	matches, err = graph.FindDefinitions("NoExist", false)
	if err != nil {
		t.Fatalf("FindDefinitions returned error: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("expected 0 matches for 'NoExist', got %d", len(matches))
	}

	// Empty pattern should error
	_, err = graph.FindDefinitions("", false)
	if err == nil {
		t.Fatalf("expected error for empty pattern, got nil")
	}
}

func TestFindDefinitionsByRegex(t *testing.T) {
	idx := &model.Index{
		Root: "/tmp/findregex",
		Files: []model.FileSummary{
			{
				Path: "a.go",
				Symbols: []model.Symbol{
					{File: "a.go", Kind: "function_definition", Name: "HandleGet", StartLine: 1, EndLine: 3},
					{File: "a.go", Kind: "function_definition", Name: "HandlePost", StartLine: 5, EndLine: 7},
					{File: "a.go", Kind: "function_definition", Name: "ProcessData", StartLine: 9, EndLine: 12},
					{File: "a.go", Kind: "method_definition", Name: "HandleDelete", StartLine: 14, EndLine: 16},
				},
			},
		},
	}

	graph, err := Build(idx)
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	// Regex matching Handle* functions
	matches, err := graph.FindDefinitions("^Handle", true)
	if err != nil {
		t.Fatalf("FindDefinitions returned error: %v", err)
	}
	if len(matches) != 3 {
		t.Fatalf("expected 3 matches for '^Handle', got %d", len(matches))
	}

	// Regex matching everything ending in Data
	matches, err = graph.FindDefinitions("Data$", true)
	if err != nil {
		t.Fatalf("FindDefinitions returned error: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected 1 match for 'Data$', got %d", len(matches))
	}
	if matches[0].Name != "ProcessData" {
		t.Fatalf("expected match name 'ProcessData', got %q", matches[0].Name)
	}

	// Dot-star matches all callables
	matches, err = graph.FindDefinitions(".*", true)
	if err != nil {
		t.Fatalf("FindDefinitions returned error: %v", err)
	}
	if len(matches) != 4 {
		t.Fatalf("expected 4 matches for '.*', got %d", len(matches))
	}

	// Bad regex should error
	_, err = graph.FindDefinitions("[invalid", true)
	if err == nil {
		t.Fatalf("expected error for invalid regex, got nil")
	}
}

func TestWalkForward(t *testing.T) {
	// Build a chain: Start -> Middle -> End
	idx := &model.Index{
		Root: "/tmp/walkfwd",
		Files: []model.FileSummary{
			{
				Path: "chain.go",
				Symbols: []model.Symbol{
					{File: "chain.go", Kind: "function_definition", Name: "Start", StartLine: 1, EndLine: 5},
					{File: "chain.go", Kind: "function_definition", Name: "Middle", StartLine: 7, EndLine: 11},
					{File: "chain.go", Kind: "function_definition", Name: "End", StartLine: 13, EndLine: 15},
				},
				References: []model.Reference{
					{File: "chain.go", Kind: "reference.call", Name: "Middle", StartLine: 3, EndLine: 3, StartColumn: 2, EndColumn: 8},
					{File: "chain.go", Kind: "reference.call", Name: "End", StartLine: 9, EndLine: 9, StartColumn: 2, EndColumn: 5},
				},
			},
		},
	}

	graph, err := Build(idx)
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	if len(graph.Edges) != 2 {
		t.Fatalf("expected 2 edges, got %d", len(graph.Edges))
	}

	// Find Start
	matches, err := graph.FindDefinitions("Start", false)
	if err != nil {
		t.Fatalf("FindDefinitions returned error: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected 1 match for Start, got %d", len(matches))
	}

	// Forward walk depth=1 from Start should reach Middle only
	walk := graph.Walk([]string{matches[0].ID}, 1, false)
	if walk.Reverse {
		t.Fatalf("expected reverse=false")
	}
	if walk.Depth != 1 {
		t.Fatalf("expected depth 1, got %d", walk.Depth)
	}
	if len(walk.Edges) != 1 {
		t.Fatalf("expected 1 edge in forward walk depth=1, got %d", len(walk.Edges))
	}
	if len(walk.Nodes) != 2 {
		t.Fatalf("expected 2 nodes (Start+Middle) in forward walk depth=1, got %d", len(walk.Nodes))
	}

	// Forward walk depth=2 from Start should reach Middle and End
	walk = graph.Walk([]string{matches[0].ID}, 2, false)
	if len(walk.Edges) != 2 {
		t.Fatalf("expected 2 edges in forward walk depth=2, got %d", len(walk.Edges))
	}
	if len(walk.Nodes) != 3 {
		t.Fatalf("expected 3 nodes in forward walk depth=2, got %d", len(walk.Nodes))
	}
}

func TestWalkReverse(t *testing.T) {
	// Build a chain: Start -> Middle -> End
	idx := &model.Index{
		Root: "/tmp/walkrev",
		Files: []model.FileSummary{
			{
				Path: "chain.go",
				Symbols: []model.Symbol{
					{File: "chain.go", Kind: "function_definition", Name: "Start", StartLine: 1, EndLine: 5},
					{File: "chain.go", Kind: "function_definition", Name: "Middle", StartLine: 7, EndLine: 11},
					{File: "chain.go", Kind: "function_definition", Name: "End", StartLine: 13, EndLine: 15},
				},
				References: []model.Reference{
					{File: "chain.go", Kind: "reference.call", Name: "Middle", StartLine: 3, EndLine: 3, StartColumn: 2, EndColumn: 8},
					{File: "chain.go", Kind: "reference.call", Name: "End", StartLine: 9, EndLine: 9, StartColumn: 2, EndColumn: 5},
				},
			},
		},
	}

	graph, err := Build(idx)
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	// Find End
	matches, err := graph.FindDefinitions("End", false)
	if err != nil {
		t.Fatalf("FindDefinitions returned error: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected 1 match for End, got %d", len(matches))
	}

	// Reverse walk depth=1 from End should reach Middle
	walk := graph.Walk([]string{matches[0].ID}, 1, true)
	if !walk.Reverse {
		t.Fatalf("expected reverse=true")
	}
	if len(walk.Edges) != 1 {
		t.Fatalf("expected 1 edge in reverse walk depth=1, got %d", len(walk.Edges))
	}
	if len(walk.Nodes) != 2 {
		t.Fatalf("expected 2 nodes (End+Middle) in reverse walk depth=1, got %d", len(walk.Nodes))
	}

	// Reverse walk depth=2 from End should reach Middle and Start
	walk = graph.Walk([]string{matches[0].ID}, 2, true)
	if len(walk.Edges) != 2 {
		t.Fatalf("expected 2 edges in reverse walk depth=2, got %d", len(walk.Edges))
	}
	if len(walk.Nodes) != 3 {
		t.Fatalf("expected 3 nodes in reverse walk depth=2, got %d", len(walk.Nodes))
	}

	// Verify roots are correct
	if len(walk.Roots) != 1 {
		t.Fatalf("expected 1 root, got %d", len(walk.Roots))
	}
	if walk.Roots[0].Name != "End" {
		t.Fatalf("expected root name 'End', got %q", walk.Roots[0].Name)
	}
}

func TestWalkDepthLimit(t *testing.T) {
	// Build a chain: A -> B -> C -> D
	idx := &model.Index{
		Root: "/tmp/walkdepth",
		Files: []model.FileSummary{
			{
				Path: "deep.go",
				Symbols: []model.Symbol{
					{File: "deep.go", Kind: "function_definition", Name: "A", StartLine: 1, EndLine: 5},
					{File: "deep.go", Kind: "function_definition", Name: "B", StartLine: 7, EndLine: 11},
					{File: "deep.go", Kind: "function_definition", Name: "C", StartLine: 13, EndLine: 17},
					{File: "deep.go", Kind: "function_definition", Name: "D", StartLine: 19, EndLine: 21},
				},
				References: []model.Reference{
					{File: "deep.go", Kind: "reference.call", Name: "B", StartLine: 3, EndLine: 3, StartColumn: 2, EndColumn: 3},
					{File: "deep.go", Kind: "reference.call", Name: "C", StartLine: 9, EndLine: 9, StartColumn: 2, EndColumn: 3},
					{File: "deep.go", Kind: "reference.call", Name: "D", StartLine: 15, EndLine: 15, StartColumn: 2, EndColumn: 3},
				},
			},
		},
	}

	graph, err := Build(idx)
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	if len(graph.Edges) != 3 {
		t.Fatalf("expected 3 edges, got %d", len(graph.Edges))
	}

	aMatches, err := graph.FindDefinitions("A", false)
	if err != nil || len(aMatches) != 1 {
		t.Fatalf("could not find definition A")
	}
	aID := aMatches[0].ID

	// depth=0 should be treated as 1 (Walk clamps to minimum 1)
	walk := graph.Walk([]string{aID}, 0, false)
	if walk.Depth != 1 {
		t.Fatalf("expected depth clamped to 1, got %d", walk.Depth)
	}
	if len(walk.Nodes) != 2 {
		t.Fatalf("expected 2 nodes at depth=0 (clamped to 1), got %d", len(walk.Nodes))
	}

	// depth=1: A -> B only
	walk = graph.Walk([]string{aID}, 1, false)
	if len(walk.Nodes) != 2 {
		t.Fatalf("expected 2 nodes at depth=1, got %d", len(walk.Nodes))
	}
	if len(walk.Edges) != 1 {
		t.Fatalf("expected 1 edge at depth=1, got %d", len(walk.Edges))
	}

	// depth=2: A -> B -> C
	walk = graph.Walk([]string{aID}, 2, false)
	if len(walk.Nodes) != 3 {
		t.Fatalf("expected 3 nodes at depth=2, got %d", len(walk.Nodes))
	}
	if len(walk.Edges) != 2 {
		t.Fatalf("expected 2 edges at depth=2, got %d", len(walk.Edges))
	}

	// depth=3: A -> B -> C -> D (full chain)
	walk = graph.Walk([]string{aID}, 3, false)
	if len(walk.Nodes) != 4 {
		t.Fatalf("expected 4 nodes at depth=3, got %d", len(walk.Nodes))
	}
	if len(walk.Edges) != 3 {
		t.Fatalf("expected 3 edges at depth=3, got %d", len(walk.Edges))
	}

	// depth=100: should still only get 4 nodes (no more to find)
	walk = graph.Walk([]string{aID}, 100, false)
	if len(walk.Nodes) != 4 {
		t.Fatalf("expected 4 nodes at depth=100, got %d", len(walk.Nodes))
	}
}

func TestDeadCodeDetection(t *testing.T) {
	// DeadFunc has zero incoming calls, LiveFunc is called by Caller
	idx := &model.Index{
		Root: "/tmp/deadcode",
		Files: []model.FileSummary{
			{
				Path: "code.go",
				Symbols: []model.Symbol{
					{File: "code.go", Kind: "function_definition", Name: "Caller", StartLine: 1, EndLine: 5},
					{File: "code.go", Kind: "function_definition", Name: "LiveFunc", StartLine: 7, EndLine: 9},
					{File: "code.go", Kind: "function_definition", Name: "DeadFunc", StartLine: 11, EndLine: 13},
				},
				References: []model.Reference{
					{File: "code.go", Kind: "reference.call", Name: "LiveFunc", StartLine: 3, EndLine: 3, StartColumn: 2, EndColumn: 10},
				},
			},
		},
	}

	graph, err := Build(idx)
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	// Identify dead code: callable definitions with zero incoming edges
	deadFuncs := []Definition{}
	for _, def := range graph.Definitions {
		if !def.Callable {
			continue
		}
		if graph.IncomingCount(def.ID) == 0 {
			deadFuncs = append(deadFuncs, def)
		}
	}

	// Caller and DeadFunc have no incoming calls; LiveFunc has 1
	if len(deadFuncs) != 2 {
		t.Fatalf("expected 2 dead functions (Caller, DeadFunc), got %d", len(deadFuncs))
	}

	// Verify DeadFunc is in the list
	foundDead := false
	for _, df := range deadFuncs {
		if df.Name == "DeadFunc" {
			foundDead = true
			break
		}
	}
	if !foundDead {
		t.Fatalf("expected DeadFunc in dead code list")
	}

	// Verify LiveFunc is NOT in the dead list
	for _, df := range deadFuncs {
		if df.Name == "LiveFunc" {
			t.Fatalf("LiveFunc should not be in dead code list")
		}
	}

	// Verify LiveFunc has exactly 1 incoming
	liveMatches, err := graph.FindDefinitions("LiveFunc", false)
	if err != nil || len(liveMatches) != 1 {
		t.Fatalf("could not find LiveFunc definition")
	}
	if graph.IncomingCount(liveMatches[0].ID) != 1 {
		t.Fatalf("expected LiveFunc incoming count 1, got %d", graph.IncomingCount(liveMatches[0].ID))
	}
}

func TestIncomingOutgoingCounts(t *testing.T) {
	// Hub calls Spoke1, Spoke2, Spoke3. Spoke1 also calls Spoke2.
	idx := &model.Index{
		Root: "/tmp/counts",
		Files: []model.FileSummary{
			{
				Path: "hub.go",
				Symbols: []model.Symbol{
					{File: "hub.go", Kind: "function_definition", Name: "Hub", StartLine: 1, EndLine: 10},
					{File: "hub.go", Kind: "function_definition", Name: "Spoke1", StartLine: 12, EndLine: 20},
					{File: "hub.go", Kind: "function_definition", Name: "Spoke2", StartLine: 22, EndLine: 25},
					{File: "hub.go", Kind: "function_definition", Name: "Spoke3", StartLine: 27, EndLine: 30},
				},
				References: []model.Reference{
					// Hub calls Spoke1
					{File: "hub.go", Kind: "reference.call", Name: "Spoke1", StartLine: 3, EndLine: 3, StartColumn: 2, EndColumn: 8},
					// Hub calls Spoke2
					{File: "hub.go", Kind: "reference.call", Name: "Spoke2", StartLine: 5, EndLine: 5, StartColumn: 2, EndColumn: 8},
					// Hub calls Spoke3
					{File: "hub.go", Kind: "reference.call", Name: "Spoke3", StartLine: 7, EndLine: 7, StartColumn: 2, EndColumn: 8},
					// Hub calls Spoke1 again (second call, same edge incremented)
					{File: "hub.go", Kind: "reference.call", Name: "Spoke1", StartLine: 9, EndLine: 9, StartColumn: 2, EndColumn: 8},
					// Spoke1 calls Spoke2
					{File: "hub.go", Kind: "reference.call", Name: "Spoke2", StartLine: 15, EndLine: 15, StartColumn: 2, EndColumn: 8},
				},
			},
		},
	}

	graph, err := Build(idx)
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	// Find all definitions
	findDef := func(name string) Definition {
		t.Helper()
		matches, err := graph.FindDefinitions(name, false)
		if err != nil || len(matches) != 1 {
			t.Fatalf("could not find unique definition for %q", name)
		}
		return matches[0]
	}

	hub := findDef("Hub")
	spoke1 := findDef("Spoke1")
	spoke2 := findDef("Spoke2")
	spoke3 := findDef("Spoke3")

	// Hub outgoing count: 2 (to Spoke1) + 1 (to Spoke2) + 1 (to Spoke3) = 4
	if got := graph.OutgoingCount(hub.ID); got != 4 {
		t.Fatalf("expected Hub outgoing count 4, got %d", got)
	}

	// Hub incoming count: 0 (nobody calls Hub)
	if got := graph.IncomingCount(hub.ID); got != 0 {
		t.Fatalf("expected Hub incoming count 0, got %d", got)
	}

	// Spoke1 outgoing count: 1 (to Spoke2)
	if got := graph.OutgoingCount(spoke1.ID); got != 1 {
		t.Fatalf("expected Spoke1 outgoing count 1, got %d", got)
	}

	// Spoke1 incoming count: 2 (from Hub, two calls)
	if got := graph.IncomingCount(spoke1.ID); got != 2 {
		t.Fatalf("expected Spoke1 incoming count 2, got %d", got)
	}

	// Spoke2 incoming count: 1 (Hub) + 1 (Spoke1) = 2
	if got := graph.IncomingCount(spoke2.ID); got != 2 {
		t.Fatalf("expected Spoke2 incoming count 2, got %d", got)
	}

	// Spoke2 outgoing count: 0
	if got := graph.OutgoingCount(spoke2.ID); got != 0 {
		t.Fatalf("expected Spoke2 outgoing count 0, got %d", got)
	}

	// Spoke3 incoming count: 1 (from Hub)
	if got := graph.IncomingCount(spoke3.ID); got != 1 {
		t.Fatalf("expected Spoke3 incoming count 1, got %d", got)
	}

	// Spoke3 outgoing count: 0
	if got := graph.OutgoingCount(spoke3.ID); got != 0 {
		t.Fatalf("expected Spoke3 outgoing count 0, got %d", got)
	}

	// Verify outgoing edges for Hub
	hubOutgoing := graph.OutgoingEdges(hub.ID)
	if len(hubOutgoing) != 3 {
		t.Fatalf("expected 3 outgoing edges from Hub, got %d", len(hubOutgoing))
	}

	// Verify incoming edges for Spoke2 (from Hub and Spoke1)
	spoke2Incoming := graph.IncomingEdges(spoke2.ID)
	if len(spoke2Incoming) != 2 {
		t.Fatalf("expected 2 incoming edges to Spoke2, got %d", len(spoke2Incoming))
	}

	// Verify the Hub->Spoke1 edge has count=2 (two call sites)
	for _, e := range hubOutgoing {
		if graph.EdgeCallee(e).Name == "Spoke1" {
			if e.Count != 2 {
				t.Fatalf("expected Hub->Spoke1 edge count 2, got %d", e.Count)
			}
			if len(e.Samples) != 2 {
				t.Fatalf("expected 2 samples for Hub->Spoke1, got %d", len(e.Samples))
			}
		}
	}
}
