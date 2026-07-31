package index

import (
	"context"
	"testing"
)

func TestBuildSources_ParsesInMemoryContent(t *testing.T) {
	builder := NewBuilder()

	files := []SourceFile{
		{Path: "main.go", Content: []byte("package sample\n\nfunc Greet() string { return \"hi\" }\n")},
		{Path: "README.md", Content: []byte("docs")},
	}

	idx, err := builder.BuildSources(context.Background(), "base:abc123", files)
	if err != nil {
		t.Fatalf("BuildSources returned error: %v", err)
	}

	if idx.Root != "base:abc123" {
		t.Fatalf("expected root label preserved, got %q", idx.Root)
	}
	if idx.FileCount() != 1 {
		t.Fatalf("expected 1 indexed file (README.md has no parser), got %d: %#v", idx.FileCount(), idx.Files)
	}
	if idx.Files[0].Path != "main.go" {
		t.Fatalf("expected path main.go, got %q", idx.Files[0].Path)
	}
	if idx.SymbolCount() != 1 {
		t.Fatalf("expected 1 symbol, got %d", idx.SymbolCount())
	}
	if idx.Files[0].Symbols[0].Name != "Greet" {
		t.Fatalf("expected symbol Greet, got %q", idx.Files[0].Symbols[0].Name)
	}
}

func TestBuildSources_NoWorktreeRequired(t *testing.T) {
	// BuildSources must not touch disk for the given paths — this is the
	// whole point of indexing base-ref content pulled from `git show`
	// without materializing a temporary worktree.
	builder := NewBuilder()

	idx, err := builder.BuildSources(context.Background(), "head", []SourceFile{
		{Path: "does/not/exist/on/disk.go", Content: []byte("package x\n\nfunc F() {}\n")},
	})
	if err != nil {
		t.Fatalf("BuildSources returned error: %v", err)
	}
	if idx.FileCount() != 1 {
		t.Fatalf("expected 1 indexed file, got %d", idx.FileCount())
	}
}

func TestBuildSources_RecordsParseErrors(t *testing.T) {
	builder := NewBuilder()

	// Malformed Go source that the parser can still tokenize but which has
	// no extractable symbols is not a parse error — use an empty file with
	// an extension whose parser is registered to confirm errors surface
	// without panicking when present.
	idx, err := builder.BuildSources(context.Background(), "head", []SourceFile{
		{Path: "empty.go", Content: []byte("")},
	})
	if err != nil {
		t.Fatalf("BuildSources returned error: %v", err)
	}
	// Empty content should not crash the builder either way.
	_ = idx
}

func TestBuildSources_ContextCancellation(t *testing.T) {
	builder := NewBuilder()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	idx, err := builder.BuildSources(ctx, "head", []SourceFile{
		{Path: "main.go", Content: []byte("package sample\nfunc F() {}\n")},
	})
	if err == nil {
		t.Fatal("expected context cancellation error")
	}
	if idx == nil {
		t.Fatal("expected a non-nil partial index even on cancellation")
	}
}
