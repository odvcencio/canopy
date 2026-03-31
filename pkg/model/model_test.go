package model

import (
	"testing"
	"time"
)

func TestIndexFileCount(t *testing.T) {
	t.Run("empty index", func(t *testing.T) {
		idx := &Index{}
		if got := idx.FileCount(); got != 0 {
			t.Errorf("FileCount() = %d, want 0", got)
		}
	})

	t.Run("index with files", func(t *testing.T) {
		idx := &Index{
			Files: []FileSummary{
				{Path: "a.go", Language: "go"},
				{Path: "b.go", Language: "go"},
				{Path: "c.py", Language: "python"},
			},
		}
		if got := idx.FileCount(); got != 3 {
			t.Errorf("FileCount() = %d, want 3", got)
		}
	})
}

func TestIndexSymbolCount(t *testing.T) {
	idx := &Index{
		Files: []FileSummary{
			{
				Path:     "a.go",
				Language: "go",
				Symbols: []Symbol{
					{File: "a.go", Kind: "function", Name: "Foo", StartLine: 1, EndLine: 5},
					{File: "a.go", Kind: "function", Name: "Bar", StartLine: 7, EndLine: 12},
				},
			},
			{
				Path:     "b.go",
				Language: "go",
				Symbols: []Symbol{
					{File: "b.go", Kind: "type", Name: "Baz", StartLine: 1, EndLine: 10},
				},
			},
			{
				Path:     "c.go",
				Language: "go",
				Symbols: nil,
			},
		},
	}
	if got := idx.SymbolCount(); got != 3 {
		t.Errorf("SymbolCount() = %d, want 3", got)
	}
}

func TestIndexReferenceCount(t *testing.T) {
	idx := &Index{
		Files: []FileSummary{
			{
				Path:     "a.go",
				Language: "go",
				References: []Reference{
					{File: "a.go", Kind: "call", Name: "fmt.Println", StartLine: 3, EndLine: 3},
				},
			},
			{
				Path:     "b.go",
				Language: "go",
				References: []Reference{
					{File: "b.go", Kind: "call", Name: "os.Exit", StartLine: 5, EndLine: 5},
					{File: "b.go", Kind: "import", Name: "os", StartLine: 1, EndLine: 1},
				},
			},
			{
				Path:       "c.go",
				Language:   "go",
				References: nil,
			},
		},
	}
	if got := idx.ReferenceCount(); got != 3 {
		t.Errorf("ReferenceCount() = %d, want 3", got)
	}
}

func TestIndexNilSafety(t *testing.T) {
	t.Run("nil index FileCount", func(t *testing.T) {
		var idx *Index
		if got := idx.FileCount(); got != 0 {
			t.Errorf("FileCount() on nil = %d, want 0", got)
		}
	})

	t.Run("nil index SymbolCount", func(t *testing.T) {
		var idx *Index
		if got := idx.SymbolCount(); got != 0 {
			t.Errorf("SymbolCount() on nil = %d, want 0", got)
		}
	})

	t.Run("nil index ReferenceCount", func(t *testing.T) {
		var idx *Index
		if got := idx.ReferenceCount(); got != 0 {
			t.Errorf("ReferenceCount() on nil = %d, want 0", got)
		}
	})

	t.Run("nil Files slice", func(t *testing.T) {
		idx := &Index{Files: nil}
		if got := idx.FileCount(); got != 0 {
			t.Errorf("FileCount() with nil Files = %d, want 0", got)
		}
		if got := idx.SymbolCount(); got != 0 {
			t.Errorf("SymbolCount() with nil Files = %d, want 0", got)
		}
		if got := idx.ReferenceCount(); got != 0 {
			t.Errorf("ReferenceCount() with nil Files = %d, want 0", got)
		}
	})

	t.Run("empty FileSummary", func(t *testing.T) {
		idx := &Index{
			Files: []FileSummary{{}},
		}
		if got := idx.FileCount(); got != 1 {
			t.Errorf("FileCount() with empty FileSummary = %d, want 1", got)
		}
		if got := idx.SymbolCount(); got != 0 {
			t.Errorf("SymbolCount() with empty FileSummary = %d, want 0", got)
		}
		if got := idx.ReferenceCount(); got != 0 {
			t.Errorf("ReferenceCount() with empty FileSummary = %d, want 0", got)
		}
	})
}

func TestSymbolFields(t *testing.T) {
	sym := Symbol{
		File:      "pkg/model/model.go",
		Kind:      "method",
		Name:      "FileCount",
		Signature: "func (idx *Index) FileCount() int",
		Receiver:  "Index",
		StartLine: 49,
		EndLine:   54,
	}

	if sym.File != "pkg/model/model.go" {
		t.Errorf("File = %q, want %q", sym.File, "pkg/model/model.go")
	}
	if sym.Kind != "method" {
		t.Errorf("Kind = %q, want %q", sym.Kind, "method")
	}
	if sym.Name != "FileCount" {
		t.Errorf("Name = %q, want %q", sym.Name, "FileCount")
	}
	if sym.Signature != "func (idx *Index) FileCount() int" {
		t.Errorf("Signature = %q, want %q", sym.Signature, "func (idx *Index) FileCount() int")
	}
	if sym.Receiver != "Index" {
		t.Errorf("Receiver = %q, want %q", sym.Receiver, "Index")
	}
	if sym.StartLine != 49 {
		t.Errorf("StartLine = %d, want %d", sym.StartLine, 49)
	}
	if sym.EndLine != 54 {
		t.Errorf("EndLine = %d, want %d", sym.EndLine, 54)
	}
}

