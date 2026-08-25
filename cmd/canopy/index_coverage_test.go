package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"m31labs.dev/canopy/internal/indexcoverage"
	"m31labs.dev/canopy/pkg/model"
)

func TestRunIndexCoverageJSON(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte("package sample\n\nfunc Work() {}\n"), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	output, runErr := captureIndexCoverageOutput(t, []string{tmpDir, "--no-cache", "--json", "--all"})
	if runErr != nil {
		t.Fatalf("runIndexCoverage returned error: %v", runErr)
	}
	var report indexcoverage.Report
	if err := json.Unmarshal(output, &report); err != nil {
		t.Fatalf("decode coverage report: %v\n%s", err, output)
	}
	if report.TotalFiles != 1 || report.Summary.Clean != 1 || len(report.Files) != 1 {
		t.Fatalf("unexpected report: %+v", report)
	}
}

func TestRunIndexCoverageStrictRejectsMalformedSource(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "broken.go"), []byte("package sample\n\nfunc Broken( {\n"), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	_, runErr := captureIndexCoverageOutput(t, []string{tmpDir, "--no-cache", "--strict"})
	if runErr == nil {
		t.Fatal("expected strict coverage to fail")
	}
	assertExitCode(t, runErr, 2)
}

func TestParseCoverageEqualDetectsReceiptChanges(t *testing.T) {
	left := []model.FileSummary{{
		Path:          "main.go",
		ParseCoverage: &model.ParseCoverage{Status: model.ParseCoverageClean},
	}}
	right := []model.FileSummary{{
		Path:          "main.go",
		ParseCoverage: &model.ParseCoverage{Status: model.ParseCoveragePartial},
	}}
	if parseCoverageEqual(left, right) {
		t.Fatal("expected different receipts to compare unequal")
	}
	right[0].ParseCoverage.Status = model.ParseCoverageClean
	if !parseCoverageEqual(left, right) {
		t.Fatal("expected identical receipts to compare equal")
	}
}

func captureIndexCoverageOutput(t *testing.T, args []string) ([]byte, error) {
	t.Helper()
	originalStdout := os.Stdout
	readPipe, writePipe, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe failed: %v", err)
	}
	os.Stdout = writePipe
	defer func() { os.Stdout = originalStdout }()

	runErr := runIndexCoverage(args)
	_ = writePipe.Close()
	var output bytes.Buffer
	if _, err := output.ReadFrom(readPipe); err != nil {
		t.Fatalf("ReadFrom failed: %v", err)
	}
	_ = readPipe.Close()
	return output.Bytes(), runErr
}
