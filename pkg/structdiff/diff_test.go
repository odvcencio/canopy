package structdiff

import (
	"testing"

	"github.com/odvcencio/canopy/pkg/model"
)

func TestCompare_SymbolAndImportChanges(t *testing.T) {
	before := &model.Index{
		Root: "/tmp/before",
		Files: []model.FileSummary{
			{
				Path:    "a.go",
				Imports: []string{"fmt", "os"},
				Symbols: []model.Symbol{
					{
						File:      "a.go",
						Kind:      "function_definition",
						Name:      "OldOnly",
						Signature: "func OldOnly()",
						StartLine: 3,
						EndLine:   3,
					},
					{
						File:      "a.go",
						Kind:      "function_definition",
						Name:      "Shared",
						Signature: "func Shared(a int)",
						StartLine: 10,
						EndLine:   12,
					},
				},
			},
		},
	}

	after := &model.Index{
		Root: "/tmp/after",
		Files: []model.FileSummary{
			{
				Path:    "a.go",
				Imports: []string{"fmt", "strings"},
				Symbols: []model.Symbol{
					{
						File:      "a.go",
						Kind:      "function_definition",
						Name:      "NewOnly",
						Signature: "func NewOnly()",
						StartLine: 5,
						EndLine:   5,
					},
					{
						File:      "a.go",
						Kind:      "function_definition",
						Name:      "Shared",
						Signature: "func Shared(a int, b string)",
						StartLine: 10,
						EndLine:   13,
					},
				},
			},
		},
	}

	report := Compare(before, after)
	if report.Stats.AddedSymbols != 1 {
		t.Fatalf("expected 1 added symbol, got %d", report.Stats.AddedSymbols)
	}
	if report.Stats.RemovedSymbols != 1 {
		t.Fatalf("expected 1 removed symbol, got %d", report.Stats.RemovedSymbols)
	}
	if report.Stats.ModifiedSymbols != 1 {
		t.Fatalf("expected 1 modified symbol, got %d", report.Stats.ModifiedSymbols)
	}
	if report.Stats.ChangedFiles != 1 {
		t.Fatalf("expected 1 changed file, got %d", report.Stats.ChangedFiles)
	}

	if len(report.ImportChanges) != 1 {
		t.Fatalf("expected 1 import change entry, got %d", len(report.ImportChanges))
	}
	imp := report.ImportChanges[0]
	if imp.File != "a.go" {
		t.Fatalf("expected import change for a.go, got %s", imp.File)
	}
	if len(imp.Added) != 1 || imp.Added[0] != "strings" {
		t.Fatalf("unexpected added imports: %v", imp.Added)
	}
	if len(imp.Removed) != 1 || imp.Removed[0] != "os" {
		t.Fatalf("unexpected removed imports: %v", imp.Removed)
	}

	mod := report.ModifiedSymbols[0]
	if len(mod.Fields) != 2 {
		t.Fatalf("expected 2 modified fields, got %v", mod.Fields)
	}
}

