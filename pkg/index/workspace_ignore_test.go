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

func TestLoadWorkspaceIgnoreLines_ReadsGraftAndGtsFiles(t *testing.T) {
	root := t.TempDir()
	// Anchor the workspace with one of the config files so workspaceIgnoreRoot
	// resolves to this directory instead of walking up past the tempdir.
	if err := os.WriteFile(filepath.Join(root, ".graftignore"), []byte("dist/\nbuild/**\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".gtsignore"), []byte("*.min.js\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	lines, err := loadWorkspaceIgnoreLines(root)
	if err != nil {
		t.Fatalf("loadWorkspaceIgnoreLines: %v", err)
	}
	got := map[string]bool{}
	for _, line := range lines {
		got[line] = true
	}
	for _, want := range []string{"dist/", "build/**", "*.min.js"} {
		if !got[want] {
			t.Errorf("expected pattern %q in loaded lines, got %v", want, lines)
		}
	}
}

func TestNewBuilderWithWorkspaceIgnoresAndExtras_MergesWorkspaceAndCLIExtras(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".gtsignore"), []byte("vendor/\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	builder, err := NewBuilderWithWorkspaceIgnoresAndExtras(root, []string{
		"client/js/bootstrap.js",
		"*.min.js",
	})
	if err != nil {
		t.Fatalf("NewBuilderWithWorkspaceIgnoresAndExtras: %v", err)
	}

	matcher := builder.Ignore()
	if matcher == nil {
		t.Fatal("expected non-nil ignore matcher when workspace patterns or extras are present")
	}

	// Workspace pattern still applies.
	if !matcher.Match("vendor/pkg/file.go", false) {
		t.Error("expected vendor/pkg/file.go to match workspace pattern vendor/")
	}
	// CLI extras apply.
	if !matcher.Match("client/js/bootstrap.js", false) {
		t.Error("expected client/js/bootstrap.js to match CLI --exclude pattern")
	}
	if !matcher.Match("some/path/app.min.js", false) {
		t.Error("expected *.min.js extra pattern to match nested path")
	}
	// Unrelated files should not match.
	if matcher.Match("client/js/bootstrap-src/01-core.js", false) {
		t.Error("unrelated source file should not be excluded")
	}
}

func TestNewBuilderWithWorkspaceIgnoresAndExtras_NoWorkspaceFileExtrasOnly(t *testing.T) {
	// No .graftignore or .gtsignore in the temp dir — extras should still apply.
	root := t.TempDir()
	// Anchor the workspace here so workspaceIgnoreRoot doesn't walk up.
	if err := os.WriteFile(filepath.Join(root, ".gtsgenerated"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}

	builder, err := NewBuilderWithWorkspaceIgnoresAndExtras(root, []string{"generated/**"})
	if err != nil {
		t.Fatalf("NewBuilderWithWorkspaceIgnoresAndExtras: %v", err)
	}
	matcher := builder.Ignore()
	if matcher == nil {
		t.Fatal("expected matcher to be populated from CLI extras alone")
	}
	if !matcher.Match("generated/foo.go", false) {
		t.Error("expected generated/foo.go to match extras-only pattern")
	}
}

func TestNewBuilderWithWorkspaceIgnoresAndExtras_NoExtrasMatchesLegacyBehavior(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".gtsignore"), []byte("node_modules/\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	builder, err := NewBuilderWithWorkspaceIgnoresAndExtras(root, nil)
	if err != nil {
		t.Fatalf("NewBuilderWithWorkspaceIgnoresAndExtras: %v", err)
	}
	matcher := builder.Ignore()
	if matcher == nil {
		t.Fatal("expected matcher from workspace patterns alone")
	}
	if !matcher.Match("node_modules/pkg/index.js", false) {
		t.Error("expected node_modules/ to match workspace pattern")
	}
}
