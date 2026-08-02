package index

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
	"time"

	"m31labs.dev/canopy/pkg/ignore"
)

func TestBuildPath_Directory(t *testing.T) {
	tmpDir := t.TempDir()

	source := `package sample

func TestMain() {}
`
	if err := os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte(source), 0o644); err != nil {
		t.Fatalf("WriteFile main.go failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "README.md"), []byte("docs"), 0o644); err != nil {
		t.Fatalf("WriteFile README.md failed: %v", err)
	}

	builder := NewBuilder()
	idx, err := builder.BuildPath(tmpDir)
	if err != nil {
		t.Fatalf("BuildPath returned error: %v", err)
	}

	if idx.FileCount() != 1 {
		t.Fatalf("expected 1 indexed file, got %d", idx.FileCount())
	}
	if len(idx.Errors) != 0 {
		t.Fatalf("expected unsupported README.md to be skipped without parse errors, got %#v", idx.Errors)
	}
	if idx.SymbolCount() != 1 {
		t.Fatalf("expected 1 symbol, got %d", idx.SymbolCount())
	}
	if idx.Files[0].Path != "main.go" {
		t.Fatalf("expected relative path main.go, got %q", idx.Files[0].Path)
	}
}

func TestBuildPathsIndexesOnlySelectedFiles(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmpDir, "nested"), 0o755); err != nil {
		t.Fatalf("MkdirAll nested failed: %v", err)
	}
	write := func(path, source string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(tmpDir, path), []byte(source), 0o644); err != nil {
			t.Fatalf("WriteFile %s failed: %v", path, err)
		}
	}
	write("selected.go", "package sample\n\nfunc Selected() {}\n")
	write("ignored.go", "package sample\n\nfunc Ignored() {}\n")
	write("nested/also_selected.go", "package nested\n\nfunc AlsoSelected() {}\n")
	if runtime.GOOS != "windows" {
		if err := os.Symlink(filepath.Join(tmpDir, "ignored.go"), filepath.Join(tmpDir, "linked.go")); err != nil {
			t.Fatalf("Symlink linked.go failed: %v", err)
		}
	}

	builder := NewBuilder()
	idx, err := builder.BuildPaths(context.Background(), tmpDir, []string{
		"nested/also_selected.go",
		"selected.go",
		"selected.go",
		"deleted.go",
		"linked.go",
	})
	if err != nil {
		t.Fatalf("BuildPaths returned error: %v", err)
	}
	if got, want := idx.FileCount(), 2; got != want {
		t.Fatalf("BuildPaths file count = %d, want %d", got, want)
	}
	gotPaths := []string{idx.Files[0].Path, idx.Files[1].Path}
	wantPaths := []string{"nested/also_selected.go", "selected.go"}
	if !reflect.DeepEqual(gotPaths, wantPaths) {
		t.Fatalf("BuildPaths paths = %v, want %v", gotPaths, wantPaths)
	}
	for _, file := range idx.Files {
		for _, symbol := range file.Symbols {
			if symbol.File != file.Path {
				t.Fatalf("symbol file = %q, want %q", symbol.File, file.Path)
			}
		}
	}
}

func TestBuildPathsRejectsOutsideRoot(t *testing.T) {
	tmpDir := t.TempDir()
	builder := NewBuilder()
	if _, err := builder.BuildPaths(context.Background(), tmpDir, []string{"../outside.go"}); err == nil {
		t.Fatal("BuildPaths accepted a path outside the root")
	}
}

func TestBuildPathsHonorsCancelledContext(t *testing.T) {
	tmpDir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	builder := NewBuilder()
	if _, err := builder.BuildPaths(ctx, tmpDir, []string{"selected.go"}); err != context.Canceled {
		t.Fatalf("BuildPaths error = %v, want context.Canceled", err)
	}
}

func TestShouldSkipIndexPath_UsesIgnoreMatcherForDirectories(t *testing.T) {
	root := filepath.Clean("/repo")
	matcher := ignore.ParsePatterns([]string{"generated/**"})

	if !shouldSkipIndexPath(root, filepath.Join(root, "generated"), true, matcher) {
		t.Fatal("expected generated directory to be skipped")
	}
	if !shouldSkipIndexPath(root, filepath.Join(root, ".cache"), true, matcher) {
		t.Fatal("expected hidden directory to be skipped")
	}
	if shouldSkipIndexPath(root, root, true, matcher) {
		t.Fatal("root directory should not be skipped")
	}
	if shouldSkipIndexPath(root, filepath.Join(root, "internal"), true, matcher) {
		t.Fatal("unexpected skip for unrelated directory")
	}
}