func TestReferenceFields(t *testing.T) {
	ref := Reference{
		File:        "cmd/main.go",
		Kind:        "call",
		Name:        "model.NewIndex",
		StartLine:   15,
		EndLine:     15,
		StartColumn: 8,
		EndColumn:   23,
	}

	if ref.File != "cmd/main.go" {
		t.Errorf("File = %q, want %q", ref.File, "cmd/main.go")
	}
	if ref.Kind != "call" {
		t.Errorf("Kind = %q, want %q", ref.Kind, "call")
	}
	if ref.Name != "model.NewIndex" {
		t.Errorf("Name = %q, want %q", ref.Name, "model.NewIndex")
	}
	if ref.StartLine != 15 {
		t.Errorf("StartLine = %d, want %d", ref.StartLine, 15)
	}
	if ref.EndLine != 15 {
		t.Errorf("EndLine = %d, want %d", ref.EndLine, 15)
	}
	if ref.StartColumn != 8 {
		t.Errorf("StartColumn = %d, want %d", ref.StartColumn, 8)
	}
	if ref.EndColumn != 23 {
		t.Errorf("EndColumn = %d, want %d", ref.EndColumn, 23)
	}
}

func TestWithoutGenerated(t *testing.T) {
	idx := &Index{
		Files: []FileSummary{
			{Path: "main.go", Language: "go"},
			{Path: "user.pb.go", Language: "go", Generated: &GeneratedInfo{Generator: "protobuf", Reason: "filename"}},
			{Path: "handler.go", Language: "go"},
			{Path: "mock_repo.go", Language: "go", Generated: &GeneratedInfo{Generator: "mockgen", Reason: "marker"}},
		},
	}

	filtered := idx.WithoutGenerated()

	if len(filtered.Files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(filtered.Files))
	}
	if filtered.Files[0].Path != "main.go" {
		t.Errorf("expected main.go, got %s", filtered.Files[0].Path)
	}
	if filtered.Files[1].Path != "handler.go" {
		t.Errorf("expected handler.go, got %s", filtered.Files[1].Path)
	}

	// Original index unchanged
	if len(idx.Files) != 4 {
		t.Fatalf("original index modified: expected 4 files, got %d", len(idx.Files))
	}
}

func TestWithoutGenerated_NilIndex(t *testing.T) {
	var idx *Index
	result := idx.WithoutGenerated()
	if result != nil {
		t.Fatalf("expected nil, got %+v", result)
	}
}

func TestWithoutGenerated_NoGenerated(t *testing.T) {
	idx := &Index{
		Files: []FileSummary{
			{Path: "main.go", Language: "go"},
		},
	}
	filtered := idx.WithoutGenerated()
	if len(filtered.Files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(filtered.Files))
	}
}

func TestGeneratedFileCount(t *testing.T) {
	idx := &Index{
		Files: []FileSummary{
			{Path: "main.go"},
			{Path: "user.pb.go", Generated: &GeneratedInfo{Generator: "protobuf"}},
			{Path: "handler.go"},
		},
	}
	if idx.GeneratedFileCount() != 1 {
		t.Fatalf("expected 1, got %d", idx.GeneratedFileCount())
	}
}

func TestGeneratedFileCount_NilIndex(t *testing.T) {
	var idx *Index
	if idx.GeneratedFileCount() != 0 {
		t.Fatalf("expected 0 for nil index")
	}
}

// Ensure the time import is used; also serves as a basic smoke test for Index fields.
func TestIndexFields(t *testing.T) {
	now := time.Now()
	idx := &Index{
		Version:     "1.0",
		Root:        "/home/user/project",
		GeneratedAt: now,
		Files:       []FileSummary{{Path: "main.go", Language: "go"}},
		Errors:      []ParseError{{Path: "bad.go", Error: "syntax error"}},
	}
	if idx.Version != "1.0" {
		t.Errorf("Version = %q, want %q", idx.Version, "1.0")
	}
	if idx.Root != "/home/user/project" {
		t.Errorf("Root = %q, want %q", idx.Root, "/home/user/project")
	}
	if !idx.GeneratedAt.Equal(now) {
		t.Errorf("GeneratedAt = %v, want %v", idx.GeneratedAt, now)
	}
	if len(idx.Errors) != 1 {
		t.Errorf("len(Errors) = %d, want 1", len(idx.Errors))
	}
}
