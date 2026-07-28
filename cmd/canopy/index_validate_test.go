package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"m31labs.dev/canopy/pkg/index"
	"m31labs.dev/canopy/pkg/model"
)

func TestBuildValidateReportDetectsSourceChanges(t *testing.T) {
	tests := []struct {
		name        string
		change      func(t *testing.T, root string)
		wantOK      int
		wantStale   int
		wantMissing int
		wantNew     int
		wantFiles   []string
	}{
		{
			name: "edited indexed file",
			change: func(t *testing.T, root string) {
				t.Helper()
				writeValidateSource(t, filepath.Join(root, "indexed.go"), "package sample\n\nfunc Changed() {}\n")
			},
			wantStale: 1,
			wantFiles: []string{"indexed.go"},
		},
		{
			name: "missing indexed file",
			change: func(t *testing.T, root string) {
				t.Helper()
				if err := os.Remove(filepath.Join(root, "indexed.go")); err != nil {
					t.Fatalf("Remove(indexed.go) failed: %v", err)
				}
			},
			wantMissing: 1,
			wantFiles:   []string{"indexed.go"},
		},
		{
			name: "new indexable file",
			change: func(t *testing.T, root string) {
				t.Helper()
				writeValidateSource(t, filepath.Join(root, "added.go"), "package sample\n\nfunc Added() {}\n")
			},
			wantOK:    1,
			wantNew:   1,
			wantFiles: []string{"added.go"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			indexedPath := filepath.Join(root, "indexed.go")
			writeValidateSource(t, indexedPath, "package sample\n\nfunc Original() {}\n")
			idx := validateTestIndex(t, root, indexedPath)

			tc.change(t, root)

			report, err := buildValidateReport(context.Background(), root, idx)
			if err != nil {
				t.Fatalf("buildValidateReport returned error: %v", err)
			}
			if report.Total != 1 {
				t.Fatalf("Total = %d, want 1", report.Total)
			}
			if report.OK != tc.wantOK {
				t.Fatalf("OK = %d, want %d", report.OK, tc.wantOK)
			}
			if report.Stale != tc.wantStale {
				t.Fatalf("Stale = %d, want %d", report.Stale, tc.wantStale)
			}
			if report.Missing != tc.wantMissing {
				t.Fatalf("Missing = %d, want %d", report.Missing, tc.wantMissing)
			}
			if report.New != tc.wantNew {
				t.Fatalf("New = %d, want %d", report.New, tc.wantNew)
			}

			var gotFiles []string
			switch {
			case tc.wantStale > 0:
				gotFiles = report.StaleFiles
			case tc.wantMissing > 0:
				gotFiles = report.MissingFiles
			case tc.wantNew > 0:
				gotFiles = report.NewFiles
			}
			if !reflect.DeepEqual(gotFiles, tc.wantFiles) {
				t.Fatalf("reported files = %#v, want %#v", gotFiles, tc.wantFiles)
			}
		})
	}
}

func TestValidateCommandJSONReturnsStaleStatus(t *testing.T) {
	root := t.TempDir()
	indexedPath := filepath.Join(root, "indexed.go")
	writeValidateSource(t, indexedPath, "package sample\n\nfunc Original() {}\n")
	idx := validateTestIndex(t, root, indexedPath)
	cachePath := filepath.Join(root, ".canopy", "index.json")
	if err := index.Save(cachePath, idx); err != nil {
		t.Fatalf("index.Save failed: %v", err)
	}
	writeValidateSource(t, indexedPath, "package sample\n\nfunc Changed() {}\n")

	output, err := captureValidateStdout(t, func() error {
		cmd := newValidateCmd()
		cmd.SilenceErrors = true
		cmd.SilenceUsage = true
		cmd.SetArgs([]string{root, "--cache", cachePath, "--json"})
		return cmd.Execute()
	})

	var exitErr interface{ ExitCode() int }
	if !errors.As(err, &exitErr) {
		t.Fatalf("Execute error = %v, want an exit code error", err)
	}
	if exitErr.ExitCode() != 2 {
		t.Fatalf("exit code = %d, want 2", exitErr.ExitCode())
	}

	var report validateReport
	if err := json.Unmarshal(output, &report); err != nil {
		t.Fatalf("JSON output is invalid: %v\n%s", err, output)
	}
	if report.Stale != 1 || !reflect.DeepEqual(report.StaleFiles, []string{"indexed.go"}) {
		t.Fatalf("JSON report = %+v, want indexed.go marked stale", report)
	}
}

func validateTestIndex(t *testing.T, root, indexedPath string) *model.Index {
	t.Helper()
	info, err := os.Stat(indexedPath)
	if err != nil {
		t.Fatalf("Stat(%s) failed: %v", indexedPath, err)
	}
	return &model.Index{
		Root:        root,
		GeneratedAt: time.Now(),
		Files: []model.FileSummary{{
			Path:            "indexed.go",
			Language:        "go",
			SizeBytes:       info.Size(),
			ModTimeUnixNano: info.ModTime().UnixNano(),
		}},
	}
}

func writeValidateSource(t *testing.T, path, source string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatalf("WriteFile(%s) failed: %v", path, err)
	}
}

func captureValidateStdout(t *testing.T, run func() error) ([]byte, error) {
	t.Helper()
	original := os.Stdout
	readPipe, writePipe, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe failed: %v", err)
	}
	os.Stdout = writePipe
	defer func() {
		os.Stdout = original
	}()

	runErr := run()
	if err := writePipe.Close(); err != nil {
		t.Fatalf("stdout writer close failed: %v", err)
	}
	output, err := io.ReadAll(readPipe)
	if err != nil {
		t.Fatalf("stdout read failed: %v", err)
	}
	if err := readPipe.Close(); err != nil {
		t.Fatalf("stdout reader close failed: %v", err)
	}
	return output, runErr
}
