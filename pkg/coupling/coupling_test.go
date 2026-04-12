package coupling

import (
	"math"
	"testing"

	"github.com/odvcencio/canopy/pkg/model"
	"github.com/odvcencio/canopy/pkg/xref"
)

// helper to build a minimal xref.Graph from definitions and edges without disk IO.
func makeGraph(defs []xref.Definition, edges []xref.Edge) xref.Graph {
	return xref.Graph{
		Definitions: defs,
		Edges:       edges,
	}
}

func TestAnalyzeNilIndex(t *testing.T) {
	graph := makeGraph(nil, nil)
	report, err := Analyze(nil, graph)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(report.Packages) != 0 {
		t.Fatalf("expected 0 packages, got %d", len(report.Packages))
	}
	if report.Summary.Count != 0 {
		t.Fatalf("expected summary count 0, got %d", report.Summary.Count)
	}
}

func TestAnalyzeEmptyIndex(t *testing.T) {
	idx := &model.Index{
		Version: "1",
		Files:   nil,
	}
	graph := makeGraph(nil, nil)
	report, err := Analyze(idx, graph)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(report.Packages) != 0 {
		t.Fatalf("expected 0 packages, got %d", len(report.Packages))
	}
}

func TestCrossPackageCoupling(t *testing.T) {
	// Two packages: pkg/a calls into pkg/b.
	// Expected: pkg/a has Ce=1 (calls out to b), pkg/b has Ca=1 (called by a).
	idx := &model.Index{
		Version: "1",
		Files: []model.FileSummary{
			{
				Path:     "pkg/a/a.go",
				Language: "go",
				Symbols: []model.Symbol{
					{Kind: "function_definition", Name: "DoA", StartLine: 1, EndLine: 5},
				},
			},
			{
				Path:     "pkg/b/b.go",
				Language: "go",
				Symbols: []model.Symbol{
					{Kind: "function_definition", Name: "DoB", StartLine: 1, EndLine: 5},
				},
			},
		},
	}

	defs := []xref.Definition{
		{ID: "a:DoA", File: "pkg/a/a.go", Package: "pkg/a", Kind: "function_definition", Name: "DoA", StartLine: 1, EndLine: 5, Callable: true},
		{ID: "b:DoB", File: "pkg/b/b.go", Package: "pkg/b", Kind: "function_definition", Name: "DoB", StartLine: 1, EndLine: 5, Callable: true},
	}
	edges := []xref.Edge{
		{CallerIdx: 0, CalleeIdx: 1}, // DoA -> DoB
	}
	graph := makeGraph(defs, edges)

	report, err := Analyze(idx, graph)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(report.Packages) != 2 {
		t.Fatalf("expected 2 packages, got %d", len(report.Packages))
	}

	pkgMap := map[string]*PackageMetrics{}
	for i := range report.Packages {
		pkgMap[report.Packages[i].Package] = &report.Packages[i]
	}

	a := pkgMap["pkg/a"]
	b := pkgMap["pkg/b"]
	if a == nil || b == nil {
		t.Fatalf("expected packages pkg/a and pkg/b, got: %v", pkgMap)
	}

	// pkg/a calls out to pkg/b: Ce=1, Ca=0
	if a.Ce != 1 {
		t.Errorf("pkg/a Ce: want 1, got %d", a.Ce)
	}
	if a.Ca != 0 {
		t.Errorf("pkg/a Ca: want 0, got %d", a.Ca)
	}

	// pkg/b is called by pkg/a: Ca=1, Ce=0
	if b.Ca != 1 {
		t.Errorf("pkg/b Ca: want 1, got %d", b.Ca)
	}
	if b.Ce != 0 {
		t.Errorf("pkg/b Ce: want 0, got %d", b.Ce)
	}
}