func TestCompareIdentical(t *testing.T) {
	idx := &model.Index{
		Root: "/project",
		Files: []model.FileSummary{
			{
				Path:    "main.go",
				Imports: []string{"fmt", "os"},
				Symbols: []model.Symbol{
					{
						File:      "main.go",
						Kind:      "function_definition",
						Name:      "Run",
						Signature: "func Run()",
						StartLine: 5,
						EndLine:   10,
					},
					{
						File:      "main.go",
						Kind:      "type_declaration",
						Name:      "Config",
						Signature: "type Config struct",
						StartLine: 12,
						EndLine:   20,
					},
				},
			},
			{
				Path:    "util.go",
				Imports: []string{"strings"},
				Symbols: []model.Symbol{
					{
						File:      "util.go",
						Kind:      "function_definition",
						Name:      "Trim",
						Signature: "func Trim(s string) string",
						StartLine: 3,
						EndLine:   5,
					},
				},
			},
		},
	}

	report := Compare(idx, idx)

	if report.Stats.AddedSymbols != 0 {
		t.Fatalf("expected 0 added symbols, got %d", report.Stats.AddedSymbols)
	}
	if report.Stats.RemovedSymbols != 0 {
		t.Fatalf("expected 0 removed symbols, got %d", report.Stats.RemovedSymbols)
	}
	if report.Stats.ModifiedSymbols != 0 {
		t.Fatalf("expected 0 modified symbols, got %d", report.Stats.ModifiedSymbols)
	}
	if report.Stats.ChangedFiles != 0 {
		t.Fatalf("expected 0 changed files, got %d", report.Stats.ChangedFiles)
	}
	if len(report.ImportChanges) != 0 {
		t.Fatalf("expected 0 import changes, got %d", len(report.ImportChanges))
	}
	if report.BeforeRoot != "/project" {
		t.Fatalf("expected BeforeRoot /project, got %s", report.BeforeRoot)
	}
	if report.AfterRoot != "/project" {
		t.Fatalf("expected AfterRoot /project, got %s", report.AfterRoot)
	}
}

func TestCompareAddedSymbol(t *testing.T) {
	before := &model.Index{
		Root: "/before",
		Files: []model.FileSummary{
			{
				Path: "handler.go",
				Symbols: []model.Symbol{
					{
						File:      "handler.go",
						Kind:      "function_definition",
						Name:      "Handle",
						Signature: "func Handle(r Request)",
						StartLine: 5,
						EndLine:   15,
					},
				},
			},
		},
	}

	after := &model.Index{
		Root: "/after",
		Files: []model.FileSummary{
			{
				Path: "handler.go",
				Symbols: []model.Symbol{
					{
						File:      "handler.go",
						Kind:      "function_definition",
						Name:      "Handle",
						Signature: "func Handle(r Request)",
						StartLine: 5,
						EndLine:   15,
					},
					{
						File:      "handler.go",
						Kind:      "function_definition",
						Name:      "Validate",
						Signature: "func Validate(r Request) error",
						StartLine: 17,
						EndLine:   25,
					},
				},
			},
		},
	}

	report := Compare(before, after)

	if report.Stats.AddedSymbols != 1 {
		t.Fatalf("expected 1 added symbol, got %d", report.Stats.AddedSymbols)
	}
	if report.Stats.RemovedSymbols != 0 {
		t.Fatalf("expected 0 removed symbols, got %d", report.Stats.RemovedSymbols)
	}
	if report.Stats.ModifiedSymbols != 0 {
		t.Fatalf("expected 0 modified symbols, got %d", report.Stats.ModifiedSymbols)
	}

	added := report.AddedSymbols[0]
	if added.Name != "Validate" {
		t.Fatalf("expected added symbol Validate, got %s", added.Name)
	}
	if added.File != "handler.go" {
		t.Fatalf("expected added symbol in handler.go, got %s", added.File)
	}
	if added.Kind != "function_definition" {
		t.Fatalf("expected kind function_definition, got %s", added.Kind)
	}
	if added.Signature != "func Validate(r Request) error" {
		t.Fatalf("unexpected signature: %s", added.Signature)
	}
	if added.StartLine != 17 || added.EndLine != 25 {
		t.Fatalf("unexpected line range: %d-%d", added.StartLine, added.EndLine)
	}
}

