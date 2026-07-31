package contextbundle

import (
	"context"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"m31labs.dev/canopy/pkg/index"
	"m31labs.dev/canopy/pkg/model"
	"m31labs.dev/canopy/pkg/testmap"
	"m31labs.dev/canopy/pkg/xref"
)

// WorkspaceTTL is how long a warm workspace remains cached after its last
// reference before it becomes eligible for reload on next access (spec 10.4).
const WorkspaceTTL = 15 * time.Minute

// Workspace holds one refreshable index and call graph for a canonical root
// (spec 10.4).
type Workspace struct {
	Root      string
	Index     *model.Index
	Graph     *xref.Graph
	Snapshot  Snapshot
	Refreshed time.Time

	mu      sync.RWMutex
	refresh singleflight.Group
}

func (w *Workspace) snapshotState() (*model.Index, *xref.Graph, Snapshot, time.Time) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.Index, w.Graph, w.Snapshot, w.Refreshed
}

func (w *Workspace) setState(idx *model.Index, graph *xref.Graph, snap Snapshot) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.Index = idx
	w.Graph = graph
	w.Snapshot = snap
	w.Refreshed = time.Now()
}

// WorkspaceManager is the production IndexProvider and GraphProvider. It
// maintains one refreshable Workspace per canonical root and uses
// singleflight so concurrent callers never build duplicate indexes for the
// same root (spec 10.3, 10.4).
type WorkspaceManager struct {
	mu         sync.Mutex
	workspaces map[string]*Workspace
	excludes   []string
}

// NewWorkspaceManager constructs an empty manager. excludePatterns are
// gitignore-style patterns applied on top of workspace ignore rules, matching
// the CLI's --exclude flag.
func NewWorkspaceManager(excludePatterns ...string) *WorkspaceManager {
	return &WorkspaceManager{
		workspaces: make(map[string]*Workspace),
		excludes:   excludePatterns,
	}
}

func canonicalRoot(root string) (string, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	return filepath.Clean(abs), nil
}

func (m *WorkspaceManager) getOrCreate(root string) *Workspace {
	m.mu.Lock()
	defer m.mu.Unlock()
	ws, ok := m.workspaces[root]
	if !ok {
		ws = &Workspace{Root: root}
		m.workspaces[root] = ws
	}
	return ws
}

// Snapshot returns the current index and snapshot identity for root,
// rebuilding only when the workspace is missing, past its TTL, or the
// worktree/config snapshot digest has changed (spec 10.4, 10.5).
func (m *WorkspaceManager) Snapshot(ctx context.Context, root string) (*model.Index, Snapshot, error) {
	canonical, err := canonicalRoot(root)
	if err != nil {
		return nil, Snapshot{}, err
	}
	ws := m.getOrCreate(canonical)

	current, err := ComputeSnapshot(canonical)
	if err != nil {
		return nil, Snapshot{}, err
	}

	cachedIdx, cachedGraph, cachedSnap, refreshed := ws.snapshotState()
	fresh := cachedIdx != nil && cachedSnap.ID == current.ID && time.Since(refreshed) < WorkspaceTTL
	if fresh {
		if cachedGraph == nil {
			graph, err := xref.Build(cachedIdx)
			if err != nil {
				return nil, Snapshot{}, err
			}
			ws.setState(cachedIdx, &graph, cachedSnap)
			return cachedIdx, cachedSnap, nil
		}
		return cachedIdx, cachedSnap, nil
	}

	result, err, _ := ws.refresh.Do("snapshot", func() (any, error) {
		// Re-check inside the singleflight critical section: another caller
		// may have already refreshed while we waited to acquire it.
		idx2, _, snap2, refreshed2 := ws.snapshotState()
		if idx2 != nil && snap2.ID == current.ID && time.Since(refreshed2) < WorkspaceTTL {
			return idx2, nil
		}

		idx, err := m.loadOrBuildIndex(canonical, cachedIdx)
		if err != nil {
			return nil, err
		}
		graph, err := xref.Build(idx)
		if err != nil {
			return nil, err
		}
		snap, err := ComputeSnapshot(canonical)
		if err != nil {
			return nil, err
		}
		ws.setState(idx, &graph, snap)
		return idx, nil
	})
	if err != nil {
		return nil, Snapshot{}, err
	}
	idx := result.(*model.Index)
	_, _, snap, _ := ws.snapshotState()
	return idx, snap, nil
}

// Refresh rebuilds the index for root, reusing unchanged files, and returns
// build statistics. Callers SHOULD invoke this immediately after a tool
// reports a file edit (spec 10.4).
func (m *WorkspaceManager) Refresh(ctx context.Context, root string, paths []string) (*model.Index, Snapshot, index.BuildStats, error) {
	canonical, err := canonicalRoot(root)
	if err != nil {
		return nil, Snapshot{}, index.BuildStats{}, err
	}
	ws := m.getOrCreate(canonical)

	result, err, _ := ws.refresh.Do("refresh", func() (any, error) {
		cachedIdx, _, _, _ := ws.snapshotState()
		builder, err := m.newBuilder(canonical)
		if err != nil {
			return nil, err
		}
		idx, stats, err := builder.BuildPathIncrementalWithOptions(ctx, canonical, cachedIdx, index.BuildOptions{})
		if err != nil {
			return nil, err
		}
		graph, err := xref.Build(idx)
		if err != nil {
			return nil, err
		}
		snap, err := ComputeSnapshot(canonical)
		if err != nil {
			return nil, err
		}
		ws.setState(idx, &graph, snap)
		return refreshResult{idx: idx, stats: stats}, nil
	})
	if err != nil {
		return nil, Snapshot{}, index.BuildStats{}, err
	}
	rr := result.(refreshResult)
	_, _, snap, _ := ws.snapshotState()
	return rr.idx, snap, rr.stats, nil
}

