package changeintel

import (
	"context"
	"testing"
)

// TestAnalyze_BoundaryIntroduced verifies a new import that violates the
// current .canopyboundaries policy shows up as Boundaries.Introduced.
func TestAnalyze_BoundaryIntroduced(t *testing.T) {
	repo := newTestRepo(t)
	repo.write("go.mod", "module example.com/sample\n\ngo 1.21\n")
	repo.write(".canopyboundaries", "module pkga deny pkgb\n")
	repo.write("pkga/a.go", "package pkga\n\nfunc A() {}\n")
	repo.write("pkgb/b.go", "package pkgb\n\nfunc B() {}\n")
	base := repo.commit("base")

	repo.write("pkga/a.go", "package pkga\n\nimport \"example.com/sample/pkgb\"\n\nfunc A() { pkgb.B() }\n")

	svc := NewService(WithClock(fixedClock()))
	receipt, err := svc.Analyze(context.Background(), Request{
		Root: repo.dir,
		Base: SnapshotRef{Ref: base},
	})
	if err != nil {
		t.Fatalf("Analyze returned error: %v", err)
	}

	if len(receipt.Boundaries.Introduced) == 0 {
		t.Fatalf("expected an introduced boundary violation, got none (resolved=%v persisting=%v)",
			receipt.Boundaries.Resolved, receipt.Boundaries.Persisting)
	}
	v := receipt.Boundaries.Introduced[0]
	if v.From != "pkga" || v.To != "pkgb" {
		t.Fatalf("expected pkga -> pkgb violation, got %+v", v)
	}
	if len(receipt.Boundaries.Resolved) != 0 {
		t.Fatalf("expected no resolved violations, got %v", receipt.Boundaries.Resolved)
	}
}

// TestAnalyze_BoundaryResolved verifies a violation present in base and
// removed in head shows up as Boundaries.Resolved, not Introduced.
func TestAnalyze_BoundaryResolved(t *testing.T) {
	repo := newTestRepo(t)
	repo.write("go.mod", "module example.com/sample\n\ngo 1.21\n")
	repo.write(".canopyboundaries", "module pkga deny pkgb\n")
	repo.write("pkga/a.go", "package pkga\n\nimport \"example.com/sample/pkgb\"\n\nfunc A() { pkgb.B() }\n")
	repo.write("pkgb/b.go", "package pkgb\n\nfunc B() {}\n")
	base := repo.commit("base")

	repo.write("pkga/a.go", "package pkga\n\nfunc A() {}\n")

	svc := NewService(WithClock(fixedClock()))
	receipt, err := svc.Analyze(context.Background(), Request{
		Root: repo.dir,
		Base: SnapshotRef{Ref: base},
	})
	if err != nil {
		t.Fatalf("Analyze returned error: %v", err)
	}

	if len(receipt.Boundaries.Resolved) == 0 {
		t.Fatalf("expected a resolved boundary violation, got none (introduced=%v)", receipt.Boundaries.Introduced)
	}
	v := receipt.Boundaries.Resolved[0]
	if v.From != "pkga" || v.To != "pkgb" {
		t.Fatalf("expected pkga -> pkgb violation, got %+v", v)
	}
	if len(receipt.Boundaries.Introduced) != 0 {
		t.Fatalf("expected no introduced violations, got %v", receipt.Boundaries.Introduced)
	}
}

// TestAnalyze_CapabilityIntroduced verifies a newly added call to a
// capability-rule API surfaces in Capabilities.Introduced.
func TestAnalyze_CapabilityIntroduced(t *testing.T) {
	repo := newTestRepo(t)
	repo.write("go.mod", "module example.com/sample\n\ngo 1.21\n")
	repo.write("net.go", "package sample\n\nfunc connect() {}\n\nfunc run() {\n}\n")
	base := repo.commit("base")

	repo.write("net.go", "package sample\n\nfunc connect() {}\n\nfunc run() {\n\tconnect()\n}\n")

	svc := NewService(WithClock(fixedClock()))
	receipt, err := svc.Analyze(context.Background(), Request{
		Root: repo.dir,
		Base: SnapshotRef{Ref: base},
	})
	if err != nil {
		t.Fatalf("Analyze returned error: %v", err)
	}

	if len(receipt.Capabilities.Introduced) == 0 {
		t.Fatalf("expected an introduced capability, got none (resolved=%v persisting=%v)",
			receipt.Capabilities.Resolved, receipt.Capabilities.Persisting)
	}
	found := false
	for _, c := range receipt.Capabilities.Introduced {
		if c.Name == "Network Communication" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected Network Communication capability introduced, got %+v", receipt.Capabilities.Introduced)
	}
}

// TestAnalyze_CapabilityResolved verifies a capability call present in base
// and removed in head surfaces in Capabilities.Resolved.
func TestAnalyze_CapabilityResolved(t *testing.T) {
	repo := newTestRepo(t)
	repo.write("go.mod", "module example.com/sample\n\ngo 1.21\n")
	repo.write("net.go", "package sample\n\nfunc connect() {}\n\nfunc run() {\n\tconnect()\n}\n")
	base := repo.commit("base")

	repo.write("net.go", "package sample\n\nfunc connect() {}\n\nfunc run() {\n}\n")

	svc := NewService(WithClock(fixedClock()))
	receipt, err := svc.Analyze(context.Background(), Request{
		Root: repo.dir,
		Base: SnapshotRef{Ref: base},
	})
	if err != nil {
		t.Fatalf("Analyze returned error: %v", err)
	}

	if len(receipt.Capabilities.Resolved) == 0 {
		t.Fatalf("expected a resolved capability, got none (introduced=%v)", receipt.Capabilities.Introduced)
	}
	found := false
	for _, c := range receipt.Capabilities.Resolved {
		if c.Name == "Network Communication" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected Network Communication capability resolved, got %+v", receipt.Capabilities.Resolved)
	}
}
