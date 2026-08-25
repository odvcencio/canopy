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
	if report.Summary.Gaps != 1 || report.Summary.Recovered != 2 || report.Summary.ParseErrors != 1 || report.Summary.Untrusted != 3 {
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

func TestBuildHealthRanksIssuesAndBoundsDetails(t *testing.T) {
	idx := &model.Index{Files: []model.FileSummary{
		{Path: "clean.go", ParseCoverage: &model.ParseCoverage{Status: model.ParseCoverageClean}},
		{Path: "partial.go", ParseCoverage: &model.ParseCoverage{Status: model.ParseCoveragePartial}},
		{Path: "stopped.go", ParseCoverage: &model.ParseCoverage{Status: model.ParseCoverageStopped}},
	}}
	health, err := BuildHealth(idx, 1)
	if err != nil {
		t.Fatalf("BuildHealth returned error: %v", err)
	}
	if health.Status != model.ParseCoverageStopped {
		t.Fatalf("status = %q, want %q", health.Status, model.ParseCoverageStopped)
	}
	if health.TotalFiles != 3 || health.Summary.Untrusted != 2 || len(health.IssueFiles) != 1 || health.IssueFiles[0].Path != "stopped.go" {
		t.Fatalf("unexpected health receipt: %+v", health)
	}
	if !health.DetailsTruncated {
		t.Fatal("expected bounded issue details to report truncation")
	}
}

func TestReportStatusUsesActionableSeverityOrder(t *testing.T) {
	tests := []struct {
		name   string
		report Report
		want   string
	}{
		{name: "empty", report: Report{}, want: model.ParseCoverageUnknown},
		{name: "generated", report: Report{TotalFiles: 1, Summary: Summary{Generated: 1}}, want: model.ParseCoverageGenerated},
		{name: "clean", report: Report{TotalFiles: 1, Summary: Summary{Clean: 1}}, want: model.ParseCoverageClean},
		{name: "unknown", report: Report{TotalFiles: 2, Summary: Summary{Clean: 1, Unknown: 1}}, want: model.ParseCoverageUnknown},
		{name: "partial", report: Report{TotalFiles: 2, Summary: Summary{Unknown: 1, Partial: 1}}, want: model.ParseCoveragePartial},
		{name: "stopped", report: Report{TotalFiles: 2, Summary: Summary{Partial: 1, ParseErrors: 1}}, want: model.ParseCoverageStopped},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.report.Status(); got != test.want {
				t.Fatalf("Status() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestBuildHealthForPathsFiltersFilesAndParseErrors(t *testing.T) {
	idx := &model.Index{
		Files: []model.FileSummary{
			{Path: "pkg/clean.go", ParseCoverage: &model.ParseCoverage{Status: model.ParseCoverageClean}},
			{Path: "pkg/partial.go", ParseCoverage: &model.ParseCoverage{Status: model.ParseCoveragePartial}},
		},
		Errors: []model.ParseError{{Path: "pkg/failed.go", Error: "failed"}},
	}
	health, err := BuildHealthForPaths(idx, []string{"pkg/clean.go", "pkg/failed.go", "pkg/clean.go"}, 5)
	if err != nil {
		t.Fatalf("BuildHealthForPaths returned error: %v", err)
	}
	if health.TotalFiles != 1 || health.Summary.Clean != 1 || health.Summary.ParseErrors != 1 || health.Summary.Untrusted != 1 {
		t.Fatalf("unexpected selected health: %+v", health)
	}
	if health.Status != model.ParseCoverageStopped {
		t.Fatalf("status = %q, want %q", health.Status, model.ParseCoverageStopped)
	}
}
