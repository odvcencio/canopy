package risk

import (
	"fmt"
	"math"
	"os"
	"testing"

	"github.com/odvcencio/canopy/pkg/complexity"
)

func TestBasicRiskScoring(t *testing.T) {
	compReport := &complexity.Report{
		Functions: []complexity.FunctionMetrics{
			{File: "a.go", Name: "simple", Kind: "function_definition", StartLine: 1, EndLine: 5, Cyclomatic: 1, FanOut: 1},
			{File: "b.go", Name: "medium", Kind: "function_definition", StartLine: 1, EndLine: 20, Cyclomatic: 5, FanOut: 4},
			{File: "c.go", Name: "complex", Kind: "function_definition", StartLine: 1, EndLine: 50, Cyclomatic: 15, FanOut: 10},
		},
	}

	dir := t.TempDir()
	input := Input{
		Root:       dir,
		Complexity: compReport,
	}

	report, err := Analyze(input)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}

	if len(report.Functions) != 3 {
		t.Fatalf("expected 3 functions, got %d", len(report.Functions))
	}

	// Highest risk function should be ranked first.
	if report.Functions[0].Name != "complex" {
		t.Errorf("expected highest risk function to be 'complex', got %q", report.Functions[0].Name)
	}
	if report.Functions[2].Name != "simple" {
		t.Errorf("expected lowest risk function to be 'simple', got %q", report.Functions[2].Name)
	}

	// Risk values should be between 0 and 1.
	for _, fr := range report.Functions {
		if fr.Risk < 0 || fr.Risk > 1 {
			t.Errorf("risk for %s out of range: %f", fr.Name, fr.Risk)
		}
	}

	// The highest risk should be strictly greater than the lowest.
	if report.Functions[0].Risk <= report.Functions[2].Risk {
		t.Errorf("highest risk (%f) should be > lowest risk (%f)",
			report.Functions[0].Risk, report.Functions[2].Risk)
	}
}

func TestUntestedBoomsRisk(t *testing.T) {
	compReport := &complexity.Report{
		Functions: []complexity.FunctionMetrics{
			{File: "a.go", Name: "tested", Kind: "function_definition", StartLine: 1, EndLine: 10, Cyclomatic: 8, FanOut: 5},
			{File: "b.go", Name: "untested", Kind: "function_definition", StartLine: 1, EndLine: 10, Cyclomatic: 8, FanOut: 5},
		},
	}

	testMap := map[string]bool{
		fmt.Sprintf("a.go\x00tested\x001"): true,
	}

	dir := t.TempDir()
	input := Input{
		Root:       dir,
		Complexity: compReport,
		TestMap:    testMap,
	}

	report, err := Analyze(input)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}

	if len(report.Functions) != 2 {
		t.Fatalf("expected 2 functions, got %d", len(report.Functions))
	}

	var testedRisk, untestedRisk float64
	for _, fr := range report.Functions {
		if fr.Name == "tested" {
			testedRisk = fr.Risk
			if !fr.HasTest {
				t.Error("tested function should have HasTest=true")
			}
			if math.Abs(fr.UntestedPct-0.01) > 1e-9 {
				t.Errorf("tested function UntestedPct should be 0.01, got %f", fr.UntestedPct)
			}
		}
		if fr.Name == "untested" {
			untestedRisk = fr.Risk
			if fr.HasTest {
				t.Error("untested function should have HasTest=false")
			}
			if math.Abs(fr.UntestedPct-1.0) > 1e-9 {
				t.Errorf("untested function UntestedPct should be 1.0, got %f", fr.UntestedPct)
			}
		}
	}

	if untestedRisk <= testedRisk {
		t.Errorf("untested risk (%f) should be > tested risk (%f)", untestedRisk, testedRisk)
	}
}

