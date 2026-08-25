package indexcoverage

import (
	"testing"

	"m31labs.dev/canopy/pkg/model"
)

func TestBuildAggregatesReceiptsAndDefaultsToUntrustedFiles(t *testing.T) {
	idx := &model.Index{
		Root: "/repo",
		Files: []model.FileSummary{
			{Path: "clean.go", Language: "go", ParseCoverage: &model.ParseCoverage{Status: model.ParseCoverageClean}},
			{
				Path:     "broken.go",
				Language: "go",
				ParseCoverage: &model.ParseCoverage{
					Status:           model.ParseCoveragePartial,
					ErrorNodes:       1,
					RecoveredRegions: 2,
					Gaps:             []model.ParseGap{{Kind: "error", StartLine: 3, EndLine: 4}},
				},
			},
			{Path: "legacy.go", Language: "go"},
			{Path: "generated.go", Language: "go", ParseCoverage: &model.ParseCoverage{Status: model.ParseCoverageGenerated}},
		},
		Errors: []model.ParseError{{Path: "failed.go", Error: "parse failure"}},
	}

	report, err := Build(idx, Options{MaxFiles: 10})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	if report.Summary.Clean != 1 || report.Summary.Partial != 1 || report.Summary.Unknown != 1 || report.Summary.Generated != 1 {
		t.Fatalf("unexpected summary: %+v", report.Summary)
	}
	if report.Summary.Gaps != 1 || report.Summary.Recovered != 2 || report.Summary.ParseErrors != 1 {
		t.Fatalf("unexpected receipt totals: %+v", report.Summary)
	}
	if len(report.Files) != 2 || report.Files[0].Path != "broken.go" || report.Files[1].Path != "legacy.go" {
		t.Fatalf("unexpected default file details: %+v", report.Files)
	}
	if !report.StrictFailure() {
		t.Fatal("expected incomplete coverage to fail strict mode")
	}
}

func TestBuildIncludesAllReceiptsAndLimitsDetails(t *testing.T) {
	idx := &model.Index{Files: []model.FileSummary{
		{Path: "a.go", ParseCoverage: &model.ParseCoverage{Status: model.ParseCoverageClean}},
		{Path: "b.go", ParseCoverage: &model.ParseCoverage{Status: model.ParseCoverageGenerated}},
		{Path: "c.go", ParseCoverage: &model.ParseCoverage{Status: model.ParseCoveragePartial}},
	}}
	report, err := Build(idx, Options{IncludeClean: true, IncludeGenerated: true, MaxFiles: 2})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	if len(report.Files) != 2 || !report.DetailsTruncated {
		t.Fatalf("expected limited file details: %+v", report)
	}
}

func TestCleanReportPassesStrictMode(t *testing.T) {
	report, err := Build(&model.Index{Files: []model.FileSummary{
		{Path: "main.go", ParseCoverage: &model.ParseCoverage{Status: model.ParseCoverageClean}},
	}}, Options{})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	if report.StrictFailure() {
		t.Fatalf("clean report failed strict mode: %+v", report)
	}
}

func TestTruncatedReceiptFailsStrictMode(t *testing.T) {
	report, err := Build(&model.Index{Files: []model.FileSummary{
		{Path: "large.go", ParseCoverage: &model.ParseCoverage{Status: model.ParseCoverageClean, Truncated: true}},
	}}, Options{})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	if !report.StrictFailure() {
		t.Fatalf("truncated receipt passed strict mode: %+v", report)
	}
}