func TestInstability(t *testing.T) {
	// pkg/a: Ca=0, Ce=1 → instability = 1/(0+1) = 1.0
	// pkg/b: Ca=1, Ce=0 → instability = 0/(1+0) = 0.0
	idx := &model.Index{
		Version: "1",
		Files: []model.FileSummary{
			{Path: "pkg/a/a.go", Language: "go", Symbols: []model.Symbol{
				{Kind: "function_definition", Name: "DoA", StartLine: 1, EndLine: 5},
			}},
			{Path: "pkg/b/b.go", Language: "go", Symbols: []model.Symbol{
				{Kind: "function_definition", Name: "DoB", StartLine: 1, EndLine: 5},
			}},
		},
	}

	defs := []xref.Definition{
		{ID: "a:DoA", File: "pkg/a/a.go", Package: "pkg/a", Kind: "function_definition", Name: "DoA", StartLine: 1, EndLine: 5, Callable: true},
		{ID: "b:DoB", File: "pkg/b/b.go", Package: "pkg/b", Kind: "function_definition", Name: "DoB", StartLine: 1, EndLine: 5, Callable: true},
	}
	edges := []xref.Edge{
		{CallerIdx: 0, CalleeIdx: 1},
	}
	graph := makeGraph(defs, edges)

	report, err := Analyze(idx, graph)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	pkgMap := map[string]*PackageMetrics{}
	for i := range report.Packages {
		pkgMap[report.Packages[i].Package] = &report.Packages[i]
	}

	a := pkgMap["pkg/a"]
	b := pkgMap["pkg/b"]

	if math.Abs(a.Instability-1.0) > 0.001 {
		t.Errorf("pkg/a instability: want 1.0, got %f", a.Instability)
	}
	if math.Abs(b.Instability-0.0) > 0.001 {
		t.Errorf("pkg/b instability: want 0.0, got %f", b.Instability)
	}
}

func TestAbstractness(t *testing.T) {
	// pkg/a has 1 interface + 1 concrete type → abstractness = 0.5
	// pkg/b has 0 interfaces + 1 concrete type → abstractness = 0.0
	idx := &model.Index{
		Version: "1",
		Files: []model.FileSummary{
			{Path: "pkg/a/a.go", Language: "go", Symbols: []model.Symbol{
				{Kind: "type_definition", Name: "MyInterface", StartLine: 1, EndLine: 3},
				{Kind: "interface", Name: "MyInterface", StartLine: 1, EndLine: 3},
				{Kind: "type_definition", Name: "MyStruct", StartLine: 5, EndLine: 7},
			}},
			{Path: "pkg/b/b.go", Language: "go", Symbols: []model.Symbol{
				{Kind: "type_definition", Name: "Concrete", StartLine: 1, EndLine: 3},
			}},
		},
	}

	graph := makeGraph(nil, nil)
	report, err := Analyze(idx, graph)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	pkgMap := map[string]*PackageMetrics{}
	for i := range report.Packages {
		pkgMap[report.Packages[i].Package] = &report.Packages[i]
	}

	a := pkgMap["pkg/a"]
	b := pkgMap["pkg/b"]

	if a == nil {
		t.Fatal("pkg/a not found")
	}
	// pkg/a: 1 interface out of 2 type_definitions → 0.5
	// The interface symbol counts as an interface, type_definitions is the denominator.
	if math.Abs(a.Abstractness-0.5) > 0.001 {
		t.Errorf("pkg/a abstractness: want 0.5, got %f", a.Abstractness)
	}

	if b == nil {
		t.Fatal("pkg/b not found")
	}
	if math.Abs(b.Abstractness-0.0) > 0.001 {
		t.Errorf("pkg/b abstractness: want 0.0, got %f", b.Abstractness)
	}
}

func TestAbstractnessNoTypes(t *testing.T) {
	// Package with only functions, no type_definitions → abstractness=0
	idx := &model.Index{
		Version: "1",
		Files: []model.FileSummary{
			{Path: "pkg/a/a.go", Language: "go", Symbols: []model.Symbol{
				{Kind: "function_definition", Name: "Foo", StartLine: 1, EndLine: 3},
			}},
		},
	}
	graph := makeGraph(nil, nil)
	report, err := Analyze(idx, graph)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(report.Packages) != 1 {
		t.Fatalf("expected 1 package, got %d", len(report.Packages))
	}
	if math.Abs(report.Packages[0].Abstractness) > 0.001 {
		t.Errorf("abstractness: want 0.0, got %f", report.Packages[0].Abstractness)
	}
}

