package stats

import (
	"testing"

	"github.com/odvcencio/canopy/pkg/model"
)

func TestBuildAggregatesCounts(t *testing.T) {
	idx := &model.Index{
		Root: "/tmp/repo",
		Files: []model.FileSummary{
			{
				Path:     "a.go",
				Language: "go",
				Imports:  []string{"fmt"},
				Symbols: []model.Symbol{
					{Kind: "function_definition", Name: "A"},
					{Kind: "type_definition", Name: "T"},
				},
			},
			{
				Path:     "b.go",
				Language: "go",
				Symbols: []model.Symbol{
					{Kind: "function_definition", Name: "B"},
				},
			},
		},
		Errors: []model.ParseError{{Path: "bad.go", Error: "parse error"}},
	}

	report, err := Build(idx, Options{TopFiles: 1})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	if report.FileCount != 2 || report.SymbolCount != 3 || report.ParseErrorCount != 1 {
		t.Fatalf("unexpected report totals: %+v", report)
	}
	if len(report.KindCounts) < 2 {
		t.Fatalf("expected at least 2 kind counts, got %d", len(report.KindCounts))
	}
	if report.KindCounts[0].Kind != "function_definition" || report.KindCounts[0].Count != 2 {
		t.Fatalf("unexpected top kind count: %+v", report.KindCounts[0])
	}
	if len(report.TopFiles) != 1 || report.TopFiles[0].Path != "a.go" {
		t.Fatalf("unexpected top files: %+v", report.TopFiles)
	}
	if len(report.Languages) != 1 || report.Languages[0].Language != "go" || report.Languages[0].Files != 2 {
		t.Fatalf("unexpected language counts: %+v", report.Languages)
	}
}

func TestBuildGeneratorBreakdown(t *testing.T) {
	idx := &model.Index{
		Root: "/tmp/repo",
		Files: []model.FileSummary{
			{
				Path:     "main.go",
				Language: "go",
				Symbols:  []model.Symbol{{Kind: "function_definition", Name: "main"}},
			},
			{
				Path:      "user.pb.go",
				Language:  "go",
				Generated: &model.GeneratedInfo{Generator: "protobuf", Reason: "filename"},
				Symbols: []model.Symbol{
					{Kind: "type_definition", Name: "User"},
					{Kind: "function_definition", Name: "GetUser"},
				},
			},
			{
				Path:      "order.pb.go",
				Language:  "go",
				Generated: &model.GeneratedInfo{Generator: "protobuf", Reason: "filename"},
				Symbols:   []model.Symbol{{Kind: "type_definition", Name: "Order"}},
			},
			{
				Path:      "mock_repo.go",
				Language:  "go",
				Generated: &model.GeneratedInfo{Generator: "mockgen", Reason: "marker"},
				Symbols:   []model.Symbol{{Kind: "function_definition", Name: "NewMockRepo"}},
			},
		},
	}

	report, err := Build(idx, Options{TopFiles: 10})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	if len(report.Generators) != 2 {
		t.Fatalf("expected 2 generators, got %d: %+v", len(report.Generators), report.Generators)
	}

	// Sorted by files descending: protobuf=2, mockgen=1
	if report.Generators[0].Generator != "protobuf" {
		t.Errorf("expected first generator to be protobuf, got %s", report.Generators[0].Generator)
	}
	if report.Generators[0].Files != 2 || report.Generators[0].Symbols != 3 {
		t.Errorf("protobuf: expected files=2 symbols=3, got files=%d symbols=%d",
			report.Generators[0].Files, report.Generators[0].Symbols)
	}
	if report.Generators[1].Generator != "mockgen" {
		t.Errorf("expected second generator to be mockgen, got %s", report.Generators[1].Generator)
	}
	if report.Generators[1].Files != 1 || report.Generators[1].Symbols != 1 {
		t.Errorf("mockgen: expected files=1 symbols=1, got files=%d symbols=%d",
			report.Generators[1].Files, report.Generators[1].Symbols)
	}
}

func TestBuildNoGeneratedFiles(t *testing.T) {
	idx := &model.Index{
		Root: "/tmp/repo",
		Files: []model.FileSummary{
			{Path: "main.go", Language: "go"},
		},
	}

	report, err := Build(idx, Options{TopFiles: 10})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	if len(report.Generators) != 0 {
		t.Fatalf("expected 0 generators, got %d", len(report.Generators))
	}
}

func TestBuildNilIndex(t *testing.T) {
	_, err := Build(nil, Options{})
	if err == nil {
		t.Fatal("expected nil index to fail")
	}
}
