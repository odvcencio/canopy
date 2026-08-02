package contextbundle

import (
	"context"
	"errors"
	"testing"
)

func TestBuild_RequiredEvidenceExceedsBudget(t *testing.T) {
	dir := t.TempDir()
	writeFixtureFiles(t, dir, map[string]string{
		"main.go": "package sample\n\nfunc Greet(name string) string {\n\treturn \"hi \" + name\n}\n",
	})
	idx := buildFixtureIndex(t, dir)
	snap := Snapshot{Kind: "imported", ID: "snap_overflow", Root: dir}
	svc := newTestService(idx, snap, PlainRenderer{})

	req := Request{
		Root: dir,
		Intent: TaskIntent{
			Kind: TaskImplement,
			Focus: []Selector{
				{File: "main.go", Symbol: "Greet", Required: true},
			},
		},
		Budget: Budget{TotalTokens: 1},
	}

	_, err := svc.Build(context.Background(), req)
	if err == nil {
		t.Fatal("expected an error when required evidence exceeds the budget")
	}
	var target *ErrRequiredEvidenceExceedsBudget
	if !errors.As(err, &target) {
		t.Fatalf("expected *ErrRequiredEvidenceExceedsBudget, got %T: %v", err, err)
	}
	if target.RequestTokens != 1 {
		t.Fatalf("expected RequestTokens=1, got %d", target.RequestTokens)
	}
	if target.MinimumTokens <= target.RequestTokens {
		t.Fatalf("expected MinimumTokens > RequestTokens, got %d vs %d", target.MinimumTokens, target.RequestTokens)
	}
}

func TestBuild_BudgetTolerance(t *testing.T) {
	dir := t.TempDir()
	writeFixtureFiles(t, dir, map[string]string{
		"main.go": `package sample

func Greet(name string) string {
	return formatGreeting(name) + suffix(name) + prefix(name)
}

func formatGreeting(name string) string { return "Hello, " + name }
func suffix(name string) string         { return name + "!" }
func prefix(name string) string         { return "> " + name }
`,
	})
	idx := buildFixtureIndex(t, dir)
	snap := Snapshot{Kind: "imported", ID: "snap_tolerance", Root: dir}
	svc := newTestService(idx, snap, PlainRenderer{})

	req := Request{
		Root: dir,
		Intent: TaskIntent{
			Kind: TaskImplement,
			Focus: []Selector{
				{File: "main.go", Symbol: "Greet", Required: true},
			},
		},
		Budget: Budget{TotalTokens: 40},
	}

	result, err := svc.Build(context.Background(), req)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	tolerance := budgetTolerance(req.Budget)
	limit := float64(req.Budget.TotalTokens) * (1 + tolerance)
	if float64(result.Receipt.EstimatedTokens) > limit {
		t.Fatalf("estimated tokens %d exceed budget*%.2f = %.1f", result.Receipt.EstimatedTokens, 1+tolerance, limit)
	}
}

func TestBuild_UnchangedItemReuse(t *testing.T) {
	dir := t.TempDir()
	writeFixtureFiles(t, dir, map[string]string{
		"main.go": "package sample\n\nfunc Greet(name string) string {\n\treturn \"hi \" + name\n}\n",
	})
	idx := buildFixtureIndex(t, dir)
	snap := Snapshot{Kind: "imported", ID: "snap_reuse", Root: dir}
	svc := newTestService(idx, snap, PlainRenderer{})

	req := Request{
		Root: dir,
		Intent: TaskIntent{
			Kind:  TaskImplement,
			Focus: []Selector{{File: "main.go", Symbol: "Greet", Required: true}},
		},
		Budget: Budget{TotalTokens: 2000},
	}

	first, err := svc.Build(context.Background(), req)
	if err != nil {
		t.Fatalf("first Build: %v", err)
	}

	// Same snapshot, no workspace change: the receipt is reusable outright.
	if !ReceiptReusable(first.Receipt, snap) {
		t.Fatal("expected receipt to be reusable against an unchanged snapshot")
	}
	reply := UnchangedReply(first.Receipt)
	if !reply.Unchanged || reply.ReceiptID != first.Receipt.ID {
		t.Fatalf("unexpected unchanged reply: %+v", reply)
	}
	if len(reply.Items) != len(first.Receipt.Items) {
		t.Fatalf("expected %d item ids in reuse reply, got %d", len(first.Receipt.Items), len(reply.Items))
	}

	// A changed snapshot is not reusable outright...
	if ReceiptReusable(first.Receipt, Snapshot{ID: "snap_changed"}) {
		t.Fatal("expected receipt to be non-reusable after snapshot changes")
	}
	// ...but an individual item whose content hash still matches is.
	for _, item := range first.Receipt.Items {
		if !ReceiptItemReusable(item, item.ContentSHA256) {
			t.Fatalf("expected item %s to be reusable by matching content hash", item.ItemID)
		}
		if ReceiptItemReusable(item, "different-hash") {
			t.Fatalf("expected item %s to be non-reusable against a different content hash", item.ItemID)
		}
	}
}
