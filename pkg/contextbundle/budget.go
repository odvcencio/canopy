package contextbundle

// defaultTotalBudget applies when a request does not specify a token budget.
const defaultTotalBudget = 8000

// defaultTolerance is the packer's overflow allowance on the rendered
// estimate versus the requested budget (spec 9.6: rendered_estimate <=
// requested_budget * 1.02).
const defaultTolerance = 0.02

// checkpointReserve is a fixed token allowance held back for the checkpoint
// and repository metadata reservations (spec 10.9 step 4), before optional
// variants compete for the remainder.
const checkpointReserve = 64

func normalizeBudget(b Budget) (total int, sections map[Section]int) {
	total = b.TotalTokens
	if total <= 0 {
		total = defaultTotalBudget
	}
	sections = make(map[Section]int, len(b.Sections))
	for k, v := range b.Sections {
		sections[k] = v
	}
	return total, sections
}

func budgetTolerance(b Budget) float64 {
	if b.Tolerance > 0 {
		return b.Tolerance
	}
	return defaultTolerance
}
