package federation

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/odvcencio/canopy/pkg/model"
)

func TestSaveAndLoadFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.canopyindex")

	exported := &ExportedIndex{
		RepoURL:    "https://github.com/example/repo",
		RepoName:   "repo",
		CommitSHA:  "abc123",
		ExportedAt: time.Now().Truncate(time.Second),
		Index: model.Index{
			Version: "1",
			Root:    "/tmp/repo",
			Files: []model.FileSummary{
				{
					Path:     "main.go",
					Language: "go",
					Imports:  []string{"fmt", "os"},
					Symbols: []model.Symbol{
						{File: "main.go", Kind: "function", Name: "main", StartLine: 1, EndLine: 10},
					},
				},
			},
		},
	}

	if err := Save(path, exported); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}

	if loaded.RepoName != "repo" {
		t.Errorf("RepoName = %q, want %q", loaded.RepoName, "repo")
	}
	if loaded.CommitSHA != "abc123" {
		t.Errorf("CommitSHA = %q, want %q", loaded.CommitSHA, "abc123")
	}
	if len(loaded.Index.Files) != 1 {
		t.Errorf("files = %d, want 1", len(loaded.Index.Files))
	}
	if loaded.Index.Files[0].Path != "main.go" {
		t.Errorf("file path = %q, want %q", loaded.Index.Files[0].Path, "main.go")
	}
}

func TestLoadDir(t *testing.T) {
	dir := t.TempDir()

	for _, name := range []string{"alpha", "beta"} {
		exported := &ExportedIndex{
			RepoName:   name,
			ExportedAt: time.Now(),
			Index: model.Index{
				Version: "1",
				Files: []model.FileSummary{
					{Path: "lib.go", Language: "go", Imports: []string{"fmt"}},
				},
			},
		}
		if err := Save(filepath.Join(dir, name+".canopyindex"), exported); err != nil {
			t.Fatalf("Save %s: %v", name, err)
		}
	}

	fi, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if len(fi.Indexes) != 2 {
		t.Fatalf("indexes = %d, want 2", len(fi.Indexes))
	}
	if fi.Indexes[0].RepoName != "alpha" {
		t.Errorf("first repo = %q, want %q", fi.Indexes[0].RepoName, "alpha")
	}
	if fi.Indexes[1].RepoName != "beta" {
		t.Errorf("second repo = %q, want %q", fi.Indexes[1].RepoName, "beta")
	}

	if fi.Merged == nil {
		t.Fatal("Merged is nil")
	}
	if len(fi.Merged.Files) != 2 {
		t.Fatalf("merged files = %d, want 2", len(fi.Merged.Files))
	}
	if fi.Merged.Files[0].Path != "alpha:lib.go" {
		t.Errorf("merged path = %q, want %q", fi.Merged.Files[0].Path, "alpha:lib.go")
	}
}

func TestLoadDirEmpty(t *testing.T) {
	dir := t.TempDir()
	_, err := Load(dir)
	if err == nil {
		t.Fatal("expected error for empty directory")
	}
}

func TestMergePrefixesSymbolsAndRefs(t *testing.T) {
	fi := &FederatedIndex{
		Indexes: []ExportedEntry{
			{
				RepoName: "myrepo",
				Index: &model.Index{
					Files: []model.FileSummary{
						{
							Path:     "pkg/foo.go",
							Language: "go",
							Symbols: []model.Symbol{
								{File: "pkg/foo.go", Kind: "function", Name: "Foo", StartLine: 1, EndLine: 5},
							},
							References: []model.Reference{
								{File: "pkg/foo.go", Kind: "call", Name: "Bar", StartLine: 3, EndLine: 3},
							},
						},
					},
					Errors: []model.ParseError{
						{Path: "bad.go", Error: "syntax error"},
					},
				},
			},
		},
	}

	merged := fi.merge()

	if len(merged.Files) != 1 {
		t.Fatalf("files = %d, want 1", len(merged.Files))
	}
	f := merged.Files[0]
	if f.Path != "myrepo:pkg/foo.go" {
		t.Errorf("path = %q, want %q", f.Path, "myrepo:pkg/foo.go")
	}
	if len(f.Symbols) != 1 || f.Symbols[0].File != "myrepo:pkg/foo.go" {
		t.Errorf("symbol file = %q, want %q", f.Symbols[0].File, "myrepo:pkg/foo.go")
	}
	if len(f.References) != 1 || f.References[0].File != "myrepo:pkg/foo.go" {
		t.Errorf("ref file = %q, want %q", f.References[0].File, "myrepo:pkg/foo.go")
	}
	if len(merged.Errors) != 1 || merged.Errors[0].Path != "myrepo:bad.go" {
		t.Errorf("error path = %q, want %q", merged.Errors[0].Path, "myrepo:bad.go")
	}
}

func TestBuildServiceGraph(t *testing.T) {
	// Create two repos: "client" imports from "lib"'s module.
	// We'll use module detection from go.mod.
	libDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(libDir, "go.mod"), []byte("module github.com/example/lib\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	clientDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(clientDir, "go.mod"), []byte("module github.com/example/client\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	fi := &FederatedIndex{
		Indexes: []ExportedEntry{
			{
				RepoName: "lib",
				Index: &model.Index{
					Root: libDir,
					Files: []model.FileSummary{
						{Path: "pkg/util.go", Language: "go", Imports: []string{"fmt"}},
					},
				},
			},
			{
				RepoName: "client",
				Index: &model.Index{
					Root: clientDir,
					Files: []model.FileSummary{
						{
							Path:     "main.go",
							Language: "go",
							Imports:  []string{"github.com/example/lib/pkg"},
						},
					},
				},
			},
		},
	}

	report := BuildServiceGraph(fi)

	if len(report.Repos) != 2 {
		t.Fatalf("repos = %d, want 2", len(report.Repos))
	}

	if len(report.Edges) != 1 {
		t.Fatalf("edges = %d, want 1", len(report.Edges))
	}
	e := report.Edges[0]
	if e.From != "client" || e.To != "lib" {
		t.Errorf("edge = %s -> %s, want client -> lib", e.From, e.To)
	}
	if e.ImportPath != "github.com/example/lib/pkg" {
		t.Errorf("import = %q, want %q", e.ImportPath, "github.com/example/lib/pkg")
	}
}

func TestBuildServiceGraphNoEdges(t *testing.T) {
	fi := &FederatedIndex{
		Indexes: []ExportedEntry{
			{
				RepoName: "alpha",
				Index: &model.Index{
					Files: []model.FileSummary{
						{Path: "main.go", Language: "go", Imports: []string{"fmt"}},
					},
				},
			},
		},
	}

	report := BuildServiceGraph(fi)
	if len(report.Edges) != 0 {
		t.Errorf("edges = %d, want 0", len(report.Edges))
	}
}

func TestDetectModuleFromGoMod(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module github.com/test/mod\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	idx := &model.Index{Root: dir}
	mod := detectModule(idx)
	if mod != "github.com/test/mod" {
		t.Errorf("module = %q, want %q", mod, "github.com/test/mod")
	}
}

func TestDetectModuleEmpty(t *testing.T) {
	idx := &model.Index{}
	mod := detectModule(idx)
	if mod != "" {
		t.Errorf("module = %q, want empty", mod)
	}
}