func TestBuildWalkPolicy_UsesShouldSkipDirForDirectoryPruning(t *testing.T) {
	root := filepath.Clean("/repo")
	builder := &Builder{
		ignore: ignore.ParsePatterns([]string{
			"generated/**",
			"third_party/snapshots/",
		}),
	}

	policy := builder.buildWalkPolicy(root, nil)
	if policy.ShouldSkipDir == nil {
		t.Fatal("expected ShouldSkipDir hook")
	}
	if policy.ShouldSkipDir(root) {
		t.Fatal("root directory should not be skipped")
	}
	if !policy.ShouldSkipDir(filepath.Join(root, "generated")) {
		t.Fatal("expected generated directory to be skipped before descent")
	}
	if !policy.ShouldSkipDir(filepath.Join(root, "third_party", "snapshots")) {
		t.Fatal("expected nested directory-only ignore pattern to be skipped before descent")
	}
	if !policy.ShouldSkipDir(filepath.Join(root, ".cache")) {
		t.Fatal("expected hidden directory to be skipped before descent")
	}
	if policy.ShouldSkipDir(filepath.Join(root, "internal")) {
		t.Fatal("unexpected skip for unrelated directory")
	}
}

func TestBuildWalkPolicy_BoundsDefaultConcurrency(t *testing.T) {
	t.Setenv("GTS_MAX_CONCURRENT", "")
	policy := (&Builder{}).buildWalkPolicy(filepath.Clean("/repo"), nil)
	want := runtime.GOMAXPROCS(0)
	if want > 2 {
		want = 2
	}
	if policy.MaxConcurrent != want {
		t.Fatalf("MaxConcurrent = %d, want %d", policy.MaxConcurrent, want)
	}
	if policy.ChannelBuffer != want+1 {
		t.Fatalf("ChannelBuffer = %d, want %d", policy.ChannelBuffer, want+1)
	}

	t.Setenv("GTS_MAX_CONCURRENT", "4")
	policy = (&Builder{}).buildWalkPolicy(filepath.Clean("/repo"), nil)
	if policy.MaxConcurrent != 4 {
		t.Fatalf("explicit MaxConcurrent = %d, want 4", policy.MaxConcurrent)
	}
}

func TestIndexGCEvery_DefaultAndOverrides(t *testing.T) {
	t.Setenv("CANOPY_INDEX_GC_EVERY", "")
	if got := indexGCEvery(); got != defaultIndexGCEvery {
		t.Fatalf("default indexGCEvery = %d, want %d", got, defaultIndexGCEvery)
	}

	t.Setenv("CANOPY_INDEX_GC_EVERY", "0")
	if got := indexGCEvery(); got != 0 {
		t.Fatalf("disabled indexGCEvery = %d, want 0", got)
	}

	t.Setenv("CANOPY_INDEX_GC_EVERY", "invalid")
	if got := indexGCEvery(); got != defaultIndexGCEvery {
		t.Fatalf("invalid indexGCEvery = %d, want %d", got, defaultIndexGCEvery)
	}
}

func TestBuildPath_Directory_MultiLanguageAutoRegistration(t *testing.T) {
	tmpDir := t.TempDir()

	goSource := `package sample

func Main() {}
`
	pythonSource := `class Worker:
    def run(self):
        return 1
`
	if err := os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte(goSource), 0o644); err != nil {
		t.Fatalf("WriteFile main.go failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "worker.py"), []byte(pythonSource), 0o644); err != nil {
		t.Fatalf("WriteFile worker.py failed: %v", err)
	}

	builder := NewBuilder()
	idx, err := builder.BuildPath(tmpDir)
	if err != nil {
		t.Fatalf("BuildPath returned error: %v", err)
	}

	if idx.FileCount() != 2 {
		t.Fatalf("expected 2 indexed files, got %d", idx.FileCount())
	}

	foundPython := false
	for _, file := range idx.Files {
		if file.Path != "worker.py" {
			continue
		}
		foundPython = true
		if file.Language != "python" {
			t.Fatalf("expected python language, got %q", file.Language)
		}
		if len(file.Symbols) == 0 {
			t.Fatal("expected python symbols from tags query")
		}
	}
	if !foundPython {
		t.Fatal("expected worker.py in index output")
	}
}

