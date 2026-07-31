package changeintel

import (
	"context"
	"reflect"
	"testing"
)

func findEntity(t *testing.T, entities []EntityChange, file, name string) EntityChange {
	t.Helper()
	for _, e := range entities {
		if e.File == file && e.Name == name {
			return e
		}
	}
	t.Fatalf("entity %s::%s not found among %d entities", file, name, len(entities))
	return EntityChange{}
}

func findFile(t *testing.T, files []FileChange, path string) FileChange {
	t.Helper()
	for _, f := range files {
		if f.Path == path {
			return f
		}
	}
	t.Fatalf("file %s not found among %d files", path, len(files))
	return FileChange{}
}

// TestAnalyze_AddRemoveRenameModify covers the first required test
// scenario: add, remove, rename, and modify all in one change set, with
// the rename resolved via git rename detection before entity matching.
func TestAnalyze_AddRemoveRenameModify(t *testing.T) {
	repo := newTestRepo(t)
	repo.write("go.mod", "module example.com/sample\n\ngo 1.21\n")
	repo.write("main.go", "package sample\n\nfunc A() {}\n")
	repo.write("old.go", "package sample\n\nfunc Old() {}\n")
	// gone.go and new.go's content must be dissimilar enough that git's
	// rename-similarity heuristic (-M, default 50%) does not mistake this
	// unrelated delete+add pair for a rename.
	repo.write("gone.go", "package sample\n\n// unrelated deleted file, distinct content on purpose.\nfunc Gone() int {\n\treturn 1 + 2 + 3\n}\n")
	repo.write("keep.go", "package sample\n\nfunc Keep() {}\n")
	base := repo.commit("base")

	// modify main.go
	repo.write("main.go", "package sample\n\nfunc A() {\n\tif true {\n\t\treturn\n\t}\n}\n")
	// rename old.go -> renamed.go
	repo.git("mv", "old.go", "renamed.go")
	// delete gone.go
	repo.remove("gone.go")
	// add new.go — git diff only considers tracked paths, so a genuinely
	// untracked new file needs staging to appear at all.
	repo.write("new.go", "package other\n\ntype Widget struct {\n\tName string\n}\n\nfunc New() *Widget {\n\treturn &Widget{}\n}\n")
	repo.git("add", "-A")

	svc := NewService(WithClock(fixedClock()))
	receipt, err := svc.Analyze(context.Background(), Request{
		Root: repo.dir,
		Base: SnapshotRef{Ref: base},
		Head: SnapshotRef{},
	})
	if err != nil {
		t.Fatalf("Analyze returned error: %v", err)
	}

	if !receipt.Impact.BaseAvailable {
		t.Fatal("expected BaseAvailable=true")
	}

	main := findFile(t, receipt.Files, "main.go")
	if main.Status != FileModified {
		t.Fatalf("main.go: expected modified, got %s", main.Status)
	}

	renamed := findFile(t, receipt.Files, "renamed.go")
	if renamed.Status != FileRenamed || renamed.OldPath != "old.go" {
		t.Fatalf("renamed.go: expected renamed from old.go, got status=%s oldPath=%s", renamed.Status, renamed.OldPath)
	}

	goneFile := findFile(t, receipt.Files, "gone.go")
	if goneFile.Status != FileRemoved {
		t.Fatalf("gone.go: expected removed, got %s", goneFile.Status)
	}

	newFile := findFile(t, receipt.Files, "new.go")
	if newFile.Status != FileAdded {
		t.Fatalf("new.go: expected added, got %s", newFile.Status)
	}

	for _, f := range receipt.Files {
		if f.Path == "keep.go" {
			t.Fatalf("keep.go should not appear in an unrelated change set")
		}
	}

	// Old() must be recognized as the *same* entity across the rename, not
	// reported as Old() removed + Old() added.
	oldEntity := findEntity(t, receipt.Entities, "renamed.go", "Old")
	if oldEntity.Status != EntityUnchanged {
		t.Fatalf("Old(): expected unchanged (rename-only, no body change), got %s (base_file=%s)", oldEntity.Status, oldEntity.BaseFile)
	}
	if oldEntity.BaseFile != "old.go" {
		t.Fatalf("Old(): expected base_file=old.go, got %q", oldEntity.BaseFile)
	}
	if oldEntity.MatchConfidence != 1.0 {
		t.Fatalf("Old(): expected match confidence 1.0, got %v", oldEntity.MatchConfidence)
	}

	newEntity := findEntity(t, receipt.Entities, "new.go", "New")
	if newEntity.Status != EntityAdded {
		t.Fatalf("New(): expected added, got %s", newEntity.Status)
	}

	goneEntity := findEntity(t, receipt.Entities, "gone.go", "Gone")
	if goneEntity.Status != EntityRemoved {
		t.Fatalf("Gone(): expected removed, got %s", goneEntity.Status)
	}

	aEntity := findEntity(t, receipt.Entities, "main.go", "A")
	if aEntity.Status != EntityModified {
		t.Fatalf("A(): expected modified, got %s", aEntity.Status)
	}
	if !aEntity.SpanChanged {
		t.Fatal("A(): expected span changed (body grew from one line to a block)")
	}
	if aEntity.Delta.Cyclomatic <= 0 {
		t.Fatalf("A(): expected a positive cyclomatic delta (branch added), got %d (base=%d head=%d)",
			aEntity.Delta.Cyclomatic, aEntity.Base.Cyclomatic, aEntity.Head.Cyclomatic)
	}
	if aEntity.Head.Cyclomatic-aEntity.Base.Cyclomatic != aEntity.Delta.Cyclomatic {
		t.Fatalf("A(): delta must equal head - base literally: head=%d base=%d delta=%d",
			aEntity.Head.Cyclomatic, aEntity.Base.Cyclomatic, aEntity.Delta.Cyclomatic)
	}
}

