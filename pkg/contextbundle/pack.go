package contextbundle

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sort"
)

// packOutcome is the result of budget packing plus its final render: the
// selected plan, the rendered bundle that satisfies the tolerance, and the
// bookkeeping needed for the receipt and metrics.
type packOutcome struct {
	Plan       RenderPlan
	Rendered   RenderedBundle
	Omissions  OmissionSummary
	Downgrades int
	Rerenders  int
}

// selection tracks one candidate's current position in its own downgrade
// chain during packing. levelIdx indexes into Variants (0 = richest,
// -1 = not yet selected).
type selection struct {
	group    *candidateVariants
	levelIdx int
	tokens   int64
}

// packBudget implements the deterministic greedy variant packer (spec 10.9):
// required items are reserved at their floor, optional items are added by
// descending marginal utility per token, leftover budget upgrades already
// selected items, and the final render is checked against tolerance and
// downgraded/re-rendered until it fits or nothing optional remains to
// downgrade.
func packBudget(ctx context.Context, req Request, snapshot Snapshot, groups []candidateVariants, tok Tokenizer, renderer Renderer) (*packOutcome, error) {
	total, sectionBudgets := normalizeBudget(req.Budget)
	tolerance := budgetTolerance(req.Budget)

	sels := make([]*selection, len(groups))
	for i := range groups {
		sels[i] = &selection{group: &groups[i], levelIdx: -1}
	}

	// Pre-compute token estimates for every variant of every candidate.
	// Content never changes during packing, only which level is selected.
	tokensOf := make(map[string][]int64, len(groups))
	for i := range groups {
		g := &groups[i]
		estimates := make([]int64, len(g.Variants))
		for j, v := range g.Variants {
			estimates[j] = tok.Estimate(int64(len(v.Content)), v.Content)
		}
		tokensOf[g.Candidate.ID] = estimates
	}

	// budgetCap is what optional selection and upgrades may spend against,
	// after step 4's fixed checkpoint/repository metadata reservation.
	budgetCap := int64(total) - checkpointReserve
	if budgetCap < 0 {
		budgetCap = 0
	}

	usedTotal := int64(0)
	usedSection := map[Section]int64{}
	seenDigest := map[string]bool{}

	// Steps 2-3: reserve required items at their floor (leanest) variant.
	for _, s := range sels {
		if !s.group.Candidate.Required {
			continue
		}
		floorIdx := len(s.group.Variants) - 1
		v := s.group.Variants[floorIdx]
		tokens := tokensOf[s.group.Candidate.ID][floorIdx]
		s.levelIdx = floorIdx
		s.tokens = tokens
		usedTotal += tokens
		usedSection[v.Section] += tokens
		seenDigest[contentDigest(v.Content)] = true
	}
	if usedTotal > int64(total) {
		return nil, &ErrRequiredEvidenceExceedsBudget{MinimumTokens: int(usedTotal), RequestTokens: total}
	}
	if usedTotal > budgetCap {
		budgetCap = usedTotal
	}

	// Steps 5-8: add optional candidates by descending marginal utility per
	// token, downgrading a candidate that does not fit until it does or
	// omitting it.
	omittedReasons := map[string]int{}
	var omittedEntities []OmittedEntity

	optional := make([]*selection, 0, len(sels))
	pending := make(map[string]bool, len(sels))
	for _, s := range sels {
		if !s.group.Candidate.Required {
			optional = append(optional, s)
			pending[s.group.Candidate.ID] = true
		}
	}

	for len(pending) > 0 {
		best, bestIdx, _, found := bestPendingChoice(optional, pending, tokensOf, seenDigest)
		if !found {
			break
		}
		delete(pending, best.group.Candidate.ID)

		chosen, tokens, variant := fitVariant(best, bestIdx, tokensOf, usedTotal, budgetCap, usedSection, sectionBudgets, seenDigest)
		if chosen == -1 {
			omittedReasons["budget_exhausted"]++
			omittedEntities = append(omittedEntities, omittedEntityFor(best.group.Candidate, "budget_exhausted"))
			continue
		}

		best.levelIdx = chosen
		best.tokens = tokens
		usedTotal += tokens
		usedSection[variant.Section] += tokens
		seenDigest[contentDigest(variant.Content)] = true
	}

	selectedCount := 0
	for _, s := range sels {
		if s.levelIdx >= 0 {
			selectedCount++
		}
	}
	omissions := buildOmissionSummary(len(groups), selectedCount, omittedReasons, omittedEntities)
	summaryItem := omissionSummaryItem(omissions)

	// Step 9: spend leftover budget on the highest-marginal single-step
	// upgrade across every already-selected candidate (required or
	// optional), repeating until nothing more fits.
	for {
		s, nextIdx, gain, ok := bestUpgrade(sels, tokensOf, usedTotal, budgetCap, usedSection, sectionBudgets)
		if !ok || gain <= 0 {
			break
		}
		curTokens := tokensOf[s.group.Candidate.ID][s.levelIdx]
		nextTokens := tokensOf[s.group.Candidate.ID][nextIdx]
		usedSection[s.group.Variants[s.levelIdx].Section] -= curTokens
		usedTotal -= curTokens
		s.levelIdx = nextIdx
		s.tokens = nextTokens
		usedTotal += nextTokens
		usedSection[s.group.Variants[nextIdx].Section] += nextTokens
	}

	buildPlan := func() RenderPlan {
		items := make([]PlanItem, 0, len(sels))
		for _, s := range sels {
			if s.levelIdx < 0 {
				continue
			}
			c := s.group.Candidate
			v := s.group.Variants[s.levelIdx]
			items = append(items, PlanItem{
				ItemID:   itemID(c.ID, v.Mode),
				EntityID: c.EntityID,
				Path:     c.Path,
				Language: c.Language,
				Variant:  v,
				Required: c.Required,
				Score:    c.Score,
				Reasons:  c.Reasons,
			})
		}
		items = append(items, summaryItem)
		sort.Slice(items, func(i, j int) bool { return planItemLess(items[i], items[j]) })
		return RenderPlan{
			Snapshot:     snapshot,
			OutputFormat: req.OutputFormat,
			LineNumbers:  req.LineNumbers,
			Items:        items,
		}
	}

	plan := buildPlan()
	rendered, err := renderer.Render(ctx, plan, total)
	if err != nil {
		return nil, err
	}

	// Steps 10-12: check the actual render against tolerance; downgrade the
	// lowest-marginal optional item and rerender until it fits or nothing
	// optional remains.
	downgrades, rerenders := 0, 0
	maxIterations := len(sels) + 4
	for iter := 0; iter < maxIterations; iter++ {
		estimate := rendered.EstimatedTokens
		if estimate == 0 && len(rendered.Content) > 0 {
			estimate = tok.Estimate(int64(len(rendered.Content)), rendered.Content)
		}
		if float64(estimate) <= float64(total)*(1+tolerance) {
			break
		}
		s := lowestMarginalDowngradable(sels)
		if s == nil {
			break
		}
		curTokens := tokensOf[s.group.Candidate.ID][s.levelIdx]
		usedSection[s.group.Variants[s.levelIdx].Section] -= curTokens
		usedTotal -= curTokens
		s.levelIdx++
		downgrades++
		nextTokens := tokensOf[s.group.Candidate.ID][s.levelIdx]
		usedTotal += nextTokens
		usedSection[s.group.Variants[s.levelIdx].Section] += nextTokens

		plan = buildPlan()
		rendered, err = renderer.Render(ctx, plan, total)
		if err != nil {
			return nil, err
		}
		rerenders++
	}

	return &packOutcome{
		Plan:       plan,
		Rendered:   rendered,
		Omissions:  omissions,
		Downgrades: downgrades,
		Rerenders:  rerenders,
	}, nil
}

