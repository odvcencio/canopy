package index

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadWorkspaceGeneratedConfig(t *testing.T) {
	root := t.TempDir()
	content := "protobuf: api/**/*.pb.go\ncustom: internal/gen/**\n"
	if err := os.WriteFile(filepath.Join(root, ".gtsgenerated"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	entries, _, err := LoadWorkspaceGeneratedConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
}

func TestLoadWorkspaceGeneratedConfig_NoFile(t *testing.T) {
	root := t.TempDir()
	entries, _, err := LoadWorkspaceGeneratedConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(entries))
	}
}