func TestDistance(t *testing.T) {
	// pkg/a: instability=1.0, abstractness=0.0 → distance = |0.0 + 1.0 - 1| = 0.0
	// pkg/b: instability=0.0, abstractness=0.0 → distance = |0.0 + 0.0 - 1| = 1.0
	idx := &model.Index{
		Version: "1",
		Files: []model.FileSummary{
			{Path: "pkg/a/a.go", Language: "go", Symbols: []model.Symbol{
				{Kind: "function_definition", Name: "DoA", StartLine: 1, EndLine: 5},
			}},
			{Path: "pkg/b/b.go", Language: "go", Symbols: []model.Symbol{
				{Kind: "function_definition", Name: "DoB", StartLine: 1, EndLine: 5},
			}},
		},
	}

	defs := []xref.Definition{
		{ID: "a:DoA", File: "pkg/a/a.go", Package: "pkg/a", Kind: "function_definition", Name: "DoA", StartLine: 1, EndLine: 5, Callable: true},
		{ID: "b:DoB", File: "pkg/b/b.go", Package: "pkg/b", Kind: "function_definition", Name: "DoB", StartLine: 1, EndLine: 5, Callable: true},
	}
	edges := []xref.Edge{
		{CallerIdx: 0, CalleeIdx: 1},
	}
	graph := makeGraph(defs, edges)

	report, err := Analyze(idx, graph)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	pkgMap := map[string]*PackageMetrics{}
	for i := range report.Packages {
		pkgMap[report.Packages[i].Package] = &report.Packages[i]
	}

	a := pkgMap["pkg/a"]
	b := pkgMap["pkg/b"]

	// pkg/a: A=0, I=1 → D = |0+1-1| = 0
	if math.Abs(a.Distance-0.0) > 0.001 {
		t.Errorf("pkg/a distance: want 0.0, got %f", a.Distance)
	}
	// pkg/b: A=0, I=0 → D = |0+0-1| = 1
	if math.Abs(b.Distance-1.0) > 0.001 {
		t.Errorf("pkg/b distance: want 1.0, got %f", b.Distance)
	}
}

func TestLCOM4TwoIndependentGroups(t *testing.T) {
	// pkg/a has 4 functions: f1->f2 (group1) and f3->f4 (group2) → LCOM=2
	idx := &model.Index{
		Version: "1",
		Files: []model.FileSummary{
			{Path: "pkg/a/a.go", Language: "go", Symbols: []model.Symbol{
				{Kind: "function_definition", Name: "f1", StartLine: 1, EndLine: 3},
				{Kind: "function_definition", Name: "f2", StartLine: 5, EndLine: 7},
				{Kind: "function_definition", Name: "f3", StartLine: 9, EndLine: 11},
				{Kind: "function_definition", Name: "f4", StartLine: 13, EndLine: 15},
			}},
		},
	}

	defs := []xref.Definition{
		{ID: "a:f1", File: "pkg/a/a.go", Package: "pkg/a", Kind: "function_definition", Name: "f1", StartLine: 1, EndLine: 3, Callable: true},
		{ID: "a:f2", File: "pkg/a/a.go", Package: "pkg/a", Kind: "function_definition", Name: "f2", StartLine: 5, EndLine: 7, Callable: true},
		{ID: "a:f3", File: "pkg/a/a.go", Package: "pkg/a", Kind: "function_definition", Name: "f3", StartLine: 9, EndLine: 11, Callable: true},
		{ID: "a:f4", File: "pkg/a/a.go", Package: "pkg/a", Kind: "function_definition", Name: "f4", StartLine: 13, EndLine: 15, Callable: true},
	}
	edges := []xref.Edge{
		{CallerIdx: 0, CalleeIdx: 1}, // f1 -> f2 (intra-package)
		{CallerIdx: 2, CalleeIdx: 3}, // f3 -> f4 (intra-package)
	}
	graph := makeGraph(defs, edges)

	report, err := Analyze(idx, graph)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(report.Packages) != 1 {
		t.Fatalf("expected 1 package, got %d", len(report.Packages))
	}

	if report.Packages[0].LCOM != 2 {
		t.Errorf("LCOM: want 2, got %d", report.Packages[0].LCOM)
	}
}

