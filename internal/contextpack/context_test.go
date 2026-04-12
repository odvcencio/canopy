package contextpack

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/odvcencio/canopy/pkg/model"
)

func TestBuild_FocusAndSnippet(t *testing.T) {
	tmpDir := t.TempDir()
	sourcePath := filepath.Join(tmpDir, "sample.go")
	source := `package sample

import "fmt"

type Service struct{}

func (s *Service) Work() {
	fmt.Println("ok")
}
`

	if err := os.WriteFile(sourcePath, []byte(source), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	idx := &model.Index{
		Root: tmpDir,
		Files: []model.FileSummary{
			{
				Path:    "sample.go",
				Imports: []string{"fmt"},
				Symbols: []model.Symbol{
					{
						File:      "sample.go",
						Kind:      "type_definition",
						Name:      "Service",
						Signature: "type Service struct{}",
						StartLine: 5,
						EndLine:   5,
					},
					{
						File:      "sample.go",
						Kind:      "method_definition",
						Name:      "Work",
						Signature: "func (s *Service) Work()",
						StartLine: 7,
						EndLine:   9,
					},
				},
			},
		},
	}

	report, err := Build(idx, Options{
		FilePath:    sourcePath,
		Line:        8,
		TokenBudget: 400,
	})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	if report.File != "sample.go" {
		t.Fatalf("expected report file sample.go, got %q", report.File)
	}
	if report.Focus == nil || report.Focus.Name != "Work" {
		t.Fatalf("expected focus symbol Work, got %#v", report.Focus)
	}
	if report.SnippetStart != 7 || report.SnippetEnd != 9 {
		t.Fatalf("unexpected snippet bounds %d:%d", report.SnippetStart, report.SnippetEnd)
	}
	if len(report.Imports) != 1 || report.Imports[0] != "fmt" {
		t.Fatalf("unexpected imports %v", report.Imports)
	}
}

func TestBuild_RequiresFilePath(t *testing.T) {
	_, err := Build(&model.Index{}, Options{})
	if err == nil {
		t.Fatal("expected Build to fail when file path is missing")
	}
}

func TestBuild_SemanticRelatedUsesCallGraph(t *testing.T) {
	tmpDir := t.TempDir()
	sourcePath := filepath.Join(tmpDir, "sample.go")
	source := `package sample

func helper() {}

func work() {
	helper()
}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	idx := &model.Index{
		Root: tmpDir,
		Files: []model.FileSummary{
			{
				Path:    "sample.go",
				Imports: nil,
				Symbols: []model.Symbol{
					{
						File:      "sample.go",
						Kind:      "function_definition",
						Name:      "helper",
						Signature: "func helper()",
						StartLine: 3,
						EndLine:   3,
					},
					{
						File:      "sample.go",
						Kind:      "function_definition",
						Name:      "work",
						Signature: "func work()",
						StartLine: 5,
						EndLine:   7,
					},
				},
				References: []model.Reference{
					{
						File:        "sample.go",
						Kind:        "reference.call",
						Name:        "helper",
						StartLine:   6,
						EndLine:     6,
						StartColumn: 2,
						EndColumn:   8,
					},
				},
			},
		},
	}

	report, err := Build(idx, Options{
		FilePath:    sourcePath,
		Line:        6,
		TokenBudget: 400,
		Semantic:    true,
	})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	if !report.Semantic {
		t.Fatalf("expected semantic mode to be true")
	}
	if report.Focus == nil || report.Focus.Name != "work" {
		t.Fatalf("expected focus symbol work, got %#v", report.Focus)
	}
	if len(report.Related) == 0 {
		t.Fatalf("expected semantic related symbols, got none")
	}
	if report.Related[0].Name != "helper" {
		t.Fatalf("expected related symbol helper, got %+v", report.Related[0])
	}
}

func TestBuild_SemanticDepthIncludesTransitiveCalls(t *testing.T) {
	tmpDir := t.TempDir()
	sourcePath := filepath.Join(tmpDir, "sample.go")
	source := `package sample

func leaf() {}

func mid() {
	leaf()
}

func work() {
	mid()
}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	idx := &model.Index{
		Root: tmpDir,
		Files: []model.FileSummary{
			{
				Path: "sample.go",
				Symbols: []model.Symbol{
					{
						File:      "sample.go",
						Kind:      "function_definition",
						Name:      "leaf",
						Signature: "func leaf()",
						StartLine: 3,
						EndLine:   3,
					},
					{
						File:      "sample.go",
						Kind:      "function_definition",
						Name:      "mid",
						Signature: "func mid()",
						StartLine: 5,
						EndLine:   7,
					},
					{
						File:      "sample.go",
						Kind:      "function_definition",
						Name:      "work",
						Signature: "func work()",
						StartLine: 9,
						EndLine:   11,
					},
				},
				References: []model.Reference{
					{
						File:        "sample.go",
						Kind:        "reference.call",
						Name:        "leaf",
						StartLine:   6,
						EndLine:     6,
						StartColumn: 2,
						EndColumn:   6,
					},
					{
						File:        "sample.go",
						Kind:        "reference.call",
						Name:        "mid",
						StartLine:   10,
						EndLine:     10,
						StartColumn: 2,
						EndColumn:   5,
					},
				},
			},
		},
	}

	depthOne, err := Build(idx, Options{
		FilePath:      sourcePath,
		Line:          10,
		TokenBudget:   400,
		Semantic:      true,
		SemanticDepth: 1,
	})
	if err != nil {
		t.Fatalf("Build depth=1 returned error: %v", err)
	}
	if len(depthOne.Related) != 1 || depthOne.Related[0].Name != "mid" {
		t.Fatalf("expected depth=1 related=[mid], got %+v", depthOne.Related)
	}

	depthTwo, err := Build(idx, Options{
		FilePath:      sourcePath,
		Line:          10,
		TokenBudget:   400,
		Semantic:      true,
		SemanticDepth: 2,
	})
	if err != nil {
		t.Fatalf("Build depth=2 returned error: %v", err)
	}
	if len(depthTwo.Related) < 2 {
		t.Fatalf("expected depth=2 to include transitive callee, got %+v", depthTwo.Related)
	}
	relatedNames := map[string]bool{}
	for _, symbol := range depthTwo.Related {
		relatedNames[symbol.Name] = true
	}
	if !relatedNames["mid"] || !relatedNames["leaf"] {
		t.Fatalf("expected depth=2 related to include mid and leaf, got %+v", depthTwo.Related)
	}
}