func TestPackageAggregation(t *testing.T) {
	compReport := &complexity.Report{
		Functions: []complexity.FunctionMetrics{
			{File: "pkg/a/x.go", Name: "high1", Kind: "function_definition", StartLine: 1, EndLine: 50, Cyclomatic: 20, FanOut: 10},
			{File: "pkg/a/y.go", Name: "high2", Kind: "function_definition", StartLine: 1, EndLine: 40, Cyclomatic: 15, FanOut: 8},
			{File: "pkg/a/z.go", Name: "low1", Kind: "function_definition", StartLine: 1, EndLine: 5, Cyclomatic: 1, FanOut: 0},
			{File: "pkg/b/w.go", Name: "med1", Kind: "function_definition", StartLine: 1, EndLine: 20, Cyclomatic: 5, FanOut: 3},
			{File: "pkg/b/v.go", Name: "low2", Kind: "function_definition", StartLine: 1, EndLine: 5, Cyclomatic: 2, FanOut: 1},
		},
	}

	dir := t.TempDir()
	input := Input{
		Root:       dir,
		Complexity: compReport,
	}

	report, err := Analyze(input)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}

	if len(report.Packages) != 2 {
		t.Fatalf("expected 2 packages, got %d", len(report.Packages))
	}

	// Packages should be sorted by MaxRisk descending.
	if report.Packages[0].MaxRisk < report.Packages[1].MaxRisk {
		t.Error("packages should be sorted by MaxRisk descending")
	}

	// Find pkg/a package.
	var pkgA *PackageRisk
	for i := range report.Packages {
		if report.Packages[i].Package == "pkg/a" {
			pkgA = &report.Packages[i]
			break
		}
	}
	if pkgA == nil {
		t.Fatal("expected package 'pkg/a'")
	}
	if pkgA.Functions != 3 {
		t.Errorf("pkg/a should have 3 functions, got %d", pkgA.Functions)
	}
	if pkgA.MaxRisk <= 0 {
		t.Error("pkg/a MaxRisk should be > 0")
	}
	if pkgA.P90Risk <= 0 {
		t.Error("pkg/a P90Risk should be > 0")
	}
}

func TestSingleFunction(t *testing.T) {
	compReport := &complexity.Report{
		Functions: []complexity.FunctionMetrics{
			{File: "a.go", Name: "only", Kind: "function_definition", StartLine: 1, EndLine: 10, Cyclomatic: 5, FanOut: 3},
		},
	}

	dir := t.TempDir()
	input := Input{
		Root:       dir,
		Complexity: compReport,
	}

	report, err := Analyze(input)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}

	if len(report.Functions) != 1 {
		t.Fatalf("expected 1 function, got %d", len(report.Functions))
	}

	fr := report.Functions[0]
	// With a single function, all percentiles should be 0.5.
	if math.Abs(fr.ComplexityPct-0.5) > 1e-9 {
		t.Errorf("single function ComplexityPct should be 0.5, got %f", fr.ComplexityPct)
	}
	if math.Abs(fr.CouplingPct-0.5) > 1e-9 {
		t.Errorf("single function CouplingPct should be 0.5, got %f", fr.CouplingPct)
	}
	if math.Abs(fr.ChurnPct-0.5) > 1e-9 {
		t.Errorf("single function ChurnPct should be 0.5, got %f", fr.ChurnPct)
	}

	if fr.Risk <= 0 || fr.Risk > 1 {
		t.Errorf("risk out of range: %f", fr.Risk)
	}

	// Summary should reflect the single function.
	if report.Summary.TotalFunctions != 1 {
		t.Errorf("expected TotalFunctions=1, got %d", report.Summary.TotalFunctions)
	}
}

func TestEmptyInput(t *testing.T) {
	dir := t.TempDir()

	// Nil complexity report.
	report, err := Analyze(Input{Root: dir, Complexity: nil})
	if err != nil {
		t.Fatalf("Analyze with nil complexity: %v", err)
	}
	if len(report.Functions) != 0 {
		t.Errorf("expected 0 functions, got %d", len(report.Functions))
	}
	if len(report.Packages) != 0 {
		t.Errorf("expected 0 packages, got %d", len(report.Packages))
	}

	// Empty complexity report.
	report, err = Analyze(Input{Root: dir, Complexity: &complexity.Report{}})
	if err != nil {
		t.Fatalf("Analyze with empty complexity: %v", err)
	}
	if len(report.Functions) != 0 {
		t.Errorf("expected 0 functions, got %d", len(report.Functions))
	}
}

func TestNoGitHistory(t *testing.T) {
	// Create a temp dir with no git repo.
	dir, err := os.MkdirTemp("", "risk-test-nogit-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	compReport := &complexity.Report{
		Functions: []complexity.FunctionMetrics{
			{File: "a.go", Name: "fn1", Kind: "function_definition", StartLine: 1, EndLine: 10, Cyclomatic: 5, FanOut: 3},
			{File: "b.go", Name: "fn2", Kind: "function_definition", StartLine: 1, EndLine: 20, Cyclomatic: 10, FanOut: 6},
		},
	}

	input := Input{
		Root:       dir,
		Complexity: compReport,
	}

	report, err := Analyze(input)
	if err != nil {
		t.Fatalf("Analyze with no git: %v", err)
	}

	if len(report.Functions) != 2 {
		t.Fatalf("expected 2 functions, got %d", len(report.Functions))
	}

	// All churn should be zero.
	for _, fr := range report.Functions {
		if fr.Commits != 0 {
			t.Errorf("expected 0 commits for %s, got %d", fr.Name, fr.Commits)
		}
	}

	// Should still produce valid risk scores.
	for _, fr := range report.Functions {
		if fr.Risk < 0 || fr.Risk > 1 {
			t.Errorf("risk out of range for %s: %f", fr.Name, fr.Risk)
		}
	}
}
