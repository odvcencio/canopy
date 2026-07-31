package contextbundle_test

import (
	"context"
	"flag"
	"os"
	"path/filepath"
	"testing"
	"time"

	"m31labs.dev/canopy/pkg/contextbundle"
	"m31labs.dev/canopy/pkg/contextbundle/lxrender"
	"m31labs.dev/canopy/pkg/index"
	"m31labs.dev/canopy/pkg/model"
)

var updateGolden = flag.Bool("update-golden", false, "rewrite golden bundle fixtures instead of comparing against them")

type fixedProvider struct {
	idx  *model.Index
	snap contextbundle.Snapshot
}

func (p fixedProvider) Snapshot(ctx context.Context, root string) (*model.Index, contextbundle.Snapshot, error) {
	return p.idx, p.snap, nil
}

func (p fixedProvider) Refresh(ctx context.Context, root string, paths []string) (*model.Index, contextbundle.Snapshot, index.BuildStats, error) {
	return p.idx, p.snap, index.BuildStats{}, nil
}

type fixedClock struct{ t time.Time }

func (c fixedClock) Now() time.Time { return c.t }

var goldenFixedTime = time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)

func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
}

func buildIndex(t *testing.T, dir string) *model.Index {
	t.Helper()
	idx, err := index.NewBuilder().BuildPath(dir)
	if err != nil {
		t.Fatalf("BuildPath: %v", err)
	}
	return idx
}

func goldenRequest(root, file, symbol string) contextbundle.Request {
	return contextbundle.Request{
		Root: root,
		Intent: contextbundle.TaskIntent{
			Kind:            contextbundle.TaskImplement,
			OriginalRequest: "implement " + symbol,
			Focus: []contextbundle.Selector{
				{File: file, Symbol: symbol, Required: true},
			},
		},
		Budget:       contextbundle.Budget{TotalTokens: 2000},
		OutputFormat: "markdown",
	}
}

func runGoldenCase(t *testing.T, name, fixtureFile, fixtureContent, symbol string) {
	t.Helper()
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{fixtureFile: fixtureContent})
	idx := buildIndex(t, dir)

	snap := contextbundle.Snapshot{Kind: "imported", ID: "snap_golden_" + name, Root: dir}
	svc := contextbundle.NewService(fixedProvider{idx: idx, snap: snap}, lxrender.Renderer{}, contextbundle.WithClock(fixedClock{t: goldenFixedTime}))

	result, err := svc.Build(context.Background(), goldenRequest(dir, fixtureFile, symbol))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	goldenPath := filepath.Join("testdata", "golden", name+".md")
	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
			t.Fatalf("mkdir golden dir: %v", err)
		}
		if err := os.WriteFile(goldenPath, result.Content, 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		t.Skipf("golden fixture %s rewritten; rerun without -update-golden", goldenPath)
	}

	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden %s: %v (run with -update-golden to create it)", goldenPath, err)
	}
	if string(result.Content) != string(want) {
		t.Errorf("rendered bundle for %s does not match golden fixture\n--- got ---\n%s\n--- want ---\n%s", name, result.Content, want)
	}

	if len(result.Receipt.Items) == 0 {
		t.Fatalf("expected at least one receipt item for %s", name)
	}
	if result.Receipt.ID == "" {
		t.Fatalf("expected non-empty receipt ID for %s", name)
	}
}

func TestGoldenBundle_Go(t *testing.T) {
	runGoldenCase(t, "go", "main.go", `package sample

// Greet returns a friendly greeting for name.
func Greet(name string) string {
	return formatGreeting(name)
}

func formatGreeting(name string) string {
	return "Hello, " + name + "!"
}
`, "Greet")
}

func TestGoldenBundle_TypeScript(t *testing.T) {
	runGoldenCase(t, "typescript", "main.ts", `export function greet(name: string): string {
  return formatGreeting(name);
}

function formatGreeting(name: string): string {
  return "Hello, " + name + "!";
}
`, "greet")
}

func TestGoldenBundle_Python(t *testing.T) {
	runGoldenCase(t, "python", "main.py", `def greet(name):
    return format_greeting(name)


def format_greeting(name):
    return "Hello, " + name + "!"
`, "greet")
}
