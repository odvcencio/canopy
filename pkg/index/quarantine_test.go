package index

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A file journaled as in-flight by a build that never completed must be
// quarantined on the next build: skipped, recorded in Index.Skipped, and
// retried automatically once the file changes.
func TestBuild_QuarantinesFileLeftInflightByCrashedBuild(t *testing.T) {
	tmpDir := t.TempDir()
	killer := filepath.Join(tmpDir, "killer.go")
	body := "package sample\n\nfunc Killer() {}\n" + strings.Repeat("// padding line to cross the tracking threshold\n", 2048)
	if err := os.WriteFile(killer, []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile killer.go: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "ok.go"), []byte("package sample\n\nfunc OK() {}\n"), 0o644); err != nil {
		t.Fatalf("WriteFile ok.go: %v", err)
	}

	fi, err := os.Stat(killer)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if fi.Size() < inflightTrackMinBytes {
		t.Fatalf("fixture too small to be tracked: %d bytes", fi.Size())
	}

	// Simulate the crashed build's leftover journal.
	canopyDir := filepath.Join(tmpDir, ".canopy")
	if err := os.MkdirAll(canopyDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	leftover := map[string]inflightEntry{
		"killer.go": {
			Path:            "killer.go",
			SizeBytes:       fi.Size(),
			ModTimeUnixNano: fi.ModTime().UnixNano(),
			StartedAt:       time.Now().UTC(),
		},
	}
	data, err := json.Marshal(leftover)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(canopyDir, inflightFileName), data, 0o644); err != nil {
		t.Fatalf("WriteFile inflight: %v", err)
	}

	builder := NewBuilder()
	idx, stats, err := builder.BuildPathIncremental(context.Background(), tmpDir, nil)
	if err != nil {
		t.Fatalf("BuildPathIncremental: %v", err)
	}
	if stats.QuarantinedSkipped != 1 {
		t.Fatalf("QuarantinedSkipped = %d, want 1 (stats: %+v)", stats.QuarantinedSkipped, stats)
	}
	if len(idx.Skipped) != 1 || idx.Skipped[0].Path != "killer.go" {
		t.Fatalf("Index.Skipped = %+v, want killer.go", idx.Skipped)
	}
	for _, f := range idx.Files {
		if f.Path == "killer.go" {
			t.Fatal("quarantined file must not be indexed")
		}
	}

	// The file changed — quarantine lifts and it parses normally.
	time.Sleep(2 * time.Millisecond)
	if err := os.WriteFile(killer, []byte(body+"\nfunc Killer2() {}\n"), 0o644); err != nil {
		t.Fatalf("WriteFile updated killer.go: %v", err)
	}
	idx2, stats2, err := builder.BuildPathIncremental(context.Background(), tmpDir, idx)
	if err != nil {
		t.Fatalf("BuildPathIncremental retry: %v", err)
	}
	if stats2.QuarantinedSkipped != 0 {
		t.Fatalf("retry QuarantinedSkipped = %d, want 0", stats2.QuarantinedSkipped)
	}
	found := false
	for _, f := range idx2.Files {
		if f.Path == "killer.go" {
			found = true
		}
	}
	if !found {
		t.Fatal("changed file should have been parsed after quarantine lift")
	}

	// A completed build leaves no in-flight journal behind.
	var inflight map[string]inflightEntry
	readJSON(filepath.Join(canopyDir, inflightFileName), &inflight)
	if len(inflight) != 0 {
		t.Fatalf("inflight journal not cleared: %+v", inflight)
	}
}

func TestClearQuarantine(t *testing.T) {
	tmpDir := t.TempDir()
	canopyDir := filepath.Join(tmpDir, ".canopy")
	if err := os.MkdirAll(canopyDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	qPath := filepath.Join(canopyDir, quarantineFileName)
	if err := os.WriteFile(qPath, []byte(`{"x.go":{"path":"x.go"}}`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := ClearQuarantine(tmpDir); err != nil {
		t.Fatalf("ClearQuarantine: %v", err)
	}
	if _, err := os.Stat(qPath); !os.IsNotExist(err) {
		t.Fatalf("quarantine file still present (err=%v)", err)
	}
	if err := ClearQuarantine(tmpDir); err != nil {
		t.Fatalf("ClearQuarantine on missing file: %v", err)
	}
}
