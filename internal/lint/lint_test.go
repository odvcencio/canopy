package lint

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/odvcencio/gts-suite/internal/deps"
	"github.com/odvcencio/gts-suite/pkg/model"
)

func TestParseRule_MaxLinesFunction(t *testing.T) {
	rule, err := ParseRule("no function longer than 50 lines")
	if err != nil {
		t.Fatalf("ParseRule returned error: %v", err)
	}
	if rule.Type != "max_lines" {
		t.Fatalf("unexpected rule type %q", rule.Type)
	}
	if rule.Kind != "function_definition" {
		t.Fatalf("unexpected kind %q", rule.Kind)
	}
	if rule.MaxLines != 50 {
		t.Fatalf("unexpected max lines %d", rule.MaxLines)
	}
}

func TestParseRule_Unsupported(t *testing.T) {
	_, err := ParseRule("ban globals")
	if err == nil {
		t.Fatal("expected ParseRule to fail for unsupported format")
	}
}

func TestEvaluate_MaxLinesViolations(t *testing.T) {
	idx := &model.Index{
		Files: []model.FileSummary{
			{
				Path: "main.go",
				Symbols: []model.Symbol{
					{
						File:      "main.go",
						Kind:      "function_definition",
						Name:      "Short",
						StartLine: 10,
						EndLine:   12,
					},
					{
						File:      "main.go",
						Kind:      "function_definition",
						Name:      "Long",
						StartLine: 20,
						EndLine:   40,
					},
				},
			},
		},
	}

	rule, err := ParseRule("no function longer than 5 lines")
	if err != nil {
		t.Fatalf("ParseRule returned error: %v", err)
	}

	violations := Evaluate(idx, []Rule{rule})
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}
	if violations[0].Name != "Long" {
		t.Fatalf("unexpected violation: %+v", violations[0])
	}
}

func TestParseRule_NoImport(t *testing.T) {
	rule, err := ParseRule(`no import "fmt"`)
	if err != nil {
		t.Fatalf("ParseRule returned error: %v", err)
	}
	if rule.Type != "no_import" {
		t.Fatalf("unexpected rule type %q", rule.Type)
	}
	if rule.ImportPath != "fmt" {
		t.Fatalf("unexpected import path %q", rule.ImportPath)
	}
}

func TestEvaluate_NoImportViolation(t *testing.T) {
	idx := &model.Index{
		Files: []model.FileSummary{
			{
				Path:    "main.go",
				Imports: []string{"fmt", "strings"},
			},
		},
	}
	rule, err := ParseRule("no import fmt")
	if err != nil {
		t.Fatalf("ParseRule returned error: %v", err)
	}
	violations := Evaluate(idx, []Rule{rule})
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}
	if violations[0].Kind != "import" || violations[0].Name != "fmt" {
		t.Fatalf("unexpected violation: %+v", violations[0])
	}
}