func TestLCOM4FullyConnected(t *testing.T) {
	// pkg/a has 3 functions all connected: f1->f2, f2->f3 → LCOM=1
	idx := &model.Index{
		Version: "1",
		Files: []model.FileSummary{
			{Path: "pkg/a/a.go", Language: "go", Symbols: []model.Symbol{
				{Kind: "function_definition", Name: "f1", StartLine: 1, EndLine: 3},
				{Kind: "function_definition", Name: "f2", StartLine: 5, EndLine: 7},
				{Kind: "function_definition", Name: "f3", StartLine: 9, EndLine: 11},
			}},
		},
	}

	defs := []xref.Definition{
		{ID: "a:f1", File: "pkg/a/a.go", Package: "pkg/a", Kind: "function_definition", Name: "f1", StartLine: 1, EndLine: 3, Callable: true},
		{ID: "a:f2", File: "pkg/a/a.go", Package: "pkg/a", Kind: "function_definition", Name: "f2", StartLine: 5, EndLine: 7, Callable: true},
		{ID: "a:f3", File: "pkg/a/a.go", Package: "pkg/a", Kind: "function_definition", Name: "f3", StartLine: 9, EndLine: 11, Callable: true},
	}
	edges := []xref.Edge{
		{CallerIdx: 0, CalleeIdx: 1}, // f1 -> f2
		{CallerIdx: 1, CalleeIdx: 2}, // f2 -> f3
	}
	graph := makeGraph(defs, edges)

	report, err := Analyze(idx, graph)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(report.Packages) != 1 {
		t.Fatalf("expected 1 package, got %d", len(report.Packages))
	}

	if report.Packages[0].LCOM != 1 {
		t.Errorf("LCOM: want 1, got %d", report.Packages[0].LCOM)
	}
}

func TestLCOM4IsolatedCallables(t *testing.T) {
	// pkg/a has 3 functions with no intra-package edges → LCOM=3
	idx := &model.Index{
		Version: "1",
		Files: []model.FileSummary{
			{Path: "pkg/a/a.go", Language: "go", Symbols: []model.Symbol{
				{Kind: "function_definition", Name: "f1", StartLine: 1, EndLine: 3},
				{Kind: "function_definition", Name: "f2", StartLine: 5, EndLine: 7},
				{Kind: "function_definition", Name: "f3", StartLine: 9, EndLine: 11},
			}},
		},
	}

	defs := []xref.Definition{
		{ID: "a:f1", File: "pkg/a/a.go", Package: "pkg/a", Kind: "function_definition", Name: "f1", StartLine: 1, EndLine: 3, Callable: true},
		{ID: "a:f2", File: "pkg/a/a.go", Package: "pkg/a", Kind: "function_definition", Name: "f2", StartLine: 5, EndLine: 7, Callable: true},
		{ID: "a:f3", File: "pkg/a/a.go", Package: "pkg/a", Kind: "function_definition", Name: "f3", StartLine: 9, EndLine: 11, Callable: true},
	}
	// No edges at all
	graph := makeGraph(defs, nil)

	report, err := Analyze(idx, graph)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(report.Packages) != 1 {
		t.Fatalf("expected 1 package, got %d", len(report.Packages))
	}

	if report.Packages[0].LCOM != 3 {
		t.Errorf("LCOM: want 3, got %d", report.Packages[0].LCOM)
	}
}

func TestLCOM4NoCallables(t *testing.T) {
	// Package with only type definitions, no callables → LCOM=0
	idx := &model.Index{
		Version: "1",
		Files: []model.FileSummary{
			{Path: "pkg/a/a.go", Language: "go", Symbols: []model.Symbol{
				{Kind: "type_definition", Name: "Foo", StartLine: 1, EndLine: 3},
			}},
		},
	}

	graph := makeGraph(nil, nil)
	report, err := Analyze(idx, graph)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(report.Packages) != 1 {
		t.Fatalf("expected 1 package, got %d", len(report.Packages))
	}

	if report.Packages[0].LCOM != 0 {
		t.Errorf("LCOM: want 0 (no callables), got %d", report.Packages[0].LCOM)
	}
}

