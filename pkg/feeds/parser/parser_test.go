package parser

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/odvcencio/canopy/pkg/feeds"
	"github.com/odvcencio/canopy/pkg/scope"
)

func TestParserFeedName(t *testing.T) {
	f := New()
	if f.Name() != "parser" {
		t.Errorf("Name() = %q, want parser", f.Name())
	}
}

func TestParserFeedSupportsAll(t *testing.T) {
	f := New()
	for _, lang := range []string{"go", "python", "rust", "typescript", "java", "unknown"} {
		if !f.Supports(lang) {
			t.Errorf("Supports(%q) = false, want true", lang)
		}
	}
}

func TestParserFeedPriority(t *testing.T) {
	f := New()
	if f.Priority() != 0 {
		t.Errorf("Priority() = %d, want 0 (lowest)", f.Priority())
	}
}

func TestParserFeedBuildsScope(t *testing.T) {
	dir := t.TempDir()
	goFile := filepath.Join(dir, "main.go")
	src := []byte("package main\n\nfunc Hello() {}\n")
	if err := os.WriteFile(goFile, src, 0644); err != nil {
		t.Fatal(err)
	}

	graph := scope.NewGraph()
	f := New()
	ctx := &feeds.FeedContext{
		WorkspaceRoot: dir,
		Logger:        slog.Default(),
	}

	err := f.Feed(graph, "main.go", src, ctx)
	if err != nil {
		t.Fatalf("Feed() error: %v", err)
	}

	fs := graph.FileScope("main.go")
	if fs == nil {
		t.Fatal("FileScope(main.go) is nil after feed")
	}

	found := false
	for _, d := range fs.Defs {
		if d.Name == "Hello" && d.Kind == scope.DefFunction {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected to find function Hello in scope graph")
	}
}
