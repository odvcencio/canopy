package contextbundle

import (
	"fmt"
	"sort"
	"strings"
)

// buildOmissionSummary accounts for every candidate that did not make it
// into the final bundle, so the model can ask for it by name instead of
// assuming it does not exist (spec 10.7, 10.11: OmissionSummary).
func buildOmissionSummary(totalCandidates, selectedCount int, reasons map[string]int, entities []OmittedEntity) OmissionSummary {
	return OmissionSummary{
		TotalCandidates: totalCandidates,
		Omitted:         totalCandidates - selectedCount,
		ByReason:        reasons,
		Entities:        entities,
	}
}

// omissionSummaryItem renders the compact receipt/omissions summary the
// model template must end with (spec 9.5). It is appended to every plan as
// an ordinary metadata-mode PlanItem in SectionOmissions so renderers need
// no special case for it.
func omissionSummaryItem(o OmissionSummary) PlanItem {
	var b strings.Builder
	fmt.Fprintf(&b, "%d candidates evaluated, %d selected, %d omitted", o.TotalCandidates, o.TotalCandidates-o.Omitted, o.Omitted)
	if len(o.ByReason) > 0 {
		reasons := make([]string, 0, len(o.ByReason))
		for reason, count := range o.ByReason {
			reasons = append(reasons, fmt.Sprintf("%s=%d", reason, count))
		}
		sort.Strings(reasons)
		fmt.Fprintf(&b, " (%s)", strings.Join(reasons, ", "))
	}
	b.WriteString(". Ask for an omitted entity by path to expand it.")

	content := []byte(b.String())
	return PlanItem{
		ItemID: "itm_omission_summary",
		Path:   "omissions",
		Variant: Variant{
			Section:          SectionOmissions,
			Mode:             ProjectionMetadata,
			Content:          content,
			InformationValue: 0,
		},
	}
}
