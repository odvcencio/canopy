package contextbundle

import (
	"bytes"
	"context"
	"fmt"
	"sort"
)

// PlainRenderer is a minimal, dependency-free Renderer implementation. It
// exists for tests and as a documented fallback; the production renderer is
// pkg/contextbundle/lxrender, which renders through LX (decision #3: "Canopy
// selects; LX renders"). PlainRenderer follows the same section ordering,
// per-item delimiting, and independent manifest measurement (spec 9.3, 9.5,
// 9.7) so it is a faithful, if unstyled, stand-in.
type PlainRenderer struct {
	Tokenizer Tokenizer
}

// Render implements Renderer.
func (r PlainRenderer) Render(ctx context.Context, plan RenderPlan, budget int) (RenderedBundle, error) {
	tok := r.Tokenizer
	if tok == nil {
		tok = defaultTokenizer{}
	}

	bySection := groupBySection(plan.Items)

	var buf bytes.Buffer
	var manifest []ProjectionRecord
	var totalTokens int64

	for _, section := range sectionOrder {
		items, ok := bySection[section]
		if !ok || len(items) == 0 {
			continue
		}
		fmt.Fprintf(&buf, "## %s\n\n", sectionTitle(section))
		for _, item := range items {
			estTokens := tok.Estimate(int64(len(item.Variant.Content)), item.Variant.Content)
			totalTokens += estTokens

			fmt.Fprintf(&buf, "<!-- evidence:item=%s snapshot=%s mode=%s -->\n", item.ItemID, plan.Snapshot.ID, item.Variant.Mode)
			fmt.Fprintf(&buf, "### `%s", item.Path)
			if item.Variant.StartLine > 0 {
				fmt.Fprintf(&buf, ":%d-%d", item.Variant.StartLine, item.Variant.EndLine)
			}
			buf.WriteString("`\n\n")
			if item.Variant.Mode != ProjectionReceiptOnly {
				buf.WriteString("```\n")
				buf.Write(item.Variant.Content)
				if len(item.Variant.Content) == 0 || item.Variant.Content[len(item.Variant.Content)-1] != '\n' {
					buf.WriteByte('\n')
				}
				buf.WriteString("```\n\n")
			}

			manifest = append(manifest, ProjectionRecord{
				ItemID:           item.ItemID,
				Section:          string(section),
				Path:             item.Path,
				EntityID:         item.EntityID,
				Language:         item.Language,
				Mode:             item.Variant.Mode,
				StartLine:        item.Variant.StartLine,
				EndLine:          item.Variant.EndLine,
				OriginalBytes:    item.Variant.RawBytes,
				ProjectedBytes:   int64(len(item.Variant.Content)),
				EstimatedTokens:  estTokens,
				ContentSHA256:    contentDigest(item.Variant.Content),
				SelectionScore:   item.Score,
				SelectionReasons: item.Reasons,
				Required:         item.Required,
			})
		}
	}

	return RenderedBundle{
		Content:         buf.Bytes(),
		Items:           manifest,
		EstimatedTokens: totalTokens,
	}, nil
}

func groupBySection(items []PlanItem) map[Section][]PlanItem {
	out := map[Section][]PlanItem{}
	for _, item := range items {
		out[item.Variant.Section] = append(out[item.Variant.Section], item)
	}
	for section := range out {
		sort.Slice(out[section], func(i, j int) bool { return planItemLess(out[section][i], out[section][j]) })
	}
	return out
}

func sectionTitle(s Section) string {
	switch s {
	case SectionCheckpoint:
		return "Task checkpoint"
	case SectionRepoMap:
		return "Repository map"
	case SectionChanges:
		return "Changed entities"
	case SectionFocus:
		return "Focus implementation"
	case SectionCallers:
		return "Incoming callers"
	case SectionCallees:
		return "Outgoing dependencies"
	case SectionTypes:
		return "Related types and contracts"
	case SectionTests:
		return "Tests and verification"
	case SectionFailures:
		return "Failures and runtime evidence"
	case SectionMemory:
		return "Project decisions and lessons"
	case SectionOmissions:
		return "Omissions and receipt"
	default:
		return string(s)
	}
}
