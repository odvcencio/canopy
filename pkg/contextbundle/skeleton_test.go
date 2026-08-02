package contextbundle

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestBuild_NoDoubleParseForIndexedSkeleton verifies decision #4: indexed
// code is never reparsed merely to produce a skeleton/signature/map
// projection. It builds an index, then deletes the source file from disk
// before requesting a signature-mode bundle for it. Signature/skeleton/map
// content comes entirely from model.Symbol fields already captured at index
// time (skeleton.go never calls os.ReadFile for those modes), so the build
// must still succeed even though the file no longer exists to reparse.
func TestBuild_NoDoubleParseForIndexedSkeleton(t *testing.T) {
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

	// Remove the source file so any attempt to reparse or re-read it fails.
	if err := os.Remove(filepath.Join(dir, "main.go")); err != nil {
		t.Fatalf("remove fixture: %v", err)
	}

	snap := Snapshot{Kind: "imported", ID: "snap_noparse", Root: dir}
	svc := newTestService(idx, snap, PlainRenderer{})

	// Explore mode over changed entities starts every candidate's downgrade
	// chain at signature/map (spec 10.10: "signatures/types for
	// high-density and task-matching files... no full bodies unless
	// explicitly focused"), which must render from already-indexed
	// Symbol.Signature fields alone, with no explicit focus selector to
	// force a full-body read.
	req := Request{
		Root: dir,
		Intent: TaskIntent{
			Kind:         TaskExplore,
			ChangedPaths: []string{"main.go"},
		},
		Budget: Budget{TotalTokens: 500},
	}

	result, err := svc.Build(context.Background(), req)
	if err != nil {
		t.Fatalf("Build failed even though signature mode should not touch the deleted source file: %v", err)
	}
	if len(result.Receipt.Items) == 0 {
		t.Fatal("expected at least one receipt item")
	}
	found := false
	for _, item := range result.Receipt.Items {
		if item.Mode == ProjectionSignature || item.Mode == ProjectionSkeleton {
			found = true
		}
		if item.Mode == ProjectionFull || item.Mode == ProjectionBody {
			t.Fatalf("did not expect a source-reading mode after the file was deleted, got %s for %s", item.Mode, item.Path)
		}
	}
	if !found {
		t.Fatalf("expected a signature or skeleton mode item, got modes: %+v", result.Receipt.Items)
	}
}

// TestProjectSymbol_SkeletonModesDoNotReadSource verifies at the unit level
// that skeleton/signature/map projections never call os.ReadFile: they are
// built entirely from the candidate's already-indexed Signature field.
func TestProjectSymbol_SkeletonModesDoNotReadSource(t *testing.T) {
	c := &candidateItem{
		Path:      "does/not/exist.go",
		Kind:      "function_definition",
		Name:      "Example",
		Signature: "func Example() error",
		StartLine: 10,
		EndLine:   12,
	}
	for _, mode := range []ProjectionMode{ProjectionSkeleton, ProjectionSignature, ProjectionMap, ProjectionMetadata} {
		content, _, err := projectSymbol("/nonexistent/root", c, mode, false)
		if err != nil {
			t.Fatalf("mode %s unexpectedly touched disk and failed: %v", mode, err)
		}
		if len(content) == 0 {
			t.Fatalf("mode %s produced empty content", mode)
		}
	}
}