type refreshResult struct {
	idx   *model.Index
	stats index.BuildStats
}

// Graph implements GraphProvider by returning the cached graph for idx's
// root, rebuilding only if the workspace has no cached graph for that exact
// index (spec 10.4: "the xref graph MUST be rebuilt only when the index
// changes").
func (m *WorkspaceManager) Graph(ctx context.Context, idx *model.Index) (*xref.Graph, error) {
	if idx == nil {
		return nil, ErrIndexNotFound
	}
	canonical, err := canonicalRoot(idx.Root)
	if err == nil {
		m.mu.Lock()
		ws, ok := m.workspaces[canonical]
		m.mu.Unlock()
		if ok {
			cachedIdx, cachedGraph, _, _ := ws.snapshotState()
			if cachedGraph != nil && cachedIdx == idx {
				return cachedGraph, nil
			}
		}
	}
	graph, err := xref.Build(idx)
	if err != nil {
		return nil, err
	}
	return &graph, nil
}

func (m *WorkspaceManager) newBuilder(root string) (*index.Builder, error) {
	if len(m.excludes) > 0 {
		return index.NewBuilderWithWorkspaceIgnoresAndExtras(root, m.excludes)
	}
	return index.NewBuilderWithWorkspaceIgnores(root)
}

// loadOrBuildIndex mirrors the CLI auto-discovery in cmd/canopy/helpers.go:
// prefer a fresh on-disk cache, rebuild when the config digest changed, and
// fall back to a full parse only when necessary. previous, if non-nil, seeds
// incremental reuse when a rebuild is required.
func (m *WorkspaceManager) loadOrBuildIndex(root string, previous *model.Index) (*model.Index, error) {
	autoPath := filepath.Join(root, ".canopy", "index.json")
	if idx, ok := tryLoadCachedIndex(root, autoPath); ok {
		return applyExcludes(idx, m.excludes), nil
	}

	builder, err := m.newBuilder(root)
	if err != nil {
		return nil, err
	}
	idx, _, err := builder.BuildPathIncrementalWithOptions(context.Background(), root, previous, index.BuildOptions{})
	if err != nil {
		return nil, err
	}
	return idx, nil
}

func tryLoadCachedIndex(root, autoPath string) (*model.Index, bool) {
	idx, err := index.LoadLenient(autoPath)
	if err != nil {
		return nil, false
	}
	if idx.ConfigHashes == nil {
		return idx, true
	}
	current, err := index.ComputeConfigHashes(root)
	if err != nil {
		return idx, true
	}
	if !configHashesEqual(idx.ConfigHashes, current) {
		return nil, false
	}
	return idx, true
}

func configHashesEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func applyExcludes(idx *model.Index, excludes []string) *model.Index {
	if len(excludes) == 0 {
		return idx
	}
	return idx.ExcludePaths(excludes)
}

// testmapMapper is the default TestMapper, backed by pkg/testmap. entities
// are xref.Definition IDs; results are resolved back to full model.Symbol
// values (with real line spans) via the graph, not the coarse TestRef the
// underlying package returns.
type testmapMapper struct{}

func (testmapMapper) RelatedTests(ctx context.Context, idx *model.Index, entities []string) ([]model.Symbol, error) {
	if idx == nil || len(entities) == 0 {
		return nil, nil
	}
	graph, err := xref.Build(idx)
	if err != nil {
		return nil, err
	}
	wanted := make(map[string]bool, len(entities))
	for _, e := range entities {
		wanted[e] = true
	}

	wantedKeys := make(map[string]bool)
	for _, def := range graph.Definitions {
		if wanted[def.ID] {
			wantedKeys[testMapKey(def.File, def.Kind, def.Name, def.StartLine)] = true
		}
	}
	if len(wantedKeys) == 0 {
		return nil, nil
	}

	report, err := testmap.Map(idx, testmap.Options{})
	if err != nil {
		return nil, err
	}

	// Index callable definitions by file+name so test hits (which carry no
	// line span) resolve to a real, renderable Symbol.
	byFileName := make(map[string]xref.Definition, len(graph.Definitions))
	for _, def := range graph.Definitions {
		if !def.Callable {
			continue
		}
		byFileName[def.File+"\x00"+def.Name] = def
	}

	var out []model.Symbol
	seen := map[string]bool{}
	for _, mapping := range report.Mappings {
		key := testMapKey(mapping.File, mapping.Kind, mapping.Symbol, mapping.StartLine)
		if !wantedKeys[key] {
			continue
		}
		for _, t := range mapping.Tests {
			dedupKey := t.File + "\x00" + t.Name
			if seen[dedupKey] {
				continue
			}
			seen[dedupKey] = true
			if def, ok := byFileName[dedupKey]; ok {
				out = append(out, model.Symbol{
					File:      def.File,
					Kind:      def.Kind,
					Name:      def.Name,
					Signature: def.Signature,
					StartLine: def.StartLine,
					EndLine:   def.EndLine,
				})
			}
		}
	}
	return out, nil
}

func testMapKey(file, kind, name string, startLine int) string {
	return file + "\x00" + kind + "\x00" + name + "\x00" + itoa(startLine)
}
