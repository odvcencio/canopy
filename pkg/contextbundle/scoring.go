package contextbundle

import (
	"math"
	"sort"
	"strings"

	"m31labs.dev/canopy/pkg/xref"
)

// Scoring signal points, matching spec 10.8 exactly. Signals without a data
// source in this PR (high-risk percentile, recency, project-memory anchors,
// receipt-proves-unchanged) are declared here for documentation but never
// applied; see the final report for why.
const (
	scoreExplicitRequired    = 10000
	scoreExplicitSelector    = 2500
	scoreChangedEntity       = 1500
	scoreFocusEntity         = 1400
	scoreFailureNamed        = 1200
	scoreDirectCallerCallee  = 800
	scoreMappedTest          = 750
	scorePublicAPI           = 650
	scoreTaskTermMax         = 600
	scoreHighRiskPercentile  = 500 // not applied: no risk data source wired in this PR
	scoreFanInHighPercentile = 400
	scoreRecentlyEdited      = 300 // not applied: no run event log in this PR
	scoreRecentlyRead        = 250 // not applied: no run event log in this PR
	scoreTransitiveDepth2    = 200
	scoreMemoryAnchor        = 200 // not applied: memory adapters are a Buckley concern
	scoreGeneratedPenalty    = -1000
	scoreDuplicatePenalty    = -2000 // applied during packing, not here
	scoreUnchangedPenalty    = -1500 // not applied: needs a receipt store this package does not depend on
)

// fanInPercentileThreshold is the spec's "≥ 0.90" cut for the fan-in bonus.
const fanInPercentileThreshold = 0.90

// scoreCandidates assigns an integer score and an ordered SelectionReason
// trail to every candidate, per the deterministic policy in spec 10.8
// (policy version contextbundle.PolicyVersion).
func scoreCandidates(candidates []*candidateItem, graph *xref.Graph, req Request) {
	fanInPercentile := fanInPercentileRanks(graph)
	queryTerms := normalizedTerms(taskQuery(req.Intent))

	// G-A: personalized PageRank proximity to the focus/changed seeds.
	// Seed entities already score through their own focus/changed
	// signals, so only non-seed candidates receive proximity points,
	// normalized against the best non-seed candidate.
	pprSeeds := map[string]bool{}
	for _, c := range candidates {
		if (c.Flags.Focus || c.Flags.Changed) && c.EntityID != "" {
			pprSeeds[c.EntityID] = true
		}
	}
	ppr := pprScores(graph, pprSeeds)
	maxPPR := 0.0
	for _, c := range candidates {
		if c.EntityID == "" || pprSeeds[c.EntityID] {
			continue
		}
		if score := ppr[c.EntityID]; score > maxPPR {
			maxPPR = score
		}
	}

	for _, c := range candidates {
		var reasons []SelectionReason
		total := 0

		add := func(signal string, points int) {
			if points == 0 {
				return
			}
			reasons = append(reasons, SelectionReason{Signal: signal, Points: points})
			total += points
		}

		if c.Flags.ExplicitRequired {
			add("explicit_required_selector", scoreExplicitRequired)
		} else if c.Flags.ExplicitSelector {
			add("explicit_non_required_selector", scoreExplicitSelector)
		}
		if c.Flags.Changed {
			add("changed_entity", scoreChangedEntity)
		}
		if c.Flags.Focus {
			add("focus_entity", scoreFocusEntity)
		}
		if c.Flags.FailureNamed {
			add("failure_evidence_names_entity", scoreFailureNamed)
		}
		if c.Flags.DirectCallerCallee {
			add("direct_caller_or_callee", scoreDirectCallerCallee)
		}
		if c.Flags.MappedTest {
			add("mapped_test", scoreMappedTest)
		}
		if c.Flags.PublicAPI {
			add("public_api_or_interface", scorePublicAPI)
		}
		if len(queryTerms) > 0 && c.Flags.TaskTermMatch == 0 {
			c.Flags.TaskTermMatch = termOverlap(queryTerms, normalizedTerms(c.Name+" "+c.Path))
		}
		if pts := int(math.Round(float64(scoreTaskTermMax) * c.Flags.TaskTermMatch)); pts > 0 {
			add("task_term_match", pts)
		}
		if c.EntityID != "" {
			if pct, ok := fanInPercentile[c.EntityID]; ok && pct >= fanInPercentileThreshold {
				c.Flags.FanInHighPercentile = true
				add("fan_in_percentile_ge_0_90", scoreFanInHighPercentile)
			}
		}
		if c.Flags.TransitiveDepth2 {
			add("transitive_graph_depth_2", scoreTransitiveDepth2)
		}
		if maxPPR > 0 && c.EntityID != "" && !pprSeeds[c.EntityID] {
			if score := ppr[c.EntityID]; score > 0 {
				if pts := int(math.Round(float64(scorePPRMax) * score / maxPPR)); pts > 0 {
					add("ppr_graph_proximity", pts)
				}
			}
		}
		if c.Flags.Generated && !c.Flags.ExplicitRequired && !c.Flags.ExplicitSelector {
			add("generated_not_explicitly_requested", scoreGeneratedPenalty)
		}

		c.Score = total
		c.Reasons = reasons
	}
}

