package treesitter

import (
	"testing"

	"github.com/odvcencio/gotreesitter"

	"m31labs.dev/canopy/pkg/model"
)

func TestParseCoverageCleanSource(t *testing.T) {
	parser, err := NewParser(findEntryByExtension(t, ".go"))
	if err != nil {
		t.Fatalf("NewParser returned error: %v", err)
	}

	summary, err := parser.Parse("main.go", []byte("package demo\n\nfunc Work() {}\n"))
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if summary.ParseCoverage == nil {
		t.Fatal("expected a parse coverage receipt")
	}
	if got := summary.ParseCoverage.Status; got != model.ParseCoverageClean {
		t.Fatalf("coverage status = %q, want clean: %+v", got, summary.ParseCoverage)
	}
	if len(summary.ParseCoverage.Gaps) != 0 {
		t.Fatalf("clean source has gaps: %+v", summary.ParseCoverage.Gaps)
	}
}

func TestParseCoverageReportsMalformedSource(t *testing.T) {
	parser, err := NewParser(findEntryByExtension(t, ".go"))
	if err != nil {
		t.Fatalf("NewParser returned error: %v", err)
	}

	summary, err := parser.Parse("broken.go", []byte("package demo\n\nfunc Broken( {\n"))
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if summary.ParseCoverage == nil {
		t.Fatal("expected a parse coverage receipt")
	}
	if summary.ParseCoverage.Status == model.ParseCoverageClean {
		t.Fatalf("malformed source reported clean: %+v", summary.ParseCoverage)
	}
	if len(summary.ParseCoverage.Gaps) == 0 {
		t.Fatalf("malformed source reported no gaps: %+v", summary.ParseCoverage)
	}
	gap := summary.ParseCoverage.Gaps[0]
	if gap.StartLine <= 0 || gap.EndLine < gap.StartLine || gap.EndByte < gap.StartByte {
		t.Fatalf("invalid gap coordinates: %+v", gap)
	}
}

func TestParseCoverageReportsNonWhitespaceTail(t *testing.T) {
	source := []byte("package demo\ntrailing")
	root := gotreesitter.NewLeafNode(0, true, 0, uint32(len("package demo\n")), gotreesitter.Point{}, gotreesitter.Point{Row: 1})
	coverage := buildParseCoverage(root, source, nil, nil)

	if coverage.Status != model.ParseCoverageStopped || coverage.StopReason != "source_not_fully_parsed" {
		t.Fatalf("unexpected coverage: %+v", coverage)
	}
	if len(coverage.Gaps) != 1 || coverage.Gaps[0].NodeType != "source_tail" {
		t.Fatalf("unexpected tail gaps: %+v", coverage.Gaps)
	}
}

func TestGapRecoveredBySymbolsRequiresFullLineCoverage(t *testing.T) {
	gap := model.ParseGap{StartLine: 10, EndLine: 14}
	if !gapRecoveredBySymbols(gap, []model.Symbol{
		{StartLine: 10, EndLine: 11},
		{StartLine: 12, EndLine: 14},
	}) {
		t.Fatal("expected contiguous symbol spans to recover the gap")
	}
	if gapRecoveredBySymbols(gap, []model.Symbol{
		{StartLine: 10, EndLine: 11},
		{StartLine: 13, EndLine: 14},
	}) {
		t.Fatal("expected an uncovered line to preserve the gap")
	}
}

func TestEmptySourceHasCleanReceipt(t *testing.T) {
	coverage := buildParseCoverage(nil, nil, nil, nil)
	if coverage.Status != model.ParseCoverageClean || len(coverage.Gaps) != 0 {
		t.Fatalf("unexpected empty-source receipt: %+v", coverage)
	}
}