func TestLoadQueryPatternMetadata(t *testing.T) {
	tmpDir := t.TempDir()
	patternPath := filepath.Join(tmpDir, "rule.scm")
	content := `; id: no-empty-functions
; message: avoid empty function bodies
(function_declaration (block) @violation)
`
	if err := os.WriteFile(patternPath, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	pattern, err := LoadQueryPattern(patternPath)
	if err != nil {
		t.Fatalf("LoadQueryPattern returned error: %v", err)
	}
	if pattern.ID != "no-empty-functions" {
		t.Fatalf("unexpected pattern id %q", pattern.ID)
	}
	if pattern.Message != "avoid empty function bodies" {
		t.Fatalf("unexpected pattern message %q", pattern.Message)
	}
}

func TestEvaluatePatterns(t *testing.T) {
	tmpDir := t.TempDir()
	sourcePath := filepath.Join(tmpDir, "main.go")
	source := `package sample

func Empty() {}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0o644); err != nil {
		t.Fatalf("WriteFile source failed: %v", err)
	}

	patternPath := filepath.Join(tmpDir, "empty.scm")
	patternBody := `(function_declaration (block) @violation)`
	if err := os.WriteFile(patternPath, []byte(patternBody), 0o644); err != nil {
		t.Fatalf("WriteFile pattern failed: %v", err)
	}

	pattern, err := LoadQueryPattern(patternPath)
	if err != nil {
		t.Fatalf("LoadQueryPattern returned error: %v", err)
	}

	idx := &model.Index{
		Root: tmpDir,
		Files: []model.FileSummary{
			{
				Path:     "main.go",
				Language: "go",
			},
		},
	}

	violations, err := EvaluatePatterns(idx, []QueryPattern{pattern})
	if err != nil {
		t.Fatalf("EvaluatePatterns returned error: %v", err)
	}
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}
	if violations[0].RuleID != pattern.ID {
		t.Fatalf("unexpected rule id %q", violations[0].RuleID)
	}
	if violations[0].Kind != "query_pattern" {
		t.Fatalf("unexpected violation kind %q", violations[0].Kind)
	}
}

func TestEvaluatePackageRules_ExportedSymbols(t *testing.T) {
	idx := &model.Index{
		Files: []model.FileSummary{
			{
				Path: "pkg/api/handler.go",
				Symbols: []model.Symbol{
					{File: "pkg/api/handler.go", Kind: "function_definition", Name: "HandleRequest"},
					{File: "pkg/api/handler.go", Kind: "function_definition", Name: "parseBody"},
					{File: "pkg/api/handler.go", Kind: "type_definition", Name: "Server"},
				},
			},
			{
				Path: "pkg/api/middleware.go",
				Symbols: []model.Symbol{
					{File: "pkg/api/middleware.go", Kind: "function_definition", Name: "Wrap"},
					{File: "pkg/api/middleware.go", Kind: "function_definition", Name: "logRequest"},
				},
			},
		},
	}

	rules := []PackageRule{
		{
			Metric:    "exported_symbols",
			Threshold: 2,
			Severity:  "warn",
			Message:   "too many exported symbols",
		},
	}

	violations, err := EvaluatePackageRules(idx, rules, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}
	v := violations[0]
	if v.RuleID != "package/exported_symbols" {
		t.Fatalf("unexpected rule id %q", v.RuleID)
	}
	if v.Kind != "package" {
		t.Fatalf("unexpected kind %q", v.Kind)
	}
	if v.Value != 3 {
		t.Fatalf("expected value 3, got %d", v.Value)
	}
}

func TestEvaluatePackageRules_ExportedSymbols_Scoped(t *testing.T) {
	idx := &model.Index{
		Files: []model.FileSummary{
			{
				Path: "pkg/api/handler.go",
				Symbols: []model.Symbol{
					{File: "pkg/api/handler.go", Kind: "function_definition", Name: "HandleRequest"},
					{File: "pkg/api/handler.go", Kind: "type_definition", Name: "Server"},
					{File: "pkg/api/handler.go", Kind: "function_definition", Name: "Route"},
				},
			},
			{
				Path: "internal/core/engine.go",
				Symbols: []model.Symbol{
					{File: "internal/core/engine.go", Kind: "function_definition", Name: "Run"},
					{File: "internal/core/engine.go", Kind: "type_definition", Name: "Engine"},
					{File: "internal/core/engine.go", Kind: "function_definition", Name: "Start"},
				},
			},
		},
	}

	rules := []PackageRule{
		{
			Metric:    "exported_symbols",
			Threshold: 2,
			Severity:  "warn",
			Message:   "too many exported symbols",
			Scope:     "pkg/*",
		},
	}

	violations, err := EvaluatePackageRules(idx, rules, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Only pkg/api should be checked; internal/core is outside the scope.
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation (pkg/api only), got %d", len(violations))
	}
	if violations[0].File != "pkg/api" {
		t.Fatalf("expected violation for pkg/api, got %q", violations[0].File)
	}
}

func TestEvaluatePackageRules_ImportDepth(t *testing.T) {
	// Chain: a -> b -> c. Roots: a (no incoming). Depths: a=0, b=1, c=2.
	edges := []deps.Edge{
		{From: "a", To: "b", Internal: true},
		{From: "b", To: "c", Internal: true},
	}

	rules := []PackageRule{
		{
			Metric:    "import_depth",
			Threshold: 1,
			Severity:  "error",
			Message:   "dependency chain too deep",
		},
	}

	violations, err := EvaluatePackageRules(&model.Index{}, rules, edges)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}
	v := violations[0]
	if v.File != "c" {
		t.Fatalf("expected violation for package 'c', got %q", v.File)
	}
	if v.Value != 2 {
		t.Fatalf("expected depth 2, got %d", v.Value)
	}
	if v.RuleID != "package/import_depth" {
		t.Fatalf("unexpected rule id %q", v.RuleID)
	}
}

func TestEvaluatePackageRules_NoImportCycles_WithCycle(t *testing.T) {
	edges := []deps.Edge{
		{From: "a", To: "b", Internal: true},
		{From: "b", To: "a", Internal: true},
	}

	rules := []PackageRule{
		{
			Metric:      "no_import_cycles",
			Severity:    "error",
			Message:     "import cycle detected",
			Enforcement: true,
		},
	}

	violations, err := EvaluatePackageRules(&model.Index{}, rules, edges)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(violations) == 0 {
		t.Fatal("expected at least 1 violation for cycle a<->b, got 0")
	}
	if violations[0].RuleID != "package/no_import_cycles" {
		t.Fatalf("unexpected rule id %q", violations[0].RuleID)
	}
	if violations[0].Kind != "package" {
		t.Fatalf("unexpected kind %q", violations[0].Kind)
	}
}

func TestEvaluatePackageRules_NoImportCycles_Clean(t *testing.T) {
	edges := []deps.Edge{
		{From: "a", To: "b", Internal: true},
		{From: "b", To: "c", Internal: true},
	}

	rules := []PackageRule{
		{
			Metric:      "no_import_cycles",
			Severity:    "error",
			Message:     "import cycle detected",
			Enforcement: true,
		},
	}

	violations, err := EvaluatePackageRules(&model.Index{}, rules, edges)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(violations) != 0 {
		t.Fatalf("expected 0 violations for clean graph, got %d", len(violations))
	}
}

func TestEvaluateThresholds_ScopedRule(t *testing.T) {
	// Create a minimal index with files in two different packages.
	// We can't easily test the full EvaluateThresholds because it needs
	// real complexity analysis, so we test the scope filtering via matchPkgGlob.
	tests := []struct {
		name    string
		scope   string
		filePkg string
		want    bool
	}{
		{"exact match", "pkg/api", "pkg/api", true},
		{"no match", "pkg/api", "internal/core", false},
		{"wildcard prefix", "pkg/*", "pkg/api", true},
		{"wildcard prefix no match", "pkg/*", "internal/core", false},
		{"global wildcard", "*", "anything", true},
		{"double star", "**", "anything/deep/nested", true},
		{"empty scope matches nothing", "", "pkg/api", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchPkgGlob(tt.scope, tt.filePkg)
			if got != tt.want {
				t.Errorf("matchPkgGlob(%q, %q) = %v, want %v", tt.scope, tt.filePkg, got, tt.want)
			}
		})
	}
}

func TestIsExported(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"HandleRequest", true},
		{"parseBody", false},
		{"Server", true},
		{"logRequest", false},
		{"X", true},
		{"x", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isExported(tt.name)
			if got != tt.want {
				t.Errorf("isExported(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}
