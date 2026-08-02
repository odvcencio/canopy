package contextbundle

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"m31labs.dev/canopy/pkg/index"
	"m31labs.dev/canopy/pkg/model"
)

// staticIndexProvider returns a fixed index and snapshot, bypassing git and
// on-disk cache discovery so tests are hermetic and independent of the
// environment's git state.
type staticIndexProvider struct {
	idx      *model.Index
	snapshot Snapshot
}

func (p staticIndexProvider) Snapshot(ctx context.Context, root string) (*model.Index, Snapshot, error) {
	return p.idx, p.snapshot, nil
}

func (p staticIndexProvider) Refresh(ctx context.Context, root string, paths []string) (*model.Index, Snapshot, index.BuildStats, error) {
	return p.idx, p.snapshot, index.BuildStats{}, nil
}

// fixedClock returns a constant time, so receipt CreatedAt (and therefore
// receipt ID) is reproducible across test runs.
type fixedClock struct{ t time.Time }

func (c fixedClock) Now() time.Time { return c.t }

var testFixedTime = time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)

// writeFixtureFiles writes files (relative path -> content) under dir.
func writeFixtureFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir for %s: %v", rel, err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
}

// buildFixtureIndex indexes dir with the default builder.
func buildFixtureIndex(t *testing.T, dir string) *model.Index {
	t.Helper()
	builder := index.NewBuilder()
	idx, err := builder.BuildPath(dir)
	if err != nil {
		t.Fatalf("BuildPath(%s): %v", dir, err)
	}
	return idx
}

// newTestService builds a Service over a fixed index/snapshot with the given
// renderer, a fixed clock, and the built-in xref/testmap defaults.
func newTestService(idx *model.Index, snapshot Snapshot, renderer Renderer) *Service {
	provider := staticIndexProvider{idx: idx, snapshot: snapshot}
	return NewService(provider, renderer, WithClock(fixedClock{t: testFixedTime}))
}