// TestAnalyze_ComplexityIncreaseAndDecrease verifies that "complexity
// delta" is a true head-base delta in both directions within the same
// change set, and that the repository-wide ComplexityDelta rollup is
// itself literally HeadTotal - BaseTotal.
func TestAnalyze_ComplexityIncreaseAndDecrease(t *testing.T) {
	repo := newTestRepo(t)
	repo.write("go.mod", "module example.com/sample\n\ngo 1.21\n")
	repo.write("funcs.go", `package sample

func Grows() {
	x := 1
	_ = x
}

func Shrinks() {
	if true {
		if true {
			return
		}
	}
}
`)
	base := repo.commit("base")

	repo.write("funcs.go", `package sample

func Grows() {
	if true {
		if true {
			return
		}
	}
}

func Shrinks() {
	x := 1
	_ = x
}
`)

	svc := NewService(WithClock(fixedClock()))
	receipt, err := svc.Analyze(context.Background(), Request{
		Root: repo.dir,
		Base: SnapshotRef{Ref: base},
	})
	if err != nil {
		t.Fatalf("Analyze returned error: %v", err)
	}

	grows := findEntity(t, receipt.Entities, "funcs.go", "Grows")
	if grows.Delta.Cyclomatic <= 0 {
		t.Fatalf("Grows(): expected positive cyclomatic delta, got %d", grows.Delta.Cyclomatic)
	}

	shrinks := findEntity(t, receipt.Entities, "funcs.go", "Shrinks")
	if shrinks.Delta.Cyclomatic >= 0 {
		t.Fatalf("Shrinks(): expected negative cyclomatic delta, got %d", shrinks.Delta.Cyclomatic)
	}

	if receipt.Complexity.DeltaCyclomatic != receipt.Complexity.HeadTotalCyclomatic-receipt.Complexity.BaseTotalCyclomatic {
		t.Fatalf("ComplexityDelta.DeltaCyclomatic must equal head total - base total: delta=%d head=%d base=%d",
			receipt.Complexity.DeltaCyclomatic, receipt.Complexity.HeadTotalCyclomatic, receipt.Complexity.BaseTotalCyclomatic)
	}
	if receipt.Complexity.IncreasedFunctions != 1 || receipt.Complexity.DecreasedFunctions != 1 {
		t.Fatalf("expected 1 increased and 1 decreased function, got increased=%d decreased=%d",
			receipt.Complexity.IncreasedFunctions, receipt.Complexity.DecreasedFunctions)
	}
}

// TestAnalyze_APISignatureChange verifies exported-symbol signature changes
// surface in APISurface.Changed, and that unexported signature changes do
// not (they are not part of the public API surface).
func TestAnalyze_APISignatureChange(t *testing.T) {
	repo := newTestRepo(t)
	repo.write("go.mod", "module example.com/sample\n\ngo 1.21\n")
	repo.write("api.go", "package sample\n\nfunc Exported() {}\n\nfunc unexported() {}\n")
	base := repo.commit("base")

	repo.write("api.go", "package sample\n\nfunc Exported(x int) string { return \"\" }\n\nfunc unexported(x int) string { return \"\" }\n")

	svc := NewService(WithClock(fixedClock()))
	receipt, err := svc.Analyze(context.Background(), Request{
		Root: repo.dir,
		Base: SnapshotRef{Ref: base},
	})
	if err != nil {
		t.Fatalf("Analyze returned error: %v", err)
	}

	found := false
	for _, c := range receipt.APISurface.Changed {
		if c.Name == "Exported" {
			found = true
			if c.Change != APISignatureChanged {
				t.Fatalf("expected signature_changed, got %s", c.Change)
			}
			if c.BaseSignature == c.HeadSignature {
				t.Fatal("expected differing base/head signatures")
			}
		}
		if c.Name == "unexported" {
			t.Fatal("unexported() must not appear in the public API surface")
		}
	}
	if !found {
		t.Fatal("expected Exported() in APISurface.Changed")
	}
}