func TestSinglePackageCoupling(t *testing.T) {
	// Single package: no external dependencies → Ca=0, Ce=0, instability=0
	idx := &model.Index{
		Version: "1",
		Files: []model.FileSummary{
			{Path: "pkg/a/a.go", Language: "go", Symbols: []model.Symbol{
				{Kind: "function_definition", Name: "DoA", StartLine: 1, EndLine: 5},
				{Kind: "function_definition", Name: "DoB", StartLine: 7, EndLine: 10},
			}},
		},
	}

	defs := []xref.Definition{
		{ID: "a:DoA", File: "pkg/a/a.go", Package: "pkg/a", Kind: "function_definition", Name: "DoA", StartLine: 1, EndLine: 5, Callable: true},
		{ID: "a:DoB", File: "pkg/a/a.go", Package: "pkg/a", Kind: "function_definition", Name: "DoB", StartLine: 7, EndLine: 10, Callable: true},
	}
	edges := []xref.Edge{
		{CallerIdx: 0, CalleeIdx: 1}, // intra-package call
	}
	graph := makeGraph(defs, edges)

	report, err := Analyze(idx, graph)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(report.Packages) != 1 {
		t.Fatalf("expected 1 package, got %d", len(report.Packages))
	}

	pkg := report.Packages[0]
	if pkg.Ca != 0 {
		t.Errorf("Ca: want 0, got %d", pkg.Ca)
	}
	if pkg.Ce != 0 {
		t.Errorf("Ce: want 0, got %d", pkg.Ce)
	}
	if math.Abs(pkg.Instability) > 0.001 {
		t.Errorf("instability: want 0.0, got %f", pkg.Instability)
	}
}

func TestSummaryAggregation(t *testing.T) {
	// Three packages with known metrics to verify summary computation.
	idx := &model.Index{
		Version: "1",
		Files: []model.FileSummary{
			{Path: "pkg/a/a.go", Language: "go", Symbols: []model.Symbol{
				{Kind: "function_definition", Name: "DoA", StartLine: 1, EndLine: 5},
			}},
			{Path: "pkg/b/b.go", Language: "go", Symbols: []model.Symbol{
				{Kind: "function_definition", Name: "DoB", StartLine: 1, EndLine: 5},
			}},
			{Path: "pkg/c/c.go", Language: "go", Symbols: []model.Symbol{
				{Kind: "function_definition", Name: "DoC", StartLine: 1, EndLine: 5},
			}},
		},
	}

	defs := []xref.Definition{
		{ID: "a:DoA", File: "pkg/a/a.go", Package: "pkg/a", Kind: "function_definition", Name: "DoA", StartLine: 1, EndLine: 5, Callable: true},
		{ID: "b:DoB", File: "pkg/b/b.go", Package: "pkg/b", Kind: "function_definition", Name: "DoB", StartLine: 1, EndLine: 5, Callable: true},
		{ID: "c:DoC", File: "pkg/c/c.go", Package: "pkg/c", Kind: "function_definition", Name: "DoC", StartLine: 1, EndLine: 5, Callable: true},
	}
	// a->b, a->c, b->c
	edges := []xref.Edge{
		{CallerIdx: 0, CalleeIdx: 1}, // a -> b
		{CallerIdx: 0, CalleeIdx: 2}, // a -> c
		{CallerIdx: 1, CalleeIdx: 2}, // b -> c
	}
	graph := makeGraph(defs, edges)

	report, err := Analyze(idx, graph)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if report.Summary.Count != 3 {
		t.Fatalf("summary count: want 3, got %d", report.Summary.Count)
	}

	// Verify instabilities:
	// a: Ca=0, Ce=2 → I=1.0
	// b: Ca=1, Ce=1 → I=0.5
	// c: Ca=2, Ce=0 → I=0.0
	// avg = (1.0+0.5+0.0)/3 ≈ 0.5
	// max = 1.0
	if math.Abs(report.Summary.AvgInstability-0.5) > 0.001 {
		t.Errorf("avg instability: want 0.5, got %f", report.Summary.AvgInstability)
	}
	if math.Abs(report.Summary.MaxInstability-1.0) > 0.001 {
		t.Errorf("max instability: want 1.0, got %f", report.Summary.MaxInstability)
	}

	// All have abstractness=0, so distance = |0+I-1| = |I-1|
	// a: |1-1|=0, b: |0.5-1|=0.5, c: |0-1|=1.0
	// avg distance = (0+0.5+1.0)/3 ≈ 0.5
	// max distance = 1.0
	if math.Abs(report.Summary.AvgDistance-0.5) > 0.001 {
		t.Errorf("avg distance: want 0.5, got %f", report.Summary.AvgDistance)
	}
	if math.Abs(report.Summary.MaxDistance-1.0) > 0.001 {
		t.Errorf("max distance: want 1.0, got %f", report.Summary.MaxDistance)
	}
}