func TestCompareRemovedSymbol(t *testing.T) {
	before := &model.Index{
		Root: "/before",
		Files: []model.FileSummary{
			{
				Path: "svc.go",
				Symbols: []model.Symbol{
					{
						File:      "svc.go",
						Kind:      "function_definition",
						Name:      "Start",
						Signature: "func Start()",
						StartLine: 1,
						EndLine:   5,
					},
					{
						File:      "svc.go",
						Kind:      "function_definition",
						Name:      "Deprecated",
						Signature: "func Deprecated()",
						StartLine: 7,
						EndLine:   10,
					},
				},
			},
		},
	}

	after := &model.Index{
		Root: "/after",
		Files: []model.FileSummary{
			{
				Path: "svc.go",
				Symbols: []model.Symbol{
					{
						File:      "svc.go",
						Kind:      "function_definition",
						Name:      "Start",
						Signature: "func Start()",
						StartLine: 1,
						EndLine:   5,
					},
				},
			},
		},
	}

	report := Compare(before, after)

	if report.Stats.AddedSymbols != 0 {
		t.Fatalf("expected 0 added symbols, got %d", report.Stats.AddedSymbols)
	}
	if report.Stats.RemovedSymbols != 1 {
		t.Fatalf("expected 1 removed symbol, got %d", report.Stats.RemovedSymbols)
	}
	if report.Stats.ModifiedSymbols != 0 {
		t.Fatalf("expected 0 modified symbols, got %d", report.Stats.ModifiedSymbols)
	}

	removed := report.RemovedSymbols[0]
	if removed.Name != "Deprecated" {
		t.Fatalf("expected removed symbol Deprecated, got %s", removed.Name)
	}
	if removed.File != "svc.go" {
		t.Fatalf("expected removed symbol in svc.go, got %s", removed.File)
	}
	if removed.Signature != "func Deprecated()" {
		t.Fatalf("unexpected signature: %s", removed.Signature)
	}
}

func TestCompareModifiedSymbol(t *testing.T) {
	before := &model.Index{
		Root: "/before",
		Files: []model.FileSummary{
			{
				Path: "calc.go",
				Symbols: []model.Symbol{
					{
						File:      "calc.go",
						Kind:      "function_definition",
						Name:      "Add",
						Signature: "func Add(a, b int) int",
						StartLine: 3,
						EndLine:   5,
					},
				},
			},
		},
	}

	t.Run("signature_change", func(t *testing.T) {
		after := &model.Index{
			Root: "/after",
			Files: []model.FileSummary{
				{
					Path: "calc.go",
					Symbols: []model.Symbol{
						{
							File:      "calc.go",
							Kind:      "function_definition",
							Name:      "Add",
							Signature: "func Add(a, b, c int) int",
							StartLine: 3,
							EndLine:   5,
						},
					},
				},
			},
		}

		report := Compare(before, after)

		if report.Stats.ModifiedSymbols != 1 {
			t.Fatalf("expected 1 modified symbol, got %d", report.Stats.ModifiedSymbols)
		}
		if report.Stats.AddedSymbols != 0 {
			t.Fatalf("expected 0 added symbols, got %d", report.Stats.AddedSymbols)
		}
		if report.Stats.RemovedSymbols != 0 {
			t.Fatalf("expected 0 removed symbols, got %d", report.Stats.RemovedSymbols)
		}

		mod := report.ModifiedSymbols[0]
		if mod.Before.Signature != "func Add(a, b int) int" {
			t.Fatalf("unexpected before signature: %s", mod.Before.Signature)
		}
		if mod.After.Signature != "func Add(a, b, c int) int" {
			t.Fatalf("unexpected after signature: %s", mod.After.Signature)
		}
		if len(mod.Fields) != 1 || mod.Fields[0] != "signature" {
			t.Fatalf("expected fields [signature], got %v", mod.Fields)
		}
	})

	t.Run("span_change", func(t *testing.T) {
		after := &model.Index{
			Root: "/after",
			Files: []model.FileSummary{
				{
					Path: "calc.go",
					Symbols: []model.Symbol{
						{
							File:      "calc.go",
							Kind:      "function_definition",
							Name:      "Add",
							Signature: "func Add(a, b int) int",
							StartLine: 3,
							EndLine:   8,
						},
					},
				},
			},
		}

		report := Compare(before, after)

		if report.Stats.ModifiedSymbols != 1 {
			t.Fatalf("expected 1 modified symbol, got %d", report.Stats.ModifiedSymbols)
		}

		mod := report.ModifiedSymbols[0]
		if len(mod.Fields) != 1 || mod.Fields[0] != "span" {
			t.Fatalf("expected fields [span], got %v", mod.Fields)
		}
		if mod.Before.EndLine != 5 {
			t.Fatalf("expected before EndLine 5, got %d", mod.Before.EndLine)
		}
		if mod.After.EndLine != 8 {
			t.Fatalf("expected after EndLine 8, got %d", mod.After.EndLine)
		}
	})

	t.Run("signature_and_span_change", func(t *testing.T) {
		after := &model.Index{
			Root: "/after",
			Files: []model.FileSummary{
				{
					Path: "calc.go",
					Symbols: []model.Symbol{
						{
							File:      "calc.go",
							Kind:      "function_definition",
							Name:      "Add",
							Signature: "func Add(nums ...int) int",
							StartLine: 3,
							EndLine:   12,
						},
					},
				},
			},
		}

		report := Compare(before, after)

		if report.Stats.ModifiedSymbols != 1 {
			t.Fatalf("expected 1 modified symbol, got %d", report.Stats.ModifiedSymbols)
		}

		mod := report.ModifiedSymbols[0]
		if len(mod.Fields) != 2 {
			t.Fatalf("expected 2 modified fields, got %v", mod.Fields)
		}
		hasSignature := false
		hasSpan := false
		for _, f := range mod.Fields {
			if f == "signature" {
				hasSignature = true
			}
			if f == "span" {
				hasSpan = true
			}
		}
		if !hasSignature || !hasSpan {
			t.Fatalf("expected fields [signature, span], got %v", mod.Fields)
		}
	})
}

