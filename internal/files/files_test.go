package files

import (
	"testing"

	"github.com/odvcencio/canopy/pkg/model"
)

func TestBuildFiltersAndSorts(t *testing.T) {
	idx := &model.Index{
		Root: "/tmp/repo",
		Files: []model.FileSummary{
			{
				Path:     "a.go",
				Language: "go",
				Imports:  []string{"fmt", "strings"},
				Symbols: []model.Symbol{
					{Kind: "function_definition", Name: "A"},
					{Kind: "type_definition", Name: "T"},
				},
				SizeBytes: 100,
			},
			{
				Path:      "b.go",
				Language:  "go",
				Imports:   []string{"fmt"},
				Symbols:   []model.Symbol{{Kind: "function_definition", Name: "B"}},
				SizeBytes: 200,
			},
			{
				Path:      "c.py",
				Language:  "python",
				Symbols:   []model.Symbol{{Kind: "function_definition", Name: "c"}},
				SizeBytes: 300,
			},
		},
	}

	report, err := Build(idx, Options{
		Language:   "go",
		MinSymbols: 1,
		SortBy:     "imports",
		Top:        10,
	})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	if report.TotalFiles != 3 || report.ShownFiles != 2 {
		t.Fatalf("unexpected counts: %+v", report)
	}
	if report.Entries[0].Path != "a.go" {
		t.Fatalf("expected a.go first by imports, got %+v", report.Entries)
	}
}

func TestBuildSortByPathAndTop(t *testing.T) {
	idx := &model.Index{
		Files: []model.FileSummary{
			{Path: "z.go", Language: "go"},
			{Path: "a.go", Language: "go"},
			{Path: "m.go", Language: "go"},
		},
	}
	report, err := Build(idx, Options{
		SortBy: "path",
		Top:    2,
	})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	if len(report.Entries) != 2 || report.Entries[0].Path != "a.go" || report.Entries[1].Path != "m.go" {
		t.Fatalf("unexpected entries: %+v", report.Entries)
	}
}

func TestBuildInvalidSort(t *testing.T) {
	_, err := Build(&model.Index{}, Options{SortBy: "bad"})
	if err == nil {
		t.Fatal("expected invalid sort to fail")
	}
}
