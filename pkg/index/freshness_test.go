package index

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"m31labs.dev/canopy/pkg/model"
)

func TestCheckFreshnessDetectsSourceTreeChanges(t *testing.T) {
	tests := []struct {
		name string
		edit func(t *testing.T, root string)
		want FreshnessReport
	}{
		{
			name: "clean",
			edit: func(*testing.T, string) {},
			want: FreshnessReport{},
		},
		{
			name: "edited",
			edit: func(t *testing.T, root string) {
				writeFreshnessSource(t, root, "main.go", "package sample\n\nfunc EditedWithLongerName() {}\n")
			},
			want: FreshnessReport{StaleFiles: []string{"main.go"}},
		},
		{
			name: "missing",
			edit: func(t *testing.T, root string) {
				if err := os.Remove(filepath.Join(root, "main.go")); err != nil {
					t.Fatalf("Remove(main.go) failed: %v", err)
				}
			},
			want: FreshnessReport{MissingFiles: []string{"main.go"}},
		},
		{
			name: "new indexable",
			edit: func(t *testing.T, root string) {
				writeFreshnessSource(t, root, "added.go", "package sample\n\nfunc Added() {}\n")
			},
			want: FreshnessReport{NewFiles: []string{"added.go"}},
		},
		{
			name: "new unsupported",
			edit: func(t *testing.T, root string) {
				writeFreshnessSource(t, root, "notes.unknown", "not source\n")
			},
			want: FreshnessReport{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeFreshnessSource(t, root, "main.go", "package sample\n\nfunc Original() {}\n")
			builder := NewBuilder()
			cached, err := builder.BuildPath(root)
			if err != nil {
				t.Fatalf("BuildPath returned error: %v", err)
			}

			tc.edit(t, root)
			report, err := builder.CheckFreshness(context.Background(), root, cached)
			if err != nil {
				t.Fatalf("CheckFreshness returned error: %v", err)
			}
			if !reflect.DeepEqual(report, tc.want) {
				t.Fatalf("CheckFreshness report = %+v, want %+v", report, tc.want)
			}
			if report.IsFresh() != tc.want.IsFresh() {
				t.Fatalf("IsFresh = %t, want %t", report.IsFresh(), tc.want.IsFresh())
			}
		})
	}
}

func TestCheckFreshnessIgnoresNewExcludedSource(t *testing.T) {
	root := t.TempDir()
	writeFreshnessSource(t, root, ".graftignore", "ignored.go\n")
	writeFreshnessSource(t, root, "main.go", "package sample\n\nfunc Original() {}\n")
	builder, err := NewBuilderWithWorkspaceIgnores(root)
	if err != nil {
		t.Fatalf("NewBuilderWithWorkspaceIgnores returned error: %v", err)
	}
	cached, err := builder.BuildPath(root)
	if err != nil {
		t.Fatalf("BuildPath returned error: %v", err)
	}

	writeFreshnessSource(t, root, "ignored.go", "package sample\n\nfunc Ignored() {}\n")
	report, err := builder.CheckFreshness(context.Background(), root, cached)
	if err != nil {
		t.Fatalf("CheckFreshness returned error: %v", err)
	}
	if !report.IsFresh() {
		t.Fatalf("ignored source made cache stale: %+v", report)
	}
}

func TestEnsureFreshCacheRefreshesAndSaves(t *testing.T) {
	root := t.TempDir()
	sourcePath := filepath.Join(root, "main.go")
	cachePath := filepath.Join(root, ".canopy", "index.json")
	writeFreshnessSource(t, root, "main.go", "package sample\n\nfunc Original() {}\n")
	builder, err := NewBuilderWithWorkspaceIgnores(root)
	if err != nil {
		t.Fatalf("NewBuilderWithWorkspaceIgnores returned error: %v", err)
	}
	cached, err := builder.BuildPath(root)
	if err != nil {
		t.Fatalf("BuildPath returned error: %v", err)
	}
	if err := Save(cachePath, cached); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}

	if err := os.WriteFile(sourcePath, []byte("package sample\n\nfunc RefreshedWithLongerName() {}\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(main.go) failed: %v", err)
	}
	refreshed, status, err := builder.EnsureFreshCache(context.Background(), root, cachePath, cached)
	if err != nil {
		t.Fatalf("EnsureFreshCache returned error: %v", err)
	}
	if status != CacheIncrementallyRefreshed {
		t.Fatalf("status = %q, want %q", status, CacheIncrementallyRefreshed)
	}
	if !indexHasSymbol(refreshed, "RefreshedWithLongerName") {
		t.Fatal("refreshed index does not contain the edited symbol")
	}

	saved, err := Load(cachePath)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if !indexHasSymbol(saved, "RefreshedWithLongerName") {
		t.Fatal("saved cache does not contain the edited symbol")
	}
}

