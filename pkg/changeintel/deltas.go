package changeintel

import (
	"context"
	"sort"
	"strings"

	"m31labs.dev/canopy/internal/deps"
	"m31labs.dev/canopy/pkg/boundaries"
	"m31labs.dev/canopy/pkg/capa"
	"m31labs.dev/canopy/pkg/model"
)

// loadBoundaryConfig reads .canopyboundaries from the head snapshot (disk
// for a working-tree head, `git show` for a ref head). The same policy is
// then evaluated against both base and head import edges so the delta
// reflects "does this change violate the policy we are landing under",
// not policy drift between the two commits.
func loadBoundaryConfig(ctx context.Context, root string, head SnapshotRef) *boundaries.Config {
	if strings.TrimSpace(head.Ref) == "" {
		cfg, err := boundaries.LoadConfig(root)
		if err != nil {
			return nil
		}
		return cfg
	}
	content, ok, err := gitShow(ctx, root, head.Ref, ".canopyboundaries")
	if err != nil || !ok {
		return nil
	}
	cfg, err := boundaries.ParseConfig(string(content))
	if err != nil {
		return nil
	}
	return cfg
}

func evaluateBoundaries(cfg *boundaries.Config, idx *model.Index) []boundaries.Violation {
	if cfg == nil || len(cfg.Rules) == 0 || idx == nil {
		return nil
	}
	report, err := deps.Build(idx, deps.Options{Mode: "package", IncludeEdges: true})
	if err != nil {
		return nil
	}
	edges := make([]boundaries.ImportEdge, 0, len(report.Edges))
	for _, e := range report.Edges {
		if e.Internal {
			edges = append(edges, boundaries.ImportEdge{From: e.From, To: e.To})
		}
	}
	return boundaries.Evaluate(cfg, edges)
}

func violationKey(v boundaries.Violation) string {
	return v.From + "|" + v.To + "|" + v.Rule + "|" + v.Module
}

func toBoundaryViolation(v boundaries.Violation) BoundaryViolation {
	return BoundaryViolation{From: v.From, To: v.To, Rule: v.Rule, Module: v.Module, Message: v.Message}
}

func computeBoundaryDelta(ctx context.Context, root string, head SnapshotRef, baseIdx, headIdx *model.Index) BoundaryDelta {
	cfg := loadBoundaryConfig(ctx, root, head)
	if cfg == nil {
		return BoundaryDelta{}
	}
	baseViolations := evaluateBoundaries(cfg, baseIdx)
	headViolations := evaluateBoundaries(cfg, headIdx)

	baseSet := make(map[string]boundaries.Violation, len(baseViolations))
	for _, v := range baseViolations {
		baseSet[violationKey(v)] = v
	}
	headSet := make(map[string]boundaries.Violation, len(headViolations))
	for _, v := range headViolations {
		headSet[violationKey(v)] = v
	}

	var delta BoundaryDelta
	for k, v := range headSet {
		if _, ok := baseSet[k]; ok {
			delta.Persisting = append(delta.Persisting, toBoundaryViolation(v))
		} else {
			delta.Introduced = append(delta.Introduced, toBoundaryViolation(v))
		}
	}
	for k, v := range baseSet {
		if _, ok := headSet[k]; !ok {
			delta.Resolved = append(delta.Resolved, toBoundaryViolation(v))
		}
	}
	sortBoundaryViolations(delta.Introduced)
	sortBoundaryViolations(delta.Resolved)
	sortBoundaryViolations(delta.Persisting)
	return delta
}

func sortBoundaryViolations(items []BoundaryViolation) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].From != items[j].From {
			return items[i].From < items[j].From
		}
		if items[i].To != items[j].To {
			return items[i].To < items[j].To
		}
		return items[i].Rule < items[j].Rule
	})
}

// capabilityMatchesByFile flattens capa.Detect's per-rule match list into a
// per-(rule, file) map so introduced/resolved/persisting classification is
// possible at file granularity.
func capabilityMatchesByFile(idx *model.Index, rules []capa.Rule) map[string]CapabilityMatch {
	out := map[string]CapabilityMatch{}
	if idx == nil {
		return out
	}
	for _, m := range capa.Detect(idx, rules) {
		for _, f := range m.Files {
			key := m.Rule.Name + "|" + f
			out[key] = CapabilityMatch{Name: m.Rule.Name, Category: m.Rule.Category, Confidence: m.Rule.Confidence, AttackID: m.Rule.AttackID, File: f}
		}
	}
	return out
}

func computeCapabilityDelta(baseIdx, headIdx *model.Index) CapabilityDelta {
	rules := capa.BuiltinRules()
	baseMatches := capabilityMatchesByFile(baseIdx, rules)
	headMatches := capabilityMatchesByFile(headIdx, rules)

	var delta CapabilityDelta
	for k, v := range headMatches {
		if _, ok := baseMatches[k]; ok {
			delta.Persisting = append(delta.Persisting, v)
		} else {
			delta.Introduced = append(delta.Introduced, v)
		}
	}
	for k, v := range baseMatches {
		if _, ok := headMatches[k]; !ok {
			delta.Resolved = append(delta.Resolved, v)
		}
	}
	sortCapabilityMatches(delta.Introduced)
	sortCapabilityMatches(delta.Resolved)
	sortCapabilityMatches(delta.Persisting)
	return delta
}

func sortCapabilityMatches(items []CapabilityMatch) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].Name != items[j].Name {
			return items[i].Name < items[j].Name
		}
		return items[i].File < items[j].File
	})
}
