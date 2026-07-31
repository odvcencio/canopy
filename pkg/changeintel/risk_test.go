package changeintel

import (
	"context"
	"testing"
)

// TestAnalyze_RiskComponents verifies the routing-risk score is only
// computed when requested, stays within [0,1], and records the individual
// weighted components the spec requires (section 11.5).
func TestAnalyze_RiskComponents(t *testing.T) {
	repo := newTestRepo(t)
	repo.write("go.mod", "module example.com/sample\n\ngo 1.21\n")
	repo.write("api.go", "package sample\n\nfunc Exported() {}\n")
	base := repo.commit("base")
	repo.write("api.go", "package sample\n\nfunc Exported(x int) string {\n\tif x > 0 {\n\t\treturn \"pos\"\n\t}\n\treturn \"\"\n}\n")

	svc := NewService(WithClock(fixedClock()))

	withoutRisk, err := svc.Analyze(context.Background(), Request{
		Root: repo.dir,
		Base: SnapshotRef{Ref: base},
	})
	if err != nil {
		t.Fatalf("Analyze returned error: %v", err)
	}
	if withoutRisk.Risk.Score != 0 {
		t.Fatalf("expected zero-value risk when IncludeRisk is false, got %+v", withoutRisk.Risk)
	}

	receipt, err := svc.Analyze(context.Background(), Request{
		Root:        repo.dir,
		Base:        SnapshotRef{Ref: base},
		IncludeRisk: true,
	})
	if err != nil {
		t.Fatalf("Analyze returned error: %v", err)
	}

	if receipt.Risk.Score < 0 || receipt.Risk.Score > 1 {
		t.Fatalf("risk score must be clamped to [0,1], got %v", receipt.Risk.Score)
	}
	// Exported() gained a signature change — public_api_change must fire.
	if receipt.Risk.Components.PublicAPIChange != 1 {
		t.Fatalf("expected public_api_change=1 for a signature change, got %+v", receipt.Risk.Components)
	}
	if receipt.Risk.Components.ComplexityIncrease <= 0 {
		t.Fatalf("expected a positive complexity_increase component, got %+v", receipt.Risk.Components)
	}
}