func TestEnsureFreshCacheRebuildsAfterConfigChange(t *testing.T) {
	root := t.TempDir()
	cachePath := filepath.Join(root, ".canopy", "index.json")
	writeFreshnessSource(t, root, "main.go", "package sample\n\nfunc Main() {}\n")
	writeFreshnessSource(t, root, "ignored.go", "package sample\n\nfunc Ignored() {}\n")
	initialBuilder, err := NewBuilderWithWorkspaceIgnores(root)
	if err != nil {
		t.Fatalf("NewBuilderWithWorkspaceIgnores returned error: %v", err)
	}
	cached, err := initialBuilder.BuildPath(root)
	if err != nil {
		t.Fatalf("BuildPath returned error: %v", err)
	}
	if err := Save(cachePath, cached); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}

	writeFreshnessSource(t, root, ".graftignore", "ignored.go\n")
	currentBuilder, err := NewBuilderWithWorkspaceIgnores(root)
	if err != nil {
		t.Fatalf("NewBuilderWithWorkspaceIgnores after config change returned error: %v", err)
	}
	refreshed, status, err := currentBuilder.EnsureFreshCache(context.Background(), root, cachePath, cached)
	if err != nil {
		t.Fatalf("EnsureFreshCache returned error: %v", err)
	}
	if status != CacheFullyRebuilt {
		t.Fatalf("status = %q, want %q", status, CacheFullyRebuilt)
	}
	if refreshed.FileCount() != 1 || indexHasSymbol(refreshed, "Ignored") {
		t.Fatalf("config rebuild retained ignored source: %+v", refreshed.Files)
	}
}

func TestEnsureFreshCacheRebuildsAfterSchemaChange(t *testing.T) {
	root := t.TempDir()
	cachePath := filepath.Join(root, ".canopy", "index.json")
	writeFreshnessSource(t, root, "main.go", "package sample\n\nfunc Main() {}\n")
	builder, err := NewBuilderWithWorkspaceIgnores(root)
	if err != nil {
		t.Fatalf("NewBuilderWithWorkspaceIgnores returned error: %v", err)
	}
	cached, err := builder.BuildPath(root)
	if err != nil {
		t.Fatalf("BuildPath returned error: %v", err)
	}
	cached.Version = "0.2.0"
	for i := range cached.Files {
		cached.Files[i].ParseCoverage = nil
	}
	if err := Save(cachePath, cached); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}

	refreshed, status, err := builder.EnsureFreshCache(context.Background(), root, cachePath, cached)
	if err != nil {
		t.Fatalf("EnsureFreshCache returned error: %v", err)
	}
	if status != CacheFullyRebuilt {
		t.Fatalf("status = %q, want %q", status, CacheFullyRebuilt)
	}
	if refreshed.Version != schemaVersion || len(refreshed.Files) != 1 || refreshed.Files[0].ParseCoverage == nil {
		t.Fatalf("schema rebuild did not restore parse receipts: %+v", refreshed)
	}
}

func writeFreshnessSource(t *testing.T, root, name, source string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, name), []byte(source), 0o644); err != nil {
		t.Fatalf("WriteFile(%s) failed: %v", name, err)
	}
}

func indexHasSymbol(idx *model.Index, name string) bool {
	for _, file := range idx.Files {
		for _, symbol := range file.Symbols {
			if symbol.Name == name {
				return true
			}
		}
	}
	return false
}

func BenchmarkCheckFreshnessWarm1000(b *testing.B) {
	root := b.TempDir()
	files := make([]model.FileSummary, 0, 1000)
	for i := 0; i < 1000; i++ {
		name := fmt.Sprintf("file_%04d.go", i)
		path := filepath.Join(root, name)
		if err := os.WriteFile(path, []byte("package sample\n"), 0o644); err != nil {
			b.Fatalf("WriteFile(%s) failed: %v", name, err)
		}
		info, err := os.Stat(path)
		if err != nil {
			b.Fatalf("Stat(%s) failed: %v", name, err)
		}
		files = append(files, model.FileSummary{
			Path:            name,
			Language:        "go",
			SizeBytes:       info.Size(),
			ModTimeUnixNano: info.ModTime().UnixNano(),
		})
	}
	cached := &model.Index{Root: root, Files: files}
	builder := NewBuilder()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		report, err := builder.CheckFreshness(context.Background(), root, cached)
		if err != nil {
			b.Fatalf("CheckFreshness returned error: %v", err)
		}
		if !report.IsFresh() {
			b.Fatalf("cache became stale: %+v", report)
		}
	}
}