func TestCompareImportChanges(t *testing.T) {
	t.Run("added_imports", func(t *testing.T) {
		before := &model.Index{
			Root: "/before",
			Files: []model.FileSummary{
				{
					Path:    "app.go",
					Imports: []string{"fmt"},
				},
			},
		}

		after := &model.Index{
			Root: "/after",
			Files: []model.FileSummary{
				{
					Path:    "app.go",
					Imports: []string{"fmt", "os", "io"},
				},
			},
		}

		report := Compare(before, after)

		if len(report.ImportChanges) != 1 {
			t.Fatalf("expected 1 import change entry, got %d", len(report.ImportChanges))
		}
		ic := report.ImportChanges[0]
		if ic.File != "app.go" {
			t.Fatalf("expected import change for app.go, got %s", ic.File)
		}
		if len(ic.Added) != 2 {
			t.Fatalf("expected 2 added imports, got %d: %v", len(ic.Added), ic.Added)
		}
		// Added should be sorted
		if ic.Added[0] != "io" || ic.Added[1] != "os" {
			t.Fatalf("expected added [io, os], got %v", ic.Added)
		}
		if len(ic.Removed) != 0 {
			t.Fatalf("expected 0 removed imports, got %v", ic.Removed)
		}
	})

	t.Run("removed_imports", func(t *testing.T) {
		before := &model.Index{
			Root: "/before",
			Files: []model.FileSummary{
				{
					Path:    "app.go",
					Imports: []string{"fmt", "os", "log"},
				},
			},
		}

		after := &model.Index{
			Root: "/after",
			Files: []model.FileSummary{
				{
					Path:    "app.go",
					Imports: []string{"fmt"},
				},
			},
		}

		report := Compare(before, after)

		if len(report.ImportChanges) != 1 {
			t.Fatalf("expected 1 import change entry, got %d", len(report.ImportChanges))
		}
		ic := report.ImportChanges[0]
		if len(ic.Added) != 0 {
			t.Fatalf("expected 0 added imports, got %v", ic.Added)
		}
		if len(ic.Removed) != 2 {
			t.Fatalf("expected 2 removed imports, got %d: %v", len(ic.Removed), ic.Removed)
		}
		// Removed should be sorted
		if ic.Removed[0] != "log" || ic.Removed[1] != "os" {
			t.Fatalf("expected removed [log, os], got %v", ic.Removed)
		}
	})

	t.Run("new_file_all_imports_added", func(t *testing.T) {
		before := &model.Index{
			Root:  "/before",
			Files: []model.FileSummary{},
		}

		after := &model.Index{
			Root: "/after",
			Files: []model.FileSummary{
				{
					Path:    "new.go",
					Imports: []string{"net/http", "encoding/json"},
				},
			},
		}

		report := Compare(before, after)

		if len(report.ImportChanges) != 1 {
			t.Fatalf("expected 1 import change entry, got %d", len(report.ImportChanges))
		}
		ic := report.ImportChanges[0]
		if ic.File != "new.go" {
			t.Fatalf("expected import change for new.go, got %s", ic.File)
		}
		if len(ic.Added) != 2 {
			t.Fatalf("expected 2 added imports, got %v", ic.Added)
		}
		if len(ic.Removed) != 0 {
			t.Fatalf("expected 0 removed imports, got %v", ic.Removed)
		}
	})

	t.Run("deleted_file_all_imports_removed", func(t *testing.T) {
		before := &model.Index{
			Root: "/before",
			Files: []model.FileSummary{
				{
					Path:    "old.go",
					Imports: []string{"sync", "context"},
				},
			},
		}

		after := &model.Index{
			Root:  "/after",
			Files: []model.FileSummary{},
		}

		report := Compare(before, after)

		if len(report.ImportChanges) != 1 {
			t.Fatalf("expected 1 import change entry, got %d", len(report.ImportChanges))
		}
		ic := report.ImportChanges[0]
		if ic.File != "old.go" {
			t.Fatalf("expected import change for old.go, got %s", ic.File)
		}
		if len(ic.Added) != 0 {
			t.Fatalf("expected 0 added imports, got %v", ic.Added)
		}
		if len(ic.Removed) != 2 {
			t.Fatalf("expected 2 removed imports, got %v", ic.Removed)
		}
	})
}

