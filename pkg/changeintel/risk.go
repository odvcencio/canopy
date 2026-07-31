package changeintel

import "strings"

// Normalization constants for risk components that are continuous rather
// than presence/absence flags. These are tunable heuristics, not spec'd
// values — see the package tests for the behavior they produce.
const (
	blastRadiusScale        = 25.0 // blast radius at or above this many affected symbols saturates the component to 1.0
	complexityIncreaseScale = 20.0 // cyclomatic delta at or above this saturates the component to 1.0
	highFaninThreshold      = 10   // fan-in at or above this counts an entity as "high fan-in"
)

var concurrencyKeywords = []string{
	"mutex", "sync.", "goroutine", "atomic", "channel", "lock",
	"concurrent", "async", "await", "thread", "semaphore", "waitgroup",
}

// computeRisk implements the routing-risk formula from spec section 11.5.
// Every component is clamped to [0,1] before weighting. The composite score
// routes review depth; callers MUST NOT treat it as an approval gate.
func computeRisk(entities []EntityChange, files []FileChange, complexity ComplexityDelta, api APIDelta, boundary BoundaryDelta, capability CapabilityDelta, impact ImpactSummary, tests TestImpact, includeTests bool) RiskDelta {
	comp := RiskComponents{
		BlastRadius:                      clamp01(float64(impact.BlastRadius) / blastRadiusScale),
		PublicAPIChange:                  boolComponent(len(api.Added)+len(api.Removed)+len(api.Changed) > 0),
		SecurityCapabilityChange:         boolComponent(len(capability.Introduced) > 0),
		BoundaryViolationChange:          boolComponent(len(boundary.Introduced) > 0),
		ComplexityIncrease:               clamp01(float64(maxInt(0, complexity.DeltaCyclomatic)) / complexityIncreaseScale),
		HighFaninChange:                  highFaninRatio(entities),
		ConcurrencyOrStatefulArea:        concurrencyScore(files, entities),
		GeneratedOrProvenanceUncertainty: uncertaintyScore(files, impact),
	}
	if includeTests {
		comp.TestGap = clamp01(1 - tests.Coverage)
	}

	score := 0.20*comp.BlastRadius +
		0.15*comp.PublicAPIChange +
		0.15*comp.SecurityCapabilityChange +
		0.10*comp.BoundaryViolationChange +
		0.10*comp.ComplexityIncrease +
		0.10*comp.HighFaninChange +
		0.10*comp.TestGap +
		0.05*comp.ConcurrencyOrStatefulArea +
		0.05*comp.GeneratedOrProvenanceUncertainty

	return RiskDelta{Score: clamp01(score), Components: comp}
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func boolComponent(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func highFaninRatio(entities []EntityChange) float64 {
	touched, highFanin := 0, 0
	for _, e := range entities {
		if e.Status == EntityUnchanged {
			continue
		}
		touched++
		if e.Base.FanIn >= highFaninThreshold || e.Head.FanIn >= highFaninThreshold {
			highFanin++
		}
	}
	if touched == 0 {
		return 0
	}
	return clamp01(float64(highFanin) / float64(touched))
}

func concurrencyScore(files []FileChange, entities []EntityChange) float64 {
	for _, f := range files {
		if containsAny(strings.ToLower(f.Path), concurrencyKeywords) {
			return 1
		}
	}
	for _, e := range entities {
		if containsAny(strings.ToLower(e.Name), concurrencyKeywords) {
			return 1
		}
	}
	return 0
}

func containsAny(haystack string, needles []string) bool {
	for _, n := range needles {
		if strings.Contains(haystack, n) {
			return true
		}
	}
	return false
}

func uncertaintyScore(files []FileChange, impact ImpactSummary) float64 {
	if !impact.BaseAvailable {
		return 1
	}
	for _, f := range files {
		if f.Generated || f.Uncertain {
			return 1
		}
	}
	return 0
}