// TestAnalyze_DeletedFiles verifies a fully deleted file's entities are
// reported as removed and the file itself as FileRemoved, with no head
// content required.
func TestAnalyze_DeletedFiles(t *testing.T) {
	repo := newTestRepo(t)
	repo.write("go.mod", "module example.com/sample\n\ngo 1.21\n")
	repo.write("doomed.go", "package sample\n\nfunc Doomed() {}\n\nfunc AlsoDoomed() {}\n")
	base := repo.commit("base")
	repo.remove("doomed.go")

	svc := NewService(WithClock(fixedClock()))
	receipt, err := svc.Analyze(context.Background(), Request{
		Root: repo.dir,
		Base: SnapshotRef{Ref: base},
	})
	if err != nil {
		t.Fatalf("Analyze returned error: %v", err)
	}

	fc := findFile(t, receipt.Files, "doomed.go")
	if fc.Status != FileRemoved {
		t.Fatalf("expected removed, got %s", fc.Status)
	}
	if fc.HeadIndexed {
		t.Fatal("a removed file must not be head-indexed")
	}

	names := map[string]bool{}
	for _, e := range receipt.Entities {
		if e.File == "doomed.go" {
			if e.Status != EntityRemoved {
				t.Fatalf("%s: expected removed, got %s", e.Name, e.Status)
			}
			names[e.Name] = true
		}
	}
	if !names["Doomed"] || !names["AlsoDoomed"] {
		t.Fatalf("expected both Doomed and AlsoDoomed reported removed, got %v", names)
	}
}

// TestAnalyze_BaseRefUnavailable verifies Analyze degrades gracefully
// (rather than erroring) when Base.Ref cannot be resolved, for example a
// shallow clone missing the base commit.
func TestAnalyze_BaseRefUnavailable(t *testing.T) {
	repo := newTestRepo(t)
	repo.write("go.mod", "module example.com/sample\n\ngo 1.21\n")
	repo.write("only.go", "package sample\n\nfunc Only() {}\n")
	repo.commit("base")

	svc := NewService(WithClock(fixedClock()))
	receipt, err := svc.Analyze(context.Background(), Request{
		Root:         repo.dir,
		Base:         SnapshotRef{Ref: "0000000000000000000000000000000000000000"},
		Head:         SnapshotRef{},
		ChangedPaths: []string{"only.go"},
	})
	if err != nil {
		t.Fatalf("Analyze must degrade gracefully, not error: %v", err)
	}
	if receipt.Impact.BaseAvailable {
		t.Fatal("expected BaseAvailable=false for an unresolvable base ref")
	}

	fc := findFile(t, receipt.Files, "only.go")
	if fc.Status != FileAdded {
		t.Fatalf("with no base available, expected the file treated as added, got %s", fc.Status)
	}
	only := findEntity(t, receipt.Entities, "only.go", "Only")
	if only.Status != EntityAdded {
		t.Fatalf("expected Only() reported added, got %s", only.Status)
	}
}