func TestCompareMultipleFiles(t *testing.T) {
	before := &model.Index{
		Root: "/before",
		Files: []model.FileSummary{
			{
				Path:    "alpha.go",
				Imports: []string{"fmt"},
				Symbols: []model.Symbol{
					{
						File:      "alpha.go",
						Kind:      "function_definition",
						Name:      "AlphaFunc",
						Signature: "func AlphaFunc()",
						StartLine: 3,
						EndLine:   6,
					},
				},
			},
			{
				Path:    "beta.go",
				Imports: []string{"os"},
				Symbols: []model.Symbol{
					{
						File:      "beta.go",
						Kind:      "function_definition",
						Name:      "BetaFunc",
						Signature: "func BetaFunc()",
						StartLine: 2,
						EndLine:   4,
					},
					{
						File:      "beta.go",
						Kind:      "type_declaration",
						Name:      "BetaType",
						Signature: "type BetaType struct",
						StartLine: 6,
						EndLine:   10,
					},
				},
			},
			{
				Path: "gamma.go",
				Symbols: []model.Symbol{
					{
						File:      "gamma.go",
						Kind:      "function_definition",
						Name:      "GammaFunc",
						Signature: "func GammaFunc()",
						StartLine: 1,
						EndLine:   3,
					},
				},
			},
		},
	}

	after := &model.Index{
		Root: "/after",
		Files: []model.FileSummary{
			{
				Path:    "alpha.go",
				Imports: []string{"fmt", "log"},
				Symbols: []model.Symbol{
					{
						File:      "alpha.go",
						Kind:      "function_definition",
						Name:      "AlphaFunc",
						Signature: "func AlphaFunc(ctx context.Context)",
						StartLine: 3,
						EndLine:   8,
					},
				},
			},
			{
				Path:    "beta.go",
				Imports: []string{"os"},
				Symbols: []model.Symbol{
					{
						File:      "beta.go",
						Kind:      "function_definition",
						Name:      "BetaFunc",
						Signature: "func BetaFunc()",
						StartLine: 2,
						EndLine:   4,
					},
					// BetaType removed
				},
			},
			// gamma.go removed entirely
			{
				Path: "delta.go",
				Symbols: []model.Symbol{
					{
						File:      "delta.go",
						Kind:      "function_definition",
						Name:      "DeltaFunc",
						Signature: "func DeltaFunc()",
						StartLine: 1,
						EndLine:   5,
					},
				},
			},
		},
	}

	report := Compare(before, after)

	// AlphaFunc modified (signature + span), BetaType removed, GammaFunc removed, DeltaFunc added
	if report.Stats.AddedSymbols != 1 {
		t.Fatalf("expected 1 added symbol, got %d", report.Stats.AddedSymbols)
	}
	if report.Stats.RemovedSymbols != 2 {
		t.Fatalf("expected 2 removed symbols, got %d", report.Stats.RemovedSymbols)
	}
	if report.Stats.ModifiedSymbols != 1 {
		t.Fatalf("expected 1 modified symbol, got %d", report.Stats.ModifiedSymbols)
	}

	// Changed files: alpha.go (modified), beta.go (removed symbol), gamma.go (removed symbol), delta.go (added symbol), alpha.go (import change)
	// Unique files: alpha.go, beta.go, gamma.go, delta.go = 4
	if report.Stats.ChangedFiles != 4 {
		t.Fatalf("expected 4 changed files, got %d", report.Stats.ChangedFiles)
	}

	// Verify added symbol
	if report.AddedSymbols[0].Name != "DeltaFunc" {
		t.Fatalf("expected added DeltaFunc, got %s", report.AddedSymbols[0].Name)
	}
	if report.AddedSymbols[0].File != "delta.go" {
		t.Fatalf("expected added symbol in delta.go, got %s", report.AddedSymbols[0].File)
	}

	// Verify removed symbols are sorted by file then line
	removedNames := make([]string, len(report.RemovedSymbols))
	for i, r := range report.RemovedSymbols {
		removedNames[i] = r.Name
	}
	if report.RemovedSymbols[0].File != "beta.go" || report.RemovedSymbols[0].Name != "BetaType" {
		t.Fatalf("expected first removed symbol to be BetaType in beta.go, got %s in %s", report.RemovedSymbols[0].Name, report.RemovedSymbols[0].File)
	}
	if report.RemovedSymbols[1].File != "gamma.go" || report.RemovedSymbols[1].Name != "GammaFunc" {
		t.Fatalf("expected second removed symbol to be GammaFunc in gamma.go, got %s in %s", report.RemovedSymbols[1].Name, report.RemovedSymbols[1].File)
	}

	// Verify modified symbol
	mod := report.ModifiedSymbols[0]
	if mod.After.Name != "AlphaFunc" {
		t.Fatalf("expected modified AlphaFunc, got %s", mod.After.Name)
	}
	if len(mod.Fields) != 2 {
		t.Fatalf("expected 2 changed fields for AlphaFunc, got %v", mod.Fields)
	}

	// Verify import changes: alpha.go gained "log"
	if len(report.ImportChanges) != 1 {
		t.Fatalf("expected 1 import change entry, got %d", len(report.ImportChanges))
	}
	if report.ImportChanges[0].File != "alpha.go" {
		t.Fatalf("expected import change for alpha.go, got %s", report.ImportChanges[0].File)
	}
	if len(report.ImportChanges[0].Added) != 1 || report.ImportChanges[0].Added[0] != "log" {
		t.Fatalf("expected added import [log], got %v", report.ImportChanges[0].Added)
	}
}

