package treesitter

import (
	"bytes"
	"sort"
	"unicode"

	"github.com/odvcencio/gotreesitter"

	"m31labs.dev/canopy/pkg/model"
)

const maxParseGaps = 64

func buildParseCoverage(root *gotreesitter.Node, src []byte, symbols []model.Symbol, lang *gotreesitter.Language) *model.ParseCoverage {
	coverage := &model.ParseCoverage{Status: model.ParseCoverageClean}
	if root == nil {
		if len(src) == 0 {
			return coverage
		}
		coverage.Status = model.ParseCoverageStopped
		coverage.StopReason = "missing_root"
		coverage.Gaps = []model.ParseGap{sourceGap("unparsed", "source", src, 0, len(src))}
		return coverage
	}

	raw := collectParseGaps(root, src, coverage, lang)
	coverage.Gaps = subtractRecoveredGaps(raw, symbols, coverage)
	appendUnparsedSourceGaps(coverage, root, src)
	if (len(coverage.Gaps) > 0 || coverage.Truncated) && coverage.Status == model.ParseCoverageClean {
		coverage.Status = model.ParseCoveragePartial
	}
	return coverage
}

func collectParseGaps(root *gotreesitter.Node, src []byte, coverage *model.ParseCoverage, lang *gotreesitter.Language) []model.ParseGap {
	if root == nil || !root.HasErrorOrMissing() {
		return nil
	}

	stack := []*gotreesitter.Node{root}
	gaps := make([]model.ParseGap, 0, 4)
	for len(stack) > 0 {
		last := len(stack) - 1
		node := stack[last]
		stack = stack[:last]
		if node == nil {
			continue
		}

		if node.IsError() || node.IsMissing() {
			if isEOFTerminatorMiss(node, len(src)) {
				coverage.IgnoredEOFMissingRegions++
				continue
			}
			if len(gaps) >= maxParseGaps {
				coverage.Truncated = true
				break
			}
			kind := "error"
			if node.IsMissing() {
				kind = "missing"
				coverage.MissingNodes++
			} else {
				coverage.ErrorNodes++
			}
			gaps = append(gaps, nodeGap(kind, node, lang))
			continue
		}

		for i := node.ChildCount() - 1; i >= 0; i-- {
			stack = append(stack, node.Child(i))
		}
	}
	return gaps
}

func isEOFTerminatorMiss(node *gotreesitter.Node, sourceLen int) bool {
	if node == nil || !node.IsMissing() || sourceLen < 0 {
		return false
	}
	return node.StartByte() == node.EndByte() && node.EndByte() == uint32(sourceLen)
}

func nodeGap(kind string, node *gotreesitter.Node, lang *gotreesitter.Language) model.ParseGap {
	if node == nil {
		return model.ParseGap{Kind: kind}
	}
	rng := node.Range()
	return model.ParseGap{
		Kind:        kind,
		NodeType:    nodeTypeForGap(node, lang),
		StartByte:   rng.StartByte,
		EndByte:     rng.EndByte,
		StartLine:   int(rng.StartPoint.Row) + 1,
		EndLine:     int(rng.EndPoint.Row) + 1,
		StartColumn: int(rng.StartPoint.Column) + 1,
		EndColumn:   int(rng.EndPoint.Column) + 1,
	}
}

func nodeTypeForGap(node *gotreesitter.Node, lang *gotreesitter.Language) string {
	if node == nil {
		return ""
	}
	if lang != nil {
		if nodeType := node.Type(lang); nodeType != "" {
			return nodeType
		}
	}
	if node.IsError() {
		return "ERROR"
	}
	// A missing node's grammar type is useful, but resolving it requires the
	// language. The caller records "MISSING" consistently across languages.
	if node.IsMissing() {
		return "MISSING"
	}
	return ""
}

func subtractRecoveredGaps(gaps []model.ParseGap, symbols []model.Symbol, coverage *model.ParseCoverage) []model.ParseGap {
	if len(gaps) == 0 || len(symbols) == 0 {
		return gaps
	}
	kept := make([]model.ParseGap, 0, len(gaps))
	for _, gap := range gaps {
		if gapRecoveredBySymbols(gap, symbols) {
			coverage.RecoveredRegions++
			continue
		}
		kept = append(kept, gap)
	}
	return kept
}