// TestAnalyze_ChangedOnlyUncertainty verifies the receipt always declares
// its index scope, and flags a changed file that Canopy has no structural
// parser for as uncertain rather than silently omitting it from the
// picture.
func TestAnalyze_ChangedOnlyUncertainty(t *testing.T) {
	repo := newTestRepo(t)
	repo.write("go.mod", "module example.com/sample\n\ngo 1.21\n")
	repo.write("main.go", "package sample\n\nfunc A() {}\n")
	repo.write("data.unsupportedext", "opaque binary-ish content\x00\x01")
	base := repo.commit("base")

	repo.write("main.go", "package sample\n\nfunc A() { _ = 1 }\n")
	repo.write("data.unsupportedext", "different opaque content\x00\x02")

	svc := NewService(WithClock(fixedClock()))
	receipt, err := svc.Analyze(context.Background(), Request{
		Root: repo.dir,
		Base: SnapshotRef{Ref: base},
	})
	if err != nil {
		t.Fatalf("Analyze returned error: %v", err)
	}

	if receipt.Impact.IndexScope != "changed_only" {
		t.Fatalf("expected index_scope=changed_only, got %q", receipt.Impact.IndexScope)
	}

	data := findFile(t, receipt.Files, "data.unsupportedext")
	if !data.Uncertain {
		t.Fatal("expected data.unsupportedext (no registered structural parser) to be flagged uncertain")
	}
	if data.BaseIndexed || data.HeadIndexed {
		t.Fatal("an unsupported-extension file should not be structurally indexed on either side")
	}

	found := false
	for _, p := range receipt.Impact.ParseUncertainty {
		if p == "data.unsupportedext" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected data.unsupportedext in Impact.ParseUncertainty, got %v", receipt.Impact.ParseUncertainty)
	}

	// main.go, in contrast, has a real parser and must not be flagged
	// uncertain just because it happens to sit in the same change set.
	main := findFile(t, receipt.Files, "main.go")
	if main.Uncertain {
		t.Fatal("main.go has a real Go parser and must not be marked uncertain")
	}
}

// TestAnalyze_Deterministic is the acceptance test: Analyze called twice
// against identical base/head content and Request options must produce
// receipts with identical Digest, ID, and content fields.
func TestAnalyze_Deterministic(t *testing.T) {
	repo := newTestRepo(t)
	repo.write("go.mod", "module example.com/sample\n\ngo 1.21\n")
	repo.write("main.go", "package sample\n\nfunc A() {}\n")
	base := repo.commit("base")
	repo.write("main.go", "package sample\n\nfunc A() {\n\tif true {\n\t\treturn\n\t}\n}\n\nfunc B() {}\n")

	req := Request{
		Root:         repo.dir,
		Base:         SnapshotRef{Ref: base},
		Head:         SnapshotRef{},
		IncludeTests: true,
		IncludeRisk:  true,
	}

	svc := NewService(WithClock(fixedClock()))
	first, err := svc.Analyze(context.Background(), req)
	if err != nil {
		t.Fatalf("first Analyze returned error: %v", err)
	}
	second, err := svc.Analyze(context.Background(), req)
	if err != nil {
		t.Fatalf("second Analyze returned error: %v", err)
	}

	if first.Digest == "" {
		t.Fatal("expected a non-empty digest")
	}
	if first.Digest != second.Digest {
		t.Fatalf("digest not deterministic: %s != %s", first.Digest, second.Digest)
	}
	if first.ID != second.ID {
		t.Fatalf("id not deterministic: %s != %s", first.ID, second.ID)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("receipts differ across identical Analyze calls:\nfirst:  %+v\nsecond: %+v", first, second)
	}

	// A change to the snapshot content must change the digest.
	repo.write("main.go", "package sample\n\nfunc A() {\n\tif true {\n\t\treturn\n\t}\n}\n\nfunc B() {}\n\nfunc C() {}\n")
	third, err := svc.Analyze(context.Background(), req)
	if err != nil {
		t.Fatalf("third Analyze returned error: %v", err)
	}
	if third.Digest == first.Digest {
		t.Fatal("expected digest to change when snapshot content changes")
	}
}

// TestAnalyze_ExplicitChangedPaths verifies the ChangedPaths override skips
// git diff discovery entirely and classifies status by content presence.
func TestAnalyze_ExplicitChangedPaths(t *testing.T) {
	repo := newTestRepo(t)
	repo.write("go.mod", "module example.com/sample\n\ngo 1.21\n")
	repo.write("main.go", "package sample\n\nfunc A() {}\n")
	repo.write("untouched.go", "package sample\n\nfunc U() {}\n")
	base := repo.commit("base")
	repo.write("main.go", "package sample\n\nfunc A() {}\nfunc B() {}\n")
	// untouched.go is legitimately unchanged but we still ask for it.

	svc := NewService(WithClock(fixedClock()))
	receipt, err := svc.Analyze(context.Background(), Request{
		Root:         repo.dir,
		Base:         SnapshotRef{Ref: base},
		ChangedPaths: []string{"main.go"},
	})
	if err != nil {
		t.Fatalf("Analyze returned error: %v", err)
	}

	if len(receipt.Files) != 1 {
		t.Fatalf("expected exactly the requested file, got %v", receipt.Files)
	}
	if receipt.Files[0].Path != "main.go" {
		t.Fatalf("expected main.go, got %s", receipt.Files[0].Path)
	}
}
