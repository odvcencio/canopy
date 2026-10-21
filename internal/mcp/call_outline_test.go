package mcp

import (
	"os"
	"path/filepath"
	"testing"

	"m31labs.dev/canopy/internal/outline"
)

func TestServiceOutline_LiveAndJailed(t *testing.T) {
	root := t.TempDir()
	name := " source.go"
	path := filepath.Join(root, name)
	if err := os.WriteFile(path, []byte("package p\nfunc First() {}\n"), 0640); err != nil {
		t.Fatal(err)
	}
	service := NewService(root, "")
	got, err := service.Call("gts_outline", map[string]any{"file": name})
	if err != nil {
		t.Fatal(err)
	}
	first := got.(*outline.Report)
	if first.Path != name || len(first.Symbols) != 1 || first.Symbols[0].Name != "First" {
		t.Fatalf("literal path lost: %+v", first)
	}
	if err := os.WriteFile(path, []byte("package p\nfunc Updated() {}\n"), 0640); err != nil {
		t.Fatal(err)
	}
	got, err = service.Call("gts_outline", map[string]any{"file": name})
	if err != nil {
		t.Fatal(err)
	}
	second := got.(*outline.Report)
	if second.SourceSHA256 == first.SourceSHA256 || second.Symbols[0].Name != "Updated" {
		t.Fatal("outline reused stale source")
	}
	outside := filepath.Join(t.TempDir(), "outside.go")
	if err := os.WriteFile(outside, []byte("package outside\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Call("gts_outline", map[string]any{"file": outside}); err == nil {
		t.Fatal("outside file accepted")
	}
	link := filepath.Join(root, "escape.go")
	if err := os.Symlink(outside, link); err == nil {
		if _, err := service.Call("gts_outline", map[string]any{"file": "escape.go"}); err == nil {
			t.Fatal("symlink escape accepted")
		}
	}
	if _, err := os.Stat(filepath.Join(root, ".canopy")); !os.IsNotExist(err) {
		t.Fatalf("outline created an index/cache: %v", err)
	}
	t.Log("literal_path_preserved=true source_change_visible=true outside_blocked=true symlink_escape_blocked=true index_files_created=0")
}