func gapRecoveredBySymbols(gap model.ParseGap, symbols []model.Symbol) bool {
	if gap.StartLine <= 0 || gap.EndLine < gap.StartLine {
		return false
	}
	type lineSpan struct {
		start int
		end   int
	}
	spans := make([]lineSpan, 0, len(symbols))
	for _, symbol := range symbols {
		if symbol.StartLine < gap.StartLine || symbol.StartLine > gap.EndLine {
			continue
		}
		end := symbol.EndLine
		if end < symbol.StartLine {
			end = symbol.StartLine
		}
		spans = append(spans, lineSpan{start: symbol.StartLine, end: end})
	}
	if len(spans) == 0 {
		return false
	}
	sort.Slice(spans, func(i, j int) bool {
		if spans[i].start == spans[j].start {
			return spans[i].end < spans[j].end
		}
		return spans[i].start < spans[j].start
	})

	coveredTo := gap.StartLine - 1
	for _, span := range spans {
		if span.start > coveredTo+1 {
			return false
		}
		if span.end > coveredTo {
			coveredTo = span.end
		}
	}
	return coveredTo >= gap.EndLine
}

func appendUnparsedSourceGaps(coverage *model.ParseCoverage, root *gotreesitter.Node, src []byte) {
	if coverage == nil || root == nil || len(src) == 0 {
		return
	}
	start := clampByteOffset(root.StartByte(), len(src))
	end := clampByteOffset(root.EndByte(), len(src))
	if start > 0 {
		if gap, ok := nonWhitespaceGap("unparsed", "source_prefix", src, 0, start); ok {
			appendSyntheticGap(coverage, gap)
		}
	}
	if end < len(src) {
		if gap, ok := nonWhitespaceGap("unparsed", "source_tail", src, end, len(src)); ok {
			appendSyntheticGap(coverage, gap)
		}
	}
}

func appendSyntheticGap(coverage *model.ParseCoverage, gap model.ParseGap) {
	for _, existing := range coverage.Gaps {
		if existing.StartByte <= gap.StartByte && existing.EndByte >= gap.EndByte {
			return
		}
	}
	if len(coverage.Gaps) >= maxParseGaps {
		coverage.Truncated = true
		return
	}
	coverage.Gaps = append(coverage.Gaps, gap)
	coverage.Status = model.ParseCoverageStopped
	if coverage.StopReason == "" {
		coverage.StopReason = "source_not_fully_parsed"
	}
}

func nonWhitespaceGap(kind, nodeType string, src []byte, start, end int) (model.ParseGap, bool) {
	start, end = clampSourceRange(start, end, len(src))
	segment := src[start:end]
	left := bytes.TrimLeftFunc(segment, unicode.IsSpace)
	if len(left) == 0 {
		return model.ParseGap{}, false
	}
	realStart := end - len(left)
	right := bytes.TrimRightFunc(left, unicode.IsSpace)
	realEnd := realStart + len(right)
	return sourceGap(kind, nodeType, src, realStart, realEnd), true
}

func sourceGap(kind, nodeType string, src []byte, start, end int) model.ParseGap {
	start, end = clampSourceRange(start, end, len(src))
	startPoint := pointAtOffset(src, start)
	endPoint := pointAtOffset(src, end)
	return model.ParseGap{
		Kind:        kind,
		NodeType:    nodeType,
		StartByte:   uint32(start),
		EndByte:     uint32(end),
		StartLine:   int(startPoint.Row) + 1,
		EndLine:     int(endPoint.Row) + 1,
		StartColumn: int(startPoint.Column) + 1,
		EndColumn:   int(endPoint.Column) + 1,
	}
}

func clampSourceRange(start, end, sourceLen int) (int, int) {
	if start < 0 {
		start = 0
	}
	if start > sourceLen {
		start = sourceLen
	}
	if end < start {
		end = start
	}
	if end > sourceLen {
		end = sourceLen
	}
	return start, end
}

func clampByteOffset(offset uint32, sourceLen int) int {
	if uint64(offset) > uint64(sourceLen) {
		return sourceLen
	}
	return int(offset)
}

func applyTreeStopReceipt(coverage *model.ParseCoverage, tree *gotreesitter.Tree) {
	if coverage == nil || tree == nil {
		return
	}
	reason := tree.ParseStopReason()
	if !tree.ParseStoppedEarly() && (reason == "" || reason == gotreesitter.ParseStopNone || reason == gotreesitter.ParseStopAccepted) {
		return
	}
	coverage.Status = model.ParseCoverageStopped
	if reason == "" || reason == gotreesitter.ParseStopNone || reason == gotreesitter.ParseStopAccepted {
		coverage.StopReason = "stopped_early"
		return
	}
	coverage.StopReason = string(reason)
}