func TestBuildPath_Directory_InferredTagsLanguage(t *testing.T) {
	tmpDir := t.TempDir()

	jsSource := `class Worker {
	run() {}
}

function main() {
	const w = new Worker()
	w.run()
}
`
	if err := os.WriteFile(filepath.Join(tmpDir, "main.js"), []byte(jsSource), 0o644); err != nil {
		t.Fatalf("WriteFile main.js failed: %v", err)
	}

	builder := NewBuilder()
	idx, err := builder.BuildPath(tmpDir)
	if err != nil {
		t.Fatalf("BuildPath returned error: %v", err)
	}

	if idx.FileCount() != 1 {
		t.Fatalf("expected 1 indexed file, got %d", idx.FileCount())
	}
	file := idx.Files[0]
	if file.Path != "main.js" {
		t.Fatalf("expected main.js, got %q", file.Path)
	}
	if file.Language != "javascript" {
		t.Fatalf("expected javascript language, got %q", file.Language)
	}
	if len(file.Symbols) == 0 {
		t.Fatal("expected javascript symbols from inferred tags query")
	}
}

func TestBuildPathIncremental_ReusesUnchangedFiles(t *testing.T) {
	tmpDir := t.TempDir()

	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(tmpDir, name), []byte(body), 0o644); err != nil {
			t.Fatalf("WriteFile %s failed: %v", name, err)
		}
	}

	write("a.go", "package sample\n\nfunc A() {}\n")
	write("b.go", "package sample\n\nfunc B() {}\n")

	builder := NewBuilder()
	first, firstStats, err := builder.BuildPathIncremental(context.Background(), tmpDir, nil)
	if err != nil {
		t.Fatalf("BuildPathIncremental first returned error: %v", err)
	}
	if firstStats.ParsedFiles != 2 || firstStats.ReusedFiles != 0 {
		t.Fatalf("unexpected first stats: %+v", firstStats)
	}

	second, secondStats, err := builder.BuildPathIncremental(context.Background(), tmpDir, first)
	if err != nil {
		t.Fatalf("BuildPathIncremental second returned error: %v", err)
	}
	if secondStats.ParsedFiles != 0 || secondStats.ReusedFiles != 2 {
		t.Fatalf("unexpected second stats: %+v", secondStats)
	}
	if second.FileCount() != 2 {
		t.Fatalf("expected 2 files, got %d", second.FileCount())
	}
}

func TestBuildPathIncremental_ParsesOnlyChangedFile(t *testing.T) {
	tmpDir := t.TempDir()
	aPath := filepath.Join(tmpDir, "a.go")
	bPath := filepath.Join(tmpDir, "b.go")

	if err := os.WriteFile(aPath, []byte("package sample\n\nfunc A() {}\n"), 0o644); err != nil {
		t.Fatalf("WriteFile a.go failed: %v", err)
	}
	if err := os.WriteFile(bPath, []byte("package sample\n\nfunc B() {}\n"), 0o644); err != nil {
		t.Fatalf("WriteFile b.go failed: %v", err)
	}

	builder := NewBuilder()
	first, _, err := builder.BuildPathIncremental(context.Background(), tmpDir, nil)
	if err != nil {
		t.Fatalf("BuildPathIncremental first returned error: %v", err)
	}

	// Ensure mtime ticks forward so reuse check observes the update.
	time.Sleep(2 * time.Millisecond)
	if err := os.WriteFile(aPath, []byte("package sample\n\nfunc A() {}\nfunc A2() {}\n"), 0o644); err != nil {
		t.Fatalf("WriteFile updated a.go failed: %v", err)
	}

	second, secondStats, err := builder.BuildPathIncremental(context.Background(), tmpDir, first)
	if err != nil {
		t.Fatalf("BuildPathIncremental second returned error: %v", err)
	}
	if secondStats.ParsedFiles != 1 || secondStats.ReusedFiles != 1 {
		t.Fatalf("unexpected second stats: %+v", secondStats)
	}
	if second.SymbolCount() <= first.SymbolCount() {
		t.Fatalf("expected symbol count to increase after change, first=%d second=%d", first.SymbolCount(), second.SymbolCount())
	}
}

