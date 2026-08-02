package contextbundle

import (
	"context"
	"testing"

	"m31labs.dev/canopy/pkg/model"
	"m31labs.dev/canopy/pkg/xref"
)

func buildTestIndexWithGenerated() *model.Index {
	return &model.Index{
		Version: "test",
		Root:    "/repo",
		Files: []model.FileSummary{
			{
				Path:     "gen/api.pb.go",
				Language: "go",
				Symbols: []model.Symbol{
					{File: "gen/api.pb.go", Kind: "function_definition", Name: "GeneratedThing", Signature: "func GeneratedThing()", StartLine: 3, EndLine: 5},
				},
				Generated: &model.GeneratedInfo{Generator: "protobuf", Reason: "marker"},
			},
			{
				Path:     "main.go",
				Language: "go",
				Symbols: []model.Symbol{
					{File: "main.go", Kind: "function_definition", Name: "Human", Signature: "func Human()", StartLine: 3, EndLine: 5},
				},
			},
		},
	}
}

// TestGenerateCandidates_GeneratedFilesExcludedByDefault verifies spec 10.6:
// generated files are excluded from candidate generation unless explicitly
// selected or IncludeGenerated is set.
func TestGenerateCandidates_GeneratedFilesExcludedByDefault(t *testing.T) {
	idx := buildTestIndexWithGenerated()
	graph, err := xref.Build(idx)
	if err != nil {
		t.Fatalf("xref.Build: %v", err)
	}

	req := Request{
		Root: idx.Root,
		Intent: TaskIntent{
			Kind:         TaskImplement,
			ChangedPaths: []string{"gen/api.pb.go", "main.go"},
		},
	}

	candidates := generateCandidates(context.Background(), idx, &graph, nil, req)
	if hasPath(candidates, "gen/api.pb.go") {
		t.Fatal("expected generated file to be excluded from candidates by default")
	}
	if !hasPath(candidates, "main.go") {
		t.Fatal("expected human-authored file to remain a candidate")
	}
}

func TestGenerateCandidates_ExplicitSelectorCarvesOutGenerated(t *testing.T) {
	idx := buildTestIndexWithGenerated()
	graph, err := xref.Build(idx)
	if err != nil {
		t.Fatalf("xref.Build: %v", err)
	}

	req := Request{
		Root: idx.Root,
		Intent: TaskIntent{
			Kind:  TaskImplement,
			Focus: []Selector{{File: "gen/api.pb.go", Symbol: "GeneratedThing", Required: true}},
		},
	}

	candidates := generateCandidates(context.Background(), idx, &graph, nil, req)
	if !hasPath(candidates, "gen/api.pb.go") {
		t.Fatal("expected explicitly selected generated file to be included (spec 10.6 carve-out)")
	}
}

func TestGenerateCandidates_IncludeGeneratedFlag(t *testing.T) {
	idx := buildTestIndexWithGenerated()
	graph, err := xref.Build(idx)
	if err != nil {
		t.Fatalf("xref.Build: %v", err)
	}

	req := Request{
		Root:             idx.Root,
		IncludeGenerated: true,
		Intent: TaskIntent{
			Kind:         TaskImplement,
			ChangedPaths: []string{"gen/api.pb.go"},
		},
	}

	candidates := generateCandidates(context.Background(), idx, &graph, nil, req)
	if !hasPath(candidates, "gen/api.pb.go") {
		t.Fatal("expected IncludeGenerated=true to surface the generated file")
	}
}

func hasPath(candidates []*candidateItem, path string) bool {
	for _, c := range candidates {
		if c.Path == path {
			return true
		}
	}
	return false
}
