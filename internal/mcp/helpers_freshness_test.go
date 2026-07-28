package mcp

import (
	"os"
	"path/filepath"
	"testing"

	"m31labs.dev/canopy/pkg/index"
	"m31labs.dev/canopy/pkg/model"
)

type mcpIndexLoader func(service *Service, root, cachePath string) (*model.Index, error)

func TestAutoDiscoveredCacheRefreshesAfterSourceEdit(t *testing.T) {
	forEachMCPIndexLoader(t, func(t *testing.T, load mcpIndexLoader) {
		root := t.TempDir()
		writeMCPFreshnessSource(t, root, "main.go", "package sample\n\nfunc CachedVersion() {}\n")
		saveMCPFreshnessCache(t, root, filepath.Join(root, ".canopy", "index.json"))

		writeMCPFreshnessSource(t, root, "main.go", "package sample\n\nfunc EditedVersionWithLongerName() {}\n")

		loaded, err := load(NewService(root, ""), root, "")
		if err != nil {
			t.Fatalf("load auto-discovered cache: %v", err)
		}
		requireMCPIndexSymbol(t, loaded, "EditedVersionWithLongerName", true)
		requireMCPIndexSymbol(t, loaded, "CachedVersion", false)
	})
}

func TestAutoDiscoveredCacheRefreshesAfterSourceFileAdded(t *testing.T) {
	forEachMCPIndexLoader(t, func(t *testing.T, load mcpIndexLoader) {
		root := t.TempDir()
		writeMCPFreshnessSource(t, root, "main.go", "package sample\n\nfunc ExistingVersion() {}\n")
		saveMCPFreshnessCache(t, root, filepath.Join(root, ".canopy", "index.json"))

		writeMCPFreshnessSource(t, root, "added.go", "package sample\n\nfunc AddedVersion() {}\n")

		loaded, err := load(NewService(root, ""), root, "")
		if err != nil {
			t.Fatalf("load auto-discovered cache: %v", err)
		}
		requireMCPIndexSymbol(t, loaded, "ExistingVersion", true)
		requireMCPIndexSymbol(t, loaded, "AddedVersion", true)
	})
}

func TestExplicitCachePathRemainsSnapshot(t *testing.T) {
	forEachMCPIndexLoader(t, func(t *testing.T, load mcpIndexLoader) {
		testDir := t.TempDir()
		root := filepath.Join(testDir, "repo")
		if err := os.MkdirAll(root, 0o755); err != nil {
			t.Fatalf("create source root: %v", err)
		}
		writeMCPFreshnessSource(t, root, "main.go", "package sample\n\nfunc SnapshotVersion() {}\n")
		cachePath := filepath.Join(testDir, "snapshots", "index.json")
		saveMCPFreshnessCache(t, root, cachePath)

		writeMCPFreshnessSource(t, root, "main.go", "package sample\n\nfunc LiveVersionWithLongerName() {}\n")
		writeMCPFreshnessSource(t, root, "added.go", "package sample\n\nfunc AddedAfterSnapshot() {}\n")

		loaded, err := load(NewService(root, cachePath), root, cachePath)
		if err != nil {
			t.Fatalf("load explicit cache: %v", err)
		}
		requireMCPIndexSymbol(t, loaded, "SnapshotVersion", true)
		requireMCPIndexSymbol(t, loaded, "LiveVersionWithLongerName", false)
		requireMCPIndexSymbol(t, loaded, "AddedAfterSnapshot", false)
	})
}

func forEachMCPIndexLoader(t *testing.T, test func(*testing.T, mcpIndexLoader)) {
	t.Helper()
	loaders := map[string]mcpIndexLoader{
		"loadOrBuild": func(service *Service, root, cachePath string) (*model.Index, error) {
			return service.loadOrBuild(cachePath, root)
		},
		"loadIndexFromSource": func(service *Service, root, cachePath string) (*model.Index, error) {
			return service.loadIndexFromSource(root, cachePath)
		},
	}
	for name, load := range loaders {
		t.Run(name, func(t *testing.T) {
			test(t, load)
		})
	}
}

func writeMCPFreshnessSource(t *testing.T, root, name, source string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatalf("write source %s: %v", name, err)
	}
}

func saveMCPFreshnessCache(t *testing.T, root, cachePath string) {
	t.Helper()
	builder, err := index.NewBuilderWithWorkspaceIgnores(root)
	if err != nil {
		t.Fatalf("create index builder: %v", err)
	}
	idx, err := builder.BuildPath(root)
	if err != nil {
		t.Fatalf("build index: %v", err)
	}
	if err := index.Save(cachePath, idx); err != nil {
		t.Fatalf("save index: %v", err)
	}
}

func requireMCPIndexSymbol(t *testing.T, idx *model.Index, name string, want bool) {
	t.Helper()
	found := false
	for _, file := range idx.Files {
		for _, symbol := range file.Symbols {
			if symbol.Name == name {
				found = true
				break
			}
		}
	}
	if found != want {
		t.Fatalf("symbol %q presence = %t, want %t", name, found, want)
	}
}
