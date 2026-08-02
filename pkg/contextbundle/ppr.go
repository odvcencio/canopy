package contextbundle

import (
	"strings"

	"m31labs.dev/canopy/pkg/xref"
)

// Personalized PageRank context selection (graph-native-harness move
// G-A). Evidence behind the design: Aider's PPR repo map, RepoGraph's
// gains across four SWE-bench frameworks with one graph, and LocAgent's
// 92.7% localization at an 86% cost cut. The pass runs at selection time
// over the already-built xref graph, with personalization mass on the
// focus and changed entities, and folds into the deterministic score
// table as one bounded component — receipts stay explainable, and a
// bundle without focus or changed seeds skips the pass entirely.
const (
	pprDamping    = 0.85
	pprIterations = 30
	// pprReverseWeight lets proximity flow callee-to-caller at reduced
	// mass: callers of a changed function matter, but less than what
	// the changed function itself calls.
	pprReverseWeight = 0.5
	// scorePPRMax bounds the signal's contribution to the score table,
	// below direct caller/callee (800) and above transitive depth-2
	// (200): graph proximity refines the neighborhood ordering without
	// overruling explicit structure.
	scorePPRMax = 400
)

// pprResolutionWeight scales edge mass by call-resolution confidence: a
// same-file or import-resolved call is a stronger proximity signal than
// a polymorphic candidate set.
func pprResolutionWeight(resolution string) float64 {
	switch {
	case resolution == "file":
		return 1.0
	case resolution == "import":
		return 0.9
	case strings.HasPrefix(resolution, "poly_"):
		return 0.6
	default:
		return 0.8
	}
}

// pprScores runs fixed-iteration personalized PageRank over the call
// graph and returns a score per definition ID. seeds carry the
// personalization mass; an empty or unmatched seed set returns nil (the
// signal only exists relative to a focus). The iteration count is fixed
// so the pass is deterministic across runs and platforms.
func pprScores(graph *xref.Graph, seeds map[string]bool) map[string]float64 {
	if graph == nil || len(seeds) == 0 || len(graph.Definitions) == 0 {
		return nil
	}
	n := len(graph.Definitions)

	personalization := make([]float64, n)
	seedCount := 0
	for i, def := range graph.Definitions {
		if seeds[def.ID] {
			personalization[i] = 1
			seedCount++
		}
	}
	if seedCount == 0 {
		return nil
	}
	for i := range personalization {
		personalization[i] /= float64(seedCount)
	}

	type arc struct {
		to     int
		weight float64
	}
	out := make([][]arc, n)
	outSum := make([]float64, n)
	addArc := func(from, to int, weight float64) {
		if from < 0 || from >= n || to < 0 || to >= n || weight <= 0 || from == to {
			return
		}
		out[from] = append(out[from], arc{to: to, weight: weight})
		outSum[from] += weight
	}
	for _, edge := range graph.Edges {
		count := edge.Count
		if count < 1 {
			count = 1
		}
		weight := float64(count) * pprResolutionWeight(edge.Resolution)
		addArc(edge.CallerIdx, edge.CalleeIdx, weight)
		addArc(edge.CalleeIdx, edge.CallerIdx, weight*pprReverseWeight)
	}

	rank := make([]float64, n)
	copy(rank, personalization)
	next := make([]float64, n)
	for iteration := 0; iteration < pprIterations; iteration++ {
		dangling := 0.0
		for i := range next {
			next[i] = (1 - pprDamping) * personalization[i]
		}
		for i, arcs := range out {
			if rank[i] == 0 {
				continue
			}
			if outSum[i] == 0 {
				dangling += rank[i]
				continue
			}
			share := pprDamping * rank[i] / outSum[i]
			for _, a := range arcs {
				next[a.to] += share * a.weight
			}
		}
		// Nodes with no outgoing mass return their rank through the
		// personalization vector, keeping total mass conserved.
		if dangling > 0 {
			for i := range next {
				next[i] += pprDamping * dangling * personalization[i]
			}
		}
		rank, next = next, rank
	}

	scores := make(map[string]float64, n)
	for i, def := range graph.Definitions {
		if rank[i] > 0 {
			scores[def.ID] = rank[i]
		}
	}
	return scores
}
