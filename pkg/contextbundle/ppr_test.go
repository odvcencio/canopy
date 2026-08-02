package contextbundle

import (
	"testing"

	"m31labs.dev/canopy/pkg/xref"
)

// pprTestGraph builds a small call graph by hand:
//
//	seed -> near (5 static calls)
//	near -> far  (1 static call)
//	seed -> weak (1 polymorphic call)
//	lonely       (no edges)
func pprTestGraph() *xref.Graph {
	return &xref.Graph{
		Definitions: []xref.Definition{
			{ID: "seed", Name: "Seed", Callable: true},
			{ID: "near", Name: "Near", Callable: true},
			{ID: "far", Name: "Far", Callable: true},
			{ID: "weak", Name: "Weak", Callable: true},
			{ID: "lonely", Name: "Lonely", Callable: true},
		},
		Edges: []xref.Edge{
			{CallerIdx: 0, CalleeIdx: 1, Resolution: "file", Count: 5},
			{CallerIdx: 1, CalleeIdx: 2, Resolution: "file", Count: 1},
			{CallerIdx: 0, CalleeIdx: 3, Resolution: "poly_pkg", Count: 1},
		},
	}
}

// TestPPRScores_ProximityOrdering locks the ranking shape: the seed
// holds the most mass, its strong static neighbor beats the weak
// polymorphic one, two hops beats disconnected, and a node with no path
// from the seeds scores zero.
func TestPPRScores_ProximityOrdering(t *testing.T) {
	t.Parallel()
	scores := pprScores(pprTestGraph(), map[string]bool{"seed": true})
	if scores == nil {
		t.Fatal("pprScores returned nil for a valid seed")
	}
	if !(scores["seed"] > scores["near"]) {
		t.Fatalf("seed (%v) must outrank near (%v)", scores["seed"], scores["near"])
	}
	if !(scores["near"] > scores["weak"]) {
		t.Fatalf("strong static neighbor (%v) must outrank weak polymorphic (%v)", scores["near"], scores["weak"])
	}
	if !(scores["far"] > 0) {
		t.Fatalf("two-hop neighbor must receive mass, got %v", scores["far"])
	}
	if scores["lonely"] != 0 {
		t.Fatalf("disconnected node scored %v, want 0", scores["lonely"])
	}
}

// TestPPRScores_Determinism locks the fixed-iteration contract: two runs
// produce identical maps.
func TestPPRScores_Determinism(t *testing.T) {
	t.Parallel()
	a := pprScores(pprTestGraph(), map[string]bool{"seed": true})
	b := pprScores(pprTestGraph(), map[string]bool{"seed": true})
	if len(a) != len(b) {
		t.Fatalf("run sizes differ: %d vs %d", len(a), len(b))
	}
	for id, score := range a {
		if b[id] != score {
			t.Fatalf("score for %s differs across runs: %v vs %v", id, score, b[id])
		}
	}
}

// TestPPRScores_NoSeedsSkips locks the gate: no personalization mass, no
// pass — the signal only exists relative to a focus.
func TestPPRScores_NoSeedsSkips(t *testing.T) {
	t.Parallel()
	if got := pprScores(pprTestGraph(), nil); got != nil {
		t.Fatalf("nil seeds produced scores: %v", got)
	}
	if got := pprScores(pprTestGraph(), map[string]bool{"unknown": true}); got != nil {
		t.Fatalf("unmatched seeds produced scores: %v", got)
	}
	if got := pprScores(nil, map[string]bool{"seed": true}); got != nil {
		t.Fatalf("nil graph produced scores: %v", got)
	}
}

// TestScoreCandidates_PPRProximityPoints locks the score-table fold: a
// non-seed candidate near the changed seed earns bounded
// ppr_graph_proximity points with a reason entry, the strongest
// neighbor earns scorePPRMax, and seed candidates earn none (their
// focus/changed signals already carry them).
func TestScoreCandidates_PPRProximityPoints(t *testing.T) {
	t.Parallel()
	graph := pprTestGraph()
	candidates := []*candidateItem{
		{EntityID: "seed", Name: "Seed", Flags: candidateFlags{Changed: true}},
		{EntityID: "near", Name: "Near"},
		{EntityID: "weak", Name: "Weak"},
		{EntityID: "lonely", Name: "Lonely"},
	}
	scoreCandidates(candidates, graph, Request{})

	points := map[string]int{}
	for _, c := range candidates {
		for _, reason := range c.Reasons {
			if reason.Signal == "ppr_graph_proximity" {
				points[c.EntityID] = reason.Points
			}
		}
	}
	if points["seed"] != 0 {
		t.Fatalf("seed earned proximity points (%d); seeds must not double-count", points["seed"])
	}
	if points["near"] != scorePPRMax {
		t.Fatalf("strongest neighbor earned %d, want %d", points["near"], scorePPRMax)
	}
	if points["weak"] <= 0 || points["weak"] >= points["near"] {
		t.Fatalf("weak neighbor points = %d, want between 1 and %d", points["weak"], points["near"])
	}
	if points["lonely"] != 0 {
		t.Fatalf("disconnected candidate earned %d proximity points", points["lonely"])
	}
}
