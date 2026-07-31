package contextbundle

import (
	"context"
	"testing"
)

func TestBuild_ReceiptDeterminism(t *testing.T) {
	dir := t.TempDir()
	writeFixtureFiles(t, dir, map[string]string{
		"main.go": `package sample

func Greet(name string) string {
	return formatGreeting(name)
}

func formatGreeting(name string) string {
	return "Hello, " + name + "!"
}
`,
	})
	idx := buildFixtureIndex(t, dir)
	snap := Snapshot{Kind: "imported", ID: "snap_determinism", Root: dir}

	req := Request{
		Root: dir,
		Intent: TaskIntent{
			Kind:  TaskImplement,
			Focus: []Selector{{File: "main.go", Symbol: "Greet", Required: true}},
		},
		Budget: Budget{TotalTokens: 2000},
	}

	build := func() Result {
		svc := newTestService(idx, snap, PlainRenderer{})
		result, err := svc.Build(context.Background(), req)
		if err != nil {
			t.Fatalf("Build: %v", err)
		}
		return *result
	}

	first := build()
	second := build()

	if first.Receipt.ID == "" {
		t.Fatal("expected non-empty receipt ID")
	}
	if first.Receipt.ID != second.Receipt.ID {
		t.Fatalf("receipt IDs differ across identical builds: %s vs %s", first.Receipt.ID, second.Receipt.ID)
	}
	if first.Receipt.BundleSHA256 != second.Receipt.BundleSHA256 {
		t.Fatalf("bundle hashes differ across identical builds: %s vs %s", first.Receipt.BundleSHA256, second.Receipt.BundleSHA256)
	}
	if string(first.Content) != string(second.Content) {
		t.Fatal("rendered content differs across identical builds")
	}

	// Changing the request (a different budget) must change the receipt ID.
	req.Budget.TotalTokens = 3000
	third := build()
	if third.Receipt.ID == first.Receipt.ID {
		t.Fatal("expected receipt ID to change when the request changes")
	}
}

func TestReceiptID_Format(t *testing.T) {
	r := Receipt{SchemaVersion: SchemaVersion, PolicyVersion: PolicyVersion}
	id := receiptID(r)
	if len(id) != len(receiptIDPrefix)+receiptIDLength {
		t.Fatalf("expected receipt ID of length %d, got %d (%s)", len(receiptIDPrefix)+receiptIDLength, len(id), id)
	}
	if id[:len(receiptIDPrefix)] != receiptIDPrefix {
		t.Fatalf("expected receipt ID to start with %q, got %q", receiptIDPrefix, id)
	}
}
