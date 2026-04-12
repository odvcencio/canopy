package impact

import (
	"testing"

	"github.com/odvcencio/canopy/pkg/model"
)

func TestAnalyzeEmpty(t *testing.T) {
	idx := &model.Index{
		Files: []model.FileSummary{},
	}
	result, err := Analyze(idx, Options{Changed: []string{"nonexistent"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.TotalAffected != 0 {
		t.Errorf("expected 0 affected, got %d", result.TotalAffected)
	}
	if len(result.AffectedFiles) != 0 {
		t.Errorf("expected 0 affected files, got %d", len(result.AffectedFiles))
	}
}

func TestAnalyzeDirectCaller(t *testing.T) {
	idx := &model.Index{
		Files: []model.FileSummary{
			{
				Path:     "handler.go",
				Language: "go",
				Symbols: []model.Symbol{
					{File: "handler.go", Kind: "function_definition", Name: "handler", StartLine: 1, EndLine: 10},
					{File: "handler.go", Kind: "function_definition", Name: "validate", StartLine: 12, EndLine: 20},
				},
				References: []model.Reference{
					{File: "handler.go", Kind: "reference.call", Name: "validate", StartLine: 5, EndLine: 5},
				},
			},
		},
	}

	result, err := Analyze(idx, Options{Changed: []string{"validate"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.TotalAffected != 1 {
		t.Fatalf("expected 1 affected, got %d", result.TotalAffected)
	}
	if result.Affected[0].Name != "handler" {
		t.Errorf("expected affected symbol 'handler', got %q", result.Affected[0].Name)
	}
	if result.Affected[0].Distance != 1 {
		t.Errorf("expected distance 1, got %d", result.Affected[0].Distance)
	}
}

func TestAnalyzeTransitive(t *testing.T) {
	idx := &model.Index{
		Files: []model.FileSummary{
			{
				Path:     "a.go",
				Language: "go",
				Symbols: []model.Symbol{
					{File: "a.go", Kind: "function_definition", Name: "a", StartLine: 1, EndLine: 10},
				},
				References: []model.Reference{
					{File: "a.go", Kind: "reference.call", Name: "b", StartLine: 5, EndLine: 5},
				},
			},
			{
				Path:     "b.go",
				Language: "go",
				Symbols: []model.Symbol{
					{File: "b.go", Kind: "function_definition", Name: "b", StartLine: 1, EndLine: 10},
				},
				References: []model.Reference{
					{File: "b.go", Kind: "reference.call", Name: "c", StartLine: 5, EndLine: 5},
				},
			},
			{
				Path:     "c.go",
				Language: "go",
				Symbols: []model.Symbol{
					{File: "c.go", Kind: "function_definition", Name: "c", StartLine: 1, EndLine: 10},
				},
			},
		},
	}

	result, err := Analyze(idx, Options{Changed: []string{"c"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.TotalAffected != 2 {
		t.Fatalf("expected 2 affected, got %d", result.TotalAffected)
	}

	// Sorted by distance: b at distance 1, a at distance 2.
	found := map[string]int{}
	for _, sym := range result.Affected {
		found[sym.Name] = sym.Distance
	}
	if found["b"] != 1 {
		t.Errorf("expected b at distance 1, got %d", found["b"])
	}
	if found["a"] != 2 {
		t.Errorf("expected a at distance 2, got %d", found["a"])
	}
}

func TestAnalyzeRiskScore(t *testing.T) {
	idx := &model.Index{
		Files: []model.FileSummary{
			{
				Path:     "handler.go",
				Language: "go",
				Symbols: []model.Symbol{
					{File: "handler.go", Kind: "function_definition", Name: "handler", StartLine: 1, EndLine: 10},
					{File: "handler.go", Kind: "function_definition", Name: "validate", StartLine: 12, EndLine: 20},
				},
				References: []model.Reference{
					{File: "handler.go", Kind: "reference.call", Name: "validate", StartLine: 5, EndLine: 5},
				},
			},
		},
	}

	result, err := Analyze(idx, Options{Changed: []string{"validate"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.TotalAffected != 1 {
		t.Fatalf("expected 1 affected, got %d", result.TotalAffected)
	}

	// Distance 1 -> risk = 1.0/(1+1) = 0.5
	risk := result.Affected[0].Risk
	if risk < 0.49 || risk > 0.51 {
		t.Errorf("expected risk ~0.5 for distance 1, got %f", risk)
	}
}

func TestMatchDiffToSymbols(t *testing.T) {
	idx := &model.Index{
		Files: []model.FileSummary{
			{
				Path:     "server.go",
				Language: "go",
				Symbols: []model.Symbol{
					{File: "server.go", Kind: "function_definition", Name: "handleRequest", StartLine: 10, EndLine: 30},
					{File: "server.go", Kind: "function_definition", Name: "startServer", StartLine: 32, EndLine: 50},
					{File: "server.go", Kind: "type_definition", Name: "Config", StartLine: 1, EndLine: 8},
				},
			},
			{
				Path:     "utils.go",
				Language: "go",
				Symbols: []model.Symbol{
					{File: "utils.go", Kind: "function_definition", Name: "parseArgs", StartLine: 5, EndLine: 20},
				},
			},
		},
	}

	diffText := `diff --git a/server.go b/server.go
index abc1234..def5678 100644
--- a/server.go
+++ b/server.go
@@ -15,3 +15,4 @@ func handleRequest() {
+    // added line
@@ -35,1 +36,1 @@ func startServer() {
-    old line
+    new line
diff --git a/utils.go b/utils.go
index abc1234..def5678 100644
--- a/utils.go
+++ b/utils.go
@@ -100,2 +100,2 @@ func other() {
-    old
+    new
`

	names := MatchDiffToSymbols(idx, diffText)

	nameSet := map[string]bool{}
	for _, n := range names {
		nameSet[n] = true
	}

	if !nameSet["handleRequest"] {
		t.Errorf("expected handleRequest in changed symbols, got %v", names)
	}
	if !nameSet["startServer"] {
		t.Errorf("expected startServer in changed symbols, got %v", names)
	}
	// Config is a type_definition, not callable, so it should not be included.
	if nameSet["Config"] {
		t.Errorf("did not expect Config (type_definition) in changed symbols, got %v", names)
	}
	// parseArgs is at lines 5-20, diff hunk at line 100 is outside its range.
	if nameSet["parseArgs"] {
		t.Errorf("did not expect parseArgs (out of range) in changed symbols, got %v", names)
	}
}