func TestMultipleFilesInSamePackage(t *testing.T) {
	// Two files in the same package should be aggregated as one package.
	idx := &model.Index{
		Version: "1",
		Files: []model.FileSummary{
			{Path: "pkg/a/x.go", Language: "go", Symbols: []model.Symbol{
				{Kind: "function_definition", Name: "X", StartLine: 1, EndLine: 3},
			}},
			{Path: "pkg/a/y.go", Language: "go", Symbols: []model.Symbol{
				{Kind: "function_definition", Name: "Y", StartLine: 1, EndLine: 3},
			}},
		},
	}

	graph := makeGraph(nil, nil)
	report, err := Analyze(idx, graph)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(report.Packages) != 1 {
		t.Fatalf("expected 1 package (aggregated), got %d", len(report.Packages))
	}

	if report.Packages[0].Files != 2 {
		t.Errorf("files: want 2, got %d", report.Packages[0].Files)
	}
	if report.Packages[0].Symbols != 2 {
		t.Errorf("symbols: want 2, got %d", report.Packages[0].Symbols)
	}
}

func TestSummaryLCOM(t *testing.T) {
	// Two packages: one with LCOM=1, one with LCOM=3
	idx := &model.Index{
		Version: "1",
		Files: []model.FileSummary{
			{Path: "pkg/a/a.go", Language: "go", Symbols: []model.Symbol{
				{Kind: "function_definition", Name: "f1", StartLine: 1, EndLine: 3},
				{Kind: "function_definition", Name: "f2", StartLine: 5, EndLine: 7},
			}},
			{Path: "pkg/b/b.go", Language: "go", Symbols: []model.Symbol{
				{Kind: "function_definition", Name: "g1", StartLine: 1, EndLine: 3},
				{Kind: "function_definition", Name: "g2", StartLine: 5, EndLine: 7},
				{Kind: "function_definition", Name: "g3", StartLine: 9, EndLine: 11},
			}},
		},
	}

	defs := []xref.Definition{
		{ID: "a:f1", File: "pkg/a/a.go", Package: "pkg/a", Kind: "function_definition", Name: "f1", StartLine: 1, EndLine: 3, Callable: true},
		{ID: "a:f2", File: "pkg/a/a.go", Package: "pkg/a", Kind: "function_definition", Name: "f2", StartLine: 5, EndLine: 7, Callable: true},
		{ID: "b:g1", File: "pkg/b/b.go", Package: "pkg/b", Kind: "function_definition", Name: "g1", StartLine: 1, EndLine: 3, Callable: true},
		{ID: "b:g2", File: "pkg/b/b.go", Package: "pkg/b", Kind: "function_definition", Name: "g2", StartLine: 5, EndLine: 7, Callable: true},
		{ID: "b:g3", File: "pkg/b/b.go", Package: "pkg/b", Kind: "function_definition", Name: "g3", StartLine: 9, EndLine: 11, Callable: true},
	}
	edges := []xref.Edge{
		{CallerIdx: 0, CalleeIdx: 1}, // a:f1 -> a:f2 (intra-package, connects them)
		// b has no intra-package edges → each callable is isolated → LCOM=3
	}
	graph := makeGraph(defs, edges)

	report, err := Analyze(idx, graph)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	pkgMap := map[string]*PackageMetrics{}
	for i := range report.Packages {
		pkgMap[report.Packages[i].Package] = &report.Packages[i]
	}

	a := pkgMap["pkg/a"]
	b := pkgMap["pkg/b"]

	if a.LCOM != 1 {
		t.Errorf("pkg/a LCOM: want 1, got %d", a.LCOM)
	}
	if b.LCOM != 3 {
		t.Errorf("pkg/b LCOM: want 3, got %d", b.LCOM)
	}

	// avg LCOM = (1+3)/2 = 2.0
	if math.Abs(report.Summary.AvgLCOM-2.0) > 0.001 {
		t.Errorf("avg LCOM: want 2.0, got %f", report.Summary.AvgLCOM)
	}
	// max LCOM = 3
	if report.Summary.MaxLCOM != 3 {
		t.Errorf("max LCOM: want 3, got %d", report.Summary.MaxLCOM)
	}
}