func TestBuildPathIncremental_RemovedFileDropsFromIndex(t *testing.T) {
	tmpDir := t.TempDir()
	aPath := filepath.Join(tmpDir, "a.go")
	bPath := filepath.Join(tmpDir, "b.go")

	if err := os.WriteFile(aPath, []byte("package sample\n\nfunc A() {}\n"), 0o644); err != nil {
		t.Fatalf("WriteFile a.go failed: %v", err)
	}
	if err := os.WriteFile(bPath, []byte("package sample\n\nfunc B() {}\n"), 0o644); err != nil {
		t.Fatalf("WriteFile b.go failed: %v", err)
	}

	builder := NewBuilder()
	first, _, err := builder.BuildPathIncremental(context.Background(), tmpDir, nil)
	if err != nil {
		t.Fatalf("BuildPathIncremental first returned error: %v", err)
	}
	if first.FileCount() != 2 {
		t.Fatalf("expected 2 files in first index, got %d", first.FileCount())
	}

	if err := os.Remove(bPath); err != nil {
		t.Fatalf("Remove b.go failed: %v", err)
	}

	second, secondStats, err := builder.BuildPathIncremental(context.Background(), tmpDir, first)
	if err != nil {
		t.Fatalf("BuildPathIncremental second returned error: %v", err)
	}
	if second.FileCount() != 1 {
		t.Fatalf("expected 1 file after removal, got %d", second.FileCount())
	}
	if secondStats.ReusedFiles != 1 || secondStats.ParsedFiles != 0 {
		t.Fatalf("unexpected stats after removal: %+v", secondStats)
	}
}

func TestBuildPath_DirectoryOrderStable(t *testing.T) {
	tmpDir := t.TempDir()

	files := []string{"zeta.go", "alpha.go", "mid.go"}
	for i, name := range files {
		source := fmt.Sprintf(`package sample

func F%d() {}
`, i)
		if err := os.WriteFile(filepath.Join(tmpDir, name), []byte(source), 0o644); err != nil {
			t.Fatalf("WriteFile %s failed: %v", name, err)
		}
	}

	builder := NewBuilder()
	idx, err := builder.BuildPath(tmpDir)
	if err != nil {
		t.Fatalf("BuildPath returned error: %v", err)
	}

	if len(idx.Files) != 3 {
		t.Fatalf("expected 3 files, got %d", len(idx.Files))
	}
	got := []string{idx.Files[0].Path, idx.Files[1].Path, idx.Files[2].Path}
	want := []string{"alpha.go", "mid.go", "zeta.go"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected file order got=%v want=%v", got, want)
	}
}

func BenchmarkBuildPath_Directory(b *testing.B) {
	tmpDir := b.TempDir()

	for i := 0; i < 300; i++ {
		filePath := filepath.Join(tmpDir, fmt.Sprintf("f%03d.go", i))
		source := fmt.Sprintf(`package sample

type Type%03d struct{}

func Func%03d() int { return %d }
`, i, i, i)
		if err := os.WriteFile(filePath, []byte(source), 0o644); err != nil {
			b.Fatalf("WriteFile failed: %v", err)
		}
	}

	builder := NewBuilder()
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		idx, err := builder.BuildPath(tmpDir)
		if err != nil {
			b.Fatalf("BuildPath returned error: %v", err)
		}
		if idx.FileCount() != 300 {
			b.Fatalf("expected 300 files, got %d", idx.FileCount())
		}
	}
}

func BenchmarkBuildPathIncremental_Warm(b *testing.B) {
	tmpDir := b.TempDir()

	for i := 0; i < 300; i++ {
		filePath := filepath.Join(tmpDir, fmt.Sprintf("f%03d.go", i))
		source := fmt.Sprintf(`package sample

type Type%03d struct{}

func Func%03d() int { return %d }
`, i, i, i)
		if err := os.WriteFile(filePath, []byte(source), 0o644); err != nil {
			b.Fatalf("WriteFile failed: %v", err)
		}
	}

	builder := NewBuilder()
	base, _, err := builder.BuildPathIncremental(context.Background(), tmpDir, nil)
	if err != nil {
		b.Fatalf("initial BuildPathIncremental returned error: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		next, _, err := builder.BuildPathIncremental(context.Background(), tmpDir, base)
		if err != nil {
			b.Fatalf("BuildPathIncremental returned error: %v", err)
		}
		if next.FileCount() != 300 {
			b.Fatalf("expected 300 files, got %d", next.FileCount())
		}
		base = next
	}
}