func omittedEntityFor(c *candidateItem, reason string) OmittedEntity {
	return OmittedEntity{
		EntityID: c.EntityID,
		Path:     c.Path,
		Section:  c.Section,
		Reason:   reason,
		Score:    c.Score,
	}
}

// fitVariant walks a candidate's chain from startIdx downward (leaner) for
// the first variant that fits the remaining total and section budgets and is
// not a duplicate of already-selected content, returning its index, token
// cost, and the variant itself, or -1 if nothing fits.
func fitVariant(s *selection, startIdx int, tokensOf map[string][]int64, usedTotal, budgetCap int64, usedSection map[Section]int64, sectionBudgets map[Section]int, seenDigest map[string]bool) (int, int64, Variant) {
	for idx := startIdx; idx < len(s.group.Variants); idx++ {
		v := s.group.Variants[idx]
		if seenDigest[contentDigest(v.Content)] {
			continue
		}
		t := tokensOf[s.group.Candidate.ID][idx]
		if usedTotal+t > budgetCap {
			continue
		}
		if cap := sectionBudgets[v.Section]; cap > 0 && usedSection[v.Section]+t > int64(cap) {
			continue
		}
		return idx, t, v
	}
	return -1, 0, Variant{}
}

// bestPendingChoice finds the pending optional candidate whose richest
// still-fitting variant has the highest utility per token, applying the
// spec's deterministic tie-break on ties (spec 10.8, 10.9). This does not
// enforce the budget itself (fitVariant does); it only ranks candidates by
// their best theoretical variant so the highest-value candidate is tried
// first.
func bestPendingChoice(optional []*selection, pending map[string]bool, tokensOf map[string][]int64, seenDigest map[string]bool) (*selection, int, float64, bool) {
	var best *selection
	bestIdx := -1
	bestUtil := 0.0
	found := false

	for _, s := range optional {
		if !pending[s.group.Candidate.ID] {
			continue
		}
		idx, util, ok := richestUnseenVariant(s, tokensOf, seenDigest)
		if !ok {
			continue
		}
		if !found || util > bestUtil || (util == bestUtil && candidateTieBreak(s.group.Candidate, best.group.Candidate, s.group.Variants[idx].Mode, best.group.Variants[bestIdx].Mode)) {
			best, bestIdx, bestUtil, found = s, idx, util, true
		}
	}
	return best, bestIdx, bestUtil, found
}

