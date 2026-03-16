package index

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNormalizeChangedPaths(t *testing.T) {
	root := filepath.Clean("/tmp/repo")
	changed := []string{
		filepath.Join(root, "a.go"),
		filepath.Join(root, "sub", "b.go"),
		filepath.Join(root, "..", "outside.go"),
		"",
	}

	got := normalizeChangedPaths(root, changed)
	if len(got) != 2 {
		t.Fatalf("expected 2 normalized paths, got %d (%v)", len(got), got)
	}
	if !got["a.go"] || !got["sub/b.go"] {
		t.Fatalf("unexpected normalized paths: %v", got)
	}
}

func TestApplyWatchChanges_SubfileIncremental(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "main.go")

	write := func(body string) {
		t.Helper()
		if err := os.WriteFile(filePath, []byte(body), 0o644); err != nil {
			t.Fatalf("WriteFile main.go failed: %v", err)
		}
	}

	write(`package sample

func A() {}

func B() {
	A()
}
`)

	builder := NewBuilder()
	current, _, err := builder.BuildPathIncremental(context.Background(), tmpDir, nil)
	if err != nil {
		t.Fatalf("BuildPathIncremental initial returned error: %v", err)
	}
	if current.SymbolCount() != 2 {
		t.Fatalf("expected 2 symbols in initial index, got %d", current.SymbolCount())
	}

	state := NewWatchState()
	defer state.Release()

	time.Sleep(2 * time.Millisecond)
	write(`package sample

func A() {}

func B() {
	A()
}

func C() {}
`)

	next, stats, err := builder.ApplyWatchChanges(current, []string{filePath}, state, WatchUpdateOptions{
		SubfileIncremental: true,
	})
	if err != nil {
		t.Fatalf("ApplyWatchChanges first returned error: %v", err)
	}
	if stats.ParsedFiles != 1 {
		t.Fatalf("expected ParsedFiles=1 for first update, got %+v", stats)
	}
	if next.SymbolCount() != 3 {
		t.Fatalf("expected symbol count 3 after first update, got %d", next.SymbolCount())
	}
	if _, ok := state.get("main.go"); !ok {
		t.Fatal("expected watch state to store parse tree for main.go")
	}

	time.Sleep(2 * time.Millisecond)
	write(`package sample

func A() {}

func B() {
	A()
	A()
}

func C() {}
`)

	next2, stats2, err := builder.ApplyWatchChanges(next, []string{filePath}, state, WatchUpdateOptions{
		SubfileIncremental: true,
	})
	if err != nil {
		t.Fatalf("ApplyWatchChanges second returned error: %v", err)
	}
	if stats2.ParsedFiles != 1 {
		t.Fatalf("expected ParsedFiles=1 for second update, got %+v", stats2)
	}
	if next2.ReferenceCount() != 2 {
		t.Fatalf("expected 2 references after second update, got %d", next2.ReferenceCount())
	}

	if err := os.Remove(filePath); err != nil {
		t.Fatalf("Remove main.go failed: %v", err)
	}

	next3, _, err := builder.ApplyWatchChanges(next2, []string{filePath}, state, WatchUpdateOptions{
		SubfileIncremental: true,
	})
	if err != nil {
		t.Fatalf("ApplyWatchChanges remove returned error: %v", err)
	}
	if next3.FileCount() != 0 {
		t.Fatalf("expected zero files after deletion, got %d", next3.FileCount())
	}
	if _, ok := state.get("main.go"); ok {
		t.Fatal("did not expect watch state entry after file deletion")
	}
}
