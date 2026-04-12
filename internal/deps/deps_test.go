package deps

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/odvcencio/canopy/pkg/model"
)

func TestBuildPackageMode(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module example.com/repo\n"), 0o644); err != nil {
		t.Fatalf("WriteFile go.mod failed: %v", err)
	}

	idx := &model.Index{
		Root: tmpDir,
		Files: []model.FileSummary{
			{
				Path:    "cmd/app/main.go",
				Imports: []string{"fmt", "example.com/repo/internal/x"},
			},
			{
				Path:    "internal/x/x.go",
				Imports: []string{"strings"},
			},
		},
	}

	report, err := Build(idx, Options{
		Mode:         "package",
		Top:          10,
		Focus:        "cmd/app",
		Depth:        2,
		IncludeEdges: true,
	})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	if report.Module != "example.com/repo" {
		t.Fatalf("unexpected module %q", report.Module)
	}
	if report.EdgeCount != 3 {
		t.Fatalf("expected 3 edges, got %d", report.EdgeCount)
	}
	if report.InternalEdgeCount != 1 {
		t.Fatalf("expected 1 internal edge, got %d", report.InternalEdgeCount)
	}
	if len(report.FocusOutgoing) != 2 {
		t.Fatalf("expected 2 focus outgoing nodes, got %v", report.FocusOutgoing)
	}
	if report.FocusDirection != "forward" || report.FocusDepth != 2 {
		t.Fatalf("unexpected focus traversal metadata: direction=%q depth=%d", report.FocusDirection, report.FocusDepth)
	}
}

func TestBuildFileMode(t *testing.T) {
	idx := &model.Index{
		Root: "/tmp/repo",
		Files: []model.FileSummary{
			{
				Path:    "a.go",
				Imports: []string{"fmt", "fmt", "strings"},
			},
		},
	}

	report, err := Build(idx, Options{
		Mode: "file",
		Top:  10,
	})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	if report.EdgeCount != 2 {
		t.Fatalf("expected deduped edge count 2, got %d", report.EdgeCount)
	}
	if len(report.TopOutgoing) != 1 || report.TopOutgoing[0].Node != "a.go" || report.TopOutgoing[0].Outgoing != 2 {
		t.Fatalf("unexpected top outgoing: %+v", report.TopOutgoing)
	}
}

func TestBuildInvalidMode(t *testing.T) {
	_, err := Build(&model.Index{}, Options{Mode: "bad"})
	if err == nil {
		t.Fatal("expected invalid mode to fail")
	}
}

func TestBuildReverseWalk(t *testing.T) {
	idx := &model.Index{
		Root: "/tmp/repo",
		Files: []model.FileSummary{
			{Path: "a/a.go", Imports: []string{"x/y"}},
			{Path: "b/b.go", Imports: []string{"a"}},
			{Path: "c/c.go", Imports: []string{"b"}},
		},
	}

	report, err := Build(idx, Options{
		Mode:    "package",
		Focus:   "a",
		Depth:   2,
		Reverse: true,
	})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	if report.FocusDirection != "reverse" {
		t.Fatalf("unexpected focus direction %q", report.FocusDirection)
	}
	if len(report.FocusWalk) == 0 {
		t.Fatalf("expected reverse walk results, got %#v", report.FocusWalk)
	}
}

func TestModulePathFromRoot(t *testing.T) {
	tmpDir := t.TempDir()
	if got := modulePathFromRoot(tmpDir); got != "" {
		t.Fatalf("expected empty module path, got %q", got)
	}

	content := "module github.com/example/project\n\ngo 1.25\n"
	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile go.mod failed: %v", err)
	}
	if got := modulePathFromRoot(tmpDir); got != "github.com/example/project" {
		t.Fatalf("unexpected module path %q", got)
	}
}