func richestUnseenVariant(s *selection, tokensOf map[string][]int64, seenDigest map[string]bool) (int, float64, bool) {
	for idx := 0; idx < len(s.group.Variants); idx++ {
		v := s.group.Variants[idx]
		if seenDigest[contentDigest(v.Content)] {
			continue
		}
		tokens := tokensOf[s.group.Candidate.ID][idx]
		util := utilityOf(s.group.Candidate, v, tokens)
		return idx, util / float64(maxInt64(tokens, 1)), true
	}
	return -1, 0, false
}

func utilityOf(c *candidateItem, v Variant, tokens int64) float64 {
	tokenCost := (tokens + 7) / 8
	return float64(v.InformationValue + c.Score - int(tokenCost))
}

// bestUpgrade finds the highest-marginal-utility single-step upgrade across
// every already-selected candidate that still fits the remaining budget
// (spec 10.9 step 9).
func bestUpgrade(sels []*selection, tokensOf map[string][]int64, usedTotal, budgetCap int64, usedSection map[Section]int64, sectionBudgets map[Section]int) (*selection, int, float64, bool) {
	var best *selection
	bestNext := -1
	bestGain := 0.0
	found := false

	for _, s := range sels {
		if s.levelIdx <= 0 {
			continue // unselected, or already at richest
		}
		nextIdx := s.levelIdx - 1
		curTokens := tokensOf[s.group.Candidate.ID][s.levelIdx]
		nextTokens := tokensOf[s.group.Candidate.ID][nextIdx]
		delta := nextTokens - curTokens
		if delta < 0 {
			delta = 0
		}
		if usedTotal+delta > budgetCap {
			continue
		}
		nextVariant := s.group.Variants[nextIdx]
		if cap := sectionBudgets[nextVariant.Section]; cap > 0 && usedSection[nextVariant.Section]+delta > int64(cap) {
			continue
		}
		curUtil := utilityOf(s.group.Candidate, s.group.Variants[s.levelIdx], curTokens)
		nextUtil := utilityOf(s.group.Candidate, nextVariant, nextTokens)
		gain := (nextUtil - curUtil) / float64(maxInt64(delta, 1))
		if !found || gain > bestGain {
			best, bestNext, bestGain, found = s, nextIdx, gain, true
		}
	}
	return best, bestNext, bestGain, found
}

// lowestMarginalDowngradable finds the selected, non-required candidate
// currently contributing the least utility per token, to downgrade first
// when the final render exceeds tolerance (spec 10.9 step 11). Required
// items never downgrade below their floor.
func lowestMarginalDowngradable(sels []*selection) *selection {
	var worst *selection
	worstUtil := 0.0
	found := false
	for _, s := range sels {
		if s.levelIdx < 0 || s.group.Candidate.Required {
			continue
		}
		if s.levelIdx >= len(s.group.Variants)-1 {
			continue // already at floor
		}
		util := utilityOf(s.group.Candidate, s.group.Variants[s.levelIdx], s.tokens)
		if !found || util < worstUtil {
			worst, worstUtil, found = s, util, true
		}
	}
	return worst
}

func planItemLess(a, b PlanItem) bool {
	if sectionRank(a.Variant.Section) != sectionRank(b.Variant.Section) {
		return sectionRank(a.Variant.Section) < sectionRank(b.Variant.Section)
	}
	if a.Path != b.Path {
		return a.Path < b.Path
	}
	if a.Variant.StartLine != b.Variant.StartLine {
		return a.Variant.StartLine < b.Variant.StartLine
	}
	if a.EntityID != b.EntityID {
		return a.EntityID < b.EntityID
	}
	return a.Variant.Mode < b.Variant.Mode
}

func contentDigest(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func itemID(candidateID string, mode ProjectionMode) string {
	sum := sha256.Sum256([]byte(candidateID + "\x00" + string(mode)))
	return "itm_" + hex.EncodeToString(sum[:])[:16]
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
