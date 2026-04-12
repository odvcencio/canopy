package main

import (
	"io"
	"path/filepath"
	"testing"
	"time"

	"github.com/odvcencio/canopy/pkg/index"
	"github.com/odvcencio/canopy/pkg/model"
)

func TestIndexCheckpointWriterSavesStreamSnapshot(t *testing.T) {
	tmpDir := t.TempDir()

	var saved *model.Index
	writer := newIndexCheckpointWriter(filepath.Join(tmpDir, "index.json"), tmpDir, &model.Index{
		Root: tmpDir,
		Files: []model.FileSummary{
			{Path: "baseline.go", Language: "go"},
		},
	})
	writer.stderr = io.Discard
	writer.saveEvery = 1
	writer.saveInterval = time.Hour
	writer.saveFn = func(path string, idx *model.Index) error {
		saved = idx
		return nil
	}

	writer.Observe(index.BuildEvent{
		Kind: index.BuildEventParsed,
		Path: "main.go",
		Summary: model.FileSummary{
			Path:     "main.go",
			Language: "go",
		},
		Stats: index.BuildStats{
			CandidateFiles: 1,
			ParsedFiles:    1,
		},
	})

	if !writer.SavedAny() {
		t.Fatal("expected checkpoint writer to record a saved snapshot")
	}
	if saved == nil {
		t.Fatal("expected saved snapshot")
	}
	if saved.FileCount() != 2 {
		t.Fatalf("expected baseline + streamed file, got %d files", saved.FileCount())
	}
	if saved.Files[0].Path != "baseline.go" || saved.Files[1].Path != "main.go" {
		t.Fatalf("unexpected snapshot files: %+v", saved.Files)
	}
}