func TestCompareEmptyIndexes(t *testing.T) {
	t.Run("both_empty", func(t *testing.T) {
		before := &model.Index{Root: "/empty1", Files: []model.FileSummary{}}
		after := &model.Index{Root: "/empty2", Files: []model.FileSummary{}}

		report := Compare(before, after)

		if report.Stats.AddedSymbols != 0 {
			t.Fatalf("expected 0 added symbols, got %d", report.Stats.AddedSymbols)
		}
		if report.Stats.RemovedSymbols != 0 {
			t.Fatalf("expected 0 removed symbols, got %d", report.Stats.RemovedSymbols)
		}
		if report.Stats.ModifiedSymbols != 0 {
			t.Fatalf("expected 0 modified symbols, got %d", report.Stats.ModifiedSymbols)
		}
		if report.Stats.ChangedFiles != 0 {
			t.Fatalf("expected 0 changed files, got %d", report.Stats.ChangedFiles)
		}
		if len(report.ImportChanges) != 0 {
			t.Fatalf("expected 0 import changes, got %d", len(report.ImportChanges))
		}
		if report.BeforeRoot != "/empty1" {
			t.Fatalf("expected BeforeRoot /empty1, got %s", report.BeforeRoot)
		}
		if report.AfterRoot != "/empty2" {
			t.Fatalf("expected AfterRoot /empty2, got %s", report.AfterRoot)
		}
	})

	t.Run("both_nil", func(t *testing.T) {
		report := Compare(nil, nil)

		if report.Stats.AddedSymbols != 0 {
			t.Fatalf("expected 0 added symbols, got %d", report.Stats.AddedSymbols)
		}
		if report.Stats.RemovedSymbols != 0 {
			t.Fatalf("expected 0 removed symbols, got %d", report.Stats.RemovedSymbols)
		}
		if report.Stats.ModifiedSymbols != 0 {
			t.Fatalf("expected 0 modified symbols, got %d", report.Stats.ModifiedSymbols)
		}
		if report.Stats.ChangedFiles != 0 {
			t.Fatalf("expected 0 changed files, got %d", report.Stats.ChangedFiles)
		}
		if report.BeforeRoot != "" {
			t.Fatalf("expected empty BeforeRoot, got %s", report.BeforeRoot)
		}
		if report.AfterRoot != "" {
			t.Fatalf("expected empty AfterRoot, got %s", report.AfterRoot)
		}
	})

	t.Run("nil_before_populated_after", func(t *testing.T) {
		after := &model.Index{
			Root: "/after",
			Files: []model.FileSummary{
				{
					Path:    "new.go",
					Imports: []string{"fmt"},
					Symbols: []model.Symbol{
						{
							File:      "new.go",
							Kind:      "function_definition",
							Name:      "New",
							Signature: "func New()",
							StartLine: 1,
							EndLine:   3,
						},
					},
				},
			},
		}

		report := Compare(nil, after)

		if report.Stats.AddedSymbols != 1 {
			t.Fatalf("expected 1 added symbol, got %d", report.Stats.AddedSymbols)
		}
		if report.Stats.RemovedSymbols != 0 {
			t.Fatalf("expected 0 removed symbols, got %d", report.Stats.RemovedSymbols)
		}
		if report.BeforeRoot != "" {
			t.Fatalf("expected empty BeforeRoot, got %s", report.BeforeRoot)
		}
		if len(report.ImportChanges) != 1 {
			t.Fatalf("expected 1 import change, got %d", len(report.ImportChanges))
		}
	})

	t.Run("populated_before_nil_after", func(t *testing.T) {
		before := &model.Index{
			Root: "/before",
			Files: []model.FileSummary{
				{
					Path:    "old.go",
					Imports: []string{"os"},
					Symbols: []model.Symbol{
						{
							File:      "old.go",
							Kind:      "function_definition",
							Name:      "Old",
							Signature: "func Old()",
							StartLine: 1,
							EndLine:   3,
						},
					},
				},
			},
		}

		report := Compare(before, nil)

		if report.Stats.AddedSymbols != 0 {
			t.Fatalf("expected 0 added symbols, got %d", report.Stats.AddedSymbols)
		}
		if report.Stats.RemovedSymbols != 1 {
			t.Fatalf("expected 1 removed symbol, got %d", report.Stats.RemovedSymbols)
		}
		if report.AfterRoot != "" {
			t.Fatalf("expected empty AfterRoot, got %s", report.AfterRoot)
		}
		if len(report.ImportChanges) != 1 {
			t.Fatalf("expected 1 import change, got %d", len(report.ImportChanges))
		}
	})
}