// fanInPercentileRanks computes each definition's incoming-call percentile
// rank within the graph (0..1, 1 = highest fan-in), matching the "fan-in
// percentile ≥ 0.90" scoring signal.
func fanInPercentileRanks(graph *xref.Graph) map[string]float64 {
	out := map[string]float64{}
	if graph == nil || len(graph.Definitions) == 0 {
		return out
	}
	type scored struct {
		id    string
		count int
	}
	items := make([]scored, 0, len(graph.Definitions))
	for _, def := range graph.Definitions {
		if !def.Callable {
			continue
		}
		items = append(items, scored{id: def.ID, count: graph.IncomingCount(def.ID)})
	}
	if len(items) == 0 {
		return out
	}
	sort.Slice(items, func(i, j int) bool { return items[i].count < items[j].count })
	n := len(items)
	for i, it := range items {
		rank := float64(i+1) / float64(n)
		out[it.id] = rank
	}
	return out
}

func taskQuery(intent TaskIntent) string {
	if strings.TrimSpace(intent.DerivedQuery) != "" {
		return intent.DerivedQuery
	}
	return intent.OriginalRequest
}

// normalizedTerms lowercases and splits identifiers/prose into a term set,
// breaking camelCase and snake_case/kebab-case boundaries so "HandleAuth"
// and "handle auth" overlap.
func normalizedTerms(s string) map[string]bool {
	terms := map[string]bool{}
	var word strings.Builder
	flush := func() {
		if word.Len() == 0 {
			return
		}
		terms[strings.ToLower(word.String())] = true
		word.Reset()
	}
	runes := []rune(s)
	for i, r := range runes {
		switch {
		case r >= 'A' && r <= 'Z':
			if i > 0 {
				prev := runes[i-1]
				if !(prev >= 'A' && prev <= 'Z') && prev != '_' && prev != '-' && prev != ' ' && prev != '.' && prev != '/' {
					flush()
				}
			}
			word.WriteRune(r)
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			word.WriteRune(r)
		default:
			flush()
		}
	}
	flush()
	return terms
}

func termOverlap(query, candidate map[string]bool) float64 {
	if len(query) == 0 || len(candidate) == 0 {
		return 0
	}
	hits := 0
	for term := range query {
		if candidate[term] {
			hits++
		}
	}
	return float64(hits) / float64(len(query))
}

// candidateTieBreak orders candidates deterministically when scores tie,
// using section, path, start line, entity ID, and mode (spec 10.8).
func candidateTieBreak(a, b *candidateItem, modeA, modeB ProjectionMode) bool {
	if sectionRank(a.Section) != sectionRank(b.Section) {
		return sectionRank(a.Section) < sectionRank(b.Section)
	}
	if a.Path != b.Path {
		return a.Path < b.Path
	}
	if a.StartLine != b.StartLine {
		return a.StartLine < b.StartLine
	}
	if a.EntityID != b.EntityID {
		return a.EntityID < b.EntityID
	}
	return modeA < modeB
}
