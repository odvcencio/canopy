package generated

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseConfigLines(t *testing.T) {
	lines := []string{
		"# comment",
		"protobuf: api/v1/**/*.pb.go",
		"custom-codegen: internal/gen/**",
		"",
		"legacy/auto/**",
		"  thrift: services/**/*_types.go  ",
	}
	entries := ParseConfigLines(lines)
	if len(entries) != 4 {
		t.Fatalf("expected 4 entries, got %d", len(entries))
	}
	if entries[0].Generator != "protobuf" || entries[0].Pattern != "api/v1/**/*.pb.go" {
		t.Errorf("entry 0: got %+v", entries[0])
	}
	if entries[1].Generator != "custom-codegen" || entries[1].Pattern != "internal/gen/**" {
		t.Errorf("entry 1: got %+v", entries[1])
	}
	if entries[2].Generator != "config" || entries[2].Pattern != "legacy/auto/**" {
		t.Errorf("entry 2: got %+v", entries[2])
	}
	if entries[3].Generator != "thrift" || entries[3].Pattern != "services/**/*_types.go" {
		t.Errorf("entry 3: got %+v", entries[3])
	}
}

func TestLoadConfigFile(t *testing.T) {
	dir := t.TempDir()
	content := "protobuf: api/**/*.pb.go\nlegacy/**\n"
	path := filepath.Join(dir, ".gtsgenerated")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	entries, err := LoadConfigFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
}

func TestLoadConfigFile_NotExist(t *testing.T) {
	entries, err := LoadConfigFile("/nonexistent/.gtsgenerated")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(entries))
	}
}

func TestParseConfigLinesWithOptions_ScanDepth(t *testing.T) {
	lines := []string{"@scan-depth 60", "protobuf: api/**/*.pb.go"}
	entries, scanDepth := ParseConfigLinesWithOptions(lines)
	if scanDepth != 60 {
		t.Errorf("expected 60, got %d", scanDepth)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
}

func TestParseConfigLinesWithOptions_InvalidScanDepth(t *testing.T) {
	lines := []string{"@scan-depth abc", "@scan-depth -5", "@scan-depth 0"}
	_, scanDepth := ParseConfigLinesWithOptions(lines)
	if scanDepth != 0 {
		t.Errorf("expected 0, got %d", scanDepth)
	}
}

func TestParseConfigLinesWithOptions_ClampedScanDepth(t *testing.T) {
	lines := []string{"@scan-depth 999"}
	_, scanDepth := ParseConfigLinesWithOptions(lines)
	if scanDepth != 200 {
		t.Errorf("expected 200, got %d", scanDepth)
	}
}
