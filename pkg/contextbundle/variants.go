package contextbundle

import "m31labs.dev/canopy/pkg/model"

// informationValue scores how much a projection mode tells the model,
// independent of any one candidate's selection score. Used by the packer's
// utility calculation (spec 10.9).
var informationValue = map[ProjectionMode]int{
	ProjectionFull:        1000,
	ProjectionBody:        850,
	ProjectionHeadTail:    650,
	ProjectionSkeleton:    450,
	ProjectionSignature:   300,
	ProjectionMap:         300,
	ProjectionMetadata:    100,
	ProjectionReceiptOnly: 10,
}

// requiredFloorMode is the leanest mode a required item may downgrade to.
// Required items must stay meaningfully useful, not merely present as a
// receipt stub (spec 10.7: "Required items MUST NOT be omitted").
const requiredFloorMode = ProjectionSignature

// initialMode picks the richest starting projection for a candidate based on
// its section and the request's task kind, following the per-mode defaults
// in spec 10.10.
func initialMode(c *candidateItem, req Request) ProjectionMode {
	if c.Flags.ExplicitRequired {
		return ProjectionFull
	}
	switch c.Section {
	case SectionRepoMap:
		return ProjectionMap
	case SectionTypes:
		return ProjectionSignature
	case SectionCallers, SectionCallees:
		return ProjectionSignature
	case SectionTests:
		if req.Intent.Kind == TaskTest {
			return ProjectionBody
		}
		return ProjectionSignature
	case SectionFailures:
		return ProjectionBody
	case SectionFocus, SectionChanges:
		if req.Intent.Kind == TaskExplore {
			return ProjectionSignature
		}
		return ProjectionFull
	default:
		return ProjectionSignature
	}
}

// mapDowngradeChain is the downgrade chain for repository-map (whole-file)
// candidates. ProjectionMap sits outside the symbol-level downgrade order
// (spec 10.7's sequence runs full -> ... -> receipt-only and never mentions
// map); a repo-map entry should never escalate to a full/body read of every
// file, so it gets its own short chain instead of falling into
// downgradeOrder's general sequence.
var mapDowngradeChain = []ProjectionMode{ProjectionMap, ProjectionMetadata, ProjectionReceiptOnly}

// downgradeChain returns the ordered sequence of modes from start down to
// the applicable floor, following the canonical downgrade order (spec 10.7).
func downgradeChain(start ProjectionMode, required bool) []ProjectionMode {
	if start == ProjectionMap {
		if required {
			return []ProjectionMode{ProjectionMap}
		}
		return append([]ProjectionMode(nil), mapDowngradeChain...)
	}

	startIdx := -1
	for i, m := range downgradeOrder {
		if m == start {
			startIdx = i
			break
		}
	}
	if startIdx == -1 {
		startIdx = 0
	}
	floor := ProjectionReceiptOnly
	if required {
		floor = requiredFloorMode
	}
	floorIdx := len(downgradeOrder) - 1
	for i, m := range downgradeOrder {
		if m == floor {
			floorIdx = i
			break
		}
	}
	if floorIdx < startIdx {
		floorIdx = startIdx
	}
	return append([]ProjectionMode(nil), downgradeOrder[startIdx:floorIdx+1]...)
}

// candidateVariants is every downgrade-chain variant available for one
// candidate, in richest-first order.
type candidateVariants struct {
	Candidate *candidateItem
	Variants  []Variant
}

// buildVariants generates the full downgrade chain of variants for every
// candidate (spec 10.7). Content for symbol-backed candidates is read from
// source by indexed line span; repository-map candidates render straight
// from already-indexed symbol signatures (decision #4).
func buildVariants(idx *model.Index, root string, candidates []*candidateItem, req Request) []candidateVariants {
	out := make([]candidateVariants, 0, len(candidates))
	for _, c := range candidates {
		start := initialMode(c, req)
		chain := downgradeChain(start, c.Required)
		variants := make([]Variant, 0, len(chain))
		for _, mode := range chain {
			var content []byte
			var rawBytes int64
			var err error
			if c.Kind == "file" {
				content, rawBytes = projectFile(idx, c.Path, mode)
			} else {
				content, rawBytes, err = projectSymbol(root, c, mode, req.LineNumbers)
				if err != nil {
					// Source is unreadable (e.g. deleted since index build):
					// fall back to the always-available signature/metadata
					// text rather than dropping the candidate outright.
					content = []byte(metadataLine(c))
					rawBytes = 0
				}
			}
			startLine, endLine := c.StartLine, c.EndLine
			if mode == ProjectionFull && c.Kind != "file" {
				// Full mode renders the whole file, not just the
				// candidate's own span, so the header should not claim a
				// partial line range (spec 9.4: "Full file | Head: -1").
				startLine, endLine = 0, 0
			}
			variants = append(variants, Variant{
				CandidateID:      c.ID,
				Mode:             mode,
				Section:          c.Section,
				Content:          content,
				RawBytes:         rawBytes,
				InformationValue: informationValue[mode],
				StartLine:        startLine,
				EndLine:          endLine,
			})
		}
		out = append(out, candidateVariants{Candidate: c, Variants: variants})
	}
	return out
}
